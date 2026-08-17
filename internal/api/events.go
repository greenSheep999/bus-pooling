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
		   AND pr.status != 'initiated'
		`, passengerID).Scan(&total); err != nil {
		return 0, nil, err
	}
	// status != 'initiated'：进行中的轮次不进历史列表 —— mapPullRoundResult 对
	// initiated 只能落 default 'failed' · 会把正在拉的轮次显示成"失败"（几秒后又变成功）
	rows, err := db.QueryContext(ctx, `
		SELECT pr.id, pr.created_at, pr.vendor_id,
		       pr.count_requested, pr.count_purchased,
		       pr.key_cost_total + pr.vendor_fee_total + pr.region_fee_total +
		       pr.single_pull_fee_total + pr.capability_fee_total + pr.service_fee_total AS total_cost,
		       pr.status,
		       -- 已派：号已离开 record 组（进车 / 推池 / 拿走）
		       (SELECT count(1) FROM credential_ledger cl
		          WHERE cl.source_pull_round_id = pr.id
		            AND (cl.owner_bus_id IS NOT NULL
		                 OR cl.pushed_to_passengerpool_at IS NOT NULL
		                 OR cl.status = 'handed_off')) AS assigned_count,
		       -- 待派：号还在 record-<pid> 组等处置
		       -- **口径跟 Overview 卡片 + 提取页待派列表一字不差**（都不排已推池 ——
		       -- push_pool 是双写 · 号还属于这个乘客 · 还能再派去别处）· 三处不一致
		       -- 就会出现"卡片说 1 个 · 列表列 2 条"（车主报的 bug）
		       (SELECT count(1) FROM credential_ledger cl
		          WHERE cl.source_pull_round_id = pr.id
		            AND cl.owner_bus_id IS NULL
		            AND cl.owner_record_passenger_id IS NOT NULL
		            AND cl.status != 'handed_off') AS pending_count
		  FROM pull_round pr
		 WHERE pr.id IN (SELECT pull_round_id FROM pending_purchase WHERE passenger_id = ? AND pull_round_id IS NOT NULL)
		   AND pr.status != 'initiated'
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
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.VendorID,
			&e.CountRequested, &e.CountPurchased, &e.TotalCost, &internalStatus,
			&e.AssignedCount, &e.PendingCount); err != nil {
			return 0, nil, err
		}
		// TotalCost 对外语义是「这一轮花了多少」· 正数（记账符号不外泄）
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
	// 用量真值 + 上限 · 前端画进度条（跟待派列表 / 车内号列表同一个 UsageMeter）
	UsageCurrent int64  `json:"usage_current"`
	UsageLimit   int64  `json:"usage_limit"`
	Subscription string `json:"subscription,omitempty"`
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
		       cl.vendor_id, COALESCE(cl.key_masked, ''), COALESCE(cl.pulled_at, ''),
		       COALESCE(b.name, ''),
		       COALESCE(cl.region, ''),
		       -- 用量 · 活号取最近一次采样 · 号死了 credits_used 才是终值
		       COALESCE(cus.current_usage_micro, cl.credits_used, 0),
		       cl.dead_at,
		       COALESCE(cus.usage_limit_micro, 0),
		       COALESCE(cl.subscription, '')
		  FROM pending_assignment pa
		  LEFT JOIN credential_ledger cl ON cl.id = pa.credential_id
		  LEFT JOIN credential_usage_snapshot cus
		    ON cus.kiro_rs_credential_id = cl.kiro_rs_credential_id
		   AND cus.observed_at = (
		         SELECT MAX(observed_at) FROM credential_usage_snapshot
		          WHERE kiro_rs_credential_id = cl.kiro_rs_credential_id
		       )
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
		var credID, vendorID, keyMasked, pulledAt, busName, region, subscription string
		var usedMicro, limitMicro int64
		var deadAt sql.NullString
		if err := rows.Scan(&e.ID, &e.CreatedAt, &target, &targetBusID, &credID,
			&vendorID, &keyMasked, &pulledAt, &busName,
			&region, &usedMicro, &deadAt, &limitMicro, &subscription); err != nil {
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
		// key_masked / region / 用量 / 寿命 全读真值 —— 原来 region/credits_used/lifespan
		// 是写死的 0 和空串（"1a 阶段先给 0"的占位）· 于是派发历史展开后每个号都显示 0
		e.Keys = []assignedKeyDTO{{
			CredentialID: credID,
			KeyMasked:    keyMasked,
			VendorID:     vendorID,
			Region:       region,
			CreditsUsed:  usedMicro,
			// "Alive at dispatch" = 截到**派发时刻**的存活时长（快照语义）·
			// 原来用 lifespanOf(→now) · 数字会自己长 · 跟列名说的不是一回事
			LifespanSeconds: lifespanAt(pulledAt, deadAt, e.CreatedAt),
			UsageCurrent:    usedMicro,
			UsageLimit:      limitMicro,
			Subscription:    subscription,
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
