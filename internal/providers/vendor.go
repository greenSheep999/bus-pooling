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
	// VendorKiroMarket · 我方第 7 家 · 手工上架货源
	// 号是运营买好后走 admin API 导入 housepool（跟前 6 家的 BatchImport 同一条链）·
	// 仅"库存不是 API 查来的 · 号不是 API 买来的"跟其他家不同 · 见 docs/24 §1 + migration 047
	VendorKiroMarket VendorID = "kiro_market"
)

type Zone string

const (
	ZoneUS Zone = "us"
	ZoneEU Zone = "eu"
	// ZoneGeneral 用于不分区的 vendor
	ZoneGeneral Zone = "general"
)

// AccountKind · 账号类型 · 上游是**两套独立货架**（不同端点 / 不同价 / 不同库存）。
//
// 跟 vendor / subscription / zone **平级**（`Offer = vendor × kind × subscription × zone`）·
// 不是 "vendor 属于某个 kind"：同一家可能两种都供、也可能只供一种（docs/24 §1）。
//
// 空值按 AccountEnterprise 处理 —— 老调用方（未传 Kind）保持原行为。
type AccountKind string

const (
	AccountEnterprise AccountKind = "enterprise"
	AccountPersonal   AccountKind = "personal"
)

// Normalize 空值归一到 enterprise（向后兼容 · 见 AccountKind 注释）
func (k AccountKind) Normalize() AccountKind {
	if k == "" {
		return AccountEnterprise
	}
	return k
}

// SubscriptionPlan · 订阅档位 · 决定该号的 quota 上限。
//
// **购买前可选**（部分 vendor personal 池按档定价）· 不是买完才观察到的属性。
//
// ⚠️ **plan 跟 AccountKind 正交** —— 企业号和个人号**都可能有全部档位**。
// 别写 `kind==enterprise ? power : [pro, pro_plus]` 这种映射（docs/24 §2 作废清单）。
// 「哪些 (kind, plan) 组合当前可买」是**运行时数据**：
//   - 真 vendor → Capability.SelectablePlans（adapter 按上游实况填）
//   - 我方手工上架 → market_inventory 表里有哪些行
//
// 当前实况只是"企业开了 power · 个人开了 pro/pro_plus"，不是模型约束。
//
// ⚠️ 跟 passenger.tier（retail/community/wholesale · 买号人的折扣档）是两回事。
//
// 档位集合**可能增长**（上游加档就多一个）· 加档时要同步 DB CHECK 约束。
type SubscriptionPlan string

const (
	PlanPower   SubscriptionPlan = "power"
	PlanPro     SubscriptionPlan = "pro"
	PlanProPlus SubscriptionPlan = "pro_plus"
	PlanProMax  SubscriptionPlan = "pro_max"
)

// AllSubscriptionPlans · 已知档位全集 · 用于校验入参 / 生成 DB CHECK。
// **不代表当前可买** —— 可买组合看 Capability.SelectablePlans / market_inventory。
var AllSubscriptionPlans = []SubscriptionPlan{PlanPower, PlanPro, PlanProPlus, PlanProMax}

// Valid 档位是否已知（拒未知值进库 · 防 housepool 返回的 raw 串直接落库）
func (p SubscriptionPlan) Valid() bool {
	for _, v := range AllSubscriptionPlans {
		if v == p {
			return true
		}
	}
	return false
}

// Money 金额。**整数 microunit**，不用浮点（钱不能有舍入漂移 · CLAUDE.md §7.2）。
type Money struct {
	Amount int64
	// Currency "credit"（vendor 内部积分）| "CNY" | "USD"
	// 混币的换算在 decider 里做（decisions §8.30）
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
	// KeyPayloadFourTuple {key, account, password, issuer_url}
	KeyPayloadFourTuple KeyPayloadShape = "four_tuple"
	// KeyPayloadJustKey 只有 key
	KeyPayloadJustKey KeyPayloadShape = "just_key"
	// KeyPayloadKeyRegion {key, region}
	KeyPayloadKeyRegion KeyPayloadShape = "key_region"
)

