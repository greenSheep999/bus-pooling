package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/paymentgw"
)

// settlementHelper · 造一个装配了 gateway client 的 env（用 test 秘钥）
func settlementHelper(t *testing.T, secret string) *testEnv {
	t.Helper()
	env := newEnv(t)
	client, err := paymentgw.New(paymentgw.Config{
		BaseURL:          "http://ignored",
		BearerToken:      "test-bearer",
		SettlementSecret: secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.server.paymentGW = client

	// 重建 mux 让 handleGatewaySettlement 路由生效
	newMux := http.NewServeMux()
	env.server.Routes(newMux)
	env.srv.Config.Handler = newMux
	return env
}

// seedPassenger 造一条乘客记录·返回 passenger.id
func seedPassenger(t *testing.T, env *testEnv, email, username, password string) string {
	t.Helper()
	sc, body := env.do(t, "POST", "/api/register", map[string]any{
		"email": email, "username": username, "password": password,
	})
	if sc != http.StatusCreated {
		t.Fatalf("seed passenger: %d %s", sc, body)
	}
	var r struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	return r.ID
}

// signSettlement 造合法签名
func signSettlement(secret string, body []byte, at time.Time) (sig, ts string) {
	ts = strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v1:"))
	mac.Write([]byte(ts))
	mac.Write([]byte(":"))
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil)), ts
}

