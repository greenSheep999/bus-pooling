package vendorview

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/stockwatch"
)

// Prober 后台跑的 vendor 探针 · 每家 vendor 一个 ticker。
//
// 关键设计：
//  1. 错峰启动 · 6 家 vendor 各差 5-10 秒 · 避免同时打上游触发反爬 / rate limit
//  2. 单次探测超时 3 秒 · 慢的家挂了不拖 goroutine
//  3. 探测 = 调 vendor.Stock（跟正式拉号是同一批 credential · 共享余额）
//  4. 结果写 vendor_probe · 出错不 log 到堆（只写 error_kind 字段 · 后台 dashboard 看）
//  5. **stock-delta 推算 restock** · 每次探完跟上一轮同 region 比 · 正增量落
//     vendor_dispatch source='vendor_self' dispatch_key='delta-{region}-{ts}' · 覆盖无 fleet
//     端点的多家 vendor · 无额外 API 调用（完全复用 Stock 端点响应）
//  6. **自适应频次**（decisions §11.12）· baseline 60s 省 API · 探到 delta 或 fleet_active
//     切 hot 10s · 6min 无事件退回 baseline · 稳态省钱热点期快
//
// 关闭：Stop() context cancel · 所有 goroutine 优雅退出。
type Prober struct {
	registry      *providers.Registry
	store         *ProbeStore
	zoneStore     *ProbeZoneStore // migration 029 · 每探针每 zone 一行 · 精确定价的权威源
	orderKeyStore *OrderKeyStore  // 用于 stock-delta 落 vendor_dispatch · 可 nil（测试）
	notifier      RestockNotifier // 抢号链通知口 · nil = 不通知（老装配 / 测试）
	pricing       PricingLookup   // vendor 报价换算入口（migration 028 · docs/18 §1.3）· nil 时走 fallback
	health        *HealthStore    // 管线心跳 · 每轮探测盖戳（migration 036）· 可 nil
	interval      time.Duration   // baseline 探测间隔（默认 60s）
	hotInterval   time.Duration   // hot 模式间隔（默认 10s · 探到 delta / fleet_active 后启用）
	hotDuration   time.Duration   // hot 持续时长（默认 6min · 无事件后退回 baseline）
	timeout       time.Duration   // 单次 Stock 调用超时（默认 3s）
	logger        *slog.Logger

	// 运行时 · Start 后填
	cancel context.CancelFunc
	done   chan struct{}

	// hot 状态跟踪（每 vendor 一份）· sync.Map 避免加锁
	hotUntil sync.Map // vendor_id → time.Time · 到这个时刻前用 hotInterval
}

// PricingLookup · vendor_pricing 表适配的抽象（实现方 pricing.Store · main.go 装配注入）。
//
// 每次探针落库前查一次 · 用 vendor 报价的原始 microunit × credits_per_unit / 1_000_000
// 换算成我方积分（唯一权威 our_unit_credits）· 之后所有读方（decider / vendorview /
// PricedFor）**读积分列 · 不再算**（docs/18 §1.3）。
type PricingLookup interface {
	// QuoteFor 返换算规则 · rawUnits 是 vendor 报价 microunit
	// 返值：quote_currency / credits_per_unit
	// nil 或找不到时装配层适配器应返 fallback（credit / 1_000_000）
	QuoteFor(ctx context.Context, vendorID string) (currency string, creditsPerUnit int64)
}

// RestockNotifier · 抢号链通知口的抽象（实现方 stockwatch.Watcher）。
//
// 定在消费侧（vendorview）· 避免 vendorview → stockwatch 硬依赖 · 也便于测试 mock。
type RestockNotifier interface {
	Notify(ctx context.Context, p stockwatch.NotifyParams) error
}

