package topup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PendingStatus pending_topup 状态机（09-transactions §6）。
//
// 目标：让 payment-Gateway settlement webhook 是**主推进**·janitor 是**兜底**·
// 崩溃后能从任意中间态恢复（不双扣款·不漏入账）。
//
// 状态流：
//
//	initial ──CreateOrder+落 pending_topup──▶ gateway_ordered
//	           │                              │
//	           └───(gateway 建单失败)          │
//	                                          └──settled webhook──▶ gateway_paid
//	                                                                 │
//	                                                                 └──MarkPaid + wallet 入账──▶ credited
//	                                                                                              │
//	                                                                                              └──▶ completed
//	  任一步卡多轮 → pending_manual
//	  超时未 settled → expired
//	  webhook refunded → refunded
type PendingStatus string

const (
	PendingInitial         PendingStatus = "initial"          // 落 pending_topup 行·gateway 还没调
	PendingGatewayCreating PendingStatus = "gateway_creating" // handler 调 CreatePayment 中 · 崩后 janitor 用 client_order_id 反查
	PendingGatewayOrdered  PendingStatus = "gateway_ordered"  // gateway_payment_id 已回填
	PendingGatewayPaid     PendingStatus = "gateway_paid"     // 收到 settled webhook · wallet 还没入账
	PendingCredited        PendingStatus = "credited"         // wallet_ledger recharge/channel_fee 已落·未推 completed
	PendingCompleted       PendingStatus = "completed"        // 终态
	PendingExpired         PendingStatus = "expired"          // gateway_ordered 后超时·gateway 也过期
	PendingCancelled       PendingStatus = "cancelled"        // 乘客主动取消（1b 可能不做）
	PendingRefunded        PendingStatus = "refunded"         // webhook 通知 refunded/reversed
	PendingManual          PendingStatus = "pending_manual"   // 卡多轮·转人工
)

// Pending 一行 pending_topup（内部形状）。
type Pending struct {
	ID                  string
	IdempotencyRecordID string
	PassengerID         string
	TopupOrderID        string
	Status              PendingStatus
	Error               string
	PollFailCount       int
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// ErrPendingNotFound · pending_topup 找不到
var ErrPendingNotFound = errors.New("topup: pending_topup 找不到")

// ErrStaleTransition · 状态推进时 from 不匹配（并发已推 · 或状态早已过期）
var ErrStaleTransition = errors.New("topup: pending_topup 状态推进 rows=0（并发或已推过）")

// PendingStore pending_topup 持久化。
type PendingStore struct{ db *sql.DB }

// NewPendingStore · db 连接同 topup.Store
func NewPendingStore(db *sql.DB) *PendingStore { return &PendingStore{db: db} }

// Create 落一行 initial（tx 版可挂到 CreateOrder 的事务里·原子保证 order + pending 一起）。
func (s *PendingStore) Create(ctx context.Context, in Pending) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	nowStr := formatTime(now)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pending_topup
		  (id, idempotency_record_id, passenger_id, topup_order_id, status,
		   created_at, updated_at)
		VALUES (?, ?, ?, ?, 'initial', ?, ?)`,
		id, in.IdempotencyRecordID, in.PassengerID, in.TopupOrderID, nowStr, nowStr)
	if err != nil {
		return "", fmt.Errorf("topup: 落 pending_topup initial: %w", err)
	}
	return id, nil
}

// CreateTx 事务版·让 CreateOrder + Create 挂同一 tx。
func (s *PendingStore) CreateTx(ctx context.Context, tx *sql.Tx, in Pending) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	nowStr := formatTime(now)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO pending_topup
		  (id, idempotency_record_id, passenger_id, topup_order_id, status,
		   created_at, updated_at)
		VALUES (?, ?, ?, ?, 'initial', ?, ?)`,
		id, in.IdempotencyRecordID, in.PassengerID, in.TopupOrderID, nowStr, nowStr)
	if err != nil {
		return "", fmt.Errorf("topup: 事务内落 pending_topup: %w", err)
	}
	return id, nil
}

// Advance 单纯改状态（from → to）· 条件 UPDATE 防并发。
//
// 状态推进原则（09-transactions §5 · 跟 pending_purchase 一致）：
//   - **只允许 from → to 的一步跳** · 跳跃或倒退返 ErrStaleTransition
//   - webhook 主推进 · janitor 兜底 · 两条路径都用 Advance
func (s *PendingStore) Advance(ctx context.Context, id string, from, to PendingStatus) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_topup
		   SET status = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		string(to), formatTime(time.Now().UTC()), id, string(from))
	if err != nil {
		return fmt.Errorf("topup: 推进 pending_topup: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleTransition
	}
	return nil
}

