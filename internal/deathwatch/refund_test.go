package deathwatch

import (
	"testing"
)

// 退款之和必须恒等于该号的号价 —— 钱不能凭空多也不能少
func assertRefundSum(t *testing.T, plan map[string]int64, want int64) {
	t.Helper()
	var sum int64
	for _, v := range plan {
		sum += v
	}
	if sum != want {
		t.Errorf("退款之和 %d != 号价 %d", sum, want)
	}
}

// 单独拉号的号死了 → 全退归属人
func TestPlanRefund_StandalonePullRefundsOwner(t *testing.T) {
	plan := planRefund(RefundCandidate{
		CredentialLedgerID:     "c1",
		KeyCostShare:           30_000_000,
		OwnerRecordPassengerID: "solo",
	})
	assertRefundSum(t, plan, 30_000_000)
	if plan["solo"] != 30_000_000 {
		t.Errorf("单独拉号应全退归属人 · got=%d", plan["solo"])
	}
}

// 2 人车各分 2 号 → 各退一半
func TestPlanRefund_TwoMembersEvenSplit(t *testing.T) {
	plan := planRefund(RefundCandidate{
		CredentialLedgerID: "c1",
		KeyCostShare:       30_000_000,
		OwnerBusID:         "bus1",
		ParticipantsSplit:  map[string]int{"a": 2, "b": 2},
	})
	assertRefundSum(t, plan, 30_000_000)
	if plan["a"] != 15_000_000 || plan["b"] != 15_000_000 {
		t.Errorf("应各退一半 · got a=%d b=%d", plan["a"], plan["b"])
	}
}

// 按当初那轮的**号数占比**退（不是按现在的 share_pct）
func TestPlanRefund_SplitsByOriginalRoundRatio(t *testing.T) {
	// 当初 a 拿 3 个、b 拿 1 个 → 退款也按 3:1
	plan := planRefund(RefundCandidate{
		CredentialLedgerID: "c1",
		KeyCostShare:       40_000_000,
		OwnerBusID:         "bus1",
		ParticipantsSplit:  map[string]int{"a": 3, "b": 1},
	})
	assertRefundSum(t, plan, 40_000_000)
	if plan["a"] != 30_000_000 || plan["b"] != 10_000_000 {
		t.Errorf("应按 3:1 退 · got a=%d b=%d", plan["a"], plan["b"])
	}
}

// 除不尽时余数不能丢
func TestPlanRefund_RemainderNotLost(t *testing.T) {
	plan := planRefund(RefundCandidate{
		CredentialLedgerID: "c1",
		KeyCostShare:       100,
		OwnerBusID:         "bus1",
		ParticipantsSplit:  map[string]int{"a": 1, "b": 1, "c": 1},
	})
	assertRefundSum(t, plan, 100) // 关键：100 一分不少
}

// 已退出车的成员照样退（用户明确：他当初真金白银付了）
// —— planRefund 不查 bus_member·只看当初那轮的 split·所以退出的人天然包含在内
func TestPlanRefund_IncludesLeftMembers(t *testing.T) {
	plan := planRefund(RefundCandidate{
		CredentialLedgerID: "c1",
		KeyCostShare:       20_000_000,
		OwnerBusID:         "bus1",
		// "gone" 已经 left_at 非空了·但当初这轮他出了钱
		ParticipantsSplit: map[string]int{"stay": 1, "gone": 1},
	})
	assertRefundSum(t, plan, 20_000_000)
	if plan["gone"] != 10_000_000 {
		t.Errorf("已退出的成员也该退 · got=%d", plan["gone"])
	}
}

// split 里号数为 0 的人不退（他当初没分到号 = 被跳过了 = 没付钱）
func TestPlanRefund_SkipsZeroKeyMembers(t *testing.T) {
	plan := planRefund(RefundCandidate{
		CredentialLedgerID: "c1",
		KeyCostShare:       30_000_000,
		OwnerBusID:         "bus1",
		ParticipantsSplit:  map[string]int{"paid": 3, "skipped": 0},
	})
	assertRefundSum(t, plan, 30_000_000)
	if _, exists := plan["skipped"]; exists {
		t.Error("当初被跳过的人（0 号）不该收到退款")
	}
	if plan["paid"] != 30_000_000 {
		t.Errorf("付钱的人该全收 · got=%d", plan["paid"])
	}
}

// 号价为 0 → 没什么可退
func TestPlanRefund_ZeroKeyCostNoRefund(t *testing.T) {
	plan := planRefund(RefundCandidate{
		CredentialLedgerID: "c1",
		KeyCostShare:       0,
		ParticipantsSplit:  map[string]int{"a": 1},
	})
	if len(plan) != 0 {
		t.Errorf("号价 0 不该退 · got=%+v", plan)
	}
}

// split 空 + 没有 record 归属人 → 退不了（数据不全）
func TestPlanRefund_NoTargetReturnsNil(t *testing.T) {
	plan := planRefund(RefundCandidate{
		CredentialLedgerID: "c1",
		KeyCostShare:       10_000_000,
	})
	if plan != nil {
		t.Errorf("无退款目标应返 nil · got=%+v", plan)
	}
}

// split 号数全 0 → 退不了
func TestPlanRefund_AllZeroKeysReturnsNil(t *testing.T) {
	plan := planRefund(RefundCandidate{
		CredentialLedgerID: "c1",
		KeyCostShare:       10_000_000,
		ParticipantsSplit:  map[string]int{"a": 0, "b": 0},
	})
	if plan != nil {
		t.Errorf("号数全 0 应返 nil · got=%+v", plan)
	}
}

// 5 人不均等分 · 验证余数分配给拿号最多的人（可复现·不随 map 顺序变）
func TestPlanRefund_DeterministicRemainder(t *testing.T) {
	cand := RefundCandidate{
		CredentialLedgerID: "c1",
		KeyCostShare:       1000,
		OwnerBusID:         "bus1",
		ParticipantsSplit:  map[string]int{"a": 5, "b": 3, "c": 1},
	}
	first := planRefund(cand)
	// 跑 20 次·结果必须完全一致（map 遍历顺序随机·排序保证确定性）
	for i := 0; i < 20; i++ {
		got := planRefund(cand)
		for k, v := range first {
			if got[k] != v {
				t.Fatalf("退款分配不确定 · %s: %d vs %d", k, v, got[k])
			}
		}
	}
	assertRefundSum(t, first, 1000)
	// 拿号最多的 a 该拿到余数
	if first["a"] <= first["b"] {
		t.Errorf("拿号最多的该退最多 · a=%d b=%d", first["a"], first["b"])
	}
}
