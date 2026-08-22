package vendorview

import (
	"context"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// I-25 · offers 端点 price_bands 从 vendor_price_tier(qty_band)读入
//
// 老 bug:TierStore 只写不读 · offers 只返 flat UnitPrice · 前端切数量重算不生效。
// 修复:offersFromSnapshot 从 TierStore.QtyBandsOf 读该 vendor 分档·填 PriceBands。

func TestOffers_LoadsPriceBandsFromTierStore(t *testing.T) {
	tdb := db.NewTestDB(t)
	defer tdb.Close()
	tierStore := NewTierStore(tdb.DB)

	// seed 3 档:1-10 @ 100 积分 · 11-50 @ 80 积分 · 51+ @ 60 积分
	err := tierStore.ReplaceQtyBands(context.Background(),
		string(providers.Vendor91Kiro),
		[]providers.QtyPriceBand{
			{Lower: 1, Upper: 10, UnitPriceCredits: 100_000_000},
			{Lower: 11, Upper: 50, UnitPriceCredits: 80_000_000},
			{Lower: 51, Upper: 0, UnitPriceCredits: 60_000_000}, // upper=0 = 及以上
		})
	if err != nil {
		t.Fatal(err)
	}

	reg := providers.NewRegistry()
	v := &mockVendor{
		id: providers.Vendor91Kiro, name: "V1",
		unitPrice: 100_000_000, available: 100,
	}
	_ = reg.Register(v, true)

	svc, err := New(Config{
		Registry:  reg,
		Rates:     decider.Rates{Service: 500}, // 5% 服务费 · 分档单价也要过
		TierStore: tierStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := svc.Offers(context.Background(), Viewer{Tier: TierRetail})
	if len(got.Vendors) == 0 {
		t.Fatal("no vendors")
	}
	offers := got.Vendors[0].Categories[providers.AccountEnterprise].Offers
	if len(offers) == 0 {
		t.Fatal("no enterprise offers")
	}
	// 每个 offer 应带 price_bands
	if len(offers[0].PriceBands) != 3 {
		t.Fatalf("PriceBands len = %d · want 3 · offer=%+v", len(offers[0].PriceBands), offers[0])
	}
	// 各档单价过计费栈:100 → 105 · 80 → 84 · 60 → 63
	tests := []struct {
		i    int
		want int64
	}{
		{0, 105_000_000}, // 100 × 1.05
		{1, 84_000_000},  // 80 × 1.05
		{2, 63_000_000},  // 60 × 1.05
	}
	for _, tt := range tests {
		if offers[0].PriceBands[tt.i].UnitPriceCredits != tt.want {
			t.Errorf("band[%d] = %d · want %d · %+v",
				tt.i, offers[0].PriceBands[tt.i].UnitPriceCredits, tt.want, offers[0].PriceBands[tt.i])
		}
	}
	// 区间边界也应保留
	if offers[0].PriceBands[0].Lower != 1 || offers[0].PriceBands[0].Upper != 10 {
		t.Errorf("band[0] 区间丢: %+v", offers[0].PriceBands[0])
	}
	if offers[0].PriceBands[2].Upper != 0 {
		t.Errorf("band[2] upper 应 0(及以上): %+v", offers[0].PriceBands[2])
	}
}

// TestOffers_NoTierStore_NoPriceBands · TierStore=nil 时 offers 不带 price_bands(老行为)
func TestOffers_NoTierStore_NoPriceBands(t *testing.T) {
	reg := providers.NewRegistry()
	v := &mockVendor{id: providers.Vendor91Kiro, unitPrice: 100_000_000, available: 10}
	_ = reg.Register(v, true)
	svc, err := New(Config{Registry: reg}) // 无 TierStore
	if err != nil {
		t.Fatal(err)
	}
	offers := svc.Offers(context.Background(), Viewer{Tier: TierRetail}).
		Vendors[0].Categories[providers.AccountEnterprise].Offers
	if len(offers) == 0 {
		t.Fatal("no offers")
	}
	if len(offers[0].PriceBands) != 0 {
		t.Errorf("无 TierStore 时不该有 price_bands · 实际 %+v", offers[0].PriceBands)
	}
}
