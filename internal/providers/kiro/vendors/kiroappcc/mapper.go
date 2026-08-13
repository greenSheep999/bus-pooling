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
//
// **必须给一条 ZoneGeneral**（2026-08-13 修 · 老代码 Zones 留 nil）：
// 下游全部按 `len(snap.Zones) > 0` 才干活 —— Zones 空意味着
//
//	· vendor_probe_zone 侧表零行（定价查不到这家）
//	· our_unit_credits 不落（明明有 keyPrice）
//	· stock_by_region 空 → stock-delta 推不出 restock → 抢号链收不到这家的补货
//
// 本 vendor 无区概念 · 用 ZoneGeneral 占位就是它的"唯一一区"。
func toStockSnapshot(sr *stockResp, raw json.RawMessage) *providers.StockSnapshot {
	snap := &providers.StockSnapshot{
		VendorID:   providers.VendorKiroAppCC,
		ObservedAt: time.Now().UTC(),
		Available:  sr.AvailableKeys,
		Raw:        raw,
	}
	// keyPrice 是唯一一档单价（无区拆分）· 0 表示 vendor 没报价 · 不造零价 zone
	if sr.KeyPrice > 0 || sr.AvailableKeys > 0 {
		snap.Zones = []providers.ZoneStock{{
			Zone:      providers.ZoneGeneral,
			Region:    "", // vendor 无 region 概念 · 空是事实
			Available: sr.AvailableKeys,
			UnitPrice: credits(sr.KeyPrice),
		}}
	}
	return snap
}
