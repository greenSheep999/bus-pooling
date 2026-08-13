package vendorview

// vendor_daily 日聚合。
//
// **为什么补这个**（2026-08-14 生产实测）：migration 021 建了 vendor_daily ·
// `ProbeStore.Incidents7d` 也在读它 · 但**全代码库没有任何写入方** —— 设计里
// 写的"每天 UTC 00:00 跑 aggregator"（decisions §11 / 021 注释）从没落地。
// 后果：/status 页的"过去 7 天事故"永远空 · 不是没事故 · 是根本没人算。
//
// 数据来源只有 vendor_probe（分钟级明细）· 不打任何上游端点 · 纯内部滚动。
// vendor_probe 30 天滚删 · vendor_daily 永久保留 —— 所以日聚合要在明细被清前算完。

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"time"
)

// incidentUptimeFloor · incident_flag 判据：当天 uptime < 95% 记为事故日。
//
// **只看 uptime · 不看 stockout**（2026-08-14 生产实测修正 021 的原判据）：
// 021 schema 注释写"uptime<95% 或 stockout>60min"· 但这个市场号一上架几分钟就被
// 抢光 · 缺货是**常态**不是事故 —— 实测每家每天 stockout 都 ~1400min · 按老判据
// 天天全红 · /status 的"7 天事故"变成纯噪音（CLAUDE.md §12.5 状态没收敛反模式）。
// stockout_minutes 仍照常记录（是有用的趋势数据）· 只是不再据它判事故。
const incidentUptimeFloor = 0.95

