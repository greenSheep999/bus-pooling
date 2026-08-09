package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/delivery/handoff"
	"github.com/bus-pooling/bus-pooling/internal/downstream"
	"github.com/bus-pooling/bus-pooling/internal/insight"
	"github.com/bus-pooling/bus-pooling/internal/passenger"
	"github.com/bus-pooling/bus-pooling/internal/pullrecord"
	"github.com/bus-pooling/bus-pooling/internal/redeem"
	"github.com/bus-pooling/bus-pooling/internal/secrets"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
	"github.com/bus-pooling/bus-pooling/internal/topup"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

type testEnv struct {
	srv     *httptest.Server
	db      *db.DB
	wallets *wallet.Store
}

func newEnv(t *testing.T) *testEnv {
	return newEnvBase(t, nil)
}

// newEnvWithDecider 装配一个带 decider 的 env（拉号端点用）。
func newEnvWithDecider(t *testing.T, vendor decider.VendorClient, pool decider.PoolClient) *testEnv {
	return newEnvBase(t, func(sqldb *db.DB) *decider.Orchestrator {
		return decider.New(decider.Config{
			DB:     sqldb.DB,
			State:  decider.NewStore(sqldb.DB),
			Vendor: vendor,
			Pool:   pool,
			Rates:  decider.Rates{Service: 500}, // 测试用极小服务费率
		})
	})
}

func newEnvBase(t *testing.T, mkDecider func(*db.DB) *decider.Orchestrator) *testEnv {
	t.Helper()
	ctx := context.Background()

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}

	wallets := wallet.NewStore(d.DB)
	var orch *decider.Orchestrator
	if mkDecider != nil {
		orch = mkDecider(d)
	}

	// downstream 需要 cipher（AES-GCM），测试固定一把
	cipher, err := secrets.New(strings.Repeat("01", 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	mux := http.NewServeMux()
	NewServer(ServerDeps{
		DB:          d.DB,
		Passengers:  passenger.NewStore(d.DB),
		Wallets:     wallets,
		Strategies:  strategy.NewStore(d.DB),
		Buses:       bus.NewStore(d.DB),
		Decider:     orch,
		Redeems:     redeem.NewStore(d.DB),
		Topups:      topup.NewStore(d.DB),
		PullRecords: pullrecord.NewStore(d.DB),
		Handoffs:    handoff.NewStore(d.DB, 0),
		Insights:    insight.NewStore(d.DB),
		Downstreams: downstream.NewStore(d.DB, cipher),
		// VendorView / Pool 保留 nil —— handler 里各自有 nil 兜底（返 503）
		SecureCookie: false,
	}).Routes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		_ = d.Close()
	})
	return &testEnv{srv: srv, db: d, wallets: wallets}
}

// walletCreditForTest 造一个测试用的充值 Move。
func walletCreditForTest(passengerID string, amount int64) wallet.Move {
	return wallet.Move{
		PassengerID: passengerID, Reason: wallet.ReasonRecharge,
		Amount: amount, Memo: "test seed",
	}
}

func (e *testEnv) do(t *testing.T, method, path string, body any, mutate ...func(*http.Request)) (int, []byte) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, e.srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, m := range mutate {
		m(req)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("解 JSON 失败: %v\n原文: %s", err, raw)
	}
	return v
}

// Iss #4 DoD：注册 → 登录 → 生成 API key → 用 API key 调 profile 全通
func TestFullAuthFlow(t *testing.T) {
	e := newEnv(t)

	// ① 注册
	status, body := e.do(t, "POST", "/api/register", map[string]any{
		"email": "alice@example.com", "username": "alice", "password": "password123",
	})
	if status != http.StatusCreated {
		t.Fatalf("注册 status = %d, body = %s", status, body)
	}
	prof := decode[map[string]any](t, body)
	if prof["email"] != "alice@example.com" {
		t.Errorf("注册返回 email = %v", prof["email"])
	}
	// 余额不该出现在 profile 里（契约：只在 /api/me/wallet）
	if _, ok := prof["balance"]; ok {
		t.Error("profile 不该带 balance")
	}

	// ② 登录拿 cookie
	jar := newJar(t)
	client := &http.Client{Jar: jar}
	loginBody, _ := json.Marshal(map[string]any{
		"account": "alice@example.com", "password": "password123",
	})
	resp, err := client.Post(e.srv.URL+"/api/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("登录 status = %d", resp.StatusCode)
	}
	if len(jar.cookies()) == 0 {
		t.Fatal("登录没下发 cookie")
	}

	// ③ 用 cookie 建 API key（这个端点只允许会话）
	keyResp, err := client.Post(e.srv.URL+"/api/me/api-keys", "application/json",
		strings.NewReader(`{"name":"CI"}`))
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(keyResp.Body)
	_ = keyResp.Body.Close()
	if keyResp.StatusCode != http.StatusCreated {
		t.Fatalf("建 key status = %d, body = %s", keyResp.StatusCode, buf.String())
	}
	created := decode[map[string]any](t, buf.Bytes())
	plaintext, _ := created["key"].(string)
	if !strings.HasPrefix(plaintext, "usr-") {
		t.Fatalf("明文 key 形状不对: %q", plaintext)
	}

	// ④ 用 API key 调 /api/me（两种 header 都要通）
	for _, setHeader := range []func(*http.Request){
		func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) },
		func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+plaintext) },
	} {
		status, body := e.do(t, "GET", "/api/me", nil, setHeader)
		if status != http.StatusOK {
			t.Fatalf("用 API key 调 /api/me status = %d, body = %s", status, body)
		}
		got := decode[map[string]any](t, body)
		if got["username"] != "alice" {
			t.Errorf("/api/me 返回 username = %v", got["username"])
		}
	}
}

