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
	// ratesResolver · surcharge_rule 实时求值（1b P1-2B）· nil = 用 o.rates（env 兜底）
	ratesResolver RatesResolver
	// limits · 拉号并发 + 数量区间（config.pull · §8.35 #18）· 零值 = 不限
	limits Limits
	// now / newID 可注入，测试里用来控时钟和 id 生成
	now   func() time.Time
	newID func() string
}

// PricingLookup 是 orchestrator 拿 vendor 换算规则的抽象接口。
// 装配层传 pricing.Store.GetOrFallback 的适配 · 测试 mock 简单。
type PricingLookup interface {
	QuoteFor(ctx context.Context, vendorID providers.VendorID) VendorQuote
}

// VendorQuote · 换算规则的最小视图（跟 internal/pricing.VendorQuote 对齐但避免包循环）
type VendorQuote struct {
	QuoteCurrency    string // CNY | USD | credit
	CreditsPerUnit   int64  // microunit · 1 单位 vendor 报价 = X microunit 我方积分
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
	// RatesResolver · surcharge_rule 表的实时求值（1b P1-2B）· nil = 用 env Rates
	RatesResolver RatesResolver
	// Limits · 拉号并发 + 数量区间上限（config.pull · decisions §8.35 #18）
	// 零值 = 全不限（老装配 / 测试兼容）
	Limits Limits
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
		ratesResolver: cfg.RatesResolver,
		limits:        cfg.Limits,
		now:           func() time.Time { return time.Now().UTC() },
		newID:         uuid.NewString,
	}
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

// convertToMicroCredits · 把 vendor 快照里的单价换成我方积分 microunit。
//
// vendor 报价（Money.Amount 是 microunit 但语义按 Money.Currency 决定）：
//   - Currency=credit / CNY → 直接是 microunit 积分（1 credit = 1 积分）·pass-through
//   - Currency=USD → 需要按 CreditsPerUnit 换算（例：5 USD × 7 CNY/USD = 35 积分）
//
// 幅度控制：只允许 USD 家走真换算 · 别的币种保持 pass-through·避免误换算把 CNY 家的号价乘 7 倍。
func (o *Orchestrator) convertToMicroCredits(ctx context.Context, vendorID providers.VendorID, unitPrice providers.Money) int64 {
	if unitPrice.Currency == providers.CurrencyUSD {
		q := o.quoteFor(ctx, vendorID)
		// unitPrice.Amount 是 USD microunit（例 5 USD = 5_000_000）·
		// q.CreditsPerUnit 是"每单位 USD 对应多少积分 microunit"（例 7_000_000）
		// 结果 = unitPrice / 1_000_000 × CreditsPerUnit = unitPrice × CreditsPerUnit / 1_000_000
		return unitPrice.Amount * q.CreditsPerUnit / 1_000_000
	}
	// CNY / credit / 未指定 · 认为已是积分 microunit
	return unitPrice.Amount
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
func (o *Orchestrator) Pull(ctx context.Context, in PullInput) (*PullResult, error) {
	if in.Count < 1 {
		return nil, fmt.Errorf("decider: count 非法: %d", in.Count)
	}
	// 数量区间校验（config.pull.min_count / max_count · §8.35 #18）
	// 放最前面 —— 超区间根本不该占 vendor 查询和冻结的开销
	if err := o.limits.checkCountRange(in.Count); err != nil {
		return nil, err
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
		return nil, ErrNoStock
	}
	if !hasEnoughStock(stock, in.Zone, in.Count) {
		return nil, ErrNoStock
	}
	// **1b P1-2A** · 按 vendor_pricing 换算（USD 家 → 积分 · CNY/credit 家 pass-through）
	unitCostHint := o.convertToMicroCredits(ctx, vendor.ID(), rawUnitPrice)

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

	clientOrderID, err := newClientOrderID()
	if err != nil {
		return nil, err
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
