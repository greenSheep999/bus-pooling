# bus-pooling · Provider / Vendor 契约

> 前置：`03-modules.md § Layer 2` · `docs/vendors/*.md`
> 本文钉 **Provider / Vendor 的 Go interface + 归一化 struct 签名**。落码时直接对应到 `internal/providers/*` 的代码。
>
> **原则**：每家 vendor 各自的字段口径不同，`adapter` 负责翻译到统一契约。**上层永远只对统一 interface 编程**。

## 1. 目录布局

```
internal/providers/
├── provider.go             顶层 Provider interface + 类型
├── vendor.go               Vendor interface + 归一化 struct
├── errors.go               统一错误契约（APIError + 分类 sentinel）
├── webhook.go              WebhookEvent 归一化 + Parser interface
└── kiro/
    ├── kiro.go             kiro provider 层公共类型（Zone / KskPayload / ...）
    ├── register.go         把 6 家 vendor 注册到全局 Registry
    └── vendors/
        ├── kiro91/
        │   ├── adapter.go      实现 Vendor + WebhookParser
        │   ├── types.go        vendor 私有 wire type（不外暴）
        │   └── adapter_test.go
        ├── kiroceo/ ...
        ├── kirooo/ ...
        ├── kiroappio/ ...
        ├── kiroappcc/ ...
        └── kirodrop/ ...
```

**Adapter 私有类型** (`types.go`) 只在自己包内可见；**归一化** struct 才 export，跨包用。

## 2. 顶层 Provider interface

```go
// internal/providers/provider.go
package providers

type ProviderID string  // "kiro" | 未来: "cursor"

type Provider interface {
    // 常量
    ID() ProviderID

    // 获取此 provider 下的所有已注册 vendor（配置 + adapter）
    Vendors(ctx context.Context) ([]VendorEntry, error)

    // 获取特定 vendor
    Vendor(vendorID VendorID) (Vendor, error)

    // 解析 webhook（provider 层的公共解析，若不同 provider 差别大，可下放到 vendor 层）
    WebhookParser() WebhookParser
}

type VendorEntry struct {
    VendorID    VendorID
    DisplayName string        // 面向用户展示的名字（"Kiro Market"）
    Vendor      Vendor
    Enabled     bool
}
```

**注册**：`internal/providers/kiro/register.go` 在 init 时把 kiro 下 6 家 vendor 注册到全局 `Registry`。

## 3. Vendor interface

**统一 6 家 vendor 都实现的最小接口**（跨家差异用可选字段 or Capability 抽象）：

```go
// internal/providers/vendor.go
package providers

type VendorID string  // "kiro91" | "kiroceo" | "kirooo" | "kiroappio" | "kiroappcc" | "kirodrop"

type Vendor interface {
    // 常量
    ID() VendorID
    ProviderID() ProviderID
    DisplayName() string

    // Capability 声明（每家不同能力用这个查）
    Capability() Capability

    // 核心业务方法（每家都要实现）
    Stock(ctx context.Context, opts StockOptions) (*StockSnapshot, error)
    Purchase(ctx context.Context, req PurchaseRequest) (*PurchaseResult, error)
    OrderKeys(ctx context.Context, orderID string) (*PurchaseResult, error)   // 补拉
    Balance(ctx context.Context) (*Balance, error)

    // 存活/用量（用于 deathwatch + 决策模型）· 见 §5 平均寿命
    KeyHealth(ctx context.Context, key string) (*KeyHealth, error)
    KeyStats(ctx context.Context, opts KeyStatsOptions) (*KeyStatsBatch, error)

    // 可选（不是每家都有）
    Redeem(ctx context.Context, code string) (*RedeemResult, error)          // 若不支持返回 ErrNotSupported
    Usage(ctx context.Context, keys []string) (*UsageBatch, error)           // 若不支持返回 ErrNotSupported
}

type Capability struct {
    SupportsIdempotency     bool  // client_order_id
    SupportsZones           bool  // us / eu
    SupportsWebhook         bool
    WebhookHasSignature     bool  // 91kiro/drop=true, kiroceo/kiro.ooo=false
    SupportsBatchPurchase   bool
    HasWarranty             bool
    WarrantyMinutes         int   // 91kiro=10; drop=?
    KeyPayloadShape         KeyPayloadShape  // FourTuple | JustKey | KeyRegion
}

type KeyPayloadShape string
const (
    KeyPayloadFourTuple KeyPayloadShape = "four_tuple"   // {key, account, password, issuer_url} 91kiro/kiroceo/kirooo/kiroappio
    KeyPayloadJustKey   KeyPayloadShape = "just_key"     // {key} kiroappcc
    KeyPayloadKeyRegion KeyPayloadShape = "key_region"   // {key, region} kirodrop
)
```

