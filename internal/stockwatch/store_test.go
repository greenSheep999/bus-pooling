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

// seedPassenger · stock_watcher 有 passenger FK · 建一个够所有测试用
func seedPassenger(t *testing.T, database *db.DB) {
	t.Helper()
	ctx := context.Background()
	_, _ = database.ExecContext(ctx,
		`INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		 VALUES ('p1','u1','u1@example.com','x','2026-01-01','2026-01-01')`)
	_, _ = database.ExecContext(ctx,
		`INSERT INTO wallet (passenger_id, balance, reserved, updated_at)
		 VALUES ('p1', 1000000, 0, '2026-01-01')`)
}

// enqueueOne · 测试用的挂单快捷方法 · 返挂单 id
func enqueueOne(t *testing.T, w *Watcher, vendorID, orderID string) string {
	t.Helper()
	id, err := w.Enqueue(context.Background(), EnqueueParams{
		PassengerID:   "p1",
		TargetGroup:   "record-p1",
		VendorID:      vendorID,
		ClientOrderID: orderID,
		Count:         1,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return id
}

// mockFirer · 记 fire 调用 · 可注入返回错
type mockFirer struct {
	mu      sync.Mutex
	calls   []WatcherRow
	nextErr error
}

func (m *mockFirer) FireWatcher(_ context.Context, row WatcherRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, row)
	return m.nextErr
}

func (m *mockFirer) firedIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.calls))
	for _, c := range m.calls {
		out = append(out, c.ID)
	}
	return out
}

func (m *mockFirer) lastRow() (WatcherRow, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return WatcherRow{}, false
	}
	return m.calls[len(m.calls)-1], true
}

// TestEnqueue_And_Notify_Fires · 端到端：Enqueue → Notify → firer 收到调用 → fulfilled
func TestEnqueue_And_Notify_Fires(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()

	id := enqueueOne(t, w, "kiroceo", "order-1")

	// 库里 status=watching
	var status string
	_ = database.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE id = ?`, id).Scan(&status)
	if status != "watching" {
		t.Fatalf("挂单后应 watching · 得 %q", status)
	}

	// 通知 restock
	if err := w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Count: 5, Source: "test"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	// firer 被调用一次 · id 对
	fired := firer.firedIDs()
	if len(fired) != 1 || fired[0] != id {
		t.Fatalf("firer 应被调用 %s 一次 · 得 %v", id, fired)
	}

	// 传给 firer 的上下文完整（自包含 · 不用回查）
	row, ok := firer.lastRow()
	if !ok {
		t.Fatal("firer 应收到 row")
	}
	if row.PassengerID != "p1" || row.TargetGroup != "record-p1" ||
		row.ClientOrderID != "order-1" || row.Count != 1 {
		t.Fatalf("firer 收到的上下文不全: %+v", row)
	}

	// status → fulfilled
	_ = database.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE id = ?`, id).Scan(&status)
	if status != "fulfilled" {
		t.Fatalf("fire 成功后应 fulfilled · 得 %q", status)
	}
}

// TestNotify_WrongVendor_NoFire · Notify 其他 vendor 不应触发
func TestNotify_WrongVendor_NoFire(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()
	enqueueOne(t, w, "kiroceo", "order-2")

	// 通知另一家
	_ = w.Notify(ctx, NotifyParams{VendorID: "kirooo", Count: 5, Source: "test"})
	if len(firer.firedIDs()) != 0 {
		t.Fatal("不同 vendor 不应 fire")
	}
}

// TestNotify_StillNoStock_RewindsToWatching · fire 后又缺货 · 回退等下次
func TestNotify_StillNoStock_RewindsToWatching(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	firer := &mockFirer{nextErr: ErrStillNoStock}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()
	id := enqueueOne(t, w, "kiroceo", "order-3")

	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Count: 5, Source: "test"})

	var status string
	var fc int
	_ = database.QueryRowContext(ctx,
		`SELECT status, fire_count FROM stock_watcher WHERE id = ?`, id).Scan(&status, &fc)
	if status != "watching" {
		t.Fatalf("ErrStillNoStock 后应回退 watching · 得 %q", status)
	}
	if fc != 1 {
		t.Fatalf("fire_count 应 1 · 得 %d", fc)
	}
}

