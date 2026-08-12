// Package vendorview 是 providers.Registry 的对外封装。
//
// 存在的理由：
//   - api 层只想拿"已经按调用者身份处理好、已经算完最终价"的数据
//   - **分项链和内部 vendor_id 常量不能对外**（CLAUDE.md §0.1 / decisions §8.20）
//   - 每家 vendor 有慢查风险，聚合端点要能容忍单家超时不拖全表
//
// 命名不叫 "vendors" —— 那容易跟 providers 混。它是**视图层**：把 registry 里
// 的原始能力翻译成给前端的、按身份定制的、脱敏后的展示数据。
package vendorview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// Service 是对外的 vendor 视图。api 层拿 Service 调 Stock / Prices / AutoPick / Status 等。
type Service struct {
	registry *providers.Registry
	rates    decider.Rates
	// stockTimeout 单家 vendor Stock 调用的超时。默认 3s。
	// 聚合端点里一家慢不能拖垮整体（默认值兜底：Config 传 0 时用 3s）。
	stockTimeout time.Duration

	// probeStore + probeInterval 供 StatusOverview 用 · nil 时 status 端点返空。
	// 探针本身跑在 Prober 里 · Service 只负责读聚合。
	probeStore    *ProbeStore
	probeInterval time.Duration

	// orderKeyStore 供 StatusOverview + Prices 用（共享 vendor_order/vendor_key）
	// nil 时 backfill 数据字段返空 · 前端展示"数据采集中"
	orderKeyStore *OrderKeyStore

	// now / newCtx 可注入 · 测试时控时钟和取消
	now func() time.Time
}

// Config 装配 Service。
type Config struct {
	Registry     *providers.Registry
	Rates        decider.Rates
	StockTimeout time.Duration
	// ProbeStore / ProbeInterval · 传 nil 就是不启用 status 视图（老部署 / 测试兼容）
	ProbeStore    *ProbeStore
	ProbeInterval time.Duration
	// OrderKeyStore 供 status/prices 读 backfill 数据 · 传 nil = 不启用
	OrderKeyStore *OrderKeyStore
}

// New 建 Service。rates 为零值时零费率（真实环境从后台配置注入）。
func New(cfg Config) (*Service, error) {
	if cfg.Registry == nil {
		return nil, errors.New("vendorview: 缺少 registry")
	}
	to := cfg.StockTimeout
	if to <= 0 {
		to = 3 * time.Second
	}
	probeInterval := cfg.ProbeInterval
	if probeInterval <= 0 {
		probeInterval = 60 * time.Second
	}
	return &Service{
		registry:      cfg.Registry,
		rates:         cfg.Rates,
		stockTimeout:  to,
		probeStore:    cfg.ProbeStore,
		probeInterval: probeInterval,
		orderKeyStore: cfg.OrderKeyStore,
		now:           func() time.Time { return time.Now().UTC() },
	}, nil
}

// Viewer 描述调用者身份，用来做**匿名化** + **是否减免**决策。
//
// Viewer · 请求者视角（docs/18 §2.1 三档定价）
//
// Tier + PassengerID 决定 PricedFor 返啥：
//   - retail    · 匿名 label · 全套分项
//   - community · 匿名 label · 免区域层
//   - wholesale · **真名** · 免 vendor 层 + 免区域层（几乎 pass-through）
//
// **老字段 Invited/WaiveMarkup 保留**做兼容 · 现有 Aggregate/AutoPick 那些路径未切
// PricedFor · 老 view 里的匿名判断仍走 Invited。切完 Step 10 后 Invited 可删。
type Viewer struct {
	Tier        string // retail / community / wholesale · 空视为 retail
	PassengerID string // 查 user_subsidy 减免用
	Invited     bool   // **DEPRECATED** · migration 028 落码前的老开关
	WaiveMarkup bool   // **DEPRECATED**
}

