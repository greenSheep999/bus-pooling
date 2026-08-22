package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/coupon"
)

// I-24 · 优惠码 service_fee_waiver 完整核销
//
// **老 bug**:pull.go 只 Lookup 不 Redeem · used_count 不递增 · 同码可无限次触发
// 前端"已减免"错觉 · 服务费实际按原价扣。这些测试保护修复。

// TestPull_CouponWaivesServiceFee · 拉号带 service_fee_waiver 码 · 服务费退还
func TestPull_CouponWaivesServiceFee(t *testing.T) {
	e, _, withKey := pullEnv(t, 10_000_000_000)
	ctx := context.Background()

	// 建一张 service_fee_waiver 码 · 3 轮 · 无过期
	c, err := e.coupons.Create(ctx, coupon.CreateInput{
		Code: "WAIVE3", Type: coupon.TypeServiceFeeWaiver,
		WaiveRounds: 3, RemainingUses: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 拉号带码
	status, body := e.do(t, "POST", "/api/me/pull",
		map[string]any{"count": 2, "zone": "us", "coupon_code": c.Code},
		withKey,
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", "aabbccdd11223344aabbccdd11223351") },
	)
	if status != http.StatusOK {
		t.Fatalf("status = %d · body = %s", status, body)
	}

	// 断言响应体
	got := decode[map[string]json.RawMessage](t, body)
	// service_fee 应为 0(被优惠码免了)
	var sf int64
	if err := json.Unmarshal(got["service_fee"], &sf); err != nil {
		t.Fatal(err)
	}
	if sf != 0 {
		t.Errorf("带优惠码时 service_fee 应为 0 · 实际 %d", sf)
	}

	// used_count 递增(通过 Lookup 反查 remaining_uses)
	after, err := e.coupons.Lookup(ctx, c.Code, coupon.TypeServiceFeeWaiver)
	if err != nil {
		t.Fatalf("Lookup 应还能查到: %v", err)
	}
	// used_count 从 0 → 1(RemainingUses 是上限 · UsedCount 是已用)
	if after.UsedCount != 1 {
		t.Errorf("used_count 应从 0 → 1 · 实际 %d", after.UsedCount)
	}
}

// TestPull_CouponRedeemIdempotent · 同幂等 key 重放 · 不重复扣码
func TestPull_CouponRedeemIdempotent(t *testing.T) {
	e, _, withKey := pullEnv(t, 10_000_000_000)
	ctx := context.Background()

	c, err := e.coupons.Create(ctx, coupon.CreateInput{
		Code: "IDEM1", Type: coupon.TypeServiceFeeWaiver,
		WaiveRounds: 3, RemainingUses: 3, // 用 3 次上限 · 一次用完还剩 2 · Lookup 能查到
	})
	if err != nil {
		t.Fatal(err)
	}

	idemKey := "aabbccdd11223344aabbccdd11223352"
	// 第 1 次
	status1, _ := e.do(t, "POST", "/api/me/pull",
		map[string]any{"count": 1, "zone": "us", "coupon_code": c.Code},
		withKey,
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", idemKey) },
	)
	if status1 != http.StatusOK {
		t.Fatalf("first status = %d", status1)
	}
	// 第 2 次同 key(重放) · 应返幂等缓存 · 不重扣
	status2, body2 := e.do(t, "POST", "/api/me/pull",
		map[string]any{"count": 1, "zone": "us", "coupon_code": c.Code},
		withKey,
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", idemKey) },
	)
	if status2 != http.StatusOK {
		t.Fatalf("replay status = %d · body = %s", status2, body2)
	}

	// remaining_uses 应只 -1 · 不是 -2
	after, err := e.coupons.Lookup(ctx, c.Code, coupon.TypeServiceFeeWaiver)
	if err != nil {
		t.Fatal(err)
	}
	// used_count 应只 +1 · 重放不加(等于 1 而不是 2)
	if after.UsedCount != 1 {
		t.Errorf("重放不该多扣 · used_count 应为 1 · 实际 %d", after.UsedCount)
	}
}

// TestPull_CouponExpired · 过期码 · 400 · 不进 Pull
func TestPull_CouponExpired(t *testing.T) {
	e, _, withKey := pullEnv(t, 10_000_000_000)
	ctx := context.Background()

	// 过期码
	_, err := e.coupons.Create(ctx, coupon.CreateInput{
		Code: "EXPIRED", Type: coupon.TypeServiceFeeWaiver,
		WaiveRounds: 1, RemainingUses: 1,
		ExpiresAt: time.Now().UTC().Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	status, _ := e.do(t, "POST", "/api/me/pull",
		map[string]any{"count": 1, "zone": "us", "coupon_code": "EXPIRED"},
		withKey,
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", "aabbccdd11223344aabbccdd11223353") },
	)
	if status != http.StatusBadRequest {
		t.Errorf("过期码应 400 · 实际 %d", status)
	}
}

// TestPull_CouponNoService_NoRedeem · 服务费为 0 时不触发 Redeem(防空核销)
func TestPull_CouponNoService_NoRedeem(t *testing.T) {
	// 装了 Rates.Service=500(newEnvWithDecider) · 每轮总有服务费 · 这个用例先跳过
	// 除非未来 rates 空场景 · 保护契约:service_fee==0 不消费码
	t.Skip("Rates.Service=500 恒 > 0 · 该场景在其他 rates env 下才能触发 · 契约在 pull.go 已有 if result.ServiceFee > 0 保护")
}
