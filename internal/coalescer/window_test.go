package coalescer

// **v2 · 2026-08-15** · 集单窗口测试
//
// 覆盖：同 bus 多人在 200ms 窗口内合流 · 达 MaxBatch 提前关 · 不同 bus 不合流 ·
// executor 报错传给所有等待者 · 单人 record group（无 bus）走 Single 兜底。

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// providersVendorID · 测试用短别名
func providersVendorID(s string) providers.VendorID {
	return providers.VendorID(s)
}

// mockExecutor · 记调用参数 + 可控返值 / err
type mockExecutor struct {
	mu        sync.Mutex
	calls     []*BatchIntent
	returns   any
	returnErr error
	delay     time.Duration
}

func (m *mockExecutor) Execute(_ context.Context, batch *BatchIntent) (any, error) {
	m.mu.Lock()
	m.calls = append(m.calls, batch)
	m.mu.Unlock()
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return m.returns, m.returnErr
}

func (m *mockExecutor) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// 3 人同 bus · 200ms 窗口内加入 · 合流成 1 次 Execute
func TestWindow_ThreeIntentsInSameBus_MergedOnce(t *testing.T) {
	exec := &mockExecutor{returns: "batch-result-obj"}
	w := NewWindow(WindowConfig{Duration: 100 * time.Millisecond, MaxBatch: 8}, exec)

	var wg sync.WaitGroup
	results := make([]*BatchResult, 3)
	errs := make([]error, 3)
	for i, pid := range []string{"p1", "p2", "p3"} {
		i, pid := i, pid
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = w.Add(context.Background(), Intent{
				PassengerID: pid, BusID: "bus1", Count: 2,
				IdempotencyRecordID: "idem-" + pid,
			})
		}()
		time.Sleep(10 * time.Millisecond) // 三个陆续到 · 都在 100ms 窗口内
	}
	wg.Wait()

	if exec.callCount() != 1 {
		t.Fatalf("Execute 应被调 1 次（合流）· 得 %d", exec.callCount())
	}
	batch := exec.calls[0]
	if batch.CountTotal != 6 {
		t.Errorf("CountTotal = %d · want 6（2+2+2）", batch.CountTotal)
	}
	if len(batch.Participants) != 3 {
		t.Errorf("参与者 %d · want 3", len(batch.Participants))
	}
	// 每个成员看到同一份 BatchResult
	for i := range results {
		if errs[i] != nil {
			t.Errorf("[%d] err = %v", i, errs[i])
		}
		if results[i] == nil || results[i].PullResult != "batch-result-obj" {
			t.Errorf("[%d] result = %+v", i, results[i])
		}
	}
}

// 不同 bus · 不合流 · 两次 Execute
func TestWindow_DifferentBuses_Separated(t *testing.T) {
	exec := &mockExecutor{returns: "ok"}
	w := NewWindow(WindowConfig{Duration: 50 * time.Millisecond, MaxBatch: 8}, exec)

	var wg sync.WaitGroup
	for _, tc := range []struct{ pid, bus string }{{"p1", "busA"}, {"p2", "busB"}} {
		tc := tc
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Add(context.Background(), Intent{PassengerID: tc.pid, BusID: tc.bus, Count: 1})
		}()
	}
	wg.Wait()

	if exec.callCount() != 2 {
		t.Errorf("不同 bus 应分别 Execute 2 次 · 得 %d", exec.callCount())
	}
}

// 同 bus 同 zone 但不同 vendor · 不合流
func TestWindow_DifferentVendors_Separated(t *testing.T) {
	exec := &mockExecutor{returns: "ok"}
	w := NewWindow(WindowConfig{Duration: 50 * time.Millisecond}, exec)

	var wg sync.WaitGroup
	for _, v := range []string{"kiro91", "kiroceo"} {
		v := v
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Add(context.Background(), Intent{
				PassengerID: "p", BusID: "bus1", Count: 1, VendorID: providersVendorID(v),
			})
		}()
	}
	wg.Wait()

	if exec.callCount() != 2 {
		t.Errorf("不同 vendor 应分别 Execute · 得 %d", exec.callCount())
	}
}

