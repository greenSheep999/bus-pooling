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

// TestPurchase_MaxTotalCNY · 涨价保护 · req.MaxTotal 非零时以 CNY 字符串落 body。
//
// vendor 端行为：价格超过 max_total_cny 时返 409 且不扣款（见 docs/vendors/drop-kiro-ss.md）。
// 本测只验**请求体格式对** · vendor 侧行为交 vendor 保证。
func TestPurchase_MaxTotalCNY_ThreadedThrough(t *testing.T) {
	var gotBody purchaseReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/my/purchase" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		// 简单假响应 · 让 unmarshal 不炸
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_order_id":"c","order_id":"o","region":"us-east-1","purchased":1,"remaining":"100","status":"completed","refunded_amount_cny":"0","keys":[{"key":"k","region":"us-east-1"}]}`))
	}))
	defer srv.Close()

	a, err := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}

	// case 1 · req.MaxTotal 非零 → body.max_total_cny 应有值
	amt := int64(884_400_000) // 884.400000 CNY
	_, _ = a.Purchase(context.Background(), providers.PurchaseRequest{
		Count:         2,
		ClientOrderID: "0123456789abcdef0123456789abcdef",
		MaxTotal:      &providers.Money{Amount: amt, Currency: providers.CurrencyCNY},
	})
	if gotBody.MaxTotalCNY != "884.400000" {
		t.Errorf("涨价保护 · body.max_total_cny 应 884.400000 · 得 %q", gotBody.MaxTotalCNY)
	}

	// case 2 · req.MaxTotal nil → body 里没这字段
	gotBody = purchaseReq{}
	_, _ = a.Purchase(context.Background(), providers.PurchaseRequest{
		Count:         2,
		ClientOrderID: "0123456789abcdef0123456789abcde0",
	})
	if gotBody.MaxTotalCNY != "" {
		t.Errorf("未传 MaxTotal · body.max_total_cny 应空 · 得 %q", gotBody.MaxTotalCNY)
	}

	// case 3 · req.MaxTotal 传 zero Money → 视为不设保护
	gotBody = purchaseReq{}
	_, _ = a.Purchase(context.Background(), providers.PurchaseRequest{
		Count:         2,
		ClientOrderID: "0123456789abcdef0123456789abcde1",
		MaxTotal:      &providers.Money{Amount: 0, Currency: providers.CurrencyCNY},
	})
	if gotBody.MaxTotalCNY != "" {
		t.Errorf("MaxTotal=0 应视为不设 · body.max_total_cny 应空 · 得 %q", gotBody.MaxTotalCNY)
	}
}
