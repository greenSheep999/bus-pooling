// Package coupon 优惠码 · 一次性减免 · decisions §8.43 v2
//
// 两种 type(互斥):
//   topup_discount     · 充值弹窗输 · 减 USD 实付 · discount_bp 百分点(500 = 5%)
//   service_fee_waiver · 拉号确认窗输 · 免 N 轮服务费 · waive_rounds 轮数
//
// 跟四码分离铁律(§8.42)对齐:
//   - 不改 tier
//   - 不建 referral
//   - 不解锁 vendor 真名
//   - 单次生效 · remaining_uses / expires_at 硬上限
//
// 核销原子性:Validate + MarkUsed 走同事务 · 防并发超用。
package coupon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Type 优惠码类型 · 数据契约 · 落 coupon_code.type
type Type string

const (
	TypeTopupDiscount     Type = "topup_discount"
	TypeServiceFeeWaiver  Type = "service_fee_waiver"
)

// Context 核销上下文 · 落 coupon_use.context
type Context string

const (
	ContextTopup Context = "topup"
	ContextPull  Context = "pull"
)

// Coupon 主表行
type Coupon struct {
	ID             string
	Code           string
	Type           Type
	DiscountBP     int64 // topup_discount 用 · 百分点(500=5%)
	WaiveRounds    int64 // service_fee_waiver 用 · 免几轮
	RemainingUses  sql.NullInt64
	UsedCount      int64
	ExpiresAt      sql.NullTime
	Status         string
	Memo           string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Store 服务 · 一进程一个
type Store struct{ db *sql.DB }

// NewStore · caller 传 db (跟 wallet / topup 一样风格)
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// 错误集
var (
	ErrNotFound        = errors.New("coupon: 码不存在")
	ErrDisabled        = errors.New("coupon: 码已停用")
	ErrExpired         = errors.New("coupon: 码已过期")
	ErrUsedUp          = errors.New("coupon: 码额度用尽")
	ErrWrongContext    = errors.New("coupon: 码不适用此场景") // topup 输 pull 码 · 或反过来
	ErrAlreadyUsed     = errors.New("coupon: 此单已核销过其他码")
)

// Lookup · 只读校验 · 不消耗额度。用于 UI 试算/预览。
//
// **不落 coupon_use** · 只判"码有效且适用此场景"。
// 想真核销走 Redeem(带事务 · 减 remaining_uses + 落 coupon_use)。
func (s *Store) Lookup(ctx context.Context, code string, wantType Type) (*Coupon, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, ErrNotFound
	}
	c, err := s.loadByCode(ctx, s.db, code)
	if err != nil {
		return nil, err
	}
	if c.Status != "active" {
		return nil, ErrDisabled
	}
	if c.Type != wantType {
		return nil, ErrWrongContext
	}
	if c.ExpiresAt.Valid && time.Now().UTC().After(c.ExpiresAt.Time) {
		return nil, ErrExpired
	}
	if c.RemainingUses.Valid && c.RemainingUses.Int64-c.UsedCount <= 0 {
		return nil, ErrUsedUp
	}
	return c, nil
}

// RedeemInput · Redeem 的入参 · 同一 context+ref 幂等
type RedeemInput struct {
	Code           string  // 用户输的码
	PassengerID    string
	Context        Context
	ContextRef     string  // topup_order.id / pull_round.id
	DiscountAmount int64   // 折后减了多少 microunit(topup)或轮数(pull)· 上层算好传下来
}

