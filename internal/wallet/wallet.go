// Package wallet 管积分余额和流水。
//
// 两条不变量，靠 SQLite 事务 + CHECK 双保险：
//  1. **balance 变更和 ledger 插入必须同事务** —— 否则会出现"钱扣了但流水没记"，
//     对账时说不清
//  2. **balance / reserved 永不为负** —— 条件 UPDATE（`WHERE balance >= ?`）挡第一层，
//     表上的 CHECK 挡第二层。并发下靠 BEGIN IMMEDIATE 串行化（见 internal/db）
//
// 金额单位一律 **整数 microunit**（1 积分 = 1_000_000 · CLAUDE.md §7.2），
// 不用浮点 —— 钱不能有舍入漂移。
package wallet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInsufficientBalance  = errors.New("wallet: 余额不足")
	ErrInsufficientReserved = errors.New("wallet: 冻结额不足")
	ErrNotFound             = errors.New("wallet: 钱包不存在")
	ErrNonPositiveAmount    = errors.New("wallet: 金额必须为正")
)

// Reason 是流水类型（05-api-contract §3 的 ?type= 枚举）。
type Reason string

const (
	ReasonRecharge       Reason = "recharge"
	ReasonChannelFee     Reason = "channel_fee"
	ReasonRedeem         Reason = "redeem"
	ReasonKeyCost        Reason = "key_cost"
	ReasonSinglePullFee  Reason = "single_pull_fee"
	ReasonCapabilityFee  Reason = "capability_fee"
	ReasonServiceFee     Reason = "service_fee"
	ReasonVendorFee      Reason = "vendor_fee"
	ReasonRegionFee      Reason = "region_fee"
	ReasonWarrantyRefund Reason = "warranty_refund"
	ReasonAdminAdjust    Reason = "admin_adjust"
	// ReasonTopupRefund 充值单被 gateway 退款/反向结算·反向 recharge + channel_fee 两条·
	// 净变化 = -credits。触发条件：gateway 送 kind=refunded / reversed 的 settlement 事件。
	ReasonTopupRefund Reason = "topup_refund"
)

type Balance struct {
	Balance  int64 // 可用
	Reserved int64 // 冻结中（拉号进行中）
	Updated  time.Time
}

// Available 是真正能花的（余额已经不含 reserved —— reserve 时就从 balance 挪走了）
func (b Balance) Available() int64 { return b.Balance }