// 三档常量（对齐 passenger.tier CHECK · docs/18 §2.1）
const (
	TierRetail    = "retail"
	TierCommunity = "community"
	TierWholesale = "wholesale"
)

// canSeeVendorName · 只 wholesale 看真名
func (v Viewer) canSeeVendorName() bool {
	return v.Tier == TierWholesale
}

// ── 对外形状 · 独立 struct 不给 registry 的内部 struct 直接出去 ──
// 字段名和形状严格对齐 web/src/types/index.ts。

// AggregateStock 是 GET /api/vendors/stock 的响应形状（对齐 StockSummary）。
type AggregateStock struct {
	TotalAvailable int              `json:"total_available"`
	ByVendor       []VendorStockRow `json:"by_vendor"`
}

// VendorStockRow 聚合视图里的一行 —— 只给"哪家" + "多少"，不给单价。
type VendorStockRow struct {
	// VendorID 内部 id · 前端拿它去 vendorLabel(id, invited) 做匿名化取色
	// 散客视角这个字段走 visibleVendorID · 已被替换为 anon_id ·
	// 只用来查颜色 / 关联再次请求。若担心还要脱敏，后续可增加 AnonID 字段。
	VendorID    string `json:"vendor_id"`
	VendorLabel string `json:"vendor_label"`
	AnonID      string `json:"anon_id"`
	Available   int    `json:"available"`
}

// VendorStockView 是 GET /api/vendors/{vendor_id}/stock 的响应形状（对齐 VendorStock）。
type VendorStockView struct {
	VendorID         string           `json:"vendor_id"`
	VendorLabel      string           `json:"vendor_label"`
	AnonID           string           `json:"anon_id"`
	Currency         string           `json:"currency"`
	WarrantyMinutes  int              `json:"warranty_minutes"`
	MaxPerOrder      int              `json:"max_per_order"`
	MinPerOrder      int              `json:"min_per_order"`
	HoldCapRemaining *int             `json:"hold_cap_remaining"`
	Zones            []VendorZoneView `json:"zones"`
}

type VendorZoneView struct {
	Zone      string `json:"zone"`
	Label     string `json:"label"`
	Enabled   bool   `json:"enabled"`
	Available int    `json:"available"`
	// UnitPrice 已应用当前 rates + 身份差异 —— **不下发原价**（decisions §8.20）
	UnitPrice int64 `json:"unit_price"`
}

// PricesView 是 GET /api/vendors/prices 的响应形状。
// 1a 阶段没有历史轮次数据，返回一个占位（trends 里 days 为空数组），
// 前端已经准备好空状态（fixtures.ts 里生成的是 mock 波形）。
type PricesView struct {
	Trends []VendorPriceTrend `json:"trends"`
	// Notice 让前端能提示"数据还在采集中"（1d 采集完成后清空即可）
	Notice string `json:"notice,omitempty"`
}

type VendorPriceTrend struct {
	VendorID        string          `json:"vendor_id"`
	VendorLabel     string          `json:"vendor_label"`
	AnonID          string          `json:"anon_id"`
	Zone            *string         `json:"zone"`
	Days            []VendorDayView `json:"days"`
	CurrentPrice    int64           `json:"current_price"`
	PriceHigh       int64           `json:"price_high"`
	PriceLow        int64           `json:"price_low"`
	PriceAvg        int64           `json:"price_avg"`
	TotalRounds     int             `json:"total_rounds"`
	AvgRoundsPerDay float64         `json:"avg_rounds_per_day"`
	Change30dPct    float64         `json:"change_30d_pct"`
	NoServiceDays   int             `json:"no_service_days"`
	LongestStreak   int             `json:"longest_streak_days"`
	InStockNow      bool            `json:"in_stock_now"`
}

type VendorDayView struct {
	Date   string       `json:"date"`
	Rounds []RoundEntry `json:"rounds"`
}

type RoundEntry struct {
	Time      string  `json:"time"`
	Zone      *string `json:"zone"`
	UnitPrice int64   `json:"unit_price"`
	KeysCount int     `json:"keys_count"`
}

