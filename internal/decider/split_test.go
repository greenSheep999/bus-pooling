package decider

import (
	"errors"
	"testing"
)

// 分摊之和必须恒等于总额 —— 钱不能凭空多也不能少。所有用例都断言这条。
func assertSumEqualsTotal(t *testing.T, p SplitPlan) {
	t.Helper()
	var sum int64
	for _, x := range p.Participants {
		sum += x.Amount
	}
	if sum != p.Total {
		t.Errorf("分摊之和 %d != 总额 %d", sum, p.Total)
	}
}

// 2 人各 50% 都有钱 → 各付一半
func TestPlanSplit_TwoMembersEven(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 50, balance: 100_000_000},
		{passengerID: "b", sharePct: 50, balance: 100_000_000},
	}
	p, err := planSplit(members, "a", 60_000_000, 4)
	if err != nil {
		t.Fatal(err)
	}
	assertSumEqualsTotal(t, p)
	if len(p.Participants) != 2 {
		t.Fatalf("参与人数 = %d · want 2", len(p.Participants))
	}
	if p.AmountFor("a") != 30_000_000 || p.AmountFor("b") != 30_000_000 {
		t.Errorf("应各付 30_000_000 · got a=%d b=%d", p.AmountFor("a"), p.AmountFor("b"))
	}
	// 号数也该均分
	keys := p.KeysMap()
	if keys["a"]+keys["b"] != 4 {
		t.Errorf("号数之和 = %d · want 4", keys["a"]+keys["b"])
	}
}

// 1 人车 → 自己付全额（回归：不能因为加了分摊把单人车搞坏）
func TestPlanSplit_SingleMemberPaysAll(t *testing.T) {
	members := []splitMember{{passengerID: "a", sharePct: 100, balance: 100_000_000}}
	p, err := planSplit(members, "a", 31_500_000, 3)
	if err != nil {
		t.Fatal(err)
	}
	assertSumEqualsTotal(t, p)
	if p.AmountFor("a") != 31_500_000 {
		t.Errorf("单人应付全额 · got=%d", p.AmountFor("a"))
	}
	if p.KeysMap()["a"] != 3 {
		t.Errorf("单人应拿全部号 · got=%d", p.KeysMap()["a"])
	}
}

// 3 人 · 一人余额不足 → 跳过他 · 剩 2 人重新归一化付完整轮
func TestPlanSplit_SkipsBrokeMemberAndRenormalizes(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 34, balance: 100_000_000},
		{passengerID: "b", sharePct: 33, balance: 100_000_000},
		{passengerID: "c", sharePct: 33, balance: 1}, // 穷
	}
	p, err := planSplit(members, "a", 60_000_000, 6)
	if err != nil {
		t.Fatal(err)
	}
	assertSumEqualsTotal(t, p)
	if len(p.Participants) != 2 {
		t.Fatalf("应剩 2 人 · got=%d", len(p.Participants))
	}
	if p.AmountFor("c") != 0 {
		t.Errorf("被跳过的人不该付钱 · got=%d", p.AmountFor("c"))
	}
	if len(p.Skipped) != 1 || p.Skipped[0].PassengerID != "c" {
		t.Errorf("skipped 应含 c · got=%+v", p.Skipped)
	}
	if p.Skipped[0].Reason != "insufficient_balance" {
		t.Errorf("跳过原因 = %q", p.Skipped[0].Reason)
	}
	// 剩下两人分完 60_000_000（不是各付 33% 只凑到 40%）
	if p.AmountFor("a")+p.AmountFor("b") != 60_000_000 {
		t.Errorf("剩余成员应付完整轮 · got=%d",
			p.AmountFor("a")+p.AmountFor("b"))
	}
}

// 挂起的成员不参与分摊也不给号（§8.26）
func TestPlanSplit_SuspendedExcluded(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 50, balance: 100_000_000},
		{passengerID: "b", sharePct: 50, balance: 100_000_000, suspended: true},
	}
	p, err := planSplit(members, "a", 40_000_000, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertSumEqualsTotal(t, p)
	if p.AmountFor("b") != 0 {
		t.Errorf("挂起的人不该付钱 · got=%d", p.AmountFor("b"))
	}
	if p.AmountFor("a") != 40_000_000 {
		t.Errorf("剩下的人付全额 · got=%d", p.AmountFor("a"))
	}
	if p.KeysMap()["b"] != 0 {
		t.Errorf("挂起的人不该拿号 · got=%d", p.KeysMap()["b"])
	}
	if len(p.Skipped) != 1 || p.Skipped[0].Reason != "suspended" {
		t.Errorf("skipped 应标 suspended · got=%+v", p.Skipped)
	}
}

// 发起人自己付不起 → 整轮失败（不能让别人替他垫 · §8.18）
func TestPlanSplit_InitiatorBrokeFailsRound(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 50, balance: 1}, // 发起人没钱
		{passengerID: "b", sharePct: 50, balance: 100_000_000},
	}
	_, err := planSplit(members, "a", 60_000_000, 4)
	if !errors.Is(err, ErrInitiatorInsufficient) {
		t.Errorf("发起人没钱应整轮失败 · got=%v", err)
	}
}

