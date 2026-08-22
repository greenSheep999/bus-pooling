package kirodrop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// I-03 · 双档接入 · personal 池共用 stock/purchase 端点 · 走 region 参数分流
// (跟另一家 vendor 走独立端点不同 · 本 vendor 是 region=personal 覆写)

// TestStock_PersonalRegionParam · Kind=Personal 时应加 ?region=personal
func TestStock_PersonalRegionParam(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"balance":"500.000000","price":"18.51","region":"personal","stock":9}`))
	}))
	defer srv.Close()

	a, err := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Stock(context.Background(), providers.StockOptions{Kind: providers.AccountPersonal})
	if err != nil {
		t.Fatalf("Stock: %v", err)
	}
	if gotPath != "/api/me/stock?region=personal" {
		t.Errorf("path = %q · want /api/me/stock?region=personal", gotPath)
	}
}

// TestStock_EnterpriseNoRegionParam · Kind 空/enterprise 时不带 region 参数(保持老行为)
func TestStock_EnterpriseNoRegionParam(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"balance":"500","price":"5.88","region":"us-east-1","stock":0}`))
	}))
	defer srv.Close()

	a, _ := New(Config{BaseURL: srv.URL, APIKey: "k"})
	// 空 Kind · Normalize 归到 enterprise · 不加 region
	_, _ = a.Stock(context.Background(), providers.StockOptions{})
	if gotPath != "/api/me/stock" {
		t.Errorf("empty kind · path = %q · want /api/me/stock", gotPath)
	}
	// 显式 enterprise · 也不加 region
	_, _ = a.Stock(context.Background(), providers.StockOptions{Kind: providers.AccountEnterprise})
	if gotPath != "/api/me/stock" {
		t.Errorf("enterprise · path = %q · want /api/me/stock", gotPath)
	}
}

// TestPurchase_PersonalRegionInBody · Kind=Personal 时 body.region 应是 "personal"
func TestPurchase_PersonalRegionInBody(t *testing.T) {
	var gotBody purchaseReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_order_id":"c","order_id":"o","region":"personal","purchased":1,"remaining":374,"status":"completed","refunded_amount_cny":"0","keys":[{"key":"k","region":"personal"}]}`))
	}))
	defer srv.Close()

	a, _ := New(Config{BaseURL: srv.URL, APIKey: "k"})
	_, err := a.Purchase(context.Background(), providers.PurchaseRequest{
		Count:         1,
		ClientOrderID: "aabbccdd11223344aabbccdd11223344",
		Kind:          providers.AccountPersonal,
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if gotBody.Zone != "personal" {
		t.Errorf("body.region = %q · want personal", gotBody.Zone)
	}
}

// TestPurchase_KindOverridesZone · Kind=Personal 时 · 就算 Zone 传了 us 也覆盖成 personal
// (防用户混用 · Kind 是权威)
func TestPurchase_KindOverridesZone(t *testing.T) {
	var gotBody purchaseReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_order_id":"c","order_id":"o","region":"personal","purchased":1,"remaining":"0","status":"completed","refunded_amount_cny":"0","keys":[]}`))
	}))
	defer srv.Close()

	a, _ := New(Config{BaseURL: srv.URL, APIKey: "k"})
	zone := providers.ZoneUS
	_, _ = a.Purchase(context.Background(), providers.PurchaseRequest{
		Count:         1,
		ClientOrderID: "aabbccdd11223344aabbccdd11223344",
		Kind:          providers.AccountPersonal,
		Zone:          &zone, // 传了 us · 应被 Kind 覆盖成 personal
	})
	if gotBody.Zone != "personal" {
		t.Errorf("Kind=Personal 应覆盖 Zone=us · 实际 body.region = %q", gotBody.Zone)
	}
}

// TestPurchase_EnterpriseUsesZone · Kind 非 Personal 时 · 保持 Zone 原样传下去
func TestPurchase_EnterpriseUsesZone(t *testing.T) {
	var gotBody purchaseReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_order_id":"c","order_id":"o","region":"us-east-1","purchased":1,"remaining":"0","status":"completed","refunded_amount_cny":"0","keys":[]}`))
	}))
	defer srv.Close()

	a, _ := New(Config{BaseURL: srv.URL, APIKey: "k"})
	zone := providers.ZoneUS
	_, _ = a.Purchase(context.Background(), providers.PurchaseRequest{
		Count:         1,
		ClientOrderID: "aabbccdd11223344aabbccdd11223344",
		Zone:          &zone,
	})
	if gotBody.Zone != "us" {
		t.Errorf("enterprise · body.region = %q · want us", gotBody.Zone)
	}
}

// TestCapability_HasPersonalAccountKind · Capability 声明双档
func TestCapability_HasPersonalAccountKind(t *testing.T) {
	a, _ := New(Config{BaseURL: "http://unused", APIKey: "k"})
	cap := a.Capability()
	if !cap.SupportsKind(providers.AccountPersonal) {
		t.Error("kirodrop 应声明支持 personal(I-03)")
	}
	if !cap.SupportsKind(providers.AccountEnterprise) {
		t.Error("kirodrop 应仍支持 enterprise")
	}
}
