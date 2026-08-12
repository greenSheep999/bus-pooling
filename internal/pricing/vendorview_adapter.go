package pricing

import "context"

// VendorViewLookup · Prober 用的 pricing 适配器（docs/18 §1.3）
//
// 跟 DeciderAdapter 区别：Prober 用 (currency, credits_per_unit) 双返值 · Decider 用
// providers.VendorQuote 大结构。类型窄化让消费侧不用依赖整个 VendorQuote。
type VendorViewLookup struct {
	store *Store
}

func NewVendorViewLookup(store *Store) *VendorViewLookup {
	return &VendorViewLookup{store: store}
}

// QuoteFor · 返 (currency, credits_per_unit) · 找不到 fallback (credit, 1_000_000)
func (a *VendorViewLookup) QuoteFor(ctx context.Context, vendorID string) (string, int64) {
	if a == nil || a.store == nil {
		return "credit", 1_000_000
	}
	q, err := a.store.Get(ctx, vendorID)
	if err != nil {
		return "credit", 1_000_000
	}
	return q.QuoteCurrency, q.CreditsPerUnit
}
