package topup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/paymentgw"
)

// P0 回归（codex 三轮）：POST /payments 返 404 ≠ ErrGatewayNotFound。
//
// 场景：反查用 CreatePayment 幂等重 POST · gateway 侧路径错 / 端点缺失时返 404。
// 旧实现把 404 一律映射成 ErrGatewayNotFound · janitor 会误 expire·丢单。
//
// 正确行为：**透出原始 error** · janitor 走 poll_fail_count 累计 · 到上限转 manual。
func TestGatewayPollerAdapter_FindByClientOrderID_POST404IsNotNotFound(t *testing.T) {
	// httptest server 全返 404 · 模拟"gateway 端点错 / 路径拼错"
	gwSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"endpoint not found"}`))
	}))
	defer gwSrv.Close()

	client, err := paymentgw.New(paymentgw.Config{
		BaseURL:          gwSrv.URL,
		BearerToken:      "test-bearer",
		SettlementSecret: "test-secret",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 提供 snapshot 让 FindByClientOrderID 能走到 POST
	adapter := &GatewayPollerAdapter{
		Client: client,
		LoadRequestSnapshot: func(_ context.Context, _ string) (*paymentgw.CreatePaymentRequest, error) {
			return &paymentgw.CreatePaymentRequest{
				ClientOrderID:  "order-abc",
				ProviderKind:   "waffo",
				ExpectedAmount: "10",
				ExpectedAsset:  "USD",
			}, nil
		},
	}

	_, err = adapter.FindByClientOrderID(context.Background(), "order-abc")
	if err == nil {
		t.Fatal("POST 404 应返 error · got nil")
	}
	if errors.Is(err, ErrGatewayNotFound) {
		t.Errorf("POST 404 **绝不能**映射为 ErrGatewayNotFound（会误 expire） · got=%v", err)
	}
	if errors.Is(err, ErrGatewayFindUnavailable) {
		t.Errorf("POST 404 也不是 ErrGatewayFindUnavailable（那是 snapshot 缺失） · got=%v", err)
	}
	// 应该是原始 paymentgw.APIError 透出 · janitor 用它走 poll_fail_count
	var apiErr *paymentgw.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Errorf("应透出 paymentgw.APIError{Status:404} · got=%T %v", err, err)
	}
}

// P0 回归：snapshot 空 → ErrGatewayFindUnavailable（janitor 走 manual · 不 expire）
func TestGatewayPollerAdapter_FindByClientOrderID_MissingSnapshot(t *testing.T) {
	adapter := &GatewayPollerAdapter{
		Client: nil, // 不该走到 Client
		LoadRequestSnapshot: func(_ context.Context, _ string) (*paymentgw.CreatePaymentRequest, error) {
			return nil, ErrGatewayFindUnavailable
		},
	}
	_, err := adapter.FindByClientOrderID(context.Background(), "order-xyz")
	if !errors.Is(err, ErrGatewayFindUnavailable) {
		t.Errorf("snapshot 缺失应返 ErrGatewayFindUnavailable · got=%v", err)
	}
}

// P0 回归：LoadRequestSnapshot 回调未装配 → ErrGatewayFindUnavailable
func TestGatewayPollerAdapter_FindByClientOrderID_NoCallback(t *testing.T) {
	adapter := &GatewayPollerAdapter{Client: nil, LoadRequestSnapshot: nil}
	_, err := adapter.FindByClientOrderID(context.Background(), "order-xyz")
	if !errors.Is(err, ErrGatewayFindUnavailable) {
		t.Errorf("回调未装配应返 ErrGatewayFindUnavailable · got=%v", err)
	}
}

// P0 回归：GET /payments/{id} 返 404 → ErrGatewayNotFound（允许 expire）。
// 跟 POST 404 语义**不同** —— GET 明确读单个 payment id · 404 就是"没这个 payment"。
func TestGatewayPollerAdapter_PollByGatewayPaymentID_GET404IsNotFound(t *testing.T) {
	gwSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("PollByGatewayPaymentID 应发 GET · got=%s", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer gwSrv.Close()

	client, _ := paymentgw.New(paymentgw.Config{
		BaseURL:          gwSrv.URL,
		BearerToken:      "test-bearer",
		SettlementSecret: "test-secret",
	})
	adapter := &GatewayPollerAdapter{Client: client}

	_, err := adapter.PollByGatewayPaymentID(context.Background(), "pay_missing")
	if !errors.Is(err, ErrGatewayNotFound) {
		t.Errorf("GET 404 应映射为 ErrGatewayNotFound · got=%v", err)
	}
}

// P0 回归：snapshot 完整性 —— 起单时冷冻的 request 反查时逐字段一致。
//
// 场景：起单构造 CreatePaymentRequest（含 PayerEmail / PayerReference / Amount）
// → 序列化 → SaveGatewayRequestSnapshot 落库 → janitor 反查 → UnmarshalRequestSnapshot
// → 得到跟起单时**同结构**的 request（重发 POST 时幂等指纹一致）。
//
// 关键：所有字段（含 PayerEmail 这类可能被"从 config 重建"漏掉的）都还原。
func TestSaveLoadRequestSnapshot_Fidelity(t *testing.T) {
	ps, _, oid := pendingTestDB(t)
	store := NewStore(ps.db)

	original := paymentgw.CreatePaymentRequest{
		ClientOrderID:    oid,
		ProviderKind:     "waffo",
		ExpectedAmount:   "12.345678",
		ExpectedAsset:    "USDT",
		PayerEmail:       "someone@example.com",
		PayerReference:   "user-42",
		ExpiresInSeconds: 900,
		SuccessURL:       "https://app.example/wallet",
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SaveGatewayRequestSnapshot(context.Background(), oid, raw); err != nil {
		t.Fatalf("SaveGatewayRequestSnapshot: %v", err)
	}
	got, err := store.FindByClientOrderID(context.Background(), oid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GatewayRequestSnapshot) == 0 {
		t.Fatal("snapshot 未落库")
	}
	reload, err := UnmarshalRequestSnapshot(got.GatewayRequestSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if reload.ClientOrderID != original.ClientOrderID ||
		reload.ProviderKind != original.ProviderKind ||
		reload.ExpectedAmount != original.ExpectedAmount ||
		reload.ExpectedAsset != original.ExpectedAsset ||
		reload.PayerEmail != original.PayerEmail ||
		reload.PayerReference != original.PayerReference ||
		reload.ExpiresInSeconds != original.ExpiresInSeconds ||
		reload.SuccessURL != original.SuccessURL {
		t.Errorf("snapshot 字段不一致\n  want=%+v\n  got=%+v", original, *reload)
	}
}

// P0 回归：SaveGatewayRequestSnapshot 空 snapshot 拒写。
func TestSaveGatewayRequestSnapshot_EmptyRejected(t *testing.T) {
	ps, _, oid := pendingTestDB(t)
	store := NewStore(ps.db)
	if err := store.SaveGatewayRequestSnapshot(context.Background(), oid, nil); err == nil {
		t.Error("空 snapshot 应拒写")
	}
	if err := store.SaveGatewayRequestSnapshot(context.Background(), oid, []byte{}); err == nil {
		t.Error("空 slice snapshot 应拒写")
	}
}
