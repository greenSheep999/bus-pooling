package api

import (
	"net/http"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/coupon"
)

// couponLookupResponse · 前端预览用 · 不改任何 DB 状态
// **对外只暴露决策数据** —— code / type / discount_bp / waive_rounds · 见 §8.43 v2
// 内部字段(coupon_code.id / used_count / status / memo) 不出去(§0.1)
type couponLookupResponse struct {
	Code        string `json:"code"`
	Type        string `json:"type"`                   // topup_discount | service_fee_waiver
	DiscountBP  int64  `json:"discount_bp,omitempty"`  // topup_discount 时给
	WaiveRounds int64  `json:"waive_rounds,omitempty"` // service_fee_waiver 时给
}

// handleCouponLookup 优惠码预校验 · GET /api/me/coupons/lookup?code=X&context=topup|pull
//
// 用途:充值弹窗输码后 debounce 调 · 前端预览显示"优惠 -X.XX USD"
//
// **不消耗额度** —— 真核销发生在 topup 起单 / pull 触发时(handleCreateTopup / handlePull)
// 幂等:每次调都过一次 Lookup · remaining_uses / expires 都会重新判 · 稳定。
func (s *Server) handleCouponLookup(w http.ResponseWriter, r *http.Request) error {
	if s.coupons == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "优惠码服务未装配")
	}
	if _, err := mustCaller(r); err != nil {
		return err
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		return ErrBadRequest("code 不能空")
	}
	ctxParam := r.URL.Query().Get("context")
	var wantType coupon.Type
	switch ctxParam {
	case "topup":
		wantType = coupon.TypeTopupDiscount
	case "pull":
		wantType = coupon.TypeServiceFeeWaiver
	default:
		return ErrBadRequest("context 必须是 topup 或 pull")
	}

	c, err := s.coupons.Lookup(r.Context(), code, wantType)
	if err != nil {
		return translateCouponErr(err)
	}

	writeJSON(w, http.StatusOK, couponLookupResponse{
		Code:        c.Code,
		Type:        string(c.Type),
		DiscountBP:  c.DiscountBP,
		WaiveRounds: c.WaiveRounds,
	})
	return nil
}
