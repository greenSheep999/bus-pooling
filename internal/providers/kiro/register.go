// Package kiro 是 kiro provider 层 —— 把这个 provider 下的各家 vendor 注册进 Registry。
//
// **只有 main 装配时 import 这个包**。业务层（decider / pullrecord）只认
// providers.Registry 和 providers.Vendor，不 import 任何 vendor 具体包
// （契约 §10）—— 那样加一家 vendor 不用改调用点。
package kiro

import (
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/providers/kiro/vendors/kiro91"
)

// Config 是 kiro provider 下各家的配置。
//
// 阶段 1a 只有 kiro91 —— 其余 5 家在 1b 按契约 §7 的接入优先级依次加
// （kiroceo → kirooo → kiroappio → kirodrop → kiroappcc）。
// **不预留空字段**：加一家时同时加 config 和 adapter，省得留一堆没实现的开关。
type Config struct {
	Kiro91 VendorConfig
}

// VendorConfig 一家 vendor 的配置。
//
// APIKey / WebhookSecret 是**明文**，由 main 装配时从 secrets 解密后传进来 ——
// 这里不碰加密，也不从 env 读（那样测试就没法注入假值了）。
type VendorConfig struct {
	Enabled       bool
	BaseURL       string
	APIKey        string
	WebhookSecret string
	Timeout       time.Duration
	MaxRetries    int
	ProxyURL      string
	NoProxy       string
}

// Register 把 kiro 下的 vendor 装进 registry。
//
// **Enabled=false 也注册** —— 停用的 vendor 手上可能还有它拉来的号，
// deathwatch / 对账要能回头查它（见 Registry.Get 的注释）。
func Register(r *providers.Registry, cfg Config) error {
	if r == nil {
		return fmt.Errorf("kiro: registry 为空")
	}

	a, err := kiro91.New(kiro91.Config{
		BaseURL:       cfg.Kiro91.BaseURL,
		APIKey:        cfg.Kiro91.APIKey,
		WebhookSecret: cfg.Kiro91.WebhookSecret,
		Timeout:       cfg.Kiro91.Timeout,
		MaxRetries:    cfg.Kiro91.MaxRetries,
		ProxyURL:      cfg.Kiro91.ProxyURL,
		NoProxy:       cfg.Kiro91.NoProxy,
	})
	if err != nil {
		return fmt.Errorf("kiro: 构建 kiro91 adapter: %w", err)
	}
	if err := r.Register(a, cfg.Kiro91.Enabled); err != nil {
		return fmt.Errorf("kiro: 注册 kiro91: %w", err)
	}

	return nil
}
