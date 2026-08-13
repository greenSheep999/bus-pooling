package kirodrop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// **形状证据**：2026-08-14 生产 session 实测响应（orders 空 · 我方那时 0 购买）：
//
//	{"metrics":{"claimed_30d":null,"claimed_today":null},
//	 "orders":[],
//	 "wallet":{"available_balance":"0.000000","currency":"CNY","held_balance":"0.000000",
//	           "total_recharged":"0.000000","total_spent":"0.000000"}}
//
// orders[] 元素形状**未在生产实测过** —— 首笔真订单来了后必须核 vendor_order.raw
// （history.go LedgerLister 注释明说的"上线纪律"）。测里用推断结构 · 至少证明"如果 vendor
// 按这个形状返"我方能解 · 如果不按也会安全跳过（Raw 存全 · 关键字段缺就返 nil 不落假行）。

const dashboardEmpty = `{"metrics":{"claimed_30d":null,"claimed_today":null},"orders":[],
 "wallet":{"available_balance":"0.000000","currency":"CNY","held_balance":"0.000000",
           "total_recharged":"0.000000","total_spent":"0.000000"}}`

// dashboardOneOrder · 推断的"一条订单 + 一把 key"形状 · 双写字段名（snake + camel）
const dashboardOneOrder = `{"metrics":{"claimed_30d":3.0,"claimed_today":1.0},
 "orders":[{"order_id":"ord_abc123","created_at":"2026-08-14T02:15:30.123456+08:00",
   "quantity":2,"purchased":2,"region":"us","status":"completed",
   "total_price_cny":"99.960000","unit_price_cny":"49.980000",
   "keys":[{"id":"key_1","key":"kdrop_abcdef0123456","region":"us","status":"active",
            "created_at":"2026-08-14T02:15:31Z","warranty_until":"2026-08-14T02:25:31Z"}]}],
 "wallet":{"available_balance":"100.000000","currency":"CNY","held_balance":"0.000000",
           "total_recharged":"200.000000","total_spent":"99.960000"}}`

// **回归哨兵 · 2026-08-14**
//
// 空 token → List{Orders,Keys,Ledger} 都返空页 + 不报错
// （backfiller 静默跳过 · 别把已存的旧值抹了 · 跟 ListTimeDecay 同一套契约）。
func TestDashboard_EmptyTokenSkips(t *testing.T) {
	a, err := New(Config{BaseURL: "http://unused", APIKey: "k"}) // SessionToken 空
	if err != nil {
		t.Fatal(err)
	}
	if p, err := a.ListOrders(context.Background(), ""); err != nil {
		t.Errorf("空 token 不该报错 · 得 %v", err)
	} else if len(p.Items) != 0 {
		t.Errorf("空 token 应返空 items · 得 %d", len(p.Items))
	}
	if p, err := a.ListKeys(context.Background(), ""); err != nil {
		t.Errorf("空 token · ListKeys · %v", err)
	} else if len(p.Items) != 0 {
		t.Errorf("空 token · Keys 应空")
	}
	if p, err := a.ListLedger(context.Background(), ""); err != nil {
		t.Errorf("空 token · ListLedger · %v", err)
	} else if len(p.Items) != 0 {
		t.Errorf("空 token · Ledger 应空")
	}
}

