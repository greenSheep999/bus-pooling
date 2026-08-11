package vendorview

import (
	"testing"
	"time"
)

// Prober.nextInterval + bumpHot · 单元测。
//
// 覆盖：
//   - baseline 状态 · 返 interval
//   - bumpHot 后立即 · 返 hotInterval
//   - bumpHot 后过了 hotDuration · 退回 baseline
func TestProber_HotInterval_Baseline(t *testing.T) {
	p := &Prober{
		interval:    60 * time.Second,
		hotInterval: 10 * time.Second,
		hotDuration: 6 * time.Minute,
	}
	if got := p.nextInterval("kiro91"); got != 60*time.Second {
		t.Errorf("baseline · 应该 60s · 得 %v", got)
	}
}

func TestProber_HotInterval_BumpedRecently(t *testing.T) {
	p := &Prober{
		interval:    60 * time.Second,
		hotInterval: 10 * time.Second,
		hotDuration: 6 * time.Minute,
	}
	p.bumpHot("kiro91")
	if got := p.nextInterval("kiro91"); got != 10*time.Second {
		t.Errorf("bump 后 · 应该 10s · 得 %v", got)
	}
}

func TestProber_HotInterval_Expired(t *testing.T) {
	p := &Prober{
		interval:    60 * time.Second,
		hotInterval: 10 * time.Second,
		hotDuration: 100 * time.Millisecond,
	}
	p.bumpHot("kiro91")
	time.Sleep(150 * time.Millisecond)
	if got := p.nextInterval("kiro91"); got != 60*time.Second {
		t.Errorf("过期后应退回 baseline 60s · 得 %v", got)
	}
}

func TestProber_HotInterval_PerVendorIsolation(t *testing.T) {
	p := &Prober{
		interval:    60 * time.Second,
		hotInterval: 10 * time.Second,
		hotDuration: 6 * time.Minute,
	}
	p.bumpHot("vendor-a")
	if got := p.nextInterval("vendor-a"); got != 10*time.Second {
		t.Errorf("vendor-a bump 后 · 应 10s · 得 %v", got)
	}
	if got := p.nextInterval("vendor-b"); got != 60*time.Second {
		t.Errorf("vendor-b 没 bump · 应 60s · 得 %v", got)
	}
}
