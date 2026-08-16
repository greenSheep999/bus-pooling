package marketstock

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// Vendor · providers.Vendor 实现 · 让第 7 家跟前 6 家走 orchestrator 同一条链。
//
// **跟前 6 家的唯一差别**（都在 Purchase 里）：
//   - 号已经在 housepool 里（运营导入时进去的）· Purchase 不打上游 API · 从
//     market_stock_item 抢一个 reserved 出来 · Key 字段填 housepool credential id 的字符串
//   - orchestrator 层看到 PurchaseResult.AlreadyInHousepool = true 就跳过 BatchImport ·
//     直接把 kiro_rs_credential_id 交给 settle 落 credential_ledger
//   - settle 里 SellTx 跟 credential_ledger INSERT 同 tx · 状态从 reserved → sold
//
// Stock 数 available 行数 · MaxPerOrder 是当前 available · 无 zone。
type Vendor struct {
	store *Store
	now   func() time.Time
}

// NewVendor · 装配层调 · 传 store 即可
func NewVendor(s *Store) *Vendor {
	return &Vendor{store: s, now: func() time.Time { return time.Now().UTC() }}
}

// ── 基本身份 ─────────────────────────────

func (v *Vendor) ID() providers.VendorID           { return providers.VendorKiroMarket }
func (v *Vendor) ProviderID() providers.ProviderID { return providers.ProviderKiro }

// DisplayName · 我方自营手工池 · 没有上游品牌要防绕单 · 所有档都看这个真名
// （前端 VENDOR_NAME["kiro_market"] 必须同值 · 别让两边漂）
func (v *Vendor) DisplayName() string { return "Kiro Vendor Market - various sources" }

// Capability · 档位不在这里声明 · offers 端点直接读 market_offer 表当前启用行
// （SelectablePlans 留 nil · 见 vendorview/offers.go offersFromMarket）。
func (v *Vendor) Capability() providers.Capability {
	return providers.Capability{
		SupportsIdempotency:   true, // pending_purchase.client_order_id 幂等
		SupportsZones:         false,
		SupportsWebhook:       false,
		WebhookHasSignature:   false,
		SupportsBatchPurchase: true,
		HasWarranty:           false, // 手工上架 · 质保由运营对号池另配
		WarrantyMinutes:       0,
		KeyPayloadShape:       providers.KeyPayloadJustKey, // 号已在池 · Key 字段传 credential id 字符串
		MinPerOrder:           1,
		MaxPerOrder:           500,
		// 两类都能供 · 具体上架哪档看 market_offer 表(enabled=1 的行)
		AccountKinds:    []providers.AccountKind{providers.AccountPersonal, providers.AccountEnterprise},
		SelectablePlans: nil, // 不用 · 档位由 market_offer 表决定
	}
}

// ── 核心：Stock · Purchase · OrderKeys ─────────────────

// Stock · 数 market_stock_item 里 available 的行数（按 kind 聚合）。
// **一律走 general 无区**（手工上架不分区 · 跟其他 vendor 的双区语义不同）。
func (v *Vendor) Stock(ctx context.Context, opts providers.StockOptions) (*providers.StockSnapshot, error) {
	kind := opts.Kind.Normalize()
	avail, err := v.store.AvailableCountByKind(ctx, string(providers.VendorKiroMarket), kind)
	if err != nil {
		return nil, err
	}
	// 单价按"1 个"档取（估价用 · 真实扣按购买数量再落到分档 · 见 Purchase）
	// 拿不到 offer 说明这个 kind 完全没上架 → 单价 0（Stock=0 上层会挡）
	var unitPrice int64
	if o, err := v.store.FindOffer(ctx, string(providers.VendorKiroMarket), kind, ""); err == nil && o != nil {
		unitPrice = o.UnitPriceFor(1)
	}

	return &providers.StockSnapshot{
		VendorID:    providers.VendorKiroMarket,
		ObservedAt:  v.now(),
		Available:   avail,
		MinPerOrder: 1,
		MaxPerOrder: avail, // 手工池的上限就是当前 available
		Zones: []providers.ZoneStock{{
			Zone:      providers.ZoneGeneral,
			Available: avail,
			UnitPrice: providers.Money{Amount: unitPrice, Currency: providers.CurrencyCredit},
		}},
		WarrantyMinutes: 0,
	}, nil
}

