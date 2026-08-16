// Package paymentgw 是 404bus-payment-gateway 的对外客户端 + 结算回调工具。
//
// 我方（bus-pooling）作为 "calling product"·跟 gateway 之间只有两个接触面：
//   - CreatePayment · POST /v1/payments · Bearer client token
//   - VerifySettlement · Gateway 主动 POST 到我方 settlement_url · HMAC-SHA256 签名
//
// 契约来自 docs/integration/02-api-reference.md + 03-settlement.md。
// 金额是**字符串小数**（不是微单位），asset 是 uppercase 字符串（USD / CNY / USDT）。
package paymentgw

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Config 是客户端配置，由 cmd 层从环境变量拼出来。
type Config struct {
	// BaseURL 例：http://127.0.0.1:18099 或 https://gw.404bus.internal
	BaseURL string
	// BearerToken client 的 Bearer（对应 gateway CLI -add-client 时的 bearer_token）
	BearerToken string
	// SettlementSecret client 的 HMAC key（对应 gateway CLI -add-client 时的 settlement_secret）
	SettlementSecret string
	// HTTPTimeout 单次请求超时·默认 15s（gateway create 一般 <5s，但要留 gateway 侧握手时间）
	HTTPTimeout time.Duration
}

// Client 承载对 gateway 的出向调用。零值不可用·必须 New(cfg)。
type Client struct {
	baseURL     string
	bearer      string
	settlementK []byte
	http        *http.Client
}

func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" || cfg.BearerToken == "" || cfg.SettlementSecret == "" {
		return nil, errors.New("paymentgw: BaseURL / BearerToken / SettlementSecret 都要有")
	}
	to := cfg.HTTPTimeout
	if to == 0 {
		to = 15 * time.Second
	}
	return &Client{
		baseURL:     cfg.BaseURL,
		bearer:      cfg.BearerToken,
		settlementK: []byte(cfg.SettlementSecret),
		http:        &http.Client{Timeout: to},
	}, nil
}

// ── /v1/payments ───────────────────────────────────────

// CreatePaymentRequest 对齐 openapi.yaml CreatePaymentRequest。
type CreatePaymentRequest struct {
	ClientOrderID    string `json:"client_order_id"`
	ProviderKind     string `json:"provider_kind"`
	ExpectedAmount   string `json:"expected_amount"` // 十进制字符串·不是 float
	ExpectedAsset    string `json:"expected_asset"`  // uppercase (USD / CNY / USDT)
	PayerEmail       string `json:"payer_email,omitempty"`
	PayerReference   string `json:"payer_reference,omitempty"`
	SuccessURL       string `json:"success_url,omitempty"`
	ExpiresInSeconds int    `json:"expires_in_seconds,omitempty"`
	Metadata         string `json:"metadata,omitempty"` // 序列化的 JSON 字符串
}

// PayerInstructions 对齐 openapi.yaml PayerInstructions·扁平的 string map。
// 用具名字段而不是 map[string]string——常用字段类型受控·未知字段进 Extra。
type PayerInstructions struct {
	CheckoutURL     string            `json:"checkout_url,omitempty"`
	QRContent       string            `json:"qr_content,omitempty"`
	DepositAddress  string            `json:"deposit_address,omitempty"`
	Network         string            `json:"network,omitempty"`
	ReceiverAccount string            `json:"receiver_account,omitempty"`
	ExactAmount     string            `json:"exact_amount,omitempty"`
	ExactAsset      string            `json:"exact_asset,omitempty"`
	ExpiresAt       string            `json:"expires_at,omitempty"`
	Notes           string            `json:"notes,omitempty"`
	Extra           map[string]string `json:"-"`
}

// Payment 对齐 openapi.yaml Payment（挑我方要用的字段·其他忽略）。
type Payment struct {
	ID                    string             `json:"id"`
	ClientOrderID         string             `json:"client_order_id"`
	ProviderKind          string             `json:"provider_kind"`
	ExpectedAmount        string             `json:"expected_amount"`
	ExpectedAsset         string             `json:"expected_asset"`
	State                 string             `json:"state"`
	ReconciliationState   string             `json:"reconciliation_state"`
	ExpiresAt             int64              `json:"expires_at,omitempty"`
	ObservedAt            int64              `json:"observed_at,omitempty"`
	SettledAt             int64              `json:"settled_at,omitempty"`
	CreatedAt             int64              `json:"created_at"`
	ExternalTransactionID string             `json:"external_transaction_id,omitempty"`
	Instructions          *PayerInstructions `json:"instructions,omitempty"`
}

