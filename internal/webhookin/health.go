package webhookin

// webhook 静默哨兵。
//
// **为什么要有**（2026-08-13 生产实测）：三家 vendor 的 webhook 先后停推 ——
// 最久的静默了 3 天 · 期间我方毫无察觉。查出来靠的是人工翻 Caddy 日志。
//
// 静默不会自己暴露：
//   - vendor 侧看我方永远是 200（我方对解析失败也返 200 · 免得 vendor 关订阅）
//   - 我方侧"没收到"跟"上游没开号"长得一模一样
//   - /status 页照常有数据 —— 探针 stock-delta 兜底了 · 图上看不出缺口
//
// 代价是抢号：webhook 是唯一能在 balance 态 fire 的信号源（`stockwatch/store.go`
// 的源-模式矩阵）· 它一静默 · 那家的挂单在均衡态下**永远抢不到**。
//
// 判据：拿**独立信源**（我方探针 stock-delta / 聚合站）当"上游确实在开号"的证据 ·
// 跟 `inbound_webhook_event` 的最后到达时刻比。有证据 + webhook 静默 = 报警。

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// minEvidenceBatch · 增量至少这么大才当"可能是一批新号"的绝对下限。
//
// 3 这个数是拿实测噪音定的：库存回升噪音（退款回流 / 上游挪货）实测集中在 1-4 个 ·
// 而真批次实测 9-20 个。3 只是兜底 —— 有自报历史的家会用它自己的批量折半，门槛更高。
const minEvidenceBatch = 3

// HealthChecker 定期比对"独立信源看到的开号" vs "webhook 收到的开号"。
type HealthChecker struct {
	db     *sql.DB
	logger *slog.Logger

	interval time.Duration // 检查间隔
	window   time.Duration // 回看窗口
	minBatch int           // 窗口内至少这么多批才算"上游确实在开号"

	cancel context.CancelFunc
	done   chan struct{}
}

type HealthConfig struct {
	DB       *sql.DB
	Logger   *slog.Logger
	Interval time.Duration
	Window   time.Duration
	MinBatch int
}

func NewHealthChecker(cfg HealthConfig) *HealthChecker {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	window := cfg.Window
	if window <= 0 {
		// 6 小时：比最活跃那家的开号间隔长得多 · 又短到能当天发现
		window = 6 * time.Hour
	}
	minBatch := cfg.MinBatch
	if minBatch <= 0 {
		// 2 批起报 —— 1 批可能是退款回流造成的库存回升（不是真开新批 · 上游本就不推）
		minBatch = 2
	}
	return &HealthChecker{
		db: cfg.DB, logger: logger,
		interval: interval, window: window, minBatch: minBatch,
	}
}

func (h *HealthChecker) Start(ctx context.Context) {
	if h.db == nil || h.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	h.done = make(chan struct{})
	h.logger.Info("webhookin.HealthChecker 启动",
		"interval", h.interval, "window", h.window, "min_batch", h.minBatch)

	go func() {
		defer close(h.done)
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				h.RunOnce(runCtx)
			}
		}
	}()
}

func (h *HealthChecker) Stop(timeout time.Duration) {
	if h.cancel == nil {
		return
	}
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(timeout):
		h.logger.Warn("webhookin.HealthChecker: Stop 超时")
	}
}

// SilentVendor 一家 webhook 静默的 vendor。
type SilentVendor struct {
	VendorID string
	// Batches 窗口内独立信源看到的开号批次数
	Batches int
	// Keys 这些批次的号总数
	Keys int
	// LastWebhookAt 最后一次收到 webhook 的时刻 · 零值 = 从来没收到过
	LastWebhookAt time.Time
}

// RunOnce 跑一次检查 · 返回静默名单（也 log）。导出是为了给 CLI / 测试直接调。
func (h *HealthChecker) RunOnce(ctx context.Context) []SilentVendor {
	silent, err := h.Check(ctx)
	if err != nil {
		h.logger.Warn("webhookin.HealthChecker: 检查失败", "err", err)
		return nil
	}
	for _, s := range silent {
		last := "从未收到"
		if !s.LastWebhookAt.IsZero() {
			last = s.LastWebhookAt.UTC().Format(time.RFC3339)
		}
		h.logger.Error("上游在开号但 webhook 静默 · 抢号链在均衡态收不到这家的信号",
			"vendor", s.VendorID,
			"batches_seen", s.Batches,
			"keys_seen", s.Keys,
			"last_webhook_at", last,
			"window", h.window.String(),
			"how_to", "查 vendor 后台的通知地址是否还在 · 再看 Caddy 访问日志确认请求有没有到")
	}
	return silent
}

