package api

import (
	"net/http"
	"time"
)

// data-health · 运维视角的数据管线新鲜度自检（migration 036 · pipeline_health）。
//
// **仅运维**：BP_ADMIN_KEY 非空才挂路由 · 请求要带 `X-Admin-Key` 匹配。绝不给乘客前端 ·
// 这里可以出现 vendor_id / pipeline 名（运维要看的就是"哪条管线停更了"）。
//
// 把 /healthz 的"HTTP 活着"升级到"数据在更新"：每条采集管线每轮盖戳 last_ok_at ·
// 本端点对比"现在 - last_ok_at"和该管线阈值 · 超了标 stale。

type dataHealthRow struct {
	Vendor    string `json:"vendor"`
	Pipeline  string `json:"pipeline"`
	Stale     bool   `json:"stale"`
	LastOKAt  string `json:"last_ok_at,omitempty"`
	LastOKAgo string `json:"last_ok_ago,omitempty"`
	LastErr   string `json:"last_err,omitempty"`
	LastErrAt string `json:"last_err_at,omitempty"`
}

type dataHealthResp struct {
	OK         bool            `json:"ok"` // 全部新鲜 = true
	StaleCount int             `json:"stale_count"`
	Total      int             `json:"total"`
	CheckedAt  string          `json:"checked_at"`
	Pipelines  []dataHealthRow `json:"pipelines"`
}

// requireAdmin · BP_ADMIN_KEY 头校验（简单共享密钥 · 非登录系统）。
func (s *Server) requireAdmin(next handler) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		if s.adminKey == "" || r.Header.Get("X-Admin-Key") != s.adminKey {
			return newFail(http.StatusUnauthorized, CodeUnauthenticated, "管理员校验失败")
		}
		return next(w, r)
	}
}

func (s *Server) handleDataHealth(w http.ResponseWriter, r *http.Request) error {
	if s.health == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "健康心跳未装配")
	}
	rows, err := s.health.Report(r.Context())
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	out := dataHealthResp{OK: true, Total: len(rows), CheckedAt: now.Format(time.RFC3339)}
	for _, rr := range rows {
		stale := rr.Stale(now)
		vendor := rr.VendorID
		if vendor == "" {
			vendor = "(global)"
		}
		row := dataHealthRow{Vendor: vendor, Pipeline: rr.Pipeline, Stale: stale, LastErr: rr.LastErr}
		if !rr.LastOKAt.IsZero() {
			row.LastOKAt = rr.LastOKAt.Format(time.RFC3339)
			row.LastOKAgo = now.Sub(rr.LastOKAt).Round(time.Second).String()
		}
		if !rr.LastErrAt.IsZero() {
			row.LastErrAt = rr.LastErrAt.Format(time.RFC3339)
		}
		if stale {
			out.StaleCount++
			out.OK = false
		}
		out.Pipelines = append(out.Pipelines, row)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}