type Entry struct {
	ID           string
	Seq          int64
	Reason       Reason
	Amount       int64 // 带符号：正 = 入账，负 = 出账
	BalanceAfter int64
	RefType      string
	RefID        string
	Memo         string
	CreatedAt    time.Time
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Get(ctx context.Context, passengerID string) (Balance, error) {
	var b Balance
	var updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT balance, reserved, updated_at FROM wallet WHERE passenger_id = ?`,
		passengerID).Scan(&b.Balance, &b.Reserved, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Balance{}, ErrNotFound
	}
	if err != nil {
		return Balance{}, fmt.Errorf("wallet: 查余额: %w", err)
	}
	b.Updated = parseTime(updated)
	return b, nil
}

// Move 描述一次记账。
type Move struct {
	PassengerID string
	Reason      Reason
	// Amount 恒为**正数** —— 方向由 Debit / Credit 决定，避免调用方传错符号
	Amount  int64
	RefType string
	RefID   string
	Memo    string
}

// Debit 扣钱 + 记流水（同事务）。余额不够返 ErrInsufficientBalance，不会扣成负数。
func (s *Store) Debit(ctx context.Context, m Move) (Entry, error) {
	return s.apply(ctx, m, -1)
}

// Credit 入账 + 记流水（同事务）。
func (s *Store) Credit(ctx context.Context, m Move) (Entry, error) {
	return s.apply(ctx, m, +1)
}

func (s *Store) apply(ctx context.Context, m Move, sign int64) (Entry, error) {
	if m.Amount <= 0 {
		return Entry{}, ErrNonPositiveAmount
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("wallet: 开事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	entry, err := ApplyTx(ctx, tx, m, sign)
	if err != nil {
		return Entry{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("wallet: 提交: %w", err)
	}
	return entry, nil
}

// ApplyTx 在**调用方的事务里**记账。
//
// 拉号那种「扣钱 + 建 pull_round + 建 credential_ledger」必须整体原子，
// 所以需要这个能挂进外部事务的版本（09-transactions §2）。
// sign: +1 入账 / -1 出账。
func ApplyTx(ctx context.Context, tx *sql.Tx, m Move, sign int64) (Entry, error) {
	if m.Amount <= 0 {
		return Entry{}, ErrNonPositiveAmount
	}
	now := time.Now().UTC()
	nowStr := formatTime(now)

	if sign < 0 {
		// 条件 UPDATE 是防超扣的第一道：余额不够时影响 0 行，而不是扣成负数。
		// 事务是 BEGIN IMMEDIATE，所以并发下这一步是串行的。
		res, err := tx.ExecContext(ctx, `
			UPDATE wallet SET balance = balance - ?, updated_at = ?
			WHERE passenger_id = ? AND balance >= ?`,
			m.Amount, nowStr, m.PassengerID, m.Amount)
		if err != nil {
			return Entry{}, fmt.Errorf("wallet: 扣款: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return Entry{}, err
		}
		if n == 0 {
			// 分辨是"钱包不存在"还是"余额不够" —— 错误信息要能指导调用方
			var exists int
			if err := tx.QueryRowContext(ctx,
				`SELECT count(1) FROM wallet WHERE passenger_id = ?`, m.PassengerID).Scan(&exists); err == nil && exists == 0 {
				return Entry{}, ErrNotFound
			}
			return Entry{}, ErrInsufficientBalance
		}
	} else {
		res, err := tx.ExecContext(ctx, `
			UPDATE wallet SET balance = balance + ?, updated_at = ? WHERE passenger_id = ?`,
			m.Amount, nowStr, m.PassengerID)
		if err != nil {
			return Entry{}, fmt.Errorf("wallet: 入账: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return Entry{}, ErrNotFound
		}
	}

	var balanceAfter int64
	if err := tx.QueryRowContext(ctx,
		`SELECT balance FROM wallet WHERE passenger_id = ?`, m.PassengerID).Scan(&balanceAfter); err != nil {
		return Entry{}, fmt.Errorf("wallet: 读扣后余额: %w", err)
	}

	// seq 在同一事务里取 max+1 —— 有 UNIQUE(passenger_id, seq) 兜底，
	// 并发下 BEGIN IMMEDIATE 保证不会算出重复值
	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM wallet_ledger WHERE passenger_id = ?`,
		m.PassengerID).Scan(&seq); err != nil {
		return Entry{}, fmt.Errorf("wallet: 算流水序号: %w", err)
	}

	signed := sign * m.Amount
	id := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_ledger
			(id, passenger_id, seq, reason, amount, balance_after, ref_type, ref_id, memo, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, m.PassengerID, seq, string(m.Reason), signed, balanceAfter,
		nullIfEmpty(m.RefType), nullIfEmpty(m.RefID), nullIfEmpty(m.Memo), nowStr); err != nil {
		return Entry{}, fmt.Errorf("wallet: 记流水: %w", err)
	}

	return Entry{
		ID: id, Seq: seq, Reason: m.Reason, Amount: signed, BalanceAfter: balanceAfter,
		RefType: m.RefType, RefID: m.RefID, Memo: m.Memo, CreatedAt: now,
	}, nil
}

// ── 冻结（拉号进行中）· 09-transactions §2 的 reserved 流转 ──
//
// 为什么要冻结而不是直接扣：拉号要先问 vendor，成交前不知道最终金额，
// 但得先占住钱防止并发把余额花光。三步：Reserve → Commit（真扣）或 Release（退回）。

