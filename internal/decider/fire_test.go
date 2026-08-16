package decider

import (
	"context"
	"sync"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/stockwatch"
)

// mockEnqueuer · 记挂单调用
type mockEnqueuer struct {
	mu    sync.Mutex
	calls []stockwatch.EnqueueParams
}

func (m *mockEnqueuer) Enqueue(_ context.Context, p stockwatch.EnqueueParams) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, p)
	return "w-" + p.ClientOrderID, nil
}

func (m *mockEnqueuer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockEnqueuer) last() (stockwatch.EnqueueParams, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return stockwatch.EnqueueParams{}, false
	}
	return m.calls[len(m.calls)-1], true
}

// auto 模式（VendorID 空）缺货 → 挂单
func TestMaybeEnqueue_AutoMode_Enqueues(t *testing.T) {
	enq := &mockEnqueuer{}
	o := &Orchestrator{enqueuer: enq}

	ok := o.maybeEnqueueOnNoStock(context.Background(), PullInput{
		PassengerID:  "p1",
		Count:        2,
		MaxUnitPrice: 80_000_000,
	}, providers.VendorID("kiroceo"), providers.VendorID(""))

	if !ok {
		t.Fatal("auto 模式缺货应挂单")
	}
	got, _ := enq.last()
	if got.PassengerID != "p1" || got.VendorID != "kiroceo" || got.Count != 2 {
		t.Fatalf("挂单参数不对: %+v", got)
	}
	if got.MaxUnitPrice != 80_000_000 {
		t.Fatalf("单价上限应带进挂单（涨价保护）· 得 %d", got.MaxUnitPrice)
	}
	if got.ClientOrderID == "" {
		t.Fatal("挂单必须带 client_order_id · fire 时要复用它保证 vendor 幂等")
	}
	if got.TargetGroup != "record-p1" {
		t.Fatalf("无 bus 应进 record group · 得 %q", got.TargetGroup)
	}
}

// 用户指定了 vendor → 不代抢（他要等的是那家）
func TestMaybeEnqueue_ExplicitVendor_NoEnqueue(t *testing.T) {
	enq := &mockEnqueuer{}
	o := &Orchestrator{enqueuer: enq}

	ok := o.maybeEnqueueOnNoStock(context.Background(), PullInput{
		PassengerID: "p1",
		Count:       1,
		VendorID:    providers.VendorID("kirooo"), // 明确指定
	}, providers.VendorID("kirooo"), providers.VendorID("kirooo"))

	if ok || enq.count() != 0 {
		t.Fatal("用户指定 vendor 时不应挂单代抢（decisions §11.15）")
	}
}

// **死循环哨兵**：fire 自己触发的那轮（ClientOrderID 非空）绝不能再挂单 ——
// 否则刚 fire 的挂单被复位成 watching · 下次事件又 fire · 无限循环
func TestMaybeEnqueue_FromFire_NoReEnqueue(t *testing.T) {
	enq := &mockEnqueuer{}
	o := &Orchestrator{enqueuer: enq}

	ok := o.maybeEnqueueOnNoStock(context.Background(), PullInput{
		PassengerID:   "p1",
		Count:         1,
		ClientOrderID: "already-a-fire", // fire 传进来的
	}, providers.VendorID("kiroceo"), providers.VendorID(""))

	if ok || enq.count() != 0 {
		t.Fatal("fire 触发的那轮不能再挂单 · 会死循环")
	}
}

// 没装 enqueuer（老装配 / 测试）→ 老行为 · 不挂
func TestMaybeEnqueue_NoEnqueuer_NoOp(t *testing.T) {
	o := &Orchestrator{} // enqueuer 为 nil
	ok := o.maybeEnqueueOnNoStock(context.Background(), PullInput{
		PassengerID: "p1", Count: 1,
	}, providers.VendorID("kiroceo"), providers.VendorID(""))
	if ok {
		t.Fatal("未装 enqueuer 时应保持老行为（缺货直接失败）")
	}
}

// 有 bus 时 target_group 走 bus-<id>
func TestMaybeEnqueue_WithBus_TargetsBusGroup(t *testing.T) {
	enq := &mockEnqueuer{}
	o := &Orchestrator{enqueuer: enq}

	o.maybeEnqueueOnNoStock(context.Background(), PullInput{
		PassengerID: "p1", BusID: "b1", Count: 1,
	}, providers.VendorID("kiroceo"), providers.VendorID(""))

	got, ok := enq.last()
	if !ok {
		t.Fatal("应挂单")
	}
	if got.BusID != "b1" || got.TargetGroup != "bus-b1" {
		t.Fatalf("有 bus 应进 bus group · 得 bus=%q group=%q", got.BusID, got.TargetGroup)
	}
}

// **第四刀关键测试**：AutoPick 填了 in.VendorID 后·requestedVendorID 仍空·应挂单
// 老 bug:老代码用 in.VendorID != "" 判非 auto · AutoPick 填后永远不挂 · 修法用 requestedVendorID
func TestMaybeEnqueue_AutoPickFilled_StillEnqueues(t *testing.T) {
	enq := &mockEnqueuer{}
	o := &Orchestrator{enqueuer: enq}

	// 模拟 Pull 里 AutoPick 已经填了 in.VendorID · 但用户原始请求为空
	ok := o.maybeEnqueueOnNoStock(context.Background(), PullInput{
		PassengerID: "p1",
		Count:       2,
		VendorID:    providers.VendorID("kiroceo"), // AutoPick 后填的 · 不是用户请求的
	}, providers.VendorID("kiroceo"), providers.VendorID("")) // requestedVendorID="" · auto 模式

	if !ok {
		t.Fatal("AutoPick 填了 vendor 后·requestedVendorID 空(auto 模式)·仍应挂单")
	}
	got, _ := enq.last()
	if got.VendorID != "kiroceo" {
		t.Fatalf("挂单 vendor 应用 AutoPick 结果 · 得 %q", got.VendorID)
	}
}
