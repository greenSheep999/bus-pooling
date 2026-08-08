package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/pullrecord"
)

// pullRecord + assign 端点。对外只暴露前端 Credential 类型的字段（web/src/types/index.ts）·
// housepool 侧的 kiro_rs_credential_id / current_group / death_source 等**绝不出响应体**
// （CLAUDE.md §0.1）。

// pullRecordResp 是列表 / 详情端点用的对外形状。
//
// 对齐 web `Credential` 类型 —— 前端已经在用这个类型渲染，字段名不能改。
type pullRecordResp struct {
	ID          string         `json:"id"`
	VendorID    string         `json:"vendor_id"`
	Status      string         `json:"status"`
	KeyMasked   string         `json:"key_masked"`
	Region      string         `json:"region"`
	CreditsUsed int64          `json:"credits_used"` // microunit
	PulledAt    string         `json:"pulled_at"`
	WarrantyUnt *string        `json:"warranty_until"`
	DeadAt      *string        `json:"dead_at"`
	PushedAt    *string        `json:"pushed_at"`
	PushFailed  bool           `json:"push_failed"`
	PushError   *pushErrorResp `json:"push_error"`
	SourceRound string         `json:"source_pull_round_id"`
}

type pushErrorResp struct {
	Code          string `json:"code"`
	Status        *int   `json:"status"`
	Message       string `json:"message"`
	Retriable     bool   `json:"retriable"`
	Attempts      int    `json:"attempts"`
	LastAttemptAt string `json:"last_attempt_at"`
}

func pullRecordOf(r pullrecord.Record) pullRecordResp {
	out := pullRecordResp{
		ID:          r.ID,
		VendorID:    r.VendorID,
		Status:      string(r.Status),
		KeyMasked:   r.KeyMasked,
		Region:      r.Region,
		CreditsUsed: r.CreditsUsed,
		PulledAt:    r.PulledAt.Format(time.RFC3339),
		PushFailed:  r.PushFailed,
		SourceRound: r.SourceRound,
	}
	if r.WarrantyUnt != nil {
		s := r.WarrantyUnt.Format(time.RFC3339)
		out.WarrantyUnt = &s
	}
	if r.DeadAt != nil {
		s := r.DeadAt.Format(time.RFC3339)
		out.DeadAt = &s
	}
	if r.PushedAt != nil {
		s := r.PushedAt.Format(time.RFC3339)
		out.PushedAt = &s
	}
	if r.PushError != nil {
		out.PushError = &pushErrorResp{
			Code:          r.PushError.Code,
			Status:        r.PushError.Status,
			Message:       r.PushError.Message,
			Retriable:     r.PushError.Retriable,
			Attempts:      r.PushError.Attempts,
			LastAttemptAt: r.PushError.LastAttemptAt.Format(time.RFC3339),
		}
	}
	return out
}

// handleListPullRecords · GET /api/me/pull-records
//
// 分页信封跟 /me/ledger 一致（total / page / page_size / pages）。
// ?history=1 时包含已死号（默认只返存活）。
func (s *Server) handleListPullRecords(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.pullRecords == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "拉号记录服务暂未装配")
	}

	page := atoiDefault(r.URL.Query().Get("page"), 1)
	pageSize := atoiDefault(r.URL.Query().Get("page_size"), 50)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 50
	}

	items, total, err := s.pullRecords.List(r.Context(), p.ID, pullrecord.ListOptions{
		IncludeHistory: r.URL.Query().Get("history") == "1",
		Limit:          pageSize,
		Offset:         (page - 1) * pageSize,
	})
	if err != nil {
		return err
	}

	out := make([]pullRecordResp, 0, len(items))
	for _, r := range items {
		out = append(out, pullRecordOf(r))
	}
	pages := 0
	if total > 0 {
		pages = (total + pageSize - 1) / pageSize
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": out, "total": total,
		"page": page, "page_size": pageSize, "pages": pages,
	})
	return nil
}

// handleGetPullRecord · GET /api/me/pull-records/{record_id}
func (s *Server) handleGetPullRecord(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.pullRecords == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "拉号记录服务暂未装配")
	}
	id := r.PathValue("record_id")
	rec, err := s.pullRecords.Get(r.Context(), id, p.ID)
	switch {
	case errors.Is(err, pullrecord.ErrNotFound):
		return ErrNotFound("找不到这条拉号记录")
	case err != nil:
		return err
	}
	writeJSON(w, http.StatusOK, pullRecordOf(*rec))
	return nil
}

