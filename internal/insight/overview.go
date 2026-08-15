package insight

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Overview 拼首页大响应。
//
// 分三段读（KPI / 车 / 提取），单个 passenger 的数据量小，串行就够；
// 出现慢查询再看是不是要并发。
func (s *Store) Overview(ctx context.Context, passengerID string) (*Overview, error) {
	kpi, err := s.overviewKPI(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	buses, err := s.overviewBuses(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	extract, err := s.overviewExtract(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	return &Overview{KPI: kpi, Buses: buses, Extract: extract}, nil
}

func (s *Store) overviewKPI(ctx context.Context, passengerID string) (KPI, error) {
	var k KPI

	// 余额
	var balance sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT balance FROM wallet WHERE passenger_id = ?`, passengerID).Scan(&balance)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return k, fmt.Errorf("insight: 读余额: %w", err)
	}
	k.Balance = balance.Int64

	// 累计充值 / 累计花费（不带时间窗，是"永久"字段 · 前端展示"你一共充了多少 / 花了多少"）
	//   充值展开 = recharge + channel_fee（内部 CLAUDE.md §1.4，channel_fee 是负号，抵掉 recharge 一部分）
	//   花费展开 = 分项六层，都是负号（wallet_ledger.amount 里）
	err = s.db.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(CASE WHEN reason IN ('recharge','channel_fee','redeem') THEN amount ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN reason IN ('key_cost','vendor_fee','region_fee',
		                                    'single_pull_fee','capability_fee','service_fee')
		                    THEN -amount ELSE 0 END), 0)
		  FROM wallet_ledger
		 WHERE passenger_id = ?`, passengerID).
		Scan(&k.BalanceDeltaTopup, &k.BalanceDeltaSpend)
	if err != nil {
		return k, fmt.Errorf("insight: 累计充值/花费: %w", err)
	}

	// 今日 / 昨日花费（跨所有拉号 · 累加分项六层的负号绝对值）
	now := s.now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
	if k.SpendToday, err = s.spendOnDate(ctx, passengerID, today); err != nil {
		return k, err
	}
	if k.SpendYesterday, err = s.spendOnDate(ctx, passengerID, yesterday); err != nil {
		return k, err
	}

	// 拉号轮次数（总数 / 本月）· 本月 = 当月 1 号 00:00 UTC 起
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format(timeLayout)
	if err := s.db.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(1), 0),
		  COALESCE(SUM(CASE WHEN pr.created_at >= ? THEN 1 ELSE 0 END), 0)
		  FROM pull_round pr
		  WHERE `+ownedRoundsWhere+``, monthStart, passengerID, passengerID).
		Scan(&k.PullTotal, &k.PullThisMonth); err != nil {
		return k, fmt.Errorf("insight: 拉号轮次: %w", err)
	}

	// 号池计数（活 / 死）· 只统计**当前属于本乘客**的号
	//   属于我 = 我参与的 bus 里的号 OR 我的 record group 号
	if err := s.db.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(CASE WHEN status = 'alive' THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE WHEN status = 'dead'  THEN 1 ELSE 0 END), 0)
		  FROM credential_ledger cl
		  WHERE `+ownedCredentialWhere+``, passengerID, passengerID).
		Scan(&k.AliveCount, &k.DeadCount); err != nil {
		return k, fmt.Errorf("insight: 号池计数: %w", err)
	}

	// 待补车（pull_intent.status IN ('pending','in_flight') 且是本乘客发起的）
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM pull_intent
		 WHERE passenger_id = ? AND status IN ('pending','in_flight')`,
		passengerID).Scan(&k.PendingRefill); err != nil {
		return k, fmt.Errorf("insight: 待补: %w", err)
	}

	// 平均寿命（死号的 pulled_at → dead_at 秒差 · 只算本乘客的死号）
	var avgSec sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT AVG((julianday(dead_at) - julianday(pulled_at)) * 86400.0)
		  FROM credential_ledger cl
		 WHERE `+ownedCredentialWhere+` AND status = 'dead' AND dead_at IS NOT NULL`,
		passengerID, passengerID).Scan(&avgSec)
	if err != nil {
		return k, fmt.Errorf("insight: 平均寿命: %w", err)
	}
	if avgSec.Valid {
		k.AvgLifespanSeconds = int64(avgSec.Float64)
	}

	return k, nil
}

