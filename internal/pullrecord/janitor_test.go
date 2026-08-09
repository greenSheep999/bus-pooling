package pullrecord

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

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
