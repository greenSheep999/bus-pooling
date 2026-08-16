package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/coupon"
	"github.com/bus-pooling/bus-pooling/internal/paymentgw"
	"github.com/bus-pooling/bus-pooling/internal/topup"
	"github.com/bus-pooling/bus-pooling/internal/topupchannel"
)

// topupRequest / topupOrderResponse 对外形状。
//
// 请求字段 `credits`：乘客目标积分（要净到账的数字）。手续费 5% 加在本金上（CLAUDE.md §1.4）。
// 请求字段 `channel`：具体渠道 id · 由 topupchannel 包的 registry 定义。
// 空 = 用 default（未来算法选路 · 现在仅第一家 hosted 渠道）。
// 请求字段 `payer_reference`：direct rail 渠道需要（UID / ID / wallet 地址等）· hosted 可空。
//
// 响应字段对齐 web/src/types/index.ts 的 TopupOrder：
//   order_id / checkout_url / paid / credits / expires_at + status
type topupRequest struct {
	Credits        int64  `json:"credits"`
	Channel        string `json:"channel"`
	PayerReference string `json:"payer_reference,omitempty"` // direct rail 用（Bybit UID / Binance ID / wallet 地址）
	// CouponCode 社群发放的一次性充值优惠码(decisions §8.43)· 减实付 USD · 不加积分
	// 阶段 1(sprint-1e)只落库不算减免 · 减免规则 sprint-1f 起手接
	CouponCode string `json:"coupon_code,omitempty"`
}

