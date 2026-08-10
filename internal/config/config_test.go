package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadWithoutFileUsesDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("addr = %q", cfg.Server.Addr)
	}
	if cfg.DB.Path != "data/bus-pooling.db" {
		t.Errorf("db.path = %q", cfg.DB.Path)
	}
	// 默认必须是 true —— 宁可开发时忘了关而不扣钱
	if !cfg.DryRun {
		t.Error("dry_run 默认应为 true")
	}
}

// 显式指定了路径但文件不存在 = 部署时写错路径，必须报错而不是静默用默认值
func TestLoadMissingFileIsError(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("指定的配置文件不存在时应该报错")
	}
}

func TestLoadFromYAML(t *testing.T) {
	// CI 里全局 DRY_RUN=1 · 会覆盖 yaml 的 dry_run: false · 这里明确清掉验证 yaml 生效
	t.Setenv("DRY_RUN", "")

	path := filepath.Join(t.TempDir(), "c.yaml")
	yaml := `
server:
  addr: ":9999"
  read_timeout: 5s
db:
  path: "/tmp/x.db"
httpx:
  timeout: 3s
  max_retries: 7
housepool:
  base_url: "https://pool.example.com"
  expected_version: "0.42.1"
dry_run: false
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":9999" {
		t.Errorf("addr = %q", cfg.Server.Addr)
	}
	if cfg.Server.ReadTimeout != 5*time.Second {
		t.Errorf("read_timeout = %v", cfg.Server.ReadTimeout)
	}
	if cfg.HTTPX.MaxRetries != 7 {
		t.Errorf("max_retries = %d", cfg.HTTPX.MaxRetries)
	}
	if cfg.Housepool.ExpectedVersion != "0.42.1" {
		t.Errorf("expected_version = %q", cfg.Housepool.ExpectedVersion)
	}
	if cfg.DryRun {
		t.Error("yaml 里 dry_run: false 没生效")
	}
	// yaml 没写的字段要保留默认值
	if cfg.Server.WriteTimeout != 30*time.Second {
		t.Errorf("未指定的 write_timeout 应保留默认，得到 %v", cfg.Server.WriteTimeout)
	}
}

func TestEnvOverridesYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("server:\n  addr: \":1111\"\ndry_run: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("BP_ADDR", ":2222")
	t.Setenv("BP_DB_PATH", "/env/path.db")
	t.Setenv("BP_HOUSEPOOL_URL", "https://env.example.com")
	t.Setenv("DRY_RUN", "0")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":2222" {
		t.Errorf("env 没覆盖 addr: %q", cfg.Server.Addr)
	}
	if cfg.DB.Path != "/env/path.db" {
		t.Errorf("env 没覆盖 db.path: %q", cfg.DB.Path)
	}
	if cfg.Housepool.BaseURL != "https://env.example.com" {
		t.Errorf("env 没覆盖 housepool.base_url: %q", cfg.Housepool.BaseURL)
	}
	if cfg.DryRun {
		t.Error("DRY_RUN=0 没生效")
	}
}

// DRY_RUN 是防误扣的开关 —— 只有明确的假值才关，垃圾值不能意外把它关掉
func TestDryRunOnlyDisabledByExplicitFalse(t *testing.T) {
	cases := []struct {
		env  string
		want bool
	}{
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"1", true},
		{"true", true},
		{"garbage", true}, // 解析不了 → 保持默认 true（安全侧）
		{"", true},        // 没设 → 默认 true
	}
	for _, tc := range cases {
		t.Run("DRY_RUN="+tc.env, func(t *testing.T) {
			if tc.env == "" {
				os.Unsetenv("DRY_RUN")
			} else {
				t.Setenv("DRY_RUN", tc.env)
			}
			cfg, err := Load("")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.DryRun != tc.want {
				t.Errorf("DRY_RUN=%q → dry_run=%v, want %v", tc.env, cfg.DryRun, tc.want)
			}
		})
	}
}

func TestSecretsComeFromEnvOnly(t *testing.T) {
	// yaml 里写 secrets 不该生效（字段标了 yaml:"-"）
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("secrets:\n  masterkey: \"from-yaml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvMasterKey, "from-env")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Secrets.MasterKey != "from-env" {
		t.Fatalf("主密钥 = %q，应该只认 env", cfg.Secrets.MasterKey)
	}
}

func TestRequireSecrets(t *testing.T) {
	cfg := Default()
	if err := cfg.RequireSecrets(); err == nil {
		t.Fatal("没配主密钥时应报错")
	}
	cfg.Secrets.MasterKey = "abc"
	if err := cfg.RequireSecrets(); err != nil {
		t.Fatalf("配了主密钥不该报错: %v", err)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"空 addr", func(c *Config) { c.Server.Addr = "" }},
		{"空 db.path", func(c *Config) { c.DB.Path = "" }},
		{"timeout 为 0", func(c *Config) { c.HTTPX.Timeout = 0 }},
		{"负重试", func(c *Config) { c.HTTPX.MaxRetries = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("应该校验失败")
			}
		})
	}

	t.Run("默认值合法", func(t *testing.T) {
		if err := Default().Validate(); err != nil {
			t.Fatalf("默认配置应合法: %v", err)
		}
	})

	// 开了 vendor 却没 base_url —— 拉号时才炸不如现在就说
	t.Run("enabled 但空 base_url", func(t *testing.T) {
		cfg := Default()
		cfg.Vendors.Kiro91.Enabled = true
		cfg.Vendors.Kiro91.BaseURL = ""
		if err := cfg.Validate(); err == nil {
			t.Fatal("应该校验失败")
		}
	})

	// 没开的 vendor 空着不该拦 —— 阶段 1a 只接一家，其余家配置为空是常态
	t.Run("disabled 时空 base_url 不拦", func(t *testing.T) {
		cfg := Default()
		cfg.Vendors.Kiro91.Enabled = false
		cfg.Vendors.Kiro91.BaseURL = ""
		if err := cfg.Validate(); err != nil {
			t.Fatalf("没开的 vendor 不该拦: %v", err)
		}
	})
}

// vendor 默认必须是关的 —— 没显式开就不该悄悄开始花钱
// **不再假设 base_url 有默认值** —— 上游 URL 从 config.yaml / env 传入·不硬编到代码
func TestVendorDefaultsDisabled(t *testing.T) {
	cfg := Default()
	if cfg.Vendors.Kiro91.Enabled {
		t.Error("vendor 默认应为 disabled")
	}
	if cfg.Vendors.Kiro91.BaseURL != "" {
		t.Errorf("默认 base_url 应为空·实际 %q · 上游 URL 不能硬编", cfg.Vendors.Kiro91.BaseURL)
	}
}

// vendor 令牌只从 env 读，绝不进 yaml（yaml 会进 git）
func TestVendorSecretsFromEnvOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	// 故意在 yaml 里塞 api_key 字段 —— 应该被忽略（struct 里没这个字段）
	// base_url 必填（默认不硬编）
	body := "vendors:\n  kiro91:\n    enabled: true\n    base_url: https://example.local\n    api_key: should-be-ignored\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvMasterKey, "mk")
	t.Setenv(EnvKiro91APIKey, "usr-from-env")
	t.Setenv(EnvKiro91WebhookSecret, "whsec-from-env")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Secrets.Kiro91APIKey != "usr-from-env" {
		t.Errorf("APIKey = %q，应从 env 读", cfg.Secrets.Kiro91APIKey)
	}
	if cfg.Secrets.Kiro91WebhookSecret != "whsec-from-env" {
		t.Errorf("WebhookSecret = %q，应从 env 读", cfg.Secrets.Kiro91WebhookSecret)
	}
}

// vendor 令牌不由 RequireSecrets 校验（生产从 vendor_account 表读 · env 里不必放）·
// 交给 buildVendorRegistry 的 resolveCred 在装配时判定。
// 曾经在 RequireSecrets 里硬校验 env 会导致"env 空 → 服务起不来" · 阻塞 seed CLI 部署路径。
func TestRequireSecretsOnlyChecksMasterKey(t *testing.T) {
	base := func() Config {
		c := Default()
		c.Secrets.MasterKey = "mk"
		c.Vendors.Kiro91.Enabled = true
		c.Vendors.Kiro91.BaseURL = "https://example.local"
		return c
	}

	t.Run("有 master key 就放行 · 即使 vendor 令牌空", func(t *testing.T) {
		c := base()
		c.DryRun = false
		if err := c.RequireSecrets(); err != nil {
			t.Fatalf("只该拦 master key 空的场景: %v", err)
		}
	})

	t.Run("master key 空必须拦", func(t *testing.T) {
		c := base()
		c.Secrets.MasterKey = ""
		if err := c.RequireSecrets(); err == nil {
			t.Fatal("master key 空必须拦")
		}
	})
}

func TestLoadRejectsBadYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("server:\n  addr: [不是字符串\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("非法 yaml 应该报错")
	}
}

func TestErrorsAreWrapped(t *testing.T) {
	// RequireSecrets 的错误要能被 errors.Is/As 之外的调用方读懂（含 env 名）
	cfg := Default()
	err := cfg.RequireSecrets()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, err) { // 平凡但确认不是 nil 包装
		t.Fatal("unexpected")
	}
	if got := err.Error(); got == "" || !contains(got, EnvMasterKey) {
		t.Fatalf("错误信息应提到环境变量名 %s，得到 %q", EnvMasterKey, got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
