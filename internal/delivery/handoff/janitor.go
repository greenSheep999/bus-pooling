package handoff

import (
	"context"
	"log/slog"
	"time"
)

// Janitor 定期扫过期的 token_issued / fulfilled 行 · 转 expired / expired_after_fulfill。
//
// **不删 credential** —— 09-transactions §4：token 过期后号仍留在池里（disabled=true），
// 乘客可以重新发起 POST /me/handoff。janitor 只标状态，实际的 housepool 侧
// disabled 复原动作由 handler / 下一轮 IssueToken 自行处理（阶段 1a 简化，1c 补齐）。
type Janitor struct {
	store *Store
	// interval 两次扫的间隔 · 0 = 15s（跟 decider janitor 一致）
	interval   time.Duration
	batchLimit int
	log        *slog.Logger
}

type JanitorConfig struct {
	Store      *Store
	Interval   time.Duration
	BatchLimit int
	Logger     *slog.Logger
}

func NewJanitor(cfg JanitorConfig) *Janitor {
	j := &Janitor{
		store:      cfg.Store,
		interval:   cfg.Interval,
		batchLimit: cfg.BatchLimit,
		log:        cfg.Logger,
	}
	if j.interval <= 0 {
		j.interval = 15 * time.Second
	}
	if j.batchLimit <= 0 {
		j.batchLimit = 50
	}
	if j.log == nil {
		j.log = slog.Default()
	}
	return j
}

// Run 阻塞循环，ctx 结束即返回。
func (j *Janitor) Run(ctx context.Context) {
	t := time.NewTicker(j.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.SweepOnce(ctx)
		}
	}
}

// SweepReport 一轮扫描统计。
type SweepReport struct {
	ExpiredTokenIssued int
	ExpiredAfterFulfil int
	Failed             int
}

// SweepOnce 扫一轮：token_issued 过期 → expired；fulfilled 过期 → expired_after_fulfill。
func (j *Janitor) SweepOnce(ctx context.Context) SweepReport {
	var r SweepReport
	for _, entry := range []struct {
		from Status
		to   Status
		acc  *int
	}{
		{StatusTokenIssued, StatusExpired, &r.ExpiredTokenIssued},
		{StatusFulfilled, StatusExpiredAfterFulfill, &r.ExpiredAfterFulfil},
	} {
		rows, err := j.store.FindStale(ctx, entry.from, j.batchLimit)
		if err != nil {
			j.log.Error("handoff janitor 扫过期行失败", "from", entry.from, "err", err)
			continue
		}
		for _, p := range rows {
			if ctx.Err() != nil {
				return r
			}
			if err := j.store.MarkExpired(ctx, p.ID, entry.from); err != nil {
				r.Failed++
				j.log.Warn("handoff janitor 标 expired 失败", "id", p.ID, "err", err)
				continue
			}
			*entry.acc++
			j.log.Info("handoff token 过期", "id", p.ID, "from", entry.from, "to", entry.to)
		}
	}
	return r
}