**为什么用 Capability 而不是"最大公约数"**：每家能力不一样（91kiro 有 idempotency、drop 没有），若上层要用"有幂等键才这么做"就查 Capability，避免统一 interface 弱化到最差水平。

## 4. 请求 / 响应归一化

### 4.1 Stock（库存 + 报价）

```go
type StockOptions struct {
    Zone *Zone     // 可选，某些 vendor 分区
}

type Zone string
const (
    ZoneUS Zone = "us"
    ZoneEU Zone = "eu"
)

type StockSnapshot struct {
    VendorID    VendorID
    ObservedAt  time.Time
    Available   int              // 总可购数
    MaxPerOrder int
    Zones       []ZoneStock      // 各区（drop/kiroappcc 无区就一个 general）
    Balance     Money            // 我方在此 vendor 侧的余额
    WarrantyMinutes int
}

type ZoneStock struct {
    Zone      Zone
    Available int
    UnitPrice Money
}

type Money struct {
    Amount   int64      // microunit（1 元 = 1_000_000）
    Currency string     // "credit" (vendor 内部积分) | "USD" | "CNY" | ...
}
```

**归一化难点** —— 六家单价字段：

| Vendor | 原字段 | 归一化 |
|---|---|---|
| 91kiro | `zones[].unit_price` | `ZoneStock.UnitPrice` per zone |
| kiroceo | `zones[].unit_price` | 同上 |
| kirooo | `key-price-tiers` 阶梯 | 取 base 或首档，`ZoneStock.UnitPrice`；tier 详情放 raw 里 |
| kiroappio | `price_min` / `price_max` | 取 `price_min`；把 `price_max` 塞 raw |
| kiroappcc | `keyPrice`（无区）| 单 ZoneStock，`Zone: general` |
| kirodrop | `stock.price` USD | `ZoneStock.UnitPrice`，`Currency: USD`；混币在 decider 里换算 |

### 4.2 Purchase（拉号）

```go
type PurchaseRequest struct {
    Count           int
    ClientOrderID   string     // 32 hex 幂等键；若 vendor 无幂等则忽略
    Zone            *Zone      // us/eu；vendor 无区忽略
    OrderID         *string    // 部分 vendor 支持指定 batch (kiroappio/drop)，可选
    MaxTotalCNY     *Money     // 部分 vendor 支持价格保护 (drop)
}

type PurchaseResult struct {
    ClientOrderID     string
    VendorOrderID     string          // vendor 侧订单 id
    Zone              Zone
    Requested         int
    Purchased         int             // 实际成交
    Keys              []KeyPayload
    UnitPrice         Money
    TotalCost         Money            // 权威值（=Σ paid）
    Remaining         Money            // 扣后余额
    WarrantyUntil     *time.Time
    Replayed          bool             // 是否幂等重放
    RawVendorResponse json.RawMessage  // 原始响应存档
}

type KeyPayload struct {
    Key         string          // ksk_ / sk-（vendor 各家）
    Account     string          // 空表示 vendor 不给
    Password    string          // 空表示 vendor 不给
    IssuerURL   string          // 空表示 vendor 不给
    Region      string          // drop 才有；vendor 无 region 就是空
    Paid        Money           // 这一把 key 实际扣的
    WarrantyUntil *time.Time    // 这一把的质保窗口
}
```

**归一化难点**：各家 KeyPayload 形态不同，见 `Capability.KeyPayloadShape`；上层拿到后按 shape 分派使用。

### 4.3 KeyHealth / KeyStats

```go
type KeyHealth struct {
    Key       string
    Alive     bool
    LastCheck time.Time
    RawStatus json.RawMessage
}

type KeyStatsOptions struct {
    Window  Window   // "24h" | "7d" | "30d"
    GroupID string   // 部分 vendor 可按 group 查（我方内部 housepool group 不适用）
}

type Window string
const (
    Window24h Window = "24h"
    Window7d  Window = "7d"
    Window30d Window = "30d"
)

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
    Concurrency  *int    // 可能 nil（vendor 未提供）
}
```

## 5. WebhookParser interface

