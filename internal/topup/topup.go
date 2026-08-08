// Package topup 管充值单：起单 → 支付网关 webhook → 到账。
//
// 起单**不即时到账** —— 只落一行 pending topup_order + 一个 pay_url，等 waffo
// webhook 到才 MarkPaid，那时才落 wallet_ledger 两条：recharge + channel_fee。
// 阶段 1a 没接真 waffo，pay_url 是 mock URL，MarkPaid 由内部端点触发。
//
// 通道费口径按 CLAUDE.md §1.4：credits × 5% 是加在本金上（不是含在总额里）。
// 一次充值两条流水：recharge +（credits+fee） 和 channel_fee -fee，净 +credits。
package topup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/wallet"
	"github.com/google/uuid"
)

// ChannelFeeBps 通道费率（万分之几）。500 = 5%。跟前端 utils.ts CHANNEL_FEE_RATE 对齐。
// 定 const 而不是配置：waffo pass-through 5% 是跟通道商谈定的合同数字，不做运营开关。
const ChannelFeeBps int64 = 500

// USDRateCNY 展示层的 CNY / USD 汇率（后端记账单位始终是积分 · 这里只算展示用的美元金额）。
// 跟 CLAUDE.md §1.4「7 CNY / USD」对齐。
const USDRateCNY = 7

var (
	ErrNotFound           = errors.New("topup: 充值单不存在")
	ErrForbidden          = errors.New("topup: 无权访问该充值单")
	ErrInvalidAmount      = errors.New("topup: 充值积分必须为正")
	ErrUnsupportedChannel = errors.New("topup: 暂不支持这个支付通道")
	ErrOrderNotPending    = errors.New("topup: 充值单已结算或过期")
	ErrExpired            = errors.New("topup: 充值单已过期")
)

// Status 是充值单的内部状态。对外收敛见 API 层。
type Status string

const (
	StatusPending   Status = "pending"
	StatusPaid      Status = "paid"
	StatusExpired   Status = "expired"
	StatusCancelled Status = "cancelled"
)