// MaxBatch 达到时提前关窗（不等到 Duration）
func TestWindow_MaxBatchTriggersEarlyClose(t *testing.T) {
	exec := &mockExecutor{returns: "ok"}
	w := NewWindow(WindowConfig{Duration: 5 * time.Second, MaxBatch: 3}, exec)

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Add(context.Background(), Intent{
				PassengerID: string(rune('a' + i)), BusID: "bus1", Count: 1,
			})
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("MaxBatch=3 达上限该提前关窗 · 却等了 %v（Duration=5s）", elapsed)
	}
	if exec.callCount() != 1 {
		t.Errorf("3 人满窗 · Execute 1 次 · 得 %d", exec.callCount())
	}
}

// executor 报错 · 所有等待者都拿到同一份 err
func TestWindow_ExecutorErrorBroadcast(t *testing.T) {
	sentinel := errors.New("vendor 端点炸")
	exec := &mockExecutor{returnErr: sentinel}
	w := NewWindow(WindowConfig{Duration: 50 * time.Millisecond}, exec)

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := 0; i < 3; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = w.Add(context.Background(), Intent{
				PassengerID: string(rune('a' + i)), BusID: "bus1", Count: 1,
			})
		}()
		time.Sleep(5 * time.Millisecond)
	}
	wg.Wait()

	for i, e := range errs {
		if !errors.Is(e, sentinel) {
			t.Errorf("[%d] err = %v · want sentinel", i, e)
		}
	}
}

// ctx 超时 · 单个成员退出不影响其他
func TestWindow_CtxCancelIndependent(t *testing.T) {
	exec := &mockExecutor{returns: "ok", delay: 100 * time.Millisecond}
	w := NewWindow(WindowConfig{Duration: 50 * time.Millisecond}, exec)

	// p1 · 立即 cancel
	ctxCancel, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	var p1Err atomic.Value
	go func() {
		_, err := w.Add(ctxCancel, Intent{PassengerID: "p1", BusID: "bus1", Count: 1})
		if err != nil {
			p1Err.Store(err)
		}
	}()

	// p2 · 正常 · 加入同 bus · 应能完成
	time.Sleep(10 * time.Millisecond)
	res, err := w.Add(context.Background(), Intent{PassengerID: "p2", BusID: "bus1", Count: 1})
	if err != nil {
		t.Errorf("p2 应正常完成 · 得 %v", err)
	}
	if res == nil || res.PullResult != "ok" {
		t.Errorf("p2 result = %+v", res)
	}
}

// record group（BusID 空）经 AnonV2 → 走 Single 兜底 · 返 ErrNoExecutor
func TestAnonV2_NoBus_FallsBackToSingle(t *testing.T) {
	// 装 default window
	SetDefaultWindow(NewWindow(WindowConfig{}, &mockExecutor{}))
	defer SetDefaultWindow(nil)

	res, err := AnonV2(context.Background(), Intent{PassengerID: "p1", BusID: "", Count: 1})
	if !errors.Is(err, ErrNoExecutor) {
		t.Errorf("无 bus 该返 ErrNoExecutor 让上层降级 · 得 %v", err)
	}
	if res == nil || res.Batch == nil {
		t.Fatal("即便报错也该返 batch 让上层能拿到 · 得 nil")
	}
	if len(res.Batch.Participants) != 1 {
		t.Errorf("Single 兜底应返 1 人 batch")
	}
}

// AnonV2 · 未装 default window · 也返 ErrNoExecutor
func TestAnonV2_NoDefaultWindow(t *testing.T) {
	SetDefaultWindow(nil)
	_, err := AnonV2(context.Background(), Intent{PassengerID: "p1", BusID: "bus1", Count: 1})
	if !errors.Is(err, ErrNoExecutor) {
		t.Errorf("未装配 window 该返 ErrNoExecutor · 得 %v", err)
	}
}

// nil Window.Add · 报 ErrNoExecutor（防御性）
func TestWindow_NilAdd(t *testing.T) {
	var w *Window
	_, err := w.Add(context.Background(), Intent{PassengerID: "p", BusID: "b", Count: 1})
	if !errors.Is(err, ErrNoExecutor) {
		t.Errorf("nil Window 该返 ErrNoExecutor · 得 %v", err)
	}
}
