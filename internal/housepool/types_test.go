package housepool

import "testing"

// I-09 · housepool 主包关键纯函数覆盖 · 类型 + 判据
// (kirors 子包已有测试 · 这里只测本包 identifier 拼接 + reason 判据)

func TestIsDeadReason(t *testing.T) {
	dead := []string{ReasonSuspended, ReasonQuotaExceeded, ReasonInvalidRefreshToken}
	notDead := []string{
		ReasonManual, ReasonTooManyFailures, ReasonTooManyRefreshFailures,
		ReasonAutoThrottled, ReasonInvalidConfig, "unknown_reason", "",
	}
	for _, r := range dead {
		if !IsDeadReason(r) {
			t.Errorf("IsDeadReason(%q) = false · want true", r)
		}
	}
	for _, r := range notDead {
		if IsDeadReason(r) {
			t.Errorf("IsDeadReason(%q) = true · want false", r)
		}
	}
}

func TestNeedsProbe(t *testing.T) {
	needs := []string{ReasonTooManyFailures, ReasonTooManyRefreshFailures, ReasonAutoThrottled}
	notNeeds := []string{
		ReasonSuspended, ReasonQuotaExceeded, ReasonInvalidRefreshToken,
		ReasonManual, ReasonInvalidConfig, "unknown", "",
	}
	for _, r := range needs {
		if !NeedsProbe(r) {
			t.Errorf("NeedsProbe(%q) = false · want true", r)
		}
	}
	for _, r := range notNeeds {
		if NeedsProbe(r) {
			t.Errorf("NeedsProbe(%q) = true · want false", r)
		}
	}
}

// BusGroup / RecordGroup · identifier 拼接 · 拼错一个字号就进错组
// 集中测这两个函数 · 避免各处手拼字符串
func TestGroupNames(t *testing.T) {
	if got := BusGroup("abc-123"); got != "bus-abc-123" {
		t.Errorf("BusGroup = %q · want bus-abc-123", got)
	}
	if got := RecordGroup("passenger-42"); got != "record-passenger-42" {
		t.Errorf("RecordGroup = %q · want record-passenger-42", got)
	}
	// MarketGroup 是常量 · 手工池号进这个 group(decisions §11.15 抢号缓冲)
	if MarketGroup != "market" {
		t.Errorf("MarketGroup = %q · want market", MarketGroup)
	}
}

// TestReasonConstants · reason 常量值稳定(跟号池后端契约挂钩 · 改了会破跨系统 wire)
func TestReasonConstants(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{ReasonManual, "Manual"},
		{ReasonSuspended, "Suspended"},
		{ReasonQuotaExceeded, "QuotaExceeded"},
		{ReasonInvalidRefreshToken, "InvalidRefreshToken"},
		{ReasonTooManyFailures, "TooManyFailures"},
		{ReasonTooManyRefreshFailures, "TooManyRefreshFailures"},
		{ReasonAutoThrottled, "AutoThrottled"},
		{ReasonInvalidConfig, "InvalidConfig"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("reason 常量漂移 · got %q · want %q", c.got, c.want)
		}
	}
}
