package decider

import (
	"context"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// mockPricing · 测试用·可控每个 vendor 的换算规则
type mockPricing struct {
	quotes map[providers.VendorID]VendorQuote
}

func (m *mockPricing) QuoteFor(_ context.Context, id providers.VendorID) VendorQuote {
	if q, ok := m.quotes[id]; ok {
		return q
	}
	// 默认 fallback CNY 1:1
	return VendorQuote{QuoteCurrency: "CNY", CreditsPerUnit: 1_000_000}
}

// USD 家 · 5 USD × 7 CNY/USD 汇率 = 35 积分（35_000_000 microunit）
func TestConvertToMicroCredits_USDAppliesRate(t *testing.T) {
	o := &Orchestrator{pricing: &mockPricing{
		quotes: map[providers.VendorID]VendorQuote{
			providers.VendorKiroDrop: {
				QuoteCurrency: "USD", CreditsPerUnit: 7_000_000,
			},
		},
	}}
	got := o.convertToMicroCredits(context.Background(),
		providers.VendorKiroDrop,
		providers.Money{Amount: 5_000_000, Currency: providers.CurrencyUSD})
	if got != 35_000_000 {
		t.Errorf("5 USD × 7 汇率 → %d microunit · want 35_000_000", got)
	}
}

// CNY 家 · 单价直接是 microunit 积分 · pass-through 不换算
func TestConvertToMicroCredits_CNYPassthrough(t *testing.T) {
	o := &Orchestrator{pricing: &mockPricing{}}
	got := o.convertToMicroCredits(context.Background(),
		providers.Vendor91Kiro,
		providers.Money{Amount: 30_000_000, Currency: providers.CurrencyCNY})
	if got != 30_000_000 {
		t.Errorf("CNY 家应 pass-through · got=%d want=30_000_000", got)
	}
}

// credit 家（vendor 内部积分·跟我方 1:1） · 也 pass-through
func TestConvertToMicroCredits_CreditPassthrough(t *testing.T) {
	o := &Orchestrator{pricing: &mockPricing{}}
	got := o.convertToMicroCredits(context.Background(),
		providers.Vendor91Kiro,
		providers.Money{Amount: 15_000_000, Currency: providers.CurrencyCredit})
	if got != 15_000_000 {
		t.Errorf("credit 家应 pass-through · got=%d want=15_000_000", got)
	}
}

// pricing = nil · 全 vendor fallback（1a 兼容）
func TestConvertToMicroCredits_NilPricingFallback(t *testing.T) {
	o := &Orchestrator{pricing: nil}
	got := o.convertToMicroCredits(context.Background(),
		providers.VendorKiroDrop,
		providers.Money{Amount: 5_000_000, Currency: providers.CurrencyUSD})
	// 无 pricing · fallback CNY 1:1 · CreditsPerUnit=1_000_000 · 5 × 1 = 5
	if got != 5_000_000 {
		t.Errorf("nil pricing 走 fallback 1:1 · got=%d want=5_000_000", got)
	}
}
