package vendorview

import (
	"context"
	"strings"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// I-19 · Offers 端点定价必须走 baseCredits + finalUnitPrice(docs/10-pricing §4)。
//
// 这三个用例覆盖:
//   1. CNY 家(credit / CNY 币种) · unit_price 应含全部分项
//   2. USD 家 · exchange_rate 必须换成积分 microunit
//   3. 三档 tier 减免正确应用
//
// 老 bug:offersFromSnapshot 直接透传 z.UnitPrice.Amount ·
// CNY 家凑巧 1:1"看着对"·USD 家 18.51 USD 被前端当 18.51 积分显示。

// TestOffers_CNYVendor_AppliesPricingStack · CNY 家 100 CNY · retail 应含全部分项
func TestOffers_CNYVendor_AppliesPricingStack(t *testing.T) {
	reg := providers.NewRegistry()
	v := &mockVendor{
		id: providers.Vendor91Kiro, name: "V1",
		unitPrice: 100_000_000, available: 10, // 100 CNY
	}
	_ = reg.Register(v, true)

	svc, err := New(Config{
		Registry: reg,
		Rates:    decider.Rates{RegionMarkup: 2000, Service: 500}, // 20% + 5%
	})
	if err != nil {
		t.Fatal(err)
	}

	got := svc.Offers(context.Background(), Viewer{Tier: TierRetail})
	if len(got.Vendors) == 0 {
		t.Fatal("no vendors")
	}
	// 100 → RegionMarkup 20% → 120 → Service 5% → 126
	// 老 bug:直接返 100_000_000 · 现在应是 126_000_000
	enterprise := got.Vendors[0].Categories[providers.AccountEnterprise]
	if len(enterprise.Offers) == 0 {
		t.Fatal("no enterprise offers")
	}
	got0 := enterprise.Offers[0].UnitPrice
	if got0 != 126_000_000 {
		t.Errorf("retail CNY 单价 = %d · want 126_000_000 (region 20 + service 5)", got0)
	}
}

// TestOffers_USDVendor_ConvertsCurrency · USD 家 · 必须过 baseCredits 换汇率
//
// 模拟 USD 家 18.51 USD 报价 · pricing.QuoteFor 返 credits_per_unit=6_800_000
// (即 1 USD = 6.8 积分 · docs/10-pricing §1.3)
// 期望:18.51 USD → 125.868 CNY 积分 → 计费栈 → 最终 microunit
func TestOffers_USDVendor_ConvertsCurrency(t *testing.T) {
	reg := providers.NewRegistry()
	v := &mockVendor{
		id: providers.VendorKiroDrop, name: "V6",
		unitPrice: 18_510_000, available: 5, // 18.51 USD microunit
	}
	// 覆盖默认 zone · 显式标 USD 币种
	v.zones = []providers.ZoneStock{{
		Zone: providers.ZoneUS, Region: "us-east-1", Available: 5,
		UnitPrice: providers.Money{
			Amount: 18_510_000, Currency: providers.CurrencyUSD,
		},
	}}
	_ = reg.Register(v, true)

	svc, err := New(Config{
		Registry: reg,
		Pricing:  &stubPricing{creditsPerUnit: 6_800_000},        // 1 USD = 6.8 积分
		Rates:    decider.Rates{RegionMarkup: 2000, Service: 500}, // 20% + 5%
	})
	if err != nil {
		t.Fatal(err)
	}

	got := svc.Offers(context.Background(), Viewer{Tier: TierRetail})
	if len(got.Vendors) == 0 {
		t.Fatal("no vendors")
	}
	enterprise := got.Vendors[0].Categories[providers.AccountEnterprise]
	if len(enterprise.Offers) == 0 {
		t.Fatal("no enterprise offers · V6 应支持企业")
	}
	// 18.51 USD × 6.8 = 125.868 积分 → 125_868_000 microunit
	// × 1.20 (region) = 151_041_600
	// × 1.05 (service) = 158_593_680
	got0 := enterprise.Offers[0].UnitPrice
	want := int64(158_593_680)
	// 允许 ±1 microunit 的整数除法尾差
	if diff := got0 - want; diff > 1 || diff < -1 {
		t.Errorf("retail USD 单价 = %d · want ≈%d (18.51 USD × 6.8 汇率 × 计费栈)", got0, want)
	}
	// 且**绝对不能**是老 bug 的 18_510_000(USD microunit 当积分)
	if got0 == 18_510_000 {
		t.Fatal("命中老 bug:USD amount 直接当积分透传 · baseCredits 换算没生效")
	}
}

// TestOffers_TierDifference_AppliedToOffers · 三档减免正确
func TestOffers_TierDifference_AppliedToOffers(t *testing.T) {
	reg := providers.NewRegistry()
	v := &mockVendor{
		id: providers.Vendor91Kiro, name: "V1",
		unitPrice: 100_000_000, available: 10,
	}
	_ = reg.Register(v, true)

	svc, err := New(Config{
		Registry: reg,
		Rates: decider.Rates{
			VendorMarkup: 1000, // 10%
			RegionMarkup: 2000, // 20%
			Service:      500,  // 5%
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// retail · 全套 · 100 → 110 → 132 → 138.6
	retail := svc.Offers(context.Background(), Viewer{Tier: TierRetail}).
		Vendors[0].Categories[providers.AccountEnterprise].Offers[0].UnitPrice
	if retail != 138_600_000 {
		t.Errorf("retail = %d · want 138_600_000", retail)
	}
	// community · 免 region · 100 → 110 → 115.5
	community := svc.Offers(context.Background(), Viewer{Tier: TierCommunity}).
		Vendors[0].Categories[providers.AccountEnterprise].Offers[0].UnitPrice
	if community != 115_500_000 {
		t.Errorf("community = %d · want 115_500_000", community)
	}
	// wholesale · 免 vendor + region · 100 → 105
	wholesale := svc.Offers(context.Background(), Viewer{Tier: TierWholesale}).
		Vendors[0].Categories[providers.AccountEnterprise].Offers[0].UnitPrice
	if wholesale != 105_000_000 {
		t.Errorf("wholesale = %d · want 105_000_000", wholesale)
	}
}

// TestOffers_NoInternalTermsInJSON · 出去的 offers 不含内部术语
func TestOffers_NoInternalTermsInJSON(t *testing.T) {
	svc, _, _ := buildService(t)
	got := svc.Offers(context.Background(), Viewer{Tier: TierRetail})
	// 简单 sanity · 主要用现有 lint-terms 兜底
	for _, r := range got.Vendors {
		for _, cat := range r.Categories {
			for _, o := range cat.Offers {
				if strings.Contains(strings.ToLower(string(o.Source)), "housepool") {
					t.Errorf("offer.Source 含内部术语: %q", o.Source)
				}
			}
		}
	}
}

// stubPricing 实现 pricing.Quoter · 单测用 · 固定返一个 credits_per_unit
type stubPricing struct{ creditsPerUnit int64 }

func (p *stubPricing) QuoteFor(_ context.Context, _ string) (string, int64) {
	return "USD", p.creditsPerUnit
}

// stubRatesResolver · 满足 decider.RatesResolver · 单测用 · 固定返一份 Rates
type stubRatesResolver struct {
	rates decider.Rates
	// vendorID 观察最近一次 Resolve 的 vendor · 断言 vendorview 有正确传参
	lastVendorID string
}

func (r *stubRatesResolver) Resolve(_ context.Context, rc decider.RateContext) decider.Rates {
	r.lastVendorID = rc.VendorID
	return r.rates
}

// TestOffers_RatesResolver_TakesPrecedenceOverEnv · I-20
//
// 装了 resolver 时 · env 的 Rates 应被完全忽略 · 全走 resolver 返回值。
// 老 bug:vendorview 不接 resolver · 展示价永远走 env 兜底 · 跟 decider 拉号
// 用的 DB surcharge_rule 脱钩 · 导致"预估 vs 实扣"不一致。
func TestOffers_RatesResolver_TakesPrecedenceOverEnv(t *testing.T) {
	reg := providers.NewRegistry()
	v := &mockVendor{
		id: providers.Vendor91Kiro, name: "V1",
		unitPrice: 100_000_000, available: 10,
	}
	_ = reg.Register(v, true)

	resolver := &stubRatesResolver{rates: decider.Rates{Service: 500}} // 5%
	svc, err := New(Config{
		Registry:      reg,
		Rates:         decider.Rates{Service: 9999}, // env 兜底(99.99%)· 装了 resolver 应无视
		RatesResolver: resolver,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := svc.Offers(context.Background(), Viewer{Tier: TierRetail}).
		Vendors[0].Categories[providers.AccountEnterprise].Offers[0].UnitPrice
	// 100 × 1.05 = 105(resolver 的 5% 服务费 · env 99.99% 被忽略)
	if got != 105_000_000 {
		t.Errorf("resolver 生效 · unit_price = %d · want 105_000_000(5%%)·如果拿到 env 99.99%% 会是别的数", got)
	}
	if resolver.lastVendorID != string(providers.Vendor91Kiro) {
		t.Errorf("resolver 应收到 vendor_id · 实际 %q", resolver.lastVendorID)
	}
}

// TestOffers_EnvFallback_WhenNoResolver · resolver=nil 时退回 env rates(老行为兼容)
func TestOffers_EnvFallback_WhenNoResolver(t *testing.T) {
	reg := providers.NewRegistry()
	v := &mockVendor{
		id: providers.Vendor91Kiro, name: "V1",
		unitPrice: 100_000_000, available: 10,
	}
	_ = reg.Register(v, true)

	svc, err := New(Config{
		Registry: reg,
		Rates:    decider.Rates{Service: 500}, // 只 env · 无 resolver
	})
	if err != nil {
		t.Fatal(err)
	}
	got := svc.Offers(context.Background(), Viewer{Tier: TierRetail}).
		Vendors[0].Categories[providers.AccountEnterprise].Offers[0].UnitPrice
	if got != 105_000_000 {
		t.Errorf("env fallback · unit_price = %d · want 105_000_000", got)
	}
}
