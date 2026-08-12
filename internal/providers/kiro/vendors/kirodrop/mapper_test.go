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
		in     string
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
