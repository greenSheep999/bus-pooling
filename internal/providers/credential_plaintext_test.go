package providers

import "testing"

// P0-4 · NewFromPlaintext · admin_market 手工塞号收敛入口(I-35 输入侧)。

func TestNewFromPlaintext_RefreshToken(t *testing.T) {
	c := NewFromPlaintext("sso:rt", "", "")
	if c.AuthMethod != AuthRefreshToken || c.RefreshToken != "sso:rt" {
		t.Errorf("三选一 refresh_token 分派错: %+v", c)
	}
	if c.KiroAPIKey != "" || c.AccessToken != "" {
		t.Errorf("refresh_token 号不该带其他字段: %+v", c)
	}
}

func TestNewFromPlaintext_APIKey(t *testing.T) {
	c := NewFromPlaintext("", "", "ksk_x")
	if c.AuthMethod != AuthAPIKey || c.KiroAPIKey != "ksk_x" {
		t.Errorf("api_key 分派错: %+v", c)
	}
}

func TestNewFromPlaintext_Bearer(t *testing.T) {
	c := NewFromPlaintext("", "bearer_at", "")
	if c.AuthMethod != AuthBearer || c.AccessToken != "bearer_at" {
		t.Errorf("bearer 分派错: %+v", c)
	}
}

// TestNewFromPlaintext_PriorityRefresh · refresh_token 优先(即使 3 个都给)
func TestNewFromPlaintext_PriorityRefresh(t *testing.T) {
	c := NewFromPlaintext("rt", "at", "ksk_x")
	if c.AuthMethod != AuthRefreshToken {
		t.Errorf("三个都给时应该按 refresh 优先 · 实际 %q", c.AuthMethod)
	}
}

// TestNewFromPlaintext_Empty · 三个都空时 AuthMethod 空(调用方应先校验)
func TestNewFromPlaintext_Empty(t *testing.T) {
	c := NewFromPlaintext("", "", "")
	if c.AuthMethod != "" {
		t.Errorf("三个都空 · AuthMethod 应为空 · 实际 %q", c.AuthMethod)
	}
}
