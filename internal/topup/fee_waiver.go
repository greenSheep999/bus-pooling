package topup

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// 手续费减免（decisions §8.29 / §8.32）。
//
// 减免额度在**起单时**消耗 · 不在 MarkPaid —— 订单金额（含二维码/跳转链接里带的）
// 在建单那一刻就定了，付款后改不动。
//
// 落库分两个字段（规则见 decisions §8.32）：
//   - channel_fee = 0        乘客不付
//   - fee_subsidy = 原手续费  我方承担的部分 · 单独记不混进 channel_fee
//
// wallet_ledger **不落 channel_fee 那笔** —— 乘客没花这钱。

// boolToInt SQLite 没有 bool 类型 · 用 0/1 存
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// consumeFeeWaiverTx 尝试用掉一次手续费减免额度。
//
// 条件 UPDATE（`fee_waiver_used < fee_waiver_total`）—— 并发下只有一个能成，
// 不会超用。返回 true = 用掉了一次，调用方该按减免后的金额建单。
//
// 没有个人码 / 额度用完 → 返 false（不是错误 · 正常路径）。
func consumeFeeWaiverTx(
	ctx context.Context, tx *sql.Tx, passengerID string, now time.Time,
) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE personal_invite_code
		   SET fee_waiver_used = fee_waiver_used + 1, updated_at = ?
		 WHERE passenger_id = ?
		   AND fee_waiver_used < fee_waiver_total`,
		formatTime(now), passengerID)
	if err != nil {
		return false, fmt.Errorf("topup: 用手续费减免额度: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// returnFeeWaiverTx 把一次减免额度退回去（订单过期 / 取消 / 退款时）。
//
// **为什么必须退**：额度是起单时就扣的。订单没付成 / 退款了，那次减免实际没发生 ——
// 不退等于用户白掉一次额度。
//
// 条件 UPDATE（`fee_waiver_used > 0`）防止退成负数（重复调 / 脏数据）。
func returnFeeWaiverTx(
	ctx context.Context, tx *sql.Tx, passengerID string, now time.Time,
) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE personal_invite_code
		   SET fee_waiver_used = fee_waiver_used - 1, updated_at = ?
		 WHERE passenger_id = ? AND fee_waiver_used > 0`,
		formatTime(now), passengerID); err != nil {
		return fmt.Errorf("topup: 退回手续费减免额度: %w", err)
	}
	return nil
}

// returnFeeWaiverForOrderTx 按订单退回减免额度（订单过期 / 取消 / 退款时调）。
//
// 先查这单有没有用过减免 —— 没用过就什么都不做（幂等 · 重复调安全）。
// 查到用过就退给这单的 passenger，并把 fee_waiver_applied 清掉防重复退。
func returnFeeWaiverForOrderTx(
	ctx context.Context, tx *sql.Tx, orderID string, now time.Time,
) error {
	var passengerID string
	var applied int
	err := tx.QueryRowContext(ctx,
		`SELECT passenger_id, fee_waiver_applied FROM topup_order WHERE id = ?`,
		orderID).Scan(&passengerID, &applied)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("topup: 查订单减免状态: %w", err)
	}
	if applied == 0 {
		return nil // 这单没用过减免 · 无需退
	}

	// 清标记 · 条件 UPDATE 保证只退一次（并发 / 重复调都安全）
	res, err := tx.ExecContext(ctx, `
		UPDATE topup_order SET fee_waiver_applied = 0, updated_at = ?
		 WHERE id = ? AND fee_waiver_applied = 1`,
		formatTime(now), orderID)
	if err != nil {
		return fmt.Errorf("topup: 清减免标记: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // 别人抢先退了
	}
	return returnFeeWaiverTx(ctx, tx, passengerID, now)
}

// HasFeeWaiver 查这个乘客还有没有可用的减免额度（起单前给前端预览用）。
//
// 只读 · 不消耗。真正的消耗在 CreateOrderWithPending 那个事务里做
// （中间余额 / 额度可能被并发改，所以判定和消耗必须同事务）。
func (s *Store) HasFeeWaiver(ctx context.Context, passengerID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM personal_invite_code
		 WHERE passenger_id = ? AND fee_waiver_used < fee_waiver_total`,
		passengerID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("topup: 查减免额度: %w", err)
	}
	return n > 0, nil
}
