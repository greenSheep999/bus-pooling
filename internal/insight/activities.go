package insight

import (
	"context"
	"database/sql"
	"encoding/json"
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
//   - topup             = wallet_ledger.reason IN (recharge, redeem, warranty_refund) 的入账行
//   - extract/into_bus  = pull_round 完成/部分（每轮一条 · 挂车则为 into_bus）
//   - into_bus/push     = pending_assignment status='completed'（派车真实落库表 · 每号一条）
//   - handoff           = pending_handoff status='completed'（拿走 · 每次一条）
//   - dead              = credential_ledger.status='dead' 的号（每号一条）
//   - push（兜底）        = credential_ledger.pushed_to_passengerpool_at（非 assign 路径 / 老数据）
//   - refill 补车事件当前无关联行 · 待有需求再接
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
	// "我充了 N 就到账 N"，手续费是内部记账细节 · CLAUDE.md §0.1 / §1.4）。
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
		walletLink := "/wallet"
		a := Activity{
			ID: "l_" + id, CreatedAt: createdAt, Summary: memo,
			Amount: &amt,
			// 入账来源（通道商充值 / 兑换码）· 前端 chip 用它区分入账类目
			TargetKind: "topup_source",
			// 充值 / 兑换 / 质保退款 → 钱包页看流水
			Link: &walletLink,
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
			// 只给码 · 文案前端出（后端塞中文 = 英文用户看到中文）
			a.SummaryCode = defaultLedgerSummaryCode(reason, amount)
		}
		all = append(all, a)
	}
	rows.Close()

	// 2) 拉号轮次
	rows, err = s.db.QueryContext(ctx, `
		SELECT pr.id, pr.vendor_id, pr.bus_id, COALESCE(b.name, ''), pr.count_purchased,
		       pr.key_cost_total + pr.vendor_fee_total + pr.region_fee_total +
		       pr.single_pull_fee_total + pr.capability_fee_total + pr.service_fee_total AS total_cost,
		       pr.status, pr.created_at
		  FROM pull_round pr
		  LEFT JOIN bus b ON b.id = pr.bus_id
		 WHERE `+ownedRoundsWhere+`
		   AND pr.status IN ('completed','partial')
		 ORDER BY pr.created_at DESC
		 LIMIT ?`, passengerID, passengerID, fetch)
	if err != nil {
		return nil, 0, fmt.Errorf("insight: 活动流 pull_round: %w", err)
	}
	for rows.Next() {
		var id, vendorID, status, createdAt, busName string
		var busID sql.NullString
		var count int
		var totalCost int64
		if err := rows.Scan(&id, &vendorID, &busID, &busName, &count, &totalCost, &status, &createdAt); err != nil {
			rows.Close()
			return nil, 0, err
		}
		spend := -totalCost
		a := Activity{
			ID:        "r_" + id,
			Kind:      ActivityExtract,
			Source:    vendorID,
			Count: count,
			// Summary 不填 —— extract/into_bus 走 flow 分支 · 前端按 kind 组句(i18n)
			Amount:    &spend,
			CreatedAt: createdAt,
		}
		if busID.Valid && busID.String != "" {
			a.Kind = ActivityIntoBus
			a.TargetKind = "into_bus"
			// 车名优先 · 建车没命名时兜底裸 id（对外 UI 别显 UUID）
			if busName != "" {
				a.Target = busName
			} else {
				a.Target = busID.String
			}
			// 进车 → 车详情
			link := "/buses/" + busID.String
			a.Link = &link
		} else {
			// **Target 留空** —— 固定去向的文案由前端按 target_kind 出（i18n）·
			// 后端塞中文会让英文用户看到中文（§0.1:对外文案不许后端硬编码语言）
			a.TargetKind = "pending"
			// 号在提取页待派 → 提取页
			link := "/extract"
			a.Link = &link
		}
		all = append(all, a)
	}
	rows.Close()

	// 3) 号失效事件
	//
	// owner_bus_id 决定跳转 —— 挂车里的号死了 → 跳车详情（车主要看剩几个活的）·
	// 挂待派池的号死了 → 跳提取页（用户在待派池看到"废号"）
	rows, err = s.db.QueryContext(ctx, `
		SELECT cl.id, cl.vendor_id, cl.pulled_at, cl.dead_at, COALESCE(cl.owner_bus_id, '')
		  FROM credential_ledger cl
		 WHERE `+ownedCredentialWhere+`
		   AND cl.status = 'dead' AND cl.dead_at IS NOT NULL
		 ORDER BY cl.dead_at DESC
		 LIMIT ?`, passengerID, passengerID, fetch)
	if err != nil {
		return nil, 0, fmt.Errorf("insight: 活动流 credential: %w", err)
	}
	for rows.Next() {
		var id, vendorID, pulledAt, deadAt, ownerBusID string
		if err := rows.Scan(&id, &vendorID, &pulledAt, &deadAt, &ownerBusID); err != nil {
			rows.Close()
			return nil, 0, err
		}
		masked := maskCredID(id)
		a := Activity{
			ID:   "d_" + id,
			Kind: ActivityDead,
			// Source=vendor · api 层按档匿名化（Summary 里的真名一并替换）
			Source:     vendorID,
			Target:     masked,
			TargetKind: "cred_dead",
			Count: 1,
			// Summary 不填 —— 前端按 kind=dead 组句(masked/vendor 是数据 · "失效"是文案)
			CreatedAt:  deadAt,
		}
		var link string
		if ownerBusID != "" {
			link = "/buses/" + ownerBusID
		} else {
			link = "/extract"
		}
		a.Link = &link
		all = append(all, a)
	}
	rows.Close()

	// 4) 派车事件（pending_assignment · 派车动作真实落库表）
	//
	// target 库值是 to-bus / to-passengerpool（历史命名）· 对外映射成
	// into_bus / push_pool（01_init.sql pending_assignment 注释 · §12.5）。
	// 只取 completed —— 派车链跑完才算数。JOIN bus 取车名（不显裸 UUID）。
	// assignedCreds 记下这里已覆盖的号 · 下面 push 段按号去重（本段优先）。
	assignedCreds := map[string]bool{}
	rows, err = s.db.QueryContext(ctx, `
		SELECT pa.id, pa.credential_id, pa.target, pa.target_bus_id,
		       COALESCE(b.name, ''), cl.vendor_id, pa.created_at
		  FROM pending_assignment pa
		  LEFT JOIN credential_ledger cl ON cl.id = pa.credential_id
		  LEFT JOIN bus b ON b.id = pa.target_bus_id
		 WHERE pa.passenger_id = ? AND pa.status = 'completed'
		 ORDER BY pa.created_at DESC
		 LIMIT ?`, passengerID, fetch)
	if err != nil {
		return nil, 0, fmt.Errorf("insight: 活动流 assign: %w", err)
	}
	for rows.Next() {
		var id, credID, target, createdAt string
		var busID, busName, vendorID sql.NullString
		if err := rows.Scan(&id, &credID, &target, &busID, &busName, &vendorID, &createdAt); err != nil {
			rows.Close()
			return nil, 0, err
		}
		assignedCreds[credID] = true
		a := Activity{
			ID:        "a_" + id,
			Source:    vendorID.String, // api 层按档匿名化
			Count:     1,
			CreatedAt: createdAt,
		}
		switch target {
		case "to-bus":
			a.Kind = ActivityIntoBus
			a.TargetKind = "into_bus"
			// 车名优先 · 没命名兜底裸 id（对外别显 UUID）
			if busName.String != "" {
				a.Target = busName.String
			} else {
				a.Target = busID.String
			}
			// Summary 只当前端兜底（前端优先按 kind + source/target 自己组句）·
			// 车名是数据不是文案 · 可以出
			a.Summary = fmt.Sprintf("%s → %s", vendorID.String, a.Target)
			// 进车 → 车详情
			if busID.String != "" {
				link := "/buses/" + busID.String
				a.Link = &link
			}
		case "to-passengerpool":
			a.Kind = ActivityPush
			// **Target 留空** · "我的号池"这种固定文案交前端 i18n（别后端硬编码中文）
			a.TargetKind = "push_pool"
			// 推池 → 号池配置页（用户去检查/配下游 URL）
			link := "/settings/downstream"
			a.Link = &link
		}
		all = append(all, a)
	}
	rows.Close()

	// 5) handoff 事件（pending_handoff · 拿走 = 号交给乘客后不再监控）
	//
	// 一次 handoff 可含多个号（credential_ids_json 数组）· Count 取数组长度。
	// 明文永不入本表（只存 id 列表）· 这里只做"拿走了 N 个号"的活动记录。
	rows, err = s.db.QueryContext(ctx, `
		SELECT ph.id, ph.credential_ids_json,
		       COALESCE(ph.completed_at, ph.created_at)
		  FROM pending_handoff ph
		 WHERE ph.passenger_id = ? AND ph.status = 'completed'
		 ORDER BY ph.created_at DESC
		 LIMIT ?`, passengerID, fetch)
	if err != nil {
		return nil, 0, fmt.Errorf("insight: 活动流 handoff: %w", err)
	}
	for rows.Next() {
		var id, credIDsJSON, createdAt string
		if err := rows.Scan(&id, &credIDsJSON, &createdAt); err != nil {
			rows.Close()
			return nil, 0, err
		}
		var credIDs []string
		_ = json.Unmarshal([]byte(credIDsJSON), &credIDs)
		count := len(credIDs)
		handoffLink := "/extract"
		a := Activity{
			ID:         "h_" + id,
			Kind: ActivityHandoff,
			// Target / CountUnit 留空 —— 固定文案和量词都交前端 i18n（§0.1）
			TargetKind: "handoff",
			Count:      count,
			CreatedAt:  createdAt,
			// 拿走 = 号从待派池离开 · 历史落在提取页
			Link: &handoffLink,
		}
		all = append(all, a)
	}
	rows.Close()

	// 6) 号推送成功事件（pushed_to_passengerpool_at 非空 = 推成功）
	//
	// 派车链走的推池已在段 4 记录 · 这里按号去重（assignedCreds），避免同一次
	// 推池出现两条。留这段是兜底 —— 老数据 / 非 assign 路径推的号还得靠它。
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
		if assignedCreds[id] {
			continue // 段 4 已记过这次推池
		}
		pushLink := "/settings/downstream"
		a := Activity{
			ID:         "p_" + id,
			Kind:       ActivityPush,
			Source:     vendorID,
			TargetKind: "push_pool",
			Count:      1,
			CreatedAt:  pushedAt,
			Link:       &pushLink,
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

// defaultLedgerSummaryCode · memo 为空时的兜底**机器码**（不是文案）
//
// 返码不返中文 —— 后端不知道调用者的语言 · 塞中文会让英文用户看到中文（§0.1）。
// 前端按码出 i18n（common:activity.ledger.*）· 认不出的码回落通用"入账/出账"。
//
// 注意:memo 非空时用 memo 原文（那是**运营写的具体说明** · 是数据不是模板文案）。
func defaultLedgerSummaryCode(reason string, amount int64) string {
	switch reason {
	case "recharge":
		return "recharge"
	case "redeem":
		return "redeem"
	case "warranty_refund":
		return "warranty_refund"
	}
	if amount > 0 {
		return "credit_in"
	}
	return "credit_out"
}
