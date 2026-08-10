package kiro91

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
		return nil, fmt.Errorf("kiro91: %w", err)
	}
	return &Adapter{cfg: cfg, client: hc}, nil
}

func (a *Adapter) ID() providers.VendorID           { return providers.Vendor91Kiro }
func (a *Adapter) ProviderID() providers.ProviderID { return providers.ProviderKiro }
func (a *Adapter) DisplayName() string              { return "Kiro Market" }

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
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/stock", nil)
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
		return nil, fmt.Errorf("kiro91: 解析 stock: %w", err)
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
	httpReq, err := a.newReq(ctx, http.MethodPost, "/api/my/purchase", payload)
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
		return nil, fmt.Errorf("kiro91: 解析 purchase: %w", err)
	}

	// Replayed 恒为 false：本 vendor 对重放返回**字节完全一致**的响应（档案 §7），
	// 也就是说响应里没有任何字段能区分首次成交与重放 —— 回显的 client_order_id
	// 首次也一样。这里**不能**拿 "id 对得上" 当重放判据，那会永远为 true，
	// 上层若据此跳过扣费 / 台账就全错了。真要判重放，只能靠我方自己的
	// pull_round 状态机（09-transactions §2）。
	return toPurchaseResult(&pr, req.Count, false, resp.Body), nil
}

func (a *Adapter) OrderKeys(ctx context.Context, orderID string) (*providers.PurchaseResult, error) {
	path := "/api/my/orders/" + orderID + "/keys"
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
		return nil, fmt.Errorf("kiro91: 解析 order keys: %w", err)
	}

	// Replayed=true：补拉本身就是"取回当时那批 key"，不产生新扣费（档案 §3「补拉」）。
	// requested 传 0 —— 补拉时"本次申请数"没有意义，看 Purchased。
	return toPurchaseResult(&pr, 0, true, resp.Body), nil
}

func (a *Adapter) Balance(ctx context.Context) (*providers.Balance, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/profile", nil)
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
		return nil, fmt.Errorf("kiro91: 解析 profile: %w", err)
	}
	return &providers.Balance{
		VendorID: providers.Vendor91Kiro,
		Balance:  credits(pr.Profile.Balance),
		Spent:    credits(pr.Profile.Spent),
		Earned:   credits(pr.Profile.Earned),
		Raw:      resp.Body,
	}, nil
}

func (a *Adapter) KeyHealth(_ context.Context, _ string) (*providers.KeyHealth, error) {
	return nil, &providers.APIError{
		VendorID: providers.Vendor91Kiro,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 没有单 key 存活探测端点",
	}
}

func (a *Adapter) KeyStats(_ context.Context, _ providers.KeyStatsOptions) (*providers.KeyStatsBatch, error) {
	return nil, &providers.APIError{
		VendorID: providers.Vendor91Kiro,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 没有 key stats 端点",
	}
}

func (a *Adapter) Redeem(ctx context.Context, code string) (*providers.RedeemResult, error) {
	payload, _ := json.Marshal(redeemReq{Code: code})
	req, err := a.newReq(ctx, http.MethodPost, "/api/my/redeem", payload)
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
		return nil, fmt.Errorf("kiro91: 解析 redeem: %w", err)
	}
	return &providers.RedeemResult{
		Quota:   credits(rr.Quota),
		Balance: credits(rr.Balance),
		Raw:     resp.Body,
	}, nil
}

func (a *Adapter) Usage(_ context.Context, _ []string) (*providers.UsageBatch, error) {
	return nil, &providers.APIError{
		VendorID: providers.Vendor91Kiro,
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
			return nil, fmt.Errorf("kiro91: %w", err)
		}
		b := body
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return nil, fmt.Errorf("kiro91: %w", err)
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
		VendorID:    providers.Vendor91Kiro,
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
//
// **先判 code 再判 status**（档案 §12 原文：「优先判 code（稳定标识），不要判文案」）。
// 反过来写会把同状态不同语义的错误压平 —— 三个 409（no_stock / purchase_cap_reached /
// retry_same_order）上层处理方式完全不同：换 vendor / 别重试 / 复用同 id 重试。
func sentinelFor(code string, status int) error {
	switch code {
	case "unauthenticated", "invalid_api_key":
		return providers.ErrAuth
	case "disabled":
		return providers.ErrDisabled
	case "csrf_failed", "session_required":
		// 这两条是"换个鉴权方式才行"，重试无用（档案 §12：用令牌重试无用）
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

func (a *Adapter) VerifySignature(secret string, headers http.Header, rawBody []byte) error {
	if secret == "" {
		return providers.ErrNoSignature
	}
	sig := headers.Get("X-KM-Signature")
	if sig == "" {
		return &providers.APIError{VendorID: providers.Vendor91Kiro, Sentinel: providers.ErrBadSignature, Message: "缺 X-KM-Signature"}
	}
	ts := headers.Get("X-KM-Timestamp")
	if ts == "" {
		return &providers.APIError{VendorID: providers.Vendor91Kiro, Sentinel: providers.ErrBadSignature, Message: "缺 X-KM-Timestamp"}
	}

	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return &providers.APIError{VendorID: providers.Vendor91Kiro, Sentinel: providers.ErrBadSignature, Message: "timestamp 非法"}
	}
	if drift := time.Now().Unix() - tsInt; drift > 300 || drift < -300 {
		return &providers.APIError{VendorID: providers.Vendor91Kiro, Sentinel: providers.ErrBadSignature, Message: "timestamp 偏离超 5 分钟"}
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(rawBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return &providers.APIError{VendorID: providers.Vendor91Kiro, Sentinel: providers.ErrBadSignature, Message: "签名不匹配"}
	}
	return nil
}

func (a *Adapter) Parse(rawBody []byte, _ http.Header) (*providers.WebhookEvent, error) {
	var wp webhookPayload
	if err := json.Unmarshal(rawBody, &wp); err != nil {
		return nil, fmt.Errorf("kiro91: 解析 webhook: %w", err)
	}

	evt := &providers.WebhookEvent{
		VendorID:        providers.Vendor91Kiro,
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
