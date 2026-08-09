package decider

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
	"github.com/google/uuid"
)

// Orchestrator 走完一次拉号的 5 步状态推进。
//
// 依赖的窄化在 deps.go；具体 vendor / pool 实现的注入在装配层。
type Orchestrator struct {
	db     *sql.DB
	state  *Store
	vendor VendorClient
	pool   PoolClient
	rates  Rates
	// now / newID 可注入，测试里用来控时钟和 id 生成
	now   func() time.Time
	newID func() string
}

// Config 是装配 Orchestrator 需要的东西。
type Config struct {
	DB     *sql.DB
	State  *Store
	Vendor VendorClient
	Pool   PoolClient
	Rates  Rates
}

func New(cfg Config) *Orchestrator {
	return &Orchestrator{
		db:     cfg.DB,
		state:  cfg.State,
		vendor: cfg.Vendor,
		pool:   cfg.Pool,
		rates:  cfg.Rates,
		now:    func() time.Time { return time.Now().UTC() },
		newID:  uuid.NewString,
	}
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
)

// Pull 走完 initial → reserved → purchasing → purchased → imported → completed。
//
// 崩溃恢复原则：**每步只推进一个字段** + **调 vendor 前必须先落 purchasing**（§2.1）。
// 中途任何步失败都留可恢复的状态，janitor 会接手（recovery.go）。
func (o *Orchestrator) Pull(ctx context.Context, in PullInput) (*PullResult, error) {
	if in.Count < 1 {
		return nil, fmt.Errorf("decider: count 非法: %d", in.Count)
	}

	// ── ① 估价 + 冻结（initial → reserved） ────────────────
	stock, err := o.vendor.Stock(ctx, providers.StockOptions{Zone: nonZeroZone(in.Zone)})
	if err != nil {
		return nil, translateVendorErr(err)
	}
	unitCostHint := stockUnitPrice(stock, in.Zone)
	if unitCostHint <= 0 {
		return nil, ErrNoStock
	}
	if !hasEnoughStock(stock, in.Zone, in.Count) {
		return nil, ErrNoStock
	}

	// 冻结按估价的上限；实扣多退少补
	reserved := Price(unitCostHint, in.Count, o.rates).Total

	clientOrderID, err := newClientOrderID()
	if err != nil {
		return nil, err
	}
	pending := Pending{
		IdempotencyRecordID: in.IdempotencyRecordID,
		PassengerID:         in.PassengerID,
		BusID:               in.BusID,
		TargetGroup:         groupFor(in.BusID, in.PassengerID),
		VendorID:            string(o.vendor.ID()),
		ClientOrderID:       clientOrderID,
		CountRequested:      in.Count,
		ReservedAmount:      reserved,
	}
	pendingID, err := o.state.Create(ctx, pending)
	if err != nil {
		return nil, err
	}

	if err := o.reserveFunds(ctx, pendingID, in.PassengerID, reserved); err != nil {
		return nil, err
	}

	// ── ② 落 purchasing 后调 vendor（reserved → purchasing → purchased） ─────
	//
	// **必须先落 purchasing 再调 vendor**（§2.1 · P0-1）。反过来的话崩在 vendor
	// 调用中途会被 janitor 当 reserved 直接释放 —— 而 vendor 可能已扣款。
	if err := o.state.Advance(ctx, pendingID, StatusReserved, StatusPurchasing); err != nil {
		return nil, err
	}

	purchase, err := o.vendor.Purchase(ctx, providers.PurchaseRequest{
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
	out, err := o.settle(ctx, pendingID, pending, purchase, credIDs)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// reserveFunds 把冻结跟状态推进包成同一个事务。
func (o *Orchestrator) reserveFunds(ctx context.Context, pendingID, passengerID string, amount int64) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := wallet.ReserveTx(ctx, tx, passengerID, amount); err != nil {
		if errors.Is(err, wallet.ErrInsufficientBalance) {
			return ErrInsufficientBalance
		}
		return err
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE pending_purchase SET status = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		string(StatusReserved), formatTime(o.now()), pendingID, string(StatusInitial))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleTransition
	}
	return tx.Commit()
}

// releaseAndCancel 释放冻结并把状态从 from 推进 cancelled_reserve。
//
// **调用条件**：确认 vendor 未扣款。janitor 从 purchasing 走这里前必须先重放 vendor
// 确认 "no such order"（§2.1）—— 否则会把已扣款单当作未扣款释放，我方吃亏。
func (o *Orchestrator) releaseAndCancel(ctx context.Context, pendingID, passengerID string, amount int64, from Status) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := wallet.ReleaseReservedTx(ctx, tx, passengerID, amount); err != nil {
		return err
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

// stockUnitPrice 从快照里取指定区的单价 · 0 = 该区无货
func stockUnitPrice(s *providers.StockSnapshot, zone providers.Zone) int64 {
	for _, z := range s.Zones {
		if zone == "" || z.Zone == zone {
			if z.Available > 0 {
				return z.UnitPrice.Amount
			}
		}
	}
	return 0
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

// VendorStock 直接问底层 vendor 拿库存快照 · api 层的 estimate 端点用。
// nil zone = vendor 默认（91kiro 是 us）。
func (o *Orchestrator) VendorStock(ctx context.Context, zone providers.Zone) (*providers.StockSnapshot, error) {
	return o.vendor.Stock(ctx, providers.StockOptions{Zone: nonZeroZone(zone)})
}

// PriceEstimate 按当前 rates 算一次预估（不下单 · 不动状态）。
// 对外只返 Total / UnitPrice / ServiceFee（其它分层由 Breakdown 私有字段承）。
func (o *Orchestrator) PriceEstimate(unitCost int64, count int) Breakdown {
	return Price(unitCost, count, o.rates)
}
