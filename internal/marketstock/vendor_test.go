package marketstock

import (
	"context"
	"strconv"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 完整生命周期 · Stock → Purchase → SellTx · 状态转换正确
func TestVendor_全链路(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	_ = seed(t, s) // 3 把 · 1个50 / 10起40
	v := NewVendor(s)
	ctx := context.Background()

	// 1) Stock(personal) 数出可用数
	snap, err := v.Stock(ctx, providers.StockOptions{Kind: providers.AccountPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Available != 3 {
		t.Errorf("Stock.Available=%d want 3", snap.Available)
	}
	if got := snap.Zones[0].UnitPrice.Amount; got != 50_000_000 {
		t.Errorf("Stock 单价=%d want 50_000_000", got)
	}
	if snap.Zones[0].Zone != providers.ZoneGeneral {
		t.Errorf("手工池必须 general 无区 · got %q", snap.Zones[0].Zone)
	}

	// 2) Purchase(count=2, personal) · 落到 1-9 档 → 单价 50
	pr, err := v.Purchase(ctx, providers.PurchaseRequest{
		Count:         2,
		ClientOrderID: "coid-abc",
		Kind:          providers.AccountPersonal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Purchased != 2 {
		t.Fatalf("Purchased=%d want 2", pr.Purchased)
	}
	if pr.UnitPrice.Amount != 50_000_000 {
		t.Errorf("UnitPrice=%d want 50M", pr.UnitPrice.Amount)
	}
	if pr.TotalCost.Amount != 100_000_000 {
		t.Errorf("TotalCost=%d want 100M", pr.TotalCost.Amount)
	}

	// 3) Meta 里有 stock_item id + source · orchestrator 靠它落 ledger
	meta, err := UnpackMeta(pr.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil {
		t.Fatal("meta 不该为 nil · Purchase 是本包出的")
	}
	if len(meta.StockItemIDs) != 2 {
		t.Errorf("StockItemIDs=%d want 2", len(meta.StockItemIDs))
	}
	if meta.Source != "SuperMan" {
		t.Errorf("Source=%q want SuperMan", meta.Source)
	}

	// 4) KeyPayload.Key = credential id 字符串
	for i, k := range pr.Keys {
		if _, err := strconv.ParseUint(k.Key, 10, 64); err != nil {
			t.Errorf("Keys[%d].Key=%q 应该是数字 credential id · err=%v", i, k.Key, err)
		}
	}

	// 5) Stock 应少 2（reserved 不算 available）
	snap2, _ := v.Stock(ctx, providers.StockOptions{Kind: providers.AccountPersonal})
	if snap2.Available != 1 {
		t.Errorf("Purchase 后 Available=%d want 1", snap2.Available)
	}

	// 6) SellTx 落地 · reserved → sold
	tx, _ := db.Begin()
	for i, sid := range meta.StockItemIDs {
		if err := s.SellTx(ctx, tx, sid, "ledger-"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// 7) Available 仍是 1（sold 也不算）
	snap3, _ := v.Stock(ctx, providers.StockOptions{Kind: providers.AccountPersonal})
	if snap3.Available != 1 {
		t.Errorf("SellTx 后 Available=%d want 1", snap3.Available)
	}
}

// 数量分档 · Purchase(count=10) 应落 10 起 40 档
func TestVendor_数量分档跳档(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	// 上 10 把 · 一次买 10 应走 40 档
	offerID, _ := s.UpsertOffer(ctx, UpsertOfferInput{
		VendorID:     "kiro_market",
		AccountKind:  providers.AccountPersonal,
		Subscription: providers.PlanPro,
		PriceBands: []providers.QtyPriceBand{
			{Lower: 1, Upper: 9, UnitPriceCredits: 50_000_000},
			{Lower: 10, Upper: 0, UnitPriceCredits: 40_000_000},
		},
		Enabled: true,
		Source:  "SuperMan",
	})
	for i := uint64(0); i < 10; i++ {
		_, _ = s.AddItem(ctx, AddItemInput{
			OfferID: offerID, KiroRSCredentialID: 20000 + i, ImportedBy: "op",
		})
	}
	v := NewVendor(s)
	pr, err := v.Purchase(ctx, providers.PurchaseRequest{
		Count:         10,
		ClientOrderID: "coid-jumping",
		Kind:          providers.AccountPersonal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pr.UnitPrice.Amount != 40_000_000 {
		t.Errorf("10 起应走 40 档 · got %d", pr.UnitPrice.Amount)
	}
	if pr.TotalCost.Amount != 400_000_000 {
		t.Errorf("Total = 10×40 = 400M · got %d", pr.TotalCost.Amount)
	}
}

// OrderKeys · 幂等补拉 · 用 client_order_id 反查
func TestVendor_OrderKeys反查(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	seed(t, s)
	v := NewVendor(s)
	ctx := context.Background()

	pr, err := v.Purchase(ctx, providers.PurchaseRequest{
		Count: 2, ClientOrderID: "coid-recover", Kind: providers.AccountPersonal,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 模拟崩溃 · 用 client_order_id 反查
	pr2, err := v.OrderKeys(ctx, "coid-recover")
	if err != nil {
		t.Fatal(err)
	}
	if pr2.Purchased != pr.Purchased {
		t.Errorf("OrderKeys 反查 %d 把 · 原 %d 把", pr2.Purchased, pr.Purchased)
	}
	meta, _ := UnpackMeta(pr2.Raw)
	if meta == nil || len(meta.StockItemIDs) != 2 {
		t.Error("OrderKeys 应能反查出 stock_item id")
	}
}

// UnpackMeta · 别家 vendor 的 raw 不应解成 meta
func TestUnpackMeta_不误伤别家(t *testing.T) {
	// 别家 vendor 的响应体 · JSON 但没有 already_in_housepool
	raw := []byte(`{"vendor_order_id":"vo-123","zones":[]}`)
	meta, err := UnpackMeta(raw)
	if err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		t.Errorf("别家 raw 不该解成 marketstock.Meta · got %+v", meta)
	}
	// 空 raw 也稳
	if m, _ := UnpackMeta(nil); m != nil {
		t.Error("nil raw 应返 nil")
	}
}
