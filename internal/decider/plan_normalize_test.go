package decider

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 号池回报的档位串写法不统一（docs/11-fields.md §1.1）· 归一错会让 quota 判断错
// （PRO 1000 被当成 Power 10000 → 用量条错 10 倍 · 死号判定永不触发）。
func TestNormalizePlan(t *testing.T) {
	cases := []struct {
		raw  string
		want providers.SubscriptionPlan
	}{
		// 标准写法
		{"power", providers.PlanPower},
		{"pro", providers.PlanPro},
		{"pro_plus", providers.PlanProPlus},
		{"pro_max", providers.PlanProMax},
		// 大小写
		{"Pro", providers.PlanPro},
		{"POWER", providers.PlanPower},
		{"PRO+", providers.PlanProPlus},
		// 带品牌前缀（号池实际可能这么返）
		{"KIRO PRO+", providers.PlanProPlus},
		{"Kiro Pro Max", providers.PlanProMax},
		{"kiro power", providers.PlanPower},
		// 分隔符变体
		{"pro-plus", providers.PlanProPlus},
		{"pro plus", providers.PlanProPlus},
		{"proplus", providers.PlanProPlus},
		{"pro max", providers.PlanProMax},
		// 空白
		{"  pro  ", providers.PlanPro},
		// 认不出 → 空（落 NULL · 宁缺勿错）
		{"", ""},
		{"unknown", ""},
		{"enterprise", ""}, // account_kind 不是 plan · 别混
		{"free", ""},
	}
	for _, c := range cases {
		if got := normalizePlan(c.raw); got != c.want {
			t.Errorf("normalizePlan(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// 归一结果必须是合法枚举 —— 防归一函数返回一个 Valid() 不认的值
func TestNormalizePlan_结果都合法(t *testing.T) {
	for _, raw := range []string{"power", "Pro", "KIRO PRO+", "pro max", "pro_plus"} {
		got := normalizePlan(raw)
		if got == "" {
			t.Errorf("normalizePlan(%q) 应该认出来", raw)
			continue
		}
		if !got.Valid() {
			t.Errorf("normalizePlan(%q) = %q · 不在 AllSubscriptionPlans 里", raw, got)
		}
	}
}

// AccountKind 空值必须归一到 enterprise —— 老数据 / 老调用方全靠这个保持原行为
func TestAccountKind_空值归一(t *testing.T) {
	var zero providers.AccountKind
	if got := zero.Normalize(); got != providers.AccountEnterprise {
		t.Errorf("空 AccountKind 归一 = %q, want %q", got, providers.AccountEnterprise)
	}
	if got := providers.AccountPersonal.Normalize(); got != providers.AccountPersonal {
		t.Errorf("personal 不该被改成 %q", got)
	}
}
