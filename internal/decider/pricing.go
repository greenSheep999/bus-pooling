package decider

// 定价算法。费率一律从配置读，代码里没有任何具体数值。

// Rate 费率，单位 basis point（1 bp = 0.01%）。0 = 该层不生效。
type Rate int64

const bpDenominator = 10_000

// Rates 各层费率，由配置注入。零值 = 不加价。
type Rates struct {
	VendorMarkup Rate
	RegionMarkup Rate
	SinglePull   Rate
	Capability   Rate
	Service      Rate
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
func Price(keyUnitCost int64, count int, r Rates) Breakdown {
	singlePull := r.SinglePull
	if count != 1 {
		singlePull = 0
	}

	s0 := keyUnitCost
	s1 := applyRate(s0, r.VendorMarkup)
	s2 := applyRate(s1, r.RegionMarkup)
	s3 := applyRate(s2, singlePull)
	s4 := applyRate(s3, r.Capability)
	s5 := applyRate(s4, r.Service)

	n := int64(count)
	return Breakdown{
		UnitPrice:  s5,
		Count:      count,
		Total:      s5 * n,
		ServiceFee: (s5 - s4) * n,

		keyCost:       s0 * n,
		vendorFee:     (s1 - s0) * n,
		regionFee:     (s2 - s1) * n,
		singlePullFee: (s3 - s2) * n,
		capabilityFee: (s4 - s3) * n,
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
