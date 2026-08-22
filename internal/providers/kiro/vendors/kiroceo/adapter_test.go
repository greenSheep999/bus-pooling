package kiroceo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// I-09 · 本 vendor 关键路径 3 条测试:stock / purchase / 401
// 端点走 /api/my/* · stock 用 max 字段 · zones 里带 unit_price + available

func newTestAdapter(t *testing.T, url string) *Adapter {
	t.Helper()
	a, err := New(Config{
		BaseURL:    url,
		APIKey:     "usr-test-ceo",
		Timeout:    2 * time.Second,
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// TestStockSuccess · GET /api/my/stock · Available 取自 max 字段(2026-08-15 P0 修)
func TestStockSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/my/stock" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "usr-test-ceo" {
			t.Fatalf("missing api key header")
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"max":          15,
			"min":          1,
			"max_purchase": 10,
			"quota":        0,
			"reserved":     0,
			"zones": []map[string]any{
				{"zone": "us", "label": "美国区", "unit_price": 30, "enabled": true, "available": 8, "max": 8, "stock": 8},
				{"zone": "eu", "label": "欧洲区", "unit_price": 10, "enabled": true, "available": 7, "max": 7, "stock": 7},
			},
		})
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv.URL)
	snap, err := a.Stock(context.Background(), providers.StockOptions{})
	if err != nil {
		t.Fatalf("Stock: %v", err)
	}
	// Available 走 stock.max 字段(P0 修 · 档案 §2.2)
	if snap.Available != 15 {
		t.Errorf("Available = %d · want 15(max 字段)", snap.Available)
	}
	if len(snap.Zones) != 2 {
		t.Fatalf("zones = %d", len(snap.Zones))
	}
	if snap.WarrantyMinutes != 0 {
		// 本 vendor 无 warranty_minutes 字段 · Capability 层硬编 10min · 这里 = 0
		t.Errorf("wire warranty_minutes = %d · want 0(硬编 10 在 Capability)", snap.WarrantyMinutes)
	}
}

// TestPurchaseSuccess · POST /api/my/purchase
func TestPurchaseSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/my/purchase" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "usr-test-ceo" {
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
			OrderID:       "ord-ceo-456",
			Zone:          "us",
			Purchased:     2,
			UnitPrice:     30,
			TotalCredits:  60,
			Keys: []keyItem{
				{ID: "k1", Key: "ksk_ceo_1", Account: "a@b.com", Password: "p1", IssuerURL: "https://d.awsapps.com/start"},
				{ID: "k2", Key: "ksk_ceo_2", Account: "a@b.com", Password: "p2", IssuerURL: "https://d.awsapps.com/start"},
			},
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

// TestPurchase401 · vendor 侧鉴权失败
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