// Reserve 把钱从 balance 挪到 reserved。**不记流水** —— 钱还没花掉。
func (s *Store) Reserve(ctx context.Context, passengerID string, amount int64) error {
	if amount <= 0 {
		return ErrNonPositiveAmount
	}
	return s.inTx(ctx, func(tx *sql.Tx) error { return ReserveTx(ctx, tx, passengerID, amount) })
}

func ReserveTx(ctx context.Context, tx *sql.Tx, passengerID string, amount int64) error {
	if amount <= 0 {
		return ErrNonPositiveAmount
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE wallet SET balance = balance - ?, reserved = reserved + ?, updated_at = ?
		WHERE passenger_id = ? AND balance >= ?`,
		amount, amount, formatTime(time.Now().UTC()), passengerID, amount)
	if err != nil {
		return fmt.Errorf("wallet: 冻结: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT count(1) FROM wallet WHERE passenger_id = ?`, passengerID).Scan(&exists); err == nil && exists == 0 {
			return ErrNotFound
		}
		return ErrInsufficientBalance
	}
	return nil
}

// CommitReserved 把冻结的钱真正花掉：reserved 减 amount + 记流水。
//
// actual 可以小于原冻结额（成交价比预估低），差额用 ReleaseReserved 退回。
func (s *Store) CommitReserved(ctx context.Context, m Move) (Entry, error) {
	var out Entry
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		e, err := CommitReservedTx(ctx, tx, m)
		out = e
		return err
	})
	return out, err
}