// ProberConfig 装配 Prober。
type ProberConfig struct {
	Registry *providers.Registry
	Store    *ProbeStore
	// ZoneStore migration 029 · 每探针每 zone 一行 · nil 时不写侧表（测试 / 老装配）
	ZoneStore *ProbeZoneStore
	// OrderKeyStore 用于 stock-delta 推算路径落 vendor_dispatch · nil 时 delta 关闭（测试）
	OrderKeyStore *OrderKeyStore
	// Notifier 抢号链通知口 · stock-delta 推出 restock 时唤醒挂单 · nil = 不通知
	Notifier RestockNotifier
	// Pricing · vendor_pricing 表换算入口（docs/18 §1.3）· nil = fallback (credit 1:1)
	Pricing PricingLookup
	// HealthStore 管线心跳（migration 036）· nil = 不盖戳（测试 / 老装配）
	HealthStore *HealthStore
	// Interval baseline 探测间隔 · 默认 60s（decisions §10.4 Q2）
	Interval time.Duration
	// HotInterval hot 模式间隔 · 默认 10s（探到 restock 事件后 6min 内提频）
	HotInterval time.Duration
	// HotDuration hot 持续时长 · 默认 6min · 无新事件退回 baseline
	HotDuration time.Duration
	// Timeout 单次 Stock 调用超时 · 默认 3s
	Timeout time.Duration
	Logger  *slog.Logger
}

