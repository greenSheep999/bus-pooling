package providers

import (
	"context"
	"encoding/json"
	"time"
)

// PublicStatusSnapshot 是 vendor 的 fleet-wide 状态快照。
//
// 跟 StockSnapshot 的区别：
//   - StockSnapshot 是"**我方账户**能看到的库存 / 价"（走 /api/my/stock）
//   - PublicStatusSnapshot 是"vendor 侧**整个平台**的累计状态"（走 /api/status）
//
// 不是所有 vendor 都支持 · 只有支持的 vendor 实现 PublicStatuser 接口。
// Prober 拿到后写 vendor_probe 表的扩展字段 · /status 页拿更丰富的数据展示。
type PublicStatusSnapshot struct {
	VendorID   VendorID
	ObservedAt time.Time

	// KeysActive vendor 侧当前活着（能用）的 key 数
	KeysActive int
	// KeysDead vendor 侧当前已失效的 key 数
	KeysDead int
	// KeysStock vendor 侧当前可购买库存（跟 StockSnapshot.Available 类似但脱敏）
	KeysStock int
	// KeysAlive kirooo 特有 · vendor 平台全部活跃 key（含 suspect + active）
	KeysAlive int
	// KeysSuspect kirooo 特有 · 探测异常但还没判死的 key
	KeysSuspect int
	// KeysTotal 平台历史累计发过的 key 总数
	KeysTotal int

	// Generating vendor 是否正在生成新 key（true = 短时供应中）
	Generating bool

	// StartedAt vendor 平台的启动时间 · 用于算 uptime 秒数
	StartedAt *time.Time
	// UptimeSeconds vendor 自报的运行时长秒数（有些 vendor 只出这个不出 started_at）
	UptimeSeconds int64

	// Raw 完整原始响应 · 排查用
	Raw json.RawMessage
}

// PublicStatuser 可选接口 · vendor adapter 实现了它，Prober 就调用 PublicStatus。
// 不实现的 adapter 类型断言失败即可（跟 Redeem / Usage 走 ErrNotSupported 不同 —
// 这个是纯 optional，不算能力协议的一部分）。
type PublicStatuser interface {
	PublicStatus(ctx context.Context) (*PublicStatusSnapshot, error)
}
