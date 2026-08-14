package api

// **v1-E · 2026-08-15**
//
// 单元测 loadAdminOverview + 格式化辅助函数。不测 handler HTTP 层
// （那走 requireAdmin + json 序列化 · pattern 跟 handleDataHealth 一致 · 复用它的测试基础）。

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func setupAdminDB(t *testing.T) *sql.DB {
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
	return d.DB
}

// 空库返空 vendors 列表 · 不炸
func TestAdminOverview_EmptyDB(t *testing.T) {
	sqldb := setupAdminDB(t)
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	rows, err := loadAdminOverview(context.Background(), sqldb, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("空库应返空 vendors · 得 %d", len(rows))
	}
}

// 一家 vendor 有探针 + dispatch · 组装完整
func TestAdminOverview_SingleVendor(t *testing.T) {
	sqldb := setupAdminDB(t)
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)

	// 探针最新一条
	_, err := sqldb.Exec(`
		INSERT INTO vendor_probe
		  (vendor_id, probed_at, alive, stock_total, stock_by_region,
		   ps_generating, ps_keys_active, ps_keys_stock, ps_keys_dead)
		VALUES ('kirotest', ?, 1, 5, '[]', 1, 100, 5, 3)`,
		now.Add(-2*time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	// 今日 dispatch 2 条
	for i, at := range []time.Time{now.Add(-3 * time.Hour), now.Add(-30 * time.Minute)} {
		_, err := sqldb.Exec(`
			INSERT INTO vendor_dispatch
			  (vendor_id, source, dispatch_key, dispatched_at, count, alive, status, fetched_at)
			VALUES ('kirotest', 'vendor_self', ?, ?, ?, ?, 'running', ?)`,
			"key-"+strconv.Itoa(i), at.Format(time.RFC3339), 10+i, 10+i, now.Format(time.RFC3339))
		if err != nil {
			t.Fatal(err)
		}
	}

	rows, err := loadAdminOverview(context.Background(), sqldb, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应有 1 家 vendor · 得 %d", len(rows))
	}
	r := rows[0]
	if r.VendorID != "kirotest" {
		t.Errorf("VendorID = %q", r.VendorID)
	}
	if !r.Alive {
		t.Error("alive 应 true")
	}
	if r.LastProbeAgo != "2m" {
		t.Errorf("LastProbeAgo = %q · want 2m", r.LastProbeAgo)
	}
	if r.FleetGenerating == nil || !*r.FleetGenerating {
		t.Errorf("FleetGenerating 应 true")
	}
	if r.FleetKeysActive == nil || *r.FleetKeysActive != 100 {
		t.Errorf("FleetKeysActive")
	}
	if r.DispatchesToday != 2 {
		t.Errorf("DispatchesToday = %d · want 2", r.DispatchesToday)
	}
	// 10 + 11 = 21
	if r.KeysDispatchedToday != 21 {
		t.Errorf("KeysDispatchedToday = %d · want 21", r.KeysDispatchedToday)
	}
	if r.LastDispatchAgo != "30m" {
		t.Errorf("LastDispatchAgo = %q · want 30m", r.LastDispatchAgo)
	}
}

// zone 数据 · 三源优先级：vendor_self > xi8 > xi8_notif
func TestAdminOverview_ZoneSourcePriority(t *testing.T) {
	sqldb := setupAdminDB(t)
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)

	// 探针（至少一条 · vendor 才会被 vendors 列表列出）
	_, err := sqldb.Exec(`
		INSERT INTO vendor_probe (vendor_id, probed_at, alive, stock_total, stock_by_region)
		VALUES ('kirotest', ?, 1, 0, '[]')`,
		now.Add(-1*time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}

	// 同 zone 三源都有值 · 应取 vendor_self
	for i, src := range []string{"xi8_notif", "xi8", "vendor_self"} {
		_, err := sqldb.Exec(`
			INSERT INTO vendor_probe_zone
			  (vendor_id, probed_at, zone, source, available, our_unit_credits)
			VALUES ('kirotest', ?, 'us', ?, ?, ?)`,
			now.Add(-time.Duration(i)*time.Minute).Format(time.RFC3339Nano),
			src, 10*(i+1), int64(100+i)*1_000_000)
		if err != nil {
			t.Fatal(err)
		}
	}

	rows, _ := loadAdminOverview(context.Background(), sqldb, now)
	if len(rows) != 1 || len(rows[0].Zones) != 1 {
		t.Fatalf("应有 1 家 1 zone · 得 %+v", rows)
	}
	z := rows[0].Zones[0]
	if z.Source != "vendor_self" {
		t.Errorf("应优先 vendor_self · 得 %q", z.Source)
	}
	if !strings.HasSuffix(z.UnitDisplay, "积分") {
		t.Errorf("UnitDisplay = %q · vendor 非 CNY 家应显示 积分", z.UnitDisplay)
	}
}

// vendor CNY 家 · 显示 CNY 不是积分
func TestMicroDisplay_CNYVendor(t *testing.T) {
	if got := microDisplay(49_980_000, "CNY"); got != "49.98 CNY" {
		t.Errorf("49_980_000 microunit CNY = %q · want 49.98 CNY", got)
	}
	if got := microDisplay(100_000_000, "credit"); got != "100 积分" {
		t.Errorf("100_000_000 microunit credit = %q · want 100 积分", got)
	}
}

// agoStr 分档
func TestAgoStr(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"},
	}
	for _, c := range cases {
		if got := agoStr(c.d); got != c.want {
			t.Errorf("agoStr(%v) = %q · want %q", c.d, got, c.want)
		}
	}
}
