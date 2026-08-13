package kirooo

import (
	"encoding/json"
	"testing"
)

// 真实响应 fixture（2026-08-14 · vendor-probe 抓的原文）
const realKeyTiersBody = `{"active":true,"bands":[` +
	`{"lower":1,"upper":5,"price":100},{"lower":6,"upper":10,"price":100},` +
	`{"lower":11,"upper":20,"price":100},{"lower":21,"upper":0,"price":100}],` +
	`"base":100,"code":0,"has_base":true,` +
	`"tiers":[{"id":1,"produced":5,"unit_price":100,"operator":"manual-us100","created_at":"2026-07-31 12:32:51"}]}`

func TestKeyTiers_RealShape(t *testing.T) {
	var body keyTiersResp
	if err := json.Unmarshal([]byte(realKeyTiersBody), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Bands) != 4 {
		t.Fatalf("应 4 档 · 得 %d", len(body.Bands))
	}
	// 第 1 档 1-5 价 100
	if body.Bands[0].Lower != 1 || body.Bands[0].Upper != 5 || body.Bands[0].Price != 100 {
		t.Errorf("第 1 档错 · %+v", body.Bands[0])
	}
	// 最高档 upper=0（21 及以上）
	if body.Bands[3].Lower != 21 || body.Bands[3].Upper != 0 {
		t.Errorf("最高档应 21+ · %+v", body.Bands[3])
	}
	// price → microunit 换算（100 积分 = 100_000_000）· 在 ListKeyTiers 里做 · 这里验字段解析
	if body.Base != 100 || !body.Active {
		t.Errorf("base/active 解析错 · base=%d active=%v", body.Base, body.Active)
	}
}
