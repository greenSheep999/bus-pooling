package pricing

import (
	"context"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/decider"
)

func testResolverEnv(t *testing.T) *SurchargeStore {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/sr.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return NewSurchargeStore(d.DB)
}

// 表空 · Resolver 返 env fallback
func TestSurchargeResolver_EmptyTableFallsBackToEnv(t *testing.T) {
	store := testResolverEnv(t)
	envRates := decider.Rates{Service: 1000, VendorMarkup: 200}
	r := NewSurchargeResolver(SurchargeResolverConfig{
		Store: store, EnvFallback: envRates, TTL: 100 * time.Millisecond,
	})
	got := r.Resolve(context.Background(), decider.RateContext{VendorID: "v1"})
	if got.Service != 1000 {
		t.Errorf("空表应用 env fallback · Service=%d · want 1000", got.Service)
	}
}

// 有规则 · Resolver 走引擎
func TestSurchargeResolver_UsesRulesFromDB(t *testing.T) {
	store := testResolverEnv(t)
	if err := store.Upsert(context.Background(), Rule{
		ID: "r1", Kind: KindService, Name: "service_fee",
		RateBp: 500, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	r := NewSurchargeResolver(SurchargeResolverConfig{
		Store:       store,
		EnvFallback: decider.Rates{Service: 9999}, // 应被覆盖
		TTL:         100 * time.Millisecond,
	})
	got := r.Resolve(context.Background(), decider.RateContext{})
	if got.Service != 500 {
		t.Errorf("DB rule 应生效 · Service=%d · want 500（不是 env 9999）", got.Service)
	}
}

// vendor 匹配才生效
func TestSurchargeResolver_VendorSpecificRule(t *testing.T) {
	store := testResolverEnv(t)
	if err := store.Upsert(context.Background(), Rule{
		ID: "r1", Kind: KindVendor, Name: "v1_extra",
		RateBp: 300, Active: true,
		AppliesWhen: Predicate{"vendor_id": "v1"},
	}); err != nil {
		t.Fatal(err)
	}
	r := NewSurchargeResolver(SurchargeResolverConfig{
		Store: store, TTL: 100 * time.Millisecond,
	})
	// v1 命中
	if got := r.Resolve(context.Background(), decider.RateContext{VendorID: "v1"}); got.VendorMarkup != 300 {
		t.Errorf("v1 应命中 · VendorMarkup=%d", got.VendorMarkup)
	}
	// v2 不命中
	if got := r.Resolve(context.Background(), decider.RateContext{VendorID: "v2"}); got.VendorMarkup != 0 {
		t.Errorf("v2 不该命中 · VendorMarkup=%d", got.VendorMarkup)
	}
}

// 缓存 TTL · Invalidate 后立刻回表
func TestSurchargeResolver_InvalidateRefetchesRules(t *testing.T) {
	store := testResolverEnv(t)
	if err := store.Upsert(context.Background(), Rule{
		ID: "r1", Kind: KindService, Name: "svc",
		RateBp: 500, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	r := NewSurchargeResolver(SurchargeResolverConfig{
		Store: store, TTL: 1 * time.Hour, // 长 TTL·靠 Invalidate 才回表
	})
	// 第一次·500
	if got := r.Resolve(context.Background(), decider.RateContext{}); got.Service != 500 {
		t.Fatalf("first Service=%d · want 500", got.Service)
	}
	// 改规则
	if err := store.Upsert(context.Background(), Rule{
		ID: "r1", Kind: KindService, Name: "svc",
		RateBp: 900, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	// TTL 没过·还是 500
	if got := r.Resolve(context.Background(), decider.RateContext{}); got.Service != 500 {
		t.Errorf("TTL 内应用缓存 · Service=%d · want 500", got.Service)
	}
	// Invalidate 后·立刻拿新值
	r.Invalidate()
	if got := r.Resolve(context.Background(), decider.RateContext{}); got.Service != 900 {
		t.Errorf("Invalidate 后应回表 · Service=%d · want 900", got.Service)
	}
}