// NewProber Registry 或 Store 为 nil 时 New 仍成功，Start 会 no-op（便于测试 / DryRun）
func NewProber(cfg ProberConfig) *Prober {
	interval := cfg.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	hotInterval := cfg.HotInterval
	if hotInterval <= 0 {
		hotInterval = 10 * time.Second
	}
	hotDuration := cfg.HotDuration
	if hotDuration <= 0 {
		hotDuration = 6 * time.Minute
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Prober{
		registry:      cfg.Registry,
		store:         cfg.Store,
		zoneStore:     cfg.ZoneStore,
		orderKeyStore: cfg.OrderKeyStore,
		notifier:      cfg.Notifier,
		pricing:       cfg.Pricing,
		health:        cfg.HealthStore,
		interval:      interval,
		hotInterval:   hotInterval,
		hotDuration:   hotDuration,
		timeout:       timeout,
		logger:        logger,
	}
}

// Start 启动所有 enabled vendor 的探针 goroutine。
// 幂等 · 重复调 no-op。
func (p *Prober) Start(ctx context.Context) {
	if p.registry == nil || p.store == nil {
		p.logger.Info("vendorview.Prober: registry 或 store 为 nil · 不启动")
		return
	}
	if p.cancel != nil {
		return // 已经启动
	}
	runCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.done = make(chan struct{})

	entries := p.registry.Enabled()
	p.logger.Info("vendorview.Prober 启动",
		"vendor_count", len(entries),
		"interval", p.interval,
		"timeout", p.timeout,
	)

	// 每 vendor 一个 goroutine · 错峰启动
	go func() {
		defer close(p.done)
		var wg sync.WaitGroup
		for i, e := range entries {
			wg.Add(1)
			offset := time.Duration(i) * 5 * time.Second // 错峰 5s
			go func(vendor providers.Vendor, offset time.Duration) {
				defer wg.Done()
				// 先等错峰
				select {
				case <-time.After(offset):
				case <-runCtx.Done():
					return
				}
				// 立即探一次（不等 timer 第一次 fire）
				p.probeOnce(runCtx, vendor)
				// 自适应循环：每次探完根据 hot 状态选下一轮间隔
				timer := time.NewTimer(p.nextInterval(string(vendor.ID())))
				defer timer.Stop()
				for {
					select {
					case <-runCtx.Done():
						return
					case <-timer.C:
						p.probeOnce(runCtx, vendor)
						timer.Reset(p.nextInterval(string(vendor.ID())))
					}
				}
			}(e.Vendor, offset)
		}
		wg.Wait()
	}()
}

// nextInterval · 决定单 vendor 下一轮探测间隔。
//
// 逻辑：hotUntil[vendor] > now → 用 hotInterval · 否则 baseline interval。
// hot 状态由 deriveStockDelta 里检测到 delta > 0 时 bump（延到 now + hotDuration）。
// computeCreditsFromMoney · 把 vendor 原始报价换成我方积分 microunit（docs/18 §1.3）
//
// 规则：
//
//	Money.Amount × credits_per_unit / 1_000_000
//
// 找不到 pricing 或 pricing 是 nil：走 credit / 1_000_000 fallback（1:1）· 老行为兼容。
// Money.Amount == 0（vendor 没货 · 常见）：返 0 · 上层用 nullIfZeroInt64 转 NULL。
func (p *Prober) computeCreditsFromMoney(ctx context.Context, vendorID string, m providers.Money) int64 {
	if m.Amount == 0 {
		return 0
	}
	perUnit := int64(1_000_000) // fallback: 1:1
	if p.pricing != nil {
		_, cpu := p.pricing.QuoteFor(ctx, vendorID)
		if cpu > 0 {
			perUnit = cpu
		}
	}
	return m.Amount * perUnit / 1_000_000
}

// zoneKeyOf · 从一个 ZoneStock 取归一后的 zone 标识（唯一一处规则 · 主表侧表都走它）
//
// 优先 z.Zone · 空则从 z.Region 兜 —— 各 vendor 给的地区字段形态不一样：
// 有的只给短名 · 有的只给完整 region 名 · 有的两个都给（docs/19-fields.md §3）。
// 两个都认不出时返空串 · **不瞎猜**。
func zoneKeyOf(z providers.ZoneStock) providers.Zone {
	if zk := providers.ZoneOf(string(z.Zone)); zk != "" {
		return zk
	}
	return providers.ZoneOf(z.Region)
}

// deltaKeyOf · stock-delta 对比用的键。
//
// 优先 Zone（归一后 · 每家都有值）· 空则回落 Region —— **回落是为了兼容老样本**：
// migration 029 之前落的 stock_by_region JSON 没有 zone 字段 · 反序列化出来 Zone 是空 ·
// 那时的键还是 region。回落让"新样本 vs 老样本"这一轮对比不会因为键变了而误判成全量 restock。
func deltaKeyOf(r RegionStock) string {
	if r.Zone != "" {
		return r.Zone
	}
	return r.Region
}

// classifyPriceSource · 标 our_unit_credits 是怎么算的 · 便于对账
func (p *Prober) classifyPriceSource(cur string) string {
	switch cur {
	case providers.CurrencyUSD:
		return "computed_from_usd"
	default:
		// credit / CNY 都视为 vendor 侧就是积分（1:1）
		return "vendor_native"
	}
}

func (p *Prober) nextInterval(vendorID string) time.Duration {
	if v, ok := p.hotUntil.Load(vendorID); ok {
		if until, ok := v.(time.Time); ok && time.Now().Before(until) {
			return p.hotInterval
		}
	}
	return p.interval
}

// bumpHot · 把某 vendor 拉进 hot 状态 · 下一轮就用 hotInterval。
// 有 delta / fleet_active / pending_on_stock 事件时调用。
func (p *Prober) bumpHot(vendorID string) {
	until := time.Now().Add(p.hotDuration)
	p.hotUntil.Store(vendorID, until)
}

// Stop 停探针 · 等所有 goroutine 退出（有 timeout 兜底避免卡死 shutdown）。
func (p *Prober) Stop(timeout time.Duration) {
	if p.cancel == nil {
		return
	}
	p.cancel()
	select {
	case <-p.done:
	case <-time.After(timeout):
		p.logger.Warn("vendorview.Prober: Stop 超时 · 强行返回")
	}
}

// probeOnce 探一次单 vendor · 结果落 store。
//
// 一次探测打两个端点：
//  1. Stock（我方账户视角 · 所有 vendor 都有）
//  2. PublicStatus（vendor fleet 视角 · 只有实现 PublicStatuser 接口的 vendor 才有）
//
// 两个端点串行调用（一次探测 timeout 内共 2×timeout 上限） · 错误独立记录。
// 错误分类：timeout / auth / http_5xx / http_4xx / other。
func (p *Prober) probeOnce(ctx context.Context, v providers.Vendor) {
	sample := ProbeSample{
		VendorID: string(v.ID()),
		ProbedAt: time.Now().UTC(),
	}

	// ─── 1. Stock 探测（我方账户视角 · 决定 Alive 主状态） ───
	stockCtx, stockCancel := context.WithTimeout(ctx, p.timeout)
	start := time.Now()
	snap, err := v.Stock(stockCtx, providers.StockOptions{})
	latency := time.Since(start)
	stockCancel()

	if err != nil {
		sample.Alive = false
		sample.ErrorKind = classifyError(err)
	} else if snap == nil {
		sample.Alive = false
		sample.ErrorKind = "empty_response"
	} else {
		sample.Alive = true
		sample.LatencyMs = int(latency.Milliseconds())
		sample.StockTotal = snap.Available
		sample.WarrantyMinutes = snap.WarrantyMinutes
		sample.MaxPerOrder = snap.MaxPerOrder

		// Money 是 struct {Amount int64, Currency string} · 取 Amount 存 · Currency 探针不管
		if len(snap.Zones) > 0 {
			regions := make([]RegionStock, 0, len(snap.Zones))
			for _, z := range snap.Zones {
				regions = append(regions, RegionStock{
					// Zone 归一后的值 · stock-delta 拿它当键（见 RegionStock 注释）
					Zone:           string(zoneKeyOf(z)),
					Region:         z.Region,
					Available:      z.Available,
					UnitPriceMicro: z.UnitPrice.Amount,
				})
			}
			sample.StockByRegion = regions
			sample.SamplePriceMicro = snap.Zones[0].UnitPrice.Amount
			sample.SamplePriceRegion = snap.Zones[0].Region

			// ── pricing 标准化（docs/18 §1.3 · migration 028）──
			// 上游原样 · 拿到就存
			z0 := snap.Zones[0]
			sample.VendorCurrency = string(z0.UnitPrice.Currency)
			sample.VendorUnitRaw = z0.UnitPrice.Amount
			if z0.UnitPrice.Currency == providers.CurrencyUSD {
				sample.VendorPriceUSDRaw = z0.UnitPrice.Amount
			}
			// 我方计算 · 唯一权威积分
			sample.OurUnitCredits = p.computeCreditsFromMoney(ctx, sample.VendorID, z0.UnitPrice)
			sample.OurUnitSource = p.classifyPriceSource(z0.UnitPrice.Currency)
			sample.OurComputedAt = sample.ProbedAt
		}

		if raw, _ := json.Marshal(snap); raw != nil {
			sample.RawSnapshot = raw
		}
	}

	// ─── 2. PublicStatus 探测（vendor fleet 视角 · 可选 · type assertion） ───
	if ps, ok := v.(providers.PublicStatuser); ok {
		psCtx, psCancel := context.WithTimeout(ctx, p.timeout)
		psSnap, psErr := ps.PublicStatus(psCtx)
		psCancel()
		if psErr != nil {
			sample.PSErrorKind = classifyError(psErr)
		} else if psSnap != nil {
			// 复制到 sample 的 PS* 字段（nil 指针形式 · vendor 没这个字段就 nil）
			if psSnap.KeysActive != 0 || psSnap.KeysDead != 0 || psSnap.KeysStock != 0 {
				v := psSnap.KeysActive
				sample.PSKeysActive = &v
			}
			if psSnap.KeysAlive != 0 {
				v := psSnap.KeysAlive
				sample.PSKeysAlive = &v
			}
			if psSnap.KeysDead != 0 {
				v := psSnap.KeysDead
				sample.PSKeysDead = &v
			}
			if psSnap.KeysStock != 0 || psSnap.KeysActive != 0 {
				// stock=0 时也要记 · 用 KeysActive 判断是否有效数据
				v := psSnap.KeysStock
				sample.PSKeysStock = &v
			}
			if psSnap.KeysSuspect != 0 {
				v := psSnap.KeysSuspect
				sample.PSKeysSuspect = &v
			}
			if psSnap.KeysTotal != 0 {
				v := psSnap.KeysTotal
				sample.PSKeysTotal = &v
			}
			sample.PSGenerating = &psSnap.Generating
			sample.PSStartedAt = psSnap.StartedAt
			if psSnap.UptimeSeconds > 0 {
				v := psSnap.UptimeSeconds
				sample.PSUptimeSeconds = &v
			}
			sample.PSRaw = psSnap.Raw
		}
	}

	// ─── 3. stock-delta 推算 restock（在写新样本前拿旧样本对比） ───
	// 逻辑：拿上一轮同 vendor 的 stock_by_region · 每 region 对比 · cur > prev 差值 =
	// 这段时间 vendor 新到批 · 落 vendor_dispatch 让 status 页立刻画到点。
	// **省钱** · 完全复用现有 Stock 端点响应 · 不加 API 调用。
	// **不重复** · dispatch_key 用 "delta-{region}-{probed_at_ts}" · 同一次探测同 region
	// 只落一次 · 不同轮不同时间戳自然不冲突。
	// **不误报** · 只有 delta > 0 且**上一轮也 alive**（否则可能只是 vendor 从挂到恢复
	// 首次读到的数不是真 restock · 例如恢复后读到已有库存但那批可能是很早的）· 且
	// 相邻两轮时间差 ≤ 2×interval（防补写老样本时误算）。
	if sample.Alive && len(sample.StockByRegion) > 0 && p.orderKeyStore != nil {
		p.deriveStockDelta(ctx, sample)
	}

	insErr := p.store.InsertProbe(ctx, sample)
	if insErr != nil {
		p.logger.Warn("vendorview.Prober: 写 vendor_probe 失败",
			"vendor", sample.VendorID,
			"err", insErr,
		)
	}

	// 管线心跳（migration 036）· probe 健康 = Stock 通 + 落库成功。
	// vendor 挂了（Stock 错）也记 err —— 持续拿不到新鲜样本本身就是要盯的信号
	// （运维再看 vendor_probe.alive 区分是 vendor 全站挂还是只我方访问断）。
	if p.health != nil {
		markErr := err
		if markErr == nil {
			markErr = insErr
		}
		_ = p.health.Mark(ctx, sample.VendorID, "probe", markErr)
	}

	// 侧表 · 逐 zone 落权威积分（docs/18 §5 未收口补齐 · migration 029）
	if p.zoneStore != nil && sample.Alive && snap != nil && len(snap.Zones) > 0 {
		zones := make([]ProbeZoneSample, 0, len(snap.Zones))
		for _, z := range snap.Zones {
			zoneKey := zoneKeyOf(z)
			credits := p.computeCreditsFromMoney(ctx, sample.VendorID, z.UnitPrice)
			zones = append(zones, ProbeZoneSample{
				VendorID:       sample.VendorID,
				ProbedAt:       sample.ProbedAt,
				Zone:           string(zoneKey),
				Region:         z.Region,
				Available:      z.Available,
				VendorCurrency: string(z.UnitPrice.Currency),
				VendorUnitRaw:  z.UnitPrice.Amount,
				OurUnitCredits: credits,
				OurUnitSource:  p.classifyPriceSource(string(z.UnitPrice.Currency)),
			})
		}
		if err := p.zoneStore.InsertBatch(ctx, zones); err != nil {
			p.logger.Warn("vendorview.Prober: 写 vendor_probe_zone 失败",
				"vendor", sample.VendorID, "err", err)
		}
	}
}

// deriveStockDelta · 探针 stock-delta 推算 restock · 落 vendor_dispatch。
//
// 参考同类聚合站做法：30 秒轮 stock 记 prev · cur > prev 差值 = 新增。我们同接法但间隔 60s ·
// 加上这条推算路径后 · 无 fleet 端点的 vendor 也能画出开号事件流。有 fleet 端点的 vendor
// 保持 fleet 为主 · delta 只做兜底补漏（同一批 dispatch_key 不同不冲突）。
//
// 已知不完美（跟 xi8 同一批局限）：
//   - vendor 侧发号-被抢空 < 2×interval 的短命批会漏（跟频次挂钩 · 提到 30s 后减一半）
//   - 恰好 prev=0, cur=0 但期间有一波来又走 · 漏（探针间隙盲区 · 只有事件源能救）
//   - 同批被多轮探到（vendor 发号后 stock 一直 > 0 拖数轮）· 只算第一轮 delta（cur>prev 一次）
func (p *Prober) deriveStockDelta(ctx context.Context, cur ProbeSample) {
	prev, err := p.store.LatestProbe(ctx, cur.VendorID)
	if err != nil || prev == nil || !prev.Alive {
		return
	}
	// 时间窗防误算 · 相邻两轮差 > 2×interval 视作重启后首采 · 不作 delta
	gap := cur.ProbedAt.Sub(prev.ProbedAt)
	if gap <= 0 || gap > 2*p.interval {
		return
	}

	// prev zone → available 映射
	//
	// **键用 zone 不用 region**（2026-08-13 修）：部分 vendor 不返 region 原文（恒空串）·
	// 拿 region 当键会让该家的 us / eu 两条**塌成一条**（后写的覆盖前面的）·
	// 结果整个区的 restock delta 被漏掉 · dispatch_key 也会撞（都是 "delta--<ts>"）。
	// zone 是归一后的标准字段 · 每家都有值（docs/19-fields.md §3）。
	prevByZone := make(map[string]int, len(prev.StockByRegion))
	for _, r := range prev.StockByRegion {
		prevByZone[deltaKeyOf(r)] = r.Available
	}

	var dispatches []providers.VendorDispatch
	for _, r := range cur.StockByRegion {
		zk := deltaKeyOf(r)
		p0 := prevByZone[zk]
		delta := r.Available - p0
		if delta <= 0 {
			continue
		}
		// dispatch_key = "delta-{zone}-{cur_probed_at}" · 幂等 · 同一探测点每 zone 只落一次
		key := "delta-" + zk + "-" + cur.ProbedAt.UTC().Format("20060102T150405Z")
		raw, _ := json.Marshal(map[string]any{
			"kind":        "stock_delta",
			"zone":        r.Zone,
			"region":      r.Region,
			"prev_stock":  p0,
			"cur_stock":   r.Available,
			"delta":       delta,
			"gap_seconds": int(gap.Seconds()),
			"probed_at":   cur.ProbedAt.UTC().Format(time.RFC3339),
		})
		dispatches = append(dispatches, providers.VendorDispatch{
			DispatchKey: key,
			// Region 字段仍存 vendor 原文（vendor_dispatch 表语义没变）· 空就空
			Region:       r.Region,
			DispatchedAt: cur.ProbedAt.UTC(),
			Count:        delta,
			Alive:        delta, // 刚探到时假设全 alive
			Status:       "running",
			Raw:          raw,
		})
	}
	if len(dispatches) == 0 {
		return
	}
	// source='vendor_self' —— 这是我们自己的探针推算 · 权威源 · 不是聚合站
	if err := p.orderKeyStore.UpsertDispatches(ctx, cur.VendorID, SourceVendorSelf, dispatches); err != nil {
		p.logger.Warn("vendorview.Prober: stock-delta 落 vendor_dispatch 失败",
			"vendor", cur.VendorID, "err", err)
		return
	}
	// 检测到 restock 事件 · 拉进 hot 模式（10s 频次 · 持续 6min）
	// 逻辑：一旦有新批次说明 vendor 正在开号 · 短期内很可能再来一波 · 提频抓准
	p.bumpHot(cur.VendorID)
	p.logger.Info("vendorview.Prober: stock-delta 推算 restock · 进入 hot 模式",
		"vendor", cur.VendorID,
		"batches", len(dispatches),
		"gap_seconds", int(gap.Seconds()),
		"hot_until", time.Now().Add(p.hotDuration).Format(time.RFC3339))

	// 通知抢号链 · 唤醒等这家补货的挂单（decisions §11.15）。
	//
	// source="stock_delta" —— stockwatch 侧只在 ModeTight（或 turbo 强制）时才真 fire ·
	// 均衡 / 冷态只观测。这条路径最慢（60s baseline 平均延迟 30s）· 真抢靠 webhook ·
	// 它的价值是"没 webhook 的 vendor 也有兜底" + "缺货期一直盯着"。
	//
	// 逐 region 通知 · 让挂了特定 region 的挂单能精确匹配。
	if p.notifier != nil {
		for _, d := range dispatches {
			// **region 归一化**（docs/16 缺口 5）· d.Region 是 vendor region 名（us-east-1）·
			// 挂单存的是 zone 名（us）· 走 ZoneOf 归一保 SQL 匹配
			if err := p.notifier.Notify(ctx, stockwatch.NotifyParams{
				VendorID: cur.VendorID,
				Region:   string(providers.ZoneOf(d.Region)),
				Count:    d.Count,
				Source:   "stock_delta",
			}); err != nil {
				p.logger.Warn("vendorview.Prober: 通知抢号链失败",
					"vendor", cur.VendorID, "region", d.Region, "err", err)
			}
		}
	}
}

// classifyError 把 vendor.Stock 返回的错误分类成短标签。
// timeout / auth / http_5xx / http_4xx / canceled / other · 用于 /status 页展示。
func classifyError(err error) string {
	if err == nil {
		return ""
	}
	// context 层
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	// net.Error timeout
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	// 用错误消息里的关键字判 · 简单粗暴
	// vendor adapter 会给带 http 状态的错误（"http 401" 之类）
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "401"),
		strings.Contains(msg, "forbidden"), strings.Contains(msg, "403"):
		return "auth"
	case strings.Contains(msg, "500"), strings.Contains(msg, "502"),
		strings.Contains(msg, "503"), strings.Contains(msg, "504"):
		return "http_5xx"
	case strings.Contains(msg, "400"), strings.Contains(msg, "404"),
		strings.Contains(msg, "429"):
		return "http_4xx"
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return "timeout"
	}
	return "other"
}
