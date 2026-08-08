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
  expected_sha: "abc123"
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
	if cfg.Housepool.ExpectedSHA != "abc123" {
		t.Errorf("expected_sha = %q", cfg.Housepool.ExpectedSHA)
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
