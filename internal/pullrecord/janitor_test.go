package pullrecord

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// mockPool · janitor reconcile 测试用（只需实现 PoolReader）·可控 GetCredential 返回。
type mockPool struct {
	groups []string
	err    error
}

func (m *mockPool) GetCredential(_ context.Context, _ housepool.CredentialID) (*housepool.Credential, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &housepool.Credential{Groups: m.groups}, nil
}

// AssignJanitor · stuck initial → need_manual · 转完可查
func TestAssignJanitor_StuckInitialToNeedManual(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "aj.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}

	// 造乘客 · idempotency_record · credential_ledger · pending_assignment initial（updated_at 2020）
	seed(t, d, "p1", "i1", "c1", "a1")

	j := NewAssignJanitor(AssignJanitorConfig{
		DB: d.DB, StuckAfter: 10 * time.Millisecond,
	})
	// 等 stuck 生效（虽然 2020 早就过了·但用小 stuckAfter 保险）
	time.Sleep(15 * time.Millisecond)
	n, err := j.SweepOnce(ctx)
	if err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	if n != 1 {
		t.Errorf("updated = %d, want 1", n)
	}

	var status string
	if err := d.DB.QueryRow(`SELECT status FROM pending_assignment WHERE id='a1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "need_manual" {
		t.Errorf("status = %s, want need_manual", status)
	}
}

// AssignJanitor · 没 stuck 的不动
func TestAssignJanitor_LeavesRecentAlone(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "aj2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}

	// 用 now 造 initial · 不算 stuck
	seedNow(t, d, "p1", "i1", "c1", "a1")

	j := NewAssignJanitor(AssignJanitorConfig{
		DB: d.DB, StuckAfter: 1 * time.Hour,
	})
	n, _ := j.SweepOnce(ctx)
	if n != 0 {
		t.Errorf("recent 行不该被扫·实际扫了 %d", n)
	}

	var status string
	if err := d.DB.QueryRow(`SELECT status FROM pending_assignment WHERE id='a1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "initial" {
		t.Errorf("status = %s, want initial", status)
	}
}

func seed(t *testing.T, d *db.DB, pid, ir, cid, aid string) {
	t.Helper()
	old := "2020-01-01T00:00:00.000Z"
	seedAt(t, d, pid, ir, cid, aid, old)
}

func seedNow(t *testing.T, d *db.DB, pid, ir, cid, aid string) {
	t.Helper()
	now := time.Now().UTC().Format(timeLayout)
	seedAt(t, d, pid, ir, cid, aid, now)
}

