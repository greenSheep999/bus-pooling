package xi8

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// DispatchStore · 我方 vendor_dispatch 落库接口（避免循环依赖 · 上层传 vendorview.OrderKeyStore）
type DispatchStore interface {
	UpsertDispatches(ctx context.Context, vendorID, source string, ds []providers.VendorDispatch) error
}

// ZoneStore · vendor_probe_zone 落库接口（xi8 作为第二价格源写入 · docs/decisions §11.11）
type ZoneStore interface {
	InsertZoneBatch(ctx context.Context, samples []ZoneSample) error
}

// FlagStore · xi8_vendor_flags 落库接口（buyable/blocked/floating · 抢号 fire-guard · docs/23-endpoints-todo §3）
type FlagStore interface {
	UpsertVendorFlags(ctx context.Context, flags []VendorFlagSample) error
}

// VendorFlagSample · xi8 /api/vendors 里一个 vendor+zone 的可买性 flag · 上层适配到 vendorview.VendorFlag
type VendorFlagSample struct {
	VendorID    string // 我方 slug（xi8 vendor_id 已映射）
	Zone        string // us / eu · 归一后
	Buyable     bool
	Blocked     bool
	BlockReason string
	Floating    bool
	PriceFen    int
}

// ZoneSample · xi8 侧的一个 zone 快照 · 上层适配到 vendorview.ProbeZoneSample
type ZoneSample struct {
	VendorID       string
	ProbedAt       time.Time
	Zone           string // us / eu · 归一后
	Region         string // xi8 原文
	Available      int
	VendorCurrency string // xi8 都是 CNY
	VendorUnitRaw  int64  // microunit · 分 → microunit 时 × 10000
	OurUnitCredits int64  // xi8 是 CNY 计价 · 1 CNY = 1 积分 · pass-through
	// Source · 区分 xi8 的两条数据路径 · 空按 "xi8" 处理：
	//   "xi8"       · /api/vendors 实时快照（"现在的价"）
	//   "xi8_notif" · /api/my/notifications 历史事件流（"那一刻的价"）
	Source string
}

// Backfiller · xi8 一次性回填 · 只做后端对账 + 历史空窗填。
//
// 跟 vendorview.Backfiller 分工：
//   - vendorview.Backfiller · 拉 vendor 自家 fleet 端点 · source=vendor_self · 5min 一轮
//   - xi8.Backfiller         · 拉 xi8 restock-log + signals · source=xi8 · 手动 CLI 触发 or 定时
//
// 前端读路径全部只查 source=vendor_self · xi8 行只做**内部对账 + 历史空窗填**（CLAUDE.md §0.1）。
type Backfiller struct {
	client    *Client
	store     DispatchStore
	zoneStore ZoneStore // xi8 逐 zone 价格落库 · nil 时不写侧表（老装配 / 未 seed）
	flagStore FlagStore // xi8 buyable/blocked/floating 落库 · nil 时不写（抢号 guard 用）
	logger    *slog.Logger

	// 后台 tick 用（Start 后填）
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once

	// lastNotifID · 已拉过的最大通知 id · 下轮 since_id 用它只拉增量。
	// **进程内状态**（不落库）· 重启后从 0 开始 · 会重拉最近 100 条 ·
	// 但侧表写入走 INSERT OR REPLACE 幂等 · 重复无害。
	lastNotifID int
}

func NewBackfiller(client *Client, store DispatchStore, logger *slog.Logger) *Backfiller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Backfiller{client: client, store: store, logger: logger}
}

// SetZoneStore · 装配 xi8 → vendor_probe_zone 侧表 · main.go 起动时调
// （避免改 NewBackfiller 签名 · 老调用点无痛升级）
func (b *Backfiller) SetZoneStore(zs ZoneStore) { b.zoneStore = zs }

// SetFlagStore · 装配 xi8 → xi8_vendor_flags（抢号 fire-guard 用 buyable/blocked/floating）
func (b *Backfiller) SetFlagStore(fs FlagStore) { b.flagStore = fs }