// HistoryView 是 GET /api/vendors/{vendor_id}/history 的响应形状（对齐 VendorHistory）。
// 1a 数据未采集，返 0 值 + notice。
type HistoryView struct {
	VendorID           string `json:"vendor_id"`
	AvgLifespanSeconds int64  `json:"avg_lifespan_seconds"`
	AliveRate30d       int    `json:"alive_rate_30d"`
	TotalPulled30d     int    `json:"total_pulled_30d"`
	// Notice 让前端能提示"数据还在采集中"
	Notice string `json:"notice,omitempty"`
}

// StatsView 是 GET /api/vendors/stats 的响应形状。
type StatsView struct {
	Stats []VendorStat  `json:"stats"`
	Share []VendorShare `json:"share"`
}

// VendorStat 一家 vendor 的监测行（对齐 VendorStat）。
// 1a 阶段真实数据未接入，全部占位 0 —— 前端已适配 pulls=0 显示 "-"。
type VendorStat struct {
	VendorID           string `json:"vendor_id"`
	VendorLabel        string `json:"vendor_label"`
	AnonID             string `json:"anon_id"`
	Rank               int    `json:"rank"`
	UnitPrice          int64  `json:"unit_price"`
	AvgLifespanSeconds int64  `json:"avg_lifespan_seconds"`
	// effective_cost 前端已注明"仅内部保留，不上 UI"，我方不下发 —— 前端已允许缺省
	AvgCreditsPerCred int64 `json:"avg_credits_per_cred"`
	WarrantyCount     int   `json:"warranty_count"`
	AliveRate         int   `json:"alive_rate"`
	PullsToday        int   `json:"pulls_today"`
	FallbackCount     int   `json:"fallback_count"`
	OutOfStock        bool  `json:"out_of_stock"`
}

type VendorShare struct {
	VendorID string  `json:"vendor_id"`
	Pulls    int     `json:"pulls"`
	Ratio    float64 `json:"ratio"`
}

// AutoPickView 是 GET /api/vendors/auto-pick 的响应形状（对齐 AutoPickResult）。
type AutoPickView struct {
	VendorLabel        string  `json:"vendor_label"`
	VendorID           string  `json:"vendor_id"`
	AnonID             string  `json:"anon_id"`
	Zone               *string `json:"zone"`
	Available          int     `json:"available"`
	UnitPrice          int64   `json:"unit_price"`
	WarrantyMinutes    int     `json:"warranty_minutes"`
	MaxPerOrder        int     `json:"max_per_order"`
	MinPerOrder        int     `json:"min_per_order"`
	AvgLifespanSeconds int64   `json:"avg_lifespan_seconds"`
	AliveRate30d       int     `json:"alive_rate_30d"`
	// Reason 对乘客可见的一句人话（不许透"decider 逻辑"）
	Reason string `json:"reason"`
}

// ── 错误哨兵 ──

// ErrVendorNotFound 单家端点找不到指定 vendor 时返回
var ErrVendorNotFound = errors.New("vendorview: vendor 未找到")

// ── 公开方法 ──

// AggregateStock 汇总所有启用 vendor 的当前库存。
//
// **每家单独超时**：默认 3s。一家慢别拖垮整个接口。
// 慢查/失败的家在结果里 available=0（不吞返回值，前端仍能拿到列表和显示名）。
func (s *Service) AggregateStock(ctx context.Context, v Viewer) *AggregateStock {
	entries := s.registry.Enabled()
	rows := make([]VendorStockRow, len(entries))

	// 并发拉每家 Stock，各自 3s 超时。
	var wg sync.WaitGroup
	for i, e := range entries {
		i, e := i, e
		wg.Add(1)
		go func() {
			defer wg.Done()
			label, anon := labelAndAnon(e, v)
			row := VendorStockRow{
				VendorID:    visibleVendorID(e.VendorID, v),
				VendorLabel: label,
				AnonID:      anon,
			}
			snap, err := s.stockOnce(ctx, e.Vendor)
			if err == nil && snap != nil {
				row.Available = snap.Available
			}
			rows[i] = row
		}()
	}
	wg.Wait()

	total := 0
	for _, r := range rows {
		total += r.Available
	}
	return &AggregateStock{TotalAvailable: total, ByVendor: rows}
}

