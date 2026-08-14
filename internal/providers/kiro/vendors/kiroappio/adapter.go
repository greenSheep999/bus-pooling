package kiroappio

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
		return nil, fmt.Errorf("kiroappio: %w", err)
	}
	return &Adapter{cfg: cfg, client: hc}, nil
}

func (a *Adapter) ID() providers.VendorID           { return providers.VendorKiroAppIO }
func (a *Adapter) ProviderID() providers.ProviderID { return providers.ProviderKiro }
func (a *Adapter) DisplayName() string              { return "Kiro App IO" }

func (a *Adapter) Capability() providers.Capability {
	return providers.Capability{
		SupportsIdempotency:   true,
		SupportsZones:         true,
		SupportsWebhook:       true,
		WebhookHasSignature:   true,
		SupportsBatchPurchase: true,
		HasWarranty:           true,
		WarrantyMinutes:       10,
		KeyPayloadShape:       providers.KeyPayloadFourTuple,
		MinPerOrder:           1,
		MaxPerOrder:           200,
	}
}

func (a *Adapter) Stock(ctx context.Context, opts providers.StockOptions) (*providers.StockSnapshot, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/me/stock", nil)
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
		return nil, fmt.Errorf("kiroappio: 解析 stock: %w", err)
	}
	return toStockSnapshot(&sr, resp.Body), nil
}

func (a *Adapter) Purchase(ctx context.Context, req providers.PurchaseRequest) (*providers.PurchaseResult, error) {
	body := purchaseReq{
		Count:         req.Count,
		ClientOrderID: req.ClientOrderID,
	}
	if req.Zone != nil {
		body.Zone = string(*req.Zone)
	}
	payload, _ := json.Marshal(body)
	httpReq, err := a.newReq(ctx, http.MethodPost, "/api/me/purchase", payload)
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

	var pr purchaseResp
	if err := json.Unmarshal(resp.Body, &pr); err != nil {
		return nil, fmt.Errorf("kiroappio: 解析 purchase: %w", err)
	}

	// Replayed 恒为 false：重放判定不能靠 vendor 响应字段（响应可能字节一致），
	// 只能靠我方 pull_round 状态机（09-transactions §2）。
	return toPurchaseResult(&pr, req.Count, false, resp.Body), nil
}

func (a *Adapter) OrderKeys(ctx context.Context, orderID string) (*providers.PurchaseResult, error) {
	path := "/api/me/orders/" + orderID + "/keys"
	req, err := a.newReq(ctx, http.MethodGet, path, nil)
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

	var pr purchaseResp
	if err := json.Unmarshal(resp.Body, &pr); err != nil {
		return nil, fmt.Errorf("kiroappio: 解析 order keys: %w", err)
	}

	// Replayed=true：补拉本身就是"取回当时那批 key"，不产生新扣费。
	// requested 传 0 —— 补拉时"本次申请数"没有意义，看 Purchased。
	return toPurchaseResult(&pr, 0, true, resp.Body), nil
}

func (a *Adapter) Balance(ctx context.Context) (*providers.Balance, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/me/profile", nil)
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

	var pr profileResp
	if err := json.Unmarshal(resp.Body, &pr); err != nil {
		return nil, fmt.Errorf("kiroappio: 解析 profile: %w", err)
	}
	// **P0 · 2026-08-15 修**：老 struct 期望 profile.balance 嵌套 · vendor 真实响应
	// 顶层平铺 · Balance 恒返 0 · 上游余额预检失效。本 vendor 只暴露 balance ·
	// 无 spent/earned（vendor 档案 §2.1 明说）· 那两个留 zero。
	return &providers.Balance{
		VendorID: providers.VendorKiroAppIO,
		Balance:  credits(pr.Balance),
		Spent:    providers.Money{},
		Earned:   providers.Money{},
		Raw:      resp.Body,
	}, nil
}

func (a *Adapter) KeyHealth(_ context.Context, _ string) (*providers.KeyHealth, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroAppIO,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 没有单 key 存活探测端点",
	}
}

func (a *Adapter) KeyStats(_ context.Context, _ providers.KeyStatsOptions) (*providers.KeyStatsBatch, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroAppIO,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 没有 key stats 端点",
	}
}

func (a *Adapter) Redeem(ctx context.Context, code string) (*providers.RedeemResult, error) {
	payload, _ := json.Marshal(redeemReq{Code: code})
	req, err := a.newReq(ctx, http.MethodPost, "/api/me/redeem", payload)
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

	var rr redeemResp
	if err := json.Unmarshal(resp.Body, &rr); err != nil {
		return nil, fmt.Errorf("kiroappio: 解析 redeem: %w", err)
	}
	return &providers.RedeemResult{
		Quota:   credits(rr.Quota),
		Balance: credits(rr.Balance),
		Raw:     resp.Body,
	}, nil
}

