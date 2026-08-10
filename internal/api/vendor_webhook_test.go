package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestVendorWebhook_UnknownSlug404 · 白名单外的 slug 返 404
func TestVendorWebhook_UnknownSlug404(t *testing.T) {
	env := newEnv(t)
	status, _ := env.do(t, "POST", "/api/webhooks/vendor/random", map[string]any{"x": 1})
	if status != http.StatusNotFound {
		t.Errorf("unknown slug 应 404·得到 %d", status)
	}
}

// TestVendorWebhook_NoHMACVendorAccepts · 无 HMAC 的 4 家不看签名·直接 200
func TestVendorWebhook_NoHMACVendorAccepts(t *testing.T) {
	env := newEnv(t)
	// kiroappcc 曾经无签名 · 2026-08-11 起 vendor 后台加了 X-Kiro-Signature HMAC 校验 ·
	// 从这组移到 HMAC 家的测试用例（见 vendor_webhook.go hmacSpecs）
	for _, slug := range []string{"kiroceo", "kirooo", "kiroappio"} {
		status, _ := env.do(t, "POST", "/api/webhooks/vendor/"+slug,
			map[string]any{"event": "credential.dead", "credential_id": "abc"})
		if status != http.StatusOK {
			t.Errorf("%s (无 HMAC 家) 应 200·得到 %d", slug, status)
		}
	}
}

// TestVendorWebhook_HMACRequiresSecret · vendor slug (HMAC 家) 未配 secret 返 401
func TestVendorWebhook_HMACRequiresSecret(t *testing.T) {
	env := newEnv(t)
	// 不 setenv BP_VENDOR_KIRO91_WEBHOOK_SECRET
	t.Setenv("BP_VENDOR_KIRO91_WEBHOOK_SECRET", "")
	status, _ := env.do(t, "POST", "/api/webhooks/vendor/91kiro",
		map[string]any{"x": 1})
	if status != http.StatusUnauthorized {
		t.Errorf("HMAC vendor 未配密钥应 401·得到 %d", status)
	}
}

// TestVendorWebhook_HMACBadSignature · 签名不对 401
func TestVendorWebhook_HMACBadSignature(t *testing.T) {
	env := newEnv(t)
	t.Setenv("BP_VENDOR_KIRO91_WEBHOOK_SECRET", "test-secret")
	status, _ := env.do(t, "POST", "/api/webhooks/vendor/91kiro",
		map[string]any{"x": 1},
		func(r *http.Request) {
			r.Header.Set("X-KM-Signature", "sha256=deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
		})
	if status != http.StatusUnauthorized {
		t.Errorf("坏签名应 401·得到 %d", status)
	}
}

// TestVendorWebhook_HMACGoodSignature · 签名对 200
func TestVendorWebhook_HMACGoodSignature(t *testing.T) {
	env := newEnv(t)
	secret := "test-secret"
	t.Setenv("BP_VENDOR_KIRO91_WEBHOOK_SECRET", secret)

	// 手工造签 · 跟 本 vendor 契约一致：sha256(ts + "." + body)
	ts := fmt.Sprintf("%d", time.Now().Unix())
	body := []byte(`{"event":"credential.dead","credential_id":"abc"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// 得用原始 bytes body 保证跟签名一致 · 不能走 env.do 的 json.Marshal
	req, err := http.NewRequest("POST", env.srv.URL+"/api/webhooks/vendor/91kiro",
		strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-KM-Signature", sig)
	req.Header.Set("X-KM-Timestamp", ts)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody := readAllForTest(t, resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("好签名应 200·得到 %d body=%s", resp.StatusCode, respBody)
	}
	if !strings.Contains(string(respBody), `"processed":false`) {
		t.Errorf("响应应说 processed=false·实际 %s", respBody)
	}
}

func readAllForTest(t *testing.T, r interface{ Read(p []byte) (int, error) }) []byte {
	t.Helper()
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 512)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf
}
