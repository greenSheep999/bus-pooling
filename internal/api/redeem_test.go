package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/passenger"
	"github.com/bus-pooling/bus-pooling/internal/redeem"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
	"github.com/bus-pooling/bus-pooling/internal/topup"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

// walletEnv 起一个装好 redeems + topups 的 env（比 pullEnv 精简，不需要 decider）。
//
// 复用 newEnvBase 会跟 api_test.go 的 helper 挂钩；这个模块自己拉一份，
// 让 handler_test 能独立跑（不受装配 agent 顺序影响）。
type walletEnv struct {
	*testEnv
	redeems *redeem.Store
	topups  *topup.Store
}

func newWalletEnv(t *testing.T) *walletEnv {
	t.Helper()
	ctx := context.Background()

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "wallet.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}

	wallets := wallet.NewStore(d.DB)
	redeems := redeem.NewStore(d.DB)
	topups := topup.NewStore(d.DB)

	mux := http.NewServeMux()
	srv := NewServer(ServerDeps{
		DB:           d.DB,
		Passengers:   passenger.NewStore(d.DB),
		Wallets:      wallets,
		Strategies:   strategy.NewStore(d.DB),
		Buses:        bus.NewStore(d.DB),
		Redeems:      redeems,
		Topups:       topups,
		SecureCookie: false,
	})
	// 只挂本模块的路由，别牵涉别人的 handler（其他模块还在装配中）
	mux.Handle("POST /api/me/redeem",
		handler(srv.RequireAuth(srv.handleRedeem)))
	mux.Handle("POST /api/me/topup",
		handler(srv.RequireAuth(srv.handleCreateTopup)))
	mux.Handle("GET /api/me/topup/{order_id}",
		handler(srv.RequireAuth(srv.handleGetTopupOrder)))
	mux.Handle("GET /api/me/topup-orders",
		handler(srv.RequireAuth(srv.handleListTopupOrders)))
	mux.Handle("POST /api/internal/topup/{order_id}/paid",
		handler(srv.RequireAuth(srv.handleDevMarkTopupPaid)))
	// 复用 auth 路由做 seed（登录 / 建 key）
	mux.Handle("POST /api/register", handler(srv.handleRegister))
	mux.Handle("POST /api/login", handler(srv.handleLogin))
	mux.Handle("POST /api/me/api-keys",
		handler(srv.RequireSession(srv.handleCreateAPIKey)))

	httpSrv := httptest.NewServer(mux)
	t.Cleanup(func() {
		httpSrv.Close()
		_ = d.Close()
	})
	return &walletEnv{
		testEnv: &testEnv{srv: httpSrv, db: d, wallets: wallets},
		redeems: redeems,
		topups:  topups,
	}
}

func TestRedeemHappyPath(t *testing.T) {
	e := newWalletEnv(t)
	plaintext := seedWithAPIKey(t, e.testEnv, "r1@example.com", "r1", "password123")
	pid := passengerIDOf(t, e.testEnv, "r1@example.com")

	if err := e.redeems.Seed(context.Background(), "KRC-HELLO", 50_000_000, "test", nil); err != nil {
		t.Fatal(err)
	}
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) }

	status, body := e.do(t, "POST", "/api/me/redeem",
		map[string]any{"code": "krc-hello"}, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[map[string]any](t, body)
	if got["credits"].(float64) != 50_000_000 {
		t.Errorf("credits = %v", got["credits"])
	}
	if got["balance_after"].(float64) != 50_000_000 {
		t.Errorf("balance_after = %v", got["balance_after"])
	}
	// 响应不该带内部字段（status / used_by 之类）
	for _, k := range []string{"status", "used_by", "expires_at", "code", "replayed"} {
		if _, ok := got[k]; ok {
			t.Errorf("响应不该带内部字段 %q，got: %v", k, got)
		}
	}
	// 钱包也应真扣款
	bal, err := e.wallets.Get(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if bal.Balance != 50_000_000 {
		t.Errorf("余额 = %d", bal.Balance)
	}
}

func TestRedeemInvalidCode(t *testing.T) {
	e := newWalletEnv(t)
	plaintext := seedWithAPIKey(t, e.testEnv, "r2@example.com", "r2", "password123")
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) }

	status, body := e.do(t, "POST", "/api/me/redeem",
		map[string]any{"code": "NOPE"}, withKey)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	// message 不能含内部术语
	got := decode[Error](t, body)
	if got.Message == "" {
		t.Error("message 应有人话")
	}
}

func TestRedeemAlreadyUsedByOther(t *testing.T) {
	e := newWalletEnv(t)
	_ = seedWithAPIKey(t, e.testEnv, "a@example.com", "aa", "password123")
	keyB := seedWithAPIKey(t, e.testEnv, "b@example.com", "bb", "password123")
	pidA := passengerIDOf(t, e.testEnv, "a@example.com")

	if err := e.redeems.Seed(context.Background(), "SHARED", 10_000_000, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.redeems.Consume(context.Background(), pidA, "SHARED"); err != nil {
		t.Fatal(err)
	}

	status, body := e.do(t, "POST", "/api/me/redeem",
		map[string]any{"code": "SHARED"},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyB) })
	if status != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[Error](t, body)
	if got.Code != CodeConflict {
		t.Errorf("code = %q", got.Code)
	}
}

func TestRedeemIdempotentReplay(t *testing.T) {
	e := newWalletEnv(t)
	plaintext := seedWithAPIKey(t, e.testEnv, "idem@example.com", "idem", "password123")
	withKey := func(r *http.Request) {
		r.Header.Set("X-API-Key", plaintext)
		r.Header.Set("X-Idempotency-Key", "cafebabecafebabecafebabecafebabe")
	}
	if err := e.redeems.Seed(context.Background(), "IDEM", 30_000_000, "", nil); err != nil {
		t.Fatal(err)
	}

	// 首次
	body1, _ := json.Marshal(map[string]any{"code": "IDEM"})
	status1, resp1 := e.do(t, "POST", "/api/me/redeem", map[string]any{"code": "IDEM"}, withKey)
	if status1 != http.StatusOK {
		t.Fatalf("status1 = %d, body = %s", status1, resp1)
	}
	// 二次带同一 idempotency-key
	status2, resp2 := e.do(t, "POST", "/api/me/redeem", map[string]any{"code": "IDEM"}, withKey)
	if status2 != http.StatusOK {
		t.Fatalf("status2 = %d, body = %s", status2, resp2)
	}
	// 字节一致（幂等承诺）
	if string(resp1) != string(resp2) {
		t.Errorf("重放响应不一致:\n1: %s\n2: %s", resp1, resp2)
	}
	_ = body1
}
