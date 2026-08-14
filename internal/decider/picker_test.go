package decider

// **回归哨兵 · P4 · 2026-08-14**
//
// AutoPick 存在但只喂 UI · 从不进下单决策 —— 用户看到 "推荐 A 家" · 真拉却走 default ·
// 割裂。修：Orchestrator 加 VendorPicker 接口 · Pull 里 VendorID 空时先问 picker ·
// picker 也没辙才 defaultVendor 兜底。

import (
	"context"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// mockPicker · 记调用 · 可控返值
type mockPicker struct {
	returns providers.VendorID
	zone    providers.Zone
	ok      bool
	calls   int
}

func (m *mockPicker) PickBestVendor(_ context.Context, _ string) (providers.VendorID, providers.Zone, bool) {
	m.calls++
	return m.returns, m.zone, m.ok
}

func (m *mockPicker) PickBestVendorExcluding(_ context.Context, _ string, _ []providers.VendorID) (providers.VendorID, providers.Zone, bool) {
	m.calls++
	return m.returns, m.zone, m.ok
}

// SetPicker 装配 · picker 生效
func TestSetPicker_LateWire(t *testing.T) {
	o := &Orchestrator{}
	if o.picker != nil {
		t.Fatal("初始应 nil")
	}
	p := &mockPicker{returns: providers.VendorKiroCEO, ok: true}
	o.SetPicker(p)
	if o.picker == nil {
		t.Error("SetPicker 后应装上")
	}
}

// SetPicker nil-safe（跟 stockwatch.SetFirer 同一契约）
func TestSetPicker_NilOrchestrator(t *testing.T) {
	// 不该 panic
	var o *Orchestrator
	o.SetPicker(&mockPicker{})
}
