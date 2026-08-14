package providers

import (
	"context"
	"encoding/json"
	"time"
)

// VendorOrder 一笔 vendor 侧订单 · 对应 vendor 的 /api/my/purchase-orders 端点。
// 字段是"能拉到什么就填什么" · 大多数 vendor 都出 CreatedAt + Purchased ·
// UnitPrice / TotalCost 部分 vendor 出。
type VendorOrder struct {
	// VendorOrderID vendor 侧的稳定单号（同一 vendor 内唯一）
	VendorOrderID string
	// CreatedAt vendor 侧创建时间
	CreatedAt time.Time
	// Purchased 这一批实际拿到的 key 数（不是 requested）
	Purchased int
	// Requested 请求数（可能 = Purchased 也可能 <）· 0 表示未知
	Requested int
	// UnitPrice / TotalCost 走 vendor 侧的 credit 单价 · **内部字段** · 只走 /prices
	// Money.Amount = 0 时视为未知
	UnitPrice Money
	TotalCost Money
	// Source api / manual / … · vendor 自报
	Source string
	// Raw 完整 order JSON · 排查用
	Raw json.RawMessage
}

// VendorKey 一把 vendor 侧的 key 的生命周期快照 · 对应 vendor 的 /api/my/keys 端点。
type VendorKey struct {
	// VendorKeyID vendor 侧稳定 key id
	VendorKeyID string
	// OrderID 关联的 order（可能空 · 部分 vendor 不关联）
	OrderID string
	// KeyMasked 明文的**脱敏版**（前 8 位 + ****）· **绝不返明文**
	KeyMasked string
	Region    string
	// Status active / dead / suspect / handed_off / unknown
	Status string
	// CreatedAt / DispatchedAt vendor 侧的时刻
	CreatedAt    time.Time
	DispatchedAt time.Time
	// DeadAt 挂掉的时间 · Status != dead 时零值
	DeadAt time.Time
	// DeadReason vendor 自报的挂原因
	DeadReason string
	// LastProbeAt vendor 最后一次探测这把 key 的时刻
	LastProbeAt time.Time
	// CurrentUsage / UsageLimit vendor 单位（credit / points）
	CurrentUsage int
	UsageLimit   int
	// WarrantyUntil vendor 侧的质保结束时间 · 跟我方 refill window 无关
	WarrantyUntil time.Time
	// UnitPrice 单把 key 单价（部分 vendor 支持） · **内部字段**
	UnitPrice Money
	// Raw 完整 key JSON · 排查用
	Raw json.RawMessage
}

// HistoryPage 一页历史（分页 · 部分 vendor 会分页 · cursor 空表示已到底）
type HistoryPage[T any] struct {
	Items      []T
	NextCursor string
}

// OrderHistoryLister 可选接口 · vendor adapter 实现了它，backfill 就会调 ListOrders。
// 不实现的 vendor 该 backfill 跳过（比如 本 vendor 没这个端点）。
type OrderHistoryLister interface {
	// ListOrders 拉一页订单历史 · cursor 空 = 从头拉 · 返 NextCursor="" 表示到底。
	// vendor 侧全量少的话可以一次返完（Items 是所有 · NextCursor 空）。
	ListOrders(ctx context.Context, cursor string) (*HistoryPage[VendorOrder], error)
}

// KeyHistoryLister 可选接口 · 拉 key 生命周期。
// 本 vendor 这类 order 里内嵌 key 的 vendor 可以只实现 OrderHistoryLister ·
// backfill 从 VendorOrder.Raw 里再抽 key 出来。
type KeyHistoryLister interface {
	ListKeys(ctx context.Context, cursor string) (*HistoryPage[VendorKey], error)
}

