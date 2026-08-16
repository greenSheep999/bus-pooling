package decider

import (
	"context"
	"errors"
	"fmt"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// Recover 处理**一条**卡住的 pending_purchase。由 janitor 按超时阈值捞出来调用。
//
// 每个状态对应一条明确的补偿规则（09-transactions §2）。**这里的每个分支都要么推进
// 状态、要么显式转 need_manual 报警** —— 静默失败是最糟糕的（下一轮 janitor 又扫到，
// 无限循环还不知道为什么）。
func (o *Orchestrator) Recover(ctx context.Context, p Pending) error {
	switch p.Status {
	case StatusInitial:
		// 未做任何外部动作 · 直接删（reserveFunds 崩在这才有可能留下）
		return o.deleteInitial(ctx, p.ID)
	case StatusReserved:
		// 未调 vendor · 释放冻结即可
		return o.releaseAndCancel(ctx, p.ID, p.PassengerID, p.ReservedAmount, StatusReserved)
	case StatusPurchasing:
		return o.recoverPurchasing(ctx, p)
	case StatusPurchased:
		return o.recoverPurchased(ctx, p)
	case StatusImported:
		return o.recoverImported(ctx, p)
	}
	// 终态 / need_* 不该被扫到；扫到了就当无操作
	return nil
}

// recoverPurchasing 是 P0-1 的补偿点（§2.1）：
//   - vendor 有幂等键 → 用同 client_order_id 重放
//   - vendor 无幂等键 → 直接转 need_manual（无法安全判断是否扣款）
func (o *Orchestrator) recoverPurchasing(ctx context.Context, p Pending) error {
	vendor, err := o.vendorFor(providers.VendorID(p.VendorID))
	if err != nil {
		return o.markNeedManual(ctx, p.ID, StatusPurchasing,
			fmt.Sprintf("vendor %q 未装配·无法恢复", p.VendorID))
	}
	if !vendor.Capability().SupportsIdempotency {
		return o.markNeedManual(ctx, p.ID, StatusPurchasing,
			"vendor 无幂等键，purchasing 无法安全恢复")
	}

	// 重放：vendor 会返回原批（若之前成功），或明确 no_stock（若之前根本没成交）
	result, err := vendor.Purchase(ctx, providers.PurchaseRequest{
		Count:         p.CountRequested,
		ClientOrderID: p.ClientOrderID,
		// **必须用落库的原 kind 重放** —— 两个池是不同端点/不同价 ·
		// 用错池等于下了一笔全新的单（migration 046 存这列就为了这个）
		Kind: p.AccountKind.Normalize(),
	})
	if err != nil {
		switch {
		case errors.Is(err, providers.ErrNoStock):
			// vendor 明确说这单没成交 · 安全释放冻结
			return o.releaseAndCancel(ctx, p.ID, p.PassengerID, p.ReservedAmount, StatusPurchasing)
		case errors.Is(err, providers.ErrRateLimited), errors.Is(err, providers.ErrUpstream), errors.Is(err, providers.ErrTimeout):
			// 上游临时问题 · 留着让下一轮再试
			return err
		default:
			// 别的错误当"不安全，别动" · 报警
			return o.markNeedManual(ctx, p.ID, StatusPurchasing,
				fmt.Sprintf("重放 vendor.Purchase 失败: %v", err))
		}
	}
	if result.Purchased == 0 {
		return o.releaseAndCancel(ctx, p.ID, p.PassengerID, p.ReservedAmount, StatusPurchasing)
	}

	// 拿到原批 · 推 purchased 后继续走导入 + 结算
	if err := o.state.AdvanceWith(ctx, p.ID, StatusPurchasing, StatusPurchased,
		Fields{VendorOrderID: result.VendorOrderID}); err != nil {
		return err
	}
	// 递归走 purchased 恢复分支（共用同一份代码路径）
	p.Status = StatusPurchased
	p.VendorOrderID = result.VendorOrderID
	return o.finishFromPurchased(ctx, p, result)
}

// recoverPurchased 是崩在"vendor 出号了但没入池"的补偿点。
// 用 vendor_order_id 补拉 vendor 侧的原批，重新走导入 + 结算。
func (o *Orchestrator) recoverPurchased(ctx context.Context, p Pending) error {
	if p.VendorOrderID == "" {
		// purchased 但没 order_id 是不该发生的（AdvanceWith 一起写的）· 报警
		return o.markNeedManual(ctx, p.ID, StatusPurchased, "purchased 状态缺 vendor_order_id")
	}
	vendor, err := o.vendorFor(providers.VendorID(p.VendorID))
	if err != nil {
		return o.markNeedManual(ctx, p.ID, StatusPurchased,
			fmt.Sprintf("vendor %q 未装配·无法恢复", p.VendorID))
	}
	result, err := vendor.OrderKeys(ctx, p.VendorOrderID)
	if err != nil {
		if errors.Is(err, providers.ErrNotFound) {
			// vendor 那边找不到 · 但状态是 purchased 说明 Purchase 曾成功 —— 转人工
			return o.markNeedManual(ctx, p.ID, StatusPurchased,
				"vendor 找不到 order_id · Purchase 曾成功但 OrderKeys 拿不到")
		}
		return err
	}
	return o.finishFromPurchased(ctx, p, result)
}

// finishFromPurchased 从 purchased 继续走导入 + 结算 —— purchasing / purchased
// 恢复的共用尾巴。
func (o *Orchestrator) finishFromPurchased(ctx context.Context, p Pending, result *providers.PurchaseResult) error {
	credIDs, err := o.importToPool(ctx, p.TargetGroup, result)
	if err != nil {
		return o.markNeedManual(ctx, p.ID, StatusPurchased,
			fmt.Sprintf("恢复期号池导入失败: %v", err))
	}
	if err := o.state.Advance(ctx, p.ID, StatusPurchased, StatusImported); err != nil {
		return err
	}
	// 恢复时用落库的 reserve_split 还原分摊方案（多人车才非空）
	if _, err := o.settle(ctx, p.ID, p, result, credIDs, p.SplitPlanFromReserve()); err != nil {
		return err
	}
	return nil
}

// recoverImported 处理"号已进池但 settle 事务没提交"的崩溃窗口（review P0-4）。
//
// 判断幂等的关键：settle 事务提交时会写 pull_round 一行 + pending_purchase.status=completed。
// 事务原子，所以三种可能：
//   - pending 还是 imported / pull_round 不存在 → settle 事务从未提交 → 重跑
//   - pending 已 completed → 事务提交过（说明 janitor 早于状态 poll 拿到旧行）→ 无操作
//
// 重跑要拿到"当时那批 vendor 号"—— 用同 client_order_id 重放 vendor.Purchase 拿回原批
// （幂等 vendor 保证）· 号池的 BatchImport 也是幂等（同 key 返 duplicate 事件）。
// 无幂等的 vendor 只能报警人工。
func (o *Orchestrator) recoverImported(ctx context.Context, p Pending) error {
	vendor, err := o.vendorFor(providers.VendorID(p.VendorID))
	if err != nil {
		return o.markNeedManual(ctx, p.ID, StatusImported,
			fmt.Sprintf("vendor %q 未装配·无法恢复", p.VendorID))
	}
	if !vendor.Capability().SupportsIdempotency {
		return o.markNeedManual(ctx, p.ID, StatusImported,
			"vendor 无幂等键，imported→completed 无法安全恢复")
	}

	// 幂等重放 vendor · 拿回原批 keys（当时那次的完整信息）
	result, err := vendor.Purchase(ctx, providers.PurchaseRequest{
		Count:         p.CountRequested,
		ClientOrderID: p.ClientOrderID,
		// **必须用落库的原 kind 重放** —— 两个池是不同端点/不同价 ·
		// 用错池等于下了一笔全新的单（migration 046 存这列就为了这个）
		Kind: p.AccountKind.Normalize(),
	})
	if err != nil {
		return err
	}
	if result.Purchased == 0 {
		// 极端：重放返 0 但状态是 imported —— 说明状态和 vendor 对不上，转人工
		return o.markNeedManual(ctx, p.ID, StatusImported,
			"imported 恢复 · vendor 重放返 0 成交，状态不一致")
	}

	// 号池 BatchImport 幂等 · 已导入的返 duplicate，credential_ids 走 duplicate 事件里的 id
	credIDs, err := o.importToPool(ctx, p.TargetGroup, result)
	if err != nil {
		return o.markNeedManual(ctx, p.ID, StatusImported,
			fmt.Sprintf("恢复期号池重导失败: %v", err))
	}

	// 走 settle · 里面的条件 UPDATE 会把 imported→completed，重复调不会双扣（第二次 RowsAffected=0 报 ErrStaleTransition）
	if _, err := o.settle(ctx, p.ID, p, result, credIDs, p.SplitPlanFromReserve()); err != nil {
		// 特殊：如果这行已经推到 completed（前一次 settle 事务其实提交了，只是我方没记录）
		// 那 advancePendingTx 里的条件 UPDATE 会命中 ErrStaleTransition —— 幂等成功
		if errors.Is(err, ErrStaleTransition) {
			return nil
		}
		return err
	}
	return nil
}

func (o *Orchestrator) deleteInitial(ctx context.Context, id string) error {
	res, err := o.db.ExecContext(ctx,
		`DELETE FROM pending_purchase WHERE id = ? AND status = ?`,
		id, string(StatusInitial))
	if err != nil {
		return fmt.Errorf("decider: 删 initial: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleTransition
	}
	return nil
}

func (o *Orchestrator) markNeedManual(ctx context.Context, id string, from Status, reason string) error {
	return o.state.AdvanceWith(ctx, id, from, StatusNeedManual, Fields{Error: reason})
}
