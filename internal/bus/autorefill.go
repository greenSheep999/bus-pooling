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

// AutoEnqueuer · 装配层给 scheduler 装的 stockwatch.Enqueue 桥
//
// **挂意图不预冻结** —— 跟 maybeEnqueueOnNoStock 一致 · fire 时走 decider.Pull 完整钱包事务。
// nil = Enqueue 分支只 log(旧行为)。
type AutoEnqueuer interface {
	Enqueue(ctx context.Context, req AutoEnqueueRequest) error
}

// AutoEnqueueRequest · scheduler → stockwatch 桥的入参
type AutoEnqueueRequest struct {
	BusID           string
	InitiatorPassengerID string
	Count           int
	PreferredVendor string // Decide 已挑好的 vendor
	MaxUnitPrice    int64  // 涨价保护
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
	// v1d-2 · 从 bus.Strategy 传下去的护栏(避免 puller 侧再查一遍)
	MaxUnitPrice    int64  // 0 = 不限
	PreferredVendor string // 空 = auto pick
}

// SchedulerDecide · 决策器接口 · 装配层实现为 decider.Decide 的适配器。
//
// 定义在 bus 包里·让 Scheduler 不直接 import decider。装配层负责:
//   1. 从 bus + passenger + stockwatch + vendorview 组装 DecideInput
//   2. 调 decider.Decide
//   3. 把输出翻译成 SchedulerVerdict
//
// nil = 老行为(直接 puller.Refill · 不过决策器) · 保 1a-1c 回归。
type SchedulerDecide interface {
	Decide(ctx context.Context, busID string, candidate SchedulerCandidate) SchedulerVerdict
}

// SchedulerCandidate · 一辆待判定车的字段快照(Scheduler 侧收集 · 传给 Decider)
//
// **注**·所有字段是**车级原始值** · **未合并全局**。Decider 侧的桥内部会调
// strategy.Effective() 拿最终值再判(§4.3.4 单入口铁律)· 这里带原始值只用来
// 给 Decider 参考(如 mock 场景 psd 表空 · 车级值即最终值)。
type SchedulerCandidate struct {
	BusID           string
	OwnerID         string
	AutoRefill      bool // 车级原始 auto_refill_enabled · 桥要覆盖走 Effective
	Watermark       int
	MinCount        int
	MaxUnitPrice    int64 // 0 = 不限
	PreferredVendor string
	AliveByVendor   map[string]int // 按 vendor 分组的活号数(空 map = 整车挂)
}

// SchedulerVerdict · Decider 返回给 Scheduler 的结果
type SchedulerVerdict struct {
	Action       SchedulerAction
	Reason       string // 拒的时候的原因
	PullCount    int    // Pull 的时候拉几个
	PullVendor   string // Pull 的时候用哪家 vendor(空 = auto)
	PullMaxPrice int64  // Pull 的时候单价上限
}

// SchedulerAction · Scheduler 侧的三态输出
type SchedulerAction int

const (
	ActionReject  SchedulerAction = iota // 不动
	ActionPull                            // 调 puller.Refill 下单
	ActionEnqueue                         // 挂 stockwatch(装配 AutoEnqueuer 时真调 · 未装配只 log)
)

// Scheduler · 定时扫水位 · 过 Decide · 触发 decider.Pull 或挂 stockwatch
type Scheduler struct {
	db       *sql.DB
	puller   AutoRefiller
	enqueuer AutoEnqueuer    // nil = ActionEnqueue 只 log
	decider  SchedulerDecide // nil = 老行为·直发 puller(1a-1c 回归)
	interval time.Duration
	logger   *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

// SetDecider · 装配层注入 decider.Decide 适配器 · 必须在 Start 前调用。
// nil 时保留老行为(直接 puller.Refill)。
func (s *Scheduler) SetDecider(d SchedulerDecide) {
	s.decider = d
}

// SetEnqueuer · 装配层注入 stockwatch.Enqueue 桥 · 必须在 Start 前调用。
// nil 时 ActionEnqueue 只 log(旧行为)。
func (s *Scheduler) SetEnqueuer(e AutoEnqueuer) {
	s.enqueuer = e
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
//
// **1f-C 铁律**(§4.3.4)· 这里只装**车级原始字段** · **不合并全局** ——
// 生效值由决策器桥调 strategy.Effective() 拿(§4.3.4 "在 Effective() 外解释
// refill_watermark / refill_min_count 语义" 是明文禁的)。
//
// 装配了 Decider 的路径这些字段只是"传给 Decider 参考的原始快照" · Decider
// 侧会再调 Effective() 重算 · 覆盖候选值。
//
// 未装 Decider 的路径(单测 · 保 1a-1c 回归)直接读这些字段做判据 —— 单测
// 场景下车级字段本身就是最终值(没有全局默认表参与) · 不算 fallback 手工合并。
type autoRefillCandidate struct {
	busID           string
	ownerID         string
	watermark       int // 车级原始值 · **不合并全局** · 用于 nil-decider 测试路径 + 传给 Decider 桥参考
	autoRefill      bool // 车级原始 auto_refill_enabled · 同上
	minCount        int  // 0 = 未设 · 用 watermark - alive 补齐
	maxUnitPrice    int64
	preferredVendor string
	aliveByVendor   map[string]int // vendor_id → alive · 空 map = 整车挂
}

// aliveTotal · 备胎判据外·还常需要整车 alive 数
func (c autoRefillCandidate) aliveTotal() int {
	total := 0
	for _, n := range c.aliveByVendor {
		total += n
	}
	return total
}

// ScanOnce · 单轮扫 · 找水位低于阈值的车 · 每辆过 Decide · 按输出触发 Pull/Enqueue
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
		if s.decideAndAct(ctx, c) {
			refilled++
		}
	}
	if touched > 0 {
		s.logger.Info("bus.Scheduler: 单轮完成", "candidates", touched, "refilled", refilled)
	}
	return touched, refilled
}

