package vendorview

// offers.go · Offer matrix 端点（docs/24 §3 · Step 4）
//
// GET /api/vendors/offers · 返 vendor × account_kind × subscription × zone 的**当前**货架。
// 前端提取页拿这一份数据同时决定:
//   1. category tab 数字（每档合计 available）
//   2. vendor 下拉可选项 + 每 vendor 每 category 的 supported/available/缺货分离
//   3. subscription 下拉合法档位（哪些 (kind, plan) 组合当前可买）
//   4. 数量分档单价（前端预估费用直接用）
//
// **为什么单端点**（docs/24 §3 单一数据源）：
//   老方案 /vendors/stats + /vendors/{id}/stock + /vendors/auto-pick 三份数据·
//   三份各自快照·会互相漂移。前端切 tab / vendor / plan 时看到"数字打架"。
//   Offer matrix 一次拉齐 · 前端所有联动都从同一份算。

import (
	"context"
	"sync"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// MarketOfferReader · vendorview 读手工池货架的抽象 · 避免硬依赖 marketstock 包。
//
// 装配层传 *marketstock.Store（它实现 ListOffers + AvailableCount）。nil 允许 —— 未
// 装配手工池时 Offers() 只返 registry 里前 6 家的 offer。
type MarketOfferReader interface {
	ListOffers(ctx context.Context) ([]MarketOffer, error)
	AvailableCount(ctx context.Context, offerID string) (int, error)
}

// MarketOffer · marketstock.Offer 的视图（不 import marketstock 包）
type MarketOffer struct {
	ID           string
	VendorID     string
	AccountKind  providers.AccountKind
	Subscription providers.SubscriptionPlan
	PriceBands   []providers.QtyPriceBand
	Enabled      bool
	Source       string
}

// ── 对外形状 · 严格对齐前端 types.ts ────────────────────

// OffersView · GET /api/vendors/offers 响应
type OffersView struct {
	Vendors []VendorOfferRow `json:"vendors"`
}

// VendorOfferRow · 一家 vendor 的全 category 视图
type VendorOfferRow struct {
	VendorID    string                                     `json:"vendor_id"`
	VendorLabel string                                     `json:"vendor_label"`
	AnonID      string                                     `json:"anon_id"`
	Categories  map[providers.AccountKind]CategoryOfferRow `json:"categories"`
}

// CategoryOfferRow · 一家 vendor × 一种 kind 的可用性
//
// **supported vs available 严格分开**（docs/24 §3 · UI 三态区分）：
//
//	supported=false           → "该 vendor 不提供"（disabled · 灰）
//	supported=true, avail=0   → "暂时缺货"（disabled · 但显示）
//	supported=true, avail>0   → 可选 · 显示数量
type CategoryOfferRow struct {
	Supported bool `json:"supported"`
	// Available 该 kind 下所有 offer 的总可用数（跨 subscription 聚合）
	Available int `json:"available"`
	// Offers 各订阅档的可买项 · 前端下拉从这个数组渲染
	// 空数组不代表 supported=false —— 可能 supported=true 只是当前无货架配置
	Offers []OfferItem `json:"offers"`
}

// OfferItem · 一条具体可买货
type OfferItem struct {
	// OfferID 手工池路径有 · registry vendor 无（vendor_id + kind + plan 已能唯一定位）
	OfferID      string                     `json:"offer_id,omitempty"`
	Subscription providers.SubscriptionPlan `json:"subscription"`
	// Zone "" = 无区（手工池 · 部分 vendor personal 池）· "us" / "eu" = 有区
	Zone      string `json:"zone,omitempty"`
	Available int    `json:"available"`
	// UnitPrice 单价（microunit · 已按 tier 档位算好）· 前端预估费用直接用
	// 分档时用 count=1 那档展示（前端切 count 会重算）
	UnitPrice int64 `json:"unit_price"`
	// PriceBands 数量分档全表 · 前端切数量后重算单价（Upper=0 = 及以上）
	// 前 6 家 vendor 无分档时是空数组 · UnitPrice 是唯一价
	PriceBands []providers.QtyPriceBand `json:"price_bands,omitempty"`
	// Source 号的提供方（"这号是谁提的" · 落 credential_ledger.source）· 前端可选展示
	Source string `json:"source,omitempty"`
}

// ── Service 方法 ───────────────────────────────────────

// Offers · GET /api/vendors/offers 处理器 · 组装完整 matrix。
//
// 数据源分两条：
//   - 前 6 家 registry vendor · Stock(kind) 拿快照 · Capability.SupportsKind 判 supported
//   - 第 7 家 kiro_market · marketReader.ListOffers · AvailableCount 精确到 offer
//
// 每家 vendor 一份 Stock 调用**并发** · 3s 超时兜底（跟 AggregateStock 一致 · 一家慢
// 不拖垮整表）。
func (s *Service) Offers(ctx context.Context, v Viewer) *OffersView {
	entries := s.registry.Enabled()
	rows := make([]VendorOfferRow, len(entries))

	// 并发查每家 · 前 6 家 Stock(kind) · 第 7 家从 marketReader
	var wg sync.WaitGroup
	for i, e := range entries {
		i, e := i, e
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows[i] = s.buildVendorRow(ctx, e, v)
		}()
	}
	wg.Wait()

	return &OffersView{Vendors: rows}
}

