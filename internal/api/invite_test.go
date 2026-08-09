package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// 1c · POST /api/me/buses/join-by-invite · 用邀请码加入 team bus
func TestJoinByInvite_HappyPath(t *testing.T) {
	env := newEnv(t)
	// A 建 team 车
	keyA := seedWithAPIKey(t, env, "a@e.local", "aa", "password123")
	statusA, bodyA := env.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "friends", "kind": "team", "max_members": 3},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyA) })
	if statusA != http.StatusCreated {
		t.Fatalf("A 建车 status=%d body=%s", statusA, bodyA)
	}
	var teamBus busResponse
	_ = json.Unmarshal(bodyA, &teamBus)
	if teamBus.InviteCode == nil || *teamBus.InviteCode == "" {
		t.Fatal("team 车没返 invite_code")
	}

	// B 用邀请码加入
	keyB := seedWithAPIKey(t, env, "b@e.local", "bb", "password123")
	status, body := env.do(t, "POST", "/api/me/buses/join-by-invite",
		map[string]any{"invite_code": *teamBus.InviteCode},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyB) })
	if status != http.StatusOK {
		t.Fatalf("加入 status=%d body=%s", status, body)
	}
	var joined busResponse
	_ = json.Unmarshal(body, &joined)
	if joined.ID != teamBus.ID {
		t.Errorf("加入的车 id 不对 · got=%q · want=%q", joined.ID, teamBus.ID)
	}
	if joined.MemberCount != 2 {
		t.Errorf("成员数 = %d · want=2", joined.MemberCount)
	}
}

// 无效邀请码 → 404
func TestJoinByInvite_InvalidCode(t *testing.T) {
	env := newEnv(t)
	key := seedWithAPIKey(t, env, "x@e.local", "xx", "password123")
	status, _ := env.do(t, "POST", "/api/me/buses/join-by-invite",
		map[string]any{"invite_code": "NOTEXIST"},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	if status != http.StatusNotFound {
		t.Errorf("无效邀请码应 404 · got=%d", status)
	}
}

// 空邀请码 → 400
func TestJoinByInvite_EmptyCode(t *testing.T) {
	env := newEnv(t)
	key := seedWithAPIKey(t, env, "x@e.local", "xx", "password123")
	status, _ := env.do(t, "POST", "/api/me/buses/join-by-invite",
		map[string]any{"invite_code": ""},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	if status != http.StatusBadRequest {
		t.Errorf("空邀请码应 400 · got=%d", status)
	}
}

// 幂等：同一乘客用同一邀请码重复加入·返 200 + 车现状（不算错）
func TestJoinByInvite_AlreadyMemberIdempotent(t *testing.T) {
	env := newEnv(t)
	keyA := seedWithAPIKey(t, env, "a@e.local", "aa", "password123")
	_, body := env.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "friends", "kind": "team", "max_members": 3},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyA) })
	var team busResponse
	_ = json.Unmarshal(body, &team)

	keyB := seedWithAPIKey(t, env, "b@e.local", "bb", "password123")
	// 第一次加入
	env.do(t, "POST", "/api/me/buses/join-by-invite",
		map[string]any{"invite_code": *team.InviteCode},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyB) })
	// 再来一次 · 应幂等
	status, _ := env.do(t, "POST", "/api/me/buses/join-by-invite",
		map[string]any{"invite_code": *team.InviteCode},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyB) })
	if status != http.StatusOK {
		t.Errorf("重复加入应幂等 200 · got=%d", status)
	}
}

