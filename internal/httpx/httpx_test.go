package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient 把 sleep 换成空实现 —— 重试测试不用真的等
func newTestClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("建 client: %v", err)
	}
	c.sleep = func(time.Duration) {}
	return c
}

func mustGet(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("建请求: %v", err)
	}
	return req
}

func TestDo200(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(t, Config{Timeout: 2 * time.Second, MaxRetries: 3})
	resp, err := c.Do(context.Background(), mustGet(t, srv.URL))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Fatalf("body = %q", resp.Body)
	}
	if resp.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1（200 不该重试）", resp.Attempts)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("服务端收到 %d 次请求，want 1", got)
	}
}

func TestDoTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 超时远小于服务端耗时 · 不重试，好让失败可断言
	c := newTestClient(t, Config{Timeout: 30 * time.Millisecond, MaxRetries: 0})
	_, err := c.Do(context.Background(), mustGet(t, srv.URL))
	if err == nil {
		t.Fatal("超时应该返回错误")
	}
	if !strings.Contains(err.Error(), "httpx") {
		t.Fatalf("错误里应带包名便于定位，得到 %v", err)
	}
}

func TestDoRetriesOn503ThenSucceeds(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := newTestClient(t, Config{Timeout: 2 * time.Second, MaxRetries: 3, RetryBaseWait: time.Millisecond})
	resp, err := c.Do(context.Background(), mustGet(t, srv.URL))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", resp.Attempts)
	}
}

func TestDoExhaustsRetriesOn503(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(t, Config{Timeout: 2 * time.Second, MaxRetries: 2, RetryBaseWait: time.Millisecond})
	resp, err := c.Do(context.Background(), mustGet(t, srv.URL))
	// 重试用尽后返回**最后那个响应**而不是错误 —— 调用方要看到 503 才能决定怎么办
	if err != nil {
		t.Fatalf("重试用尽应返回最后的响应，不是错误: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if want := int32(3); atomic.LoadInt32(&hits) != want { // 1 次 + 2 次重试
		t.Fatalf("服务端收到 %d 次，want %d", hits, want)
	}
}

func TestDoRateLimitHonorsRetryAfter(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var slept []time.Duration
	c := newTestClient(t, Config{Timeout: 2 * time.Second, MaxRetries: 2, RetryBaseWait: time.Millisecond})
	c.sleep = func(d time.Duration) { slept = append(slept, d) }

	resp, err := c.Do(context.Background(), mustGet(t, srv.URL))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(slept) != 1 {
		t.Fatalf("应等待 1 次，实际 %d 次", len(slept))
	}
	// 听上游的 Retry-After，而不是用自己的退避基数
	if slept[0] != time.Second {
		t.Fatalf("等了 %v，应按 Retry-After 等 1s", slept[0])
	}
}

// 4xx（除 429）不该重试 —— 重试改变不了结果，只是浪费上游配额
func TestDoDoesNotRetry4xx(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict} {
		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(status)
		}))

		c := newTestClient(t, Config{Timeout: time.Second, MaxRetries: 3, RetryBaseWait: time.Millisecond})
		resp, err := c.Do(context.Background(), mustGet(t, srv.URL))
		srv.Close()

		if err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		if resp.Attempts != 1 {
			t.Fatalf("status %d 重试了 %d 次，4xx 不该重试", status, resp.Attempts)
		}
		if atomic.LoadInt32(&hits) != 1 {
			t.Fatalf("status %d 服务端收到 %d 次请求", status, hits)
		}
	}
}

func TestDoRespectsContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	c := newTestClient(t, Config{Timeout: 5 * time.Second, MaxRetries: 3, RetryBaseWait: time.Millisecond})
	if _, err := c.Do(ctx, mustGet(t, srv.URL)); err == nil {
		t.Fatal("ctx 超时应返回错误")
	}
}

// POST 重试要能重放 body（GetBody 由 http.NewRequest 对已知类型自动设置）
func TestDoReplaysBodyOnRetry(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		bodies = append(bodies, string(buf))
		if len(bodies) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"count":3}`))
	if err != nil {
		t.Fatal(err)
	}

	c := newTestClient(t, Config{Timeout: 2 * time.Second, MaxRetries: 2, RetryBaseWait: time.Millisecond})
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(bodies) != 2 {
		t.Fatalf("服务端收到 %d 次请求", len(bodies))
	}
	if bodies[0] != bodies[1] {
		t.Fatalf("重试时 body 不一致: %q vs %q", bodies[0], bodies[1])
	}
}

func TestNoProxyMatching(t *testing.T) {
	cases := []struct {
		host    string
		noProxy []string
		want    bool
	}{
		{"kiro.aibbq.xyz", []string{"aibbq.xyz"}, true},
		{"aibbq.xyz", []string{"aibbq.xyz"}, true},
		{"evil-aibbq.xyz", []string{"aibbq.xyz"}, false}, // 不能只做后缀字符串匹配
		{"api.91kiro.com", []string{"aibbq.xyz"}, false},
		{"localhost", []string{"localhost", "127.0.0.1"}, true},
		{"KIRO.AIBBQ.XYZ", []string{"aibbq.xyz"}, true}, // 大小写不敏感
		{"anything", nil, false},
	}
	for _, tc := range cases {
		if got := matchNoProxy(tc.host, tc.noProxy); got != tc.want {
			t.Errorf("matchNoProxy(%q, %v) = %v, want %v", tc.host, tc.noProxy, got, tc.want)
		}
	}
}

func TestNewRejectsBadProxy(t *testing.T) {
	if _, err := New(Config{Proxy: "://not a url"}); err == nil {
		t.Fatal("非法代理地址应报错")
	}
}

func TestRetryAfterParsing(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"5", 5 * time.Second},
		{" 2 ", 2 * time.Second},
		{"0", 0},
		{"abc", 0},                           // 不合法就回退到指数退避
		{"-1", 0},                            // 负数忽略
		{"Wed, 21 Oct 2015 07:28:00 GMT", 0}, // HTTP-date 形式暂不支持，回退退避
	}
	for _, tc := range cases {
		h := http.Header{}
		if tc.in != "" {
			h.Set("Retry-After", tc.in)
		}
		if got := retryAfter(h); got != tc.want {
			t.Errorf("retryAfter(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
