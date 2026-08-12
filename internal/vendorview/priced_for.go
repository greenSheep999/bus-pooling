package vendorview

// PricedFor · 唯一定价查询入口（docs/18 §4）
//
// 拉号 / 拼车 / Pricing 页 / Status 页 全部走它 · 不再有第二处算价：
//
//	Server 层拿到 (vendorID · region · count · viewer)
//	  ↓
//	读 vendor_probe.our_unit_credits（机制 A · 权威积分）
//	  · 缺失 fallback 上一条已知（打时间戳）· 都空返 ErrPriceMissing
//	  ↓
//	按 viewer.Tier 走静态计费栈（§2.2）
//	  · retail · vendor + region + [single_pull if count=1] + service
//	  · community · vendor + [single_pull if count=1] + service
//	  · wholesale · [single_pull if count=1] + service
//	  ↓
//	按 user_subsidy 应用减免栈（§3）· 有 remaining_uses/expires_at
//	  ↓
//	按 tier 决定 label · 只 wholesale 看真名 · 其他匿名 label
//	  ↓
//	→ PricedView { Label, PriceCredits, StaleAge, Breakdown }

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/pricing"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ErrPriceMissing · vendor_probe 从没探到过这家 vendor 的价（长期断线 / 刚接入）
var ErrPriceMissing = errors.New("vendorview: 该 vendor 无可用价格")

// PricedView · PricedFor 返值 · 对外可展示的结果
type PricedView struct {
	VendorLabel    string        // wholesale 看真名 · 其他匿名 label（Vendor 01）
	AnonID         string        // 稳定 6 位 hash · UI 排序用
	PriceCredits   int64         // ★ 最终积分单价 microunit · 前端一律读这个
	TotalCredits   int64         // = PriceCredits × count（含分档乘 + 减免后）
	StaleAge       time.Duration // 价格距现在多久（Q2 fallback · 前端显示"更新于 N 分钟前"）
	SourceProbedAt time.Time     // 探测时刻 · 便于对账
	Breakdown      Breakdown     // 分项拆分（对账 · 前端不显示细节）
}

// Breakdown · 计费栈分项 · 每层多少积分
type Breakdown struct {
	Base            int64 // 上游积分单价（wholesale 看到的 · 基础）
	VendorMarkup    int64 // 我方 vendor 分项加了多少
	RegionMarkup    int64 // 区域分项加了多少
	SinglePullExtra int64 // 单拉分项（count=1 时）加了多少
	ServiceFee      int64 // 服务费加了多少
	SubsidyWaived   int64 // 减免栈减了多少（正数 · 折扣额）
}

// PricedForInput
type PricedForInput struct {
	VendorID string
	Region   string // 空 = 任意 · 走该 vendor 最近一条
	Count    int
	Viewer   Viewer
}

// PricedFor · 唯一门面
//
// 三种错：
//   - ErrPriceMissing · 无价可用 · api 层返 404 或"暂缺"
//   - ErrVendorNotFound · vendor 不在 registry
//   - ErrForbidden · viewer 无权访问价（未登录 · api 层返 401）
func (s *Service) PricedFor(ctx context.Context, in PricedForInput) (*PricedView, error) {
	if s == nil {
		return nil, fmt.Errorf("vendorview: Service 未装配")
	}
	if in.Viewer.Tier == "" {
		in.Viewer.Tier = TierRetail // 默认档
	}
	if in.Count <= 0 {
		in.Count = 1
	}

	// 1. 读机制 A 权威积分（vendor_probe.our_unit_credits）
	credits, probedAt, err := s.latestCredits(ctx, in.VendorID, in.Region)
	if err != nil {
		return nil, err
	}
	if credits <= 0 {
		return nil, ErrPriceMissing
	}
	staleAge := time.Since(probedAt)

	// 2. 按 tier 走静态计费栈
	rates := s.rates
	bd := computeBreakdown(credits, in.Count, in.Viewer.Tier, rates)

	// 3. 减免栈（docs/18 §3）· 查 user_subsidy · 有 remaining_uses/expires_at 才生效
	bd.SubsidyWaived = s.applySubsidies(ctx, in.Viewer.PassengerID, bd)

	// 4. 组合价 = base + vendor + region + single_pull + service - subsidy
	unitPrice := bd.Base + bd.VendorMarkup + bd.RegionMarkup + bd.SinglePullExtra + bd.ServiceFee - bd.SubsidyWaived
	if unitPrice < 0 {
		unitPrice = 0
	}

	// 5. tier label
	label, anonID := s.tierLabel(in.VendorID, in.Viewer)

	return &PricedView{
		VendorLabel:    label,
		AnonID:         anonID,
		PriceCredits:   unitPrice,
		TotalCredits:   unitPrice * int64(in.Count),
		StaleAge:       staleAge,
		SourceProbedAt: probedAt,
		Breakdown:      bd,
	}, nil
}

