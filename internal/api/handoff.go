package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
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

	// ── P0 数据丢失保护（撤销上一轮的默认降级） ────────────────
	//
	// 三种模式（默认拒·两个开关分别开占位路径的不同段）：
	//
	// 1) **默认** · fulfill 直接 501 · 状态不推进 · confirm 也走不下去。
	//    这是唯一安全的默认 —— housepool 明文 endpoint 还没接。
	//    对应 docs/05-api-contract §handoff 的"号池明文未开放"路径。
	//
	// 2) `BP_ALLOW_HANDOFF_PLACEHOLDER=1` · fulfill 返显式占位字符串
	//    "PLACEHOLDER:not-a-real-key:<id>" · **但 pending 状态推进到
	//    placeholder_delivered**（非 fulfilled）· confirm 拒绝走 DELETE
	//    分支 —— 只做 handoff 状态 → confirmed_placeholder。这样：
	//    - 前端联调三段流程能跑
	//    - 号不会被真删（confirm 分支下面有 handoff.StatusPlaceholder 拒绝）
	//    - 明显不是真号 · 前端也能识别
	//
	// 3) `BP_HANDOFF_TRUE_PLAINTEXT=1` + housepool 明文 endpoint 接了 · 走真明文
	//    → fulfilled → confirm DELETE · **生产唯一允许的模式**。
	//    阶段 1a-1c 这个开关不放·1c 后接明文端点才放。
	if os.Getenv("BP_HANDOFF_TRUE_PLAINTEXT") != "1" {
		if os.Getenv("BP_ALLOW_HANDOFF_PLACEHOLDER") != "1" {
			return newFail(http.StatusNotImplemented, "handoff_not_ready",
				"取号功能未开放（housepool 明文导出端点未接）· 号仍在你的池里，可以派进车或推自己号池")
		}
		// 联调路径：返占位 · 状态推进到 placeholder_delivered（非 fulfilled）
		keys := s.readHandoffPlaceholder(pending.CredentialIDs)
		if err := s.handoffs.MarkPlaceholderDelivered(r.Context(), pending.ID); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, handoffFulfillResponse{Keys: keys})
		return nil
	}

	// 真明文路径（生产）· housepool 明文 endpoint 已接
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

// readHandoffPlaceholder · 显式占位·不假装是真明文
// 只在 BP_ALLOW_HANDOFF_PLACEHOLDER=1 时被调用（联调用）
func (s *Server) readHandoffPlaceholder(credIDs []string) []handoffKey {
	if len(credIDs) == 0 {
		return nil
	}
	out := make([]handoffKey, 0, len(credIDs))
	for _, id := range credIDs {
		out = append(out, handoffKey{
			CredentialID: id,
			Key:          "PLACEHOLDER:not-a-real-key:" + id,
			VendorID:     "",
			Account:      "",
		})
	}
	return out
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
	// completed / confirmed / confirmed_placeholder 幂等静默返回 ok
	switch pending.Status {
	case handoff.StatusFulfilled:
		// 真明文路径 · 走 DELETE 交号
	case handoff.StatusPlaceholderDelivered:
		// **占位路径** · 明文是假的 · 号绝不能删 · 只推进到 confirmed_placeholder
		// 前端调 confirm 时前端知道拿到的是占位 · 但保留 200 让联调三段能跑完
		if err := s.handoffs.MarkConfirmedPlaceholder(r.Context(), pending.ID); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"warning":  "placeholder mode · 号未删除 · 明文是占位不能用",
			"real_key": false,
		})
		return nil
	case handoff.StatusConfirmed, handoff.StatusCompleted,
		handoff.StatusConfirmedPlaceholder:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return nil
	default:
		return newFail(http.StatusConflict, CodeConflict, "还没取过明文，不能确认")
	}

	// ── 真明文路径（生产·BP_HANDOFF_TRUE_PLAINTEXT=1 才能进这里）──

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

// errHandoffPlaintextUnavailable 真明文路径的兜底：housepool 明文 endpoint 还没接·
// 就不许有任何"能返 keys"的代码路径·上一轮审计报告的 P0：BP_HANDOFF_TRUE_PLAINTEXT=1
// 进真明文分支·readHandoffPlaintext 却返 PLACEHOLDER·confirm 又走 DELETE 删真号。
// 现在 · 真明文路径永远 error 出去·pending_handoff 状态不推进·号不删。
var errHandoffPlaintextUnavailable = &Fail{
	Status: http.StatusNotImplemented,
	Err: &Error{
		Code:    "handoff_plaintext_unavailable",
		Message: "取号明文暂未开放（本 vendor 明文导出端点未接·联系管理员）",
	},
}

// readHandoffPlaintext 从 housepool 读**真明文** —— 只在真接了 housepool 明文 endpoint
// 之后才会有实现。
//
// **当前实现是拒绝** —— 因为 housepool 侧还没开放"读明文"admin 端点
// （08-housepool-contract §12 承诺但未定义 endpoint）·任何走到这个函数的代码
// 路径都会返 error·pending_handoff 状态不推 fulfilled·confirm 分支就走不了 DELETE。
//
// **联调用占位路径**：用 BP_ALLOW_HANDOFF_PLACEHOLDER=1 · 那条走 readHandoffPlaceholder
// + MarkPlaceholderDelivered · 状态推到 placeholder_delivered · confirm 分支特判走
// MarkConfirmedPlaceholder 不删号。**跟这个函数**完全无关。
//
// **上线**：接了 housepool 明文 endpoint 后·把下面 return error 换成真调 pool 读明文。
func (s *Server) readHandoffPlaintext(_ context.Context, _ []string) ([]handoffKey, error) {
	return nil, errHandoffPlaintextUnavailable
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

// completeHandoff · 委托给 handoff.Complete 做 confirm 之后的收尾。
// 两处调用（api / janitor）共用 · 消 Standards duplication。
func (s *Server) completeHandoff(ctx context.Context, credIDs []string) error {
	return handoff.Complete(ctx, handoff.CompleteDeps{DB: s.db, Pool: s.pool}, credIDs)
}