// VendorStock 单家实时快照。
func (s *Service) VendorStock(ctx context.Context, vendorID string, v Viewer) (*VendorStockView, error) {
	e, ok := s.lookupEnabled(vendorID)
	if !ok {
		return nil, ErrVendorNotFound
	}

	snap, err := s.stockOnce(ctx, e.Vendor)
	if err != nil {
		// 上游暂时不可用不算 404 —— 让 api 层翻译成 502/503。
		return nil, err
	}

	label, anon := labelAndAnon(e, v)
	cap := e.Vendor.Capability()
	view := &VendorStockView{
		VendorID:        visibleVendorID(e.VendorID, v),
		VendorLabel:     label,
		AnonID:          anon,
		Currency:        currencyOfSnapshot(snap),
		WarrantyMinutes: snap.WarrantyMinutes,
		MaxPerOrder:     snap.MaxPerOrder,
		MinPerOrder:     snap.MinPerOrder,
	}
	// 只有部分 vendor 有 hold cap（前端约定没有的家给 null）；1a 从 snapshot 里
	// 暂时没有一个稳定的通道拿到，先给 nil，未来 provider 契约扩后再填。
	_ = cap

	view.Zones = make([]VendorZoneView, 0, len(snap.Zones))
	for _, z := range snap.Zones {
		view.Zones = append(view.Zones, VendorZoneView{
			Zone:      zoneKey(z.Zone),
			Label:     zoneLabel(z.Zone),
			Enabled:   true,
			Available: z.Available,
			UnitPrice: s.finalUnitPrice(z.UnitPrice.Amount, v),
		})
	}
	return view, nil
}

