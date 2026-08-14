package vendorview

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/decider"
)

// computeBreakdown · 三档计费栈锁死（docs/10-pricing §2.3）
//
// 号价 20 积分（20 × 1_000_000 microunit）· 率见 §2.5
//   vendor 67% · region 20% · single_pull 20% · service 5%
//
// **retail 批量**：20 × 1.67 × 1.20 × 1.05 = 42.084
// **community 批量**：20 × 1.67 × 1.05 = 35.07
// **wholesale 批量**：20 × 1.05 = 21.00
func TestComputeBreakdown_ThreeTiers_BatchCount(t *testing.T) {
	const base int64 = 20_000_000 // 20 积分 microunit
	rates := decider.Rates{
		VendorMarkup: 6700, // 67%
		RegionMarkup: 2000, // 20%
		SinglePull:   2000, // 20%
		Service:      500,  // 5%
	}

	// 批量（count=3）· single_pull 不加

	// retail · 全加
	// step 1: 20 × 1.67 = 33.4
	// step 2: 33.4 × 1.20 = 40.08
	// step 3: 40.08 × 1.05 = 42.084
	bdR := computeBreakdown(base, 3, TierRetail, rates)
	final := bdR.Base + bdR.VendorMarkup + bdR.RegionMarkup + bdR.SinglePullExtra + bdR.ServiceFee
	if final != 42_084_000 {
		t.Errorf("retail 批量 · 期 42.084 = 42_084_000 · 得 %d", final)
	}
	if bdR.SinglePullExtra != 0 {
		t.Errorf("count>1 时 SinglePullExtra 应 0 · 得 %d", bdR.SinglePullExtra)
	}

	// community · 免区域
	// step 1: 20 × 1.67 = 33.4
	// step 2: 33.4 × 1.05 = 35.07
	bdC := computeBreakdown(base, 3, TierCommunity, rates)
	final = bdC.Base + bdC.VendorMarkup + bdC.RegionMarkup + bdC.SinglePullExtra + bdC.ServiceFee
	if final != 35_070_000 {
		t.Errorf("community 批量 · 期 35.07 = 35_070_000 · 得 %d", final)
	}
	if bdC.RegionMarkup != 0 {
		t.Errorf("community 应免 region · 得 %d", bdC.RegionMarkup)
	}

	// wholesale · 免 vendor + region
	// step 1: 20 × 1.05 = 21.0
	bdW := computeBreakdown(base, 3, TierWholesale, rates)
	final = bdW.Base + bdW.VendorMarkup + bdW.RegionMarkup + bdW.SinglePullExtra + bdW.ServiceFee
	if final != 21_000_000 {
		t.Errorf("wholesale 批量 · 期 21.0 = 21_000_000 · 得 %d", final)
	}
	if bdW.VendorMarkup != 0 || bdW.RegionMarkup != 0 {
		t.Errorf("wholesale 应免 vendor + region · 得 %d / %d", bdW.VendorMarkup, bdW.RegionMarkup)
	}
}

// **单拉 count=1** 三档都加单拉分项 20%
//
// **retail 单拉**：20 × 1.67 × 1.20 × 1.20 × 1.05 = 50.5008
// **community 单拉**：20 × 1.67 × 1.20 × 1.05 = 42.084
// **wholesale 单拉**：20 × 1.20 × 1.05 = 25.20
func TestComputeBreakdown_ThreeTiers_SinglePull(t *testing.T) {
	const base int64 = 20_000_000
	rates := decider.Rates{
		VendorMarkup: 6700,
		RegionMarkup: 2000,
		SinglePull:   2000,
		Service:      500,
	}

	bdR := computeBreakdown(base, 1, TierRetail, rates)
	final := bdR.Base + bdR.VendorMarkup + bdR.RegionMarkup + bdR.SinglePullExtra + bdR.ServiceFee
	if final != 50_500_800 {
		t.Errorf("retail 单拉 · 期 50.5008 · 得 %d", final)
	}
	if bdR.SinglePullExtra == 0 {
		t.Error("retail count=1 应有 SinglePullExtra")
	}

	bdC := computeBreakdown(base, 1, TierCommunity, rates)
	final = bdC.Base + bdC.VendorMarkup + bdC.RegionMarkup + bdC.SinglePullExtra + bdC.ServiceFee
	if final != 42_084_000 {
		t.Errorf("community 单拉 · 期 42.084 · 得 %d", final)
	}

	bdW := computeBreakdown(base, 1, TierWholesale, rates)
	final = bdW.Base + bdW.VendorMarkup + bdW.RegionMarkup + bdW.SinglePullExtra + bdW.ServiceFee
	if final != 25_200_000 {
		t.Errorf("wholesale 单拉 · 期 25.20 · 得 %d", final)
	}
}

// **retail > community > wholesale** · 单调性哨兵（顺序错就整个模型崩）
func TestComputeBreakdown_Monotonic_RetailAboveCommunityAboveWholesale(t *testing.T) {
	const base int64 = 20_000_000
	rates := decider.Rates{
		VendorMarkup: 6700, RegionMarkup: 2000, SinglePull: 2000, Service: 500,
	}
	for _, count := range []int{1, 3, 10} {
		total := func(tier string) int64 {
			bd := computeBreakdown(base, count, tier, rates)
			return bd.Base + bd.VendorMarkup + bd.RegionMarkup + bd.SinglePullExtra + bd.ServiceFee
		}
		r, c, w := total(TierRetail), total(TierCommunity), total(TierWholesale)
		if !(r > c && c > w) {
			t.Errorf("count=%d · 单调性破：retail=%d community=%d wholesale=%d · 应 r>c>w",
				count, r, c, w)
		}
	}
}

// canSeeVendorName · 只 wholesale 看真名
func TestViewer_CanSeeVendorName(t *testing.T) {
	if (Viewer{Tier: TierRetail}).canSeeVendorName() {
		t.Error("retail 不该看真名")
	}
	if (Viewer{Tier: TierCommunity}).canSeeVendorName() {
		t.Error("community 不该看真名")
	}
	if !(Viewer{Tier: TierWholesale}).canSeeVendorName() {
		t.Error("wholesale 必须看真名")
	}
	if (Viewer{}).canSeeVendorName() {
		t.Error("空 tier（默认 retail）不该看真名")
	}
}
