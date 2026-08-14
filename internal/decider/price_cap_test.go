package decider

// **回归哨兵 · P2 · 2026-08-14**
//
// 用户设的单价上限 · 老代码只把它折成 vendor 币种传给 adapter 的 max_total_cny 参数 ·
// 但只有其中一家 vendor 原生支持这个参数 · 其他 5 家 adapter 直接无视 —— 涨价保护形同虚设。
// 修：在 decider 里拿到我方权威积分单价（unitCostHint · from vendor_probe_zone）后 ·
// 用积分口径直接硬拦 · 不发下单请求 · 不冻结钱包。
//
// 本文件只测 orchestrator.Pull 里那段护栏（真进去打端点太重 · fixture 也难 mock）·
// 用 white-box：直接构造 Orchestrator + 触发条件 · 断 LimitError。

import (
	"context"
	"errors"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
)

// 单元测试护栏本身的判据（不走 Orchestrator.Pull 全链）· 判据抽出来更容易测。
//
// 语义：
//   - MaxUnitPrice == 0 · 不设上限（0 = 不限制）
//   - unitCostHint == 0 · 未知价 · 不拦（前面已经走别的兜底）
//   - unitCostHint > MaxUnitPrice · 拦
//   - unitCostHint <= MaxUnitPrice · 放行
func TestPriceCapGuard(t *testing.T) {
	cases := []struct {
		name         string
		maxUnitPrice int64
		unitCost     int64
		shouldBlock  bool
	}{
		{"未设上限 · 放行", 0, 100_000_000, false},
		{"上限充足 · 放行", 200_000_000, 100_000_000, false},
		{"恰好等于 · 放行（<= 语义）", 100_000_000, 100_000_000, false},
		{"超上限 1 分 · 拦", 100_000_000, 100_000_001, true},
		{"超上限 10% · 拦", 100_000_000, 110_000_000, true},
		{"未知价 · 不拦（unitCost=0）", 100_000_000, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			blocked := priceCapExceeded(c.maxUnitPrice, c.unitCost)
			if blocked != c.shouldBlock {
				t.Errorf("MaxUnitPrice=%d unitCost=%d · 拦=%v · want %v",
					c.maxUnitPrice, c.unitCost, blocked, c.shouldBlock)
			}
		})
	}
}

// LimitError 结构对得上（strategy 层能通过 errors.As 拿到）
func TestPriceCapErrorShape(t *testing.T) {
	err := &strategy.LimitError{
		Kind:    strategy.LimitUnitPrice,
		Cap:     100_000_000,
		Current: 110_000_000,
	}
	// 上层能识别为 ErrLimitReached
	if !errors.Is(err, strategy.ErrLimitReached) {
		t.Error("应被识别为 ErrLimitReached · 便于 API 层统一处理")
	}
	// errors.As 能取细节
	var le *strategy.LimitError
	if !errors.As(err, &le) {
		t.Error("errors.As 应能取到 LimitError")
	}
	if le.Kind != strategy.LimitUnitPrice {
		t.Errorf("Kind = %v · want LimitUnitPrice", le.Kind)
	}
}

// **未使用抑制** —— 保留 context import · 后续接 Pull 全链集成测试用
var _ = context.Background
var _ = providers.Vendor91Kiro
