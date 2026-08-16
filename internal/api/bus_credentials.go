package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/bus"
)

// GET /api/me/buses/{bus_id}/credentials · Credential[] · 前端 TS 契约
// GET /api/me/buses/{bus_id}/pulls · PullRound[]

type credDTO struct {
	ID              string  `json:"id"`
	VendorID        string  `json:"vendor_id"`
	Status          string  `json:"status"` // alive | dead
	KeyMasked       string  `json:"key_masked"`
	Account         string  `json:"account"`
	Region          string  `json:"region"`
	IssuerURL       string  `json:"issuer_url"`
	CreditsUsed     int64   `json:"credits_used"`
	PulledAt        string  `json:"pulled_at"`
	WarrantyUntil   *string `json:"warranty_until"`
	DeadAt          *string `json:"dead_at"`
	LifespanSeconds int64   `json:"lifespan_seconds"`
	Paid            int64   `json:"paid"`
	OwnerBusID      *string `json:"owner_bus_id"`
	PushedAt        *string `json:"pushed_at"`
	PushFailed      bool    `json:"push_failed"`
	PushError       any     `json:"push_error"`
	// Offer 维度 + 用量真值（跟 /me/pull-records 同源 · 别让两个页面对同一个号说不同的话）
	AccountKind  string `json:"account_kind,omitempty"`
	Subscription string `json:"subscription,omitempty"`
	UsageLimit   int64  `json:"usage_limit"`
	UsageCurrent int64  `json:"usage_current"`
}

