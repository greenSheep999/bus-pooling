package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
)

// pullRequest / pullResponse 是 POST /api/me/pull 的对外形状（05-api-contract §5）。
//
// **对外只暴露最终三项金额** + `credential_ids`（派发用）+ 元信息。
// 加价链的各分层（key_cost / vendor_fee / …）**不出响应体**（CLAUDE.md §0.1）。
type pullRequest struct {
	Count    int    `json:"count"`
	VendorID string `json:"vendor_id,omitempty"` // 乘客偏好；服务端可否决
	Zone     string `json:"zone,omitempty"`      // us | eu | 空
}

type pullResponse struct {
	PullRoundID      string   `json:"pull_round_id"`
	VendorID         string   `json:"vendor_id"`
	Purchased        int      `json:"purchased"`
	CredentialIDs    []string `json:"credential_ids"`
	UnitPrice        int64    `json:"unit_price"`
	ServiceFee       int64    `json:"service_fee"`
	TotalDebit       int64    `json:"total_debit"`
	BalanceRemaining int64    `json:"balance_remaining"`
}

// handlePull 走完整个拉号闭环：幂等 → strategy 校验 → decider.Pull → 落幂等响应。
//
// 顺序**不能改**：
//   - 幂等必须在最前 —— 重放直接返 first response
//   - strategy 校验早于 vendor 调用 —— 上限触发就不该产生 pending_purchase 行
//   - 幂等响应落库放在最后 —— 只有真跑完才有 first response 可回
func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) error {
	if s.decider == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal,
			"拉号服务暂未装配（阶段 1a 部分部署）")
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}

	body, err := readBody(r)
	if err != nil {
		return err
	}
	var req pullRequest
	if err := decodeStrict(body, &req); err != nil {
		return err
	}
	if req.Count < 1 {
		return ErrBadRequest("count 必须 ≥ 1")
	}

	key := r.Header.Get("X-Idempotency-Key")
	if key == "" {
		return newFail(http.StatusBadRequest, CodeBadIdempotencyKey,
			"拉号必须带 X-Idempotency-Key（32 位十六进制）")
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

	// strategy 校验（读余额 + 用量）
	bal, err := s.wallets.Get(r.Context(), p.ID)
	if err != nil {
		return err
	}
	used, err := s.wallets.TodayUsage(r.Context(), p.ID)
	if err != nil {
		return err
	}
	// 单独拉号 · 没有 bus 级上限
	if _, err := s.strategies.CanPull(r.Context(), p.ID, strategy.CheckInput{
		BusID:   "",
		Count:   req.Count,
		Balance: bal.Balance,
		Used:    strategy.Usage{Rounds: used.Rounds, Spend: used.Spend},
	}); err != nil {
		if fail := translateStrategyErr(err); fail != nil {
			return fail
		}
		return err
	}

	// 交给 decider 走完 5 状态
	result, err := s.decider.Pull(r.Context(), decider.PullInput{
		PassengerID:         p.ID,
		BusID:               "",
		Count:               req.Count,
		Zone:                providers.Zone(req.Zone),
		IdempotencyRecordID: hit.recordID,
	})
	if err != nil {
		if fail := translateDeciderErr(err); fail != nil {
			return fail
		}
		return err
	}

	resp := pullResponse{
		PullRoundID:      result.PullRoundID,
		VendorID:         result.VendorID,
		Purchased:        result.Purchased,
		CredentialIDs:    result.CredentialIDs,
		UnitPrice:        result.UnitPrice,
		ServiceFee:       result.ServiceFee,
		TotalDebit:       result.TotalDebit,
		BalanceRemaining: result.BalanceRemaining,
	}

	// 先写响应，再落幂等（顺序反了的话，客户端拿到响应后重放会拿到 conflict）
	respBody, _ := json.Marshal(resp)
	_ = saveIdempotentResponse(r.Context(), s.db, hit.recordID, http.StatusOK, respBody)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
	return nil
}

// translateStrategyErr 把 strategy 的上限 / 余额错误映射到对外错误码。
func translateStrategyErr(err error) *Fail {
	if f := failFromLimitError(err); f != nil {
		return f
	}
	if errors.Is(err, strategy.ErrInsufficientBalance) {
		return ErrInsufficientBalance("")
	}
	if errors.Is(err, strategy.ErrBadCount) {
		return ErrBadRequest("count 数量不合法")
	}
	// 未识别的 strategy 错就当 500 上报 —— 别把内部错误往外透
	return nil
}

// translateDeciderErr 把 decider sentinel 映射到对外错误码。
func translateDeciderErr(err error) *Fail {
	switch {
	case errors.Is(err, decider.ErrInsufficientBalance):
		return ErrInsufficientBalance("")
	case errors.Is(err, decider.ErrNoStock):
		return ErrConflict("no_stock", "暂无可拉的号，稍后再试")
	case errors.Is(err, decider.ErrRateLimited):
		return &Fail{Status: http.StatusTooManyRequests,
			Err: &Error{Code: CodeRateLimited, Message: "请求太频繁，稍后再试"}}
	case errors.Is(err, decider.ErrPurchaseCap):
		return ErrConflict("purchase_cap_reached", "已达上限，请等已有的号失效后再拉")
	case errors.Is(err, decider.ErrNeedManual):
		return newFail(http.StatusInternalServerError, CodeInternal,
			"这笔拉号需要客服核对，请稍等")
	}
	// 未识别的返 nil，让上层当 500 处理（把细节留在日志里）
	return nil
}

// readBody 把请求体读完（带上限 · 兼容 decodeJSON 的行为），供幂等指纹用。
func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	limited := http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, &Fail{Status: http.StatusRequestEntityTooLarge,
				Err: &Error{Code: CodeBodyTooLarge, Message: "请求内容太大"}}
		}
		return nil, ErrBadJSON("读请求失败")
	}
	return body, nil
}

// decodeStrict 用 DisallowUnknownFields 严格解 —— 未知字段拒收（跟 decodeJSON 一致）。
func decodeStrict(body []byte, dst any) error {
	if len(body) == 0 {
		return ErrBadJSON("请求内容为空")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return ErrBadJSON("")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return ErrBadJSON("请求内容格式不对")
	}
	return nil
}
