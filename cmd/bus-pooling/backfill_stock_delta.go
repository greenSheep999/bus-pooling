// backfill_stock_delta · 从历史 vendor_probe.raw_snapshot 复原漏掉的 restock 批次。
//
// **为什么会漏**（2026-08-13 生产实测）：`deriveStockDelta` 有个前置门 ——
// `len(sample.StockByRegion) > 0` 才推算。某家 vendor 的 adapter 把 `Zones` 留 nil ·
// 于是 `stock_by_region` 恒空 · **这家的 restock 一次都没推算过**。
// 实测那天：raw_snapshot 里躺着 16 批 287 个 key · vendor_dispatch 里一条都没有。
//
// 本命令把 raw_snapshot 里的库存序列重放一遍 · 找"库存由低变高"的点 · 补 dispatch。
//
// **幂等**：dispatch_key 用跟 deriveStockDelta 同一套规则
// （`delta-{zone}-{probed_at}`）· UpsertDispatches 走 upsert · 重跑不重复。
//
// **保守判据**（跟实时逻辑对齐 · 免得回填出一堆假批次）：
//   - 只在**相邻两轮都 alive** 且间隔 ≤ 2×探针间隔时算 delta（重启后首采不算）
//   - delta > 0 才算
//   - 缺 raw_snapshot 的行跳过

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
)

// 探针 baseline 间隔 · 跟 main.go 的 probeInterval 一致
const backfillProbeInterval = 60 * time.Second

func runBackfillStockDelta(ctx context.Context, cfg config.Config, args []string) error {
	// 可选参数：起始时间（RFC3339）· 默认最近 7 天
	since := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	if len(args) > 0 && args[0] != "" {
		if _, err := time.Parse(time.RFC3339, args[0]); err != nil {
			return fmt.Errorf("起始时间要 RFC3339 格式（例 2026-08-13T00:00:00Z）: %w", err)
		}
		since = args[0]
	}

	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	store := vendorview.NewOrderKeyStore(database.DB)

	// 先拿有哪些 vendor
	vendors, err := distinctProbeVendors(ctx, database, since)
	if err != nil {
		return err
	}
	slog.Info("回填 stock-delta 开始", "since", since, "vendors", len(vendors))

	totalFound, totalWrote := 0, 0
	for _, vid := range vendors {
		found, wrote, err := backfillOneVendor(ctx, database, store, vid, since)
		if err != nil {
			slog.Warn("回填失败 · 跳过该 vendor", "vendor", vid, "err", err)
			continue
		}
		totalFound += found
		totalWrote += wrote
		slog.Info("回填完成", "vendor", vid, "复原批次", found, "落库", wrote)
	}
	slog.Info("回填 stock-delta 结束", "复原批次合计", totalFound, "落库合计", totalWrote)
	return nil
}