// AutoPick 系统推荐的 vendor + 理由。
//
// 打分：成活率 × 0.6 + (1 - 相对价) × 0.4；成活率 1a 未采集，等价于价低者胜。
// 理由是给乘客看的（cheapest / most_stock / balanced），**不许透 decider 逻辑**。
func (s *Service) AutoPick(ctx context.Context, zoneHint string, v Viewer) *AutoPickView {
	entries := s.registry.Enabled()

	type cand struct {
		entry providers.VendorEntry
		snap  *providers.StockSnapshot
		// pickedZone 当前 vendor 里得分最高的那个 zone；无区 vendor 则用 general
		pickedZone providers.ZoneStock
		hasZone    bool
		score      float64
	}

	// 并发拿每家 stock（同样 3s 超时兜底）
	snaps := make([]*providers.StockSnapshot, len(entries))
	var wg sync.WaitGroup
	for i, e := range entries {
		i, e := i, e
		wg.Add(1)
		go func() {
			defer wg.Done()
			snaps[i], _ = s.stockOnce(ctx, e.Vendor)
		}()
	}
	wg.Wait()

	// 收集候选：库存 > 0 且 zone 匹配
	cands := make([]cand, 0, len(entries))
	for i, e := range entries {
		snap := snaps[i]
		if snap == nil {
			continue
		}
		for _, z := range snap.Zones {
			if z.Available <= 0 {
				continue
			}
			if zoneHint != "" && zoneHint != "auto" && string(z.Zone) != zoneHint {
				continue
			}
			cands = append(cands, cand{
				entry: e, snap: snap,
				pickedZone: z, hasZone: len(snap.Zones) > 1 || z.Zone != "",
			})
		}
	}

	if len(cands) == 0 {
		// 全网缺货：返回空壳让 UI 显示缺货态（对齐 fixtures.ts autoPick 的兜底）
		label, anon := "", ""
		// 拿第一家启用的 vendor 当占位（前端仅取色和显示"暂时缺货"）
		if len(entries) > 0 {
			label, anon = labelAndAnon(entries[0], v)
			return &AutoPickView{
				VendorLabel: label, VendorID: visibleVendorID(entries[0].VendorID, v), AnonID: anon,
				Zone: nonZeroZonePtr(zoneHint), Available: 0,
				Reason: "全网暂时缺货",
			}
		}
		return &AutoPickView{Reason: "全网暂时缺货"}
	}

	// 打分：以候选中最高单价做分母做相对价
	maxPrice := int64(0)
	for _, c := range cands {
		if p := c.pickedZone.UnitPrice.Amount; p > maxPrice {
			maxPrice = p
		}
	}
	if maxPrice <= 0 {
		maxPrice = 1
	}
	for i := range cands {
		p := cands[i].pickedZone.UnitPrice.Amount
		// 成活率 1a 无数据 → 50 常数，等价于纯价格排序
		aliveRate := 50.0
		cands[i].score = aliveRate/100*0.6 + (1-float64(p)/float64(maxPrice))*0.4
	}
	// 稳定排序，score 高优先；同 score 按 VendorID 字典序（可复现）
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].entry.VendorID < cands[j].entry.VendorID
	})
	best := cands[0]

	// 推荐理由 · 三档：cheapest / most_stock / balanced
	cheapest := cands[0]
	mostStock := cands[0]
	for _, c := range cands {
		if c.pickedZone.UnitPrice.Amount < cheapest.pickedZone.UnitPrice.Amount {
			cheapest = c
		}
		if c.pickedZone.Available > mostStock.pickedZone.Available {
			mostStock = c
		}
	}
	reason := "库存与单价综合最优"
	if best.entry.VendorID == cheapest.entry.VendorID {
		reason = "当前单价最低 · 库存充足"
	} else if best.entry.VendorID == mostStock.entry.VendorID {
		reason = "当前库存最多 · 单价合理"
	}

	label, anon := labelAndAnon(best.entry, v)
	var zonePtr *string
	if best.hasZone && best.pickedZone.Zone != "" {
		z := zoneKey(best.pickedZone.Zone)
		zonePtr = &z
	}

	return &AutoPickView{
		VendorLabel:     label,
		VendorID:        string(best.entry.VendorID),
		AnonID:          anon,
		Zone:            zonePtr,
		Available:       best.pickedZone.Available,
		UnitPrice:       s.finalUnitPrice(best.pickedZone.UnitPrice.Amount, v),
		WarrantyMinutes: best.snap.WarrantyMinutes,
		MaxPerOrder:     best.snap.MaxPerOrder,
		MinPerOrder:     best.snap.MinPerOrder,
		// 1a 无历史数据，寿命/成活率给 0，前端会显示 "-"
		AvgLifespanSeconds: 0,
		AliveRate30d:       0,
		Reason:             reason,
	}
}

// Prices 1a 阶段占位：返回启用 vendor 的空日历 + 提示。
// 1d 采集轮次数据后，用真实历史填充 Days 即可。
func (s *Service) Prices(_ context.Context, zoneHint string, days int, v Viewer) *PricesView {
	if days <= 0 {
		days = 30
	}
	entries := s.registry.Enabled()
	trends := make([]VendorPriceTrend, 0, len(entries))
	today := s.now()

	for _, e := range entries {
		label, anon := labelAndAnon(e, v)
		emptyDays := make([]VendorDayView, days)
		for i := 0; i < days; i++ {
			d := today.AddDate(0, 0, -(days - 1 - i))
			emptyDays[i] = VendorDayView{
				Date: d.Format("2006-01-02"), Rounds: []RoundEntry{},
			}
		}
		var zonePtr *string
		if zoneHint != "" && zoneHint != "auto" {
			z := zoneKey(providers.Zone(zoneHint))
			zonePtr = &z
		}
		trends = append(trends, VendorPriceTrend{
			VendorID: string(e.VendorID), VendorLabel: label, AnonID: anon,
			Zone: zonePtr, Days: emptyDays,
			// 无历史 → 汇总全 0；前端已适配 in_stock_now=false 走空态
		})
	}
	return &PricesView{
		Trends: trends,
		Notice: "历史价格数据还在采集中",
	}
}