// API key 不能建新 key、不能改密码 —— 防"泄露的 key 换成新 key 把主人锁在门外"
func TestAPIKeyCannotEscalate(t *testing.T) {
	e := newEnv(t)
	plaintext := seedWithAPIKey(t, e, "bob@example.com", "bob", "password123")

	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) }

	status, body := e.do(t, "POST", "/api/me/api-keys", map[string]any{"name": "second"}, withKey)
	if status != http.StatusForbidden {
		t.Errorf("用 API key 建新 key status = %d，应为 403，body = %s", status, body)
	}
	if got := decode[Error](t, body); got.Code != CodeSessionRequired {
		t.Errorf("错误码 = %q，应为 %q", got.Code, CodeSessionRequired)
	}

	status, body = e.do(t, "POST", "/api/me/password", map[string]any{
		"old_password": "password123", "new_password": "newpassword",
	}, withKey)
	if status != http.StatusForbidden {
		t.Errorf("用 API key 改密码 status = %d，应为 403", status)
	}
	if got := decode[Error](t, body); got.Code != CodeSessionRequired {
		t.Errorf("错误码 = %q，应为 %q", got.Code, CodeSessionRequired)
	}
}

func TestUnauthenticatedRejected(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/api/me", "/api/me/wallet", "/api/me/ledger", "/api/me/api-keys"} {
		status, body := e.do(t, "GET", path, nil)
		if status != http.StatusUnauthorized {
			t.Errorf("%s 无鉴权 status = %d，应为 401", path, status)
		}
		if got := decode[Error](t, body); got.Code != CodeUnauthenticated {
			t.Errorf("%s 错误码 = %q", path, got.Code)
		}
	}
}

func TestInvalidAPIKeyRejected(t *testing.T) {
	e := newEnv(t)
	status, body := e.do(t, "GET", "/api/me", nil, func(r *http.Request) {
		r.Header.Set("X-API-Key", "usr-deadbeefdeadbeef")
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d", status)
	}
	if got := decode[Error](t, body); got.Code != CodeInvalidAPIKey {
		t.Errorf("错误码 = %q，应为 %q", got.Code, CodeInvalidAPIKey)
	}
}

// 登录失败不能区分"账号不存在"和"密码错"
func TestLoginDoesNotLeakAccountExistence(t *testing.T) {
	e := newEnv(t)
	seed(t, e, "carol@example.com", "carol", "password123")

	_, bodyWrongPw := e.do(t, "POST", "/api/login", map[string]any{
		"account": "carol@example.com", "password": "wrong"})
	_, bodyNoAcct := e.do(t, "POST", "/api/login", map[string]any{
		"account": "nobody@example.com", "password": "wrong"})

	a := decode[Error](t, bodyWrongPw)
	b := decode[Error](t, bodyNoAcct)
	if a.Code != b.Code || a.Message != b.Message {
		t.Fatalf("两种失败的响应不同，泄露了账号是否存在:\n密码错: %+v\n无账号: %+v", a, b)
	}
}

func TestRegisterValidation(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"密码太短", map[string]any{"email": "x@y.com", "username": "xx", "password": "short"}},
		{"邮箱没 @", map[string]any{"email": "nope", "username": "xx", "password": "password123"}},
		{"用户名太短", map[string]any{"email": "x@y.com", "username": "a", "password": "password123"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := e.do(t, "POST", "/api/register", tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", status, body)
			}
		})
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	e := newEnv(t)
	seed(t, e, "dup@example.com", "dupuser", "password123")

	status, body := e.do(t, "POST", "/api/register", map[string]any{
		"email": "dup@example.com", "username": "other", "password": "password123"})
	if status != http.StatusConflict {
		t.Fatalf("重复注册 status = %d，应为 409，body = %s", status, body)
	}
}

