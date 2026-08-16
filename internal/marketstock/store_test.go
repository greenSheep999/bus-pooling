package marketstock

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// openTestDB · 建临时库并 migrate 到最新（含 047）
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := dbpkg.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MigrateUp(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d.DB
}

// 一个 offer + 3 把库存
func seed(t *testing.T, s *Store) *Offer {
	t.Helper()
	ctx := context.Background()
	offerID, err := s.UpsertOffer(ctx, UpsertOfferInput{
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
	if err != nil {
		t.Fatal(err)
	}
	for i := uint64(1); i <= 3; i++ {
		if _, err := s.AddItem(ctx, AddItemInput{
			OfferID: offerID, KiroRSCredentialID: 10000 + i, ImportedBy: "op",
		}); err != nil {
			t.Fatal(err)
		}
	}
	o, err := s.FindOffer(ctx, "kiro_market",
		providers.AccountPersonal, providers.PlanPro)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// 单价按数量分档 · 边界值别算错
func TestOffer_UnitPriceFor(t *testing.T) {
	o := Offer{PriceBands: []providers.QtyPriceBand{
		{Lower: 1, Upper: 9, UnitPriceCredits: 50_000_000},
		{Lower: 10, Upper: 0, UnitPriceCredits: 40_000_000},
	}}
	cases := []struct {
		count int
		want  int64
	}{
		{1, 50_000_000},
		{9, 50_000_000},
		{10, 40_000_000},
		{100, 40_000_000},
	}
	for _, c := range cases {
		if got := o.UnitPriceFor(c.count); got != c.want {
			t.Errorf("UnitPriceFor(%d)=%d want %d", c.count, got, c.want)
		}
	}
}

// Reserve → SellTx 主流程
func TestReserveAndSell(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	o := seed(t, s)
	ctx := context.Background()

	items, err := s.Reserve(ctx, o.ID, "pending-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("reserved %d want 2", len(items))
	}
	if items[0].Source != "SuperMan" {
		t.Errorf("source = %q want SuperMan", items[0].Source)
	}

	// Available 少了 2
	n, _ := s.AvailableCount(ctx, o.ID)
	if n != 1 {
		t.Errorf("available=%d want 1", n)
	}

	// Sell（模拟 settle tx）
	tx, _ := db.Begin()
	if err := s.SellTx(ctx, tx, items[0].StockItemID, "ledger-1"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Sell 后不能再 sell
	tx2, _ := db.Begin()
	err = s.SellTx(ctx, tx2, items[0].StockItemID, "ledger-2")
	_ = tx2.Rollback()
	if err == nil {
		t.Fatal("重复 sell 应该失败")
	}
}

// 并发抢货 · 10 个 goroutine · 3 把库存 · 加起来不能超卖
func TestReserve_并发不超卖(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	o := seed(t, s) // 3 把
	ctx := context.Background()

	const workers = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	var reservedTotal int

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			items, err := s.Reserve(ctx, o.ID, "pending-"+string(rune('a'+i)), 1)
			if errors.Is(err, ErrNoStock) {
				return
			}
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			mu.Lock()
			reservedTotal += len(items)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if reservedTotal != 3 {
		t.Errorf("并发 reserve 拿到 %d 把 · 库存只有 3 把", reservedTotal)
	}
	// 库存清零
	n, _ := s.AvailableCount(ctx, o.ID)
	if n != 0 {
		t.Errorf("available=%d want 0", n)
	}
}

// Release 归还
func TestRelease(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	o := seed(t, s)
	ctx := context.Background()

	items, _ := s.Reserve(ctx, o.ID, "p1", 2)
	if err := s.Release(ctx, items[0].StockItemID); err != nil {
		t.Fatal(err)
	}
	n, _ := s.AvailableCount(ctx, o.ID)
	if n != 2 {
		t.Errorf("release 后 available=%d want 2", n)
	}
	// 幂等 · release 已 available 的号也不报错
	if err := s.Release(ctx, items[0].StockItemID); err != nil {
		t.Errorf("重复 release 应该幂等 · got %v", err)
	}
}

// ReleaseByPending 批量释放
func TestReleaseByPending(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	o := seed(t, s)
	ctx := context.Background()

	_, _ = s.Reserve(ctx, o.ID, "p1", 2)
	n, err := s.ReleaseByPending(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("release %d want 2", n)
	}
	avail, _ := s.AvailableCount(ctx, o.ID)
	if avail != 3 {
		t.Errorf("available=%d want 3", avail)
	}
}

// 超时 sweeper · reserved 超过 TTL 自动释放
func TestSweepExpired(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	o := seed(t, s)
	ctx := context.Background()

	items, _ := s.Reserve(ctx, o.ID, "p1", 1)
	// 手工把 reserved_at 往回拨到过期外
	past := time.Now().UTC().Add(-2 * ReserveTTL).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx,
		`UPDATE market_stock_item SET reserved_at=? WHERE id=?`,
		past, items[0].StockItemID); err != nil {
		t.Fatal(err)
	}
	n, err := s.SweepExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("sweep=%d want 1", n)
	}
	avail, _ := s.AvailableCount(ctx, o.ID)
	if avail != 3 {
		t.Errorf("sweep 后 available=%d want 3", avail)
	}
}

// 分档校验（validateBands）· 有空洞 / 顺序错都要拒
func TestValidateBands(t *testing.T) {
	good := []providers.QtyPriceBand{
		{Lower: 1, Upper: 9, UnitPriceCredits: 50_000_000},
		{Lower: 10, Upper: 0, UnitPriceCredits: 40_000_000},
	}
	if err := validateBands(good); err != nil {
		t.Errorf("合法分档应通过 · got %v", err)
	}
	bad := [][]providers.QtyPriceBand{
		nil,
		{{Lower: 1, Upper: 5, UnitPriceCredits: 50_000_000},
			{Lower: 3, Upper: 0, UnitPriceCredits: 40_000_000}}, // 顺序乱
		{{Lower: 1, Upper: 0, UnitPriceCredits: 50_000_000},
			{Lower: 10, Upper: 0, UnitPriceCredits: 40_000_000}}, // Upper=0 不在最后
		{{Lower: 1, Upper: 9, UnitPriceCredits: 0}}, // 价 0
	}
	for i, b := range bad {
		if err := validateBands(b); err == nil {
			t.Errorf("case %d 应拒 · 反而通过", i)
		}
	}
}

// FindOffer · 找不到时返 ErrOfferMissing
func TestFindOffer_未启用不返(t *testing.T) {
	db := openTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	_, err := s.UpsertOffer(ctx, UpsertOfferInput{
		VendorID:     "kiro_market",
		AccountKind:  providers.AccountPersonal,
		Subscription: providers.PlanPro,
		PriceBands: []providers.QtyPriceBand{
			{Lower: 1, Upper: 0, UnitPriceCredits: 50_000_000},
		},
		Enabled: false, // 禁用
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.FindOffer(ctx, "kiro_market",
		providers.AccountPersonal, providers.PlanPro)
	if !errors.Is(err, ErrOfferMissing) {
		t.Errorf("禁用的 offer 应返 ErrOfferMissing · got %v", err)
	}
}
