package api

// strategy_effective · 1f-C · 策略优先级铁律(15-scheduling §4.3.4)接入点。
//
// 定义 EffectiveDeps 组合器 · api 层各 handler(手动拉号 / 建车 / record 单独拉)
// 都通过它调 strategy.Effective() 拿 EffectiveStrategy。
//
// **组合器抽出来**是为了跟其他触发路径(scheduler / deathwatch / webhook 桥 · 见
// cmd/bus-pooling/main.go)复用同一份接线 —— 换 store 只改一处。

import (
	"context"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
)

// effectiveDeps · strategy.EffectiveDeps 的 api 层实现。
//
// 三个 store 都从 Server 直接引 · 不额外持锁 · 每次 Effective() 都读一次库
// (v1 期请求量低 · 不缓存)。
type effectiveDeps struct {
	strategies *strategy.Store
	buses      *bus.Store
	sys        strategy.SystemDefaults
}

func (d *effectiveDeps) GlobalGet(ctx context.Context, passengerID string) (strategy.Strategy, error) {
	return d.strategies.Get(ctx, passengerID)
}

func (d *effectiveDeps) BusGet(ctx context.Context, busID string) (*strategy.BusStrategy, error) {
	return d.buses.EffectiveBusGet(ctx, busID)
}

func (d *effectiveDeps) SystemDefaults() strategy.SystemDefaults {
	if d.sys.PerRoundCount < 1 {
		// 兜底 · 装配层没传 SysDefaults 时不能让 Effective() 返 0
		d.sys.PerRoundCount = 1
	}
	if d.sys.DefaultZone == "" {
		d.sys.DefaultZone = strategy.ZoneAuto
	}
	return d.sys
}

// effective · 拿 Server 里三 store 组装 EffectiveDeps · 调 strategy.Effective。
//
// 所有 api handler 走这一个入口 —— 手动拉号 · 建车 · record 单独拉。
func (s *Server) effective(ctx context.Context, passengerID, busID string, req *strategy.RequestOverride) (strategy.EffectiveStrategy, error) {
	deps := &effectiveDeps{
		strategies: s.strategies,
		buses:      s.buses,
		sys:        s.sysDefaults,
	}
	return strategy.Effective(ctx, deps, passengerID, busID, req)
}

// buildManualPullOverride · 从 pullRequest 组装 request override(§4.3.2d)。
//
// 拉号 API(/api/me/pull / /api/me/buses/{id}/pull) 传的一次性字段:
//   - Count · 有值(≥1)就走 request 链最高优先级 · 走完 Effective 后仍从 pullRequest.Count 传给 decider
//   - VendorID · "auto" 已在上游清成 ""(空 = 不指定 · 不覆盖)
//   - Zone · "auto" 已在上游清成 ""(同上)
//   - pullRequest **没有** max_unit_price 字段 · 手动收紧走 request 层要新加字段(未来)
func buildManualPullOverride(req pullRequest) *strategy.RequestOverride {
	o := &strategy.RequestOverride{}
	if req.Count > 0 {
		c := req.Count
		o.Count = &c
	}
	if req.VendorID != "" {
		v := req.VendorID
		o.Vendor = &v
	}
	if req.Zone != "" {
		z := req.Zone
		o.Zone = &z
	}
	return o
}

// nilIfZeroInt64 · 0 → nil · 非 0 → 指针。strategy.CanPull 的 BusMaxUnitPrice 用
// nil 表"车级不限" · Effective 输出的 MaxUnitPrice 用 0 表不限 · 这里翻译。
func nilIfZeroInt64(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}
