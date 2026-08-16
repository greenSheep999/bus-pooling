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
	gotKind providers.AccountKind
}

func (m *mockPicker) PickBestVendor(_ context.Context, _ string) (providers.VendorID, providers.Zone, bool) {
	m.calls++
	return m.returns, m.zone, m.ok
}

// gotKind 记最后一次拿到的 kind · 断言"Pull 把 account_kind 传下来了"
func (m *mockPicker) PickBestVendorForKind(
	_ context.Context, _ string, kind providers.AccountKind,
) (providers.VendorID, providers.Zone, bool) {
	m.calls++
	m.gotKind = kind
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

// picker 必须收到本轮的 account_kind
//
// **回归防线**（2026-08-17 生产事故）：auto 模式选 vendor 时不传 kind ·
// personal 请求被派到只有企业库存的家 → 冻结积分后 ErrNoStock · 手工池那家永远选不到。
// 这里锁住"Pull 把 in.AccountKind 传给 picker"这条契约。
func TestPull_PickerGetsAccountKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   providers.AccountKind
		want providers.AccountKind
	}{
		{"personal 原样传", providers.AccountPersonal, providers.AccountPersonal},
		{"enterprise 原样传", providers.AccountEnterprise, providers.AccountEnterprise},
		// 空值归一成 enterprise（老前端不带这个字段）
		{"空归一成 enterprise", "", providers.AccountEnterprise},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &mockPicker{ok: false} // ok=false 让 Pull 早退 · 只验 picker 入参
			o := &Orchestrator{picker: p}
			// vendorFor 会因为没装 vendor 而报错 · 那之后的流程本测试不关心
			_, _ = o.Pull(context.Background(), PullInput{
				PassengerID: "p1", Count: 1, AccountKind: tc.in,
			})
			if p.calls == 0 {
				t.Fatal("picker 没被调用 —— auto 模式必须问 picker")
			}
			if p.gotKind != tc.want {
				t.Errorf("picker 收到 kind = %q · 期望 %q", p.gotKind, tc.want)
			}
		})
	}
}
