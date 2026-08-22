package api

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/passenger"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
)

// I-15 · vendors/status 内存缓存(2026-08-22 生产延迟排查)
//
// 症状:loopback 也要 240ms · vendorview.StatusOverview 遍历 7 vendor × 5 SQL(24h/168h
// 窗口聚合)· 全用户共享同一份数据。
//
// 30s TTL 内存缓存 · 全用户共享 · 跨用户复用 · O(1) 读。
// key = windowHours(不同窗口不同缓存 · 但只 168/24/720 三个常用值 · 内存开销可忽略)。
type statusCache struct {
	mu   sync.RWMutex
	data map[int]statusCacheEntry
}

type statusCacheEntry struct {
	value  vendorview.StatusOverview
	expiry time.Time
}

const statusCacheTTL = 30 * time.Second

var globalStatusCache = &statusCache{data: make(map[int]statusCacheEntry)}

func (c *statusCache) get(windowHours int) (vendorview.StatusOverview, bool) {
	c.mu.RLock()
	entry, ok := c.data[windowHours]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expiry) {
		return vendorview.StatusOverview{}, false
	}
	return entry.value, true
}

func (c *statusCache) set(windowHours int, v vendorview.StatusOverview) {
	c.mu.Lock()
	c.data[windowHours] = statusCacheEntry{value: v, expiry: time.Now().Add(statusCacheTTL)}
	c.mu.Unlock()
}

// vendors 只读端点。**要鉴权** —— 单价按调用者身份差异化（decisions §8.20），
// 拿不到身份就没法定价。

// TODO(assembly): s.vendorView 需要在 ServerDeps 加：
//   type: *vendorview.Service
//   来源: vendorview.New(vendorview.Config{Registry, Rates}) （main.go）

// viewerOf 把 passenger 翻译成 vendorview.Viewer。
//
// coupon_code 阶段 1a 简化：**永远 false**（暂不接优惠码逻辑；文档已注明 1a 待接）。
//
// **Tier 必须填** —— vendorview 用它决定计费链免哪几个分项 + 能不能看 vendor 展示名
// （只 wholesale 能 · docs/10-pricing §2.1）。漏填会让所有人退到 retail 视角（贵但不漏名 · 安全侧）。
func viewerOf(p *passenger.Passenger, r *http.Request) vendorview.Viewer {
	_ = r
	if p == nil {
		return vendorview.Viewer{Tier: vendorview.TierRetail}
	}
	tier := p.Tier
	if tier == "" {
		tier = vendorview.TierRetail
	}
	return vendorview.Viewer{
		PassengerID: p.ID,
		Tier:        tier,
		Invited:     p.Invited,
		WaiveMarkup: false,
	}
}

// GET /api/vendors/status · **公开端点**（不要求登录）
//
// 返回值经 vendorview.StatusOverview 已做脱敏（匿名 label · 档位化 · 无价格 · 无内部字段）。
// vendorView == nil 时（老装配路径）返空的 StatusOverview · 前端展示"数据采集中"。
func (s *Server) handleVendorsStatus(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		writeJSON(w, http.StatusOK, map[string]any{"vendors": []any{}})
		return nil
	}
	// window 参数决定 Quality 里 Volume/Freshness 的评估窗口 + 排序
	// 默认 168h（7d）· 上限 720h（30d）· 跟 events 端点一致
	windowHours := atoiDefault(strings.TrimSuffix(r.URL.Query().Get("window"), "h"), 168)
	if windowHours < 1 {
		windowHours = 168
	}
	if windowHours > 720 {
		windowHours = 720
	}
	// I-15 · 30s 内存缓存 · 跨用户共享 · 生产 240ms → <1ms 感知消失
	if cached, ok := globalStatusCache.get(windowHours); ok {
		writeJSON(w, http.StatusOK, cached)
		return nil
	}
	out := s.vendorView.StatusOverview(r.Context(), windowHours)
	globalStatusCache.set(windowHours, out)
	writeJSON(w, http.StatusOK, out)
	return nil
}

