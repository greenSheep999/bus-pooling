package kirooo

import (
	"encoding/json"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// wire → 归一化。所有 本 vendor 字段口径的翻译都在这个文件里，
// adapter.go 只管发请求 / 判错。

// credits 把 本 vendor 的积分转成归一化 Money。
//
// 本 vendor 是 6 家里**唯一显式把积分挂钩 CNY 的家**（档案 §4：1 积分 = 1 元），
// 但仍标 CurrencyCredit —— 1:1 是当前兑换率，不是恒等式，让 decider 显式换算。
func credits(v int64) providers.Money {
	return providers.Money{Amount: creditToMicro(v), Currency: providers.CurrencyCredit}
}

// creditToMicro 积分 → microunit（1 积分 = 1_000_000 · CLAUDE.md §7.2 钱走整数）
func creditToMicro(v int64) int64 {
	return v * 1_000_000
}

// parseTime 解析 ISO-8601。空串或非法值 → nil（空 = 没这个时间点，不是零值时间）。
func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

// toKeyPayloads 翻译 keys[]。
func toKeyPayloads(items []keyItem) []providers.KeyPayload {
	if len(items) == 0 {
		return nil
	}
	out := make([]providers.KeyPayload, 0, len(items))
	for _, k := range items {
		out = append(out, providers.KeyPayload{
			VendorKeyID:   k.ID,
			Key:           k.Key,
			Account:       k.Account,
			Password:      k.Password,
			IssuerURL:     k.IssuerURL,
			Paid:          credits(k.Paid),
			WarrantyUntil: parseTime(k.WarrantyUntil),
			Free:          k.Free,
		})
	}
	return out
}

// toPurchaseResult 翻译 claim / order-keys 响应（形状相近）。
//
// requested 由调用方传：claim 传本次申请数，补拉传 0。
// 上层一律按 Purchased 处理（申请 5 拿到 3 是正常结果）。
func toPurchaseResult(pr *purchaseResp, requested int, replayed bool, raw json.RawMessage) *providers.PurchaseResult {
	return &providers.PurchaseResult{
		ClientOrderID: pr.ClientOrderID,
		VendorOrderID: pr.OrderID,
		Zone:          providers.Zone(pr.Zone),
		Requested:     requested,
		Purchased:     pr.Purchased,
		Keys:          toKeyPayloads(pr.Keys),
		UnitPrice:     credits(pr.UnitPrice),
		TotalCost:     credits(pr.TotalCredits),
		Remaining:     credits(pr.Remaining),
		WarrantyUntil: parseTime(pr.WarrantyUntil),
		Replayed:      replayed,
		Raw:           raw,
	}
}

// toStockSnapshotFromRegions · 从 /api/my/stock/regions 响应构造 snapshot。
//
// 优点（跟单值 /api/my/stock 比）：
//   - 有 region 拆分 · stock-delta 推算路径下能按区落 vendor_dispatch
//   - 同一次请求还带当前 fleet_session 的 dispatches[] · 后端 mapper 层不用另拉
//   - 库存端点跟 fleet 端点合一 · 60s 探针频次直接够画分区图
//
// 单价按 region 单独填 · Available 用总和（跨区总可购数 · 跟 stock 老口径一致）。
func toStockSnapshotFromRegions(rr *regionsResp, raw json.RawMessage) *providers.StockSnapshot {
	snap := &providers.StockSnapshot{
		VendorID:   providers.VendorKiroOOO,
		ObservedAt: time.Now().UTC(),
		Raw:        raw,
	}
	total := 0
	for _, r := range rr.Regions {
		snap.Zones = append(snap.Zones, providers.ZoneStock{
			Zone:      providers.Zone(r.Region),
			Region:    r.Region,
			Available: r.Stock,
			UnitPrice: credits(r.UnitPrice),
		})
		total += r.Stock
	}
	snap.Available = total
	return snap
}

// toStockSnapshot 翻译 stock 响应。
//
// 本 vendor 的 stock 主字段是 `claimable`（档案 §6），优先取；
// 若为 0 再回退到 public_available（兼容其他家常用字段口径）。
// toStockSnapshot 翻译 stock 响应 · 兼容 stock 字段的两种形状（见 types.go 注释）：
//   - 数字型（新）："stock": 7 · 同层还有 claimable / unit_price
//   - 对象型（旧）："stock": {"claimable": N, "public_available": M}
func toStockSnapshot(sr *stockResp, raw json.RawMessage) *providers.StockSnapshot {
	avail := 0
	if len(sr.Stock) > 0 {
		var n int
		if err := json.Unmarshal(sr.Stock, &n); err == nil {
			// 新形状 · stock 是数字 · claimable 是"我方现在能领几个"（更贴近可购量）
			avail = n
			if sr.Claimable > 0 && sr.Claimable < avail {
				avail = sr.Claimable
			}
		} else {
			var nested stockNested
			if err := json.Unmarshal(sr.Stock, &nested); err == nil {
				avail = nested.Claimable
				if avail == 0 {
					avail = nested.PublicAvailable
				}
			}
		}
	}

	snap := &providers.StockSnapshot{
		VendorID:        providers.VendorKiroOOO,
		ObservedAt:      time.Now().UTC(),
		Available:       avail,
		MinPerOrder:     sr.Min,
		MaxPerOrder:     sr.MaxPO,
		WarrantyMinutes: sr.WM,
		Raw:             raw,
	}
	for _, z := range sr.Zones {
		snap.Zones = append(snap.Zones, providers.ZoneStock{
			Zone:      providers.Zone(z.Zone),
			Region:    z.Region,
			Available: z.Available,
			UnitPrice: credits(z.UnitPrice),
		})
	}
	// 新形状没有 zones 数组 · 但有顶层 unit_price · 补一个默认 zone
	// 让 status 页的 region_count / 价格不是 0
	if len(snap.Zones) == 0 && sr.UnitPrice > 0 {
		snap.Zones = append(snap.Zones, providers.ZoneStock{
			Available: avail,
			UnitPrice: credits(sr.UnitPrice),
		})
	}
	return snap
}
