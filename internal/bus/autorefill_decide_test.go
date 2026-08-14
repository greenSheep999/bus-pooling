package bus

// 第二刀单测 · Scheduler 装配了 decider 后行为
//
// 覆盖:
//   · Decide 返 Reject → 不调 puller
//   · Decide 返 Enqueue → 不调 puller(第五刀接 stockwatch·现在先跳)
//   · Decide 返 Pull → 调 puller · 参数从 verdict 传下去
//   · loadCandidates 按 vendor 分组 · 多 vendor 车 aliveByVendor 正确

import (
	"context"
	"sync"
	"testing"
)

// mockDecider · 记录调用参数 + 返固定 verdict
type mockDecider struct {
	mu      sync.Mutex
	calls   []SchedulerCandidate
	verdict SchedulerVerdict
}

func (m *mockDecider) Decide(_ context.Context, _ string, cand SchedulerCandidate) SchedulerVerdict {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, cand)
	return m.verdict
}

func (m *mockDecider) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockDecider) lastCand() SchedulerCandidate {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return SchedulerCandidate{}
	}
	return m.calls[len(m.calls)-1]
}

func TestSchedulerDecide_Reject_NotPulling(t *testing.T) {
	s, refill, exec := setupSchedulerDB(t)
	insertBus(exec, "b1", true, 5, -1, 2) // alive=2 · watermark=5

	dec := &mockDecider{verdict: SchedulerVerdict{Action: ActionReject, Reason: "test"}}
	s.SetDecider(dec)

	s.ScanOnce(context.Background())

	if dec.callCount() != 1 {
		t.Errorf("Decide 应调 1 次·得 %d", dec.callCount())
	}
	if refill.count() != 0 {
		t.Errorf("Reject 不应调 puller · 得 %d", refill.count())
	}
}

// mockEnqueuer · 记录调用 · 用于覆盖 Enqueue 执行链
type mockEnqueuer struct {
	mu    sync.Mutex
	calls []AutoEnqueueRequest
	err   error
}

func (m *mockEnqueuer) Enqueue(_ context.Context, req AutoEnqueueRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	return m.err
}

