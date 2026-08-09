package pricing

import (
	"context"

	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// DeciderAdapter 把 pricing.Store 适配成 decider.PricingLookup。
// 装配层用：orch = decider.New(decider.Config{Pricing: pricing.NewDeciderAdapter(store)})
type DeciderAdapter struct {
	store *Store
}

func NewDeciderAdapter(store *Store) *DeciderAdapter {
	return &DeciderAdapter{store: store}
}

func (a *DeciderAdapter) QuoteFor(ctx context.Context, vendorID providers.VendorID) decider.VendorQuote {
	q := a.store.GetOrFallback(ctx, string(vendorID))
	return decider.VendorQuote{
		QuoteCurrency:     q.QuoteCurrency,
		CreditsPerUnit:    q.CreditsPerUnit,
		VendorSurchargeBp: q.VendorSurchargeBp,
	}
}
