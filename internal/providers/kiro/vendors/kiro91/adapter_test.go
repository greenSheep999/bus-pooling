package kiro91

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

func newTestAdapter(t *testing.T, url string) *Adapter {
	t.Helper()
	a, err := New(Config{
		BaseURL:    url,
		APIKey:     "usr-test-key",
		Timeout:    2 * time.Second,
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func TestStockSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/my/stock" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "usr-test-key" {
			t.Fatalf("missing api key header")
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(stockResp{
			Stock: struct {
				PublicAvailable int `json:"public_available"`
				MyPrivate       int `json:"my_private"`
			}{PublicAvailable: 12},
			Zones: []zoneItem{
				{Zone: "us", Region: "us-east-1", Available: 8, UnitPrice: 30},
				{Zone: "eu", Region: "eu-central-1", Available: 4, UnitPrice: 10},
			},
			Min:   1,
			MaxPO: 200,
			WM:    10,
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	snap, err := a.Stock(context.Background(), providers.StockOptions{})
	if err != nil {
		t.Fatalf("Stock: %v", err)
	}
	if snap.Available != 12 {
		t.Errorf("available = %d, want 12", snap.Available)
	}
	if len(snap.Zones) != 2 {
		t.Fatalf("zones = %d", len(snap.Zones))
	}
	if snap.Zones[0].UnitPrice.Amount != 30_000_000 {
		t.Errorf("us unit_price = %d micro, want 30_000_000", snap.Zones[0].UnitPrice.Amount)
	}
	if snap.WarrantyMinutes != 10 {
		t.Errorf("warranty_minutes = %d", snap.WarrantyMinutes)
	}
}

func TestPurchaseSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/my/purchase" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body purchaseReq
		json.NewDecoder(r.Body).Decode(&body)
		if body.Count != 3 {
			t.Fatalf("count = %d", body.Count)
		}
		if body.Zone != "us" {
			t.Fatalf("zone = %q", body.Zone)
		}
		if body.ClientOrderID != "aabbccdd11223344aabbccdd11223344" {
			t.Fatalf("client_order_id = %q", body.ClientOrderID)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(purchaseResp{
			ClientOrderID: "aabbccdd11223344aabbccdd11223344",
			OrderID:       "ord-123",
			Zone:          "us",
			Purchased:     3,
			UnitPrice:     30,
			TotalCredits:  90,
			Remaining:     4410,
			Keys: []keyItem{
				{ID: "k1", Key: "ksk_1", Account: "a@b.com", Password: "p1", IssuerURL: "https://d-1.awsapps.com/start", Paid: 30, WarrantyUntil: "2026-08-01T12:00:00Z"},
				{ID: "k2", Key: "ksk_2", Account: "a@b.com", Password: "p2", IssuerURL: "https://d-1.awsapps.com/start", Paid: 30, WarrantyUntil: "2026-08-01T12:00:00Z"},
				{ID: "k3", Key: "ksk_3", Account: "a@b.com", Password: "p3", IssuerURL: "https://d-1.awsapps.com/start", Paid: 30, WarrantyUntil: "2026-08-01T12:00:00Z"},
			},
			WarrantyUntil: "2026-08-01T12:00:00Z",
			WM:            10,
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	zone := providers.ZoneUS
	result, err := a.Purchase(context.Background(), providers.PurchaseRequest{
		Count:         3,
		ClientOrderID: "aabbccdd11223344aabbccdd11223344",
		Zone:          &zone,
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if result.Purchased != 3 {
		t.Errorf("purchased = %d", result.Purchased)
	}
	if result.TotalCost.Amount != 90_000_000 {
		t.Errorf("total_cost = %d", result.TotalCost.Amount)
	}
	if len(result.Keys) != 3 {
		t.Fatalf("keys = %d", len(result.Keys))
	}
	if result.Keys[0].Key != "ksk_1" {
		t.Errorf("keys[0].key = %q", result.Keys[0].Key)
	}
	if result.Keys[0].Account != "a@b.com" {
		t.Errorf("keys[0].account = %q", result.Keys[0].Account)
	}
	if result.WarrantyUntil == nil {
		t.Error("warranty_until 应该有值")
	}
}

func TestStock401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(errorResp{Code: "invalid_api_key", Message: "invalid api key"})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	_, err := a.Stock(context.Background(), providers.StockOptions{})
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !errors.Is(err, providers.ErrAuth) {
		t.Fatalf("应是 ErrAuth，得到 %v", err)
	}
	var ae *providers.APIError
	if !errors.As(err, &ae) {
		t.Fatal("应是 APIError")
	}
	if ae.StatusCode != 401 {
		t.Errorf("status = %d", ae.StatusCode)
	}
}

func TestPurchase429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(errorResp{Code: "rate_limited", Message: "slow down"})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	_, err := a.Purchase(context.Background(), providers.PurchaseRequest{Count: 1, ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90"})
	if !errors.Is(err, providers.ErrRateLimited) {
		t.Fatalf("应是 ErrRateLimited，得到 %v", err)
	}
	var ae *providers.APIError
	if !errors.As(err, &ae) {
		t.Fatal("应是 APIError")
	}
	if !ae.Retryable() {
		t.Error("429 应该 retryable")
	}
	if ae.RetryAfter == nil || *ae.RetryAfter != 5*time.Second {
		t.Errorf("retry_after = %v", ae.RetryAfter)
	}
}

func TestPurchaseNoStock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(errorResp{Code: "no_stock", Message: "no keys available"})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	_, err := a.Purchase(context.Background(), providers.PurchaseRequest{Count: 5, ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90"})
	if !errors.Is(err, providers.ErrNoStock) {
		t.Fatalf("应是 ErrNoStock，得到 %v", err)
	}
	var ae *providers.APIError
	errors.As(err, &ae)
	if ae.Retryable() {
		t.Error("no_stock 不 retryable（换 vendor 去）")
	}
}

func TestPurchaseRetrySameOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(errorResp{Code: "retry_same_order", Message: "stock depleted mid-tx, retry with same order id"})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	_, err := a.Purchase(context.Background(), providers.PurchaseRequest{Count: 5, ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90"})
	if !errors.Is(err, providers.ErrRetrySameOrder) {
		t.Fatalf("应是 ErrRetrySameOrder，得到 %v", err)
	}
	var ae *providers.APIError
	errors.As(err, &ae)
	if !ae.Retryable() {
		t.Error("retry_same_order 应 retryable")
	}
	if !ae.MustReuseOrderID() {
		t.Error("必须复用 order id")
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	a := newTestAdapter(t, "http://unused")
	secret := "my-webhook-secret"
	body := []byte(`{"event":"new_keys_available","new_keys":5}`)
	ts := fmt.Sprintf("%d", time.Now().Unix())

	h := http.Header{}
	h.Set("X-KM-Signature", computeSig(secret, ts, body))
	h.Set("X-KM-Timestamp", ts)

	if err := a.VerifySignature(secret, h, body); err != nil {
		t.Errorf("合法签名应通过: %v", err)
	}

	h.Set("X-KM-Signature", "sha256=bad")
	if err := a.VerifySignature(secret, h, body); err == nil {
		t.Error("错误签名应拒绝")
	}

	if err := a.VerifySignature("", h, body); !errors.Is(err, providers.ErrNoSignature) {
		t.Errorf("空 secret 应返回 ErrNoSignature，得到 %v", err)
	}
}

func computeSig(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// TestSentinelForLocksDocTable 把档案 §12 的错误码表钉死。
//
// 关键：**判 code 优先于判 status** —— 三个 409 语义完全不同。
func TestSentinelForLocksDocTable(t *testing.T) {
	cases := []struct {
		code      string
		status    int
		want      error
		retryable bool
	}{
		// 400 家族 · 全都不该重试
		{"bad_json", 400, providers.ErrBadRequest, false},
		{"bad_order_id", 400, providers.ErrBadRequest, false},
		{"bad_count", 400, providers.ErrBadCount, false},
		{"bad_zone", 400, providers.ErrBadZone, false},
		{"idempotency_conflict", 400, providers.ErrIdempotencyConflict, false},
		{"body_too_large", 413, providers.ErrBadRequest, false},

		// 鉴权 / 停用
		{"unauthenticated", 401, providers.ErrAuth, false},
		{"invalid_api_key", 401, providers.ErrAuth, false},
		{"disabled", 403, providers.ErrDisabled, false},
		{"session_required", 403, providers.ErrAuth, false},

		{"insufficient_balance", 402, providers.ErrInsufficientFunds, false},
		{"not_found", 404, providers.ErrNotFound, false},
		{"redeem_invalid", 404, providers.ErrNotFound, false},

		// 三个 409 —— 上层处理完全不同，绝不能压平
		{"no_stock", 409, providers.ErrNoStock, false},
		{"purchase_cap_reached", 409, providers.ErrPurchaseCapReached, false},
		{"retry_same_order", 409, providers.ErrRetrySameOrder, true},

		{"rate_limited", 429, providers.ErrRateLimited, true},
		{"verify_failed", 502, providers.ErrUpstream, true},
		{"quota_failed", 502, providers.ErrUpstream, true},
		{"internal", 500, providers.ErrUpstream, true},

		// code 缺失 → 退回按 status
		{"", 401, providers.ErrAuth, false},
		{"", 429, providers.ErrRateLimited, true},
		{"", 503, providers.ErrUpstream, true},
		// 没见过的 4xx 不能给 ErrUpstream（那会被判成可重试）
		{"brand_new_code", 418, providers.ErrBadRequest, false},
	}

	for _, c := range cases {
		got := sentinelFor(c.code, c.status)
		if !errors.Is(got, c.want) {
			t.Errorf("sentinelFor(%q, %d) = %v，want %v", c.code, c.status, got, c.want)
			continue
		}
		ae := &providers.APIError{Sentinel: got}
		if ae.Retryable() != c.retryable {
			t.Errorf("sentinelFor(%q, %d) → %v: Retryable = %v，want %v",
				c.code, c.status, got, ae.Retryable(), c.retryable)
		}
	}
}

// TestPurchaseMixedPriceOrder 混价单：unit_price 只是其中一把的价，
// 乘数量跟实扣不一致。权威值是 total_credits，且恒等于 Σ keys[].paid（档案 §7）。
func TestPurchaseMixedPriceOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(purchaseResp{
			ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90",
			OrderID:       "ord-mixed",
			Zone:          "us",
			Purchased:     3,
			UnitPrice:     30, // 只反映一辆车
			TotalCredits:  75, // 30 + 30 + 15，不等于 30×3
			Remaining:     925,
			Keys: []keyItem{
				{ID: "k1", Key: "ksk_1", Paid: 30},
				{ID: "k2", Key: "ksk_2", Paid: 30},
				{ID: "k3", Key: "ksk_3", Paid: 15}, // 另一辆车，便宜
			},
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	got, err := a.Purchase(context.Background(), providers.PurchaseRequest{
		Count: 3, ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90",
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}

	if got.TotalCost.Amount != 75_000_000 {
		t.Errorf("TotalCost = %d，want 75_000_000（权威值，不是 unit_price×3）", got.TotalCost.Amount)
	}
	// 恒等式：Σ paid == TotalCost
	var sum int64
	for _, k := range got.Keys {
		sum += k.Paid.Amount
	}
	if sum != got.TotalCost.Amount {
		t.Errorf("Σ keys[].paid = %d，TotalCost = %d —— 档案 §7 说这俩恒等", sum, got.TotalCost.Amount)
	}
	// 别用 unit_price 乘数量
	if naive := got.UnitPrice.Amount * int64(got.Purchased); naive == got.TotalCost.Amount {
		t.Error("这个 fixture 本意是混价单，unit_price×数量 不该等于 TotalCost")
	}
}

// TestPurchasePartialFill 申请 5 拿到 3 是正常结果 —— 按 purchased 处理，不是 count。
func TestPurchasePartialFill(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(purchaseResp{
			ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90",
			OrderID:       "ord-partial",
			Zone:          "us",
			Purchased:     3,
			UnitPrice:     30,
			TotalCredits:  90,
			Keys: []keyItem{
				{ID: "k1", Key: "ksk_1", Paid: 30},
				{ID: "k2", Key: "ksk_2", Paid: 30},
				{ID: "k3", Key: "ksk_3", Paid: 30},
			},
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	got, err := a.Purchase(context.Background(), providers.PurchaseRequest{
		Count: 5, ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90",
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if got.Requested != 5 {
		t.Errorf("Requested = %d，want 5", got.Requested)
	}
	if got.Purchased != 3 {
		t.Errorf("Purchased = %d，want 3", got.Purchased)
	}
	if len(got.Keys) != got.Purchased {
		t.Errorf("len(Keys) = %d，应等于 Purchased = %d", len(got.Keys), got.Purchased)
	}
}

// TestPurchaseFreeKeyHasNoWarranty 免费领的（留自用车）没有可退积分，
// warranty_until 为空（档案 §4.1）。
func TestPurchaseFreeKeyHasNoWarranty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(purchaseResp{
			ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90",
			OrderID:       "ord-free",
			Zone:          "us",
			Purchased:     1,
			TotalCredits:  0,
			Keys: []keyItem{
				{ID: "k1", Key: "ksk_free", Free: true, Paid: 0, WarrantyUntil: ""},
			},
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	got, err := a.Purchase(context.Background(), providers.PurchaseRequest{
		Count: 1, ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90",
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	k := got.Keys[0]
	if !k.Free {
		t.Error("Free 应为 true")
	}
	if k.WarrantyUntil != nil {
		t.Error("免费领的没有质保窗口，WarrantyUntil 应为 nil")
	}
	if k.Paid.Amount != 0 {
		t.Errorf("Paid = %d，免费的应为 0", k.Paid.Amount)
	}
}

// TestStockDefaultZoneNotSent 不传 zone 时请求体里就不该有 zone 字段 ——
// 缺省 = 只取美国区，让服务端自己决定（档案 §7）。
func TestPurchaseOmitsEmptyZone(t *testing.T) {
	var gotBody purchaseReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if bytes.Contains(raw, []byte(`"zone"`)) {
			t.Errorf("没传 Zone 时请求体不该有 zone 字段，得到 %s", raw)
		}
		json.Unmarshal(raw, &gotBody)
		json.NewEncoder(w).Encode(purchaseResp{
			ClientOrderID: gotBody.ClientOrderID, OrderID: "o1", Zone: "us", Purchased: 1,
			Keys:         []keyItem{{ID: "k1", Key: "ksk_1", Paid: 30}},
			TotalCredits: 30,
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	if _, err := a.Purchase(context.Background(), providers.PurchaseRequest{
		Count: 1, ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90",
	}); err != nil {
		t.Fatalf("Purchase: %v", err)
	}
}

// TestPurchaseNeverClaimsReplayed 本 vendor 对重放返回**字节完全一致**的响应，
// 也就是响应里没有任何字段能区分首次成交与重放（回显的 client_order_id 首次也一样）。
//
// 所以 Purchase 必须恒报 Replayed=false。曾经写成 `pr.ClientOrderID == req.ClientOrderID`
// —— 那个判据首次调用就为 true，上层若据此跳过扣费 / 台账会全错。
func TestPurchaseNeverClaimsReplayed(t *testing.T) {
	const coid = "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// vendor 回显同一个 client_order_id（首次成交也这样）
		json.NewEncoder(w).Encode(purchaseResp{
			ClientOrderID: coid, OrderID: "ord-1", Zone: "us", Purchased: 1,
			TotalCredits: 30, Keys: []keyItem{{ID: "k1", Key: "ksk_1", Paid: 30}},
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	got, err := a.Purchase(context.Background(), providers.PurchaseRequest{Count: 1, ClientOrderID: coid})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if got.Replayed {
		t.Error("Purchase 不该报 Replayed —— 响应里没有重放信号，判重放要靠我方 pull_round 状态机")
	}
}

// TestOrderKeysIsReplay 补拉本身就是"取回当时那批 key"，不产生新扣费。
func TestOrderKeysIsReplay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/my/orders/ord-1/keys" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(purchaseResp{
			ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90", OrderID: "ord-1",
			Zone: "us", Purchased: 2, TotalCredits: 60,
			Keys: []keyItem{{ID: "k1", Key: "ksk_1", Paid: 30}, {ID: "k2", Key: "ksk_2", Paid: 30}},
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	got, err := a.OrderKeys(context.Background(), "ord-1")
	if err != nil {
		t.Fatalf("OrderKeys: %v", err)
	}
	if !got.Replayed {
		t.Error("补拉应标 Replayed=true")
	}
	if len(got.Keys) != 2 {
		t.Errorf("keys = %d，want 2", len(got.Keys))
	}
	if got.Keys[0].Key != "ksk_1" {
		t.Errorf("补拉应原样返回 key 正文，得到 %q", got.Keys[0].Key)
	}
}

// TestStockExcludesPrivate Available 只算 public_available ——
// 自己车的免费号（my_private）不是"可买库存"，混进去会让 decider 以为有货。
func TestStockExcludesPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"stock": {"public_available": 0, "my_private": 7, "my_keys": 27},
			"zones": [{"zone":"us","region":"us-east-1","available":0,"unit_price":30}],
			"max": 0, "min_per_order": 1, "max_per_order": 200, "warranty_minutes": 10
		}`))
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	snap, err := a.Stock(context.Background(), providers.StockOptions{})
	if err != nil {
		t.Fatalf("Stock: %v", err)
	}
	if snap.Available != 0 {
		t.Errorf("Available = %d，want 0 —— my_private=7 不该算进可买库存", snap.Available)
	}
}

// TestUnsupportedCapabilities 本 vendor 没有这几个端点 —— 必须返回 ErrNotSupported，
// 让 deathwatch 走 housepool 探活而不是等一个不存在的接口。
func TestUnsupportedCapabilities(t *testing.T) {
	a := newTestAdapter(t, "http://unused")
	ctx := context.Background()

	if _, err := a.KeyHealth(ctx, "ksk_x"); !errors.Is(err, providers.ErrNotSupported) {
		t.Errorf("KeyHealth 应返回 ErrNotSupported，得到 %v", err)
	}
	if _, err := a.KeyStats(ctx, providers.KeyStatsOptions{Window: providers.Window24h}); !errors.Is(err, providers.ErrNotSupported) {
		t.Errorf("KeyStats 应返回 ErrNotSupported，得到 %v", err)
	}
	if _, err := a.Usage(ctx, []string{"ksk_x"}); !errors.Is(err, providers.ErrNotSupported) {
		t.Errorf("Usage 应返回 ErrNotSupported，得到 %v", err)
	}
}