func (a *Adapter) Usage(_ context.Context, _ []string) (*providers.UsageBatch, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroAppIO,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor usage 需逐 key 调用，不走 batch 接口",
	}
}

func (a *Adapter) newReq(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	u := strings.TrimRight(a.cfg.BaseURL, "/") + path
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("kiroappio: %w", err)
		}
		b := body
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return nil, fmt.Errorf("kiroappio: %w", err)
		}
	}
	req.Header.Set("X-API-Key", a.cfg.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (a *Adapter) parseError(resp *httpx.Response) error {
	ae := &providers.APIError{
		VendorID:    providers.VendorKiroAppIO,
		StatusCode:  resp.StatusCode,
		RawResponse: resp.Body,
	}
	var er errorResp
	if json.Unmarshal(resp.Body, &er) == nil {
		ae.VendorCode = er.Code
		ae.Message = er.msg()
	}

	if h := resp.Header.Get("Retry-After"); h != "" {
		if secs, err := strconv.Atoi(h); err == nil {
			d := time.Duration(secs) * time.Second
			ae.RetryAfter = &d
		}
	}

	ae.Sentinel = sentinelFor(ae.VendorCode, resp.StatusCode)
	return ae
}

// sentinelFor 把 vendor 的 code 映射到统一 sentinel。
// 先判 code 再判 status —— 同状态不同语义的错误上层处理方式完全不同。
func sentinelFor(code string, status int) error {
	switch code {
	case "unauthenticated", "invalid_api_key":
		return providers.ErrAuth
	case "disabled":
		return providers.ErrDisabled
	case "csrf_failed", "session_required":
		return providers.ErrAuth
	case "rate_limited":
		return providers.ErrRateLimited
	case "insufficient_balance":
		return providers.ErrInsufficientFunds
	case "no_stock":
		return providers.ErrNoStock
	case "purchase_cap_reached":
		return providers.ErrPurchaseCapReached
	case "retry_same_order":
		return providers.ErrRetrySameOrder
	case "idempotency_conflict":
		return providers.ErrIdempotencyConflict
	case "bad_zone":
		return providers.ErrBadZone
	case "bad_count":
		return providers.ErrBadCount
	case "not_found", "redeem_invalid":
		return providers.ErrNotFound
	case "bad_json", "bad_order_id", "body_too_large":
		return providers.ErrBadRequest
	case "verify_failed", "quota_failed", "internal":
		return providers.ErrUpstream
	}

	// code 缺失或没见过 —— 退回按 status 判
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
		// 未识别的 4xx 一律当"请求有问题" —— 不能给 ErrUpstream，
		// 那会被 Retryable() 判成可重试，白烧 3 次配额
		return providers.ErrBadRequest
	}
	return providers.ErrUpstream
}

// ── WebhookParser interface ─────────────────────────

// VerifySignature · 本 vendor webhook 无 HMAC 签名（档案未定义 header 或算法）·
// 接收端只能靠 URL 秘密路径 / query token 自保护。
//
// 我方策略：总返 ErrNoSignature · 由 handler 决定要不要接。
func (a *Adapter) VerifySignature(_ string, _ http.Header, _ []byte) error {
	return providers.ErrNoSignature
}

func (a *Adapter) Parse(rawBody []byte, _ http.Header) (*providers.WebhookEvent, error) {
	var wp webhookPayload
	if err := json.Unmarshal(rawBody, &wp); err != nil {
		return nil, fmt.Errorf("kiroappio: 解析 webhook: %w", err)
	}

	evt := &providers.WebhookEvent{
		VendorID:        providers.VendorKiroAppIO,
		EventID:         wp.EventID,
		OrderID:         wp.OrderID,
		PurchaseOrderID: wp.PurchaseOrderID,
		NewKeys:         wp.NewKeys,
		DeadKeys:        wp.Dead,
		Zone:            providers.Zone(wp.Zone),
		ReceivedAt:      time.Now().UTC(),
		RawPayload:      rawBody,
	}

	switch wp.Event {
	case "new_keys_available":
		evt.EventType = providers.EventNewKeysAvailable
	case "all_keys_dead":
		evt.EventType = providers.EventAllKeysDead
	case "warranty_refund":
		evt.EventType = providers.EventWarrantyRefund
		m := credits(wp.RefundedQuota)
		evt.RefundAmount = &m
	case "key_revoked_abuse":
		// 本 vendor 独家事件 · 上游主动收回已售号（判定用量滥用）· 且不退积分。
		// 显式列出而不是靠 default 的字符串透传 —— 那样依赖"事件名恰好等于枚举值"
		// 这个巧合 · 上游改名就静默失效（漏处理 = 用户拿废号）。
		evt.EventType = providers.EventKeyRevokedAbuse
	case "test", "webhook_test":
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
