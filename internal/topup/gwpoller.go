package topup

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/paymentgw"
)

// GatewayPollerAdapter 把 paymentgw.Client 适配成 GatewayPoller · 装配层用。
//
// 让 topup janitor 能主动 poll gateway 状态 · 覆盖 webhook 丢失。
// state 用归一化字符串：settled / pending / expired / cancelled / failed。
type GatewayPollerAdapter struct {
	Client *paymentgw.Client

	// LoadRequestSnapshot · janitor 用 client_order_id 反查时·从 topup_order 读
	// 起单时冷冻的 CreatePaymentRequest JSON。
	//
	// **为什么必须冷冻**（P0 修 · codex 三轮）：
	// 起单跟 janitor 反查之间·汇率 / channel config / payer_email 都可能变过。
	// 从当前 config 重建 request → gateway 幂等指纹跟起单时不同 → gateway 会
	// 当"新单"建·而不是 replay 原单。语义错。
	//
	// 装配层实现：从 orders.getBy(clientOrderID) 拿 order.GatewayRequestSnapshot
	// 反序列化返回。snapshot 空（起单没走到 SaveGatewayRequestSnapshot·极少见）
	// 时·返 (nil, ErrGatewayFindUnavailable) · janitor 走 pending_manual 兜底。
	LoadRequestSnapshot func(ctx context.Context, clientOrderID string) (*paymentgw.CreatePaymentRequest, error)
}

// PollByGatewayPaymentID · 按 gateway_payment_id GET 查现状。
//
// 语义：这是**读**接口（GET /payments/{id}）· 4xx 就是明确响应：
//   - 404 → 该 payment_id 在 gateway 侧不存在 → 归为 ErrGatewayNotFound（允许双表 expire）
//   - 其他 → 透出（janitor 累计 poll_fail_count）
func (a *GatewayPollerAdapter) PollByGatewayPaymentID(ctx context.Context, gatewayPaymentID string) (string, error) {
	p, err := a.Client.GetPayment(ctx, gatewayPaymentID)
	if err != nil {
		return "", translateGetErrToNotFound(err)
	}
	return normalizeState(p.State), nil
}

// FindByClientOrderID · 用 client_order_id 反查 gateway 侧是否已建。
//
// **策略**：走 gateway 的 CreatePayment 幂等 —— 同 client_order_id 再 POST：
//   - gateway 侧幂等表命中 → 返 200 + 原 Payment（我方"确认已建"）
//   - 无记录 → 201 新建（我方也算"确认已建"·继续走 gateway_ordered）
//
// **四类返回**：
//   - (*GatewayPayment, nil)             · 确认已建（含刚新建）
//   - (nil, ErrGatewayFindUnavailable)   · snapshot 缺失 · 反查能力不完整
//   - (nil, 其他 error)                  · 网络错 / 5xx / **POST 404** · janitor 累计 · **绝不 expire**
//
// **注意**：POST 404 语义 ≠ "payment 不存在"。
// CreatePayment 是**写**接口 · 语义只有 201 新建 / 200 replay / 4xx 拒绝：
//   - 404 = 端点缺失（gateway 部署错 / 路径错） · 不代表 "payment_id 不存在"
//   - 402/409/422 = 拒绝（余额 / 参数 / 幂等冲突）
//
// 一律走"网络错"分支 · janitor 累计到上限转 pending_manual · **绝不 expire**。
//
// **注意幂等 CreatePayment 的语义**：即使 gateway 侧真无记录·POST 会**新建**（201）·
// 相当于把"gateway 侧无单"变成"gateway 侧有单" —— 这**不是丢单**·反而修复了
// "本地已扣但 gateway 无单"的窗口。janitor 拿到结果后回填 gateway_payment_id 即可。
func (a *GatewayPollerAdapter) FindByClientOrderID(ctx context.Context, clientOrderID string) (*GatewayPayment, error) {
	if a.LoadRequestSnapshot == nil {
		return nil, ErrGatewayFindUnavailable
	}
	req, err := a.LoadRequestSnapshot(ctx, clientOrderID)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, ErrGatewayFindUnavailable
	}
	p, err := a.Client.CreatePayment(ctx, *req)
	if err != nil {
		// **绝不映射成 ErrGatewayNotFound** —— POST 语义里 404 是端点错·
		// 不是"payment 不存在"。透出让 janitor 走 poll_fail_count 累计。
		return nil, err
	}
	return &GatewayPayment{
		ID:          p.ID,
		State:       normalizeState(p.State),
		CheckoutURL: checkoutOf(p),
		QRContent:   qrOf(p),
	}, nil
}

// UnmarshalRequestSnapshot 装配层辅助 · 从 topup_order.gateway_request_snapshot BLOB
// 反序列化。空 slice → (nil, ErrGatewayFindUnavailable)。
func UnmarshalRequestSnapshot(snapshot []byte) (*paymentgw.CreatePaymentRequest, error) {
	if len(snapshot) == 0 {
		return nil, ErrGatewayFindUnavailable
	}
	var req paymentgw.CreatePaymentRequest
	if err := json.Unmarshal(snapshot, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// ErrGatewayFindUnavailable · snapshot 未存 or 反查回调未装配·反查能力缺失。
// 语义跟 ErrGatewayNotFound 不同 —— "查不了不知道" vs "确认没有"。
// janitor 见到这个 · 走 pending_manual 兜底 · **绝不 expire**。
var ErrGatewayFindUnavailable = errors.New("topup: gateway 反查能力缺失（snapshot 未存 / 回调未装配）")

// translateGetErrToNotFound · **仅** GET /payments/{id} 用 · 404 → NotFound sentinel。
//
// POST replay 走 CreatePayment · 语义不同（见 FindByClientOrderID 注释）·
// **不能**共用这个翻译。
func translateGetErrToNotFound(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *paymentgw.APIError
	if errors.As(err, &apiErr) && apiErr.Status == 404 {
		return ErrGatewayNotFound
	}
	return err
}

func normalizeState(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "settled":
		return "settled"
	case "expired":
		return "expired"
	case "cancelled", "canceled":
		return "cancelled"
	case "failed":
		return "failed"
	default:
		return "pending"
	}
}

func checkoutOf(p *paymentgw.Payment) string {
	if p.Instructions != nil {
		return p.Instructions.CheckoutURL
	}
	return ""
}

func qrOf(p *paymentgw.Payment) string {
	if p.Instructions != nil {
		return p.Instructions.QRContent
	}
	return ""
}
