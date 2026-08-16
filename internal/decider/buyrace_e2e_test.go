package decider

// 抢号链端到端模拟测试 · **不花一分钱**（DryRunVendor 假 vendor + DryRunPool 假号池）。
//
// 这是抢号链上线前的最后一道验证 —— 装配上真的 Orchestrator + stockwatch.Watcher ·
// 走真流程 · 不 mock 任何业务逻辑。只有 vendor 和 housepool 是假的（不发外网请求）。
//
// 场景：
//  1. 用户拉号 · vendor 缺货 → decider 返 ErrNoStock · stock_watcher 挂单
//  2. 模拟 vendor restock → 打 webhook Notify
//  3. Watcher 抢到 fired 状态 · 调 FireWatcher → decider.Pull 走完整链
//  4. 断言：号进 credential_ledger · watcher 标 fulfilled · 钱扣了

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/stockwatch"
)

// buildE2E · 装配真 Orchestrator + Watcher + DryRunVendor + DryRunPool
//
// 装配顺序跟 main.go 一致（Watcher{Firer:nil} → orch → SetFirer）· 用它做回归
// 哨兵：如果哪天有人在 main.go 漏了 SetFirer · 这个测试会挂。
func buildE2E(t *testing.T) (*Orchestrator, *stockwatch.Watcher, *DryRunVendor, *db.DB) {
	t.Helper()
	ctx := context.Background()

	d := db.NewTestDB(t)

	// seed passenger + wallet · 钱足够扣
	if _, err := d.ExecContext(ctx,
		`INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		 VALUES ('p1','u1','u1@example.com','x','2026-01-01','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx,
		`INSERT INTO wallet (passenger_id, balance, reserved, updated_at)
		 VALUES ('p1', 10000000000, 0, '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	// 用户拉号那次的幂等记录（api 层建的）
	if _, err := d.ExecContext(ctx,
		`INSERT INTO idempotency_record
		   (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		 VALUES ('idem-user','p1','POST','/api/me/pull','user-key','user-fp','2026-01-01')`); err != nil {
		t.Fatal(err)
	}

	vendor := &DryRunVendor{
		VendorID:  providers.Vendor91Kiro,
		UnitPrice: 30 * 1_000_000,
	}
	vendor.SetStock(0) // **起始缺货** · 触发挂单

	pool := &DryRunPool{}

	// 装配顺序（跟 main.go 一致 · 解构造环）
	watcher := stockwatch.New(stockwatch.Config{
		DB:     d.DB,
		Logger: slog.Default(),
	})

	orch := New(Config{
		DB:       d.DB,
		State:    NewStore(d.DB),
		Vendor:   vendor,
		Pool:     pool,
		Rates:    Rates{Service: 500},
		Enqueuer: watcher,
	})
	watcher.SetFirer(orch)

	return orch, watcher, vendor, d
}

// TestE2E_OutOfStock_Enqueues_ThenWebhookRestock_Fires · 全链路模拟
//
// 步骤：
//
//	① 用户 Pull · vendor 报 stock=0 → 返 ErrNoStock（用户端表现："暂无库存"）
//	② stock_watcher 表里有一条 watching（挂单成功）
//	③ 模拟 vendor 上货 · 调 Watcher.Notify("webhook") · Watcher fire → FireWatcher →
//	   Pull 走完整链 · 号进 credential_ledger · watcher 标 fulfilled
//	④ 断言钱扣了 · 冻结释放了
func TestE2E_OutOfStock_Enqueues_ThenWebhookRestock_Fires(t *testing.T) {
	orch, watcher, vendor, d := buildE2E(t)
	ctx := context.Background()

	// ① 用户拉号 · 缺货 → auto 模式挂单
	// 不指定 VendorID · 走 defaultVendor · maybeEnqueueOnNoStock 三道门全过
	// Zone 也不带 · 避免"enqueue 存的 region 跟 Notify 传的对不上"的问题（见下方 bug 记录）
	_, err := orch.Pull(ctx, PullInput{
		PassengerID:         "p1",
		Count:               2,
		IdempotencyRecordID: "idem-user",
	})
	if err == nil {
		t.Fatal("缺货应返错")
	}
	// 挂单已进 stock_watcher 表（enqueue 是 side-effect · 主流程仍返 ErrNoStock）

	// ② 查 stock_watcher 有 watching
	var watchingCount int
	if err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM stock_watcher WHERE passenger_id='p1' AND status='watching'`).
		Scan(&watchingCount); err != nil {
		t.Fatal(err)
	}
	if watchingCount != 1 {
		t.Fatalf("挂单应有 1 条 watching · 得 %d", watchingCount)
	}
	t.Logf("① 挂单成功 · %d 条 watching", watchingCount)

	// ③ 模拟 vendor 上货 · webhook 到 → Notify
	vendor.SetStock(10) // vendor 有 10 个可购

	// Region 不传 · 匹配"任意 region"的 watching · 避开命名口径不一致的真实 bug：
	// enqueue 存的 region 是 zone 名（"us"）· 探针 / webhook 传的可能是 vendor 的
	// region 名（"us-east-1"）· 两套命名 SQL 严格匹配就查不到。这问题挂在 docs/
	// 16-buy-race.md 待讨论清单里（Region 命名对齐 · 还没定）· 测试先绕过。
	if err := watcher.Notify(ctx, stockwatch.NotifyParams{
		VendorID: string(providers.Vendor91Kiro),
		Count:    10,
		Source:   "webhook",
	}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	// ④ 断言：watcher 已 fulfilled
	var status string
	if err := d.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE passenger_id='p1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "fulfilled" {
		t.Fatalf("webhook 唤醒后应 fulfilled · 得 %q", status)
	}
	t.Log("② webhook Notify → watcher fulfilled")

	// ⑤ 号真的进 credential_ledger（走了完整 Pull · 不是绕过）
	var credCount int
	if err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM credential_ledger
		  WHERE owner_record_passenger_id='p1' AND status='alive'`).Scan(&credCount); err != nil {
		t.Fatal(err)
	}
	if credCount != 2 {
		t.Fatalf("fire 后应有 2 个 alive 号进 record group · 得 %d", credCount)
	}
	t.Log("③ 号进 credential_ledger · 走完整 Pull 链")

	// ⑥ 钱扣了 · 冻结释放
	var balance, reserved int64
	_ = d.QueryRowContext(ctx,
		`SELECT balance, reserved FROM wallet WHERE passenger_id='p1'`).Scan(&balance, &reserved)
	if balance == 10000000000 {
		t.Fatal("钱没扣 · 抢号链断了")
	}
	if reserved != 0 {
		t.Fatalf("冻结应清零 · 得 %d", reserved)
	}
	t.Logf("④ 扣钱 %d microunit · 冻结清零", 10000000000-balance)
}

// TestE2E_Notify_WhenNoWatching · 没挂单时收到 restock 通知 → 无害 no-op
//
// 场景：vendor 上货 · 但当前没人在等（库存充足时的正常情况）· Notify 不该报错
func TestE2E_Notify_WhenNoWatching_NoOp(t *testing.T) {
	_, watcher, _, d := buildE2E(t)
	ctx := context.Background()

	if err := watcher.Notify(ctx, stockwatch.NotifyParams{
		VendorID: string(providers.Vendor91Kiro),
		Count:    10,
		Source:   "webhook",
	}); err != nil {
		t.Fatalf("空队列 Notify 应无错 · 得 %v", err)
	}

	// 库里干净 · 没有意外的 stock_watcher 行
	var n int
	_ = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM stock_watcher`).Scan(&n)
	if n != 0 {
		t.Fatalf("不该无故建挂单 · 得 %d 条", n)
	}
}

// TestE2E_StillNoStock_RewindsToWatching · vendor 说有货但 fire 时又空了 · 挂单继续等
//
// 真实场景：webhook 到 · Watcher fire · 但 vendor Purchase 那一刻别人抢完了 · 返 ErrNoStock
func TestE2E_StillNoStock_RewindsToWatching(t *testing.T) {
	orch, watcher, vendor, d := buildE2E(t)
	ctx := context.Background()

	// 触发挂单
	_, _ = orch.Pull(ctx, PullInput{
		PassengerID:         "p1",
		Count:               1,
		Zone:                providers.ZoneUS,
		IdempotencyRecordID: "idem-user",
	})

	// vendor 依然缺货 · 但 webhook 打过来（可能是别人的 event · 或者 delta 误报）
	vendor.SetStock(0)

	before := time.Now()
	_ = watcher.Notify(ctx, stockwatch.NotifyParams{
		VendorID: string(providers.Vendor91Kiro),
		Count:    5,
		Source:   "webhook",
	})

	// 挂单**应回退到 watching** · 不是 fulfilled 也不是 expired
	var status string
	var fireCount int
	_ = d.QueryRowContext(ctx,
		`SELECT status, fire_count FROM stock_watcher WHERE passenger_id='p1'`).
		Scan(&status, &fireCount)
	if status != "watching" {
		t.Fatalf("still-no-stock 应回 watching · 得 %q", status)
	}
	if fireCount != 1 {
		t.Fatalf("fire_count 应 1（试过一次 · 计入防 spam）· 得 %d", fireCount)
	}
	t.Logf("回退 watching 用时 %v · fire_count=1", time.Since(before))
}