func (s *Server) handleBusCredentials(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	if _, err := s.buses.GetForPassenger(r.Context(), busID, p.ID); err != nil {
		if errors.Is(err, bus.ErrNotFound) || errors.Is(err, bus.ErrNotMember) {
			return ErrNotFound("找不到这辆车")
		}
		return err
	}

	// 推送态 / 用量 / masked 都查真值 —— 之前只查 6 列 · masked 拿行 id 拼假的 ·
	// 推送态根本不查 → 车详情"Push log"永远空 · Push 列永远 "Not pushed"（实测生产:
	// DB 里 pushed_to_passengerpool_at 有值 · 接口返 null）。
	// 用量 LEFT JOIN 最近一条快照 · 跟 /me/pull-records 同一套口径。
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT cl.id, COALESCE(cl.vendor_id, ''), cl.status,
		       COALESCE(cl.key_masked, ''),
		       COALESCE(cl.region, ''),
		       COALESCE(cl.credits_used, 0),
		       COALESCE(cl.pulled_at, ''), cl.warranty_until, cl.dead_at,
		       cl.pushed_to_passengerpool_at,
		       COALESCE(cl.push_error_code, ''),
		       COALESCE(cl.push_error_message, ''),
		       COALESCE(cl.account_kind, ''),
		       COALESCE(cl.subscription, ''),
		       COALESCE(cus.usage_limit_micro, 0),
		       COALESCE(cus.current_usage_micro, 0)
		  FROM credential_ledger cl
		  LEFT JOIN credential_usage_snapshot cus
		    ON cus.kiro_rs_credential_id = cl.kiro_rs_credential_id
		   AND cus.observed_at = (
		         SELECT MAX(observed_at) FROM credential_usage_snapshot
		          WHERE kiro_rs_credential_id = cl.kiro_rs_credential_id
		       )
		 WHERE cl.owner_bus_id = ?
		 ORDER BY cl.pulled_at DESC`, busID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]credDTO, 0)
	for rows.Next() {
		var c credDTO
		var warranty, deadAt, pushedAt sql.NullString
		var pushErrCode, pushErrMsg string
		if err := rows.Scan(
			&c.ID, &c.VendorID, &c.Status, &c.KeyMasked, &c.Region, &c.CreditsUsed,
			&c.PulledAt, &warranty, &deadAt,
			&pushedAt, &pushErrCode, &pushErrMsg,
			&c.AccountKind, &c.Subscription, &c.UsageLimit, &c.UsageCurrent,
		); err != nil {
			return err
		}
		if warranty.Valid {
			s := warranty.String
			c.WarrantyUntil = &s
		}
		if deadAt.Valid {
			s := deadAt.String
			c.DeadAt = &s
		}
		if pushedAt.Valid && pushedAt.String != "" {
			s := pushedAt.String
			c.PushedAt = &s
		}
		// 推送失败 = 有错误码且还没推成功（推成功后错误码留着当历史 · 不该再报红）
		if pushErrCode != "" && c.PushedAt == nil {
			c.PushFailed = true
			if pushErrMsg != "" {
				c.PushError = pushErrMsg
			} else {
				c.PushError = pushErrCode
			}
		}
		// 寿命:死了算到 dead_at · 活着算到现在（前端"存活 Nh"要真值）
		if c.PulledAt != "" {
			if t0, err := time.Parse(time.RFC3339, c.PulledAt); err == nil {
				end := time.Now().UTC()
				if deadAt.Valid && deadAt.String != "" {
					if t1, err := time.Parse(time.RFC3339, deadAt.String); err == nil {
						end = t1
					}
				}
				if d := end.Sub(t0); d > 0 {
					c.LifespanSeconds = int64(d.Seconds())
				}
			}
		}
		bid := busID
		c.OwnerBusID = &bid
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, items)
	return nil
}

type busPullDTO struct {
	ID             string  `json:"id"`
	VendorID       string  `json:"vendor_id"`
	BusID          *string `json:"bus_id"`
	BusName        *string `json:"bus_name"`
	Result         string  `json:"result"`
	CountRequested int     `json:"count_requested"`
	CountPurchased int     `json:"count_purchased"`
	AliveCount     int     `json:"alive_count"`
	DeadCount      int     `json:"dead_count"`
	PushState      string  `json:"push_state"`
	PushRatio      *string `json:"push_ratio"`
	TotalCost      int64   `json:"total_cost"`
	FailReason     *string `json:"fail_reason"`
	CreatedAt      string  `json:"created_at"`
}

func (s *Server) handleBusPulls(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	if _, err := s.buses.GetForPassenger(r.Context(), busID, p.ID); err != nil {
		if errors.Is(err, bus.ErrNotFound) || errors.Is(err, bus.ErrNotMember) {
			return ErrNotFound("找不到这辆车")
		}
		return err
	}

	// 直拉本车的轮次(pr.bus_id = 本车) OR 产出了本车号的轮次(号 owner_bus_id = 本车)都算 ——
	// 提取页拉的号再"进车"时 · 那一轮 pull_round.bus_id 是空的（拉时还没归属车）· 只按
	// pr.bus_id 过滤会让这类轮次永远不匹配 → 列表恒空 · 跟"今日花费恒 0"是同一个坑
	// （见 busCredStats 的回溯写法）。IN 子查询里 DISTINCT · 外层一轮一行不会翻倍。
	//
	// alive/dead/推送计数都额外收口到 owner_bus_id = 本车 —— 只数落进本车的那部分号 ·
	// 拆分轮次不会把派去别处的号也算进来（直拉轮次整轮都在本车 · 不受影响）。
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT pr.id, pr.vendor_id, pr.bus_id, pr.count_requested, pr.count_purchased,
		       pr.key_cost_total + pr.vendor_fee_total + pr.region_fee_total +
		       pr.single_pull_fee_total + pr.capability_fee_total + pr.service_fee_total AS total_cost,
		       pr.status, pr.created_at,
		       (SELECT COUNT(1) FROM credential_ledger
		         WHERE source_pull_round_id = pr.id AND owner_bus_id = ? AND status = 'alive'),
		       (SELECT COUNT(1) FROM credential_ledger
		         WHERE source_pull_round_id = pr.id AND owner_bus_id = ? AND status = 'dead'),
		       (SELECT COUNT(1) FROM credential_ledger
		         WHERE source_pull_round_id = pr.id AND owner_bus_id = ?),
		       (SELECT COUNT(1) FROM credential_ledger
		         WHERE source_pull_round_id = pr.id AND owner_bus_id = ?
		           AND pushed_to_passengerpool_at IS NOT NULL),
		       (SELECT COUNT(1) FROM credential_ledger
		         WHERE source_pull_round_id = pr.id AND owner_bus_id = ?
		           AND push_error_code IS NOT NULL AND pushed_to_passengerpool_at IS NULL)
		  FROM pull_round pr
		 WHERE pr.bus_id = ?
		    OR pr.id IN (
		         SELECT DISTINCT source_pull_round_id FROM credential_ledger
		          WHERE owner_bus_id = ? AND source_pull_round_id IS NOT NULL)
		 ORDER BY pr.created_at DESC`,
		busID, busID, busID, busID, busID, busID, busID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]busPullDTO, 0)
	for rows.Next() {
		var p busPullDTO
		var busIDCol sql.NullString
		var internalStatus string
		var totalCost int64
		var ownCount, pushedCount, pushFailCount int
		if err := rows.Scan(&p.ID, &p.VendorID, &busIDCol,
			&p.CountRequested, &p.CountPurchased, &totalCost,
			&internalStatus, &p.CreatedAt, &p.AliveCount, &p.DeadCount,
			&ownCount, &pushedCount, &pushFailCount); err != nil {
			return err
		}
		if busIDCol.Valid {
			s := busIDCol.String
			p.BusID = &s
		}
		p.TotalCost = -totalCost
		p.Result = mapPullRoundResult(internalStatus, p.CountRequested, p.CountPurchased)
		// 推送态按本车从该轮拿到的号算真值：全推成功=pushed · 部分=partial(带比例) ·
		// 一个没推成但有推送错误=failed · 没动过=none（推 passengerpool 双写见 1e）。
		switch {
		case ownCount == 0 || (pushedCount == 0 && pushFailCount == 0):
			p.PushState = "none"
		case pushedCount >= ownCount:
			p.PushState = "pushed"
		case pushedCount > 0:
			p.PushState = "partial"
			ratio := fmt.Sprintf("%d/%d", pushedCount, ownCount)
			p.PushRatio = &ratio
		default:
			p.PushState = "failed"
		}
		items = append(items, p)
	}
	writeJSON(w, http.StatusOK, items)
	return nil
}
