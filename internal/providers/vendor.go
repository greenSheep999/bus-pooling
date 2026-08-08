// Package providers 是上游 provider / vendor 的抽象层。
//
// 分层（docs/01-architecture.md Layer 2）：
//
//	Provider（协议族 · 当前只有 kiro）
//	  └── Vendor（具体供应源 · 6 家）
//
// **上层只 import 这个包**，不 import 具体 vendor 实现。
//
// 归一化的边界在这里：各家的字段名、价格口径、KeyPayload 形态都不一样
// （契约 §4 / §7），adapter 负责翻译成这里的类型。
package providers

import (
	"context"
	"encoding/json"
	"time"
)

type ProviderID string

const ProviderKiro ProviderID = "kiro"

// VendorID 内部标识。**不要直接展示给用户** —— 展示名走 DisplayName()，
// 而且无邀请码用户看到的是匿名编号（decisions §8.20）。
type VendorID string

const (
	Vendor91Kiro    VendorID = "kiro91"
	VendorKiroCEO   VendorID = "kiroceo"
	VendorKiroOOO   VendorID = "kirooo"
	VendorKiroAppIO VendorID = "kiroappio"
	VendorKiroAppCC VendorID = "kiroappcc"
	VendorKiroDrop  VendorID = "kirodrop"
)

type Zone string

const (
	ZoneUS Zone = "us"
	ZoneEU Zone = "eu"
	// ZoneGeneral 用于不分区的 vendor（kiroappcc）
	ZoneGeneral Zone = "general"
)

// Money 金额。**整数 microunit**，不用浮点（钱不能有舍入漂移 · CLAUDE.md §7.2）。
type Money struct {
	Amount int64
	// Currency "credit"（vendor 内部积分）| "CNY" | "USD"
	// 混币的换算在 decider 里做（kirodrop 报 USD · decisions §8.30）
	Currency string
}

const (
	CurrencyCredit = "credit"
	CurrencyCNY    = "CNY"
	CurrencyUSD    = "USD"
)

// KeyPayloadShape 各家给的 key 形态不同，上层按这个分派（契约 §3）。
type KeyPayloadShape string

const (
	// KeyPayloadFourTuple {key, account, password, issuer_url} —— 91kiro / kiroceo / kirooo / kiroappio
	KeyPayloadFourTuple KeyPayloadShape = "four_tuple"
	// KeyPayloadJustKey 只有 key —— kiroappcc
	KeyPayloadJustKey KeyPayloadShape = "just_key"
	// KeyPayloadKeyRegion {key, region} —— kirodrop
	KeyPayloadKeyRegion KeyPayloadShape = "key_region"
)

// Capability 声明每家的能力差异。
//
// **为什么不做"最大公约数"接口**（契约 §3）：每家能力不一样（91kiro 有幂等键、
// kiroappcc 没有）。若统一到最差水平，有幂等键的家也用不上，那就把「网络超时可能
// 双扣」这个风险扩散到所有家了。上层要用「有幂等键才这么做」就查这里。
type Capability struct {
	SupportsIdempotency   bool
	SupportsZones         bool
	SupportsWebhook       bool
	WebhookHasSignature   bool
	SupportsBatchPurchase bool
	HasWarranty           bool
	WarrantyMinutes       int
	KeyPayloadShape       KeyPayloadShape
	MinPerOrder           int
	MaxPerOrder           int
}

// Vendor 是 6 家都实现的最小接口。
type Vendor interface {
	ID() VendorID
	ProviderID() ProviderID
	// DisplayName 面向用户的名字（"Kiro Market"）· 匿名编号由上层按 invited 决定
	DisplayName() string
	Capability() Capability

	Stock(ctx context.Context, opts StockOptions) (*StockSnapshot, error)
	Purchase(ctx context.Context, req PurchaseRequest) (*PurchaseResult, error)
	// OrderKeys 按订单号补拉。webhook 通知里不放密钥，拿 order_id 来换。
	OrderKeys(ctx context.Context, orderID string) (*PurchaseResult, error)
	// Balance 我方在这家 vendor 侧的余额
	Balance(ctx context.Context) (*Balance, error)

	// KeyHealth / KeyStats 供 deathwatch 和决策模型用（1d 才实现）
	KeyHealth(ctx context.Context, key string) (*KeyHealth, error)
	KeyStats(ctx context.Context, opts KeyStatsOptions) (*KeyStatsBatch, error)

	// 可选能力 · 不支持时返回 ErrNotSupported
	Redeem(ctx context.Context, code string) (*RedeemResult, error)
	Usage(ctx context.Context, keys []string) (*UsageBatch, error)
}

