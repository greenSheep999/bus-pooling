package pricing

import (
	"context"
	"errors"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/vp.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return NewStore(d.DB)
}

// CNY 家 · 1 单位 vendor 报价 = 1 积分 = 1_000_000 microunit
func TestConvert_CNYPassthrough(t *testing.T) {
	q := VendorQuote{
		QuoteCurrency: "CNY", CreditsPerUnit: 1_000_000,
	}
	got := q.ConvertToCredits(30)
	if got != 30_000_000 {
		t.Errorf("30 CNY → %d microunit · want 30_000_000", got)
	}
}

// USD 家 · 1 USD = 7 CNY = 7 积分 = 7_000_000 microunit
func TestConvert_USDByRate(t *testing.T) {
	q := VendorQuote{
		QuoteCurrency: "USD", CreditsPerUnit: 7_000_000,
	}
	got := q.ConvertToCredits(5)
	if got != 35_000_000 {
		t.Errorf("5 USD → %d microunit · want 35_000_000（5 × 7）", got)
	}
}

// Upsert 覆盖同 vendor · 保留 primary key
func TestUpsert_OverwritesInPlace(t *testing.T) {
	s := testStore(t)
	q := VendorQuote{
		VendorID: "vtest", QuoteCurrency: "CNY",
		CreditsPerUnit: 1_000_000, Active: true,
	}
	if err := s.Upsert(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	q.CreditsPerUnit = 8_000_000
	q.QuoteCurrency = "USD"
	if err := s.Upsert(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), "vtest")
	if err != nil {
		t.Fatal(err)
	}
	if got.CreditsPerUnit != 8_000_000 || got.QuoteCurrency != "USD" {
		t.Errorf("Upsert 未覆盖 · got=%+v", got)
	}
}

// active=0 的行不出现在 Get · 走 fallback
func TestGet_InactiveIsNotFound(t *testing.T) {
	s := testStore(t)
	if err := s.Upsert(context.Background(), VendorQuote{
		VendorID: "closed", QuoteCurrency: "CNY",
		CreditsPerUnit: 1_000_000, Active: false,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(context.Background(), "closed")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("active=0 应返 ErrNotFound · got=%v", err)
	}
}

// GetOrFallback · 未配 vendor 走 fallback（CNY 1:1）
func TestGetOrFallback_UsesDefaultWhenAbsent(t *testing.T) {
	s := testStore(t)
	q := s.GetOrFallback(context.Background(), "notconfigured")
	if q.QuoteCurrency != "CNY" || q.CreditsPerUnit != 1_000_000 {
		t.Errorf("fallback = %+v · want CNY 1:1", q)
	}
	if q.RateSource != "fallback" {
		t.Errorf("fallback.RateSource = %q · want fallback", q.RateSource)
	}
}

// 非法 quote_currency · CHECK 挡住
func TestUpsert_RejectsBadCurrency(t *testing.T) {
	s := testStore(t)
	err := s.Upsert(context.Background(), VendorQuote{
		VendorID: "bad", QuoteCurrency: "JPY",
		CreditsPerUnit: 1_000_000, Active: true,
	})
	if err == nil {
		t.Error("非法 quote_currency 应报错")
	}
}
