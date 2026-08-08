package api

import (
	"context"
	"net/http"
	"testing"
)

func TestCreateTopupHappyPath(t *testing.T) {
	e := newWalletEnv(t)
	plaintext := seedWithAPIKey(t, e.testEnv, "t1@example.com", "t1", "password123")
	withKey := func(r *http.Request) {
		r.Header.Set("X-API-Key", plaintext)
		r.Header.Set("X-Idempotency-Key", "abcdef0123456789abcdef0123456789")
	}

	status, body := e.do(t, "POST", "/api/me/topup",
		map[string]any{"credits": 100_000_000, "channel": "waffo"}, withKey)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[map[string]any](t, body)

	// 契约字段（对齐 TS TopupOrder）都在
	for _, k := range []string{"order_id", "qr_payload", "paid", "credits", "expires_at", "status", "created_at"} {
		if _, ok := got[k]; !ok {
			t.Errorf("响应缺 %q，got %v", k, got)
		}
	}
	// paid = credits × 1.05
	if got["paid"].(float64) != 105_000_000 {
		t.Errorf("paid = %v", got["paid"])
	}
	if got["credits"].(float64) != 100_000_000 {
		t.Errorf("credits = %v", got["credits"])
	}
	if got["status"] != "pending" {
		t.Errorf("status = %v，应 pending", got["status"])
	}
	// **内部字段不该出**
	for _, forbidden := range []string{"wallet_ledger_id", "channel_fee", "pay_url", "channel"} {
		if _, ok := got[forbidden]; ok {
			t.Errorf("响应不该带 %q（内部字段）", forbidden)
		}
	}
	// 起单**不**入账（等 webhook 到才入）
	pid := passengerIDOf(t, e.testEnv, "t1@example.com")
	bal, _ := e.wallets.Get(context.Background(), pid)
	if bal.Balance != 0 {
		t.Errorf("起单后余额 = %d，应 0（未到账）", bal.Balance)
	}
}

func TestCreateTopupRequiresIdempotencyKey(t *testing.T) {
	e := newWalletEnv(t)
	plaintext := seedWithAPIKey(t, e.testEnv, "t2@example.com", "t2", "password123")
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) }

	status, body := e.do(t, "POST", "/api/me/topup",
		map[string]any{"credits": 100_000_000, "channel": "waffo"}, withKey)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[Error](t, body)
	if got.Code != CodeBadIdempotencyKey {
		t.Errorf("code = %q", got.Code)
	}
}

func TestCreateTopupRejectsInvalid(t *testing.T) {
	e := newWalletEnv(t)
	plaintext := seedWithAPIKey(t, e.testEnv, "t3@example.com", "t3", "password123")
	mk := func(idem string) func(*http.Request) {
		return func(r *http.Request) {
			r.Header.Set("X-API-Key", plaintext)
			r.Header.Set("X-Idempotency-Key", idem)
		}
	}

	cases := []struct {
		name string
		body map[string]any
		key  string
	}{
		{"负数 credits", map[string]any{"credits": -1, "channel": "waffo"}, "11111111111111111111111111111111"},
		{"未知 channel", map[string]any{"credits": 100, "channel": "alipay"}, "22222222222222222222222222222222"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := e.do(t, "POST", "/api/me/topup", tc.body, mk(tc.key))
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", status, body)
			}
		})
	}
}

func TestGetTopupOrder(t *testing.T) {
	e := newWalletEnv(t)
	plaintext := seedWithAPIKey(t, e.testEnv, "t4@example.com", "t4", "password123")
	withKey := func(r *http.Request) {
		r.Header.Set("X-API-Key", plaintext)
		r.Header.Set("X-Idempotency-Key", "33333333333333333333333333333333")
	}
	_, body := e.do(t, "POST", "/api/me/topup",
		map[string]any{"credits": 100_000_000, "channel": "waffo"}, withKey)
	orderID := decode[map[string]any](t, body)["order_id"].(string)

	status, body2 := e.do(t, "GET", "/api/me/topup/"+orderID, nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) })
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body2)
	}
	got := decode[map[string]any](t, body2)
	if got["order_id"] != orderID {
		t.Errorf("order_id = %v", got["order_id"])
	}
}

func TestGetTopupOrderTenantIsolation(t *testing.T) {
	e := newWalletEnv(t)
	keyA := seedWithAPIKey(t, e.testEnv, "ta@example.com", "ta", "password123")
	keyB := seedWithAPIKey(t, e.testEnv, "tb@example.com", "tb", "password123")

	// A 起单
	_, body := e.do(t, "POST", "/api/me/topup",
		map[string]any{"credits": 100_000_000, "channel": "waffo"},
		func(r *http.Request) {
			r.Header.Set("X-API-Key", keyA)
			r.Header.Set("X-Idempotency-Key", "44444444444444444444444444444444")
		})
	orderID := decode[map[string]any](t, body)["order_id"].(string)

	// B 查 A 的单 → 应 404（不暴露"存在但不是你的"）
	status, _ := e.do(t, "GET", "/api/me/topup/"+orderID, nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", keyB) })
	if status != http.StatusNotFound {
		t.Errorf("串号查 status = %d，应 404", status)
	}
}

func TestListTopupOrders(t *testing.T) {
	e := newWalletEnv(t)
	plaintext := seedWithAPIKey(t, e.testEnv, "t5@example.com", "t5", "password123")

	// 起两单
	for _, idem := range []string{
		"55555555555555555555555555555551",
		"55555555555555555555555555555552",
	} {
		_, body := e.do(t, "POST", "/api/me/topup",
			map[string]any{"credits": 50_000_000, "channel": "waffo"},
			func(r *http.Request) {
				r.Header.Set("X-API-Key", plaintext)
				r.Header.Set("X-Idempotency-Key", idem)
			})
		if len(body) == 0 {
			t.Fatal("空响应")
		}
	}

	status, body := e.do(t, "GET", "/api/me/topup-orders", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) })
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}](t, body)
	if got.Total != 2 || len(got.Items) != 2 {
		t.Fatalf("total = %d, len = %d", got.Total, len(got.Items))
	}
	// 每条都不含内部字段
	for _, it := range got.Items {
		for _, forbidden := range []string{"wallet_ledger_id", "channel_fee", "channel"} {
			if _, ok := it[forbidden]; ok {
				t.Errorf("列表项不该带 %q", forbidden)
			}
		}
	}
}

func TestDevMarkTopupPaid(t *testing.T) {
	e := newWalletEnv(t)
	plaintext := seedWithAPIKey(t, e.testEnv, "t6@example.com", "t6", "password123")
	pid := passengerIDOf(t, e.testEnv, "t6@example.com")

	_, body := e.do(t, "POST", "/api/me/topup",
		map[string]any{"credits": 100_000_000, "channel": "waffo"},
		func(r *http.Request) {
			r.Header.Set("X-API-Key", plaintext)
			r.Header.Set("X-Idempotency-Key", "66666666666666666666666666666666")
		})
	orderID := decode[map[string]any](t, body)["order_id"].(string)

	// mock webhook 到账
	status, resp := e.do(t, "POST", "/api/internal/topup/"+orderID+"/paid", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) })
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, resp)
	}
	got := decode[map[string]any](t, resp)
	if got["status"] != "paid" {
		t.Errorf("status = %v", got["status"])
	}
	// 钱包应 +100（净）
	bal, _ := e.wallets.Get(context.Background(), pid)
	if bal.Balance != 100_000_000 {
		t.Errorf("到账后余额 = %d", bal.Balance)
	}
}
