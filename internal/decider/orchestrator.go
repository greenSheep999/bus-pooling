package decider

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/stockwatch"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
	"github.com/google/uuid"
)

// Orchestrator 走完一次拉号的 5 步状态推进。
//
// 依赖的窄化在 deps.go；具体 vendor / pool 实现的注入在装配层。
//
// 支持多 vendor：`vendors` 是 vendor_id → client 的 registry。Pull 时按 PullInput.VendorID
// 选取；recovery 用 pending_purchase.vendor_id 选取。传统单 vendor 场景可只装 `defaultVendor`
// （对应 Config.Vendor · 走 fallback）。
type Orchestrator struct {
	db            *sql.DB
	state         *Store
	vendors       map[providers.VendorID]VendorClient
	defaultVendor VendorClient
	pool          PoolClient
	rates         Rates
	// pricing · vendor 报价换算规则（vendor_pricing 表 · 1b P1-2A）·
	// nil = 全 vendor 走 fallback（1a 兼容·CNY 1:1）
	pricing PricingLookup
	// credits · vendor_probe.our_unit_credits 读取器（docs/18 §1.4）·
	// 估价基准优先读它 · nil / 无数据时退回按本轮快照现算
	credits CreditsLookup
	// ratesResolver · surcharge_rule 实时求值（1b P1-2B）· nil = 用 o.rates（env 兜底）
	ratesResolver RatesResolver
	// limits · 拉号并发 + 数量区间（config.pull · §8.35 #18）· 零值 = 不限
	limits Limits
	// enqueuer · 抢号链缺货挂单（stockwatch.Watcher）· nil = 缺货直接失败（老行为）
	// 只在 auto 模式（未指定 vendor）缺货时挂 · 见 maybeEnqueueOnNoStock
	enqueuer StockEnqueuer
	// picker · 自动选 vendor（P4）· nil = 走 defaultVendor（老行为 · 测试默认）
	picker VendorPicker
	// balanceChecker · 上游余额预检（P5）· nil = 不预检
	balanceChecker BalanceChecker
	// now / newID 可注入，测试里用来控时钟和 id 生成
	now   func() time.Time
	newID func() string
}

// StockEnqueuer · 缺货挂单的抽象 · 避免 decider → stockwatch 硬依赖（也便于测试 mock）。
//
// 实现方是 stockwatch.Watcher.Enqueue · 装配层在 main.go 注入。
// **反方向的 Firer 接口在 stockwatch 里定义** —— 两个接口各在自己的消费侧 ·
// 不构成循环 import（stockwatch 不 import decider）。
type StockEnqueuer interface {
	Enqueue(ctx context.Context, p stockwatch.EnqueueParams) (string, error)
}

// PricingLookup 是 orchestrator 拿 vendor 换算规则的抽象接口。
// 装配层传 pricing.Store.GetOrFallback 的适配 · 测试 mock 简单。
type PricingLookup interface {
	QuoteFor(ctx context.Context, vendorID providers.VendorID) VendorQuote
}

// VendorPicker · 自动选 vendor 的抽象（避免 decider → vendorview 硬依赖）·
// 实现方 vendorview.Service.PickBestVendor · 装配层注入。
//
// **P4 · 2026-08-14**：Pull 里 VendorID 空 · 用户偏好也空时 · 调这个选一家 ·
// 老代码直接走 defaultVendor · 用户看到 UI 说"推荐 A 家" · 真拉却总走 default · 割裂。
type VendorPicker interface {
	// PickBestVendor 返 (vendorID, zone, ok)。全网缺货 ok=false · 上层 defaultVendor 兜底。
	PickBestVendor(ctx context.Context, zoneHint string) (providers.VendorID, providers.Zone, bool)
	// PickBestVendorExcluding · 排除若干 vendor 后再选（余额不够切下一家用）·
	// 全排除后无 ok=false · 上层判 ErrVendorInsufficient。
	PickBestVendorExcluding(ctx context.Context, zoneHint string, exclude []providers.VendorID) (providers.VendorID, providers.Zone, bool)
}

// BalanceChecker · 上游余额预检的抽象（避免 decider → vendorbalance 硬依赖）。
// 实现方 vendorbalance.Cache · 装配层注入。
//
// **P5 · 2026-08-14**：Pull 拉号前查缓存 · 余额<预估总额直接返 ErrVendorInsufficient ·
// 不发下单请求 · 避免 vendor 侧返 insufficient_balance 的被动失败。
type BalanceChecker interface {
	// Enough 判定 vendor 余额是否够 estCredits · ok=false 表示不够 · remain 是剩余。
	// 未 poll 过 / USD 家未换算币种 · 一律返 (true, _) 保守放行（不误伤）。
	Enough(vendorID providers.VendorID, estCredits int64) (ok bool, remain int64)
}

// CreditsLookup · 读 vendor_probe / vendor_probe_zone 的积分（docs/18 §1.4）·
// 实现方 pricing.ProbeCredits · 装配层注入 · nil = 退回按快照现算（冷启动 / 测试）
//
// zone 参数：内部已归一（"us" / "eu" / ""）· 空 = 跨 zone 找该 vendor 最近一条。
type CreditsLookup interface {
	LatestCredits(ctx context.Context, vendorID string, zone string) (int64, time.Time, bool)
}

