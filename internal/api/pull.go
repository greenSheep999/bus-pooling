package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/coupon"
	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
)

// pullRequest / pullResponse 是 POST /api/me/pull 的对外形状（05-api-contract §5）。
//
// **对外只暴露最终三项金额** + `credential_ids`（派发用）+ 元信息。
// 各分层（key_cost / vendor_fee / …）**不出响应体**（CLAUDE.md §0.1）。
type pullRequest struct {
	Count    int    `json:"count"`
	VendorID string `json:"vendor_id,omitempty"` // 乘客偏好；服务端可否决
	Zone     string `json:"zone,omitempty"`      // us | eu | 空
	// CouponCode 阶段 1a 收但不消费 —— 前端确认窗允许填优惠码 ·
	// 后端估价还没接优惠逻辑（decisions §8.20）· 收下防 decodeStrict 拒未知字段
	CouponCode string `json:"coupon_code,omitempty"`
	// Offer 维度（docs/24 §5 · Step 5d）:
	//   AccountKind · 本轮买 enterprise / personal · 空 = enterprise（兼容老前端）
	//   Plan · 订阅档 power / pro / pro_plus / pro_max · 空 = 不指定
	// 手动拉号时是**硬约束** —— 用户点了"拉个人号"不能因缺货降级买企业号（docs/24 §7）
	AccountKind string `json:"account_kind,omitempty"`
	Plan        string `json:"plan,omitempty"`
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

	// decisions §8.43 v2 · 优惠码 pull 场景校验 · service_fee_waiver type
	// 阶段 1:只 Lookup 不 Redeem · 拉号本身还没真跑(需要上游 vendor 支持 · #190 挂起)
	// 校验通过 = 前端 UI 收 200 · 不报错("码适用此场景")· 等真拉号联通后再补 Redeem + service_fee_waived 逻辑
	if s.coupons != nil && strings.TrimSpace(req.CouponCode) != "" {
		if _, err := s.coupons.Lookup(r.Context(), req.CouponCode, coupon.TypeServiceFeeWaiver); err != nil {
			return translateCouponErr(err)
		}
	}
	// 前端会发 "auto" 表示"让系统派" —— 服务端等价于空
	if req.VendorID == "auto" {
		req.VendorID = ""
	}
	if req.Zone == "auto" {
		req.Zone = ""
	}
	// 请求指定 vendor 时·校验已装配·否则 400（防让请求走到 decider 才发现）
	if req.VendorID != "" && s.decider != nil {
		known := false
		for _, id := range s.decider.KnownVendors() {
			if string(id) == req.VendorID {
				known = true
				break
			}
		}
		if !known {
			return ErrBadRequest("请求的 vendor 未装配·请换或不填")
		}
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

	// 1f-C · 策略优先级铁律 · record 单独拉(busID 空 · 不查车级 · §4.3.3 底部)。
	// request override 携带用户显式指定的 count/vendor/zone(手动动作)。
	reqOverride := buildManualPullOverride(req)
	eff, err := s.effective(r.Context(), p.ID, "", reqOverride)
	if err != nil {
		return err
	}
	bal, err := s.wallets.Get(r.Context(), p.ID)
	if err != nil {
		return err
	}
	used, err := s.wallets.TodayUsage(r.Context(), p.ID)
	if err != nil {
		return err
	}
	// 硬护栏(余额/上限)校验 · BusMaxUnitPrice 留 nil(record 无车级 · §8.27)
	_, err = s.strategies.CanPull(r.Context(), p.ID, strategy.CheckInput{
		BusID:   "",
		Count:   req.Count,
		Balance: bal.Balance,
		Used:    strategy.Usage{Rounds: used.Rounds, Spend: used.Spend},
	})
	if err != nil {
		if fail := translateStrategyErr(err); fail != nil {
			return fail
		}
		return err
	}

	// vendor / zone / max_price 由 Effective 按 §4.3.1 优先级挑好 · 直接用。
	vendorID := eff.PreferredVendor
	zoneOut := eff.Zone
	if zoneOut == strategy.ZoneAuto {
		zoneOut = ""
	}
	result, err := s.decider.Pull(r.Context(), decider.PullInput{
		PassengerID:         p.ID,
		BusID:               "",
		Count:               req.Count,
		Zone:                providers.Zone(zoneOut),
		VendorID:            providers.VendorID(vendorID),
		IdempotencyRecordID: hit.recordID,
		// 生效的单价上限 · 由 Effective 取严得到 · 0 = 不限 ·
		// 传给 decider 两个用途:
		//   ① 缺货挂单时存进 stock_watcher · fire 时继续守同一上限
		//   ② 换算成 vendor 币种传涨价保护(部分 vendor 原生支持)
		MaxUnitPrice: eff.MaxUnitPrice,
		// Offer 维度 · 空 = enterprise（兼容未升级的前端）
		AccountKind: providers.AccountKind(req.AccountKind),
		Plan:        providers.SubscriptionPlan(req.Plan),
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

	// 并发限流（§8.35 #18）· 429 让客户端稍后重试 —— 不是失败·是"现在别挤"
	case errors.Is(err, decider.ErrPassengerBusy):
		return &Fail{Status: http.StatusTooManyRequests,
			Err: &Error{Code: CodeRateLimited, Message: "你有拉号正在进行中，等它完成再试"}}
	case errors.Is(err, decider.ErrVendorBusy):
		return &Fail{Status: http.StatusTooManyRequests,
			Err: &Error{Code: CodeRateLimited, Message: "当前拉号请求太多，稍后再试"}}
	// 数量超区间 → 400。**不能直接透 err.Error()** —— 那串带内部包名前缀
	// （CLAUDE.md §0.1 对外 message 不出内部术语）· 用 decider 给的纯数字重组
	case errors.Is(err, decider.ErrCountOutOfRange):
		if lo, hi, ok := decider.CountRangeOf(err); ok {
			return ErrBadRequest(fmt.Sprintf("一次最少 %d 个、最多 %d 个", lo, hi))
		}
		return ErrBadRequest("拉号数量超出允许范围")

	// 车里没人能分摊（全挂起 / 全余额不足）· §8.35 #3/#4
	case errors.Is(err, decider.ErrNoPayableMember):
		return ErrConflict("no_payable_member",
			"车里没有能分摊的车友 · 等他们充值或先解挂")
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
