package bus

// 1d · 自动补车 scheduler · 定时扫水位低于阈值的 auto_refill bus · 触发 decider.Pull
//
// **触发口径**：单一 · 水位触发（活号数 < refill_watermark）。CLAUDE §7 阶段表 1d 提的
// "时钟 / 水位 / vendor webhook 触发" 三种 · 后两种其实都已经在别处做了：
//   - **vendor webhook 触发** = stockwatch 抢号链 · new_keys webhook 到 → fire 挂单
//   - **号死后自动补** = deathwatch RefillTick · 号死立即补一次（内容跟本 scheduler 一样）
// 所以本 scheduler 只负责"水位巡检" —— 覆盖"号还没死但用得快 / 抢号链没抢到 / 上次补
// 车失败重试"三种漏网情形。
//
// **周期**：5min · 短于 stockwatch fire 又长于 deathwatch sweep · 天然错峰。
// **防抖**：单进程 sqlite · 同辆车不会在窗口内被同一 scheduler 双触发 · scheduler 本身
// 就串行扫。多副本部署时可能被两个副本各触发一次 —— 但 decider 的幂等主键（bus_id+ts）
// 会去重（老 pending_purchase 未 settle 时同 bus 直接返 in-flight）· 不重复扣钱。

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// AutoRefiller · 装配层给 scheduler 装的 decider 桥 · 跟 deathwatch.RefillPuller 同款。
// 单独定义在这里是为了避免 bus 包反向 import decider（否则包循环）。
type AutoRefiller interface {
	Refill(ctx context.Context, req AutoRefillRequest) error
}

// AutoRefillRequest · scheduler → decider 桥的入参
type AutoRefillRequest struct {
	// BusID 目标车
	BusID string
	// InitiatorPassengerID 发起人 · 用 owner（他一定活跃）· 分摊按 planSplit 走
	InitiatorPassengerID string
	// Count 本次要拉的号数（= refill_min_count · nil 时按 refill_watermark 补齐差额）
	Count int
	// IdempotencyRecordID · scheduler 自建的幂等键（每轮扫码一个新 UUID）
	IdempotencyRecordID string
}

// Scheduler · 定时扫水位 · 触发 decider.Pull
type Scheduler struct {
	db       *sql.DB
	puller   AutoRefiller
	interval time.Duration
	logger   *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

// NewScheduler · interval<=0 用默认 5min · puller/db 为 nil 时 Start 是 no-op（不 panic）
func NewScheduler(db *sql.DB, puller AutoRefiller, interval time.Duration, logger *slog.Logger) *Scheduler {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{db: db, puller: puller, interval: interval, logger: logger}
}

// Start · 后台 goroutine · 首查延后一个 interval（避免刚起来数据未就绪误触发）
func (s *Scheduler) Start(ctx context.Context) {
	if s.db == nil || s.puller == nil {
		s.logger.Info("bus.Scheduler: db 或 puller 未装配 · 不启动")
		return
	}
	if s.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.logger.Info("bus.Scheduler 自动补车启动", "interval", s.interval)

	go func() {
		defer close(s.done)
		first := time.NewTimer(s.interval)
		defer first.Stop()
		select {
		case <-runCtx.Done():
			return
		case <-first.C:
			s.ScanOnce(runCtx)
		}
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				s.ScanOnce(runCtx)
			}
		}
	}()
}

// Stop · 优雅停 · 等 goroutine 收尾
func (s *Scheduler) Stop(timeout time.Duration) {
	if s.cancel == nil {
		return
	}
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(timeout):
		s.logger.Warn("bus.Scheduler: Stop 超时")
	}
}

// autoRefillCandidate · 一辆待补车的当前状态
type autoRefillCandidate struct {
	busID          string
	ownerID        string
	watermark      int
	minCount       int // 0 = 未设 · 用 watermark - alive 补齐
	aliveKeys      int
}

// ScanOnce · 单轮扫 · 找水位低于阈值的车 · 逐辆触发 decider.Pull
//
// **公开是为了测试** · 生产走 Start 的 ticker · 单轮扫可 unit-test 掉。
func (s *Scheduler) ScanOnce(ctx context.Context) (touched, refilled int) {
	if s.db == nil || s.puller == nil {
		return 0, 0
	}
	rows, err := s.loadCandidates(ctx)
	if err != nil {
		s.logger.Warn("bus.Scheduler: 扫候选失败", "err", err)
		return 0, 0
	}
	touched = len(rows)
	for _, c := range rows {
		if c.aliveKeys >= c.watermark {
			continue
		}
		count := c.minCount
		if count <= 0 {
			count = c.watermark - c.aliveKeys // 补到水位线
		}
		if count <= 0 {
			continue
		}
		idem, err := newAutoRefillIdemID()
		if err != nil {
			s.logger.Warn("bus.Scheduler: 生成幂等键失败", "err", err, "bus", c.busID)
			continue
		}
		s.logger.Info("bus.Scheduler: 触发自动补车",
			"bus", c.busID, "alive", c.aliveKeys, "watermark", c.watermark, "count", count)
		err = s.puller.Refill(ctx, AutoRefillRequest{
			BusID:                c.busID,
			InitiatorPassengerID: c.ownerID,
			Count:                count,
			IdempotencyRecordID:  idem,
		})
		if err != nil {
			// 补车常见失败（余额不足 / 无库存 / 达上限）都不当致命 · 下次周期再试
			s.logger.Info("bus.Scheduler: 本轮补车未成 · 下轮再试",
				"bus", c.busID, "err", err)
			continue
		}
		refilled++
	}
	if touched > 0 {
		s.logger.Info("bus.Scheduler: 单轮完成", "candidates", touched, "refilled", refilled)
	}
	return touched, refilled
}

// loadCandidates · 一次 SQL 拿全部 auto_refill=1 的车 + 活号数 + owner_id
//
// **性能**：LEFT JOIN + GROUP BY · v1 期用户量单进程 sqlite 秒级完成 · 后续量大再拆分页
func (s *Scheduler) loadCandidates(ctx context.Context) ([]autoRefillCandidate, error) {
	q := `
		SELECT b.id, b.creator_passenger_id, b.refill_watermark,
		       COALESCE(b.refill_min_count, 0),
		       COALESCE(SUM(CASE WHEN cl.status = 'alive' THEN 1 ELSE 0 END), 0) AS alive
		  FROM bus b
		  LEFT JOIN credential_ledger cl ON cl.owner_bus_id = b.id
		 WHERE b.auto_refill_enabled = 1
		   AND b.status = 'active'
		   AND b.refill_watermark > 0
		 GROUP BY b.id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("bus.Scheduler: 查候选: %w", err)
	}
	defer rows.Close()

	out := make([]autoRefillCandidate, 0, 8)
	for rows.Next() {
		var c autoRefillCandidate
		if err := rows.Scan(&c.busID, &c.ownerID, &c.watermark, &c.minCount, &c.aliveKeys); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// newAutoRefillIdemID · scheduler 每轮扫的幂等键 · 32 位十六进制（跟 api 层同格式）
func newAutoRefillIdemID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ErrPullerMissing · puller 未装配时的哨兵（测试用）
var ErrPullerMissing = errors.New("bus.Scheduler: puller 未装配")