// Capability 声明每家的能力差异。
//
// **为什么不做"最大公约数"接口**（契约 §3）：各家能力不同（幂等键 / 分区 /
// webhook 签名等）。若统一到最差水平，有幂等键的家也用不上，那就把「网络超时可能
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

	// AccountKinds 这家支持哪些账号类型 · 空 = 只 enterprise（老 adapter 默认）。
	//
	// **"不支持"与"支持但缺货"必须分开** —— 前端两种展示不同（docs/24 §3）：
	//   不在这个列表   → "该 vendor 不提供"
	//   在列表但 stock=0 → "暂时缺货"
	AccountKinds []AccountKind
	// SelectablePlans 购买前可选的订阅档 · 按 kind 分组 · 空 = 该 kind 不分档。
	//
	// **由 adapter 按上游实况填** —— 任何 kind 都可能有任何档（plan 与 kind 正交）。
	// 当前实况：多数家企业池只开 power · 个别家个人池开 pro/pro_plus ·
	// 但这是 vendor 侧配置，不是代码假设。上游开新档 → adapter 这里多一项即可。
	SelectablePlans map[AccountKind][]SubscriptionPlan
}

// SupportsKind 该 vendor 是否供这种账号类型（空 AccountKinds 视为只供 enterprise）
func (c Capability) SupportsKind(k AccountKind) bool {
	k = k.Normalize()
	if len(c.AccountKinds) == 0 {
		return k == AccountEnterprise
	}
	for _, v := range c.AccountKinds {
		if v == k {
			return true
		}
	}
	return false
}

// Vendor 是各家 adapter 都要实现的最小接口。
type Vendor interface {
	ID() VendorID
	ProviderID() ProviderID
	// DisplayName 面向用户的名字 · 匿名编号由上层按 invited 决定
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
	// Zone nil = 用 vendor 默认。**某些 vendor 的默认可能是"只取一个区"**·
	// 需要跨区必须显式传（契约 §4.1 / vendor 档案 §7）
	Zone *Zone
	// Kind 要查哪种账号类型的货架 · 空 = AccountEnterprise（老行为 · 向后兼容）。
	//
	// 上游是**两套独立货架**（不同端点 / 不同价 / 不同库存）· 见 docs/24 §1。
	// vendor 不支持该 kind 时返 ErrNotSupported —— 上层据此判 supported=false。
	Kind AccountKind
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

	// TieredPricing · 阶梯降价规则 · 只部分 vendor 有 · 其他恒 nil。
	// 拿到就填 · Prober 落 vendor_price_tier 表 · docs/10-pricing §1.2 · §1.6 Q3。
	TieredPricing *TieredPricing
}

// TieredPricing · vendor 侧的阶梯降价 schedule（docs/10-pricing §1.2）
//
// 目前只部分 vendor 支持（返 timed_pricing 的端点通常需 cookie · 我方 API key
// 不能直接调）· 现阶段 TieredPricing 恒 nil。留字段供未来 vendor 开放时接入。
type TieredPricing struct {
	Region        string         // us / eu / "" 全区（reservation 逐区一份 schedule）
	Enabled       bool           // 是否启用分档降价
	Active        bool           // 当前是否在降价窗口
	IntervalMin   int            // 每档间隔（分钟）
	MaxReductions int            // 最多降几次
	Applied       int            // 已降几次
	StartAt       time.Time      // 阶梯启动时刻
	Schedule      []TierSchedule // 每档一条
}

// TierSchedule · 阶梯的每一档（docs/10-pricing §1.2 vendor_price_tier 表对齐）
type TierSchedule struct {
	Index            int       // 0 = base · 1 = 第一次降 · ...
	EffectiveAt      time.Time // 这档生效时刻
	UnitPriceCredits int64     // microunit · 这档的我方积分（Prober 落库时算好）
	UnitPriceUSDRaw  int64     // microunit · 这档 USD 原值（有则存 · 部分 vendor 独家）
}

