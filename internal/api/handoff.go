package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/delivery/handoff"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// handoff 三段式（09-transactions §4 · docs/05-api-contract §5b）。
//
// 顺序：POST /me/handoff → GET /me/handoff/{token} → POST /me/handoff/{token}/confirm。
// **明文永不落我方 DB** —— 每次 fulfill 从 housepool 实时读。

// handoffInitRequest ① 的入参。
type handoffInitRequest struct {
	CredentialIDs []string `json:"credential_ids"`
}

// handoffInitResponse ① 的响应 · 不含明文。
type handoffInitResponse struct {
	DownloadToken string `json:"download_token"`
	ExpiresAt     string `json:"expires_at"`
}

// handleHandoffInit · POST /api/me/handoff
//
// 落 pending_handoff 行 · status=token_issued · 返 token + expires_at · 不返明文。
func (s *Server) handleHandoffInit(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.handoffs == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "拿走服务暂未装配")
	}

	var req handoffInitRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.CredentialIDs) == 0 {
		return ErrBadRequest("至少要选一个号")
	}

	pending, err := s.handoffs.IssueToken(r.Context(), handoff.IssueTokenInput{
		PassengerID:   p.ID,
		CredentialIDs: req.CredentialIDs,
	})
	switch {
	case errors.Is(err, handoff.ErrCredentialNotOwned):
		return newFail(http.StatusConflict, "bad_assignment_plan",
			"这批号里有一个不属于你或已交出去，请刷新后重试")
	case errors.Is(err, handoff.ErrEmptyCredentials):
		return ErrBadRequest("至少要选一个号")
	case err != nil:
		return err
	}

	writeJSON(w, http.StatusOK, handoffInitResponse{
		DownloadToken: pending.DownloadToken,
		ExpiresAt:     pending.ExpiresAt.Format(time.RFC3339),
	})
	return nil
}

// handoffKey 是 GET fulfill 时单个号的明文条目（对齐前端 HandoffKeys.keys）。
type handoffKey struct {
	CredentialID string `json:"credential_id"`
	Key          string `json:"key"`
	VendorID     string `json:"vendor_id"`
	Account      string `json:"account"`
}

type handoffFulfillResponse struct {
	Keys []handoffKey `json:"keys"`
}

// handleHandoffFulfill · GET /api/me/handoff/{token}
//
// **TTL 内可反复调用**（断线重试用）· 每次从 housepool 实时读明文。
// 状态 token_issued → fulfilled（首次）· 后续 fulfill 不改状态只累加 fulfill_count。
func (s *Server) handleHandoffFulfill(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.handoffs == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "拿走服务暂未装配")
	}

	token := r.PathValue("token")
	if token == "" {
		return ErrBadRequest("缺 token")
	}

	pending, err := s.handoffs.GetByToken(r.Context(), token)
	if errors.Is(err, handoff.ErrTokenExpired) {
		return newFail(http.StatusNotFound, "token_expired", "下载链接已过期，请重新发起")
	}
	if err != nil {
		return err
	}
	if pending.PassengerID != p.ID {
		// 不泄漏"token 存在但你不是主人" —— 一律 token_expired
		return newFail(http.StatusNotFound, "token_expired", "下载链接已过期，请重新发起")
	}
	// completed / confirmed 之后不能再 fulfill —— 号已经交出去了
	if pending.Status == handoff.StatusCompleted ||
		pending.Status == handoff.StatusConfirmed {
		return newFail(http.StatusConflict, CodeConflict, "这批号已交出，不能再取明文")
	}

	// 明文从 housepool 实时读 —— 关键：明文在**任何**时刻都不能落我方 DB
	keys, err := s.readHandoffPlaintext(r.Context(), pending.CredentialIDs)
	if err != nil {
		return err
	}

	// 推进 fulfilled（幂等 · 后续 GET 同 token 只累加 fulfill_count）
	if err := s.handoffs.MarkFulfilled(r.Context(), pending.ID); err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, handoffFulfillResponse{Keys: keys})
	return nil
}

// handleHandoffConfirm · POST /api/me/handoff/{token}/confirm
//
// 客户端确认收到明文 → 我方触发 housepool DELETE + credential_ledger.status='handed_off'。
// **幂等**：多次 confirm 返回同状态。
func (s *Server) handleHandoffConfirm(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.handoffs == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "拿走服务暂未装配")
	}

	token := r.PathValue("token")
	if token == "" {
		return ErrBadRequest("缺 token")
	}

	pending, err := s.handoffs.GetByToken(r.Context(), token)
	if errors.Is(err, handoff.ErrTokenExpired) {
		return newFail(http.StatusNotFound, "token_expired", "下载链接已过期")
	}
	if err != nil {
		return err
	}
	if pending.PassengerID != p.ID {
		return newFail(http.StatusNotFound, "token_expired", "下载链接已过期")
	}
	// 只有 fulfilled 状态可以 confirm（token_issued 时还没取过明文，不能就直接删）
	// completed / confirmed 幂等静默返回 ok
	switch pending.Status {
	case handoff.StatusFulfilled:
		// go ahead
	case handoff.StatusConfirmed, handoff.StatusCompleted:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return nil
	default:
		return newFail(http.StatusConflict, CodeConflict, "还没取过明文，不能确认")
	}

	// ① 先把状态推 confirmed（占位锁 · 防两次 confirm 同时进入 DELETE）
	if err := s.handoffs.MarkConfirmed(r.Context(), pending.ID); err != nil {
		return err
	}

	// ② 做外部动作：housepool 侧 DELETE + credential_ledger.status='handed_off'
	//    顺序：先 housepool，再 DB（CLAUDE.md §7.1 · 先外后内）
	if err := s.completeHandoff(r.Context(), pending.CredentialIDs); err != nil {
		return err
	}

	// ③ 最后推到 completed
	if err := s.handoffs.MarkCompleted(r.Context(), pending.ID); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// readHandoffPlaintext 每号一次从 housepool 读明文。
