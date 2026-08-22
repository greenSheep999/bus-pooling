package kiroappio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// I-09 · 本 vendor 关键路径 3 条测试:stock / purchase / webhook
// 端点走 /api/me/* · types 支持双形状 stock(数字型 + 嵌套对象兼容)

func newTestAdapter(t *testing.T, url string) *Adapter {
	t.Helper()
	a, err := New(Config{
		BaseURL:    url,
		APIKey:     "usr-test-appio",
		Timeout:    2 * time.Second,
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// TestStockNewShape · 新形状 stock 字段是整数 · 分区库存走 stock_us/stock_eu
func TestStockNewShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me/stock" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "usr-test-appio" {
			t.Fatalf("missing api key header")
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stock":            0,
			"stock_us":         5,
			"stock_eu":         3,
			"price":            80,
			"price_us":         30,
			"price_eu":         10,
			"min_per_order":    1,
			"max_per_order":    200,
			"warranty_minutes": 10,
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	snap, err := a.Stock(context.Background(), providers.StockOptions{})
	if err != nil {
		t.Fatalf("Stock: %v", err)
	}
	// 新形状 · stock 字段本身是 0 · 分区库存走 Zones(us=5 + eu=3 由 mapper 拼)
	if snap.Available != 0 {
		t.Errorf("Available(stock 字段) = %d · want 0(新形状汇总走 Zones)", snap.Available)
	}
	var zoneSum int
	for _, z := range snap.Zones {
		zoneSum += z.Available
	}
	if zoneSum != 8 {
		t.Errorf("Zones 汇总 = %d · want 8(us=5 + eu=3)", zoneSum)
	}
	if snap.WarrantyMinutes != 10 {
		t.Errorf("warranty_minutes = %d", snap.WarrantyMinutes)
	}
}

// TestStockOldShape · 旧形状 stock 是嵌套对象(public_available / my_private)
// 兼容双形状是 P0 修的场景(见 types.go 头注)· 老 vendor 版本必须能读
func TestStockOldShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stock": map[string]int{
				"public_available": 12,
				"my_private":       0,
			},
			"zones": []zoneItem{
				{Zone: "us", Region: "us-east-1", Available: 8, UnitPrice: 30},
			},
			"min_per_order":    1,
			"max_per_order":    200,
			"warranty_minutes": 10,
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	snap, err := a.Stock(context.Background(), providers.StockOptions{})
	if err != nil {
		t.Fatalf("Stock: %v", err)
	}
	if snap.Available != 12 {
		t.Errorf("available = %d · want 12(old shape)", snap.Available)
	}
}

// TestPurchaseSuccess · POST /api/me/purchase · 拉号成功 · X-API-Key header 校验
func TestPurchaseSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/me/purchase" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "usr-test-appio" {
			t.Fatalf("missing api key")
		}
		var body purchaseReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Count != 2 {
			t.Fatalf("count = %d", body.Count)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(purchaseResp{
			ClientOrderID: body.ClientOrderID,
			OrderID:       "ord-appio-123",
			Zone:          "us",
			Purchased:     2,
			UnitPrice:     30,
			TotalCredits:  60,
			Keys: []keyItem{
				{ID: "k1", Key: "ksk_appio_1", Account: "a@b.com", Password: "p1", IssuerURL: "https://d.awsapps.com/start"},
				{ID: "k2", Key: "ksk_appio_2", Account: "a@b.com", Password: "p2", IssuerURL: "https://d.awsapps.com/start"},
			},
			WM: 10,
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	result, err := a.Purchase(context.Background(), providers.PurchaseRequest{
		Count:         2,
		ClientOrderID: "aabbccdd11223344aabbccdd11223344",
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if result.Purchased != 2 {
		t.Errorf("purchased = %d", result.Purchased)
	}
	if len(result.Keys) != 2 {
		t.Errorf("keys = %d · want 2", len(result.Keys))
	}
}

// TestPurchase401 · 401 → ErrUnauthorized · vendor 侧 API key 失效判据
func TestPurchase401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResp{Code: "unauthorized", Message: "bad api key"})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	_, err := a.Purchase(context.Background(), providers.PurchaseRequest{
		Count: 1, ClientOrderID: "aabbccdd11223344aabbccdd11223344",
	})
	if err == nil {
		t.Fatal("401 应报错")
	}
}
