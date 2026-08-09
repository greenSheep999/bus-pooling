package pullrecord

import (
	"errors"
	"testing"
)

// 收支必须相等 —— 我方是清算通道不抽成（§8.23）
func assertBalanced(t *testing.T, st Settlement) {
	t.Helper()
	var paid int64
	for _, p := range st.Payers {
		paid += p.Amount
	}
	if paid != st.Income {
		t.Errorf("收支不平 · 车友付出 %d · 派入者收到 %d", paid, st.Income)
	}
}

// §8.23 文档里的例子：我 40% / A 30% / B 30% · 成本 100 → 我收 60 净支出 40
func TestPlanSettlement_DocExample(t *testing.T) {
	members := []settlementMember{
		{passengerID: "me", sharePct: 40, balance: 100_000_000},
		{passengerID: "a", sharePct: 30, balance: 100_000_000},
		{passengerID: "b", sharePct: 30, balance: 100_000_000},
	}
	st, err := planSettlement(members, "me", 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	assertBalanced(t, st)
	if st.Income != 60_000_000 {
		t.Errorf("派入者应收 60_000_000（A 30 + B 30）· got=%d", st.Income)
	}
	if len(st.Payers) != 2 {
		t.Errorf("付款人数 = %d · want 2（派入者自己不在里面）", len(st.Payers))
	}
	// 派入者不该出现在 payers 里
	for _, p := range st.Payers {
		if p.PassengerID == "me" {
			t.Error("派入者不该给自己付钱")
		}
	}
}

// 单人车不清算（没有分摊对象）
func TestPlanSettlement_SoloNoSettle(t *testing.T) {
	members := []settlementMember{
		{passengerID: "me", sharePct: 100, balance: 100_000_000},
	}
	st, err := planSettlement(members, "me", 50_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Solo {
		t.Error("单人车该返 Solo=true")
	}
	if st.Income != 0 || len(st.Payers) != 0 {
		t.Errorf("单人车不该动钱 · income=%d payers=%d", st.Income, len(st.Payers))
	}
}

// 余额不足的成员**只跳过他**·不拦整车（§8.23 明文修正）
func TestPlanSettlement_SkipsBrokeButProceeds(t *testing.T) {
	members := []settlementMember{
		{passengerID: "me", sharePct: 40, balance: 100_000_000},
		{passengerID: "rich", sharePct: 30, balance: 100_000_000},
		{passengerID: "broke", sharePct: 30, balance: 1}, // 付不起
	}
	st, err := planSettlement(members, "me", 100_000_000)
	if err != nil {
		t.Fatalf("有人付不起也该放行 · got err=%v", err)
	}
	assertBalanced(t, st)
	if st.Income != 30_000_000 {
		t.Errorf("只收得起的那份 · got=%d want=30_000_000", st.Income)
	}
	if st.Lost != 30_000_000 {
		t.Errorf("少收的该记 Lost · got=%d", st.Lost)
	}
	if len(st.Skipped) != 1 || st.Skipped[0].PassengerID != "broke" {
		t.Errorf("skipped 应含 broke · got=%+v", st.Skipped)
	}
	if st.Skipped[0].SkipReason != "insufficient_balance" {
		t.Errorf("跳过原因 = %q", st.Skipped[0].SkipReason)
	}
}

// 挂起的成员不参与（§8.26）
func TestPlanSettlement_SuspendedSkipped(t *testing.T) {
	members := []settlementMember{
		{passengerID: "me", sharePct: 40, balance: 100_000_000},
		{passengerID: "ok", sharePct: 30, balance: 100_000_000},
		{passengerID: "susp", sharePct: 30, balance: 100_000_000, suspended: true},
	}
	st, err := planSettlement(members, "me", 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	assertBalanced(t, st)
	if st.Income != 30_000_000 {
		t.Errorf("挂起的不参与 · got=%d", st.Income)
	}
	if len(st.Skipped) != 1 || st.Skipped[0].SkipReason != "suspended" {
		t.Errorf("skipped 应标 suspended · got=%+v", st.Skipped)
	}
}

// 一个车友都参与不了 → 拦（派进去等于纯赠送 · §8.23）
func TestPlanSettlement_AllUnpayableBlocks(t *testing.T) {
	members := []settlementMember{
		{passengerID: "me", sharePct: 40, balance: 100_000_000},
		{passengerID: "a", sharePct: 30, balance: 1},
		{passengerID: "b", sharePct: 30, balance: 100_000_000, suspended: true},
	}
	_, err := planSettlement(members, "me", 100_000_000)
	if !errors.Is(err, ErrNoPayableMember) {
		t.Errorf("全员参与不了应返 ErrNoPayableMember · got=%v", err)
	}
}

// 派入者自己的 share_pct 不摊给别人（他承担自己那份）
func TestPlanSettlement_AssignerBearsOwnShare(t *testing.T) {
	members := []settlementMember{
		{passengerID: "me", sharePct: 50, balance: 100_000_000},
		{passengerID: "other", sharePct: 50, balance: 100_000_000},
	}
	st, err := planSettlement(members, "me", 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	assertBalanced(t, st)
	// 只收 other 那 50% · 剩下 50% 是我自己该承担的
	if st.Income != 50_000_000 {
		t.Errorf("应只收对方那份 · got=%d want=50_000_000", st.Income)
	}
}

// share_pct=0 的成员没份额可付（不该出现在 payers 也不该算 Lost）
func TestPlanSettlement_ZeroSharePctMemberPaysNothing(t *testing.T) {
	members := []settlementMember{
		{passengerID: "me", sharePct: 60, balance: 100_000_000},
		{passengerID: "real", sharePct: 40, balance: 100_000_000},
		{passengerID: "zero", sharePct: 0, balance: 100_000_000},
	}
	st, err := planSettlement(members, "me", 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	assertBalanced(t, st)
	if st.Income != 40_000_000 {
		t.Errorf("只有 real 付 40%% · got=%d", st.Income)
	}
	for _, p := range st.Payers {
		if p.PassengerID == "zero" {
			t.Error("share_pct=0 的不该在 payers 里")
		}
	}
}

// 结果可复现（map/slice 顺序不影响金额）
func TestPlanSettlement_Deterministic(t *testing.T) {
	members := []settlementMember{
		{passengerID: "me", sharePct: 25, balance: 100_000_000},
		{passengerID: "c", sharePct: 25, balance: 100_000_000},
		{passengerID: "a", sharePct: 25, balance: 100_000_000},
		{passengerID: "b", sharePct: 25, balance: 100_000_000},
	}
	first, err := planSettlement(members, "me", 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		got, _ := planSettlement(members, "me", 100_000_000)
		if got.Income != first.Income {
			t.Fatalf("清算不确定 · %d vs %d", first.Income, got.Income)
		}
		for j := range got.Payers {
			if got.Payers[j].PassengerID != first.Payers[j].PassengerID {
				t.Fatal("payers 顺序不确定")
			}
		}
	}
}

// 成本为 0 → 报错（调用方该在这之前挡掉）
func TestPlanSettlement_ZeroCostRejected(t *testing.T) {
	members := []settlementMember{
		{passengerID: "me", sharePct: 50, balance: 100},
		{passengerID: "a", sharePct: 50, balance: 100},
	}
	if _, err := planSettlement(members, "me", 0); err == nil {
		t.Error("成本 0 应报错")
	}
}

// 余额刚好够 → 不该被跳过（边界）
func TestPlanSettlement_ExactBalanceParticipates(t *testing.T) {
	members := []settlementMember{
		{passengerID: "me", sharePct: 50, balance: 100_000_000},
		{passengerID: "exact", sharePct: 50, balance: 50_000_000}, // 刚好
	}
	st, err := planSettlement(members, "me", 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	assertBalanced(t, st)
	if len(st.Payers) != 1 || st.Payers[0].PassengerID != "exact" {
		t.Errorf("余额刚好够该参与 · got=%+v", st.Payers)
	}
}
