package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/paymentgw"
	"github.com/bus-pooling/bus-pooling/internal/topup"
	"github.com/bus-pooling/bus-pooling/internal/topupchannel"
)

// **P0 回归**（审计二轮）：pending_topup 落库失败时 · **绝不**能调外部 CreatePayment。
//
// 场景：EnsureAtLeast(gateway_creating) 失败 · handler 必须 hard fail 返 500 ·
// 一次 CreatePayment 都不能发（不然崩溃后 gateway 侧已建单本地无痕 · 走 initial → expire 丢单）。
//
// 复现方法：SQL trigger 拦截 pending_topup 的 UPDATE · 强制失败。
func TestCreateTopup_LedgerWriteFailedBlocksGatewayCall(t *testing.T) {
	env := newEnv(t)

	// 装 gateway client · 指向记数的 httptest server
	var gwCalls atomic.Int32
	gwServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gwCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"pay_never_should_reach","state":"pending"}`))
	}))
	t.Cleanup(gwServer.Close)

	client, err := paymentgw.New(paymentgw.Config{
		BaseURL:          gwServer.URL,
		BearerToken:      "test-bearer",
		SettlementSecret: "test-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	env.server.paymentGW = client
	env.server.pendingTopups = topup.NewPendingStore(env.db.DB)
	env.server.topupChannels = topupchannel.New(nil)
	// 重建 mux 让 handler 用新装配
	newMux := http.NewServeMux()
	env.server.Routes(newMux)
	env.srv.Config.Handler = newMux

	key := seedWithAPIKey(t, env, "p0@e.local", "p0u", "password123")

	// **关键**：装 trigger · 拦截 pending_topup 的 status → gateway_creating 更新
	// 让 EnsureAtLeast 命中 UPDATE 后 RAISE 错误
	if _, err := env.db.DB.Exec(`
		CREATE TRIGGER block_gateway_creating
		BEFORE UPDATE OF status ON pending_topup
		FOR EACH ROW WHEN NEW.status = 'gateway_creating'
		BEGIN
		  SELECT RAISE(ABORT, 'blocked by test trigger');
		END;
	`); err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"credits": 100_000_000, "channel": "waffo"}
	status, respBody := env.do(t, "POST", "/api/me/topup", body,
		func(r *http.Request) {
			r.Header.Set("X-API-Key", key)
			r.Header.Set("X-Idempotency-Key", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		})

	// 断言 1：handler 拒绝起单（500）
	if status != http.StatusInternalServerError {
		t.Errorf("落库失败应返 500 · got=%d body=%s", status, respBody)
	}

	// 断言 2：**CreatePayment 一次都没被调**
	if n := gwCalls.Load(); n != 0 {
		t.Errorf("gateway CreatePayment 被调 %d 次 · want 0（P0 修：落库失败必须 hard fail）", n)
	}

	// 断言 3：response 不含 order_id（起单没成功）
	var r map[string]any
	_ = json.Unmarshal(respBody, &r)
	if _, ok := r["order_id"]; ok {
		t.Errorf("失败响应不该带 order_id · got=%v", r)
	}
}

// **装配硬约束**（P0 修）：paymentGW != nil 但 pendingTopups nil · NewServer 应 panic。
func TestNewServer_PanicsIfGatewayWithoutPending(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("装配 paymentGW 但缺 pendingTopups · 应 panic")
		}
	}()
	// 用 nil DB / stores 是 OK · 只要触发校验就行
	fake, _ := paymentgw.New(paymentgw.Config{
		BaseURL: "http://ignored", BearerToken: "t", SettlementSecret: "s",
	})
	NewServer(ServerDeps{
		PaymentGW:     fake,
		PendingTopups: nil, // 关键：nil
	})
}
