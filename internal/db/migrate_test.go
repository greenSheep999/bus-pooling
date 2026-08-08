package db

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
)

// 19 张业务表（不含 schema_migration）· 跟 docs/06-db-schema.md §Migration 1a 一致。
// 这个清单是**故意硬编码**的：迁移少建一张表是后面 issue 的硬故障
// （少 session → Iss #4 跑不了 / 少 idempotency_record → 所有写端点跑不了），
// 所以要在这里锁死，而不是从迁移文件反推。
var wantTables = []string{
	"bus", "bus_member",
	"credential_ledger",
	"idempotency_record",
	"passenger", "passenger_api_key", "passenger_daily_counter",
	"passenger_downstream", "passenger_strategy_default",
	"pending_assignment", "pending_dissolution", "pending_handoff", "pending_purchase",
	"pull_intent", "pull_round",
	"session",
	"vendor_account",
	"wallet", "wallet_ledger",
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("打开测试库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func tableNames(t *testing.T, d *DB) []string {
	t.Helper()
	rows, err := d.Query(
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("查表名: %v", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == "schema_migration" {
			continue
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMigrateUpCreatesAll19Tables(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	ran, err := d.MigrateUp(ctx, "")
	if err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	if len(ran) == 0 {
		t.Fatal("没有应用任何迁移")
	}

	got := tableNames(t, d)
	if len(got) != len(wantTables) {
		t.Fatalf("建了 %d 张表，期望 %d 张\n实际: %v", len(got), len(wantTables), got)
	}

	want := append([]string(nil), wantTables...)
	sort.Strings(want)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("表 %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestMigrateUpIsIdempotent(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatalf("第一次 up: %v", err)
	}
	ran, err := d.MigrateUp(ctx, "")
	if err != nil {
		t.Fatalf("第二次 up: %v", err)
	}
	if len(ran) != 0 {
		t.Fatalf("第二次 up 又应用了 %d 个迁移，应该是 0", len(ran))
	}
}

// Iss #3 的 DoD：migrate up / down 干净
func TestMigrateDownDropsEverything(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatalf("up: %v", err)
	}
	if _, err := d.MigrateDown(ctx, "", 1); err != nil {
		t.Fatalf("down: %v", err)
	}

	if got := tableNames(t, d); len(got) != 0 {
		t.Fatalf("回滚后还剩 %d 张表: %v", len(got), got)
	}

	// 回滚后应该能重新 up（不是一次性的）
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatalf("回滚后重新 up: %v", err)
	}
	if got := tableNames(t, d); len(got) != len(wantTables) {
		t.Fatalf("重新 up 后 %d 张表，期望 %d", len(got), len(wantTables))
	}
}

func TestMigrateStatus(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	all, applied, err := d.MigrateStatus(ctx, "")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("没找到迁移文件")
	}
	if len(applied) != 0 {
		t.Fatalf("还没 up 就有 %d 个已应用", len(applied))
	}

	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatal(err)
	}
	all, applied, err = d.MigrateStatus(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(all) {
		t.Fatalf("up 之后 %d/%d 已应用", len(applied), len(all))
	}
}

func TestLoadMigrationsRequiresUpAndDown(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"缺 up", "-- +migrate down\nDROP TABLE x;"},
		{"缺 down", "-- +migrate up\nCREATE TABLE x (id TEXT);"},
		{"down 在 up 前", "-- +migrate down\nDROP TABLE x;\n-- +migrate up\nCREATE TABLE x (id TEXT);"},
		{"up 段空", "-- +migrate up\n-- +migrate down\nDROP TABLE x;"},
		{"down 段空", "-- +migrate up\nCREATE TABLE x (id TEXT);\n-- +migrate down\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := splitUpDown(tc.raw); err == nil {
				t.Fatal("应该报错")
			}
		})
	}
}

// 外键约束要真的生效（Open 里开了 foreign_keys pragma）
func TestForeignKeysEnforced(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatal(err)
	}

	// 插一个引用不存在乘客的 session
	_, err := d.ExecContext(ctx,
		`INSERT INTO session (id, passenger_id, created_at, last_used_at, expires_at)
		 VALUES ('s1', 'nonexistent-passenger', '2026-01-01', '2026-01-01', '2026-02-01')`)
	if err == nil {
		t.Fatal("外键约束没生效：插入了引用不存在乘客的 session")
	}
}

// credential_ledger 的 CHECK：号要么属于车，要么属于某乘客的拉号记录，不能同时/都不
func TestCredentialOwnershipCheck(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatal(err)
	}

	seed(t, d)

	base := `INSERT INTO credential_ledger
		(id, kiro_rs_credential_id, owner_bus_id, owner_record_passenger_id, current_group,
		 vendor_id, source_pull_round_id, status, pulled_at)
		VALUES (?, ?, ?, ?, 'g', 'kiro91', 'r1', 'alive', '2026-01-01')`

	t.Run("两个 owner 都填 → 拒绝", func(t *testing.T) {
		if _, err := d.ExecContext(ctx, base, "c1", 1, "b1", "p1"); err == nil {
			t.Fatal("同时属于车和拉号记录，应该被 CHECK 拒绝")
		}
	})
	t.Run("两个 owner 都空 → 拒绝", func(t *testing.T) {
		if _, err := d.ExecContext(ctx, base, "c2", 2, nil, nil); err == nil {
			t.Fatal("两个 owner 都空，应该被 CHECK 拒绝")
		}
	})
	t.Run("只填 bus → 通过", func(t *testing.T) {
		if _, err := d.ExecContext(ctx, base, "c3", 3, "b1", nil); err != nil {
			t.Fatalf("只属于车应该允许: %v", err)
		}
	})
	t.Run("只填 record → 通过", func(t *testing.T) {
		if _, err := d.ExecContext(ctx, base, "c4", 4, nil, "p1"); err != nil {
			t.Fatalf("只属于拉号记录应该允许: %v", err)
		}
	})
}

