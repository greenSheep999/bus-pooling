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
//
// 关闭：Stop() context cancel · 所有 goroutine 优雅退出。
type Prober struct {
	registry      *providers.Registry
	store         *ProbeStore
	orderKeyStore *OrderKeyStore // 用于 stock-delta 落 vendor_dispatch · 可 nil（测试）
	interval      time.Duration  // 每 vendor 的探测间隔（默认 60s）
	timeout       time.Duration  // 单次 Stock 调用超时（默认 3s）
	logger        *slog.Logger

	// 运行时 · Start 后填
	cancel context.CancelFunc
	done   chan struct{}
}

// ProberConfig 装配 Prober。
type ProberConfig struct {
	Registry *providers.Registry
	Store    *ProbeStore
	// OrderKeyStore 用于 stock-delta 推算路径落 vendor_dispatch · nil 时 delta 关闭（测试）
	OrderKeyStore *OrderKeyStore
	// Interval 探测间隔 · 默认 60s（decisions §10.4 Q2）
	Interval time.Duration
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
		orderKeyStore: cfg.OrderKeyStore,
		interval:      interval,
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
				// 立即探一次（不等 ticker 第一次 tick）
				p.probeOnce(runCtx, vendor)
				// 之后按 interval 走
				ticker := time.NewTicker(p.interval)
				defer ticker.Stop()
				for {
					select {
					case <-runCtx.Done():
						return
					case <-ticker.C:
						p.probeOnce(runCtx, vendor)
					}
				}
			}(e.Vendor, offset)
		}
		wg.Wait()
	}()
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
//   1. Stock（我方账户视角 · 所有 vendor 都有）
//   2. PublicStatus（vendor fleet 视角 · 只有实现 PublicStatuser 接口的 vendor 才有）
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
					Region:         z.Region,
					Available:      z.Available,
					UnitPriceMicro: z.UnitPrice.Amount,
				})
			}
			sample.StockByRegion = regions
			sample.SamplePriceMicro = snap.Zones[0].UnitPrice.Amount
			sample.SamplePriceRegion = snap.Zones[0].Region
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

	if err := p.store.InsertProbe(ctx, sample); err != nil {
		p.logger.Warn("vendorview.Prober: 写 vendor_probe 失败",
			"vendor", sample.VendorID,
			"err", err,
		)
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

	// prev region → available 映射
	prevByRegion := make(map[string]int, len(prev.StockByRegion))
	for _, r := range prev.StockByRegion {
		prevByRegion[r.Region] = r.Available
	}

	var dispatches []providers.VendorDispatch
	for _, r := range cur.StockByRegion {
		p0 := prevByRegion[r.Region]
		delta := r.Available - p0
		if delta <= 0 {
			continue
		}
		// dispatch_key = "delta-{region}-{cur_probed_at_unix}" · 幂等 · 同一探测点只落一次
		key := "delta-" + r.Region + "-" + cur.ProbedAt.UTC().Format("20060102T150405Z")
		raw, _ := json.Marshal(map[string]any{
			"kind":        "stock_delta",
			"region":      r.Region,
			"prev_stock":  p0,
			"cur_stock":   r.Available,
			"delta":       delta,
			"gap_seconds": int(gap.Seconds()),
			"probed_at":   cur.ProbedAt.UTC().Format(time.RFC3339),
		})
		dispatches = append(dispatches, providers.VendorDispatch{
			DispatchKey:  key,
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
	// source='vendor_self' —— 这是我们自己的探针推算 · 权威源 · 不是 xi8
	if err := p.orderKeyStore.UpsertDispatches(ctx, cur.VendorID, SourceVendorSelf, dispatches); err != nil {
		p.logger.Warn("vendorview.Prober: stock-delta 落 vendor_dispatch 失败",
			"vendor", cur.VendorID, "err", err)
		return
	}
	p.logger.Info("vendorview.Prober: stock-delta 推算 restock",
		"vendor", cur.VendorID,
		"batches", len(dispatches),
		"gap_seconds", int(gap.Seconds()))
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
