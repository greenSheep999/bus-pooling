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
	// 绑定 kiro.rs 版本防契约漂移（sprint Iss #13）· 空 = 不校验
	ExpectedSHA string `yaml:"expected_sha"`
}

// Secrets 全部来自 env。
type Secrets struct {
	// MasterKey AES-256-GCM 主密钥 · 32 字节的 hex（64 个字符）
	MasterKey string
	// HousepoolAdminKey 我方 kiro.rs 的 admin API key
	HousepoolAdminKey string
}

const (
	EnvMasterKey         = "BP_MASTER_KEY"
	EnvHousepoolAdminKey = "BP_HOUSEPOOL_ADMIN_KEY"
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
	if v := os.Getenv("BP_HOUSEPOOL_EXPECTED_SHA"); v != "" {
		cfg.Housepool.ExpectedSHA = v
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

	cfg.Secrets.MasterKey = os.Getenv(EnvMasterKey)
	cfg.Secrets.HousepoolAdminKey = os.Getenv(EnvHousepoolAdminKey)
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
	return nil
}

// RequireSecrets 校验运行期必需的密钥。
//
// 跟 Validate 分开：migrate 之类的子命令不需要主密钥，不该因为没配 env 就跑不了。
func (c Config) RequireSecrets() error {
	if c.Secrets.MasterKey == "" {
		return fmt.Errorf("缺少环境变量 %s（AES-256-GCM 主密钥 · 64 位 hex）", EnvMasterKey)
	}
	return nil
}
