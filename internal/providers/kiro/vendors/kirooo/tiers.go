package kirooo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ListKeyTiers · GET /api/my/key-price-tiers · vendor 侧**数量分档**表（阶梯价格 · docs/20）。
//
// **响应形状实测确认**（2026-08-14 · vendor-probe · 真路径是 /api/my/ 不是文档写的 /my/）：
//
//	{"active":true,"has_base":true,"base":100,
//	 "bands":[{"lower":1,"upper":5,"price":100},{"lower":6,"upper":10,"price":100},
//	          {"lower":11,"upper":20,"price":100},{"lower":21,"upper":0,"price":100}],
//	 "tiers":[{"id":1,"produced":5,"unit_price":100,"operator":"...","created_at":"..."}]}
//
// 要点：
//   - `bands[].{lower,upper,price}` 是分档表 · **upper=0 表示"及以上"**（最高档无上限）
//   - `price` 单位是积分（kirooo 1 积分 = 1 CNY = 1 RMB · §1.4）→ ×1_000_000 到 microunit
//   - 端点不带 region · 是账号级分档 · 落库 region="" (全区)
//   - 当前实测各档同价（flat · 无实际折扣）· 但结构真实 · vendor 一开分档就自动反映
func (a *Adapter) ListKeyTiers(ctx context.Context) ([]providers.QtyPriceBand, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/key-price-tiers", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kirooo: key-price-tiers: http %d", resp.StatusCode)
	}
	var body keyTiersResp
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return nil, fmt.Errorf("kirooo: key-price-tiers 解析: %w", err)
	}

	out := make([]providers.QtyPriceBand, 0, len(body.Bands))
	for _, b := range body.Bands {
		out = append(out, providers.QtyPriceBand{
			Lower:            b.Lower,
			Upper:            b.Upper, // 0 = 及以上
			UnitPriceCredits: int64(b.Price) * 1_000_000,
			Region:           "", // 端点不分区 · 账号级
		})
	}
	return out, nil
}

type keyTiersResp struct {
	Active  bool           `json:"active"`
	HasBase bool           `json:"has_base"`
	Base    int            `json:"base"`
	Bands   []keyTiersBand `json:"bands"`
}

type keyTiersBand struct {
	Lower int `json:"lower"`
	Upper int `json:"upper"` // 0 = 及以上
	Price int `json:"price"` // 积分
}