// Start · 起后台 goroutine · 每 tickInterval 拉一次 signals（增量小·省流量）·
// 每 fullInterval 拉一次 restock-log 全量（500 条 · 覆盖 2 天）。
//
// 服务停跑期 xi8 侧会继续记 · 服务重启后一次全量 fetch 就能补回来。
// 只做 signals + restock-log · vendors 端点是实时状态 · 探针已经在拉不重复。
//
// tickInterval 建议 30s（xi8 服务端上货推 3s 内会到 signals）
// fullInterval 建议 5min（覆盖 signals 漏批 + xi8 自己推算延迟）
// client 或 store nil 时 · Start 是 no-op（未 seed xi8 API key 场景）
func (b *Backfiller) Start(ctx context.Context, tickInterval, fullInterval time.Duration) {
	if b.client == nil || b.store == nil {
		b.logger.Info("xi8.Backfiller: client 或 store nil · 不启动")
		return
	}
	if tickInterval <= 0 {
		tickInterval = 30 * time.Second
	}
	if fullInterval <= 0 {
		fullInterval = 5 * time.Minute
	}

	runCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.done = make(chan struct{})

	go func() {
		defer close(b.done)
		// 启动立即拉一次全量
		if _, _, err := b.RunOnce(runCtx, 500); err != nil {
			b.logger.Warn("xi8.Backfiller: 启动首次 RunOnce 失败", "err", err)
		}
		signalsTicker := time.NewTicker(tickInterval)
		defer signalsTicker.Stop()
		fullTicker := time.NewTicker(fullInterval)
		defer fullTicker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-signalsTicker.C:
				// 只拉 signals 增量（100 条够 30s 内的 · 省流量）
				if err := b.pullSignalsOnly(runCtx, 100); err != nil {
					b.logger.Warn("xi8.Backfiller: signals tick 失败", "err", err)
				}
			case <-fullTicker.C:
				// 全量 · 补 signals 漏批 + restock-log 推算源
				if _, _, err := b.RunOnce(runCtx, 500); err != nil {
					b.logger.Warn("xi8.Backfiller: full tick 失败", "err", err)
				}
			}
		}
	}()
}

// Stop 停后台 goroutine · 有 timeout 兜底
func (b *Backfiller) Stop(timeout time.Duration) {
	if b.cancel == nil {
		return
	}
	b.cancel()
	select {
	case <-b.done:
	case <-time.After(timeout):
		b.logger.Warn("xi8.Backfiller: Stop 超时 · 强行返回")
	}
}

// pullSignalsOnly · 只拉 signals · 增量 tick 用
func (b *Backfiller) pullSignalsOnly(ctx context.Context, limit int) error {
	signals, err := b.client.ListSignals(ctx, limit)
	if err != nil {
		return err
	}
	byVendor := make(map[string][]providers.VendorDispatch)
	for _, s := range signals.Signals {
		slug := VendorSlugForXi8ID(s.VendorID)
		if slug == "" {
			continue
		}
		t, err := parseXi8Time(s.At.ISO)
		if err != nil {
			continue
		}
		raw, _ := json.Marshal(s)
		d := providers.VendorDispatch{
			DispatchKey:  "xi8-sig-" + s.VendorOrderID,
			Region:       xi8RegionToOurs(joinRegions(s.Regions)),
			DispatchedAt: t,
			Count:        s.Count,
			Alive:        s.Count,
			Status:       "running",
			Raw:          raw,
		}
		byVendor[slug] = append(byVendor[slug], d)
	}
	for slug, ds := range byVendor {
		if err := b.store.UpsertDispatches(ctx, slug, "xi8", ds); err != nil {
			b.logger.Warn("xi8.Backfiller: signals upsert 失败", "vendor", slug, "err", err)
		}
	}
	return nil
}

