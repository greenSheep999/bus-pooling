package kirodrop

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// **回归哨兵 · 2026-08-12**
//
// vendor 侧 stock 响应新形状带 `price:"7.35"` 字符串（USD）· 老 mapper 完全忽略 ·
// 导致 UnitPrice.Amount=0 · Prices 页显示为 0。修复：解析成 microunit USD Money。
//
// docs/18 §1.3 · Prober 落库时会经 vendor_pricing.credits_per_unit 换算成积分。
func TestParseUSDStringToMoney(t *testing.T) {
	cases := []struct {
		in      string
		wantAmt int64  // microunit
		wantCur string // Currency
	}{
		// 生产实测（kirodrop UI 2026-08-12 · $7.35）
		{"7.35", 7350000, string(providers.CurrencyUSD)},
		// 阶梯降价示例
		{"5.88", 5880000, string(providers.CurrencyUSD)},
		// 整数
		{"10", 10000000, string(providers.CurrencyUSD)},
		// 小数很短
		{"0.5", 500000, string(providers.CurrencyUSD)},
		// 6 位精确
		{"1.234567", 1234567, string(providers.CurrencyUSD)},
		// 超过 6 位 · 截断
		{"1.2345678", 1234567, string(providers.CurrencyUSD)},
		// 空 · 返零 Money（Currency 空）
		{"", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := parseUSDStringToMoney(tc.in)
			if got.Amount != tc.wantAmt {
				t.Errorf("parseUSDStringToMoney(%q).Amount = %d · 期 %d", tc.in, got.Amount, tc.wantAmt)
			}
			if string(got.Currency) != tc.wantCur {
				t.Errorf("parseUSDStringToMoney(%q).Currency = %q · 期 %q", tc.in, got.Currency, tc.wantCur)
			}
		})
	}
}

// **回归哨兵 · 2026-08-13**
//
// 本 vendor 只给 `region:"us-east-1"` · **不给 zone 短名** · 老 mapper 在
// "无 zones 数组" 的兜底分支里完全没填 Zone → 侧表 zone 列落空 →
// PricedFor 按 zone 查匹配不到（docs/19-fields.md §3）。
//
// 修复：兜底分支和 zones[] 分支都过 providers.ZoneOf 归一。
func TestToStockSnapshot_ZoneNormalized(t *testing.T) {
	t.Run("无 zones 数组 · 从 region 归一", func(t *testing.T) {
		sr := &stockResp{
			Stock:  []byte("5"),
			Region: "us-east-1",
			Price:  "7.35",
		}
		snap := toStockSnapshot(sr, nil)
		if len(snap.Zones) != 1 {
			t.Fatalf("应补一个默认 zone · got %d", len(snap.Zones))
		}
		z := snap.Zones[0]
		if z.Zone != providers.ZoneUS {
			t.Errorf("Zone = %q · want %q（老 bug 是落空）", z.Zone, providers.ZoneUS)
		}
		if z.Region != "us-east-1" {
			t.Errorf("Region = %q · 原文该保留", z.Region)
		}
		if z.UnitPrice.Amount != 7_350_000 || z.UnitPrice.Currency != providers.CurrencyUSD {
			t.Errorf("UnitPrice = %+v · want {7350000 USD}", z.UnitPrice)
		}
	})

	t.Run("eu region 也归一", func(t *testing.T) {
		sr := &stockResp{Stock: []byte("3"), Region: "eu-central-1", Price: "5.20"}
		snap := toStockSnapshot(sr, nil)
		if len(snap.Zones) != 1 || snap.Zones[0].Zone != providers.ZoneEU {
			t.Errorf("eu-central-1 应归一到 %q · got %+v", providers.ZoneEU, snap.Zones)
		}
	})

	t.Run("有 zones 数组 · 只给 region 时也归一", func(t *testing.T) {
		sr := &stockResp{
			Stock: []byte("0"),
			Zones: []zoneItem{{Region: "us-east-1", Available: 2, UnitPrice: 50}},
		}
		snap := toStockSnapshot(sr, nil)
		if len(snap.Zones) != 1 {
			t.Fatalf("got %d zones", len(snap.Zones))
		}
		if snap.Zones[0].Zone != providers.ZoneUS {
			t.Errorf("zones[] 分支 Zone = %q · want %q", snap.Zones[0].Zone, providers.ZoneUS)
		}
	})
}
