package db

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigration039_PreservesBusAutoRefillValues · 迁移保行为铁律(15-scheduling §4.3.2b)：
//
// 老车的 auto_refill_enabled / refill_watermark 值在 039 前后**必须一模一样** ·
// 不能借 migration 一律转 NULL(那会让老车"跟随全局" · 全局改了老车行为跟着变 ·
// 违反用户预期)。测试路径：
//
//  1. 完整 up 到 039
//  2. 插三辆老车(auto=1 / auto=0 / 显式关) + 各一条 alive credential(过 FK)
//  3. down 1(回滚到 038 · bus 变回 NOT NULL DEFAULT 0)
//  4. 再 up 到 039 · 三行值应完全保留
//  5. 断言:auto_refill_enabled / refill_watermark 值一致
func TestMigration039_PreservesBusAutoRefillValues(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatalf("初始 up: %v", err)
	}

	// 建乘客 · 车 FK 依赖
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES ('p1', 'u1', 'u1@example.com', 'x', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatalf("建乘客: %v", err)
	}

	// 三辆老车 · 分别覆盖三种落库形态
	// b1 · 用户明确开自动补(auto=1 · watermark=5)
	// b2 · 用户明确关自动补(auto=0 · watermark=0)
	// b3 · 显式关 + 高水位(auto=0 · watermark=10) · 也算显式覆盖
	fixtures := []struct {
		id                string
		autoRefillEnabled int
		refillWatermark   int
	}{
		{"b1", 1, 5},
		{"b2", 0, 0},
		{"b3", 0, 10},
	}
	for _, f := range fixtures {
		if _, err := d.ExecContext(ctx, `
			INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at,
			                 auto_refill_enabled, refill_watermark)
			VALUES (?, ?, 'single', 'p1', 'active', '2026-01-01T00:00:00Z', ?, ?)`,
			f.id, "bus-"+f.id, f.autoRefillEnabled, f.refillWatermark); err != nil {
			t.Fatalf("建车 %s: %v", f.id, err)
		}
	}

	// 快照 migration 前值 · 用 map 便于 down/up 后对比
	before := readBusAutoRefill(t, d.DB)

	// 回滚 039(bus 变回 NOT NULL DEFAULT 0) · 再前进 · 数据应完全保留
	if _, err := d.MigrateDown(ctx, "", 1); err != nil {
		t.Fatalf("down 1: %v", err)
	}
	if _, err := d.MigrateUp(ctx, ""); err != nil {
		t.Fatalf("重新 up: %v", err)
	}

	after := readBusAutoRefill(t, d.DB)

	for id, wantVal := range before {
		gotVal, ok := after[id]
		if !ok {
			t.Fatalf("车 %s 在 039 前后消失了", id)
		}
		if !wantVal.equal(gotVal) {
			t.Errorf("车 %s · 039 前后值不一致 · before=%+v after=%+v", id, wantVal, gotVal)
		}
	}

	// 断言:值仍是**非 NULL**(不是被一律转成 NULL)
	for id, v := range after {
		if !v.autoValid || !v.watermarkValid {
			t.Errorf("车 %s · 039 后有字段是 NULL(违反保行为铁律) · %+v", id, v)
		}
	}
}

