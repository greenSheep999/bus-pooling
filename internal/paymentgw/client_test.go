package paymentgw

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret-please-change"

func newTestClient(t *testing.T, base string) *Client {
	t.Helper()
	c, err := New(Config{BaseURL: base, BearerToken: "test-bearer", SettlementSecret: testSecret})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestNewValidates 空必填拒建。
func TestNewValidates(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatalf("空配置应报错")
	}
	if _, err := New(Config{BaseURL: "x", BearerToken: "y"}); err == nil {
		t.Fatalf("缺 SettlementSecret 应报错")
	}
}

// TestCreatePaymentHappy · 201 + Bearer header 正确 + JSON body 无未知字段。
func TestCreatePaymentHappy(t *testing.T) {
	var gotAuth, gotCT string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{
			"id":"pay_abc",
			"client_order_id":"topup-1",
			"provider_kind":"waffo_checkout",
			"expected_amount":"10.50",
			"expected_asset":"USD",
			"state":"pending",
			"reconciliation_state":"unchecked",
			"created_at":1700000000,
			"instructions":{"checkout_url":"https://pay.example/co/abc","expires_at":"1700003600"}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	p, err := c.CreatePayment(context.Background(), CreatePaymentRequest{
		ClientOrderID: "topup-1", ProviderKind: "waffo_checkout",
		ExpectedAmount: "10.50", ExpectedAsset: "USD",
	})
	if err != nil {
		t.Fatalf("CreatePayment: %v", err)
	}
	if p.ID != "pay_abc" {
		t.Errorf("ID = %q, want pay_abc", p.ID)
	}
	if p.Instructions == nil || p.Instructions.CheckoutURL == "" {
		t.Fatalf("instructions.checkout_url 丢了: %#v", p.Instructions)
	}
	if gotAuth != "Bearer test-bearer" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if !strings.Contains(string(gotBody), `"expected_amount":"10.50"`) {
		t.Errorf("请求体 amount 未原样透传: %s", gotBody)
	}
}

// TestCreatePaymentError · gateway 4xx 转成 APIError。
func TestCreatePaymentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":"conflicting_order","detail":"client_order_id already exists"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.CreatePayment(context.Background(), CreatePaymentRequest{
		ClientOrderID: "dup", ProviderKind: "waffo_checkout",
		ExpectedAmount: "1", ExpectedAsset: "USD",
	})
	ae, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err 应是 *APIError, got %T: %v", err, err)
	}
	if ae.Status != 409 || ae.Code != "conflicting_order" {
		t.Errorf("APIError = %+v", ae)
	}
}

// signBody 用契约算法生成 hex 签名·测试辅助。
func signBody(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v1:"))
	mac.Write([]byte(timestamp))
	mac.Write([]byte(":"))
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// TestVerifySettlementHappy · 合法签名 + 合法窗口应通过。
func TestVerifySettlementHappy(t *testing.T) {
	c := newTestClient(t, "http://x")
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{"event_id":"e1","gateway_payment_id":"pay_1","state":"settled"}`)
	sig := signBody(testSecret, "1700000000", body)
	if err := c.VerifySettlement(sig, "1700000000", body, now); err != nil {
		t.Fatalf("VerifySettlement: %v", err)
	}
}

// TestVerifySettlementRejects · 各种坏情况。
func TestVerifySettlementRejects(t *testing.T) {
	c := newTestClient(t, "http://x")
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{"event_id":"e1"}`)
	goodSig := signBody(testSecret, "1700000000", body)

	cases := []struct {
		name string
		sig  string
		ts   string
		body []byte
		now  time.Time
		want error
	}{
		{"header 格式错 · 无前缀", "abcd", "1700000000", body, now, ErrBadHeader},
		{"header hex 长度不对", "v1=deadbeef", "1700000000", body, now, ErrBadHeader},
		{"timestamp 非数字", goodSig, "not-a-number", body, now, ErrBadTimestamp},
		{"时间戳早太多", goodSig, "1700000000", body, now.Add(10 * time.Minute), ErrStale},
		{"时间戳晚太多", goodSig, "1700000000", body, now.Add(-10 * time.Minute), ErrStale},
		{"body 被改", goodSig, "1700000000", []byte(`{"event_id":"e1"}x`), now, ErrBadSignature},
		{"secret 不对（签名不匹配）", signBody("wrong", "1700000000", body), "1700000000", body, now, ErrBadSignature},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.VerifySettlement(tc.sig, tc.ts, tc.body, tc.now)
			if err != tc.want {
				t.Errorf("want %v, got %v", tc.want, err)
			}
		})
	}
}

// TestVerifySettlementRawBodyExact · 契约明示不能 re-encode·任何字节变化必拒。
func TestVerifySettlementRawBodyExact(t *testing.T) {
	c := newTestClient(t, "http://x")
	now := time.Unix(1_700_000_000, 0)
	raw := []byte(`{"a": 1, "b": 2}`) // 带空格的合法 JSON
	sig := signBody(testSecret, "1700000000", raw)

	// 去掉空格后重签会 fail
	tighter := []byte(`{"a":1,"b":2}`)
	if err := c.VerifySettlement(sig, "1700000000", tighter, now); err != ErrBadSignature {
		t.Errorf("空格差异应致签名不匹配·got %v", err)
	}
	// 原始 body 应通过
	if err := c.VerifySettlement(sig, "1700000000", raw, now); err != nil {
		t.Errorf("原始 body 应通过·got %v", err)
	}
}
