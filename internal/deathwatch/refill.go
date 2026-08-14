package deathwatch

// refill · 号死后触发自动补车（P6 · 2026-08-14）
//
// **设计取舍**：
// 老代码 markDead 只改状态 · 不触发补。用户要"自动补车"闭环。但真"补"涉及：
//   · 反查这号原本属于哪一轮 / 哪辆车 / 谁的策略
//   · 判断策略允不允许自动补（用户可能设了"只手动"）
//   · 并发限流（一堆号同时死 · 不能瞬间发一百个下单）
//   · 幂等（重复扫到同一死号不能重复补）
//
// **拆两步**（阶段性回滚友好）：
//   Step 1（本 commit · P6 skeleton）：markDead 后往 pending_refill 塞一条 ·
//     记 dead_credential_id / bus_id / passenger_id / count · 状态 pending。
//     跑独立 worker（本文件的 refillTick）· **只 log 不真 fire** —— 先看落多少条 ·
//     触发场景合不合理 · 攻击面在哪。
//   Step 2（1d · 独立 commit）：refillTick 里调 decider.Pull 真拉 · 加策略校验 +
//     幂等 + 限流。改动集中在这个函数 · 出错回退到 Step 1 状态。
//
// **和 stockwatch.Enqueue 的区别**：
//   stockwatch 是"缺货挂单等补货" · 有完整拉号上下文（reserved 冻结钱 · client_order_id）
//   pending_refill 是"号死了想再拉一批" · 只有死号 + bus/passenger · **没有钱冻结**·
//   1d 真 fire 时要重新走 wallet.Reserve → decider.Pull 全链。
//
// 两者互不替代 · 各解各的问题。

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EnqueueRefill · markDead 触发后调 · 幂等塞 pending_refill 一条。
//
// 参数从 credential_ledger 反查（source_pull_round_id → passenger_id / bus_id）·
// count 用 1（补一个 · Step 2 可能改成"按车级策略配的每轮量"）。
//
// **UNIQUE (dead_credential_id) 保证幂等** —— 重复扫到同一死号不重塞（SQLite INSERT
// OR IGNORE 即可）。返值 (inserted bool, err) · inserted=false 表示已存在。
func (w *Watcher) enqueueRefill(ctx context.Context, credentialID string, reason string) (bool, error) {
	if w.db == nil {
		return false, nil
	}

	// 反查上下文 · owner_bus_id / owner_record_passenger_id 互斥（credential_ledger CHECK）
	// 拿到 passenger_id 才能塞（passenger 是 pending_refill 的必填 FK）。
	var (
		busID        sql.NullString
		recordPass   sql.NullString
		pullRoundID  string
	)
	err := w.db.QueryRowContext(ctx, `
		SELECT COALESCE(owner_bus_id, '') AS bid,
		       COALESCE(owner_record_passenger_id, '') AS pid,
		       source_pull_round_id
		  FROM credential_ledger
		 WHERE id = ?
	`, credentialID).Scan(&busID, &recordPass, &pullRoundID)
	if err != nil {
		return false, fmt.Errorf("反查 credential_ledger: %w", err)
	}

	// 拿 passenger：优先 record group 的 owner · 有 bus 就去 bus 里挑发起人
	passengerID := recordPass.String
	if passengerID == "" && busID.Valid && busID.String != "" {
		err := w.db.QueryRowContext(ctx, `
			SELECT creator_passenger_id FROM bus WHERE id = ?`, busID.String).Scan(&passengerID)
		if err != nil {
			return false, fmt.Errorf("查 bus creator: %w", err)
		}
	}
	if passengerID == "" {
		// 数据不一致 —— credential 既没归属 bus 也没归属 passenger · 不该走到这
		return false, fmt.Errorf("credential %s 无归属 · 无法确定补给谁", credentialID)
	}

	// 从上一轮的 pull_round 拿 vendor_id / count（补一个还是补原批数 · 留 Step 2 决定）
	// Step 1 直接固定 count=1（保守 · 免得一次死一大批瞬间触发大量下单）
	var vendorID string
	if pullRoundID != "" {
		_ = w.db.QueryRowContext(ctx,
			`SELECT vendor_id FROM pull_round WHERE id = ?`, pullRoundID).Scan(&vendorID)
	}

	now := w.now().UTC().Format(time.RFC3339)
	res, err := w.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO pending_refill
		  (id, dead_credential_id, bus_id, passenger_id, count, vendor_id,
		   status, attempts, reason, created_at)
		VALUES (?, ?, ?, ?, 1, ?, 'pending', 0, ?, ?)`,
		uuid.NewString(), credentialID,
		nullableStr(busID.String), passengerID, nullableStr(vendorID),
		reason, now,
	)
	if err != nil {
		return false, fmt.Errorf("塞 pending_refill: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// RefillTick · 消费 pending_refill · **Step 1 只 log 不真 fire**（P6 拆两步的第一步）。
//
// 上层可以在 janitor / ticker 里定期调 · 未来 Step 2 会在这里真调 decider.Pull。
//
// 返回处理的条数 · err 是致命错误（DB 挂）· 单行处理失败只 log 不阻塞。
func (w *Watcher) RefillTick(ctx context.Context, limit int) (processed int, err error) {
	if w.db == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := w.db.QueryContext(ctx, `
		SELECT id, dead_credential_id, COALESCE(bus_id, ''), passenger_id,
		       count, COALESCE(vendor_id, ''), attempts
		  FROM pending_refill
		 WHERE status = 'pending'
		 ORDER BY created_at
		 LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type row struct {
		ID, CredID, BusID, PassengerID, VendorID string
		Count, Attempts                          int
	}
	var items []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.CredID, &r.BusID, &r.PassengerID,
			&r.Count, &r.VendorID, &r.Attempts); err != nil {
			return processed, err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return processed, err
	}

	for _, r := range items {
		// **Step 1**：只 log · 状态标 skipped 表示"看见了但没真拉"
		// Step 2 会改成：调 decider.Pull → fulfilled / expired · 失败重试 3 次进 expired
		w.log.Info("deathwatch: pending_refill 待补（Step 1 只 log · 1d 起真拉）",
			"refill_id", r.ID, "dead_cred", r.CredID, "bus", r.BusID,
			"passenger", r.PassengerID, "count", r.Count, "vendor", r.VendorID)

		_, uerr := w.db.ExecContext(ctx, `
			UPDATE pending_refill
			   SET status = 'skipped',
			       last_attempt_at = ?,
			       last_error = 'step1_log_only',
			       resolved_at = ?
			 WHERE id = ? AND status = 'pending'`,
			w.now().UTC().Format(time.RFC3339),
			w.now().UTC().Format(time.RFC3339), r.ID)
		if uerr != nil {
			w.log.Warn("pending_refill 标 skipped 失败", "id", r.ID, "err", uerr)
			continue
		}
		processed++
	}
	return processed, nil
}
