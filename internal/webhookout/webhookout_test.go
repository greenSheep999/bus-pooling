package webhookout

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

// ── 签名黄金值 · 换 secret / body 都影响 hex ─────────────

func TestSignPayload(t *testing.T) {
	secret := "test-secret-xxx"
	ts := time.Unix(1700000000, 0).UTC()
	body := []byte(`{"event":"test"}`)

	got := SignPayload(secret, ts, body)
	if !strings.HasPrefix(got, "sha256=") {
		t.Fatalf("签名应以 sha256= 开头: %s", got)
	}

	// 手工重算 · 确认稳定(黄金值)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts.Unix(), 10)))
	mac.Write([]byte{'.'})
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Errorf("签名不稳定 got=%s want=%s", got, want)
	}

	// 换 secret · 签名全变
	other := SignPayload("other-secret", ts, body)
	if other == got {
		t.Error("换 secret 签名应变")
	}
	// 换 body 一位 · 签名全变
	other2 := SignPayload(secret, ts, []byte(`{"event":"tesx"}`))
	if other2 == got {
		t.Error("换 body 一位签名应变")
	}
}

// ── mock DownstreamStore + HTTPDoer ────────────────────

type mockStore struct {
	mu         sync.Mutex // -race · InsertDelivery/断言 goroutine 之间同步 deliveries 切片
	url        string
	secret     string
	configured bool
	busOnly    bool
	// enabled/events · 1e-2 P0-1/2 落库开关 + 订阅白名单
	// 默认 enabled=true(跟 downstream.Defaults 一致) · 测试构造时想关就显式 false
	disabled bool     // 反向命名(零值 false = enabled) 减少构造模板负担
	events   []string // nil = 全订阅兜底
	// InsertDelivery 记录的行 · 测试断言用
	deliveries []DeliveryAttempt
	getErr     error
}

func (m *mockStore) Get(_ context.Context, pid string) (DownstreamConfig, error) {
	if m.getErr != nil {
		return DownstreamConfig{}, m.getErr
	}
	return DownstreamConfig{
		PassengerID:             pid,
		WebhookURL:              m.url,
		WebhookSecretEncrypted:  []byte(m.secret),
		WebhookSecretConfigured: m.configured,
		Enabled:                 !m.disabled,
		Events:                  m.events,
		BusOnly:                 m.busOnly,
	}, nil
}

func (m *mockStore) DecryptWebhookSecret(b []byte) (string, error) {
	return string(b), nil
}

func (m *mockStore) InsertDelivery(_ context.Context, a DeliveryAttempt) (DeliveryRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliveries = append(m.deliveries, a)
	return DeliveryRow{ID: "row-" + a.EventID}, nil
}

// snapDeliveries · 测试断言用 · 返 deliveries 副本 · 读锁保护
func (m *mockStore) snapDeliveries() []DeliveryAttempt {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]DeliveryAttempt, len(m.deliveries))
	copy(out, m.deliveries)
	return out
}

// mockHTTP · 转成 httptest.Server 拿到的响应
type mockHTTP struct {
	handler http.HandlerFunc
}