// GET /api/vendors/status/{anon_id}/trend?window=24h · **公开端点**
//
// 返回单家 vendor 过去 windowHours 小时的 15min 分桶数据（uptime + stock 档位）。
// 用 anon_id 定位 · 内部 vendor_id 永远不出。窗口默认 24h · 最大 168h（7 天）。
func (s *Server) handleVendorStatusTrend(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return ErrNotFound("vendor status 未装配")
	}
	anonID := r.PathValue("anon_id")
	if anonID == "" {
		return ErrBadRequest("缺少 anon_id")
	}
	windowHours := atoiDefault(strings.TrimSuffix(r.URL.Query().Get("window"), "h"), 24)
	if windowHours < 1 {
		windowHours = 24
	}
	if windowHours > 168 {
		windowHours = 168
	}
	out, err := s.vendorView.StatusTrend(r.Context(), anonID, windowHours)
	if err != nil {
		return err
	}
	if out == nil {
		return ErrNotFound("找不到这家 vendor")
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// GET /api/vendors/status/{anon_id}/events?window=168h&limit=200 · **公开端点**
//
// 统一的开号事件流 —— 6 家**同一形状**（有 fleet 端点的读 vendor 自报批次 ·
// 没有的从探针增量推同形状事件 · 标 derived=true）。前端只画一种图 + 一个 log 列表。
//
// 取代老的 /trend（那个按 source 返两种 schema · 前端得 if/else 画柱或线）。
func (s *Server) handleVendorDispatchEvents(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return ErrNotFound("vendor status 未装配")
	}
	anonID := r.PathValue("anon_id")
	if anonID == "" {
		return ErrBadRequest("缺少 anon_id")
	}
	windowHours := atoiDefault(strings.TrimSuffix(r.URL.Query().Get("window"), "h"), 168)
	limit := atoiDefault(r.URL.Query().Get("limit"), 200)

	out, err := s.vendorView.DispatchEvents(r.Context(), anonID, windowHours, limit)
	if err != nil {
		return err
	}
	if out == nil {
		return ErrNotFound("找不到这家 vendor")
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// GET /api/vendors/stock
func (s *Server) handleVendorsStock(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return vendorViewUnavailable()
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	out := s.vendorView.AggregateStock(r.Context(), viewerOf(p, r))
	writeJSON(w, http.StatusOK, out)
	return nil
}

// GET /api/vendors/prices?days=&zone=
func (s *Server) handleVendorsPrices(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return vendorViewUnavailable()
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	days := atoiDefault(r.URL.Query().Get("days"), 30)
	if days < 1 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	zone := r.URL.Query().Get("zone")
	out := s.vendorView.Prices(r.Context(), zone, days, viewerOf(p, r))
	writeJSON(w, http.StatusOK, out)
	return nil
}

// GET /api/vendors/{vendor_id}/stock
func (s *Server) handleVendorStock(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return vendorViewUnavailable()
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	id := r.PathValue("vendor_id")
	if id == "" {
		return ErrBadRequest("缺少 vendor_id")
	}
	out, err := s.vendorView.VendorStock(r.Context(), id, viewerOf(p, r))
	if errors.Is(err, vendorview.ErrVendorNotFound) {
		return ErrNotFound("找不到这家 vendor")
	}
	if err != nil {
		// 上游临时不可用 —— 对外只说"稍后再试"，不透具体原因（CLAUDE.md §0.1）
		return newFail(http.StatusBadGateway, CodeInternal,
			"上游暂时不可用，稍后再试")
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// GET /api/vendors/{vendor_id}/prices/daily?days=30
//
// 返 vendor 过去 days 天每日单价 min/max/avg + 样本数 · 用于 /prices 页动态曲线。
// 数据源：vendor_probe.sample_price_micro（Prober 60s 一条 · 24h 约 1440 条 / 家）·
// 前端可画蜡烛图（min-max 区间 + avg 点）。
func (s *Server) handleVendorPricesDaily(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return vendorViewUnavailable()
	}
	if _, err := mustCaller(r); err != nil {
		return err
	}
	id := r.PathValue("vendor_id")
	if id == "" {
		return ErrBadRequest("缺少 vendor_id")
	}
	days := atoiDefault(r.URL.Query().Get("days"), 30)
	// 从 anon_id 还原 vendor_id · vendorView 有这个能力
	realVendorID, ok := s.vendorView.ResolveAnonID(id)
	if !ok {
		return ErrNotFound("找不到这家 vendor")
	}
	points, err := s.vendorView.DailyPrices(r.Context(), realVendorID, days)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"vendor_id": id, // 对外返 anon_id · 不下发真名
		"days":      days,
		"points":    points,
	})
	return nil
}

// GET /api/vendors/{vendor_id}/history
func (s *Server) handleVendorHistory(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return vendorViewUnavailable()
	}
	if _, err := mustCaller(r); err != nil {
		return err
	}
	id := r.PathValue("vendor_id")
	if id == "" {
		return ErrBadRequest("缺少 vendor_id")
	}
	out, err := s.vendorView.History(r.Context(), id)
	if errors.Is(err, vendorview.ErrVendorNotFound) {
		return ErrNotFound("找不到这家 vendor")
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// GET /api/vendors/stats
func (s *Server) handleVendorsStats(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return vendorViewUnavailable()
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	out := s.vendorView.Stats(r.Context(), viewerOf(p, r))
	writeJSON(w, http.StatusOK, out)
	return nil
}

// GET /api/vendors/offers · Offer matrix（docs/24 §3 · Step 4）
//
// 前端提取页拿这一份数据同时决定:
//   - category tab 数字（每档合计 available）
//   - vendor 下拉可选项 · supported/available 分离
//   - subscription 下拉合法档位
//   - 数量分档单价（前端预估）
//
// 老 /vendors/stats + /vendors/{id}/stock + /vendors/auto-pick 会互相漂移 · 现在统一走这个。
func (s *Server) handleVendorsOffers(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return vendorViewUnavailable()
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	out := s.vendorView.Offers(r.Context(), viewerOf(p, r))
	writeJSON(w, http.StatusOK, out)
	return nil
}

// GET /api/vendors/auto-pick?zone=us|eu|auto
func (s *Server) handleVendorsAutoPick(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return vendorViewUnavailable()
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	zone := r.URL.Query().Get("zone")
	out := s.vendorView.AutoPick(r.Context(), zone, viewerOf(p, r))
	writeJSON(w, http.StatusOK, out)
	return nil
}

// vendorViewUnavailable 装配阶段未注入 Service 时的兜底 —— 跟 pull.go 里 decider nil 的处理一致。
func vendorViewUnavailable() *Fail {
	return newFail(http.StatusServiceUnavailable, CodeInternal,
		"vendor 视图暂未装配（阶段 1a 部分部署）")
}
