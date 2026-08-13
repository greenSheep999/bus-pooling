package kirodrop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// dashboard.go · 接 GET /api/v1/dashboard · session-gated · 一次响应喂三张表：
//
//   orders[]              → vendor_order（OrderHistoryLister）
//   orders[].keys[]       → vendor_key（KeyHistoryLister）· 内嵌 · 若 vendor 出
//   wallet + metrics 聚合 → vendor_ledger（LedgerLister · 每天一条 total_spent 汇总）
//
// **鉴权**：跟 tiers.go 同一套 SessionToken · 401 认为 token 过期。
// **调用节流**：backfiller 每 5min 分别调 List{Orders,Keys,Ledger} 三次 · 若都真打 dashboard
// 就是 3 倍 HTTP。这里加**响应缓存**（默认 30s TTL · 三个 List 调用共享同一响应）·
// backfiller 一轮 5min 只打 1 次 vendor · 省流量也少一次触发 token 过期的机会。
//
// **⚠️ 上线纪律**（history.go LedgerLister 注释明说）：orders[] 元素形状**尚未在生产实测过**
// （2026-08-14 我方在这家 0 购买 · orders[] 是空）· 解析用**容错优先 + 永远存 Raw** ·
// 首次有真数据后必须核 vendor_order.raw / vendor_key.raw 里的字段名 · 别信推断。

// 编译期确认实现了三个 Lister · backfiller 靠断言接线。
var (
	_ providers.OrderHistoryLister = (*Adapter)(nil)
	_ providers.KeyHistoryLister   = (*Adapter)(nil)
	_ providers.LedgerLister       = (*Adapter)(nil)
)

// dashboardCacheTTL · 一次响应在三个 Lister 之间共享的窗口 · 短点更安全（token 过期时
// 缓存里存的可能是最后一次成功响应 · 但上层已经通过 error 传达了 401 · 缓存只在同一批 backfill
// 内共用 · TTL 短 = 每次 backfill 都是新数据）。
const dashboardCacheTTL = 30 * time.Second

// dashboardResp · GET /api/v1/dashboard 顶层字段（2026-08-14 生产实测 · orders 空）：
//
//	{"metrics":{"claimed_30d":null,"claimed_today":null},
//	 "orders":[],
//	 "wallet":{"available_balance":"0","currency":"CNY","held_balance":"0",
//	           "total_recharged":"0","total_spent":"0"}}
//
// orders[] 元素**未实测**（我方 0 购买）· 用 json.RawMessage 收 + 尽力字段名解析。
type dashboardResp struct {
	Metrics dashboardMetrics `json:"metrics"`
	// Orders 每个元素透传成 RawMessage · 由 parseDashboardOrder 尽力解析
	Orders []json.RawMessage `json:"orders"`
	Wallet dashboardWallet   `json:"wallet"`
}

type dashboardMetrics struct {
	// *_30d / *_today 生产实测为 null · 用 *float64 收
	Claimed30d   *float64 `json:"claimed_30d"`
	ClaimedToday *float64 `json:"claimed_today"`
}

type dashboardWallet struct {
	AvailableBalance string `json:"available_balance"`
	Currency         string `json:"currency"` // 实测 "CNY"
	HeldBalance      string `json:"held_balance"`
	TotalRecharged   string `json:"total_recharged"`
	TotalSpent       string `json:"total_spent"` // ★ ledger 汇总来源
}