// VendorQuote · 换算规则的最小视图（跟 internal/pricing.VendorQuote 对齐但避免包循环）
type VendorQuote struct {
	QuoteCurrency     string // CNY | USD | credit
	CreditsPerUnit    int64  // microunit · 1 单位 vendor 报价 = X microunit 我方积分
	VendorSurchargeBp int64
}

// Config 是装配 Orchestrator 需要的东西。
type Config struct {
	DB *sql.DB
	// State 是 pending_purchase 状态存储
	State *Store
	// Vendor · 单 vendor 场景（跟以前一样·会作为 defaultVendor 装配）·
	// **推荐**多 vendor 场景走 Vendors map · 装配层从 providers.Registry 转
	Vendor VendorClient
	// Vendors 多 vendor 注册表 · Pull(PullInput{VendorID: X}) 时按 X 选。
	// 若 map 中找不到 · fallback 到 Vendor（default） · 都没有报 ErrUnknownVendor。
	Vendors map[providers.VendorID]VendorClient
	Pool    PoolClient
	Rates   Rates
	// Pricing · vendor_pricing 表的换算规则（1b P1-2A）·nil = 全走 fallback
	Pricing PricingLookup
	// Credits · vendor_probe.our_unit_credits 读取器（docs/18 §1.4）·
	// nil = 退回按快照现算（测试 / 冷启动）
	Credits CreditsLookup
	// RatesResolver · surcharge_rule 表的实时求值（1b P1-2B）· nil = 用 env Rates
	RatesResolver RatesResolver
	// Limits · 拉号并发 + 数量区间上限（config.pull · decisions §8.35 #18）
	// 零值 = 全不限（老装配 / 测试兼容）
	Limits Limits
	// Enqueuer · 抢号链缺货挂单 · nil = 缺货直接失败退款（老行为 · 测试默认）
	Enqueuer StockEnqueuer
	// Picker · 自动选 vendor（VendorID 空 + preferred 也空时用）· nil = 走 defaultVendor（老行为）
	Picker VendorPicker
	// BalanceChecker · 上游余额预检（P5）· nil = 不预检（老行为 · 测试默认）
	BalanceChecker BalanceChecker
}

func New(cfg Config) *Orchestrator {
	vendors := make(map[providers.VendorID]VendorClient)
	for id, v := range cfg.Vendors {
		vendors[id] = v
	}
	if cfg.Vendor != nil {
		if _, ok := vendors[cfg.Vendor.ID()]; !ok {
			vendors[cfg.Vendor.ID()] = cfg.Vendor
		}
	}
	return &Orchestrator{
		db:            cfg.DB,
		state:         cfg.State,
		vendors:       vendors,
		defaultVendor: cfg.Vendor,
		pool:          cfg.Pool,
		rates:         cfg.Rates,
		pricing:       cfg.Pricing,
		credits:       cfg.Credits,
		ratesResolver: cfg.RatesResolver,
		limits:        cfg.Limits,
		enqueuer:      cfg.Enqueuer,
		picker:         cfg.Picker,
		balanceChecker: cfg.BalanceChecker,
		now:            func() time.Time { return time.Now().UTC() },
		newID:          uuid.NewString,
	}
}

// SetBalanceChecker · 装配后补设 · 跟 SetPicker 同样解构造环。
func (o *Orchestrator) SetBalanceChecker(c BalanceChecker) {
	if o == nil {
		return
	}
	o.balanceChecker = c
}

// resolveRates · 1b P1-2B · 从 surcharge_rule 引擎按上下文求 Rates ·
// 无 resolver 时 fallback env Rates（1a 兼容）。
func (o *Orchestrator) resolveRates(ctx context.Context, rc RateContext) Rates {
	if o.ratesResolver == nil {
		return o.rates
	}
	return o.ratesResolver.Resolve(ctx, rc)
}

// quoteFor · 拿 vendor 的换算规则 · nil pricing 或 vendor 未配走 fallback（CNY 1:1）
func (o *Orchestrator) quoteFor(ctx context.Context, vendorID providers.VendorID) VendorQuote {
	if o.pricing == nil {
		return VendorQuote{QuoteCurrency: "CNY", CreditsPerUnit: 1_000_000}
	}
	return o.pricing.QuoteFor(ctx, vendorID)
}

// unitCreditsFor · 拿这一轮的**估价基准**（我方积分 microunit）。
//
// **优先读库**（docs/18 §1.4 · 机制 A）：`vendor_probe.our_unit_credits` 是入库时
// 一次换算好的权威积分 · 下游只读结果 · 不再各自拿汇率反推。
//
// 读不到才按本轮快照现算（冷启动 · 探针还没跑完第一轮 · 新接入 vendor）——
// **不能硬失败**：这个值只用于冻结估价和 vendor 侧上限 · 实扣走
// `settle` 里的 `purchase.TotalCost`（vendor 权威值）· 为了估价把整条拉号链停掉不值当。
//
// 返回的 fromDB 只用于打点区分来源。
func (o *Orchestrator) unitCreditsFor(
	ctx context.Context, vendorID providers.VendorID, zone providers.Zone, snapshotPrice providers.Money,
) (credits int64, fromDB bool) {
	if o.credits != nil {
		// 归一 · 上游传的可能是 "us-east-1" 或 "美国区" · 侧表存的是 zone 名（"us"/"eu"）
		z := string(providers.ZoneOf(string(zone)))
		if c, _, ok := o.credits.LatestCredits(ctx, string(vendorID), z); ok && c > 0 {
			return c, true
		}
	}
	return o.convertSnapshotPrice(ctx, vendorID, snapshotPrice), false
}

