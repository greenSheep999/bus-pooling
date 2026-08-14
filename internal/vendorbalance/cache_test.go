package vendorbalance

// **回归哨兵 · P5 · 2026-08-14**
//
// 上游余额预检 · 老代码没做 —— vendor 没钱只能等 insufficient_balance 被动失败。
// 修：Cache 5min poll · decider.Pull 前 Enough 查一次 · 不够拒。

import (
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// Get 未命中 · Enough 保守放行（不误伤）
func TestEnough_NoData(t *testing.T) {
	c := New(Config{})
	ok, remain := c.Enough(providers.Vendor91Kiro, 100_000_000)
	if !ok {
		t.Error("未 poll 过应保守放行")
	}
	if remain != 0 {
		t.Errorf("remain 应 0 · 得 %d", remain)
	}
}

// 余额充足 · 放行
func TestEnough_Sufficient(t *testing.T) {
	c := New(Config{})
	c.mu.Lock()
	c.balances[providers.Vendor91Kiro] = Balance{
		VendorID: providers.Vendor91Kiro,
		Amount:   500_000_000,
		Currency: providers.CurrencyCredit,
	}
	c.mu.Unlock()

	ok, remain := c.Enough(providers.Vendor91Kiro, 100_000_000)
	if !ok {
		t.Error("500 microunit ≥ 100 · 应放行")
	}
	if remain != 500_000_000 {
		t.Errorf("remain = %d · 应返实际余额", remain)
	}
}

// 余额不足 · 拦
func TestEnough_Insufficient(t *testing.T) {
	c := New(Config{})
	c.mu.Lock()
	c.balances[providers.Vendor91Kiro] = Balance{
		VendorID: providers.Vendor91Kiro,
		Amount:   50_000_000,
		Currency: providers.CurrencyCredit,
	}
	c.mu.Unlock()

	ok, remain := c.Enough(providers.Vendor91Kiro, 100_000_000)
	if ok {
		t.Error("50 < 100 · 应拦")
	}
	if remain != 50_000_000 {
		t.Errorf("remain 应返实际余额 · 得 %d", remain)
	}
}

// USD 家不同币种 · 保守放行（未换算 · 让 vendor 自己拒）
func TestEnough_USDVendorSkips(t *testing.T) {
	c := New(Config{})
	c.mu.Lock()
	c.balances[providers.VendorKiroDrop] = Balance{
		VendorID: providers.VendorKiroDrop,
		Amount:   10_000_000, // USD $10 · 但按积分口径不能直接比
		Currency: providers.CurrencyUSD,
	}
	c.mu.Unlock()

	ok, remain := c.Enough(providers.VendorKiroDrop, 100_000_000)
	if !ok {
		t.Error("USD 币种未换算 · 应保守放行 · 不误伤")
	}
	if remain != 10_000_000 {
		t.Errorf("remain 应返原值 · 得 %d", remain)
	}
}

// Get / Enough 对 nil cache 安全
func TestNilCache(t *testing.T) {
	var c *Cache
	if _, ok := c.Get(providers.Vendor91Kiro); ok {
		t.Error("nil cache Get 应返 false")
	}
	ok, _ := c.Enough(providers.Vendor91Kiro, 100)
	if !ok {
		t.Error("nil cache Enough 应保守放行")
	}
}