// ── 数量分档（quantity band · docs/23-endpoints-todo · migration 035）─────────────
//
// 跟 TieredPricing（时间降价）是两种不同的"阶梯"模型：
//   - 时间降价：价随时间降（部分 vendor 的 timed pricing）
//   - 数量分档：价按产量/购买量档位（部分 vendor 的 key-price-tiers bands）
// 数量分档端点通常 API key 可直达（无需 session）· 能进 backfiller 稳定拉。

// QtyPriceBand · 一个数量档（价按落在哪个数量区间定）
type QtyPriceBand struct {
	// Lower / Upper 数量区间 · Upper=0 表示"及以上"（最高档无上限）
	Lower int `json:"lower"`
	Upper int `json:"upper"`
	// UnitPriceCredits microunit · 这档单价（我方积分 · adapter 换算好）
	UnitPriceCredits int64 `json:"unit_price_credits"`
	// Region us / eu / "" 全区
	Region string `json:"region,omitempty"`
}

// KeyTierLister 可选接口 · vendor 有"数量分档"端点就实现（如 key-price-tiers）。
// backfiller 拉进来落 vendor_price_tier（tier_kind='qty_band'）。
type KeyTierLister interface {
	// ListKeyTiers 拉当前数量分档表 · 空 slice = 无分档（当前 flat）
	ListKeyTiers(ctx context.Context) ([]QtyPriceBand, error)
}

// TimeDecayLister 可选接口 · vendor 有"时间降价"报价端点就实现（逐区一份 schedule）。
// backfiller 拉进来落 vendor_price_tier（tier_kind='time_decay'）。
//
// 部分 vendor 的降价 schedule 端点需网页 session token（API key 打不了）· 实现方
// 自己处理鉴权 · 未配置 token 返 (nil, nil)（backfiller 静默跳过 · 不清旧值）。
type TimeDecayLister interface {
	// ListTimeDecay 拉各区当前降价 schedule · 每区一个 TieredPricing（Region 已填）。
	// 返 nil = 未配置 / 拿不到（保留上次落库值 · 不清空）。
	ListTimeDecay(ctx context.Context) ([]TieredPricing, error)
}

type ZoneStock struct {
	Zone      Zone
	Region    string
	Available int
	// UnitPrice **仅供估价**。实扣以 Purchase 返回的 TotalCost 为准 ——
	// 同区可能有多辆车混价（vendor 档案 §7 明确警告过）。
	//
	// **注意** · 这个字段是 vendor 侧的**原始报价**（可能 USD / CNY / credit）·
	// 落库时经 vendor_pricing.credits_per_unit 换算成积分（our_unit_credits · docs/10-pricing §1.3）。
	// 之后所有读方（decider / vendorview / PricedFor）**读积分列 · 不再算**。
	UnitPrice Money
}

// ── Purchase ─────────────────────────────────────────

type PurchaseRequest struct {
	Count int
	// ClientOrderID 32 hex 幂等键。vendor 不支持幂等时忽略。
	// **重试时必须传同一个值**，否则会变成两笔独立订单。
	ClientOrderID string
	Zone          *Zone
	// OrderID 部分 vendor 支持指定批次
	OrderID *string
	// MaxTotal 价格保护（部分 vendor 支持）
	MaxTotal *Money
	// Kind 买哪种账号类型 · 空 = AccountEnterprise（老行为 · 向后兼容）。
	//
	// **必须跟估价时用的 kind 一致** —— 两套货架价不同（docs/24 §1）·
	// 用企业价估、按个人池下单会导致实扣与预估不符。
	Kind AccountKind
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
	// PartiallyRefunded · vendor 侧已把未成交部分退了（部分 vendor 独家有这个态）。
	//
	// **为什么要这个字段**：`Purchased < Requested` 有两种含义 ——
	//   ① 只成交了一部分 · **差额还在 vendor 那边没退** → 我方要自己对账追
	//   ② 只成交了一部分 · **vendor 已经把差额退了** → 我方按 TotalCost 结算就完事
	// 混在一起会导致重复退款或漏退。vendor 明确告知②时置 true。
	PartiallyRefunded bool
	Raw               json.RawMessage
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