func (m *mockHTTP) Do(ctx context.Context, req *HTTPReq) (*HTTPResp, error) {
	// 走真的 httptest.Server(通过 URL 转发)
	r, err := http.NewRequestWithContext(ctx, req.Method, req.URL, strings.NewReader(string(req.Body)))
	if err != nil {
		return nil, err
	}
	for k, v := range req.Headers {
		r.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return &HTTPResp{
		StatusCode: resp.StatusCode,
		Body:       body,
	}, nil
}

// setupTestDB · 建临时 SQLite + 跑迁移(migration 003 有 outbound_webhook_delivery)
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	// 迁移路径 · 从项目根算
	_, err = d.MigrateUp(context.Background(), "../db/migrations")
	if err != nil {
		t.Fatalf("迁移: %v", err)
	}
	// 塞一个 passenger 满足 FK
	_, err = d.DB.ExecContext(context.Background(), `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES ('p1', 'p1', 'p1@x.tmp', 'h', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`)
	if err != nil {
		t.Fatalf("塞 passenger: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d.DB
}

// ── 200 端到端 · 收到签名 · 落 delivered 台账 ──────────

func TestDispatchDelivered(t *testing.T) {
	var (
		mu            sync.Mutex // -race · handler goroutine 跟主 goroutine 同步 header 读取
		receivedSig   string
		receivedEvent string
	)
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		mu.Lock()
		receivedSig = r.Header.Get("X-Bus-Signature")
		receivedEvent = r.Header.Get("X-Bus-Event")
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := &mockStore{url: srv.URL, secret: "sec1", configured: true}
	d := New(Config{
		DB:         setupTestDB(t),
		Store:      store,
		HTTPX:      &mockHTTP{},
		Timeout:    3 * time.Second,
		MaxRetries: 3,
	})
	d.Start(context.Background())
	defer d.Stop(3 * time.Second)

	d.Dispatch(context.Background(), "p1", EventBoarded, BoardedPayload{
		EnvelopeMeta:  buildEnvelope(d.cfg.Now(), "evt-1", "p1", EventBoarded),
		CredentialIDs: []string{"c-1"},
		Route:         "push_pool",
	})

	waitFor(t, 3*time.Second, func() bool { return atomic.LoadInt32(&callCount) >= 1 })

	mu.Lock()
	gotSig := receivedSig
	gotEvent := receivedEvent
	mu.Unlock()
	if !strings.HasPrefix(gotSig, "sha256=") {
		t.Errorf("对家应收到 sha256= 签名 · 得到 %q", gotSig)
	}
	if gotEvent != "boarded" {
		t.Errorf("X-Bus-Event = %q · 应是 boarded", gotEvent)
	}
	waitFor(t, 3*time.Second, func() bool { return len(store.snapDeliveries()) > 0 })
	if store.snapDeliveries()[0].Status != "delivered" {
		t.Errorf("台账 status = %q · 应是 delivered", store.snapDeliveries()[0].Status)
	}
}

// ── 4xx 直接 failed(不重试) ────────────────────────

func TestDispatch4xxFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	store := &mockStore{url: srv.URL, secret: "sec1", configured: true}
	d := New(Config{
		DB: setupTestDB(t), Store: store, HTTPX: &mockHTTP{}, Timeout: 3 * time.Second, MaxRetries: 3,
	})
	d.Start(context.Background())
	defer d.Stop(3 * time.Second)

	d.Dispatch(context.Background(), "p1", EventBoarded, BoardedPayload{
		EnvelopeMeta:  buildEnvelope(d.cfg.Now(), "evt-4xx", "p1", EventBoarded),
		CredentialIDs: []string{"c-1"},
	})

	waitFor(t, 3*time.Second, func() bool { return len(store.snapDeliveries()) > 0 })
	if store.snapDeliveries()[0].Status != "failed" {
		t.Errorf("4xx 应直接 failed · 得到 %q", store.snapDeliveries()[0].Status)
	}
}

// ── 5xx → pending · retriable=true ────────────────────

func TestDispatch5xxPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	store := &mockStore{url: srv.URL, secret: "sec1", configured: true}
	d := New(Config{
		DB: setupTestDB(t), Store: store, HTTPX: &mockHTTP{}, Timeout: 3 * time.Second, MaxRetries: 3,
	})
	d.Start(context.Background())
	defer d.Stop(3 * time.Second)

	d.Dispatch(context.Background(), "p1", EventBoarded, BoardedPayload{
		EnvelopeMeta:  buildEnvelope(d.cfg.Now(), "evt-5xx", "p1", EventBoarded),
		CredentialIDs: []string{"c-1"},
	})

	waitFor(t, 3*time.Second, func() bool { return len(store.snapDeliveries()) > 0 })
	if store.snapDeliveries()[0].Status != "pending" {
		t.Errorf("5xx 应 pending 让 retrier 重试 · 得到 %q", store.snapDeliveries()[0].Status)
	}
}

// ── 未配 webhook · Dispatch 不发不落台账 ───────────────

func TestDispatchNoWebhook(t *testing.T) {
	store := &mockStore{configured: false}
	d := New(Config{
		DB: setupTestDB(t), Store: store, HTTPX: &mockHTTP{}, Timeout: 3 * time.Second, MaxRetries: 3,
	})
	d.Start(context.Background())
	defer d.Stop(3 * time.Second)

	d.Dispatch(context.Background(), "p1", EventBoarded, BoardedPayload{
		EnvelopeMeta:  buildEnvelope(d.cfg.Now(), "evt-x", "p1", EventBoarded),
		CredentialIDs: []string{"c-1"},
	})
	// 等一下 consume 跑完
	time.Sleep(200 * time.Millisecond)
	if len(store.snapDeliveries()) != 0 {
		t.Errorf("未配 webhook 不该落台账 · 得到 %+v", store.deliveries)
	}
}

// ── SendTest 同步返(delivered / status / latency) ─────

func TestSendTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := &mockStore{url: srv.URL, secret: "sec1", configured: true}
	d := New(Config{
		DB: setupTestDB(t), Store: store, HTTPX: &mockHTTP{}, Timeout: 3 * time.Second, MaxRetries: 3,
	})
	// 不 Start · SendTest 是同步的

	ok, status, _, errMsg := d.SendTest(context.Background(), "p1")
	if !ok {
		t.Errorf("SendTest ok=false · status=%d err=%q", status, errMsg)
	}
	if status != 200 {
		t.Errorf("status = %d · 应是 200", status)
	}
	if len(store.snapDeliveries()) != 1 || store.snapDeliveries()[0].EventType != "test" {
		t.Errorf("SendTest 应落一条 test 台账 · 得到 %+v", store.deliveries)
	}
}

