package webhookin

// **回归哨兵 · 2026-08-14 · 真钱前必修 bug**
//
// 老代码：onWarrantyRefund → 直接调 deathwatch.RefundOnce
// bug：RefundOnce 里 FindRefundable 的 SQL 要求 `pull_round.status='refunded'` ·
// 但**全库没人把 pull_round 标 refunded** —— 候选集永远是空 · 号在质保内死了
// 用户永远退不到钱。dry_run 模式没发现是因为 dry_run 从不生成真订单。
//
// 修：先根据 vendor_order_id / client_order_id UPDATE pull_round.status='refunded' ·
// 再调 RefundOnce。

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ── DB fixture ──────────────────────────────────────

func setupPullRoundDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		_ = d.Close()
		t.Fatalf("迁移: %v", err)
	}
	// 建最少父行
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES ('p1', 'u1', 'u1@example.com', 'x', '2026-01-01', '2026-01-01')`); err != nil {
		_ = d.Close()
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `
		INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at)
		VALUES ('b1', 'test bus', 'single', 'p1', 'active', '2026-01-01')`); err != nil {
		_ = d.Close()
		t.Fatal(err)
	}
	return d.DB, func() { _ = d.Close() }
}

func insertRound(t *testing.T, sqldb *sql.DB, roundID, clientOrderID, vendorOrderID, status string) {
	t.Helper()
	_, err := sqldb.ExecContext(context.Background(), `
		INSERT INTO pull_round
		  (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
		   key_cost_total, service_fee_total, participants_split_json,
		   status, vendor_order_id, created_at)
		VALUES (?, 'kiro91', ?, 'b1', 1, 1, 0, 0, '{}', ?, ?, '2026-01-01')`,
		roundID, clientOrderID, status, nullIfEmpty(vendorOrderID))
	if err != nil {
		t.Fatalf("insertRound: %v", err)
	}
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func roundStatus(t *testing.T, sqldb *sql.DB, roundID string) string {
	t.Helper()
	var s string
	err := sqldb.QueryRowContext(context.Background(),
		`SELECT status FROM pull_round WHERE id = ?`, roundID).Scan(&s)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// ── mock SweepTrigger（记调用不做事）──────────────────

type nopSweeper struct{ refunds int }

func (n *nopSweeper) SweepOnce(_ context.Context) SweepReport {
	return SweepReport{}
}
func (n *nopSweeper) RefundOnce(_ context.Context, _ int) RefundReport {
	n.refunds++
	return RefundReport{}
}

// ── 测试 ────────────────────────────────────────────

// 场景 A：按 vendor_order_id 匹配 → completed → refunded
func TestMarkRoundRefunded_ByVendorOrderID(t *testing.T) {
	sqldb, cleanup := setupPullRoundDB(t)
	defer cleanup()
	insertRound(t, sqldb, "r1", "co-1", "vo-abc", "completed")

	d := New(Config{DB: sqldb, Deathwatch: &nopSweeper{}, Logger: slog.Default()})
	evt := &providers.WebhookEvent{
		VendorID:  providers.Vendor91Kiro,
		EventID:   "evt-1",
		OrderID:   "vo-abc", // WebhookEvent.OrderID = vendor 侧订单号
		EventType: providers.EventWarrantyRefund,
	}
	status, err := d.onWarrantyRefund(context.Background(), evt)
	if err != nil {
		t.Fatalf("onWarrantyRefund: %v", err)
	}
	if status != "ok" {
		t.Fatalf("status = %q · want ok", status)
	}
	if got := roundStatus(t, sqldb, "r1"); got != "refunded" {
		t.Errorf("pull_round.status = %q · want refunded（老 bug：永远停在 completed）", got)
	}
}

// 场景 B：只发 client_order_id（部分 vendor · WebhookEvent.PurchaseOrderID 即我方 client_order_id）
func TestMarkRoundRefunded_ByPurchaseOrderID(t *testing.T) {
	sqldb, cleanup := setupPullRoundDB(t)
	defer cleanup()
	insertRound(t, sqldb, "r2", "co-xyz", "", "completed") // 无 vendor_order_id

	d := New(Config{DB: sqldb, Deathwatch: &nopSweeper{}, Logger: slog.Default()})
	evt := &providers.WebhookEvent{
		VendorID:        providers.Vendor91Kiro,
		EventID:         "evt-2",
		PurchaseOrderID: "co-xyz", // 就是我方 client_order_id
		EventType:       providers.EventWarrantyRefund,
	}
	if _, err := d.onWarrantyRefund(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if got := roundStatus(t, sqldb, "r2"); got != "refunded" {
		t.Errorf("status = %q · want refunded（应回落到 client_order_id 匹配）", got)
	}
}

// 场景 C：partial 也要能标 · 不只 completed
func TestMarkRoundRefunded_FromPartial(t *testing.T) {
	sqldb, cleanup := setupPullRoundDB(t)
	defer cleanup()
	insertRound(t, sqldb, "r3", "co-p", "vo-p", "partial")

	d := New(Config{DB: sqldb, Deathwatch: &nopSweeper{}, Logger: slog.Default()})
	evt := &providers.WebhookEvent{
		VendorID:  providers.Vendor91Kiro,
		EventID:   "evt-3",
		OrderID:   "vo-p",
		EventType: providers.EventWarrantyRefund,
	}
	if _, err := d.onWarrantyRefund(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if got := roundStatus(t, sqldb, "r3"); got != "refunded" {
		t.Errorf("partial → refunded 该通 · 得 %q", got)
	}
}

// 场景 D：幂等 · 已经 refunded 的重放不重刷（也不报错 · RefundOnce 仍会跑）
func TestMarkRoundRefunded_Idempotent(t *testing.T) {
	sqldb, cleanup := setupPullRoundDB(t)
	defer cleanup()
	insertRound(t, sqldb, "r4", "co-i", "vo-i", "refunded") // 已经 refunded

	sweeper := &nopSweeper{}
	d := New(Config{DB: sqldb, Deathwatch: sweeper, Logger: slog.Default()})
	evt := &providers.WebhookEvent{
		VendorID:  providers.Vendor91Kiro,
		EventID:   "evt-4",
		OrderID:   "vo-i",
		EventType: providers.EventWarrantyRefund,
	}
	if _, err := d.onWarrantyRefund(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	// status 保持 refunded
	if got := roundStatus(t, sqldb, "r4"); got != "refunded" {
		t.Errorf("重放 status = %q · want refunded", got)
	}
	// RefundOnce 仍被调（deathwatch 有自己的幂等锚 warranty_refunded_at）
	if sweeper.refunds != 1 {
		t.Errorf("RefundOnce 应被调 1 次 · 得 %d", sweeper.refunds)
	}
}

// 场景 E：完全找不到匹配（外来 webhook / 包量预留等不走 pull_round 的路径）
// —— 不报错 · 不 panic · RefundOnce 仍跑（防漏退别家已在 refunded 态的老单）
func TestMarkRoundRefunded_NoMatch(t *testing.T) {
	sqldb, cleanup := setupPullRoundDB(t)
	defer cleanup()
	insertRound(t, sqldb, "r5", "co-x", "vo-x", "completed")

	sweeper := &nopSweeper{}
	d := New(Config{DB: sqldb, Deathwatch: sweeper, Logger: slog.Default()})
	evt := &providers.WebhookEvent{
		VendorID:  providers.Vendor91Kiro,
		EventID:   "evt-5",
		OrderID:   "vo-not-exist", // 找不到
		EventType: providers.EventWarrantyRefund,
	}
	if _, err := d.onWarrantyRefund(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	// 别人的 round 没被误标
	if got := roundStatus(t, sqldb, "r5"); got != "completed" {
		t.Errorf("不匹配的 round 不该被动 · 得 %q", got)
	}
	// RefundOnce 还是跑（可能有别的老 refunded 单要退）
	if sweeper.refunds != 1 {
		t.Errorf("即使无匹配 · RefundOnce 仍该跑 · 得 %d", sweeper.refunds)
	}
}

// 场景 F：跨 vendor 隔离 · vendor_id 不同不会误改
func TestMarkRoundRefunded_VendorIsolated(t *testing.T) {
	sqldb, cleanup := setupPullRoundDB(t)
	defer cleanup()
	// r6 属 vendor A
	insertRound(t, sqldb, "r6", "co-collide", "vo-collide", "completed")
	// 建一个另一家 vendor 的 round · client_order_id 恰好撞
	_, err := sqldb.ExecContext(context.Background(), `
		INSERT INTO pull_round
		  (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
		   key_cost_total, service_fee_total, participants_split_json,
		   status, vendor_order_id, created_at)
		VALUES ('r7', 'kiroceo', 'co-collide', 'b1', 1, 1, 0, 0, '{}', 'completed', 'vo-collide', '2026-01-01')`)
	if err != nil {
		t.Fatal(err)
	}

	d := New(Config{DB: sqldb, Deathwatch: &nopSweeper{}, Logger: slog.Default()})
	// 只标 vendor A 的
	evt := &providers.WebhookEvent{
		VendorID:  providers.Vendor91Kiro,
		EventID:   "evt-6",
		OrderID:   "vo-collide",
		EventType: providers.EventWarrantyRefund,
	}
	if _, err := d.onWarrantyRefund(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if got := roundStatus(t, sqldb, "r6"); got != "refunded" {
		t.Errorf("vendor A 的 round 应被标 · 得 %q", got)
	}
	if got := roundStatus(t, sqldb, "r7"); got != "completed" {
		t.Errorf("另一家的 round 不该被动 · 得 %q（跨 vendor 撞键的隔离）", got)
	}
}

// 场景 G：两个键都空 · 静默不做事（vendor 契约违规 · 我方防御性处理）
func TestMarkRoundRefunded_BothKeysEmpty(t *testing.T) {
	sqldb, cleanup := setupPullRoundDB(t)
	defer cleanup()
	insertRound(t, sqldb, "r8", "co-e", "vo-e", "completed")

	sweeper := &nopSweeper{}
	d := New(Config{DB: sqldb, Deathwatch: sweeper, Logger: slog.Default()})
	evt := &providers.WebhookEvent{
		VendorID:  providers.Vendor91Kiro,
		EventID:   "evt-7",
		EventType: providers.EventWarrantyRefund,
		// OrderID / PurchaseOrderID 都空
	}
	if _, err := d.onWarrantyRefund(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	// 什么都没被动
	if got := roundStatus(t, sqldb, "r8"); got != "completed" {
		t.Errorf("键都空时不该乱动别人 · 得 %q", got)
	}
	// RefundOnce 仍跑
	if sweeper.refunds != 1 {
		t.Errorf("RefundOnce 仍跑 · 得 %d", sweeper.refunds)
	}
}

var _ = time.Now // 保留 import
