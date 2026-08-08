package decider

import (
	"context"
	"log/slog"
	"time"
)

// StateTimeouts 各状态卡住多久算超时（09-transactions §2 "关键约定 5"）。
// 零值 = 用默认。
type StateTimeouts struct {
	Initial    time.Duration // 默认 30s（本不该卡在这，卡了大概率是 bug）
	Reserved   time.Duration // 默认 60s（vendor 未响应）
	Purchasing time.Duration // 默认 30s（发出去响应没回）
	Purchased  time.Duration // 默认 5min（号池未响应）
	Imported   time.Duration // 默认 30s（结账未完成）
}

func (t StateTimeouts) or(x, def time.Duration) time.Duration {
	if x <= 0 {
		return def
	}
	return x
}

func (t StateTimeouts) init() time.Duration       { return t.or(t.Initial, 30*time.Second) }
func (t StateTimeouts) reserved() time.Duration   { return t.or(t.Reserved, 60*time.Second) }
func (t StateTimeouts) purchasing() time.Duration { return t.or(t.Purchasing, 30*time.Second) }
func (t StateTimeouts) purchased() time.Duration  { return t.or(t.Purchased, 5*time.Minute) }
func (t StateTimeouts) imported() time.Duration   { return t.or(t.Imported, 30*time.Second) }

// Janitor 扫超时的 pending_purchase 行、按状态派给 Orchestrator.Recover。
//
// 一个进程只需要一个 Janitor · 用 goroutine 跑 Run(ctx)，ctx 结束就停。
type Janitor struct {
	orch     *Orchestrator
	state    *Store
	timeouts StateTimeouts
	// interval 两轮扫描间隔 · 0 = 默认 15s
	interval time.Duration
	// batchLimit 每状态每轮最多处理条数 · 防扫描把 DB 打死
	batchLimit int
	log        *slog.Logger
}

// JanitorConfig 装配参数。零值全部用默认。
type JanitorConfig struct {
	Orchestrator *Orchestrator
	State        *Store
	Timeouts     StateTimeouts
	Interval     time.Duration
	BatchLimit   int
	Logger       *slog.Logger
}

func NewJanitor(cfg JanitorConfig) *Janitor {
	j := &Janitor{
		orch:       cfg.Orchestrator,
		state:      cfg.State,
		timeouts:   cfg.Timeouts,
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

// SweepReport 一次扫描的统计（测试断言用）。
type SweepReport struct {
	Recovered int // 成功推进的
	Failed    int // 恢复失败（下次再试）
	Skipped   int // 已被别人推进（ErrStaleTransition）
}

// SweepOnce 扫一遍所有状态。每状态并行没意义（都在同一个 DB），顺序处理更简单。
func (j *Janitor) SweepOnce(ctx context.Context) SweepReport {
	var rep SweepReport
	// 顺序：短超时先扫（initial/purchasing/imported/reserved 都是"发出去很快就该回"），
	// purchased 最后（那是等号池，天然慢）
	rep.merge(j.sweepStatus(ctx, StatusInitial, j.timeouts.init()))
	rep.merge(j.sweepStatus(ctx, StatusReserved, j.timeouts.reserved()))
	rep.merge(j.sweepStatus(ctx, StatusPurchasing, j.timeouts.purchasing()))
	rep.merge(j.sweepStatus(ctx, StatusImported, j.timeouts.imported()))
	rep.merge(j.sweepStatus(ctx, StatusPurchased, j.timeouts.purchased()))
	return rep
}

func (j *Janitor) sweepStatus(ctx context.Context, status Status, olderThan time.Duration) SweepReport {
	var rep SweepReport
	rows, err := j.state.FindStale(ctx, status, olderThan, j.batchLimit)
	if err != nil {
		j.log.Error("janitor 扫超时单失败", "status", status, "err", err)
		return rep
	}
	for _, p := range rows {
		if ctx.Err() != nil {
			return rep
		}
		err := j.orch.Recover(ctx, p)
		switch {
		case err == nil:
			rep.Recovered++
			j.log.Info("janitor 恢复", "id", p.ID, "from", status)
		case isStaleErr(err):
			rep.Skipped++
		default:
			rep.Failed++
			j.log.Warn("janitor 恢复失败", "id", p.ID, "from", status, "err", err)
		}
	}
	return rep
}

func (r *SweepReport) merge(o SweepReport) {
	r.Recovered += o.Recovered
	r.Failed += o.Failed
	r.Skipped += o.Skipped
}

func isStaleErr(err error) bool {
	return err != nil && err.Error() != "" && (containsErr(err, ErrStaleTransition) ||
		containsErr(err, ErrPendingNotFound))
}

// containsErr 是 errors.Is 的简写 —— 避免每处都 import errors 包
func containsErr(err, target error) bool {
	for e := err; e != nil; {
		if e == target {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