```go
// internal/providers/webhook.go
package providers

type WebhookParser interface {
    // VerifySignature 校验签名（若 vendor 无签名，返回 ErrNoSignature 让上层按 URL secret 判定）
    VerifySignature(secret string, headers http.Header, rawBody []byte) error

    // Parse 归一化为内部事件
    Parse(rawBody []byte, headers http.Header) (*WebhookEvent, error)
}

type WebhookEvent struct {
    VendorID       VendorID
    EventID        string
    EventType      EventType
    OrderID        string      // 若事件带
    PurchaseOrderID string     // 91kiro/drop 的幂等键
    NewKeys        int
    DeadKeys       int
    RefundAmount   *Money
    Zone           Zone
    ReceivedAt     time.Time
    RawPayload     json.RawMessage
}

type EventType string
const (
    EventNewKeysAvailable EventType = "new_keys_available"
    EventAllKeysDead      EventType = "all_keys_dead"
    EventKeyRevokedAbuse  EventType = "key_revoked_abuse"     // 仅 kiroappio
    EventKeySuspect       EventType = "key_suspect"            // 仅 kirooo (via Telegram)
    EventWarrantyRefund   EventType = "warranty_refund"
    EventTest             EventType = "test"
)
```

**归一化难点** —— 各家事件字段不一致：

| Vendor | new_keys_available 关键字段 | 归一化到 |
|---|---|---|
| 91kiro | `zone / new_keys / purchase_order_id / pool_id` | Zone / NewKeys / PurchaseOrderID |
| kiroceo | `zone / new_keys / purchase_order_id / pool_id` | 同上 |
| kirooo | `new_keys / client_order_id` （也叫 purchase_order_id） | 同上 |
| kiroappio | `new_keys / order_id / purchase_order_id / mother_id / visibility / stock_us / stock_eu / price_us / price_eu` | NewKeys / OrderID / PurchaseOrderID；其余进 RawPayload |
| kiroappcc | `event / count / available / price / time / id` | 尽力映射，字段名不同用 raw 兜 |
| kirodrop | `region / new_keys / order_id / purchase_order_id / dispatch_id / regions[] / _by_region` | Zone / NewKeys / OrderID / PurchaseOrderID；multi-region 合并事件走 RawPayload 处理 |

**签名验证** 每家规则不同：

| Vendor | Header | 算法 | 备注 |
|---|---|---|---|
| 91kiro | `X-KM-Signature: sha256=<hex>` | HMAC-SHA256 (`timestamp + "." + body`) | timestamp 5 min 窗口 |
| kirodrop | `X-Kiro-Signature: v1=<hex>` | HMAC-SHA256 (`timestamp + "." + body`) | 同 |
| kiroceo | 无签名 | — | 靠 URL secret |
| kirooo | 无签名 | — | 靠 URL secret |
| kiroappio | 未在 api-docs 展示 | 待登录后台再确认 | TBD |
| kiroappcc | `X-Kiro-Signature` HMAC-SHA256（`webhook_secret` 加密请求体） | HMAC-SHA256 | 事件字段不同 |

## 6. 错误契约

```go
// internal/providers/errors.go
package providers

// Sentinel 错误（用 errors.Is 分派）
var (
    ErrAuth              = errors.New("provider: auth failure")
    ErrRateLimited       = errors.New("provider: rate limited")
    ErrInsufficientFunds = errors.New("provider: insufficient funds")
    ErrNoStock           = errors.New("provider: no stock")
    ErrPurchaseCapReached = errors.New("provider: purchase cap reached")
    ErrRetrySameOrder    = errors.New("provider: retry same client_order_id")
    ErrIdempotencyConflict = errors.New("provider: idempotency conflict")
    ErrBadZone           = errors.New("provider: bad zone")
    ErrNotSupported      = errors.New("provider: capability not supported")
    ErrUpstream          = errors.New("provider: upstream error")
    ErrTimeout           = errors.New("provider: timeout")
    ErrNoSignature       = errors.New("provider: no signature configured")
    ErrBadSignature      = errors.New("provider: signature mismatch")
)

// APIError 携带具体信息，包裹 sentinel
type APIError struct {
    VendorID    VendorID
    StatusCode  int
    VendorCode  string        // vendor 原始 code（若有）
    Message     string
    RetryAfter  *time.Duration
    Sentinel    error         // 上面的一个
    RawResponse json.RawMessage
}

func (e *APIError) Error() string  { ... }
func (e *APIError) Unwrap() error { return e.Sentinel }
```

**决策模型**在 fallback 时 `errors.Is(err, ErrNoStock)` 就走次选、`errors.Is(err, ErrRateLimited)` 就退避、`errors.Is(err, ErrRetrySameOrder)` 就复用同 `client_order_id` 重试。

## 7. 每家 vendor 归一化 · 快查