// wallet 的 CHECK：余额和冻结都不能为负（超扣的第一道防线）
func TestWalletNonNegativeCheck(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatal(err)
	}
	seed(t, d)

	if _, err := d.ExecContext(ctx,
		`UPDATE wallet SET balance = -1 WHERE passenger_id = 'p1'`); err == nil {
		t.Fatal("余额置负应该被 CHECK 拒绝")
	}
	if _, err := d.ExecContext(ctx,
		`UPDATE wallet SET reserved = -1 WHERE passenger_id = 'p1'`); err == nil {
		t.Fatal("冻结置负应该被 CHECK 拒绝")
	}
}

// pending_purchase 必须接受 purchasing 状态（09-transactions §2.1 · P0-1）
func TestPendingPurchaseAcceptsPurchasingState(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatal(err)
	}
	seed(t, d)

	ins := `INSERT INTO pending_purchase
		(id, idempotency_record_id, passenger_id, target_group, vendor_id, client_order_id,
		 count_requested, reserved_amount, status, created_at, updated_at)
		VALUES (?, 'i1', 'p1', 'record-p1', 'kiro91', ?, 1, 1000000, ?, '2026-01-01', '2026-01-01')`

	for _, st := range []string{
		"initial", "reserved", "purchasing", "purchased", "imported",
		"completed", "cancelled_reserve", "need_recover_vendor", "need_manual",
	} {
		if _, err := d.ExecContext(ctx, ins, "pp-"+st, "order-"+st, st); err != nil {
			t.Errorf("状态 %q 应该被接受: %v", st, err)
		}
	}

	if _, err := d.ExecContext(ctx, ins, "pp-bogus", "order-bogus", "not_a_state"); err == nil {
		t.Fatal("非法状态应该被 CHECK 拒绝")
	}
}

// pending_handoff 的状态名必须是三段式那套（不是被作废的一次性版本）
func TestPendingHandoffStates(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatal(err)
	}
	seed(t, d)

	ins := `INSERT INTO pending_handoff
		(id, passenger_id, download_token, credential_ids_json, status, expires_at, created_at, updated_at)
		VALUES (?, 'p1', ?, '[]', ?, '2026-01-01', '2026-01-01', '2026-01-01')`

	for _, st := range []string{
		"token_issued", "fulfilled", "confirmed", "completed",
		"expired", "expired_after_fulfill", "need_manual",
	} {
		if _, err := d.ExecContext(ctx, ins, "ph-"+st, "tok-"+st, st); err != nil {
			t.Errorf("状态 %q 应该被接受: %v", st, err)
		}
	}

	// 被作废的一次性版本状态名不该还能用
	for _, st := range []string{"plaintext_captured", "returned_to_user", "housepool_deleted"} {
		if _, err := d.ExecContext(ctx, ins, "ph-old-"+st, "tok-old-"+st, st); err == nil {
			t.Errorf("已作废的状态名 %q 不该被接受（那是 P0-3 漏洞的一次性版本）", st)
		}
	}
}

// bus_member 的 share_pct 范围（2a 分摊要用）
func TestBusMemberSharePctRange(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatal(err)
	}
	seed(t, d)

	ins := `INSERT INTO bus_member (bus_id, passenger_id, role, joined_at, share_pct)
	        VALUES ('b1', 'p1', 'owner', ?, ?)`
	if _, err := d.ExecContext(ctx, ins, "2026-01-02", 101); err == nil {
		t.Fatal("share_pct = 101 应该被拒绝")
	}
	if _, err := d.ExecContext(ctx, ins, "2026-01-03", -1); err == nil {
		t.Fatal("share_pct = -1 应该被拒绝")
	}
	if _, err := d.ExecContext(ctx, ins, "2026-01-04", 100); err != nil {
		t.Fatalf("share_pct = 100 应该允许: %v", err)
	}
}

// seed 插最小的一套关联数据，供各 CHECK 测试复用
func seed(t *testing.T, d *DB) {
	t.Helper()
	ctx := context.Background()
	stmts := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		  VALUES ('p1','u1','u1@example.com','x','2026-01-01','2026-01-01')`, nil},
		{`INSERT INTO wallet (passenger_id, balance, reserved, updated_at)
		  VALUES ('p1', 1000000, 0, '2026-01-01')`, nil},
		{`INSERT INTO bus (id, name, kind, creator_passenger_id, created_at)
		  VALUES ('b1','车','single','p1','2026-01-01')`, nil},
		{`INSERT INTO pull_round
		  (id, vendor_id, client_order_id, count_requested, count_purchased,
		   key_cost_total, service_fee_total, participants_split_json, status, created_at)
		  VALUES ('r1','kiro91','ord1',1,1,20000000,1000000,'{}','completed','2026-01-01')`, nil},
		{`INSERT INTO idempotency_record
		  (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		  VALUES ('i1','p1','POST','/api/me/pull','k1','fp1','2026-01-01')`, nil},
	}
	for _, s := range stmts {
		if _, err := d.ExecContext(ctx, s.q, s.args...); err != nil {
			t.Fatalf("seed 失败: %v\nSQL: %s", err, s.q)
		}
	}
}