// History 1a 阶段占位：返回 0 值 + notice。
func (s *Service) History(_ context.Context, vendorID string) (*HistoryView, error) {
	if _, ok := s.lookupEnabled(vendorID); !ok {
		// 找不到 vendor 才是 404；采集未完成不是 404
		if _, exists := s.lookupAny(vendorID); !exists {
			return nil, ErrVendorNotFound
		}
	}
	return &HistoryView{VendorID: vendorID, Notice: "历史统计数据还在采集中"}, nil
}

// Stats 1a 阶段占位：返回 6 家启用 vendor 的空行 + 空占比。
func (s *Service) Stats(_ context.Context, v Viewer) *StatsView {
	entries := s.registry.Enabled()
	stats := make([]VendorStat, 0, len(entries))
	share := make([]VendorShare, 0, len(entries))
	for i, e := range entries {
		label, anon := labelAndAnon(e, v)
		stats = append(stats, VendorStat{
			VendorID:    string(e.VendorID),
			VendorLabel: label,
			AnonID:      anon,
			Rank:        i + 1,
			OutOfStock:  true,
		})
		share = append(share, VendorShare{VendorID: string(e.VendorID)})
	}
	return &StatsView{Stats: stats, Share: share}
}

// ── 内部工具 ──

// stockOnce 拿一次 stock 快照，带独立超时。
// **不返 partial 数据** —— 出错时返 nil，上层自己决定 fallback。
func (s *Service) stockOnce(ctx context.Context, v providers.Vendor) (*providers.StockSnapshot, error) {
	cctx, cancel := context.WithTimeout(ctx, s.stockTimeout)
	defer cancel()
	return v.Stock(cctx, providers.StockOptions{})
}

func (s *Service) lookupEnabled(id string) (providers.VendorEntry, bool) {
	for _, e := range s.registry.Enabled() {
		if string(e.VendorID) == id {
			return e, true
		}
	}
	return providers.VendorEntry{}, false
}

// LookupVendor · 拿 vendor 实例（包括未启用的·webhook receiver 用 · 停用 vendor 仍
// 会有历史事件到达 · 应该继续接住并落 event log）· 返 (nil, false) = 未注册。
func (s *Service) LookupVendor(vendorID string) (providers.Vendor, bool) {
	if s.registry == nil {
		return nil, false
	}
	for _, e := range s.registry.All() {
		if string(e.VendorID) == vendorID {
			return e.Vendor, true
		}
	}
	return nil, false
}

func (s *Service) lookupAny(id string) (providers.VendorEntry, bool) {
	for _, e := range s.registry.All() {
		if string(e.VendorID) == id {
			return e, true
		}
	}
	return providers.VendorEntry{}, false
}

// finalUnitPrice 应用当前 rates 和身份差异。
//
// 计费链见 decisions §8.34（逐层乘）。这里**只对外暴露最终价** ——
// 分层已经在 decider.Breakdown 里私有化了，vendorview 不再回头拆分。
//
// Viewer.Invited=true 或 Viewer.WaiveMarkup=true → 跳过 RegionMarkup / SinglePull /
// Capability（对散客生效的分项）· Service（我方服务费）仍然算。
// 简化实现：直接用 decider.Price(unit, 1, rates)；invited 时把这几层 rate 置 0。
func (s *Service) finalUnitPrice(unit int64, v Viewer) int64 {
	if unit <= 0 {
		return 0
	}
	rates := s.rates
	if v.Invited || v.WaiveMarkup {
		rates.RegionMarkup = 0
		rates.SinglePull = 0
		rates.Capability = 0
	}
	// count=1 让 SinglePull 有机会生效；非邀请用户看到"一份"的最终报价。
	return decider.Price(unit, 1, rates).UnitPrice
}

