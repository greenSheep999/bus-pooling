package decider

import (
	"context"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 编译期确认实现了 decider 需要的窄接口 —— 装配层直接扔进 Config
var (
	_ VendorClient = (*DryRunVendor)(nil)
	_ PoolClient   = (*DryRunPool)(nil)
)

func TestDryRunVendorPurchaseReturnsRequestedCount(t *testing.T) {
	v := &DryRunVendor{VendorID: providers.Vendor91Kiro}
	got, err := v.Purchase(context.Background(), providers.PurchaseRequest{
		Count: 5, ClientOrderID: "a1b2c3d4e5f60718293a4b5c6d7e8f90",
	})
	if err != nil {
		t.Fatalf("Purchase: %v", err)
	}
	if got.Purchased != 5 || len(got.Keys) != 5 {
		t.Errorf("Purchased/keys = %d/%d，want 5/5", got.Purchased, len(got.Keys))
	}
	// TotalCost 恒等于 单价 × 数量 —— DRY_RUN 也要满足这条会计不变量
	if got.TotalCost.Amount != got.UnitPrice.Amount*int64(got.Purchased) {
		t.Errorf("TotalCost = %d，应恒等 unit × count", got.TotalCost.Amount)
	}
}

func TestDryRunPoolReportsAllVerified(t *testing.T) {
	p := &DryRunPool{}
	req := housepool.BatchImportRequest{
		Credentials: []housepool.ImportCredential{
			{KiroAPIKey: "k1"}, {KiroAPIKey: "k2"}, {KiroAPIKey: "k3"},
		},
	}
	res, err := p.BatchImport(context.Background(), req)
	if err != nil {
		t.Fatalf("BatchImport: %v", err)
	}
	n := 0
	for ev := range res.Events {
		if ev.Status != housepool.ImportStatusVerified {
			t.Errorf("status = %q，DRY_RUN 应全部 verified", ev.Status)
		}
		n++
	}
	if n != 3 {
		t.Errorf("事件数 = %d, want 3", n)
	}
	<-res.Summary
	if err := res.Err(); err != nil {
		t.Errorf("Err = %v", err)
	}
}