func postSettlement(t *testing.T, env *testEnv, body []byte, sig, ts string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest("POST", env.srv.URL+"/api/hooks/paymentgw/settlement",
		bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-404bus-Signature", sig)
	req.Header.Set("X-404bus-Timestamp", ts)
	req.Header.Set("X-404bus-Event-Id", extractEventID(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, buf
}

func extractEventID(body []byte) string {
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	if v, ok := m["event_id"].(string); ok {
		return v
	}
	return ""
}

// TestSettlement_BadSignatureRejected · 坏签名 401
func TestSettlement_BadSignatureRejected(t *testing.T) {
	env := settlementHelper(t, "test-secret")
	body := []byte(`{"event_id":"e1","gateway_payment_id":"pay_1","state":"settled","kind":"settled"}`)
	status, _ := postSettlement(t, env, body, "v1="+hex.EncodeToString(make([]byte, 32)),
		strconv.FormatInt(time.Now().Unix(), 10))
	if status != http.StatusUnauthorized {
		t.Errorf("坏签应 401·得到 %d", status)
	}
}

// TestSettlement_SettledMarksPaid · 好签 + kind=settled + 已建 order → wallet 加钱
func TestSettlement_SettledMarksPaid(t *testing.T) {
	secret := "settled-secret"
	env := settlementHelper(t, secret)

	// 起 order + attach gateway id
	pid := seedPassenger(t, env, "st@e.com", "stuser", "password123")
	order, err := env.topups.CreateOrder(t.Context(), pid, "waffo", 100_000_000,
		"pending://gateway", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.topups.AttachGateway(t.Context(), order.ID, "pay_st1",
		"https://checkout", ""); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(paymentgw.SettlementEvent{
		EventID:          "ev_settled_1",
		GatewayPaymentID: "pay_st1",
		ClientOrderID:    order.ID,
		ProviderKind:     "waffo_checkout",
		ExpectedAmount:   "15.00",
		ExpectedAsset:    "USD",
		ReceivedAmount:   "15.00",
		ReceivedAsset:    "USD",
		State:            "settled",
		Kind:             "settled",
		SettledAt:        time.Now().Unix(),
	})
	sig, ts := signSettlement(secret, body, time.Now())
	status, respBody := postSettlement(t, env, body, sig, ts)
	if status != http.StatusOK {
		t.Fatalf("settled 应 200·得到 %d body=%s", status, respBody)
	}
	outcome := decode[map[string]string](t, respBody)["outcome"]
	if outcome != "accepted" {
		t.Errorf("outcome=%q·want accepted", outcome)
	}
	// wallet 余额应到账 = credits 100
	bal, err := env.wallets.Get(t.Context(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Balance != 100_000_000 {
		t.Errorf("wallet.balance = %d, want 100_000_000", bal.Balance)
	}

	// 幂等重放：同 event_id 再来一次·outcome=duplicate·余额不变
	sig2, ts2 := signSettlement(secret, body, time.Now())
	status2, respBody2 := postSettlement(t, env, body, sig2, ts2)
	if status2 != http.StatusOK {
		t.Fatalf("重放应 200·得到 %d", status2)
	}
	if got := decode[map[string]string](t, respBody2)["outcome"]; got != "duplicate" {
		t.Errorf("重放 outcome=%q·want duplicate", got)
	}
	bal2, _ := env.wallets.Get(t.Context(), pid)
	if bal2.Balance != 100_000_000 {
		t.Errorf("重放后 balance = %d·超扣了", bal2.Balance)
	}
}

// TestSettlement_RefundedReversesWallet · settled 后 refund · wallet 反向到 0
func TestSettlement_RefundedReversesWallet(t *testing.T) {
	secret := "refund-secret"
	env := settlementHelper(t, secret)

	pid := seedPassenger(t, env, "rf@e.com", "rfuser", "password123")
	order, _ := env.topups.CreateOrder(t.Context(), pid, "waffo", 100_000_000,
		"pending://x", 15*time.Minute)
	_ = env.topups.AttachGateway(t.Context(), order.ID, "pay_rf1", "https://co", "")

	// 先 settled
	settleBody, _ := json.Marshal(paymentgw.SettlementEvent{
		EventID: "ev_rf_settled", GatewayPaymentID: "pay_rf1",
		ClientOrderID: order.ID, ProviderKind: "waffo_checkout",
		ExpectedAmount: "15", ExpectedAsset: "USD",
		ReceivedAmount: "15", ReceivedAsset: "USD",
		State: "settled", Kind: "settled", SettledAt: time.Now().Unix(),
	})
	sig, ts := signSettlement(secret, settleBody, time.Now())
	postSettlement(t, env, settleBody, sig, ts)

	balAfterSettle, _ := env.wallets.Get(t.Context(), pid)
	if balAfterSettle.Balance != 100_000_000 {
		t.Fatalf("settle 后 balance = %d, want 100_000_000", balAfterSettle.Balance)
	}

	// 再 refunded
	refundBody, _ := json.Marshal(paymentgw.SettlementEvent{
		EventID: "ev_rf_refund", GatewayPaymentID: "pay_rf1",
		ClientOrderID: order.ID, ProviderKind: "waffo_checkout",
		ExpectedAmount: "15", ExpectedAsset: "USD",
		ReceivedAmount: "15", ReceivedAsset: "USD",
		State: "refunded", Kind: "refunded", SettledAt: time.Now().Unix(),
	})
	sig2, ts2 := signSettlement(secret, refundBody, time.Now())
	status, respBody := postSettlement(t, env, refundBody, sig2, ts2)
	if status != http.StatusOK {
		t.Fatalf("refund 应 200·得到 %d body=%s", status, respBody)
	}

	balAfterRefund, _ := env.wallets.Get(t.Context(), pid)
	if balAfterRefund.Balance != 0 {
		t.Errorf("refund 后 balance = %d·want 0", balAfterRefund.Balance)
	}
}

// TestSettlement_UnmatchedGatewayID · gateway_payment_id 找不到 · outcome=unmatched
func TestSettlement_UnmatchedGatewayID(t *testing.T) {
	secret := "um-secret"
	env := settlementHelper(t, secret)

	body, _ := json.Marshal(paymentgw.SettlementEvent{
		EventID: "ev_um", GatewayPaymentID: "pay_nonexistent",
		Kind: "settled", State: "settled", SettledAt: time.Now().Unix(),
	})
	sig, ts := signSettlement(secret, body, time.Now())
	status, respBody := postSettlement(t, env, body, sig, ts)
	if status != http.StatusOK {
		t.Fatalf("unmatched 应 200 (让 gateway 不重发)·得到 %d", status)
	}
	if got := decode[map[string]string](t, respBody)["outcome"]; got != "unmatched" {
		t.Errorf("outcome=%q·want unmatched", got)
	}
}

var _ = fmt.Sprintf // keep import if unused
