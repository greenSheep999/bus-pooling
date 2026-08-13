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

// 两个订单号都空时回落 EventID（2026-08-13 生产实测：有一家一个订单号字段都不发 ·
// 只给 "evt_xxx" 去重 id · 原来两级 fallback 让它整天静默丢事件）
func TestOnNewKeys_NoOrderIDs_FallsBackToEventID(t *testing.T) {
	store := &mockDispatchStore{}
	notifier := &mockNotifier{}
	d := New(Config{DispatchStore: store, Notifier: notifier, Logger: slog.Default()})

	evt := &providers.WebhookEvent{
		VendorID:   providers.VendorKiroAppCC,
		EventID:    "evt_BsawZMiNERBGITaBl5DcGNwV",
		NewKeys:    50,
		ReceivedAt: time.Now().UTC(),
		EventType:  providers.EventNewKeysAvailable,
	}

	status, err := d.onNewKeys(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	if status != "ok" {
		t.Fatalf("有 EventID 就该落库 · 得 %q", status)
	}
	keys := store.dispatchKeys()
	if len(keys) != 1 || keys[0] != "evt_BsawZMiNERBGITaBl5DcGNwV" {
		t.Fatalf("dispatch_key 应回落 EventID · 得 %v", keys)
	}
	// 抢号链要被唤醒 —— 这家只能靠 webhook 拿到 balance 态的 fire 资格
	if notifier.count() != 1 {
		t.Errorf("抢号链应被通知 1 次 · 得 %d", notifier.count())
	}
}

// 三级 fallback 全空才 skip（防误改成"永不 skip"落一堆无主行）
func TestOnNewKeys_AllKeysEmpty_Skips(t *testing.T) {
	store := &mockDispatchStore{}
	notifier := &mockNotifier{}
	d := New(Config{DispatchStore: store, Notifier: notifier, Logger: slog.Default()})

	evt := &providers.WebhookEvent{
		VendorID:   providers.VendorKiroCEO,
		NewKeys:    5,
		ReceivedAt: time.Now().UTC(),
		EventType:  providers.EventNewKeysAvailable,
	}

	status, err := d.onNewKeys(context.Background(), evt)
	if err != nil {
		t.Fatal(err)
	}
	if status != "skipped" {
		t.Fatalf("三个键都空应 skip · 得 %q", status)
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

// ── 双区合并通知（某家 vendor 独家 · docs/19-fields.md §11）──

// **回归哨兵 · 2026-08-13**
//
// 那家 vendor 一次到货**只推 1 条** webhook（notification_scope="dual"）·
// 但 body 里带两个区的完整信息 · **且幂等键按区分开**。
//
// 老代码只认顶级字段 · 后果：
//
//	· 只落 1 条 dispatch → 另一区的开号批次在 /status 页完全看不到
//	· 只 Notify 1 次 → 挂在另一区的挂单收不到唤醒 · 那区抢号率恒 0
func TestOnNewKeys_DualZoneFansOut(t *testing.T) {
	store := &mockDispatchStore{}
	notifier := &mockNotifier{}
	d := New(Config{DispatchStore: store, Notifier: notifier, Logger: slog.Default()})

	evt := &providers.WebhookEvent{
		VendorID:        providers.VendorKiroDrop,
		EventID:         "evt-dual-1",
		OrderID:         "ord-top",
		PurchaseOrderID: "poid-top", // 顶级那个 · 双区场景不该用它
		NewKeys:         5,          // 合计
		ReceivedAt:      time.Now().UTC(),
		EventType:       providers.EventNewKeysAvailable,
		PerZone: []providers.ZoneDelivery{
			{Zone: providers.ZoneUS, Region: "us-east-1", NewKeys: 3, PurchaseOrderID: "poid-us"},
			{Zone: providers.ZoneEU, Region: "eu-central-1", NewKeys: 2, PurchaseOrderID: "poid-eu"},
		},
	}

	status, err := d.onNewKeys(context.Background(), evt)
	if err != nil {
		t.Fatalf("onNewKeys: %v", err)
	}
	if status != "ok" {
		t.Fatalf("status = %q · want ok", status)
	}

	// **两条 dispatch** · 各用该区的幂等键（不是顶级那个）
	keys := store.dispatchKeys()
	if len(keys) != 2 {
		t.Fatalf("应落 2 条 dispatch（一区一条）· 得 %d：%v（老 bug 是 1 条）", len(keys), keys)
	}
	want := map[string]bool{"poid-us": true, "poid-eu": true}
	for _, k := range keys {
		if !want[k] {
			t.Errorf("dispatch_key = %q · 应是逐区的 purchase_order_id · 不是顶级 poid-top", k)
		}
	}

	// **两次 Notify** · 各带该区的 zone 和数量
	notifier.mu.Lock()
	calls := append([]stockwatch.NotifyParams(nil), notifier.calls...)
	notifier.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("应逐区唤醒 2 次 · 得 %d（老 bug 是 1 次 · 另一区挂单永远等不到）", len(calls))
	}
	byZone := map[string]int{}
	for _, c := range calls {
		byZone[c.Region] = c.Count
	}
	if byZone["us"] != 3 {
		t.Errorf("us 区应唤醒 count=3 · 得 %d", byZone["us"])
	}
	if byZone["eu"] != 2 {
		t.Errorf("eu 区应唤醒 count=2 · 得 %d", byZone["eu"])
	}
}

// 某区这次没到货（new_keys=0）· 不唤醒那区
func TestOnNewKeys_DualZone_SkipsZeroZone(t *testing.T) {
	store := &mockDispatchStore{}
	notifier := &mockNotifier{}
	d := New(Config{DispatchStore: store, Notifier: notifier, Logger: slog.Default()})

	evt := &providers.WebhookEvent{
		VendorID:   providers.VendorKiroDrop,
		EventID:    "evt-dual-2",
		NewKeys:    4,
		ReceivedAt: time.Now().UTC(),
		EventType:  providers.EventNewKeysAvailable,
		PerZone: []providers.ZoneDelivery{
			{Zone: providers.ZoneUS, NewKeys: 4, PurchaseOrderID: "poid-us"},
			{Zone: providers.ZoneEU, NewKeys: 0, PurchaseOrderID: "poid-eu"}, // 这区没到货
		},
	}

	if _, err := d.onNewKeys(context.Background(), evt); err != nil {
		t.Fatalf("onNewKeys: %v", err)
	}
	// dispatch 两条都落（留痕 · 哪怕 0）· 但只唤醒有货那区
	if n := len(store.dispatchKeys()); n != 2 {
		t.Errorf("dispatch 应落 2 条 · 得 %d", n)
	}
	if notifier.count() != 1 {
		t.Errorf("只该唤醒有货的那一区 · 得 %d 次", notifier.count())
	}
}

// 单区通知（PerZone 空）· 走老路径不变 —— 别把其他 5 家 vendor 带坏
func TestOnNewKeys_SingleZoneUnaffected(t *testing.T) {
	store := &mockDispatchStore{}
	notifier := &mockNotifier{}
	d := New(Config{DispatchStore: store, Notifier: notifier, Logger: slog.Default()})

	evt := &providers.WebhookEvent{
		VendorID:        providers.VendorKiroCEO,
		EventID:         "evt-single",
		PurchaseOrderID: "poid-single",
		NewKeys:         7,
		Zone:            providers.ZoneUS,
		ReceivedAt:      time.Now().UTC(),
		EventType:       providers.EventNewKeysAvailable,
		PerZone:         nil, // 单区 vendor
	}

	if _, err := d.onNewKeys(context.Background(), evt); err != nil {
		t.Fatalf("onNewKeys: %v", err)
	}
	keys := store.dispatchKeys()
	if len(keys) != 1 || keys[0] != "poid-single" {
		t.Errorf("单区该走老路径落 1 条 · 得 %v", keys)
	}
	if notifier.count() != 1 {
		t.Errorf("单区该唤醒 1 次 · 得 %d", notifier.count())
	}
}
