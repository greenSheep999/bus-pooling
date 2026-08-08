// Package strategy 存乘客的全局策略，并在拉号前判「当下能不能拉」。
//
// 两类字段语义完全不同（decisions §8.27 / 06-db-schema §16），**别混**：
//
//   - **硬上限** MaxUnitPrice / DailyRoundLimit / DailySpendLimit
//     每次拉号前校验，超了**拒绝**。
//   - **新车默认值** PerRoundCount / PreferredVendor / DefaultZone
//     只在建车时填初值，改它**不影响已有的车**。
//
// 本包**不选 vendor、不动号池**（03-modules §7）—— 只产出 Intent 交给 decider。
package strategy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Zone 取值。auto = 让系统决定（不是"某个区"）。
const (
	ZoneAuto = "auto"
	ZoneUS   = "us"
	ZoneEU   = "eu"
)

// 默认值 —— 乘客从没配过策略时用这套。
//
// 三个硬上限默认 **nil（不限）**：给一个"合理"的默认上限等于替乘客做决定，
// 而他根本没设过 —— 拉不动号时也无从知道是被谁拦的。
const defaultPerRoundCount = 1

// Strategy 是一份全局策略。指针字段 nil = 不限 / 让系统决定。
type Strategy struct {
	PassengerID string

	// ── 硬上限（超了拒绝拉号）──
	// MaxUnitPrice microunit · 单价超它就不拉 · nil = 不限
	MaxUnitPrice *int64
	// DailyRoundLimit 每天最多几轮 · **跨所有车累加** · nil = 不限
	DailyRoundLimit *int
	// DailySpendLimit microunit · 每天最多花多少 · 跨所有车累加 · nil = 不限
	DailySpendLimit *int64

	// ── 新车默认值（改它不动已有的车）──
	PerRoundCount   int
	PreferredVendor *string
	DefaultZone     string

	UpdatedAt time.Time
}