// convertSnapshotPrice · 冷启动兜底 · 按 vendor_pricing 规则换算本轮快照单价。
//
// 换算式跟 Prober 落库时**同一条**（docs/18 §1.3）：
//
//	credits = Amount × credits_per_unit / 1_000_000
//
// credit / CNY 家 credits_per_unit = 1_000_000 · 退化成恒等（pass-through）。
func (o *Orchestrator) convertSnapshotPrice(
	ctx context.Context, vendorID providers.VendorID, m providers.Money,
) int64 {
	if m.Amount == 0 {
		return 0
	}
	perUnit := int64(1_000_000)
	if q := o.quoteFor(ctx, vendorID); q.CreditsPerUnit > 0 {
		perUnit = q.CreditsPerUnit
	}
	return m.Amount * perUnit / 1_000_000
}

// vendorFor 从注册表选 vendor · id 空回落到 defaultVendor（向后兼容）。
// **未注册的 vendor 返 ErrUnknownVendor** —— api 层要在校验时挡·别让请求跑到这里报错。
func (o *Orchestrator) vendorFor(id providers.VendorID) (VendorClient, error) {
	if id == "" {
		if o.defaultVendor == nil {
			return nil, ErrUnknownVendor
		}
		return o.defaultVendor, nil
	}
	if v, ok := o.vendors[id]; ok {
		return v, nil
	}
	if o.defaultVendor != nil {
		return o.defaultVendor, nil
	}
	return nil, ErrUnknownVendor
}

// KnownVendors 列出已装配 vendor id · api 层用来做请求 vendor 校验。
func (o *Orchestrator) KnownVendors() []providers.VendorID {
	out := make([]providers.VendorID, 0, len(o.vendors))
	for id := range o.vendors {
		out = append(out, id)
	}
	return out
}

// PullInput 是发起一次拉号需要的信息。
//
// 上层已经跑过 strategy.CanPull 校验（余额 / 上限 / 单价），并拿到 wallet 里
// 当时的余额估算 —— 那些不在这里重做。这里只关心「能不能真的花钱把号拉回来」。
type PullInput struct {
	PassengerID string
	// BusID 空 = 单独拉号（进 record group）
	BusID string
	Count int
	// Zone 空 = vendor 默认
	Zone providers.Zone
	// IdempotencyRecordID 前面幂等层已建好的 idempotency_record.id
	IdempotencyRecordID string
	// VendorID 请求指定 vendor · 空 = 用 defaultVendor（1a 兼容）·
	// 1b 起 api 层从 strategy / request 决定后传进来
	VendorID providers.VendorID
	// ClientOrderID 覆盖内部生成的 vendor 幂等键 · 空 = 自动生成（正常路径）。
	//
	// **只给抢号链 fire 用**（stockwatch）：挂单落库时就定了 client_order_id ·
	// fire 时必须复用同一个 · 否则"上一次 fire 其实买到了但返回超时 · 回退 watching
	// 后再 fire"会在 vendor 侧变成两单（重复扣款）。传同一个 key · vendor 侧幂等
	// 返回上次那批（09-transactions §2）。
	ClientOrderID string
	// MaxUnitPrice 生效的单价上限（microunit · 已经是全局跟车级取严后的结果 ·
	// 见 strategy.decide）· 0 = 不限。
	//
	// **decider 不重新判上限** —— api 层 strategy.CanPull 已经判过。这里带进来只为
	// 缺货挂单时存进 stock_watcher · fire 时能继续守住同一个上限（涨价保护）。
	MaxUnitPrice int64
}

// PullResult 是**对外**结果（跟 05-api-contract §5 的 POST /me/pull 响应一致）。
// 内部分项不出这个 struct（CLAUDE.md §0.1）。
type PullResult struct {
	PullRoundID      string
	VendorID         string
	Purchased        int
	CredentialIDs    []string
	UnitPrice        int64
	ServiceFee       int64
	TotalDebit       int64
	BalanceRemaining int64
}

// 明确的对外错误。api 层按 errors.Is 映射到 HTTP code。
var (
	ErrNoStock             = errors.New("decider: 上游暂无库存")
	ErrRateLimited         = errors.New("decider: 被上游限流")
	ErrPurchaseCap         = errors.New("decider: 达上游持有上限")
	ErrInsufficientBalance = errors.New("decider: 积分不足")
	// ErrNeedManual 号池导入反复失败或崩在 purchasing 的无幂等 vendor · 需人工
	ErrNeedManual = errors.New("decider: 需人工处理")
	// ErrPartialFill vendor 只成交了一部分。**这不是错**，只是让上层知道差额已退回
	ErrPartialFill = errors.New("decider: 部分成交（差额已退回）")
	// ErrUnknownVendor · 请求的 vendor 未装配（api 层应先校验挡·防走到这里）
	ErrUnknownVendor = errors.New("decider: 未知 vendor")
	// ErrVendorInsufficient · 上游 vendor 余额不足（P5 · 预检拦下 · 不发下单请求）
	// **区分于 ErrInsufficientBalance** —— 那个是我方乘客积分不足 · 这个是我方在 vendor 侧的钱不够
	ErrVendorInsufficient = errors.New("decider: 上游 vendor 余额不足")
	// ErrInitiatorInsufficient · 发起人付不起自己那份分摊 · 整轮失败
	// （不能让其他成员替他垫 · decisions §8.18）
	ErrInitiatorInsufficient = errors.New("decider: 余额不足")
	// ErrNoPayableMember · 车里没有能付款的活跃成员（全挂起 / 全余额不足）
	ErrNoPayableMember = errors.New("decider: 车里没有能分摊的成员")
)

