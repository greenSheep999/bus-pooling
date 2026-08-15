package api

// topup_coupon_test · 优惠码 API 层集成 · decisions §8.43 v2
//
// 主要验:
//   - translateCouponErr 把 coupon 错误翻译成 4xx Fail
//   - handleCouponLookup 400 / 200 契约(pending: 需要完整 test env · 只做最简)

import (
	"errors"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/coupon"
)

// 翻译层 · 各 coupon 错映射到对应用户可见文案
func TestTranslateCouponErr(t *testing.T) {
	cases := []struct {
		in   error
		want string
	}{
		{coupon.ErrNotFound, "优惠码不存在"},
		{coupon.ErrDisabled, "优惠码已停用"},
		{coupon.ErrExpired, "优惠码已过期"},
		{coupon.ErrUsedUp, "优惠码额度已用尽"},
		{coupon.ErrWrongContext, "优惠码不适用此场景"},
	}
	for _, c := range cases {
		err := translateCouponErr(c.in)
		if err == nil {
			t.Errorf("in=%v got nil", c.in)
			continue
		}
		var f *Fail
		if !errors.As(err, &f) {
			t.Errorf("in=%v got %T not *Fail", c.in, err)
			continue
		}
		if f.Err == nil || f.Err.Message != c.want {
			t.Errorf("in=%v got msg=%q want %q", c.in, f.Err.Message, c.want)
		}
	}
}

// 未知错走 default 分支返原 err
func TestTranslateCouponErrUnknown(t *testing.T) {
	orig := errors.New("some internal db err")
	got := translateCouponErr(orig)
	if !errors.Is(got, orig) {
		t.Errorf("未知错应返原 err · got %v", got)
	}
}
