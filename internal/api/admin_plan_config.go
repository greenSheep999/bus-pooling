// admin_plan_config.go · I-29 · 运营改 vendor 档位开关(vendor_plan_config)不改 SQL。
//
// migration 049 建了表 · Store.UpsertPlan/ListAll 有 · 但缺 admin API 面向后台。
// 遵循 CLAUDE "费率 / 开关不写代码"铁律。
package api

import (
	"encoding/json"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// adminPlanConfigRow · GET 响应行
type adminPlanConfigRow struct {
	VendorID     string `json:"vendor_id"`
	AccountKind  string `json:"account_kind"`
	Subscription string `json:"subscription"`
	Enabled      bool   `json:"enabled"`
	UpdatedAt    string `json:"updated_at"`
}

// handleAdminListPlanConfig · GET /api/admin/vendor-plan-config
//
// 返所有 vendor × kind × plan 的开关状态 · 供后台渲染管理 UI。
func (s *Server) handleAdminListPlanConfig(w http.ResponseWriter, r *http.Request) error {
	if s.planConfigStore == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal,
			"vendor_plan_config 未装配")
	}
	rows, err := s.planConfigStore.ListAll(r.Context())
	if err != nil {
		return err
	}
	out := make([]adminPlanConfigRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, adminPlanConfigRow{
			VendorID: r.VendorID, AccountKind: r.AccountKind, Subscription: r.Subscription,
			Enabled: r.Enabled, UpdatedAt: r.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": out})
	return nil
}

// adminPlanConfigUpsertReq · PUT 请求体
type adminPlanConfigUpsertReq struct {
	VendorID     string `json:"vendor_id"`
	AccountKind  string `json:"account_kind"`  // enterprise | personal
	Subscription string `json:"subscription"`  // power | pro | pro_plus | pro_max
	Enabled      bool   `json:"enabled"`
}

// handleAdminUpsertPlanConfig · PUT /api/admin/vendor-plan-config
//
// upsert 一条 (vendor_id, account_kind, subscription) → enabled 开关。
// 幂等 · 重复调用只更新时间戳。
func (s *Server) handleAdminUpsertPlanConfig(w http.ResponseWriter, r *http.Request) error {
	if s.planConfigStore == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal,
			"vendor_plan_config 未装配")
	}
	body, err := readBody(r)
	if err != nil {
		return err
	}
	var req adminPlanConfigUpsertReq
	if err := json.Unmarshal(body, &req); err != nil {
		return ErrBadRequest("body 不是合法 JSON")
	}
	if req.VendorID == "" || req.AccountKind == "" || req.Subscription == "" {
		return ErrBadRequest("vendor_id / account_kind / subscription 都不能空")
	}
	kind := providers.AccountKind(req.AccountKind)
	plan := providers.SubscriptionPlan(req.Subscription)
	if err := s.planConfigStore.UpsertPlan(r.Context(), req.VendorID, kind, plan, req.Enabled); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}
