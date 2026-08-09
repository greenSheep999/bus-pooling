package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// 建单人车：只有 name + kind 需要
func TestCreateBusSingle(t *testing.T) {
	e, _, withKey := pullEnv(t, 10_000_000_000)

	status, body := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "我的号池", "kind": "single"}, withKey)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[map[string]json.RawMessage](t, body)
	for _, k := range []string{"id", "name", "kind", "status", "member_count", "invite_code", "created_at", "members"} {
		if _, ok := got[k]; !ok {
			t.Errorf("响应缺字段 %q", k)
		}
	}
	// 内部字段不出（§0.1）
	for _, k := range []string{"creator_passenger_id", "dissolved_at", "auto_refill_enabled"} {
		if _, leaked := got[k]; leaked {
			t.Errorf("响应泄漏内部字段 %q", k)
		}
	}
	// creator 自动成 owner
	var full busResponse
	if err := json.Unmarshal(body, &full); err != nil {
		t.Fatal(err)
	}
	if full.MemberCount != 1 {
		t.Errorf("member_count = %d，want 1", full.MemberCount)
	}
	if len(full.Members) != 1 || full.Members[0].Role != "owner" || full.Members[0].SharePct != 100 {
		t.Errorf("owner 成员不对: %+v", full.Members)
	}
	// 1c · 用户建的车一律带邀请码（1 人独享·车主随时可邀人变多人拼车）
	if full.InviteCode == nil || *full.InviteCode == "" {
		t.Error("用户建的车该带邀请码 · got=nil/空")
	}
}

// 1c · single / anon / team 都允许 · 只挡未知 kind
func TestCreateBusAcceptsAllValidKinds(t *testing.T) {
	e, _, withKey := pullEnv(t, 0)

	// team 允许 · 建成的车应带 invite_code
	status, body := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "friends", "kind": "team", "max_members": 3}, withKey)
	if status != http.StatusCreated {
		t.Fatalf("team 应 201 · got=%d body=%s", status, body)
	}
	teamBus := decode[busResponse](t, body)
	if teamBus.InviteCode == nil || *teamBus.InviteCode == "" {
		t.Errorf("team 车应返 invite_code · got=%v", teamBus.InviteCode)
	}

	// anon 允许
	status, body = e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "拼车", "kind": "anon", "max_members": 3}, withKey)
	if status != http.StatusCreated {
		t.Errorf("anon 应 201 · got=%d body=%s", status, body)
	}

	// garbage 拒绝
	status, _ = e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "x", "kind": "garbage"}, withKey)
	if status != http.StatusBadRequest {
		t.Errorf("未知 kind 应 400 · got=%d", status)
	}
}

// GET /api/me/buses 只返自己的活跃车
func TestListBusesScopedToPassenger(t *testing.T) {
	e, _, withKey := pullEnv(t, 0)

	// 建两辆
	for _, name := range []string{"车 A", "车 B"} {
		if s, b := e.do(t, "POST", "/api/me/buses",
			map[string]any{"name": name, "kind": "single"}, withKey); s != http.StatusCreated {
			t.Fatalf("建 %s status = %d, body = %s", name, s, b)
		}
	}

	status, body := e.do(t, "GET", "/api/me/buses", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	got := decode[struct {
		Items []busResponse `json:"items"`
		Total int           `json:"total"`
	}](t, body)
	if got.Total != 2 || len(got.Items) != 2 {
		t.Fatalf("列表 total=%d len=%d", got.Total, len(got.Items))
	}
}

// GET /api/me/buses/{id} · 非成员返 404（不泄漏车存在）
func TestGetBusNonMemberReturns404(t *testing.T) {
	e, _, withKey := pullEnv(t, 0)
	// 建一辆
	_, body := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "x", "kind": "single"}, withKey)
	var b busResponse
	_ = json.Unmarshal(body, &b)

	// 换一个乘客
	key2 := seedWithAPIKey(t, e, "other@e.com", "other", "password123")
	withOther := func(r *http.Request) { r.Header.Set("X-API-Key", key2) }

	status, _ := e.do(t, "GET", "/api/me/buses/"+b.ID, nil, withOther)
	if status != http.StatusNotFound {
		t.Errorf("非成员应返 404，得到 %d", status)
	}
}

