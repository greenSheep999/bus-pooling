package providers

import (
	"context"
	"errors"
	"testing"
)

// fakeVendor 只为测 Registry —— 不碰网络。
type fakeVendor struct {
	id   VendorID
	name string
}

func (f fakeVendor) ID() VendorID           { return f.id }
func (f fakeVendor) ProviderID() ProviderID { return ProviderKiro }
func (f fakeVendor) DisplayName() string    { return f.name }
func (f fakeVendor) Capability() Capability { return Capability{} }

func (f fakeVendor) Stock(context.Context, StockOptions) (*StockSnapshot, error) {
	return nil, ErrNotSupported
}
func (f fakeVendor) Purchase(context.Context, PurchaseRequest) (*PurchaseResult, error) {
	return nil, ErrNotSupported
}
func (f fakeVendor) OrderKeys(context.Context, string) (*PurchaseResult, error) {
	return nil, ErrNotSupported
}
func (f fakeVendor) Balance(context.Context) (*Balance, error) { return nil, ErrNotSupported }
func (f fakeVendor) KeyHealth(context.Context, string) (*KeyHealth, error) {
	return nil, ErrNotSupported
}
func (f fakeVendor) KeyStats(context.Context, KeyStatsOptions) (*KeyStatsBatch, error) {
	return nil, ErrNotSupported
}
func (f fakeVendor) Redeem(context.Context, string) (*RedeemResult, error) {
	return nil, ErrNotSupported
}
func (f fakeVendor) Usage(context.Context, []string) (*UsageBatch, error) {
	return nil, ErrNotSupported
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeVendor{id: Vendor91Kiro, name: "Kiro Market"}, true); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := r.Get(Vendor91Kiro)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != Vendor91Kiro {
		t.Errorf("ID = %q", got.ID())
	}

	if _, err := r.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("未注册的应返回 ErrNotFound，得到 %v", err)
	}
}

// 重复注册必须报错 —— 静默覆盖会让"用了哪份配置"变成猜谜。
func TestRegistryRejectsDuplicate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeVendor{id: Vendor91Kiro, name: "A"}, true); err != nil {
		t.Fatalf("首次注册: %v", err)
	}
	if err := r.Register(fakeVendor{id: Vendor91Kiro, name: "B"}, true); err == nil {
		t.Fatal("重复注册同一个 VendorID 应该报错")
	}
	// 第一份配置不该被覆盖
	got, _ := r.Get(Vendor91Kiro)
	if got.DisplayName() != "A" {
		t.Errorf("DisplayName = %q，重复注册失败后应保留第一份", got.DisplayName())
	}
}

func TestRegistryRejectsNilAndEmptyID(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil, true); err == nil {
		t.Error("nil vendor 应报错")
	}
	if err := r.Register(fakeVendor{id: "", name: "x"}, true); err == nil {
		t.Error("空 ID 应报错")
	}
}

// Enabled() 只给启用的；All() 给全部 —— 停用的 vendor 手上可能还有它拉的号，
// deathwatch / 对账要能回头查它。
func TestRegistryEnabledVsAll(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, fakeVendor{id: Vendor91Kiro, name: "A"}, true)
	mustRegister(t, r, fakeVendor{id: VendorKiroCEO, name: "B"}, false)
	mustRegister(t, r, fakeVendor{id: VendorKiroOOO, name: "C"}, true)

	if all := r.All(); len(all) != 3 {
		t.Errorf("All = %d，want 3（含停用的）", len(all))
	}
	en := r.Enabled()
	if len(en) != 2 {
		t.Fatalf("Enabled = %d，want 2", len(en))
	}
	for _, e := range en {
		if e.VendorID == VendorKiroCEO {
			t.Error("停用的 kiroceo 不该出现在 Enabled() 里")
		}
	}

	// 停用的仍要能 Get 到
	if _, err := r.Get(VendorKiroCEO); err != nil {
		t.Errorf("停用的 vendor 也应能 Get 到: %v", err)
	}
}

// 顺序要稳定，否则日志和测试没法复现。
func TestRegistryOrderIsStable(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, fakeVendor{id: VendorKiroOOO}, true)
	mustRegister(t, r, fakeVendor{id: Vendor91Kiro}, true)
	mustRegister(t, r, fakeVendor{id: VendorKiroCEO}, true)

	want := []VendorID{Vendor91Kiro, VendorKiroCEO, VendorKiroOOO} // 字典序
	for i := 0; i < 5; i++ {
		got := r.All()
		for j, e := range got {
			if e.VendorID != want[j] {
				t.Fatalf("第 %d 次调用: All()[%d] = %q，want %q", i, j, e.VendorID, want[j])
			}
		}
	}
}

func TestRegistrySetEnabled(t *testing.T) {
	r := NewRegistry()
	mustRegister(t, r, fakeVendor{id: Vendor91Kiro}, true)

	if err := r.SetEnabled(Vendor91Kiro, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if len(r.Enabled()) != 0 {
		t.Error("停用后 Enabled() 应为空")
	}
	if err := r.SetEnabled("nope", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("对未注册的 SetEnabled 应返回 ErrNotFound，得到 %v", err)
	}
}

func mustRegister(t *testing.T, r *Registry, v Vendor, enabled bool) {
	t.Helper()
	if err := r.Register(v, enabled); err != nil {
		t.Fatalf("Register %q: %v", v.ID(), err)
	}
}
