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

	// pricing · vendor_pricing 换算规则（docs/10-pricing §1.3）·
	// 展示价换算的兜底路径用（库里 our_unit_credits 还没落时）· nil 走 1:1
	pricing PricingLookup

	// quality · 从 vendor_key 表聚合的号寿命 / 30d 存活率（Task 65）·
	// nil 时 AutoPick 打分退回 aliveRate=50 常数（老行为 · 等价纯价格排序）
	quality *QualityStore

	// marketReader · 手工池货架读取抽象（Step 4）· nil = 不装第 7 家 · Offers 只列前 6 家
	// 实现方 *marketstock.Store · 见 offers.go MarketOfferReader
	marketReader MarketOfferReader

	// planConfig · vendor 档位开关读取(migration 049)· nil 时用 defaultEnabledPlans 兜底
	// 实现方 *PlanConfigStore · 见 offers.go PlanConfigReader
	planConfig PlanConfigReader

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
	// Pricing · vendor_pricing 换算规则 · 传 nil = 展示价兜底按 1:1 算
	Pricing PricingLookup
	// Quality · vendor_key 表聚合的号寿命 / 存活率（Task 65 · 喂 AutoPick 打分）
	// 传 nil = AutoPick 用 aliveRate=50 常数兜底（老行为）
	Quality *QualityStore
	// MarketReader · 我方第 7 家 kiro_market 手工池 · 传 *marketstock.Store · nil = 不装
	MarketReader MarketOfferReader
	// PlanConfig · vendor 档位开关读取 · 传 *PlanConfigStore · nil = 用默认档兜底
	PlanConfig PlanConfigReader
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
		pricing:       cfg.Pricing,
		quality:       cfg.Quality,
		marketReader:  cfg.MarketReader,
		planConfig:    cfg.PlanConfig,
		now:           func() time.Time { return time.Now().UTC() },
	}, nil
}

// Viewer 描述调用者身份，用来做**匿名化** + **是否减免**决策。
//
// Viewer · 请求者视角（docs/10-pricing §2.1 三档定价）
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

// 三档常量（对齐 passenger.tier CHECK · docs/10-pricing §2.1）
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
	//
	// **只当 i18n 兜底** —— 前端优先用 ReasonCode 出本地化文案。后端不知道
	// 调用者的语言 · 这里返中文会让英文用户看到中文（实测 Extract 页 pill）。
	Reason string `json:"reason"`
	// ReasonCode 稳定机器码 · 前端 i18n key（extract:pull-form.upstream.reason.*）
	// 加新理由时**同步加前端词条** · 前端查不到 key 就回落 Reason 原文。
	ReasonCode string `json:"reason_code"`
}

// AutoPick 推荐理由的机器码 · 跟前端 i18n key 一一对应
const (
	ReasonOutOfStock = "out_of_stock" // 全网暂时缺货
	ReasonCheapest   = "cheapest"     // 当前单价最低 · 库存充足
	ReasonMostStock  = "most_stock"   // 当前库存最多 · 单价合理
	ReasonBalanced   = "balanced"     // 库存与单价综合最优
)

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
			// 先按 vendor_pricing 换成积分 · 再进计费栈（USD 家不换会少算 6.8 倍）
			UnitPrice: s.finalUnitPrice(s.baseCredits(ctx, e.VendorID, z.UnitPrice), v),
		})
	}
	return view, nil
}

// PickBestVendor · 内部选 vendor · 返真 vendor id + zone（不脱敏）。
//
// 跟 AutoPick 共用同一套打分（成活率 × 0.6 + (1 - 相对价) × 0.4）· 但**只返内部原文** ·
// 用于 decider.Pull 在 VendorID 空且 preferred 也空时选一家。
//
// zoneHint 传空或 "auto" · 让全 zone 参赛。
// 全网缺货返 ("", "", false) · 上层用 defaultVendor 兜底。
//
// **P4 · 2026-08-14**：老代码 AutoPick 只喂 UI · 从不进下单决策。这个方法让 decider
// 能拿到跟前端 UI 一致的选择 · 用户看到的推荐跟真拉时用的是同一家。
func (s *Service) PickBestVendor(ctx context.Context, zoneHint string) (providers.VendorID, providers.Zone, bool) {
	return s.pickBestInternal(ctx, zoneHint, nil, providers.AccountEnterprise)
}

