package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/delivery/handoff"
)

// setupHandoff 建 env + 两号 · 返回 env + 密钥 fn + pid + 两个号 id
//
// 默认给测试放行占位路径（BP_ALLOW_HANDOFF_PLACEHOLDER=1）· 让联调三段能跑到 confirm。
// 单个测试可以再 setenv BP_HANDOFF_TRUE_PLAINTEXT=1 走真明文分支（需要 mock housepool）。
func setupHandoff(t *testing.T) (*prEnv, *testEnv, func(*http.Request), string) {
	t.Helper()
	t.Setenv("BP_ALLOW_HANDOFF_PLACEHOLDER", "1")
	e := newPREnv(t)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "hf@e.com", "hfuser", "password123")
	pid := passengerIDOf(t, base, "hf@e.com")

	e.insertRound(t, "round-1")
	e.insertRecordCred(t, "c1", pid, "round-1", "alive", 1)
	e.insertRecordCred(t, "c2", pid, "round-1", "alive", 2)

	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", key) }
	return e, base, withKey, pid
}

// ① POST /api/me/handoff → 返 token + 不返明文
func TestHandoffInit_ReturnsToken(t *testing.T) {
	_, base, withKey, _ := setupHandoff(t)

	status, body := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1", "c2"}},
		withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[map[string]json.RawMessage](t, body)
	if _, ok := got["download_token"]; !ok {
		t.Errorf("响应缺 download_token")
	}
	if _, ok := got["expires_at"]; !ok {
		t.Errorf("响应缺 expires_at")
	}
	// 明文绝不能在这个响应里（09-transactions §4）
	if _, leaked := got["keys"]; leaked {
		t.Errorf("① 阶段响应不能含明文 keys")
	}
	if _, leaked := got["key"]; leaked {
		t.Errorf("① 阶段响应不能含明文 key")
	}
}