func distinctProbeVendors(ctx context.Context, d *db.DB, since string) ([]string, error) {
	rows, err := d.DB.QueryContext(ctx,
		`SELECT DISTINCT vendor_id FROM vendor_probe WHERE probed_at >= ? ORDER BY vendor_id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// probePoint · 复原用的一轮快照
type probePoint struct {
	At    time.Time
	Alive bool
	// byZone 该轮每个 zone 的可购数（zone 已归一）
	byZone map[string]int
	// region 留痕（vendor 原文 · 可能空）
	regionOf map[string]string
}

func backfillOneVendor(
	ctx context.Context, d *db.DB, store *vendorview.OrderKeyStore, vendorID, since string,
) (found, wrote int, err error) {
	rows, err := d.DB.QueryContext(ctx, `
		SELECT probed_at, alive, raw_snapshot
		  FROM vendor_probe
		 WHERE vendor_id = ? AND probed_at >= ?
		   AND raw_snapshot IS NOT NULL AND LENGTH(raw_snapshot) > 0
		 ORDER BY probed_at`, vendorID, since)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	var series []probePoint
	for rows.Next() {
		var at string
		var alive int
		var raw []byte
		if err := rows.Scan(&at, &alive, &raw); err != nil {
			return 0, 0, err
		}
		t, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			continue
		}
		byZone, regionOf := zoneStockFromRaw(raw)
		if len(byZone) == 0 {
			continue
		}
		series = append(series, probePoint{
			At: t, Alive: alive != 0, byZone: byZone, regionOf: regionOf,
		})
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if len(series) < 2 {
		return 0, 0, nil
	}

	var dispatches []providers.VendorDispatch
	for i := 1; i < len(series); i++ {
		prev, cur := series[i-1], series[i]
		// 跟实时逻辑同一套保守判据
		if !prev.Alive || !cur.Alive {
			continue
		}
		gap := cur.At.Sub(prev.At)
		if gap <= 0 || gap > 2*backfillProbeInterval {
			continue
		}
		for zone, curAvail := range cur.byZone {
			delta := curAvail - prev.byZone[zone]
			if delta <= 0 {
				continue
			}
			found++
			raw, _ := json.Marshal(map[string]any{
				"kind":        "stock_delta",
				"zone":        zone,
				"region":      cur.regionOf[zone],
				"prev_stock":  prev.byZone[zone],
				"cur_stock":   curAvail,
				"delta":       delta,
				"gap_seconds": int(gap.Seconds()),
				"probed_at":   cur.At.UTC().Format(time.RFC3339),
				"backfilled":  true, // 标记来源 · 区分实时推算的
			})
			dispatches = append(dispatches, providers.VendorDispatch{
				// key 规则跟 deriveStockDelta 完全一致 · 保证幂等
				DispatchKey:  "delta-" + zone + "-" + cur.At.UTC().Format("20060102T150405Z"),
				Region:       cur.regionOf[zone],
				DispatchedAt: cur.At.UTC(),
				Count:        delta,
				Alive:        delta,
				Status:       "running",
				Raw:          raw,
			})
		}
	}
	if len(dispatches) == 0 {
		return found, 0, nil
	}
	if err := store.UpsertDispatches(ctx, vendorID, "vendor_self", dispatches); err != nil {
		return found, 0, err
	}
	return found, len(dispatches), nil
}

// zoneStockFromRaw · 从 raw_snapshot 复原每 zone 的可购数。
//
// **两条路**：
//  1. `Zones[]` 有内容 → 逐 zone 取（正常形状）
//  2. `Zones` 是 null（就是漏批次的那个 bug 的现场）→ 用顶级 `Available` 当单 zone ·
//     zone 名从 `Raw.region` 归一 · 都没有就归 general（无区 vendor）
func zoneStockFromRaw(raw []byte) (byZone map[string]int, regionOf map[string]string) {
	var snap struct {
		Available int                   `json:"Available"`
		Zones     []providers.ZoneStock `json:"Zones"`
		Raw       map[string]any        `json:"Raw"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, nil
	}
	byZone = make(map[string]int, 2)
	regionOf = make(map[string]string, 2)

	if len(snap.Zones) > 0 {
		for _, z := range snap.Zones {
			zk := providers.ZoneOf(string(z.Zone))
			if zk == "" {
				zk = providers.ZoneOf(z.Region)
			}
			byZone[string(zk)] = z.Available
			regionOf[string(zk)] = z.Region
		}
		return byZone, regionOf
	}

	// Zones 为 null 的历史行 —— 用顶级 Available 当单 zone
	zoneKey := ""
	if snap.Raw != nil {
		if r, ok := snap.Raw["region"].(string); ok {
			zoneKey = string(providers.ZoneOf(r))
		}
	}
	if zoneKey == "" {
		// 无区 vendor（响应里连 region 字段都没有）
		zoneKey = string(providers.ZoneGeneral)
	}
	byZone[zoneKey] = snap.Available
	regionOf[zoneKey] = ""
	return byZone, regionOf
}