// RollupDay 把某个 UTC 日期的 vendor_probe 明细聚合进 vendor_daily（幂等 upsert）。
//
// date 格式 "2006-01-02"（UTC 边界）· 用 substr(probed_at,1,10) 匹配 · 对
// RFC3339 / RFC3339Nano 两种存法都成立（都以 YYYY-MM-DD 开头）。
// 返回写入的 vendor 行数。
func (s *ProbeStore) RollupDay(ctx context.Context, date string) (int, error) {
	if s.db == nil {
		return 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT vendor_id,
		       COUNT(*)                                                        AS total,
		       COALESCE(SUM(alive), 0)                                         AS alive_cnt,
		       COALESCE(SUM(CASE WHEN alive = 1
		                          AND (stock_total IS NULL OR stock_total <= 0)
		                         THEN 1 ELSE 0 END), 0)                        AS stockout_cnt,
		       AVG(CASE WHEN alive = 1 THEN stock_total END)                   AS stock_avg,
		       MIN(CASE WHEN alive = 1 THEN stock_total END)                   AS stock_min,
		       MIN(probed_at)                                                  AS first_at,
		       MAX(probed_at)                                                  AS last_at,
		       MAX(warranty_minutes)                                           AS warranty
		  FROM vendor_probe
		 WHERE substr(probed_at, 1, 10) = ?
		 GROUP BY vendor_id
	`, date)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type agg struct {
		vendorID        string
		total, aliveCnt int
		stockoutCnt     int
		stockAvg        sql.NullFloat64
		stockMin        sql.NullInt64
		firstAt, lastAt string
		warranty        sql.NullInt64
	}
	var aggs []agg
	for rows.Next() {
		var a agg
		if err := rows.Scan(&a.vendorID, &a.total, &a.aliveCnt, &a.stockoutCnt,
			&a.stockAvg, &a.stockMin, &a.firstAt, &a.lastAt, &a.warranty); err != nil {
			return 0, err
		}
		aggs = append(aggs, a)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(aggs) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vendor_daily
		  (vendor_id, date, uptime_pct, stock_avg, stock_min,
		   stockout_minutes, warranty_minutes, incident_flag)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vendor_id, date) DO UPDATE SET
		  uptime_pct       = excluded.uptime_pct,
		  stock_avg        = excluded.stock_avg,
		  stock_min        = excluded.stock_min,
		  stockout_minutes = excluded.stockout_minutes,
		  warranty_minutes = excluded.warranty_minutes,
		  incident_flag    = excluded.incident_flag
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	n := 0
	for _, a := range aggs {
		if a.total == 0 {
			continue
		}
		uptime := float64(a.aliveCnt) / float64(a.total)

		// stockout 分钟数 · 用样本占比 × 当天实际覆盖时长 —— **频次无关**。
		// 探针是自适应频次（baseline 60s / hot 10s）· 拿"样本数 × 固定间隔"会
		// 在 hot 期严重高估 · 所以按占比 × 覆盖跨度算。
		stockoutMin := 0
		if a.stockoutCnt > 0 {
			span := spanMinutes(a.firstAt, a.lastAt)
			if span > 0 {
				stockoutMin = int(math.Round(span * float64(a.stockoutCnt) / float64(a.total)))
			}
		}

		incident := 0
		if uptime < incidentUptimeFloor {
			incident = 1
		}

		var stockAvg interface{}
		if a.stockAvg.Valid {
			stockAvg = a.stockAvg.Float64
		}
		var stockMin interface{}
		if a.stockMin.Valid {
			stockMin = a.stockMin.Int64
		}
		var warranty interface{}
		if a.warranty.Valid && a.warranty.Int64 > 0 {
			warranty = a.warranty.Int64
		}

		if _, err := stmt.ExecContext(ctx,
			a.vendorID, date, uptime, stockAvg, stockMin,
			stockoutMin, warranty, incident,
		); err != nil {
			return 0, err
		}
		n++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// DistinctProbeDates 返回 vendor_probe 里出现过的 UTC 日期（近 sinceDays 天内）· 升序。
// 用于启动时回补历史日聚合（明细在 · 日行还没算的那些天）。
func (s *ProbeStore) DistinctProbeDates(ctx context.Context, sinceDays int) ([]string, error) {
	if s.db == nil {
		return nil, nil
	}
	if sinceDays <= 0 {
		sinceDays = 30
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -sinceDays).Format("2006-01-02")
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT substr(probed_at, 1, 10) AS d
		  FROM vendor_probe
		 WHERE substr(probed_at, 1, 10) >= ?
		 ORDER BY d ASC
	`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// spanMinutes 解析首尾探测时刻 · 返回跨度分钟数（解析失败返 0）。
func spanMinutes(firstAt, lastAt string) float64 {
	f, err1 := parseProbeTime(firstAt)
	l, err2 := parseProbeTime(lastAt)
	if err1 != nil || err2 != nil {
		return 0
	}
	d := l.Sub(f).Minutes()
	if d < 0 {
		return 0
	}
	return d
}

func parseProbeTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// ── 后台滚动任务 ──

// DailyRollupper 定时把 vendor_probe 聚合进 vendor_daily。
//
// 启动即回补 vendor_probe 里所有存量日期（明细在但日行缺的那些天）· 之后每小时
// 重滚"今天 + 昨天"—— 今天在累积 · 昨天在 UTC 跨零点后做最终定稿。
type DailyRollupper struct {
	store    *ProbeStore
	logger   *slog.Logger
	interval time.Duration

	cancel context.CancelFunc
	done   chan struct{}
}

func NewDailyRollupper(store *ProbeStore, interval time.Duration, logger *slog.Logger) *DailyRollupper {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = time.Hour
	}
	return &DailyRollupper{store: store, logger: logger, interval: interval}
}

func (r *DailyRollupper) Start(ctx context.Context) {
	if r.store == nil || r.store.db == nil || r.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.done = make(chan struct{})

	go func() {
		defer close(r.done)

		// 启动回补：把存量明细里每一天都滚一遍（幂等 · 重启不重复计）
		if dates, err := r.store.DistinctProbeDates(runCtx, 30); err == nil {
			total := 0
			for _, d := range dates {
				n, err := r.store.RollupDay(runCtx, d)
				if err != nil {
					r.logger.Warn("vendorview.DailyRollupper: 回补失败", "date", d, "err", err)
					continue
				}
				total += n
			}
			r.logger.Info("vendorview.DailyRollupper: 启动回补完成",
				"days", len(dates), "rows", total)
		} else {
			r.logger.Warn("vendorview.DailyRollupper: 列存量日期失败", "err", err)
		}

		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				r.rollupRecent(runCtx)
			}
		}
	}()
}

func (r *DailyRollupper) Stop(timeout time.Duration) {
	if r.cancel == nil {
		return
	}
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(timeout):
		r.logger.Warn("vendorview.DailyRollupper: Stop 超时")
	}
}

// rollupRecent 滚今天 + 昨天（昨天为了 UTC 跨零点后定稿）。
func (r *DailyRollupper) rollupRecent(ctx context.Context) {
	now := time.Now().UTC()
	for _, d := range []string{
		now.AddDate(0, 0, -1).Format("2006-01-02"),
		now.Format("2006-01-02"),
	} {
		if _, err := r.store.RollupDay(ctx, d); err != nil {
			r.logger.Warn("vendorview.DailyRollupper: 滚动失败", "date", d, "err", err)
		}
	}
}
