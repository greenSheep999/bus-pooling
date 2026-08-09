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

// toStockSnapshot 翻译 stock 响应。
//
// 本 vendor 的 stock 主字段是 `claimable`（档案 §6），优先取；
// 若为 0 再回退到 public_available（兼容其他家常用字段口径）。
func toStockSnapshot(sr *stockResp, raw json.RawMessage) *providers.StockSnapshot {
	avail := sr.Stock.Claimable
	if avail == 0 {
		avail = sr.Stock.PublicAvailable
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
	return snap
}
