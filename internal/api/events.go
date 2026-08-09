package api

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
)

// GET /api/me/pull/events · 提取历史（每次拉号一条 · TS ExtractEvent）
// GET /api/me/assign/events · 派发历史（每次派动作一条 · TS AssignEvent）
//
// 数据来源：pull_round + pending_assignment。1a 阶段返回真实统计但不做复杂 join
// 详情 · 只给前端画列表要的字段。

type extractEventDTO struct {
	ID             string  `json:"id"`
	CreatedAt      string  `json:"created_at"`
	VendorID       string  `json:"vendor_id"`
	Zone           *string `json:"zone"`
	CountRequested int     `json:"count_requested"`
	CountPurchased int     `json:"count_purchased"`
	TotalCost      int64   `json:"total_cost"`
	Result         string  `json:"result"` // success | partial | failed | refunded
	FailReason     *string `json:"fail_reason"`
	AssignedCount  int     `json:"assigned_count"`
	PendingCount   int     `json:"pending_count"`
}

func (s *Server) handleListPullEvents(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	page, pageSize := parsePaging(r)

	// 数据源：pull_round（我方发起的拉号）· 通过 pending_purchase 关到 passenger
	// 用 join，SQL 简单也快
	total, items, err := listPullEvents(r.Context(), s.db, p.ID, pageSize, (page-1)*pageSize)
	if err != nil {
		return err
	}
	pages := 0
	if total > 0 {
		pages = (total + pageSize - 1) / pageSize
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
	})
	return nil
}

