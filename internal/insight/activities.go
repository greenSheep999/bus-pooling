package insight

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// activityFetchCap 每源单次拉的上限。25 页 × 20 条 = 500 · 单个乘客的活动量
// 短期内远达不到这个量（旧号 8 小时就 die，活跃期活动量也就几十条/天）。到达
// 上限时前端的翻页会出现 total 少统计的情况 —— 阶段 1a 可接受，之后接了归档
// 再优化（新加 activity_index 视图或类似）。
const activityFetchCap = 500

// Activities 混流活动记录，按时间倒序 · 分页。
//
// 数据源（对外映射 · CLAUDE.md §12.5）：
//   - topup      = wallet_ledger.reason IN (recharge, redeem, warranty_refund) 的入账行
//   - extract    = pull_round 完成/部分（每轮一条）
//   - dead       = credential_ledger.status='dead' 的号（每号一条）
//   - refill/push/into_bus 阶段 1a 还没有真实数据源（补车 / 派去向 handler 之后接）
//
// 分页：从各源取 max(page*size + buffer) 条，合并后取窗口。
// 数据量小 · UUID v7 主键天然带时间序，SUBSTR 取前 10 分组本身就是索引友好。
func (s *Store) Activities(
	ctx context.Context, passengerID string, page, pageSize int,
) ([]Activity, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	// 各源都拉 activityFetchCap 条上限（够看 25 页 × 20 条 · 单乘客活动量级远低于此）·
	// 之后合并再截窗口 —— total 才准，否则每源被 offset+pageSize 截掉后合并的 total 是假的
	fetch := offset + pageSize
	if fetch < activityFetchCap {
		fetch = activityFetchCap
	}

	var all []Activity

	// 1) 钱包类活动（充值 / 兑换 / 质保退款）
	//
	// recharge 显示**净到账**（减掉同一次充值订单的 channel_fee，让乘客看到
	// "我充了 N 就到账 N"，通道费是内部记账细节 · CLAUDE.md §0.1 / §1.4）。
	// 兑换和质保退款没有 channel_fee 抵扣，amount 就是本身。
	rows, err := s.db.QueryContext(ctx, `
		SELECT wl.id, wl.reason,
		       wl.amount + COALESCE((
		         SELECT SUM(fee.amount) FROM wallet_ledger fee
		          WHERE fee.passenger_id = wl.passenger_id
		            AND fee.reason = 'channel_fee'
		            AND fee.ref_type = wl.ref_type
		            AND fee.ref_id = wl.ref_id
		       ), 0) AS net_amount,
		       COALESCE(wl.memo, ''), wl.created_at
		  FROM wallet_ledger wl
		 WHERE wl.passenger_id = ?
		   AND wl.reason IN ('recharge','redeem','warranty_refund')
		 ORDER BY wl.created_at DESC
		 LIMIT ?`, passengerID, fetch)
	if err != nil {
		return nil, 0, fmt.Errorf("insight: 活动流 ledger: %w", err)
	}
	for rows.Next() {
		var id, reason, memo, createdAt string
		var amount int64
		if err := rows.Scan(&id, &reason, &amount, &memo, &createdAt); err != nil {
			rows.Close()
			return nil, 0, err
		}
		amt := amount
		a := Activity{
			ID: "l_" + id, CreatedAt: createdAt, Summary: memo,
			Amount: &amt,
		}
		switch reason {
		case "recharge":
			a.Kind = ActivityTopup
		case "redeem":
			a.Kind = ActivityRedeem
		case "warranty_refund":
			// 质保退款没独立 kind，归到 topup 类（对外都是入账）· 保持前端 chip 简洁
			a.Kind = ActivityTopup
		}
		if memo == "" {
			a.Summary = defaultLedgerSummary(reason, amount)
		}
		all = append(all, a)
	}
	rows.Close()

	// 2) 拉号轮次
	rows, err = s.db.QueryContext(ctx, `
		SELECT pr.id, pr.vendor_id, pr.bus_id, pr.count_purchased,
		       pr.key_cost_total + pr.vendor_fee_total + pr.region_fee_total +
		       pr.single_pull_fee_total + pr.capability_fee_total + pr.service_fee_total AS total_cost,
		       pr.status, pr.created_at
		  FROM pull_round pr
		 WHERE `+ownedRoundsWhere+`
		   AND pr.status IN ('completed','partial')
		 ORDER BY pr.created_at DESC
		 LIMIT ?`, passengerID, passengerID, fetch)
	if err != nil {
		return nil, 0, fmt.Errorf("insight: 活动流 pull_round: %w", err)
	}
	for rows.Next() {
		var id, vendorID, status, createdAt string
		var busID sql.NullString
		var count int
		var totalCost int64
		if err := rows.Scan(&id, &vendorID, &busID, &count, &totalCost, &status, &createdAt); err != nil {
			rows.Close()
			return nil, 0, err
		}
		spend := -totalCost
		a := Activity{
			ID:        "r_" + id,
			Kind:      ActivityExtract,
			Source:    vendorID,
			Count:     count,
			CountUnit: "个 key",
			Summary:   fmt.Sprintf("%s · 拉 %d 个", vendorID, count),
			Amount:    &spend,
			CreatedAt: createdAt,
		}
		if busID.Valid && busID.String != "" {
			a.Kind = ActivityIntoBus
			a.TargetKind = "into_bus"
			a.Target = busID.String
		} else {
			a.TargetKind = "pending"
			a.Target = "待派"
		}
		all = append(all, a)
	}
	rows.Close()

	// 3) 号失效事件
	rows, err = s.db.QueryContext(ctx, `
		SELECT cl.id, cl.vendor_id, cl.pulled_at, cl.dead_at
		  FROM credential_ledger cl
		 WHERE `+ownedCredentialWhere+`
		   AND cl.status = 'dead' AND cl.dead_at IS NOT NULL
		 ORDER BY cl.dead_at DESC
		 LIMIT ?`, passengerID, passengerID, fetch)
	if err != nil {
		return nil, 0, fmt.Errorf("insight: 活动流 credential: %w", err)
	}
	for rows.Next() {
		var id, vendorID, pulledAt, deadAt string
		if err := rows.Scan(&id, &vendorID, &pulledAt, &deadAt); err != nil {
			rows.Close()
			return nil, 0, err
		}
		masked := maskCredID(id)
		a := Activity{
			ID:        "d_" + id,
			Kind:      ActivityDead,
			Summary:   fmt.Sprintf("%s · %s · 失效", masked, vendorID),
			CreatedAt: deadAt,
		}
		all = append(all, a)
	}
	rows.Close()

	// 4) 号推送成功事件（pushed_to_passengerpool_at 非空 = 推成功）
	rows, err = s.db.QueryContext(ctx, `
		SELECT cl.id, cl.vendor_id, cl.pushed_to_passengerpool_at
		  FROM credential_ledger cl
		 WHERE `+ownedCredentialWhere+`
		   AND cl.pushed_to_passengerpool_at IS NOT NULL
		 ORDER BY cl.pushed_to_passengerpool_at DESC
		 LIMIT ?`, passengerID, passengerID, fetch)
	if err != nil {
		return nil, 0, fmt.Errorf("insight: 活动流 push: %w", err)
	}
	for rows.Next() {
		var id, vendorID, pushedAt string
		if err := rows.Scan(&id, &vendorID, &pushedAt); err != nil {
			rows.Close()
			return nil, 0, err
		}
		a := Activity{
			ID:         "p_" + id,
			Kind:       ActivityPush,
			Source:     vendorID,
			Target:     "我的号池",
			TargetKind: "push_pool",
			Count:      1,
			CountUnit:  "个号",
			Summary:    fmt.Sprintf("%s → 我的号池", vendorID),
			CreatedAt:  pushedAt,
		}
		all = append(all, a)
	}
	rows.Close()

	// 合并按 created_at 倒序
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].CreatedAt > all[j].CreatedAt
	})

	total := len(all)
	// 分页切片
	if offset >= total {
		return []Activity{}, total, nil
	}
	end := offset + pageSize
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

// maskCredID 打码：cred_...4F2 · 只留末 3 位（跟 fixtures.ts 里的样式对齐）
func maskCredID(id string) string {
	if len(id) < 3 {
		return "cred_..."
	}
	return "cred_..." + id[len(id)-3:]
}

// defaultLedgerSummary 备用文案 —— 走 ledger.memo 为空时兜底。
func defaultLedgerSummary(reason string, amount int64) string {
	switch reason {
	case "recharge":
		return "充值到账"
	case "redeem":
		return "兑换码到账"
	case "warranty_refund":
		return "质保退款"
	}
	if amount > 0 {
		return "入账"
	}
	return "出账"
}