// labelAndAnon 按身份出显示名 + 匿名编号。
//
//   - Invited=true → 真名（vendor.DisplayName）+ anon 仍返（前端偶尔用它取色/查表）
//   - Invited=false → "AWS-Q Kiro Vendor 0N" + anon
//
// 1a 简化：AnonID 用 VendorID sha256 前 6 位（稳定编号，前端可用）。
// 前端约定：优先渲染 VendorLabel；VendorID 只用于取色（vendorColor）不直接展示。
func labelAndAnon(e providers.VendorEntry, v Viewer) (label, anon string) {
	anon = anonIDOf(e.VendorID)
	if v.Invited {
		return e.DisplayName, anon
	}
	return anonLabelOf(e.VendorID), anon
}

// visibleVendorID · **决定 vendor_id 字段是否泄漏真名**（CLAUDE.md §8.20 硬约束）：
//   - Invited=true：返真 vendor_id
//   - Invited=false：返 anon_id
//
// 所有对外 view struct 的 VendorID 字段**必须**走这个函数拼装。
func visibleVendorID(id providers.VendorID, v Viewer) string {
	if v.Invited {
		return string(id)
	}
	return anonIDOf(id)
}

// anonIDOf 稳定短 id · 无邀请码用户暴露一个短 anon 而不是真 vendor_id 常量
// —— 未来若前端下发彻底切换到 anon，这里就是那道墙。
func anonIDOf(id providers.VendorID) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:])[:6]
}

// anonLabelOf 匿名显示名。编号顺序按 CLAUDE.md §1.1 六家列表定义。
func anonLabelOf(id providers.VendorID) string {
	if n, ok := anonIndex[id]; ok {
		return fmt.Sprintf("AWS-Q Kiro Vendor %02d", n)
	}
	return "AWS-Q Kiro Vendor"
}

var anonIndex = map[providers.VendorID]int{
	providers.Vendor91Kiro:    1,
	providers.VendorKiroCEO:   2,
	providers.VendorKiroOOO:   3,
	providers.VendorKiroAppIO: 4,
	providers.VendorKiroAppCC: 5,
	providers.VendorKiroDrop:  6,
}

func zoneLabel(z providers.Zone) string {
	switch z {
	case providers.ZoneUS:
		return "美国区"
	case providers.ZoneEU:
		return "欧洲区"
	case providers.ZoneGeneral:
		return "全区"
	}
	return "全区"
}

// zoneKey 归一化 zone 到前端 TS 联合类型 "us" | "eu"。
//
// 内部 providers.ZoneGeneral 值是 "general"，但前端 Zone 类型只允许 us / eu
// （见 web/src/types/index.ts）· 无区 vendor 在 fixtures 里也把 zone 设成 "us"。
// 我方跟着这个约定：general → us。前端类型扩了才需要改回来。
func zoneKey(z providers.Zone) string {
	switch z {
	case providers.ZoneEU:
		return "eu"
	case providers.ZoneUS, providers.ZoneGeneral, "":
		return "us"
	}
	return string(z)
}

func currencyOfSnapshot(s *providers.StockSnapshot) string {
	// 前端约定：credits 或 cny_usd 两种。1a 已接的 vendor 都是 credits；
	// USD 结算的 vendor 接入时按 Balance.Currency 判定。
	if s != nil && len(s.Zones) > 0 && s.Zones[0].UnitPrice.Currency == providers.CurrencyUSD {
		return "cny_usd"
	}
	return "credits"
}

func nonZeroZonePtr(z string) *string {
	if z == "" || z == "auto" {
		return nil
	}
	out := zoneKey(providers.Zone(z))
	return &out
}
