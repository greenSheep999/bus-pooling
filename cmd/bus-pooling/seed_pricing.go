package main

// seed_pricing · 手动往 vendor_pricing 表塞一条换算规则(I-34 修部分 P1)。
//
// **为什么**:vendor_pricing 表存 vendor → 我方积分的换算规则(credits_per_unit)。
// 生产 seed 空时 · fallback (credit, 1_000_000) · USD 家 18.51 USD 会被当 18.51 积分展示。
// 用这个 CLI 补 · 确保 kirodrop 6_800_000(1 USD = 6.8 积分) 等 USD 家有值。
//
// 用法(vps22 · docker exec):
//   docker exec kirobus /app/bus-pooling seed-pricing -vendor kirodrop -currency USD -credits-per-unit 6800000
//
// 幂等:同 vendor 二次调覆盖(UPSERT)。

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/pricing"
)

func runSeedPricing(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("seed-pricing", flag.ContinueOnError)
	vendorID := fs.String("vendor", "", "vendor_id · kiro91/kiroceo/kirooo/kiroappio/kiroappcc/kirodrop")
	currency := fs.String("currency", "", "credit | CNY | USD")
	creditsPerUnit := fs.Int64("credits-per-unit", 0, "microunit · 1 单位 vendor 报价 = N microunit 积分(CNY 家 1_000_000 · USD 家含汇率如 6_800_000)")
	source := fs.String("source", "manual", "配置来源标记 · 落 vendor_pricing.source · 默认 manual")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *vendorID == "" || *currency == "" || *creditsPerUnit <= 0 {
		return errors.New("需要:-vendor + -currency + -credits-per-unit(> 0)")
	}

	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	store := pricing.NewStore(database.DB)
	if err := store.Upsert(ctx, pricing.VendorQuote{
		VendorID:       *vendorID,
		QuoteCurrency:  *currency,
		CreditsPerUnit: *creditsPerUnit,
		RateSource:     *source,
		Active:         true,
	}); err != nil {
		return fmt.Errorf("Upsert: %w", err)
	}
	fmt.Printf("done · vendor=%s currency=%s credits_per_unit=%d\n",
		*vendorID, *currency, *creditsPerUnit)
	return nil
}
