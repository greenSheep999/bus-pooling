package bus

// 1d · 自动补车 scheduler unit test
//
// 覆盖：不触发（禁用 / 解散 / 满水位）· 触发一次（补齐差额）· puller 报错不 panic。

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

// mockRefiller · 记录调用
type mockRefiller struct {
	mu    sync.Mutex
	calls []AutoRefillRequest
	err   error
}

func (m *mockRefiller) Refill(_ context.Context, req AutoRefillRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	return m.err
}

func (m *mockRefiller) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func setupSchedulerDB(t *testing.T) (*Scheduler, *mockRefiller, func(sql string, args ...any)) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	// 建一个 passenger · 满足外键
	if _, err := d.DB.Exec(`INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
	                        VALUES ('p1', 'alice', 'a@x.io', 'x', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	m := &mockRefiller{}
	s := NewScheduler(d.DB, m, 5*time.Minute, nil)
	// 1f-C · Scheduler 现在强制走 Decider(§4.3.4)· 单测里用 rawFallbackDecider
	// 直接把车级字段当最终值判决 · 语义 = 老 nil-decider 兜底路径(passenger_strategy_default
	// 表在 mock 场景空表 · 车级字段即"最终生效值")。
	s.SetDecider(&rawFallbackDecider{})
	exec := func(sql string, args ...any) {
		if _, err := d.DB.Exec(sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	return s, m, exec
}

// rawFallbackDecider · 单测用 · 把 SchedulerCandidate 的车级原始字段当最终值判决 ·
// 复刻 1a-1c 老 nil-decider 语义(单测 mock 场景 psd 表空 · 车级字段即最终值)。
type rawFallbackDecider struct{}

func (rawFallbackDecider) Decide(_ context.Context, _ string, cand SchedulerCandidate) SchedulerVerdict {
	// 计算 aliveTotal
	alive := 0
	for _, n := range cand.AliveByVendor {
		alive += n
	}
	if !cand.AutoRefill {
		return SchedulerVerdict{Action: ActionReject, Reason: "auto_refill_off"}
	}
	if cand.Watermark <= 0 {
		return SchedulerVerdict{Action: ActionReject, Reason: "watermark_zero"}
	}
	if alive >= cand.Watermark {
		return SchedulerVerdict{Action: ActionReject, Reason: "at_or_above_watermark"}
	}
	count := cand.MinCount
	if count <= 0 {
		count = cand.Watermark - alive
	}
	if count <= 0 {
		return SchedulerVerdict{Action: ActionReject, Reason: "no_gap"}
	}
	return SchedulerVerdict{
		Action:       ActionPull,
		PullCount:    count,
		PullVendor:   cand.PreferredVendor,
		PullMaxPrice: cand.MaxUnitPrice,
	}
}

// kiroCredIDCounter · 每次插号自增（credential_ledger.kiro_rs_credential_id UNIQUE）
var kiroCredIDCounter uint64 = 1000

// 建一辆 auto_refill 车 · alive 号数按参数塞
//
// minCount<0 表 NULL（走"补齐差额"路径）· ≥0 表显式值。
func insertBus(exec func(string, ...any), busID string, autoRefill bool, watermark, minCount, alive int) {
	autoInt := 0
	if autoRefill {
		autoInt = 1
	}
	var mc any
	if minCount < 0 {
		mc = nil
	} else {
		mc = minCount
	}
	exec(`INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at,
	                       auto_refill_enabled, refill_watermark, refill_min_count)
	      VALUES (?, ?, 'single', 'p1', 'active', '2026-01-01T00:00:00Z', ?, ?, ?)`,
		busID, "bus-"+busID, autoInt, watermark, mc)
	if alive == 0 {
		return
	}
	// pull_round 父行（外键 · 一辆车共用一条即可）
	roundID := "r-" + busID
	exec(`INSERT INTO pull_round
	        (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
	         key_cost_total, service_fee_total, participants_split_json, status, created_at)
	      VALUES (?, 'kiro91', ?, ?, ?, ?, 0, 0, '{}', 'completed', '2026-01-01T00:00:00Z')`,
		roundID, "co-"+busID, busID, alive, alive)
	for i := 0; i < alive; i++ {
		credID := busID + "-alive-" + string(rune('a'+i))
		kiroCredIDCounter++
		exec(`INSERT INTO credential_ledger
		        (id, kiro_rs_credential_id, owner_bus_id, current_group, vendor_id,
		         source_pull_round_id, status, pulled_at)
		      VALUES (?, ?, ?, ?, 'kiro91', ?, 'alive', '2026-01-01T00:00:00Z')`,
			credID, kiroCredIDCounter, busID, "bus-"+busID, roundID)
	}
}

// 禁用 auto_refill · 不该触发
//
// **1f-C 后**·SQL 不再筛 auto_refill_enabled(§4.3.4 · 不做 fallback 合并)·
// 所有 active 车都进 candidate 池 · 由 Decider 拒 · 所以 touched=1(扫过) ·
// refilled=0(拒绝补) · puller 0 次调用。
func TestScheduler_DisabledBusNotTriggered(t *testing.T) {
	s, m, exec := setupSchedulerDB(t)
	insertBus(exec, "b1", false, 5, 3, 0)
	_, refilled := s.ScanOnce(context.Background())
	if refilled != 0 {
		t.Errorf("禁用车不该补 · refilled=%d", refilled)
	}
	if m.count() != 0 {
		t.Errorf("不该调 puller · 得 %d", m.count())
	}
}

// 已解散车 · 不该触发
func TestScheduler_DissolvedBusNotTriggered(t *testing.T) {
	s, m, exec := setupSchedulerDB(t)
	exec(`INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at,
	                       dissolved_at, auto_refill_enabled, refill_watermark)
	      VALUES ('b1', 'gone', 'single', 'p1', 'dissolved', '2026-01-01T00:00:00Z',
	              '2026-01-02T00:00:00Z', 1, 5)`)
	s.ScanOnce(context.Background())
	if m.count() != 0 {
		t.Errorf("解散车不该触发 · 得 %d", m.count())
	}
}

// alive >= watermark · 不该触发
func TestScheduler_AtWatermarkNotTriggered(t *testing.T) {
	s, m, exec := setupSchedulerDB(t)
	insertBus(exec, "b1", true, 3, -1, 3)
	touched, refilled := s.ScanOnce(context.Background())
	if touched != 1 {
		t.Errorf("扫到 1 辆 · 得 %d", touched)
	}
	if refilled != 0 || m.count() != 0 {
		t.Errorf("满水位不该补 · refilled=%d calls=%d", refilled, m.count())
	}
}

// alive < watermark · 无 min_count · 补齐差额
func TestScheduler_TriggersRefillToWatermark(t *testing.T) {
	s, m, exec := setupSchedulerDB(t)
	insertBus(exec, "b1", true, 5, -1, 2) // watermark=5 alive=2 · 应补 3
	_, refilled := s.ScanOnce(context.Background())
	if refilled != 1 {
		t.Errorf("应补 1 次 · 得 %d", refilled)
	}
	if m.count() != 1 {
		t.Fatalf("puller 应调 1 次 · 得 %d", m.count())
	}
	if m.calls[0].Count != 3 {
		t.Errorf("补齐差额应 = 3 · 得 %d", m.calls[0].Count)
	}
	if m.calls[0].BusID != "b1" {
		t.Errorf("BusID = %q · want b1", m.calls[0].BusID)
	}
	if m.calls[0].InitiatorPassengerID != "p1" {
		t.Errorf("发起人应是 creator=p1 · 得 %q", m.calls[0].InitiatorPassengerID)
	}
	if len(m.calls[0].IdempotencyRecordID) != 32 {
		t.Errorf("幂等键应 32 位 hex · 得 %q", m.calls[0].IdempotencyRecordID)
	}
}

// min_count 显式设 · 用它不用差额
func TestScheduler_MinCountOverride(t *testing.T) {
	s, m, exec := setupSchedulerDB(t)
	insertBus(exec, "b1", true, 5, 10, 2) // watermark=5 min_count=10 · 应按 10 拉
	s.ScanOnce(context.Background())
	if m.count() != 1 || m.calls[0].Count != 10 {
		t.Errorf("应按 min_count=10 拉 · 得 count=%d calls=%d",
			m.calls[0].Count, m.count())
	}
}

// puller 报错 · 不 panic · touched=1 refilled=0
func TestScheduler_PullerErrorNotFatal(t *testing.T) {
	s, m, exec := setupSchedulerDB(t)
	m.err = errors.New("模拟余额不足")
	insertBus(exec, "b1", true, 5, -1, 0)
	touched, refilled := s.ScanOnce(context.Background())
	if touched != 1 {
		t.Errorf("touched = %d · want 1", touched)
	}
	if refilled != 0 {
		t.Errorf("refilled = %d · want 0（err 不算成功）", refilled)
	}
	if m.count() != 1 {
		t.Errorf("puller 仍被调过 · 得 %d", m.count())
	}
}

// 多辆车 · 逐辆处理
func TestScheduler_MultipleBuses(t *testing.T) {
	s, m, exec := setupSchedulerDB(t)
	insertBus(exec, "b1", true, 5, -1, 2) // 差 3
	insertBus(exec, "b2", true, 3, -1, 3) // 满水位 · 跳
	insertBus(exec, "b3", true, 4, 2, 1)  // 差 3 · 但 min_count=2 → 按 2 拉
	touched, refilled := s.ScanOnce(context.Background())
	if touched != 3 {
		t.Errorf("扫 3 辆 · 得 %d", touched)
	}
	if refilled != 2 {
		t.Errorf("应补 2 辆 · 得 %d", refilled)
	}
	// 检查 count 参数
	counts := map[string]int{}
	for _, c := range m.calls {
		counts[c.BusID] = c.Count
	}
	if counts["b1"] != 3 {
		t.Errorf("b1 应补 3 · 得 %d", counts["b1"])
	}
	if counts["b3"] != 2 {
		t.Errorf("b3 应按 min_count=2 · 得 %d", counts["b3"])
	}
	if _, ok := counts["b2"]; ok {
		t.Errorf("b2 满水位不该补 · 得 %d", counts["b2"])
	}
}

// puller nil · Start 应 no-op（不 panic）
func TestScheduler_NilPullerNoPanic(t *testing.T) {
	s := NewScheduler(nil, nil, 0, nil)
	// 不 panic 即 OK
	s.Start(context.Background())
	touched, refilled := s.ScanOnce(context.Background())
	if touched != 0 || refilled != 0 {
		t.Errorf("nil puller 应静默返 · 得 touched=%d refilled=%d", touched, refilled)
	}
	s.Stop(100 * time.Millisecond)
}
