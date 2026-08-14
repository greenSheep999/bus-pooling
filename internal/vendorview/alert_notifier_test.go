package vendorview

// v3.5 · 告警外发 unit test
//
// 覆盖：冷却窗去重 · 恢复后清冷却 · 下游失败静默 · nil-notifier nil-safe。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mockAlertServer(t *testing.T) (*httptest.Server, *atomic.Int32, *[]AlertPayload, *sync.Mutex) {
	t.Helper()
	var count atomic.Int32
	got := make([]AlertPayload, 0)
	mu := &sync.Mutex{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p AlertPayload
		_ = json.Unmarshal(body, &p)
		mu.Lock()
		got = append(got, p)
		mu.Unlock()
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &count, &got, mu
}

// 冷却窗内同一管线只发一次 stale
func TestWebhookNotifier_Cooldown(t *testing.T) {
	srv, count, _, _ := mockAlertServer(t)
	n := NewWebhookNotifier(srv.URL, 30*time.Minute, nil)
	if n == nil {
		t.Fatal("空 URL 才该返 nil")
	}

	rows := []PipelineHealthRow{{VendorID: "kirotest", Pipeline: "probe"}}
	now := time.Now()

	n.Notify(context.Background(), rows, now)
	// 1min 后再报 · 冷却窗内 · 不应再发
	n.Notify(context.Background(), rows, now.Add(1*time.Minute))
	// 31min 后 · 出冷却窗 · 应再发一次
	n.Notify(context.Background(), rows, now.Add(31*time.Minute))

	// httptest 是同步的 · 但 post 是当前 goroutine 里跑 · 等 100ms 稳
	time.Sleep(50 * time.Millisecond)
	if got := count.Load(); got != 2 {
		t.Errorf("冷却窗应只放 2 次 · 得 %d", got)
	}
}

// 恢复后清冷却窗 · 下次陈旧立即能报
func TestWebhookNotifier_RecoveryClearsCooldown(t *testing.T) {
	srv, count, got, mu := mockAlertServer(t)
	n := NewWebhookNotifier(srv.URL, 30*time.Minute, nil)

	rows := []PipelineHealthRow{{VendorID: "kirotest", Pipeline: "probe"}}
	now := time.Now()

	n.Notify(context.Background(), rows, now)
	// 恢复
	n.NotifyRecovered(context.Background(),
		[]AlertKey{{VendorID: "kirotest", Pipeline: "probe"}}, now.Add(2*time.Minute))
	// 恢复后 1min 再陈旧 · 应立即再发（冷却窗被清）
	n.Notify(context.Background(), rows, now.Add(3*time.Minute))

	time.Sleep(50 * time.Millisecond)
	if c := count.Load(); c != 3 {
		t.Errorf("stale + recovered + stale = 3 · 得 %d", c)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 3 {
		t.Fatalf("got len %d", len(*got))
	}
	// 顺序：stale · recovered · stale
	if (*got)[0].Type != "stale" || (*got)[1].Type != "recovered" || (*got)[2].Type != "stale" {
		t.Errorf("顺序错 · 得 %v %v %v", (*got)[0].Type, (*got)[1].Type, (*got)[2].Type)
	}
}

// 没告警过的管线 · 不应发 recovered
func TestWebhookNotifier_NoRecoveryIfNotActive(t *testing.T) {
	srv, count, _, _ := mockAlertServer(t)
	n := NewWebhookNotifier(srv.URL, 30*time.Minute, nil)

	// 直接调 NotifyRecovered · active 集为空 · 不应发
	n.NotifyRecovered(context.Background(),
		[]AlertKey{{VendorID: "kirotest", Pipeline: "probe"}}, time.Now())

	time.Sleep(50 * time.Millisecond)
	if got := count.Load(); got != 0 {
		t.Errorf("无 active · 不该发 · 得 %d", got)
	}
}

// 下游返 500 · 不该 panic · 只 WARN
func TestWebhookNotifier_DownstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, 30*time.Minute, nil)
	rows := []PipelineHealthRow{{VendorID: "kirotest", Pipeline: "probe"}}
	// 不 panic 即 OK
	n.Notify(context.Background(), rows, time.Now())
}

// 空 URL · 返 nil · 调用侧 nil-safe
func TestWebhookNotifier_EmptyURL(t *testing.T) {
	n := NewWebhookNotifier("", 0, nil)
	if n != nil {
		t.Fatal("空 URL 应返 nil")
	}
	// nil.Notify 不 panic
	n.Notify(context.Background(), nil, time.Now())
	n.NotifyRecovered(context.Background(), nil, time.Now())
}