// dashboardOrder · orders[] 元素的**推断结构**（未在生产实测 · 首次真订单来了要核）。
//
// 字段名同时试 snake_case 和 camelCase —— 本 vendor 的 API 面用 snake_case
// （`total_price_cny`）· SPA 面（`/api/v1/*`）实测混用（reservation 是 snake_case ·
// dashboard 未知）· 双写兜底避免上线首笔真订单落成空行。
type dashboardOrder struct {
	// 单号
	OrderID       string `json:"order_id"`
	OrderIDCamel  string `json:"orderId"`
	OrderNo       string `json:"order_no"` // 万一是这个名
	ClientOrderID string `json:"client_order_id"`

	// 时间
	CreatedAt      string `json:"created_at"`
	CreatedAtCamel string `json:"createdAt"`
	FinishedAt     string `json:"finished_at"`

	// 数量
	Quantity  int `json:"quantity"`
	Purchased int `json:"purchased"`
	Count     int `json:"count"`

	// 金额（CNY 字符串 · 跟 wallet 同口径）
	TotalPriceCNY string `json:"total_price_cny"`
	TotalCNY      string `json:"total_cny"`
	UnitPriceCNY  string `json:"unit_price_cny"`

	// 区域
	Region string `json:"region"`

	// 状态（vendor 文档说可能 completed / partially_refunded / refunded）
	Status string `json:"status"`

	// key 内嵌（若有）· 元素也当 RawMessage 收 · 交给 parseDashboardKey
	Keys []json.RawMessage `json:"keys"`
}