// TestMigration039_GlobalDefaultChangesDoNotAffectExistingBus · 全局默认改变
// 不影响老车 —— migration 保行为的关键场景(15-scheduling §4.3.2b)：
//
//  1. 建老车(auto=1 / auto=0 各一辆) → migration 039
//  2. 把全局默认 default_auto_refill_enabled 从 0 改成 1
//  3. 老车的 auto_refill_enabled 字段值不变(仍是 1 / 0) · **不 fallback 到全局**
//
// Effective() 层的运行时 fallback(NULL 时读全局)是 1f-C 的事 · 这里只测**存储层**：
// 老车的字段仍是显式值 · 不因全局变化被"隐式重写"。
func TestMigration039_GlobalDefaultChangesDoNotAffectExistingBus(t *testing.T) {
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
	// 老车 · auto=1
	if _, err := d.ExecContext(ctx, `
		INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at,
		                 auto_refill_enabled, refill_watermark)
		VALUES ('b1', 'oldbus', 'single', 'p1', 'active', '2026-01-01T00:00:00Z', 1, 5)`); err != nil {
		t.Fatalf("建车: %v", err)
	}
	// 全局默认改成 auto=1 · watermark=100
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger_strategy_default
		  (passenger_id, per_round_count, default_zone,
		   default_auto_refill_enabled, default_refill_watermark, updated_at)
		VALUES ('p1', 1, 'auto', 1, 100, '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("写全局: %v", err)
	}

	// 老车字段值不变(仍是显式 1 / 5) · 全局的 100 不入侵老车
	got := readBusAutoRefill(t, d.DB)
	v, ok := got["b1"]
	if !ok {
		t.Fatal("车 b1 找不到")
	}
	if !v.autoValid || v.autoRefillEnabled != 1 {
		t.Errorf("老车 auto_refill_enabled = %+v · 全局改变后应仍是显式 1", v)
	}
	if !v.watermarkValid || v.refillWatermark != 5 {
		t.Errorf("老车 refill_watermark = %+v · 全局改变后应仍是显式 5(不 fallback 到全局的 100)", v)
	}
}

// TestMigration039_GlobalDefaultsExistWithZero · 全局补的 3 字段落库正确 ·
// 老乘客(039 前建的行)的 default_auto_refill_enabled / default_refill_watermark
// 默认应为 0 · default_refill_min_count 应为 NULL(§4.3.2c 选项 X 语义).
func TestMigration039_GlobalDefaultsExistWithZero(t *testing.T) {
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
	// 写一条最小字段的 strategy · 3 新字段走默认
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger_strategy_default
		  (passenger_id, per_round_count, default_zone, updated_at)
		VALUES ('p1', 1, 'auto', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatalf("写全局: %v", err)
	}
	var auto, watermark int
	var minCount sql.NullInt64
	if err := d.QueryRowContext(ctx, `
		SELECT default_auto_refill_enabled, default_refill_watermark, default_refill_min_count
		  FROM passenger_strategy_default WHERE passenger_id = 'p1'`).
		Scan(&auto, &watermark, &minCount); err != nil {
		t.Fatalf("读全局: %v", err)
	}
	if auto != 0 || watermark != 0 {
		t.Errorf("全局默认 auto=%d watermark=%d · 应都是 0", auto, watermark)
	}
	if minCount.Valid {
		t.Errorf("全局默认 default_refill_min_count = %v · 应为 NULL", minCount.Int64)
	}
}

// busAutoRefillRow · 迁移保行为快照 · Valid=false 表 NULL
type busAutoRefillRow struct {
	autoRefillEnabled int64
	autoValid         bool
	refillWatermark   int64
	watermarkValid    bool
}

func (a busAutoRefillRow) equal(b busAutoRefillRow) bool {
	return a.autoRefillEnabled == b.autoRefillEnabled && a.autoValid == b.autoValid &&
		a.refillWatermark == b.refillWatermark && a.watermarkValid == b.watermarkValid
}

func readBusAutoRefill(t *testing.T, db *sql.DB) map[string]busAutoRefillRow {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT id, auto_refill_enabled, refill_watermark FROM bus ORDER BY id`)
	if err != nil {
		t.Fatalf("读 bus: %v", err)
	}
	defer rows.Close()
	out := make(map[string]busAutoRefillRow)
	for rows.Next() {
		var id string
		var auto, watermark sql.NullInt64
		if err := rows.Scan(&id, &auto, &watermark); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[id] = busAutoRefillRow{
			autoRefillEnabled: auto.Int64,
			autoValid:         auto.Valid,
			refillWatermark:   watermark.Int64,
			watermarkValid:    watermark.Valid,
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	return out
}
