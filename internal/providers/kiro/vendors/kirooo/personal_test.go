package kirooo

import (
	"encoding/json"
	"testing"
)

// personalBands 是计费入口的一环（数量分档决定实扣单价）· 边界算错就是真钱错。
func TestPersonalBands(t *testing.T) {
	tests := []struct {
		name  string
		base  int
		tiers []personalTier
		want  []struct {
			lower, upper int
			credits      int64
		}
	}{
		{
			// 实测形状（2026-08-16）· base 50 · 10 个起 40
			name:  "实测·单档",
			base:  50,
			tiers: []personalTier{{MinQty: 10, UnitPrice: 40}},
			want: []struct {
				lower, upper int
				credits      int64
			}{
				{1, 9, 50_000_000},
				{10, 0, 40_000_000}, // Upper=0 = 及以上
			},
		},
		{
			name:  "无分档·只基准价",
			base:  50,
			tiers: nil,
			want: []struct {
				lower, upper int
				credits      int64
			}{
				{1, 0, 50_000_000},
			},
		},
		{
			name: "多档·上游乱序也要排好",
			base: 60,
			tiers: []personalTier{
				{MinQty: 50, UnitPrice: 30},
				{MinQty: 10, UnitPrice: 45},
				{MinQty: 20, UnitPrice: 38},
			},
			want: []struct {
				lower, upper int
				credits      int64
			}{
				{1, 9, 60_000_000},
				{10, 19, 45_000_000},
				{20, 49, 38_000_000},
				{50, 0, 30_000_000},
			},
		},
		{
			// min_qty=1 时基准档不该出现（否则会多一条 Lower>Upper 的空区间）
			name:  "首档从1起·无基准档",
			base:  50,
			tiers: []personalTier{{MinQty: 1, UnitPrice: 42}},
			want: []struct {
				lower, upper int
				credits      int64
			}{
				{1, 0, 42_000_000},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := personalBands(tc.base, tc.tiers)
			if len(got) != len(tc.want) {
				t.Fatalf("档数 = %d, want %d · got=%+v", len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				if got[i].Lower != w.lower || got[i].Upper != w.upper {
					t.Errorf("[%d] 区间 = [%d,%d], want [%d,%d]",
						i, got[i].Lower, got[i].Upper, w.lower, w.upper)
				}
				if got[i].UnitPriceCredits != w.credits {
					t.Errorf("[%d] 单价 = %d, want %d", i, got[i].UnitPriceCredits, w.credits)
				}
				if got[i].Region != "" {
					t.Errorf("[%d] 个人池无区 · Region 应为空, got %q", i, got[i].Region)
				}
			}
		})
	}
}

// 区间必须连续无空洞 —— 有洞会导致某个数量算不出价
func TestPersonalBands_区间连续(t *testing.T) {
	bands := personalBands(60, []personalTier{
		{MinQty: 10, UnitPrice: 45},
		{MinQty: 20, UnitPrice: 38},
	})
	for i := 1; i < len(bands); i++ {
		if bands[i].Lower != bands[i-1].Upper+1 {
			t.Errorf("档 %d 与 %d 之间有空洞: 前档 Upper=%d · 本档 Lower=%d",
				i-1, i, bands[i-1].Upper, bands[i].Lower)
		}
	}
	if last := bands[len(bands)-1]; last.Upper != 0 {
		t.Errorf("最高档 Upper 应为 0(及以上), got %d", last.Upper)
	}
}

// 真实响应体能解出来（字段名拼错就在这里炸）
func TestPersonalPoolResp_解析实测响应(t *testing.T) {
	// 2026-08-16 真调 GET /api/my/stock/personal-pool 的原样响应
	raw := `{"afford":0,"can_buy":false,"credits":10,"ok":true,"remaining":10,
	         "stock":0,"tiers":[{"min_qty":10,"unit_price":40}],
	         "unit_price":50,"user_special_price":false}`
	var pp personalPoolResp
	if err := json.Unmarshal([]byte(raw), &pp); err != nil {
		t.Fatal(err)
	}
	if pp.UnitPrice != 50 {
		t.Errorf("unit_price = %d, want 50", pp.UnitPrice)
	}
	if len(pp.Tiers) != 1 || pp.Tiers[0].MinQty != 10 || pp.Tiers[0].UnitPrice != 40 {
		t.Errorf("tiers 解析错: %+v", pp.Tiers)
	}
	if pp.Remaining != 10 {
		t.Errorf("remaining = %d, want 10", pp.Remaining)
	}
	if !pp.OK {
		t.Error("ok 应为 true")
	}
}