// Pull 走完 initial → reserved → purchasing → purchased → imported → completed。
//
// 崩溃恢复原则：**每步只推进一个字段** + **调 vendor 前必须先落 purchasing**（§2.1）。
// 中途任何步失败都留可恢复的状态，janitor 会接手（recovery.go）。
// SetPicker · 装配后补设 VendorPicker · 解决构造环。
//
// **为什么需要**：vendorview.Service 用同一批 vendor registry 才能选 · 而 registry
// 在 buildDecider 里构造。若把 vendorSvc 塞进 decider.Config · 又得先建 vendorSvc ·
// 而 vendorSvc 又要 registry —— 跟 stockwatch 一样构造环。装配顺序：
//
//	orch := decider.New(Config{Picker: nil, ...})
//	vendorSvc := vendorview.New(...)  // 用同 registry
//	orch.SetPicker(vendorSvc)
//
// 只在 main.go 装配时调一次 · 之后不变。
func (o *Orchestrator) SetPicker(p VendorPicker) {
	if o == nil {
		return
	}
	o.picker = p
}

func (o *Orchestrator) Pull(ctx context.Context, in PullInput) (*PullResult, error) {
	if in.Count < 1 {
		return nil, fmt.Errorf("decider: count 非法: %d", in.Count)
	}
	// 数量区间校验（config.pull.min_count / max_count · §8.35 #18）
	// 放最前面 —— 超区间根本不该占 vendor 查询和冻结的开销
	if err := o.limits.checkCountRange(in.Count); err != nil {
		return nil, err
	}
	// **P4 · AutoPick 进 decider**（2026-08-14）：VendorID 空时先问 picker（比价+库存综合选）·
	// picker 返 false 或未装配才走 defaultVendor 兜底。
	// 好处：用户看到 UI 说"推荐 A 家" · 真拉时用的就是 A 家（老代码割裂：UI 显示 A · 实拉走 default）。
	// 顺带把 picker 选的 zone 也用上（缺货挂单和 stock 请求都受益）。
	if in.VendorID == "" && o.picker != nil {
		if pv, pz, ok := o.picker.PickBestVendor(ctx, string(in.Zone)); ok {
			in.VendorID = pv
			// zone 空时用 picker 的（用户没显式指定就跟推荐一致）
			if in.Zone == "" {
				in.Zone = pz
			}
		}
	}
	vendor, err := o.vendorFor(in.VendorID)
	if err != nil {
		return nil, err
	}
	// 并发限流（config.pull.max_concurrent_* · §8.35 #18）
	// 在**建 pending_purchase 之前**查 —— 这时候数出来的是别人的在飞数
	if err := o.limits.checkConcurrency(
		ctx, o.db, in.PassengerID, string(vendor.ID())); err != nil {
		return nil, err
	}

	// ── ① 估价 + 冻结（initial → reserved） ────────────────
	stock, err := vendor.Stock(ctx, providers.StockOptions{Zone: nonZeroZone(in.Zone)})
	if err != nil {
		return nil, translateVendorErr(err)
	}
	rawUnitPrice, ok := stockUnitPriceMoney(stock, in.Zone)
	if !ok || rawUnitPrice.Amount <= 0 {
		// auto 模式挂单等补货（decisions §11.15）· 挂上也照样返 ErrNoStock ——
		// 这一轮确实没拿到号 · api 层照常告诉用户"暂无库存"。补到货后 fire 会
		// 走一轮新的 Pull · 号直接进 group（用户在"我的号"里看到）。
		o.maybeEnqueueOnNoStock(ctx, in, vendor.ID())
		return nil, ErrNoStock
	}
	if !hasEnoughStock(stock, in.Zone, in.Count) {
		o.maybeEnqueueOnNoStock(ctx, in, vendor.ID())
		return nil, ErrNoStock
	}
	// 估价基准 · 优先按 zone 读 vendor_probe_zone.our_unit_credits · 精确到区（docs/18 §1.4）·
	// 读不到才按本轮快照现算（冷启动兜底）。实扣不看这个值 —— 走 settle 里 vendor 的 TotalCost。
	unitCostHint, _ := o.unitCreditsFor(ctx, vendor.ID(), in.Zone, rawUnitPrice)

	// **单价上限硬拦（积分口径 · 2026-08-14 P2 修）**：
	//
	// 老代码只把 MaxUnitPrice 折成 vendorCap（vendor 币种）传给 adapter 的 max_total_cny ·
	// **但只有其中一家 vendor 原生支持这个参数** · 其他 5 家 adapter 直接无视 —— 涨价保护形同虚设。
	//
	// 这里在积分口径下再硬拦一次：unitCostHint 是我方权威积分单价（vendor_probe_zone.our_unit_credits）·
	// 超过用户设的 MaxUnitPrice 就直接返 LimitError · 不发下单请求 · 也不冻结钱包。
	//
	// **为什么在这里做而不在 canpull**：canpull 阶段 API 层没传 UnitPriceHint（恒 0）·
	// 判不了。到 decider 里才拿到真实积分单价 · 是护栏的最后一道防线（真钱在这里出去）。
	if priceCapExceeded(in.MaxUnitPrice, unitCostHint) {
		return nil, &strategy.LimitError{
			Kind:    strategy.LimitUnitPrice,
			Cap:     in.MaxUnitPrice,
			Current: unitCostHint,
		}
	}

	// 冻结按估价的上限；实扣多退少补
	// 1b P1-2B · 按本次拉号上下文实时求 Rates（surcharge_rule 表） · nil resolver 走 env
	zoneStr := ""
	if z := nonZeroZone(in.Zone); z != nil {
		zoneStr = string(*z)
	}
	rc := RateContext{
		VendorID: string(vendor.ID()),
		Zone:     zoneStr,
		Count:    in.Count,
		// PassengerInvited · 1c 加乘客 profile 传入 · 1b 保守 false
	}
	rates := o.resolveRates(ctx, rc)
	reserved := Price(unitCostHint, in.Count, rates).Total

	// **P5 · 上游余额预检**（2026-08-14）：查缓存 · 余额不够直接拒 · 不发下单请求。
	//
	// 老代码没做 —— 上游没钱只能等 vendor 返 insufficient_balance 被动失败 · 用户体验差 ·
	// 我方账本也没预警。缓存 5min 一 poll · Enough 未 poll 过或 USD 家未换算币种保守放行
	// （不误伤 · 让 vendor 自己拒）· 只在真判定不够时拦。
	if o.balanceChecker != nil {
		if ok, remain := o.balanceChecker.Enough(vendor.ID(), reserved); !ok {
			// ：当前 vendor 余额不够 · 尝试切下一家。
			// 只当 in.VendorID 空时切（auto 模式）· 用户显式指定的不换（尊重意图）。
			// 装了 picker 才切 · 切不到就走老逻辑返 ErrVendorInsufficient。
			//
			// **不重算 reserved**：切换后新 vendor 的 unitCostHint 可能不同 · 但严格重算
			// 意味着大改主流程（rates / rawUnitPrice 都要跟着走）· 首版**保守用同一 reserved**
			// 判 · 相当于"我方账户能覆盖当前价 · 换家至少不比当前贵才行"—— 这在同 zone 内多家
			// 差价不大（1% 内 · docs 实测）时正确率够高。
			if o.picker != nil && in.VendorID == "" {
				excluded := []providers.VendorID{vendor.ID()}
				if pv, pz, ok := o.picker.PickBestVendorExcluding(ctx, string(in.Zone), excluded); ok {
					// 切换后重新走一遍 · 但为了避免死循环 · 用尾递归退化成再拉一次 Pull
					// **注意**：这里返 SwitchedVendor 让上层看到 · 简单起见用直接 return
					newIn := in
					newIn.VendorID = pv
					if in.Zone == "" {
						newIn.Zone = pz
					}
					return o.Pull(ctx, newIn) // ← 递归一次 · 新 vendor 会再判 balance
				}
			}
			return nil, fmt.Errorf("%w: 预估需 %d microunit · 余 %d",
				ErrVendorInsufficient, reserved, remain)
		}
	}

	// 涨价保护上限 · 用 vendor_pricing 的换算规则把积分上限折回 vendor 币种。
	// 表里币种跟快照币种对不上时不设（配错的表会把上限放大几倍 · 见 quoteCurrencyMatches）
	var vendorCap *providers.Money
	if q := o.quoteFor(ctx, vendor.ID()); quoteCurrencyMatches(q.QuoteCurrency, rawUnitPrice) {
		vendorCap = vendorMaxTotal(in.MaxUnitPrice, in.Count, rawUnitPrice, q.CreditsPerUnit)
	}

	// 抢号链 fire 传现成的（挂单时已定 · 复用保证 vendor 侧幂等）· 其他路径自动生成
	clientOrderID := in.ClientOrderID
	if clientOrderID == "" {
		var err error
		clientOrderID, err = newClientOrderID()
		if err != nil {
			return nil, err
		}
	}
	pending := Pending{
		IdempotencyRecordID: in.IdempotencyRecordID,
		PassengerID:         in.PassengerID,
		BusID:               in.BusID,
		TargetGroup:         groupFor(in.BusID, in.PassengerID),
		VendorID:            string(vendor.ID()),
		ClientOrderID:       clientOrderID,
		CountRequested:      in.Count,
		ReservedAmount:      reserved,
	}
	pendingID, err := o.state.Create(ctx, pending)
	if err != nil {
		return nil, err
	}

	// 多人车在这里按 share_pct 分摊冻结 · 返回的方案给 settle 复用
	reservePlan, err := o.reserveFunds(
		ctx, pendingID, in.PassengerID, in.BusID, reserved, in.Count)
	if err != nil {
		return nil, err
	}

	// ── ② 落 purchasing 后调 vendor（reserved → purchasing → purchased） ─────
	//
	// **必须先落 purchasing 再调 vendor**（§2.1 · P0-1）。反过来的话崩在 vendor
	// 调用中途会被 janitor 当 reserved 直接释放 —— 而 vendor 可能已扣款。
	if err := o.state.Advance(ctx, pendingID, StatusReserved, StatusPurchasing); err != nil {
		return nil, err
	}

	purchase, err := vendor.Purchase(ctx, providers.PurchaseRequest{
		Count:         in.Count,
		ClientOrderID: clientOrderID,
		Zone:          nonZeroZone(in.Zone),
		// 涨价保护 · 把用户的**积分**上限换回 vendor 报价币种（部分 vendor 原生支持 ·
		// 不支持的 adapter 忽略这个字段）。见 vendorMaxTotal 说明。
		MaxTotal: vendorCap,
	})
	if err != nil {
		// 崩在这里的**不能**回 reserved 释放 —— vendor 可能已扣款。留在 purchasing，janitor 接手。
		_ = o.state.AdvanceWith(ctx, pendingID, StatusPurchasing, StatusPurchasing,
			Fields{Error: err.Error()})
		return nil, translateVendorErr(err)
	}
	if purchase.Purchased == 0 {
		// vendor 明确说 0 成交：安全释放冻结
		_ = o.releaseAndCancel(ctx, pendingID, in.PassengerID, reserved, StatusPurchasing)
		return nil, ErrNoStock
	}

	if err := o.state.AdvanceWith(ctx, pendingID, StatusPurchasing, StatusPurchased,
		Fields{VendorOrderID: purchase.VendorOrderID}); err != nil {
		return nil, err
	}

	// ── ③ 号入池（purchased → imported） ─────────────────────
	credIDs, err := o.importToPool(ctx, pending.TargetGroup, purchase)
	if err != nil {
		_ = o.state.AdvanceWith(ctx, pendingID, StatusPurchased, StatusNeedManual,
			Fields{Error: err.Error()})
		return nil, ErrNeedManual
	}
	if err := o.state.Advance(ctx, pendingID, StatusPurchased, StatusImported); err != nil {
		return nil, err
	}

	// ── ④ 落账 + 落库 + 完成（imported → completed） ─────────
	out, err := o.settle(ctx, pendingID, pending, purchase, credIDs, reservePlan)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// reserveFunds 把冻结跟状态推进包成同一个事务。
//
// **多人车按 share_pct 分摊冻结**（1c · decisions §8.18）：
//   - 单独拉号（busID 空）/ 单人车 → 全冻发起人（老行为不变）
//   - 多人车 → 按 planSplit 的方案逐人冻 · 被跳过的人 skipped_count +1
//
// 分摊方案落 pending_purchase.reserve_split_json —— settle 和 janitor 恢复
// 都要知道"该退给谁多少"，只存总额没法按人释放。
//
// 返回实际生效的分摊方案（settle 复用它落账·避免重算时余额已变导致口径漂移）。
func (o *Orchestrator) reserveFunds(
	ctx context.Context, pendingID, passengerID, busID string, amount int64, keys int,
) (SplitPlan, error) {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return SplitPlan{}, err
	}
	defer func() { _ = tx.Rollback() }()

	plan, err := o.buildReservePlan(ctx, tx, passengerID, busID, amount, keys)
	if err != nil {
		return SplitPlan{}, err
	}

	// 逐人冻结
	for _, part := range plan.Participants {
		if part.Amount <= 0 {
			continue
		}
		if err := wallet.ReserveTx(ctx, tx, part.PassengerID, part.Amount); err != nil {
			if errors.Is(err, wallet.ErrInsufficientBalance) {
				// planSplit 已经按同一事务内的余额快照筛过·走到这儿说明并发扣走了。
				// 发起人不足 → 整轮失败；其他人不足 → 也整轮失败（这轮方案已作废·
				// 让用户重试拿新方案·比在这里悄悄改分摊安全）
				return SplitPlan{}, ErrInsufficientBalance
			}
			return SplitPlan{}, err
		}
	}

	// 被跳过的成员 skipped_count +1（§8.26 · 连续 3 次自动挂起）
	for _, sk := range plan.Skipped {
		if sk.Reason != "insufficient_balance" {
			continue // suspended 的不重复计数
		}
		if err := bumpSkippedTx(ctx, tx, busID, sk.PassengerID, o.now()); err != nil {
			return SplitPlan{}, err
		}
	}

	splitJSON, err := json.Marshal(plan.SplitMap())
	if err != nil {
		return SplitPlan{}, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE pending_purchase
		   SET status = ?, updated_at = ?, reserve_split_json = ?
		 WHERE id = ? AND status = ?`,
		string(StatusReserved), formatTime(o.now()), string(splitJSON),
		pendingID, string(StatusInitial))
	if err != nil {
		return SplitPlan{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return SplitPlan{}, ErrStaleTransition
	}
	if err := tx.Commit(); err != nil {
		return SplitPlan{}, err
	}
	return plan, nil
}

// buildReservePlan 决定这轮谁付多少。
//
// 单独拉号 / 单人车走"全归发起人"的快路径 —— 不查成员表·省一次 IO·
// 也保证 1a 语义完全不变（回归安全）。
func (o *Orchestrator) buildReservePlan(
	ctx context.Context, tx *sql.Tx,
	passengerID, busID string, amount int64, keys int,
) (SplitPlan, error) {
	soloPlan := SplitPlan{
		Participants: []Participant{{PassengerID: passengerID, Amount: amount, Keys: keys}},
		Total:        amount,
	}
	if busID == "" {
		return soloPlan, nil
	}
	members, err := loadBusMembersForSplit(ctx, tx, busID)
	if err != nil {
		return SplitPlan{}, err
	}
	if len(members) <= 1 {
		return soloPlan, nil
	}
	plan, err := planSplit(members, passengerID, amount, keys)
	if err != nil {
		if errors.Is(err, ErrInitiatorInsufficient) {
			return SplitPlan{}, ErrInsufficientBalance
		}
		return SplitPlan{}, err
	}
	return plan, nil
}

// bumpSkippedTx 记一次"因余额不足本次跳过"· 连续到 3 次自动挂起（§8.26）。
// 充值后归零由 wallet 那边处理（乘客充值时清计数）。
func bumpSkippedTx(
	ctx context.Context, tx *sql.Tx, busID, passengerID string, now time.Time,
) error {
	const suspendAfter = 3
	if _, err := tx.ExecContext(ctx, `
		UPDATE bus_member
		   SET skipped_count = skipped_count + 1,
		       last_skipped_at = ?,
		       status = CASE WHEN skipped_count + 1 >= ? THEN 'suspended' ELSE status END
		 WHERE bus_id = ? AND passenger_id = ? AND left_at IS NULL`,
		formatTime(now), suspendAfter, busID, passengerID); err != nil {
		return fmt.Errorf("decider: 记跳过次数: %w", err)
	}
	return nil
}

// releaseAndCancel 释放冻结并把状态从 from 推进 cancelled_reserve。
//
// **调用条件**：确认 vendor 未扣款。janitor 从 purchasing 走这里前必须先重放 vendor
// 确认 "no such order"（§2.1）—— 否则会把已扣款单当作未扣款释放，我方吃亏。
// releaseAndCancel 释放冻结 + 转 cancelled_reserve。
//
// **多人车按人退**（1c）：从 pending_purchase.reserve_split_json 读"谁冻了多少"·
// 逐人释放。读不到（老数据 / 单人）时退回"全退发起人"的老语义。
//
// 退错人比退少更严重 —— 会让 A 的钱进 B 的账。所以只信落库的 split·不重算 share_pct
// （中途成员可能变过）。
func (o *Orchestrator) releaseAndCancel(ctx context.Context, pendingID, passengerID string, amount int64, from Status) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 读落库的分摊方案（同事务内读·跟释放原子）
	var splitJSON string
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(reserve_split_json, '') FROM pending_purchase WHERE id = ?`,
		pendingID).Scan(&splitJSON); err != nil {
		return fmt.Errorf("decider: 读分摊方案: %w", err)
	}
	split := decodeReserveSplit(splitJSON)

	if len(split) == 0 {
		// 单人 / 老数据 · 全退发起人
		if err := wallet.ReleaseReservedTx(ctx, tx, passengerID, amount); err != nil {
			return err
		}
	} else {
		// 多人 · 逐人退各自冻的那份
		ids := make([]string, 0, len(split))
		for id := range split {
			ids = append(ids, id)
		}
		sort.Strings(ids) // 确定顺序·便于复现问题
		for _, id := range ids {
			if split[id] <= 0 {
				continue
			}
			if err := wallet.ReleaseReservedTx(ctx, tx, id, split[id]); err != nil {
				return fmt.Errorf("decider: 释放冻结(%s): %w", id, err)
			}
		}
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE pending_purchase SET status = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		string(StatusCancelledReserve), formatTime(o.now()), pendingID, string(from))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleTransition
	}
	return tx.Commit()
}

