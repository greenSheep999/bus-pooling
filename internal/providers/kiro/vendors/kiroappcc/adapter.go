package kiroappcc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/httpx"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

type Config struct {
	BaseURL       string
	APIKey        string
	WebhookSecret string
	Timeout       time.Duration
	MaxRetries    int
	ProxyURL      string
	NoProxy       string
}

type Adapter struct {
	cfg    Config
	client *httpx.Client
}

func New(cfg Config) (*Adapter, error) {
	hc, err := httpx.New(httpx.Config{
		Timeout:       cfg.Timeout,
		MaxRetries:    cfg.MaxRetries,
		RetryBaseWait: 500 * time.Millisecond,
		Proxy:         cfg.ProxyURL,
		NoProxy:       cfg.NoProxy,
	})
	if err != nil {
		return nil, fmt.Errorf("kiroappcc: %w", err)
	}
	return &Adapter{cfg: cfg, client: hc}, nil
}

func (a *Adapter) ID() providers.VendorID           { return providers.VendorKiroAppCC }
func (a *Adapter) ProviderID() providers.ProviderID { return providers.ProviderKiro }
func (a *Adapter) DisplayName() string              { return "Kiro App CC" }

// Capability 反映 本 vendor 独家差异（vendor 档案 §14）：
//   - **不支持幂等键**（本 vendor 最大接入风险 —— 网络超时重试会双扣）
//   - **不支持区域**（单价一档到底）
//   - **有 webhook 但无签名**（VerifySignature 恒返 ErrNoSignature）
//   - **key payload 只有 key**（无 account/password/issuer_url/region）
func (a *Adapter) Capability() providers.Capability {
	return providers.Capability{
		SupportsIdempotency:   false,
		SupportsZones:         false,
		SupportsWebhook:       true,
		WebhookHasSignature:   false,
		SupportsBatchPurchase: true,
		HasWarranty:           true,
		// WarrantyMinutes: vendor 档案 §13 —— 页面暗示存在但没写具体时长，留 0
		WarrantyMinutes: 0,
		KeyPayloadShape: providers.KeyPayloadJustKey,
		MinPerOrder:     1,
		MaxPerOrder:     200,
	}
}

func (a *Adapter) Stock(ctx context.Context, _ providers.StockOptions) (*providers.StockSnapshot, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/openapi/stock", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError(resp)
	}

	var sr stockResp
	if err := json.Unmarshal(resp.Body, &sr); err != nil {
		return nil, fmt.Errorf("kiroappcc: 解析 stock: %w", err)
	}
	return toStockSnapshot(&sr, resp.Body), nil
}

// Purchase 拉号 —— **/openapi/claim**（不是 /purchase）。
//
// **无 client_order_id / 无 zone / 无 order_id 回显**（vendor 档案 §7）。
// 请求体只有 count；响应形态因 count 而异（单个 `{key}` vs 批量 `{keys:[...]}`），
// mapper 里做归一化。
func (a *Adapter) Purchase(ctx context.Context, req providers.PurchaseRequest) (*providers.PurchaseResult, error) {
	body := claimReq{Count: req.Count}
	payload, _ := json.Marshal(body)
	httpReq, err := a.newReq(ctx, http.MethodPost, "/openapi/claim", payload)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError(resp)
	}

	var cr claimResp
	if err := json.Unmarshal(resp.Body, &cr); err != nil {
		return nil, fmt.Errorf("kiroappcc: 解析 claim: %w", err)
	}
	return toPurchaseResult(&cr, req.Count, resp.Body), nil
}

// OrderKeys 补拉 —— 走 `/openapi/orders` 全量拉一遍 · 匹配 orderNo。
//
// 老档案说本 vendor 无 orders 端点 · 实测存在（endpoints-audit 2026-08-12）·
// 每条订单有 orderNo/kiroApiKey/probeState/warrantyStatus · vendor 一单一 key。
// 匹配到就复用 claimResp 结构返 · Replayed=true 补拉不重复扣费。
func (a *Adapter) OrderKeys(ctx context.Context, orderID string) (*providers.PurchaseResult, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/openapi/orders", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiroappcc: OrderKeys orders http %d", resp.StatusCode)
	}
	var rows []ccOrder
	if err := json.Unmarshal(resp.Body, &rows); err != nil {
		return nil, fmt.Errorf("kiroappcc: OrderKeys 解析: %w", err)
	}
	for _, r := range rows {
		if r.OrderNo != orderID {
			continue
		}
		raw, _ := json.Marshal(r)
		// 从 ccOrder 拼 claimResp（本 vendor 一单一 key · Key 单值）
		cr := claimResp{
			Key:        r.KiroApiKey,
			PointsCost: r.PointsCost,
		}
		out := toPurchaseResult(&cr, 1, raw)
		out.Replayed = true
		return out, nil
	}
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroAppCC,
		Sentinel: providers.ErrNotFound,
		Message:  fmt.Sprintf("orderNo %q 不在 /openapi/orders 里", orderID),
	}
}

// Balance 走**独立** `/openapi/balance`（vendor 档案 §5 —— 6 家里唯一没有 /profile 的）。
// 响应只有一个 balance 字段，spent / earned 拿不到。
func (a *Adapter) Balance(ctx context.Context) (*providers.Balance, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/openapi/balance", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError(resp)
	}

	var br balanceResp
	if err := json.Unmarshal(resp.Body, &br); err != nil {
		return nil, fmt.Errorf("kiroappcc: 解析 balance: %w", err)
	}
	return &providers.Balance{
		VendorID: providers.VendorKiroAppCC,
		Balance:  credits(br.Balance),
		Raw:      resp.Body,
	}, nil
}

