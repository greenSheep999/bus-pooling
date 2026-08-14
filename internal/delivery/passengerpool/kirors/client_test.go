package kirors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/httpx"
)

// newMock 建 httptest.Server + Client(带 httpx)· 让上层用起来跟真调对家一样。
func newMock(t *testing.T, h http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	hc, err := httpx.New(httpx.Config{
		Timeout:       3 * time.Second,
		MaxRetries:    0, // SSE 不走 Do 的重试路径 · 但显式 0 让归错分类明确
		RetryBaseWait: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("httpx: %v", err)
	}
	c, err := New(Config{BaseURL: srv.URL, AdminKey: "test-admin-key-xxx"}, hc)
	if err != nil {
		t.Fatalf("kirors: %v", err)
	}
	return srv, c
}

// writeSSE 帮 handler 简化 SSE 输出。
func writeSSE(t *testing.T, w http.ResponseWriter, events ...wireBatchImportEvent) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	fl, _ := w.(http.Flusher)
	for _, ev := range events {
		raw, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", raw)
		if fl != nil {
			fl.Flush()
		}
	}
}

func strPtr(s string) *string { return &s }

// ── 成功路径 ─────────────────────────────────────────

func TestBatchImportSSESuccess(t *testing.T) {
	i0, i1, i2 := 0, 1, 2
	id1 := uint64(101)
	srv, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		// 校验请求 header + body(refresh_token 塞进去了)
		if r.Header.Get("x-api-key") != "test-admin-key-xxx" {
			t.Errorf("缺 x-api-key: %v", r.Header)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("缺 Accept: text/event-stream: %v", r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "refreshToken") {
			t.Errorf("body 应含 refreshToken(camelCase): %s", body)
		}
		if !strings.Contains(string(body), `"verify":true`) {
			t.Errorf("verify 应显式 true: %s", body)
		}
		writeSSE(t, w,
			wireBatchImportEvent{Index: &i0, Status: "verified", CredentialID: &id1, Email: strPtr("a@k.tmp")},
			wireBatchImportEvent{Index: &i1, Status: "duplicate", Email: strPtr("b@k.tmp")},
			wireBatchImportEvent{Index: &i2, Status: "failed", Error: strPtr("token invalid")},
			wireBatchImportEvent{Status: "summary", Summary: &wireBatchImportSummary{
				Total: 3, Verified: 1, Duplicate: 1, Failed: 1, RolledBack: 1, Imported: 0,
			}},
		)
	})
	_ = srv
	defer c.Close()

	res, err := c.BatchImport(context.Background(), []ImportInput{
		{CredentialID: "cred-1", RefreshToken: "rt1", Groups: []string{"push"}, SourceChannel: "vendor-a"},
		{CredentialID: "cred-2", RefreshToken: "rt2"},
		{CredentialID: "cred-3", RefreshToken: "rt3-bad"},
	})
	if err != nil {
		t.Fatalf("BatchImport: %v", err)
	}
	if len(res.PerIndex) != 3 {
		t.Fatalf("PerIndex 应 3 条 · 得到 %d", len(res.PerIndex))
	}
	if res.PerIndex[0].Status != "verified" || res.PerIndex[0].Index != 0 {
		t.Errorf("事件 0 = %+v", res.PerIndex[0])
	}
	if res.PerIndex[1].Status != "duplicate" || res.PerIndex[1].Index != 1 {
		t.Errorf("事件 1 = %+v", res.PerIndex[1])
	}
	if res.PerIndex[2].Status != "failed" || res.PerIndex[2].Error != "token invalid" {
		t.Errorf("事件 2 = %+v", res.PerIndex[2])
	}
	if res.Summary.Total != 3 || res.Summary.Verified != 1 || res.Summary.Duplicate != 1 || res.Summary.Failed != 1 {
		t.Errorf("summary = %+v", res.Summary)
	}
}

// ── 归错分类：401 → unauthorized 不可重试 ───────────────

