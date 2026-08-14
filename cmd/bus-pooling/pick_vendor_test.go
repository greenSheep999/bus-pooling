package main

// P0 回归 · Enqueue 分支 vendor 三级降级(codex 二次审计)
//
// 场景:
//   - bus preferred 非空 → 用它
//   - bus 空 · passenger 非空 → 用 passenger
//   - 都空 · picker nil → 返空(上层判空 skip)

import (
	"context"
	"testing"
)

func TestPickVendorForEnqueue_BusPreferredWins(t *testing.T) {
	got := pickVendorForEnqueue(context.Background(), "vBus", "vPassenger", nil)
	if got != "vBus" {
		t.Errorf("bus preferred 应赢 · 得 %q", got)
	}
}

func TestPickVendorForEnqueue_FallbackToPassenger(t *testing.T) {
	got := pickVendorForEnqueue(context.Background(), "", "vPassenger", nil)
	if got != "vPassenger" {
		t.Errorf("bus 空时应用 passenger · 得 %q", got)
	}
}

func TestPickVendorForEnqueue_AllEmpty_NoPicker_ReturnsEmpty(t *testing.T) {
	got := pickVendorForEnqueue(context.Background(), "", "", nil)
	if got != "" {
		t.Errorf("全空且无 picker 应返空 · 得 %q", got)
	}
}

func TestStrictestMaxPrice(t *testing.T) {
	cases := []struct{ a, b, want int64 }{
		{0, 0, 0},
		{100, 0, 100},
		{0, 100, 100},
		{50, 100, 50},
		{100, 50, 50},
	}
	for _, c := range cases {
		if got := strictestMaxPrice(c.a, c.b); got != c.want {
			t.Errorf("strictestMaxPrice(%d, %d) = %d · want %d", c.a, c.b, got, c.want)
		}
	}
}
