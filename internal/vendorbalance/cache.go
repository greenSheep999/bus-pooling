// Package vendorbalance 缓存每家 vendor 的余额 · 拉号前预检用。
//
// **P5 · 2026-08-14**：上游余额管理 · 之前是空白 —— adapter 有 Balance() 但业务层
// 从不调 · 上游没钱只能等 vendor 返 insufficient_balance 被动失败 · 用户体验差 ·
// 我方账本也没预警。
//
// 本包做**最小可用**：
//   - 后台 poller 每 N 分钟拉一次每家 vendor 的 Balance()
//   - 结果缓存在内存 map · vendor_id → (amount, currency, checkedAt)
//   - decider 拉号前 Get(vendorID) 查一次 · 余额不足 log WARN
//
// **不做**（留 1d）：
//   - 自动切换到有钱的 vendor（需改 decider 主流程 · 复杂）
//   - 落库历史（现在只需实时预检 · 历史看 vendor_ledger）
//   - 告警外发（先 log · 后续接监控）
package vendorbalance

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// Balance · 单家 vendor 的余额快照
type Balance struct {
	VendorID  providers.VendorID
	Amount    int64 // microunit
	Currency  string
	CheckedAt time.Time
	// Err · 上次 poll 失败原因 · 非空说明 Amount 是"上次成功值"（可能过期）
	// nil = 最新
	Err error
}

// Cache 内存缓存
type Cache struct {
	mu       sync.RWMutex
	balances map[providers.VendorID]Balance

	registry *providers.Registry
	logger   *slog.Logger
	interval time.Duration
	timeout  time.Duration

	// 后台 poller 用
	cancel context.CancelFunc
	done   chan struct{}
}

// Config 装配 Cache
type Config struct {
	Registry *providers.Registry
	Logger   *slog.Logger
	// Interval 每 vendor 的 poll 间隔 · 默认 5min（余额变化不频繁 · 省 API 调用）
	Interval time.Duration
	// Timeout 单次 Balance() 调用超时 · 默认 8s
	Timeout time.Duration
}

// New 装配 Cache · 未调 Start 前 Get 恒返 (Balance{}, false)（老装配兼容）
func New(cfg Config) *Cache {
	interval := cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Cache{
		balances: make(map[providers.VendorID]Balance),
		registry: cfg.Registry,
		logger:   logger,
		interval: interval,
		timeout:  timeout,
	}
}

// Start · 起后台 poller · 每家 vendor 每 interval 拉一次 Balance()。
//
// 幂等 · 重复调 no-op。registry 或 nil 时 · Start 只 log 不启动（未装配路径）。
func (c *Cache) Start(ctx context.Context) {
	if c == nil || c.registry == nil {
		return
	}
	if c.cancel != nil {
		return // 已启动
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.done = make(chan struct{})

	go func() {
		defer close(c.done)
		// 启动立即拉一次
		c.pollAll(runCtx)
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				c.pollAll(runCtx)
			}
		}
	}()
}

// Stop · 等 poller goroutine 退出（timeout 兜底避免卡 shutdown）
func (c *Cache) Stop(timeout time.Duration) {
	if c == nil || c.cancel == nil {
		return
	}
	c.cancel()
	select {
	case <-c.done:
	case <-time.After(timeout):
		c.logger.Warn("vendorbalance.Cache: Stop 超时 · 强行返回")
	}
}

// pollAll · 拉一轮每家 vendor 的余额 · 失败保留旧值不清空
func (c *Cache) pollAll(ctx context.Context) {
	for _, e := range c.registry.Enabled() {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		bal, err := e.Vendor.Balance(callCtx)
		cancel()

		c.mu.Lock()
		if err != nil {
			// 失败 · 保留上次成功值（Amount 不清）· 记 Err
			old := c.balances[e.VendorID]
			old.Err = err
			old.CheckedAt = time.Now().UTC()
			c.balances[e.VendorID] = old
			c.logger.Warn("vendorbalance: poll 失败 · 保留旧值",
				"vendor", e.VendorID, "err", err)
		} else if bal != nil {
			c.balances[e.VendorID] = Balance{
				VendorID:  e.VendorID,
				Amount:    bal.Balance.Amount,
				Currency:  bal.Balance.Currency,
				CheckedAt: time.Now().UTC(),
			}
		}
		c.mu.Unlock()
	}
}

// Get · 查缓存 · 未命中返 (Balance{}, false)（未 poll 过或 vendor 不在 registry）
//
// **注意**：Cache 是"上次 poll 成功值" · 可能比 interval 老一点。预检用够（余额 poll
// 间隔 vs 拉号价值取舍 · 5min 间隔 vs 一次可能几十上百积分的下单风险 · 值得）。
func (c *Cache) Get(vendorID providers.VendorID) (Balance, bool) {
	if c == nil {
		return Balance{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	b, ok := c.balances[vendorID]
	return b, ok
}

// Enough · 判定余额是否够本次估价（micro 单位对齐）· 不够返 false + 剩余余额。
//
// 参数 estCredits 是我方积分口径的预估总额。cache 里存的可能是 CNY / USD 币种 ·
// **只在两边同币种时可靠**（credit / CNY 家 1:1 可直接比 · USD 家需要换算）——
// 未换算的直接判会误伤 · 所以 USD 币种返 (true, ...) 相当于"不判"（保守放行 · 让 vendor 自己拒）。
//
// TODO 后续：把 vendor_pricing 的 credits_per_unit 注入进来 · USD 家也能真判。
func (c *Cache) Enough(vendorID providers.VendorID, estCredits int64) (bool, int64) {
	b, ok := c.Get(vendorID)
	if !ok || b.Amount <= 0 {
		return true, 0 // 没数据 · 保守放行（老行为）
	}
	// USD 家不同币种 · 保守放行
	if b.Currency == providers.CurrencyUSD {
		return true, b.Amount
	}
	if b.Amount < estCredits {
		return false, b.Amount
	}
	return true, b.Amount
}