// ② GET /api/me/handoff/{token} · 占位路径（默认）· 返显式占位 · 状态推 placeholder_delivered
func TestHandoffFulfill_ReturnsKeys(t *testing.T) {
	e, base, withKey, _ := setupHandoff(t)

	_, body := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1"}}, withKey)
	init := decode[map[string]string](t, body)
	token := init["download_token"]

	status, body := base.do(t, "GET", "/api/me/handoff/"+token, nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[struct {
		Keys []map[string]string `json:"keys"`
	}](t, body)
	if len(got.Keys) != 1 {
		t.Fatalf("keys 长度 %d, want 1", len(got.Keys))
	}
	if got.Keys[0]["credential_id"] != "c1" {
		t.Errorf("credential_id = %s, want c1", got.Keys[0]["credential_id"])
	}
	// 占位路径 · key 必须是显式占位串（防前端把它当真号）
	if key := got.Keys[0]["key"]; key == "" {
		t.Errorf("key 不能空")
	} else if !strings.HasPrefix(key, "PLACEHOLDER:") {
		t.Errorf("占位路径 key 必须 PLACEHOLDER: 前缀·got %q", key)
	}

	// 状态推 placeholder_delivered · **不是** fulfilled
	// 这样 confirm handler 一看状态就知道是占位·不能走真 DELETE
	pending, _ := e.handoffs.GetByToken(context.Background(), token)
	if pending.Status != handoff.StatusPlaceholderDelivered {
		t.Errorf("status = %s, want placeholder_delivered（占位路径不能推 fulfilled）", pending.Status)
	}
}

// ② **P0 锁**：真明文模式（BP_HANDOFF_TRUE_PLAINTEXT=1）当前 housepool 明文 endpoint 未接·
// fulfill 必须返 501 · pending_handoff 状态不推 fulfilled · 号绝不会被删。
// 上一轮审计发现的漏洞：真明文分支实际调 readHandoffPlaintext·但那函数返占位串·
// confirm 又走 DELETE·拿到假 key 却删真号。修完后本测试锁死。
func TestHandoffFulfill_TrueTextMode_RejectsUntilPlaintextEndpointReady(t *testing.T) {
	t.Setenv("BP_HANDOFF_TRUE_PLAINTEXT", "1")
	e, base, withKey, _ := setupHandoff(t)

	_, body := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1"}}, withKey)
	init := decode[map[string]string](t, body)
	token := init["download_token"]

	status, resp := base.do(t, "GET", "/api/me/handoff/"+token, nil, withKey)
	if status != http.StatusNotImplemented {
		t.Fatalf("真明文路径 fulfill 应 501·得到 %d body=%s", status, resp)
	}
	got := decode[Error](t, resp)
	if got.Code != "handoff_plaintext_unavailable" {
		t.Errorf("code = %s·want handoff_plaintext_unavailable", got.Code)
	}

	// **关键锁**：pending_handoff 状态**不能**推到 fulfilled·防 confirm 走 DELETE
	pending, _ := e.handoffs.GetByToken(context.Background(), token)
	if pending.Status == handoff.StatusFulfilled {
		t.Errorf("真明文 fulfill 失败后·status 不能推 fulfilled·实际 %s", pending.Status)
	}

	// **关键锁**：号仍 alive
	var credStatus string
	if err := e.db.DB.QueryRow(`SELECT status FROM credential_ledger WHERE id='c1'`).Scan(&credStatus); err != nil {
		t.Fatal(err)
	}
	if credStatus != "alive" {
		t.Errorf("真明文失败后·号绝不能被 handed_off·实际 status=%s", credStatus)
	}
}

// ② TTL 内可反复调 · fulfill_count 累加
func TestHandoffFulfill_MultipleRetries(t *testing.T) {
	e, base, withKey, _ := setupHandoff(t)

	_, body := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1"}}, withKey)
	init := decode[map[string]string](t, body)
	token := init["download_token"]

	for i := 0; i < 3; i++ {
		s, _ := base.do(t, "GET", "/api/me/handoff/"+token, nil, withKey)
		if s != http.StatusOK {
			t.Fatalf("第 %d 次 fulfill status = %d", i+1, s)
		}
	}
	pending, _ := e.handoffs.GetByToken(context.Background(), token)
	if pending.FulfillCount != 3 {
		t.Errorf("fulfill_count = %d, want 3", pending.FulfillCount)
	}
}

// ② 假 token → token_expired
func TestHandoffFulfill_BadToken(t *testing.T) {
	_, base, withKey, _ := setupHandoff(t)
	status, body := base.do(t, "GET",
		"/api/me/handoff/00000000000000000000000000000000",
		nil, withKey)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	got := decode[Error](t, body)
	if got.Code != "token_expired" {
		t.Errorf("code = %s, want token_expired", got.Code)
	}
}

// ② 别人的 token → token_expired（不泄漏 token 是否存在）
func TestHandoffFulfill_OtherPassenger(t *testing.T) {
	_, base, withKey, _ := setupHandoff(t)
	// 别的乘客
	keyOther := seedWithAPIKey(t, base, "other@e.com", "other", "password123")

	_, body := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1"}}, withKey)
	init := decode[map[string]string](t, body)
	token := init["download_token"]

	status, respBody := base.do(t, "GET", "/api/me/handoff/"+token, nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", keyOther) })
	if status != http.StatusNotFound {
		t.Errorf("别人的 token 该 404，得到 %d", status)
	}
	got := decode[Error](t, respBody)
	if got.Code != "token_expired" {
		t.Errorf("code = %s, want token_expired（不泄漏 token 存在）", got.Code)
	}
}

// ③ 真明文路径下 confirm → 号 handed_off · pool DELETE ·
// **skip**：当前 readHandoffPlaintext 一定返 error（housepool 明文 endpoint 未接）·
// 真明文路径根本走不到 confirm 分支。接了 endpoint 后·把 readHandoffPlaintext
// 里的 return error 换成真调 pool·然后打开这个测试。
func TestHandoffConfirm_MarksHandedOff_TrueTextPath(t *testing.T) {
	t.Skip("真明文路径 fulfill 当前 501·接了 housepool 明文 endpoint 后打开·" +
		"届时改 readHandoffPlaintext 真调 pool 明文 API")
}

// ③ **P0 保护**：占位路径下 confirm 不删号 · 号仍 alive · status=confirmed_placeholder
func TestHandoffConfirm_PlaceholderPathNeverDeletes(t *testing.T) {
	e, base, withKey, _ := setupHandoff(t)

	_, body := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1"}}, withKey)
	init := decode[map[string]string](t, body)
	token := init["download_token"]

	// fulfill → placeholder_delivered
	if s, _ := base.do(t, "GET", "/api/me/handoff/"+token, nil, withKey); s != http.StatusOK {
		t.Fatalf("fulfill 失败 status=%d", s)
	}

	status, resp := base.do(t, "POST", "/api/me/handoff/"+token+"/confirm", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("占位 confirm status = %d body=%s", status, resp)
	}

	// pending_handoff 应到 confirmed_placeholder · **不是** completed
	pending, _ := e.handoffs.GetByToken(context.Background(), token)
	if pending.Status != handoff.StatusConfirmedPlaceholder {
		t.Errorf("status = %s, want confirmed_placeholder（占位不能删号）", pending.Status)
	}

	// 关键：credential_ledger.status 必须仍是 alive · 号没被删
	var credStatus string
	if err := e.db.DB.QueryRow(
		`SELECT status FROM credential_ledger WHERE id = 'c1'`).Scan(&credStatus); err != nil {
		t.Fatal(err)
	}
	if credStatus != "alive" {
		t.Errorf("credential_ledger.status = %s, want alive（占位路径 confirm 绝不能标 handed_off · 会真删）", credStatus)
	}
}

// ③ 未 fulfill 就 confirm → 409
func TestHandoffConfirm_RequiresFulfillFirst(t *testing.T) {
	_, base, withKey, _ := setupHandoff(t)
	_, body := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1"}}, withKey)
	init := decode[map[string]string](t, body)
	token := init["download_token"]

	status, _ := base.do(t, "POST", "/api/me/handoff/"+token+"/confirm", nil, withKey)
	if status != http.StatusConflict {
		t.Errorf("未 fulfill 直接 confirm 该 409，得到 %d", status)
	}
}

// ③ 重复 confirm 幂等 · 返 ok
func TestHandoffConfirm_Idempotent(t *testing.T) {
	_, base, withKey, _ := setupHandoff(t)
	_, body := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1"}}, withKey)
	init := decode[map[string]string](t, body)
	token := init["download_token"]

	_, _ = base.do(t, "GET", "/api/me/handoff/"+token, nil, withKey)
	_, _ = base.do(t, "POST", "/api/me/handoff/"+token+"/confirm", nil, withKey)

	// 二次 confirm 该幂等静默返回 ok
	status, _ := base.do(t, "POST", "/api/me/handoff/"+token+"/confirm", nil, withKey)
	if status != http.StatusOK {
		t.Errorf("重复 confirm 该幂等 ok，得到 %d", status)
	}
}

// ① 非本人号 → 409（bad_assignment_plan）
func TestHandoffInit_RejectsNonOwned(t *testing.T) {
	_, base, withKey, _ := setupHandoff(t)
	status, _ := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1", "does-not-exist"}}, withKey)
	if status != http.StatusConflict {
		t.Errorf("非本人号 status = %d, want 409", status)
	}
}

// TTL 后 fulfill 应报 token_expired
func TestHandoffFulfill_TTLExpired(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	// 缩短 TTL 到 20ms
	e.handoffs = handoff.NewStore(e.db.DB, 20*time.Millisecond)
	// 重装 · 因为 handoffs 已经被 srv 引用，简单起见我们再造一个 server
	// 用底层 pending_handoff 表直接测：不通过 API
	key := seedWithAPIKey(t, base, "ttl@e.com", "ttluser", "password123")
	pid := passengerIDOf(t, base, "ttl@e.com")
	e.insertRound(t, "round-1")
	e.insertRecordCred(t, "c1", pid, "round-1", "alive", 1)
	_ = key

	// 用 store 直接发 token
	p, err := e.handoffs.IssueToken(context.Background(), handoff.IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1"},
	})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := e.handoffs.GetByToken(context.Background(), p.DownloadToken); err != handoff.ErrTokenExpired {
		t.Errorf("过期后应 ErrTokenExpired，得到 %v", err)
	}
}

// **P0 断言**：BP_STRICT_HANDOFF=1 时 fulfill 必须 501。
// 生产上线前把这个开关翻上·防降级路径把占位串漏进真实交付。
func TestHandoffFulfill_StrictModeRejectsPlaceholder(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "np@e.com", "nptester", "password123")
	pid := passengerIDOf(t, base, "np@e.com")
	e.insertRound(t, "round-1")
	e.insertRecordCred(t, "c1", pid, "round-1", "alive", 1)
	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", key) }

	t.Setenv("BP_STRICT_HANDOFF", "1")

	_, body := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1"}}, withKey)
	init := decode[map[string]string](t, body)
	token := init["download_token"]

	status, respBody := base.do(t, "GET", "/api/me/handoff/"+token, nil, withKey)
	if status != http.StatusNotImplemented {
		t.Fatalf("STRICT 模式 fulfill 必须 501，得到 %d body=%s", status, respBody)
	}
	if got := decode[Error](t, respBody); got.Code != "handoff_not_ready" {
		t.Errorf("code = %q，want handoff_not_ready", got.Code)
	}
}
