// backfill_probe_zone · 从历史 vendor_probe.raw_snapshot 回填 vendor_probe_zone 侧表。
//
// **场景**：migration 029 之前的探针行 · 侧表全空。raw_snapshot 里保留了完整 StockSnapshot
// JSON · 逐 zone 拆开 · 按 vendor_pricing 规则换算 · 落侧表。
//
// **幂等**：InsertBatch 走 INSERT OR REPLACE · 重跑不重复。
// **只回填 alive 且 raw_snapshot 非空的行** · 没 raw 的行跳过 ·
// 反正探针继续跑 · 后续新行会自动落侧表。

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/pricing"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
)

func runBackfillProbeZone(ctx context.Context, cfg config.Config, _ []string) error {
	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	// vendor_pricing 换算规则（跟 Prober 落库时同一条 · docs/10-pricing §1.3）
	pricingStore := pricing.NewStore(database.DB)
	zoneStore := vendorview.NewProbeZoneStore(database.DB)

	// 分页 · 一批 500 行 · 一批一事务 · 慢但省内存 + 崩了能续跑
	const batchSize = 500
	total, converted, skipped := 0, 0, 0
	var lastProbedAt string
	var lastVendorID string

	for {
		rows, err := database.DB.QueryContext(ctx, `
			SELECT vendor_id, probed_at, raw_snapshot
			  FROM vendor_probe
			 WHERE alive = 1
			   AND raw_snapshot IS NOT NULL
			   AND LENGTH(raw_snapshot) > 0
			   AND (probed_at, vendor_id) > (?, ?)
			 ORDER BY probed_at, vendor_id
			 LIMIT ?`, lastProbedAt, lastVendorID, batchSize)
		if err != nil {
			return fmt.Errorf("查历史探针: %w", err)
		}

		batch := make([]probeRow, 0, batchSize)
		for rows.Next() {
			var pr probeRow
			var raw []byte
			if err := rows.Scan(&pr.VendorID, &pr.ProbedAt, &raw); err != nil {
				rows.Close()
				return err
			}
			pr.Raw = raw
			batch = append(batch, pr)
		}
		rows.Close()

		if len(batch) == 0 {
			break
		}

		samples := make([]vendorview.ProbeZoneSample, 0, len(batch)*2)
		for _, pr := range batch {
			total++
			pt, err := time.Parse(time.RFC3339Nano, pr.ProbedAt)
			if err != nil {
				skipped++
				continue
			}
			zones := parseRawSnapshotZones(pr.Raw)
			if len(zones) == 0 {
				skipped++
				continue
			}
			for _, z := range zones {
				credits := convertOneToCredits(ctx, pricingStore, pr.VendorID, z.UnitPrice)
				samples = append(samples, vendorview.ProbeZoneSample{
					VendorID:       pr.VendorID,
					ProbedAt:       pt,
					Zone:           string(providers.ZoneOf(fallbackZone(z))),
					Region:         z.Region,
					Available:      z.Available,
					VendorCurrency: string(z.UnitPrice.Currency),
					VendorUnitRaw:  z.UnitPrice.Amount,
					OurUnitCredits: credits,
					OurUnitSource:  classifyPriceSource(string(z.UnitPrice.Currency)),
				})
				converted++
			}
			lastProbedAt = pr.ProbedAt
			lastVendorID = pr.VendorID
		}

		if err := zoneStore.InsertBatch(ctx, samples); err != nil {
			return fmt.Errorf("落侧表: %w", err)
		}

		slog.Info("回填进度",
			"processed_rows", total, "wrote_zone_rows", converted, "skipped", skipped)

		if len(batch) < batchSize {
			break
		}
	}

	slog.Info("回填完成", "processed_rows", total, "wrote_zone_rows", converted, "skipped", skipped)
	return nil
}

type probeRow struct {
	VendorID string
	ProbedAt string
	Raw      []byte
}

// rawSnapshot · 只拆我们要的字段（Zones）· 兼容各 vendor 的 raw json 差异
type rawSnapshot struct {
	Zones []providers.ZoneStock `json:"Zones"`
}

func parseRawSnapshotZones(raw []byte) []providers.ZoneStock {
	if len(raw) == 0 {
		return nil
	}
	var s rawSnapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	return s.Zones
}

// fallbackZone · z.Zone 空就用 z.Region · 单 zone vendor（部分 vendor）Zone 字段空
func fallbackZone(z providers.ZoneStock) string {
	if z.Zone != "" {
		return string(z.Zone)
	}
	return z.Region
}

// convertOneToCredits · 跟 Prober.computeCreditsFromMoney 同一条规则（docs/10-pricing §1.3）
func convertOneToCredits(
	ctx context.Context, store *pricing.Store, vendorID string, m providers.Money,
) int64 {
	if m.Amount == 0 {
		return 0
	}
	q := store.GetOrFallback(ctx, vendorID)
	perUnit := q.CreditsPerUnit
	if perUnit <= 0 {
		perUnit = 1_000_000
	}
	return m.Amount * perUnit / 1_000_000
}

// classifyPriceSource · 跟 Prober 那份对齐
func classifyPriceSource(cur string) string {
	if cur == providers.CurrencyUSD {
		return "computed_from_usd"
	}
	return "vendor_native"
}

// suppress unused import warning if refactored later
var _ = sql.ErrNoRows
