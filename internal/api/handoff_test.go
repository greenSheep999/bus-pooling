package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/delivery/handoff"
)

// setupHandoff 建 env + 两号 · 返回 env + 密钥 fn + pid + 两个号 id
func setupHandoff(t *testing.T) (*prEnv, *testEnv, func(*http.Request), string) {
	t.Helper()
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

// ② GET /api/me/handoff/{token} → 返明文 · 状态推进 fulfilled
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
	if got.Keys[0]["key"] == "" {
		t.Errorf("key 不能空")
	}

	// 状态推 fulfilled
	pending, _ := e.handoffs.GetByToken(context.Background(), token)
	if pending.Status != handoff.StatusFulfilled {
		t.Errorf("status = %s, want fulfilled", pending.Status)
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

// ③ POST /api/me/handoff/{token}/confirm → status=completed · credential_ledger 标 handed_off
func TestHandoffConfirm_MarksHandedOff(t *testing.T) {
	e, base, withKey, _ := setupHandoff(t)

	_, body := base.do(t, "POST", "/api/me/handoff",
		map[string]any{"credential_ids": []string{"c1"}}, withKey)
	init := decode[map[string]string](t, body)
	token := init["download_token"]

	// 先 fulfill 才能 confirm
	if s, _ := base.do(t, "GET", "/api/me/handoff/"+token, nil, withKey); s != http.StatusOK {
		t.Fatalf("fulfill 失败 status=%d", s)
	}

	status, _ := base.do(t, "POST", "/api/me/handoff/"+token+"/confirm", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("confirm status = %d", status)
	}

	// pending_handoff 标 completed
	pending, _ := e.handoffs.GetByToken(context.Background(), token)
	if pending.Status != handoff.StatusCompleted {
		t.Errorf("status = %s, want completed", pending.Status)
	}

	// credential_ledger 标 handed_off
	var credStatus string
	if err := e.db.DB.QueryRow(
		`SELECT status FROM credential_ledger WHERE id = 'c1'`).Scan(&credStatus); err != nil {
		t.Fatal(err)
	}
	if credStatus != "handed_off" {
		t.Errorf("credential_ledger.status = %s, want handed_off", credStatus)
	}

	// handoff 后号从 pull-records 视图消失
	_, listBody := base.do(t, "GET", "/api/me/pull-records", nil, withKey)
	list := decode[struct {
		Total int `json:"total"`
	}](t, listBody)
	// c2 还在（本用例只 handoff c1）
	if list.Total != 1 {
		t.Errorf("handoff 后 record 视图 total = %d, want 1（c2 未 handoff）", list.Total)
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