// RunOnce · 拉一次 xi8 数据 · 落 vendor_dispatch source='xi8'。
//
// 步骤：
//  1. 先拉 signals（有 vendor_order_id · 最准 · 优先）· dispatch_key = "xi8-sig-{order_id}"
//  2. 再拉 restock-log（推算 · 兜底 · 覆盖 signals 没接的 vendor）· dispatch_key = "xi8-log-{vendor}-{region}-{iso}"
//  3. 同一 dispatch_key 幂等 upsert · 反复跑不重
//
// 返 (signalsCount, restocksCount) · 便于 CLI 打进度。
func (b *Backfiller) RunOnce(ctx context.Context, limit int) (int, int, error) {
	if b.client == nil {
		return 0, 0, fmt.Errorf("xi8: client nil · 未 seed API key")
	}

	// 1. signals（精准源 · 有 vendor_order_id）
	sigCount := 0
	if signals, err := b.client.ListSignals(ctx, limit); err == nil {
		byVendor := make(map[string][]providers.VendorDispatch)
		for _, s := range signals.Signals {
			slug := VendorSlugForXi8ID(s.VendorID)
			if slug == "" {
				b.logger.Warn("xi8: signal 未映射 vendor · 跳过", "xi8_vendor_id", s.VendorID, "name", s.Name)
				continue
			}
			t, err := parseXi8Time(s.At.ISO)
			if err != nil {
				continue
			}
			raw, _ := json.Marshal(s)
			d := providers.VendorDispatch{
				DispatchKey:  "xi8-sig-" + s.VendorOrderID,
				Region:       xi8RegionToOurs(joinRegions(s.Regions)),
				DispatchedAt: t,
				Count:        s.Count,
				Alive:        s.Count,
				Status:       "running",
				Raw:          raw,
			}
			byVendor[slug] = append(byVendor[slug], d)
			sigCount++
		}
		for slug, ds := range byVendor {
			if err := b.store.UpsertDispatches(ctx, slug, "xi8", ds); err != nil {
				b.logger.Warn("xi8: upsert signals 失败", "vendor", slug, "err", err)
			}
		}
	} else {
		b.logger.Warn("xi8: 拉 signals 失败 · 继续 restock-log", "err", err)
	}

	// 2. restock-log（推算源 · 覆盖广 · 兜底）
	rsCount := 0
	logs, err := b.client.ListRestockLog(ctx, limit)
	if err != nil {
		return sigCount, 0, fmt.Errorf("xi8: 拉 restock-log: %w", err)
	}
	byVendor := make(map[string][]providers.VendorDispatch)
	for _, r := range logs.Rows {
		slug := VendorSlugForXi8ID(r.VendorID)
		if slug == "" {
			b.logger.Warn("xi8: restock 未映射 vendor · 跳过", "xi8_vendor_id", r.VendorID, "name", r.Name)
			continue
		}
		t, err := parseXi8Time(r.At.ISO)
		if err != nil {
			continue
		}
		raw, _ := json.Marshal(r)
		d := providers.VendorDispatch{
			DispatchKey:  fmt.Sprintf("xi8-log-%s-%s", xi8RegionToOurs(r.Region), t.UTC().Format("20060102T150405Z")),
			Region:       xi8RegionToOurs(r.Region),
			DispatchedAt: t,
			Count:        r.Added,
			Alive:        r.Added,
			Status:       "running",
			Raw:          raw,
		}
		byVendor[slug] = append(byVendor[slug], d)
		rsCount++
	}
	for slug, ds := range byVendor {
		if err := b.store.UpsertDispatches(ctx, slug, "xi8", ds); err != nil {
			b.logger.Warn("xi8: upsert restock-log 失败", "vendor", slug, "err", err)
		}
	}
	// 3. vendors · 5 家实时逐 zone 单价 · 落 vendor_probe_zone 做第二源交叉核对
	//    （只补漏 · 权威值仍是 vendor_self · 见 PricedFor / decider 的 LatestZoneCredits 顺序）
	//    **同一次响应**里还带 buyable/blocked/floating · 顺手落 xi8_vendor_flags（抢号 guard）
	zoneCount := 0
	if b.zoneStore != nil || b.flagStore != nil {
		if vs, err := b.client.ListVendors(ctx); err == nil {
			if b.zoneStore != nil {
				zoneCount = b.pushVendorsToZone(ctx, vs)
			}
			if b.flagStore != nil {
				b.pushVendorFlags(ctx, vs)
			}
		} else {
			b.logger.Warn("xi8: 拉 /api/vendors 失败 · 侧表/flag 这轮不更新", "err", err)
		}
	}

	// 4. notifications · **唯一带历史价格的端点** · 落 source='xi8_notif'
	//    每条通知带那一刻的 price_fen · ProbedAt 用通知时刻 · 补探针上线前的价格空窗。
	//    since_id 只拉增量 —— 服务端 limit 硬顶 100 · 不带 since_id 每轮都在重复拉同一批。
	notifCount := 0
	if b.zoneStore != nil {
		if ns, err := b.client.ListNotifications(ctx, 100, b.lastNotifID); err == nil {
			notifCount = b.pushNotificationsToZone(ctx, ns)
			// 记住最大 id · 下轮只拉更新的
			for _, n := range ns.Items {
				if n.ID > b.lastNotifID {
					b.lastNotifID = n.ID
				}
			}
		} else {
			b.logger.Warn("xi8: 拉通知失败 · 历史价这轮不更新", "err", err)
		}
	}

	b.logger.Info("xi8: backfill 完成",
		"signals", sigCount, "restocks", rsCount,
		"vendor_zones", zoneCount, "notif_prices", notifCount,
		"last_notif_id", b.lastNotifID)
	return sigCount, rsCount, nil
}