// 拒绝未知字段 —— 客户端拼错字段名时早报错，而不是静默忽略
func TestRejectsUnknownFields(t *testing.T) {
	e := newEnv(t)
	status, body := e.do(t, "POST", "/api/register", map[string]any{
		"email": "z@example.com", "username": "zed", "password": "password123",
		"is_admin": true, // 不存在的字段
	})
	if status != http.StatusBadRequest {
		t.Fatalf("含未知字段应报 400，得到 %d，body = %s", status, body)
	}
	if got := decode[Error](t, body); got.Code != CodeBadJSON {
		t.Errorf("错误码 = %q，应为 %q", got.Code, CodeBadJSON)
	}
}

func TestWalletAndLedgerEndpoints(t *testing.T) {
	e := newEnv(t)
	plaintext := seedWithAPIKey(t, e, "wal@example.com", "wally", "password123")
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", plaintext) }

	// 新账号余额 0
	status, body := e.do(t, "GET", "/api/me/wallet", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	w := decode[map[string]any](t, body)
	if w["balance"].(float64) != 0 {
		t.Errorf("新账号余额 = %v", w["balance"])
	}

	// 充点钱再看流水
	pid := passengerIDOf(t, e, "wal@example.com")
	if _, err := e.wallets.Credit(context.Background(), wallet.Move{
		PassengerID: pid, Reason: wallet.ReasonRecharge, Amount: 100_000_000, Memo: "测试充值",
	}); err != nil {
		t.Fatal(err)
	}

	status, body = e.do(t, "GET", "/api/me/wallet", nil, withKey)
	if status != http.StatusOK {
		t.Fatal(status)
	}
	w = decode[map[string]any](t, body)
	if w["balance"].(float64) != 100_000_000 {
		t.Errorf("充值后余额 = %v", w["balance"])
	}

	status, body = e.do(t, "GET", "/api/me/ledger", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	l := decode[struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}](t, body)
	if l.Total != 1 || len(l.Items) != 1 {
		t.Fatalf("流水 total=%d len=%d", l.Total, len(l.Items))
	}
	// 对外类型是 topup，不是内部的 recharge（CLAUDE.md §0.1）
	if l.Items[0]["type"] != "topup" {
		t.Errorf("流水类型 = %v，want topup（内部 recharge 应映射成对外 topup）", l.Items[0]["type"])
	}
}

// 一个乘客不能看到另一个乘客的钱包 / 流水 / key
func TestTenantIsolation(t *testing.T) {
	e := newEnv(t)
	keyA := seedWithAPIKey(t, e, "a2@example.com", "aaa", "password123")
	keyB := seedWithAPIKey(t, e, "b2@example.com", "bbb", "password123")

	pidA := passengerIDOf(t, e, "a2@example.com")
	if _, err := e.wallets.Credit(context.Background(), wallet.Move{
		PassengerID: pidA, Reason: wallet.ReasonRecharge, Amount: 50_000_000,
	}); err != nil {
		t.Fatal(err)
	}

	// B 看自己的钱包应该是 0，不该看到 A 的 50
	_, body := e.do(t, "GET", "/api/me/wallet", nil, func(r *http.Request) {
		r.Header.Set("X-API-Key", keyB)
	})
	if got := decode[map[string]any](t, body); got["balance"].(float64) != 0 {
		t.Fatalf("B 看到了余额 %v，应为 0（串号了）", got["balance"])
	}

	// A 的 key 列表里不该有 B 的（前端 TS 契约：ApiKey[] 纯数组）
	_, body = e.do(t, "GET", "/api/me/api-keys", nil, func(r *http.Request) {
		r.Header.Set("X-API-Key", keyA)
	})
	keys := decode[[]map[string]any](t, body)
	if len(keys) != 1 {
		t.Fatalf("A 看到 %d 个 key，应为 1", len(keys))
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	e := newEnv(t)
	seed(t, e, "out@example.com", "outy", "password123")

	jar := newJar(t)
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]any{"account": "out@example.com", "password": "password123"})
	resp, err := client.Post(e.srv.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	// 登出前能访问
	r1, err := client.Get(e.srv.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	_ = r1.Body.Close()
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("登出前 /api/me status = %d", r1.StatusCode)
	}

	resp, err = client.Post(e.srv.URL+"/api/logout", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	// 登出后不能
	r2, err := client.Get(e.srv.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	_ = r2.Body.Close()
	if r2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("登出后 /api/me status = %d，应为 401", r2.StatusCode)
	}
}

// 错误响应必须是契约那个形状，且 message 不含内部术语（CLAUDE.md §12.6）
func TestErrorShapeAndNoInternalTerms(t *testing.T) {
	e := newEnv(t)
	_, body := e.do(t, "GET", "/api/me", nil)

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("错误响应不是 JSON: %s", body)
	}
	if _, ok := raw["code"]; !ok {
		t.Error("错误响应缺 code")
	}
	if _, ok := raw["message"]; !ok {
		t.Error("错误响应缺 message")
	}

	banned := []string{"housepool", "record group", "record-", "provider", "credential_ledger",
		"pending_purchase", "handed_off", "sql", "SQLITE"}
	msg := strings.ToLower(raw["message"].(string))
	for _, b := range banned {
		if strings.Contains(msg, strings.ToLower(b)) {
			t.Errorf("错误 message 含内部术语 %q: %q", b, msg)
		}
	}
}

func TestRevokeAPIKeyEndpoint(t *testing.T) {
	e := newEnv(t)
	seed(t, e, "rev@example.com", "revy", "password123")

	jar := newJar(t)
	client := &http.Client{Jar: jar}
	lb, _ := json.Marshal(map[string]any{"account": "rev@example.com", "password": "password123"})
	resp, _ := client.Post(e.srv.URL+"/api/login", "application/json", bytes.NewReader(lb))
	_ = resp.Body.Close()

	kr, err := client.Post(e.srv.URL+"/api/me/api-keys", "application/json", strings.NewReader(`{"name":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(kr.Body)
	_ = kr.Body.Close()
	created := decode[struct {
		Key  string `json:"key"`
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}](t, buf.Bytes())

	// 吊销后这把 key 立刻失效
	req, _ := http.NewRequest("DELETE", e.srv.URL+"/api/me/api-keys/"+created.Item.ID, nil)
	dr, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = dr.Body.Close()
	if dr.StatusCode != http.StatusOK {
		t.Fatalf("吊销 status = %d", dr.StatusCode)
	}

	status, _ := e.do(t, "GET", "/api/me", nil, func(r *http.Request) {
		r.Header.Set("X-API-Key", created.Key)
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("吊销后仍能用，status = %d", status)
	}

	// 吊销不存在的返 404
	req2, _ := http.NewRequest("DELETE", e.srv.URL+"/api/me/api-keys/nonexistent", nil)
	dr2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	_ = dr2.Body.Close()
	if dr2.StatusCode != http.StatusNotFound {
		t.Errorf("吊销不存在的 key status = %d，应为 404", dr2.StatusCode)
	}
}

// ── 测试辅助 ──────────────────────────────────────

func seed(t *testing.T, e *testEnv, email, username, password string) {
	t.Helper()
	status, body := e.do(t, "POST", "/api/register", map[string]any{
		"email": email, "username": username, "password": password})
	if status != http.StatusCreated {
		t.Fatalf("seed 注册失败 status=%d body=%s", status, body)
	}
}

func seedWithAPIKey(t *testing.T, e *testEnv, email, username, password string) string {
	t.Helper()
	seed(t, e, email, username, password)

	jar := newJar(t)
	client := &http.Client{Jar: jar}
	lb, _ := json.Marshal(map[string]any{"account": email, "password": password})
	resp, err := client.Post(e.srv.URL+"/api/login", "application/json", bytes.NewReader(lb))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	kr, err := client.Post(e.srv.URL+"/api/me/api-keys", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(kr.Body)
	_ = kr.Body.Close()
	if kr.StatusCode != http.StatusCreated {
		t.Fatalf("seed 建 key 失败 status=%d body=%s", kr.StatusCode, buf.String())
	}
	return decode[struct {
		Key string `json:"key"`
	}](t, buf.Bytes()).Key
}

func passengerIDOf(t *testing.T, e *testEnv, email string) string {
	t.Helper()
	var id string
	if err := e.db.QueryRow(`SELECT id FROM passenger WHERE email = ?`, email).Scan(&id); err != nil {
		t.Fatalf("查乘客 id: %v", err)
	}
	return id
}
