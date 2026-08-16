package kirooo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 个人号货架 · 跟企业号是**两套独立端点**（docs/vendors/kiro-ooo.md §2.3b / §2.4b）：
//
//	                企业号                        个人号
//	查库存   /api/my/stock/regions          /api/my/stock/personal-pool
//	下单     /api/my/keys/claim             /api/my/keys/claim-personal
//	分区     ✅ us-east-1 / eu-central-1     ❌ 无区概念
//	单价     100（逐区）                     50 基准 + 内嵌数量分档
//	数量分档 /api/my/key-price-tiers（flat）  响应内嵌 tiers[]（真有折扣）
//
// 实测确认（2026-08-16）· 详细响应体见档案 §2.3b。

// personalPoolResp · GET /api/my/stock/personal-pool 响应
type personalPoolResp struct {
	OK        bool `json:"ok"`
	Stock     int  `json:"stock"`     // 个人池库存（跟企业池独立）
	Remaining int  `json:"remaining"` // 剩余配额
	Credits   int  `json:"credits"`   // 我方在该 vendor 的余额
	CanBuy    bool `json:"can_buy"`
	Afford    int  `json:"afford"`
	// UnitPrice 基准单价（积分/个）· 数量不够任何 tier 时用它
	UnitPrice int `json:"unit_price"`
	// Tiers 数量分档 · min_qty 起该价 · 实测 [{min_qty:10, unit_price:40}]
	Tiers []personalTier `json:"tiers"`
	// UserSpecialPrice 是否走了我方专属价（true 时上面的价已是专属价）
	UserSpecialPrice bool `json:"user_special_price"`
}

type personalTier struct {
	MinQty    int `json:"min_qty"`
	UnitPrice int `json:"unit_price"`
}

// stockPersonal · 个人号货架快照。
//
// 个人池**无区** —— 返回单个 ZoneStock（Zone=general）· 别硬塞 us/eu
// （`docs/11-fields.md §3.1` 定死 zone/region 分工）。
func (a *Adapter) stockPersonal(ctx context.Context) (*providers.StockSnapshot, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/stock/personal-pool", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError(resp)
	}
	var pp personalPoolResp
	if err := json.Unmarshal(resp.Body, &pp); err != nil {
		return nil, fmt.Errorf("kirooo: 解析 personal-pool: %w", err)
	}

	unit := providers.Money{Amount: int64(pp.UnitPrice) * 1_000_000, Currency: "credit"}
	return &providers.StockSnapshot{
		VendorID:    providers.VendorKiroOOO,
		ObservedAt:  time.Now().UTC(),
		Available:   pp.Stock,
		MinPerOrder: 1,
		MaxPerOrder: pp.Stock, // 个人池不单独给上限 · 最多买光库存
		Zones: []providers.ZoneStock{{
			Zone:      providers.ZoneGeneral,
			Available: pp.Stock,
			UnitPrice: unit,
		}},
		Balance:         providers.Money{Amount: int64(pp.Credits) * 1_000_000, Currency: "credit"},
		WarrantyMinutes: 0, // 个人池响应不给质保 · 别照抄企业池的 10 分钟
		Raw:             resp.Body,
	}, nil
}

// purchasePersonal · 个人号下单 · POST /api/my/keys/claim-personal
//
// 参数形状跟企业池 claim 一致（count + client_order_id）· **无 region**（不分区）·
// **无 plan**（该池买前不能选档 · 见 ListPersonalTiers 注释）。
func (a *Adapter) purchasePersonal(ctx context.Context, req providers.PurchaseRequest) (*providers.PurchaseResult, error) {
	body := purchaseReq{
		Count:         req.Count,
		ClientOrderID: req.ClientOrderID,
	}
	payload, _ := json.Marshal(body)
	httpReq, err := a.newReq(ctx, http.MethodPost, "/api/my/keys/claim-personal", payload)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError(resp)
	}
	var pr purchaseResp
	if err := json.Unmarshal(resp.Body, &pr); err != nil {
		return nil, fmt.Errorf("kirooo: 解析 claim-personal: %w", err)
	}
	// Replayed 恒 false · 同企业池（响应体无字段区分首次成交与重放）
	res := toPurchaseResult(&pr, req.Count, false, resp.Body)
	// 个人池无区 · 覆盖掉 mapper 可能填的区值
	res.Zone = providers.ZoneGeneral
	return res, nil
}

// ListPersonalTiers · 个人池的**数量分档**（复用 providers.QtyPriceBand 契约）。
//
// 分档来自 personal-pool 响应内嵌的 `tiers[]` + `unit_price` 基准价，拼成连续区间：
//
//	unit_price=50 · tiers=[{min_qty:10, unit_price:40}]
//	  → [{Lower:1, Upper:9, 50}, {Lower:10, Upper:0, 40}]   (Upper=0 = 及以上)
//
// ⚠️ **跟企业池的 /api/my/key-price-tiers 不是一回事** —— 那个实测 4 档全 100（flat 无折扣）·
// 真折扣在个人池这里。
//
// ⚠️ **这个池买前不能选订阅档** —— 实测 `?plan=pro|pro_plus|pro_max` 三值返回完全相同
// （参数被忽略）· 所以 Capability.SelectablePlans[personal] 留空 · 号是哪档只能
// 导入后从 housepool 的 Subscription 观察。
func (a *Adapter) ListPersonalTiers(ctx context.Context) ([]providers.QtyPriceBand, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/stock/personal-pool", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError(resp)
	}
	var pp personalPoolResp
	if err := json.Unmarshal(resp.Body, &pp); err != nil {
		return nil, fmt.Errorf("kirooo: 解析 personal-pool tiers: %w", err)
	}
	return personalBands(pp.UnitPrice, pp.Tiers), nil
}

// personalBands · 基准价 + min_qty 分档 → 连续的 QtyPriceBand 区间。
//
// 上游只给"N 个起多少钱"，我方契约要的是闭区间 —— 用下一档的 min_qty-1 当上界，
// 最高档 Upper=0（及以上）。tiers 为空时只有基准档一条。
func personalBands(basePrice int, tiers []personalTier) []providers.QtyPriceBand {
	toMicro := func(p int) int64 { return int64(p) * 1_000_000 }

	// 按 min_qty 升序（上游顺序不保证）
	sorted := make([]personalTier, len(tiers))
	copy(sorted, tiers)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].MinQty < sorted[j-1].MinQty; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	out := make([]providers.QtyPriceBand, 0, len(sorted)+1)
	lower := 1
	price := basePrice
	for _, t := range sorted {
		// min_qty<=1 时基准档不存在（第一档就从 1 起）
		if t.MinQty > lower {
			out = append(out, providers.QtyPriceBand{
				Lower:            lower,
				Upper:            t.MinQty - 1,
				UnitPriceCredits: toMicro(price),
				Region:           "", // 个人池无区
			})
		}
		lower = t.MinQty
		price = t.UnitPrice
	}
	// 最高档 · Upper=0 表示"及以上"
	out = append(out, providers.QtyPriceBand{
		Lower:            lower,
		Upper:            0,
		UnitPriceCredits: toMicro(price),
		Region:           "",
	})
	return out
}
