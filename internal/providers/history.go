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
// 不实现的 vendor 该 backfill 跳过（比如 kirodrop 没这个端点）。
type OrderHistoryLister interface {
	// ListOrders 拉一页订单历史 · cursor 空 = 从头拉 · 返 NextCursor="" 表示到底。
	// vendor 侧全量少的话可以一次返完（Items 是所有 · NextCursor 空）。
	ListOrders(ctx context.Context, cursor string) (*HistoryPage[VendorOrder], error)
}

// KeyHistoryLister 可选接口 · 拉 key 生命周期。
// kiroappcc 这类 order 里内嵌 key 的 vendor 可以只实现 OrderHistoryLister ·
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
