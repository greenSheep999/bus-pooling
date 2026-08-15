package api

// bus_credential_push.go · 车内号手动重推 · decisions §8.44 · Task #194
//
// 场景: 车里的号(owner_bus_id 非空)自动 push_pool 失败后 · 用户想手动重试
// UI: BusDetail 号列表 · pushed_to_passengerpool_at 空 或 push_error_code 非空时显 '重推' 按钮
// API: POST /api/me/buses/{bus_id}/credentials/{cred_id}/push
//
// 幂等: 已 pushed 二次调 no-op 返 200(state: already_pushed)
// 权限: bus.creator 或 bus 成员
// 死号护栏: pool.TestCredential 探活 · 失败拒重推(减 §8.43 语义)

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/delivery/passengerpool"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// busCredentialPushResp · 手动重推响应
type busCredentialPushResp struct {
	// State: pushed(新推成功)· already_pushed(已推过 · 幂等)· failed(推失败)· dead(号已失效)
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// handleBusCredentialPush · POST /api/me/buses/{bus_id}/credentials/{cred_id}/push
func (s *Server) handleBusCredentialPush(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	busID := r.PathValue("bus_id")
	credID := r.PathValue("cred_id")
	if busID == "" || credID == "" {
		return ErrBadRequest("bus_id / cred_id 缺失")
	}

	// 权限:bus.creator 或 bus 成员
	if _, err := s.buses.GetForPassenger(r.Context(), busID, p.ID); err != nil {
		if errors.Is(err, bus.ErrNotFound) || errors.Is(err, bus.ErrNotMember) {
			return ErrNotFound("找不到这辆车")
		}
		return err
	}

	// 找号 · 校验属该 bus + alive
	var (
		krID       int64
		pushedAt   sql.NullString
		pushErrCode sql.NullString
		status     string
	)
	err = s.db.QueryRowContext(r.Context(), `
		SELECT kiro_rs_credential_id, pushed_to_passengerpool_at, push_error_code, status
		  FROM credential_ledger
		 WHERE id = ? AND owner_bus_id = ?`,
		credID, busID).Scan(&krID, &pushedAt, &pushErrCode, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound("这个号不在该车里")
	}
	if err != nil {
		return err
	}
	if status != "alive" {
		return ErrBadRequest("号已 " + status + " · 不能重推")
	}

	// 幂等:已推过 且 无失败标记 → no-op 200
	if pushedAt.Valid && pushedAt.String != "" && !pushErrCode.Valid {
		writeJSON(w, http.StatusOK, busCredentialPushResp{
			State:   "already_pushed",
			Message: "号已推过 · 无需重复",
		})
		return nil
	}

	// 死号护栏:pool.TestCredential 失败 = 拒重推
	// **P1-l/m 修(2026-08-16)**: 车内号是**共享资源** · dead 或 quota 都拒
	// 用户澄清:"车拉的肯定不能推" · 车里号影响所有车友 · 严格
	// 判据:①TestCredential 失败 (真死) · ②usage_percentage >= 95%(边界保护)
	if s.pool != nil {
		if terr := s.pool.TestCredential(r.Context(), housepool.CredentialID(krID)); terr != nil {
			msg := terr.Error()
			hint := "号探活失败 · 已失效不能重推"
			if strings.Contains(msg, "quota_exceeded") || strings.Contains(msg, "MONTHLY_REQUEST_COUNT") ||
				strings.Contains(msg, "Payment Required") || strings.Contains(msg, "reached the limit") {
				hint = "号已用完额度 · 车里共享号需活号 · 请换号或等 quota 重置"
			}
			writeJSON(w, http.StatusOK, busCredentialPushResp{
				State:   "dead",
				Message: hint + ": " + truncate(msg, 200),
			})
			return nil
		}
		// 边界:探活通过但 usage 快满 · 车里共享号该拒(下一次就 402)
		bal, berr := s.pool.GetBalance(r.Context(), housepool.CredentialID(krID))
		slog.Info("busCredentialPush · GetBalance",
			"cred", credID, "kr_id", krID, "bal", bal, "err", berr)
		if berr == nil && bal != nil && bal.UsagePercentage >= 95.0 {
			writeJSON(w, http.StatusOK, busCredentialPushResp{
				State: "dead",
				Message: fmt.Sprintf("号快用完(%.1f%%)· 车里共享号需活号 · 请换号或等 quota 重置",
					bal.UsagePercentage),
			})
			return nil
		}
	}

	// 拉元数据 · 走 pusher 推
	metas, err := s.selectPushMeta(r.Context(), []string{credID}, p.ID)
	if err != nil {
		return fmt.Errorf("busCredentialPush · 查号 meta: %w", err)
	}
	m := metas[credID]
	creds := []passengerpool.PushCredential{{
		CredentialID: credID,
		Region:       m.region,
		VendorLabel:  m.vendorLabel,
	}}

	if s.pusher == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal,
			"passengerpool.Pusher 未装配 · 请配下游 URL + token")
	}
	pushResult, perr := s.pusher.Push(r.Context(), p.ID, creds)
	if perr != nil {
		if errors.Is(perr, passengerpool.ErrNoTarget) {
			return newFail(http.StatusBadRequest, "downstream_not_configured",
				"下游 passengerpool 未配 · 无法推送")
		}
		return fmt.Errorf("busCredentialPush · Push: %w", perr)
	}

	// pushResult 拆开 · 成功 vs 失败
	success := false
	for _, id := range pushResult.Success {
		if id == credID {
			success = true
			break
		}
	}
	for _, id := range pushResult.Duplicate {
		if id == credID {
			success = true // duplicate 视为成功
			break
		}
	}
	if success {
		// 落 credential_ledger.pushed_to_passengerpool_at · 撤 push_error_*
		nowStr := nowRFC3339()
		if _, uerr := s.db.ExecContext(r.Context(), `
			UPDATE credential_ledger
			   SET pushed_to_passengerpool_at = ?,
			       push_error_code = NULL, push_error_status = NULL,
			       push_error_message = NULL, push_error_retriable = NULL,
			       push_attempts = push_attempts + 1,
			       push_last_attempt_at = ?
			 WHERE id = ?`, nowStr, nowStr, credID); uerr != nil {
			slog.Warn("busCredentialPush · 落 pushed_at 失败", "cred", credID, "err", uerr)
		}
		writeJSON(w, http.StatusOK, busCredentialPushResp{
			State:   "pushed",
			Message: "重推成功",
		})
		return nil
	}

	// 失败 · 落 push_error_* 六字段
	var failMsg string
	for _, f := range pushResult.Failed {
		if f.CredentialID == credID {
			failMsg = f.Err.Message
			break
		}
	}
	writeJSON(w, http.StatusOK, busCredentialPushResp{
		State:   "failed",
		Message: "推送失败: " + truncate(failMsg, 200),
	})
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// nowRFC3339 in server.go(shared)· body 里的 json.Marshal encoder 引用 · 保 import 用
var _ = json.Marshal