// Redeem · 原子核销 · Lookup 校验 → 减 used_count → 落 coupon_use
//
// 幂等: 同一 (context, context_ref) 二次调返 (nil, ErrAlreadyUsed) · 不重复减额度。
//       调方决定是否忽略(重放场景)。
//
// 事务: BEGIN IMMEDIATE 独占 · 并发场景不会超用(SQLite 单写 · 天然序列化)。
func (s *Store) Redeem(ctx context.Context, in RedeemInput) (*Coupon, error) {
	wantType := TypeTopupDiscount
	if in.Context == ContextPull {
		wantType = TypeServiceFeeWaiver
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("coupon: 开事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	c, err := s.lookupTx(ctx, tx, in.Code, wantType)
	if err != nil {
		return nil, err
	}

	// 幂等: 已核销就返 ErrAlreadyUsed · 不重复扣
	var existing int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(1) FROM coupon_use WHERE context = ? AND context_ref = ?`,
		string(in.Context), in.ContextRef).Scan(&existing); err != nil {
		return nil, fmt.Errorf("coupon: 查幂等: %w", err)
	}
	if existing > 0 {
		return nil, ErrAlreadyUsed
	}

	// 扣额度 · 用 CAS(compare-and-set)避免并发超用
	// remaining_uses 是 NULL 时不减 · 只增 used_count
	res, err := tx.ExecContext(ctx, `
		UPDATE coupon_code
		   SET used_count = used_count + 1,
		       updated_at = ?
		 WHERE id = ?
		   AND (remaining_uses IS NULL OR used_count < remaining_uses)`,
		time.Now().UTC().Format(time.RFC3339Nano), c.ID)
	if err != nil {
		return nil, fmt.Errorf("coupon: 扣额度: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrUsedUp
	}

	// 落 coupon_use
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO coupon_use
		  (id, coupon_code_id, passenger_id, context, context_ref, discount_amount, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), c.ID, in.PassengerID, string(in.Context), in.ContextRef,
		in.DiscountAmount, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return nil, fmt.Errorf("coupon: 落 coupon_use: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("coupon: 提交事务: %w", err)
	}
	return c, nil
}

// CreateInput · 建码用(admin / seed / test)
type CreateInput struct {
	Code           string
	Type           Type
	DiscountBP     int64 // topup_discount 用
	WaiveRounds    int64 // service_fee_waiver 用
	RemainingUses  int64 // 0 = NULL 不限
	ExpiresAt      time.Time // zero = NULL 不限时
	Memo           string
}

// Create · 建一张码。参数校验参考 §8.43 表结构 CHECK。
func (s *Store) Create(ctx context.Context, in CreateInput) (*Coupon, error) {
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if code == "" {
		return nil, errors.New("coupon: code 不能空")
	}
	switch in.Type {
	case TypeTopupDiscount:
		if in.DiscountBP <= 0 || in.DiscountBP > 10000 {
			return nil, errors.New("coupon: topup_discount 需 discount_bp 1..10000")
		}
		if in.WaiveRounds != 0 {
			return nil, errors.New("coupon: topup_discount 不能带 waive_rounds")
		}
	case TypeServiceFeeWaiver:
		if in.WaiveRounds <= 0 {
			return nil, errors.New("coupon: service_fee_waiver 需 waive_rounds >= 1")
		}
		if in.DiscountBP != 0 {
			return nil, errors.New("coupon: service_fee_waiver 不能带 discount_bp")
		}
	default:
		return nil, fmt.Errorf("coupon: 未知 type %q", in.Type)
	}

	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var (
		discountBP  any = nil
		waiveRounds any = nil
		remaining   any = nil
		expiresAt   any = nil
		memo        any = nil
	)
	if in.Type == TypeTopupDiscount {
		discountBP = in.DiscountBP
	} else {
		waiveRounds = in.WaiveRounds
	}
	if in.RemainingUses > 0 {
		remaining = in.RemainingUses
	}
	if !in.ExpiresAt.IsZero() {
		expiresAt = in.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if in.Memo != "" {
		memo = in.Memo
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO coupon_code
		  (id, code, type, discount_bp, waive_rounds, remaining_uses, expires_at, status, memo, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)`,
		id, code, string(in.Type), discountBP, waiveRounds, remaining, expiresAt, memo, now, now,
	); err != nil {
		return nil, fmt.Errorf("coupon: 插入: %w", err)
	}
	return s.loadByCode(ctx, s.db, code)
}

// ── 内部 ──

func (s *Store) loadByCode(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, code string) (*Coupon, error) {
	var (
		c              Coupon
		discountBP     sql.NullInt64
		waiveRounds    sql.NullInt64
		expiresAt      sql.NullString
		memo           sql.NullString
		createdAt      string
		updatedAt      string
	)
	err := q.QueryRowContext(ctx, `
		SELECT id, code, type, discount_bp, waive_rounds, remaining_uses, used_count,
		       expires_at, status, memo, created_at, updated_at
		  FROM coupon_code WHERE code = ?`, code).Scan(
		&c.ID, &c.Code, &c.Type, &discountBP, &waiveRounds, &c.RemainingUses, &c.UsedCount,
		&expiresAt, &c.Status, &memo, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if discountBP.Valid {
		c.DiscountBP = discountBP.Int64
	}
	if waiveRounds.Valid {
		c.WaiveRounds = waiveRounds.Int64
	}
	if expiresAt.Valid {
		t, perr := time.Parse(time.RFC3339Nano, expiresAt.String)
		if perr == nil {
			c.ExpiresAt = sql.NullTime{Time: t, Valid: true}
		}
	}
	if memo.Valid {
		c.Memo = memo.String
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &c, nil
}

// lookupTx 事务版 · 只做 read 部分 · 校验规则跟 Lookup 一致
func (s *Store) lookupTx(ctx context.Context, tx *sql.Tx, code string, wantType Type) (*Coupon, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, ErrNotFound
	}
	c, err := s.loadByCode(ctx, tx, code)
	if err != nil {
		return nil, err
	}
	if c.Status != "active" {
		return nil, ErrDisabled
	}
	if c.Type != wantType {
		return nil, ErrWrongContext
	}
	if c.ExpiresAt.Valid && time.Now().UTC().After(c.ExpiresAt.Time) {
		return nil, ErrExpired
	}
	if c.RemainingUses.Valid && c.RemainingUses.Int64-c.UsedCount <= 0 {
		return nil, ErrUsedUp
	}
	return c, nil
}
