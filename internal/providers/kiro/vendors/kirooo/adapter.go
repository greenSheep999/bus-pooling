package kirooo

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

// adapter — 本 vendor 差异：
//   - Base URL 已含 /api，endpoint 相对路径**不再重复 /api 前缀**
//   - 拉号入口是 `/my/keys/claim`（不是 /my/purchase）
//   - Webhook 无 HMAC 签名（档案 §11）
//   - Profile 字段是 `credits`（不是 balance）

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
		return nil, fmt.Errorf("kirooo: %w", err)
	}
	return &Adapter{cfg: cfg, client: hc}, nil
}

func (a *Adapter) ID() providers.VendorID           { return providers.VendorKiroOOO }
func (a *Adapter) ProviderID() providers.ProviderID { return providers.ProviderKiro }
func (a *Adapter) DisplayName() string              { return "Kiro OOO" }

func (a *Adapter) Capability() providers.Capability {
	return providers.Capability{
		SupportsIdempotency:   true,
		SupportsZones:         true,
		SupportsWebhook:       true,
		WebhookHasSignature:   false, // 档案 §11：明确"不签名"
		SupportsBatchPurchase: true,
		HasWarranty:           true,
		WarrantyMinutes:       10,
		KeyPayloadShape:       providers.KeyPayloadFourTuple,
		MinPerOrder:           1,
		MaxPerOrder:           500, // 档案 §7：单次上限 500

		// 两种账号类型都供（实测 2026-08-16 · 档案 §2.3b）
		AccountKinds: []providers.AccountKind{
			providers.AccountEnterprise,
			providers.AccountPersonal,
		},
		// 两个池**买前都不能选订阅档**：
		//   企业池 · 端点无 plan 参数
		//   个人池 · 实测 ?plan=pro|pro_plus|pro_max 三值返回完全相同（参数被忽略）
		// 号是哪档只能导入后从 housepool 的 Subscription 观察。
		// 上游哪天开了按档下单 · 在这里补 map 即可（别在别处硬编码组合）。
		SelectablePlans: nil,
	}
}

func (a *Adapter) Stock(ctx context.Context, opts providers.StockOptions) (*providers.StockSnapshot, error) {
	// 个人号走另一套端点（personal.go · 档案 §2.3b）· 两个池库存/价格完全独立
	if opts.Kind.Normalize() == providers.AccountPersonal {
		return a.stockPersonal(ctx)
	}
	// 优先打 /api/my/stock/regions（fleet 视角 · 有 regions[].stock 分区 + dispatches）
	// 失败降级 /api/my/stock（我方账户视角 · 只单值 · 无 region 拆分）
	//
	// 为什么优先 fleet 端点（诊断报告 2026-08-11-kirooo-collection.md）：
	//   - 单值 stock 端点 stock-delta 推算路径下 region 信息全缺 · 落 vendor_dispatch.region=''
	//   - fleet 端点同一次请求既有 stock 又有 region 拆分 · 探针 60s 频次直接够画分区图
	//   - 响应体大不了多少 · 无额外成本
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/stock/regions", nil)
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

	// 先试 regions 形状
	var rr regionsResp
	if err := json.Unmarshal(resp.Body, &rr); err == nil && len(rr.Regions) > 0 {
		return toStockSnapshotFromRegions(&rr, resp.Body), nil
	}
	// 降级 · 老 stock 形状
	var sr stockResp
	if err := json.Unmarshal(resp.Body, &sr); err != nil {
		return nil, fmt.Errorf("kirooo: 解析 stock: %w", err)
	}
	return toStockSnapshot(&sr, resp.Body), nil
}

func (a *Adapter) Purchase(ctx context.Context, req providers.PurchaseRequest) (*providers.PurchaseResult, error) {
	// 个人号走 claim-personal（personal.go · 档案 §2.4b）
	// **必须跟估价时的 kind 一致** —— 两池价不同（50 vs 100）· 用错池会实扣与预估不符
	if req.Kind.Normalize() == providers.AccountPersonal {
		return a.purchasePersonal(ctx, req)
	}
	// 本 vendor 拉号入口：POST /my/keys/claim（档案 §7）
	// Body 只吃 {count, client_order_id}，不带 zone 字段（档案未列 zone 参数）
	body := purchaseReq{
		Count:         req.Count,
		ClientOrderID: req.ClientOrderID,
	}
	payload, _ := json.Marshal(body)
	httpReq, err := a.newReq(ctx, http.MethodPost, "/api/my/keys/claim", payload)
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
		return nil, fmt.Errorf("kirooo: 解析 claim: %w", err)
	}

	// Replayed 恒为 false：档案 §7 说"同一单号重复提交返回上次那批 Key"，
	// 但响应体没有字段能区分首次成交与重放。真要判重放，只能靠我方
	// pull_round 状态机（09-transactions §2）。
	return toPurchaseResult(&pr, req.Count, false, resp.Body), nil
}

