package bus

import (
	"context"
	"errors"

	"github.com/bus-pooling/bus-pooling/internal/strategy"
)

// ToStrategyBus · 把 bus.Strategy 翻译成 strategy.BusStrategy · Effective() 用。
//
// 只做字段搬运 · 保指针语义:
//
//	nil       = 跟随全局默认(§4.3.2b 方案 A)
//	非 nil    = 覆盖本车(含显式 0 / false)
//
// **单独一个函数** —— 让 strategy 包不必 import bus(避免以后 bus.Strategy 长出
// 更多字段时 strategy 包被迫跟着改)。装配层调这个把车级策略喂进 Effective()。
func ToStrategyBus(b Strategy) *strategy.BusStrategy {
	return &strategy.BusStrategy{
		AutoRefillEnabled: b.AutoRefillEnabled,
		RefillWatermark:   b.RefillWatermark,
		RefillMinCount:    b.RefillMinCount,
		PerRoundCount:     b.PerRoundCount,
		MaxUnitPrice:      b.MaxUnitPrice,
		PreferredVendor:   b.PreferredVendor,
		// Zone · bus.Strategy 目前不存(anon_zone 是撮合用) · 常年 nil
		Zone: nil,
	}
}

// EffectiveBusGet · strategy.EffectiveDeps.BusGet 的默认实现 · 装配层用。
//
// **语义** · strategy.EffectiveDeps 要求"车不存在返 nil 不返错"(§4.3.4 记录:
// busID 空 / 车不存在 → 走全局-only 路径)· 这里把 ErrNotFound 吞成 nil。
// 其他错(数据库挂等)照常向上抛。
func (s *Store) EffectiveBusGet(ctx context.Context, busID string) (*strategy.BusStrategy, error) {
	if busID == "" {
		return nil, nil
	}
	b, err := s.Get(ctx, busID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return ToStrategyBus(b.Strategy), nil
}
