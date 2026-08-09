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

// isUniqueConstraintErr 判断 sqlite driver 返回的错误是否 UNIQUE 冲突（错误码 2067·
// 但 sqlite3 driver 通过 message 暴露）。用 message 匹配比 errno 好带（多 driver 兼容）。
func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}

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
	// Settlement 派进多人车时的份额清算结果（decisions §8.23）· 单人车 / 无清算时省略
	Settlement *assignSettlementDTO `json:"settlement,omitempty"`
}

// assignSettlementDTO 清算结果对外形状。
//
// **只给结果**（§8.23 "只给结果，不列明细"）：收到多少 / 少收多少 / 谁被跳过。
// 不出内部字段：不返各人 share_pct、不返余额、不返内部 reason 枚举。
type assignSettlementDTO struct {
	// Income 车友分摊后你实际收到多少（microunit）
	Income int64 `json:"income"`
	// Lost 因为有人本次跳过·你少收多少（0 = 所有人都参与了）
	Lost int64 `json:"lost"`
	// SkippedUsernames 本次跳过的车友用户名（要让派入者知道是谁）
	SkippedUsernames []string `json:"skipped_usernames"`
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

	if dest == "into_bus" && req.BusID == "" {
		return ErrBadRequest("into_bus 必须带 bus_id")
	}

	// 幂等预检 · tx1 之前先 SELECT 看是否已完成（response_status 非 NULL）·
	// 命中 replay → 直接返回·**不做归属校验**（首次成功后号已派·再校验必失败·
	// 幂等语义要求"同 key 同 body → 同响应"）。
	// 只是 SELECT · 不拿写锁 · 不影响 tx1。
	if replayBody, replayStatus, ok, err := checkIdempotencyReplay(r.Context(), s.db, p.ID, r.URL.Path, key, body); err != nil {
		return err
	} else if ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(replayStatus)
		_, _ = w.Write(replayBody)
		return nil
	}

	// 未命中 replay · 走 tx 外的 bus 归属预检（读快照·不上写锁·早失败）·
	// **credential 归属检查**放到 tx1 内做（防两个并发请求都通过·见 P0-1 修复）。
	if dest == "into_bus" {
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
	// respBody 在清算跑完后才序列化（清算结果要进响应体 + 幂等快照）

	// ── tx1 · idempotency 记录 + pending_assignment initial · 同一原子提交 ──
	//
	// P1 修复（审计发现）：以前 ensureIdempotencyRecord 独立 commit·跟 pending_assignment
	// initial 分两个 tx·中间崩溃留个 orphan idempotency_record（response_status IS NULL）·
	// 同 key 重试永远 hit in-flight conflict。现在合到一个 tx · 崩溃时两条一起消失。
	assignIDs := make([]string, 0, len(req.CredentialIDs))
	var recordID string
	{
		tx1, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		hit, err := ensureIdempotencyRecordTx(r.Context(), tx1, p.ID, r.Method, r.URL.Path, key, body)
		if err != nil {
			_ = tx1.Rollback()
			return err
		}
		switch hit.status {
		case idemReplay:
			// 已完成 · 直接重放原字节 · pending_assignment 不动
			// 关键：**不做**归属校验 —— 首次成功后号已派·再校验必失败·
			// 幂等语义要求"同 key 同 body → 同响应"。
			_ = tx1.Rollback()
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(hit.responseStatus)
			_, _ = w.Write(hit.responseBody)
			return nil
		case idemConflict:
			_ = tx1.Rollback()
			return ErrIdempotencyConflict()
		}
		// idemFresh · 关键 P0-1 修：归属校验搬进 tx1 · 落 initial 前一起做。
		//
		// 为什么要搬进 tx1：
		//   Reader (R1) 校验 pass → BEGIN → INSERT initial → COMMIT
		//   Reader (R2) 同一 cid 校验 pass → BEGIN → INSERT initial → COMMIT ← 两个都过
		// 搬进 tx1 后：BEGIN IMMEDIATE 拿写锁 · 校验 + INSERT 原子 · **加 UNIQUE(cid) partial index**
		// 让 R2 的 INSERT 直接被约束挡住·返 409 "credential 已被派单"。
		recordID = hit.recordID
		ownership, err := pullrecord.GetOwnershipsTx(r.Context(), tx1, req.CredentialIDs, p.ID)
		if err != nil {
			_ = tx1.Rollback()
			return err
		}
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
			_ = tx1.Rollback()
			writeJSON(w, http.StatusConflict, assignResponse{Assigned: 0, Errors: bad})
			return nil
		}

		now := nowRFC3339()
		for _, cid := range req.CredentialIDs {
			aid := uuidNewString()
			assignIDs = append(assignIDs, aid)
			// P0-1 修：UNIQUE(credential_id) WHERE status='initial'（migration 012）
			// 让并发 R2 在这里 fail · 早失败 · 不落 initial · 不走 pool。
			if _, err := tx1.ExecContext(r.Context(), `
				INSERT INTO pending_assignment
				  (id, idempotency_record_id, passenger_id, credential_id, target, target_bus_id,
				   status, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, 'initial', ?, ?)`,
				aid, recordID, p.ID, cid, target, targetBusID, now, now); err != nil {
				_ = tx1.Rollback()
				if isUniqueConstraintErr(err) {
					// 有另一并发 assign 正在跑 · R2 让位
					return newFail(http.StatusConflict, "credential_assign_in_flight",
						"这个号正在被另一个派单请求处理·请稍后再试")
				}
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
	var settlement pullrecord.Settlement
	switch dest {
	case "into_bus":
		if err := pullrecord.AssignToBusTx(r.Context(), tx, req.CredentialIDs, p.ID, req.BusID); err != nil {
			if errors.Is(err, pullrecord.ErrNotFound) {
				return newFail(http.StatusConflict, "bad_assignment_plan",
					"这批号里有一个不属于你或已被派出，请刷新后重试")
			}
			return err
		}
		// 自费号派进多人车 · 按份额即时清算（decisions §8.23）
		// **必须跟 owner_bus_id 变更同事务** —— 否则会出现"号进车了但钱没结"
		st, err := pullrecord.SettleAssignToBusTx(
			r.Context(), tx, req.CredentialIDs, p.ID, req.BusID)
		if err != nil {
			if errors.Is(err, pullrecord.ErrNoPayableMember) {
				return newFail(http.StatusConflict, "no_payable_member",
					"没有车友能参与这次分摊 · 现在派进去等于你白送 · 等他们充值或先解挂")
			}
			return err
		}
		settlement = st
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

	// 清算结果拼进响应（多人车才有 · 单人车 settlement.Solo=true 时省略）
	if !settlement.Solo && (settlement.Income > 0 || len(settlement.Skipped) > 0) {
		dto := &assignSettlementDTO{
			Income:           settlement.Income,
			Lost:             settlement.Lost,
			SkippedUsernames: []string{},
		}
		for _, sk := range settlement.Skipped {
			username, _, err := s.passengerBriefFor(r, sk.PassengerID)
			if err != nil {
				// 拿不到名字不该让整个 assign 失败 · 用占位
				username = "车友"
			}
			dto.SkippedUsernames = append(dto.SkippedUsernames, username)
		}
		resp.Settlement = dto
	}
	respBody, _ := json.Marshal(resp)

	if err := saveIdempotentResponseTx(r.Context(), tx, recordID, http.StatusOK, respBody); err != nil {
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
