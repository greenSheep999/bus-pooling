package webhookin

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func healthDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "health.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return d.DB
}

// putDispatch 造一条 dispatch 行 · key 前缀决定它算不算"独立信源"
func putDispatch(t *testing.T, dbc *sql.DB, vendor, key, source string, at time.Time, count int) {
	t.Helper()
	iso := at.UTC().Format(time.RFC3339)
	_, err := dbc.Exec(`
		INSERT INTO vendor_dispatch (vendor_id, dispatch_key, source, dispatched_at, count, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, vendor, key, source, iso, count, iso)
	if err != nil {
		t.Fatalf("插 dispatch: %v", err)
	}
}

func putWebhookEvent(t *testing.T, dbc *sql.DB, vendor, eventID string, at time.Time) {
	t.Helper()
	_, err := dbc.Exec(`
		INSERT INTO inbound_webhook_event (vendor_id, event_id, event_type, received_at)
		VALUES (?, ?, 'new_keys_available', ?)
	`, vendor, eventID, at.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("插 webhook event: %v", err)
	}
}

func newHealth(dbc *sql.DB) *HealthChecker {
	return NewHealthChecker(HealthConfig{DB: dbc, Logger: slog.Default()})
}

// 核心场景：探针一直看到上游开号 · webhook 一条不来 → 必须报
func TestCheck_ProbeSeesRestockButWebhookSilent(t *testing.T) {
	dbc := healthDB(t)
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		at := now.Add(-time.Duration(i+1) * time.Hour)
		putDispatch(t, dbc, "vendorA", "delta-us-"+at.Format("20060102T150405Z"),
			"vendor_self", at, 10)
	}
	// 通道在窗口外还活过 · 之后死了
	putWebhookEvent(t, dbc, "vendorA", "old-evt", now.Add(-48*time.Hour))

	silent, err := newHealth(dbc).Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(silent) != 1 {
		t.Fatalf("应报 1 家静默 · 得 %d 家 %+v", len(silent), silent)
	}
	if silent[0].VendorID != "vendorA" || silent[0].Batches != 3 || silent[0].Keys != 30 {
		t.Errorf("统计错 · %+v", silent[0])
	}
	if silent[0].LastWebhookAt.IsZero() {
		t.Error("应带上最后一次 webhook 时刻 · 便于判断静默多久")
	}
}

// 反向哨兵：通道活着就绝不能报 —— 误报会让真报警被无视
func TestCheck_WebhookAliveNotReported(t *testing.T) {
	dbc := healthDB(t)
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		at := now.Add(-time.Duration(i+1) * time.Hour)
		putDispatch(t, dbc, "vendorA", "delta-us-"+at.Format("20060102T150405Z"),
			"vendor_self", at, 10)
	}
	putWebhookEvent(t, dbc, "vendorA", "fresh-evt", now.Add(-30*time.Minute))

	silent, err := newHealth(dbc).Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(silent) != 0 {
		t.Fatalf("窗口内收到过 webhook 不该报 · 得 %+v", silent)
	}
}

// 上游本来就没开号 · 不是静默 · 不报（这类"没数据"最容易做成噪音）
func TestCheck_NoRestockEvidenceNoAlarm(t *testing.T) {
	dbc := healthDB(t)
	silent, err := newHealth(dbc).Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(silent) != 0 {
		t.Fatalf("无开号证据不该报 · 得 %+v", silent)
	}
}

// 单批不报 —— 退款回流也会让库存回升 · 上游本就不推 webhook
func TestCheck_SingleBatchBelowThreshold(t *testing.T) {
	dbc := healthDB(t)
	now := time.Now().UTC()
	putDispatch(t, dbc, "vendorA", "delta-us-x", "vendor_self", now.Add(-time.Hour), 5)

	silent, err := newHealth(dbc).Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(silent) != 0 {
		t.Fatalf("单批不该报 · 得 %+v", silent)
	}
}

// webhook 自己落的 dispatch 行不能当"独立信源" —— 否则是循环论证 · 永远报不出来
func TestCheck_WebhookOriginRowsAreNotEvidence(t *testing.T) {
	dbc := healthDB(t)
	now := time.Now().UTC()

	// vendor 原生订单号做 key = webhook / fleet 落的行 · 不算独立信源
	for i := 0; i < 3; i++ {
		at := now.Add(-time.Duration(i+1) * time.Hour)
		putDispatch(t, dbc, "vendorA", "poid-"+at.Format("150405"), "vendor_self", at, 10)
	}

	silent, err := newHealth(dbc).Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(silent) != 0 {
		t.Fatalf("非独立信源不该触发报警 · 得 %+v", silent)
	}
}

// 聚合站的行也算独立信源（我方探针漏采时它是唯一证据）
func TestCheck_Xi8RowsCountAsEvidence(t *testing.T) {
	dbc := healthDB(t)
	now := time.Now().UTC()

	for i := 0; i < 2; i++ {
		at := now.Add(-time.Duration(i+1) * time.Hour)
		putDispatch(t, dbc, "vendorB", "xi8-log-"+at.Format("150405"), "xi8", at, 4)
	}

	silent, err := newHealth(dbc).Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(silent) != 1 || silent[0].VendorID != "vendorB" {
		t.Fatalf("聚合站证据应触发 · 得 %+v", silent)
	}
	// 从未收到过 webhook · 时刻留零值
	if !silent[0].LastWebhookAt.IsZero() {
		t.Errorf("从未收到应是零值 · 得 %v", silent[0].LastWebhookAt)
	}
}
