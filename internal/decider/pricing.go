package decider

import "context"

// 定价算法。费率一律从配置读，代码里没有任何具体数值。

// Rate 费率，单位 basis point（1 bp = 0.01%）。0 = 该层不生效。
type Rate int64

const bpDenominator = 10_000

// Rates 各层费率，由配置注入。零值 = 零费率。
//
// **1a 版本**：从 env 一次性读入 · 全 vendor / zone / count 用同一份。
// **1b 版本**（1b P1-2B）：优先从 surcharge_rule 表按 EvalContext 求值 · env 只兜底。
type Rates struct {
	VendorMarkup Rate
	RegionMarkup Rate
	SinglePull   Rate
	Capability   Rate
	Service      Rate
	// Retail · 零售分项（未 invited 用户）· 1b 新加·迁到 surcharge_rule 之前 env 不填。
	Retail Rate
	// Adhoc · 临时分项（活得特别长的车等）· 1b 新加·env 一般不填。
	Adhoc Rate
}

// RatesResolver 是"根据本次拉号上下文实时算 Rates"的抽象。
//
// **1b 起** orchestrator 用它替代静态 `o.rates` · 让费率从 DB 表来。
// 装配层传 pricing 包的 SurchargeResolver 适配 · 测试用 mock。
type RatesResolver interface {
	Resolve(ctx context.Context, ec RateContext) Rates
}

// RateContext · 求费率时的上下文·映射到 surcharge Rule.applies_when 判定。
type RateContext struct {
	VendorID         string
	Zone             string
	Count            int
	PassengerInvited bool
	BusAvgLifespanH  float64
}

// Breakdown 计费结果。
//
// 导出的三个字段就是**可以对外展示的全部**（§8.20）：单价、总额、服务费。
// 其余分项只用于内部记账和落库，不出包。
type Breakdown struct {
	UnitPrice  int64
	Count      int
	Total      int64
	ServiceFee int64

	keyCost       int64
	vendorFee     int64
	regionFee     int64
	singlePullFee int64
	capabilityFee int64
}

// Price 按配置的费率算出单价与分项。keyUnitCost 为 vendor 侧实付单价（microunit）。
//
// **分项计算顺序**（decisions §8.30 C）：
//   号价 → vendor 附加 → region → single_pull(count==1) → retail → capability → adhoc → service
//
// **落库分项**（pull_round 表列有限）：
//   Retail / Adhoc 归到 capabilityFee 桶（capabilityFee = retail + capability + adhoc）·
//   1c 加分离列时再拆。ServiceFee 单独列（对外可见）。
func Price(keyUnitCost int64, count int, r Rates) Breakdown {
	singlePull := r.SinglePull
	if count != 1 {
		singlePull = 0
	}

	s0 := keyUnitCost
	s1 := applyRate(s0, r.VendorMarkup)
	s2 := applyRate(s1, r.RegionMarkup)
	s3 := applyRate(s2, singlePull)
	s4 := applyRate(s3, r.Retail)
	s5 := applyRate(s4, r.Capability)
	s6 := applyRate(s5, r.Adhoc)
	s7 := applyRate(s6, r.Service)

	n := int64(count)
	// retail + capability + adhoc 都归入 capabilityFee 桶（跟落库列匹配 · 1c 再拆）
	capBucket := (s6 - s3) * n
	return Breakdown{
		UnitPrice:  s7,
		Count:      count,
		Total:      s7 * n,
		ServiceFee: (s7 - s6) * n,

		keyCost:       s0 * n,
		vendorFee:     (s1 - s0) * n,
		regionFee:     (s2 - s1) * n,
		singlePullFee: (s3 - s2) * n,
		capabilityFee: capBucket,
	}
}

// applyRate 逐层作用，四舍五入到整 microunit。截断会在多层链上持续向下偏。
func applyRate(subtotal int64, rate Rate) int64 {
	if rate == 0 {
		return subtotal
	}
	return (subtotal*(bpDenominator+int64(rate)) + bpDenominator/2) / bpDenominator
}

// Verify 校验分项之和等于总额。对不上就不该扣款 —— 那样对账永远平不了。
func (b Breakdown) Verify() bool {
	return b.keyCost+b.vendorFee+b.regionFee+
		b.singlePullFee+b.capabilityFee+b.ServiceFee == b.Total
}

// roundColumns 是 pull_round 的计费列。只在包内落库用。
func (b Breakdown) roundColumns() []any {
	return []any{
		b.keyCost, b.vendorFee, b.regionFee,
		b.singlePullFee, b.capabilityFee, b.ServiceFee,
	}
}
