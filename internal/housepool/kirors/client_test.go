package kirors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
)

const testAdminKey = "test-admin-key"

// recorded 记下 mock 服务端收到的请求，用于断言路径 / header / body
type recorded struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   string
}

type mockPool struct {
	t   *testing.T
	srv *httptest.Server
	got []recorded
	// handler 每个测试自己塞
	handler func(w http.ResponseWriter, r *http.Request)
}

func newMock(t *testing.T, handler func(http.ResponseWriter, *http.Request)) (*mockPool, *Client) {
	t.Helper()
	m := &mockPool{t: t, handler: handler}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(strings.Builder)
		if r.Body != nil {
			_, _ = io_Copy(buf, r.Body)
		}
		m.got = append(m.got, recorded{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			Header: r.Header.Clone(), Body: buf.String(),
		})
		m.handler(w, r)
	}))
	t.Cleanup(m.srv.Close)

	hc, err := httpx.New(httpx.Config{Timeout: 5 * time.Second, MaxRetries: 0})
	if err != nil {
		t.Fatalf("建 httpx: %v", err)
	}
	c, err := New(Config{BaseURL: m.srv.URL, AdminKey: testAdminKey}, hc)
	if err != nil {
		t.Fatalf("建 client: %v", err)
	}
	return m, c
}

// io_Copy 避免为一行引 io（测试里只用这一处）
func io_Copy(dst *strings.Builder, src interface{ Read([]byte) (int, error) }) (int, error) {
	buf := make([]byte, 4096)
	total := 0
	for {
		n, err := src.Read(buf)
		if n > 0 {
			dst.Write(buf[:n])
			total += n
		}
		if err != nil {
			return total, nil
		}
	}
}

func (m *mockPool) last() recorded {
	m.t.Helper()
	if len(m.got) == 0 {
		m.t.Fatal("mock 没收到任何请求")
	}
	return m.got[len(m.got)-1]
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Fatalf("mock 写响应: %v", err)
		}
	}
}

// ── 底座：前缀 / 鉴权 ────────────────────────────────

// 所有请求都要打在 /api/admin 前缀下，并带 x-api-key
func TestRequestsUseAdminPrefixAndAuthHeader(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, wireGroupList{Total: 0, Groups: nil})
	})

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	got := m.last()
	if got.Path != "/api/admin/groups" {
		t.Errorf("路径 = %q，应带 /api/admin 前缀", got.Path)
	}
	if got.Header.Get("x-api-key") != testAdminKey {
		t.Errorf("缺 x-api-key header，实际 = %q", got.Header.Get("x-api-key"))
	}
}

// BaseURL 带尾斜杠或已含前缀时不能拼出双斜杠
func TestBaseURLNormalization(t *testing.T) {
	hc, _ := httpx.New(httpx.Config{Timeout: time.Second})
	c, err := New(Config{BaseURL: "https://pool.example.com/", AdminKey: "k"}, hc)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.urlFor("/groups", nil); got != "https://pool.example.com/api/admin/groups" {
		t.Errorf("urlFor = %q", got)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	hc, _ := httpx.New(httpx.Config{Timeout: time.Second})
	cases := []struct {
		name string
		cfg  Config
		hc   *httpx.Client
	}{
		{"缺 BaseURL", Config{AdminKey: "k"}, hc},
		{"缺 AdminKey", Config{BaseURL: "https://x"}, hc},
		{"缺 httpx", Config{BaseURL: "https://x", AdminKey: "k"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg, tc.hc); err == nil {
				t.Fatal("应该报错")
			}
		})
	}
}

// ── Credential ──────────────────────────────────────

func sampleCredentialList() wireCredentialList {
	email1, email2 := "a@kiro.tmp", "b@kiro.tmp"
	src := "kiro91"
	reason := housepool.ReasonManual
	created := "2026-08-01T10:00:00Z"
	return wireCredentialList{
		Total: 2, Available: 1, DisabledCount: 1, CoolingCount: 0,
		InFlightTotal: 3, RPMTotal: 42, TPMTotal: 1234,
		Credentials: []wireCredential{
			{
				ID: 101, Priority: 1, Disabled: false, Email: &email1,
				SourceChannel: &src, Groups: []string{"bus-b1"},
				SuccessCount: 10, Endpoint: "default", CreatedAt: &created,
			},
			{
				ID: 102, Priority: 2, Disabled: true, Email: &email2,
				DisabledReason: &reason, SourceChannel: &src,
				Groups: []string{"record-p1"}, Endpoint: "default",
			},
		},
	}
}

