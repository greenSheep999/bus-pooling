package deathwatch

// **回归哨兵 · P6 · 2026-08-14**
//
// 号死后往 pending_refill 塞待补记录 · worker 消费（Step 1 只 log · Step 2 真拉）。
// 本测证：markDead 触发 enqueue · 幂等 · 找不到归属安全跳过 · RefillTick 走通。

import (
	"context"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// 标死会往 pending_refill 塞一条 · rep.RefillEnqueued 计数
func TestMarkDeadEnqueuesRefill(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 1001, busID, roundID, "alive", true)

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 1001, Disabled: true, DisabledReason: housepool.ReasonSuspended},
	}

	w := newWatcher(sqldb, pool)
	rep := w.SweepOnce(context.Background())

	if rep.MarkedDead != 1 {
		t.Fatalf("MarkedDead = %d · want 1", rep.MarkedDead)
	}
	if rep.RefillEnqueued != 1 {
		t.Fatalf("RefillEnqueued = %d · want 1（标死后应塞待补）", rep.RefillEnqueued)
	}

	// 查表：应有一条 pending 记录
	var n int
	if err := sqldb.QueryRow(`SELECT COUNT(*) FROM pending_refill WHERE dead_credential_id='c1' AND status='pending'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pending_refill 应有 1 条 pending · 得 %d", n)
	}
}

// 幂等：重复扫到同一死号只塞一条
func TestEnqueueRefill_Idempotent(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 1001, busID, roundID, "alive", true)

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 1001, Disabled: true, DisabledReason: housepool.ReasonSuspended},
	}
	w := newWatcher(sqldb, pool)

	// 第一次扫 · 标死 + 塞待补
	w.SweepOnce(context.Background())
	// 第二次扫 · 号已经 dead · markDead 幂等 · enqueueRefill 也幂等
	rep2 := w.SweepOnce(context.Background())

	if rep2.MarkedDead != 0 {
		t.Errorf("已 dead 应幂等 · MarkedDead = %d · want 0", rep2.MarkedDead)
	}
	// 已 dead 的号不会重新走 markDead · 也就不会重复 enqueue（rep2.RefillEnqueued 应 0）
	if rep2.RefillEnqueued != 0 {
		t.Errorf("已 dead 的号不该重复 enqueue · RefillEnqueued = %d", rep2.RefillEnqueued)
	}

	// 表里只有 1 条
	var n int
	sqldb.QueryRow(`SELECT COUNT(*) FROM pending_refill WHERE dead_credential_id='c1'`).Scan(&n)
	if n != 1 {
		t.Errorf("pending_refill 应只有 1 条 · 得 %d", n)
	}
}

// RefillTick · Step 1 只 log · 标 skipped
func TestRefillTick_Step1LogOnly(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 1001, busID, roundID, "alive", true)

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 1001, Disabled: true, DisabledReason: housepool.ReasonSuspended},
	}
	w := newWatcher(sqldb, pool)
	w.SweepOnce(context.Background()) // 先塞一条

	// 跑一轮 refill tick
	n, err := w.RefillTick(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("应处理 1 条 · 得 %d", n)
	}

	// 状态应变 skipped（Step 1 只 log · 不真 fire）
	var status, lastErr string
	sqldb.QueryRow(`SELECT status, COALESCE(last_error,'') FROM pending_refill WHERE dead_credential_id='c1'`).Scan(&status, &lastErr)
	if status != "skipped" {
		t.Errorf("Step 1 应标 skipped · 得 %q", status)
	}
	if lastErr != "step1_log_only" {
		t.Errorf("last_error 应说明是 step1 · 得 %q", lastErr)
	}
}

// 第二次 RefillTick 应 no-op（status 已经 skipped · 不是 pending）
func TestRefillTick_OnlyPending(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 1001, busID, roundID, "alive", true)

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 1001, Disabled: true, DisabledReason: housepool.ReasonSuspended},
	}
	w := newWatcher(sqldb, pool)
	w.SweepOnce(context.Background())
	w.RefillTick(context.Background(), 10) // 第一次消费 · 标 skipped

	// 第二次应 0
	n, err := w.RefillTick(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("已 skipped 不该再处理 · 得 %d", n)
	}
}