// Defaults 是没存过策略时的那份。
//
// 单独一个函数而不是散在 Get 里 —— PUT 的部分更新也要拿它当底，
// 两处必须是同一套值，否则"没配过"和"配过又清空"会得到不同行为。
func Defaults(passengerID string) Strategy {
	return Strategy{
		PassengerID:   passengerID,
		PerRoundCount: defaultPerRoundCount,
		DefaultZone:   ZoneAuto,
	}
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Get 读策略。**没存过不是错误** —— 返回默认值（乘客注册后就该能拉号，
// 不该因为没进过设置页而卡住）。
func (s *Store) Get(ctx context.Context, passengerID string) (Strategy, error) {
	out := Defaults(passengerID)
	var (
		maxUnitPrice sql.NullInt64
		roundLimit   sql.NullInt64
		spendLimit   sql.NullInt64
		perRound     sql.NullInt64
		vendor       sql.NullString
		zone         string
		updatedAt    string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT max_unit_price, daily_round_limit, daily_spend_limit,
		       per_round_count, preferred_vendor, default_zone, updated_at
		  FROM passenger_strategy_default
		 WHERE passenger_id = ?`, passengerID).
		Scan(&maxUnitPrice, &roundLimit, &spendLimit, &perRound, &vendor, &zone, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return Strategy{}, fmt.Errorf("strategy: 读策略: %w", err)
	}

	if maxUnitPrice.Valid {
		out.MaxUnitPrice = &maxUnitPrice.Int64
	}
	if roundLimit.Valid {
		n := int(roundLimit.Int64)
		out.DailyRoundLimit = &n
	}
	if spendLimit.Valid {
		out.DailySpendLimit = &spendLimit.Int64
	}
	if perRound.Valid {
		out.PerRoundCount = int(perRound.Int64)
	}
	if vendor.Valid && vendor.String != "" {
		out.PreferredVendor = &vendor.String
	}
	if zone != "" {
		out.DefaultZone = zone
	}
	out.UpdatedAt = parseTime(updatedAt)
	return out, nil
}

// Patch 是部分更新 —— 只有非 nil 的字段会被写。
//
// **为什么套两层指针**：`*(*int64)` 分得清「没提这个字段」（外层 nil）和
// 「显式设成不限」（外层非 nil、内层 nil）。用一层的话，客户端想清空上限
// 就没法表达（传 null 会被当成"没提"，上限永远清不掉）。
type Patch struct {
	MaxUnitPrice    **int64
	DailyRoundLimit **int
	DailySpendLimit **int64
	PerRoundCount   *int
	PreferredVendor **string
	DefaultZone     *string
}

var (
	ErrBadZone          = errors.New("strategy: 区域取值非法")
	ErrBadPerRoundCount = errors.New("strategy: 每轮数量超范围")
	ErrNegativeLimit    = errors.New("strategy: 上限不能为负")
)

// maxPerRoundCount 跟 vendor 侧单次提货上限对齐（91kiro 是 200）。
// 这里拦一道是为了早失败 —— 免得走到 vendor 才被 bad_count 拒。
const maxPerRoundCount = 200

// Put 应用 patch 并落库（upsert）。
func (s *Store) Put(ctx context.Context, passengerID string, p Patch) (Strategy, error) {
	cur, err := s.Get(ctx, passengerID)
	if err != nil {
		return Strategy{}, err
	}

	if p.MaxUnitPrice != nil {
		cur.MaxUnitPrice = *p.MaxUnitPrice
	}
	if p.DailyRoundLimit != nil {
		cur.DailyRoundLimit = *p.DailyRoundLimit
	}
	if p.DailySpendLimit != nil {
		cur.DailySpendLimit = *p.DailySpendLimit
	}
	if p.PerRoundCount != nil {
		cur.PerRoundCount = *p.PerRoundCount
	}
	if p.PreferredVendor != nil {
		cur.PreferredVendor = *p.PreferredVendor
	}
	if p.DefaultZone != nil {
		cur.DefaultZone = *p.DefaultZone
	}

	if err := validate(cur); err != nil {
		return Strategy{}, err
	}

	cur.UpdatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO passenger_strategy_default
		  (passenger_id, max_unit_price, daily_round_limit, daily_spend_limit,
		   per_round_count, preferred_vendor, default_zone, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (passenger_id) DO UPDATE SET
		  max_unit_price    = excluded.max_unit_price,
		  daily_round_limit = excluded.daily_round_limit,
		  daily_spend_limit = excluded.daily_spend_limit,
		  per_round_count   = excluded.per_round_count,
		  preferred_vendor  = excluded.preferred_vendor,
		  default_zone      = excluded.default_zone,
		  updated_at        = excluded.updated_at`,
		passengerID, nullInt64(cur.MaxUnitPrice), nullInt(cur.DailyRoundLimit),
		nullInt64(cur.DailySpendLimit), cur.PerRoundCount,
		nullString(cur.PreferredVendor), cur.DefaultZone,
		cur.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Strategy{}, fmt.Errorf("strategy: 写策略: %w", err)
	}
	return cur, nil
}

func validate(s Strategy) error {
	switch s.DefaultZone {
	case ZoneAuto, ZoneUS, ZoneEU:
	default:
		return fmt.Errorf("%w: %q", ErrBadZone, s.DefaultZone)
	}
	if s.PerRoundCount < 1 || s.PerRoundCount > maxPerRoundCount {
		return fmt.Errorf("%w: %d", ErrBadPerRoundCount, s.PerRoundCount)
	}
	// 负上限不是"不限"，是配错了 —— 会让每次拉号都被拦，且很难看出原因。
	// 想不限就传 null。
	if s.MaxUnitPrice != nil && *s.MaxUnitPrice < 0 {
		return fmt.Errorf("%w: max_unit_price=%d", ErrNegativeLimit, *s.MaxUnitPrice)
	}
	if s.DailyRoundLimit != nil && *s.DailyRoundLimit < 0 {
		return fmt.Errorf("%w: daily_round_limit=%d", ErrNegativeLimit, *s.DailyRoundLimit)
	}
	if s.DailySpendLimit != nil && *s.DailySpendLimit < 0 {
		return fmt.Errorf("%w: daily_spend_limit=%d", ErrNegativeLimit, *s.DailySpendLimit)
	}
	return nil
}

// ── 工具 ────────────────────────────────────────────

func nullInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullString(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