// ── 工具 ────────────────────────────────────────────

func newClientOrderID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("decider: 生成 client_order_id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func nonZeroZone(z providers.Zone) *providers.Zone {
	if z == "" {
		return nil
	}
	return &z
}

func groupFor(busID, passengerID string) string {
	if busID != "" {
		return housepool.BusGroup(busID)
	}
	return housepool.RecordGroup(passengerID)
}

// vendorMaxTotal 把用户的**积分**单价上限换回 vendor 报价币种的总价上限。
//
// **为什么要换算**：用户填的上限是"我方积分/个"（`strategy.decide` 已取过全局跟车级
// 更严的那个）· vendor 的涨价保护参数用的是**它自己的币种**。直接把积分数传过去会把上限设错几倍。
//
// **换算走 vendor_pricing 同一条规则的逆式**（docs/18 §1.3）——
// 入库时 `credits = raw × credits_per_unit / 1_000_000` · 这里反过来：
//
//	vendor 侧单价上限 = maxUnitPrice × 1_000_000 / credits_per_unit
//
// 不再用"本轮快照两个量等比映射"（老实现）—— 那个依赖 unitCostHint 必须来自同一次快照 ·
// 现在估价基准优先读库（可能是上一轮探针的值）· 等比映射的前提就不成立了。
//
// 返 nil 的四种情况（都表示"不设保护" · adapter 会跳过这个字段）：
//   - 用户没设上限（maxUnitPrice <= 0）
//   - count <= 0（不该发生 · 防御）
//   - credits_per_unit <= 0（换算规则缺失 · 宁可不设也不要设错）
//   - **quote 币种跟快照币种不一致** —— 说明 vendor_pricing 没配对（例：表里记 CNY ·
//     vendor 实际报 USD）· 这时换出来的数字会挂错币种标签 · 宁可不设
//
// **只有原生支持的 vendor 会用它** · 其他 adapter 忽略 ·
// 我方仍有 `strategy.decide` 那层护栏挡着 · 所以返 nil 不会失去保护。
func vendorMaxTotal(
	maxUnitPrice int64, count int, snapshotPrice providers.Money, creditsPerUnit int64,
) *providers.Money {
	if maxUnitPrice <= 0 || count <= 0 || creditsPerUnit <= 0 {
		return nil
	}
	// credit / CNY 家 creditsPerUnit = 1_000_000 · 逆式退化成恒等
	vendorUnitCap := maxUnitPrice * 1_000_000 / creditsPerUnit
	if vendorUnitCap <= 0 {
		return nil
	}
	return &providers.Money{
		Amount:   vendorUnitCap * int64(count),
		Currency: snapshotPrice.Currency,
	}
}