func TestListCredentials(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, sampleCredentialList())
	})

	// 默认不含 disabled
	creds, snap, err := c.ListCredentials(context.Background(), housepool.CredentialFilter{})
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("默认应过滤掉 disabled，得到 %d 条", len(creds))
	}
	if creds[0].ID != 101 || creds[0].Email != "a@kiro.tmp" {
		t.Errorf("解出来不对: %+v", creds[0])
	}
	if creds[0].SourceChannel != "kiro91" {
		t.Errorf("sourceChannel（camelCase）没解出来: %q", creds[0].SourceChannel)
	}
	if creds[0].CreatedAt.IsZero() {
		t.Error("createdAt 没解出来")
	}

	// 列表端点顺带给的池子快照（§10b ②）
	if snap == nil {
		t.Fatal("没返回 PoolSnapshot")
	}
	if snap.Total != 2 || snap.Available != 1 || snap.InFlightTotal != 3 || snap.TPMTotal != 1234 {
		t.Errorf("快照不对: %+v", *snap)
	}
}

func TestListCredentialsIncludeDisabledAndGroupFilter(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, sampleCredentialList())
	})
	ctx := context.Background()

	all, _, err := c.ListCredentials(ctx, housepool.CredentialFilter{IncludeDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("IncludeDisabled 应返回 2 条，得到 %d", len(all))
	}

	// 按 group 过滤
	byGroup, _, err := c.ListCredentials(ctx, housepool.CredentialFilter{
		Groups: []string{"record-p1"}, IncludeDisabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(byGroup) != 1 || byGroup[0].ID != 102 {
		t.Fatalf("group 过滤不对: %+v", byGroup)
	}

	// 不存在的 group → 空
	none, _, err := c.ListCredentials(ctx, housepool.CredentialFilter{
		Groups: []string{"bus-nope"}, IncludeDisabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("不存在的 group 应返回空，得到 %d 条", len(none))
	}
}

func TestGetCredential(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, sampleCredentialList())
	})
	ctx := context.Background()

	got, err := c.GetCredential(ctx, 102)
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if got.ID != 102 {
		t.Errorf("拿到 id=%d", got.ID)
	}
	if got.DisabledReason != housepool.ReasonManual {
		t.Errorf("disabledReason = %q", got.DisabledReason)
	}

	// 不存在 → ErrNotFound
	_, err = c.GetCredential(ctx, 999)
	if !errors.Is(err, housepool.ErrNotFound) {
		t.Fatalf("不存在的号应报 ErrNotFound，得到 %v", err)
	}
}

func TestSetDisabled(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, map[string]bool{"ok": true})
	})

	if err := c.SetDisabled(context.Background(), 101, true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}
	got := m.last()
	if got.Method != http.MethodPost || got.Path != "/api/admin/credentials/101/disabled" {
		t.Errorf("%s %s", got.Method, got.Path)
	}
	// body 只能有 disabled —— 传 reason 是我方一厢情愿，号池不收（§10b ④）
	var body map[string]any
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatalf("body 不是 JSON: %q", got.Body)
	}
	if len(body) != 1 {
		t.Errorf("body 应只有 disabled 一个字段，实际 %v", body)
	}
	if body["disabled"] != true {
		t.Errorf("disabled = %v", body["disabled"])
	}
}

func TestSetDisabledBatch(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, map[string]any{"succeeded": 2})
	})

	if err := c.SetDisabledBatch(context.Background(),
		[]housepool.CredentialID{101, 102}, true); err != nil {
		t.Fatalf("SetDisabledBatch: %v", err)
	}
	got := m.last()
	if got.Path != "/api/admin/credentials/batch/disabled" {
		t.Errorf("path = %q", got.Path)
	}
	if !strings.Contains(got.Body, "101") || !strings.Contains(got.Body, "102") {
		t.Errorf("body 里没带上 id: %q", got.Body)
	}

	// 空列表不该发请求
	before := len(m.got)
	if err := c.SetDisabledBatch(context.Background(), nil, true); err != nil {
		t.Fatal(err)
	}
	if len(m.got) != before {
		t.Error("空列表不该发请求")
	}
}

