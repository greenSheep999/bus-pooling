package decider

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// vendorMaxTotal · 把用户的积分上限换回 vendor 报价币种。
//
// 这是**静默出错高危**的换算：传错了会把涨价保护设成几倍宽松（等于没保护）·
// 或者几倍严格（一直 409 拉不到号）。所以每种币种组合都要锁住。
//
// 换算走 vendor_pricing.credits_per_unit 的逆式（docs/18 §1.3）·
// 不再依赖"本轮快照的 raw/hint 等比映射"（估价基准现在可能来自库里上一轮探针）。
func TestVendorMaxTotal(t *testing.T) {
	const micro = 1_000_000

	cases := []struct {
		name           string
		maxUnitPrice   int64 // 用户设的积分上限（microunit）
		count          int
		rawUnitPrice   providers.Money  // vendor 原始报价（只取币种标签）
		creditsPerUnit int64            // vendor_pricing.credits_per_unit
		want           *providers.Money // 期望传给 vendor 的总价上限
	}{
		{
			name:           "credit 家 · 逆式退化成恒等",
			maxUnitPrice:   80 * micro,
			count:          3,
			rawUnitPrice:   providers.Money{Amount: 60 * micro, Currency: providers.CurrencyCredit},
			creditsPerUnit: 1 * micro, // 1:1
			// 80 × 1 × 3 = 240
			want: &providers.Money{Amount: 240 * micro, Currency: providers.CurrencyCredit},
		},
		{
			name:           "USD 家 · 汇率 7 · 上限要换回 USD",
			maxUnitPrice:   70 * micro, // 用户说"最多 70 积分/个"
			count:          2,
			rawUnitPrice:   providers.Money{Amount: 5 * micro, Currency: providers.CurrencyUSD},
			creditsPerUnit: 7 * micro, // 1 USD = 7 积分
			// 70 积分 ÷ 7 = 10 USD/个 · × 2 = 20 USD
			want: &providers.Money{Amount: 20 * micro, Currency: providers.CurrencyUSD},
		},
		{
			name:           "上限刚好等于当前价 · 换算后也刚好",
			maxUnitPrice:   35 * micro,
			count:          1,
			rawUnitPrice:   providers.Money{Amount: 5 * micro, Currency: providers.CurrencyUSD},
			creditsPerUnit: 7 * micro,
			// 35 ÷ 7 = 5 USD
			want: &providers.Money{Amount: 5 * micro, Currency: providers.CurrencyUSD},
		},
		{
			name:           "用户没设上限 · 不设保护",
			maxUnitPrice:   0,
			count:          3,
			rawUnitPrice:   providers.Money{Amount: 60 * micro, Currency: providers.CurrencyCredit},
			creditsPerUnit: 1 * micro,
			want:           nil,
		},
		{
			name:           "换算规则缺失 · 宁可不设也不设错",
			maxUnitPrice:   80 * micro,
			count:          3,
			rawUnitPrice:   providers.Money{Amount: 60 * micro, Currency: providers.CurrencyCredit},
			creditsPerUnit: 0, // 缺
			want:           nil,
		},
		{
			name:           "count 0 · 防御",
			maxUnitPrice:   80 * micro,
			count:          0,
			rawUnitPrice:   providers.Money{Amount: 60 * micro, Currency: providers.CurrencyCredit},
			creditsPerUnit: 1 * micro,
			want:           nil,
		},
		{
			name:           "上限极低 · 换算后归零则不设（避免传 0 被当无限制）",
			maxUnitPrice:   1, // 1 microunit · 极低
			count:          1,
			rawUnitPrice:   providers.Money{Amount: 5 * micro, Currency: providers.CurrencyUSD},
			creditsPerUnit: 7 * micro,
			// 1 × 1_000_000 / 7_000_000 = 0（整数除法归零）→ 不设
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := vendorMaxTotal(tc.maxUnitPrice, tc.count, tc.rawUnitPrice, tc.creditsPerUnit)
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
	raw := providers.Money{Amount: 5 * micro, Currency: providers.CurrencyUSD}

	// 各种汇率都试 · 包括除不尽的（6.8 是当前实际配的值）
	for _, perUnit := range []int64{1 * micro, 7 * micro, 6_800_000, 3_333_333} {
		for _, userCap := range []int64{35 * micro, 70 * micro, 100 * micro, 1000 * micro} {
			for _, count := range []int{1, 3, 10} {
				got := vendorMaxTotal(userCap, count, raw, perUnit)
				if got == nil {
					t.Fatalf("perUnit=%d userCap=%d count=%d 应有保护", perUnit, userCap, count)
				}
				// 折回积分：vendorTotal × credits_per_unit / 1_000_000（入库时的正式）
				backToCredits := got.Amount * perUnit / micro
				userTotal := userCap * int64(count)
				if backToCredits > userTotal {
					t.Errorf("perUnit=%d userCap=%d count=%d · 折回 %d 积分 > 用户上限 %d · 保护被放宽了",
						perUnit, userCap, count, backToCredits, userTotal)
				}
			}
		}
	}
}
