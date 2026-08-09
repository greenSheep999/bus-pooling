// Package pricing 管 vendor 报价换算 + surcharge 规则。
//
// docs/decisions §8.30 A · B：定价栈的两个核心表。
//   - vendor_pricing · 各家 vendor 报价 → 我方积分的换算率 · 加 vendor 层分项
//   - surcharge_rule · 5 类分项统一到一张表（1b P1-2B 实现）
//
// **不硬编费率 / 汇率** —— 都从 DB 读·后台可调。空表时回落到 config env（1a 行为）。
package pricing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// microUnit 1 单位 = 1_000_000 microunit（跟 wallet / topup 一致）
const microUnit int64 = 1_000_000

// VendorQuote · vendor 报价换算规则（vendor_pricing 表一行）。
type VendorQuote struct {
	VendorID          string
	QuoteCurrency     string // CNY | USD | credit
	CreditsPerUnit    int64  // microunit · 1 单位 vendor 报价 = X microunit 我方积分
	RateSource        string // manual | api
	RateUpdatedAt     time.Time
	VendorSurchargeBp int64 // basis point · 500 = 5%
	Active            bool
}

// ErrNotFound · vendor_pricing 里没有这个 vendor
var ErrNotFound = errors.New("pricing: vendor 未配置换算规则")

// Store · vendor_pricing 表的 CRUD。
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Get 拿某个 vendor 的换算规则。**只返 active=1 的行**·关闭的 vendor 走 fallback。
func (s *Store) Get(ctx context.Context, vendorID string) (VendorQuote, error) {
	var (
		q         VendorQuote
		updatedAt string
		active    int
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT vendor_id, quote_currency, credits_per_unit, rate_source,
		       rate_updated_at, vendor_surcharge_bp, active
		  FROM vendor_pricing WHERE vendor_id = ? AND active = 1`,
		vendorID).Scan(&q.VendorID, &q.QuoteCurrency, &q.CreditsPerUnit,
		&q.RateSource, &updatedAt, &q.VendorSurchargeBp, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return VendorQuote{}, ErrNotFound
	}
	if err != nil {
		return VendorQuote{}, fmt.Errorf("pricing: 查 vendor_pricing: %w", err)
	}
	q.Active = active == 1
	q.RateUpdatedAt = parseTime(updatedAt)
	return q, nil
}

// Upsert 写入或覆盖一行 · 后台配置 / 运营调汇率用。
func (s *Store) Upsert(ctx context.Context, q VendorQuote) error {
	if q.CreditsPerUnit <= 0 {
		return fmt.Errorf("pricing: credits_per_unit 必须 > 0")
	}
	switch q.QuoteCurrency {
	case "CNY", "USD", "credit":
	default:
		return fmt.Errorf("pricing: 不支持的 quote_currency: %q", q.QuoteCurrency)
	}
	if q.RateSource == "" {
		q.RateSource = "manual"
	}
	if q.RateUpdatedAt.IsZero() {
		q.RateUpdatedAt = time.Now().UTC()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vendor_pricing
		  (vendor_id, quote_currency, credits_per_unit, rate_source,
		   rate_updated_at, vendor_surcharge_bp, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vendor_id) DO UPDATE SET
		  quote_currency      = excluded.quote_currency,
		  credits_per_unit    = excluded.credits_per_unit,
		  rate_source         = excluded.rate_source,
		  rate_updated_at     = excluded.rate_updated_at,
		  vendor_surcharge_bp = excluded.vendor_surcharge_bp,
		  active              = excluded.active,
		  updated_at          = excluded.updated_at`,
		q.VendorID, q.QuoteCurrency, q.CreditsPerUnit, q.RateSource,
		q.RateUpdatedAt.Format(time.RFC3339Nano), q.VendorSurchargeBp,
		boolToInt(q.Active), now, now)
	if err != nil {
		return fmt.Errorf("pricing: upsert vendor_pricing: %w", err)
	}
	return nil
}

// ConvertToCredits 把 vendor 原始报价的**一个单位**换算成我方积分 microunit。
//
// vendor 报价的语义各家不同：
//   - CNY 家：unit_price 是"每号多少 CNY"·例 30 → 30 * credits_per_unit
//   - USD 家：unit_price 是"每号多少 USD"·例 5 USD × 7 CNY/USD = 35 → 5 * credits_per_unit
//   - credit 家：unit_price 是"每号多少积分"（vendor 内部积分·跟我方 1:1）
//
// **不做 vendor_surcharge**（那是 Price 的下一层） · 这里只做币种换算。
func (q VendorQuote) ConvertToCredits(rawUnits int64) int64 {
	// rawUnits 可以是整数 CNY / USD / credit · credits_per_unit 是 microunit
	// 换算：rawUnits × credits_per_unit 直接就是 microunit
	return rawUnits * q.CreditsPerUnit
}

// FallbackQuote · 表里没配时的兜底 · CNY 1:1 · 无 vendor_surcharge
func FallbackQuote(vendorID string) VendorQuote {
	return VendorQuote{
		VendorID:       vendorID,
		QuoteCurrency:  "CNY",
		CreditsPerUnit: microUnit, // 1 CNY = 1 积分（1a 默认行为·所有 vendor 都当 CNY 计）
		RateSource:     "fallback",
		Active:         true,
	}
}

// GetOrFallback · Get 的便利封装 · vendor 未配时走 FallbackQuote 而不报错。
// 上层用它·让 vendor 表未 seed 时也能跑通（1a → 1b 平滑过渡）。
func (s *Store) GetOrFallback(ctx context.Context, vendorID string) VendorQuote {
	q, err := s.Get(ctx, vendorID)
	if err != nil {
		return FallbackQuote(vendorID)
	}
	return q
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
