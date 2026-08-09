package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/passenger"
)

// 个人邀请码（GET /api/me/invite · decisions §8.29 / §8.32）。
//
// **对外只给结果**（CLAUDE.md §0.1）：码 / 邀请了几人 / 还剩几次减免。
// 不出内部字段：不返 fee_waiver_total 的计算规则、不返推荐关系明细、
// 不返"每邀 1 人给几次"这个营销参数（那是我方成本结构）。

type inviteResp struct {
	// Code 我的个人邀请码 · 发给朋友让他注册时填
	Code string `json:"code"`
	// InvitedCount 我成功邀请了几个人注册
	InvitedCount int `json:"invited_count"`
	// WaiverRemaining 还剩几次手续费减免
	WaiverRemaining int `json:"waiver_remaining"`
	// WaiverUsed 已经用掉几次
	WaiverUsed int `json:"waiver_used"`
	// Referrals 邀请记录（最新在前）
	Referrals []referralDTO `json:"referrals"`
}

// referralDTO 一条邀请记录。
//
// **被邀请人只给脱敏标识** —— 邀请人不该拿到第三方的完整邮箱（PII）。
// 留前 3 位够他认出是谁·又不能拿去撞库 / 群发。
type referralDTO struct {
	// Invitee 脱敏后的被邀请人（如 `zha***@gmail.com`）
	Invitee string `json:"invitee"`
	// WaiverGranted 这条带来几次减免额度
	WaiverGranted int `json:"waiver_granted"`
	// JoinedAt 他注册的时间
	JoinedAt string `json:"joined_at"`
}

// handleGetMyInvite · GET /api/me/invite
//
// 没有码的老账号读到就补（EnsurePersonalCode 幂等）· 不做批量 migration。
func (s *Server) handleGetMyInvite(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	pi, err := s.passengers.EnsurePersonalCode(r.Context(), p.ID)
	if err != nil {
		return err
	}
	refs, err := s.passengers.ListReferrals(r.Context(), p.ID, 50)
	if err != nil {
		return err
	}
	items := make([]referralDTO, 0, len(refs))
	for _, x := range refs {
		items = append(items, referralDTO{
			Invitee:       x.InviteeMasked,
			WaiverGranted: x.WaiverGranted,
			JoinedAt:      x.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, inviteResp{
		Code:            pi.Code,
		InvitedCount:    pi.InvitedCount,
		WaiverRemaining: pi.Remaining(),
		WaiverUsed:      pi.FeeWaiverUsed,
		Referrals:       items,
	})
	return nil
}

type bindCodeReq struct {
	Code string `json:"code"`
}

// handleBindSystemCode · POST /api/me/community-code
//
// 已注册用户补绑专属邀请码 · 拿社群身份（看 vendor 真名 + 社群价）。
// 对外文案叫「专属邀请码」· 跟好友的个人邀请码区分（用户会混）。
//
// 错误码：
//   - 404 码无效 / 停用 / 过期 / 用满（**不区分** —— 防枚举）
//   - 409 已经是社群成员（一个账号只能绑一次）
func (s *Server) handleBindSystemCode(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	var req bindCodeReq
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.Code == "" {
		return ErrBadRequest("缺 code")
	}

	switch err := s.passengers.BindSystemCode(r.Context(), p.ID, req.Code); {
	case errors.Is(err, passenger.ErrInviteCodeInvalid):
		return ErrNotFound("这个专属邀请码无效、已停用或已用满")
	case errors.Is(err, passenger.ErrAlreadyMember):
		return newFail(http.StatusConflict, "already_member", "你已经是社群成员了")
	case err != nil:
		return err
	}

	// 返回更新后的账号（前端要刷新 invited 状态 · 影响 vendor 显示名和价格）
	fresh, err := s.passengers.ByID(r.Context(), p.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, profileOf(fresh))
	return nil
}
