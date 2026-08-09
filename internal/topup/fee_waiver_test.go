package topup

import (
	"context"
	"testing"
	"time"
)

// seedWaiver 给某乘客种 n 次可用减免额度
func seedWaiver(t *testing.T, s *Store, passengerID string, total int) {
	t.Helper()
	_, err := s.db.Exec(`
		INSERT INTO personal_invite_code
		  (code, passenger_id, invited_count, fee_waiver_total, fee_waiver_used,
		   created_at, updated_at)
		VALUES (?, ?, 1, ?, 0, '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`,
		"WAIVER"+passengerID[:2], passengerID, total)
	if err != nil {
		t.Fatal(err)
	}
}

func waiverUsed(t *testing.T, s *Store, passengerID string) int {
	t.Helper()
	var used int
	if err := s.db.QueryRow(
		`SELECT fee_waiver_used FROM personal_invite_code WHERE passenger_id = ?`,
		passengerID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	return used
}

// 有额度 → 起单时手续费归零 · 乘客只付本金 · 我方记 fee_subsidy
func TestCreateOrder_WaiverZeroesFee(t *testing.T) {
	ps, pid, _ := pendingTestDB(t)
	s := NewStore(ps.db)
	seedWaiver(t, s, pid, 3)

	o, _, err := s.CreateOrderWithPending(context.Background(), OrderInput{
		PassengerID: pid, Channel: "waffo", Credits: 100_000_000,
		PayURL: "https://x", TTL: 15 * time.Minute,
	}, "i-test")
	if err != nil {
		t.Fatal(err)
	}
	if !o.FeeWaiverApplied {
		t.Error("有额度时该用掉一次减免")
	}
	if o.ChannelFee != 0 {
		t.Errorf("减免后手续费该为 0 · got=%d", o.ChannelFee)
	}
	if o.Paid != o.Credits {
		t.Errorf("减免后只付本金 · paid=%d credits=%d", o.Paid, o.Credits)
	}
	// 我方垫付的记 fee_subsidy（5% of 100_000_000 = 5_000_000）
	if o.FeeSubsidy != 5_000_000 {
		t.Errorf("fee_subsidy = %d · want 5_000_000（我方垫付的那 5%%）", o.FeeSubsidy)
	}
	if used := waiverUsed(t, s, pid); used != 1 {
		t.Errorf("额度该用掉 1 次 · got=%d", used)
	}
}

// 没额度 → 正常收手续费
func TestCreateOrder_NoWaiverChargesFee(t *testing.T) {
	ps, pid, _ := pendingTestDB(t)
	s := NewStore(ps.db)
	// 不种额度

	o, _, err := s.CreateOrderWithPending(context.Background(), OrderInput{
		PassengerID: pid, Channel: "waffo", Credits: 100_000_000,
		PayURL: "https://x", TTL: 15 * time.Minute,
	}, "i-test")
	if err != nil {
		t.Fatal(err)
	}
	if o.FeeWaiverApplied {
		t.Error("没额度不该标记减免")
	}
	if o.ChannelFee != 5_000_000 {
		t.Errorf("正常该收 5%% 手续费 · got=%d", o.ChannelFee)
	}
	if o.Paid != 105_000_000 {
		t.Errorf("paid 该是本金+手续费 · got=%d", o.Paid)
	}
	if o.FeeSubsidy != 0 {
		t.Errorf("没减免不该有补贴 · got=%d", o.FeeSubsidy)
	}
}

// 额度用完 → 后续订单正常收费
func TestCreateOrder_WaiverExhausted(t *testing.T) {
	ps, pid, _ := pendingTestDB(t)
	s := NewStore(ps.db)
	seedWaiver(t, s, pid, 1) // 只有 1 次

	ctx := context.Background()
	mk := func(idem string) Order {
		o, _, err := s.CreateOrderWithPending(ctx, OrderInput{
			PassengerID: pid, Channel: "waffo", Credits: 100_000_000,
			PayURL: "https://x", TTL: 15 * time.Minute,
		}, idem)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	first := mk("i-test")
	if !first.FeeWaiverApplied || first.ChannelFee != 0 {
		t.Error("第一单该用掉额度")
	}
	second := mk("i-test")
	if second.FeeWaiverApplied {
		t.Error("额度用完第二单不该再减免")
	}
	if second.ChannelFee != 5_000_000 {
		t.Errorf("第二单该正常收费 · got=%d", second.ChannelFee)
	}
}

// 订单过期 → 额度退回（那次减免实际没发生）
func TestExpire_ReturnsWaiver(t *testing.T) {
	ps, pid, _ := pendingTestDB(t)
	s := NewStore(ps.db)
	seedWaiver(t, s, pid, 3)
	ctx := context.Background()

	o, pendingID, err := s.CreateOrderWithPending(ctx, OrderInput{
		PassengerID: pid, Channel: "waffo", Credits: 100_000_000,
		PayURL: "https://x", TTL: 15 * time.Minute,
	}, "i-test")
	if err != nil {
		t.Fatal(err)
	}
	if waiverUsed(t, s, pid) != 1 {
		t.Fatal("前置条件：起单该扣一次额度")
	}

	// 过期
	did, err := ps.ExpireBoth(ctx, pendingID, o.ID, PendingInitial)
	if err != nil {
		t.Fatal(err)
	}
	if !did {
		t.Fatal("该 expire 成功")
	}
	if used := waiverUsed(t, s, pid); used != 0 {
		t.Errorf("过期该退回额度 · fee_waiver_used = %d · want 0", used)
	}
}

// 重复 expire 不该重复退额度（幂等）
func TestExpire_ReturnsWaiverOnlyOnce(t *testing.T) {
	ps, pid, _ := pendingTestDB(t)
	s := NewStore(ps.db)
	seedWaiver(t, s, pid, 3)
	ctx := context.Background()

	o, pendingID, _ := s.CreateOrderWithPending(ctx, OrderInput{
		PassengerID: pid, Channel: "waffo", Credits: 100_000_000,
		PayURL: "https://x", TTL: 15 * time.Minute,
	}, "i-test")

	_, _ = ps.ExpireBoth(ctx, pendingID, o.ID, PendingInitial)
	// 再 expire 一次（并发 / janitor 重跑）
	_, _ = ps.ExpireBoth(ctx, pendingID, o.ID, PendingInitial)

	if used := waiverUsed(t, s, pid); used != 0 {
		t.Errorf("重复 expire 不该退成负数 · fee_waiver_used = %d · want 0", used)
	}
}

// 没用减免的订单过期 → 不动额度
func TestExpire_NoWaiverNoReturn(t *testing.T) {
	ps, pid, _ := pendingTestDB(t)
	s := NewStore(ps.db)
	seedWaiver(t, s, pid, 3)
	ctx := context.Background()

	// 先把额度耗光·让这单没减免
	for i := 0; i < 3; i++ {
		_, _, _ = s.CreateOrderWithPending(ctx, OrderInput{
			PassengerID: pid, Channel: "waffo", Credits: 10_000_000,
			PayURL: "https://x", TTL: 15 * time.Minute,
		}, "i-test")
	}
	if waiverUsed(t, s, pid) != 3 {
		t.Fatal("前置条件：额度该耗光")
	}

	o, pendingID, _ := s.CreateOrderWithPending(ctx, OrderInput{
		PassengerID: pid, Channel: "waffo", Credits: 100_000_000,
		PayURL: "https://x", TTL: 15 * time.Minute,
	}, "i-test")
	if o.FeeWaiverApplied {
		t.Fatal("前置条件：这单不该有减免")
	}

	_, _ = ps.ExpireBoth(ctx, pendingID, o.ID, PendingInitial)
	if used := waiverUsed(t, s, pid); used != 3 {
		t.Errorf("没用减免的单过期不该动额度 · got=%d want=3", used)
	}
}

// HasFeeWaiver 只读不消耗
func TestHasFeeWaiver_DoesNotConsume(t *testing.T) {
	ps, pid, _ := pendingTestDB(t)
	s := NewStore(ps.db)
	seedWaiver(t, s, pid, 2)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		has, err := s.HasFeeWaiver(ctx, pid)
		if err != nil {
			t.Fatal(err)
		}
		if !has {
			t.Fatal("该有额度")
		}
	}
	if used := waiverUsed(t, s, pid); used != 0 {
		t.Errorf("HasFeeWaiver 是只读的·不该消耗 · got=%d", used)
	}
}

// 没有个人邀请码的乘客 → 不报错·正常收费
func TestCreateOrder_NoPersonalCodeNoError(t *testing.T) {
	ps, pid, _ := pendingTestDB(t)
	s := NewStore(ps.db)
	// 完全不种 personal_invite_code

	has, err := s.HasFeeWaiver(context.Background(), pid)
	if err != nil {
		t.Fatalf("没个人码查额度不该报错: %v", err)
	}
	if has {
		t.Error("没个人码不该有额度")
	}
	o, _, err := s.CreateOrderWithPending(context.Background(), OrderInput{
		PassengerID: pid, Channel: "waffo", Credits: 100_000_000,
		PayURL: "https://x", TTL: 15 * time.Minute,
	}, "i-test")
	if err != nil {
		t.Fatalf("没个人码起单不该报错: %v", err)
	}
	if o.ChannelFee != 5_000_000 {
		t.Errorf("该正常收费 · got=%d", o.ChannelFee)
	}
}