// 1c · owner 换邀请码 · 旧码失效
func TestRegenInviteCode_HappyPath(t *testing.T) {
	env := newEnv(t)
	keyA := seedWithAPIKey(t, env, "a@e.local", "aa", "password123")
	_, body := env.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "rotate", "kind": "team", "max_members": 5},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyA) })
	var b busResponse
	_ = json.Unmarshal(body, &b)
	oldCode := *b.InviteCode

	// A 换码
	status, respBody := env.do(t, "POST", "/api/me/buses/"+b.ID+"/invite-code", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", keyA) })
	if status != http.StatusOK {
		t.Fatalf("换码 status=%d body=%s", status, respBody)
	}
	var out struct {
		InviteCode string `json:"invite_code"`
	}
	_ = json.Unmarshal(respBody, &out)
	if out.InviteCode == "" || out.InviteCode == oldCode {
		t.Errorf("换码后返值异常 · new=%q · old=%q", out.InviteCode, oldCode)
	}

	// 旧码不能再加入
	keyB := seedWithAPIKey(t, env, "b@e.local", "bb", "password123")
	statusB, _ := env.do(t, "POST", "/api/me/buses/join-by-invite",
		map[string]any{"invite_code": oldCode},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyB) })
	if statusB != http.StatusNotFound {
		t.Errorf("旧码换掉后应 404 · got=%d", statusB)
	}
	// 新码可用
	statusB2, _ := env.do(t, "POST", "/api/me/buses/join-by-invite",
		map[string]any{"invite_code": out.InviteCode},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyB) })
	if statusB2 != http.StatusOK {
		t.Errorf("新码应可加入 · got=%d", statusB2)
	}
}

// 非 owner 换邀请码 → 403
func TestRegenInviteCode_NonOwner403(t *testing.T) {
	env := newEnv(t)
	keyA := seedWithAPIKey(t, env, "a@e.local", "aa", "password123")
	_, body := env.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "guard", "kind": "team", "max_members": 5},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyA) })
	var b busResponse
	_ = json.Unmarshal(body, &b)

	// B 尝试换（B 都不是成员）
	keyB := seedWithAPIKey(t, env, "b@e.local", "bb", "password123")
	status, _ := env.do(t, "POST", "/api/me/buses/"+b.ID+"/invite-code", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", keyB) })
	if status != http.StatusForbidden {
		t.Errorf("非 owner 换码应 403 · got=%d", status)
	}
}

// 用户建的车（single/team）都能换码 · 只有系统 anon 撮合池不能
func TestRegenInviteCode_UserBusAllowedAnonRejected(t *testing.T) {
	env := newEnv(t)
	key := seedWithAPIKey(t, env, "a@e.local", "aa", "password123")

	// single 车能换（single 跟 team 行为一致）
	_, body := env.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "solo", "kind": "single"},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	var solo busResponse
	_ = json.Unmarshal(body, &solo)
	if solo.InviteCode == nil || *solo.InviteCode == "" {
		t.Error("single 车该带邀请码")
	}
	status, respBody := env.do(t, "POST", "/api/me/buses/"+solo.ID+"/invite-code", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	if status != http.StatusOK {
		t.Errorf("single 车换码应 200 · got=%d body=%s", status, respBody)
	}

	// anon 撮合池没码可换 → 400
	_, body = env.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "share", "kind": "anon"},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	var anon busResponse
	_ = json.Unmarshal(body, &anon)
	status, _ = env.do(t, "POST", "/api/me/buses/"+anon.ID+"/invite-code", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	if status != http.StatusBadRequest {
		t.Errorf("anon 车换码应 400 · got=%d", status)
	}
}

// 用邀请码加入 single 车 · 1 人独享变多人拼车（不是错误路径）
func TestJoinByInvite_SingleBusBecomesMulti(t *testing.T) {
	env := newEnv(t)
	keyA := seedWithAPIKey(t, env, "a@e.local", "aa", "password123")
	_, body := env.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "我的车", "kind": "single"},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyA) })
	var mine busResponse
	_ = json.Unmarshal(body, &mine)
	if mine.InviteCode == nil {
		t.Fatal("single 车没返邀请码")
	}

	keyB := seedWithAPIKey(t, env, "b@e.local", "bb", "password123")
	status, joinBody := env.do(t, "POST", "/api/me/buses/join-by-invite",
		map[string]any{"invite_code": *mine.InviteCode},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyB) })
	if status != http.StatusOK {
		t.Fatalf("single 车应能用邀请码加入 · got=%d body=%s", status, joinBody)
	}
	var joined busResponse
	_ = json.Unmarshal(joinBody, &joined)
	if joined.MemberCount != 2 {
		t.Errorf("加入后成员数 = %d · want=2", joined.MemberCount)
	}
}