// quoteCurrencyMatches · vendor_pricing 记的币种跟快照实际报的币种对不对得上。
//
// 对不上说明表没配对 · 换算会挂错币种标签（例：USD 家表里记 CNY · 算出来的数字
// 按 CNY 传给 vendor · vendor 当 USD 读 → 上限放大 6.8 倍 · 涨价保护形同虚设）。
//
// `credit` 和 `CNY` 视为同一族（我方积分口径 1:1 · docs/18 §1.3）。
func quoteCurrencyMatches(quoteCurrency string, snapshot providers.Money) bool {
	if snapshot.Currency == "" || quoteCurrency == "" {
		// 任一侧没标币种 · 无从校验 · 交给调用方的其他护栏
		return true
	}
	norm := func(c string) string {
		if c == providers.CurrencyCredit || c == "CNY" {
			return "credit"
		}
		return c
	}
	return norm(quoteCurrency) == norm(snapshot.Currency)
}

// stockUnitPrice 从快照里取指定区的单价 · 0 = 该区无货（**deprecated · 1a 兼容**）
// 新代码用 stockUnitPriceMoney · 拿到带 Currency 的完整 Money 才能做换算。
func stockUnitPrice(s *providers.StockSnapshot, zone providers.Zone) int64 {
	m, ok := stockUnitPriceMoney(s, zone)
	if !ok {
		return 0
	}
	return m.Amount
}