// pushVendorsToZone · 把 xi8 逐 zone 单价 → vendor_probe_zone（source='xi8'）
//
// xi8 定价一律 CNY 计价（分 → microunit 换算：×10000）· pass-through 到我方积分
// （1 CNY = 1 积分 · docs/10-pricing §1.4）· 不做 vendor_pricing 换算（那条是给 USD 家的）。
func (b *Backfiller) pushVendorsToZone(ctx context.Context, resp *VendorsResp) int {
	if resp == nil || len(resp.Vendors) == 0 {
		return 0
	}
	now := time.Now().UTC()
	samples := make([]ZoneSample, 0, len(resp.Vendors)*2)
	for _, v := range resp.Vendors {
		slug := VendorSlugForXi8ID(v.VendorID)
		if slug == "" {
			continue
		}
		for _, r := range v.Regions {
			if r.PriceFen == 0 {
				continue // 该 zone 无价 · 不落
			}
			// 分（10^-2 CNY）→ microunit（10^-6 CNY）· ×10000
			rawMicro := int64(r.PriceFen) * 10_000
			samples = append(samples, ZoneSample{
				VendorID: slug,
				ProbedAt: now,
				// zone 保留归一后的 · 跟 vendor_self 一路口径（"us"/"eu"）
				Zone:           string(providers.ZoneOf(r.Region)),
				Region:         xi8RegionToOurs(r.Region), // 展开成我方 region 名 · 便于对账
				Available:      r.Stock,
				VendorCurrency: "CNY",
				VendorUnitRaw:  rawMicro,
				OurUnitCredits: rawMicro, // CNY 1:1 到积分
			})
		}
	}
	if len(samples) == 0 {
		return 0
	}
	if err := b.zoneStore.InsertZoneBatch(ctx, samples); err != nil {
		b.logger.Warn("xi8: 写 vendor_probe_zone 失败", "err", err)
		return 0
	}
	return len(samples)
}

