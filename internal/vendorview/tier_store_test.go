package vendorview

import (
	"context"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

func tierDB(t *testing.T) *TierStore {
	t.Helper()
	d := db.NewTestDB(t)
	return NewTierStore(d.DB)
}

// 真实 vendor bands 形状（2026-08-14 实测 · 4 档 · upper=0 表示及以上）
func TestTierStore_ReplaceAndRead(t *testing.T) {
	s := tierDB(t)
	ctx := context.Background()
	bands := []providers.QtyPriceBand{
		{Lower: 1, Upper: 5, UnitPriceCredits: 100_000_000},
		{Lower: 6, Upper: 10, UnitPriceCredits: 100_000_000},
		{Lower: 11, Upper: 20, UnitPriceCredits: 100_000_000},
		{Lower: 21, Upper: 0, UnitPriceCredits: 100_000_000}, // 及以上
	}
	if err := s.ReplaceQtyBands(ctx, "kirooo", bands); err != nil {
		t.Fatal(err)
	}
	got, err := s.QtyBandsOf(ctx, "kirooo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("应 4 档 · 得 %d", len(got))
	}
	if got[0].Lower != 1 || got[0].Upper != 5 || got[0].UnitPriceCredits != 100_000_000 {
		t.Errorf("第 1 档错 · %+v", got[0])
	}
	// upper=0（及以上）· 存 NULL · 读回 0
	if got[3].Lower != 21 || got[3].Upper != 0 {
		t.Errorf("最高档应 21+ · 得 %+v", got[3])
	}
}

// 整表覆盖：4 档变 2 档 · 旧的清掉不残留
func TestTierStore_ReplaceShrinks(t *testing.T) {
	s := tierDB(t)
	ctx := context.Background()
	_ = s.ReplaceQtyBands(ctx, "kirooo", []providers.QtyPriceBand{
		{Lower: 1, Upper: 5, UnitPriceCredits: 100_000_000},
		{Lower: 6, Upper: 10, UnitPriceCredits: 90_000_000},
		{Lower: 11, Upper: 0, UnitPriceCredits: 80_000_000},
	})
	// 新一轮只剩 2 档
	_ = s.ReplaceQtyBands(ctx, "kirooo", []providers.QtyPriceBand{
		{Lower: 1, Upper: 10, UnitPriceCredits: 70_000_000},
		{Lower: 11, Upper: 0, UnitPriceCredits: 60_000_000},
	})
	got, _ := s.QtyBandsOf(ctx, "kirooo")
	if len(got) != 2 {
		t.Fatalf("覆盖后应 2 档 · 得 %d（旧档残留？）", len(got))
	}
	if got[0].UnitPriceCredits != 70_000_000 {
		t.Errorf("应是新价 · 得 %d", got[0].UnitPriceCredits)
	}
}

// 空 bands（当前 flat/无分档）→ 清空
func TestTierStore_EmptyClears(t *testing.T) {
	s := tierDB(t)
	ctx := context.Background()
	_ = s.ReplaceQtyBands(ctx, "kirooo", []providers.QtyPriceBand{
		{Lower: 1, Upper: 0, UnitPriceCredits: 100_000_000},
	})
	_ = s.ReplaceQtyBands(ctx, "kirooo", nil) // 无分档了
	got, _ := s.QtyBandsOf(ctx, "kirooo")
	if len(got) != 0 {
		t.Fatalf("空应清掉 · 得 %d", len(got))
	}
}

// qty_band 不碰 time_decay 行（两种阶梯共表 · 各管各的）
func TestTierStore_DoesNotTouchTimeDecay(t *testing.T) {
	s := tierDB(t)
	ctx := context.Background()
	// 手插一条 time_decay 行（模拟时间降价 vendor）
	_, err := s.db.Exec(`INSERT INTO vendor_price_tier
		(vendor_id, region, probed_at, tier_kind, tier_index, effective_at, unit_price_credits, created_at)
		VALUES ('kirodrop','eu','2026-08-14T00:00:00Z','time_decay',0,'2026-08-14T00:00:00Z',49980000,'2026-08-14T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	// qty_band 覆盖不应动同 vendor 的 time_decay 行
	_ = s.ReplaceQtyBands(ctx, "kirodrop", []providers.QtyPriceBand{{Lower: 1, Upper: 0, UnitPriceCredits: 100_000_000}})
	var td int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM vendor_price_tier WHERE tier_kind='time_decay'`).Scan(&td)
	if td != 1 {
		t.Errorf("time_decay 行不该被 qty_band 覆盖动到 · 得 %d", td)
	}
}

// ── 时间降价 time_decay ──

// 真实降价表形状（2026-08-14 实测 reservation · us/eu 各 base+1 档 · 每 30min 降）
func TestTierStore_ReplaceTimeDecayAndRead(t *testing.T) {
	s := tierDB(t)
	ctx := context.Background()
	start := time.Date(2026, 8, 12, 7, 20, 27, 0, time.UTC)
	tiers := []providers.TieredPricing{
		{Region: "us", Enabled: true, Active: false, IntervalMin: 30, MaxReductions: 1, StartAt: start, Schedule: []providers.TierSchedule{
			{Index: 0, EffectiveAt: start, UnitPriceCredits: 49_980_000, UnitPriceUSDRaw: 7_350_000},
			{Index: 1, EffectiveAt: start.Add(30 * time.Minute), UnitPriceCredits: 39_984_000, UnitPriceUSDRaw: 5_880_000},
		}},
		{Region: "eu", Enabled: true, IntervalMin: 30, MaxReductions: 1, StartAt: start, Schedule: []providers.TierSchedule{
			{Index: 0, EffectiveAt: start, UnitPriceCredits: 34_952_000, UnitPriceUSDRaw: 5_140_000},
			{Index: 1, EffectiveAt: start.Add(30 * time.Minute), UnitPriceCredits: 24_956_000, UnitPriceUSDRaw: 3_670_000},
		}},
	}
	if err := s.ReplaceTimeDecay(ctx, "kirodrop", tiers); err != nil {
		t.Fatal(err)
	}
	got, err := s.TimeDecayOf(ctx, "kirodrop")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("应 2 区 · 得 %d", len(got))
	}
	// 排序 region ASC → eu 在前
	var us providers.TieredPricing
	for _, tp := range got {
		if tp.Region == "us" {
			us = tp
		}
	}
	if !us.Enabled || us.IntervalMin != 30 || len(us.Schedule) != 2 {
		t.Fatalf("us 头/档数错 · %+v", us)
	}
	if us.Schedule[0].UnitPriceCredits != 49_980_000 || us.Schedule[1].UnitPriceCredits != 39_984_000 {
		t.Errorf("us 价错 · %+v", us.Schedule)
	}
	if us.Schedule[0].UnitPriceUSDRaw != 7_350_000 {
		t.Errorf("us USD raw 应存 · 得 %d", us.Schedule[0].UnitPriceUSDRaw)
	}
}

// time_decay 覆盖不碰 qty_band（反向验证 · 两种阶梯各管各的）
func TestTierStore_TimeDecayDoesNotTouchQtyBand(t *testing.T) {
	s := tierDB(t)
	ctx := context.Background()
	_ = s.ReplaceQtyBands(ctx, "kirodrop", []providers.QtyPriceBand{{Lower: 1, Upper: 0, UnitPriceCredits: 100_000_000}})
	_ = s.ReplaceTimeDecay(ctx, "kirodrop", []providers.TieredPricing{
		{Region: "us", Enabled: true, Schedule: []providers.TierSchedule{{Index: 0, EffectiveAt: time.Now().UTC(), UnitPriceCredits: 49_980_000}}},
	})
	var qb int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM vendor_price_tier WHERE tier_kind='qty_band'`).Scan(&qb)
	if qb != 1 {
		t.Errorf("qty_band 不该被 time_decay 覆盖动到 · 得 %d", qb)
	}
}