func TestBatchImportUnauthorized(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad admin key"}`))
	})
	defer c.Close()

	_, err := c.BatchImport(context.Background(), []ImportInput{{CredentialID: "c", RefreshToken: "rt"}})
	if err == nil {
		t.Fatal("401 应报错")
	}
	var se *StreamError
	if !errors.As(err, &se) {
		t.Fatalf("应是 StreamError · 得到 %T: %v", err, err)
	}
	if se.Kind != KindUnauthorized {
		t.Errorf("Kind = %q · 应是 unauthorized", se.Kind)
	}
	if se.Status != http.StatusUnauthorized {
		t.Errorf("Status = %d", se.Status)
	}
}

// ── 归错分类：5xx → timeout(可重试) ───────────────────

func TestBatchImportServerError(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server crashed"}`))
	})
	defer c.Close()

	_, err := c.BatchImport(context.Background(), []ImportInput{{CredentialID: "c", RefreshToken: "rt"}})
	if err == nil {
		t.Fatal("5xx 应报错")
	}
	var se *StreamError
	if !errors.As(err, &se) || se.Kind != KindTimeout {
		t.Errorf("Kind = %q · 应是 timeout(可重试)", se.Kind)
	}
}

// ── 归错分类：404 → not_found 不可重试 ─────────────────

func TestBatchImportNotFound(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer c.Close()

	_, err := c.BatchImport(context.Background(), []ImportInput{{CredentialID: "c", RefreshToken: "rt"}})
	if err == nil {
		t.Fatal("404 应报错")
	}
	var se *StreamError
	if !errors.As(err, &se) || se.Kind != KindNotFound {
		t.Errorf("Kind = %q · 应是 not_found", se.Kind)
	}
}

// ── SSE 断流 → stream_broken 可重试 ─────────────────

func TestBatchImportSSEBroken(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		// 只发一半 · 客户端断流
		fmt.Fprintf(w, "data: {\"status\":\"verified\",\"index\":0}\n\n")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		// 不发 summary · 让上层判"事件缺失"
	})
	defer c.Close()

	res, err := c.BatchImport(context.Background(), []ImportInput{
		{CredentialID: "cred-1", RefreshToken: "rt1"},
		{CredentialID: "cred-2", RefreshToken: "rt2"},
	})
	if err != nil {
		t.Fatalf("SSE 提前断不该报 top-level err · 上层按 PerIndex 判缺失: %v", err)
	}
	// 只有第一条 verified · 第二条上层要识别为 stream_broken(在 Pusher.classifyResult)
	if len(res.PerIndex) != 1 {
		t.Errorf("PerIndex = %d · 应 1", len(res.PerIndex))
	}
}

// ── Close 后再调 · 拒服务 ─────────────────────────────

func TestClientClosed(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Close 后不该发请求")
	})
	c.Close()
	_, err := c.BatchImport(context.Background(), []ImportInput{{CredentialID: "c", RefreshToken: "rt"}})
	if err == nil {
		t.Fatal("Close 后应报错")
	}
	var se *StreamError
	if !errors.As(err, &se) || se.Kind != KindBadRequest {
		t.Errorf("Kind = %q · 应是 bad_request", se.Kind)
	}
}

// ── 空请求 · 拒服务 ───────────────────────────────────

func TestBatchImportEmpty(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("空批不该发请求")
	})
	defer c.Close()
	_, err := c.BatchImport(context.Background(), nil)
	if err == nil {
		t.Fatal("空批应报错")
	}
}

// ── 超时 → timeout 可重试 ───────────────────────────────

func TestBatchImportTimeout(t *testing.T) {
	_, c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		// hang 到客户端超时
		time.Sleep(5 * time.Second)
	})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := c.BatchImport(ctx, []ImportInput{{CredentialID: "c", RefreshToken: "rt"}})
	if err == nil {
		t.Fatal("超时应报错")
	}
	var se *StreamError
	if !errors.As(err, &se) || se.Kind != KindTimeout {
		t.Errorf("Kind = %q · 应是 timeout", se.Kind)
	}
}
