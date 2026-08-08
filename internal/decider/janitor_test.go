package decider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// forceOldUpdatedAt 把 pending 的 updated_at 拨回 3 分钟前 · 让 janitor 立刻能扫到
func forceOldUpdatedAt(t *testing.T, o *Orchestrator, id string) {
	t.Helper()
	old := time.Now().UTC().Add(-3 * time.Minute).Truncate(time.Second)
	if _, err := o.db.ExecContext(context.Background(),
		`UPDATE pending_purchase SET updated_at = ? WHERE id = ?`,
		formatTime(old), id); err != nil {
		t.Fatal(err)
	}
}

func janitorFor(o *Orchestrator) *Janitor {
	return NewJanitor(JanitorConfig{
		Orchestrator: o,
		State:        o.state,
		// 阈值全设 30s，测试里用 forceOldUpdatedAt 拨老
		Timeouts:   StateTimeouts{Initial: 30 * time.Second, Reserved: 30 * time.Second, Purchasing: 30 * time.Second, Purchased: 30 * time.Second, Imported: 30 * time.Second},
		BatchLimit: 10,
	})
}

// initial 卡住 → janitor 直接删。
func TestJanitorDeletesStaleInitial(t *testing.T) {
	o, _, _, pid := newOrchTest(t)
	ctx := context.Background()

	id, err := o.state.Create(ctx, newPending(pid))
	if err != nil {
		t.Fatal(err)
	}
	forceOldUpdatedAt(t, o, id)

	rep := janitorFor(o).SweepOnce(ctx)
	if rep.Recovered != 1 {
		t.Fatalf("Recovered = %d, want 1", rep.Recovered)
	}
	if _, err := o.state.Get(ctx, id); !errors.Is(err, ErrPendingNotFound) {
		t.Errorf("initial 应被删掉，得到 %v", err)
	}
}

// reserved 卡住 → janitor 释放冻结、状态转 cancelled_reserve。
// 关键：wallet.reserved 归零，balance 恢复。
func TestJanitorReleasesStaleReserved(t *testing.T) {
	o, _, _, pid := newOrchTest(t)
	ctx := context.Background()

	// 手动走一遍：Create → reserve
	p := newPending(pid)
	p.ReservedAmount = 100 * testMicro
	id, _ := o.state.Create(ctx, p)
	if err := o.reserveFunds(ctx, id, pid, p.ReservedAmount); err != nil {
		t.Fatal(err)
	}
	forceOldUpdatedAt(t, o, id)

	before := reservedOf(t, o, pid)
	if before != 100*testMicro {
		t.Fatalf("前置：reserved = %d，测试自己没设对", before)
	}

	rep := janitorFor(o).SweepOnce(ctx)
	if rep.Recovered != 1 {
		t.Fatalf("Recovered = %d", rep.Recovered)
	}
	if reservedOf(t, o, pid) != 0 {
		t.Errorf("reserved 应归零，得到 %d", reservedOf(t, o, pid))
	}
	got, _ := o.state.Get(ctx, id)
	if got.Status != StatusCancelledReserve {
		t.Errorf("status = %q, want cancelled_reserve", got.Status)
	}
}

// purchasing 卡住 · vendor **有幂等键** · 重放拿到原批 → 走完 purchased→completed
// 这是 §2.1 的黄金路径。
func TestJanitorReplaysPurchasingWithIdempotentVendor(t *testing.T) {
	o, vendor, _, pid := newOrchTest(t)
	ctx := context.Background()

	// 手动推到 purchasing
	p := newPending(pid)
	p.ReservedAmount = 100 * testMicro
	id, _ := o.state.Create(ctx, p)
	if err := o.reserveFunds(ctx, id, pid, p.ReservedAmount); err != nil {
		t.Fatal(err)
	}
	if err := o.state.Advance(ctx, id, StatusReserved, StatusPurchasing); err != nil {
		t.Fatal(err)
	}
	forceOldUpdatedAt(t, o, id)

	// vendor.Purchase 返回正常成交
	replayCalls := 0
	vendor.purchaseHook = func(req providers.PurchaseRequest) (*providers.PurchaseResult, error) {
		replayCalls++
		if req.ClientOrderID != p.ClientOrderID {
			t.Errorf("重放没用同一个 client_order_id: %q vs %q", req.ClientOrderID, p.ClientOrderID)
		}
		return &providers.PurchaseResult{
			ClientOrderID: req.ClientOrderID, VendorOrderID: "ord-recovered",
			Zone: providers.ZoneUS, Purchased: 2,
			Keys: []providers.KeyPayload{
				{Key: "k1", Paid: providers.Money{Amount: 30 * testMicro, Currency: providers.CurrencyCredit}},
				{Key: "k2", Paid: providers.Money{Amount: 30 * testMicro, Currency: providers.CurrencyCredit}},
			},
			TotalCost: providers.Money{Amount: 60 * testMicro, Currency: providers.CurrencyCredit},
		}, nil
	}

	rep := janitorFor(o).SweepOnce(ctx)
	if rep.Recovered != 1 {
		t.Fatalf("Recovered = %d", rep.Recovered)
	}
	if replayCalls != 1 {
		t.Errorf("vendor.Purchase 被调 %d 次，重放应恰好 1 次", replayCalls)
	}
	got, _ := o.state.Get(ctx, id)
	if got.Status != StatusCompleted {
		t.Fatalf("终态 = %q，janitor 应把恢复走完到 completed", got.Status)
	}
	if reservedOf(t, o, pid) != 0 {
		t.Errorf("reserved 应归零（已转消费），得到 %d", reservedOf(t, o, pid))
	}
}

