package stockwatch

// P0 回归 · Enqueue 参数校验 · 特别是 VendorID 非空
// codex 二次审计:scheduler/deathwatch 桥接层可能传空 VendorID · 会挂不上
// 这条测试守住"stockwatch.Enqueue 要求 VendorID 非空"这个契约

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestEnqueue_RejectsEmptyVendorID(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)
	w := New(Config{DB: database.DB, Logger: slog.Default()})

	_, err := w.Enqueue(context.Background(), EnqueueParams{
		PassengerID:   "p1",
		TargetGroup:   "bus-b1",
		VendorID:      "", // 空 · 应拒
		ClientOrderID: "coid-empty-vendor",
		Count:         1,
	})
	if err == nil {
		t.Fatal("空 VendorID 应返参数非法")
	}
	if !strings.Contains(err.Error(), "参数非法") {
		t.Fatalf("err 消息应说'参数非法' · 得 %v", err)
	}
}

func TestEnqueue_RejectsEmptyClientOrderID(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)
	w := New(Config{DB: database.DB, Logger: slog.Default()})
	_, err := w.Enqueue(context.Background(), EnqueueParams{
		PassengerID: "p1",
		TargetGroup: "bus-b1",
		VendorID:    "vA",
		Count:       1,
	})
	if err == nil {
		t.Fatal("空 ClientOrderID 应返参数非法")
	}
}

func TestEnqueue_RejectsZeroCount(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)
	w := New(Config{DB: database.DB, Logger: slog.Default()})
	_, err := w.Enqueue(context.Background(), EnqueueParams{
		PassengerID:   "p1",
		TargetGroup:   "bus-b1",
		VendorID:      "vA",
		ClientOrderID: "coid-zero",
		Count:         0,
	})
	if err == nil {
		t.Fatal("count=0 应返参数非法")
	}
}

func TestEnqueue_AcceptsValidParams(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)
	w := New(Config{DB: database.DB, Logger: slog.Default()})
	id, err := w.Enqueue(context.Background(), EnqueueParams{
		PassengerID: "p1",
		// 不填 BusID 避免 FK · 用 record group
		TargetGroup:   "record-p1",
		VendorID:      "vA",
		ClientOrderID: "coid-ok",
		Count:         3,
		MaxUnitPrice:  100_000_000,
	})
	if err != nil {
		t.Fatalf("合法参数应挂上 · 得 err=%v", err)
	}
	if id == "" {
		t.Fatal("Enqueue 应返 id")
	}

	// 断言真的进了 stock_watcher 表
	var status, vendorID string
	err = database.QueryRowContext(context.Background(),
		`SELECT status, vendor_id FROM stock_watcher WHERE id = ?`, id).Scan(&status, &vendorID)
	if err != nil {
		t.Fatalf("查 stock_watcher: %v", err)
	}
	if status != "watching" {
		t.Errorf("status = %q · want watching", status)
	}
	if vendorID != "vA" {
		t.Errorf("vendor_id = %q · want vA", vendorID)
	}
}