// Order 一张充值单的内部形状。
type Order struct {
	ID             string
	PassengerID    string
	Channel        string
	Credits        int64 // 净到账积分
	ChannelFee     int64 // 通道费
	Paid           int64 // 用户付的总积分 = Credits + ChannelFee
	PayURL         string
	Status         Status
	ExpiresAt      time.Time
	PaidAt         time.Time
	WalletLedgerID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Breakdown 是给起单时算钱的辅助结构。
//
// credits = 目标积分（乘客想充的数字）
// fee     = credits × 5%
// paid    = credits + fee（乘客真花的总积分，等值 CNY）
// paidUSD = paid / 7（展示给乘客看的美元数字，只是数字，非落库口径）
type Breakdown struct {
	Credits    int64
	ChannelFee int64
	Paid       int64
}

// BreakdownFor 根据目标积分算通道费和总付款。
// 用整除，避免舍入把用户占便宜或占亏 —— 5% × credits 通常刚好整。
func BreakdownFor(credits int64) Breakdown {
	fee := credits * ChannelFeeBps / 10000
	return Breakdown{
		Credits:    credits,
		ChannelFee: fee,
		Paid:       credits + fee,
	}
}

// Store 是充值单的持久化封装。
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// CreateOrder 起一张充值单。
//
// mock 阶段 payURL 由调用方提供（api 层构造成 https://waffo.example/order/{id}）。
// TTL 15 分钟：过期后 janitor 应扫过来把 status 改成 expired（阶段 1a 允许粗糙，
// 让重复 Get 时看到 expired 也行 —— 前端只需要 expires_at）。
func (s *Store) CreateOrder(ctx context.Context, passengerID, channel string, credits int64, payURL string, ttl time.Duration) (Order, error) {
	if credits <= 0 {
		return Order{}, ErrInvalidAmount
	}
	if channel != "waffo" {
		return Order{}, ErrUnsupportedChannel
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}

	b := BreakdownFor(credits)
	now := time.Now().UTC()
	id := uuid.NewString()
	exp := now.Add(ttl)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO topup_order
		  (id, passenger_id, channel, credits, channel_fee, paid, pay_url,
		   status, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)`,
		id, passengerID, channel, b.Credits, b.ChannelFee, b.Paid, payURL,
		formatTime(exp), formatTime(now), formatTime(now))
	if err != nil {
		return Order{}, fmt.Errorf("topup: 插入充值单: %w", err)
	}

	return Order{
		ID: id, PassengerID: passengerID, Channel: channel,
		Credits: b.Credits, ChannelFee: b.ChannelFee, Paid: b.Paid,
		PayURL: payURL, Status: StatusPending, ExpiresAt: exp,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// MarkPaid webhook 到账时调用。原子做三件事：
//  1. 条件 UPDATE topup_order status pending → paid（幂等：非 pending 直接 return）
//  2. Credit 一条 recharge = paid
//  3. Debit 一条 channel_fee = fee
//
// 净变化 = +credits。两条流水在一个事务里，避免"到账了但通道费没扣"或反之。
func (s *Store) MarkPaid(ctx context.Context, orderID string) (Order, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, fmt.Errorf("topup: 开事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		passengerID string
		credits     int64
		channelFee  int64
		paid        int64
		status      string
		expiresAt   string
	)
	err = tx.QueryRowContext(ctx,
		`SELECT passenger_id, credits, channel_fee, paid, status, expires_at
		 FROM topup_order WHERE id = ?`, orderID).
		Scan(&passengerID, &credits, &channelFee, &paid, &status, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("topup: 查订单: %w", err)
	}

	// 幂等：已 paid 直接返回当前订单（webhook 常规重发）
	if status == string(StatusPaid) {
		return s.getInTx(ctx, tx, orderID)
	}
	if status != string(StatusPending) {
		return Order{}, ErrOrderNotPending
	}

	// 条件 UPDATE：只在 pending 才改，防并发重入
	now := time.Now().UTC()
	nowStr := formatTime(now)
	res, err := tx.ExecContext(ctx, `
		UPDATE topup_order
		   SET status = 'paid', paid_at = ?, updated_at = ?
		 WHERE id = ? AND status = 'pending'`,
		nowStr, nowStr, orderID)
	if err != nil {
		return Order{}, fmt.Errorf("topup: 标记 paid: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Order{}, ErrOrderNotPending
	}

	// 保证钱包存在（充值有可能是新用户第一次入账）
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet (passenger_id, balance, reserved, updated_at)
		VALUES (?, 0, 0, ?)
		ON CONFLICT (passenger_id) DO NOTHING`,
		passengerID, nowStr); err != nil {
		return Order{}, fmt.Errorf("topup: 保证钱包: %w", err)
	}

	// 两条流水必须落进同一事务：recharge +paid 然后 channel_fee -fee
	rechargeEntry, err := wallet.ApplyTx(ctx, tx, wallet.Move{
		PassengerID: passengerID,
		Reason:      wallet.ReasonRecharge,
		Amount:      paid,
		RefType:     "topup_order",
		RefID:       orderID,
		Memo:        "充值到账",
	}, +1)
	if err != nil {
		return Order{}, err
	}
	if channelFee > 0 {
		if _, err := wallet.ApplyTx(ctx, tx, wallet.Move{
			PassengerID: passengerID,
			Reason:      wallet.ReasonChannelFee,
			Amount:      channelFee,
			RefType:     "topup_order",
			RefID:       orderID,
			Memo:        "通道费（waffo 5%）",
		}, -1); err != nil {
			return Order{}, err
		}
	}

	// 反填 wallet_ledger_id（用来给对账/售后追）—— 指向 recharge 那条
	if _, err := tx.ExecContext(ctx,
		`UPDATE topup_order SET wallet_ledger_id = ? WHERE id = ?`,
		rechargeEntry.ID, orderID); err != nil {
		return Order{}, fmt.Errorf("topup: 反填流水 id: %w", err)
	}

	out, err := s.getInTx(ctx, tx, orderID)
	if err != nil {
		return Order{}, err
	}
	if err := tx.Commit(); err != nil {
		return Order{}, fmt.Errorf("topup: 提交: %w", err)
	}
	return out, nil
}