// purchasing 卡住 · vendor 重放明确 no_stock → 安全释放（没扣款）。
func TestJanitorPurchasingNoStockReleases(t *testing.T) {
	o, vendor, _, pid := newOrchTest(t)
	ctx := context.Background()

	p := newPending(pid)
	p.ReservedAmount = 90 * testMicro
	id, _ := o.state.Create(ctx, p)
	if err := o.reserveFunds(ctx, id, pid, p.ReservedAmount); err != nil {
		t.Fatal(err)
	}
	if err := o.state.Advance(ctx, id, StatusReserved, StatusPurchasing); err != nil {
		t.Fatal(err)
	}
	forceOldUpdatedAt(t, o, id)

	vendor.purchaseHook = func(providers.PurchaseRequest) (*providers.PurchaseResult, error) {
		return nil, &providers.APIError{
			VendorID: providers.Vendor91Kiro, Sentinel: providers.ErrNoStock,
		}
	}

	rep := janitorFor(o).SweepOnce(ctx)
	if rep.Recovered != 1 {
		t.Fatalf("Recovered = %d", rep.Recovered)
	}
	got, _ := o.state.Get(ctx, id)
	if got.Status != StatusCancelledReserve {
		t.Errorf("status = %q, want cancelled_reserve", got.Status)
	}
	if reservedOf(t, o, pid) != 0 {
		t.Errorf("reserved 应释放，得到 %d", reservedOf(t, o, pid))
	}
}

// purchasing 卡住 · vendor **无幂等键** → 转 need_manual（**不能释放冻结**，vendor 可能已扣款）
// 这是 §2.1 的核心安全断言。
func TestJanitorPurchasingWithoutIdempotencyGoesManual(t *testing.T) {
	o, vendor, _, pid := newOrchTest(t)
	ctx := context.Background()

	// 关掉 vendor 幂等能力（模拟 kiroappcc）
	origCap := vendor.capability
	vendor.capability = &providers.Capability{SupportsIdempotency: false}
	t.Cleanup(func() { vendor.capability = origCap })

	p := newPending(pid)
	p.ReservedAmount = 100 * testMicro
	id, _ := o.state.Create(ctx, p)
	if err := o.reserveFunds(ctx, id, pid, p.ReservedAmount); err != nil {
		t.Fatal(err)
	}
	if err := o.state.Advance(ctx, id, StatusReserved, StatusPurchasing); err != nil {
		t.Fatal(err)
	}
	forceOldUpdatedAt(t, o, id)

	// vendor.Purchase 不该被调（Capability=false 直接跳过重放）
	vendor.purchaseHook = func(providers.PurchaseRequest) (*providers.PurchaseResult, error) {
		t.Error("无幂等键的 vendor 不该被重放")
		return nil, errors.New("shouldn't be called")
	}

	rep := janitorFor(o).SweepOnce(ctx)
	if rep.Recovered != 1 {
		t.Fatalf("Recovered = %d, err count = %d", rep.Recovered, rep.Failed)
	}

	got, _ := o.state.Get(ctx, id)
	if got.Status != StatusNeedManual {
		t.Errorf("status = %q, want need_manual", got.Status)
	}
	// 关键：冻结**必须保留** —— vendor 那边可能已扣款，人工核对前不能释放
	if reservedOf(t, o, pid) != 100*testMicro {
		t.Errorf("冻结应保留 %d，得到 %d —— 违反 §2.1 P0-1", 100*testMicro, reservedOf(t, o, pid))
	}
}

// 已被推进的行不该被 janitor 当作 initial 再处理（并发保护）·
// Advance 会刷新 updated_at，所以新状态不再算超时 —— 这也是 FindStale 按
// updated_at 而不是 created_at 的原因。
func TestJanitorRespectsUpdatedAtAfterAdvance(t *testing.T) {
	o, _, _, pid := newOrchTest(t)
	ctx := context.Background()

	id, _ := o.state.Create(ctx, newPending(pid))
	forceOldUpdatedAt(t, o, id)

	// 主线程刚把它推到 reserved（Advance 会刷新 updated_at）
	if err := o.state.Advance(ctx, id, StatusInitial, StatusReserved); err != nil {
		t.Fatal(err)
	}

	rep := janitorFor(o).SweepOnce(ctx)
	if rep.Recovered != 0 {
		t.Errorf("Advance 后 updated_at 已刷新，不该被当超时处理，得到 Recovered=%d", rep.Recovered)
	}
	got, _ := o.state.Get(ctx, id)
	if got.Status != StatusReserved {
		t.Errorf("status = %q，janitor 不该动它", got.Status)
	}
}

// ── helpers ────────────────────────────────────────

func reservedOf(t *testing.T, o *Orchestrator, pid string) int64 {
	t.Helper()
	var r int64
	if err := o.db.QueryRowContext(context.Background(),
		`SELECT reserved FROM wallet WHERE passenger_id = ?`, pid).Scan(&r); err != nil {
		t.Fatal(err)
	}
	return r
}
