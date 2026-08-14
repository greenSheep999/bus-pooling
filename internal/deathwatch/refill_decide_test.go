package deathwatch

// 第三刀单测 · RefillTick 装配 refillDecider 后行为
//
// 覆盖:
//   · Decide 返 Reject → 标 skipped · 不调 puller
//   · Decide 返 Enqueue → 保 pending 等下轮
//   · Decide 返 Pull → 调 puller · 用 verdict 覆盖 count/vendor/maxPrice
//   · 无 Decider → 老行为(直接 puller)

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// mockRefillDecider · 记录调用参数 + 返固定 verdict
type mockRefillDecider struct {
	mu      sync.Mutex
	calls   []RefillRequest
	verdict RefillVerdict
}

func (m *mockRefillDecider) Decide(_ context.Context, req RefillRequest) RefillVerdict {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	return m.verdict
}

// mockRecordingPuller · 记录调用参数(不同于 refill_step2_test 的 mockPuller · 那个只计次)
type mockRecordingPuller struct {
	mu    sync.Mutex
	calls []RefillRequest
	err   error
}

func (m *mockRecordingPuller) Refill(_ context.Context, req RefillRequest) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	return m.err == nil, m.err
}

// insertPendingRefill · 塞一条 pending_refill
func insertPendingRefill(t *testing.T, sqldb *sql.DB, id, busID, credID string) {
	t.Helper()
	// 前置需要 credential_ledger 存在
	if _, err := sqldb.Exec(`
		INSERT INTO pending_refill
		  (id, dead_credential_id, bus_id, passenger_id, count, vendor_id, status, attempts, created_at)
		VALUES (?, ?, ?, 'p1', 1, 'vA', 'pending', 0, ?)`,
		id, credID, busID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("插 pending_refill: %v", err)
	}
}

func TestRefillTick_DecideReject_MarksSkipped(t *testing.T) {
	sqldb, pid, busID, roundID := setupDB(t)
	_ = pid
	insertCred(t, sqldb, "c1", 111, busID, roundID, "dead", true)
	insertPendingRefill(t, sqldb, "pr1", busID, "c1")

	dec := &mockRefillDecider{verdict: RefillVerdict{Action: RefillReject, Reason: "test"}}
	pull := &mockRecordingPuller{}
	w := &Watcher{
		db:            sqldb,
		refillPuller:  pull,
		refillDecider: dec,
		now:           func() time.Time { return time.Now() },
	}
	w.log = slog.Default()

	processed, err := w.RefillTick(context.Background(), 10)
	if err != nil {
		t.Fatalf("RefillTick: %v", err)
	}
	if processed != 1 {
		t.Errorf("processed = %d · want 1", processed)
	}
	if len(pull.calls) != 0 {
		t.Errorf("Reject 不应调 puller · 得 %d 次", len(pull.calls))
	}
	// 状态应为 skipped
	var status string
	sqldb.QueryRow(`SELECT status FROM pending_refill WHERE id='pr1'`).Scan(&status)
	if status != "skipped" {
		t.Errorf("pending_refill.status = %q · want skipped", status)
	}
}

func TestRefillTick_DecidePull_UsesVerdictParams(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 111, busID, roundID, "dead", true)
	insertPendingRefill(t, sqldb, "pr1", busID, "c1")

	dec := &mockRefillDecider{verdict: RefillVerdict{
		Action:       RefillPull,
		PullCount:    5,
		PullVendor:   "vC",
		PullMaxPrice: 200_000_000,
	}}
	pull := &mockRecordingPuller{}
	w := &Watcher{
		db:            sqldb,
		refillPuller:  pull,
		refillDecider: dec,
		now:           func() time.Time { return time.Now() },
	}
	w.log = slog.Default()

	w.RefillTick(context.Background(), 10)
	if len(pull.calls) != 1 {
		t.Fatalf("Pull 应调 puller · 得 %d 次", len(pull.calls))
	}
	got := pull.calls[0]
	if got.Count != 5 {
		t.Errorf("Count = %d · want 5", got.Count)
	}
	if got.VendorID != "vC" {
		t.Errorf("VendorID = %q · want vC", got.VendorID)
	}
	if got.MaxUnitPrice != 200_000_000 {
		t.Errorf("MaxUnitPrice = %d · want 200_000_000", got.MaxUnitPrice)
	}
}