// decideAndAct · 单车决策 + 动作 · 返 true 表示真触发了 Pull
//
// **1f-C 铁律**(§4.3.4)· 决策**必须**过 Decider(装配层桥内部会调
// strategy.Effective) · 不允许在这里读车级原始字段做判据(§4.3.4 明文禁"在
// Effective() 外解释 refill_watermark / refill_min_count 语义")。
//
// nil-decider = 装配未完成 · 静默 skip · 不走"raw 字段兜底"分支。单测请显式
// SetDecider(见 autorefill_test.go 里的 defaultTestDecider)。
func (s *Scheduler) decideAndAct(ctx context.Context, c autoRefillCandidate) bool {
	if s.decider == nil {
		// 生产装配层一定注入 · 走到这条说明 wiring bug · log 一次不阻塞
		s.logger.Debug("bus.Scheduler: decider 未装配 · skip", "bus", c.busID)
		return false
	}
	verdict := s.decider.Decide(ctx, c.busID, SchedulerCandidate{
		BusID:           c.busID,
		OwnerID:         c.ownerID,
		AutoRefill:      c.autoRefill,
		Watermark:       c.watermark,
		MinCount:        c.minCount,
		MaxUnitPrice:    c.maxUnitPrice,
		PreferredVendor: c.preferredVendor,
		AliveByVendor:   c.aliveByVendor,
	})
	switch verdict.Action {
	case ActionReject:
		s.logger.Debug("bus.Scheduler: Decide 拒", "bus", c.busID, "reason", verdict.Reason)
		return false
	case ActionEnqueue:
		return s.doEnqueue(ctx, c, verdict.PullCount, verdict.PullVendor, verdict.PullMaxPrice)
	case ActionPull:
		return s.doPull(ctx, c, verdict.PullCount, verdict.PullVendor, verdict.PullMaxPrice)
	}
	return false
}

// doEnqueue · 挂 stockwatch(挂意图不预冻结 · fire 时走 decider.Pull 完整钱包事务)
//
// codex 拍板:挂意图不预冻结·跟 maybeEnqueueOnNoStock 一致。
// 未装配 enqueuer 时只 log(1a-1c 回归)。
func (s *Scheduler) doEnqueue(ctx context.Context, c autoRefillCandidate, count int, vendorID string, maxPrice int64) bool {
	if s.enqueuer == nil {
		s.logger.Info("bus.Scheduler: Decide 判挂单但 enqueuer 未装配 · 只 log",
			"bus", c.busID, "vendor", vendorID, "count", count)
		return false
	}
	if count <= 0 {
		count = 1
	}
	err := s.enqueuer.Enqueue(ctx, AutoEnqueueRequest{
		BusID:                c.busID,
		InitiatorPassengerID: c.ownerID,
		Count:                count,
		PreferredVendor:      vendorID,
		MaxUnitPrice:         maxPrice,
	})
	if err != nil {
		s.logger.Info("bus.Scheduler: 挂 stockwatch 失败 · 下轮再试",
			"bus", c.busID, "err", err)
		return false
	}
	s.logger.Info("bus.Scheduler: 挂 stockwatch",
		"bus", c.busID, "vendor", vendorID, "count", count, "max_price", maxPrice)
	return true
}