// stockUnitPriceMoney 从快照里取指定区的**带币种**单价。ok=false 时该区无货。
func stockUnitPriceMoney(s *providers.StockSnapshot, zone providers.Zone) (providers.Money, bool) {
	for _, z := range s.Zones {
		if zone == "" || z.Zone == zone {
			if z.Available > 0 {
				return z.UnitPrice, true
			}
		}
	}
	return providers.Money{}, false
}

func hasEnoughStock(s *providers.StockSnapshot, zone providers.Zone, want int) bool {
	if zone == "" {
		return s.Available >= want
	}
	for _, z := range s.Zones {
		if z.Zone == zone {
			return z.Available >= want
		}
	}
	return false
}

// translateVendorErr 把 provider sentinel 翻译到 decider 的对外错误。
func translateVendorErr(err error) error {
	switch {
	case errors.Is(err, providers.ErrNoStock):
		return ErrNoStock
	case errors.Is(err, providers.ErrRateLimited):
		return ErrRateLimited
	case errors.Is(err, providers.ErrPurchaseCapReached):
		return ErrPurchaseCap
	case errors.Is(err, providers.ErrInsufficientFunds):
		// vendor 侧我方余额不足 —— 对乘客来说是"服务暂不可用"，不透上游细节
		return fmt.Errorf("decider: 服务暂时不可用")
	}
	return err
}

