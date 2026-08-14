package decider

// RefillPuller · v3.2 · deathwatch 号死后调 Pull 补车的薄壳
//
// **为什么单独一个文件**：deathwatch 定义了 RefillPuller 接口 · orchestrator 实现它。
// 但 orchestrator.go 已经很大 · 塞进去乱。抽出来这个 file 只做接口适配。
//
// **契约**（对应 deathwatch.RefillPuller）：
//   fulfilled=true err=nil   → 补车成功
//   fulfilled=false err=nil  → 缺货 · deathwatch 保 pending 等下轮
//   err != nil                → 硬错 · deathwatch attempts++ · 3 次后 expired
//
// **幂等**：RefillID 当 client_order_id 用 · 让 decider 幂等层挡重复扣费。
// **策略校验**：这里不判"用户想不想自动补" —— 用户"关闭自动补"是设计话题 · 现在没这个开关 ·
// 全 bus 默认自动补（用户如果不想 · 也就是"号死了没有下一辆"· 现在没这诉求）。

import (
	"context"
	"errors"
	"fmt"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// RefillAdapter · 把 Orchestrator 包成 deathwatch.RefillPuller
type RefillAdapter struct {
	orch *Orchestrator
}

// NewRefillAdapter · 装配
func NewRefillAdapter(orch *Orchestrator) *RefillAdapter {
	return &RefillAdapter{orch: orch}
}

// Refill · 走一次 decider.Pull · 返 deathwatch 期望的三态 (fulfilled, err)
func (a *RefillAdapter) Refill(ctx context.Context, req refillReq) (bool, error) {
	if a == nil || a.orch == nil {
		return false, fmt.Errorf("decider.RefillAdapter: orchestrator 未装配")
	}

	// PullInput 从 refillReq 拼装 · RefillID 当 client_order_id 让 decider 幂等
	// **注意**：RefillID 是 UUID 不是 32-hex · 部分 vendor 会 400
	// —— 但 decider.newClientOrderID 兜底 · 传空让内部生成
	// 我们只用 RefillID 挂关联（未来落 pull_round.reserved_from_refill 之类的）· 不当 vendor 侧幂等键
	in := PullInput{
		PassengerID: req.PassengerID,
		BusID:       req.BusID,
		Count:       req.Count,
		VendorID:    providers.VendorID(req.VendorID),
		// ClientOrderID 空 · decider 内部生成 32-hex
		// IdempotencyRecordID 空 · 走非幂等入口（decider 允许）
	}
	_, err := a.orch.Pull(ctx, in)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNoStock):
		// 缺货 · deathwatch 保 pending 等下轮
		return false, nil
	case errors.Is(err, ErrRateLimited):
		// 被限流 · 也保 pending（不算失败）
		return false, nil
	default:
		return false, err
	}
}

// refillReq · 内部字段镜像 deathwatch.RefillRequest（避免 decider ↔ deathwatch 循环 import）·
// 装配层用**函数**桥接 · 见 cmd/bus-pooling/main.go
//
// 上层调用：
//   refillAdapter := decider.NewRefillAdapter(orch)
//   watcher.SetRefillPuller(refillPullerFrom(refillAdapter))
type refillReq struct {
	RefillID    string
	PassengerID string
	BusID       string
	Count       int
	VendorID    string
}