// Get 查一张充值单（校验属主，防串号）。
func (s *Store) Get(ctx context.Context, passengerID, orderID string) (Order, error) {
	o, err := s.getBy(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if o.PassengerID != passengerID {
		return Order{}, ErrForbidden
	}
	return o, nil
}

func (s *Store) getBy(ctx context.Context, orderID string) (Order, error) {
	var (
		o         Order
		expiresAt string
		createdAt string
		updatedAt string
		paidAt    sql.NullString
		ledgerID  sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, passenger_id, channel, credits, channel_fee, paid, pay_url,
		       status, expires_at, paid_at, wallet_ledger_id, created_at, updated_at
		FROM topup_order WHERE id = ?`, orderID).Scan(
		&o.ID, &o.PassengerID, &o.Channel, &o.Credits, &o.ChannelFee, &o.Paid,
		&o.PayURL, (*string)(&o.Status), &expiresAt, &paidAt, &ledgerID,
		&createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("topup: 查订单: %w", err)
	}
	o.ExpiresAt = parseTime(expiresAt)
	o.CreatedAt = parseTime(createdAt)
	o.UpdatedAt = parseTime(updatedAt)
	if paidAt.Valid {
		o.PaidAt = parseTime(paidAt.String)
	}
	if ledgerID.Valid {
		o.WalletLedgerID = ledgerID.String
	}
	return o, nil
}

func (s *Store) getInTx(ctx context.Context, tx *sql.Tx, orderID string) (Order, error) {
	var (
		o         Order
		expiresAt string
		createdAt string
		updatedAt string
		paidAt    sql.NullString
		ledgerID  sql.NullString
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, passenger_id, channel, credits, channel_fee, paid, pay_url,
		       status, expires_at, paid_at, wallet_ledger_id, created_at, updated_at
		FROM topup_order WHERE id = ?`, orderID).Scan(
		&o.ID, &o.PassengerID, &o.Channel, &o.Credits, &o.ChannelFee, &o.Paid,
		&o.PayURL, (*string)(&o.Status), &expiresAt, &paidAt, &ledgerID,
		&createdAt, &updatedAt)
	if err != nil {
		return Order{}, fmt.Errorf("topup: 事务内查订单: %w", err)
	}
	o.ExpiresAt = parseTime(expiresAt)
	o.CreatedAt = parseTime(createdAt)
	o.UpdatedAt = parseTime(updatedAt)
	if paidAt.Valid {
		o.PaidAt = parseTime(paidAt.String)
	}
	if ledgerID.Valid {
		o.WalletLedgerID = ledgerID.String
	}
	return o, nil
}

// ListOptions 分页 + 状态过滤。
type ListOptions struct {
	Status Status // 空 = 不筛
	Limit  int
	Offset int
}

// List 查某乘客的充值单历史，倒序返回。
func (s *Store) List(ctx context.Context, passengerID string, opt ListOptions) ([]Order, int, error) {
	if opt.Limit <= 0 || opt.Limit > 500 {
		opt.Limit = 50
	}
	if opt.Offset < 0 {
		opt.Offset = 0
	}
	where := `WHERE passenger_id = ?`
	args := []any{passengerID}
	if opt.Status != "" {
		where += ` AND status = ?`
		args = append(args, string(opt.Status))
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(1) FROM topup_order `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("topup: 统计: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, passenger_id, channel, credits, channel_fee, paid, pay_url,
		       status, expires_at, COALESCE(paid_at,''), COALESCE(wallet_ledger_id,''),
		       created_at, updated_at
		FROM topup_order `+where+`
		ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, opt.Limit, opt.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("topup: 查列表: %w", err)
	}
	defer rows.Close()

	var out []Order
	for rows.Next() {
		var (
			o         Order
			expiresAt string
			paidAt    string
			ledgerID  string
			createdAt string
			updatedAt string
		)
		if err := rows.Scan(
			&o.ID, &o.PassengerID, &o.Channel, &o.Credits, &o.ChannelFee, &o.Paid,
			&o.PayURL, (*string)(&o.Status), &expiresAt, &paidAt, &ledgerID,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, 0, err
		}
		o.ExpiresAt = parseTime(expiresAt)
		o.CreatedAt = parseTime(createdAt)
		o.UpdatedAt = parseTime(updatedAt)
		if paidAt != "" {
			o.PaidAt = parseTime(paidAt)
		}
		o.WalletLedgerID = ledgerID
		out = append(out, o)
	}
	return out, total, rows.Err()
}

// ExpirePending 后台清扫用：把过了 expires_at 的 pending 单标 expired。
// 阶段 1a mock 场景可以先不接 janitor，返回受影响行数够用了。
func (s *Store) ExpirePending(ctx context.Context) (int64, error) {
	now := formatTime(time.Now().UTC())
	res, err := s.db.ExecContext(ctx, `
		UPDATE topup_order
		   SET status = 'expired', updated_at = ?
		 WHERE status = 'pending' AND expires_at < ?`, now, now)
	if err != nil {
		return 0, fmt.Errorf("topup: 扫过期: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

const timeLayout = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, l := range []string{timeLayout, time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
