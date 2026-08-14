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

func TestRefillTick_DecideEnqueue_Reschedules(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 111, busID, roundID, "dead", true)
	insertPendingRefill(t, sqldb, "pr1", busID, "c1")

	dec := &mockRefillDecider{verdict: RefillVerdict{Action: RefillEnqueue, Reason: "tight_enqueue"}}
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
		t.Errorf("Enqueue 不该调 puller(第五刀待接) · 得 %d 次", len(pull.calls))
	}
	// 保 pending 等下轮
	var status string
	sqldb.QueryRow(`SELECT status FROM pending_refill WHERE id='pr1'`).Scan(&status)
	if status != "pending" {
		t.Errorf("pending_refill.status = %q · want pending", status)
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