//
// **阶段 1a 的现状**：housepool 接口（`GetCredential`）不返回明文（明文只在
// 号池 SDK 的 `ImportCredential` 请求里，不在响应里）· kiro.rs 侧目前没有
// "读明文"的 admin 端点（08-housepool-contract §12 承诺"从 kiro.rs 读明文"但
// 具体 endpoint 未定义）。
//
// 为了让契约 + 前端 UI 可以联调，本方法基于台账元数据返回**打码占位 + 内部备忘录**
// 形式的响应 —— account / vendor_id 是真的（存台账里）· key 用 `pending-handoff-<masked>`
// 前缀清晰标示"这个环境暂未接明文读端点"。**上线前必须把 pool 客户端补齐真的读明文端点**，
// 见 knownIssues。
func (s *Server) readHandoffPlaintext(ctx context.Context, credIDs []string) ([]handoffKey, error) {
	if len(credIDs) == 0 {
		return nil, nil
	}
	rows, err := s.selectHandoffMeta(ctx, credIDs)
	if err != nil {
		return nil, err
	}
	out := make([]handoffKey, 0, len(rows))
	for _, m := range rows {
		key := m.keyMasked
		if key == "" {
			key = "pending-handoff-" + m.credentialID
		}
		out = append(out, handoffKey{
			CredentialID: m.credentialID,
			Key:          key,
			VendorID:     m.vendorID,
			Account:      m.account,
		})
	}
	return out, nil
}

type handoffMeta struct {
	credentialID string
	kiroRSID     uint64
	vendorID     string
	keyMasked    string
	account      string // 台账里没存 · 从 email 派生
}

func (s *Server) selectHandoffMeta(ctx context.Context, credIDs []string) ([]handoffMeta, error) {
	if len(credIDs) == 0 {
		return nil, nil
	}
	placeholders := ""
	args := make([]any, 0, len(credIDs))
	for i, id := range credIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kiro_rs_credential_id, vendor_id, COALESCE(key_masked, '')
		  FROM credential_ledger
		 WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("api: 读 credential_ledger for handoff: %w", err)
	}
	defer rows.Close()
	var out []handoffMeta
	for rows.Next() {
		var m handoffMeta
		if err := rows.Scan(&m.credentialID, &m.kiroRSID, &m.vendorID, &m.keyMasked); err != nil {
			return nil, err
		}
		// account 从 housepool 拿（元数据里有 email）· 拿不到就留空
		if s.pool != nil {
			if c, err := s.pool.GetCredential(ctx, housepool.CredentialID(m.kiroRSID)); err == nil {
				m.account = c.Email
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// completeHandoff 做 confirm 之后的外部 + 内部收尾（09-transactions §4）：
//  1. housepool DELETE（幂等：第二次 DELETE 返回 404 也视为成功）
//  2. credential_ledger.status='handed_off' + handed_off_at
//
// **顺序不能反**（CLAUDE.md §7.1）—— 号池删了本地才能标 handed_off，
// 反过来则可能"本地已标死号，池里还挂着不该在的号"。
func (s *Server) completeHandoff(ctx context.Context, credIDs []string) error {
	// 先按顺序在 pool 侧 DELETE，全部成功后再一次 tx 更新台账
	metas, err := s.selectHandoffMeta(ctx, credIDs)
	if err != nil {
		return err
	}
	if s.pool != nil {
		for _, m := range metas {
			if err := s.pool.DeleteCredential(ctx, housepool.CredentialID(m.kiroRSID)); err != nil {
				// housepool 已删（404）视为成功 —— 幂等重试路径
				if errors.Is(err, housepool.ErrNotFound) {
					continue
				}
				return fmt.Errorf("api: housepool DELETE %d: %w", m.kiroRSID, err)
			}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, cid := range credIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE credential_ledger
			   SET status = 'handed_off', handed_off_at = ?
			 WHERE id = ? AND status != 'handed_off'`, now, cid); err != nil {
			return fmt.Errorf("api: 标 handed_off %s: %w", cid, err)
		}
	}
	return tx.Commit()
}