func (m *mockEnqueuer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func TestSchedulerDecide_Enqueue_NoEnqueuer_LogsOnly(t *testing.T) {
	// nil enqueuer · Enqueue 分支只 log · 不调 puller
	s, refill, exec := setupSchedulerDB(t)
	insertBus(exec, "b1", true, 5, -1, 2)

	dec := &mockDecider{verdict: SchedulerVerdict{Action: ActionEnqueue}}
	s.SetDecider(dec)
	// 不 SetEnqueuer

	s.ScanOnce(context.Background())

	if refill.count() != 0 {
		t.Errorf("Enqueue 不调 puller · 得 %d", refill.count())
	}
}

// **闭环测试** · Decide 返 Enqueue → 真调 stockwatch.Enqueue
func TestSchedulerDecide_Enqueue_CallsEnqueuer(t *testing.T) {
	s, refill, exec := setupSchedulerDB(t)
	insertBus(exec, "b1", true, 5, -1, 2) // watermark=5·alive=2

	dec := &mockDecider{verdict: SchedulerVerdict{
		Action:       ActionEnqueue,
		PullCount:    3,
		PullVendor:   "vT",
		PullMaxPrice: 80_000_000,
	}}
	enq := &mockEnqueuer{}
	s.SetDecider(dec)
	s.SetEnqueuer(enq)

	s.ScanOnce(context.Background())

	if enq.count() != 1 {
		t.Fatalf("Enqueue 应调 enqueuer 1 次 · 得 %d", enq.count())
	}
	if refill.count() != 0 {
		t.Errorf("Enqueue 不该调 puller · 得 %d", refill.count())
	}
	got := enq.calls[0]
	if got.BusID != "b1" {
		t.Errorf("BusID = %q · want b1", got.BusID)
	}
	if got.Count != 3 {
		t.Errorf("Count = %d · want 3", got.Count)
	}
	if got.PreferredVendor != "vT" {
		t.Errorf("PreferredVendor = %q · want vT", got.PreferredVendor)
	}
	if got.MaxUnitPrice != 80_000_000 {
		t.Errorf("MaxUnitPrice = %d · want 80_000_000", got.MaxUnitPrice)
	}
}

func TestSchedulerDecide_Pull_UsesVerdictParams(t *testing.T) {
	s, refill, exec := setupSchedulerDB(t)
	insertBus(exec, "b1", true, 5, -1, 2)

	dec := &mockDecider{verdict: SchedulerVerdict{
		Action:       ActionPull,
		PullCount:    7,
		PullVendor:   "vB",
		PullMaxPrice: 100_000_000,
	}}
	s.SetDecider(dec)

	s.ScanOnce(context.Background())

	if refill.count() != 1 {
		t.Fatalf("Pull 应调 puller 1 次·得 %d", refill.count())
	}
	req := refill.calls[0]
	if req.Count != 7 {
		t.Errorf("Count = %d · want 7", req.Count)
	}
	if req.PreferredVendor != "vB" {
		t.Errorf("PreferredVendor = %q · want vB", req.PreferredVendor)
	}
	if req.MaxUnitPrice != 100_000_000 {
		t.Errorf("MaxUnitPrice = %d · want 100_000_000", req.MaxUnitPrice)
	}
}

func TestSchedulerDecide_LoadsAliveByVendor(t *testing.T) {
	s, _, exec := setupSchedulerDB(t)
	// 建两辆车 · b1 只有 vendor A · b2 有 vendor A 和 vendor B
	insertBus(exec, "b1", true, 5, -1, 2) // 2 个 vA(insertBus 里 vendor_id 硬写)
	// b2 分别插 vA=3 + vB=4 需要手工插
	exec(`INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at,
	                       auto_refill_enabled, refill_watermark, refill_min_count)
	      VALUES ('b2', 'multi', 'single', 'p1', 'active', '2026-01-01T00:00:00Z', 1, 10, ?)`, nil)
	exec(`INSERT INTO pull_round
	        (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
	         key_cost_total, service_fee_total, participants_split_json, status, created_at)
	      VALUES ('r-b2-a', 'vA', 'co-b2-a', 'b2', 3, 3, 0, 0, '{}', 'completed', '2026-01-01T00:00:00Z')`)
	exec(`INSERT INTO pull_round
	        (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
	         key_cost_total, service_fee_total, participants_split_json, status, created_at)
	      VALUES ('r-b2-b', 'vB', 'co-b2-b', 'b2', 4, 4, 0, 0, '{}', 'completed', '2026-01-01T00:00:00Z')`)
	base := kiroCredIDCounter
	for i := 0; i < 3; i++ {
		base++
		id := "b2-a-" + string(rune('a'+i))
		exec(`INSERT INTO credential_ledger
		        (id, kiro_rs_credential_id, owner_bus_id, current_group, vendor_id,
		         source_pull_round_id, status, pulled_at)
		      VALUES (?, ?, 'b2', 'bus-b2', 'vA', 'r-b2-a', 'alive', '2026-01-01T00:00:00Z')`,
			id, base)
	}
	for i := 0; i < 4; i++ {
		base++
		id := "b2-b-" + string(rune('a'+i))
		exec(`INSERT INTO credential_ledger
		        (id, kiro_rs_credential_id, owner_bus_id, current_group, vendor_id,
		         source_pull_round_id, status, pulled_at)
		      VALUES (?, ?, 'b2', 'bus-b2', 'vB', 'r-b2-b', 'alive', '2026-01-01T00:00:00Z')`,
			id, base)
	}
	kiroCredIDCounter = base

	dec := &mockDecider{verdict: SchedulerVerdict{Action: ActionReject}}
	s.SetDecider(dec)
	s.ScanOnce(context.Background())

	// 应该对 b1 和 b2 各调一次
	if dec.callCount() != 2 {
		t.Fatalf("应调 Decide 2 次(b1+b2) · 得 %d", dec.callCount())
	}
	// 找 b2 的 candidate · 验 aliveByVendor 正确按 vendor 分组
	var b2 SchedulerCandidate
	for _, c := range dec.calls {
		if c.BusID == "b2" {
			b2 = c
			break
		}
	}
	if b2.BusID != "b2" {
		t.Fatal("找不到 b2 candidate")
	}
	if b2.AliveByVendor["vA"] != 3 {
		t.Errorf("b2.aliveByVendor[vA] = %d · want 3", b2.AliveByVendor["vA"])
	}
	if b2.AliveByVendor["vB"] != 4 {
		t.Errorf("b2.aliveByVendor[vB] = %d · want 4", b2.AliveByVendor["vB"])
	}
}