// doPull · 生成幂等键·调 puller
func (s *Scheduler) doPull(ctx context.Context, c autoRefillCandidate, count int, vendorID string, maxPrice int64) bool {
	idem, err := newAutoRefillIdemID()
	if err != nil {
		s.logger.Warn("bus.Scheduler: 生成幂等键失败", "err", err, "bus", c.busID)
		return false
	}
	s.logger.Info("bus.Scheduler: 触发自动补车",
		"bus", c.busID, "alive", c.aliveTotal(), "watermark", c.watermark,
		"count", count, "vendor", vendorID, "max_price", maxPrice)
	err = s.puller.Refill(ctx, AutoRefillRequest{
		BusID:                c.busID,
		InitiatorPassengerID: c.ownerID,
		Count:                count,
		IdempotencyRecordID:  idem,
		MaxUnitPrice:         maxPrice,
		PreferredVendor:      vendorID,
	})
	if err != nil {
		// 补车常见失败（余额不足 / 无库存 / 达上限）都不当致命 · 下次周期再试
		s.logger.Info("bus.Scheduler: 本轮补车未成 · 下轮再试",
			"bus", c.busID, "err", err)
		return false
	}
	return true
}

// loadCandidates · 一次 SQL 拿全部 active 车的车级原始字段 + 按 vendor 分组的活号 + owner_id
//
// **1f-C 铁律**(§4.3.4)· SQL **只**过滤 `status='active'` · **不做**任何"跟随全局"
// 的 IFNULL 合并 —— auto_refill_enabled / refill_watermark 的"nil = 跟随全局"
// 语义**只**在 strategy.Effective() 里解释(§4.3.4 明文禁在 Effective 外解释)。
//
// 决策路径(装配 Decider)会逐辆调 Effective 判"生效字段"是否满足触发条件 ·
// 单测路径(未装 Decider)直接用车级原始字段 —— 单测场景没有 psd 表参与 ·
// 车级字段就是最终值。
//
// **性能取舍**:候选池从"只筛符合 auto+watermark 的车"扩到"全部 active 车" ·
// v1 期用户量下每 5min 一轮扫依然秒级完成 · 换回 §4.3.4 单入口合规。
func (s *Scheduler) loadCandidates(ctx context.Context) ([]autoRefillCandidate, error) {
	q := `
		SELECT b.id, b.creator_passenger_id,
		       COALESCE(b.auto_refill_enabled, 0) AS auto_refill_raw,
		       COALESCE(b.refill_watermark, 0)    AS watermark_raw,
		       COALESCE(b.refill_min_count, 0)    AS min_count_raw,
		       COALESCE(b.max_unit_price, 0),
		       COALESCE(b.preferred_vendor, ''),
		       COALESCE(cl.vendor_id, ''),
		       COALESCE(SUM(CASE WHEN cl.status = 'alive' THEN 1 ELSE 0 END), 0) AS alive
		  FROM bus b
		  LEFT JOIN credential_ledger cl ON cl.owner_bus_id = b.id
		 WHERE b.status = 'active'
		 GROUP BY b.id, cl.vendor_id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("bus.Scheduler: 查候选: %w", err)
	}
	defer rows.Close()

	// 按 bus_id 聚 · 每行是 (bus, vendor) 组合
	byBus := make(map[string]*autoRefillCandidate)
	order := make([]string, 0, 8)
	for rows.Next() {
		var (
			busID, ownerID  string
			autoRefillInt   int
			watermark       int
			minCount        int
			maxPrice        int64
			preferredVendor string
			vendorID        string
			alive           int
		)
		if err := rows.Scan(&busID, &ownerID, &autoRefillInt, &watermark, &minCount, &maxPrice, &preferredVendor, &vendorID, &alive); err != nil {
			return nil, err
		}
		c, ok := byBus[busID]
		if !ok {
			c = &autoRefillCandidate{
				busID:           busID,
				ownerID:         ownerID,
				watermark:       watermark,
				autoRefill:      autoRefillInt == 1,
				minCount:        minCount,
				maxUnitPrice:    maxPrice,
				preferredVendor: preferredVendor,
				aliveByVendor:   make(map[string]int),
			}
			byBus[busID] = c
			order = append(order, busID)
		}
		// vendor_id 空(LEFT JOIN 无匹配·车里没号) · 只加个空 map
		if vendorID != "" && alive > 0 {
			c.aliveByVendor[vendorID] = alive
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]autoRefillCandidate, 0, len(order))
	for _, id := range order {
		out = append(out, *byBus[id])
	}
	return out, nil
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
