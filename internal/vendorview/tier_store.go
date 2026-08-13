package vendorview

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// TierStore 读写 vendor_price_tier 表：**数量分档**（tier_kind='qty_band'）+
// **时间降价**（tier_kind='time_decay'）· 两种阶梯共表 · 各清各的互不干扰（migration 035）。
// **纯内部 pricing 数据**（CLAUDE.md §0.1）· 经 vendorview 脱敏后才展示。
type TierStore struct {
	db *sql.DB
}

func NewTierStore(db *sql.DB) *TierStore {
	return &TierStore{db: db}
}

// nullIfZeroTime · 零值 time → NULL · 否则 RFC3339 字符串（tier_start_at 可空）
func nullIfZeroTime(t time.Time) sql.NullString {
	if t.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(time.RFC3339), Valid: true}
}

// ReplaceQtyBands 用最新一轮数量分档覆盖某 vendor 的旧 qty_band 行。
//
// 为什么"先删后插"而不是 upsert：分档表是**整表快照**（档数可能变：4 档变 3 档）·
// upsert 会残留已删除的档。按 (vendor_id, tier_kind='qty_band') 清掉旧的再插新的。
// 只动 qty_band 行 · 不碰 time_decay 行。
func (s *TierStore) ReplaceQtyBands(ctx context.Context, vendorID string, bands []providers.QtyPriceBand) error {
	if s.db == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 先清该 vendor 的旧 qty_band（不碰 time_decay）
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM vendor_price_tier WHERE vendor_id = ? AND tier_kind = 'qty_band'`,
		vendorID); err != nil {
		return fmt.Errorf("清旧 qty_band: %w", err)
	}
	if len(bands) == 0 {
		return tx.Commit() // 当前无分档 · 清空即可
	}

	now := time.Now().UTC().Format(time.RFC3339)
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vendor_price_tier (
			vendor_id, region, probed_at, tier_kind,
			tier_index, qty_lower, qty_upper,
			effective_at, unit_price_credits, created_at
		) VALUES (?, ?, ?, 'qty_band', ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vendor_id, region, probed_at, tier_index) DO UPDATE SET
			qty_lower          = excluded.qty_lower,
			qty_upper          = excluded.qty_upper,
			unit_price_credits = excluded.unit_price_credits
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, b := range bands {
		region := nullIfEmpty(b.Region)
		// effective_at 对数量分档无"时间"语义 · 填 now 满足 NOT NULL（时间列留空）
		if _, err := stmt.ExecContext(ctx,
			vendorID, region, now, i,
			b.Lower, nullIfZero(b.Upper), // upper=0（及以上）存 NULL
			now, b.UnitPriceCredits, now,
		); err != nil {
			return fmt.Errorf("插 qty_band: %w", err)
		}
	}
	return tx.Commit()
}

// ReplaceTimeDecay 用最新一轮时间降价 schedule 覆盖某 vendor 的旧 time_decay 行。
//
// 逐区一份 schedule（us/eu 各自 base + 降价档）· 每档一行（tier_index = 降价序号）。
// 先删后插同 ReplaceQtyBands · 只动 time_decay · 不碰 qty_band。
// **注意**：token 拿不到时上游返 nil（backfiller 不调本方法）· 保留上次落库值 · 不清空。
func (s *TierStore) ReplaceTimeDecay(ctx context.Context, vendorID string, tiers []providers.TieredPricing) error {
	if s.db == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM vendor_price_tier WHERE vendor_id = ? AND tier_kind = 'time_decay'`,
		vendorID); err != nil {
		return fmt.Errorf("清旧 time_decay: %w", err)
	}
	if len(tiers) == 0 {
		return tx.Commit()
	}

	now := time.Now().UTC().Format(time.RFC3339)
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vendor_price_tier (
			vendor_id, region, probed_at, tier_kind,
			tier_enabled, tier_active, tier_interval_min, tier_max_reductions,
			tier_applied, tier_start_at,
			tier_index, effective_at, unit_price_credits, unit_price_usd_raw, created_at
		) VALUES (?, ?, ?, 'time_decay', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, tp := range tiers {
		region := nullIfEmpty(tp.Region)
		startAt := nullIfZeroTime(tp.StartAt)
		for _, sc := range tp.Schedule {
			effAt := sc.EffectiveAt.UTC().Format(time.RFC3339)
			if _, err := stmt.ExecContext(ctx,
				vendorID, region, now,
				boolToInt(tp.Enabled), boolToInt(tp.Active), tp.IntervalMin, tp.MaxReductions,
				tp.Applied, startAt,
				sc.Index, effAt, sc.UnitPriceCredits, nullIfZeroInt64(sc.UnitPriceUSDRaw), now,
			); err != nil {
				return fmt.Errorf("插 time_decay: %w", err)
			}
		}
	}
	return tx.Commit()
}

// TimeDecayOf 读某 vendor 当前时间降价档（对账 / 脱敏展示用）· 按 region + tier_index 升序。
func (s *TierStore) TimeDecayOf(ctx context.Context, vendorID string) ([]providers.TieredPricing, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(region,''), COALESCE(tier_enabled,0), COALESCE(tier_active,0),
		       COALESCE(tier_interval_min,0), COALESCE(tier_max_reductions,0),
		       tier_index, effective_at, unit_price_credits, COALESCE(unit_price_usd_raw,0)
		  FROM vendor_price_tier
		 WHERE vendor_id = ? AND tier_kind = 'time_decay'
		 ORDER BY region ASC, tier_index ASC
	`, vendorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byRegion := map[string]*providers.TieredPricing{}
	var order []string
	for rows.Next() {
		var region, effAt string
		var enabled, active int
		var interval, maxRed, idx int
		var credits, usdRaw int64
		if err := rows.Scan(&region, &enabled, &active, &interval, &maxRed,
			&idx, &effAt, &credits, &usdRaw); err != nil {
			return nil, err
		}
		tp := byRegion[region]
		if tp == nil {
			tp = &providers.TieredPricing{
				Region: region, Enabled: enabled == 1, Active: active == 1,
				IntervalMin: interval, MaxReductions: maxRed,
			}
			byRegion[region] = tp
			order = append(order, region)
		}
		t, _ := time.Parse(time.RFC3339, effAt)
		tp.Schedule = append(tp.Schedule, providers.TierSchedule{
			Index: idx, EffectiveAt: t, UnitPriceCredits: credits, UnitPriceUSDRaw: usdRaw,
		})
	}
	out := make([]providers.TieredPricing, 0, len(order))
	for _, r := range order {
		out = append(out, *byRegion[r])
	}
	return out, rows.Err()
}

// QtyBandsOf 读某 vendor 当前数量分档（对外展示 / 对账用）· 按 tier_index 升序。
func (s *TierStore) QtyBandsOf(ctx context.Context, vendorID string) ([]providers.QtyPriceBand, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(region,''), qty_lower, COALESCE(qty_upper,0), unit_price_credits
		  FROM vendor_price_tier
		 WHERE vendor_id = ? AND tier_kind = 'qty_band'
		 ORDER BY tier_index ASC
	`, vendorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []providers.QtyPriceBand
	for rows.Next() {
		var b providers.QtyPriceBand
		var lower sql.NullInt64
		if err := rows.Scan(&b.Region, &lower, &b.Upper, &b.UnitPriceCredits); err != nil {
			return nil, err
		}
		b.Lower = int(lower.Int64)
		out = append(out, b)
	}
	return out, rows.Err()
}