func TestDeleteCredential(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.DeleteCredential(context.Background(), 101); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	got := m.last()
	if got.Method != http.MethodDelete || got.Path != "/api/admin/credentials/101" {
		t.Errorf("%s %s", got.Method, got.Path)
	}
}

func TestDeleteCredentialBatch(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, map[string]any{"succeeded": 2})
	})
	if err := c.DeleteCredentialBatch(context.Background(),
		[]housepool.CredentialID{7, 8}); err != nil {
		t.Fatalf("DeleteCredentialBatch: %v", err)
	}
	if got := m.last(); got.Path != "/api/admin/credentials/batch/delete" {
		t.Errorf("path = %q", got.Path)
	}
}

func TestUpdateCredential(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, map[string]bool{"ok": true})
	})

	groups := []string{"bus-b2"}
	email := "new@kiro.tmp"
	if err := c.UpdateCredential(context.Background(), 101, housepool.CredentialPatch{
		Email: &email, Groups: &groups,
	}); err != nil {
		t.Fatalf("UpdateCredential: %v", err)
	}

	got := m.last()
	if got.Method != http.MethodPut || got.Path != "/api/admin/credentials/101" {
		t.Errorf("%s %s", got.Method, got.Path)
	}
	// 只该带传了的字段 —— omitempty 保证没传的不出现（号池那边 None = 不动）
	var body map[string]any
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 {
		t.Errorf("body 应只含 email + groups，实际 %v", body)
	}
	if _, ok := body["proxyUrl"]; ok {
		t.Error("没传的字段不该出现在 body 里")
	}
}

func TestGetBalance(t *testing.T) {
	title := "Pro"
	// housepool 后端 1.8.3 balance.nextResetAt 返 unix epoch number(不是 string)· json.Number 承接
	reset := json.Number("1788220800")
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, wireBalance{
			ID: 101, SubscriptionTitle: &title,
			CurrentUsage: 4700, UsageLimit: 10000, Remaining: 5300,
			UsagePercentage: 47, NextResetAt: &reset,
		})
	})

	b, err := c.GetBalance(context.Background(), 101)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if b.SubscriptionTitle != "Pro" || b.Remaining != 5300 {
		t.Errorf("解出来不对: %+v", *b)
	}
	if b.NextResetAt == nil {
		t.Error("nextResetAt 没解出来")
	}
}

func TestTestCredential(t *testing.T) {
	// 成功
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, map[string]bool{"ok": true})
	})
	if err := c.TestCredential(context.Background(), 101); err != nil {
		t.Fatalf("TestCredential 成功用例: %v", err)
	}
	if got := m.last(); got.Path != "/api/admin/credentials/101/test" {
		t.Errorf("path = %q", got.Path)
	}
}

// 探活失败要能被上层识别成"判死" —— deathwatch 靠这个（契约 §DisabledReason 判据）
func TestTestCredentialFailureIsError(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 502, wireError{Error: "upstream rejected"})
	})
	err := c.TestCredential(context.Background(), 101)
	if err == nil {
		t.Fatal("探活失败应返回错误（deathwatch 靠这个判死）")
	}
	if !errors.Is(err, housepool.ErrUnavailable) {
		t.Errorf("5xx 应归到 ErrUnavailable，得到 %v", err)
	}
}

func TestRefreshToken(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, map[string]bool{"ok": true})
	})
	if err := c.RefreshToken(context.Background(), 101); err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if got := m.last(); got.Path != "/api/admin/credentials/101/refresh" {
		t.Errorf("path = %q", got.Path)
	}
}

// ── Group ───────────────────────────────────────────

func TestListGroups(t *testing.T) {
	desc := "周末拼车局"
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, wireGroupList{
			Total: 1,
			Groups: []wireGroup{{
				Name: "bus-b1", Description: &desc, CreatedAt: "2026-08-01T10:00:00Z",
				CredentialCount: 5, ClientKeyCount: 1,
			}},
		})
	})

	groups, err := c.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("得到 %d 个 group", len(groups))
	}
	// 列表端点是包着的对象，要取 .groups（§10b ②）
	if groups[0].Name != "bus-b1" {
		t.Errorf("name = %q", groups[0].Name)
	}
	if groups[0].CredentialCount != 5 || groups[0].ClientKeyCount != 1 {
		t.Errorf("credentialCount/clientKeyCount 没解出来: %+v", groups[0])
	}
	if groups[0].CreatedAt.IsZero() {
		t.Error("createdAt 没解出来")
	}
}

