package handoff

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// Janitor 定期扫两类卡单：
//   - 过期 token（token_issued / fulfilled 超时）→ expired / expired_after_fulfill
//   - **卡在 confirmed 但没走到 completed** → 重试 completeHandoff · 多次仍失败转 need_manual
//
// **不删 credential** —— 09-transactions §4：token 过期后号仍留在池里（disabled=true），
// 乘客可以重新发起 POST /me/handoff。confirmed 卡单是**必须处理**的·因为状态已经承诺
// 号会交给用户·pool DELETE 迟迟不做会导致运营看到 pool 里有"已交出但未删"的号。
type Janitor struct {
	store *Store
	// pool 用来重试 housepool.DeleteCredential · nil = 只标状态不做外部动作
	// （mock 模式 / DRY_RUN 下 pool 是 nil · janitor 依然运行但 confirmed sweep 走
	//  need_manual 兜底）
	pool housepool.HousePool
	// completeHandoffFn · 外注入的"重新做完整外部动作"函数·避免 handoff 包依赖 api 层
	// 一般是 api.completeHandoff 的适配版·nil = 不做（跟 pool=nil 效果一样）
	completeFn func(ctx context.Context, p Pending) error
	// interval 两次扫的间隔 · 0 = 15s（跟 decider janitor 一致）
	interval time.Duration
	// stuckAfter · confirmed 卡多久算 stuck · 0 = 60s（confirm 后 60s 还没 completed 就重试）
	stuckAfter time.Duration
	// maxRetries · 单个 confirmed 单最多重试几次 · 超过转 need_manual · 0 = 5
	maxRetries int
	batchLimit int
	log        *slog.Logger
}

type JanitorConfig struct {
	Store      *Store
	Pool       housepool.HousePool
	CompleteFn func(ctx context.Context, p Pending) error
	Interval   time.Duration
	StuckAfter time.Duration
	MaxRetries int
	BatchLimit int
	Logger     *slog.Logger
}

func NewJanitor(cfg JanitorConfig) *Janitor {
	j := &Janitor{
		store:      cfg.Store,
		pool:       cfg.Pool,
		completeFn: cfg.CompleteFn,
		interval:   cfg.Interval,
		stuckAfter: cfg.StuckAfter,
		maxRetries: cfg.MaxRetries,
		batchLimit: cfg.BatchLimit,
		log:        cfg.Logger,
	}
	if j.interval <= 0 {
		j.interval = 15 * time.Second
	}
	if j.stuckAfter <= 0 {
		j.stuckAfter = 60 * time.Second
	}
	if j.maxRetries <= 0 {
		// 3 次转人工·跟 docs/09-transactions.md:195 一致
		j.maxRetries = 3
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
	// StuckConfirmedRetried · 重试 completeHandoff 成功推到 completed 的数
	StuckConfirmedRetried int
	// StuckConfirmedManual · 重试次数超上限·转 need_manual 的数
	StuckConfirmedManual int
	Failed               int
}

// SweepOnce 扫一轮：
//  1. token_issued 过期 → expired
//  2. fulfilled 过期 → expired_after_fulfill
//  3. **卡在 confirmed 超过 stuckAfter** → 重试 completeHandoff · 多次仍败转 need_manual
func (j *Janitor) SweepOnce(ctx context.Context) SweepReport {
	var r SweepReport

	// 1) 2) 过期扫描
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

	// 3) 卡在 confirmed 的·重试 completeHandoff（外部注入的 completeFn）
	//    重试计数落库·服务重启不清零（P1-B 修复）
	//    3 次仍失败转 need_manual（docs/09-transactions.md:195）
	stuck, err := j.store.FindStuckConfirmed(ctx, j.stuckAfter, j.batchLimit)
	if err != nil {
		j.log.Error("handoff janitor 扫 stuck confirmed 失败", "err", err)
		return r
	}
	for _, p := range stuck {
		if ctx.Err() != nil {
			return r
		}
		attempts, err := j.store.IncrRetryCount(ctx, p.ID)
		if err != nil {
			r.Failed++
			j.log.Error("handoff janitor 累加 retry_count 失败", "id", p.ID, "err", err)
			continue
		}
		if attempts > j.maxRetries {
			// 超过重试上限 · 转 need_manual · 让运营查
			reason := fmt.Sprintf("confirmed → completed 卡住·重试 %d 次仍失败", attempts)
			if err := j.store.MarkNeedManual(ctx, p.ID, reason); err != nil {
				r.Failed++
				j.log.Error("handoff janitor 标 need_manual 失败", "id", p.ID, "err", err)
				continue
			}
			r.StuckConfirmedManual++
			j.log.Warn("handoff confirmed 转 need_manual", "id", p.ID, "attempts", attempts)
			continue
		}
		if j.completeFn == nil {
			// 没接 completeFn（DRY_RUN / mock）· 不重试外部动作 · 只 log
			j.log.Warn("handoff confirmed 卡单但 completeFn=nil · 只计数不重试",
				"id", p.ID, "attempts", attempts)
			continue
		}
		if err := j.completeFn(ctx, p); err != nil {
			j.log.Warn("handoff janitor 重试 completeHandoff 失败",
				"id", p.ID, "attempts", attempts, "err", err)
			continue
		}
		// 完成外部动作 · 推 completed
		if err := j.store.MarkCompleted(ctx, p.ID); err != nil {
			j.log.Warn("handoff janitor 推 completed 失败", "id", p.ID, "err", err)
			continue
		}
		r.StuckConfirmedRetried++
		j.log.Info("handoff confirmed 恢复成功", "id", p.ID, "attempts", attempts)
	}

	return r
}
