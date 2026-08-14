package passengerpool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/downstream"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
)

// mockDownstreams 实现 DownstreamStore 让 pusher_test 不依赖 SQLite。
type mockDownstreams struct {
	cfg     downstream.Config
	getErr  error
	decrypt func([]byte) (string, error)
}

func (m *mockDownstreams) Get(_ context.Context, _ string) (downstream.Config, error) {
	return m.cfg, m.getErr
}

func (m *mockDownstreams) DecryptPassengerpoolToken(b []byte) (string, error) {
	if m.decrypt != nil {
		return m.decrypt(b)
	}
	return string(b), nil // mock 直接返 blob 的字节字符串
}

// mockPlaintext 让 pusher 拿到指定明文(测试真明文分支)。
type mockPlaintext struct {
	fetch func(ids []string) (map[string]PushCredential, error)
}

func (m *mockPlaintext) FetchPlaintext(_ context.Context, ids []string) (map[string]PushCredential, error) {
	if m.fetch != nil {
		return m.fetch(ids)
	}
	out := make(map[string]PushCredential, len(ids))
	for _, id := range ids {
		out[id] = PushCredential{CredentialID: id, RefreshToken: "rt-" + id}
	}
	return out, nil
}

// httpxClient 建一个走短超时的 httpx 实例(测试用)。
func httpxClient(t *testing.T) *httpx.Client {
	t.Helper()
	hc, err := httpx.New(httpx.Config{Timeout: 3 * time.Second, MaxRetries: 0})
	if err != nil {
		t.Fatalf("httpx: %v", err)
	}
	return hc
}

// serveOK 建 httptest.Server 让所有号都 verified · 附带断言 body 里明文有出现。
func serveOK(t *testing.T, wantRT string) (*httptest.Server, *int) {
	t.Helper()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		if wantRT != "" && !containsStr(string(body), wantRT) {
			t.Errorf("body 应含 refresh_token %q: %s", wantRT, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		i0 := 0
		ev, _ := json.Marshal(map[string]any{"status": "verified", "index": i0})
		fmt.Fprintf(w, "data: %s\n\n", ev)
		if fl != nil {
			fl.Flush()
		}
		sum, _ := json.Marshal(map[string]any{
			"status": "summary",
			"summary": map[string]int{
				"total": 1, "verified": 1,
			},
		})
		fmt.Fprintf(w, "data: %s\n\n", sum)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── 成功路径：真明文 · verified ────────────────────────

func TestPushSuccess(t *testing.T) {
	srv, hits := serveOK(t, "rt-cred-1")
	deps := PusherDeps{
		Downstreams: &mockDownstreams{
			cfg: downstream.Config{
				PassengerpoolURL:             srv.URL,
				PassengerpoolTokenEncrypted:  []byte("admin-token-123"),
				PassengerpoolTokenConfigured: true,
			},
		},
		Plaintext: &mockPlaintext{},
		HTTPX:     httpxClient(t),
	}
	p := NewPusher(deps)
	res, err := p.Push(context.Background(), "pid-1", []PushCredential{
		{CredentialID: "cred-1"},
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(res.Success) != 1 || res.Success[0] != "cred-1" {
		t.Errorf("Success = %+v", res.Success)
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed = %+v", res.Failed)
	}
	if *hits != 1 {
		t.Errorf("对家应收到 1 次请求 · 实际 %d", *hits)
	}
}

// ── duplicate 视为成功 ──────────────────────────────────

func TestPushDuplicateIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		i0 := 0
		ev, _ := json.Marshal(map[string]any{"status": "duplicate", "index": i0})
		fmt.Fprintf(w, "data: %s\n\n", ev)
		if fl != nil {
			fl.Flush()
		}
		sum, _ := json.Marshal(map[string]any{
			"status":  "summary",
			"summary": map[string]int{"total": 1, "duplicate": 1},
		})
		fmt.Fprintf(w, "data: %s\n\n", sum)
	}))
	defer srv.Close()

	deps := PusherDeps{
		Downstreams: &mockDownstreams{
			cfg: downstream.Config{
				PassengerpoolURL:             srv.URL,
				PassengerpoolTokenEncrypted:  []byte("k"),
				PassengerpoolTokenConfigured: true,
			},
		},
		Plaintext: &mockPlaintext{},
		HTTPX:     httpxClient(t),
	}
	res, err := NewPusher(deps).Push(context.Background(), "p", []PushCredential{{CredentialID: "c"}})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(res.Duplicate) != 1 {
		t.Errorf("Duplicate 应 1 · 得到 %+v", res.Duplicate)
	}
	if len(res.Failed) != 0 {
		t.Errorf("Failed 应 0 · 得到 %+v", res.Failed)
	}
}

// ── 401 → 每号 Failed · Retriable=false ──────────────────

func TestPush401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad token"}`))
	}))
	defer srv.Close()

	deps := PusherDeps{
		Downstreams: &mockDownstreams{
			cfg: downstream.Config{
				PassengerpoolURL:             srv.URL,
				PassengerpoolTokenEncrypted:  []byte("k"),
				PassengerpoolTokenConfigured: true,
			},
		},
		Plaintext: &mockPlaintext{},
		HTTPX:     httpxClient(t),
	}
	res, err := NewPusher(deps).Push(context.Background(), "p", []PushCredential{
		{CredentialID: "c1"}, {CredentialID: "c2"},
	})
	if err != nil {
		t.Fatalf("top-level err 应 nil(HTTP 错走 PushResult.Failed): %v", err)
	}
	if len(res.Failed) != 2 {
		t.Fatalf("Failed 应 2 · 得到 %d", len(res.Failed))
	}
	for _, f := range res.Failed {
		if f.Err.Kind != ErrKindUnauthorized {
			t.Errorf("Kind = %q · 应是 unauthorized", f.Err.Kind)
		}
		if f.Err.Retriable() {
			t.Error("401 不可重试")
		}
	}
}

