// Package decider 跨 vendor 决策 + 发起拉号 + 记账 + 存进号池。
//
// 一次拉号横跨 3 个系统（vendor / 号池 / 本地库）且无两阶段提交，
// 所以建成持久化状态机，每步崩溃后有确定的恢复规则（09-transactions §2）。
package decider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Status 是 pending_purchase 的状态。取值跟 06-db-schema §20 的 CHECK 约束一致。
type Status string

const (
	// StatusInitial 已写行，未做外部动作 · 崩了直接删
	StatusInitial Status = "initial"
	// StatusReserved 已冻结，未调 vendor · 崩了可安全释放冻结
	StatusReserved Status = "reserved"
	// StatusPurchasing 请求已发 vendor、响应未确认。
	// **崩在这里不能释放冻结** —— vendor 可能已扣款，释放等于我方吃亏（§2.1）
	StatusPurchasing Status = "purchasing"
	// StatusPurchased vendor 已出号，未入号池 · 崩了用 OrderKeys 补拉
	StatusPurchased Status = "purchased"
	// StatusImported 号已进池，未落 ledger · 崩了补记账
	StatusImported  Status = "imported"
	StatusCompleted Status = "completed"
	// StatusCancelledReserve 冻结已释放（未发生 vendor 扣款）
	StatusCancelledReserve Status = "cancelled_reserve"
	// StatusNeedRecoverVendor vendor 成功但补拉失败多次 · 报警
	StatusNeedRecoverVendor Status = "need_recover_vendor"
	// StatusNeedManual 号池导入反复失败，或无幂等键的 vendor 崩在 purchasing · 报警
	StatusNeedManual Status = "need_manual"
)

// Pending 是一行 pending_purchase。
type Pending struct {
	ID                  string
	IdempotencyRecordID string
	PassengerID         string
	// BusID 空 = 单独拉号（进 record group）
	BusID string
	// TargetGroup bus-<id> | record-<pid>
	TargetGroup    string
	VendorID       string
	ClientOrderID  string
	CountRequested int
	// ReservedAmount 冻结的积分（microunit）
	ReservedAmount int64
	Status         Status
	VendorOrderID  string
	PullRoundID    string
	Error          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

var (
	// ErrStaleTransition 当前状态跟期望不一致，说明已被别人推进 —— 并发保护生效，不是 bug
	ErrStaleTransition = errors.New("decider: 状态已被别人推进")
	ErrPendingNotFound = errors.New("decider: 找不到这笔待处理拉号")
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Create 写 initial 行。
func (s *Store) Create(ctx context.Context, p Pending) (string, error) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pending_purchase
		  (id, idempotency_record_id, passenger_id, bus_id, target_group, vendor_id,
		   client_order_id, count_requested, reserved_amount, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.IdempotencyRecordID, p.PassengerID, nullIfEmpty(p.BusID), p.TargetGroup,
		p.VendorID, p.ClientOrderID, p.CountRequested, p.ReservedAmount,
		string(StatusInitial), formatTime(now), formatTime(now))
	if err != nil {
		return "", fmt.Errorf("decider: 写 pending_purchase: %w", err)
	}
	return p.ID, nil
}

// Advance 条件推进：只有当前状态等于 from 时才改成 to。
//
// 条件是并发保护 —— janitor 和请求线程可能同时看到同一行，
// 不加条件两边都会往下走，同一笔单会扣两次款、导两次号。
func (s *Store) Advance(ctx context.Context, id string, from, to Status) error {
	return s.advance(ctx, id, from, to, nil)
}

// AdvanceWith 推进状态并同时写字段。
//
// 字段跟状态必须在同一条 UPDATE 里，否则崩在中间会留下
// 「状态是 purchased 但没有 vendor_order_id」的行，恢复时不知道补拉哪个订单。
func (s *Store) AdvanceWith(ctx context.Context, id string, from, to Status, set Fields) error {
	return s.advance(ctx, id, from, to, &set)
}

// Fields 是推进状态时顺带写的字段。零值 = 不改。
type Fields struct {
	VendorOrderID string
	PullRoundID   string
	Error         string
}

func (s *Store) advance(ctx context.Context, id string, from, to Status, set *Fields) error {
	query := `UPDATE pending_purchase SET status = ?, updated_at = ?`
	args := []any{string(to), formatTime(time.Now().UTC())}

	if set != nil {
		if set.VendorOrderID != "" {
			query += `, vendor_order_id = ?`
			args = append(args, set.VendorOrderID)
		}
		if set.PullRoundID != "" {
			query += `, pull_round_id = ?`
			args = append(args, set.PullRoundID)
		}
		if set.Error != "" {
			query += `, error = ?`
			args = append(args, set.Error)
		}
	}
	query += ` WHERE id = ? AND status = ?`
	args = append(args, id, string(from))

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("decider: 推进状态 %s→%s: %w", from, to, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// 分清「行不存在」和「状态不匹配」：后者是正常并发，前者是 bug
		var cur string
		err := s.db.QueryRowContext(ctx,
			`SELECT status FROM pending_purchase WHERE id = ?`, id).Scan(&cur)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPendingNotFound
		}
		return fmt.Errorf("%w（想从 %s 推到 %s，实际是 %s）", ErrStaleTransition, from, to, cur)
	}
	return nil
}

