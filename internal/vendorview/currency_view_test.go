package vendorview

// 展示价的币种换算 · 锁两个真出过的 bug：
//   ① VendorStock / AutoPick 拿 UnitPrice.Amount 当积分用 —— USD 家展示价只有实际的 1/6.8
//   ② AutoPick 跨 vendor 比价用 raw amount —— USD 家的 "7.35" 跟 credit 家的 "30" 比 · 永远赢

import (
	"context"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// mockQuotes · 按 vendor 给换算规则
type mockQuotes struct{ perUnit map[string]int64 }

func (m *mockQuotes) QuoteFor(_ context.Context, vendorID string) (string, int64) {
	if c, ok := m.perUnit[vendorID]; ok {
		return "USD", c
	}
	return "credit", 1_000_000
}

// USD 家的快照单价要先换成积分再进计费栈
func TestBaseCredits_ConvertsUSD(t *testing.T) {
	s := &Service{pricing: &mockQuotes{perUnit: map[string]int64{
		string(providers.VendorKiroDrop): 6_800_000, // $1 = 6.8 积分
	}}}
	// 7.35 USD → 49.98 积分
	got := s.baseCredits(context.Background(), providers.VendorKiroDrop,
		providers.Money{Amount: 7_350_000, Currency: providers.CurrencyUSD})
	if got != 49_980_000 {
		t.Errorf("7.35 USD → %d · want 49_980_000", got)
	}
}

// credit / CNY 家退化成恒等
func TestBaseCredits_CreditPassthrough(t *testing.T) {
	s := &Service{pricing: &mockQuotes{}}
	got := s.baseCredits(context.Background(), providers.Vendor91Kiro,
		providers.Money{Amount: 30_000_000, Currency: providers.CurrencyCredit})
	if got != 30_000_000 {
		t.Errorf("credit 家应 pass-through · got=%d", got)
	}
}

// pricing 未装配 · 走 1:1（不炸 · 老部署兼容）
func TestBaseCredits_NilPricing(t *testing.T) {
	s := &Service{pricing: nil}
	got := s.baseCredits(context.Background(), providers.VendorKiroDrop,
		providers.Money{Amount: 7_350_000, Currency: providers.CurrencyUSD})
	if got != 7_350_000 {
		t.Errorf("nil pricing 走 1:1 · got=%d", got)
	}
}

// 0 单价（缺货）不换算
func TestBaseCredits_Zero(t *testing.T) {
	s := &Service{pricing: &mockQuotes{}}
	if got := s.baseCredits(context.Background(), providers.Vendor91Kiro,
		providers.Money{}); got != 0 {
		t.Errorf("0 应返 0 · got=%d", got)
	}
}

// AutoPick 跨 vendor 比价要用换算后的积分。
//
// 场景：USD 家报 7.35 USD（= 49.98 积分 · **贵**）· credit 家报 30 积分（**便宜**）。
// 拿 raw amount 比会认为 7.35 < 30 → 推荐 USD 家（错）。换算后 49.98 > 30 → 推荐 credit 家（对）。
func TestAutoPick_ComparesInCredits(t *testing.T) {
	reg := providers.NewRegistry()
	usdVendor := &mockVendor{
		id: providers.VendorKiroDrop, name: "Kiro Drop",
		available: 20,
		zones: []providers.ZoneStock{{
			Zone: providers.ZoneUS, Region: "us-east-1", Available: 20,
			UnitPrice: providers.Money{Amount: 7_350_000, Currency: providers.CurrencyUSD},
		}},
	}
	creditVendor := &mockVendor{
		id: providers.Vendor91Kiro, name: "Kiro Market",
		unitPrice: 30_000_000, available: 20, // 30 积分
	}
	if err := reg.Register(usdVendor, true); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(creditVendor, true); err != nil {
		t.Fatal(err)
	}

	svc, err := New(Config{
		Registry: reg,
		Pricing: &mockQuotes{perUnit: map[string]int64{
			string(providers.VendorKiroDrop): 6_800_000,
		}},
		StockTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	pick := svc.AutoPick(context.Background(), "auto", Viewer{Tier: TierWholesale})
	if pick == nil {
		t.Fatal("应有推荐")
	}
	if pick.VendorLabel != "Kiro Market" {
		t.Errorf("应推荐便宜的那家（30 积分）· got=%q · 说明比价没换算币种",
			pick.VendorLabel)
	}
}

// 非 wholesale 档 · AutoPick 不许返真 vendor_id（原来直接返 string(VendorID)）
func TestAutoPick_HidesRealVendorIDForNonWholesale(t *testing.T) {
	svc, _, _ := buildService(t)
	for _, tier := range []string{TierRetail, TierCommunity} {
		pick := svc.AutoPick(context.Background(), "auto", Viewer{Tier: tier})
		if pick == nil {
			t.Fatalf("%s · 应有推荐", tier)
		}
		if pick.VendorID == string(providers.Vendor91Kiro) ||
			pick.VendorID == string(providers.VendorKiroCEO) {
			t.Errorf("%s 档不该拿到真 vendor_id · got=%q", tier, pick.VendorID)
		}
	}
}

// **换算后再进计费栈** —— 顺序错了展示价会差几倍
func TestFinalUnitPrice_AfterConversion(t *testing.T) {
	s := &Service{
		rates: decider.Rates{Service: 500}, // 只服务费 5%
		pricing: &mockQuotes{perUnit: map[string]int64{
			string(providers.VendorKiroDrop): 6_800_000,
		}},
	}
	base := s.baseCredits(context.Background(), providers.VendorKiroDrop,
		providers.Money{Amount: 7_350_000, Currency: providers.CurrencyUSD})
	got := s.finalUnitPrice(base, Viewer{Tier: TierWholesale})
	// 49.98 × 1.05 = 52.479 积分
	if got != 52_479_000 {
		t.Errorf("展示价 = %d · want 52_479_000（49.98 积分 × 1.05）", got)
	}
	// 不换算直接进栈的话是 7.35 × 1.05 = 7.7175 —— 差 6.8 倍
	if wrong := s.finalUnitPrice(7_350_000, Viewer{Tier: TierWholesale}); wrong == got {
		t.Error("换算前后应该不同 —— 否则说明换算没生效")
	}
}
