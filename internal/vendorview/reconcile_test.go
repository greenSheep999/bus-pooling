package vendorview

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

func reconDB(t *testing.T) *db.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "recon.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return d
}

// 造一笔 pull_round（成交）· 最小字段集
func putRound(t *testing.T, d *db.DB, id, vendorID, vendorOrderID string, count int, keyCostMicro int64, status string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.DB.Exec(`
		INSERT INTO pull_round (id, vendor_id, client_order_id, count_requested,
			count_purchased, key_cost_total, service_fee_total, participants_split_json,
			status, vendor_order_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, '{}', ?, ?, ?)
	`, id, vendorID, "cli-"+id, count, count, keyCostMicro, status, vendorOrderID, now)
	if err != nil {
		t.Fatalf("插 pull_round: %v", err)
	}
}

func putVendorOrder(t *testing.T, d *db.DB, vendorID, orderID string, purchased int) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.DB.Exec(`
		INSERT INTO vendor_order (vendor_id, vendor_order_id, created_at, purchased, fetched_at)
		VALUES (?, ?, ?, ?, ?)
	`, vendorID, orderID, now, purchased, now)
	if err != nil {
		t.Fatalf("插 vendor_order: %v", err)
	}
}

// 全对得上 · 零差异
func TestReconcile_AllMatch(t *testing.T) {
	d := reconDB(t)
	putRound(t, d, "r1", "kiro91", "ord-1", 3, 150_000_000, "completed")
	putVendorOrder(t, d, "kiro91", "ord-1", 3)

	rec := NewReconciler(d.DB, NewLedgerStore(d.DB))
	discs, sum, err := rec.Reconcile(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Discrepancies != 0 {
		t.Fatalf("应零差异 · 得 %d：%+v", sum.Discrepancies, discs)
	}
	if sum.RoundsChecked != 1 {
		t.Errorf("应核对 1 笔 · 得 %d", sum.RoundsChecked)
	}
}

// 我方成交 · vendor 账本查无此单 → orphan_ours
func TestReconcile_OrphanOurs(t *testing.T) {
	d := reconDB(t)
	putRound(t, d, "r1", "kiro91", "ord-x", 2, 100_000_000, "completed")
	// 不插 vendor_order

	rec := NewReconciler(d.DB, NewLedgerStore(d.DB))
	discs, sum, _ := rec.Reconcile(context.Background(), 30)
	if sum.ByKind[DiscOrphanOurs] != 1 {
		t.Fatalf("应 1 条 orphan_ours · 得 %+v", discs)
	}
}

// 数量对不上 → count_mismatch
func TestReconcile_CountMismatch(t *testing.T) {
	d := reconDB(t)
	putRound(t, d, "r1", "kiro91", "ord-1", 3, 150_000_000, "completed")
	putVendorOrder(t, d, "kiro91", "ord-1", 2) // vendor 只记 2

	rec := NewReconciler(d.DB, NewLedgerStore(d.DB))
	discs, sum, _ := rec.Reconcile(context.Background(), 30)
	if sum.ByKind[DiscCountMismatch] != 1 {
		t.Fatalf("应 1 条 count_mismatch · 得 %+v", discs)
	}
	if discs[0].OursCount != 3 || discs[0].VendorCount != 2 {
		t.Errorf("数量记录错 · %+v", discs[0])
	}
}

// 金额对不上 → amount_mismatch（需 vendor_ledger）
func TestReconcile_AmountMismatch(t *testing.T) {
	d := reconDB(t)
	putRound(t, d, "r1", "kiro91", "ord-1", 3, 150_000_000, "completed")
	putVendorOrder(t, d, "kiro91", "ord-1", 3) // 数量对
	// vendor ledger 记扣了 180（我方记 150）
	ls := NewLedgerStore(d.DB)
	_ = ls.UpsertLedger(context.Background(), "kiro91", []providers.VendorLedgerEntry{{
		EntryID: "e1", OrderID: "ord-1", Reason: providers.LedgerPurchase,
		Amount: providers.Money{Amount: -180_000_000, Currency: providers.CurrencyCredit},
		CreatedAt: time.Now().UTC(),
	}})

	rec := NewReconciler(d.DB, ls)
	discs, sum, _ := rec.Reconcile(context.Background(), 30)
	if sum.ByKind[DiscAmountMismatch] != 1 {
		t.Fatalf("应 1 条 amount_mismatch · 得 %+v", discs)
	}
	// 我方 150 · vendor 180
	if discs[0].OursMicro != 150_000_000 || discs[0].VendorMicro != 180_000_000 {
		t.Errorf("金额记录错 · %+v", discs[0])
	}
}

// 我方退款 · vendor 账本无退款 → refund_missing
func TestReconcile_RefundMissing(t *testing.T) {
	d := reconDB(t)
	putRound(t, d, "r1", "kiro91", "ord-1", 3, 150_000_000, "refunded")
	putVendorOrder(t, d, "kiro91", "ord-1", 3)
	ls := NewLedgerStore(d.DB)
	// vendor 只有 purchase · 无 refund
	_ = ls.UpsertLedger(context.Background(), "kiro91", []providers.VendorLedgerEntry{{
		EntryID: "e1", OrderID: "ord-1", Reason: providers.LedgerPurchase,
		Amount: providers.Money{Amount: -150_000_000, Currency: providers.CurrencyCredit},
		CreatedAt: time.Now().UTC(),
	}})

	rec := NewReconciler(d.DB, ls)
	discs, sum, _ := rec.Reconcile(context.Background(), 30)
	if sum.ByKind[DiscRefundMissing] != 1 {
		t.Fatalf("应 1 条 refund_missing · 得 %+v", discs)
	}
}

// vendor 有退款 → 不报 refund_missing
func TestReconcile_RefundPresent_NoAlarm(t *testing.T) {
	d := reconDB(t)
	putRound(t, d, "r1", "kiro91", "ord-1", 3, 150_000_000, "refunded")
	putVendorOrder(t, d, "kiro91", "ord-1", 3)
	ls := NewLedgerStore(d.DB)
	_ = ls.UpsertLedger(context.Background(), "kiro91", []providers.VendorLedgerEntry{
		{EntryID: "e1", OrderID: "ord-1", Reason: providers.LedgerPurchase,
			Amount: providers.Money{Amount: -150_000_000, Currency: providers.CurrencyCredit}, CreatedAt: time.Now().UTC()},
		{EntryID: "e2", OrderID: "ord-1", Reason: providers.LedgerRefund,
			Amount: providers.Money{Amount: 150_000_000, Currency: providers.CurrencyCredit}, CreatedAt: time.Now().UTC()},
	})

	rec := NewReconciler(d.DB, ls)
	_, sum, _ := rec.Reconcile(context.Background(), 30)
	if sum.Discrepancies != 0 {
		t.Fatalf("退款齐全应零差异 · 得 %d", sum.Discrepancies)
	}
}

// initiated / failed 不对账（没真扣费）
func TestReconcile_SkipsNonSettled(t *testing.T) {
	d := reconDB(t)
	putRound(t, d, "r1", "kiro91", "", 3, 150_000_000, "initiated")
	putRound(t, d, "r2", "kiro91", "", 3, 150_000_000, "failed")

	rec := NewReconciler(d.DB, NewLedgerStore(d.DB))
	_, sum, _ := rec.Reconcile(context.Background(), 30)
	if sum.RoundsChecked != 0 {
		t.Errorf("initiated/failed 不该对账 · 得 %d", sum.RoundsChecked)
	}
}

// UpsertLedger 幂等
func TestUpsertLedger_Idempotent(t *testing.T) {
	d := reconDB(t)
	ls := NewLedgerStore(d.DB)
	e := []providers.VendorLedgerEntry{{
		EntryID: "e1", OrderID: "ord-1", Reason: providers.LedgerPurchase,
		Amount: providers.Money{Amount: -100_000_000, Currency: providers.CurrencyCredit},
		CreatedAt: time.Now().UTC(),
	}}
	_ = ls.UpsertLedger(context.Background(), "kiro91", e)
	_ = ls.UpsertLedger(context.Background(), "kiro91", e)

	var cnt int
	if err := d.DB.QueryRow(`SELECT COUNT(*) FROM vendor_ledger`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Errorf("幂等应 1 行 · 得 %d", cnt)
	}
}
