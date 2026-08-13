package vendorview

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// TierStore 读写 vendor_price_tier 表的**数量分档**部分（migration 035 · tier_kind='qty_band'）。
//
// 时间降价部分（tier_kind='time_decay'）由 Prober 从 snapshot.TieredPricing 落 · 各走各的。
// **纯内部 pricing 数据**（CLAUDE.md §0.1）· 经 vendorview 脱敏后才展示。
type TierStore struct {
	db *sql.DB
}

func NewTierStore(db *sql.DB) *TierStore {
	return &TierStore{db: db}
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