// spendOnDate 按 UTC 日期算某天的支出（返回正数）。
func (s *Store) spendOnDate(ctx context.Context, passengerID, date string) (int64, error) {
	var v sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(-amount), 0) FROM wallet_ledger
		 WHERE passenger_id = ?
		   AND reason IN ('key_cost','vendor_fee','region_fee',
		                  'single_pull_fee','capability_fee','service_fee')
		   AND substr(created_at, 1, 10) = ?`, passengerID, date).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("insight: 日花费 %s: %w", date, err)
	}
	return v.Int64, nil
}

// overviewBuses 拉车汇总。
func (s *Store) overviewBuses(ctx context.Context, passengerID string) (BusesSummary, error) {
	var out BusesSummary

	// 车列表 · 只要活跃的
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.name, b.creator_passenger_id
		  FROM bus b
		  JOIN bus_member m ON m.bus_id = b.id
		 WHERE m.passenger_id = ? AND m.left_at IS NULL
		   AND b.status = 'active'
		 ORDER BY b.created_at DESC`, passengerID)
	if err != nil {
		return out, fmt.Errorf("insight: 列车: %w", err)
	}
	defer rows.Close()
	type busRef struct {
		id, name    string
		creatorID   string
		alive, dead int
		spend       int64
	}
	var buses []busRef
	for rows.Next() {
		var b busRef
		if err := rows.Scan(&b.id, &b.name, &b.creatorID); err != nil {
			return out, err
		}
		buses = append(buses, b)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	// 每辆车的号池数字 + 今日花费
	today := s.now().Format("2006-01-02")
	for i := range buses {
		if err := s.db.QueryRowContext(ctx, `
			SELECT
			  COALESCE(SUM(CASE WHEN status = 'alive' THEN 1 ELSE 0 END), 0),
			  COALESCE(SUM(CASE WHEN status = 'dead'  THEN 1 ELSE 0 END), 0)
			  FROM credential_ledger
			 WHERE owner_bus_id = ?`, buses[i].id).
			Scan(&buses[i].alive, &buses[i].dead); err != nil {
			return out, fmt.Errorf("insight: 车号统计: %w", err)
		}
		// 今日车级花费 · 本乘客在这辆车里今天花了多少
		//   participants_split_json 里的 count 决定本人的分摊，1a 单人车恒等于总数
		//   1a 简化：直接按 pull_round.key_cost + service_fee + single_pull_fee 累加
		if err := s.db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(
			  key_cost_total + vendor_fee_total + region_fee_total +
			  single_pull_fee_total + capability_fee_total + service_fee_total), 0)
			  FROM pull_round
			 WHERE bus_id = ? AND substr(created_at, 1, 10) = ?
			   AND status IN ('completed','partial')`, buses[i].id, today).
			Scan(&buses[i].spend); err != nil {
			return out, fmt.Errorf("insight: 车今日花: %w", err)
		}
	}

	// 汇总
	out.BusCount = len(buses)
	items := make([]BusSummaryRow, 0, len(buses))
	for _, b := range buses {
		role := "member"
		if b.creatorID == passengerID {
			role = "owner"
		}
		items = append(items, BusSummaryRow{
			ID: b.id, Name: b.name, Role: role,
			Alive: b.alive, Dead: b.dead, Spend: b.spend,
		})
		out.TotalCredentials += b.alive + b.dead
	}
	out.Items = items

	// refill_count：今日自动补车触发次数 · 简化实现 =
	//   我方发起的 pull_intent 里 constraints_json 含 refill 标签的今日行数
	// 1a 补车触发链尚未接通（Iss #12 才做）· 先给 0，字段不能少
	out.RefillCount = 0
	// coalesce_rate：拼车合流率 · 1a 全 single bus，恒 0；等 1c anon 撮合再算
	out.CoalesceRate = 0

	return out, nil
}

// overviewExtract 号池的当前分布 + 今日提取动作数 + 今日花费。
//
// destination 映射（对外术语 · CLAUDE.md §12.5）：
//   - pending    = 号在 record group（owner_record_passenger_id 非空 · disabled=1）
//   - into_bus   = 号在某辆车里（owner_bus_id 非空）
//   - push_pool  = 号已推给乘客号池（pushed_to_passengerpool_at 非空）
//
// **handoff 不算去向** —— 号已 DELETE，不在池里（fixtures.ts 有明确注释）。
// 台账行 status='handed_off' 只做售后追溯，不出这个统计。
func (s *Store) overviewExtract(ctx context.Context, passengerID string) (ExtractSummary, error) {
	var out ExtractSummary

	// 今日拉号动作数（提取事件数量的粗指标）· 只算完成 / 部分
	today := s.now().Format("2006-01-02")
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM pull_round pr
		 WHERE `+ownedRoundsWhere+`
		   AND substr(pr.created_at, 1, 10) = ?
		   AND pr.status IN ('completed','partial')`,
		passengerID, passengerID, today).Scan(&out.CountToday); err != nil {
		return out, fmt.Errorf("insight: 今日拉号: %w", err)
	}

	// 号池分布(4 桶)· 用户视角:拉出来的号最终去哪儿了?
	// 待派(pending)  = 我的 record group 里 alive 且未推
	// 进车(into_bus) = 我参与的 bus 里 alive 且未推
	// 推池(push_pool)= 已推(pushed_to_passengerpool_at 非空)
	// 拿走(handoff)  = 已 handoff · status='handed_off'(号已 DELETE 但台账留着做售后追溯)
	// **handoff 也算一种去向** —— 用户视角"这号我下载拿走了"是 3 种去向之一(见 fixtures 里对齐)
	var pending, intoBus, pushPool, handoff int
	err := s.db.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(CASE
		    WHEN status = 'alive' AND pushed_to_passengerpool_at IS NULL
		         AND owner_record_passenger_id IS NOT NULL
		    THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE
		    WHEN status = 'alive' AND pushed_to_passengerpool_at IS NULL
		         AND owner_bus_id IS NOT NULL
		    THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE
		    WHEN status = 'alive' AND pushed_to_passengerpool_at IS NOT NULL
		    THEN 1 ELSE 0 END), 0),
		  COALESCE(SUM(CASE
		    WHEN status = 'handed_off'
		    THEN 1 ELSE 0 END), 0)
		  FROM credential_ledger cl
		 WHERE `+ownedCredentialWhere+``, passengerID, passengerID).
		Scan(&pending, &intoBus, &pushPool, &handoff)
	if err != nil {
		return out, fmt.Errorf("insight: 号池分布: %w", err)
	}
	out.Pending = pending
	// TotalCredentials **= 池里还在的号数**(用户视角"我手上还有几个能用的")
	// **不含 handoff** · 已拿走的号在池里已 DELETE · 不算池里
	// handoff 单独统计走 by_destination 那里 · 展示"历史上拿走过多少"
	out.TotalCredentials = pending + intoBus + pushPool
	out.ByDestination = []DestinationRow{
		{Destination: "pending", Count: pending},
		{Destination: "into_bus", Count: intoBus},
		{Destination: "push_pool", Count: pushPool},
		{Destination: "handoff", Count: handoff},
	}

	// 今日提取花费（就是当日所有分项链的负号累加，跟 spend_today 语义一致；
	// UI 上"提取总花费"跟"今日花费"目前是一个东西）
	if out.Spend, err = s.spendOnDate(ctx, passengerID, today); err != nil {
		return out, err
	}
	return out, nil
}

// ownedRoundsWhere 复用的 SQL 片段：**pr 别名**的 pull_round 属于当前乘客。
// 归属 = 挂在我的 bus 上 OR （单独拉号）participants_split_json 里有我的 id。
//
// 用 json_extract 通配 key 麻烦，转成"文本包含 passenger_id"来判 —— UUID 碰撞概率
// 可忽略；且 pull_round 的 participants_split_json 结构固定就是 {passenger_id: n}。
//
// 参数顺序：passengerID, passengerID
const ownedRoundsWhere = `(
    pr.bus_id IN (SELECT bus_id FROM bus_member
                   WHERE passenger_id = ? AND left_at IS NULL)
    OR (pr.bus_id IS NULL
        AND pr.participants_split_json LIKE '%"' || ? || '"%')
)`

// ownedCredentialWhere 复用的 SQL 片段：**cl 别名**的 credential_ledger 归当前乘客。
// 参数顺序：passengerID, passengerID
const ownedCredentialWhere = `(
    cl.owner_record_passenger_id = ?
    OR cl.owner_bus_id IN (SELECT bus_id FROM bus_member
                            WHERE passenger_id = ? AND left_at IS NULL)
)`

// timeLayout ISO-8601 UTC · 跟 wallet / bus 一致，跨包保持相同格式
const timeLayout = "2006-01-02T15:04:05.000Z"