// ── 队列满 · 落 dropped 台账 ─────────────────────────

func TestQueueFullDropped(t *testing.T) {
	// 建 queue_size=1 · 塞 2 条不消费 · 第二条 dropped
	store := &mockStore{url: "http://mock", secret: "s", configured: true}
	d := New(Config{
		DB: setupTestDB(t), Store: store, HTTPX: &mockHTTP{},
		Timeout: 3 * time.Second, MaxRetries: 3,
		QueueSize: 1,
	})
	// **不 Start** · 让 queue 塞满
	d.Dispatch(context.Background(), "p1", EventBoarded, BoardedPayload{EnvelopeMeta: buildEnvelope(time.Now(), "e1", "p1", EventBoarded)})
	d.Dispatch(context.Background(), "p1", EventBoarded, BoardedPayload{EnvelopeMeta: buildEnvelope(time.Now(), "e2", "p1", EventBoarded)})
	if len(store.snapDeliveries()) != 1 || store.snapDeliveries()[0].Status != "dropped" {
		t.Errorf("第二条应 dropped · 得到 %+v", store.deliveries)
	}
}

// ── bus_only 过滤:BoardedPayload HasBusID=false · 关掉过滤后能发 ──

func TestBusOnlyFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := &mockStore{url: srv.URL, secret: "s", configured: true, busOnly: true}
	d := New(Config{
		DB: setupTestDB(t), Store: store, HTTPX: &mockHTTP{}, Timeout: 3 * time.Second, MaxRetries: 3,
	})
	d.Start(context.Background())
	defer d.Stop(3 * time.Second)

	// Boarded 不带 bus_id · busOnly=true → 不发
	d.Dispatch(context.Background(), "p1", EventBoarded, BoardedPayload{EnvelopeMeta: buildEnvelope(time.Now(), "e1", "p1", EventBoarded)})
	time.Sleep(200 * time.Millisecond)
	if len(store.snapDeliveries()) != 0 {
		t.Errorf("bus_only 应过滤 boarded(无 bus_id) · 得到 %+v", store.deliveries)
	}

	// NewKeysAvailable 带 bus_id · 应发
	d.Dispatch(context.Background(), "p1", EventNewKeysAvailable, NewKeysAvailablePayload{
		EnvelopeMeta: buildEnvelope(time.Now(), "e2", "p1", EventNewKeysAvailable),
		BusID:        "bus-1",
		NewKeys:      2,
	})
	waitFor(t, 3*time.Second, func() bool { return len(store.snapDeliveries()) > 0 })
	if store.snapDeliveries()[0].EventType != "new_keys_available" {
		t.Errorf("应发 new_keys_available · 得到 %+v", store.deliveries)
	}
}

// ── waitFor 帮 goroutine 消费类测试等条件成立 ────────────

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("waitFor 超时")
}

// ── nil 保护 · Dispatch nil dispatcher 不 panic ────────

func TestDispatchNilSafe(t *testing.T) {
	var d *Dispatcher
	// 不 panic 就算过
	d.Dispatch(context.Background(), "p1", EventBoarded, TestPayload{})
	ok, _, _, msg := d.SendTest(context.Background(), "p1")
	if ok {
		t.Error("nil Dispatcher SendTest 应返 false")
	}
	if !strings.Contains(msg, "未装配") {
		t.Errorf("msg = %q", msg)
	}
}

// ── Get 失败 · 落 dropped ─────────────────────────────

func TestDispatchGetError(t *testing.T) {
	store := &mockStore{getErr: errors.New("db down")}
	d := New(Config{
		DB: setupTestDB(t), Store: store, HTTPX: &mockHTTP{}, Timeout: 3 * time.Second, MaxRetries: 3,
	})
	d.Start(context.Background())
	defer d.Stop(3 * time.Second)

	d.Dispatch(context.Background(), "p1", EventBoarded, BoardedPayload{
		EnvelopeMeta:  buildEnvelope(time.Now(), "e1", "p1", EventBoarded),
		CredentialIDs: []string{"c1"},
	})
	waitFor(t, 3*time.Second, func() bool { return len(store.snapDeliveries()) > 0 })
	if store.snapDeliveries()[0].Status != "dropped" {
		t.Errorf("Get 失败应落 dropped · 得到 %q", store.snapDeliveries()[0].Status)
	}
}