// topupOrderResponse 单张充值单的对外形状。
//
// 对外只暴露决策数据：ID / 支付跳转 / 显示所需的两个金额 / 过期时间 / 状态。
// **不出**：wallet_ledger_id、pending 状态机的中间态、gateway payment id（内部关联）。
type topupOrderResponse struct {
	OrderID     string `json:"order_id"`
	CheckoutURL string `json:"checkout_url"`         // gateway.instructions.checkout_url · 前端跳转
	QRContent   string `json:"qr_content,omitempty"` // 有 QR 的 rail 才给·hosted checkout 一般没
	Paid        int64  `json:"paid"`                 // 乘客支付总积分 = credits + channel_fee
	Credits     int64  `json:"credits"`              // 净到账
	ExpiresAt   string `json:"expires_at"`
	Status      string `json:"status"` // pending | paid | failed
	PaidAt      string `json:"paid_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	// FeeWaived 这单用掉了一次手续费减免（个人邀请码额度 · decisions §8.29）
	// **只给这个 bool** —— 不出 fee_subsidy（我方垫付多少是成本结构 · CLAUDE.md §0.1）
	FeeWaived bool `json:"fee_waived,omitempty"`
}

// TopupOrderTTL 起单后未支付的过期时长。定 const 而不是配置：15 分钟是通道商
// 收款链接的常规 TTL·改的话得同步改 gateway 侧。
const TopupOrderTTL = 15 * time.Minute

// couponAppliedInfo · 优惠码校验后传入建单/减 USD 的中间值
type couponAppliedInfo struct {
	CouponID   string // coupon_code.id
	Code       string // 大写规范后的码
	DiscountBP int64  // 折扣百分点(500=5%, 2000=20%)
}

// translateCouponErr · 把 coupon 包错误翻译成 Fail 返给前端
func translateCouponErr(err error) error {
	switch {
	case errors.Is(err, coupon.ErrNotFound):
		return ErrBadRequest("优惠码不存在")
	case errors.Is(err, coupon.ErrDisabled):
		return ErrBadRequest("优惠码已停用")
	case errors.Is(err, coupon.ErrExpired):
		return ErrBadRequest("优惠码已过期")
	case errors.Is(err, coupon.ErrUsedUp):
		return ErrBadRequest("优惠码额度已用尽")
	case errors.Is(err, coupon.ErrWrongContext):
		return ErrBadRequest("优惠码不适用此场景")
	default:
		return err
	}
}

// usdRateCNY 展示层 CNY/USD 汇率（CLAUDE.md §1.4）。const 而不是配置：
// 阶段 1a 汇率写死·等接了汇率服务再放开。前端和后端保持一致。
const usdRateCNY int64 = 7

// moneyMicro 内部所有 money 都是整数微单位 · 1 CNY = 1_000_000
const moneyMicro int64 = 1_000_000

// microToDecimalString 把微单位积分转成 "10.50" 这种十进制字符串（给 gateway）。
// gateway 要求 Amount 是字符串·允许 0-18 位小数。我方内部固定 6 位小数。
func microToDecimalString(micro int64) string {
	whole := micro / moneyMicro
	frac := micro % moneyMicro
	if frac == 0 {
		return fmt.Sprintf("%d", whole)
	}
	// 6 位小数·去尾零保持可读性
	s := fmt.Sprintf("%d.%06d", whole, frac)
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

// topupChannelResp 一个渠道对外形状（前端确认窗按此渲染）。
//
// 三维属性都暴露 · 前端可按 region 分区 · 按 rail 分组 · 按 enabled 决定能否点。
// **provider_kind 是我方对 gateway 的实现细节 · 不暴露**（术语铁律 §12.6 · CLAUDE.md §0.1）。
type topupChannelResp struct {
	ID                     string `json:"id"`                                // 渠道稳定 id
	DisplayName            string `json:"display_name"`                      // 前端展示名
	Region                 string `json:"region"`                            // domestic | overseas
	Rail                   string `json:"rail"`                              // direct | hosted
	Asset                  string `json:"asset"`                             // USD | USDT | CNY | ...
	Enabled                bool   `json:"enabled"`                           // 关的前端可展示但不能下单
	RequiresPayerReference bool   `json:"requires_payer_reference"`          // direct rail 通常要
	PayerReferenceLabel    string `json:"payer_reference_label,omitempty"`   // 前端表单标签
	Note                   string `json:"note,omitempty"`                    // 展示给乘客的一行提示
}

func (s *Server) handleListTopupChannels(w http.ResponseWriter, r *http.Request) error {
	if s.topupChannels == nil {
		writeJSON(w, http.StatusOK, map[string]any{"channels": []topupChannelResp{}})
		return nil
	}
	all := s.topupChannels.List()
	out := make([]topupChannelResp, 0, len(all))
	for _, c := range all {
		out = append(out, topupChannelResp{
			ID:                     string(c.ID),
			DisplayName:            c.DisplayName,
			Region:                 string(c.Region),
			Rail:                   string(c.Rail),
			Asset:                  c.Asset,
			Enabled:                c.Enabled,
			RequiresPayerReference: c.RequiresPayerReference,
			PayerReferenceLabel:    c.PayerReferenceLabel,
			Note:                   c.Note,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
	return nil
}

func (s *Server) handleCreateTopup(w http.ResponseWriter, r *http.Request) error {
	if s.topups == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "充值服务暂未装配")
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}

	body, err := readBody(r)
	if err != nil {
		return err
	}
	var req topupRequest
	if err := decodeStrict(body, &req); err != nil {
		return err
	}
	if req.Credits <= 0 {
		return ErrBadRequest("请填要充值的积分数量")
	}
	if req.Channel == "" {
		req.Channel = "waffo"
	}
	if s.topupChannels == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "充值渠道未装配")
	}
	channel, err := s.topupChannels.GetEnabled(req.Channel)
	if err != nil {
		if errors.Is(err, topupchannel.ErrUnknownChannel) {
			return ErrBadRequest("未知支付渠道")
		}
		if errors.Is(err, topupchannel.ErrDisabledChannel) {
			return newFail(http.StatusServiceUnavailable, "channel_disabled",
				"该支付渠道暂未开放·请换其他方式")
		}
		return err
	}
	// direct rail 校验 payer_reference · hosted 用 profile email
	payerRef := strings.TrimSpace(req.PayerReference)
	if channel.RequiresPayerReference && payerRef == "" {
		return ErrBadRequest("该渠道需要 " + channel.PayerReferenceLabel)
	}
	if payerRef == "" && channel.Rail == topupchannel.RailHosted {
		payerRef = p.Email
	}

	// 幂等键：契约（05-api-contract §基础）规定充值起单**必须**带
	key := r.Header.Get("X-Idempotency-Key")
	if key == "" {
		return newFail(http.StatusBadRequest, CodeBadIdempotencyKey,
			"充值起单必须带 X-Idempotency-Key（32 位十六进制）")
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

	// 步骤 0.5:优惠码校验 · decisions §8.43 v2
	// coupons=nil 或 code 空 · 跳过 · 走无折扣路径
	// 校验失败(码错/过期/用尽/type 不对) · 返 4xx 让前端提示
	// **只 Lookup 不 Redeem** —— Redeem 在 gateway 建单成功后再做(防"码扣了但订单建失败")
	var couponInfo *couponAppliedInfo
	if s.coupons != nil && strings.TrimSpace(req.CouponCode) != "" {
		c, err := s.coupons.Lookup(r.Context(), req.CouponCode, coupon.TypeTopupDiscount)
		if err != nil {
			return translateCouponErr(err)
		}
		couponInfo = &couponAppliedInfo{
			CouponID:   c.ID,
			DiscountBP: c.DiscountBP,
			Code:       c.Code,
		}
	}

	// 步骤 1:**P1-2 修** · order + pending_topup 原子创建(同一事务)
	//        以前分两步·中间崩溃留 order 但 pending 缺失·janitor 扫不到。
	//        pendingTopups 未装配(早期 DRY_RUN)时退化为单表 CreateOrderIn。
	placeholderURL := "pending://gateway"
	var order topup.Order
	if s.pendingTopups != nil {
		o, _, err := s.topups.CreateOrderWithPending(r.Context(), topup.OrderInput{
			PassengerID:    p.ID,
			Channel:        req.Channel,
			Region:         string(channel.Region),
			Rail:           string(channel.Rail),
			ProviderKind:   channel.ProviderKind,
			PayerReference: payerRef,
			Credits:        req.Credits,
			PayURL:         placeholderURL,
			TTL:            TopupOrderTTL,
			CouponCode:     strings.TrimSpace(req.CouponCode),
		}, hit.recordID)
		if err != nil {
			return translateTopupErr(err)
		}
		order = o
	} else {
		o, err := s.topups.CreateOrderIn(r.Context(), topup.OrderInput{
			PassengerID:    p.ID,
			Channel:        req.Channel,
			Region:         string(channel.Region),
			Rail:           string(channel.Rail),
			ProviderKind:   channel.ProviderKind,
			PayerReference: payerRef,
			Credits:        req.Credits,
			PayURL:         placeholderURL,
			TTL:            TopupOrderTTL,
			CouponCode:     strings.TrimSpace(req.CouponCode),
		})
		if err != nil {
			return translateTopupErr(err)
		}
		order = o
	}

	// 步骤 2：调 gateway 建单（可选·未装配时退回 mock 兼容）
	if s.paymentGW != nil {
		// **P0 修**（审计二轮发现）：CreatePayment 之前必须先把 pending_topup 落到
		// gateway_creating · 崩溃后 janitor 用 client_order_id 反查 · 避免"已收款但 expire"丢单。
		// 落库失败**必须 hard fail**·**不能**继续调 CreatePayment —— 不然崩溃后
		// pending 还在 initial 里·janitor 走 initial → expire 分支·gateway 已收款 = 丢单。
		//
		// paymentGW != nil 时 pendingTopups 也应非 nil（装配层校验·见 buildDecider）。
		// 这里再兜一层：nil 直接 502·别赌。
		if s.pendingTopups == nil {
			slog.Error("paymentGW 装配了但 pendingTopups nil · 状态无法落库 · 拒起单",
				"order_id", order.ID)
			return newFail(http.StatusServiceUnavailable, CodeInternal,
				"充值服务未完全装配·请联系管理员")
		}
		if err := s.pendingTopups.EnsureAtLeast(r.Context(), order.ID, topup.PendingGatewayCreating); err != nil {
			slog.Error("pending_topup 推 gateway_creating 失败 · 拒调 CreatePayment 防丢单",
				"order_id", order.ID, "err", err)
			return newFail(http.StatusInternalServerError, CodeInternal,
				"充值起单失败·请稍后重试")
		}
		// asset 按 channel 属性决定（registry 里配 · USD / USDT / CNY / ...）·
		// 内部积分永远是 CNY 微单位·计算展示汇率时按 asset 折算
		// 1 积分 ≡ 1 CNY · USD/USDT 汇率约 7 · 展示层做换算（CLAUDE.md §1.4）
		amountMicro := order.Paid / usdRateCNY

		// 优惠码 topup_discount 减 USD 实付 · decisions §8.43 v2
		// 折后金额 = 原额 × (1 - discount_bp/10000)
		// **只减 gateway 侧收的额** —— 积分数量不动(想充 N 到账 N)· wallet_ledger recharge / channel_fee 也不动
		// 差额由我方承担(coupon_use.discount_amount 记账 · 未来结算再对)
		if couponInfo != nil {
			// discount = amountMicro * discount_bp / 10000
			discount := amountMicro * couponInfo.DiscountBP / 10000
			amountMicro = amountMicro - discount
			if amountMicro < 1 {
				amountMicro = 1 // 别减到 0/负 · gateway 会拒
			}
			slog.Info("topup coupon 折扣已应用",
				"order_id", order.ID, "code", couponInfo.Code, "discount_bp", couponInfo.DiscountBP,
				"amount_after_micro", amountMicro)
		}

		gwReq := paymentgw.CreatePaymentRequest{
			ClientOrderID:    order.ID,
			ProviderKind:     channel.ProviderKind,
			ExpectedAmount:   microToDecimalString(amountMicro),
			ExpectedAsset:    channel.Asset,
			PayerEmail:       p.Email, // hosted rail 用 · direct rail gateway 忽略
			PayerReference:   payerRef, // direct rail 用（乘客提供的 UID）· hosted 也发不影响
			ExpiresInSeconds: int(TopupOrderTTL / time.Second),
		}
		if s.paymentGWSuccessURL != "" {
			gwReq.SuccessURL = s.paymentGWSuccessURL
		}
		// **P0 修**（codex 三轮）：冷冻 request 到 topup_order · janitor 反查用它重 POST。
		// 从当前 config 重建 request 会因为汇率 / channel / email 变化命中不同的幂等指纹。
		// 必须在 CreatePayment **之前**落库·崩溃在中间时 snapshot 已就绪。
		snapshot, mErr := json.Marshal(gwReq)
		if mErr != nil {
			slog.Error("序列化 gateway request 失败 · 拒调 CreatePayment", "order_id", order.ID, "err", mErr)
			return newFail(http.StatusInternalServerError, CodeInternal,
				"充值起单失败·请稍后重试")
		}
		if err := s.topups.SaveGatewayRequestSnapshot(r.Context(), order.ID, snapshot); err != nil {
			slog.Error("落 gateway_request_snapshot 失败 · 拒调 CreatePayment 防反查失效",
				"order_id", order.ID, "err", err)
			return newFail(http.StatusInternalServerError, CodeInternal,
				"充值起单失败·请稍后重试")
		}
		payment, err := s.paymentGW.CreatePayment(r.Context(), gwReq)
		if err != nil {
			// gateway 报错 · pending_topup 保留 gateway_creating · janitor 后续用
			// client_order_id 反查（gateway 端可能已建单也可能没建 · 反查决定）
			slog.Warn("paymentgw create 失败", "order_id", order.ID, "err", err)
			return newFail(http.StatusBadGateway, "payment_gateway_error",
				"支付通道暂时不可用，请稍后再试")
		}
		checkoutURL := ""
		qrContent := ""
		if payment.Instructions != nil {
			checkoutURL = payment.Instructions.CheckoutURL
			qrContent = payment.Instructions.QRContent
		}
		if err := s.topups.AttachGateway(r.Context(), order.ID, payment.ID, checkoutURL, qrContent); err != nil {
			slog.Error("paymentgw 回写失败", "order_id", order.ID, "err", err)
			return newFail(http.StatusInternalServerError, CodeInternal, "起单失败，请稍后重试")
		}
		order.GatewayPaymentID = payment.ID
		order.CheckoutURL = checkoutURL
		order.QRContent = qrContent
		// pending_topup: 推到至少 gateway_ordered · 用 EnsureAtLeast 兼容 webhook 早到（已 gateway_paid）
		if s.pendingTopups != nil {
			if err := s.pendingTopups.EnsureAtLeast(r.Context(), order.ID, topup.PendingGatewayOrdered); err != nil {
				slog.Warn("pending_topup 推 gateway_ordered 失败·主流程继续",
					"order_id", order.ID, "err", err)
			}
		}
	} else {
		// 无 gateway · 走 dev mock 路径 · 前端展示这个假 URL 触发 BP_ENABLE_DEV_TOPUP 端点标 paid
		fullURL := "https://mock-checkout.example/order/" + order.ID
		if err := s.topups.AttachGateway(r.Context(), order.ID, "", fullURL, ""); err != nil {
			return err
		}
		order.CheckoutURL = fullURL
		// mock 路径也推到 gateway_ordered · dev-mark-paid 会一路推到 completed
		if s.pendingTopups != nil {
			_ = s.pendingTopups.EnsureAtLeast(r.Context(), order.ID, topup.PendingGatewayOrdered)
		}
	}

	// 步骤 3:优惠码 Redeem · 落 coupon_use + 扣 remaining_uses(§8.43 v2)
	// 到这里 order 已成功建 + gateway 已收单 · 可以安全消耗额度。
	// 幂等: 同一 order.ID 二次调返 ErrAlreadyUsed · 不重复扣。
	// 校验前面 Lookup 已过 · 这里再 Lookup 一次(race window 内可能被别人用光)· 极小概率不匹配 log warn
	// discount_amount = 减了多少积分等值 microunit(用户视角看到的"少付了多少")
	if couponInfo != nil && s.coupons != nil {
		// 减免的等值积分 = order.Paid * discount_bp / 10000
		discountMicro := order.Paid * couponInfo.DiscountBP / 10000
		_, rerr := s.coupons.Redeem(r.Context(), coupon.RedeemInput{
			Code:           couponInfo.Code,
			PassengerID:    p.ID,
			Context:        coupon.ContextTopup,
			ContextRef:     order.ID,
			DiscountAmount: discountMicro,
		})
		if rerr != nil && !errors.Is(rerr, coupon.ErrAlreadyUsed) {
			// Redeem 失败但 gateway 已建单 —— 折扣已经在 gateway 侧生效
			// 只 log · 不打断响应(避免让用户支付流程失败 · 差额从内部对账)
			slog.Warn("topup coupon Redeem 失败·gateway 折扣已应用·后台对账",
				"order_id", order.ID, "code", couponInfo.Code, "err", rerr)
		}
	}

	resp := topupOrderResponseOf(order)
	respBody, _ := json.Marshal(resp)
	_ = saveIdempotentResponse(r.Context(), s.db, hit.recordID, http.StatusCreated, respBody)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(respBody)
	return nil
}

// handleGatewaySettlement 收 404bus-payment-gateway 结算回调。
//
// 契约（docs/integration/03-settlement.md）：
//  1. header X-404bus-Signature: v1={hex} · Timestamp · Event-Id
//  2. HMAC 覆盖 v1:{ts}:{raw_body}
//  3. 时间戳 ±5 分钟窗口
//  4. event_id 幂等去重
//  5. 处理完再返 2xx（gateway 收到 4xx/5xx 会重试）
//
// 我方策略：
//   - 签名 / 时间戳错 → 401 让 gateway 停重发（避免被中间人爆缓存）
//   - event_id 已见 → 200 outcome=duplicate
//   - kind=settled → MarkPaid（如未 paid）· 记 accepted
//   - kind=refunded / reversed → 阶段 1a 不做退款（TODO Iss #13）· 记 ignored·**返 200**（否则 gateway 重试到死）
func (s *Server) handleGatewaySettlement(w http.ResponseWriter, r *http.Request) error {
	if s.paymentGW == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "支付网关未装配")
	}
	// 契约明确：验签**必须**用原始字节 · 不能 decode 再 re-encode
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return ErrBadJSON("回调 body 读取失败")
	}

	sigHeader := r.Header.Get("X-404bus-Signature")
	tsHeader := r.Header.Get("X-404bus-Timestamp")
	eventID := r.Header.Get("X-404bus-Event-Id")

	if err := s.paymentGW.VerifySettlement(sigHeader, tsHeader, raw, time.Now()); err != nil {
		// 契约建议 gateway 收到 4xx 停重试·反正也不会成功
		slog.Warn("paymentgw settlement 验签失败",
			"event_id", eventID, "err", err, "ts", tsHeader)
		return &Fail{Status: http.StatusUnauthorized,
			Err: &Error{Code: "bad_signature", Message: "签名不通过"}}
	}

	var ev paymentgw.SettlementEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return ErrBadJSON("回调 body 不是合法 JSON")
	}
	if ev.EventID == "" || ev.GatewayPaymentID == "" {
		return ErrBadRequest("event_id / gateway_payment_id 缺失")
	}

	// event_id 幂等·同 event_id 已处理过就直接返 200
	var seen int
	if err := s.db.QueryRowContext(r.Context(),
		`SELECT count(1) FROM settlement_event WHERE event_id = ?`, ev.EventID).Scan(&seen); err != nil {
		return err
	}
	if seen > 0 {
		writeJSON(w, http.StatusOK, map[string]string{"outcome": "duplicate"})
		return nil
	}

	// 按 kind 分派 · state 和 kind 契约保证一致·优先看 kind
	kind := ev.Kind
	if kind == "" {
		kind = ev.State
	}

	outcome, detail := s.applySettlement(r.Context(), ev, kind)

	// P0-A/B 修：error:retry **不落 settlement_event**·让 gateway 重试
	//   - error:retry：DB / wallet 内部错·让 gateway 重试
	// unmatched 现在**加了 client_order_id fallback**·如果 fallback 也匹配不上·
	// 说明这不是我们发出的 order（gateway 配错 / 重放）·200 停重发合理。
	// 幂等表记 accepted / duplicate / ignored / unmatched·让重试能识别 duplicate。
	shouldRetry := outcome == "error:retry"
	if !shouldRetry {
		if _, err := s.db.ExecContext(r.Context(), `
			INSERT INTO settlement_event (event_id, gateway_payment_id, kind, received_at, outcome, detail)
			VALUES (?, ?, ?, ?, ?, ?)`,
			ev.EventID, ev.GatewayPaymentID, kind, nowRFC3339(), outcome, detail); err != nil {
			// 落幂等表失败·业务已处理·只 warn。重复回调会重跑一次 MarkPaid（幂等保护）
			slog.Warn("settlement_event 落库失败·业务已处理",
				"event_id", ev.EventID, "err", err)
		}
	}
	if shouldRetry {
		slog.Warn("settlement 未处理·让 gateway 重试",
			"event_id", ev.EventID, "outcome", outcome, "detail", detail)
		return newFail(http.StatusServiceUnavailable, "settlement_defer",
			"事件暂未匹配到订单或内部错误·请稍后重试")
	}
	writeJSON(w, http.StatusOK, map[string]string{"outcome": outcome})
	return nil
}

// applySettlement 根据 event kind 更新我方状态·返回 outcome 和详情。
func (s *Server) applySettlement(ctx context.Context, ev paymentgw.SettlementEvent, kind string) (outcome, detail string) {
	// 反查我方 order · gateway_payment_id 优先·失败 fallback client_order_id
	// P0-A 修：CreateOrder → gateway.CreatePayment → AttachGateway 之间有 tiny 窗口·
	// gateway 极快时 webhook 先到·gateway_payment_id 还没回填。fallback 用 client_order_id
	// （= 我方 order.ID · 从签名保护的 body 里读）保证不 unmatched。
	// 匹配成功后**顺手回填 gateway_payment_id**·后续 refund/reversed 就能走主路径。
	order, err := s.topups.FindByGatewayPaymentID(ctx, ev.GatewayPaymentID)
	if err != nil && ev.ClientOrderID != "" {
		if o2, err2 := s.topups.FindByClientOrderID(ctx, ev.ClientOrderID); err2 == nil {
			order = o2
			err = nil
			// 回填 gateway_payment_id + checkout（占位）· webhook 顺手治好 AttachGateway 之前的空窗
			if order.GatewayPaymentID == "" && ev.GatewayPaymentID != "" {
				if aerr := s.topups.AttachGateway(ctx, order.ID, ev.GatewayPaymentID, order.CheckoutURL, order.QRContent); aerr != nil {
					slog.Warn("settlement fallback 回填 gateway_payment_id 失败", "order_id", order.ID, "err", aerr)
				}
			}
		}
	}
	if err != nil {
		return "unmatched", err.Error()
	}

	switch kind {
	case "settled":
		// **P0-2 修**：以前用 AdvanceByOrderID(gateway_ordered→gateway_paid)·
		// early settlement 时 pending 还在 initial（AttachGateway 没跑到）· 静默 rows=0 ·
		// 订单 credited 但 pending 卡 initial · janitor 后续误标 expired。
		// 现在用 EnsureAtLeast · 允许跨态跃迁（initial→gateway_paid 也接受）。
		if s.pendingTopups != nil {
			if err := s.pendingTopups.EnsureAtLeast(ctx, order.ID, topup.PendingGatewayPaid); err != nil {
				// 支线态（refunded/expired/…）会返错 · log 但不阻塞 · MarkPaid 幂等会挡
				slog.Warn("pending_topup 推 gateway_paid 失败·MarkPaid 幂等兜底",
					"order_id", order.ID, "err", err)
			}
		}
		if _, err := s.topups.MarkPaid(ctx, order.ID); err != nil {
			// ErrOrderNotPending 说明这单已 paid（另一次回调抢先了）· 也算 duplicate
			slog.Warn("MarkPaid 失败", "order_id", order.ID, "err", err)
			return "duplicate", "already paid"
		}
		// pending_topup 一路推到 completed（EnsureAtLeast 幂等·允许重放）
		if s.pendingTopups != nil {
			if err := s.pendingTopups.EnsureAtLeast(ctx, order.ID, topup.PendingCompleted); err != nil {
				slog.Warn("pending_topup 推 completed 失败·janitor 兜底",
					"order_id", order.ID, "err", err)
			}
		}
		return "accepted", ""
	case "refunded", "reversed":
		// 反向 recharge + 反向 channel_fee 事务合并·refund 允许 wallet 走到负（wallet.ForceApplyTx）
		// P0-B 修：以前 "已花光余额不够" 吞成 duplicate 让 gateway 停重试·订单永卡 paid
		if _, err := s.topups.MarkRefunded(ctx, order.ID, kind); err != nil {
			if errors.Is(err, topup.ErrOrderNotPending) ||
				strings.Contains(err.Error(), "不能 refund") ||
				strings.Contains(err.Error(), "已结算或过期") {
				// 状态机拒绝（已 paid → 已 refunded 走到 duplicate 分支之外·或不合法 kind）
				slog.Warn("MarkRefunded 状态不合法", "order_id", order.ID, "err", err)
				return "ignored", err.Error()
			}
			// 其他错误（DB / wallet 出错）**不吞成 duplicate**·往上抛让 handler 返 5xx
			// 其他错误（DB / wallet 出错）**不吞成 duplicate**·往上抛让 handler 返 5xx
			slog.Error("MarkRefunded 内部错误·让 gateway 重试", "order_id", order.ID, "err", err)
			return "error:retry", err.Error()
		}
		// pending_topup 打成 refunded 终态（任何前置态都能走到 · 第一个成功推的即返回）
		if s.pendingTopups != nil {
			for _, from := range []topup.PendingStatus{
				topup.PendingCompleted, topup.PendingCredited, topup.PendingGatewayPaid,
				topup.PendingGatewayOrdered, topup.PendingInitial,
			} {
				if ok, err := s.pendingTopups.AdvanceByOrderID(ctx, order.ID, from, topup.PendingRefunded); err == nil && ok {
					break
				}
			}
		}
		return "accepted", "wallet reversed"
	}
	return "ignored", "未知 kind: " + kind
}

func (s *Server) handleGetTopupOrder(w http.ResponseWriter, r *http.Request) error {
	if s.topups == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "充值服务暂未装配")
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	orderID := r.PathValue("order_id")
	if orderID == "" {
		return ErrBadRequest("缺少 order_id")
	}
	order, err := s.topups.Get(r.Context(), p.ID, orderID)
	switch {
	case errors.Is(err, topup.ErrNotFound), errors.Is(err, topup.ErrForbidden):
		// 属主错也当 not_found —— 别暴露"这个 id 存在但不是你的"
		return ErrNotFound("找不到这个充值单")
	case err != nil:
		return err
	}
	writeJSON(w, http.StatusOK, topupOrderResponseOf(order))
	return nil
}

func (s *Server) handleListTopupOrders(w http.ResponseWriter, r *http.Request) error {
	if s.topups == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "充值服务暂未装配")
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	pageSize := atoiDefault(r.URL.Query().Get("page_size"), 50)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 50
	}

	// ?status= 是**对外**枚举 · 收敛见 §12.5
	// pending / paid / failed —— cancelled / expired 合并到 failed
	var internalStatus topup.Status
	switch strings.ToLower(r.URL.Query().Get("status")) {
	case "pending":
		internalStatus = topup.StatusPending
	case "paid":
		internalStatus = topup.StatusPaid
	case "failed":
		// 对外 failed 涵盖 expired + cancelled —— 二选一先取 expired（1a 主要走这条）
		internalStatus = topup.StatusExpired
	}

	orders, total, err := s.topups.List(r.Context(), p.ID, topup.ListOptions{
		Status: internalStatus,
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	})
	if err != nil {
		return err
	}
	items := make([]topupOrderResponse, 0, len(orders))
	for _, o := range orders {
		items = append(items, topupOrderResponseOf(o))
	}
	pages := (total + pageSize - 1) / pageSize
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
	})
	return nil
}

// handleDevMarkTopupPaid 是**开发用**端点·mock 支付网关 webhook。
//
// 阶段 1a 未接真网关：前端点了「我付好了」后调这个端点触发到账·
// 对齐前端 mock 的即时到账体验。接了真网关就撤掉这个端点·改成签名验证的 webhook。
//
// **安全**：只允许当前登录乘客给自己的订单标 paid（不能替别人标）。
func (s *Server) handleDevMarkTopupPaid(w http.ResponseWriter, r *http.Request) error {
	if s.topups == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "充值服务暂未装配")
	}
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	orderID := r.PathValue("order_id")
	if orderID == "" {
		return ErrBadRequest("缺少 order_id")
	}

	// 先校验属主，防串号
	if _, err := s.topups.Get(r.Context(), p.ID, orderID); err != nil {
		switch {
		case errors.Is(err, topup.ErrNotFound), errors.Is(err, topup.ErrForbidden):
			return ErrNotFound("找不到这个充值单")
		default:
			return err
		}
	}

	order, err := s.topups.MarkPaid(r.Context(), orderID)
	if err != nil {
		return translateTopupErr(err)
	}
	// dev mock 也推 pending_topup 走完完整闭环（走真链路时是 webhook 推·mock 是这里）
	// 用 EnsureAtLeast 一步推到 completed · 跨态跃迁允许
	if s.pendingTopups != nil {
		if err := s.pendingTopups.EnsureAtLeast(r.Context(), orderID, topup.PendingCompleted); err != nil {
			slog.Warn("dev-mark-paid 推 pending completed 失败·janitor 兜底",
				"order_id", orderID, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, topupOrderResponseOf(order))
	return nil
}

func topupOrderResponseOf(o topup.Order) topupOrderResponse {
	// checkout_url 优先·空 fallback 到 pay_url（老 mock 单没走 AttachGateway）
	url := o.CheckoutURL
	if url == "" {
		url = o.PayURL
	}
	resp := topupOrderResponse{
		OrderID:     o.ID,
		CheckoutURL: url,
		QRContent:   o.QRContent,
		Paid:        o.Paid,
		Credits:     o.Credits,
		ExpiresAt:   o.ExpiresAt.Format(time.RFC3339),
		Status:      publicTopupStatus(o.Status),
		CreatedAt:   o.CreatedAt.Format(time.RFC3339),
		FeeWaived:   o.FeeWaiverApplied,
	}
	if !o.PaidAt.IsZero() {
		resp.PaidAt = o.PaidAt.Format(time.RFC3339)
	}
	return resp
}

// publicTopupStatus 内部 4 态 → 对外 3 态（§12.5 收敛）。
// pending / paid / failed —— cancelled 和 expired 都合并到 failed（用户视角一致）。
func publicTopupStatus(s topup.Status) string {
	switch s {
	case topup.StatusPending:
		return "pending"
	case topup.StatusPaid:
		return "paid"
	case topup.StatusExpired, topup.StatusCancelled:
		return "failed"
	}
	return "pending"
}

func translateTopupErr(err error) error {
	switch {
	case errors.Is(err, topup.ErrInvalidAmount):
		return ErrBadRequest("充值积分必须为正")
	case errors.Is(err, topup.ErrUnsupportedChannel):
		return ErrBadRequest("暂不支持这个支付通道")
	case errors.Is(err, topup.ErrOrderNotPending):
		return ErrConflict(CodeConflict, "该充值单已结算或过期")
	case errors.Is(err, topup.ErrExpired):
		return ErrConflict(CodeConflict, "该充值单已过期")
	case errors.Is(err, topup.ErrNotFound):
		return ErrNotFound("找不到这个充值单")
	}
	return err
}