// Purchase · 从 market_stock_item 抢 Reserve → 返回一批 KeyPayload。
//
// 关键契约：
//   - Key = kiro_rs_credential_id（字符串形式）· 上层 orchestrator 识别
//     PurchaseResult 里 stockItemIDs meta 就跳过 BatchImport（号已在池）
//   - VendorOrderID = req.ClientOrderID（幂等键复用）· 崩溃恢复靠它+Reserve 的
//     reserved_by_pending 反查已占的号
//   - Paid 按分档单价填 · TotalCost = Σ Paid（跟 provider.PurchaseResult 契约一致）
func (v *Vendor) Purchase(ctx context.Context, req providers.PurchaseRequest) (*providers.PurchaseResult, error) {
	if req.Count <= 0 {
		return nil, fmt.Errorf("marketstock: count 必须 > 0")
	}
	kind := req.Kind.Normalize()

	// **决定用哪个 offer**：
	//   - 有 Plan 传下来 → 精确匹配（enterprise 未来会有 · 个人现在也精确匹配）
	//   - 无 Plan → 只 kind 匹配（vendorview 层做 auto-pick 时才这样）
	//     此时无法精确算价 · 上层应先解析成具体 offer 再调 Purchase
	//     （orchestrator 拉号前先经过 estimate · 那时会带 Plan · 见 Step5d）
	offer, err := v.store.FindOffer(ctx, string(providers.VendorKiroMarket), kind, providers.SubscriptionPlan(""))
	if err != nil {
		return nil, err
	}

	// Reserve · reserved_by_pending 需要 pending 的 id · 但这里拿到的是 ClientOrderID
	// 用 client_order_id 当占用凭据 · 崩溃恢复时反查同样能找到
	items, err := v.store.Reserve(ctx, offer.ID, req.ClientOrderID, req.Count)
	if err != nil {
		return nil, err
	}

	unit := offer.UnitPriceFor(req.Count)
	unitMoney := providers.Money{Amount: unit, Currency: providers.CurrencyCredit}
	keys := make([]providers.KeyPayload, len(items))
	for i, it := range items {
		// Key 字段填 credential id · 上层看到 AlreadyInPool 就用这个 id 直接落 ledger
		keys[i] = providers.KeyPayload{
			VendorKeyID: strconv.FormatUint(it.KiroRSCredentialID, 10),
			Key:         strconv.FormatUint(it.KiroRSCredentialID, 10),
			Paid:        unitMoney,
		}
	}
	total := providers.Money{Amount: unit * int64(req.Count), Currency: providers.CurrencyCredit}

	// 把 stock_item id 放到 Raw · orchestrator 层用它调 SellTx
	raw, _ := json.Marshal(purchaseMeta{
		AlreadyInHousepool: true,
		StockItemIDs:       stockItemIDs(items),
		OfferID:            offer.ID,
		Source:             offer.Source,
		Subscription:       string(offer.Subscription),
	})

	return &providers.PurchaseResult{
		ClientOrderID: req.ClientOrderID,
		VendorOrderID: req.ClientOrderID, // 手工池无 vendor 侧 order · 复用 client_order_id
		Zone:          providers.ZoneGeneral,
		Requested:     req.Count,
		Purchased:     len(items),
		Keys:          keys,
		UnitPrice:     unitMoney,
		TotalCost:     total,
		Remaining:     providers.Money{Amount: 0, Currency: providers.CurrencyCredit},
		Raw:           raw,
	}, nil
}

// purchaseMeta · 塞在 PurchaseResult.Raw 里 · orchestrator 反序列化拿 stock_item id
type purchaseMeta struct {
	AlreadyInHousepool bool     `json:"already_in_housepool"`
	StockItemIDs       []string `json:"stock_item_ids"` // 跟 Keys[] 顺序一致
	OfferID            string   `json:"offer_id"`
	Source             string   `json:"source"`
	// Subscription · 运营上架时选的档 · **手工池的权威源**（不用等号池 Balance 观察）·
	// settle 把它直接落 credential_ledger.subscription
	Subscription string `json:"subscription"`
}