// AdvanceByOrderID · webhook 场景：按 topup_order_id 找对应 pending_topup 推进。
//
// **P1-3 修**（审计发现）：以前无论是否更新到行都返 nil · 静默吞错。
// 现在返回 (advanced bool, err error)：
//   - advanced=true · 从 from 推到了 to（成功推进）
//   - advanced=false, err=nil · 行不存在 · 或状态不匹配 · 或已 = to（幂等重放）
//   - err != nil · 底层 SQL 错
//
// 调用方**必须**判 advanced —— 让静态错误明确暴露（P0-2 early settlement 回归就是
// 因为静默吞错导致 pending 卡 initial · 后续 janitor 误标 expired）。
func (s *PendingStore) AdvanceByOrderID(ctx context.Context, orderID string, from, to PendingStatus) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_topup
		   SET status = ?, updated_at = ?
		 WHERE topup_order_id = ? AND status = ?`,
		string(to), formatTime(time.Now().UTC()), orderID, string(from))
	if err != nil {
		return false, fmt.Errorf("topup: 按 order 推进 pending_topup: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// EnsureAtLeast · 幂等推进 · 允许"跨态跃迁"从任何**合法前置态**直接推到 target。
//
// 用途：webhook 早到时（AttachGateway 还没跑）· 我方 pending 还是 initial ·
// gateway 上却已经 settled → wallet 已入账 → pending 需要一路推 initial→gateway_paid。
// 用 AdvanceByOrderID 单步推的话 · from='gateway_ordered' 匹配不上 · 静默失败。
//
// 语义：allowedFroms 里的任一态都能跳到 target。当前 status 已是 target = 幂等 pass。
// 当前 status 是 target 之后（比如 target=gateway_paid · 但已 credited）也 pass（不倒退）。
// 除此外返 error（明确暴露状态机异常）。
//
// **状态偏序**（数值越大越"完成"）：
//
//	initial(0) < gateway_ordered(1) < gateway_paid(2) < credited(3) < completed(4)
//	expired / cancelled / refunded / pending_manual 是"支线终态"·不参与主线偏序·
//	碰到这些 target 用 EnsureAtLeast 会退化成 AdvanceByOrderID 语义（from 必须精确）。
func (s *PendingStore) EnsureAtLeast(ctx context.Context, orderID string, target PendingStatus) error {
	targetOrd, ok := statusOrder(target)
	if !ok {
		return fmt.Errorf("topup: EnsureAtLeast 不支持非主线 target=%q（用 Advance）", target)
	}
	// 先读当前 status
	var cur string
	err := s.db.QueryRowContext(ctx,
		`SELECT status FROM pending_topup WHERE topup_order_id = ?`, orderID).Scan(&cur)
	if err != nil {
		return fmt.Errorf("topup: EnsureAtLeast 读现状: %w", err)
	}
	curOrd, isMain := statusOrder(PendingStatus(cur))
	if !isMain {
		// 支线终态（expired/refunded/…）不动
		return fmt.Errorf("topup: EnsureAtLeast 当前 status=%q 是支线态·不能推进到 %q", cur, target)
	}
	if curOrd >= targetOrd {
		// 已在 target 或更后·幂等 pass
		return nil
	}
	// 一路推到 target
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_topup
		   SET status = ?, updated_at = ?
		 WHERE topup_order_id = ? AND status = ?`,
		string(target), formatTime(time.Now().UTC()), orderID, cur)
	if err != nil {
		return fmt.Errorf("topup: EnsureAtLeast 推进: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 并发另一路径已推进 · 再读一次判断
		var after string
		_ = s.db.QueryRowContext(ctx,
			`SELECT status FROM pending_topup WHERE topup_order_id = ?`, orderID).Scan(&after)
		if afterOrd, ok := statusOrder(PendingStatus(after)); ok && afterOrd >= targetOrd {
			return nil // 并发 · 状态已达
		}
		return fmt.Errorf("topup: EnsureAtLeast 推进 rows=0 · 并发但状态未达 (was=%s after=%s target=%s)",
			cur, after, target)
	}
	return nil
}

// statusOrder 返回主线状态的偏序值 · 支线返 false。
func statusOrder(s PendingStatus) (int, bool) {
	switch s {
	case PendingInitial:
		return 0, true
	case PendingGatewayCreating:
		return 1, true
	case PendingGatewayOrdered:
		return 2, true
	case PendingGatewayPaid:
		return 3, true
	case PendingCredited:
		return 4, true
	case PendingCompleted:
		return 5, true
	default:
		return 0, false
	}
}

