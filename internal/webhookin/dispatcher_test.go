package webhookin

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/stockwatch"
)

// mockDispatchStore · 记 upsert 调用
type mockDispatchStore struct {
	mu    sync.Mutex
	calls []providers.VendorDispatch
}

func (m *mockDispatchStore) UpsertDispatches(
	_ context.Context, _ string, _ string, ds []providers.VendorDispatch,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, ds...)
	return nil
}

func (m *mockDispatchStore) dispatchKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.calls))
	for _, d := range m.calls {
		out = append(out, d.DispatchKey)
	}
	return out
}

// mockNotifier · 记抢号链通知
type mockNotifier struct {
	mu    sync.Mutex
	calls []stockwatch.NotifyParams
}

func (m *mockNotifier) Notify(_ context.Context, p stockwatch.NotifyParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, p)
	return nil
}

func (m *mockNotifier) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// **回归哨兵 · 2026-08-12 生产实测踩的坑**
//
// 部分 vendor 的 new_keys_available webhook **只发 client_order_id / purchase_order_id ·
// 不发独立 order_id**。老逻辑判 `e.OrderID == ""` 直接 skip · 结果：
//   - dispatch 不落库（Status 页看不到这批开号）
//   - **抢号链根本不 Notify · fire 率恒 0**
//
// 生产日志证据（上线首 3 分钟）：
//
//	15:52:26  vendor=A  "缺 order_id · 跳过"（body 里明明有 client_order_id）
//	15:53:29  vendor=B  同上
//	15:55:35  vendor=B  同上
//
// 三条真 webhook 全被 skip · 抢号链空转。修法：OrderID 空 fallback PurchaseOrderID。
func TestOnNewKeys_FallbackToPurchaseOrderID(t *testing.T) {
	store := &mockDispatchStore{}
	notifier := &mockNotifier{}
	d := New(Config{
		DispatchStore: store,
		Notifier:      notifier,
		Logger:        slog.Default(),
	})

	// 只有 PurchaseOrderID · 没有 OrderID（真实 vendor 行为）
	evt := &providers.WebhookEvent{
		VendorID:        providers.VendorKiroOOO,
		EventID:         "854d5a6e9a55cdea5e609cd3b564e560",
		PurchaseOrderID: "af6db51a8b8ed0fe7106e48821a2539a",
		OrderID:         "", // ← vendor 没发
		NewKeys:         12,
		ReceivedAt:      time.Now().UTC(),
		EventType:       providers.EventNewKeysAvailable,
	}

	status, err := d.onNewKeys(context.Background(), evt)
	if err != nil {
		t.Fatalf("onNewKeys: %v", err)
	}
	if status != "ok" {
		t.Fatalf("应处理成功（不该 skip）· 得 %q", status)
	}

	// dispatch 落库了 · dispatch_key 用 PurchaseOrderID
	keys := store.dispatchKeys()
	if len(keys) != 1 {
		t.Fatalf("应落 1 条 dispatch · 得 %d", len(keys))
	}
	if keys[0] != "af6db51a8b8ed0fe7106e48821a2539a" {
		t.Errorf("dispatch_key 应 fallback 到 PurchaseOrderID · 得 %q", keys[0])
	}

	// **抢号链被通知了**（这是整个修复的意义 · 不然 fire 率恒 0）
	if notifier.count() != 1 {
		t.Errorf("抢号链应被通知 1 次 · 得 %d", notifier.count())
	}
}

