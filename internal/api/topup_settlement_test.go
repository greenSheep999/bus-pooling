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
	"github.com/bus-pooling/bus-pooling/internal/topup"
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
	// pending_topup 支持（P0-2 / P1-3 修）
	env.server.pendingTopups = topup.NewPendingStore(env.db.DB)

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

// **P0-2 复现**：early settlement · pending_topup 卡 initial
//
// 场景：CreateOrder 后 · 在 AttachGateway 之前 · webhook 已到（gateway 极快）·
// pending_topup 仍是 initial。修前 settled 分支只做 gateway_ordered→gateway_paid ·
// 静默 rows=0 · 订单已 credited 但 pending_topup 永远卡 initial · janitor 后续误标 expired。
// 修后 EnsureAtLeast 跨态跃迁 · pending 一路推到 completed。
func TestSettlement_EarlyPendingRecovers(t *testing.T) {
	secret := "early-secret"
	env := settlementHelper(t, secret)

	pid := seedPassenger(t, env, "early@e.com", "earlyuser", "password123")
	// **不**调 AttachGateway · pending_topup 也预先造一个 initial 行
	order, _ := env.topups.CreateOrder(t.Context(), pid, "waffo", 100_000_000,
		"pending://gateway", 15*time.Minute)
	// 手工建 pending_topup initial（真实场景 handleCreateTopup 会顺手落 · 单测这里跳过 handler）
	if _, err := env.server.pendingTopups.Create(t.Context(), topup.Pending{
		IdempotencyRecordID: "irec-early",
		PassengerID:         pid,
		TopupOrderID:        order.ID,
	}); err != nil {
		// idempotency_record FK 需要 · 先造
		if _, err := env.server.db.Exec(`
			INSERT INTO idempotency_record (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
			VALUES ('irec-early', ?, 'POST', '/api/me/topup', 'kk-early-0000000000000000000', 'fp', ?)`,
			pid, time.Now().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		if _, err := env.server.pendingTopups.Create(t.Context(), topup.Pending{
			IdempotencyRecordID: "irec-early",
			PassengerID:         pid,
			TopupOrderID:        order.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 现在 pending 是 initial · settlement webhook 到
	body, _ := json.Marshal(paymentgw.SettlementEvent{
		EventID:          "ev_early_1",
		GatewayPaymentID: "pay_racing_early",
		ClientOrderID:    order.ID, // fallback 匹配
		Kind:             "settled", State: "settled", SettledAt: time.Now().Unix(),
	})
	sig, ts := signSettlement(secret, body, time.Now())
	status, respBody := postSettlement(t, env, body, sig, ts)
	if status != http.StatusOK {
		t.Fatalf("early settlement 应 200·得到 %d body=%s", status, respBody)
	}

	// 断言：钱包到账
	bal, _ := env.wallets.Get(t.Context(), pid)
	if bal.Balance != 100_000_000 {
		t.Errorf("wallet.balance = %d · want 100_000_000（early settlement 未入账）", bal.Balance)
	}

	// 断言：pending_topup **不能卡在 initial** · 应该 = completed
	p, err := env.server.pendingTopups.GetByOrderID(t.Context(), order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != topup.PendingCompleted {
		t.Errorf("pending_topup.status = %s · want completed（P0-2 修：跨态跃迁一路推）",
			p.Status)
	}
}

// TestSettlement_FallbackByClientOrderID · P0-A · gateway 极快 webhook 先到·
// gateway_payment_id 还没 AttachGateway · client_order_id fallback 匹配成功·wallet 应到账
func TestSettlement_FallbackByClientOrderID(t *testing.T) {
	secret := "fb-secret"
	env := settlementHelper(t, secret)

	pid := seedPassenger(t, env, "fb@e.com", "fbuser", "password123")
	// **不**调 AttachGateway · 模拟 webhook 先到 · gateway_payment_id 还未回填
	order, _ := env.topups.CreateOrder(t.Context(), pid, "waffo", 100_000_000,
		"pending://gateway", 15*time.Minute)

	body, _ := json.Marshal(paymentgw.SettlementEvent{
		EventID:          "ev_fb_1",
		GatewayPaymentID: "pay_racing", // 我方从未见过
		ClientOrderID:    order.ID,     // 我方发出的 order.ID
		Kind:             "settled", State: "settled", SettledAt: time.Now().Unix(),
	})
	sig, ts := signSettlement(secret, body, time.Now())
	status, respBody := postSettlement(t, env, body, sig, ts)
	if status != http.StatusOK {
		t.Fatalf("fallback settled 应 200·得到 %d body=%s", status, respBody)
	}
	if got := decode[map[string]string](t, respBody)["outcome"]; got != "accepted" {
		t.Errorf("outcome=%q·want accepted (fallback 应匹配)", got)
	}
	bal, _ := env.wallets.Get(t.Context(), pid)
	if bal.Balance != 100_000_000 {
		t.Errorf("fallback 后 balance = %d·want 100_000_000", bal.Balance)
	}
	// 回填 gateway_payment_id 应生效
	o2, _ := env.topups.FindByGatewayPaymentID(t.Context(), "pay_racing")
	if o2.ID != order.ID {
		t.Errorf("fallback 未回填 gateway_payment_id (o2.ID=%q · order.ID=%q)", o2.ID, order.ID)
	}
}

// TestSettlement_RefundAllowsNegativeBalance · P0-B · settled → 用户花光 → refund
// refund 应把 balance 扣成负·不能吞成 duplicate 让 gateway 停重试·订单状态应到 refunded
func TestSettlement_RefundAllowsNegativeBalance(t *testing.T) {
	secret := "neg-secret"
	env := settlementHelper(t, secret)

	pid := seedPassenger(t, env, "neg@e.com", "neguser", "password123")
	order, _ := env.topups.CreateOrder(t.Context(), pid, "waffo", 100_000_000,
		"pending://x", 15*time.Minute)
	_ = env.topups.AttachGateway(t.Context(), order.ID, "pay_neg", "https://co", "")

	// settle · balance +100M
	body, _ := json.Marshal(paymentgw.SettlementEvent{
		EventID: "ev_neg_settled", GatewayPaymentID: "pay_neg", ClientOrderID: order.ID,
		Kind: "settled", State: "settled", SettledAt: time.Now().Unix(),
	})
	sig, ts := signSettlement(secret, body, time.Now())
	postSettlement(t, env, body, sig, ts)

	// **模拟用户花光** · SQL 直接压 balance=0（模拟拉号扣款 · 不走 wallet.Debit 免污染流水）
	if _, err := env.server.db.Exec(
		`UPDATE wallet SET balance = 0 WHERE passenger_id = ?`, pid); err != nil {
		t.Fatal(err)
	}

	// refund · 期望 balance 走到 -95M（reversed 100M paid + 5M fee）
	refundBody, _ := json.Marshal(paymentgw.SettlementEvent{
		EventID: "ev_neg_refund", GatewayPaymentID: "pay_neg", ClientOrderID: order.ID,
		Kind: "refunded", State: "refunded", SettledAt: time.Now().Unix(),
	})
	rsig, rts := signSettlement(secret, refundBody, time.Now())
	status, respBody := postSettlement(t, env, refundBody, rsig, rts)
	if status != http.StatusOK {
		t.Fatalf("refund 应 200·得到 %d body=%s", status, respBody)
	}
	if got := decode[map[string]string](t, respBody)["outcome"]; got != "accepted" {
		t.Errorf("outcome=%q·want accepted (不能吞成 duplicate)", got)
	}
	bal, _ := env.wallets.Get(t.Context(), pid)
	// paid=100M+5M=105M(recharge) · fee=5M · reverse: +5M -105M · 从 0 → -100M
	if bal.Balance != -100_000_000 {
		t.Errorf("refund 后 balance = %d · want -100_000_000（负余额记录负债）", bal.Balance)
	}
	// order 状态应到 refunded
	got, _ := env.topups.Get(t.Context(), pid, order.ID)
	if got.Status != "refunded" {
		t.Errorf("order.status = %q · want refunded", got.Status)
	}
}

var _ = fmt.Sprintf // keep import if unused