// latestCredits · 读 vendor_probe.our_unit_credits（机制 A 的出口）
//
// **region 参数当前不生效** —— `vendor_probe` 没有 region 列 · `our_unit_credits` 是
// 首个 zone 的采样值（`sample_price_region` 记了是哪个 zone）· 一条探针行只有一个积分值。
// 要精确到区得先给 vendor_probe 加 region 维度（当前多 region 差价不大 · 不值当）。
// 签名保留 region 是为了将来加维度时调用方不用改。
func (s *Service) latestCredits(ctx context.Context, vendorID, region string) (int64, time.Time, error) {
	_ = region // 见上方说明
	if s.probeStore == nil {
		return 0, time.Time{}, ErrPriceMissing
	}
	credits, probedAt, ok := s.probeCredits().LatestCredits(ctx, vendorID)
	if !ok {
		return 0, time.Time{}, ErrPriceMissing
	}
	return credits, probedAt, nil
}

// probeCredits · 复用 pricing 那份读取器 · 保证跟 decider 估价读的是同一列同一条 SQL
func (s *Service) probeCredits() *pricing.ProbeCredits {
	return pricing.NewProbeCredits(s.probeStore.db)
}

// tierLabel · docs/18 §2.1 · 只 wholesale 看真名
func (s *Service) tierLabel(vendorID string, v Viewer) (label, anon string) {
	vid := providers.VendorID(vendorID)
	anon = anonIDOf(vid)
	if v.canSeeVendorName() {
		e, ok := s.lookupAny(vendorID)
		if ok {
			return e.Vendor.DisplayName(), anon
		}
	}
	return anonLabelOf(vid), anon
}

// computeBreakdown · 静态计费栈（docs/18 §2.2 · 逐层乘 · §8.34 铁律）
//
// **计费栈**：
//   retail    · base × (1 + vendor 67%) × (1 + region 20%) × (1 + single_pull 20% if count=1) × (1 + service 5%)
//   community · base × (1 + vendor 67%)                    × (1 + single_pull 20% if count=1) × (1 + service 5%)
//   wholesale · base                                        × (1 + single_pull 20% if count=1) × (1 + service 5%)
//
// **Rates 单位**：basis point · 500 = 5%（对齐 decider.Rates 老口径）
func computeBreakdown(base int64, count int, tier string, rates decider.Rates) Breakdown {
	bd := Breakdown{Base: base}
	cur := base

	// 逐层乘 · 各层增量记进 Breakdown
	addLayer := func(bp decider.Rate, capture *int64) {
		if bp == 0 {
			return
		}
		delta := cur * int64(bp) / 10000
		*capture = delta
		cur += delta
	}

	// vendor 层 · retail + community 都加 · wholesale 免
	if tier != TierWholesale {
		addLayer(rates.VendorMarkup, &bd.VendorMarkup)
	}
	// 区域层 · 只 retail 加 · community + wholesale 都免
	if tier == TierRetail {
		addLayer(rates.RegionMarkup, &bd.RegionMarkup)
	}
	// 单拉分项 · 三档都加（count=1 时）
	if count == 1 {
		addLayer(rates.SinglePull, &bd.SinglePullExtra)
	}
	// 服务费 · 三档都加（链末尾）
	addLayer(rates.Service, &bd.ServiceFee)

	return bd
}
