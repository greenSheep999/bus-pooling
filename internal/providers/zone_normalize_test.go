package providers_test

// zone 归一回归测试 · 锁「所有 vendor 的 ZoneStock.Zone 必须是归一后的值」
//
// **为什么单独一份**：每家 vendor 上游给的地区字段形态都不一样（docs/11-fields.md §3）·
// 有的给短名（"us"）· 有的只给完整 region 名（"us-east-1"）· 有的只给中文 label（"美国区"）·
// 有的两个都不给（平铺 _us/_eu 后缀）· 有的完全无区概念。
//
// zone 列是我方唯一的地区标准（providers.Zone · us/eu/general）· 落错会让
// vendor_probe_zone 跨 vendor 对不上 · PricedFor 按 zone 查匹配不到。
// 两家 vendor 曾因此落错（一家落 "us-east-1" · 一家落空）· 见各家档案 §6 缺口。

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ZoneOf 的出口只能是这三个值（加空串表示识别不出）
func TestZoneOf_OnlyEmitsStandardValues(t *testing.T) {
	// 各 vendor 实际会传进来的形态（抓自 docs/vendors/*.md 实测响应）
	inputs := []string{
		"us", "eu", // 短名
		"us-east-1", "eu-central-1", // 完整 region 名
		"美国区", "欧洲区", // 中文 label（部分 vendor）
		"美区", "欧区", // 中文 label（聚合源）
		"us-east-1-dryrun",              // 我方 DryRun vendor
		"", "unknown", "ap-southeast-1", // 边界
	}
	valid := map[providers.Zone]bool{
		providers.ZoneUS:      true,
		providers.ZoneEU:      true,
		providers.ZoneGeneral: true, // 不分区 vendor 的唯一一区
		"":                    true, // 识别不出 · 交给调用方兜底
	}
	for _, in := range inputs {
		got := providers.ZoneOf(in)
		if !valid[got] {
			t.Errorf("ZoneOf(%q) = %q · 不是标准值（us/eu/空）", in, got)
		}
	}
}

// 各家实际形态都能归一到 us/eu
func TestZoneOf_AllVendorShapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want providers.Zone
	}{
		{"短名 us", "us", providers.ZoneUS},
		{"短名 eu", "eu", providers.ZoneEU},
		{"region 名 us", "us-east-1", providers.ZoneUS},
		{"region 名 eu", "eu-central-1", providers.ZoneEU},
		{"中文 美国区", "美国区", providers.ZoneUS},
		{"中文 欧洲区", "欧洲区", providers.ZoneEU},
		{"中文 美区", "美区", providers.ZoneUS},
		{"中文 欧区", "欧区", providers.ZoneEU},
		{"dryrun 后缀", "us-east-1-dryrun", providers.ZoneUS},
		{"大写", "US-EAST-1", providers.ZoneUS},
		{"带空格", "  eu  ", providers.ZoneEU},
		// general · 不分区 vendor 的唯一一区 · **必须原样保留**
		// 归一成空串会让那家的侧表行 zone 列空 → PricedFor 按 zone 查匹配不到 →
		// 等于这家 vendor 在定价链上"不存在"（2026-08-13 生产实测：该家侧表 0 行）
		{"general 原样保留", "general", providers.ZoneGeneral},
		{"general 大写", "GENERAL", providers.ZoneGeneral},
		{"general 带空格", "  general  ", providers.ZoneGeneral},
		{"空串", "", ""},
		{"认不出", "ap-southeast-1", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := providers.ZoneOf(c.in); got != c.want {
				t.Errorf("ZoneOf(%q) = %q · want %q", c.in, got, c.want)
			}
		})
	}
}

// **归一必须幂等** —— 已经是标准值的再过一次不变（adapter 可能重复调）
func TestZoneOf_Idempotent(t *testing.T) {
	for _, z := range []providers.Zone{providers.ZoneUS, providers.ZoneEU, providers.ZoneGeneral} {
		once := providers.ZoneOf(string(z))
		twice := providers.ZoneOf(string(once))
		if once != twice {
			t.Errorf("ZoneOf 不幂等 · %q → %q → %q", z, once, twice)
		}
		if once != z {
			t.Errorf("ZoneOf(%q) = %q · 标准值过一遍应不变", z, once)
		}
	}
}
