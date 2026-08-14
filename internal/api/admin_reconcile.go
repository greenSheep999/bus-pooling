package api

// admin_reconcile · 对账 dashboard 的 HTTP 入口
//
// **背景**：上线后运维要看 wallet_ledger 跟 vendor_ledger 有没有对上 ·
// 老代码只在 CLI 有 `bus-pooling reconcile` · 后台看不见。这里挂个只读 HTTP 端点。
//
// **纯只读**：跟现有 Reconciler 一样只查 · 不改任何表。BP_ADMIN_KEY 头校验 · 绝不给前端。
//
// **参数**：?since_days=N（默认 30 · 上限 90 · 太久跑不动）
//
// **响应**：Reconciler.Reconcile 的原生结果 · 每条差异含 pull_round_id + kind + amount。

import (
	"net/http"
	"strconv"
)

type adminReconcileResp struct {
	OK           bool                            `json:"ok"`
	SinceDays    int                             `json:"since_days"`
	Summary      adminReconcileSummary           `json:"summary"`
	Discrepancies []adminReconcileDiscrepancyRow `json:"discrepancies"`
}

type adminReconcileSummary struct {
	RoundsChecked int            `json:"rounds_checked"`
	Discrepancies int            `json:"discrepancies"`
	ByKind        map[string]int `json:"by_kind"`
}

type adminReconcileDiscrepancyRow struct {
	PullRoundID string `json:"pull_round_id"`
	VendorID    string `json:"vendor_id"`
	Kind        string `json:"kind"`
	OursMicro   int64  `json:"ours_micro"`
	VendorMicro int64  `json:"vendor_micro"`
	DiffMicro   int64  `json:"diff_micro"`
	Detail      string `json:"detail,omitempty"`
}

func (s *Server) handleAdminReconcile(w http.ResponseWriter, r *http.Request) error {
	if s.reconciler == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "对账器未装配")
	}
	sinceDays := 30
	if v := r.URL.Query().Get("since_days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 90 {
				n = 90
			}
			sinceDays = n
		}
	}
	discs, summary, err := s.reconciler.Reconcile(r.Context(), sinceDays)
	if err != nil {
		return err
	}
	rows := make([]adminReconcileDiscrepancyRow, 0, len(discs))
	for _, d := range discs {
		rows = append(rows, adminReconcileDiscrepancyRow{
			PullRoundID: d.RoundID,
			VendorID:    d.VendorID,
			Kind:        d.Kind,
			OursMicro:   d.OursMicro,
			VendorMicro: d.VendorMicro,
			DiffMicro:   d.VendorMicro - d.OursMicro,
			Detail:      d.Detail,
		})
	}
	writeJSON(w, http.StatusOK, adminReconcileResp{
		OK:        true,
		SinceDays: sinceDays,
		Summary: adminReconcileSummary{
			RoundsChecked: summary.RoundsChecked,
			Discrepancies: summary.Discrepancies,
			ByKind:        summary.ByKind,
		},
		Discrepancies: rows,
	})
	return nil
}
