package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// downstream Store 已经由 newEnvBase 装配到 ServerDeps.Downstreams，
// 本文件里的 attachDownstreams 已合并成空 helper 保留调用点最小改动。
func attachDownstreams(t *testing.T, _ *testEnv) {
	t.Helper()
}

// —— 用例 ——

// GET /api/me/downstream · 未配过时返"空但完整"的形状，且不含明文
func TestGetDownstreamEmpty(t *testing.T) {
	e := newEnv(t)
	attachDownstreams(t, e)
	pt := seedWithAPIKey(t, e, "ds1@example.com", "ds1", "password123")
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", pt) }

	status, body := e.do(t, "GET", "/api/me/downstream", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[map[string]any](t, body)
	if got["passengerpool_url"] != "" {
		t.Errorf("url = %v, want empty", got["passengerpool_url"])
	}
	if got["passengerpool_token_masked"] != "" {
		t.Errorf("mask 不为空: %v（未配过就该是空）", got["passengerpool_token_masked"])
	}
	// 铁律：响应体里不能出现明文
	assertNoPlaintextTokens(t, body)
}

// PUT passengerpool · token 传进去后 GET 只回 mask，不回明文
func TestPutPassengerpoolStoresTokenMasked(t *testing.T) {
	e := newEnv(t)
	attachDownstreams(t, e)
	pt := seedWithAPIKey(t, e, "ds2@example.com", "ds2", "password123")
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", pt) }

	status, body := e.do(t, "PUT", "/api/me/downstream/passengerpool", map[string]any{
		"passengerpool_url": "https://kiro.example.com",
		"token":             "sk-super-secret-abcd1234",
	}, withKey)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", status, body)
	}

	status, body = e.do(t, "GET", "/api/me/downstream", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("GET status = %d", status)
	}
	got := decode[map[string]any](t, body)
	if got["passengerpool_url"] != "https://kiro.example.com" {
		t.Errorf("url = %v", got["passengerpool_url"])
	}
	mask, _ := got["passengerpool_token_masked"].(string)
	if !strings.HasPrefix(mask, "kiro_admin_") {
		t.Errorf("mask 缺前缀: %q", mask)
	}
	assertNoPlaintextTokens(t, body)
}

// PUT · URL 校验拒内网 / 回环 —— 防 SSRF
func TestPutPassengerpoolRejectsBadURL(t *testing.T) {
	e := newEnv(t)
	attachDownstreams(t, e)
	pt := seedWithAPIKey(t, e, "ds3@example.com", "ds3", "password123")
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", pt) }

	for _, bad := range []string{
		"http://127.0.0.1/hook",
		"http://169.254.169.254/latest",
		"http://localhost:8080",
		"ftp://x.com",
	} {
		status, body := e.do(t, "PUT", "/api/me/downstream/passengerpool", map[string]any{
			"passengerpool_url": bad,
		}, withKey)
		if status != http.StatusBadRequest {
			t.Errorf("%q → status %d, want 400 (body=%s)", bad, status, body)
		}
	}
}

// POST /webhook/secret · 明文只这一次返回，后续 GET 只回 mask
func TestRotateWebhookSecretReturnsPlaintextOnce(t *testing.T) {
	e := newEnv(t)
	attachDownstreams(t, e)
	pt := seedWithAPIKey(t, e, "ds4@example.com", "ds4", "password123")
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", pt) }

	status, body := e.do(t, "POST", "/api/me/downstream/webhook/secret", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("rotate status = %d, body = %s", status, body)
	}
	rot := decode[map[string]string](t, body)
	secret := rot["secret"]
	if len(secret) != 64 {
		t.Fatalf("secret 长度 = %d, want 64（32 字节 hex）", len(secret))
	}

	// 后续 GET 不再吐明文
	status, body = e.do(t, "GET", "/api/me/downstream/webhook", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("get webhook status = %d", status)
	}
	if strings.Contains(string(body), secret) {
		t.Errorf("GET webhook 返回了明文 secret（禁止）: %s", body)
	}
	got := decode[map[string]any](t, body)
	if mask, _ := got["secret_masked"].(string); !strings.HasPrefix(mask, "whsec_") {
		t.Errorf("mask 缺前缀: %q", mask)
	}
}

// POST /passengerpool/test · 未配 URL 时提示"先配 URL"（不是 500 也不是假的 ok=true）
//
// **不测**"真的 probe httptest server"，因为 URL 校验会拦回环地址（SSRF 防护）；
// 用假域名 probe 会 DNS 失败拿到 latency=3000ms，测的是超时行为不是 handler 逻辑。
// probe 本身走 stdlib http.Client · 逻辑简单，交给 e2e / 手工验收。
func TestPassengerpoolTestRequiresURL(t *testing.T) {
	e := newEnv(t)
	attachDownstreams(t, e)
	pt := seedWithAPIKey(t, e, "ds5b@example.com", "ds5b", "password123")
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", pt) }

	status, body := e.do(t, "POST", "/api/me/downstream/passengerpool/test", nil, withKey)
	if status != http.StatusBadRequest {
		t.Fatalf("未配 URL 时应返 400, got %d body=%s", status, body)
	}
	if got := decode[Error](t, body); got.Code != CodeBadRequest {
		t.Errorf("错误码 = %q", got.Code)
	}
}

// GET /webhook/deliveries · 未有任何投递时返空数组（不是 null / 不是 {items:...}）
func TestListDeliveriesEmpty(t *testing.T) {
	e := newEnv(t)
	attachDownstreams(t, e)
	pt := seedWithAPIKey(t, e, "ds6@example.com", "ds6", "password123")
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", pt) }

	status, body := e.do(t, "GET", "/api/me/downstream/webhook/deliveries", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	// 契约（web/src/api/hooks.ts:386）：直接是数组
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		t.Fatalf("响应不是数组: %s", body)
	}
	if len(arr) != 0 {
		t.Errorf("新账号该返空数组, got %d 条", len(arr))
	}
}

// 铁律：响应体不含明文 token 关键字（sk-* / usr-*）
func assertNoPlaintextTokens(t *testing.T, body []byte) {
	t.Helper()
	for _, needle := range []string{
		"sk-plain", "sk-super-secret", "sk-x", // 我们测试里塞进去的明文
	} {
		if strings.Contains(string(body), needle) {
			t.Errorf("响应体含明文 %q（禁止）: %s", needle, body)
		}
	}
}
