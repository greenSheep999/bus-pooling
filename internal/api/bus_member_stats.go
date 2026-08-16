package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/bus-pooling/bus-pooling/internal/bus"
)

// 车的成员维度统计（decisions §8.19 · 1c 多人拼车落地后开放）。
//
// 数据源全部是已有字段·不新建表：
//   - pull_round.participants_split_json  → {passenger_id: 号数}·谁分了几个号
//   - credential_ledger.owner_bus_id      → 这辆车的号 · 存活 / 推送状态
//   - bus_member.share_pct                → 分摊比例
//
// **不返内部字段**（CLAUDE.md §0.1）：不出 participants_split_json 原文 / 不出计费分项·
// 只给算好的结果（号数 / 积分 / 占比）。

type memberStatDTO struct {
	PassengerID string `json:"passenger_id"`
	Username    string `json:"username"`
	Role        string `json:"role"` // owner | member
	SharePct    int    `json:"share_pct"`
	Status      string `json:"status"` // active | suspended

	// 拉号参与：这个成员在这辆车里分到过多少号 · 参与了几轮
	KeysTaken    int `json:"keys_taken"`
	RoundsJoined int `json:"rounds_joined"`

	// 消费：按 participants_split 里的号数占比摊到的积分（microunit · 正数表示花掉多少）
	SpendTotal int64 `json:"spend_total"`

	// 推自己号池的成功率 · 1e 双写落地后才有非零值
	PushedOK   int `json:"pushed_ok"`
	PushFailed int `json:"push_failed"`
}

type busMemberStatsResp struct {
	Members []memberStatDTO `json:"members"`
	// TotalKeys / TotalSpend 给前端算占比用（省一次遍历 · 也保证跟后端口径一致）
	TotalKeys  int   `json:"total_keys"`
	TotalSpend int64 `json:"total_spend"`
}

// handleBusMemberStats · GET /api/me/buses/{bus_id}/member-stats
//
// 只有车成员能看（GetForPassenger 已做归属校验 · 非成员返 404 不泄漏车存在）。
func (s *Server) handleBusMemberStats(w http.ResponseWriter, r *http.Request) error {
	caller, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	if _, err := s.buses.GetForPassenger(r.Context(), busID, caller.ID); err != nil {
		if errors.Is(err, bus.ErrNotFound) || errors.Is(err, bus.ErrNotMember) {
			return ErrNotFound("找不到这辆车")
		}
		return err
	}

	members, err := s.buses.Members(r.Context(), busID)
	if err != nil {
		return err
	}

	// 先把成员骨架建好 · 没参与过拉号的成员也要出现在结果里（0 值）
	byID := make(map[string]*memberStatDTO, len(members))
	out := make([]memberStatDTO, 0, len(members))
	for _, m := range members {
		username, _, err := s.passengerBriefFor(r, m.PassengerID)
		if err != nil {
			return err
		}
		out = append(out, memberStatDTO{
			PassengerID: m.PassengerID,
			Username:    username,
			Role:        m.Role,
			SharePct:    m.SharePct,
			Status:      m.Status,
		})
	}
	for i := range out {
		byID[out[i].PassengerID] = &out[i]
	}

	// 逐轮读 participants_split_json · 按号数占比摊消费
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT pr.participants_split_json,
		       pr.key_cost_total + pr.vendor_fee_total + pr.region_fee_total +
		       pr.single_pull_fee_total + pr.capability_fee_total + pr.service_fee_total AS total_cost,
		       pr.count_purchased
		  FROM pull_round pr
		 WHERE pr.bus_id = ? AND pr.status IN ('completed', 'partial')`, busID)
	if err != nil {
		return err
	}
	defer rows.Close()

	totalKeys := 0
	var totalSpend int64
	for rows.Next() {
		var splitJSON string
		var roundCost int64
		var countPurchased int
		if err := rows.Scan(&splitJSON, &roundCost, &countPurchased); err != nil {
			return err
		}
		var split map[string]int
		if err := json.Unmarshal([]byte(splitJSON), &split); err != nil {
			// 脏数据不该让整个接口挂 · 跳过这轮
			continue
		}
		// 这轮实际分出去的号数（用 split 之和·而不是 count_purchased ——
		// partial 轮里两者可能不等·按实际分配算才对得上钱）
		roundKeys := 0
		for _, n := range split {
			roundKeys += n
		}
		if roundKeys <= 0 {
			continue
		}
		for pid, n := range split {
			m, ok := byID[pid]
			if !ok {
				// 已退出的成员（bus_member.left_at 非空）不在 Members() 里 ·
				// 他的历史消费不计入当前成员视图
				continue
			}
			m.KeysTaken += n
			m.RoundsJoined++
			// 按号数占比摊这轮的钱 · 整数除法·余数留给最后一个（差 1 microunit 无所谓）
			m.SpendTotal += roundCost * int64(n) / int64(roundKeys)
		}
		totalKeys += roundKeys
		totalSpend += roundCost
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// 推送成功 / 失败按成员归属统计（1e 双写落地后才有非零）
	pushRows, err := s.db.QueryContext(r.Context(), `
		SELECT cl.owner_record_passenger_id,
		       SUM(CASE WHEN cl.pushed_to_passengerpool_at IS NOT NULL THEN 1 ELSE 0 END),
		       SUM(CASE WHEN cl.push_error_code IS NOT NULL THEN 1 ELSE 0 END)
		  FROM credential_ledger cl
		 WHERE cl.owner_bus_id = ? AND cl.owner_record_passenger_id IS NOT NULL
		 GROUP BY cl.owner_record_passenger_id`, busID)
	if err == nil {
		defer pushRows.Close()
		for pushRows.Next() {
			var pid string
			var ok, failed int
			if err := pushRows.Scan(&pid, &ok, &failed); err != nil {
				continue
			}
			if m, exists := byID[pid]; exists {
				m.PushedOK = ok
				m.PushFailed = failed
			}
		}
	}

	// owner 排前面 · 然后按分到的号数降序（看谁用得多一目了然）
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Role == "owner") != (out[j].Role == "owner") {
			return out[i].Role == "owner"
		}
		return out[i].KeysTaken > out[j].KeysTaken
	})

	writeJSON(w, http.StatusOK, busMemberStatsResp{
		Members:    out,
		TotalKeys:  totalKeys,
		TotalSpend: totalSpend,
	})
	return nil
}
