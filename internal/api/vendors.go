package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/passenger"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
)

// vendors 只读端点。**要鉴权** —— 单价按调用者身份差异化（decisions §8.20），
// 拿不到身份就没法定价。

// TODO(assembly): s.vendorView 需要在 ServerDeps 加：
//   type: *vendorview.Service
//   来源: vendorview.New(vendorview.Config{Registry, Rates}) （main.go）

// viewerOf 把 passenger 翻译成 vendorview.Viewer。
//
// coupon_code 阶段 1a 简化：**永远 false**（暂不接优惠码逻辑；文档已注明 1a 待接）。
// Invited 直接来自 passenger.Invited（注册时是否填了邀请码 · decisions §8.29）。
func viewerOf(p *passenger.Passenger, r *http.Request) vendorview.Viewer {
	_ = r
	return vendorview.Viewer{
		Invited:     p != nil && p.Invited,
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
	out := s.vendorView.StatusOverview(r.Context())
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