// dashboardKey · orders[].keys[] 推断结构（同样未实测）。
type dashboardKey struct {
	KeyID        string `json:"key_id"`
	ID           string `json:"id"`
	Key          string `json:"key"` // 明文 · adapter 层脱敏后落 KeyMasked
	Region       string `json:"region"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
	DispatchedAt string `json:"dispatched_at"`
	DeadAt       string `json:"dead_at"`
	DeadReason   string `json:"dead_reason"`

	// 质保 · 用量 · 单价 · 有则填
	WarrantyUntil string `json:"warranty_until"`
	CurrentUsage  int    `json:"current_usage"`
	UsageLimit    int    `json:"usage_limit"`
	UnitPriceCNY  string `json:"unit_price_cny"`
}

// fetchDashboardCached · 拿一次响应 · 30s 内三个 Lister 共享 · 401 转 error 而非缓存。
//
// 返 (dashboard, true, nil) = 有 token 且拿到了
// 返 (nil, false, nil)      = 未配 token（未启用 · backfiller 静默跳过）
// 返 (nil, false, err)      = HTTP / 401 / 解析失败
func (a *Adapter) fetchDashboardCached(ctx context.Context) (*dashboardResp, bool, error) {
	if strings.TrimSpace(a.cfg.SessionToken) == "" {
		return nil, false, nil
	}

	a.dashMu.Lock()
	if a.dashCache != nil && time.Since(a.dashCachedAt) < dashboardCacheTTL {
		out := a.dashCache
		a.dashMu.Unlock()
		return out, true, nil
	}
	a.dashMu.Unlock()

	req, err := a.newBearerReq(ctx, http.MethodGet, "/api/v1/dashboard")
	if err != nil {
		return nil, false, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, false, fmt.Errorf("kirodrop: dashboard: 401 · session token 过期或无效 · 需重新 seed")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("kirodrop: dashboard: http %d", resp.StatusCode)
	}
	var d dashboardResp
	if err := json.Unmarshal(resp.Body, &d); err != nil {
		return nil, false, fmt.Errorf("kirodrop: dashboard 解析: %w", err)
	}

	a.dashMu.Lock()
	a.dashCache = &d
	a.dashCachedAt = time.Now()
	a.dashMu.Unlock()

	return &d, true, nil
}

// ListOrders · OrderHistoryLister 实现 · 本 vendor 一次全量 · 无分页。
//
// cursor 传空 = 从头拉 · 传非空 = 已到底（本 vendor 不支持分页 · 直接返空页）。
func (a *Adapter) ListOrders(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorOrder], error) {
	if cursor != "" {
		return &providers.HistoryPage[providers.VendorOrder]{}, nil
	}
	d, ok, err := a.fetchDashboardCached(ctx)
	if err != nil {
		return nil, err
	}
	if !ok || d == nil {
		return &providers.HistoryPage[providers.VendorOrder]{}, nil
	}
	items := make([]providers.VendorOrder, 0, len(d.Orders))
	for _, rawOrder := range d.Orders {
		if o := parseDashboardOrder(rawOrder); o != nil {
			items = append(items, *o)
		}
	}
	return &providers.HistoryPage[providers.VendorOrder]{Items: items}, nil
}

// ListKeys · KeyHistoryLister 实现 · 从 orders[].keys[] 内嵌取（跟另一家的 orders 内嵌 keys 同风格）。
func (a *Adapter) ListKeys(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorKey], error) {
	if cursor != "" {
		return &providers.HistoryPage[providers.VendorKey]{}, nil
	}
	d, ok, err := a.fetchDashboardCached(ctx)
	if err != nil {
		return nil, err
	}
	if !ok || d == nil {
		return &providers.HistoryPage[providers.VendorKey]{}, nil
	}
	items := make([]providers.VendorKey, 0)
	for _, rawOrder := range d.Orders {
		var o dashboardOrder
		if err := json.Unmarshal(rawOrder, &o); err != nil {
			continue
		}
		orderID := firstNonEmpty(o.OrderID, o.OrderIDCamel, o.OrderNo)
		for _, rawKey := range o.Keys {
			if k := parseDashboardKey(rawKey, orderID); k != nil {
				items = append(items, *k)
			}
		}
	}
	return &providers.HistoryPage[providers.VendorKey]{Items: items}, nil
}

// ListLedger · LedgerLister 实现 · dashboard 没有传统流水表 · 用 wallet.total_spent
// 每天一条聚合成 ledger entry（reason=purchase · amount=负 · balance_after=available_balance）。
//
// **对账语义**：total_spent 是**累计**扣费 · 我方每天拉一次 · 相邻两天差 = 那天扣费。
// 这里不做差值计算（那是 store 层的事）· 只把当日快照存一条 · EntryID 用日期做幂等键。
//
// 每次 backfill 只产 0 或 1 条（当日快照）· 上层 upsert 幂等 · 重跑不重复。
func (a *Adapter) ListLedger(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorLedgerEntry], error) {
	if cursor != "" {
		return &providers.HistoryPage[providers.VendorLedgerEntry]{}, nil
	}
	d, ok, err := a.fetchDashboardCached(ctx)
	if err != nil {
		return nil, err
	}
	if !ok || d == nil {
		return &providers.HistoryPage[providers.VendorLedgerEntry]{}, nil
	}

	spentMicro := priceToMicro(d.Wallet.TotalSpent)
	availMicro := priceToMicro(d.Wallet.AvailableBalance)
	rechargedMicro := priceToMicro(d.Wallet.TotalRecharged)
	heldMicro := priceToMicro(d.Wallet.HeldBalance)

	// 全零 = 我方在这家从没花过钱也没充过 · 不落假快照
	// （注意 vendor 会返 "0.000000" 字符串 · 不是空串 · 要按数值零判定）
	if spentMicro == 0 && availMicro == 0 && rechargedMicro == 0 && heldMicro == 0 {
		return &providers.HistoryPage[providers.VendorLedgerEntry]{}, nil
	}

	now := time.Now().UTC()
	// EntryID 用日期 · 每天最多一条 · upsert 幂等
	entryID := "dashboard-spent-" + now.Format("20060102")

	// dashboard 返聚合 · 严格说不是"一笔流水"· 但 vendor_ledger 表跟对账链约定
	// 只关心 purchase/refund 两类 —— 这里用 reason=other 标注是快照类
	// （不参与差值对账 · 只作为余额留痕）· raw_reason 说清楚。
	raw, _ := json.Marshal(d.Wallet)
	entry := providers.VendorLedgerEntry{
		EntryID:      entryID,
		OrderID:      "",
		Reason:       providers.LedgerOther, // 快照 · 非单笔流水
		RawReason:    "dashboard_wallet_snapshot",
		Amount:       providers.Money{Amount: -spentMicro, Currency: providers.CurrencyCNY},
		BalanceAfter: providers.Money{Amount: availMicro, Currency: providers.CurrencyCNY},
		CreatedAt:    now,
		Raw:          raw,
	}
	return &providers.HistoryPage[providers.VendorLedgerEntry]{Items: []providers.VendorLedgerEntry{entry}}, nil
}

// parseDashboardOrder · 尽力从一条 orders[] 元素解出 VendorOrder。
//
// 关键字段：VendorOrderID 必须能拿到（幂等主键 · 拿不到就跳过 · 别落匿名行）。
// 其他字段任缺 · 尽量填 · 兜底 Raw。
func parseDashboardOrder(rawMsg json.RawMessage) *providers.VendorOrder {
	var o dashboardOrder
	if err := json.Unmarshal(rawMsg, &o); err != nil {
		return nil
	}
	orderID := firstNonEmpty(o.OrderID, o.OrderIDCamel, o.OrderNo)
	if orderID == "" {
		return nil // 无稳定主键 · 别落
	}
	createdRaw := firstNonEmpty(o.CreatedAt, o.CreatedAtCamel, o.FinishedAt)
	purchased := o.Purchased
	if purchased == 0 {
		purchased = firstNonZero(o.Quantity, o.Count)
	}
	totalStr := firstNonEmpty(o.TotalPriceCNY, o.TotalCNY)
	unitStr := o.UnitPriceCNY

	return &providers.VendorOrder{
		VendorOrderID: orderID,
		CreatedAt:     parseTimeAny(createdRaw),
		Purchased:     purchased,
		Requested:     purchased, // 本 vendor 无独立 requested 字段 · 用 purchased 兜
		UnitPrice:     providers.Money{Amount: priceToMicro(unitStr), Currency: providers.CurrencyCNY},
		TotalCost:     providers.Money{Amount: priceToMicro(totalStr), Currency: providers.CurrencyCNY},
		Source:        "api",
		Raw:           rawMsg,
	}
}

// parseDashboardKey · 尽力从一条 orders[].keys[] 元素解出 VendorKey。
func parseDashboardKey(rawMsg json.RawMessage, orderID string) *providers.VendorKey {
	var k dashboardKey
	if err := json.Unmarshal(rawMsg, &k); err != nil {
		return nil
	}
	keyID := firstNonEmpty(k.KeyID, k.ID)
	if keyID == "" && k.Key == "" {
		return nil // 既无 id 也无正文 · 跳过
	}
	status := k.Status
	if status == "" {
		status = "unknown"
	}
	return &providers.VendorKey{
		VendorKeyID:   keyID,
		OrderID:       orderID,
		KeyMasked:     maskKirodropKey(k.Key),
		Region:        k.Region,
		Status:        status,
		CreatedAt:     parseTimeAny(k.CreatedAt),
		DispatchedAt:  parseTimeAny(k.DispatchedAt),
		DeadAt:        parseTimeAny(k.DeadAt),
		DeadReason:    k.DeadReason,
		CurrentUsage:  k.CurrentUsage,
		UsageLimit:    k.UsageLimit,
		WarrantyUntil: parseTimeAny(k.WarrantyUntil),
		UnitPrice:     providers.Money{Amount: priceToMicro(k.UnitPriceCNY), Currency: providers.CurrencyCNY},
		Raw:           rawMsg,
	}
}

// firstNonEmpty · 取第一个非空字符串
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// firstNonZero · 取第一个非零 int
func firstNonZero(ns ...int) int {
	for _, n := range ns {
		if n != 0 {
			return n
		}
	}
	return 0
}

// parseTimeAny · 时间字符串万能解析 · RFC3339 / ISO8601 / 北京墙钟都试
func parseTimeAny(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	// 北京墙钟兜底（跟 tiers.go 一样口径）
	if t, err := time.ParseInLocation(decayTimeLayout, s, decayTZ); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// maskKirodropKey · key 明文脱敏 · 前 8 位 + ****
func maskKirodropKey(k string) string {
	if len(k) <= 8 {
		return k
	}
	return k[:8] + "****"
}