// ExpireBoth · P1-1 修：pending_topup + topup_order 同一事务改 expired · 消双表分叉。
//
// from · 期望 pending 当前状态（防覆盖已 completed / refunded 之类的终态）·
// 只有当前正好 = from 才推进两表。返回 (didExpire bool, err error)。
func (s *PendingStore) ExpireBoth(ctx context.Context, pendingID, orderID string, from PendingStatus) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	now := formatTime(time.Now().UTC())
	res, err := tx.ExecContext(ctx, `
		UPDATE pending_topup
		   SET status = 'expired', updated_at = ?
		 WHERE id = ? AND status = ?`,
		now, pendingID, string(from))
	if err != nil {
		return false, fmt.Errorf("topup: ExpireBoth pending: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// 并发已推进 · 不 expire
		return false, nil
	}
	// 同步 topup_order · 只处理 pending 态（paid 后不该被 expired 覆盖）
	res2, err := tx.ExecContext(ctx, `
		UPDATE topup_order
		   SET status = 'expired', updated_at = ?
		 WHERE id = ? AND status = 'pending'`,
		now, orderID)
	if err != nil {
		return false, fmt.Errorf("topup: ExpireBoth order: %w", err)
	}

	// 这单如果用过手续费减免额度 → **退回去**（§8.29）。
	// 额度是起单时扣的·单子没付成那次减免实际没发生·不退等于用户白掉一次。
	// 只在 order 真的被改成 expired 时退（n2>0）· 否则会重复退。
	if n2, _ := res2.RowsAffected(); n2 > 0 {
		if err := returnFeeWaiverForOrderTx(ctx, tx, orderID, time.Now().UTC()); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// MarkManual 卡多轮转人工。
func (s *PendingStore) MarkManual(ctx context.Context, id, reason string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE pending_topup
		   SET status = 'pending_manual', error = ?, updated_at = ?
		 WHERE id = ? AND status NOT IN ('completed', 'refunded', 'pending_manual')`,
		reason, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("topup: 转 pending_manual: %w", err)
	}
	return nil
}

// GetByOrderID · 通过 topup_order_id 反查 pending_topup（janitor 用）。
func (s *PendingStore) GetByOrderID(ctx context.Context, orderID string) (Pending, error) {
	var (
		p         Pending
		errStr    sql.NullString
		createdAt string
		updatedAt string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, idempotency_record_id, passenger_id, topup_order_id, status,
		       error, poll_fail_count, created_at, updated_at
		  FROM pending_topup WHERE topup_order_id = ?`, orderID).Scan(
		&p.ID, &p.IdempotencyRecordID, &p.PassengerID, &p.TopupOrderID,
		(*string)(&p.Status), &errStr, &p.PollFailCount, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Pending{}, ErrPendingNotFound
	}
	if err != nil {
		return Pending{}, fmt.Errorf("topup: 查 pending_topup: %w", err)
	}
	if errStr.Valid {
		p.Error = errStr.String
	}
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	return p, nil
}

// FindStuck · janitor 扫卡在中间态超过 threshold 的行。
//
// 中间态 = 非 completed / refunded / pending_manual / cancelled / expired。
// 返回顺序：updated_at 早的先扫（老单先处理）· limit 兜个数免得撑爆。
func (s *PendingStore) FindStuck(ctx context.Context, threshold time.Time, limit int) ([]Pending, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, idempotency_record_id, passenger_id, topup_order_id, status,
		       error, poll_fail_count, created_at, updated_at
		  FROM pending_topup
		 WHERE status IN ('initial', 'gateway_creating', 'gateway_ordered', 'gateway_paid', 'credited')
		   AND updated_at < ?
		 ORDER BY updated_at ASC
		 LIMIT ?`,
		formatTime(threshold), limit)
	if err != nil {
		return nil, fmt.Errorf("topup: 扫 pending_topup 卡单: %w", err)
	}
	defer rows.Close()
	var out []Pending
	for rows.Next() {
		var (
			p         Pending
			errStr    sql.NullString
			createdAt string
			updatedAt string
		)
		if err := rows.Scan(
			&p.ID, &p.IdempotencyRecordID, &p.PassengerID, &p.TopupOrderID,
			(*string)(&p.Status), &errStr, &p.PollFailCount, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		if errStr.Valid {
			p.Error = errStr.String
		}
		p.CreatedAt = parseTime(createdAt)
		p.UpdatedAt = parseTime(updatedAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

// IncrPollFailCount · P0/P1 修 · janitor poll gateway 失败时累计 · 返回累加后的值。
// 用来判断"暂不可达 vs 反复失败"·避免把网络抖动当作 expired。
func (s *PendingStore) IncrPollFailCount(ctx context.Context, id string) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_topup
		   SET poll_fail_count = poll_fail_count + 1, updated_at = ?
		 WHERE id = ?`,
		formatTime(time.Now().UTC()), id)
	if err != nil {
		return 0, fmt.Errorf("topup: 累计 poll_fail_count: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrPendingNotFound
	}
	var cnt int
	if err := s.db.QueryRowContext(ctx,
		`SELECT poll_fail_count FROM pending_topup WHERE id = ?`, id).Scan(&cnt); err != nil {
		return 0, fmt.Errorf("topup: 读 poll_fail_count: %w", err)
	}
	return cnt, nil
}
