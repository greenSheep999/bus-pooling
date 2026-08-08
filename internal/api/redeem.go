package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/redeem"
)

// redeemRequest / redeemResponse 是 POST /api/me/redeem 的对外形状。
//
// 对外形状对齐 web/src/types/index.ts 的 RedeemResult（TS 是可执行契约）。
// 内部 status 多态（unused / used / expired）**不出响应体** —— 只返回入账结果。
type redeemRequest struct {
	Code string `json:"code"`
}

type redeemResponse struct {
	Credits      int64  `json:"credits"`       // 到账积分（microunit）
	Memo         string `json:"memo"`          // "兑换码 KRC-XXXX"
	BalanceAfter int64  `json:"balance_after"` // 入账后余额（前端顺手更新）
}

func (s *Server) handleRedeem(w http.ResponseWriter, r *http.Request) error {
	if s.redeems == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "兑换服务暂未装配")
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}

	body, err := readBody(r)
	if err != nil {
		return err
	}
	var req redeemRequest
	if err := decodeStrict(body, &req); err != nil {
		return err
	}
	if redeem.Normalize(req.Code) == "" {
		return ErrBadRequest("请填兑换码")
	}

	// 幂等键**可选**，但强烈建议客户端带（重放场景保证响应字节一致）
	// 未带 key 时 redeem.Consume 内部有并发保护（条件 UPDATE + 单一乘客 replay）
	if key := r.Header.Get("X-Idempotency-Key"); key != "" {
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

		result, err := s.redeems.Consume(r.Context(), p.ID, req.Code)
		if err != nil {
			return translateRedeemErr(err)
		}
		resp := redeemResponse{
			Credits:      result.Credits,
			Memo:         "兑换码 " + redeem.Normalize(req.Code),
			BalanceAfter: result.BalanceAfter,
		}
		respBody, _ := json.Marshal(resp)
		_ = saveIdempotentResponse(r.Context(), s.db, hit.recordID, http.StatusOK, respBody)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBody)
		return nil
	}

	result, err := s.redeems.Consume(r.Context(), p.ID, req.Code)
	if err != nil {
		return translateRedeemErr(err)
	}
	writeJSON(w, http.StatusOK, redeemResponse{
		Credits:      result.Credits,
		Memo:         "兑换码 " + redeem.Normalize(req.Code),
		BalanceAfter: result.BalanceAfter,
	})
	return nil
}

// translateRedeemErr 把内部错误映射成对外错误码 —— message 一律人话，
// 不给「已被其他账号使用」这种细节泄露（防用扫码器猜别人有没有兑过）。
func translateRedeemErr(err error) error {
	switch {
	case errors.Is(err, redeem.ErrNotFound):
		return ErrBadRequest("兑换码无效")
	case errors.Is(err, redeem.ErrEmptyCode):
		return ErrBadRequest("请填兑换码")
	case errors.Is(err, redeem.ErrUsed), errors.Is(err, redeem.ErrClaimedByOther):
		return ErrConflict(CodeConflict, "兑换码已被使用")
	case errors.Is(err, redeem.ErrExpired):
		return ErrConflict(CodeConflict, "兑换码已过期")
	}
	return err
}