func CommitReservedTx(ctx context.Context, tx *sql.Tx, m Move) (Entry, error) {
	if m.Amount <= 0 {
		return Entry{}, ErrNonPositiveAmount
	}
	now := formatTime(time.Now().UTC())

	res, err := tx.ExecContext(ctx, `
		UPDATE wallet SET reserved = reserved - ?, updated_at = ?
		WHERE passenger_id = ? AND reserved >= ?`,
		m.Amount, now, m.PassengerID, m.Amount)
	if err != nil {
		return Entry{}, fmt.Errorf("wallet: 结算冻结: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Entry{}, ErrInsufficientReserved
	}

	// 钱在 Reserve 时已从 balance 扣走，这里只记流水（balance 不再变）
	var balanceAfter int64
	if err := tx.QueryRowContext(ctx,
		`SELECT balance FROM wallet WHERE passenger_id = ?`, m.PassengerID).Scan(&balanceAfter); err != nil {
		return Entry{}, fmt.Errorf("wallet: 读余额: %w", err)
	}

	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM wallet_ledger WHERE passenger_id = ?`,
		m.PassengerID).Scan(&seq); err != nil {
		return Entry{}, fmt.Errorf("wallet: 算流水序号: %w", err)
	}

	id := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallet_ledger
			(id, passenger_id, seq, reason, amount, balance_after, ref_type, ref_id, memo, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, m.PassengerID, seq, string(m.Reason), -m.Amount, balanceAfter,
		nullIfEmpty(m.RefType), nullIfEmpty(m.RefID), nullIfEmpty(m.Memo), now); err != nil {
		return Entry{}, fmt.Errorf("wallet: 记流水: %w", err)
	}

	return Entry{
		ID: id, Seq: seq, Reason: m.Reason, Amount: -m.Amount, BalanceAfter: balanceAfter,
		RefType: m.RefType, RefID: m.RefID, Memo: m.Memo, CreatedAt: time.Now().UTC(),
	}, nil
}

// ReleaseReserved 把冻结退回 balance（拉号失败 / 成交价低于预估）。不记流水。
func (s *Store) ReleaseReserved(ctx context.Context, passengerID string, amount int64) error {
	if amount <= 0 {
		return ErrNonPositiveAmount
	}
	return s.inTx(ctx, func(tx *sql.Tx) error { return ReleaseReservedTx(ctx, tx, passengerID, amount) })
}

func ReleaseReservedTx(ctx context.Context, tx *sql.Tx, passengerID string, amount int64) error {
	if amount <= 0 {
		return ErrNonPositiveAmount
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE wallet SET reserved = reserved - ?, balance = balance + ?, updated_at = ?
		WHERE passenger_id = ? AND reserved >= ?`,
		amount, amount, formatTime(time.Now().UTC()), passengerID, amount)
	if err != nil {
		return fmt.Errorf("wallet: 退回冻结: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrInsufficientReserved
	}
	return nil
}

// ── 流水查询 ──────────────────────────────────────────

type ListOptions struct {
	// Reason 空 = 不筛（**内部** reason，单个精确匹配）
	Reason Reason
	// Reasons 非空时按 IN (...) 过滤（对外 spend 一个 type 对应内部多个 reason）
	Reasons []Reason
	Limit   int
	Offset  int
}

func (s *Store) List(ctx context.Context, passengerID string, opt ListOptions) ([]Entry, int, error) {
	if opt.Limit <= 0 || opt.Limit > 500 {
		opt.Limit = 50 // 契约上限 500（05-api-contract §基础）
	}
	if opt.Offset < 0 {
		opt.Offset = 0
	}

	where := `WHERE passenger_id = ?`
	args := []any{passengerID}
	switch {
	case len(opt.Reasons) > 0:
		placeholders := ""
		for i, r := range opt.Reasons {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, string(r))
		}
		where += ` AND reason IN (` + placeholders + `)`
	case opt.Reason != "":
		where += ` AND reason = ?`
		args = append(args, string(opt.Reason))
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(1) FROM wallet_ledger `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("wallet: 统计流水: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, seq, reason, amount, balance_after,
		       COALESCE(ref_type,''), COALESCE(ref_id,''), COALESCE(memo,''), created_at
		FROM wallet_ledger `+where+`
		ORDER BY seq DESC LIMIT ? OFFSET ?`,
		append(args, opt.Limit, opt.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("wallet: 查流水: %w", err)
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		var reason, createdAt string
		if err := rows.Scan(&e.ID, &e.Seq, &reason, &e.Amount, &e.BalanceAfter,
			&e.RefType, &e.RefID, &e.Memo, &createdAt); err != nil {
			return nil, 0, err
		}
		e.Reason = Reason(reason)
		e.CreatedAt = parseTime(createdAt)
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// ── 每日计数（策略上限用 · decisions §8.27） ──

// BumpDaily 累加今日的轮数和消费。跟扣款同事务调用，保证计数不漏。
func BumpDailyTx(ctx context.Context, tx *sql.Tx, passengerID string, rounds int, spend int64) error {
	date := time.Now().UTC().Format("2006-01-02")
	_, err := tx.ExecContext(ctx, `
		INSERT INTO passenger_daily_counter (passenger_id, date, round_count, spend_total)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (passenger_id, date) DO UPDATE SET
			round_count = round_count + excluded.round_count,
			spend_total = spend_total + excluded.spend_total`,
		passengerID, date, rounds, spend)
	if err != nil {
		return fmt.Errorf("wallet: 累加日计数: %w", err)
	}
	return nil
}

type DailyUsage struct {
	Rounds int
	Spend  int64
}

func (s *Store) TodayUsage(ctx context.Context, passengerID string) (DailyUsage, error) {
	date := time.Now().UTC().Format("2006-01-02")
	var u DailyUsage
	err := s.db.QueryRowContext(ctx,
		`SELECT round_count, spend_total FROM passenger_daily_counter
		 WHERE passenger_id = ? AND date = ?`, passengerID, date).Scan(&u.Rounds, &u.Spend)
	if errors.Is(err, sql.ErrNoRows) {
		return DailyUsage{}, nil // 今天还没拉过 = 0
	}
	if err != nil {
		return DailyUsage{}, fmt.Errorf("wallet: 查日计数: %w", err)
	}
	return u, nil
}

func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("wallet: 开事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("wallet: 提交: %w", err)
	}
	return nil
}

const timeLayout = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) time.Time {
	for _, l := range []string{timeLayout, time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
