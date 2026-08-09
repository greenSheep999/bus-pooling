package topupchannel

import (
	"errors"
	"testing"
)

func TestRegistry_DefaultsOnlyWaffoEnabled(t *testing.T) {
	r := New(nil)
	// 4 家全注册
	all := r.List()
	if len(all) != 4 {
		t.Fatalf("List() len=%d · want 4", len(all))
	}
	enabled := 0
	for _, c := range all {
		if c.Enabled {
			enabled++
			if c.ID != Waffo {
				t.Errorf("默认只主 hosted 渠道启·实际启了 %s", c.ID)
			}
		}
	}
	if enabled != 1 {
		t.Errorf("默认启用数 = %d · want 1", enabled)
	}
}

func TestRegistry_GetEnabled(t *testing.T) {
	r := New(nil)
	if _, err := r.GetEnabled("waffo"); err != nil {
		t.Errorf("默认 hosted 渠道应可用: %v", err)
	}
	_, err := r.GetEnabled("bybit")
	if !errors.Is(err, ErrDisabledChannel) {
		t.Errorf("bybit 应 disabled: %v", err)
	}
	_, err = r.GetEnabled("notreal")
	if !errors.Is(err, ErrUnknownChannel) {
		t.Errorf("未知 channel 应 ErrUnknownChannel: %v", err)
	}
}

func TestRegistry_Override(t *testing.T) {
	r := New(map[ID]bool{Bybit: true, Waffo: false})
	if c, _ := r.Get("bybit"); !c.Enabled {
		t.Error("bybit override 后应启用")
	}
	if c, _ := r.Get("waffo"); c.Enabled {
		t.Error("主渠道 override 后应关闭")
	}
}

func TestChannelAttributes(t *testing.T) {
	r := New(nil)
	// 三个正交维度定义合理性检查
	tests := []struct {
		id       ID
		region   Region
		rail     Rail
		provider string
	}{
		{Waffo, RegionOverseas, RailHosted, "waffo_checkout"},
		{EPUSDT, RegionOverseas, RailDirect, "epusdt_onchain"},
		{Bybit, RegionOverseas, RailDirect, "bybit_internal"},
		{Binance, RegionOverseas, RailDirect, "binance_internal"},
	}
	for _, tt := range tests {
		c, err := r.Get(string(tt.id))
		if err != nil {
			t.Fatalf("%s: %v", tt.id, err)
		}
		if c.Region != tt.region {
			t.Errorf("%s region = %s want %s", tt.id, c.Region, tt.region)
		}
		if c.Rail != tt.rail {
			t.Errorf("%s rail = %s want %s", tt.id, c.Rail, tt.rail)
		}
		if c.ProviderKind != tt.provider {
			t.Errorf("%s provider_kind = %s want %s", tt.id, c.ProviderKind, tt.provider)
		}
	}
}