func (a *Adapter) KeyHealth(_ context.Context, _ string) (*providers.KeyHealth, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroAppCC,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 没有单 key 存活探测端点",
	}
}

func (a *Adapter) KeyStats(_ context.Context, _ providers.KeyStatsOptions) (*providers.KeyStatsBatch, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroAppCC,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 没有 key stats 端点",
	}
}

// Redeem 不支持 —— vendor 档案 §5：兑换只能走网页 UI，`/openapi/*` 无兑换端点。
func (a *Adapter) Redeem(_ context.Context, _ string) (*providers.RedeemResult, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroAppCC,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 无 /openapi 兑换端点，只能走网页 UI",
	}
}

func (a *Adapter) Usage(_ context.Context, _ []string) (*providers.UsageBatch, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroAppCC,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 无 key usage 端点",
	}
}

// newReq —— **Authorization: Bearer <api key>**（vendor 档案 §2）。
// 注意 本 vendor 的 sk- 前缀跟 OpenAI 撞名，别在客户端搞混。
func (a *Adapter) newReq(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	u := strings.TrimRight(a.cfg.BaseURL, "/") + path
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("kiroappcc: %w", err)
		}
		b := body
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return nil, fmt.Errorf("kiroappcc: %w", err)
		}
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// parseError 本 vendor 错误 envelope 独家嵌套（vendor 档案 §12）：
//
//	{"error": {"type": "<code>", "message": "<msg>"}, "retryAfter": 180}
//
// 限流时 retryAfter 出现在响应体（顶层），Retry-After header 也会带；两处都吃。
func (a *Adapter) parseError(resp *httpx.Response) error {
	ae := &providers.APIError{
		VendorID:    providers.VendorKiroAppCC,
		StatusCode:  resp.StatusCode,
		RawResponse: resp.Body,
	}
	var er errorResp
	if json.Unmarshal(resp.Body, &er) == nil {
		ae.VendorCode = er.code()
		ae.Message = er.msg()
		if er.RetryAfter > 0 {
			d := time.Duration(er.RetryAfter) * time.Second
			ae.RetryAfter = &d
		}
	}

	if ae.RetryAfter == nil {
		if h := resp.Header.Get("Retry-After"); h != "" {
			if secs, err := strconv.Atoi(h); err == nil {
				d := time.Duration(secs) * time.Second
				ae.RetryAfter = &d
			}
		}
	}

	ae.Sentinel = sentinelFor(ae.VendorCode, resp.StatusCode)
	return ae
}

// sentinelFor 本 vendor 没有全表 code 枚举（vendor 档案 §12）。
// 已知 code 只有 `rate_limit_exceeded`，其余靠 status 判。
//
// **先判 code 再判 status**（跟 其他 vendor / 其他 vendor 同规矩，防同状态不同语义压平）。
func sentinelFor(code string, status int) error {
	switch code {
	case "rate_limit_exceeded":
		return providers.ErrRateLimited
	case "unauthenticated", "invalid_api_key":
		return providers.ErrAuth
	case "insufficient_balance":
		return providers.ErrInsufficientFunds
	case "no_stock":
		return providers.ErrNoStock
	}

	// code 缺失 —— 退回按 status 判
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return providers.ErrAuth
	case status == http.StatusPaymentRequired:
		return providers.ErrInsufficientFunds
	case status == http.StatusNotFound:
		return providers.ErrNotFound
	case status == http.StatusTooManyRequests:
		return providers.ErrRateLimited
	case status >= 500:
		return providers.ErrUpstream
	case status >= 400:
		// 未识别的 4xx 一律当"请求有问题"（同 其他 vendor 注释：给 ErrUpstream 会被
		// Retryable 判可重试白烧配额）
		return providers.ErrBadRequest
	}
	return providers.ErrUpstream
}

// ── WebhookParser interface ─────────────────────────

// VerifySignature · 本 vendor webhook **无签名**（vendor 档案 §10 明示：
// 没有 header 或算法说明，是 6 家里 webhook 文档最简的）。
//
// 我方策略：**总返 ErrNoSignature** · handler 决定是否接受无签名家。
func (a *Adapter) VerifySignature(_ string, _ http.Header, _ []byte) error {
	return providers.ErrNoSignature
}

// Parse · vendor 档案 §10：payload schema 未公开，只有一句"有新库存时推一条 JSON"。
// 骨架先按 6 家共性字段解析；实际字段等对接联调时再修。
func (a *Adapter) Parse(rawBody []byte, _ http.Header) (*providers.WebhookEvent, error) {
	var wp webhookPayload
	if err := json.Unmarshal(rawBody, &wp); err != nil {
		return nil, fmt.Errorf("kiroappcc: 解析 webhook: %w", err)
	}

	evt := &providers.WebhookEvent{
		VendorID:        providers.VendorKiroAppCC,
		EventID:         wp.EventID,
		OrderID:         wp.OrderID,
		PurchaseOrderID: wp.PurchaseOrderID,
		NewKeys:         wp.NewKeys,
		Zone:            providers.ZoneGeneral,
		ReceivedAt:      time.Now().UTC(),
		RawPayload:      rawBody,
	}

	// vendor 档案 §10：只有一种事件"新库存"，没有 all_keys_dead / warranty_refund 事件。
	// 事件字符串留裸值时用作 EventType，未来对齐再收敛。
	switch wp.Event {
	case "new_keys_available", "":
		evt.EventType = providers.EventNewKeysAvailable
	case "webhook_test":
		evt.EventType = providers.EventTest
	default:
		evt.EventType = providers.EventType(wp.Event)
	}
	return evt, nil
}

var (
	_ providers.Vendor        = (*Adapter)(nil)
	_ providers.WebhookParser = (*Adapter)(nil)
)
