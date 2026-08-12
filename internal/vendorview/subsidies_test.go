package vendorview

import (
	"testing"
)

// computeSubsidyDelta · 4 种减免 × 2 种规则组合
func TestComputeSubsidyDelta(t *testing.T) {
	bd := Breakdown{
		Base:            20_000_000,
		VendorMarkup:    13_400_000, // 67%
		RegionMarkup:    6_680_000,  // 20%
		SinglePullExtra: 8_000_000,  // 20%（假设 count=1）
		ServiceFee:      2_400_000,  // 5%
	}

	cases := []struct {
		name    string
		kind    string
		rule    string
		want    int64
	}{
		// service_fee · waive · 全免 ServiceFee 层
		{"service_fee waive", "service_fee", `{"kind":"waive"}`, 2_400_000},
		// service_fee · pct 50% · 减半
		{"service_fee pct 50", "service_fee", `{"kind":"pct","pct":50}`, 1_200_000},
		// single_pull · waive
		{"single_pull waive", "single_pull", `{"kind":"waive"}`, 8_000_000},
		// total_discount · 10% · 减总组合价的 10%
		// total = 20 + 13.4 + 6.68 + 8 + 2.4 = 50.48 microunit ×10^6 = 50_480_000
		// 10% = 5_048_000
		{"total 10 pct", "total_discount", `{"kind":"pct","pct":10}`, 5_048_000},
		// 未知 kind
		{"unknown kind", "unknown", `{"kind":"waive"}`, 0},
		// 未知 rule kind
		{"unknown rule", "service_fee", `{"kind":"weird"}`, 0},
		// 非法 JSON
		{"bad json", "service_fee", `not-json`, 0},
		// pct 越界
		{"pct too high", "service_fee", `{"kind":"pct","pct":200}`, 0},
		{"pct zero", "service_fee", `{"kind":"pct","pct":0}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeSubsidyDelta(tc.kind, tc.rule, bd)
			if got != tc.want {
				t.Errorf("kind=%s rule=%s · 得 %d · 期 %d", tc.kind, tc.rule, got, tc.want)
			}
		})
	}
}

// 减免不应把总价减成负数（PricedFor 里有 unitPrice<0 兜底 · 这里锁 delta 逻辑本身）
func TestComputeSubsidyDelta_NeverNegative(t *testing.T) {
	bd := Breakdown{Base: 1_000_000}
	got := computeSubsidyDelta("service_fee", `{"kind":"waive"}`, bd)
	if got != 0 {
		t.Errorf("ServiceFee 层是 0 · 应减 0 · 得 %d", got)
	}
}