// VendorDispatch 一批 vendor 侧发出的 key（fleet-wide · 全网都能看到）
// 用于 /api/vendors/status 每张卡的"最近开号"曲线。
type VendorDispatch struct {
	// DispatchKey vendor_id 内稳定的批次标识 · 用 dispatched_at 字符串足够
	DispatchKey  string
	Region       string    // us-east-1 之类 · 单区 vendor 留空
	DispatchedAt time.Time // 这批发出时刻
	Count        int       // 这批发了几个 key
	Alive        int       // 现在活着几个（vendor 支持时填）
	Dead         int       // 挂了几个
	DeadAt       time.Time // 全批挂完时刻（可选）
	Status       string    // running / done / dead
	Raw          json.RawMessage
}

// FleetLister 可选接口 · vendor adapter 实现了它，backfill 就会调 ListDispatches。
// 拉的是"vendor 平台**全网**最近开号 / 发货节奏"，跟我方账户交易无关 ·
// **6 家 vendor 只要有类似端点都能实现**，让 /status 页每张卡都有真数据。
type FleetLister interface {
	// ListDispatches 拉最近 N 批 · limit=0 用 vendor 默认
	ListDispatches(ctx context.Context, limit int) ([]VendorDispatch, error)
}

// ── 交叉对账（维度 A① · docs/23-endpoints-todo §1）──────────────────────────
//
// vendor 侧的**积分流水**（recharge / purchase / refund / …）· 拿来跟我方
// `pull_round` + `wallet_ledger` 双向核对：我方记的扣费能不能在 vendor 账本里对上 ·
// 有没有被多扣 / 漏退。**纯内部**（CLAUDE.md §0.1）· 绝不出前端。

// 归一后的流水类型 · 各家原文 reason 五花八门 · adapter 映射到这几个。
// 对账只关心 purchase（扣费）和 refund（退款）· 其余留原文备查。
const (
	LedgerPurchase = "purchase" // 领 key 扣费
	LedgerRefund   = "refund"   // 质保 / 售后退款
	LedgerRecharge = "recharge" // 充值 / 兑换码
	LedgerIncome   = "income"   // 供应侧收入（母号被买走返分）
	LedgerAdjust   = "adjust"   // 运营手工调整
	LedgerOther    = "other"    // 未归类
)

// VendorLedgerEntry 一笔 vendor 侧流水 · 对应各家的 ledger / credits / txns 端点。
type VendorLedgerEntry struct {
	// EntryID vendor 侧稳定流水 id（同 vendor 内唯一 · 幂等主键）。
	// vendor 不给稳定 id 时 · adapter 用 (created_at + reason + amount) 合成指纹。
	EntryID string
	// OrderID 关联的订单号 · **对账的 join 键**（跟我方 pull_round.vendor_order_id /
	// client_order_id 对） · 非 purchase/refund 类可能空。
	OrderID string
	// Reason 归一后类型（上面常量之一）
	Reason string
	// RawReason vendor 原文 reason · 留证
	RawReason string
	// Amount 带符号 microunit · **扣费为负 · 入账为正**（各家口径不一 · adapter 统一到这个约定）
	Amount Money
	// BalanceAfter 该笔后余额（vendor 支持时）· Amount=0 且 BalanceAfter=0 视为未知
	BalanceAfter Money
	// CreatedAt vendor 侧时刻
	CreatedAt time.Time
	// Raw 完整流水 JSON · **对账排查靠它**（字段推断不准时至少 raw 是真的）
	Raw json.RawMessage
}

// LedgerLister 可选接口 · vendor 有积分流水端点就实现。
//
// **⚠️ 上线纪律**（2026-08-14 · 吸取某家 webhook 100% 丢的教训）：
// vendor 不公开响应 schema 时 · adapter 用**容错解析**（多字段名 fallback）+ **永远
// 存 Raw** · 上线后**必须**对着真实响应核一遍字段（看 vendor_ledger.raw）· 别信文档推断。
type LedgerLister interface {
	// ListLedger 拉一页流水 · cursor 空 = 从头 · 返 NextCursor="" 到底。
	ListLedger(ctx context.Context, cursor string) (*HistoryPage[VendorLedgerEntry], error)
}
