// Package topupchannel 是 topup 渠道**注册表** —— 不建表·纯 config 驱动。
//
// 三个正交维度：
//
//	channel  · 具体渠道 id（枚举·由 New 里注册的行决定）
//	region   · 地区（domestic 国内 · overseas 海外）
//	rail     · 到账方式（direct 乘客直转 / hosted 三方托管 checkout）
//
// 每个 channel 对应 payment-Gateway 侧的一个 rail（`provider_kind` 由 registry 里配）。
//
// **注册 vs 启用**：所有渠道都注册（前端可展示"即将开放"占位）·**只启用**
// 有 gateway 支持的。乘客发起 POST /api/me/topup 时·
// Server 查渠道是否 Enabled·关的返 400 "该渠道暂未开放"（术语铁律 §12.6）。
package topupchannel

import (
	"errors"
	"strings"
)

// Region 地区维度 · 前端先按 region 分区展示。
type Region string

const (
	RegionDomestic Region = "domestic"
	RegionOverseas Region = "overseas"
)

// Rail 到账方式维度 · direct 需乘客提供 payer_reference（Bybit UID / Binance ID / wallet 地址）
// · hosted 走 checkout URL 跳转支付。
type Rail string

const (
	RailDirect Rail = "direct"  // 乘客直转 · 我方对账
	RailHosted Rail = "hosted"  // 三方托管 checkout
)

// ID 具体渠道 id（stable · 会落 topup_order.channel）。
type ID string

const (
	Waffo   ID = "waffo"
	EPUSDT  ID = "epusdt"
	Bybit   ID = "bybit"
	Binance ID = "binance"
)

// Channel 一个渠道的完整属性。
type Channel struct {
	ID              ID     // 稳定标识（DB 存这个）
	DisplayName     string // 前端展示名（人类可读）
	Region          Region
	Rail            Rail
	ProviderKind    string // payment-Gateway 侧的 provider_kind
	Asset           string // gateway 侧结算币种（USD / USDT / CNY / ...）
	Enabled         bool   // 是否开放乘客发起（关的前端可展示但不能下单）
	// RequiresPayerReference · direct rail 通常需要（Bybit UID / Binance ID / 钱包地址）·
	// hosted rail 从 profile 拿（email）。前端根据这个决定确认窗要不要多问一个字段。
	RequiresPayerReference bool
	// PayerReferenceLabel 前端表单字段标签（例："请输入你的 Bybit UID"）
	PayerReferenceLabel string
	// Note 展示给乘客的一行提示（可为空）
	Note string
}

// ErrUnknownChannel 未注册的渠道 id
var ErrUnknownChannel = errors.New("topupchannel: 未知渠道")

// ErrDisabledChannel 已注册但未启用（暂关）
var ErrDisabledChannel = errors.New("topupchannel: 该渠道暂未开放")

// Registry 是渠道注册表 · Server 装配时构建一次 · 只读。
type Registry struct {
	byID map[ID]Channel
	all  []Channel // 稳定顺序（按注册顺序 · 前端展示按这个序）
}

// New 建注册表并注册当前四家渠道（一家启用 · 其余关但预留）。
//
// Enabled 可从 env 覆盖：
//   BP_TOPUP_WAFFO_ENABLED=1|0
//   BP_TOPUP_EPUSDT_ENABLED=1|0
//   BP_TOPUP_BYBIT_ENABLED=1|0
//   BP_TOPUP_BINANCE_ENABLED=1|0
//
// 默认（不设 env）：只主 hosted 渠道启用·其余关。
func New(overrides map[ID]bool) *Registry {
	r := &Registry{byID: make(map[ID]Channel)}
	// 注册顺序 = 前端展示顺序（先易用后小众）
	channels := []Channel{
		{
			ID: Waffo, DisplayName: "Waffo 支付",
			Region: RegionOverseas, Rail: RailHosted,
			ProviderKind:           "waffo_checkout",
			Asset:                  "USD",
			Enabled:                true,
			RequiresPayerReference: false,
			Note:                   "跳转 Waffo checkout · 支持卡 / 电子钱包",
		},
		{
			ID: Bybit, DisplayName: "Bybit UID 内转",
			Region: RegionOverseas, Rail: RailDirect,
			ProviderKind:           "bybit_internal",
			Asset:                  "USDT",
			Enabled:                false,
			RequiresPayerReference: true,
			PayerReferenceLabel:    "你的 Bybit UID（数字）",
			Note:                   "Bybit 内部转账 · 免手续费",
		},
		{
			ID: Binance, DisplayName: "Binance ID 内转",
			Region: RegionOverseas, Rail: RailDirect,
			ProviderKind:           "binance_internal",
			Asset:                  "USDT",
			Enabled:                false,
			RequiresPayerReference: true,
			PayerReferenceLabel:    "你的 Binance ID（数字）",
			Note:                   "Binance 内部转账 · 免手续费",
		},
		{
			ID: EPUSDT, DisplayName: "USDT 链上转账",
			Region: RegionOverseas, Rail: RailDirect,
			ProviderKind:           "epusdt_onchain",
			Asset:                  "USDT",
			Enabled:                false,
			RequiresPayerReference: false, // 链上地址可选·省了 dis-ambiguation
			PayerReferenceLabel:    "发送钱包地址（可选）",
			Note:                   "TRC-20 / BEP-20 链上转账",
		},
	}
	for _, c := range channels {
		if v, ok := overrides[c.ID]; ok {
			c.Enabled = v
		}
		r.byID[c.ID] = c
		r.all = append(r.all, c)
	}
	return r
}

// Get 查一个渠道（不管 enabled）。未注册返 ErrUnknownChannel。
func (r *Registry) Get(id string) (Channel, error) {
	c, ok := r.byID[ID(strings.ToLower(strings.TrimSpace(id)))]
	if !ok {
		return Channel{}, ErrUnknownChannel
	}
	return c, nil
}

// GetEnabled 查渠道且要求 enabled。未注册返 ErrUnknownChannel·关的返 ErrDisabledChannel。
// POST /api/me/topup 用这个 —— 关渠道乘客点不通。
func (r *Registry) GetEnabled(id string) (Channel, error) {
	c, err := r.Get(id)
	if err != nil {
		return Channel{}, err
	}
	if !c.Enabled {
		return Channel{}, ErrDisabledChannel
	}
	return c, nil
}

// List 列出所有已注册渠道（含 disabled·前端展示"即将开放"占位）。
// 顺序稳定 · 按 New 里的注册顺序。
func (r *Registry) List() []Channel {
	out := make([]Channel, len(r.all))
	copy(out, r.all)
	return out
}