// pushVendorFlags · 从 /api/vendors 捞 buyable/blocked/floating 落 xi8_vendor_flags（抢号 guard）。
//
// 跟 pushVendorsToZone 分开：那个落价格（有价才落）· 这个落可买性（**无价也要落** ——
// blocked 的 vendor 恰恰是 price_fen=0 · 正是 guard 最该拦的情况）。
func (b *Backfiller) pushVendorFlags(ctx context.Context, resp *VendorsResp) {
	if resp == nil || len(resp.Vendors) == 0 {
		return
	}
	flags := make([]VendorFlagSample, 0, len(resp.Vendors)*2)
	for _, v := range resp.Vendors {
		slug := VendorSlugForXi8ID(v.VendorID)
		if slug == "" {
			continue
		}
		for _, r := range v.Regions {
			flags = append(flags, VendorFlagSample{
				VendorID:    slug,
				Zone:        string(providers.ZoneOf(r.Region)),
				Buyable:     r.Buyable,
				Blocked:     r.Blocked,
				BlockReason: r.BlockReason,
				Floating:    r.Floating,
				PriceFen:    r.PriceFen,
			})
		}
	}
	if len(flags) == 0 {
		return
	}
	if err := b.flagStore.UpsertVendorFlags(ctx, flags); err != nil {
		b.logger.Warn("xi8: 写 xi8_vendor_flags 失败", "err", err)
	}
}

// pushNotificationsToZone · 站内通知 → vendor_probe_zone（source='xi8_notif'）
//
// **为什么单独一条路径**：`/vendors` 是**实时快照**（写"现在的价"）· 通知是**历史事件流**
// （每条带那一刻的 `price_fen`）· 是唯一能补探针上线前价格的源。两者 source 分开标 ·
// 免得"历史回填"和"实时观测"混在一起分不清哪条更权威。
//
// **ProbedAt 用通知自己的时刻**（不是拉取时刻）—— 这样落进去就是**那一刻的价** ·
// 时间轴对得上我方探针的行。
//
// 只落 `price_fen > 0` 的 —— `sold_out` 通知没价（也没意义）。
func (b *Backfiller) pushNotificationsToZone(ctx context.Context, resp *NotificationsResp) int {
	if resp == nil || len(resp.Items) == 0 {
		return 0
	}
	samples := make([]ZoneSample, 0, len(resp.Items))
	for _, n := range resp.Items {
		if n.PriceFen <= 0 {
			continue
		}
		// 通知里**没有 vendor_id · 只有昵称** —— 靠昵称反查我方 slug
		slug := VendorSlugForXi8Name(n.Vendor)
		if slug == "" {
			continue // 未映射的昵称 · 跳过（宁可少一条 · 不要归错家）
		}
		at, err := parseXi8Time(n.At.ISO)
		if err != nil {
			continue
		}
		rawMicro := int64(n.PriceFen) * 10_000 // 分 → microunit
		samples = append(samples, ZoneSample{
			VendorID:       slug,
			ProbedAt:       at, // ★ 用通知时刻 · 不是拉取时刻
			Zone:           string(providers.ZoneOf(n.Region)),
			Region:         xi8RegionToOurs(n.Region),
			Available:      n.Stock,
			VendorCurrency: "CNY",
			VendorUnitRaw:  rawMicro,
			OurUnitCredits: rawMicro, // CNY 1:1 到积分
			Source:         "xi8_notif",
		})
	}
	if len(samples) == 0 {
		return 0
	}
	if err := b.zoneStore.InsertZoneBatch(ctx, samples); err != nil {
		b.logger.Warn("xi8: 写通知历史价失败", "err", err)
		return 0
	}
	return len(samples)
}

// parseXi8Time · xi8 iso 字段带 +08:00 · Parse 后 .UTC() 才对。
func parseXi8Time(iso string) (time.Time, error) {
	if iso == "" {
		return time.Time{}, fmt.Errorf("empty iso")
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// xi8RegionToOurs · xi8 用 "us" / "eu" · 我方用 "us-east-1" / "eu-central-1"
// 未来若某家 vendor 有更多 region · 需扩这个映射。
func xi8RegionToOurs(r string) string {
	switch r {
	case "us":
		return "us-east-1"
	case "eu":
		return "eu-central-1"
	default:
		return r
	}
}

func joinRegions(rs []string) string {
	if len(rs) == 0 {
		return ""
	}
	return rs[0] // signals 通常一个批次一个 region · 取第一个
}
