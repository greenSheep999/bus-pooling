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

	"github.com/bus-pooling/bus-pooling/internal/paymentgw"
	"github.com/bus-pooling/bus-pooling/internal/topup"
)

// topupRequest / topupOrderResponse 对外形状。
//
// 请求字段 `credits`：乘客目标积分（要净到账的数字）。通道费 5% 加在本金上（CLAUDE.md §1.4）。
// 响应字段对齐 web/src/types/index.ts 的 TopupOrder：
//
//	order_id / checkout_url / paid / credits / expires_at + status（收敛后的对外三态）。
type topupRequest struct {
	Credits int64  `json:"credits"`
	Channel string `json:"channel"`
}

// topupOrderResponse 单张充值单的对外形状。
//
// 对外只暴露决策数据：ID / 支付跳转 / 显示所需的两个金额 / 过期时间 / 状态。
// **不出**：wallet_ledger_id、pending 状态机的中间态、gateway payment id（内部关联）。
type topupOrderResponse struct {
	OrderID     string `json:"order_id"`
	CheckoutURL string `json:"checkout_url"`         // gateway.instructions.checkout_url · 前端跳转
	QRContent   string `json:"qr_content,omitempty"` // 有 QR 的 rail 会给·waffo 一般没
	Paid        int64  `json:"paid"`                 // 乘客支付总积分 = credits + channel_fee
	Credits     int64  `json:"credits"`              // 净到账
	ExpiresAt   string `json:"expires_at"`
	Status      string `json:"status"` // pending | paid | failed
	PaidAt      string `json:"paid_at,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// TopupOrderTTL 起单后未支付的过期时长。定 const 而不是配置：15 分钟是 waffo
// 那边收款链接的常规 TTL，跟通道商合同一致，改的话得同步改 waffo 侧。
const TopupOrderTTL = 15 * time.Minute

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
	if req.Channel != "waffo" {
		return ErrBadRequest("暂只支持 waffo 通道")
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

	// 步骤 1：先落一行 pending。用我方 order.ID 做 gateway 的 client_order_id
	//        （§8.1 契约·gateway 侧幂等键就是这个）
	//        起单时 checkout_url 先写占位·gateway 建成后 AttachGateway 回填
	placeholderURL := "pending://gateway"
	order, err := s.topups.CreateOrder(r.Context(), p.ID, req.Channel, req.Credits, placeholderURL, TopupOrderTTL)
	if err != nil {
		return translateTopupErr(err)
	}

	// 步骤 2：调 gateway 建单（可选·未装配时退回 mock 兼容）
	if s.paymentGW != nil {
		// gateway 侧的 asset = CNY（1 积分 ≡ 1 元 · 前端已知汇率展示层做换算）
		// gateway 的 waffo_checkout rail 支持 USD/CNY 等 fiat（openapi Asset pattern 是宽的）·
		// 由 gateway 侧配 waffo store 币种决定是否被 waffo 拒。
		gwReq := paymentgw.CreatePaymentRequest{
			ClientOrderID:    order.ID,
			ProviderKind:     "waffo_checkout",
			ExpectedAmount:   microToDecimalString(order.Paid),
			ExpectedAsset:    "CNY",
			PayerEmail:       p.Email,
			ExpiresInSeconds: int(TopupOrderTTL / time.Second),
		}
		if s.paymentGWSuccessURL != "" {
			gwReq.SuccessURL = s.paymentGWSuccessURL
		}
		payment, err := s.paymentGW.CreatePayment(r.Context(), gwReq)
		if err != nil {
			// gateway 报错 · order 保留 pending（janitor 会清·或乘客换 idempotency-key 重试）
			// 别把 gateway 内部错误码往前端透 · 统一 502
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
	} else {
		// 无 gateway · 走 dev mock 路径 · 前端展示这个假 URL 触发 BP_ENABLE_DEV_TOPUP 端点标 paid
		fullURL := "https://waffo.example/order/" + order.ID
		if err := s.topups.AttachGateway(r.Context(), order.ID, "", fullURL, ""); err != nil {
			return err
		}
		order.CheckoutURL = fullURL
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

	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO settlement_event (event_id, gateway_payment_id, kind, received_at, outcome, detail)
		VALUES (?, ?, ?, ?, ?, ?)`,
		ev.EventID, ev.GatewayPaymentID, kind, nowRFC3339(), outcome, detail); err != nil {
		// 落幂等表失败·仍然返 200（业务已经做了 MarkPaid 有幂等保护），
		// 但记 warn — 重复回调会走一次 MarkPaid 检查·代价可控
		slog.Warn("settlement_event 落库失败·业务已处理",
			"event_id", ev.EventID, "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"outcome": outcome})
	return nil
}

// applySettlement 根据 event kind 更新我方状态·返回 outcome 和详情。
func (s *Server) applySettlement(ctx context.Context, ev paymentgw.SettlementEvent, kind string) (outcome, detail string) {
	// 反查我方 order
	order, err := s.topups.FindByGatewayPaymentID(ctx, ev.GatewayPaymentID)
	if err != nil {
		return "unmatched", err.Error()
	}

	switch kind {
	case "settled":
		if _, err := s.topups.MarkPaid(ctx, order.ID); err != nil {
			// ErrOrderNotPending 说明这单已 paid（另一次回调抢先了）· 也算 duplicate
			slog.Warn("MarkPaid 失败", "order_id", order.ID, "err", err)
			return "duplicate", "already paid"
		}
		return "accepted", ""
	case "refunded", "reversed":
		// 阶段 1a 不接退款反向流水（Iss #13）·记录·不动 wallet·让人工看
		return "ignored", "退款/反向结算阶段 1a 未实现·请人工处理"
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

// handleDevMarkTopupPaid 是**开发用**端点，mock waffo webhook。
//
// 阶段 1a 没接真 waffo：前端点了「我付好了」后调这个端点触发到账，
// 对齐前端 mock 的即时到账体验。接了真 waffo 就撤掉这个端点，改成签名验证的 webhook。
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
