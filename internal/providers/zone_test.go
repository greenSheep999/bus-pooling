package providers

import "testing"

// ZoneOf · docs/22-buy-race 缺口 5 · 3 套命名归一测试
func TestZoneOf(t *testing.T) {
	cases := []struct {
		in   string
		want Zone
	}{
		// 已经归一好的（decider / webhook 里传的）
		{"us", ZoneUS},
		{"eu", ZoneEU},
		{"US", ZoneUS},
		{"EU", ZoneEU},
		// vendor region 名（探针 / webhook 一些家）
		{"us-east-1", ZoneUS},
		{"eu-central-1", ZoneEU},
		{"us-east-1-dryrun", ZoneUS},
		// vendor 侧中文 label（部分 vendor stock 端点返 label:"美国区"）
		{"美国区", ZoneUS},
		{"欧洲区", ZoneEU},
		// 未知 · 返空 · SQL 分支不带 region 过滤
		{"", ""},
		{"asia-east-1", ""}, // 未来若有亚太区 · 单独加规则
		{"random", ""},
		// 大小写混合
		{"US-East-1", ZoneUS},
		// 带前后空白
		{"  us  ", ZoneUS},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := ZoneOf(tc.in)
			if got != tc.want {
				t.Errorf("ZoneOf(%q) = %q · 期 %q", tc.in, got, tc.want)
			}
		})
	}
}
