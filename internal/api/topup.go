package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/topup"
)

// topupRequest / topupOrderResponse 对外形状。
//
// 请求字段 `credits`：乘客目标积分（要净到账的数字）。通道费 5% 加在本金上（CLAUDE.md §1.4）。
// 响应字段对齐 web/src/types/index.ts 的 TopupOrder：
//
//	order_id / qr_payload / paid / credits / expires_at + status（收敛后的对外三态）。
type topupRequest struct {
	Credits int64  `json:"credits"`
	Channel string `json:"channel"`
}

// topupOrderResponse 单张充值单的对外形状。
//
// 对外只暴露决策数据：ID / 支付跳转 / 显示所需的两个金额 / 过期时间 / 状态。
// **不出**：wallet_ledger_id、pending 状态机的中间态。
type topupOrderResponse struct {
	OrderID   string `json:"order_id"`
	QRPayload string `json:"qr_payload"` // waffo 收款链接 · 前端渲染成 QR
	Paid      int64  `json:"paid"`       // 乘客支付总积分 = credits + channel_fee
	Credits   int64  `json:"credits"`    // 净到账
	ExpiresAt string `json:"expires_at"`
	Status    string `json:"status"` // pending | paid | failed
	PaidAt    string `json:"paid_at,omitempty"`
	CreatedAt string `json:"created_at"`
}

// TopupOrderTTL 起单后未支付的过期时长。定 const 而不是配置：15 分钟是 waffo
// 那边收款链接的常规 TTL，跟通道商合同一致，改的话得同步改 waffo 侧。
const TopupOrderTTL = 15 * time.Minute

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

	// mock 阶段的 pay_url —— 生产接 waffo SDK 时改
	payURL := "https://waffo.example/order/" // 占位，加 order id 后拼完
	order, err := s.topups.CreateOrder(r.Context(), p.ID, req.Channel, req.Credits, payURL, TopupOrderTTL)
	if err != nil {
		return translateTopupErr(err)
	}

	// order.ID 生成后补进 pay_url —— 让 URL 唯一，方便对账
	// 用一次 UPDATE 而不是重构 CreateOrder，保持 store 单一入口
	fullURL := payURL + order.ID
	if _, err := s.db.ExecContext(r.Context(),
		`UPDATE topup_order SET pay_url = ? WHERE id = ?`, fullURL, order.ID); err != nil {
		return err
	}
	order.PayURL = fullURL

	resp := topupOrderResponseOf(order)
	respBody, _ := json.Marshal(resp)
	_ = saveIdempotentResponse(r.Context(), s.db, hit.recordID, http.StatusCreated, respBody)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(respBody)
	return nil
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
	resp := topupOrderResponse{
		OrderID:   o.ID,
		QRPayload: o.PayURL,
		Paid:      o.Paid,
		Credits:   o.Credits,
		ExpiresAt: o.ExpiresAt.Format(time.RFC3339),
		Status:    publicTopupStatus(o.Status),
		CreatedAt: o.CreatedAt.Format(time.RFC3339),
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