// VendorStock 直接问指定 vendor 拿库存快照 · api 层的 estimate 端点用。
// vendorID 空 = defaultVendor（1a 兼容）· nil zone = vendor 默认（部分 vendor 默认限区）。
func (o *Orchestrator) VendorStock(ctx context.Context, vendorID providers.VendorID, zone providers.Zone) (*providers.StockSnapshot, error) {
	v, err := o.vendorFor(vendorID)
	if err != nil {
		return nil, err
	}
	return v.Stock(ctx, providers.StockOptions{Zone: nonZeroZone(zone)})
}

// PriceEstimate 按当前 rates 算一次预估（不下单 · 不动状态）。
// 对外只返 Total / UnitPrice / ServiceFee（其它分层由 Breakdown 私有字段承）。
func (o *Orchestrator) PriceEstimate(unitCost int64, count int) Breakdown {
	return Price(unitCost, count, o.rates)
}

// priceCapExceeded · 单价上限判据 · 抽出来是为了可单测（P2 · 2026-08-14）
//
// 语义：MaxUnitPrice==0 = 不设 · unitCost==0 = 未知不拦 · 严格 > 才拦
func priceCapExceeded(maxUnitPrice, unitCost int64) bool {
	return maxUnitPrice > 0 && unitCost > 0 && unitCost > maxUnitPrice
}
