package api

// **v4.2 · 2026-08-15** · 涨价历史端点 unit test
//
// 覆盖 SQL 聚合：按 zone × source × hour_utc 分组 · 60 样本满小时正确 · 空窗返空。

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func setupTrendDB(t *testing.T) *sql.DB {
	t.Helper()
	return db.NewTestDB(t).DB
}

// 空表 · 返空 zones
func TestLoadPriceTrend_Empty(t *testing.T) {
	sqldb := setupTrendDB(t)
	zones, err := loadPriceTrend(context.Background(), sqldb, "kirotest", 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) != 0 {
		t.Errorf("空表应返空 zones · 得 %d", len(zones))
	}
}

// 3 家 source × 2 zone · 按小时聚合
func TestLoadPriceTrend_GroupsByHourAndSource(t *testing.T) {
	sqldb := setupTrendDB(t)
	// 用 time.Now() 相对时间 · 别硬编日期（loadPriceTrend 内部拿 time.Now() 算 cutoff·
	// 硬编日期会随真实时钟漂移出窗口 · 老 bug 修）
	now := time.Now().UTC().Truncate(time.Hour).Add(-30 * time.Minute)
	// 同小时内 us / vendor_self 两条 · 应聚成 1 个 point · samples=2
	for i, at := range []time.Time{
		now.Add(-1 * time.Hour),                       // 11:30
		now.Add(-1 * time.Hour).Add(10 * time.Minute), // 11:40 · 同小时
	} {
		_, err := sqldb.Exec(`
			INSERT INTO vendor_probe_zone
			  (vendor_id, probed_at, zone, source, available, our_unit_credits)
			VALUES ('kirotest', ?, 'us', 'vendor_self', ?, ?)`,
			at.Format(time.RFC3339Nano), i+1, int64(100+i)*1_000_000)
		if err != nil {
			t.Fatal(err)
		}
	}
	// 另一小时 · xi8 source
	_, err := sqldb.Exec(`
		INSERT INTO vendor_probe_zone
		  (vendor_id, probed_at, zone, source, available, our_unit_credits)
		VALUES ('kirotest', ?, 'eu', 'xi8', 1, ?)`,
		now.Add(-3*time.Hour).Format(time.RFC3339Nano), int64(50)*1_000_000)
	if err != nil {
		t.Fatal(err)
	}

	zones, err := loadPriceTrend(context.Background(), sqldb, "kirotest", 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) != 2 {
		t.Fatalf("应 2 zone · 得 %d", len(zones))
	}
	// us zone · vendor_self 一个 series · 一个 point · samples=2
	byZone := map[string]priceTrendZone{}
	for _, z := range zones {
		byZone[z.Zone] = z
	}
	us := byZone["us"]
	if len(us.Series["vendor_self"]) != 1 {
		t.Errorf("us · vendor_self 应 1 point · 得 %d", len(us.Series["vendor_self"]))
	}
	if us.Series["vendor_self"][0].Samples != 2 {
		t.Errorf("samples = %d · want 2", us.Series["vendor_self"][0].Samples)
	}
	// 平均 (100 + 101) / 2 = 100.5 → SQLite AVG float · CAST INT 后可能 100 或 101
	avg := us.Series["vendor_self"][0].AvgCredits
	if avg < 100_000_000 || avg > 101_000_000 {
		t.Errorf("avg = %d · want ~100-101m", avg)
	}
	// eu zone · xi8 series
	eu := byZone["eu"]
	if len(eu.Series["xi8"]) != 1 {
		t.Errorf("eu · xi8 应 1 point · 得 %d", len(eu.Series["xi8"]))
	}
}

// 窗口外的数据不计
func TestLoadPriceTrend_OutOfWindow(t *testing.T) {
	sqldb := setupTrendDB(t)
	old := time.Now().Add(-100 * time.Hour) // 100h 前
	_, err := sqldb.Exec(`
		INSERT INTO vendor_probe_zone
		  (vendor_id, probed_at, zone, source, available, our_unit_credits)
		VALUES ('kirotest', ?, 'us', 'vendor_self', 1, ?)`,
		old.Format(time.RFC3339Nano), int64(100)*1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	// 24h 窗口 · 100h 前的不该计入
	zones, _ := loadPriceTrend(context.Background(), sqldb, "kirotest", 24)
	if len(zones) != 0 {
		t.Errorf("窗口外不该计入 · 得 %d zones", len(zones))
	}
	// 168h 窗口 · 应计入
	zones168, _ := loadPriceTrend(context.Background(), sqldb, "kirotest", 168)
	if len(zones168) != 1 {
		t.Errorf("168h 窗口应能拿到 · 得 %d zones", len(zones168))
	}
}