// assignRequest 是 POST /api/me/pull-records/assign 的入参。
//
// 一次一个 destination（05-api-contract §5）· destination=into_bus 必带 bus_id。
// **不做**混合去向（CLAUDE.md §2 已废 "混合上车 / allocation 组件"）。
type assignRequest struct {
	CredentialIDs []string `json:"credential_ids"`
	Destination   string   `json:"destination"` // into_bus | push_pool
	BusID         string   `json:"bus_id,omitempty"`
}

// assignResponse 派去向结果。errors 用打码 id 标出哪几个没派成功（部分失败时）。
type assignResponse struct {
	Assigned int             `json:"assigned"`
	Errors   []assignErrItem `json:"errors"`
}

type assignErrItem struct {
	CredentialID string `json:"credential_id"`
	Code         string `json:"code"`    // not_owned | into_bus_bus_not_owned | …
	Message      string `json:"message"` // 中文人话
}

// handleAssign · POST /api/me/pull-records/assign
//
// 阶段 1a：
//   - into_bus：改 credential_ledger.owner_bus_id / current_group（真的 housepool group 迁移在 1c）
//   - push_pool：只标 pushed_to_passengerpool_at 时间戳（真的推 passengerpool 在 1c，走 mock）
//
// **不走 handoff** —— handoff 走 §5b 三段式，不在这个端点里。
func (s *Server) handleAssign(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.pullRecords == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "拉号记录服务暂未装配")
	}

	var req assignRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.CredentialIDs) == 0 {
		return ErrBadRequest("至少要选一个号")
	}
	dest := strings.ToLower(strings.TrimSpace(req.Destination))
	switch dest {
	case "into_bus", "push_pool":
		// ok
	case "handoff":
		return ErrBadRequest("拿走走 POST /api/me/handoff，不在这个端点")
	default:
		return newFail(http.StatusBadRequest, "bad_assignment_plan",
			"destination 只能是 into_bus 或 push_pool")
	}

	// 归属校验：所有传入的号都必须在此乘客的 record group 里且未派
	ownership, err := s.pullRecords.GetOwnerships(r.Context(), req.CredentialIDs, p.ID)
	if err != nil {
		return err
	}
	// 收集不归此乘客的号 · 只要有一个不归就拒整批（handoff 那种的整批语义）
	var bad []assignErrItem
	for _, cid := range req.CredentialIDs {
		if !ownership[cid] {
			bad = append(bad, assignErrItem{
				CredentialID: cid, Code: "not_owned",
				Message: "这个号不属于你或已派出",
			})
		}
	}
	if len(bad) > 0 {
		writeJSON(w, http.StatusConflict, assignResponse{Assigned: 0, Errors: bad})
		return nil
	}

	switch dest {
	case "into_bus":
		if req.BusID == "" {
			return ErrBadRequest("into_bus 必须带 bus_id")
		}
		// 校验车归此乘客（防越权派进别人的车）
		if _, err := s.buses.GetForPassenger(r.Context(), req.BusID, p.ID); err != nil {
			return ErrNotFound("找不到这辆车")
		}
		if err := s.pullRecords.AssignToBus(r.Context(), req.CredentialIDs, p.ID, req.BusID); err != nil {
			if errors.Is(err, pullrecord.ErrNotFound) {
				return newFail(http.StatusConflict, "bad_assignment_plan",
					"这批号里有一个不属于你或已被派出，请刷新后重试")
			}
			return err
		}
	case "push_pool":
		if err := s.pullRecords.MarkPushed(r.Context(), req.CredentialIDs, p.ID); err != nil {
			if errors.Is(err, pullrecord.ErrNotFound) {
				return newFail(http.StatusConflict, "bad_assignment_plan",
					"这批号里有一个不属于你或已被派出，请刷新后重试")
			}
			return err
		}
	}

	writeJSON(w, http.StatusOK, assignResponse{
		Assigned: len(req.CredentialIDs),
		Errors:   []assignErrItem{},
	})
	return nil
}
