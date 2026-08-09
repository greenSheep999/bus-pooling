package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Sentinel 错误 —— 上层用 errors.Is 分派。
//
// decider 的 fallback 逻辑直接依赖这几个（契约 §6）：
//   - ErrNoStock → 走次选 vendor
//   - ErrRateLimited → 退避
//   - ErrRetrySameOrder → **复用同一个 client_order_id 重试**（不是换新的）
//   - ErrPurchaseCapReached → 重试无用，别浪费
var (
	ErrAuth              = errors.New("provider: 鉴权失败")
	ErrRateLimited       = errors.New("provider: 被限流")
	ErrInsufficientFunds = errors.New("provider: vendor 侧余额不足")
	ErrNoStock           = errors.New("provider: 缺货")
	// ErrPurchaseCapReached 达持有上限 —— **重试无用**，要么调高上限要么等手里的号失效
	ErrPurchaseCapReached = errors.New("provider: 达持有上限")
	// ErrRetrySameOrder 库存被并发领走 —— **用同一个 client_order_id 重试**。
	// 换新 id 会变成两笔独立订单，可能双扣。
	ErrRetrySameOrder      = errors.New("provider: 应用同一 client_order_id 重试")
	ErrIdempotencyConflict = errors.New("provider: 幂等键冲突")
	ErrBadZone             = errors.New("provider: 区域参数非法")
	ErrBadCount            = errors.New("provider: 数量超范围")
	// ErrBadRequest 请求本身不合法（bad_json / bad_order_id / body_too_large / 未识别的 4xx）。
	// **故意不在 Retryable() 里** —— 重试同一个坏请求只是白烧配额。
	ErrBadRequest   = errors.New("provider: 请求非法")
	ErrNotSupported = errors.New("provider: 该能力不支持")
	ErrNotFound     = errors.New("provider: 资源不存在")
	ErrDisabled     = errors.New("provider: 账号被停用")
	ErrUpstream     = errors.New("provider: 上游错误")
	ErrTimeout      = errors.New("provider: 超时")
	ErrNoSignature  = errors.New("provider: 未配置签名密钥")
	ErrBadSignature = errors.New("provider: 签名不匹配")
)

// APIError 带上 vendor 的原始信息，包裹一个 sentinel。
//
// **不要把 Message 直接透给用户** —— 那是 vendor 的文案，可能含内部术语
// （CLAUDE.md §12.6）。api 层要映射成我方的人话。
type APIError struct {
	VendorID   VendorID
	StatusCode int
	// VendorCode vendor 的稳定错误标识 · 判这个不判文案
	VendorCode  string
	Message     string
	RetryAfter  *time.Duration
	Sentinel    error
	RawResponse json.RawMessage
}

func (e *APIError) Error() string {
	base := fmt.Sprintf("provider %s: HTTP %d", e.VendorID, e.StatusCode)
	if e.VendorCode != "" {
		base += " code=" + e.VendorCode
	}
	if e.Message != "" {
		base += ": " + e.Message
	}
	return base
}

func (e *APIError) Unwrap() error { return e.Sentinel }

// Retryable 说明这个错误重试有没有意义。
//
// 单独给一个方法是因为「能重试」和「用同一个 order id 重试」是两件事，
// 上层容易混（前者可以换 id，后者必须复用）。
func (e *APIError) Retryable() bool {
	switch {
	case errors.Is(e.Sentinel, ErrRateLimited),
		errors.Is(e.Sentinel, ErrRetrySameOrder),
		errors.Is(e.Sentinel, ErrUpstream),
		errors.Is(e.Sentinel, ErrTimeout):
		return true
	}
	return false
}

// MustReuseOrderID 为 true 时**必须**用同一个 client_order_id 重试。
func (e *APIError) MustReuseOrderID() bool {
	return errors.Is(e.Sentinel, ErrRetrySameOrder)
}