func listPullEvents(ctx context.Context, db *sql.DB, passengerID string, limit, offset int) (int, []extractEventDTO, error) {
	var total int
	if err := db.QueryRowContext(ctx, `
		SELECT count(1) FROM pull_round pr
		 WHERE pr.id IN (SELECT pull_round_id FROM pending_purchase WHERE passenger_id = ? AND pull_round_id IS NOT NULL)
		`, passengerID).Scan(&total); err != nil {
		return 0, nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT pr.id, pr.created_at, pr.vendor_id,
		       pr.count_requested, pr.count_purchased,
		       pr.key_cost_total + pr.vendor_fee_total + pr.region_fee_total +
		       pr.single_pull_fee_total + pr.capability_fee_total + pr.service_fee_total AS total_cost,
		       pr.status
		  FROM pull_round pr
		 WHERE pr.id IN (SELECT pull_round_id FROM pending_purchase WHERE passenger_id = ? AND pull_round_id IS NOT NULL)
		 ORDER BY pr.created_at DESC
		 LIMIT ? OFFSET ?`, passengerID, limit, offset)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := make([]extractEventDTO, 0)
	for rows.Next() {
		var e extractEventDTO
		var internalStatus string
		var totalCost int64
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.VendorID,
			&e.CountRequested, &e.CountPurchased, &totalCost, &internalStatus); err != nil {
			return 0, nil, err
		}
		// TotalCost 用**负数**表示扣款（跟前端 activities 的 amount 一致）
		e.TotalCost = -totalCost
		// 内部 status → 对外 result（§12.5 收敛）
		e.Result = mapPullRoundResult(internalStatus, e.CountRequested, e.CountPurchased)
		items = append(items, e)
	}
	return total, items, rows.Err()
}

// mapPullRoundResult 内部 pull_round.status → 对外 result
func mapPullRoundResult(status string, requested, purchased int) string {
	switch status {
	case "completed":
		if purchased < requested {
			return "partial"
		}
		return "success"
	case "refunded":
		return "refunded"
	case "failed":
		return "failed"
	case "partial":
		return "partial"
	default:
		return "failed"
	}
}

// ── /me/assign/events ───────────────────────────────

type assignEventDTO struct {
	ID          string           `json:"id"`
	CreatedAt   string           `json:"created_at"`
	Destination string           `json:"destination"`
	BusID       *string          `json:"bus_id"`
	BusName     *string          `json:"bus_name"`
	Count       int              `json:"count"`
	Keys        []assignedKeyDTO `json:"keys"` // 前端 AssignedKey[] · TS 契约为准
	TargetHost  *string          `json:"target_host"`
	Vendors     []string         `json:"vendors"`
}

// assignedKeyDTO 对齐前端 AssignedKey · web/src/types/index.ts:294。
// key_masked 派发那一刻的打码 · credits_used / lifespan_seconds 派发瞬时快照。
type assignedKeyDTO struct {
	CredentialID    string `json:"credential_id"`
	KeyMasked       string `json:"key_masked"`
	VendorID        string `json:"vendor_id"`
	Region          string `json:"region"`
	CreditsUsed     int64  `json:"credits_used"`
	LifespanSeconds int64  `json:"lifespan_seconds"`
}

func (s *Server) handleListAssignEvents(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	page, pageSize := parsePaging(r)

	total, items, err := listAssignEvents(r.Context(), s.db, p.ID, pageSize, (page-1)*pageSize)
	if err != nil {
		return err
	}
	pages := 0
	if total > 0 {
		pages = (total + pageSize - 1) / pageSize
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
	})
	return nil
}

func listAssignEvents(ctx context.Context, db *sql.DB, passengerID string, limit, offset int) (int, []assignEventDTO, error) {
	// pending_assignment 表 · target_bus_id / target 用来分组
	var total int
	if err := db.QueryRowContext(ctx, `
		SELECT count(1) FROM pending_assignment WHERE passenger_id = ? AND status = 'completed'`,
		passengerID).Scan(&total); err != nil {
		return 0, nil, err
	}
	// 拉 assign 记录 + 关联的 credential 快照
	rows, err := db.QueryContext(ctx, `
		SELECT pa.id, pa.created_at, pa.target, pa.target_bus_id, pa.credential_id,
		       cl.vendor_id, COALESCE(cl.pulled_at, ''),
		       COALESCE(b.name, '')
		  FROM pending_assignment pa
		  LEFT JOIN credential_ledger cl ON cl.id = pa.credential_id
		  LEFT JOIN bus b ON b.id = pa.target_bus_id
		 WHERE pa.passenger_id = ? AND pa.status = 'completed'
		 ORDER BY pa.created_at DESC
		 LIMIT ? OFFSET ?`, passengerID, limit, offset)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items := make([]assignEventDTO, 0)
	for rows.Next() {
		var e assignEventDTO
		var target string
		var targetBusID sql.NullString
		var credID, vendorID, pulledAt, busName string
		if err := rows.Scan(&e.ID, &e.CreatedAt, &target, &targetBusID, &credID,
			&vendorID, &pulledAt, &busName); err != nil {
			return 0, nil, err
		}
		e.Destination = mapAssignDestination(target)
		if targetBusID.Valid {
			s := targetBusID.String
			e.BusID = &s
		}
		if busName != "" {
			e.BusName = &busName
		}
		e.Count = 1
		// 派发瞬时快照 · key 打码用固定 pattern（handoff 之前不出明文）
		e.Keys = []assignedKeyDTO{{
			CredentialID: credID,
			KeyMasked:    "ksk_" + shortID(credID) + "…" + tailID(credID),
			VendorID:     vendorID,
			// region / credits_used / lifespan 1a 阶段查号池 stats 才有，先给 0/空
			Region:          "",
			CreditsUsed:     0,
			LifespanSeconds: 0,
		}}
		if vendorID != "" {
			e.Vendors = []string{vendorID}
		} else {
			e.Vendors = []string{}
		}
		items = append(items, e)
	}
	return total, items, rows.Err()
}

func shortID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}
func tailID(id string) string {
	if len(id) < 4 {
		return id
	}
	return id[len(id)-4:]
}

// mapAssignDestination 内部 target → 对外 destination（§12.5 · 05-api-contract §5）
func mapAssignDestination(target string) string {
	switch target {
	case "to-bus":
		return "into_bus"
	case "to-passengerpool":
		return "push_pool"
	case "handoff":
		return "handoff"
	default:
		return target
	}
}

func parsePaging(r *http.Request) (page, pageSize int) {
	page = atoiOr(r.URL.Query().Get("page"), 1)
	pageSize = atoiOr(r.URL.Query().Get("page_size"), 20)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 20
	}
	return page, pageSize
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
