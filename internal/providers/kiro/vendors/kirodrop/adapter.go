package kirodrop

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/httpx"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

type Config struct {
	BaseURL       string
	APIKey        string
	WebhookSecret string
	// SessionToken · 网页登录后的 kiro_session_token（Bearer）· /api/v1/* 独家端点用
	// （API key 打不了 · 登录带图形验证码不能自动重登 · 人工 seed · 会过期）。
	// 空 = 不拉 /api/v1/* 的降价 schedule（现价仍走 api_key 的 /api/me/stock 不受影响）。
	SessionToken string
	Timeout      time.Duration
	MaxRetries   int
	ProxyURL     string
	NoProxy      string
}

type Adapter struct {
	cfg    Config
	client *httpx.Client

	// dashboard 响应缓存 · 三个 Lister（orders/keys/ledger）共享同一次 HTTP 调用。
	// 避免 backfiller 每 5min 打 3 次同一端点（省流量 · 少一次触发 token 过期机会）。
	// TTL 见 dashboard.go 的 dashboardCacheTTL。
	dashMu       sync.Mutex
	dashCache    *dashboardResp
	dashCachedAt time.Time
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
		return nil, fmt.Errorf("kirodrop: %w", err)
	}
	return &Adapter{cfg: cfg, client: hc}, nil
}

func (a *Adapter) ID() providers.VendorID           { return providers.VendorKiroDrop }
func (a *Adapter) ProviderID() providers.ProviderID { return providers.ProviderKiro }
func (a *Adapter) DisplayName() string              { return "Kiro Drop" }

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
	// 本 vendor 特殊：stock 走 /api/me/stock（同其他 vendor 惯用 /api/my/* 不一致）
	// profile / purchase 仍然走 /api/my/* — 见 vendor 档案 §7
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
		return nil, fmt.Errorf("kirodrop: 解析 stock: %w", err)
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
	// **涨价保护** · req.MaxTotal 传下来的是 microunit · vendor 要 CNY 保留 6 位小数
	// 传 0 或 nil = 不设保护（保持老行为）· 只有非零才落 body
	if req.MaxTotal != nil && req.MaxTotal.Amount > 0 {
		// microunit -> CNY 字符串（vendor 档案 §4.1 例 "884.400000"）
		amt := req.MaxTotal.Amount
		body.MaxTotalCNY = fmt.Sprintf("%d.%06d", amt/1_000_000, amt%1_000_000)
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
		return nil, fmt.Errorf("kirodrop: 解析 purchase: %w", err)
	}
	// Replayed 恒 false —— 判重放靠 pull_round 状态机，不靠 vendor 响应回显。
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
		return nil, fmt.Errorf("kirodrop: 解析 order keys: %w", err)
	}
	// 补拉 = 取回当时那批 key，不产生新扣费。requested 传 0（申请数已无意义）。
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
		return nil, fmt.Errorf("kirodrop: 解析 profile: %w", err)
	}
	return &providers.Balance{
		VendorID: providers.VendorKiroDrop,
		Balance:  credits(pr.Profile.Balance),
		Spent:    credits(pr.Profile.Spent),
		Earned:   credits(pr.Profile.Earned),
		Raw:      resp.Body,
	}, nil
}

func (a *Adapter) KeyHealth(_ context.Context, _ string) (*providers.KeyHealth, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroDrop,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 没有单 key 存活探测端点",
	}
}

func (a *Adapter) KeyStats(_ context.Context, _ providers.KeyStatsOptions) (*providers.KeyStatsBatch, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroDrop,
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
		return nil, fmt.Errorf("kirodrop: 解析 redeem: %w", err)
	}
	return &providers.RedeemResult{
		Quota:   credits(rr.Quota),
		Balance: credits(rr.Balance),
		Raw:     resp.Body,
	}, nil
}

func (a *Adapter) Usage(_ context.Context, _ []string) (*providers.UsageBatch, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroDrop,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor usage 需逐 key 调用，不走 batch 接口",
	}
}

// Reservation 预约库存 —— 本 vendor 特有端点（GET /api/v1/reservation?quantity=N&region=X）。
// 阶段 1a 不实现，返 ErrNotSupported。留着提醒后续接入。
func (a *Adapter) Reservation(_ context.Context, _ int, _ string) error {
	return &providers.APIError{
		VendorID: providers.VendorKiroDrop,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor reservation 阶段 1a 未接入",
	}
}

func (a *Adapter) newReq(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	u := strings.TrimRight(a.cfg.BaseURL, "/") + path
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("kirodrop: %w", err)
		}
		b := body
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return nil, fmt.Errorf("kirodrop: %w", err)
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
		VendorID:    providers.VendorKiroDrop,
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

// sentinelFor 把 vendor 的 code 映射到统一 sentinel。**先判 code 再判 status**。
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
		return providers.ErrBadRequest
	}
	return providers.ErrUpstream
}