// PickBestVendorForKind · 按 account kind 选最优 vendor
//
// personal 请求必须走这条 —— 不带 kind 只会看企业池 · 手工池那家永远选不到。
func (s *Service) PickBestVendorForKind(
	ctx context.Context, zoneHint string, kind providers.AccountKind,
) (providers.VendorID, providers.Zone, bool) {
	return s.pickBestInternal(ctx, zoneHint, nil, kind)
}

// PickBestVendorExcluding · 排除若干 vendor 后再选（余额自动切换用）
//
// **首版**：exclude 空 = 老行为；exclude 非空 = 若最优不在 exclude 就返之 ·
// 否则暂返 false（还没做 nth-best 打分器）。上层 orchestrator 收 false = 走 ErrVendorInsufficient
// 兜底 · 不会退化。
//
// **完整 nth-best 实现放**：抽 AutoPick 内部 cand 数组返 · 上层遍历。
func (s *Service) PickBestVendorExcluding(ctx context.Context, zoneHint string, exclude []providers.VendorID) (providers.VendorID, providers.Zone, bool) {
	return s.pickBestInternal(ctx, zoneHint, exclude, providers.AccountEnterprise)
}

// pickBestInternal · PickBestVendor 和 PickBestVendorExcluding 的共用实现 · **不递归**
// (2026-08-15 修 stack overflow · 之前俩公开函数互相调造成无限递归 · 从没跑到过)
func (s *Service) pickBestInternal(
	ctx context.Context, zoneHint string, exclude []providers.VendorID, kind providers.AccountKind,
) (providers.VendorID, providers.Zone, bool) {
	// 复用 AutoPick 的打分逻辑 · 只返 vendor id 不组装 View
	// 简版:调 AutoPick 拿 top1 · 再判 exclude
	//
	// **必须传 wholesale viewer** —— 这里要的是真 vendor_id 去下单 ·
	// retail viewer 会返 anon_id · 拿去 vendorFor() 查不到（老代码靠 kiro_market
	// 恒返真 id 侥幸没炸 · 其他家都是错的）
	view := s.autoPickForKind(ctx, zoneHint, Viewer{Tier: TierWholesale}, kind)
	if view == nil || view.VendorID == "" {
		return "", "", false
	}
	vid := providers.VendorID(view.VendorID)
	// 缺货态 view 会返"暂时缺货" label 但 AliveRate30d 会是 0 · 用它判
	if view.Available <= 0 {
		return "", "", false
	}
	for _, e := range exclude {
		if e == vid {
			return "", "", false
		}
	}
	zn := providers.Zone("")
	if view.Zone != nil && *view.Zone != "" {
		zn = providers.Zone(*view.Zone)
	}
	return vid, zn, true
}

// AutoPick 系统推荐的 vendor + 理由。
//
// 打分：成活率 × 0.6 + (1 - 相对价) × 0.4；成活率 1a 未采集，等价于价低者胜。
// 理由是给乘客看的（cheapest / most_stock / balanced），**不许透 decider 逻辑**。
func (s *Service) AutoPick(ctx context.Context, zoneHint string, v Viewer) *AutoPickView {
	return s.autoPickForKind(ctx, zoneHint, v, providers.AccountEnterprise)
}

