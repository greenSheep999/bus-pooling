package downstream

import (
	"context"
	"strings"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/secrets"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()

	d := db.NewTestDB(t)

	// 32 字节的固定测试密钥（32 * "01" = 64 位 hex）
	key := ""
	for i := 0; i < 32; i++ {
		key += "01"
	}
	c, err := secrets.New(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	// 造一个测试 passenger（passenger_downstream 有 FK）
	pid := "p_test_" + t.Name()
	if _, err := d.DB.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, datetime('now'), datetime('now'))`,
		pid, "u_"+t.Name(), pid+"@x.com", "x"); err != nil {
		t.Fatalf("seed passenger: %v", err)
	}

	return NewStore(d.DB, c), pid
}

// Get 从未配过时返回 Defaults + ErrNotFound —— handler 依赖这行为返"未配置"响应。
func TestGetReturnsDefaultsWhenMissing(t *testing.T) {
	s, pid := newTestStore(t)
	cfg, err := s.Get(context.Background(), pid)
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if cfg.PassengerID != pid {
		t.Errorf("PassengerID = %q", cfg.PassengerID)
	}
	if !cfg.PushOnPull || !cfg.ResyncOnDead || !cfg.RetryOnFailure {
		t.Error("Defaults 应该 push_on_pull/resync/retry 全 true")
	}
	if cfg.BusOnly {
		t.Error("Defaults bus_only 应为 false")
	}
	if cfg.PassengerpoolTokenConfigured || cfg.WebhookSecretConfigured {
		t.Error("Defaults 不应该标记 configured")
	}
}

// SavePassengerpool 把明文加密后落库，Get 时能看到 configured 但拿不到明文。
func TestSavePassengerpoolEncryptsToken(t *testing.T) {
	s, pid := newTestStore(t)
	ctx := context.Background()

	if err := s.SavePassengerpool(ctx, pid, "https://my.example.com", "sk-plain-abcd1234"); err != nil {
		t.Fatalf("save: %v", err)
	}

	cfg, err := s.Get(ctx, pid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cfg.PassengerpoolURL != "https://my.example.com" {
		t.Errorf("URL = %q", cfg.PassengerpoolURL)
	}
	if !cfg.PassengerpoolTokenConfigured {
		t.Fatal("TokenConfigured 应为 true")
	}
	if len(cfg.PassengerpoolTokenEncrypted) == 0 {
		t.Fatal("加密 blob 为空")
	}
	// 明文绝不能出现在 blob 里
	if containsBytes(cfg.PassengerpoolTokenEncrypted, "sk-plain-abcd1234") {
		t.Error("加密 blob 里含明文 —— 加密没生效")
	}

	// 解密回来验证 round-trip
	plaintext, err := s.DecryptPassengerpoolToken(cfg.PassengerpoolTokenEncrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plaintext != "sk-plain-abcd1234" {
		t.Errorf("解密结果 = %q", plaintext)
	}
}

// SavePassengerpool token 传空时，只更新 URL，不清 token（防"改 URL 时误清 token"）。
func TestSavePassengerpoolEmptyTokenKeepsExisting(t *testing.T) {
	s, pid := newTestStore(t)
	ctx := context.Background()

	if err := s.SavePassengerpool(ctx, pid, "https://a.com", "first-token"); err != nil {
		t.Fatal(err)
	}
	// 再传空 token · 只改 URL
	if err := s.SavePassengerpool(ctx, pid, "https://b.com", ""); err != nil {
		t.Fatal(err)
	}

	cfg, err := s.Get(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PassengerpoolURL != "https://b.com" {
		t.Errorf("URL 应更新为 b.com, got %q", cfg.PassengerpoolURL)
	}
	if !cfg.PassengerpoolTokenConfigured {
		t.Fatal("token 应该保留 · 传空不该清")
	}
	plaintext, err := s.DecryptPassengerpoolToken(cfg.PassengerpoolTokenEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	if plaintext != "first-token" {
		t.Errorf("token 被清了, got %q", plaintext)
	}
}

// RotateWebhookSecret 生成新 secret · 每次不同 · 明文长度 = 70（whsec_ + 64 hex）。
func TestRotateWebhookSecret(t *testing.T) {
	s, pid := newTestStore(t)
	ctx := context.Background()

	first, err := s.RotateWebhookSecret(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	// 1e-2 起加统一 whsec_ 前缀 · 长度 = 6 + 64 = 70
	if len(first) != 70 {
		t.Errorf("secret 长度 = %d, want 70", len(first))
	}
	if !strings.HasPrefix(first, "whsec_") {
		t.Errorf("secret 缺 whsec_ 前缀: %q", first)
	}

	second, err := s.RotateWebhookSecret(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Error("两次轮换应该不同")
	}

	cfg, err := s.Get(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.WebhookSecretConfigured {
		t.Fatal("WebhookSecretConfigured 应为 true")
	}
	plain, err := s.DecryptWebhookSecret(cfg.WebhookSecretEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	if plain != second {
		t.Errorf("解密 = %q, want %q（最后一次）", plain, second)
	}
}

func TestValidateTargetURL(t *testing.T) {
	good := []string{
		"https://kiro.example.com",
		"http://kiro.example.com:8080/hook",
		"https://my-server.tld/path?x=1",
	}
	for _, u := range good {
		if err := ValidateTargetURL(u); err != nil {
			t.Errorf("%q 应通过, got %v", u, err)
		}
	}

	bad := map[string]string{
		"":                                "空",
		"not-a-url":                       "格式",
		"ftp://x.com":                     "scheme",
		"http://127.0.0.1":                "回环",
		"http://localhost":                "localhost",
		"http://10.0.0.1":                 "私网 10/8",
		"http://192.168.1.1":              "私网 192.168",
		"http://172.16.0.1":               "私网 172.16",
		"http://169.254.169.254/latest":   "AWS 元数据",
		"http://[::1]/hook":               "IPv6 回环",
		"http://[fe80::1]/hook":           "IPv6 link-local",
		"http://metadata.google.internal": "GCP 元数据",
		"http://foo.internal":             "内网后缀",
	}
	for u := range bad {
		if err := ValidateTargetURL(u); err == nil {
			t.Errorf("%q 应被拒", u)
		}
	}
}

func TestMaskToken(t *testing.T) {
	m := MaskToken("kiro_admin_ABCD1234")
	if m == "" {
		t.Fatal("mask 为空")
	}
	// 尾部 4 位可见 · 前缀固定
	if got := m[len(m)-4:]; got != "1234" {
		t.Errorf("尾部 = %q", got)
	}
	// 空明文 → 空 mask（handler 判空展示 "未配置"）
	if MaskToken("") != "" {
		t.Error("空明文应返空 mask")
	}
}

// containsBytes 帮 test 判断加密 blob 里没夹带明文。
func containsBytes(blob []byte, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	nb := []byte(needle)
	for i := 0; i+len(nb) <= len(blob); i++ {
		match := true
		for j := range nb {
			if blob[i+j] != nb[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
