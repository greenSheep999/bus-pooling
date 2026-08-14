package main

// 1f-C · 策略 Effective() 桥 · 供 scheduler / deathwatch / webhook 扫号等自动
// 触发路径复用(同 api 层调 strategy.Effective 一致 · §4.3.4)。
//
// 桥的职责:
//  1. 装 strategy.Store + bus.Store + SystemDefaults 一份 · 给多处调用点复用
//  2. 反查每辆车的 creator_passenger_id(自动触发时 pending_refill / bus_id 里没
//     直接带 passenger_id · 用 creator 作 fallback · 跟老代码语义一致)
//
// 不做:
//  - 校验车归属(自动触发路径都由 SQL 已过滤 auto_refill_enabled=1 · 车主 owner)
//  - 缓存(v1 期请求量低 · 每次读库便宜)

import (
	"context"
	"database/sql"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
)

// effectiveDepsMain · strategy.EffectiveDeps 装配层实现(自动触发路径)。
//
// 跟 api.effectiveDeps 结构一致 · 但独立一份 —— api 那份是给 handler 用
// (跟 Server 生命周期绑) · 这份是桥 / 后台任务用。
type effectiveDepsMain struct {
	strategies *strategy.Store
	buses      *bus.Store
	sys        strategy.SystemDefaults
}

func (d *effectiveDepsMain) GlobalGet(ctx context.Context, passengerID string) (strategy.Strategy, error) {
	return d.strategies.Get(ctx, passengerID)
}

func (d *effectiveDepsMain) BusGet(ctx context.Context, busID string) (*strategy.BusStrategy, error) {
	return d.buses.EffectiveBusGet(ctx, busID)
}

func (d *effectiveDepsMain) SystemDefaults() strategy.SystemDefaults {
	if d.sys.PerRoundCount < 1 {
		d.sys.PerRoundCount = 1
	}
	if d.sys.DefaultZone == "" {
		d.sys.DefaultZone = strategy.ZoneAuto
	}
	return d.sys
}

// creatorOfBus · 从 bus 表读 creator_passenger_id(自动触发路径用)。
//
// 桥拿到 busID · 需要 passengerID 才能调 Effective(全局策略是按 passenger 存的)。
// bus_scheduler 的 SQL 已经 SELECT 出 owner_id · 这里保留一个便捷函数给别处用。
func creatorOfBus(ctx context.Context, db *sql.DB, busID string) string {
	if db == nil || busID == "" {
		return ""
	}
	var creator string
	_ = db.QueryRowContext(ctx,
		`SELECT creator_passenger_id FROM bus WHERE id = ?`, busID).Scan(&creator)
	return creator
}