// ── 5xx → Retriable=true(timeout) ────────────────────────

func TestPush5xxRetriable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	deps := PusherDeps{
		Downstreams: &mockDownstreams{
			cfg: downstream.Config{
				PassengerpoolURL:             srv.URL,
				PassengerpoolTokenEncrypted:  []byte("k"),
				PassengerpoolTokenConfigured: true,
			},
		},
		Plaintext: &mockPlaintext{},
		HTTPX:     httpxClient(t),
	}
	res, err := NewPusher(deps).Push(context.Background(), "p", []PushCredential{{CredentialID: "c"}})
	if err != nil {
		t.Fatalf("top-level err 应 nil: %v", err)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("Failed = %d", len(res.Failed))
	}
	if !res.Failed[0].Err.Retriable() {
		t.Error("5xx 应可重试")
	}
	if res.Failed[0].Err.Kind != ErrKindTimeout {
		t.Errorf("Kind = %q", res.Failed[0].Err.Kind)
	}
}

// ── 未配 passengerpool_url → ErrNoTarget ─────────────────

func TestPushDryRunNoURL(t *testing.T) {
	deps := PusherDeps{
		Downstreams: &mockDownstreams{
			cfg: downstream.Config{},
		},
		HTTPX: httpxClient(t),
	}
	_, err := NewPusher(deps).Push(context.Background(), "p", []PushCredential{{CredentialID: "c"}})
	if !errors.Is(err, ErrNoTarget) {
		t.Errorf("应报 ErrNoTarget · 得到 %v", err)
	}
}

// ── 未配 token → ErrNoTarget ────────────────────────────

func TestPushDryRunNoToken(t *testing.T) {
	deps := PusherDeps{
		Downstreams: &mockDownstreams{
			cfg: downstream.Config{
				PassengerpoolURL: "https://mock.example.com",
			},
		},
		HTTPX: httpxClient(t),
	}
	_, err := NewPusher(deps).Push(context.Background(), "p", []PushCredential{{CredentialID: "c"}})
	if !errors.Is(err, ErrNoTarget) {
		t.Errorf("应报 ErrNoTarget · 得到 %v", err)
	}
}

// ── SSE 断流 · 未返事件的号 stream_broken ─────────────────

func TestPushSSEIncomplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		i0 := 0
		ev, _ := json.Marshal(map[string]any{"status": "verified", "index": i0})
		fmt.Fprintf(w, "data: %s\n\n", ev)
		if fl != nil {
			fl.Flush()
		}
		// 不发第二条 · 不发 summary · 让 client 消完流后返 res 里只有 1 条
	}))
	defer srv.Close()

	deps := PusherDeps{
		Downstreams: &mockDownstreams{
			cfg: downstream.Config{
				PassengerpoolURL:             srv.URL,
				PassengerpoolTokenEncrypted:  []byte("k"),
				PassengerpoolTokenConfigured: true,
			},
		},
		Plaintext: &mockPlaintext{},
		HTTPX:     httpxClient(t),
	}
	res, err := NewPusher(deps).Push(context.Background(), "p", []PushCredential{
		{CredentialID: "c1"}, {CredentialID: "c2"},
	})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(res.Success) != 1 || res.Success[0] != "c1" {
		t.Errorf("Success = %+v", res.Success)
	}
	if len(res.Failed) != 1 || res.Failed[0].CredentialID != "c2" {
		t.Errorf("Failed = %+v", res.Failed)
	}
	if res.Failed[0].Err.Kind != ErrKindStreamBroken {
		t.Errorf("Kind = %q · 应是 stream_broken", res.Failed[0].Err.Kind)
	}
	if !res.Failed[0].Err.Retriable() {
		t.Error("stream_broken 应可重试")
	}
}

// ── placeholder 兜底 · Plaintext nil 时走 PLACEHOLDER ─────

func TestPushPlaceholderPlaintext(t *testing.T) {
	var seenBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		seenBody = string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		i0 := 0
		ev, _ := json.Marshal(map[string]any{"status": "verified", "index": i0})
		fmt.Fprintf(w, "data: %s\n\n", ev)
		if fl != nil {
			fl.Flush()
		}
	}))
	defer srv.Close()

	deps := PusherDeps{
		Downstreams: &mockDownstreams{
			cfg: downstream.Config{
				PassengerpoolURL:             srv.URL,
				PassengerpoolTokenEncrypted:  []byte("k"),
				PassengerpoolTokenConfigured: true,
			},
		},
		Plaintext: nil, // 明文缺口 · 走 placeholder
		HTTPX:     httpxClient(t),
	}
	res, err := NewPusher(deps).Push(context.Background(), "p", []PushCredential{{CredentialID: "cred-x"}})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !containsStr(seenBody, "PLACEHOLDER") {
		t.Errorf("body 应含 PLACEHOLDER · 得到 %s", seenBody)
	}
	if len(res.Success) != 1 {
		t.Errorf("Success = %+v", res.Success)
	}
}
