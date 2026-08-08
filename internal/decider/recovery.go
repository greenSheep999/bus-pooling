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
		// imported 崩了 = settle 事务没提交 · settle 是幂等的（用条件 UPDATE 推 completed）
		// 但 CommitReservedTx 会重复扣款 —— 所以这里保守转 need_manual，让人工核对
		return o.markNeedManual(ctx, p.ID, StatusImported, "imported→completed 恢复未实现，人工核对 ledger")
	}
	// 终态 / need_* 不该被扫到；扫到了就当无操作
	return nil
}

// recoverPurchasing 是 P0-1 的补偿点（§2.1）：
//   - vendor 有幂等键 → 用同 client_order_id 重放
//   - vendor 无幂等键 → 直接转 need_manual（无法安全判断是否扣款）
func (o *Orchestrator) recoverPurchasing(ctx context.Context, p Pending) error {
	if !o.vendor.Capability().SupportsIdempotency {
		return o.markNeedManual(ctx, p.ID, StatusPurchasing,
			"vendor 无幂等键，purchasing 无法安全恢复")
	}

	// 重放：vendor 会返回原批（若之前成功），或明确 no_stock（若之前根本没成交）
	result, err := o.vendor.Purchase(ctx, providers.PurchaseRequest{
		Count:         p.CountRequested,
		ClientOrderID: p.ClientOrderID,
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
	result, err := o.vendor.OrderKeys(ctx, p.VendorOrderID)
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
	if _, err := o.settle(ctx, p.ID, p, result, credIDs); err != nil {
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
