package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// TestMigration040_BusColumnsAreNotNull · migration 040 撤 039 的 nullable ·
// bus.auto_refill_enabled / refill_watermark 回到 NOT NULL DEFAULT 0
// (decisions §13.5 · issues-log I-05)。
//
// **不测 down-then-up** —— migration 046 的 down 有缺陷(重新 up 时 duplicate column)·
// 走"up 到最新 → 检查 schema" 更稳。049/050 的 down 也未验证过，别踩坑。
func TestMigration040_BusColumnsAreNotNull(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatalf("up: %v", err)
	}

	var sqlDef sql.NullString
	if err := d.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='bus'`).
		Scan(&sqlDef); err != nil {
		t.Fatalf("读 bus schema: %v", err)
	}
	schema := sqlDef.String

	// 关键断言 · migration 040 后 · 两字段 NOT NULL DEFAULT 0
	if !containsBoth(schema, "auto_refill_enabled", "NOT NULL DEFAULT 0") {
		t.Errorf("bus.auto_refill_enabled 应 NOT NULL DEFAULT 0 · schema:\n%s", schema)
	}
	if !containsBoth(schema, "refill_watermark", "NOT NULL DEFAULT 0") {
		t.Errorf("bus.refill_watermark 应 NOT NULL DEFAULT 0 · schema:\n%s", schema)
	}
	// refill_min_count 保留 nullable (nil = 按 gap 补差额)
	if strings.Contains(schema, "refill_min_count       INTEGER NOT NULL") {
		t.Errorf("bus.refill_min_count 应 nullable · schema:\n%s", schema)
	}
}

// TestMigration040_BusNotNullBlocksNullInsert · migration 040 后 · 显式 NULL
// 插入应被 NOT NULL 拒绝 —— 保证运行时代码不能塞 NULL 值(迁移铁律靠 schema 层护)
func TestMigration040_BusNotNullBlocksNullInsert(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatalf("up: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES ('p1', 'u1', 'u1@example.com', 'x', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatalf("建乘客: %v", err)
	}
	// 显式 NULL 应被拒
	_, err := d.ExecContext(ctx, `
		INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at,
		                 auto_refill_enabled, refill_watermark)
		VALUES ('b-null', 'nullbus', 'single', 'p1', 'active', '2026-01-01T00:00:00Z', NULL, NULL)`)
	if err == nil {
		t.Fatal("040 后应能 NOT NULL 拒 NULL 插入 · 但没报错")
	}
	if !strings.Contains(err.Error(), "NOT NULL") {
		t.Errorf("应报 NOT NULL 错 · 得 %v", err)
	}

	// 显式 0 / 5 应能过
	if _, err := d.ExecContext(ctx, `
		INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at,
		                 auto_refill_enabled, refill_watermark)
		VALUES ('b-ok', 'okbus', 'single', 'p1', 'active', '2026-01-01T00:00:00Z', 0, 5)`); err != nil {
		t.Errorf("显式 0/5 应能过 · 得 %v", err)
	}
}

// TestMigration040_AddsCrossFleetGuardrails · passenger_strategy_default 加了
// 三个跨车调度护栏字段 · 040 后老乘客(NULL) + 新乘客(可显式)都能读写。
func TestMigration040_AddsCrossFleetGuardrails(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatalf("up: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES ('p1', 'u1', 'u1@example.com', 'x', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatalf("建乘客: %v", err)
	}
	// 老乘客 · 只填必填字段 · 3 护栏默认 NULL
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger_strategy_default
		  (passenger_id, per_round_count, default_zone, updated_at)
		VALUES ('p1', 1, 'auto', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("写全局: %v", err)
	}
	var budget, minReserve sql.NullInt64
	var allowlist sql.NullString
	if err := d.QueryRowContext(ctx, `
		SELECT auto_refill_daily_budget, auto_refill_min_wallet_reserve, auto_refill_vendor_allowlist
		  FROM passenger_strategy_default WHERE passenger_id = 'p1'`).
		Scan(&budget, &minReserve, &allowlist); err != nil {
		t.Fatalf("读 3 护栏: %v", err)
	}
	if budget.Valid || minReserve.Valid || allowlist.Valid {
		t.Errorf("老乘客 3 护栏应全 NULL · 得 budget=%v minReserve=%v allowlist=%v",
			budget, minReserve, allowlist)
	}

	// 显式写值 · 读回应一致
	if _, err := d.ExecContext(ctx, `
		UPDATE passenger_strategy_default
		   SET auto_refill_daily_budget = 100000000,
		       auto_refill_min_wallet_reserve = 50000000,
		       auto_refill_vendor_allowlist = '["kiro91","kirodrop"]'
		 WHERE passenger_id = 'p1'`); err != nil {
		t.Fatalf("update 3 护栏: %v", err)
	}
	if err := d.QueryRowContext(ctx, `
		SELECT auto_refill_daily_budget, auto_refill_min_wallet_reserve, auto_refill_vendor_allowlist
		  FROM passenger_strategy_default WHERE passenger_id = 'p1'`).
		Scan(&budget, &minReserve, &allowlist); err != nil {
		t.Fatalf("读回: %v", err)
	}
	if budget.Int64 != 100000000 || minReserve.Int64 != 50000000 ||
		allowlist.String != `["kiro91","kirodrop"]` {
		t.Errorf("写回读不一致 · budget=%v minReserve=%v allowlist=%v",
			budget, minReserve, allowlist)
	}
}

func containsBoth(schema, col, constraint string) bool {
	// 找 `col 至少空 1 位 类型 至少空 1 位 constraint`
	// 简化:同一行(schema 里换行是可选)· 用宽松包含
	if !strings.Contains(schema, col) {
		return false
	}
	// 找到 col 后 · 后面 200 字符内应有 constraint
	idx := strings.Index(schema, col)
	end := idx + 200
	if end > len(schema) {
		end = len(schema)
	}
	return strings.Contains(schema[idx:end], constraint)
}
