package main

// marketreader.go · 装配层 · 让 marketstock.Store 满足 vendorview.MarketOfferReader
//
// vendorview 不 import marketstock（打破包循环 · 保持视图层只依赖 providers 契约）·
// marketstock 也不 import vendorview（同样理由 · 保持核心业务不知道对外视图）·
// 主装配层在这里桥接:实现 vendorview.MarketOfferReader 接口 · 内部转发到 marketstock.Store。

import (
	"context"

	"github.com/bus-pooling/bus-pooling/internal/marketstock"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
)

// marketReaderAdapter · 只在装配层用 · 不给业务链引
type marketReaderAdapter struct{ store *marketstock.Store }

// 编译期断言:实现了 vendorview.MarketOfferReader 接口（缺方法立即编译错）
var _ vendorview.MarketOfferReader = (*marketReaderAdapter)(nil)

func newMarketReader(s *marketstock.Store) *marketReaderAdapter {
	if s == nil {
		return nil
	}
	return &marketReaderAdapter{store: s}
}

func (a *marketReaderAdapter) ListOffers(ctx context.Context) ([]vendorview.MarketOffer, error) {
	rows, err := a.store.ListOffers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]vendorview.MarketOffer, 0, len(rows))
	for _, o := range rows {
		out = append(out, vendorview.MarketOffer{
			ID: o.ID, VendorID: o.VendorID,
			AccountKind: o.AccountKind, Subscription: o.Subscription,
			PriceBands: o.PriceBands, Enabled: o.Enabled, Source: o.Source,
		})
	}
	return out, nil
}

func (a *marketReaderAdapter) AvailableCount(ctx context.Context, offerID string) (int, error) {
	return a.store.AvailableCount(ctx, offerID)
}