// APIError 是 gateway 的 4xx/5xx 错误·包含 error code + detail。
type APIError struct {
	Status int
	Code   string `json:"error"`
	Detail string `json:"detail"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("paymentgw: %d %s: %s", e.Status, e.Code, e.Detail)
}

// CreatePayment 建单·同 client_order_id 幂等·200 = replay·201 = 新建。
func (c *Client) CreatePayment(ctx context.Context, req CreatePaymentRequest) (*Payment, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("paymentgw: 编码请求: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/payments", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.bearer)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("paymentgw: 建单请求: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var ae APIError
		_ = json.Unmarshal(respBody, &ae)
		ae.Status = resp.StatusCode
		return nil, &ae
	}
	var p Payment
	if err := json.Unmarshal(respBody, &p); err != nil {
		return nil, fmt.Errorf("paymentgw: 解码响应: %w", err)
	}
	return &p, nil
}

// GetPayment 拉当前状态·主要给运营 / 兜底 poll 用（回调走 VerifySettlement）。
func (c *Client) GetPayment(ctx context.Context, id string) (*Payment, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/v1/payments/"+id, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.bearer)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("paymentgw: 查单请求: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var ae APIError
		_ = json.Unmarshal(respBody, &ae)
		ae.Status = resp.StatusCode
		return nil, &ae
	}
	var p Payment
	if err := json.Unmarshal(respBody, &p); err != nil {
		return nil, fmt.Errorf("paymentgw: 解码响应: %w", err)
	}
	return &p, nil
}

// ── settlement 回调 ────────────────────────────────────

// SettlementEvent 对齐 03-settlement.md 的 body。
type SettlementEvent struct {
	EventID               string `json:"event_id"`
	GatewayPaymentID      string `json:"gateway_payment_id"`
	ClientOrderID         string `json:"client_order_id"`
	ProviderKind          string `json:"provider_kind"`
	ExternalTransactionID string `json:"external_transaction_id"`
	ExpectedAmount        string `json:"expected_amount"`
	ExpectedAsset         string `json:"expected_asset"`
	ReceivedAmount        string `json:"received_amount"`
	ReceivedAsset         string `json:"received_asset"`
	State                 string `json:"state"` // settled | refunded | reversed
	Kind                  string `json:"kind"`  // 同 state·新版用这个
	SettledAt             int64  `json:"settled_at"`
	Metadata              string `json:"metadata"`
	SignatureVersion      string `json:"signature_version"`
}

// SkewSeconds 允许的时钟偏差·超过就拒。契约建议 5 分钟。
const SkewSeconds = 5 * 60

// ErrBadSignature 签名不通过·或时间戳窗口外。返 401 让 gateway 停止重发无意义的。
// （契约建议签名错也允许 200 dedup·但我方选严格·防中间人）
var (
	ErrBadSignature = errors.New("paymentgw: settlement 签名验证失败")
	ErrStale        = errors.New("paymentgw: settlement 时间戳超窗")
	ErrBadTimestamp = errors.New("paymentgw: settlement 时间戳非法")
	ErrBadHeader    = errors.New("paymentgw: settlement header 格式错")
)

// VerifySettlement 按 03-settlement.md 验签。
//
// 契约：
//
//	signedString = "v1:" + X-404bus-Timestamp + ":" + rawBody
//	X-404bus-Signature = "v1=" + hex(HMAC-SHA256(settlement_secret, signedString))
//
// **必须**用**原始字节** rawBody·别 decode 再 re-encode（契约明确警告）。
func (c *Client) VerifySettlement(sigHeader, tsHeader string, rawBody []byte, now time.Time) error {
	// 1. header 形态
	if len(sigHeader) < 4 || sigHeader[:3] != "v1=" {
		return ErrBadHeader
	}
	gotHex := sigHeader[3:]
	if len(gotHex) != 64 {
		return ErrBadHeader
	}
	got, err := hex.DecodeString(gotHex)
	if err != nil {
		return ErrBadHeader
	}
	// 2. 时间戳窗口
	tsSec, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return ErrBadTimestamp
	}
	drift := now.Unix() - tsSec
	if drift < -SkewSeconds || drift > SkewSeconds {
		return ErrStale
	}
	// 3. HMAC
	mac := hmac.New(sha256.New, c.settlementK)
	mac.Write([]byte("v1:"))
	mac.Write([]byte(tsHeader))
	mac.Write([]byte(":"))
	mac.Write(rawBody)
	want := mac.Sum(nil)
	if !hmac.Equal(got, want) {
		return ErrBadSignature
	}
	return nil
}