// 全员余额不足 → 整轮失败
func TestPlanSplit_AllBrokeFails(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 50, balance: 1},
		{passengerID: "b", sharePct: 50, balance: 1},
	}
	_, err := planSplit(members, "a", 60_000_000, 4)
	if err == nil {
		t.Error("全员没钱应失败")
	}
}

// 全员挂起 → 没有可分摊成员
func TestPlanSplit_AllSuspendedFails(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 50, balance: 100_000_000, suspended: true},
		{passengerID: "b", sharePct: 50, balance: 100_000_000, suspended: true},
	}
	_, err := planSplit(members, "a", 60_000_000, 4)
	if !errors.Is(err, ErrNoPayableMember) {
		t.Errorf("全挂起应返 ErrNoPayableMember · got=%v", err)
	}
}

// 除不尽时余数不能丢 · 3 人分 100（33.33...）
func TestPlanSplit_RemainderNotLost(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 34, balance: 1_000_000},
		{passengerID: "b", sharePct: 33, balance: 1_000_000},
		{passengerID: "c", sharePct: 33, balance: 1_000_000},
	}
	p, err := planSplit(members, "a", 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	assertSumEqualsTotal(t, p) // 关键：100 一分不少
}

// share_pct 不均（车主 60 · 成员 40）→ 按比例付
func TestPlanSplit_UnevenShares(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 60, balance: 100_000_000},
		{passengerID: "b", sharePct: 40, balance: 100_000_000},
	}
	p, err := planSplit(members, "a", 100_000_000, 10)
	if err != nil {
		t.Fatal(err)
	}
	assertSumEqualsTotal(t, p)
	if p.AmountFor("a") != 60_000_000 || p.AmountFor("b") != 40_000_000 {
		t.Errorf("应按 60/40 分 · got a=%d b=%d", p.AmountFor("a"), p.AmountFor("b"))
	}
	keys := p.KeysMap()
	if keys["a"] != 6 || keys["b"] != 4 {
		t.Errorf("号数应按 6/4 分 · got a=%d b=%d", keys["a"], keys["b"])
	}
}

// share_pct 全 0（脏数据）→ 均分兜底·不炸
func TestPlanSplit_ZeroSharesFallsBackToEven(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 0, balance: 100_000_000},
		{passengerID: "b", sharePct: 0, balance: 100_000_000},
	}
	p, err := planSplit(members, "a", 50_000_000, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertSumEqualsTotal(t, p)
	if p.AmountFor("a") != 25_000_000 || p.AmountFor("b") != 25_000_000 {
		t.Errorf("全 0 share 应均分 · got a=%d b=%d", p.AmountFor("a"), p.AmountFor("b"))
	}
}

// 刚好够付自己那份（边界：balance == amount）→ 不该被跳过
func TestPlanSplit_ExactBalanceNotSkipped(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 50, balance: 30_000_000},
		{passengerID: "b", sharePct: 50, balance: 30_000_000}, // 刚好
	}
	p, err := planSplit(members, "a", 60_000_000, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertSumEqualsTotal(t, p)
	if len(p.Participants) != 2 {
		t.Errorf("余额刚好够不该被跳过 · got=%d 人", len(p.Participants))
	}
}

// 连环踢：踢掉一个后·剩下的人份额变大又付不起 → 继续踢到收敛
func TestPlanSplit_CascadingSkip(t *testing.T) {
	members := []splitMember{
		{passengerID: "rich", sharePct: 25, balance: 100_000_000},
		{passengerID: "mid", sharePct: 25, balance: 30_000_000}, // 25% 够·50% 不够
		{passengerID: "poor1", sharePct: 25, balance: 1},
		{passengerID: "poor2", sharePct: 25, balance: 1},
	}
	// 总额 100_000_000：4 人各 25% = 25_000_000（mid 够）
	// 踢掉 2 个 poor 后 · 剩 2 人各 50% = 50_000_000（mid 不够 → 再踢）
	// 最后 rich 一人付 100_000_000（够）
	p, err := planSplit(members, "rich", 100_000_000, 4)
	if err != nil {
		t.Fatal(err)
	}
	assertSumEqualsTotal(t, p)
	if len(p.Participants) != 1 || p.Participants[0].PassengerID != "rich" {
		t.Errorf("应收敛到只剩 rich · got=%+v", p.Participants)
	}
	if len(p.Skipped) != 3 {
		t.Errorf("应跳过 3 人 · got=%d", len(p.Skipped))
	}
}

// SplitMap / KeysMap 形状对（落库用）
func TestSplitPlan_Maps(t *testing.T) {
	members := []splitMember{
		{passengerID: "a", sharePct: 50, balance: 100_000_000},
		{passengerID: "b", sharePct: 50, balance: 100_000_000},
	}
	p, _ := planSplit(members, "a", 60_000_000, 4)
	sm := p.SplitMap()
	if sm["a"] != 30_000_000 || sm["b"] != 30_000_000 {
		t.Errorf("SplitMap 不对 · got=%+v", sm)
	}
	km := p.KeysMap()
	if km["a"]+km["b"] != 4 {
		t.Errorf("KeysMap 号数之和不对 · got=%+v", km)
	}
}
