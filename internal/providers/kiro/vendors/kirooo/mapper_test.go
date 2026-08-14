package kirooo

// **回归哨兵 · 2026-08-13**
//
// 本 vendor 的 /my/stock/regions 只给 `region:"us-east-1"` · **不给 zone 短名** ·
// 老 mapper 写的是 `Zone: providers.Zone(r.Region)` 直接强转 → zone 列落成
// "us-east-1" · 跟其他 vendor 的 "us" 对不上 → PricedFor 按 zone 查匹配不到
// （docs/11-fields.md §3 组 3 · 区域标识）。
//
// 修复：过 providers.ZoneOf 归一。

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

func TestToStockSnapshotFromRegions_ZoneNormalized(t *testing.T) {
	rr := &regionsResp{
		FleetActive: true,
		Regions: []regionEntry{
			{Region: "us-east-1", Label: "美国区", UnitPrice: 100, Stock: 5},
			{Region: "eu-central-1", Label: "欧洲区", UnitPrice: 70, Stock: 3},
		},
	}
	snap := toStockSnapshotFromRegions(rr, nil)

	if len(snap.Zones) != 2 {
		t.Fatalf("应有 2 个 zone · got %d", len(snap.Zones))
	}

	// zone 必须归一成 us / eu · 不是 us-east-1 / eu-central-1
	wantZones := []providers.Zone{providers.ZoneUS, providers.ZoneEU}
	wantRegions := []string{"us-east-1", "eu-central-1"}
	for i, z := range snap.Zones {
		if z.Zone != wantZones[i] {
			t.Errorf("Zones[%d].Zone = %q · want %q（老 bug 是落 %q）",
				i, z.Zone, wantZones[i], wantRegions[i])
		}
		// region 原文该保留
		if z.Region != wantRegions[i] {
			t.Errorf("Zones[%d].Region = %q · want %q", i, z.Region, wantRegions[i])
		}
	}

	// 单价 / 库存不受归一影响
	if snap.Zones[0].UnitPrice.Amount != 100_000_000 {
		t.Errorf("us 单价 = %d · want 100_000_000", snap.Zones[0].UnitPrice.Amount)
	}
	if snap.Zones[1].UnitPrice.Amount != 70_000_000 {
		t.Errorf("eu 单价 = %d · want 70_000_000", snap.Zones[1].UnitPrice.Amount)
	}
	if snap.Available != 8 {
		t.Errorf("总库存 = %d · want 8", snap.Available)
	}
}

// 老形状（/my/stock 带 zones[]）· zone 空时从 region 归一
func TestToStockSnapshot_ZoneFallbackToRegion(t *testing.T) {
	t.Run("zone 有值 · 直接归一", func(t *testing.T) {
		sr := &stockResp{Zones: []zoneItem{
			{Zone: "us", Region: "us-east-1", Available: 2, UnitPrice: 100},
		}}
		snap := toStockSnapshot(sr, nil)
		if len(snap.Zones) != 1 || snap.Zones[0].Zone != providers.ZoneUS {
			t.Errorf("got %+v · want Zone=us", snap.Zones)
		}
	})

	t.Run("zone 空 · 从 region 兜", func(t *testing.T) {
		sr := &stockResp{Zones: []zoneItem{
			{Zone: "", Region: "eu-central-1", Available: 1, UnitPrice: 70},
		}}
		snap := toStockSnapshot(sr, nil)
		if len(snap.Zones) != 1 || snap.Zones[0].Zone != providers.ZoneEU {
			t.Errorf("got %+v · want Zone=eu（从 region 兜）", snap.Zones)
		}
	})

	t.Run("两个都空 · zone 留空不猜", func(t *testing.T) {
		sr := &stockResp{Zones: []zoneItem{
			{Zone: "", Region: "", Available: 1, UnitPrice: 70},
		}}
		snap := toStockSnapshot(sr, nil)
		if len(snap.Zones) != 1 {
			t.Fatalf("got %d zones", len(snap.Zones))
		}
		if snap.Zones[0].Zone != "" {
			t.Errorf("Zone = %q · 认不出时该留空不瞎猜", snap.Zones[0].Zone)
		}
	})
}
