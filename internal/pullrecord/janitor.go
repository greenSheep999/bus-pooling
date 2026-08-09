package pullrecord

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// AssignJanitor 扫 pending_assignment 卡在 initial 太久的行 · 转 need_manual。
//
// 背景（09-transactions §5 · P0-B 修复）：
// assign 是三段式（tx1 initial → tx 外 pool.UpdateCredential → tx2 completed）。
// tx1 与 pool 之间 · 或 pool 与 tx2 之间发生 SIGKILL / 网络断 · pending_assignment
// 会卡在 initial · 台账可能已改也可能未改·housepool 侧 group 可能已迁也可能未迁。
//
// 阶段 1a 的策略：**不自动 reconcile**（不去 pool 查 group）· 直接转 need_manual。
// 因为：
//   - 自动 forward 需要查 pool group 决定 · 走一个额外的 IO 路径 · 出错概率不小
//   - 生产上量前·assign 崩溃很少·让运营看更安全
//   - 1c 加真 housepool group 迁移时再补自动 reconcile
type AssignJanitor struct {
	db         *sql.DB
	interval   time.Duration
	stuckAfter time.Duration
	log        *slog.Logger
}

type AssignJanitorConfig struct {
	DB         *sql.DB
	Interval   time.Duration // 两轮之间的间隔·默认 30s
	StuckAfter time.Duration // 卡多久算 stuck·默认 5min
	Logger     *slog.Logger
}

func NewAssignJanitor(cfg AssignJanitorConfig) *AssignJanitor {
	j := &AssignJanitor{
		db: cfg.DB, interval: cfg.Interval, stuckAfter: cfg.StuckAfter, log: cfg.Logger,
	}
	if j.interval <= 0 {
		j.interval = 30 * time.Second
	}
	if j.stuckAfter <= 0 {
		j.stuckAfter = 5 * time.Minute
	}
	if j.log == nil {
		j.log = slog.Default()
	}
	return j
}

func (j *AssignJanitor) Run(ctx context.Context) {
	t := time.NewTicker(j.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.sweepOnce(ctx)
		}
	}
}

// SweepOnce 供测试直接调用。
func (j *AssignJanitor) SweepOnce(ctx context.Context) (int, error) {
	return j.sweepOnce(ctx)
}

func (j *AssignJanitor) sweepOnce(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().Add(-j.stuckAfter).Format(timeLayout)
	rows, err := j.db.QueryContext(ctx, `
		SELECT id, passenger_id, credential_id, target, target_bus_id, updated_at
		  FROM pending_assignment
		 WHERE status = 'initial' AND updated_at < ?`, cutoff)
	if err != nil {
		j.log.Error("assign janitor 查 stuck initial 失败", "err", err)
		return 0, err
	}
	defer rows.Close()

	type row struct {
		id, pid, cid, tgt string
		busID             sql.NullString
	}
	var stuck []row
	for rows.Next() {
		var r row
		var ua string
		if err := rows.Scan(&r.id, &r.pid, &r.cid, &r.tgt, &r.busID, &ua); err != nil {
			return 0, err
		}
		stuck = append(stuck, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if len(stuck) == 0 {
		return 0, nil
	}

	updated := 0
	for _, r := range stuck {
		// 转 need_manual · log 出来让运营对账
		_, err := j.db.ExecContext(ctx, `
			UPDATE pending_assignment
			   SET status = 'need_manual',
			       error = 'stuck 太久 · 可能是 tx1 与 pool 之间或 pool 与 tx2 之间崩溃',
			       updated_at = ?
			 WHERE id = ? AND status = 'initial'`,
			time.Now().UTC().Format(timeLayout), r.id)
		if err != nil {
			j.log.Error("assign janitor 转 need_manual 失败", "id", r.id, "err", err)
			continue
		}
		updated++
		j.log.Warn("assign janitor · pending_assignment stuck initial → need_manual",
			"id", r.id, "passenger_id", r.pid, "credential_id", r.cid,
			"target", r.tgt, "bus_id", r.busID.String,
			"reason", "崩溃窗口 · 运营查 housepool group + credential_ledger 对账")
	}
	if updated > 0 {
		return updated, nil
	}
	return 0, fmt.Errorf("assign janitor · 扫到 %d 卡单但全部更新失败", len(stuck))
}