// Get 读一行。
func (s *Store) Get(ctx context.Context, id string) (*Pending, error) {
	return s.scanOne(s.db.QueryRowContext(ctx, selectPending+` WHERE id = ?`, id))
}

// FindByClientOrderID 按 vendor 幂等键找 · 恢复对账用。
func (s *Store) FindByClientOrderID(ctx context.Context, vendorID, clientOrderID string) (*Pending, error) {
	return s.scanOne(s.db.QueryRowContext(ctx,
		selectPending+` WHERE vendor_id = ? AND client_order_id = ?`, vendorID, clientOrderID))
}

const selectPending = `
	SELECT id, idempotency_record_id, passenger_id, COALESCE(bus_id, ''), target_group,
	       vendor_id, client_order_id, count_requested, reserved_amount, status,
	       COALESCE(vendor_order_id, ''), COALESCE(pull_round_id, ''), COALESCE(error, ''),
	       created_at, updated_at
	  FROM pending_purchase`

func (s *Store) scanOne(row *sql.Row) (*Pending, error) {
	var p Pending
	var status, createdAt, updatedAt string
	err := row.Scan(&p.ID, &p.IdempotencyRecordID, &p.PassengerID, &p.BusID, &p.TargetGroup,
		&p.VendorID, &p.ClientOrderID, &p.CountRequested, &p.ReservedAmount, &status,
		&p.VendorOrderID, &p.PullRoundID, &p.Error, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPendingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("decider: 读 pending_purchase: %w", err)
	}
	p.Status = Status(status)
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	return &p, nil
}

// FindStale 找卡在某状态超过 olderThan 的行 · janitor 用。
//
// 按 updated_at 不按 created_at：关心的是在**当前**状态卡了多久。
func (s *Store) FindStale(ctx context.Context, status Status, olderThan time.Duration, limit int) ([]Pending, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	rows, err := s.db.QueryContext(ctx,
		selectPending+` WHERE status = ? AND updated_at < ? ORDER BY updated_at LIMIT ?`,
		string(status), formatTime(cutoff), limit)
	if err != nil {
		return nil, fmt.Errorf("decider: 扫超时单: %w", err)
	}
	defer rows.Close()

	var out []Pending
	for rows.Next() {
		var p Pending
		var st, createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.IdempotencyRecordID, &p.PassengerID, &p.BusID, &p.TargetGroup,
			&p.VendorID, &p.ClientOrderID, &p.CountRequested, &p.ReservedAmount, &st,
			&p.VendorOrderID, &p.PullRoundID, &p.Error, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Status = Status(st)
		p.CreatedAt = parseTime(createdAt)
		p.UpdatedAt = parseTime(updatedAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ── 工具 ────────────────────────────────────────────

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// timeLayout 定宽 ISO-8601（UTC），跟 wallet / passenger 一致。
//
// 不能用 RFC3339Nano：它省掉小数尾随零，字符串比较会反过来（'Z' > '.'），
// FindStale 的 `updated_at < ?` 会静默漏掉该恢复的单子。
const timeLayout = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) time.Time {
	for _, layout := range []string{timeLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
