package insight

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Trend 生成 days 长度的时序点，metric 决定 y 值语义。
//
// **补齐空点**：每一天都有一个点（0 也保留），前端不用自己填 gap；
// 图表连线时空缺 = 0，视觉上不会消失但也不会误导（比"当天没数据 = 没这个点"更清晰）。
func (s *Store) Trend(
	ctx context.Context, passengerID string,
	metric TrendMetric, days int, scope TrendScope,
) ([]TrendPoint, error) {
	if days < 1 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	if scope.BusID != "" && scope.VendorID != "" {
		return nil, errors.New("insight: bus_id 和 vendor 不能同时传")
	}

	// 计算窗口
	now := s.now()
	todayEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
	windowStart := todayEnd.AddDate(0, 0, -(days - 1)).Truncate(24 * time.Hour)

	// 先建 date → 0 的映射 · 保证每天都有点
	byDate := make(map[string]float64, days)
	order := make([]string, 0, days)
	for i := 0; i < days; i++ {
		d := windowStart.AddDate(0, 0, i).Format("2006-01-02")
		byDate[d] = 0
		order = append(order, d)
	}

	switch metric {
	case TrendCredits, "":
		if err := s.trendCredits(ctx, passengerID, scope, windowStart, byDate); err != nil {
			return nil, err
		}
	case TrendPulls:
		if err := s.trendPulls(ctx, passengerID, scope, windowStart, byDate); err != nil {
			return nil, err
		}
	case TrendLifespan:
		if err := s.trendLifespan(ctx, passengerID, scope, windowStart, byDate); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("insight: 不支持的 metric %q", metric)
	}

	out := make([]TrendPoint, 0, days)
	for _, d := range order {
		out = append(out, TrendPoint{Date: d, Value: byDate[d]})
	}
	return out, nil
}

// trendCredits 每日花费（正数）。scope.BusID 时限定该车；scope.VendorID 时限定该家。
//
// 花费定义：wallet_ledger 里加价链 6 层的负号绝对值 SUM。
// bus scope：join pull_round 找 ref_id in bus 的 rounds，再 sum 那些 rounds
//
//	的分项 —— 简化实现直接按 pull_round 表分项累加。
//
// vendor scope：按 pull_round.vendor_id 过滤同上。
func (s *Store) trendCredits(
	ctx context.Context, passengerID string, scope TrendScope,
	start time.Time, byDate map[string]float64,
) error {
	if scope.BusID != "" || scope.VendorID != "" {
		return s.trendCreditsFromRounds(ctx, passengerID, scope, start, byDate)
	}
	// 无 scope：直接 wallet_ledger 加价链 6 层
	rows, err := s.db.QueryContext(ctx, `
		SELECT substr(created_at, 1, 10) AS d, COALESCE(SUM(-amount), 0)
		  FROM wallet_ledger
		 WHERE passenger_id = ?
		   AND reason IN ('key_cost','vendor_fee','region_fee',
		                  'single_pull_fee','capability_fee','service_fee')
		   AND created_at >= ?
		 GROUP BY d`, passengerID, formatTime(start))
	if err != nil {
		return fmt.Errorf("insight: trend credits: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var v int64
		if err := rows.Scan(&d, &v); err != nil {
			return err
		}
		// microunit → 积分（float）· 前端展示"多少积分"，不用还 micro 值
		if _, ok := byDate[d]; ok {
			byDate[d] = float64(v) / 1_000_000.0
		}
	}
	return rows.Err()
}

// trendCreditsFromRounds bus / vendor scope 时按 pull_round 分项累加。
func (s *Store) trendCreditsFromRounds(
	ctx context.Context, passengerID string, scope TrendScope,
	start time.Time, byDate map[string]float64,
) error {
	q := `
		SELECT substr(pr.created_at, 1, 10) AS d, COALESCE(SUM(
		  pr.key_cost_total + pr.vendor_fee_total + pr.region_fee_total +
		  pr.single_pull_fee_total + pr.capability_fee_total + pr.service_fee_total), 0)
		  FROM pull_round pr
		 WHERE ` + ownedRoundsWhere + `
		   AND pr.created_at >= ?
		   AND pr.status IN ('completed','partial')`
	args := []any{passengerID, passengerID, formatTime(start)}
	if scope.BusID != "" {
		q += ` AND pr.bus_id = ?`
		args = append(args, scope.BusID)
	}
	if scope.VendorID != "" {
		q += ` AND pr.vendor_id = ?`
		args = append(args, scope.VendorID)
	}
	q += ` GROUP BY d`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("insight: trend credits (scoped): %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var v int64
		if err := rows.Scan(&d, &v); err != nil {
			return err
		}
		if _, ok := byDate[d]; ok {
			byDate[d] = float64(v) / 1_000_000.0
		}
	}
	return rows.Err()
}

// trendPulls 每日拉号轮次数（整数）。
func (s *Store) trendPulls(
	ctx context.Context, passengerID string, scope TrendScope,
	start time.Time, byDate map[string]float64,
) error {
	q := `
		SELECT substr(pr.created_at, 1, 10) AS d, COUNT(1)
		  FROM pull_round pr
		 WHERE ` + ownedRoundsWhere + `
		   AND pr.created_at >= ?`
	args := []any{passengerID, passengerID, formatTime(start)}
	if scope.BusID != "" {
		q += ` AND pr.bus_id = ?`
		args = append(args, scope.BusID)
	}
	if scope.VendorID != "" {
		q += ` AND pr.vendor_id = ?`
		args = append(args, scope.VendorID)
	}
	q += ` GROUP BY d`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("insight: trend pulls: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var v int
		if err := rows.Scan(&d, &v); err != nil {
			return err
		}
		if _, ok := byDate[d]; ok {
			byDate[d] = float64(v)
		}
	}
	return rows.Err()
}

// trendLifespan 每日"死号平均寿命"（小时数）。按死亡时间归日。
//
// 只有那天有号"死"了才有值，其他天为 0（跟 fixtures.ts 的语义一致）。
func (s *Store) trendLifespan(
	ctx context.Context, passengerID string, scope TrendScope,
	start time.Time, byDate map[string]float64,
) error {
	q := `
		SELECT substr(cl.dead_at, 1, 10) AS d,
		       AVG((julianday(cl.dead_at) - julianday(cl.pulled_at)) * 24.0)
		  FROM credential_ledger cl
		 WHERE ` + ownedCredentialWhere + `
		   AND cl.status = 'dead' AND cl.dead_at IS NOT NULL
		   AND cl.dead_at >= ?`
	args := []any{passengerID, passengerID, formatTime(start)}
	if scope.BusID != "" {
		q += ` AND cl.owner_bus_id = ?`
		args = append(args, scope.BusID)
	}
	if scope.VendorID != "" {
		q += ` AND cl.vendor_id = ?`
		args = append(args, scope.VendorID)
	}
	q += ` GROUP BY d`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("insight: trend lifespan: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var v sql.NullFloat64
		if err := rows.Scan(&d, &v); err != nil {
			return err
		}
		if _, ok := byDate[d]; ok && v.Valid {
			byDate[d] = v.Float64
		}
	}
	return rows.Err()
}

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }
