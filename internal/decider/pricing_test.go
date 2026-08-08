package decider

import "testing"

const micro = 1_000_000

// 测试用的任意费率 —— 不是生产配置，只为跑通算法各条分支。
var testRates = Rates{
	VendorMarkup: 5000, // 50%
	RegionMarkup: 1000, // 10%
	SinglePull:   2000, // 20%
	Service:      400,  // 4%
}

// 逐层乘：每层作用在上一层结果上，不是各层都加在基数上。
func TestPriceIsMultiplicativeNotAdditive(t *testing.T) {
	// 100 × 1.5 × 1.1 = 165（加法会得 160）
	got := Price(100*micro, 1, Rates{VendorMarkup: 5000, RegionMarkup: 1000})
	if got.UnitPrice != 165*micro {
		t.Errorf("单价 = %d，want 165_000_000（逐层乘）", got.UnitPrice)
	}
}

// 服务费作用在**已加价的小计**上，不是基数。
func TestServiceFeeAppliesToMarkedUpSubtotal(t *testing.T) {
	base := Price(100*micro, 1, Rates{Service: 400})
	marked := Price(100*micro, 1, Rates{VendorMarkup: 5000, Service: 400})

	if base.ServiceFee != 4*micro {
		t.Errorf("无加价时服务费 = %d，want 4_000_000", base.ServiceFee)
	}
	// 150 × 4% = 6
	if marked.ServiceFee != 6*micro {
		t.Errorf("有加价时服务费 = %d，want 6_000_000", marked.ServiceFee)
	}
	if marked.ServiceFee <= base.ServiceFee {
		t.Error("服务费应随小计变大")
	}
}

// 单次议价只在 count==1 时上链。
func TestSinglePullOnlyAppliesWhenCountIsOne(t *testing.T) {
	batch := Price(100*micro, 3, testRates)
	if batch.singlePullFee != 0 {
		t.Errorf("count=3 时单次议价 = %d，want 0", batch.singlePullFee)
	}

	single := Price(100*micro, 1, testRates)
	if single.singlePullFee == 0 {
		t.Error("count=1 时单次议价必须上链")
	}
	if single.UnitPrice <= batch.UnitPrice {
		t.Errorf("单拉单价 %d 应高于批量 %d", single.UnitPrice, batch.UnitPrice)
	}
}

// 零值 Rates = 按号价原样收。
func TestZeroRatesIsPassThrough(t *testing.T) {
	got := Price(17*micro, 4, Rates{})
	if got.UnitPrice != 17*micro {
		t.Errorf("单价 = %d", got.UnitPrice)
	}
	if got.Total != 68*micro {
		t.Errorf("总额 = %d", got.Total)
	}
	if got.ServiceFee != 0 {
		t.Errorf("服务费 = %d，零费率应为 0", got.ServiceFee)
	}
	if !got.Verify() {
		t.Error("恒等式不成立")
	}
}

// 未启用的层费额必须是 0，不能是舍入出来的 ±1。
func TestUnusedLayersAreExactlyZero(t *testing.T) {
	got := Price(333_333, 1, Rates{Service: 400})
	if got.vendorFee != 0 || got.regionFee != 0 || got.capabilityFee != 0 || got.singlePullFee != 0 {
		t.Errorf("未启用层不为 0: vendor=%d region=%d cap=%d single=%d",
			got.vendorFee, got.regionFee, got.capabilityFee, got.singlePullFee)
	}
}

// 恒等式是核心：分项之和对不上总额，几笔 ledger 就加不出应扣总额。
func TestIdentityHoldsAcrossManyInputs(t *testing.T) {
	costs := []int64{1, 7, 333, 999_999, 1 * micro, 3_333_333, 17_777_777}
	rateSets := []Rates{
		{},
		{Service: 1},
		{VendorMarkup: 3333, Service: 777},
		{VendorMarkup: 5000, RegionMarkup: 1000, SinglePull: 2000, Capability: 9999, Service: 400},
		{VendorMarkup: 1, RegionMarkup: 1, SinglePull: 1, Capability: 1, Service: 1},
		{VendorMarkup: 100_000, Service: 100_000},
	}
	counts := []int{1, 2, 3, 7, 200}

	for _, cost := range costs {
		for _, r := range rateSets {
			for _, n := range counts {
				got := Price(cost, n, r)
				if !got.Verify() {
					t.Errorf("恒等式不成立: cost=%d count=%d rates=%+v", cost, n, r)
				}
				if got.Total != got.UnitPrice*int64(n) {
					t.Errorf("总额(%d) != 单价(%d)×%d", got.Total, got.UnitPrice, n)
				}
				if got.UnitPrice < cost {
					t.Errorf("加价后单价 %d 低于号价 %d", got.UnitPrice, cost)
				}
			}
		}
	}
}

// 总额跟层顺序无关（乘法可交换），但分项跟顺序有关 —— 所以链的顺序钉死。
func TestTotalIsOrderIndependentButSplitIsNot(t *testing.T) {
	a := Price(100*micro, 1, Rates{VendorMarkup: 5000, Service: 400})
	b := Price(100*micro, 1, Rates{VendorMarkup: 400, Service: 5000})

	if a.Total != b.Total {
		t.Errorf("总额应相等: %d vs %d", a.Total, b.Total)
	}
	if a.ServiceFee == b.ServiceFee {
		t.Error("分项应随顺序变")
	}
}

// 四舍五入而非截断：截断在多层链上持续向下偏，累积成可观的少收。
func TestRoundingIsHalfUp(t *testing.T) {
	if got := applyRate(1, 5000); got != 2 {
		t.Errorf("applyRate(1, 50%%) = %d，want 2", got)
	}
	if got := applyRate(1, 4900); got != 1 {
		t.Errorf("applyRate(1, 49%%) = %d，want 1", got)
	}
	if got := applyRate(12345, 0); got != 12345 {
		t.Errorf("零费率应原样返回，得到 %d", got)
	}
}

func TestVerifyCatchesInconsistency(t *testing.T) {
	b := Price(100*micro, 1, testRates)
	if !b.Verify() {
		t.Fatal("正常结果应通过")
	}
	b.ServiceFee++
	if b.Verify() {
		t.Error("Verify 没抓到分项和 != 总额")
	}
}

// 对外只露单价 / 总额 / 服务费；其余分项不出包（§8.20）。
func TestOnlyPublicFieldsAreExported(t *testing.T) {
	b := Price(100*micro, 2, testRates)
	if b.UnitPrice == 0 || b.Total == 0 || b.ServiceFee == 0 {
		t.Error("对外三个字段应有值")
	}
	// keyCost 等是小写字段 —— 包外拿不到，这里只确认包内落库能用
	if len(b.roundColumns()) != 6 {
		t.Errorf("落库列数 = %d，want 6", len(b.roundColumns()))
	}
}
