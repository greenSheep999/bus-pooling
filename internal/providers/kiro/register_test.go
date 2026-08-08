package kiro

import (
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

func testConfig(enabled bool) Config {
	return Config{
		Kiro91: VendorConfig{
			Enabled: enabled,
			BaseURL: "https://api.example.invalid",
			APIKey:  "usr-test",
			Timeout: 2 * time.Second,
		},
	}
}

func TestRegisterPutsKiro91InRegistry(t *testing.T) {
	r := providers.NewRegistry()
	if err := Register(r, testConfig(true)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	v, err := r.Get(providers.Vendor91Kiro)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.ProviderID() != providers.ProviderKiro {
		t.Errorf("ProviderID = %q，want %q", v.ProviderID(), providers.ProviderKiro)
	}
	if len(r.Enabled()) != 1 {
		t.Errorf("Enabled = %d，want 1", len(r.Enabled()))
	}
}

// Enabled=false 也要注册 —— 停用的 vendor 手上可能还有它拉来的号，
// deathwatch / 对账要能回头查它。
func TestRegisterKeepsDisabledVendorLookupable(t *testing.T) {
	r := providers.NewRegistry()
	if err := Register(r, testConfig(false)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := r.Get(providers.Vendor91Kiro); err != nil {
		t.Errorf("停用的 vendor 也应能 Get 到: %v", err)
	}
	if len(r.Enabled()) != 0 {
		t.Errorf("Enabled = %d，停用的不该出现", len(r.Enabled()))
	}
	if len(r.All()) != 1 {
		t.Errorf("All = %d，want 1", len(r.All()))
	}
}

func TestRegisterNilRegistry(t *testing.T) {
	if err := Register(nil, testConfig(true)); err == nil {
		t.Error("registry 为空应报错")
	}
}

// 同一个 registry 注册两次应该报错（Registry 拒绝重复），
// 防止装配代码被误调两遍后静默用了第二份配置。
func TestRegisterTwiceFails(t *testing.T) {
	r := providers.NewRegistry()
	if err := Register(r, testConfig(true)); err != nil {
		t.Fatalf("首次: %v", err)
	}
	if err := Register(r, testConfig(true)); err == nil {
		t.Error("重复 Register 应报错")
	}
}
