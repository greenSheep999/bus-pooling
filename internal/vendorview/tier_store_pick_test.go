package vendorview

import (
	"context"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// I-39 · TierStore.UnitPriceFor · 按 count 命中数量分档取单价
//
// decider 和 offers 都调这个 · 保证展示 vs 扣费同源。

func setupTierStore(t *testing.T) *TierStore {
	t.Helper()
	tdb := db.NewTestDB(t)
	t.Cleanup(func() { _ = tdb.Close() })
	return NewTierStore(tdb.DB)
}

func TestUnitPriceFor_HitsMiddleTier(t *testing.T) {
	s := setupTierStore(t)
	ctx := context.Background()
	// 3 档:1-9 每个 100 积分 · 10-49 每个 80 · 50+ 每个 60
	err := s.ReplaceQtyBands(ctx, "vtest",
		[]providers.QtyPriceBand{
			{Lower: 1, Upper: 9, UnitPriceCredits: 100_000_000},
			{Lower: 10, Upper: 49, UnitPriceCredits: 80_000_000},
			{Lower: 50, Upper: 0, UnitPriceCredits: 60_000_000}, // upper=0 = 及以上
		})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		count int
		want  int64
		hit   bool
	}{
		{count: 1, want: 100_000_000, hit: true},   // 落最低档
		{count: 5, want: 100_000_000, hit: true},   // 落最低档中间
		{count: 9, want: 100_000_000, hit: true},   // 最低档 upper
		{count: 10, want: 80_000_000, hit: true},   // 落中间档 lower
		{count: 30, want: 80_000_000, hit: true},   // 落中间档中间
		{count: 49, want: 80_000_000, hit: true},   // 中间档 upper
		{count: 50, want: 60_000_000, hit: true},   // 落最高档 lower
		{count: 200, want: 60_000_000, hit: true},  // 最高档 upper=0 · 及以上
	}
	for _, tt := range tests {
		got, hit := s.UnitPriceFor(ctx, "vtest", tt.count)
		if got != tt.want || hit != tt.hit {
			t.Errorf("count=%d · price=%d hit=%v · want price=%d hit=%v",
				tt.count, got, hit, tt.want, tt.hit)
		}
	}
}

func TestUnitPriceFor_NoBands_MissesGracefully(t *testing.T) {
	s := setupTierStore(t)
	// vendor 无分档配置 · 表空
	price, hit := s.UnitPriceFor(context.Background(), "unknown_vendor", 5)
	if hit || price != 0 {
		t.Errorf("空 vendor · 应 miss · 实际 price=%d hit=%v", price, hit)
	}
}

func TestUnitPriceFor_ZeroCount_Rejected(t *testing.T) {
	s := setupTierStore(t)
	price, hit := s.UnitPriceFor(context.Background(), "v", 0)
	if hit || price != 0 {
		t.Errorf("count=0 · 应 miss · 实际 price=%d hit=%v", price, hit)
	}
}

func TestUnitPriceFor_NilStore_Safe(t *testing.T) {
	var s *TierStore
	price, hit := s.UnitPriceFor(context.Background(), "v", 5)
	if hit || price != 0 {
		t.Errorf("nil store · 应 miss · 实际 price=%d hit=%v", price, hit)
	}
}
