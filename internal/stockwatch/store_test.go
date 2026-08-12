package stockwatch

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

// 装配测试 DB · 应用所有迁移
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := d.MigrateUp(context.Background(), ""); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// seedIntent · 建最小 pull_intent 依赖 · stock_watcher 有 FK 需要它先在
func seedIntent(t *testing.T, database *db.DB, intentID string) {
	t.Helper()
	ctx := context.Background()
	// passenger + wallet 只塞一次 · 忽略冲突（同一测试多次 seed 时）
	_, _ = database.ExecContext(ctx,
		`INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		 VALUES ('p1','u1','u1@example.com','x','2026-01-01','2026-01-01')`)
	_, _ = database.ExecContext(ctx,
		`INSERT INTO wallet (passenger_id, balance, reserved, updated_at)
		 VALUES ('p1', 1000000, 0, '2026-01-01')`)
	// pull_intent 每 intentID 一条
	if _, err := database.ExecContext(ctx,
		`INSERT INTO pull_intent (id, passenger_id, target, count_requested, status, created_at, updated_at)
		 VALUES (?, 'p1', 'to-record', 1, 'pending', '2026-01-01', '2026-01-01')`,
		intentID); err != nil {
		t.Fatalf("seed intent: %v", err)
	}
}

// mockFirer · 记 fire 调用 · 可注入返回错
type mockFirer struct {
	mu    sync.Mutex
	calls []string
	nextErr error
}

func (m *mockFirer) FireByIntent(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, id)
	return m.nextErr
}

func (m *mockFirer) firedIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

// TestEnqueue_And_Notify_FiresIntent · 端到端：Enqueue → Notify → firer 收到调用 → fulfilled
func TestEnqueue_And_Notify_FiresIntent(t *testing.T) {
	database := openTestDB(t)
	seedIntent(t, database, "int-1")

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})

	ctx := context.Background()
	if err := w.Enqueue(ctx, EnqueueParams{
		IntentID: "int-1", VendorID: "kiroceo", Count: 1,
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// 库里 status=watching
	var status string
	_ = database.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE intent_id = ?`, "int-1").Scan(&status)
	if status != "watching" {
		t.Fatalf("挂单后应 watching · 得 %q", status)
	}

	// 通知 restock
	if err := w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Count: 5, Source: "test"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	// firer 被调用一次 · intent_id 对
	fired := firer.firedIDs()
	if len(fired) != 1 || fired[0] != "int-1" {
		t.Fatalf("firer 应被调用 int-1 一次 · 得 %v", fired)
	}

	// status → fulfilled
	_ = database.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE intent_id = ?`, "int-1").Scan(&status)
	if status != "fulfilled" {
		t.Fatalf("fire 成功后应 fulfilled · 得 %q", status)
	}
}

// TestNotify_WrongVendor_NoFire · Notify 其他 vendor 不应触发
func TestNotify_WrongVendor_NoFire(t *testing.T) {
	database := openTestDB(t)
	seedIntent(t, database, "int-2")

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()
	_ = w.Enqueue(ctx, EnqueueParams{IntentID: "int-2", VendorID: "kiroceo", Count: 1})

	// 通知另一家
	_ = w.Notify(ctx, NotifyParams{VendorID: "kirooo", Count: 5, Source: "test"})
	if len(firer.firedIDs()) != 0 {
		t.Fatal("不同 vendor 不应 fire")
	}
}

// TestNotify_StillNoStock_RewindsToWatching · fire 后又缺货 · 回退等下次
func TestNotify_StillNoStock_RewindsToWatching(t *testing.T) {
	database := openTestDB(t)
	seedIntent(t, database, "int-3")

	firer := &mockFirer{nextErr: ErrStillNoStock}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()
	_ = w.Enqueue(ctx, EnqueueParams{IntentID: "int-3", VendorID: "kiroceo", Count: 1})

	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Count: 5, Source: "test"})

	var status string
	_ = database.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE intent_id = ?`, "int-3").Scan(&status)
	if status != "watching" {
		t.Fatalf("ErrStillNoStock 后应回退 watching · 得 %q", status)
	}

	// fire_count 应该 = 1
	var fc int
	_ = database.QueryRowContext(ctx,
		`SELECT fire_count FROM stock_watcher WHERE intent_id = ?`, "int-3").Scan(&fc)
	if fc != 1 {
		t.Fatalf("fire_count 应 1 · 得 %d", fc)
	}
}

// TestNotify_HardFail_MarksExpired · fire 返其他错 · 标 expired
func TestNotify_HardFail_MarksExpired(t *testing.T) {
	database := openTestDB(t)
	seedIntent(t, database, "int-4")

	firer := &mockFirer{nextErr: errors.New("单价涨超上限")}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()
	_ = w.Enqueue(ctx, EnqueueParams{IntentID: "int-4", VendorID: "kiroceo", Count: 1})
	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Count: 5, Source: "test"})

	var status, reason sql.NullString
	_ = database.QueryRowContext(ctx,
		`SELECT status, fired_reason FROM stock_watcher WHERE intent_id = ?`, "int-4").Scan(&status, &reason)
	if status.String != "expired" {
		t.Fatalf("硬失败应 expired · 得 %q", status.String)
	}
	if reason.String == "" {
		t.Fatal("expired 时 fired_reason 应有值 · 记具体错")
	}
}

// TestSweep_ExpiresOldWatching · TTL 到期扫成 expired
func TestSweep_ExpiresOldWatching(t *testing.T) {
	database := openTestDB(t)
	seedIntent(t, database, "int-5")

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()

	// 挂一个 TTL 只有 1ms 的
	_ = w.Enqueue(ctx, EnqueueParams{
		IntentID: "int-5", VendorID: "kiroceo", Count: 1,
		TTL: 1 * time.Millisecond,
	})
	time.Sleep(10 * time.Millisecond)

	res := w.Sweep(ctx)
	if res.Expired != 1 {
		t.Fatalf("应扫出 1 条过期 · 得 %d", res.Expired)
	}

	var status string
	_ = database.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE intent_id = ?`, "int-5").Scan(&status)
	if status != "expired" {
		t.Fatalf("Sweep 后应 expired · 得 %q", status)
	}
}

// TestEnqueue_Idempotent · 同 intent_id 重挂 · 覆盖参数保留 fire_count
func TestEnqueue_Idempotent(t *testing.T) {
	database := openTestDB(t)
	seedIntent(t, database, "int-6")

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()

	_ = w.Enqueue(ctx, EnqueueParams{IntentID: "int-6", VendorID: "kiroceo", Count: 1})
	// 手动模拟已经 fire 过一次
	_, _ = database.ExecContext(ctx,
		`UPDATE stock_watcher SET fire_count = 3 WHERE intent_id = ?`, "int-6")

	// 重挂 · 换参数
	_ = w.Enqueue(ctx, EnqueueParams{IntentID: "int-6", VendorID: "kirooo", Count: 5})

	var vendor string
	var fc int
	var status string
	_ = database.QueryRowContext(ctx,
		`SELECT vendor_id, fire_count, status FROM stock_watcher WHERE intent_id = ?`, "int-6").
		Scan(&vendor, &fc, &status)
	if vendor != "kirooo" || status != "watching" {
		t.Fatalf("重挂应覆盖 vendor + 复位 watching · 得 %s / %s", vendor, status)
	}
	// fire_count 应保留（防 spam）
	if fc != 3 {
		t.Errorf("重挂应保留 fire_count · 得 %d 期 3", fc)
	}
}
