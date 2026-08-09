package kiroappcc

import (
	"encoding/json"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// wire → 归一化。本 vendor 字段口径的翻译都在这个文件里，
// adapter.go 只管发请求 / 判错。

// credits 把 本 vendor 的积分转成归一化 Money。
//
// vendor 档案 §4：单价 50 积分 / 个，币种就是积分（无 CNY 直换率暴露）。
// 标 CurrencyCredit 让 decider 显式做换算，换率变了只改一处。
func credits(v int64) providers.Money {
	return providers.Money{Amount: creditToMicro(v), Currency: providers.CurrencyCredit}
}

// creditToMicro 积分 → microunit（1 积分 = 1_000_000 · CLAUDE.md §7.2 钱走整数）
func creditToMicro(v int64) int64 {
	return v * 1_000_000
}

// toKeyPayloads 归一化 claim 响应的 keys。
//
// 本 vendor 的 KeyPayloadShape 是 JustKey —— account / password / issuer_url / region
// **一律没有**（Capability.KeyPayloadShape）。逐把的 Paid 也没有（vendor 只给 pointsCost
// 总额），Paid 留零值，权威值走 PurchaseResult.TotalCost。
func toKeyPayloads(single string, batch []string) []providers.KeyPayload {
	if single != "" {
		return []providers.KeyPayload{{Key: single}}
	}
	if len(batch) == 0 {
		return nil
	}
	out := make([]providers.KeyPayload, 0, len(batch))
	for _, k := range batch {
		out = append(out, providers.KeyPayload{Key: k})
	}
	return out
}

// toPurchaseResult 翻译 claim 响应。
//
// **本 vendor 没有 client_order_id / order_id / warranty_until** —— 那几个字段留空。
// **UnitPrice 走 stock 上次读到的 keyPrice**，vendor 在 claim 响应里不回 unit price，
// 但骨架期没有 stock 缓存，先把 UnitPrice 留零，让 decider 从上次 Stock 快照里取。
// **TotalCost = pointsCost**（车主自取时为 0 —— vendor 档案 §7 特权）。
// Replayed 恒为 false：本 vendor 无幂等键，"重放"这个概念不存在。
func toPurchaseResult(cr *claimResp, requested int, raw json.RawMessage) *providers.PurchaseResult {
	keys := toKeyPayloads(cr.Key, cr.Keys)
	return &providers.PurchaseResult{
		Zone:      providers.ZoneGeneral,
		Requested: requested,
		Purchased: len(keys),
		Keys:      keys,
		TotalCost: credits(cr.PointsCost),
		Replayed:  false,
		Raw:       raw,
	}
}

// toStockSnapshot 翻译 stock 响应。
//
// vendor 档案 §6：**无区域字段**、无 min/max per order、无质保分钟数。
// Zones 留空 —— 上层按 Capability.SupportsZones=false 判断分区能力。
func toStockSnapshot(sr *stockResp, raw json.RawMessage) *providers.StockSnapshot {
	return &providers.StockSnapshot{
		VendorID:   providers.VendorKiroAppCC,
		ObservedAt: time.Now().UTC(),
		Available:  sr.AvailableKeys,
		Raw:        raw,
	}
}