// OrderID 存在时优先用它（另一类 vendor 的语义：order_id = 开号批次 id）
func TestOnNewKeys_PrefersOrderID(t *testing.T) {
	store := &mockDispatchStore{}
	d := New(Config{DispatchStore: store, Logger: slog.Default()})

	evt := &providers.WebhookEvent{
		VendorID:        providers.VendorKiroAppIO,
		EventID:         "e1",
		OrderID:         "batch_us_1",  // 开号批次 id
		PurchaseOrderID: "idem_key_32", // 我方幂等键
		NewKeys:         10,
		ReceivedAt:      time.Now().UTC(),
		EventType:       providers.EventNewKeysAvailable,
	}

	if _, err := d.onNewKeys(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	keys := store.dispatchKeys()
	if len(keys) != 1 || keys[0] != "batch_us_1" {
		t.Fatalf("有 OrderID 时应优先用它 · 得 %v", keys)
	}
}

// 两个都空才 skip（防误改成"永不 skip"）
func TestOnNewKeys_BothEmpty_Skips(t *testing.T) {
	store := &mockDispatchStore{}
	notifier := &mockNotifier{}
	d := New(Config{DispatchStore: store, Notifier: notifier, Logger: slog.Default()})

	evt := &providers.WebhookEvent{
		VendorID:   providers.VendorKiroCEO,
		EventID:    "e2",
		NewKeys:    5,
		ReceivedAt: time.Now().UTC(),
		EventType:  providers.EventNewKeysAvailable,
	}

	status, err := d.onNewKeys(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	if status != "skipped" {
		t.Fatalf("两个 id 都空应 skip · 得 %q", status)
	}
	if len(store.dispatchKeys()) != 0 {
		t.Error("skip 时不该落 dispatch")
	}
	if notifier.count() != 0 {
		t.Error("skip 时不该通知抢号链")
	}
}

// ── 两个"不处理就出事"的独家事件（2026-08-13 补 · docs/19-fields.md §10）──

// mockSweeper · 记 deathwatch 触发次数
type mockSweeper struct {
	mu     sync.Mutex
	sweeps int
}

func (m *mockSweeper) SweepOnce(_ context.Context) SweepReport {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweeps++
	return SweepReport{Scanned: 3, MarkedDead: 1}
}

func (m *mockSweeper) RefundOnce(_ context.Context, _ int) RefundReport {
	return RefundReport{}
}

func (m *mockSweeper) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sweeps
}

// **reserved_keys_delivered · 包量预留已交付 · 钱已扣 · 号已是我方的**
//
// 漏处理的后果（上游档案明确警告）：钱扣了 · 号记在我方名下 · 但上游 keys 列表
// 只给前缀 —— **这条通知里的 order_id 是取到正文的唯一入口** · 漏了就永远拿不到。
//
// 老代码：providers.EventType 枚举里都没定义 · 走 dispatchByType 的 default 只 log。
func TestDispatchByType_ReservedKeysDelivered(t *testing.T) {
	store := &mockDispatchStore{}
	notifier := &mockNotifier{}
	d := New(Config{DispatchStore: store, Notifier: notifier, Logger: slog.Default()})

	evt := &providers.WebhookEvent{
		VendorID:   providers.Vendor91Kiro,
		EventID:    "evt-reserved-1",
		OrderID:    "ord-reserved-abc", // ★ 取正文的唯一入口
		NewKeys:    3,
		Zone:       providers.ZoneUS,
		ReceivedAt: time.Now().UTC(),
		EventType:  providers.EventReservedKeysDelivered,
	}

	status, err := d.dispatchByType(context.Background(), evt)
	if err != nil {
		t.Fatalf("dispatchByType: %v", err)
	}
	if status != "ok" {
		t.Fatalf("应处理成功 · 得 %q（老代码走 default 返 skipped）", status)
	}

	// 落了 dispatch · key 带 reserved- 前缀（跟普通开号批次区分开）
	keys := store.dispatchKeys()
	if len(keys) != 1 {
		t.Fatalf("应落 1 条 dispatch · 得 %d", len(keys))
	}
	if keys[0] != "reserved-ord-reserved-abc" {
		t.Errorf("dispatch_key = %q · want reserved-ord-reserved-abc", keys[0])
	}

	// **绝不能唤醒抢号链** —— 这批号已经是我方的了 · 不是"有货可抢"
	// 通知抢号链会让挂单去 Purchase · 那是按公共价再买一批（上游明确警告）
	if notifier.count() != 0 {
		t.Errorf("包量预留不该唤醒抢号链 · 得 %d 次通知", notifier.count())
	}
}

// 缺 order_id · 号可能永久拿不到 · 必须报错（不能静默 skip）
func TestDispatchByType_ReservedKeysDelivered_MissingOrderID(t *testing.T) {
	store := &mockDispatchStore{}
	d := New(Config{DispatchStore: store, Logger: slog.Default()})

	evt := &providers.WebhookEvent{
		VendorID:   providers.Vendor91Kiro,
		EventID:    "evt-reserved-2",
		OrderID:    "", // ← 缺
		NewKeys:    3,
		ReceivedAt: time.Now().UTC(),
		EventType:  providers.EventReservedKeysDelivered,
	}

	status, err := d.dispatchByType(context.Background(), evt)
	if err == nil {
		t.Error("缺 order_id 该返 error（钱扣了拿不到号 · 要告警）· 得 nil")
	}
	if status != "error" {
		t.Errorf("status = %q · want error", status)
	}
}

// **key_revoked_abuse · 上游收回已售号**
//
// 漏处理的后果：号已被上游作废 · 我方 credential 还是 alive → **用户拿到废号**。
//
// 老代码：枚举有 EventKeyRevokedAbuse · 但 dispatchByType 无 case 分支 · 走 default 只 log。
func TestDispatchByType_KeyRevokedAbuse(t *testing.T) {
	sweeper := &mockSweeper{}
	d := New(Config{Deathwatch: sweeper, Logger: slog.Default()})

	evt := &providers.WebhookEvent{
		VendorID:   providers.VendorKiroAppIO,
		EventID:    "evt-revoked-1",
		ReceivedAt: time.Now().UTC(),
		EventType:  providers.EventKeyRevokedAbuse,
	}

	status, err := d.dispatchByType(context.Background(), evt)
	if err != nil {
		t.Fatalf("dispatchByType: %v", err)
	}
	if status != "ok" {
		t.Fatalf("应处理成功 · 得 %q（老代码走 default 返 skipped）", status)
	}

	// **触发了 deathwatch 探活** —— 这是让废号被标 dead 的唯一路径
	if sweeper.count() != 1 {
		t.Errorf("应触发 1 次 deathwatch 扫描 · 得 %d（老 bug 是 0 · 废号一直 alive）",
			sweeper.count())
	}
}

// deathwatch 未装配时不 panic · 返 skipped
func TestDispatchByType_KeyRevokedAbuse_NoDeathwatch(t *testing.T) {
	d := New(Config{Logger: slog.Default()})
	evt := &providers.WebhookEvent{
		VendorID:  providers.VendorKiroAppIO,
		EventType: providers.EventKeyRevokedAbuse,
	}
	status, err := d.dispatchByType(context.Background(), evt)
	if err != nil {
		t.Errorf("未装配 deathwatch 不该报错 · 得 %v", err)
	}
	if status != "skipped" {
		t.Errorf("status = %q · want skipped", status)
	}
}
