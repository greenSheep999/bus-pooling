package api

// 乘客对账页 · GET /api/me/history-summary
//
// **给乘客看的**（不是 admin · 但只显示自己的）：
//   "我买过多少号 · 死了多少 · 退了多少"
//
// 数据全从 credential_ledger + wallet_ledger 汇总 · 不打 vendor 端点。
//
// **脱敏**：不出 vendor 真名 · 用 anon_id / anon_label（跟 status 页一致）·
// 乘客看到的是"AWS-Q Kiro Vendor 01 花了 X 积分"· 不是真 vendor id。

import (
	"context"
	"database/sql"
	"net/http"
)

type meHistorySummaryResp struct {
	OK              bool                       `json:"ok"`
	TotalKeys       int                        `json:"total_keys"`        // 我买过的总号数（含死）
	AliveKeys       int                        `json:"alive_keys"`
	DeadKeys        int                        `json:"dead_keys"`
	TotalSpentMicro int64                      `json:"total_spent_micro"` // 我总共花过（含退回来的）· 积分 microunit
	TotalRefundMicro int64                     `json:"total_refund_micro"` // 我从退款里收回来的
	ByVendor        []meHistorySummaryVendor   `json:"by_vendor"`
}

type meHistorySummaryVendor struct {
	// 脱敏字段（跟 status 页一致 · 不出真名）
	AnonID    string `json:"anon_id"`
	AnonLabel string `json:"anon_label"`

	TotalKeys       int   `json:"total_keys"`
	AliveKeys       int   `json:"alive_keys"`
	DeadKeys        int   `json:"dead_keys"`
	SpentMicro      int64 `json:"spent_micro"`
	RefundMicro     int64 `json:"refund_micro"`
}

func (s *Server) handleMeHistorySummary(w http.ResponseWriter, r *http.Request) error {
	p, ok := callerFrom(r.Context())
	if !ok {
		return newFail(http.StatusUnauthorized, CodeUnauthenticated, "未登录")
	}
	pid := p.ID
	if s.db == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "db 未装配")
	}
	if s.vendorView == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "vendor 服务未装配")
	}

	resp, err := loadMeHistorySummary(r.Context(), s.db, pid, s.vendorView)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

// loadMeHistorySummary · 从 credential_ledger + wallet_ledger 聚合
func loadMeHistorySummary(ctx context.Context, db *sql.DB, passengerID string, vv anonResolver) (*meHistorySummaryResp, error) {
	resp := &meHistorySummaryResp{OK: true}

	// 1. 从 credential_ledger 拿我拥有的号 · 按 vendor 分组
	// credential_ledger 记的是分给我的号（owner_record_passenger_id 或经 bus_member 关联）
	// 简化：这里只统计 owner_record_passenger_id = 我的 · bus 内的号需另外 join
	// 首版：只统计 record group（单独拉的号）· bus 的号  再补
	credRows, err := db.QueryContext(ctx, `
		SELECT pr.vendor_id, cl.status,
		       COALESCE(pr.key_cost_total / NULLIF(pr.count_purchased, 0), 0) AS cost_per_key
		  FROM credential_ledger cl
		  JOIN pull_round pr ON pr.id = cl.source_pull_round_id
		 WHERE cl.owner_record_passenger_id = ?`, passengerID)
	if err != nil {
		return nil, err
	}
	defer credRows.Close()

	type stat struct {
		total, alive, dead int
		spent              int64
	}
	byVid := make(map[string]*stat)
	for credRows.Next() {
		var vid, status string
		var costPerKey int64
		if err := credRows.Scan(&vid, &status, &costPerKey); err != nil {
			return nil, err
		}
		s := byVid[vid]
		if s == nil {
			s = &stat{}
			byVid[vid] = s
		}
		s.total++
		s.spent += costPerKey
		switch status {
		case "alive":
			s.alive++
		case "dead":
			s.dead++
		}
		resp.TotalKeys++
		resp.TotalSpentMicro += costPerKey
		if status == "alive" {
			resp.AliveKeys++
		} else if status == "dead" {
			resp.DeadKeys++
		}
	}
	if err := credRows.Err(); err != nil {
		return nil, err
	}

	// 2. 从 wallet_ledger 拿 warranty_refund 类型的入账 · 按 vendor 分不了（wallet_ledger 无 vendor_id）·
	// 只算总数（前端展示"总退款"）
	err = db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		  FROM wallet_ledger
		 WHERE passenger_id = ? AND reason = 'warranty_refund'`,
		passengerID).Scan(&resp.TotalRefundMicro)
	if err != nil {
		return nil, err
	}

	// 3. 组装脱敏 by_vendor 列表
	for vid, s := range byVid {
		resp.ByVendor = append(resp.ByVendor, meHistorySummaryVendor{
			AnonID:     vv.AnonIDFor(vid),
			AnonLabel:  vv.AnonLabelFor(vid),
			TotalKeys:  s.total,
			AliveKeys:  s.alive,
			DeadKeys:   s.dead,
			SpentMicro: s.spent,
			// RefundMicro · 按 vendor 拆分需要 wallet_ledger.vendor_id 冗余列（不做）·
			// 首版返 0 · 前端只显示总量
			RefundMicro: 0,
		})
	}
	return resp, nil
}

// anonResolver · 让 loadMeHistorySummary 可 mock
type anonResolver interface {
	AnonIDFor(vendorID string) string
	AnonLabelFor(vendorID string) string
}
