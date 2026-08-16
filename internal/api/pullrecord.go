package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/delivery/passengerpool"
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
		Assigned: len(req.CredentialIDs), // 后面 push_pool 部分失败会减
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

	// ── tx 外 · 外部动作（into_bus: housepool 迁 group · push_pool: 双写乘客号池） ──
	// 崩溃发生在这里 · initial 行留在 DB · janitor 扫 → 查 housepool 决定 forward/rollback
	//
	// **push_pool 分支** · s.pusher == nil 时走 dry-run(只标 pushed_at)· nil 兜底跟 into_bus
	// 无 s.pool 时一致(mock 环境不装 housepool 也不装 pusher) · handler 不能因此崩。
	//
	// **push_pool 部分失败** · pushResult 拆开 · 成功号走 tx2 MarkPushSuccessTx · 失败号
	// 用独立事务落 push_error_* 六字段 + pending_assignment 保 initial 让 janitor 兜。
	// 响应体走 errors[] · 跟 into_bus 归错结构一致。
	var pushResult *passengerpool.PushResult
	pushErrItems := []assignErrItem{}
	successIDs := req.CredentialIDs // 默认：全部成功 (dry-run / into_bus)

	// **P1-l 修(2026-08-16)**: 号状态分层拒推(用户澄清)
	//
	// **3 种状态**:
	//   - dead(真死)  · 401/403 认证错 · 或 housepool 后端 Invalid/Suspended reason · 无条件拒
	//   - quota(用完) · 402 quota_exceeded · monthly 到限额 · 可能重置后恢复
	//   - ok           · 正常 · 允许推
	//
	// **2 种目的地策略**:
	//   - into_bus     · 共享资源 · dead + quota 都拒(车友取到废号影响所有人)
	//   - push_pool    · 用户自己号池 · **只拒 dead** · quota 允许推(用户自己决定)
	//     · 用户视角:"我知道用完了 · 先推到自己池等重置或换号"
	if s.pool != nil {
		krIDs, err := s.pullRecords.LookupKiroRSCredentialIDs(r.Context(), req.CredentialIDs, p.ID)
		if err != nil {
			return err
		}
		type credErr struct {
			kind    string // "dead" 或 "quota" 或其它
			message string
		}
		badIDs := map[string]credErr{}
		// **P1-m 修(2026-08-16)**: 除了 TestCredential(true dead 判)· 也查 GetBalance
		// 拿 usage_percentage · >=95% 视为 quota(housepool 后端 API 层 TestCredential 只在 100%
		// 时才拒 · 99.9% 边界能过 · 但号推给车友下一次就 402)
		const quotaThreshold = 95.0
		for _, cid := range req.CredentialIDs {
			krID, ok := krIDs[cid]
			if !ok {
				continue
			}
			// ① 探活 · 401/403/Invalid 直接判 dead
			if err := s.pool.TestCredential(r.Context(), housepool.CredentialID(krID)); err != nil {
				msg := err.Error()
				kind := "dead"
				if containsAny(msg, "quota_exceeded", "MONTHLY_REQUEST_COUNT", "402", "Payment Required", "reached the limit") {
					kind = "quota"
				}
				badIDs[cid] = credErr{kind: kind, message: msg}
				continue
			}
			// ② 探活通过 · 但看 usage · >=95% 也当 quota(边界保护)
			bal, berr := s.pool.GetBalance(r.Context(), housepool.CredentialID(krID))
			if berr == nil && bal != nil && bal.UsagePercentage >= quotaThreshold {
				badIDs[cid] = credErr{
					kind:    "quota",
					message: fmt.Sprintf("usage %.1f%% >= %.0f%% (limit %.0f · used %.0f)", bal.UsagePercentage, quotaThreshold, bal.UsageLimit, bal.CurrentUsage),
				}
			}
		}

		// 按 destination 决定拒哪些
		rejected := map[string]credErr{}
		for cid, ce := range badIDs {
			switch dest {
			case "into_bus":
				// 车里共享资源 · dead 和 quota 都拒
				rejected[cid] = ce
			case "push_pool":
				// 用户自己号池 · 只拒 dead(quota 允许推 · 用户自己判)
				if ce.kind == "dead" {
					rejected[cid] = ce
				}
			}
		}

		if len(rejected) > 0 {
			for cid, ce := range rejected {
				code := "credential_dead"
				userMsg := "号已失效 · 不能派(housepool 后端 探活返错)"
				if ce.kind == "quota" {
					code = "credential_quota_exceeded"
					userMsg = "号已用完额度 · 拼车共享号需活号 · 请换号或等 quota 重置"
				}
				if _, uerr := s.db.ExecContext(r.Context(), `
					UPDATE pending_assignment
					   SET status = 'need_manual', updated_at = ?, error = ?
					 WHERE credential_id = ? AND status = 'initial'`,
					nowRFC3339(), code+": "+ce.message, cid); uerr != nil {
					slogWarn("assign · 标 need_manual 失败", "cred", cid, "err", uerr)
				}
				pushErrItems = append(pushErrItems, assignErrItem{
					CredentialID: cid,
					Code:         code,
					Message:      userMsg,
				})
			}
			// successIDs 剔除被拒的号(quota 号在 push_pool 场景保留)
			live := make([]string, 0, len(req.CredentialIDs))
			for _, cid := range req.CredentialIDs {
				if _, rej := rejected[cid]; !rej {
					live = append(live, cid)
				}
			}
			successIDs = live
		}
	}

	if dest == "into_bus" && s.pool != nil {
		krIDs, err := s.pullRecords.LookupKiroRSCredentialIDs(r.Context(), successIDs, p.ID)
		if err != nil {
			return err
		}
		targetGroups := []string{"bus-" + req.BusID}
		for _, cid := range successIDs {
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
	} else if dest == "push_pool" && s.pusher != nil && len(successIDs) > 0 {
		// 只推 verify 通过的号(死号 successIDs 已剔除 · pushErrItems 已记 credential_dead)
		metas, err := s.selectPushMeta(r.Context(), successIDs, p.ID)
		if err != nil {
			return fmt.Errorf("assign push_pool · 查号 meta: %w", err)
		}
		creds := make([]passengerpool.PushCredential, 0, len(successIDs))
		for _, cid := range successIDs {
			m := metas[cid]
			creds = append(creds, passengerpool.PushCredential{
				CredentialID: cid,
				Region:       m.region,
				VendorLabel:  m.vendorLabel, // 打码后的 · 不带真名
			})
		}
		pushResult, err = s.pusher.Push(r.Context(), p.ID, creds)
		if err != nil {
			// 顶层 error = 拉配置 / 解密失败 = 没配对家 · 走 dry-run
			if errors.Is(err, passengerpool.ErrNoTarget) {
				slogWarn("passengerpool.dryrun", "mode", "no_target", "passenger", p.ID)
				// fallthrough → 走原 MarkPushedTx dry-run 路径
			} else {
				return fmt.Errorf("assign push_pool · Push: %w", err)
			}
		} else if pushResult != nil {
			// 有 pushResult · 拆开成功 / 失败
			ok := map[string]bool{}
			for _, id := range pushResult.Success {
				ok[id] = true
			}
			for _, id := range pushResult.Duplicate {
				ok[id] = true // duplicate 视为成功
			}
			pushSuccess := successIDs[:0]
			for _, id := range successIDs {
				if ok[id] {
					pushSuccess = append(pushSuccess, id)
				}
			}
			successIDs = pushSuccess
			// 失败号 · 独立事务落 push_error_* + pending_assignment 保 initial
			if len(pushResult.Failed) > 0 {
				if err := s.recordPushFailures(r.Context(), pushResult.Failed, p.ID); err != nil {
					return fmt.Errorf("assign push_pool · 落 push_error: %w", err)
				}
				for _, f := range pushResult.Failed {
					pushErrItems = append(pushErrItems, assignErrItem{
						CredentialID: f.CredentialID,
						Code:         string(f.Err.Kind),
						Message:      f.Err.Message,
					})
				}
			}
		}
	} else if dest == "push_pool" {
		slogWarn("passengerpool.dryrun", "mode", "nopusher", "passenger", p.ID)
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
		if pushResult == nil {
			// dry-run 路径(s.pusher nil / ErrNoTarget)· 只标 successIDs 时间戳
			// **P1-k 修(2026-08-16)**: 之前用 req.CredentialIDs 会把 verify 拒的死号也标 pushed_at
			// 场景:K5 用尽号 · verify 拒 · successIDs=[] · 但 dry-run 走 req.CredentialIDs 落 pushed_at
			if len(successIDs) > 0 {
				if err := pullrecord.MarkPushedTx(r.Context(), tx, successIDs, p.ID); err != nil {
					if errors.Is(err, pullrecord.ErrNotFound) {
						return newFail(http.StatusConflict, "bad_assignment_plan",
							"这批号里有一个不属于你或已被派出·请刷新后重试")
					}
					return err
				}
			}
		} else if len(successIDs) > 0 {
			// 真推路径 · 成功号走 MarkPushSuccessTx(清 push_error_* + attempts+1 + 时间戳)
			if err := pullrecord.MarkPushSuccessTx(r.Context(), tx, successIDs, p.ID); err != nil {
				if errors.Is(err, pullrecord.ErrNotFound) {
					return newFail(http.StatusConflict, "bad_assignment_plan",
						"这批号里有一个不属于你或已被派出，请刷新后重试")
				}
				return err
			}
		}
	}

	// initial → completed · SQLite 单 tx 下直接一步。
	// 以前有 initial → external_done → status_updated → completed 三次 UPDATE ·
	// 但同 tx 提交本质是一次原子写 · 那三次 UPDATE 是"给审计看的假状态机" · 移除。
	// 真的分步是 tx1（initial）+ tx 外（pool）+ tx2（completed）· 现在已经三段。
	//
	// **push_pool 部分失败** · 成功号走 completed · 失败号已在 recordPushFailures
	// 里独立事务标 need_manual · 这里跳过它们(assignIDs 顺序跟 req.CredentialIDs 一致)。
	failedCredIDs := map[string]bool{}
	for _, f := range pushErrItems {
		failedCredIDs[f.CredentialID] = true
	}
	for i, aid := range assignIDs {
		if i < len(req.CredentialIDs) && failedCredIDs[req.CredentialIDs[i]] {
			continue // 失败号的 pending_assignment 由 recordPushFailures 处理 · 别推 completed
		}
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

	// push_pool 失败号进响应 errors[] · assigned 减去失败数
	if len(pushErrItems) > 0 {
		resp.Errors = append(resp.Errors, pushErrItems...)
		resp.Assigned = len(req.CredentialIDs) - len(pushErrItems)
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

	// 1e-2 · 通知对外 webhook · push_pool 成功后喊一声 boarded
	// **响应已发** · 通知失败也不影响用户看到的结果
	if dest == "push_pool" && len(successIDs) > 0 && s.webhookOut != nil {
		s.webhookOut.NotifyBoarded(r.Context(), p.ID, successIDs, "push_pool")
	}
	return nil
}

// pushMeta 是 push_pool 分支拉的号元数据。**不含明文** — 明文由 Pusher 自己拿。
type pushMeta struct {
	region      string
	vendorLabel string
}

// selectPushMeta 拉一批号的 region + vendor_id · 只返对家可见的元数据。
//
// **vendor_id 不出 label** — 用 anon 打码(跟 vendorview 一致) · CLAUDE.md §0.1。
// 校验归属由 tx1 的 GetOwnershipsTx 保证 · 这里不重做。
func (s *Server) selectPushMeta(ctx context.Context, credIDs []string, passengerID string) (map[string]pushMeta, error) {
	out := make(map[string]pushMeta, len(credIDs))
	if len(credIDs) == 0 {
		return out, nil
	}
	placeholders := ""
	args := make([]any, 0, len(credIDs)+1)
	args = append(args, passengerID)
	for i, id := range credIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(vendor_id, ''), COALESCE(region, '')
		  FROM credential_ledger
		 WHERE owner_record_passenger_id = ?
		   AND owner_bus_id IS NULL
		   AND status != 'handed_off'
		   AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, vendorID, region string
		if err := rows.Scan(&id, &vendorID, &region); err != nil {
			return nil, err
		}
		out[id] = pushMeta{
			region: region,
			// vendor label 走脱敏 · 不给对家看 vendor 真名
			// 阶段 1e-1 简化：直接给一个通用 tag · 未来接 vendorview.anon 映射
			vendorLabel: "provider",
		}
	}
	return out, rows.Err()
}

// recordPushFailures 独立事务落六字段 + 把对应 pending_assignment 转 need_manual。
//
// **不合到 tx2**：tx2 里成功号要落 completed · 失败号要落 need_manual · 混一个 tx
// 里语义混乱。独立事务保证：即使 tx2 崩了 · push_error_* 也已经落库 · janitor 兜。
func (s *Server) recordPushFailures(ctx context.Context, failed []passengerpool.FailedItem, passengerID string) error {
	if len(failed) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 六字段
	credIDs := make([]string, 0, len(failed))
	fields := make(map[string]pullrecord.PushFailureFields, len(failed))
	for _, f := range failed {
		credIDs = append(credIDs, f.CredentialID)
		var st *int
		if f.Err.Status > 0 {
			st = &f.Err.Status
		}
		fields[f.CredentialID] = pullrecord.PushFailureFields{
			Code:      string(f.Err.Kind),
			Status:    st,
			Message:   f.Err.Message,
			Retriable: f.Err.Retriable(),
		}
	}
	if err := pullrecord.MarkPushFailureTx(ctx, tx, credIDs, passengerID, fields); err != nil {
		// 号不存在等 - 落 log 别炸 · 归属校验在 tx1 已过 · 这里出错基本是并发
		slogWarn("push_failures.mark_failed",
			"passenger", passengerID, "err", err)
	}

	// pending_assignment 转 need_manual(可重试的话 janitor 后续会重试)
	// **不删 initial 行** — 保留让 janitor 走 reconcile 路径
	failedSet := ""
	fargs := []any{}
	for i, id := range credIDs {
		if i > 0 {
			failedSet += ","
		}
		failedSet += "?"
		fargs = append(fargs, id)
	}
	fargs = append(fargs, passengerID, "to-passengerpool")
	_, err = tx.ExecContext(ctx, `
		UPDATE pending_assignment
		   SET status = 'need_manual',
		       error = 'passengerpool_push_failed',
		       updated_at = ?
		 WHERE status = 'initial'
		   AND credential_id IN (`+failedSet+`)
		   AND passenger_id = ?
		   AND target = ?`,
		append([]any{nowRFC3339()}, fargs...)...)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// containsAny · 简单帮手 · haystack 含任意一个 needle 就返 true(case-sensitive)
// 用途:错误 message 里判 quota_exceeded / 402 等关键字
func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
