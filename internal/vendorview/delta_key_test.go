package vendorview

// stock-delta 对比键的回归测试
//
// **锁的 bug（2026-08-13 发现）**：老代码用 `RegionStock.Region` 当对比键 ·
// 但部分 vendor 的库存响应**不返 region 原文**（恒空串）· 于是该家的 us / eu
// 两条在 `prevByRegion` map 里**塌成一条**（后写的覆盖前面的）· 结果：
//   ① 整个区的 restock delta 被漏掉 → 抢号链收不到唤醒
//   ② dispatch_key 撞车（两条都是 "delta--<timestamp>"）→ 只落一条
//
// 修法：键改用归一后的 `Zone`（每家都有值 · docs/19-fields.md §3）。

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 不返 region 的 vendor · 两个 zone 不能塌成一条
func TestDeltaKeyOf_NoRegionVendorKeepsZonesSeparate(t *testing.T) {
	// 模拟"只给 zone 不给 region"的 vendor（region 恒空）
	rows := []RegionStock{
		{Zone: "us", Region: "", Available: 0},
		{Zone: "eu", Region: "", Available: 5},
	}

	seen := make(map[string]int)
	for _, r := range rows {
		seen[deltaKeyOf(r)] = r.Available
	}

	if len(seen) != 2 {
		t.Fatalf("两个 zone 应有两个不同的键 · got %d 个：%v（老 bug 是塌成 1 个）",
			len(seen), seen)
	}
	if seen["us"] != 0 || seen["eu"] != 5 {
		t.Errorf("键值对错 · got %v · want map[us:0 eu:5]", seen)
	}
}

// 老样本（migration 029 之前 · JSON 里没 zone 字段）· 回落用 region
func TestDeltaKeyOf_FallsBackToRegionForOldSamples(t *testing.T) {
	old := RegionStock{Zone: "", Region: "us-east-1", Available: 3}
	if got := deltaKeyOf(old); got != "us-east-1" {
		t.Errorf("老样本该回落 region · got %q want %q", got, "us-east-1")
	}
}

// Zone 有值时优先 Zone（不用 region）
func TestDeltaKeyOf_PrefersZone(t *testing.T) {
	r := RegionStock{Zone: "us", Region: "us-east-1", Available: 1}
	if got := deltaKeyOf(r); got != "us" {
		t.Errorf("应优先 Zone · got %q want %q", got, "us")
	}
}

// 两个都空 · 键就是空串（认不出的 vendor · 至少不 panic）
func TestDeltaKeyOf_BothEmpty(t *testing.T) {
	if got := deltaKeyOf(RegionStock{}); got != "" {
		t.Errorf("两个都空该返空串 · got %q", got)
	}
}

// zoneKeyOf · 从 ZoneStock 取归一 zone · 主表侧表共用这一处规则
func TestZoneKeyOf(t *testing.T) {
	cases := []struct {
		name string
		in   providers.ZoneStock
		want providers.Zone
	}{
		{"两个都给 · 优先 Zone", providers.ZoneStock{Zone: "us", Region: "us-east-1"}, providers.ZoneUS},
		{"只给短名", providers.ZoneStock{Zone: "eu"}, providers.ZoneEU},
		{"只给 region 名 · 从它归一", providers.ZoneStock{Region: "us-east-1"}, providers.ZoneUS},
		{"只给 region 名 eu", providers.ZoneStock{Region: "eu-central-1"}, providers.ZoneEU},
		{"Zone 是完整 region 名 · 也能归一", providers.ZoneStock{Zone: "us-east-1"}, providers.ZoneUS},
		{"两个都空 · 不瞎猜", providers.ZoneStock{}, ""},
		{"都认不出 · 不瞎猜", providers.ZoneStock{Zone: "xx", Region: "yy"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := zoneKeyOf(c.in); got != c.want {
				t.Errorf("zoneKeyOf(%+v) = %q · want %q", c.in, got, c.want)
			}
		})
	}
}
