// Package topup 管充值单：起单 → 支付网关 webhook → 到账。
//
// 起单**不即时到账** —— 只落一行 pending topup_order + 一个 pay_url，等支付网关
// webhook 到才 MarkPaid，那时才落 wallet_ledger 两条：recharge + channel_fee。
// 阶段 1a 未接真网关时 pay_url 是 mock URL·MarkPaid 由内部端点触发。
//
// 手续费口径按 CLAUDE.md §1.4：credits × 5% 是加在本金上（不是含在总额里）。
// 一次充值两条流水：recharge +（credits+fee） 和 channel_fee -fee，净 +credits。
package topup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/wallet"
	"github.com/google/uuid"
)

// ChannelFeeBps 手续费率（万分之几）。500 = 5%。跟前端 utils.ts CHANNEL_FEE_RATE 对齐。
// 定 const 而不是配置：pass-through 5% 是跟通道商谈定的合同数字，不做运营开关。
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
//
// **三维属性**（1b 起 · migration 010）：
//   - Channel:  具体渠道 id（枚举·由 channel registry 定义）
//   - Region:   地区（domestic / overseas）
//   - Rail:     到账方式（direct 乘客直转 · hosted 三方 checkout）
//   - ProviderKind: 支付网关侧的 rail 名（channel 决定 · registry 里配）
//   - PayerReference: direct rail 需要乘客提供 · hosted 可空
type Order struct {
	ID               string
	PassengerID      string
	Channel          string
	Region           string
	Rail             string
	ProviderKind     string
	PayerReference   string
	Credits          int64 // 净到账积分
	ChannelFee       int64 // 手续费
	Paid             int64 // 用户付的总积分 = Credits + ChannelFee
	PayURL           string
	CheckoutURL      string // gateway.instructions.checkout_url · 前端跳转
	QRContent        string // 有 QR 的 rail 才给
	GatewayPaymentID string // gateway 返的 pay_xxx · 内部关联·不对外
	// FeeWaiverApplied 这单用掉了一次手续费减免（个人邀请码额度 · §8.29）
	FeeWaiverApplied bool
	// FeeSubsidy 我方垫付给支付通道的手续费（microunit）· 单独记不混进 channel_fee
	// **对外不出** —— 这是我方成本结构（CLAUDE.md §0.1）
	FeeSubsidy int64
	// GatewayRequestSnapshot · 起单时冻结的 CreatePaymentRequest JSON。
	// janitor 反查用它重新 POST · 保证幂等指纹跟初次一致（汇率 / 配置 / email 都可能变过）。
	// 空 = 起单没走到 SaveGatewayRequestSnapshot（旧行 or 未装 gateway）· 反查走 pending_manual 兜底。
	GatewayRequestSnapshot []byte
	Status                 Status
	ExpiresAt              time.Time
	PaidAt                 time.Time
	WalletLedgerID         string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// OrderInput 是 CreateOrder 的入参（1b 起用 · 支持三维属性）。
type OrderInput struct {
	PassengerID    string
	Channel        string
	Region         string
	Rail           string
	ProviderKind   string
	PayerReference string
	Credits        int64
	PayURL         string
	TTL            time.Duration
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

// BreakdownFor 根据目标积分算手续费和总付款。
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

// CreateOrder · **已 deprecated** · 保留仅为兼容旧 caller 编译通过。
//
// 三维属性（region / rail / provider_kind）来自 channel registry 而不是 hardcode ·
// 新代码 API 层直接调 CreateOrderIn 传完整属性。
func (s *Store) CreateOrder(ctx context.Context, passengerID, channel string, credits int64, payURL string, ttl time.Duration) (Order, error) {
	return s.CreateOrderIn(ctx, OrderInput{
		PassengerID: passengerID, Channel: channel, Credits: credits,
		PayURL: payURL, TTL: ttl,
	})
}

// CreateOrderWithPending · P1-2 修：order + pending_topup 原子创建。
//
// 场景：handleCreateTopup 以前先 commit order · 再 Create pending · 中间崩溃
// 会留一个 order 但 pending 缺失 · janitor 扫不到 · 状态机永远静默。
//
// 本方法在**同一事务**内插两条记录 · 崩溃时两条一起消失（同 idem key 重试当新单）。
// idempotencyRecordID 是 handler 层已经在 tx1 里落好的 idempotency_record.id。
func (s *Store) CreateOrderWithPending(ctx context.Context, in OrderInput, idempotencyRecordID string) (Order, string, error) {
	if in.Credits <= 0 {
		return Order{}, "", ErrInvalidAmount
	}
	if in.TTL <= 0 {
		in.TTL = 15 * time.Minute
	}
	if in.Channel == "" {
		return Order{}, "", ErrUnsupportedChannel
	}
	// channel 白名单校验由 caller 走 topupchannel.Registry 做（api 层已做）·
	// 走到这里非法 channel 会撞 SQL CHECK · 翻译成 ErrUnsupportedChannel。
	if in.Region == "" {
		in.Region = "overseas"
	}
	if in.Rail == "" {
		in.Rail = "hosted"
	}

	b := BreakdownFor(in.Credits)
	now := time.Now().UTC()
	nowStr := formatTime(now)
	orderID := uuid.NewString()
	pendingID := uuid.NewString()
	exp := now.Add(in.TTL)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Order{}, "", fmt.Errorf("topup: 起单开事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 手续费减免（个人邀请码额度 · decisions §8.29/§8.32）
	//
	// **判定和消耗必须同事务** —— 分开的话并发能超用（两个请求都看到"还有额度"）。
	// 用掉一次 → 乘客不付手续费（paid = credits）· 那 5% 由我方垫付给支付通道，
	// 记 fee_subsidy 单独科目（混进 channel_fee 财务上看不出补贴多少）。
	waived, err := consumeFeeWaiverTx(ctx, tx, in.PassengerID, now)
	if err != nil {
		return Order{}, "", err
	}
	var feeSubsidy int64
	if waived {
		feeSubsidy = b.ChannelFee // 我方垫付的金额
		b.ChannelFee = 0          // 乘客不付
		b.Paid = b.Credits        // 只付本金
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO topup_order
		  (id, passenger_id, channel, region, rail, credits, channel_fee, paid, pay_url,
		   status, expires_at, provider_kind, payer_reference,
		   fee_waiver_applied, fee_subsidy, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?)`,
		orderID, in.PassengerID, in.Channel, in.Region, in.Rail,
		b.Credits, b.ChannelFee, b.Paid, in.PayURL, formatTime(exp),
		nullIfEmpty(in.ProviderKind), nullIfEmpty(in.PayerReference),
		boolToInt(waived), feeSubsidy, nowStr, nowStr); err != nil {
		if isCheckConstraintErr(err) {
			return Order{}, "", ErrUnsupportedChannel
		}
		return Order{}, "", fmt.Errorf("topup: 插入充值单: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pending_topup
		  (id, idempotency_record_id, passenger_id, topup_order_id, status,
		   created_at, updated_at)
		VALUES (?, ?, ?, ?, 'initial', ?, ?)`,
		pendingID, idempotencyRecordID, in.PassengerID, orderID, nowStr, nowStr); err != nil {
		return Order{}, "", fmt.Errorf("topup: 落 pending_topup initial: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Order{}, "", fmt.Errorf("topup: 起单 commit: %w", err)
	}

	return Order{
		ID: orderID, PassengerID: in.PassengerID,
		Channel: in.Channel, Region: in.Region, Rail: in.Rail,
		ProviderKind: in.ProviderKind, PayerReference: in.PayerReference,
		Credits: b.Credits, ChannelFee: b.ChannelFee, Paid: b.Paid,
		FeeWaiverApplied: waived, FeeSubsidy: feeSubsidy,
		PayURL: in.PayURL, CheckoutURL: in.PayURL,
		Status: StatusPending, ExpiresAt: exp,
		CreatedAt: now, UpdatedAt: now,
	}, pendingID, nil
}

// CreateOrderIn 起一张充值单（1b 起 · 支持多渠道 · 完整属性）。
//
// TTL 15 分钟：过期后 janitor 应扫过来把 status 改成 expired。
// **不校验 channel 是否 enabled** —— 那是 api 层的事（用 topupchannel.Registry.GetEnabled）·
// Store 只落库 · 允许 mock / 后台 admin 调用启用状态之外的 channel（跑不通就返 unsupported）。
func (s *Store) CreateOrderIn(ctx context.Context, in OrderInput) (Order, error) {
	if in.Credits <= 0 {
		return Order{}, ErrInvalidAmount
	}
	if in.TTL <= 0 {
		in.TTL = 15 * time.Minute
	}
	// channel 白名单校验由 caller 走 topupchannel.Registry 做（api 层已做）·
	// 走到这里非法 channel 会撞 SQL CHECK · 下面 INSERT 时翻译成 ErrUnsupportedChannel。
	if in.Channel == "" {
		return Order{}, ErrUnsupportedChannel
	}
	if in.Region == "" {
		in.Region = "overseas"
	}
	if in.Rail == "" {
		in.Rail = "hosted"
	}

	b := BreakdownFor(in.Credits)
	now := time.Now().UTC()
	id := uuid.NewString()
	exp := now.Add(in.TTL)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO topup_order
		  (id, passenger_id, channel, region, rail, credits, channel_fee, paid, pay_url,
		   status, expires_at, provider_kind, payer_reference, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?)`,
		id, in.PassengerID, in.Channel, in.Region, in.Rail,
		b.Credits, b.ChannelFee, b.Paid, in.PayURL,
		formatTime(exp),
		nullIfEmpty(in.ProviderKind), nullIfEmpty(in.PayerReference),
		formatTime(now), formatTime(now))
	if err != nil {
		if isCheckConstraintErr(err) {
			return Order{}, ErrUnsupportedChannel
		}
		return Order{}, fmt.Errorf("topup: 插入充值单: %w", err)
	}

	return Order{
		ID: id, PassengerID: in.PassengerID,
		Channel: in.Channel, Region: in.Region, Rail: in.Rail,
		ProviderKind: in.ProviderKind, PayerReference: in.PayerReference,
		Credits: b.Credits, ChannelFee: b.ChannelFee, Paid: b.Paid,
		PayURL: in.PayURL, CheckoutURL: in.PayURL,
		Status: StatusPending, ExpiresAt: exp,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// nullIfEmpty · sqlite 里想存 NULL 而非空串（partial UNIQUE 索引 / query 判空要）
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// isCheckConstraintErr · sqlite driver 返回 CHECK 冲突时的错误串匹配
// （用来把非法 channel 之类的翻译成 ErrUnsupportedChannel · 不硬编枚举值到代码）
func isCheckConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "CHECK constraint failed") ||
		strings.Contains(msg, "constraint failed: CHECK")
}

// SaveGatewayRequestSnapshot 冷冻 CreatePaymentRequest JSON 到 topup_order。
//
// 起单流程（P0 修 · codex 三轮）：
//   1. handler 建 CreatePaymentRequest（含 payer_email / payer_reference / amount / asset ...）
//   2. handler 序列化 request 调本方法落库（**先落库·再调 gateway**）
//   3. handler 调 gateway.CreatePayment
//   4. janitor 反查时读回 snapshot · **原样** POST · gateway 幂等指纹一致 → 200 replay
//
// 为什么必须冷冻：起单跟 janitor 反查中间·汇率可能变、channel config 可能改、
// 甚至乘客 email 都可能改。从当前 config 重建 request → 幂等指纹不同 → gateway 侧
// 认为是"新单"而不是"replay" → 语义错。
//
// 幂等：orderID 唯一 · 重复保存幂等（同 orderID 覆盖）。空 snapshot 拒写。
func (s *Store) SaveGatewayRequestSnapshot(ctx context.Context, orderID string, snapshot []byte) error {
	if len(snapshot) == 0 {
		return fmt.Errorf("topup: SaveGatewayRequestSnapshot 空 snapshot")
	}
	now := formatTime(time.Now().UTC())
	res, err := s.db.ExecContext(ctx, `
		UPDATE topup_order
		   SET gateway_request_snapshot = ?, updated_at = ?
		 WHERE id = ?`,
		snapshot, now, orderID)
	if err != nil {
		return fmt.Errorf("topup: 写 gateway_request_snapshot: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AttachGateway 把 gateway 侧的信息回写到已建的 topup_order。
//
// 起单流程：先 CreateOrder 落一行 pending（拿到我方 id 作为 client_order_id），
// 然后调 gateway.CreatePayment 拿 payment_id + checkout_url，回来 AttachGateway。
// 这样即使 gateway 调用失败，也不会污染 wallet；乘客可以对同 order_id 重试建单。
//
// 空 gatewayPaymentID / qrContent 存 NULL 而不是空串·UNIQUE 索引才能允许多单共存。
func (s *Store) AttachGateway(ctx context.Context, orderID, gatewayPaymentID, checkoutURL, qrContent string) error {
	now := formatTime(time.Now().UTC())
	var gwArg any = gatewayPaymentID
	if gatewayPaymentID == "" {
		gwArg = nil
	}
	var qrArg any = qrContent
	if qrContent == "" {
		qrArg = nil
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE topup_order
		   SET gateway_payment_id = ?, checkout_url = ?, qr_content = ?, updated_at = ?
		 WHERE id = ? AND status = 'pending'`,
		gwArg, checkoutURL, qrArg, now, orderID)
	if err != nil {
		return fmt.Errorf("topup: 回写 gateway 字段: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrOrderNotPending
	}
	return nil
}

// FindByClientOrderID · settlement 回调时 fallback 匹配（GatewayPaymentID 还没回填）。
//
// P0-A 修：CreateOrder 建单 → gateway.CreatePayment → AttachGateway 有 tiny 窗口·
// gateway 极快时 webhook 先到·gateway_payment_id 还没落库·FindByGatewayPaymentID 会
// unmatched。用 client_order_id（= 我方 order.ID · webhook 签名保护）稳可匹配。
//
// 安全性：client_order_id 从 signed webhook body 拿·签名校验过·不能被伪造。
func (s *Store) FindByClientOrderID(ctx context.Context, clientOrderID string) (Order, error) {
	o, err := hydrateOrder(s.db.QueryRowContext(ctx,
		`SELECT `+selectOrderCols+` FROM topup_order WHERE id = ?`, clientOrderID))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("topup: 用 client_order_id 反查订单: %w", err)
	}
	return o, nil
}

// FindByGatewayPaymentID · gateway 回调时用来反查我方 order。
// 回调 body 里带 client_order_id（= 我方 id），但 gateway_payment_id 更稳
// （client_order_id 可能被人伪造·gateway_payment_id 是 gateway 分配的）。
func (s *Store) FindByGatewayPaymentID(ctx context.Context, gatewayPaymentID string) (Order, error) {
	o, err := hydrateOrder(s.db.QueryRowContext(ctx,
		`SELECT `+selectOrderCols+` FROM topup_order WHERE gateway_payment_id = ?`, gatewayPaymentID))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("topup: 反查订单: %w", err)
	}
	return o, nil
}

// MarkPaid webhook 到账时调用。原子做三件事：
//  1. 条件 UPDATE topup_order status pending → paid（幂等：非 pending 直接 return）
//  2. Credit 一条 recharge = paid
//  3. Debit 一条 channel_fee = fee
//
// 净变化 = +credits。两条流水在一个事务里，避免"到账了但手续费没扣"或反之。
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
			Memo:        "手续费 5%",
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

	// 充值成功 → 该乘客在所有车里自动解挂 + 跳过计数归零（decisions §8.26
	// "他充值 → 自己解挂 · skipped_count 归零 · 不用车主批"）。
	//
	// 直接写 SQL 而不调 bus 包 —— topup 不该依赖 bus（层次方向）。这条 UPDATE
	// 语义单一（把欠费状态清掉），不涉及 bus 的业务规则。
	//
	// **从下一轮开始生效**：这里只清状态，本轮已经算完的分摊不追溯（跟用户口述一致）。
	if err := wallet.ClearOverdueStateTx(ctx, tx, passengerID); err != nil {
		return Order{}, err
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

// MarkRefunded gateway 发 refunded / reversed 事件时调用。
//
// 原子做两件事：
//  1. 反向流水两条 · reason=topup_refund · amount=-paid (recharge 反向) +fee (channel_fee 反向)
//     净变化 = -credits
//  2. topup_order.status = refunded / reversed
//
// **只在 status=paid 时能调**：pending 直接跳 refunded 走 refund 无意义；
// refunded / reversed 状态**幂等静默 return 当前订单**（gateway at-least-once）。
//
// kind: "refunded" (payer 主动退款) or "reversed" (下游反向 · 拒付/回单)
//
//nolint:funlen
func (s *Store) MarkRefunded(ctx context.Context, orderID, kind string) (Order, error) {
	if kind != "refunded" && kind != "reversed" {
		return Order{}, fmt.Errorf("topup: MarkRefunded kind 必须是 refunded / reversed, got %q", kind)
	}
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
	)
	err = tx.QueryRowContext(ctx,
		`SELECT passenger_id, credits, channel_fee, paid, status
		 FROM topup_order WHERE id = ?`, orderID).
		Scan(&passengerID, &credits, &channelFee, &paid, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("topup: 查订单: %w", err)
	}

	// 幂等 · 已 refunded / reversed / cancelled 直接返回
	if status == kind || status == "refunded" || status == "reversed" || status == "cancelled" {
		return s.getInTx(ctx, tx, orderID)
	}
	// 只能从 paid 走 · pending / expired 拒
	if status != string(StatusPaid) {
		return Order{}, fmt.Errorf("topup: %w · order.status=%s 不能 refund", ErrOrderNotPending, status)
	}

	now := time.Now().UTC()
	nowStr := formatTime(now)
	res, err := tx.ExecContext(ctx, `
		UPDATE topup_order
		   SET status = ?, updated_at = ?
		 WHERE id = ? AND status = 'paid'`,
		kind, nowStr, orderID)
	if err != nil {
		return Order{}, fmt.Errorf("topup: 标 %s: %w", kind, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Order{}, ErrOrderNotPending
	}

	// 反向顺序：先加 fee 回来 · 再扣 paid（否则余额不够扣 -paid）
	// 净变化 = +fee - paid = -credits
	if channelFee > 0 {
		if _, err := wallet.ApplyTx(ctx, tx, wallet.Move{
			PassengerID: passengerID,
			Reason:      wallet.ReasonTopupRefund,
			Amount:      channelFee,
			RefType:     "topup_order",
			RefID:       orderID,
			Memo:        "充值 " + kind + "（反向 channel_fee）",
		}, +1); err != nil {
			return Order{}, err
		}
	}
	// 反向 recharge：-paid（把当初 credit 的都退回给 gateway）
	// **允许 balance 走到负** —— 用户可能已经把充值花光·refund 是把钱退给 gateway 不是
	// 换取用户余额·系统必须记这笔"负债"（P0-B 修：以前吞成 duplicate 让 gateway 停重试）
	if _, err := wallet.ForceApplyTx(ctx, tx, wallet.Move{
		PassengerID: passengerID,
		Reason:      wallet.ReasonTopupRefund,
		Amount:      paid,
		RefType:     "topup_order",
		RefID:       orderID,
		Memo:        "充值 " + kind + "（反向 recharge · 可能致余额为负）",
	}, -1); err != nil {
		return Order{}, err
	}
	_ = credits // avoid unused warning · 保留意图注释

	// 退款了 → 这单用过的手续费减免额度退回去（§8.29）
	// 那次减免实际没生效（钱都退回 gateway 了）· 不退等于用户白掉一次额度
	if err := returnFeeWaiverForOrderTx(ctx, tx, orderID, now); err != nil {
		return Order{}, err
	}

	out, err := s.getInTx(ctx, tx, orderID)
	if err != nil {
		return Order{}, err
	}
	if err := tx.Commit(); err != nil {
		return Order{}, fmt.Errorf("topup: 提交 refund: %w", err)
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

// rowScanner sql.Row / sql.Rows 共用接口·让 hydrateOrder 一份代码
type rowScanner interface {
	Scan(dest ...any) error
}

// hydrateOrder 从一行读取所有 topup_order 列到 Order。
// 列顺序要跟下面的 SELECT 保持一致。加列时同时改这里。
func hydrateOrder(scan rowScanner) (Order, error) {
	var (
		o         Order
		expiresAt string
		createdAt string
		updatedAt string
		paidAt    sql.NullString
		ledgerID  sql.NullString
		gwID      sql.NullString
		checkout  sql.NullString
		qr        sql.NullString
		providerK sql.NullString
		payerRef  sql.NullString
		snapshot  []byte
		waived    int
		subsidy   int64
	)
	if err := scan.Scan(
		&o.ID, &o.PassengerID, &o.Channel, &o.Region, &o.Rail,
		&o.Credits, &o.ChannelFee, &o.Paid,
		&o.PayURL, (*string)(&o.Status), &expiresAt, &paidAt, &ledgerID,
		&createdAt, &updatedAt, &gwID, &checkout, &qr, &providerK, &payerRef,
		&snapshot, &waived, &subsidy,
	); err != nil {
		return Order{}, err
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
	if gwID.Valid {
		o.GatewayPaymentID = gwID.String
	}
	if checkout.Valid {
		o.CheckoutURL = checkout.String
	}
	if qr.Valid {
		o.QRContent = qr.String
	}
	if providerK.Valid {
		o.ProviderKind = providerK.String
	}
	if payerRef.Valid {
		o.PayerReference = payerRef.String
	}
	if len(snapshot) > 0 {
		o.GatewayRequestSnapshot = snapshot
	}
	o.FeeWaiverApplied = waived != 0
	o.FeeSubsidy = subsidy
	return o, nil
}

const selectOrderCols = `id, passenger_id, channel, region, rail, credits, channel_fee, paid, pay_url,
	status, expires_at, paid_at, wallet_ledger_id, created_at, updated_at,
	gateway_payment_id, checkout_url, qr_content, provider_kind, payer_reference,
	gateway_request_snapshot, fee_waiver_applied, fee_subsidy`

func (s *Store) getBy(ctx context.Context, orderID string) (Order, error) {
	o, err := hydrateOrder(s.db.QueryRowContext(ctx,
		`SELECT `+selectOrderCols+` FROM topup_order WHERE id = ?`, orderID))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("topup: 查订单: %w", err)
	}
	return o, nil
}

func (s *Store) getInTx(ctx context.Context, tx *sql.Tx, orderID string) (Order, error) {
	o, err := hydrateOrder(tx.QueryRowContext(ctx,
		`SELECT `+selectOrderCols+` FROM topup_order WHERE id = ?`, orderID))
	if err != nil {
		return Order{}, fmt.Errorf("topup: 事务内查订单: %w", err)
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

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+selectOrderCols+` FROM topup_order `+where+`
		 ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, opt.Limit, opt.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("topup: 查列表: %w", err)
	}
	defer rows.Close()

	var out []Order
	for rows.Next() {
		o, err := hydrateOrder(rows)
		if err != nil {
			return nil, 0, err
		}
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
