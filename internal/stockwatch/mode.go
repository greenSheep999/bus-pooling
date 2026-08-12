package stockwatch

import (
	"context"
	"database/sql"
	"sync/atomic"
	"time"
)

// Mode · 运营态 · 决定"什么时候抢号"策略。
//
// 自动切换 · 每 30s 采样一次：
//
//	demand = pending + in_flight 的 pull_intent 总数
//	supply = 6 家 vendor 过去 5min 的 stock 均值累加
//	ratio  = demand / max(1, supply)
//
//	ratio > 2       → ModeTight   紧张 · 车多 key 少 · Prober + webhook 都抢
//	0.3 < ratio ≤ 2 → ModeBalance 均衡 · 只 webhook 抢 · Prober 只观测
//	ratio ≤ 0.3     → ModeCool    冷 · 都不主动抢 · 用户来了才打
//
// 这些阈值先写死 · 观察数据后如需调再放 config（decisions §11.15）。
type Mode int32

const (
	ModeCool    Mode = iota // 库存充足 · 不主动抢
	ModeBalance             // 均衡 · 只 webhook fire
	ModeTight               // 紧张 · 探针 + webhook 都 fire
)

func (m Mode) String() string {
	switch m {
	case ModeTight:
		return "tight"
	case ModeBalance:
		return "balance"
	default:
		return "cool"
	}
}

// ModeMgr · 运营态管理器 · 后台每 30s 采样一次 · 存原子变量供 hot path 读。
type ModeMgr struct {
	db      *sql.DB
	current atomic.Int32 // 存 Mode

	// 观测窗口 · 默认 5min · Stock 均值统计窗
	stockWindow time.Duration
	// 采样间隔 · 默认 30s
	tickInterval time.Duration

	cancel context.CancelFunc
	done   chan struct{}
}

func NewModeMgr(database *sql.DB) *ModeMgr {
	m := &ModeMgr{
		db:           database,
		stockWindow:  5 * time.Minute,
		tickInterval: 30 * time.Second,
	}
	m.current.Store(int32(ModeCool)) // 冷启动默认 cool · 采样后才升级
	return m
}

// Current · 当前运营态 · 无锁读 · 供 Prober / webhook 判断要不要 fire
func (m *ModeMgr) Current() Mode {
	if m == nil {
		return ModeCool
	}
	return Mode(m.current.Load())
}

// Start · 起后台 goroutine · 每 tickInterval 重采一次运营态
func (m *ModeMgr) Start(ctx context.Context) {
	if m == nil || m.db == nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.done = make(chan struct{})

	go func() {
		defer close(m.done)
		// 启动立即采一次
		m.sample(runCtx)
		ticker := time.NewTicker(m.tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				m.sample(runCtx)
			}
		}
	}()
}

// Stop · 停后台 · timeout 兜底
func (m *ModeMgr) Stop(timeout time.Duration) {
	if m == nil || m.cancel == nil {
		return
	}
	m.cancel()
	select {
	case <-m.done:
	case <-time.After(timeout):
	}
}

// sample · 采一次 demand/supply · 算 ratio 更新 mode
//
// **demand 查 pending_purchase 不查 pull_intent** —— 实际拉号链路走
// pending_purchase 状态机（decider/state.go）· pull_intent 是 001 建表时的规划
// 但生产代码从没写过它（coalescer 的 Anon/Team 还是 stub）。查 pull_intent 会恒得 0 ·
// 让 mode 永远锁在 cool · 抢号链一次都不 fire。
//
// 算 demand 的状态口径：
//   - initial / reserved · 已冻结钱等着买 · 算需求
//   - purchasing · 请求已发 vendor 未确认 · 算需求（还没拿到号）
//   - need_recover_vendor / need_manual · 卡住待人工 · 算需求（钱还冻着）
//   - purchased / imported / completed · 已拿到号 · 不算
//   - cancelled_reserve · 已释放冻结 · 不算
//
// 另外加 stock_watcher 里 watching 的条数 —— 那些是已经在等补货的真实需求 ·
// 它们不在 pending_purchase 的活跃状态里（缺货时已经退出主流程）。
func (m *ModeMgr) sample(ctx context.Context) {
	var pendingDemand int
	if err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pending_purchase
		 WHERE status IN ('initial','reserved','purchasing',
		                  'need_recover_vendor','need_manual')`).Scan(&pendingDemand); err != nil {
		return // 库查错 · 保持当前 mode
	}
	var watchingDemand int
	if err := m.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM stock_watcher WHERE status = 'watching'`).Scan(&watchingDemand); err != nil {
		return
	}
	demand := pendingDemand + watchingDemand

	// vendor_probe 过去 5min stock 均值 · 累加 6 家
	cutoff := time.Now().UTC().Add(-m.stockWindow).Format(time.RFC3339)
	var supply sql.NullFloat64
	if err := m.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(avg_per_vendor), 0) FROM (
			SELECT AVG(COALESCE(stock_total, 0)) as avg_per_vendor
			  FROM vendor_probe
			 WHERE probed_at >= ? AND alive = 1
			 GROUP BY vendor_id
		)`, cutoff).Scan(&supply); err != nil {
		return
	}

	newMode := decideMode(demand, supply.Float64)
	old := Mode(m.current.Swap(int32(newMode)))
	_ = old // 想 log 切换时可用
}

// decideMode · 纯函数 · 便于测
func decideMode(demand int, supply float64) Mode {
	if supply < 1 {
		supply = 1
	}
	ratio := float64(demand) / supply
	switch {
	case ratio > 2:
		return ModeTight
	case ratio > 0.3:
		return ModeBalance
	default:
		return ModeCool
	}
}

// ShouldProberFire · 探针 delta 检测到 stock>0 时 · 该不该主动 fire Purchase？
// 紧张态才主动抢 · 均衡 / 冷都只观测。
func (m *ModeMgr) ShouldProberFire() bool {
	return m.Current() == ModeTight
}

// ShouldWebhookFire · vendor webhook new_keys 到时 · 该不该唤醒队列？
// 紧张 + 均衡都 fire · 冷不 fire（库存充足没排队 · 白抢）。
func (m *ModeMgr) ShouldWebhookFire() bool {
	c := m.Current()
	return c == ModeTight || c == ModeBalance
}