// ── WebhookParser interface ─────────────────────────
//
// 本 vendor 的签名跟 其他 vendor 同算法（HMAC-SHA256 over ts + "." + body、±5min 时窗），
// **只是 header 名和 prefix 不同**：
//   - Signature header : X-Kiro-Signature   （其他 vendor 是 X-KM-Signature）
//   - Timestamp header : X-Kiro-Timestamp   （其他 vendor 是 X-KM-Timestamp）
//   - Prefix           : v1=<hex>           （其他 vendor 是 sha256=<hex>）

func (a *Adapter) VerifySignature(secret string, headers http.Header, rawBody []byte) error {
	if secret == "" {
		return providers.ErrNoSignature
	}
	sig := headers.Get("X-Kiro-Signature")
	if sig == "" {
		return &providers.APIError{VendorID: providers.VendorKiroDrop, Sentinel: providers.ErrBadSignature, Message: "缺 X-Kiro-Signature"}
	}
	ts := headers.Get("X-Kiro-Timestamp")
	if ts == "" {
		return &providers.APIError{VendorID: providers.VendorKiroDrop, Sentinel: providers.ErrBadSignature, Message: "缺 X-Kiro-Timestamp"}
	}

	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return &providers.APIError{VendorID: providers.VendorKiroDrop, Sentinel: providers.ErrBadSignature, Message: "timestamp 非法"}
	}
	if drift := time.Now().Unix() - tsInt; drift > 300 || drift < -300 {
		return &providers.APIError{VendorID: providers.VendorKiroDrop, Sentinel: providers.ErrBadSignature, Message: "timestamp 偏离超 5 分钟"}
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(rawBody)
	expected := "v1=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return &providers.APIError{VendorID: providers.VendorKiroDrop, Sentinel: providers.ErrBadSignature, Message: "签名不匹配"}
	}
	return nil
}

func (a *Adapter) Parse(rawBody []byte, _ http.Header) (*providers.WebhookEvent, error) {
	var wp webhookPayload
	if err := json.Unmarshal(rawBody, &wp); err != nil {
		return nil, fmt.Errorf("kirodrop: 解析 webhook: %w", err)
	}

	evt := &providers.WebhookEvent{
		VendorID:        providers.VendorKiroDrop,
		EventID:         wp.EventID,
		OrderID:         wp.OrderID,
		PurchaseOrderID: wp.PurchaseOrderID,
		NewKeys:         wp.NewKeys,
		DeadKeys:        wp.Dead,
		// zone 归一 · 本 vendor 顶级可能给 zone 也可能只给 region
		Zone:       firstNonEmptyZone(wp.Zone, wp.Region),
		ReceivedAt: time.Now().UTC(),
		RawPayload: rawBody,
		// 双区合并通知 · 逐区拆开给上层（见 providers.WebhookEvent.PerZone 注释）
		PerZone: perZoneDeliveries(&wp),
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

// firstNonEmptyZone · 顶级 zone 字段可能空 · 回落 region 归一
func firstNonEmptyZone(zone, region string) providers.Zone {
	if z := providers.ZoneOf(zone); z != "" {
		return z
	}
	return providers.ZoneOf(region)
}

// perZoneDeliveries · 把双区合并通知拆成逐区。
//
// **只在真是双区通知时返非空** —— 判据是 `purchase_order_ids_by_region` 有内容
// （那才是按区的幂等键 · 没它就没法逐区 Purchase · 拆了也没用）。
// 单区通知返 nil · 上层走老的顶级字段路径。
//
// 数据源优先级：`regions[]` 列出的区为准 · 逐区取 new_keys / purchase_order_id / batch_ids。
// `regions[]` 缺失时退回用 `purchase_order_ids_by_region` 的 key 集合。
func perZoneDeliveries(wp *webhookPayload) []providers.ZoneDelivery {
	if len(wp.PurchaseOrderIDsByRegion) == 0 {
		return nil
	}
	regions := wp.Regions
	if len(regions) == 0 {
		regions = make([]string, 0, len(wp.PurchaseOrderIDsByRegion))
		for r := range wp.PurchaseOrderIDsByRegion {
			regions = append(regions, r)
		}
		sort.Strings(regions) // 稳定顺序 · 便于测试和日志比对
	}
	out := make([]providers.ZoneDelivery, 0, len(regions))
	for _, r := range regions {
		poid := wp.PurchaseOrderIDsByRegion[r]
		if poid == "" {
			// 没有该区的幂等键 · 拉不了 · 跳过（宁可少一区也别用错的键去 Purchase）
			continue
		}
		out = append(out, providers.ZoneDelivery{
			Zone:            providers.ZoneOf(r),
			Region:          r,
			NewKeys:         wp.NewKeysByRegion[r],
			PurchaseOrderID: poid,
			BatchIDs:        wp.BatchIDsByRegion[r],
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var (
	_ providers.Vendor        = (*Adapter)(nil)
	_ providers.WebhookParser = (*Adapter)(nil)
)
