package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/pullrecord"
	"github.com/google/uuid"
)

func uuidNewString() string            { return uuid.NewString() }
func nowRFC3339() string               { return time.Now().UTC().Format(time.RFC3339Nano) }
func slogWarn(msg string, args ...any) { slog.Warn(msg, args...) }

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

	body, err := readBody(r)
	if err != nil {
		return err
	}
	var req assignRequest
	if err := decodeStrict(body, &req); err != nil {
		return err
	}

	// 幂等（05-api-contract §幂等键 · 派去向"必须"带）
	key := r.Header.Get("X-Idempotency-Key")
	if key == "" {
		return newFail(http.StatusBadRequest, CodeBadIdempotencyKey,
			"派去向必须带 X-Idempotency-Key（32 位十六进制）")
	}
	hit, err := ensureIdempotencyRecord(r.Context(), s.db, p.ID, r.Method, r.URL.Path, key, body)
	if err != nil {
		return err
	}
	switch hit.status {
	case idemReplay:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(hit.responseStatus)
		_, _ = w.Write(hit.responseBody)
		return nil
	case idemConflict:
		return ErrIdempotencyConflict()
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

	if dest == "into_bus" {
		if req.BusID == "" {
			return ErrBadRequest("into_bus 必须带 bus_id")
		}
		// 校验车归此乘客（防越权派进别人的车）· tx 外校验·车归属只读快照
		if _, err := s.buses.GetForPassenger(r.Context(), req.BusID, p.ID); err != nil {
			return ErrNotFound("找不到这辆车")
		}
	}

	// 09-transactions §5 · pending_assignment 崩溃安全的三段式：
	//
	//   tx1: INSERT initial + Commit          ← 承诺"这个 idempotency_key 要做 assign"
	//   tx 外: pool.UpdateCredential          ← 外部动作·崩溃留 initial 供 janitor 兜
	//   tx2: 台账 + 推 completed + finalize   ← 走完
	//
	// 崩溃窗口分析：
	//   - tx1 之前崩：什么痕迹都没有·同 key 重试 = 新单
	//   - tx1 与 pool 之间崩：pending_assignment=initial · janitor 扫到 → 无外部动作·delete·同 key 可重放
	//   - pool 与 tx2 之间崩：pending_assignment=initial · housepool 已迁·台账未改 →
	//                        janitor 查号池 group 对账·前推到 completed 或转 need_manual
	//   - tx2 之后：completed 终态
	//
	// P0 修复（审计发现）：之前先 pool.UpdateCredential 再 tx·中间崩溃会导致
	//   housepool 已迁 · 本地无痕迹 · 同 key 重试 hit idempotency in-flight conflict。
	target := "to-bus"
	var targetBusID any = nil
	if dest == "push_pool" {
		target = "to-passengerpool"
	} else if req.BusID != "" {
		targetBusID = req.BusID
	}

	resp := assignResponse{
		Assigned: len(req.CredentialIDs),
		Errors:   []assignErrItem{},
	}
	respBody, _ := json.Marshal(resp)

	// ── tx1 · 落 initial + commit（承诺 · janitor 可见的锚点）──
	assignIDs := make([]string, 0, len(req.CredentialIDs))
	{
		tx1, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		now := nowRFC3339()
		for _, cid := range req.CredentialIDs {
			aid := uuidNewString()
			assignIDs = append(assignIDs, aid)
			if _, err := tx1.ExecContext(r.Context(), `
				INSERT INTO pending_assignment
				  (id, idempotency_record_id, passenger_id, credential_id, target, target_bus_id,
				   status, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, 'initial', ?, ?)`,
				aid, hit.recordID, p.ID, cid, target, targetBusID, now, now); err != nil {
				_ = tx1.Rollback()
				return fmt.Errorf("assign tx1 · 落 initial 失败: %w", err)
			}
		}
		if err := tx1.Commit(); err != nil {
			return fmt.Errorf("assign tx1 · commit: %w", err)
		}
	}

	// ── tx 外 · 外部动作（housepool 迁 group）──
	// 崩溃发生在这里 · initial 行留在 DB · janitor 扫 → 查 housepool 决定 forward/rollback
	if dest == "into_bus" && s.pool != nil {
		krIDs, err := s.pullRecords.LookupKiroRSCredentialIDs(r.Context(), req.CredentialIDs, p.ID)
		if err != nil {
			return err
		}
		targetGroups := []string{"bus-" + req.BusID}
		for _, cid := range req.CredentialIDs {
			krID, ok := krIDs[cid]
			if !ok {
				continue
			}
			if err := s.pool.UpdateCredential(r.Context(),
				housepool.CredentialID(krID),
				housepool.CredentialPatch{Groups: &targetGroups}); err != nil {
				// 外部动作失败：initial 行留库·janitor 后续走 recover 分支（查 pool group）
				return fmt.Errorf("assign into_bus · housepool 迁 group 失败 (cred=%s krID=%d): %w",
					cid, krID, err)
			}
		}
	}

	// ── tx2 · 台账更新 + 状态推 completed + 幂等响应 ──
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// credential_ledger 更新（into_bus 迁 owner_bus_id · push_pool 标 pushed_at）
	switch dest {
	case "into_bus":
		if err := pullrecord.AssignToBusTx(r.Context(), tx, req.CredentialIDs, p.ID, req.BusID); err != nil {
			if errors.Is(err, pullrecord.ErrNotFound) {
				return newFail(http.StatusConflict, "bad_assignment_plan",
					"这批号里有一个不属于你或已被派出，请刷新后重试")
			}
			return err
		}
	case "push_pool":
		if err := pullrecord.MarkPushedTx(r.Context(), tx, req.CredentialIDs, p.ID); err != nil {
			if errors.Is(err, pullrecord.ErrNotFound) {
				return newFail(http.StatusConflict, "bad_assignment_plan",
					"这批号里有一个不属于你或已被派出，请刷新后重试")
			}
			return err
		}
	}

	// initial → completed · SQLite 单 tx 下直接一步。
	// 以前有 initial → external_done → status_updated → completed 三次 UPDATE ·
	// 但同 tx 提交本质是一次原子写 · 那三次 UPDATE 是"给审计看的假状态机" · 移除。
	// 真的分步是 tx1（initial）+ tx 外（pool）+ tx2（completed）· 现在已经三段。
	for _, aid := range assignIDs {
		res, err := tx.ExecContext(r.Context(), `
			UPDATE pending_assignment
			   SET status = 'completed', updated_at = ?
			 WHERE id = ? AND status = 'initial'`,
			nowRFC3339(), aid)
		if err != nil {
			return fmt.Errorf("assign: 状态推进 completed 失败: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("assign: 状态推进 rows=0（并发或已推过）")
		}
	}

	if err := saveIdempotentResponseTx(r.Context(), tx, hit.recordID, http.StatusOK, respBody); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
	return nil
}
