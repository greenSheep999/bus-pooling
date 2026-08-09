package pricing

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/decider"
)

// SurchargeResolver 把 surcharge_rule 引擎适配成 decider.RatesResolver。
//
// **1b P1-2B**：让 decider 从 DB 表求费率·不再靠 env const。
//
// 缓存：每次 Resolve 都跑 ListActive 会打 DB · 加个 TTL 缓存（默认 30s）·
// **rule 表改动最多 30s 生效**·不至于每次拉号都读表。
type SurchargeResolver struct {
	store *SurchargeStore
	ttl   time.Duration
	log   *slog.Logger

	mu        sync.RWMutex
	cached    []Rule
	cachedAt  time.Time
	// envFallback · 表空 / 出错时的兜底 Rates（1a env 值 · 保底不炸）
	envFallback decider.Rates
}

// SurchargeResolverConfig · 装配参数。
type SurchargeResolverConfig struct {
	Store       *SurchargeStore
	TTL         time.Duration
	Logger      *slog.Logger
	EnvFallback decider.Rates
}

func NewSurchargeResolver(cfg SurchargeResolverConfig) *SurchargeResolver {
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &SurchargeResolver{
		store:       cfg.Store,
		ttl:         cfg.TTL,
		log:         cfg.Logger,
		envFallback: cfg.EnvFallback,
	}
}

// Resolve · 按 ctx 求 Rates · 满足 decider.RatesResolver 接口。
//
// 流程：
//   1. 查（缓存 or DB）所有 active 规则
//   2. Engine.Eval(ctx) 求命中的 rate_bp 汇总
//   3. 转成 decider.Rates（各 kind 桶 → Rate 字段）
//   4. 表空 / 出错时 · 用 envFallback 兜底
func (r *SurchargeResolver) Resolve(ctx context.Context, rc decider.RateContext) decider.Rates {
	rules, err := r.rules(ctx)
	if err != nil {
		r.log.Warn("surcharge 规则加载失败·走 env fallback", "err", err)
		return r.envFallback
	}
	if len(rules) == 0 {
		// 表空 · 用 env 兜底（1a 兼容）
		return r.envFallback
	}
	engine := NewEngine(rules)
	res := engine.Eval(EvalContext{
		VendorID:         rc.VendorID,
		Zone:             rc.Zone,
		Count:            rc.Count,
		PassengerInvited: rc.PassengerInvited,
		BusAvgLifespanH:  rc.BusAvgLifespanH,
	})
	return decider.Rates{
		VendorMarkup: decider.Rate(res.Vendor),
		RegionMarkup: decider.Rate(res.Zone),
		SinglePull:   decider.Rate(res.SinglePull),
		Capability:   decider.Rate(res.Capability),
		Service:      decider.Rate(res.Service),
		Retail:       decider.Rate(res.Retail),
		Adhoc:        decider.Rate(res.Adhoc),
	}
}

// rules · 拿缓存里的规则·过 TTL 时回表。
func (r *SurchargeResolver) rules(ctx context.Context) ([]Rule, error) {
	r.mu.RLock()
	if time.Since(r.cachedAt) < r.ttl && r.cached != nil {
		out := r.cached
		r.mu.RUnlock()
		return out, nil
	}
	r.mu.RUnlock()

	// 缓存过期·回表
	r.mu.Lock()
	defer r.mu.Unlock()
	// double-check（并发 reader 可能同时进这里）
	if time.Since(r.cachedAt) < r.ttl && r.cached != nil {
		return r.cached, nil
	}
	rules, err := r.store.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	r.cached = rules
	r.cachedAt = time.Now()
	return rules, nil
}

// Invalidate · 后台 upsert 完可以主动清缓存（跳过 TTL）。
func (r *SurchargeResolver) Invalidate() {
	r.mu.Lock()
	r.cached = nil
	r.cachedAt = time.Time{}
	r.mu.Unlock()
}