func TestCreateGroup(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 201, wireGroup{Name: "bus-new", CreatedAt: "2026-08-08T00:00:00Z"})
	})

	g, err := c.CreateGroup(context.Background(), housepool.GroupRequest{Name: "bus-new"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if g.Name != "bus-new" {
		t.Errorf("name = %q", g.Name)
	}
	got := m.last()
	if got.Method != http.MethodPost || got.Path != "/api/admin/groups" {
		t.Errorf("%s %s", got.Method, got.Path)
	}
}

// 建 group 只返 204 时也要能拿到名字（用请求值兜）
func TestCreateGroupWithEmptyResponse(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	})
	g, err := c.CreateGroup(context.Background(), housepool.GroupRequest{Name: "bus-x"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if g.Name != "bus-x" {
		t.Errorf("空响应时应用请求里的名字兜住，得到 %q", g.Name)
	}
}

func TestUpdateGroupAndDelete(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, map[string]bool{"ok": true})
	})
	ctx := context.Background()

	// 改描述（不改名）→ body 不该有 newName
	if err := c.UpdateGroup(ctx, "bus-b1", housepool.GroupRequest{Description: "改了"}); err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	got := m.last()
	if got.Method != http.MethodPatch || got.Path != "/api/admin/groups/bus-b1" {
		t.Errorf("%s %s", got.Method, got.Path)
	}
	if strings.Contains(got.Body, "newName") {
		t.Errorf("没改名却传了 newName: %q", got.Body)
	}

	// 改名 → 要有 newName
	if err := c.UpdateGroup(ctx, "bus-b1", housepool.GroupRequest{Name: "bus-b2"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.last().Body, "newName") {
		t.Errorf("改名时应传 newName: %q", m.last().Body)
	}

	// 删
	if err := c.DeleteGroup(ctx, "bus-b1"); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	if got := m.last(); got.Method != http.MethodDelete {
		t.Errorf("method = %s", got.Method)
	}
}

// group 名含特殊字符时要转义，别拼出畸形 URL
func TestGroupNameIsEscaped(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, 200, map[string]bool{"ok": true})
	})
	if err := c.DeleteGroup(context.Background(), "bus-a b/c"); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	// httptest 会解码回来，路径里应该是完整的 group 名而不是被切断
	if got := m.last(); !strings.HasPrefix(got.Path, "/api/admin/groups/") {
		t.Errorf("path = %q", got.Path)
	}
}

// ── 错误分类 ────────────────────────────────────────

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusNotFound, housepool.ErrNotFound},
		{http.StatusUnauthorized, housepool.ErrUnauthorized},
		{http.StatusForbidden, housepool.ErrUnauthorized},
		{http.StatusConflict, housepool.ErrConflict},
		{http.StatusInternalServerError, housepool.ErrUnavailable},
		{http.StatusBadGateway, housepool.ErrUnavailable},
		{http.StatusServiceUnavailable, housepool.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d", tc.status), func(t *testing.T) {
			_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tc.status, wireError{Message: "boom"})
			})
			err := c.SetDisabled(context.Background(), 1, true)
			if !errors.Is(err, tc.want) {
				t.Fatalf("HTTP %d 应归到 %v，得到 %v", tc.status, tc.want, err)
			}
			// 错误里要带上操作名和状态码，便于日志排查
			var he *housepool.Error
			if !errors.As(err, &he) {
				t.Fatalf("应是 *housepool.Error，得到 %T", err)
			}
			if he.Op != "SetDisabled" || he.Status != tc.status {
				t.Errorf("Op=%q Status=%d", he.Op, he.Status)
			}
			if he.Message != "boom" {
				t.Errorf("没带上号池的错误文本: %q", he.Message)
			}
		})
	}
}

// 4xx（非 401/403/404/409）不该被当成"号池挂了" —— 那是我方请求的问题
func TestBadRequestIsNotUnavailable(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusBadRequest, wireError{Error: "bad id"})
	})
	err := c.SetDisabled(context.Background(), 1, true)
	if err == nil {
		t.Fatal("400 应该报错")
	}
	if errors.Is(err, housepool.ErrUnavailable) {
		t.Error("400 不该归到 ErrUnavailable（那会让上层以为号池挂了去重试）")
	}
}

