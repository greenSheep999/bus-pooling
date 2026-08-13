package kiroappcc

// **回归哨兵 · 2026-08-13 · 生产实测发现**
//
// 本 vendor 的 stock 响应只有 `{availableKeys, keyPrice}` · 无区域概念 ·
// 老 mapper 因此把 `Zones` 留 nil。后果是这家在整条定价链上"不存在"：
//
//   下游全部按 `len(snap.Zones) > 0` 才干活 —— Zones 空意味着
//     · vendor_probe_zone 侧表 **0 行**（生产实测 4209 条探针 · 侧表零行）
//     · our_unit_credits 不落（明明 keyPrice=100）
//     · stock_by_region 空 → stock-delta 推不出 restock → **抢号链收不到这家的补货**
//
// 修法：无区 vendor 用 ZoneGeneral 当"唯一一区"占位。
// 配套：providers.ZoneOf("general") 必须原样返 ZoneGeneral（否则又被归一成空）。

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

func TestToStockSnapshot_EmitsGeneralZone(t *testing.T) {
	t.Run("有货有价 · 出一条 general zone", func(t *testing.T) {
		snap := toStockSnapshot(&stockResp{AvailableKeys: 36, KeyPrice: 100}, nil)

		if snap.Available != 36 {
			t.Errorf("Available = %d · want 36", snap.Available)
		}
		if len(snap.Zones) != 1 {
			t.Fatalf("应出 1 条 zone · got %d（老 bug 是 nil）", len(snap.Zones))
		}
		z := snap.Zones[0]
		if z.Zone != providers.ZoneGeneral {
			t.Errorf("Zone = %q · want %q", z.Zone, providers.ZoneGeneral)
		}
		if z.Region != "" {
			t.Errorf("Region = %q · 本 vendor 无 region 概念 · 空是事实", z.Region)
		}
		if z.Available != 36 {
			t.Errorf("zone.Available = %d · want 36", z.Available)
		}
		// keyPrice=100 → 100 积分 microunit
		if z.UnitPrice.Amount != 100_000_000 {
			t.Errorf("UnitPrice.Amount = %d · want 100_000_000（老 bug 是单价整个丢掉）",
				z.UnitPrice.Amount)
		}
		if z.UnitPrice.Currency != providers.CurrencyCredit {
			t.Errorf("Currency = %q · want %q", z.UnitPrice.Currency, providers.CurrencyCredit)
		}
	})

	t.Run("缺货但有价 · 仍出 zone（价格要落库）", func(t *testing.T) {
		snap := toStockSnapshot(&stockResp{AvailableKeys: 0, KeyPrice: 100}, nil)
		if len(snap.Zones) != 1 {
			t.Fatalf("有价就该出 zone · got %d", len(snap.Zones))
		}
		if snap.Zones[0].UnitPrice.Amount != 100_000_000 {
			t.Errorf("缺货时单价仍该落 · got %d", snap.Zones[0].UnitPrice.Amount)
		}
	})

	t.Run("有货无价 · 仍出 zone（库存要落库）", func(t *testing.T) {
		snap := toStockSnapshot(&stockResp{AvailableKeys: 5, KeyPrice: 0}, nil)
		if len(snap.Zones) != 1 {
			t.Fatalf("有货就该出 zone · got %d", len(snap.Zones))
		}
		if snap.Zones[0].Available != 5 {
			t.Errorf("Available = %d · want 5", snap.Zones[0].Available)
		}
	})

	t.Run("全零 · 不造零价 zone", func(t *testing.T) {
		snap := toStockSnapshot(&stockResp{AvailableKeys: 0, KeyPrice: 0}, nil)
		if len(snap.Zones) != 0 {
			t.Errorf("全零时不该造 zone · got %+v", snap.Zones)
		}
	})
}

// general 必须活过归一（配套 providers.ZoneOf 的修复）
func TestGeneralZoneSurvivesNormalize(t *testing.T) {
	snap := toStockSnapshot(&stockResp{AvailableKeys: 36, KeyPrice: 100}, nil)
	if len(snap.Zones) != 1 {
		t.Fatal("前置条件不满足")
	}
	got := providers.ZoneOf(string(snap.Zones[0].Zone))
	if got != providers.ZoneGeneral {
		t.Errorf("ZoneOf(%q) = %q · 归一后必须还是 general · 否则侧表 zone 列会空",
			snap.Zones[0].Zone, got)
	}
}