// autoPickForKind · AutoPick 的带 kind 版本
//
// **kind 必须传到 Stock** —— 不传只查企业池 · personal 请求会挑到一个 personal
// 库存为 0 的家（手工池那家永远选不到）· 冻结积分后 ErrNoStock。生产实测过。
func (s *Service) autoPickForKind(
	ctx context.Context, zoneHint string, v Viewer, kind providers.AccountKind,
) *AutoPickView {
	entries := s.registry.Enabled()

	type cand struct {
		entry providers.VendorEntry
		snap  *providers.StockSnapshot
		// pickedZone 当前 vendor 里得分最高的那个 zone；无区 vendor 则用 general
		pickedZone providers.ZoneStock
		hasZone    bool
		// credits · pickedZone 单价换成我方积分后的值。
		// **跨 vendor 比价必须用它** —— UnitPrice.Amount 的语义随 Currency 变 ·
		// 拿 raw 直接比会让 USD 家的 "7.35" 跟 credit 家的 "30" 比 · USD 家永远赢
		credits int64
		score   float64
	}

	// 并发拿每家 stock（同样 3s 超时兜底）
	snaps := make([]*providers.StockSnapshot, len(entries))
	var wg sync.WaitGroup
	for i, e := range entries {
		i, e := i, e
		wg.Add(1)
		go func() {
			defer wg.Done()
			snaps[i], _ = s.stockOnceKind(ctx, e.Vendor, kind)
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
				credits: s.baseCredits(ctx, e.VendorID, z.UnitPrice),
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
				Reason: "全网暂时缺货", ReasonCode: ReasonOutOfStock,
			}
		}
		return &AutoPickView{Reason: "全网暂时缺货", ReasonCode: ReasonOutOfStock}
	}

	// 打分：以候选中最高单价做分母做相对价（**用换算后的积分** · 见 cand.credits）
	maxPrice := int64(0)
	for _, c := range cands {
		if p := c.credits; p > maxPrice {
			maxPrice = p
		}
	}
	if maxPrice <= 0 {
		maxPrice = 1
	}
	// **Task 65 · 2026-08-14**：从 vendor_key 表聚合 30d 存活率喂进打分公式 ·
	// 无数据的家降级 50 常数（不误伤 · 新接入 vendor 保 30d 才拿真数据）。
	// 打分公式不变（0.6 存活 + 0.4 价格）· 数据源变了 —— 老代码恒 50 等价纯价格排序 ·
	// 现在能反映"这家最近的号好不好"。
	for i := range cands {
		p := cands[i].credits
		aliveRate := 50.0
		if s.quality != nil {
			if stats, ok, _ := s.quality.Get(ctx, string(cands[i].entry.VendorID)); ok && stats != nil {
				aliveRate = float64(stats.AliveRate30d)
			}
		}
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
		if c.credits < cheapest.credits {
			cheapest = c
		}
		if c.pickedZone.Available > mostStock.pickedZone.Available {
			mostStock = c
		}
	}
	reason, reasonCode := "库存与单价综合最优", ReasonBalanced
	if best.entry.VendorID == cheapest.entry.VendorID {
		reason, reasonCode = "当前单价最低 · 库存充足", ReasonCheapest
	} else if best.entry.VendorID == mostStock.entry.VendorID {
		reason, reasonCode = "当前库存最多 · 单价合理", ReasonMostStock
	}

	label, anon := labelAndAnon(best.entry, v)
	var zonePtr *string
	if best.hasZone && best.pickedZone.Zone != "" {
		z := zoneKey(best.pickedZone.Zone)
		zonePtr = &z
	}

	// Task 65：从 vendor_key 聚合取真实值 · 无数据返 0（前端显示 "-"）
	var avgLifespan int64
	var aliveRate30d int
	if s.quality != nil {
		if stats, ok, _ := s.quality.Get(ctx, string(best.entry.VendorID)); ok && stats != nil {
			avgLifespan = stats.AvgLifespanSeconds
			aliveRate30d = stats.AliveRate30d
		}
	}

	return &AutoPickView{
		VendorLabel: label,
		// 走 visibleVendorID · 非 wholesale 档返 anon_id（原来这里直接返真 id · 漏名）
		VendorID:        visibleVendorID(best.entry.VendorID, v),
		AnonID:          anon,
		Zone:            zonePtr,
		Available:       best.pickedZone.Available,
		UnitPrice:       s.finalUnitPrice(best.credits, v),
		WarrantyMinutes: best.snap.WarrantyMinutes,
		MaxPerOrder:     best.snap.MaxPerOrder,
		MinPerOrder:     best.snap.MinPerOrder,
		// 真实值 · vendor_key 无数据时为 0（前端显示 "-"）
		AvgLifespanSeconds: avgLifespan,
		AliveRate30d:       aliveRate30d,
		Reason:             reason,
		ReasonCode:         reasonCode,
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

// stockOnceKind 带 account kind 打 Stock · Offers() 用这个走对个人/企业池
// 某些 vendor 个人池走独立端点(如 /stock/personal-pool)·不加 kind 会拿到企业池快照
func (s *Service) stockOnceKind(ctx context.Context, v providers.Vendor, kind providers.AccountKind) (*providers.StockSnapshot, error) {
	cctx, cancel := context.WithTimeout(ctx, s.stockTimeout)
	defer cancel()
	return v.Stock(cctx, providers.StockOptions{Kind: kind})
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

// baseCredits · 把 vendor 快照单价换成我方积分 microunit（计费栈的 base）。
//
// **必须先换算再进计费栈** —— `Money.Amount` 的语义由 `Money.Currency` 决定：
// USD 家的 7_350_000 是 7.35 USD 不是 7.35 积分 · 直接当积分喂给 finalUnitPrice
// 会把展示价算成实际的 1/6.8（用户看到的比真实扣费低几倍）。
//
// 换算式跟 Prober 落库 our_unit_credits 时**同一条**（docs/10-pricing §1.3）：
//
//	credits = Amount × credits_per_unit / 1_000_000
//
// credit / CNY 家 credits_per_unit = 1_000_000 · 退化成恒等（pass-through）。
//
// **为什么这里现算而不是读库**：库里 `our_unit_credits` 只有**首个 zone** 的采样值
// （vendor_probe 无 region 列）· 而这里要逐 zone 出价。用同一条换算规则保证跟库里
// 那个值口径一致 · 又能覆盖到其他 zone。
func (s *Service) baseCredits(ctx context.Context, vendorID providers.VendorID, m providers.Money) int64 {
	if m.Amount == 0 {
		return 0
	}
	perUnit := int64(1_000_000)
	if s.pricing != nil {
		if _, cpu := s.pricing.QuoteFor(ctx, string(vendorID)); cpu > 0 {
			perUnit = cpu
		}
	}
	return m.Amount * perUnit / 1_000_000
}

// finalUnitPrice 应用当前 rates 和身份差异。
//
// 计费链见 decisions §8.34（逐层乘）。这里**只对外暴露最终价** ——
// 分层已经在 decider.Breakdown 里私有化了，vendorview 不再回头拆分。
//
// **档次决定免哪层**（docs/10-pricing §2.2）：
//   - retail    · 全套
//   - community · 免 region_markup
//   - wholesale · 免 vendor_markup + region_markup
//
// `single_pull` / `service` 三档都收 —— 别在这里置 0。
// WaiveMarkup 是运营手工豁免（跟档次正交）· 置 0 区域层。
func (s *Service) finalUnitPrice(unit int64, v Viewer) int64 {
	if unit <= 0 {
		return 0
	}
	rates := s.rates
	switch v.Tier {
	case TierWholesale:
		rates.VendorMarkup = 0
		rates.RegionMarkup = 0
	case TierCommunity:
		rates.RegionMarkup = 0
	}
	if v.WaiveMarkup {
		rates.RegionMarkup = 0
	}
	// count=1 让 SinglePull 有机会生效；列表页展示的是"单拉一份"的最终报价
	return decider.Price(unit, 1, rates).UnitPrice
}

// labelAndAnon 按档次出显示名 + 匿名编号。
//
//   - tier=wholesale → 真名（vendor.DisplayName）+ anon 仍返（前端偶尔用它取色/查表）
//   - 其他档          → "AWS-Q Kiro Vendor 0N" + anon
//
// **别用 Invited 判** —— community 档也是 Invited=true · 拿它当门会把真名漏给社群档
// （docs/10-pricing §2.1 定死只 wholesale 可见）。
//
// 1a 简化：AnonID 用 VendorID sha256 前 6 位（稳定编号，前端可用）。
// 前端约定：优先渲染 VendorLabel；VendorID 只用于取色（vendorColor）不直接展示。
func labelAndAnon(e providers.VendorEntry, v Viewer) (label, anon string) {
	anon = anonIDOf(e.VendorID)
	// 我方自营手工池 · 所有档都看真名。
	// 匿名的目的是"别让乘客绕过我方直接找上游买"（decisions §8.20）· 这家的号是
	// 运营自己导进来的 · **没有上游可绕** · 藏名字只会让用户看不懂这是哪来的号。
	if e.VendorID == providers.VendorKiroMarket {
		return e.DisplayName, anon
	}
	if v.canSeeVendorName() {
		return e.DisplayName, anon
	}
	return anonLabelOf(e.VendorID), anon
}

// visibleVendorID · **决定 vendor_id 字段是否泄漏真名**（CLAUDE.md §0.1 硬约束）：
//   - tier=wholesale：返真 vendor_id
//   - 其他档：返 anon_id
//   - kiro_market:恒返真 id —— 我方自营(无上游可绕)· 且前端要认这个 id 出中文名
//
// 所有对外 view struct 的 VendorID 字段**必须**走这个函数拼装。
func visibleVendorID(id providers.VendorID, v Viewer) string {
	if id == providers.VendorKiroMarket {
		return string(id)
	}
	if v.canSeeVendorName() {
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

// VendorIDForAnon · anon → 内部 vendor_id 反查
//
// 匿名端点（前端传 anon_id）要落到真 vendor 表查数据。跟 StatusTrend 里的循环
// 反查同一套逻辑 · 抽出来公开。未找到返 ""。
func (s *Service) VendorIDForAnon(anonID string) string {
	if s == nil || s.registry == nil {
		return ""
	}
	for _, e := range s.registry.Enabled() {
		if anonIDOf(e.Vendor.ID()) == anonID {
			return string(e.Vendor.ID())
		}
	}
	return ""
}

// AnonIDFor · vendor_id → anon_id · 脱敏字段的公开出口
func (s *Service) AnonIDFor(vendorID string) string {
	return anonIDOf(providers.VendorID(vendorID))
}

// AnonLabelFor · vendor_id → 匿名 label
func (s *Service) AnonLabelFor(vendorID string) string {
	return anonLabelOf(providers.VendorID(vendorID))
}

// LabelFor · 按 Viewer 档次返 vendor 显示名 · **对外展示名唯一权威源**
//
//	wholesale → 真名（vendor.DisplayName）
//	其他档    → "AWS-Q Kiro Vendor NN"
//
// 未注册的 vendorID 返 "" —— caller 据此判断"这不是 vendor 名"（activity 的 Source
// 也可能是 bus_id / credential_id，不能一律替换）。
func (s *Service) LabelFor(vendorID string, v Viewer) string {
	if s == nil || vendorID == "" {
		return ""
	}
	e, ok := s.lookupAny(vendorID)
	if !ok {
		return ""
	}
	if v.canSeeVendorName() {
		return e.DisplayName
	}
	return anonLabelOf(e.VendorID)
}

// anonLabelOf 匿名显示名。编号顺序按 CLAUDE.md §1.1 六家列表定义。
func anonLabelOf(id providers.VendorID) string {
	if n, ok := anonIndex[id]; ok {
		return fmt.Sprintf("AWS-Q Kiro Vendor %02d", n)
	}
	return "AWS-Q Kiro Vendor"
}

var anonIndex = map[providers.VendorID]int{
	providers.Vendor91Kiro:     1,
	providers.VendorKiroCEO:    2,
	providers.VendorKiroOOO:    3,
	providers.VendorKiroAppIO:  4,
	providers.VendorKiroAppCC:  5,
	providers.VendorKiroDrop:   6,
	providers.VendorKiroMarket: 7,
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
