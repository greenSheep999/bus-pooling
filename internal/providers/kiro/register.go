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
	"github.com/bus-pooling/bus-pooling/internal/providers/kiro/vendors/kiroappcc"
	"github.com/bus-pooling/bus-pooling/internal/providers/kiro/vendors/kiroappio"
	"github.com/bus-pooling/bus-pooling/internal/providers/kiro/vendors/kiroceo"
	"github.com/bus-pooling/bus-pooling/internal/providers/kiro/vendors/kirodrop"
	"github.com/bus-pooling/bus-pooling/internal/providers/kiro/vendors/kirooo"
)

// Config 是 kiro provider 下各家的配置。
//
// 6 家 vendor（1a: kiro91 · 1b: kiroceo/kirooo/kiroappio/kirodrop/kiroappcc）。
// **不预留空字段**：加一家时同时加 config 和 adapter · 省得留一堆没实现的开关。
type Config struct {
	Kiro91    VendorConfig
	KiroCEO   VendorConfig
	KiroOOO   VendorConfig
	KiroAppIO VendorConfig
	KiroAppCC VendorConfig
	KiroDrop  VendorConfig
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

	// kiro91
	a91, err := kiro91.New(kiro91.Config{
		BaseURL:       cfg.Kiro91.BaseURL,
		APIKey:        cfg.Kiro91.APIKey,
		WebhookSecret: cfg.Kiro91.WebhookSecret,
		Timeout:       cfg.Kiro91.Timeout, MaxRetries: cfg.Kiro91.MaxRetries,
		ProxyURL: cfg.Kiro91.ProxyURL, NoProxy: cfg.Kiro91.NoProxy,
	})
	if err != nil {
		return fmt.Errorf("kiro: 构建 kiro91: %w", err)
	}
	if err := r.Register(a91, cfg.Kiro91.Enabled); err != nil {
		return fmt.Errorf("kiro: 注册 kiro91: %w", err)
	}

	// kiroceo
	if cfg.KiroCEO.BaseURL != "" {
		aCEO, err := kiroceo.New(kiroceo.Config{
			BaseURL: cfg.KiroCEO.BaseURL, APIKey: cfg.KiroCEO.APIKey,
			WebhookSecret: cfg.KiroCEO.WebhookSecret,
			Timeout:       cfg.KiroCEO.Timeout, MaxRetries: cfg.KiroCEO.MaxRetries,
			ProxyURL: cfg.KiroCEO.ProxyURL, NoProxy: cfg.KiroCEO.NoProxy,
		})
		if err != nil {
			return fmt.Errorf("kiro: 构建 kiroceo: %w", err)
		}
		if err := r.Register(aCEO, cfg.KiroCEO.Enabled); err != nil {
			return fmt.Errorf("kiro: 注册 kiroceo: %w", err)
		}
	}

	// kirooo
	if cfg.KiroOOO.BaseURL != "" {
		aOOO, err := kirooo.New(kirooo.Config{
			BaseURL: cfg.KiroOOO.BaseURL, APIKey: cfg.KiroOOO.APIKey,
			WebhookSecret: cfg.KiroOOO.WebhookSecret,
			Timeout:       cfg.KiroOOO.Timeout, MaxRetries: cfg.KiroOOO.MaxRetries,
			ProxyURL: cfg.KiroOOO.ProxyURL, NoProxy: cfg.KiroOOO.NoProxy,
		})
		if err != nil {
			return fmt.Errorf("kiro: 构建 kirooo: %w", err)
		}
		if err := r.Register(aOOO, cfg.KiroOOO.Enabled); err != nil {
			return fmt.Errorf("kiro: 注册 kirooo: %w", err)
		}
	}

	// kiroappio
	if cfg.KiroAppIO.BaseURL != "" {
		aIO, err := kiroappio.New(kiroappio.Config{
			BaseURL: cfg.KiroAppIO.BaseURL, APIKey: cfg.KiroAppIO.APIKey,
			WebhookSecret: cfg.KiroAppIO.WebhookSecret,
			Timeout:       cfg.KiroAppIO.Timeout, MaxRetries: cfg.KiroAppIO.MaxRetries,
			ProxyURL: cfg.KiroAppIO.ProxyURL, NoProxy: cfg.KiroAppIO.NoProxy,
		})
		if err != nil {
			return fmt.Errorf("kiro: 构建 kiroappio: %w", err)
		}
		if err := r.Register(aIO, cfg.KiroAppIO.Enabled); err != nil {
			return fmt.Errorf("kiro: 注册 kiroappio: %w", err)
		}
	}

	// kiroappcc
	if cfg.KiroAppCC.BaseURL != "" {
		aCC, err := kiroappcc.New(kiroappcc.Config{
			BaseURL: cfg.KiroAppCC.BaseURL, APIKey: cfg.KiroAppCC.APIKey,
			WebhookSecret: cfg.KiroAppCC.WebhookSecret,
			Timeout:       cfg.KiroAppCC.Timeout, MaxRetries: cfg.KiroAppCC.MaxRetries,
			ProxyURL: cfg.KiroAppCC.ProxyURL, NoProxy: cfg.KiroAppCC.NoProxy,
		})
		if err != nil {
			return fmt.Errorf("kiro: 构建 kiroappcc: %w", err)
		}
		if err := r.Register(aCC, cfg.KiroAppCC.Enabled); err != nil {
			return fmt.Errorf("kiro: 注册 kiroappcc: %w", err)
		}
	}

	// kirodrop
	if cfg.KiroDrop.BaseURL != "" {
		aDrop, err := kirodrop.New(kirodrop.Config{
			BaseURL: cfg.KiroDrop.BaseURL, APIKey: cfg.KiroDrop.APIKey,
			WebhookSecret: cfg.KiroDrop.WebhookSecret,
			Timeout:       cfg.KiroDrop.Timeout, MaxRetries: cfg.KiroDrop.MaxRetries,
			ProxyURL: cfg.KiroDrop.ProxyURL, NoProxy: cfg.KiroDrop.NoProxy,
		})
		if err != nil {
			return fmt.Errorf("kiro: 构建 kirodrop: %w", err)
		}
		if err := r.Register(aDrop, cfg.KiroDrop.Enabled); err != nil {
			return fmt.Errorf("kiro: 注册 kirodrop: %w", err)
		}
	}

	return nil
}
