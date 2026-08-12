package decider

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// vendorMaxTotal · 把用户的积分上限换回 vendor 报价币种。
//
// 这是**静默出错高危**的换算：传错了会把涨价保护设成几倍宽松（等于没保护）·
// 或者几倍严格（一直 409 拉不到号）。所以每种币种组合都要锁住。
func TestVendorMaxTotal(t *testing.T) {
	const micro = 1_000_000

	cases := []struct {
		name         string
		maxUnitPrice int64 // 用户设的积分上限（microunit）
		count        int
		rawUnitPrice providers.Money  // vendor 原始报价
		unitCostHint int64            // 换成积分后的值
		want         *providers.Money // 期望传给 vendor 的总价上限
	}{
		{
			name:         "credit 家 · 等比退化成恒等",
			maxUnitPrice: 80 * micro,
			count:        3,
			rawUnitPrice: providers.Money{Amount: 60 * micro, Currency: providers.CurrencyCredit},
			unitCostHint: 60 * micro, // credit 家 raw == hint
			// 80 × 60/60 × 3 = 240
			want: &providers.Money{Amount: 240 * micro, Currency: providers.CurrencyCredit},
		},
		{
			name:         "USD 家 · 汇率 7 · 上限要换回 USD",
			maxUnitPrice: 70 * micro, // 用户说"最多 70 积分/个"
			count:        2,
			rawUnitPrice: providers.Money{Amount: 5 * micro, Currency: providers.CurrencyUSD}, // vendor 报 5 USD
			unitCostHint: 35 * micro,                                                          // 5 USD × 7 = 35 积分
			// 70 积分上限 ÷ (35 积分/5 USD) = 10 USD/个 · × 2 = 20 USD
			want: &providers.Money{Amount: 20 * micro, Currency: providers.CurrencyUSD},
		},
		{
			name:         "上限刚好等于当前价 · 换算后也刚好",
			maxUnitPrice: 35 * micro,
			count:        1,
			rawUnitPrice: providers.Money{Amount: 5 * micro, Currency: providers.CurrencyUSD},
			unitCostHint: 35 * micro,
			// 35 ÷ 7 = 5 USD
			want: &providers.Money{Amount: 5 * micro, Currency: providers.CurrencyUSD},
		},
		{
			name:         "用户没设上限 · 不设保护",
			maxUnitPrice: 0,
			count:        3,
			rawUnitPrice: providers.Money{Amount: 60 * micro, Currency: providers.CurrencyCredit},
			unitCostHint: 60 * micro,
			want:         nil,
		},
		{
			name:         "换算基准缺失 · 宁可不设也不设错",
			maxUnitPrice: 80 * micro,
			count:        3,
			rawUnitPrice: providers.Money{Amount: 60 * micro, Currency: providers.CurrencyCredit},
			unitCostHint: 0, // 缺
			want:         nil,
		},
		{
			name:         "count 0 · 防御",
			maxUnitPrice: 80 * micro,
			count:        0,
			rawUnitPrice: providers.Money{Amount: 60 * micro, Currency: providers.CurrencyCredit},
			unitCostHint: 60 * micro,
			want:         nil,
		},
		{
			name:         "上限极低 · 换算后归零则不设（避免传 0 被当无限制）",
			maxUnitPrice: 1, // 1 microunit · 极低
			count:        1,
			rawUnitPrice: providers.Money{Amount: 5 * micro, Currency: providers.CurrencyUSD},
			unitCostHint: 35 * micro,
			// 1 × 5_000_000 / 35_000_000 = 0（整数除法归零）→ 不设
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := vendorMaxTotal(tc.maxUnitPrice, tc.count, tc.rawUnitPrice, tc.unitCostHint)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("应返 nil（不设保护）· 得 %+v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("应返 %+v · 得 nil", *tc.want)
			}
			if got.Amount != tc.want.Amount {
				t.Errorf("Amount = %d · 期 %d", got.Amount, tc.want.Amount)
			}
			if got.Currency != tc.want.Currency {
				t.Errorf("Currency = %q · 期 %q（必须跟 vendor 报价币种一致）",
					got.Currency, tc.want.Currency)
			}
		})
	}
}

// **保护不能比用户设的更宽松** —— 这是这个换算唯一不能错的方向。
//
// 若换算后的 vendor 侧上限折回积分 > 用户设的上限 · 等于保护形同虚设。
func TestVendorMaxTotal_NeverLooserThanUserCap(t *testing.T) {
	const micro = 1_000_000
	// vendor 报 5 USD = 35 积分（汇率 7）
	raw := providers.Money{Amount: 5 * micro, Currency: providers.CurrencyUSD}
	hint := int64(35 * micro)

	for _, userCap := range []int64{35 * micro, 70 * micro, 100 * micro, 1000 * micro} {
		for _, count := range []int{1, 3, 10} {
			got := vendorMaxTotal(userCap, count, raw, hint)
			if got == nil {
				t.Fatalf("userCap=%d count=%d 应有保护", userCap, count)
			}
			// 折回积分：vendorTotal × (hint / raw)
			backToCredits := got.Amount * hint / raw.Amount
			userTotal := userCap * int64(count)
			if backToCredits > userTotal {
				t.Errorf("userCap=%d count=%d · 折回 %d 积分 > 用户上限 %d · 保护被放宽了",
					userCap, count, backToCredits, userTotal)
			}
		}
	}
}
