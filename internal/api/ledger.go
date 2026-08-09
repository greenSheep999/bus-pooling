package api

import "github.com/bus-pooling/bus-pooling/internal/wallet"

// LedgerType 是对外的流水类型，跟 web/src/types/index.ts 的 LedgerType 对齐。
type LedgerType string

const (
	LedgerTopup          LedgerType = "topup"
	LedgerSpend          LedgerType = "spend"
	LedgerRedeem         LedgerType = "redeem"
	LedgerRefund         LedgerType = "refund"
	LedgerWarrantyRefund LedgerType = "warranty_refund"
)

// spendInternalReasons 对外 spend（拉号消费）展开的内部 reason 集合。
// **只含加价链那几层** —— 通道费和运营调整是别的动作，不归这里。
var spendInternalReasons = []wallet.Reason{
	wallet.ReasonKeyCost,
	wallet.ReasonVendorFee,
	wallet.ReasonRegionFee,
	wallet.ReasonSinglePullFee,
	wallet.ReasonCapabilityFee,
	wallet.ReasonServiceFee,
}

// topupInternalReasons 对外 topup（充值）展开的内部 reason 集合。
// 一次充值在内部拆两笔：recharge（乘客真金白银换到的总积分）+ channel_fee（pass-through
// 给通道方的部分，立刻扣回）· 净变化 = 到账目标积分。详见 CLAUDE.md §1.4。
var topupInternalReasons = []wallet.Reason{
	wallet.ReasonRecharge,
	wallet.ReasonChannelFee,
}

// internalReasonsFor 把对外 LedgerType 展开成内部 reason，用于 ?type= 过滤。
// 空 type = 不过滤；未知 type 返回不匹配任何行的 sentinel。
func internalReasonsFor(publicType string) []wallet.Reason {
	switch LedgerType(publicType) {
	case "":
		return nil
	case LedgerTopup:
		return topupInternalReasons
	case LedgerRedeem:
		return []wallet.Reason{wallet.ReasonRedeem}
	case LedgerWarrantyRefund:
		return []wallet.Reason{wallet.ReasonWarrantyRefund}
	case LedgerRefund:
		// gateway 退款 (topup_refund) 和运营调整 (admin_adjust) 对外都是 refund
		return []wallet.Reason{wallet.ReasonAdminAdjust, wallet.ReasonTopupRefund}
	case LedgerSpend:
		return spendInternalReasons
	default:
		return []wallet.Reason{wallet.Reason("__unknown__")}
	}
}

// publicLedgerType 把内部 wallet.Reason 映射成对外 LedgerType。
func publicLedgerType(r wallet.Reason) LedgerType {
	switch r {
	case wallet.ReasonRecharge, wallet.ReasonChannelFee:
		return LedgerTopup
	case wallet.ReasonRedeem:
		return LedgerRedeem
	case wallet.ReasonWarrantyRefund:
		return LedgerWarrantyRefund
	case wallet.ReasonAdminAdjust, wallet.ReasonTopupRefund:
		return LedgerRefund
	case wallet.ReasonKeyCost, wallet.ReasonVendorFee, wallet.ReasonRegionFee,
		wallet.ReasonSinglePullFee, wallet.ReasonCapabilityFee, wallet.ReasonServiceFee:
		return LedgerSpend
	default:
		// 未知 reason 保守归 spend（负号）· 添加新 reason 时应显式加映射
		return LedgerSpend
	}
}