// mockRefillEnqueuer · 记录调用
type mockRefillEnqueuer struct {
	mu    sync.Mutex
	calls []RefillEnqueueRequest
	err   error
}

func (m *mockRefillEnqueuer) Enqueue(_ context.Context, req RefillEnqueueRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	return m.err
}

func TestRefillTick_DecideEnqueue_NoEnqueuer_Reschedules(t *testing.T) {
	// 未装 enqueuer · 保 pending 兼容旧路径
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 111, busID, roundID, "dead", true)
	insertPendingRefill(t, sqldb, "pr1", busID, "c1")

	dec := &mockRefillDecider{verdict: RefillVerdict{Action: RefillEnqueue, Reason: "tight"}}
	pull := &mockRecordingPuller{}
	w := &Watcher{
		db:            sqldb,
		refillPuller:  pull,
		refillDecider: dec,
		now:           func() time.Time { return time.Now() },
	}
	w.log = slog.Default()

	w.RefillTick(context.Background(), 10)
	if len(pull.calls) != 0 {
		t.Errorf("Enqueue 不该调 puller · 得 %d 次", len(pull.calls))
	}
	var status string
	sqldb.QueryRow(`SELECT status FROM pending_refill WHERE id='pr1'`).Scan(&status)
	if status != "pending" {
		t.Errorf("nil enqueuer 应保 pending · 得 %q", status)
	}
}

// **闭环测试** · Decide 返 Enqueue → 真调 enqueuer + pending_refill 标 fulfilled
func TestRefillTick_DecideEnqueue_CallsEnqueuer_MarksFulfilled(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 111, busID, roundID, "dead", true)
	insertPendingRefill(t, sqldb, "pr1", busID, "c1")

	dec := &mockRefillDecider{verdict: RefillVerdict{
		Action:       RefillEnqueue,
		PullVendor:   "vT",
		PullMaxPrice: 150_000_000,
	}}
	pull := &mockRecordingPuller{}
	enq := &mockRefillEnqueuer{}
	w := &Watcher{
		db:             sqldb,
		refillPuller:   pull,
		refillDecider:  dec,
		refillEnqueuer: enq,
		now:            func() time.Time { return time.Now() },
	}
	w.log = slog.Default()

	w.RefillTick(context.Background(), 10)
	if len(enq.calls) != 1 {
		t.Fatalf("Enqueue 应调 enqueuer 1 次 · 得 %d", len(enq.calls))
	}
	if len(pull.calls) != 0 {
		t.Errorf("Enqueue 不该调 puller · 得 %d 次", len(pull.calls))
	}
	got := enq.calls[0]
	if got.RefillID != "pr1" || got.BusID != busID || got.PreferredVendor != "vT" || got.MaxUnitPrice != 150_000_000 {
		t.Errorf("enqueue 参数不对: %+v", got)
	}
	// pending_refill 应标 fulfilled · 不无限 pending
	var status, errCol sql.NullString
	sqldb.QueryRow(`SELECT status, last_error FROM pending_refill WHERE id='pr1'`).Scan(&status, &errCol)
	if status.String != "fulfilled" {
		t.Errorf("pending_refill.status = %q · want fulfilled(挂 stockwatch 后不无限 pending)", status.String)
	}
	if errCol.String != "enqueued_to_stockwatch" {
		t.Errorf("pending_refill.last_error = %q · want enqueued_to_stockwatch", errCol.String)
	}
}

func TestRefillTick_NoDecider_LegacyPath(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 111, busID, roundID, "dead", true)
	insertPendingRefill(t, sqldb, "pr1", busID, "c1")

	pull := &mockRecordingPuller{}
	w := &Watcher{
		db:           sqldb,
		refillPuller: pull,
		// 不装 refillDecider · 走老行为
		now: func() time.Time { return time.Now() },
	}
	w.log = slog.Default()

	w.RefillTick(context.Background(), 10)
	if len(pull.calls) != 1 {
		t.Errorf("nil decider 应走老逻辑直接 puller · 得 %d 次", len(pull.calls))
	}
}