// Check 比对独立信源与 webhook 到达 · 返回静默名单（按批次数降序）。
//
// **独立信源**只认两类 dispatch 行（都不可能由 webhook 产生 · 否则是循环论证）：
//   - `delta-%` · 我方探针 stock-delta 推算
//   - `source='xi8'` · 聚合站
//
// **只认够大的批**（2026-08-13 实测教训）：库存回升不等于开新批 —— 质保退款回流、
// 上游内部挪货都会让库存涨几个 · 上游对这些本来就不推 webhook。拿"任意正增量"当
// 证据会天天误报（实测某家真批次恒 10-12 个 · 而噪音增量全是 1-4 个）。
//
// 门槛按**该家自己的历史批量**定：取 vendor 自报批次里最常见的那个规模 · 折半当下限 ·
// 再跟绝对下限 3 取大。没有自报历史的家只用绝对下限。
//
// webhook 到达时刻取 `inbound_webhook_event` 里**非 rejected** 行的最大值 ——
// 收到任何一条能进分派的事件都说明通道是通的；`rejected` 是"推来了但没接住" ·
// 算进去的话全量丢弃的那家反而永远报不出来。
func (h *HealthChecker) Check(ctx context.Context) ([]SilentVendor, error) {
	if h.db == nil {
		return nil, nil
	}
	cutoff := time.Now().UTC().Add(-h.window).Format(time.RFC3339)

	rows, err := h.db.QueryContext(ctx, `
		WITH confirmed AS (
		    -- vendor 自报的批次规模分布（webhook / fleet 落的行 · 不含探针推算）
		    SELECT vendor_id, count AS sz, COUNT(*) AS freq
		      FROM vendor_dispatch
		     WHERE source = 'vendor_self' AND dispatch_key NOT LIKE 'delta-%'
		       AND count > 0
		     GROUP BY vendor_id, count
		),
		typical AS (
		    -- 每家最常见的批量 · 并列时取最大（宁可门槛高 · 少报胜过误报）
		    SELECT vendor_id, MAX(sz) AS sz
		      FROM confirmed c
		     WHERE freq = (SELECT MAX(freq) FROM confirmed c2 WHERE c2.vendor_id = c.vendor_id)
		     GROUP BY vendor_id
		)
		SELECT d.vendor_id,
		       COUNT(*)                       AS batches,
		       COALESCE(SUM(d.count), 0)      AS keys,
		       -- **排除 rejected 行**：那是"推来了但我方没接住" · 不能算通道活着 ·
		       -- 否则恰恰是全量丢弃的那家永远报不出来（本末倒置）
		       COALESCE((SELECT MAX(w.received_at)
		                   FROM inbound_webhook_event w
		                  WHERE w.vendor_id = d.vendor_id
		                    AND w.event_type <> 'rejected'), '') AS last_webhook
		  FROM vendor_dispatch d
		  LEFT JOIN typical t ON t.vendor_id = d.vendor_id
		 WHERE d.dispatched_at >= ?
		   AND (d.dispatch_key LIKE 'delta-%' OR d.source = 'xi8')
		   AND d.count >= MAX(?, COALESCE(t.sz, 0) / 2)
		 GROUP BY d.vendor_id
		HAVING batches >= ?
		 ORDER BY batches DESC
	`, cutoff, minEvidenceBatch, h.minBatch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SilentVendor
	for rows.Next() {
		var (
			s           SilentVendor
			lastWebhook string
		)
		if err := rows.Scan(&s.VendorID, &s.Batches, &s.Keys, &lastWebhook); err != nil {
			return nil, err
		}
		if lastWebhook != "" {
			t, perr := time.Parse(time.RFC3339, lastWebhook)
			if perr != nil {
				// 存进去的就该是 RFC3339 · 解析不了当"没收到过"处理（宁可多报一次）
				h.logger.Warn("webhookin.HealthChecker: received_at 格式异常",
					"vendor", s.VendorID, "value", lastWebhook)
			} else {
				s.LastWebhookAt = t.UTC()
				// 窗口内收到过 webhook = 通道活着 · 不报
				if t.UTC().After(time.Now().UTC().Add(-h.window)) {
					continue
				}
			}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