// 401 → 报错 · 上层记 WARN 保留旧值不清空（跟 tiers 同一套契约）
func TestDashboard_401Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauth", http.StatusUnauthorized)
	}))
	defer srv.Close()
	a, err := New(Config{BaseURL: srv.URL, APIKey: "k", SessionToken: "expired"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListOrders(context.Background(), ""); err == nil {
		t.Error("401 该报错 · 让 backfiller 记 WARN · 得 nil")
	}
	if _, err := a.ListLedger(context.Background(), ""); err == nil {
		t.Error("401 · Ledger 也该报错")
	}
}

// 全零 wallet + 空 orders → Ledger 返空 · 不落假快照（避免生成一堆全零聚合行）
func TestDashboard_EmptyWalletNoLedgerEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dashboardEmpty))
	}))
	defer srv.Close()
	a, err := New(Config{BaseURL: srv.URL, APIKey: "k", SessionToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.ListLedger(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 0 {
		t.Errorf("全零 wallet 不该造快照 · 得 %d 条", len(p.Items))
	}
	// orders / keys 空也是空
	if o, _ := a.ListOrders(context.Background(), ""); len(o.Items) != 0 {
		t.Errorf("空 orders · 得 %d", len(o.Items))
	}
}

// 单订单 · 检字段解析 + Ledger 快照 + 逐把 key 抽取
func TestDashboard_OneOrderRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/dashboard" {
			t.Errorf("路径应为 /api/v1/dashboard · 得 %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Errorf("鉴权头应为 Bearer t")
		}
		if r.Header.Get("X-API-Key") != "" {
			t.Errorf("/api/v1/* 不该带 X-API-Key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dashboardOneOrder))
	}))
	defer srv.Close()
	a, err := New(Config{BaseURL: srv.URL, APIKey: "k", SessionToken: "t"})
	if err != nil {
		t.Fatal(err)
	}

	// ListOrders
	op, err := a.ListOrders(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(op.Items) != 1 {
		t.Fatalf("应 1 单 · 得 %d", len(op.Items))
	}
	o := op.Items[0]
	if o.VendorOrderID != "ord_abc123" {
		t.Errorf("VendorOrderID = %q", o.VendorOrderID)
	}
	if o.Purchased != 2 {
		t.Errorf("Purchased = %d · 应 2", o.Purchased)
	}
	if o.TotalCost.Currency != providers.CurrencyCNY || o.TotalCost.Amount != 99_960_000 {
		t.Errorf("TotalCost = %+v · 应 {99_960_000, CNY}", o.TotalCost)
	}
	if o.UnitPrice.Amount != 49_980_000 {
		t.Errorf("UnitPrice = %d · 应 49_980_000", o.UnitPrice.Amount)
	}
	if len(o.Raw) == 0 {
		t.Error("Raw 应有内容 · 便于上线首笔核字段")
	}

	// ListKeys · 从 orders[].keys 抽
	kp, err := a.ListKeys(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(kp.Items) != 1 {
		t.Fatalf("应 1 把 key · 得 %d", len(kp.Items))
	}
	k := kp.Items[0]
	if k.VendorKeyID != "key_1" {
		t.Errorf("VendorKeyID = %q", k.VendorKeyID)
	}
	if k.OrderID != "ord_abc123" {
		t.Errorf("OrderID = %q · 应从父 order 继承", k.OrderID)
	}
	// key 明文脱敏 · 前 8 位 + ****
	if k.KeyMasked != "kdrop_ab****" {
		t.Errorf("KeyMasked = %q · 应 kdrop_ab****（脱敏）", k.KeyMasked)
	}
	if k.Region != "us" || k.Status != "active" {
		t.Errorf("Region/Status = %q/%q", k.Region, k.Status)
	}

	// ListLedger · wallet 快照
	lp, err := a.ListLedger(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(lp.Items) != 1 {
		t.Fatalf("应 1 条 wallet 快照 · 得 %d", len(lp.Items))
	}
	le := lp.Items[0]
	// EntryID 按日期做幂等键 · 前缀固定
	if len(le.EntryID) < 20 || le.EntryID[:17] != "dashboard-spent-2" {
		t.Errorf("EntryID = %q · 应带日期前缀", le.EntryID)
	}
	// total_spent 99.96 → -99_960_000（扣费为负 · history.go 约定）
	if le.Amount.Amount != -99_960_000 {
		t.Errorf("Amount = %d · 应 -99_960_000（扣费为负）", le.Amount.Amount)
	}
	if le.Amount.Currency != providers.CurrencyCNY {
		t.Errorf("Currency = %q · 应 CNY", le.Amount.Currency)
	}
	// BalanceAfter = available_balance 100.00
	if le.BalanceAfter.Amount != 100_000_000 {
		t.Errorf("BalanceAfter = %d · 应 100_000_000", le.BalanceAfter.Amount)
	}
	if le.Reason != providers.LedgerOther {
		t.Errorf("Reason = %q · 快照类应 other · 别混进 purchase 干扰对账差值", le.Reason)
	}
}

// **缓存共享**：一轮 backfill 里三个 List 调用只该打一次 HTTP · 省流量 + 少一次触发 token 过期
func TestDashboard_CacheSharedAcrossListers(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dashboardOneOrder))
	}))
	defer srv.Close()
	a, err := New(Config{BaseURL: srv.URL, APIKey: "k", SessionToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// 三次调用 · 30s 内应只打 1 次 HTTP
	if _, err := a.ListOrders(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListKeys(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListLedger(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("三个 Lister 应共享一次 HTTP · 实际打了 %d 次", n)
	}
}

// cursor 非空 · 视为已到底 · 返空页（本 vendor 无分页）
func TestDashboard_CursorNonemptyReturnsEmpty(t *testing.T) {
	a, err := New(Config{BaseURL: "http://unused", APIKey: "k", SessionToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := a.ListOrders(context.Background(), "next"); len(p.Items) != 0 {
		t.Errorf("cursor 非空该视为到底 · 得 %d 条", len(p.Items))
	}
}

// 无稳定 order_id 的元素 · 跳过不落匿名行
func TestDashboard_OrderWithoutIDIsSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metrics":{},
			"orders":[{"quantity":1,"total_price_cny":"10.0"}],
			"wallet":{"total_spent":"10.0","available_balance":"5.0","currency":"CNY","total_recharged":"15.0","held_balance":"0"}}`))
	}))
	defer srv.Close()
	a, err := New(Config{BaseURL: srv.URL, APIKey: "k", SessionToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.ListOrders(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 0 {
		t.Errorf("无 order_id 的元素该跳过 · 得 %d 条", len(p.Items))
	}
}
