package vendorview

import (
	"context"
	"log/slog"
	"time"
)

// StalenessChecker 定时扫 pipeline_health · 把"某条管线太久没成功"从"藏在 WARN 日志里"
// 变成**主动大声 ERROR**（+ 可被日志告警抓到）。这是"系统自己盯"的那一步：
// serve 挂了、vendor 改形状、session token 过期 —— 都会让对应管线 last_ok_at 停住 ·
// 本 checker 到点就喊。
//
// 只读 · 不改数据 · 失败不影响任何采集链路。
type StalenessChecker struct {
	health   *HealthStore
	interval time.Duration
	logger   *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

func NewStalenessChecker(health *HealthStore, interval time.Duration, logger *slog.Logger) *StalenessChecker {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &StalenessChecker{health: health, interval: interval, logger: logger}
}

func (c *StalenessChecker) Start(ctx context.Context) {
	if c.health == nil {
		c.logger.Info("vendorview.StalenessChecker: health 为 nil · 不启动")
		return
	}
	if c.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.done = make(chan struct{})
	c.logger.Info("vendorview.StalenessChecker 启动", "interval", c.interval)

	go func() {
		defer close(c.done)
		// 启动给足一轮采集时间再首查（避免刚起来啥都还没盖戳就误报）
		first := time.NewTimer(c.interval)
		defer first.Stop()
		select {
		case <-runCtx.Done():
			return
		case <-first.C:
			c.check(runCtx)
		}
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				c.check(runCtx)
			}
		}
	}()
}

func (c *StalenessChecker) Stop(timeout time.Duration) {
	if c.cancel == nil {
		return
	}
	c.cancel()
	select {
	case <-c.done:
	case <-time.After(timeout):
		c.logger.Warn("vendorview.StalenessChecker: Stop 超时")
	}
}

func (c *StalenessChecker) check(ctx context.Context) {
	rows, err := c.health.Report(ctx)
	if err != nil {
		c.logger.Warn("StalenessChecker: 读 pipeline_health 失败", "err", err)
		return
	}
	now := time.Now().UTC()
	stale := 0
	for _, r := range rows {
		if !r.Stale(now) {
			continue
		}
		stale++
		age := "从未成功"
		if !r.LastOKAt.IsZero() {
			age = now.Sub(r.LastOKAt).Round(time.Second).String()
		}
		c.logger.Error("数据管线陈旧 · 该管线太久没成功了",
			"vendor", vendorOrGlobal(r.VendorID),
			"pipeline", r.Pipeline,
			"last_ok_ago", age,
			"threshold", MaxAge(r.Pipeline).String(),
			"last_err", r.LastErr,
		)
	}
	if stale == 0 {
		c.logger.Info("数据管线全部新鲜", "checked", len(rows))
	} else {
		c.logger.Error("数据管线新鲜度体检", "stale", stale, "total", len(rows))
	}
}

func vendorOrGlobal(vid string) string {
	if vid == "" {
		return "(global)"
	}
	return vid
}
