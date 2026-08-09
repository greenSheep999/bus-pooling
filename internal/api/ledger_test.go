package api

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

// 分项链的分层 reason 一律映射成 spend，对外只看到"花了多少"，
// 不该看到内部怎么分项（CLAUDE.md §0.1 · decisions §8.20）。
func TestPublicLedgerTypeHidesMarkupLayers(t *testing.T) {
	mustHide := []wallet.Reason{
		wallet.ReasonKeyCost, wallet.ReasonVendorFee, wallet.ReasonRegionFee,
		wallet.ReasonSinglePullFee, wallet.ReasonCapabilityFee, wallet.ReasonServiceFee,
	}
	for _, r := range mustHide {
		got := publicLedgerType(r)
		if got != LedgerSpend {
			t.Errorf("%q 应映射到 spend（隐藏分项层），得到 %q", r, got)
		}
	}
}

func TestPublicLedgerTypeBasicMappings(t *testing.T) {
	cases := []struct {
		in   wallet.Reason
		want LedgerType
	}{
		{wallet.ReasonRecharge, LedgerTopup},
		{wallet.ReasonRedeem, LedgerRedeem},
		{wallet.ReasonWarrantyRefund, LedgerWarrantyRefund},
		// channel_fee 是充值时 pass-through 给通道方的一笔（CLAUDE.md §1.4），
		// 跟 recharge 是同一次充值的两条明细，归 topup 而不是 spend
		{wallet.ReasonChannelFee, LedgerTopup},
		// admin_adjust 归 refund：运营补偿 / 手工修正跟拉号消费是不同动作
		{wallet.ReasonAdminAdjust, LedgerRefund},
	}
	for _, c := range cases {
		if got := publicLedgerType(c.in); got != c.want {
			t.Errorf("publicLedgerType(%q) = %q，want %q", c.in, got, c.want)
		}
	}
}

// channel_fee 不该混进 spend —— 那是充值上下文，不是拉号消费。
// 混一起前端筛"我这次拉号花了多少"会把手续费也算进去。
func TestSpendDoesNotIncludeTopupOrAdjustReasons(t *testing.T) {
	spend := internalReasonsFor(string(LedgerSpend))
	for _, r := range spend {
		if r == wallet.ReasonRecharge || r == wallet.ReasonChannelFee || r == wallet.ReasonAdminAdjust {
			t.Errorf("spend 里出现了 %q，那不是拉号消费", r)
		}
	}
}

// topup 展开必须同时含 recharge 和 channel_fee —— 一次充值在内部是两条明细，
// 前端筛 topup 应该两条都能看到（不然充值明细就断了）
func TestTopupExpandsToRechargeAndChannelFee(t *testing.T) {
	got := internalReasonsFor(string(LedgerTopup))
	seen := map[wallet.Reason]bool{}
	for _, r := range got {
		seen[r] = true
	}
	if !seen[wallet.ReasonRecharge] {
		t.Error("topup 展开缺 recharge")
	}
	if !seen[wallet.ReasonChannelFee] {
		t.Error("topup 展开缺 channel_fee —— 前端筛充值明细看不到手续费那条")
	}
}

// 对外 type=spend 应展开成拉号那几层，前端才能筛出"这次花了多少"的行。
func TestInternalReasonsForSpendExpandsAllMarkupLayers(t *testing.T) {
	got := internalReasonsFor(string(LedgerSpend))
	if len(got) == 0 {
		t.Fatal("spend 应展开成多个内部 reason")
	}

	must := map[wallet.Reason]bool{
		wallet.ReasonKeyCost:       false,
		wallet.ReasonServiceFee:    false,
		wallet.ReasonVendorFee:     false,
		wallet.ReasonRegionFee:     false,
		wallet.ReasonSinglePullFee: false,
	}
	for _, r := range got {
		if _, ok := must[r]; ok {
			must[r] = true
		}
	}
	for r, seen := range must {
		if !seen {
			t.Errorf("spend 展开里缺 %q —— 前端筛不出这层，用量对不上", r)
		}
	}
}

// 空 type = 不过滤。未知 type = 匹配不到任何行（不能变成"不过滤"）。
func TestInternalReasonsForEdges(t *testing.T) {
	if internalReasonsFor("") != nil {
		t.Error("空 type 应返回 nil（不过滤）")
	}
	got := internalReasonsFor("garbage")
	if len(got) == 0 {
		t.Fatal("未知 type 应返回不匹配任何行的 sentinel，不能变成'不过滤'")
	}
}
