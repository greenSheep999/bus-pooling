// Package config 读 yaml 配置 + env 覆盖。
//
// 约定：
//   - 敏感值（主密钥 / admin key / vendor 凭证）**只从 env 读**，不进 yaml
//     —— yaml 会进 git，env 不会
//   - 其余配置走 yaml，env 可覆盖（部署时不用改文件）
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    Server    `yaml:"server"`
	DB        DB        `yaml:"db"`
	HTTPX     HTTPX     `yaml:"httpx"`
	Housepool Housepool `yaml:"housepool"`
	Vendors   Vendors   `yaml:"vendors"`
	Decider   Decider   `yaml:"decider"`
	Bus       Bus       `yaml:"bus"`
	Pull      Pull      `yaml:"pull"`
	Promo     Promo     `yaml:"promo"`
	Community Community `yaml:"community"`

	// DryRun=true 时 vendor 调用走 mock，不产生真实扣款（sprint Iss #13）。
	// 默认 true —— 宁可开发时忘了关而不扣钱，也不要上线时忘了开而误扣。
	DryRun bool `yaml:"dry_run"`

	// Secrets 不从 yaml 读，见 loadSecrets。
	Secrets Secrets `yaml:"-"`
}

type Server struct {
	Addr            string        `yaml:"addr"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type DB struct {
	// SQLite 文件路径。WAL 单节点（CLAUDE.md §7.2）
	Path string `yaml:"path"`
	// 迁移文件目录为空则用编译进二进制的 embed FS
	MigrationsDir string `yaml:"migrations_dir"`
}

type HTTPX struct {
	Timeout       time.Duration `yaml:"timeout"`
	MaxRetries    int           `yaml:"max_retries"`
	RetryBaseWait time.Duration `yaml:"retry_base_wait"`
	// 出向代理 · 空 = 不走代理（CLAUDE.md §7.1 要求出向 HTTP 统一走 httpx）
	Proxy string `yaml:"proxy"`
	// 这些 host 不走代理（逗号分隔）
	NoProxy string `yaml:"no_proxy"`
}

type Housepool struct {
	BaseURL string `yaml:"base_url"`
	// ExpectedVersion 绑 housepool 语义版本（**不是 commit SHA**）·空 = 不校验。
	//
	// 启动时调 housepool 版本探测端点 · 比对返回的 current_version ·
	// 不等就启动失败 · 跟 rates 零费率同款守护 · 防契约漂移。
	//
	// 字段名从 ExpectedSHA 改了 · 老 yaml/env 的兼容读取见 applyEnv。
	ExpectedVersion string `yaml:"expected_version"`
}

// Vendors 各家 vendor 的**非敏感**配置。
//
// 空 BaseURL = 不装配那家 · registry 层跳过（不参与比价 · 不注册）。
// 敏感值（API key / webhook secret）不在这里 · 从 env 读。
type Vendors struct {
	Kiro91    Vendor `yaml:"kiro91"`
	KiroCEO   Vendor `yaml:"kiroceo"`
	KiroOOO   Vendor `yaml:"kirooo"`
	KiroAppIO Vendor `yaml:"kiroappio"`
	KiroAppCC Vendor `yaml:"kiroappcc"`
	KiroDrop  Vendor `yaml:"kirodrop"`
}

type Vendor struct {
	// Enabled=false 仍会注册（停用的 vendor 手上可能还有它拉的号，对账要查得到），
	// 只是不参与比价。默认 false —— 没配就不该悄悄开始花钱。
	Enabled bool   `yaml:"enabled"`
	BaseURL string `yaml:"base_url"`
}

// Decider · 拉号决策器的配置（比价算法·default vendor 之类）。
//
// 阶段 1a-1b：DefaultVendor 决定"客户端不传 vendor_id 时用哪家"·**必须显式配**。
// 未来（1d+）真比价算法上线后·这个字段变成"算法失败时的兜底"。
type Decider struct {
	// DefaultVendor · 客户端不传 vendor_id 时用的默认 · live 模式必填。
	DefaultVendor string `yaml:"default_vendor"`
}

// Bus · 拼车实体的系统级配置（跟成员 / 撮合 / 邀请码相关）。
//
// 具体值走 config.yaml · 别写死在代码里（CLAUDE.md §7.3 铁律：
// 具体阈值 / 上限不进代码 · 只进 config / 后台 / 文档）。
type Bus struct {
	// MaxMembers · 每辆车最多几人 · 阶段 1c 硬性上限
	// 应用范围：包括用户建的 team 车 + 系统建的 anon 搭车池
	// 忽略前端传入的 max_members 入参·全走 config
	MaxMembers int `yaml:"max_members"`
}

// Community · 社群渠道链接（TG / Discord / GitHub / X 等）。
//
// 运营改一次不用重发前端。前端从 `GET /api/community/channels` 拉。
// 空数组 = 前端展示"敬请期待"占位。
type Community struct {
	Channels []CommunityChannel `yaml:"channels"`
}

// CommunityChannel 一条社群入口。
type CommunityChannel struct {
	// ID 稳定标识 · 前端做 key + 决定用哪个 logo
	//   telegram_channel / telegram_group / discord / github / x
	ID string `yaml:"id"`
	// Name 展示名（"Telegram 频道" / "Discord 服务器"）· 兜底
	Name string `yaml:"name"`
	// NameI18n BCP-47 → 展示名 · 前端按 i18n.language 选一条
	NameI18n map[string]string `yaml:"name_i18n"`
	// URL 点击去哪 · 空 = 不下发（未上线的渠道别渲染死链）
	URL string `yaml:"url"`
	// Enabled false = 不下发
	Enabled bool `yaml:"enabled"`
}

// Promo · 顶部跑马灯活动位（系统配置 · 不是给用户改的）。
//
// 为什么走 config 而不是硬编在前端：文案 / 倒计时 / 开关都是运营要随时改的东西，
// 改一次不该重新 build 部署前端。前端从 `GET /api/promos` 拉。
//
// **文案铁律**（改之前先过 CLAUDE.md §12.6 + 00 §7.5 规则 B）：
//   - 不出通道商 / vendor 真名 · 不出内部阶段标签（1a/1b）· 不出计费分项结构
//   - **不承诺任何时长 / 质量指标**（规则 B：我方不承诺号的可用时长）
type Promo struct {
	Items []PromoItem `yaml:"items"`
}

// PromoItem 一条活动。
type PromoItem struct {
	// ID 稳定标识 · 前端用它做 key（顺序可能变）
	ID string `yaml:"id"`
	// Text 兜底文案 · 前端语言在 TextI18n 里找不到时用这个
	Text string `yaml:"text"`
	// TextI18n 按 BCP-47 语言标签给多语文案 · 前端按 i18n.language 选
	// key 举例：`en` / `zh-CN` · 找不到就 fallback 到 Text
	TextI18n map[string]string `yaml:"text_i18n"`
	// To 点击去哪 · 空 = 不可点（纯公告）
	To string `yaml:"to"`
	// Enabled false = 不下发（下线一条活动不用删配置）
	Enabled bool `yaml:"enabled"`
	// CountdownUntil 倒计时截止（RFC3339 · 空 = 不显示倒计时）
	// 到点后这条自动不下发 —— 免得运营忘了关，过期活动还挂着
	CountdownUntil string `yaml:"countdown_until"`
}

// Pull · 拉号参数的全局配置（decisions §8.35 #18）。
//
// 目标：**避免并发打爆上游 + 尽量拉到号**。每辆车 / 每个乘客的策略在自己的
// `bus.per_round_count` 等字段里，这里是系统级的**兜底和区间** —— 谁也不能超。
type Pull struct {
	// DefaultCount · 客户端没指定数量时拉几个
	DefaultCount int `yaml:"default_count"`
	// MinCount / MaxCount · 单次拉号数量的允许区间 · 超出直接拒（不静默截断 ——
	// 静默改数量用户会以为系统坏了）
	MinCount int `yaml:"min_count"`
	MaxCount int `yaml:"max_count"`
	// MaxConcurrentPerVendor · 同一 vendor 同时最多几个在飞的拉号请求
	// 0 = 不限（阶段 1c 单机够用 · 上量后必须设）
	MaxConcurrentPerVendor int `yaml:"max_concurrent_per_vendor"`
	// MaxConcurrentPerPassenger · 同一乘客同时最多几个在飞的拉号
	// 防一个人刷爆额度 · 也防他自己的并发互相挤冻结
	MaxConcurrentPerPassenger int `yaml:"max_concurrent_per_passenger"`
}

// Secrets 全部来自 env。
type Secrets struct {
	// MasterKey AES-256-GCM 主密钥 · 32 字节的 hex（64 个字符）
	MasterKey string
	// HousepoolAdminKey · housepool admin API key
	HousepoolAdminKey string

	// vendor API keys · 主动拉号用
	Kiro91APIKey    string
	KiroCEOAPIKey   string
	KiroOOOAPIKey   string
	KiroAppIOAPIKey string
	KiroAppCCAPIKey string
	KiroDropAPIKey  string

	// vendor webhook secrets · 收 vendor 回调时验签用
	// 只有支持 HMAC 的 vendor 才需要配（其他家 vendor 侧无签名·靠 URL 保密）
	Kiro91WebhookSecret   string
	KiroDropWebhookSecret string
}

const (
	EnvMasterKey             = "BP_MASTER_KEY"
	EnvHousepoolAdminKey     = "BP_HOUSEPOOL_ADMIN_KEY"
	EnvKiro91APIKey          = "BP_VENDOR_KIRO91_API_KEY"
	EnvKiro91WebhookSecret   = "BP_VENDOR_KIRO91_WEBHOOK_SECRET"
	EnvKiroCEOAPIKey         = "BP_VENDOR_KIROCEO_API_KEY"
	EnvKiroOOOAPIKey         = "BP_VENDOR_KIROOOO_API_KEY"
	EnvKiroAppIOAPIKey       = "BP_VENDOR_KIROAPPIO_API_KEY"
	EnvKiroAppCCAPIKey       = "BP_VENDOR_KIROAPPCC_API_KEY"
	EnvKiroDropAPIKey        = "BP_VENDOR_KIRODROP_API_KEY"
	EnvKiroDropWebhookSecret = "BP_VENDOR_KIRODROP_WEBHOOK_SECRET"
)

// Default 是不读任何文件时的配置 —— 本地起服务应该零配置能跑。
func Default() Config {
	return Config{
		Server: Server{
			Addr:            ":8080",
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 20 * time.Second,
		},
		DB: DB{Path: "data/bus-pooling.db"},
		HTTPX: HTTPX{
			Timeout:       20 * time.Second,
			MaxRetries:    3,
			RetryBaseWait: 500 * time.Millisecond,
		},
		Vendors: Vendors{
			// 默认 Enabled=false · BaseURL 空 —— 上游 URL 从 config.yaml 或 env 传入
			// 不硬编到代码（CLAUDE.md §0.1）· vendor 档案 URL 参考 docs/vendors/*.md
		},
		Bus: Bus{
			// 默认 5 · 阶段 1c 每辆车硬性上限（config.yaml 可覆盖）
			MaxMembers: 5,
		},
		Pull: Pull{
			// 默认拉 3 个 · 区间 1-50 · 并发暂不限（单机 1c 够用）
			DefaultCount:              3,
			MinCount:                  1,
			MaxCount:                  50,
			MaxConcurrentPerVendor:    0,
			MaxConcurrentPerPassenger: 3,
		},
		// Promo 默认空 —— 文案是运营内容·不硬编在代码里（config.yaml 配）
		Promo: Promo{},
		// Community 默认空 —— 社群渠道链接也是运营内容
		Community: Community{},
		DryRun: true,
	}
}

// Load 读 yaml（path 为空或文件不存在时用默认值），再套 env 覆盖，最后校验。
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		raw, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := yaml.Unmarshal(raw, &cfg); err != nil {
				return Config{}, fmt.Errorf("解析配置 %s: %w", path, err)
			}
		case errors.Is(err, os.ErrNotExist):
			// 显式指定了路径但文件不存在 —— 这通常是部署时写错路径，不该静默用默认值
			return Config{}, fmt.Errorf("配置文件不存在: %s", path)
		default:
			return Config{}, fmt.Errorf("读配置 %s: %w", path, err)
		}
	}

	applyEnv(&cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("BP_ADDR"); v != "" {
		cfg.Server.Addr = v
	}
	if v := os.Getenv("BP_DB_PATH"); v != "" {
		cfg.DB.Path = v
	}
	if v := os.Getenv("BP_HOUSEPOOL_URL"); v != "" {
		cfg.Housepool.BaseURL = v
	}
	// BP_HOUSEPOOL_EXPECTED_VERSION 是新名·BP_HOUSEPOOL_EXPECTED_SHA 是兼容旧名（misnomer）
	// 新名优先·两个都设新名生效。
	if v := os.Getenv("BP_HOUSEPOOL_EXPECTED_VERSION"); v != "" {
		cfg.Housepool.ExpectedVersion = v
	} else if v := os.Getenv("BP_HOUSEPOOL_EXPECTED_SHA"); v != "" {
		cfg.Housepool.ExpectedVersion = v
	}
	if v := os.Getenv("BP_PROXY"); v != "" {
		cfg.HTTPX.Proxy = v
	}
	if v := os.Getenv("BP_NO_PROXY"); v != "" {
		cfg.HTTPX.NoProxy = v
	}
	// DRY_RUN 只认显式的 "0"/"false" 才关 —— 见 Config.DryRun 的注释
	if v := os.Getenv("DRY_RUN"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.DryRun = b
		}
	}

	// 6 家 vendor · 各自 URL / ENABLED 都从 env 走
	applyVendorEnv(&cfg.Vendors.Kiro91, "BP_VENDOR_KIRO91_URL", "BP_VENDOR_KIRO91_ENABLED")
	applyVendorEnv(&cfg.Vendors.KiroCEO, "BP_VENDOR_KIROCEO_URL", "BP_VENDOR_KIROCEO_ENABLED")
	applyVendorEnv(&cfg.Vendors.KiroOOO, "BP_VENDOR_KIROOOO_URL", "BP_VENDOR_KIROOOO_ENABLED")
	applyVendorEnv(&cfg.Vendors.KiroAppIO, "BP_VENDOR_KIROAPPIO_URL", "BP_VENDOR_KIROAPPIO_ENABLED")
	applyVendorEnv(&cfg.Vendors.KiroAppCC, "BP_VENDOR_KIROAPPCC_URL", "BP_VENDOR_KIROAPPCC_ENABLED")
	applyVendorEnv(&cfg.Vendors.KiroDrop, "BP_VENDOR_KIRODROP_URL", "BP_VENDOR_KIRODROP_ENABLED")

	// decider · default vendor
	if v := os.Getenv("BP_DECIDER_DEFAULT_VENDOR"); v != "" {
		cfg.Decider.DefaultVendor = v
	}

	cfg.Secrets.MasterKey = os.Getenv(EnvMasterKey)
	cfg.Secrets.HousepoolAdminKey = os.Getenv(EnvHousepoolAdminKey)
	cfg.Secrets.Kiro91APIKey = os.Getenv(EnvKiro91APIKey)
	cfg.Secrets.Kiro91WebhookSecret = os.Getenv(EnvKiro91WebhookSecret)
	cfg.Secrets.KiroCEOAPIKey = os.Getenv(EnvKiroCEOAPIKey)
	cfg.Secrets.KiroOOOAPIKey = os.Getenv(EnvKiroOOOAPIKey)
	cfg.Secrets.KiroAppIOAPIKey = os.Getenv(EnvKiroAppIOAPIKey)
	cfg.Secrets.KiroAppCCAPIKey = os.Getenv(EnvKiroAppCCAPIKey)
	cfg.Secrets.KiroDropAPIKey = os.Getenv(EnvKiroDropAPIKey)
	cfg.Secrets.KiroDropWebhookSecret = os.Getenv(EnvKiroDropWebhookSecret)
}

// applyVendorEnv 通用 · 从 env 覆盖 vendor 的 URL 和 Enabled
func applyVendorEnv(v *Vendor, urlEnv, enabledEnv string) {
	if s := os.Getenv(urlEnv); s != "" {
		v.BaseURL = s
	}
	if s := os.Getenv(enabledEnv); s != "" {
		if b, err := strconv.ParseBool(s); err == nil {
			v.Enabled = b
		}
	}
}

func (c Config) Validate() error {
	if c.Server.Addr == "" {
		return errors.New("server.addr 不能为空")
	}
	if c.DB.Path == "" {
		return errors.New("db.path 不能为空")
	}
	if c.HTTPX.Timeout <= 0 {
		return errors.New("httpx.timeout 必须 > 0")
	}
	if c.HTTPX.MaxRetries < 0 {
		return errors.New("httpx.max_retries 不能为负")
	}
	// 开了一家 vendor 却没给 base_url —— 拉号时才炸不如现在就说
	if c.Vendors.Kiro91.Enabled && c.Vendors.Kiro91.BaseURL == "" {
		return errors.New("vendors.kiro91.enabled=true 但 base_url 为空")
	}
	// bus.max_members 零 = 建的车没人能加·连 owner 都进不去 · 拒启动
	if c.Bus.MaxMembers <= 0 {
		return errors.New("bus.max_members 必须 > 0")
	}
	// 拉号区间自相矛盾 → 拒启动（min > max 会让所有拉号都失败·启动时就该发现）
	if c.Pull.MinCount <= 0 {
		return errors.New("pull.min_count 必须 > 0")
	}
	if c.Pull.MaxCount < c.Pull.MinCount {
		return fmt.Errorf("pull.max_count(%d) 不能小于 pull.min_count(%d)",
			c.Pull.MaxCount, c.Pull.MinCount)
	}
	if c.Pull.DefaultCount < c.Pull.MinCount || c.Pull.DefaultCount > c.Pull.MaxCount {
		return fmt.Errorf("pull.default_count(%d) 必须落在 [%d, %d] 区间内",
			c.Pull.DefaultCount, c.Pull.MinCount, c.Pull.MaxCount)
	}
	return nil
}

// RequireSecrets 校验运行期必需的密钥。
//
// 跟 Validate 分开：migrate 之类的子命令不需要主密钥，不该因为没配 env 就跑不了。
func (c Config) RequireSecrets() error {
	if c.Secrets.MasterKey == "" {
		return fmt.Errorf("缺少环境变量 %s（AES-256-GCM 主密钥 · 64 位 hex）", EnvMasterKey)
	}
	// 开了 vendor 却没给令牌 —— 起服务时就说，别等到第一次拉号才 401。
	// DryRun 下不要求：那时根本不调真 vendor。
	if c.Vendors.Kiro91.Enabled && !c.DryRun && c.Secrets.Kiro91APIKey == "" {
		return fmt.Errorf("vendors.kiro91.enabled=true 但缺少环境变量 %s", EnvKiro91APIKey)
	}
	return nil
}