// ── Stock ────────────────────────────────────────────

type StockOptions struct {
	// Zone nil = 用 vendor 默认。**注意 91kiro 的默认是"只取美国区"**，
	// 想要欧区必须显式传（契约 §4.1 / vendor 档案 §7）
	Zone *Zone
}

type StockSnapshot struct {
	VendorID   VendorID
	ObservedAt time.Time
	// Available 总可购数
	Available   int
	MinPerOrder int
	MaxPerOrder int
	Zones       []ZoneStock
	// Balance 我方在此 vendor 的余额
	Balance         Money
	WarrantyMinutes int
	Raw             json.RawMessage
}

type ZoneStock struct {
	Zone      Zone
	Region    string
	Available int
	// UnitPrice **仅供估价**。实扣以 Purchase 返回的 TotalCost 为准 ——
	// 同区可能有多辆车混价（vendor 档案 §7 明确警告过）
	UnitPrice Money
}

// ── Purchase ─────────────────────────────────────────

type PurchaseRequest struct {
	Count int
	// ClientOrderID 32 hex 幂等键。vendor 不支持幂等时忽略。
	// **重试时必须传同一个值**，否则会变成两笔独立订单。
	ClientOrderID string
	Zone          *Zone
	// OrderID 部分 vendor 支持指定批次（kiroappio / kirodrop）
	OrderID *string
	// MaxTotal 价格保护（kirodrop 支持）
	MaxTotal *Money
}

type PurchaseResult struct {
	ClientOrderID string
	VendorOrderID string
	Zone          Zone
	Requested     int
	// Purchased **实际成交数**。库存是并发争抢的，申请 5 拿到 3 是正常结果，
	// 上层必须按这个数处理而不是 Requested（vendor 档案 §7 明确警告）
	Purchased int
	Keys      []KeyPayload
	// UnitPrice 只是其中一把的价 —— 混价单里乘数量会跟实扣不一致
	UnitPrice Money
	// TotalCost **权威值** · 恒等于 Σ Keys[].Paid
	TotalCost Money
	// Remaining 扣后 vendor 侧余额
	Remaining     Money
	WarrantyUntil *time.Time
	// Replayed 是否幂等重放（同 client_order_id 重复调用）
	Replayed bool
	Raw      json.RawMessage
}

type KeyPayload struct {
	// VendorKeyID vendor 侧的 key id（补拉 / 对账用）
	VendorKeyID string
	Key         string
	// 以下几个按 Capability.KeyPayloadShape 决定有没有值
	Account   string
	Password  string
	IssuerURL string
	Region    string
	// Paid 这一把实际扣的（质保能退的就是这个数）
	Paid Money
	// WarrantyUntil 空 = 这把没质保（免费交付即如此）
	WarrantyUntil *time.Time
	Free          bool
}

type Balance struct {
	VendorID VendorID
	Balance  Money
	Spent    Money
	Earned   Money
	Raw      json.RawMessage
}

// ── KeyHealth / KeyStats（1d） ───────────────────────

type KeyHealth struct {
	Key       string
	Alive     bool
	LastCheck time.Time
	Raw       json.RawMessage
}

type Window string

const (
	Window24h Window = "24h"
	Window7d  Window = "7d"
	Window30d Window = "30d"
)

type KeyStatsOptions struct {
	Window Window
	Keys   []string
}

type KeyStatsBatch struct {
	VendorID   VendorID
	Window     Window
	ObservedAt time.Time
	Items      []KeyStatsItem
}

type KeyStatsItem struct {
	Key          string
	Calls        int64
	InputTokens  int64
	OutputTokens int64
	Errors       int64
	CreditsUsed  Money
	// Concurrency nil = vendor 没给
	Concurrency *int
}

// ── 可选能力 ─────────────────────────────────────────

type RedeemResult struct {
	Quota   Money
	Balance Money
	Raw     json.RawMessage
}

type UsageBatch struct {
	VendorID   VendorID
	ObservedAt time.Time
	Items      []UsageItem
}

type UsageItem struct {
	Key         string
	CreditsUsed Money
	Remaining   Money
	Raw         json.RawMessage
}