func (a *Adapter) OrderKeys(ctx context.Context, orderID string) (*providers.PurchaseResult, error) {
	// 补拉端点：GET /my/purchase-orders/{order_id}/keys（任务卡指定路径）
	path := "/api/my/purchase-orders/" + orderID + "/keys"
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
		return nil, fmt.Errorf("kirooo: 解析 order keys: %w", err)
	}

	// Replayed=true：补拉本身就是"取回当时那批 key"，不产生新扣费。
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
		return nil, fmt.Errorf("kirooo: 解析 profile: %w", err)
	}
	// **P0 · 2026-08-15 修**：老 struct 期望 profile.credits 嵌套 · vendor 真实响应
	// 顶层平铺 · 余额是 `remaining` int（本 vendor 积分 = CNY 1:1）·
	// 老代码 Balance 恒返 0 · 上游余额预检失效。
	return &providers.Balance{
		VendorID: providers.VendorKiroOOO,
		Balance:  credits(pr.Remaining),
		Spent:    credits(pr.UsedQuota),
		Earned:   providers.Money{},
		Raw:      resp.Body,
	}, nil
}

func (a *Adapter) KeyHealth(_ context.Context, _ string) (*providers.KeyHealth, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroOOO,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 没有单 key 存活探测端点",
	}
}

func (a *Adapter) KeyStats(_ context.Context, _ providers.KeyStatsOptions) (*providers.KeyStatsBatch, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroOOO,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 没有 key stats 端点",
	}
}

func (a *Adapter) Redeem(_ context.Context, _ string) (*providers.RedeemResult, error) {
	// 档案 §8：本 vendor 走 USDT 上链充值，**没有兑换码 / 支付宝**（独家）
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroOOO,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor 不做兑换码，充值走 USDT 上链",
	}
}

func (a *Adapter) Usage(_ context.Context, _ []string) (*providers.UsageBatch, error) {
	return nil, &providers.APIError{
		VendorID: providers.VendorKiroOOO,
		Sentinel: providers.ErrNotSupported,
		Message:  "本 vendor usage 未见 batch 接口",
	}
}

func (a *Adapter) newReq(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	// Base URL 已含 /api（档案 §1），path 直接接在后面
	u := strings.TrimRight(a.cfg.BaseURL, "/") + path
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("kirooo: %w", err)
		}
		b := body
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		}
	} else {
		req, err = http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return nil, fmt.Errorf("kirooo: %w", err)
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
		VendorID:    providers.VendorKiroOOO,
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
// 档案 §13：没有全表 code 枚举·只明写"取不到货 4xx / 限流 429"。
// 保留同 provider 家族其他 vendor 的 code 判断作为**兼容尝试**（命名大概率相近）·
// 命中不了退回按 status 判。
func sentinelFor(code string, status int) error {
	switch code {
	case "unauthenticated", "invalid_api_key":
		return providers.ErrAuth
	case "disabled":
		return providers.ErrDisabled
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
	case "bad_count":
		return providers.ErrBadCount
	case "not_found":
		return providers.ErrNotFound
	case "bad_json", "bad_order_id", "body_too_large":
		return providers.ErrBadRequest
	case "verify_failed", "internal":
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
		// 档案 §13 明写"4xx 视为你明确拒绝，不再重试" —— 归 ErrBadRequest
		// 好让 Retryable() 判成不可重试，避免白烧配额
		return providers.ErrBadRequest
	}
	return providers.ErrUpstream
}

// ── WebhookParser interface ─────────────────────────

// VerifySignature · 本 vendor webhook **无 HMAC 签名**（档案 §11 原文：
// 「不带签名，请自己用不可猜的 URL 路径当口令」）。
//
// 策略：**总返 ErrNoSignature** · 上层收到这个错误意味着"这家不签"·
// 由 handler 决定要不要接（当前 handler 允许无签名家直接 200）。
func (a *Adapter) VerifySignature(_ string, _ http.Header, _ []byte) error {
	return providers.ErrNoSignature
}

func (a *Adapter) Parse(rawBody []byte, _ http.Header) (*providers.WebhookEvent, error) {
	var wp webhookPayload
	if err := json.Unmarshal(rawBody, &wp); err != nil {
		return nil, fmt.Errorf("kirooo: 解析 webhook: %w", err)
	}

	// 本 vendor 里 client_order_id 与 purchase_order_id 字面同值（档案 §11：
	// purchase_order_id 是 client_order_id 的老名字）· 优先取 purchase_order_id
	// 保持归一化字段跟同 provider 其他家 vendor 一致·若空再兜底 client_order_id。
	orderID := wp.PurchaseOrderID
	if orderID == "" {
		orderID = wp.ClientOrderID
	}

	evt := &providers.WebhookEvent{
		VendorID:        providers.VendorKiroOOO,
		EventID:         wp.EventID,
		OrderID:         wp.OrderID,
		PurchaseOrderID: orderID,
		NewKeys:         wp.NewKeys,
		DeadKeys:        wp.Dead,
		ReceivedAt:      time.Now().UTC(),
		RawPayload:      rawBody,
	}

	switch wp.Event {
	case "new_keys_available":
		evt.EventType = providers.EventNewKeysAvailable
	case "all_keys_dead":
		evt.EventType = providers.EventAllKeysDead
	case "test":
		// 本 vendor 测试事件叫 `test`（不是 webhook_test，档案 §11）
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
