package vendorview

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func rollupDB(t *testing.T) *ProbeStore {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "rollup.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return NewProbeStore(d.DB)
}

func insertProbe(t *testing.T, s *ProbeStore, vendor string, at time.Time, alive bool, stock int) {
	t.Helper()
	a := 0
	if alive {
		a = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO vendor_probe (vendor_id, probed_at, alive, stock_total, warranty_minutes)
		VALUES (?, ?, ?, ?, 10)
	`, vendor, at.UTC().Format(time.RFC3339), a, stock)
	if err != nil {
		t.Fatalf("插 probe: %v", err)
	}
}

// 全天在线有货 → uptime=1 · 无事故
func TestRollupDay_HealthyDay(t *testing.T) {
	s := rollupDB(t)
	base := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		insertProbe(t, s, "vA", base.Add(time.Duration(i)*time.Minute), true, 30)
	}

	n, err := s.RollupDay(context.Background(), "2026-08-12")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("应写 1 行 · 得 %d", n)
	}

	inc, err := s.Incidents7dFrom(context.Background(), "vA", base.AddDate(0, 0, 3))
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 0 {
		t.Errorf("健康日不该是事故 · 得 %v", inc)
	}
}

// uptime < 95% → 事故日 · Incidents7d 能读到
func TestRollupDay_LowUptimeIsIncident(t *testing.T) {
	s := rollupDB(t)
	base := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	// 10 条 · 只 5 条 alive → uptime 50%
	for i := 0; i < 5; i++ {
		insertProbe(t, s, "vB", base.Add(time.Duration(i)*time.Minute), true, 30)
	}
	for i := 5; i < 10; i++ {
		insertProbe(t, s, "vB", base.Add(time.Duration(i)*time.Minute), false, 0)
	}

	if _, err := s.RollupDay(context.Background(), "2026-08-12"); err != nil {
		t.Fatal(err)
	}
	inc, err := s.Incidents7dFrom(context.Background(), "vB", base.AddDate(0, 0, 2))
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 1 || inc[0] != "2026-08-12" {
		t.Errorf("低 uptime 应记事故 · 得 %v", inc)
	}
}

// 长时间缺货（在线但 stock=0）**不是**事故 —— 这个市场缺货是常态（2026-08-14 修正）。
// stockout_minutes 照常记录 · 但不据它判事故 · 否则天天全红成噪音。
func TestRollupDay_StockoutAloneIsNotIncident(t *testing.T) {
	s := rollupDB(t)
	base := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	// 跨度 2 小时全程在线但 stock=0
	for i := 0; i <= 12; i++ {
		insertProbe(t, s, "vC", base.Add(time.Duration(i*10)*time.Minute), true, 0)
	}

	if _, err := s.RollupDay(context.Background(), "2026-08-12"); err != nil {
		t.Fatal(err)
	}
	inc, err := s.Incidents7dFrom(context.Background(), "vC", base.AddDate(0, 0, 2))
	if err != nil {
		t.Fatal(err)
	}
	if len(inc) != 0 {
		t.Errorf("在线缺货不该记事故 · 得 %v", inc)
	}
	// 但 stockout_minutes 要记下来（趋势数据 · 全程缺货 ≈ 120min）
	var somin int
	if err := s.db.QueryRow(
		`SELECT stockout_minutes FROM vendor_daily WHERE vendor_id='vC'`).Scan(&somin); err != nil {
		t.Fatal(err)
	}
	if somin < 100 {
		t.Errorf("stockout_minutes 应记录 · 得 %d", somin)
	}
}

// 幂等：同一天滚两遍 · 结果不变 · 不产生第二行
func TestRollupDay_Idempotent(t *testing.T) {
	s := rollupDB(t)
	base := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		insertProbe(t, s, "vD", base.Add(time.Duration(i)*time.Minute), true, 5)
	}

	n1, _ := s.RollupDay(context.Background(), "2026-08-12")
	n2, _ := s.RollupDay(context.Background(), "2026-08-12")
	if n1 != 1 || n2 != 1 {
		t.Fatalf("两次都应写 1 行（upsert）· 得 %d / %d", n1, n2)
	}
	var cnt int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM vendor_daily WHERE vendor_id='vD'`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Errorf("幂等应只 1 行 · 得 %d", cnt)
	}
}

// 多 vendor 同日 · 各自独立聚合
func TestRollupDay_MultiVendor(t *testing.T) {
	s := rollupDB(t)
	base := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		insertProbe(t, s, "vX", base.Add(time.Duration(i)*time.Minute), true, 20)
		insertProbe(t, s, "vY", base.Add(time.Duration(i)*time.Minute), i < 3, 0)
	}
	n, err := s.RollupDay(context.Background(), "2026-08-12")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("两家各一行 · 得 %d", n)
	}
}

// DistinctProbeDates 只返窗口内出现过的日期
func TestDistinctProbeDates(t *testing.T) {
	s := rollupDB(t)
	now := time.Now().UTC()
	insertProbe(t, s, "vZ", now, true, 1)
	insertProbe(t, s, "vZ", now.AddDate(0, 0, -1), true, 1)
	insertProbe(t, s, "vZ", now.AddDate(0, 0, -40), true, 1) // 窗口外

	dates, err := s.DistinctProbeDates(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(dates) != 2 {
		t.Errorf("窗口内应 2 天 · 得 %v", dates)
	}
}