// owner 退车应被拒（单人车必须走 dissolve）
func TestOwnerCannotLeaveBusEndpoint(t *testing.T) {
	e, _, withKey := pullEnv(t, 0)
	_, body := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "x", "kind": "single"}, withKey)
	var b busResponse
	_ = json.Unmarshal(body, &b)

	status, resp := e.do(t, "POST", "/api/me/buses/"+b.ID+"/leave", nil, withKey)
	if status != http.StatusConflict {
		t.Errorf("owner 退车 status = %d, body = %s", status, resp)
	}
}

// DELETE /api/me/buses/{id} 解散 · 之后列表看不到
func TestDissolveBusEndpoint(t *testing.T) {
	e, _, withKey := pullEnv(t, 0)
	_, body := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "x", "kind": "single"}, withKey)
	var b busResponse
	_ = json.Unmarshal(body, &b)

	if s, _ := e.do(t, "DELETE", "/api/me/buses/"+b.ID, nil, withKey); s != http.StatusOK {
		t.Fatalf("解散 status = %d", s)
	}
	// 解散后列表不返
	_, listBody := e.do(t, "GET", "/api/me/buses", nil, withKey)
	got := decode[struct {
		Items []busResponse `json:"items"`
	}](t, listBody)
	if len(got.Items) != 0 {
		t.Errorf("解散后列表应为空，得到 %d 条", len(got.Items))
	}
}

// **Iss #10 DoD**：建 1 人 bus → 拉 5 个号 → bus 详情能看到（1a 阶段先看凭证 id 数量）
func TestBusPullEndToEnd(t *testing.T) {
	e, _, withKey := pullEnv(t, 10_000_000_000)

	// 建车
	_, cBody := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "拉号入车", "kind": "single"}, withKey)
	var b busResponse
	_ = json.Unmarshal(cBody, &b)
	if b.ID == "" {
		t.Fatal("建车没返 id")
	}

	// 拉 5 个号进车
	status, pullBody := e.do(t, "POST", "/api/me/buses/"+b.ID+"/pull",
		map[string]any{"count": 5, "zone": "us"}, withKey,
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", "bbccddee11223344bbccddee11223344") })
	if status != http.StatusOK {
		t.Fatalf("拉号 status = %d, body = %s", status, pullBody)
	}
	got := decode[pullResponse](t, pullBody)
	if got.Purchased != 5 {
		t.Fatalf("Purchased = %d, want 5", got.Purchased)
	}
	if len(got.CredentialIDs) != 5 {
		t.Fatalf("credential_ids = %d, want 5", len(got.CredentialIDs))
	}
	// 响应体只 8 字段（跟 /me/pull 一致 · §0.1）
	rawMap := decode[map[string]json.RawMessage](t, pullBody)
	banned := []string{"key_cost", "vendor_fee", "single_pull_fee", "channel_fee", "kiro_rs_id"}
	for _, k := range banned {
		if _, leaked := rawMap[k]; leaked {
			t.Errorf("bus 拉号响应泄漏内部字段 %q", k)
		}
	}

	// 从 credential_ledger 查这辆车下的号数 · 走库直查（credentials 端点是 Iss #11 才做）
	var n int
	if err := e.db.QueryRowContext(context.Background(),
		`SELECT count(1) FROM credential_ledger WHERE owner_bus_id = ?`, b.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("credential_ledger 里 owner_bus_id=%s 的号数 = %d，want 5", b.ID, n)
	}
}

// 拉号到别人车里应被拒（防越权）
func TestBusPullRejectsCrossPassenger(t *testing.T) {
	e, _, withKey := pullEnv(t, 10_000_000_000)
	// 建 A 的车
	_, body := e.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "A 的", "kind": "single"}, withKey)
	var b busResponse
	_ = json.Unmarshal(body, &b)

	// B 尝试拉到 A 的车里
	key2 := seedWithAPIKey(t, e, "b2@e.com", "bobby", "password123")
	status, _ := e.do(t, "POST", "/api/me/buses/"+b.ID+"/pull",
		map[string]any{"count": 1, "zone": "us"},
		func(r *http.Request) { r.Header.Set("X-API-Key", key2) },
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", "aa000000000000000000000000000001") })
	if status != http.StatusNotFound {
		t.Errorf("越权拉号应返 404，得到 %d", status)
	}
}
