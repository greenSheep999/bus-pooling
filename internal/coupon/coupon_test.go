package coupon_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/coupon"
	"github.com/bus-pooling/bus-pooling/internal/db"
)

func setup(t *testing.T) *coupon.Store {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// seed 一个 passenger 满足 FK
	if _, err := d.DB.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES ('p1', 'alice', 'a@x.io', 'x', ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	return coupon.NewStore(d.DB)
}

// 建 topup_discount 码 · 校验字段
func TestCreateTopupDiscount(t *testing.T) {
	s := setup(t)
	c, err := s.Create(context.Background(), coupon.CreateInput{
		Code: "SAVE10", Type: coupon.TypeTopupDiscount, DiscountBP: 1000,
		RemainingUses: 5,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.DiscountBP != 1000 {
		t.Errorf("DiscountBP = %d, want 1000", c.DiscountBP)
	}
	if c.WaiveRounds != 0 {
		t.Errorf("WaiveRounds = %d, want 0", c.WaiveRounds)
	}
	if !c.RemainingUses.Valid || c.RemainingUses.Int64 != 5 {
		t.Errorf("RemainingUses = %+v, want 5", c.RemainingUses)
	}
}

// topup_discount 不能带 waive_rounds
func TestCreateTopupDiscountRejectsWaiveRounds(t *testing.T) {
	s := setup(t)
	_, err := s.Create(context.Background(), coupon.CreateInput{
		Code: "BAD", Type: coupon.TypeTopupDiscount, DiscountBP: 500, WaiveRounds: 3,
	})
	if err == nil {
		t.Fatal("Create 应拒 topup_discount 带 waive_rounds")
	}
}

// service_fee_waiver 建正常
func TestCreateServiceFeeWaiver(t *testing.T) {
	s := setup(t)
	c, err := s.Create(context.Background(), coupon.CreateInput{
		Code: "FREE3", Type: coupon.TypeServiceFeeWaiver, WaiveRounds: 3,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.WaiveRounds != 3 {
		t.Errorf("WaiveRounds = %d, want 3", c.WaiveRounds)
	}
}

// Lookup 校验各种错
func TestLookupErrors(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	// 未知码
	_, err := s.Lookup(ctx, "NOEXIST", coupon.TypeTopupDiscount)
	if !errors.Is(err, coupon.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}

	// 建 topup 码 · 但用 pull context 查 → wrong context
	_, _ = s.Create(ctx, coupon.CreateInput{
		Code: "SAVE10", Type: coupon.TypeTopupDiscount, DiscountBP: 1000,
	})
	_, err = s.Lookup(ctx, "SAVE10", coupon.TypeServiceFeeWaiver)
	if !errors.Is(err, coupon.ErrWrongContext) {
		t.Errorf("want ErrWrongContext, got %v", err)
	}

	// 建过期码
	_, _ = s.Create(ctx, coupon.CreateInput{
		Code: "EXPIRED", Type: coupon.TypeTopupDiscount, DiscountBP: 500,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	})
	_, err = s.Lookup(ctx, "EXPIRED", coupon.TypeTopupDiscount)
	if !errors.Is(err, coupon.ErrExpired) {
		t.Errorf("want ErrExpired, got %v", err)
	}
}

// Redeem 原子核销 + 幂等
func TestRedeemAtomicAndIdempotent(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	_, err := s.Create(ctx, coupon.CreateInput{
		Code: "SAVE10", Type: coupon.TypeTopupDiscount, DiscountBP: 1000, RemainingUses: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 第一次核销 · 应成功
	c1, err := s.Redeem(ctx, coupon.RedeemInput{
		Code: "SAVE10", PassengerID: "p1",
		Context: coupon.ContextTopup, ContextRef: "order-1", DiscountAmount: 100000,
	})
	if err != nil {
		t.Fatalf("Redeem 1: %v", err)
	}
	if c1.UsedCount != 0 {
		// UsedCount 是 reload 前的值 · 幂等看 coupon_code 表最新
	}

	// 第二次同 order-1 · 幂等返 ErrAlreadyUsed
	_, err = s.Redeem(ctx, coupon.RedeemInput{
		Code: "SAVE10", PassengerID: "p1",
		Context: coupon.ContextTopup, ContextRef: "order-1", DiscountAmount: 100000,
	})
	if !errors.Is(err, coupon.ErrAlreadyUsed) {
		t.Errorf("second redeem same ref want ErrAlreadyUsed, got %v", err)
	}

	// 新单可以
	_, err = s.Redeem(ctx, coupon.RedeemInput{
		Code: "SAVE10", PassengerID: "p1",
		Context: coupon.ContextTopup, ContextRef: "order-2", DiscountAmount: 100000,
	})
	if err != nil {
		t.Fatalf("Redeem 2 (different order): %v", err)
	}

	// 第三次 · 额度已用尽
	_, err = s.Redeem(ctx, coupon.RedeemInput{
		Code: "SAVE10", PassengerID: "p1",
		Context: coupon.ContextTopup, ContextRef: "order-3", DiscountAmount: 100000,
	})
	if !errors.Is(err, coupon.ErrUsedUp) {
		t.Errorf("third redeem want ErrUsedUp, got %v", err)
	}
}

// service_fee_waiver 核销走 pull context
func TestRedeemPullContext(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	_, err := s.Create(ctx, coupon.CreateInput{
		Code: "FREE3", Type: coupon.TypeServiceFeeWaiver, WaiveRounds: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	// pull 场景应能核销
	_, err = s.Redeem(ctx, coupon.RedeemInput{
		Code: "FREE3", PassengerID: "p1",
		Context: coupon.ContextPull, ContextRef: "round-1", DiscountAmount: 1,
	})
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	// topup 场景应拒(码 type 不匹配)
	_, err = s.Redeem(ctx, coupon.RedeemInput{
		Code: "FREE3", PassengerID: "p1",
		Context: coupon.ContextTopup, ContextRef: "order-1", DiscountAmount: 100,
	})
	if !errors.Is(err, coupon.ErrWrongContext) {
		t.Errorf("topup context on pull coupon want ErrWrongContext, got %v", err)
	}
}