// buildVendorRow · 单家 vendor 的 CategoryOfferRow 矩阵
func (s *Service) buildVendorRow(ctx context.Context, e providers.VendorEntry, v Viewer) VendorOfferRow {
	label, anon := labelAndAnon(e, v)
	row := VendorOfferRow{
		VendorID:    visibleVendorID(e.VendorID, v),
		VendorLabel: label,
		AnonID:      anon,
		Categories:  make(map[providers.AccountKind]CategoryOfferRow, 2),
	}

	// 两个 kind 都要给（前端 §4.1 要求缺货 tab 也显示）
	for _, kind := range []providers.AccountKind{providers.AccountEnterprise, providers.AccountPersonal} {
		cell := CategoryOfferRow{
			Supported: e.Vendor.Capability().SupportsKind(kind),
		}
		if cell.Supported {
			cell.Offers = s.offersForVendor(ctx, e, kind)
			for _, o := range cell.Offers {
				cell.Available += o.Available
			}
		}
		row.Categories[kind] = cell
	}
	return row
}

// offersForVendor · 单家 vendor + 单个 kind 的具体 offer 列表
//
// 手工池路径:marketReader.ListOffers 过滤 · 每 offer 一条 OfferItem（带分档价 + source）
// 正常 vendor 路径:Stock(kind) 拿快照 · Capability.SelectablePlans[kind] 展开成多档
//
//	（多数 vendor 买前不能选档 · 那 SelectablePlans[kind] 为空 · 只出一条无 plan 的 OfferItem）
func (s *Service) offersForVendor(
	ctx context.Context, e providers.VendorEntry, kind providers.AccountKind,
) []OfferItem {
	// ── 手工池 · 只有 kiro_market 走这条 ─────────────────
	if e.VendorID == providers.VendorKiroMarket {
		return s.offersFromMarket(ctx, string(e.VendorID), kind)
	}

	// ── 正常 vendor · 打 Stock(kind) ─────────────────────
	snap, err := s.stockOnce(ctx, e.Vendor)
	if err != nil || snap == nil {
		return nil
	}
	cap := e.Vendor.Capability()
	plans := cap.SelectablePlans[kind]
	// 无可选档 · 只出一条无 plan 的 · 前端下拉禁用 / 隐藏
	if len(plans) == 0 {
		return offersFromSnapshot(snap, "")
	}
	// 有可选档 · 每档一条（当前 vendor 侧不支持"按 plan 查库存"· 各档共享同一 Zones 数字）
	out := make([]OfferItem, 0, len(plans)*len(snap.Zones))
	for _, plan := range plans {
		out = append(out, offersFromSnapshot(snap, plan)...)
	}
	return out
}

// offersFromSnapshot · 把 StockSnapshot.Zones 展开成 OfferItem · 可选带 plan
func offersFromSnapshot(snap *providers.StockSnapshot, plan providers.SubscriptionPlan) []OfferItem {
	out := make([]OfferItem, 0, len(snap.Zones))
	for _, z := range snap.Zones {
		zone := string(z.Zone)
		if zone == string(providers.ZoneGeneral) {
			zone = "" // 无区 vendor · 前端约定空串
		}
		out = append(out, OfferItem{
			Subscription: plan,
			Zone:         zone,
			Available:    z.Available,
			UnitPrice:    z.UnitPrice.Amount,
		})
	}
	return out
}

// offersFromMarket · 从 marketReader 组 · 每 offer 一条
func (s *Service) offersFromMarket(
	ctx context.Context, vendorID string, kind providers.AccountKind,
) []OfferItem {
	if s.marketReader == nil {
		return nil
	}
	all, err := s.marketReader.ListOffers(ctx)
	if err != nil {
		return nil
	}
	out := make([]OfferItem, 0)
	for _, o := range all {
		if !o.Enabled || o.VendorID != vendorID || o.AccountKind != kind {
			continue
		}
		n, err := s.marketReader.AvailableCount(ctx, o.ID)
		if err != nil {
			continue
		}
		unit := int64(0)
		if len(o.PriceBands) > 0 {
			unit = o.PriceBands[0].UnitPriceCredits
		}
		out = append(out, OfferItem{
			OfferID:      o.ID,
			Subscription: o.Subscription,
			Zone:         "", // 手工池无区
			Available:    n,
			UnitPrice:    unit,
			PriceBands:   o.PriceBands,
			Source:       o.Source,
		})
	}
	return out
}
