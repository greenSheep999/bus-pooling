package vendorview

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// Backfiller 后台定期拉 vendor 侧的历史订单 + key 生命周期。
//
// 跟 Prober 的分工：
//   - Prober 探活（60s · Stock + PublicStatus）· 落 vendor_probe · 高频轻量
//   - Backfiller 全量拉历史（5min · orders + keys）· 落 vendor_order + vendor_key · 低频大批
//
// 一次 backfill：对每家实现了 OrderHistoryLister/KeyHistoryLister 的 vendor 拉全量 upsert。
// vendor 侧订单量 / key 量都不大（几百到几千），全量比增量简单可靠。
type Backfiller struct {
	registry    *providers.Registry
	store       *OrderKeyStore
	ledgerStore *LedgerStore // 可 nil（老装配 / 测试）· 非 nil 才拉 ledger
	interval    time.Duration // 全量间隔（默认 5min）
	timeout     time.Duration // 单家 vendor 单次 backfill 超时
	logger      *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

type BackfillerConfig struct {
	Registry *providers.Registry
	Store    *OrderKeyStore
	// LedgerStore 交叉对账用（docs/20 §1）· 传 nil = 不拉 ledger（vendor 无端点时也不影响）
	LedgerStore *LedgerStore
	Interval    time.Duration
	Timeout     time.Duration
	Logger      *slog.Logger
}

func NewBackfiller(cfg BackfillerConfig) *Backfiller {
	interval := cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Backfiller{
		registry: cfg.Registry, store: cfg.Store, ledgerStore: cfg.LedgerStore,
		interval: interval, timeout: timeout, logger: logger,
	}
}

func (b *Backfiller) Start(ctx context.Context) {
	if b.registry == nil || b.store == nil {
		b.logger.Info("vendorview.Backfiller: registry / store 为 nil · 不启动")
		return
	}
	if b.cancel != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.done = make(chan struct{})

	entries := b.registry.Enabled()
	b.logger.Info("vendorview.Backfiller 启动",
		"vendor_count", len(entries),
		"interval", b.interval, "timeout", b.timeout,
	)

	go func() {
		defer close(b.done)
		// 启动时立即跑一次全量
		b.runOnce(runCtx)
		ticker := time.NewTicker(b.interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				b.runOnce(runCtx)
			}
		}
	}()
}

func (b *Backfiller) Stop(timeout time.Duration) {
	if b.cancel == nil {
		return
	}
	b.cancel()
	select {
	case <-b.done:
	case <-time.After(timeout):
		b.logger.Warn("vendorview.Backfiller: Stop 超时")
	}
}

// runOnce 对所有 enabled vendor 并发跑一次 backfill（每家一个 goroutine · 互不阻塞）
func (b *Backfiller) runOnce(ctx context.Context) {
	entries := b.registry.Enabled()
	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func(v providers.Vendor) {
			defer wg.Done()
			b.backfillVendor(ctx, v)
		}(e.Vendor)
	}
	wg.Wait()
}

// backfillVendor 单家 vendor 的 backfill · orders + keys 各一次
func (b *Backfiller) backfillVendor(ctx context.Context, v providers.Vendor) {
	vid := string(v.ID())

	// 1. Orders（如果 vendor 实现了 OrderHistoryLister）
	if lister, ok := v.(providers.OrderHistoryLister); ok {
		orders, err := b.collectOrders(ctx, lister)
		if err != nil {
			b.logger.Warn("backfill orders 失败",
				"vendor", vid, "err", err,
			)
		} else if len(orders) > 0 {
			if err := b.store.UpsertOrders(ctx, vid, orders); err != nil {
				b.logger.Warn("upsert orders 失败", "vendor", vid, "err", err)
			}
		}
	}

	// 2. Keys（如果 vendor 实现了 KeyHistoryLister）
	if lister, ok := v.(providers.KeyHistoryLister); ok {
		keys, err := b.collectKeys(ctx, lister)
		if err != nil {
			b.logger.Warn("backfill keys 失败",
				"vendor", vid, "err", err,
			)
		} else if len(keys) > 0 {
			if err := b.store.UpsertKeys(ctx, vid, keys); err != nil {
				b.logger.Warn("upsert keys 失败", "vendor", vid, "err", err)
			}
		}
	}

	// 3. Dispatches（fleet-wide 最近开号 · 若 vendor 实现了 FleetLister）
	// 这一层是"这个 vendor 平台整体发货节奏" · 每家 vendor 只要有类似端点都能实现 ·
	// /status 页每张 vendor 卡都能显示"平均 X 分钟一批 · 累计发过 Y 个 key"
	if lister, ok := v.(providers.FleetLister); ok {
		callCtx, cancel := context.WithTimeout(ctx, b.timeout)
		dispatches, err := lister.ListDispatches(callCtx, 0)
		cancel()
		if err != nil {
			b.logger.Warn("backfill dispatches 失败",
				"vendor", vid, "err", err,
			)
		} else if len(dispatches) > 0 {
			// source="vendor_self" · vendor 自家 fleet 端点是权威源
			if err := b.store.UpsertDispatches(ctx, vid, "vendor_self", dispatches); err != nil {
				b.logger.Warn("upsert dispatches 失败", "vendor", vid, "err", err)
			}
		}
	}

	// 4. Ledger（vendor 侧积分流水 · 交叉对账 · docs/20 §1 · 若实现了 LedgerLister）
	if b.ledgerStore != nil {
		if lister, ok := v.(providers.LedgerLister); ok {
			entries, err := b.collectLedger(ctx, lister)
			if err != nil {
				b.logger.Warn("backfill ledger 失败", "vendor", vid, "err", err)
			} else if len(entries) > 0 {
				if err := b.ledgerStore.UpsertLedger(ctx, vid, entries); err != nil {
					b.logger.Warn("upsert ledger 失败", "vendor", vid, "err", err)
				}
			}
		}
	}
}

// collectLedger 分页拉全部流水 · 单页超时 b.timeout · 最多 100 页兜底
func (b *Backfiller) collectLedger(
	ctx context.Context, lister providers.LedgerLister,
) ([]providers.VendorLedgerEntry, error) {
	var all []providers.VendorLedgerEntry
	cursor := ""
	for pages := 0; pages < 100; pages++ {
		pageCtx, cancel := context.WithTimeout(ctx, b.timeout)
		page, err := lister.ListLedger(pageCtx, cursor)
		cancel()
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		all = append(all, page.Items...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return all, nil
}

// collectOrders 分页拉全部 · 单页超时 b.timeout · 最多 100 页兜底防死循环
func (b *Backfiller) collectOrders(
	ctx context.Context, lister providers.OrderHistoryLister,
) ([]providers.VendorOrder, error) {
	var all []providers.VendorOrder
	cursor := ""
	for pages := 0; pages < 100; pages++ {
		pageCtx, cancel := context.WithTimeout(ctx, b.timeout)
		page, err := lister.ListOrders(pageCtx, cursor)
		cancel()
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		all = append(all, page.Items...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return all, nil
}

func (b *Backfiller) collectKeys(
	ctx context.Context, lister providers.KeyHistoryLister,
) ([]providers.VendorKey, error) {
	var all []providers.VendorKey
	cursor := ""
	for pages := 0; pages < 100; pages++ {
		pageCtx, cancel := context.WithTimeout(ctx, b.timeout)
		page, err := lister.ListKeys(pageCtx, cursor)
		cancel()
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		all = append(all, page.Items...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return all, nil
}