func seedAt(t *testing.T, d *db.DB, pid, ir, cid, aid, ts string) {
	t.Helper()
	if _, err := d.DB.Exec(`
		INSERT INTO passenger (id, email, username, password_hash, created_at, updated_at)
		VALUES (?, ?, ?, 'x', ?, ?)`,
		pid, pid+"@x.com", pid, ts, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.Exec(`
		INSERT INTO idempotency_record (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		VALUES (?, ?, 'POST', '/api/me/pull-records/assign', ?, 'fp', ?)`,
		ir, pid, ir+"0000000000000000000000000000000", ts); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.Exec(`
		INSERT INTO pull_round (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
		                       key_cost_total, service_fee_total, participants_split_json, status, created_at)
		VALUES ('r1', '91kiro', 'co1', NULL, 1, 1, 100, 10, '{}', 'completed', ?)`, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.Exec(`
		INSERT INTO credential_ledger (id, kiro_rs_credential_id, owner_bus_id, owner_record_passenger_id,
		                              current_group, vendor_id, source_pull_round_id, status, disabled, pulled_at, credits_used)
		VALUES (?, 1, NULL, ?, ?, '91kiro', 'r1', 'alive', 0, ?, 0)`,
		cid, pid, "record-"+pid, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.Exec(`
		INSERT INTO pending_assignment (id, idempotency_record_id, passenger_id, credential_id, target, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'to-bus', 'initial', ?, ?)`,
		aid, ir, pid, cid, ts, ts); err != nil {
		t.Fatal(err)
	}
}

// seedForBus · 造带 target_bus_id 的 stuck 行 · pool reconcile 场景专用。
func seedForBus(t *testing.T, d *db.DB, pid, ir, cid, aid, busID string) {
	t.Helper()
	ts := "2020-01-01T00:00:00.000Z"
	seedAt(t, d, pid, ir, cid, aid, ts)
	// bus + bus_member（AssignToBusTx 要求成员归属才通过）
	if _, err := d.DB.Exec(`
		INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at,
		                 auto_refill_enabled, refill_watermark)
		VALUES (?, 'test-bus', 'single', ?, 'active', ?, 0, 0)`,
		busID, pid, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.Exec(`
		INSERT INTO bus_member (bus_id, passenger_id, role, share_pct, status, joined_at)
		VALUES (?, ?, 'owner', 100, 'active', ?)`,
		busID, pid, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.Exec(`
		UPDATE pending_assignment SET target_bus_id = ? WHERE id = ?`, busID, aid); err != nil {
		t.Fatal(err)
	}
}

// 1a 收尾 · pool 已迁 → 前推 completed（forward 分支）
func TestAssignJanitor_Reconcile_Forward(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "af.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	seedForBus(t, d, "p1", "i1", "c1", "a1", "b1")

	pool := &mockPool{groups: []string{"bus-b1"}} // pool 侧 group 已到目标
	j := NewAssignJanitor(AssignJanitorConfig{
		DB: d.DB, Store: NewStore(d.DB), Pool: pool,
		StuckAfter: 10 * time.Millisecond,
	})
	time.Sleep(15 * time.Millisecond)
	if _, err := j.SweepOnce(ctx); err != nil {
		t.Fatal(err)
	}

	var status string
	_ = d.DB.QueryRow(`SELECT status FROM pending_assignment WHERE id='a1'`).Scan(&status)
	if status != "completed" {
		t.Errorf("status = %s · want completed", status)
	}
	// credential_ledger.owner_bus_id 应更新
	var busID string
	_ = d.DB.QueryRow(`SELECT COALESCE(owner_bus_id,'') FROM credential_ledger WHERE id='c1'`).Scan(&busID)
	if busID != "b1" {
		t.Errorf("owner_bus_id = %q · want b1", busID)
	}
	// 幂等响应体应回填
	var respStatus int
	_ = d.DB.QueryRow(`SELECT COALESCE(response_status,0) FROM idempotency_record WHERE id='i1'`).Scan(&respStatus)
	if respStatus != 200 {
		t.Errorf("response_status = %d · want 200", respStatus)
	}
}

// 1a 收尾 · pool 未迁 → 回滚（rollback 分支 · 允许重试）
func TestAssignJanitor_Reconcile_Rollback(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "ar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	seedForBus(t, d, "p1", "i1", "c1", "a1", "b1")

	pool := &mockPool{groups: []string{"record-p1"}} // pool 侧还在 record group · 外部动作没做
	j := NewAssignJanitor(AssignJanitorConfig{
		DB: d.DB, Store: NewStore(d.DB), Pool: pool,
		StuckAfter: 10 * time.Millisecond,
	})
	time.Sleep(15 * time.Millisecond)
	if _, err := j.SweepOnce(ctx); err != nil {
		t.Fatal(err)
	}

	// pending_assignment 应被删
	var cnt int
	_ = d.DB.QueryRow(`SELECT count(1) FROM pending_assignment WHERE id='a1'`).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("pending_assignment 未删除 · cnt=%d", cnt)
	}
	// idempotency_record 也删（response_status IS NULL 才删 · 我们的 seed 是空的）
	_ = d.DB.QueryRow(`SELECT count(1) FROM idempotency_record WHERE id='i1'`).Scan(&cnt)
	if cnt != 0 {
		t.Errorf("idempotency_record 未删除 · cnt=%d（同 key 无法重放）", cnt)
	}
	// credential_ledger 不动
	var ownerBus string
	_ = d.DB.QueryRow(`SELECT COALESCE(owner_bus_id,'') FROM credential_ledger WHERE id='c1'`).Scan(&ownerBus)
	if ownerBus != "" {
		t.Errorf("credential_ledger.owner_bus_id = %q · want 空（未迁）", ownerBus)
	}
}

// **Standards-3 复现** · pool 在 target group · **但台账 owner_bus_id 已被别路径改成另一辆车**
// 应识别为分叉·转 need_manual · 不能直推 completed。
func TestAssignJanitor_Reconcile_ForkDetected(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "afork.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	// 建两个 bus (b1 / b2) · pending 指向 b1 · 但**台账 owner_bus_id 已被别路径设成 b2**
	seedForBus(t, d, "p1", "i1", "c1", "a1", "b1")
	// 造第二辆车 b2 · 并把 credential.owner_bus_id 手工设为 b2 · owner_record_passenger_id=NULL
	ts := "2020-01-01T00:00:00.000Z"
	if _, err := d.DB.Exec(`
		INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at,
		                 auto_refill_enabled, refill_watermark)
		VALUES ('b2', 'other', 'anon', 'p1', 'active', ?, 0, 0)`, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.Exec(`
		UPDATE credential_ledger SET owner_bus_id='b2', owner_record_passenger_id=NULL,
		       current_group='bus-b2'
		 WHERE id='c1'`); err != nil {
		t.Fatal(err)
	}

	// pool 在 target group（bus-b1）· 但台账已在 b2 → 分叉
	pool := &mockPool{groups: []string{"bus-b1"}}
	j := NewAssignJanitor(AssignJanitorConfig{
		DB: d.DB, Store: NewStore(d.DB), Pool: pool,
		StuckAfter: 10 * time.Millisecond,
	})
	time.Sleep(15 * time.Millisecond)
	if _, err := j.SweepOnce(ctx); err != nil {
		t.Fatal(err)
	}

	// 期望：转 need_manual · 不推 completed
	var status, errMsg string
	_ = d.DB.QueryRow(
		`SELECT status, COALESCE(error,'') FROM pending_assignment WHERE id='a1'`,
	).Scan(&status, &errMsg)
	if status != "need_manual" {
		t.Errorf("分叉未检测到 · status = %s · want need_manual", status)
	}
	if !strings.Contains(errMsg, "分叉") {
		t.Errorf("need_manual reason 应含 '分叉' · got=%q", errMsg)
	}
	// credential_ledger 保持 b2（janitor 不该动它）
	var busID string
	_ = d.DB.QueryRow(`SELECT owner_bus_id FROM credential_ledger WHERE id='c1'`).Scan(&busID)
	if busID != "b2" {
		t.Errorf("credential 被 janitor 改动了 · owner_bus_id=%q · want b2（分叉时不动）", busID)
	}
}

// 1a 收尾 · pool 查询失败 → retry（不改状态）
func TestAssignJanitor_Reconcile_PoolErrorRetry(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "ap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	seedForBus(t, d, "p1", "i1", "c1", "a1", "b1")

	pool := &mockPool{err: &housepool.Error{Op: "GetCredential", Status: 500, Message: "boom"}}
	j := NewAssignJanitor(AssignJanitorConfig{
		DB: d.DB, Store: NewStore(d.DB), Pool: pool,
		StuckAfter: 10 * time.Millisecond,
	})
	time.Sleep(15 * time.Millisecond)
	if _, err := j.SweepOnce(ctx); err != nil {
		t.Fatal(err)
	}

	// 状态保留 initial（下轮再试 · 不改）
	var status string
	_ = d.DB.QueryRow(`SELECT status FROM pending_assignment WHERE id='a1'`).Scan(&status)
	if status != "initial" {
		t.Errorf("status = %s · want initial（网络错该下轮重试·不改）", status)
	}
}
