package kirodrop

import (
	"encoding/json"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// wire → 归一化。所有 本 vendor 字段口径的翻译都在这个文件里，
// adapter.go 只管发请求 / 判错。

// credits 把 本 vendor 的积分转成归一化 Money。
//
// 本 vendor 的 credit 与 CNY 1:1，但**仍标成 CurrencyCredit 而不是 CNY** ——
// 1:1 是这家当前的兑换率，不是恒等式。标 credit 让 decider 显式做换算，换率变了只改一处。
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

// toKeyPayloads 翻译 keys[]。四件套 {key, account, password, issuer_url}。
// 逐把的 paid 是权威值（混价单里 Σ paid == total_credits）。
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

// toPurchaseResult 翻译 purchase / order-keys 的响应（两个端点同形状）。
//
// requested 由调用方传入：purchase 传本次申请数，补拉传 0（那时"申请数"已无意义，
// 用 Purchased 就够）。上层一律按 Purchased 处理（申请 5 拿到 3 是正常结果）。
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
// vendor 侧的 stock 字段有两种形状（见 types.go 注释）· 这里两种都尝试解一次：
//   - 数字型（新）："stock": 0 · 直接取 · 无 zones 字段
//   - 对象型（旧）："stock": {"public_available": N, "my_private": M} · 取 public_available
//
// Available **不含 my_private** —— 自己车的免费号不是"可买库存"，
// 混进去会让 decider 以为有货可拉。
func toStockSnapshot(sr *stockResp, raw json.RawMessage) *providers.StockSnapshot {
	available := 0
	if len(sr.Stock) > 0 {
		// 先尝试数字（新形状 · 大多数账户）
		var n int
		if err := json.Unmarshal(sr.Stock, &n); err == nil {
			available = n
		} else {
			// 再尝试对象（旧形状）
			var nested stockNested
			if err := json.Unmarshal(sr.Stock, &nested); err == nil {
				available = nested.PublicAvailable
			}
		}
	}

	snap := &providers.StockSnapshot{
		VendorID:        providers.VendorKiroDrop,
		ObservedAt:      time.Now().UTC(),
		Available:       available,
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
	// 新形状无 zones 数组 · 但 sr.Region 有值时补一个默认 zone · 让 status 页
	// region_count 能显示"1 区可用"而不是 0
	if len(snap.Zones) == 0 && sr.Region != "" {
		snap.Zones = append(snap.Zones, providers.ZoneStock{
			Region:    sr.Region,
			Available: available,
		})
	}
	return snap
}
