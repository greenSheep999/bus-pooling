// Package coalescer · bus 维度集单调度（03-modules §8）。
//
// 目的：同 bus 内多成员的拉号意图·在窗口内合流成一次拉号（04-scenarios §A2）。
// **不跨 bus 合并**（避免抢资源 / 分账混乱）· **1 人 bus 不集单**（直发 decider）。
//
// 1c-1 骨架说明：
//
//   - 定义 Intent / BatchIntent 类型
//   - `Single(Intent)` · 1 人 bus 意图直发 decider（**已实现**·纯 pass-through）
//   - `Anon(Intent)` · 匿名撮合 bus 合流（**1c-2 才做**·占位报 ErrNotImplemented）
//   - `Team(Intent)` · 邀请码组队合流（**2a 才做**·占位）
//
// 真集单需要意图池（DB 表）+ 窗口定时器 + 决策器 · 1c-2 补。1c-1 阶段前端拿到 bus_id
// 走原 POST /api/me/buses/{id}/pull · 多人同 bus 各自触发 · decider 分别处理。
package coalescer

import (
	"context"
	"errors"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// Intent 一位乘客的拉号意图（strategy 判过·可以下单了）。
type Intent struct {
	PassengerID  string
	BusID        string // 空 = 单独拉号（record group）
	Count        int
	Zone         providers.Zone
	VendorID     providers.VendorID // 空 = auto
	// IdempotencyRecordID · 幂等键关联（已在 api 层建 idempotency_record）
	IdempotencyRecordID string
}

// BatchIntent · 一次合流后的批量意图（1c-2 会真返回）。
//
// Participants 是合流的乘客 id 列表（含请求发起者·顺序 = 加入意图池顺序）。
// CountTotal = sum(Intent.Count)。
type BatchIntent struct {
	BusID          string
	Participants   []string
	CountTotal     int
	Zone           providers.Zone
	VendorID       providers.VendorID
	// IdempotencyRecordIDs · 每个参与意图对应的幂等键 id · 便于 decider
	// 落 pending_purchase 后回填每人的响应（不同乘客拿各自的响应）
	IdempotencyRecordIDs []string
}

// ErrNotImplemented · 1c-2 才做的分支
var ErrNotImplemented = errors.New("coalescer: 1c-2 才做集单窗口调度")

// Single · 1 人 bus 意图直发 · 纯 pass-through。
//
// 语义：Intent → BatchIntent{单人 · 直接下发}。decider 拿到 BatchIntent 后
// 走一次拉号（跟单独拉号等价）。这个函数存在是为了让 decider 的入口统一。
func Single(_ context.Context, in Intent) (*BatchIntent, error) {
	return &BatchIntent{
		BusID:                in.BusID,
		Participants:         []string{in.PassengerID},
		CountTotal:           in.Count,
		Zone:                 in.Zone,
		VendorID:             in.VendorID,
		IdempotencyRecordIDs: []string{in.IdempotencyRecordID},
	}, nil
}

// Anon · 匿名撮合 bus 合流（**1c-2 才做**）。
//
// 1c-1 骨架：签名 + 占位报 ErrNotImplemented · 让上层看到清晰的"未启用"错误·
// 不至于误以为已合流实际却漏了。
func Anon(_ context.Context, _ Intent) (*BatchIntent, error) {
	return nil, ErrNotImplemented
}

// Team · 邀请码组队合流（**2a 才做**）。
func Team(_ context.Context, _ Intent) (*BatchIntent, error) {
	return nil, ErrNotImplemented
}