| Vendor | 幂等键 | 签名 | 混币 | 分区 | KeyPayload | 特殊 |
|---|---|---|---|---|---|---|
| 91kiro | 32-hex `client_order_id` 强制 | sha256= HMAC | 无（纯 credit） | us/eu | FourTuple | vendor 内 credit=1:1 CNY |
| kiroceo | 32-hex `client_order_id` 强制 | 无 | 无（纯 credit） | us/eu | FourTuple | 单价随卖家动 |
| kirooo | `client_order_id` 存在（形态是 `ORD` 前缀短串） | 无 | 无（1 积分 = 1 CNY 显式） | us/eu | FourTuple | Telegram 通道；发车能力富 |
| kiroappio | 32-hex `client_order_id` 强制 | 未确认 | 无（纯 credit） | us/eu | FourTuple + `price` per key | key_revoked_abuse 事件独有 |
| kiroappcc | **无幂等键（风险）** | HMAC-SHA256 X-Kiro-Signature | 无（纯 credit） | 无 | JustKey | camelCase 字段；网络超时可能双扣 |
| kirodrop | 32-hex `client_order_id` 强制 | v1= HMAC | **有**（余额 CNY / 单价 USD）| us/eu | KeyRegion | 多 AK/SK 合并事件 |

**接入优先级**（对应阶段 1a → 1b）：

1. **91kiro** （首家，文档最全，语义最严）→ 阶段 1a
2. kiroceo（次全）
3. kirooo（能力富，Telegram 通道要单独接口）
4. kiroappio（webhook 签名未确认）
5. kirodrop（混币，比价归一时特殊处理）
6. **kiroappcc**（**最难/最风险**，无幂等；最后接）

## 8. 注册与配置

```go
// internal/providers/kiro/register.go
package kiro

import "bus-pooling/internal/providers"

func Register(r *providers.Registry, cfg Config) error {
    r.Register(kiro91.New(cfg.Kiro91))
    r.Register(kiroceo.New(cfg.Kiroceo))
    r.Register(kirooo.New(cfg.Kirooo))
    r.Register(kiroappio.New(cfg.Kiroappio))
    r.Register(kiroappcc.New(cfg.Kiroappcc))
    r.Register(kirodrop.New(cfg.Kirodrop))
    return nil
}

type Config struct {
    Kiro91    kiro91.Config
    Kiroceo   kiroceo.Config
    // ...
}

// 各家自己的 config
type Config struct {   // in kiro91 pkg
    Enabled   bool
    BaseURL   string     // 例："https://api.91kiro.com"
    APIKeyRef string     // 指向 secrets 表里加密存的 key
    ProxyURL  string     // 可选
    Timeout   time.Duration
}
```

**vendor account 凭证明文永不落 config 文件**；`APIKeyRef` 指向 `secrets` 包管理的加密存储。

## 9. Adapter 单测约定

每家 vendor 包必须自带：

- `adapter_test.go` · 用 `httptest` mock vendor endpoint，覆盖 Stock / Purchase / OrderKeys 的成功 + 错误场景
- `webhook_test.go` · 用真实签名或 fixture 覆盖 Parse / VerifySignature 各种事件

Mock 数据可从 `docs/vendors/*.md` 里的响应示例摘录，或未来抓自真实 vendor（脱敏）。

## 10. 决策模型如何用（示例）

`decider` 拉快照 + 归一算价 + 选 vendor：

```go
snapshots := []providers.StockSnapshot{}
for _, entry := range providersRegistry.All() {
    snap, err := entry.Vendor.Stock(ctx, providers.StockOptions{Zone: intent.Zone})
    if err != nil {
        // errors.Is(err, providers.ErrRateLimited) → 跳过 + 退避
        continue
    }
    snapshots = append(snapshots, *snap)
}

// 跨 vendor 归一算 "每积分能活多久"
best := pickBest(snapshots, intent)
result, err := best.Vendor.Purchase(ctx, providers.PurchaseRequest{...})
if errors.Is(err, providers.ErrNoStock) {
    // fallback 到次选
    ...
}
```

**注意**：`decider` **不 import** 任何 vendor 具体包，只用 `providers.Vendor` interface。

## 11. TBD

- **kiroappio webhook 签名规则** —— 阶段 1b 接入时探明并补进 §5 签名表
- **kirooo Telegram 通道** —— 是否也走 `WebhookParser`，或另开一个 interface（未决）
- **KeyHealth 的 vendor 侧兜底** —— 若 vendor 未提供直接的 KeyHealth 端点（drop 有 `/api/status`，其他家用 stats 推），adapter 内部如何组织