func TestUnparsableResponseBody(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{not json`))
	})
	_, _, err := c.ListCredentials(context.Background(), housepool.CredentialFilter{})
	if err == nil {
		t.Fatal("响应体解不开应该报错")
	}
	// 错误里要带原文片段，否则排查时不知道号池到底返了什么
	if !strings.Contains(err.Error(), "not json") {
		t.Errorf("错误应含响应原文片段，得到 %v", err)
	}
}

// ── BatchImport（SSE） ──────────────────────────────

func TestBatchImportSSE(t *testing.T) {
	m, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)

		id1 := uint64(201)
		i0, i1, i2 := 0, 1, 2
		events := []wireBatchImportEvent{
			{Index: &i0, Status: "verified", CredentialID: &id1, Email: strPtr("a@k.tmp"), Usage: strPtr("0/1000")},
			{Index: &i1, Status: "duplicate", Email: strPtr("b@k.tmp")},
			{Index: &i2, Status: "failed", Error: strPtr("token invalid"), RolledBack: boolPtr(true)},
			{Status: "summary", Summary: &wireBatchImportSummary{
				Total: 3, Imported: 0, Verified: 1, Duplicate: 1, Failed: 1, RolledBack: 1,
			}},
		}
		for _, ev := range events {
			raw, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", raw)
			if fl != nil {
				fl.Flush()
			}
		}
	})

	res, err := c.BatchImport(context.Background(), housepool.BatchImportRequest{
		Credentials: []housepool.ImportCredential{
			{RefreshToken: "rt1", Groups: []string{"bus-b1"}, SourceChannel: "kiro91"},
		},
		Verify: true,
	})
	if err != nil {
		t.Fatalf("BatchImport: %v", err)
	}

	var got []housepool.BatchImportEvent
	for ev := range res.Events {
		got = append(got, ev)
	}
	sum := <-res.Summary

	if err := res.Err(); err != nil {
		t.Fatalf("流出错: %v", err)
	}

	// summary 不该出现在 Events 里（它被抽到 Summary 通道了）
	if len(got) != 3 {
		t.Fatalf("得到 %d 个事件，应为 3（summary 不算）", len(got))
	}
	if got[0].Status != housepool.ImportStatusVerified {
		t.Errorf("事件 0 status = %q", got[0].Status)
	}
	if got[0].CredentialID == nil || *got[0].CredentialID != 201 {
		t.Errorf("credentialId（camelCase）没解出来: %+v", got[0].CredentialID)
	}
	if got[2].Status != housepool.ImportStatusFailed || got[2].Error != "token invalid" {
		t.Errorf("失败事件不对: %+v", got[2])
	}
	if got[2].RolledBack == nil || !*got[2].RolledBack {
		t.Error("rolledBack 没解出来")
	}

	if sum.Total != 3 || sum.Verified != 1 || sum.Duplicate != 1 || sum.Failed != 1 {
		t.Errorf("summary 不对: %+v", sum)
	}

	// 请求形状
	rec := m.last()
	if rec.Path != "/api/admin/credentials/batch-import" {
		t.Errorf("path = %q", rec.Path)
	}
	if rec.Header.Get("Accept") != "text/event-stream" {
		t.Errorf("缺 Accept: text/event-stream")
	}
	if !strings.Contains(rec.Body, "refreshToken") {
		t.Errorf("body 应是 camelCase 的 refreshToken: %q", rec.Body)
	}
	if !strings.Contains(rec.Body, `"verify":true`) {
		t.Errorf("verify 应显式传: %q", rec.Body)
	}
}

// SSE 里夹杂心跳注释和畸形行时不能崩，也不能丢正常事件
func TestBatchImportToleratesNoiseInStream(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		id := uint64(301)
		ev, _ := json.Marshal(wireBatchImportEvent{Status: "verified", CredentialID: &id})
		sum, _ := json.Marshal(wireBatchImportEvent{
			Status: "summary", Summary: &wireBatchImportSummary{Total: 1, Verified: 1}})

		_, _ = fmt.Fprintf(w, ": heartbeat\n\n")  // 注释行
		_, _ = fmt.Fprintf(w, "event: message\n") // 我方不用的字段
		_, _ = fmt.Fprintf(w, "data: %s\n\n", ev)
		_, _ = fmt.Fprintf(w, "data: {broken json\n\n") // 畸形，应跳过
		_, _ = fmt.Fprintf(w, "data: \n\n")             // 空 data
		_, _ = fmt.Fprintf(w, "data: %s\n\n", sum)
	})

	res, err := c.BatchImport(context.Background(), housepool.BatchImportRequest{
		Credentials: []housepool.ImportCredential{{RefreshToken: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for range res.Events {
		n++
	}
	sum := <-res.Summary
	if n != 1 {
		t.Fatalf("应只解出 1 个正常事件，得到 %d", n)
	}
	if sum.Verified != 1 {
		t.Errorf("summary = %+v", sum)
	}
}

func TestBatchImportRejectsEmpty(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {})
	if _, err := c.BatchImport(context.Background(), housepool.BatchImportRequest{}); err == nil {
		t.Fatal("空凭证列表应该报错")
	}
}

func TestBatchImportErrorStatus(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnauthorized, wireError{Error: "bad admin key"})
	})
	_, err := c.BatchImport(context.Background(), housepool.BatchImportRequest{
		Credentials: []housepool.ImportCredential{{RefreshToken: "x"}},
	})
	if !errors.Is(err, housepool.ErrUnauthorized) {
		t.Fatalf("401 应报 ErrUnauthorized，得到 %v", err)
	}
}

// ── 1a 未实现的要明确报 ErrNotSupported，而不是静默返空 ──

func TestUnimplementedMethodsReturnNotSupported(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {})
	ctx := context.Background()

	checks := map[string]error{}
	_, err := c.ListClientKeys(ctx, housepool.ClientKeyFilter{})
	checks["ListClientKeys"] = err
	_, err = c.CreateClientKey(ctx, housepool.ClientKeyRequest{})
	checks["CreateClientKey"] = err
	_, err = c.StatsOverview(ctx)
	checks["StatsOverview"] = err
	_, err = c.StatsByCredential(ctx, housepool.StatsOptions{})
	checks["StatsByCredential"] = err
	_, err = c.GetConcurrency(ctx, 1)
	checks["GetConcurrency"] = err
	checks["DeleteClientKey"] = c.DeleteClientKey(ctx, 1)

	for name, err := range checks {
		if !errors.Is(err, housepool.ErrNotSupported) {
			t.Errorf("%s 应报 ErrNotSupported，得到 %v", name, err)
		}
	}
}

// ── group 名助手 ────────────────────────────────────

func TestGroupNameHelpers(t *testing.T) {
	if got := housepool.BusGroup("01H8"); got != "bus-01H8" {
		t.Errorf("BusGroup = %q", got)
	}
	if got := housepool.RecordGroup("p1"); got != "record-p1" {
		t.Errorf("RecordGroup = %q", got)
	}
	if housepool.MarketGroup != "market" {
		t.Errorf("MarketGroup = %q", housepool.MarketGroup)
	}
}

// ── DisabledReason 判据（deathwatch 依赖） ──────────

func TestDisabledReasonClassification(t *testing.T) {
	// Manual 是我方主动 disable 的（拉号记录待派 / handoff 待确认 / 成员挂起）
	// —— 绝不能判死，否则全部待派号会被误标
	if housepool.IsDeadReason(housepool.ReasonManual) {
		t.Error("Manual 不该判死（那是我方自己 disable 的）")
	}
	if housepool.NeedsProbe(housepool.ReasonManual) {
		t.Error("Manual 不需要复核")
	}

	for _, r := range []string{
		housepool.ReasonSuspended, housepool.ReasonQuotaExceeded, housepool.ReasonInvalidRefreshToken,
	} {
		if !housepool.IsDeadReason(r) {
			t.Errorf("%s 应直接判死", r)
		}
	}

	for _, r := range []string{
		housepool.ReasonTooManyFailures, housepool.ReasonTooManyRefreshFailures, housepool.ReasonAutoThrottled,
	} {
		if housepool.IsDeadReason(r) {
			t.Errorf("%s 不该直接判死（号池侧可能自愈）", r)
		}
		if !housepool.NeedsProbe(r) {
			t.Errorf("%s 应该走 TestCredential 复核", r)
		}
	}

	// InvalidConfig 是我方导入错了，既不判死也不复核 —— 该报警人工看
	if housepool.IsDeadReason(housepool.ReasonInvalidConfig) || housepool.NeedsProbe(housepool.ReasonInvalidConfig) {
		t.Error("InvalidConfig 既不判死也不复核（我方导入错了，报警人工处理）")
	}
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