// UnpackMeta · 上层从 PurchaseResult.Raw 里取回 stock_item id + source。
// 返 (nil, nil) 表示这批 PurchaseResult 不是从我方手工池来的（正常 vendor · 走 BatchImport）。
func UnpackMeta(raw []byte) (*Meta, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m purchaseMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil // Raw 是别家 vendor 的响应体 · 不是我方 meta · 静默跳过
	}
	if !m.AlreadyInHousepool {
		return nil, nil
	}
	return &Meta{
		StockItemIDs: m.StockItemIDs,
		OfferID:      m.OfferID,
		Source:       m.Source,
		Subscription: m.Subscription,
	}, nil
}

// Meta · 对 orchestrator 暴露的稳定视图（隔离 purchaseMeta 的内部字段）
type Meta struct {
	StockItemIDs []string
	OfferID      string
	Source       string
	Subscription string // 手工池权威档位（offer 上架时选的）
}

func stockItemIDs(items []ReservedItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.StockItemID
	}
	return out
}

// ── 其余 Vendor 接口方法 · 手工池不适用返 ErrNotSupported ─

// OrderKeys · 补拉走反查 reserved_by_pending · orderID 是 client_order_id
func (v *Vendor) OrderKeys(ctx context.Context, orderID string) (*providers.PurchaseResult, error) {
	items, err := v.store.FindReservedByPending(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, providers.ErrNoStock
	}
	// 补拉时无法确定当时按几个算价 · 用当前分档的"count"档
	offer, err := v.store.FindOffer(ctx, string(providers.VendorKiroMarket),
		providers.AccountPersonal, providers.SubscriptionPlan("")) // 补拉一律先查个人（当前实况）
	unit := int64(0)
	if err == nil && offer != nil {
		unit = offer.UnitPriceFor(len(items))
	}
	unitMoney := providers.Money{Amount: unit, Currency: providers.CurrencyCredit}
	keys := make([]providers.KeyPayload, len(items))
	for i, it := range items {
		keys[i] = providers.KeyPayload{
			VendorKeyID: strconv.FormatUint(it.KiroRSCredentialID, 10),
			Key:         strconv.FormatUint(it.KiroRSCredentialID, 10),
			Paid:        unitMoney,
		}
	}
	total := providers.Money{Amount: unit * int64(len(items)), Currency: providers.CurrencyCredit}
	raw, _ := json.Marshal(purchaseMeta{
		AlreadyInHousepool: true,
		StockItemIDs:       stockItemIDs(items),
		OfferID:            offer.ID,
		Source:             offer.Source,
		Subscription:       string(offer.Subscription),
	})
	return &providers.PurchaseResult{
		ClientOrderID: orderID,
		VendorOrderID: orderID,
		Zone:          providers.ZoneGeneral,
		Requested:     len(items),
		Purchased:     len(items),
		Keys:          keys,
		UnitPrice:     unitMoney,
		TotalCost:     total,
		Raw:           raw,
	}, nil
}

func (v *Vendor) Balance(_ context.Context) (*providers.Balance, error) {
	// 手工池无"我方在 vendor 侧的余额"概念 · 返 0（表示无预付款）
	return &providers.Balance{
		VendorID: providers.VendorKiroMarket,
		Balance:  providers.Money{Amount: 0, Currency: providers.CurrencyCredit},
	}, nil
}

func (v *Vendor) KeyHealth(_ context.Context, _ string) (*providers.KeyHealth, error) {
	// 号已进池 · 死活由 housepool 探针管 · 这里不重复实现
	return nil, providers.ErrNotSupported
}

func (v *Vendor) KeyStats(_ context.Context, _ providers.KeyStatsOptions) (*providers.KeyStatsBatch, error) {
	return nil, providers.ErrNotSupported
}

func (v *Vendor) Redeem(_ context.Context, _ string) (*providers.RedeemResult, error) {
	return nil, providers.ErrNotSupported
}

func (v *Vendor) Usage(_ context.Context, _ []string) (*providers.UsageBatch, error) {
	return nil, providers.ErrNotSupported
}