// TestNotify_HardFail_MarksExpired · fire 返其他错 · 标 expired
func TestNotify_HardFail_MarksExpired(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	firer := &mockFirer{nextErr: errors.New("单价涨超上限")}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()
	id := enqueueOne(t, w, "kiroceo", "order-4")
	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Count: 5, Source: "test"})

	var status, reason sql.NullString
	_ = database.QueryRowContext(ctx,
		`SELECT status, fired_reason FROM stock_watcher WHERE id = ?`, id).Scan(&status, &reason)
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
	seedPassenger(t, database)

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()

	// 挂一个 TTL 只有 1ms 的
	id, err := w.Enqueue(ctx, EnqueueParams{
		PassengerID: "p1", TargetGroup: "record-p1",
		VendorID: "kiroceo", ClientOrderID: "order-5", Count: 1,
		TTL: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	res := w.Sweep(ctx)
	if res.Expired != 1 {
		t.Fatalf("应扫出 1 条过期 · 得 %d", res.Expired)
	}

	var status string
	_ = database.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE id = ?`, id).Scan(&status)
	if status != "expired" {
		t.Fatalf("Sweep 后应 expired · 得 %q", status)
	}
}

// TestEnqueue_Idempotent · 同 client_order_id 重挂 · 覆盖参数保留 fire_count
func TestEnqueue_Idempotent(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()})
	ctx := context.Background()

	id := enqueueOne(t, w, "kiroceo", "order-6")
	// 手动模拟已经 fire 过 3 次
	_, _ = database.ExecContext(ctx,
		`UPDATE stock_watcher SET fire_count = 3 WHERE id = ?`, id)

	// 重挂 · 同 vendor + 同 client_order_id · 换 count
	id2, err := w.Enqueue(ctx, EnqueueParams{
		PassengerID: "p1", TargetGroup: "record-p1",
		VendorID: "kiroceo", ClientOrderID: "order-6", Count: 5,
	})
	if err != nil {
		t.Fatalf("重挂: %v", err)
	}
	_ = id2

	// 只有一行（UNIQUE 生效 · 走了 UPDATE 不是 INSERT）
	var n int
	_ = database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM stock_watcher WHERE client_order_id = ?`, "order-6").Scan(&n)
	if n != 1 {
		t.Fatalf("同 client_order_id 应只有一行 · 得 %d", n)
	}

	var cnt, fc int
	var status string
	_ = database.QueryRowContext(ctx,
		`SELECT count, fire_count, status FROM stock_watcher WHERE id = ?`, id).
		Scan(&cnt, &fc, &status)
	if cnt != 5 || status != "watching" {
		t.Fatalf("重挂应覆盖 count + 复位 watching · 得 count=%d status=%s", cnt, status)
	}
	// fire_count 应保留（防 spam）
	if fc != 3 {
		t.Errorf("重挂应保留 fire_count · 得 %d 期 3", fc)
	}
}

// TestExpiredRelease_Flow · expired 挂单带冻结 · janitor 能查到并标释放
func TestExpiredRelease_Flow(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	w := New(Config{DB: database.DB, Firer: &mockFirer{}, Logger: slog.Default()})
	ctx := context.Background()

	id, err := w.Enqueue(ctx, EnqueueParams{
		PassengerID: "p1", TargetGroup: "record-p1",
		VendorID: "kiroceo", ClientOrderID: "order-7", Count: 1,
		ReservedAmount: 20_000_000, // 冻了 20 积分
		TTL:            1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	w.Sweep(ctx)

	// janitor 能查到这条待释放
	pending, err := w.ListExpiredNeedingRelease(ctx, 10)
	if err != nil {
		t.Fatalf("ListExpiredNeedingRelease: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("应查到 1 条待释放 · 得 %d 条", len(pending))
	}
	if pending[0].ReservedAmount != 20_000_000 {
		t.Fatalf("冻结额应带出来 · 得 %d", pending[0].ReservedAmount)
	}

	// 释放后清零 · 再查查不到（幂等 · 不会重复释放）
	if err := w.MarkReleased(ctx, id); err != nil {
		t.Fatalf("MarkReleased: %v", err)
	}
	pending2, _ := w.ListExpiredNeedingRelease(ctx, 10)
	if len(pending2) != 0 {
		t.Fatalf("释放后不应再查到 · 得 %d 条", len(pending2))
	}
}
