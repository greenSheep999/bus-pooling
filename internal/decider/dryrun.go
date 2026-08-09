package decider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// DryRunVendor 是 DRY_RUN 模式下的假 vendor —— 不发外网请求，直接造假号。
//
// 用途：开发 / 联调 / CI 里让整条拉号闭环能跑通，不真扣款、不真占 vendor 库存。
// 生产环境**绝不该注册它**（config.DryRun=false 时装配层跳过）。
type DryRunVendor struct {
	VendorID     providers.VendorID
	UnitPrice    int64 // microunit · 假单价
	MaxAvailable int   // 每次 Stock 报的可购量
	orderSeq     atomic.Uint64
	keySeq       atomic.Uint64
}

func (d *DryRunVendor) ID() providers.VendorID { return d.VendorID }

func (d *DryRunVendor) Capability() providers.Capability {
	return providers.Capability{
		SupportsIdempotency: true,
		SupportsZones:       true,
		SupportsWebhook:     false,
		HasWarranty:         false,
		KeyPayloadShape:     providers.KeyPayloadFourTuple,
		MinPerOrder:         1,
		MaxPerOrder:         200,
	}
}

func (d *DryRunVendor) Stock(_ context.Context, _ providers.StockOptions) (*providers.StockSnapshot, error) {
	unit := d.UnitPrice
	if unit <= 0 {
		unit = 30 * 1_000_000 // 30 积分作为占位单价，测试可读
	}
	avail := d.MaxAvailable
	if avail <= 0 {
		avail = 100
	}
	return &providers.StockSnapshot{
		VendorID:    d.VendorID,
		ObservedAt:  time.Now().UTC(),
		Available:   avail,
		MinPerOrder: 1,
		MaxPerOrder: 200,
		Zones: []providers.ZoneStock{{
			Zone: providers.ZoneUS, Region: "us-east-1-dryrun",
			Available: avail,
			UnitPrice: providers.Money{Amount: unit, Currency: providers.CurrencyCredit},
		}},
	}, nil
}

func (d *DryRunVendor) Purchase(_ context.Context, req providers.PurchaseRequest) (*providers.PurchaseResult, error) {
	unit := d.UnitPrice
	if unit <= 0 {
		unit = 30 * 1_000_000
	}
	orderID := fmt.Sprintf("dryrun-ord-%d", d.orderSeq.Add(1))
	keys := make([]providers.KeyPayload, req.Count)
	for i := range keys {
		id := d.keySeq.Add(1)
		keys[i] = providers.KeyPayload{
			VendorKeyID: fmt.Sprintf("dryrun-key-%d", id),
			Key:         "DRYRUN_" + randHex(16),
			Account:     fmt.Sprintf("dryrun-%d@example.invalid", id),
			Password:    "",
			IssuerURL:   "https://dryrun.example.invalid/start",
			Paid:        providers.Money{Amount: unit, Currency: providers.CurrencyCredit},
		}
	}
	return &providers.PurchaseResult{
		ClientOrderID: req.ClientOrderID,
		VendorOrderID: orderID,
		Zone:          providers.ZoneUS,
		Requested:     req.Count,
		Purchased:     req.Count,
		Keys:          keys,
		UnitPrice:     providers.Money{Amount: unit, Currency: providers.CurrencyCredit},
		TotalCost:     providers.Money{Amount: unit * int64(req.Count), Currency: providers.CurrencyCredit},
		Remaining:     providers.Money{Amount: 999_999 * int64(1_000_000), Currency: providers.CurrencyCredit},
	}, nil
}

func (d *DryRunVendor) OrderKeys(_ context.Context, orderID string) (*providers.PurchaseResult, error) {
	// DRY_RUN 下不需要补拉：主路径不会崩，janitor 也不会真扫到 purchased
	return nil, &providers.APIError{
		VendorID: d.VendorID, Sentinel: providers.ErrNotFound,
		Message: "dryrun: 没有历史订单缓存",
	}
}

// DryRunPool 是 DRY_RUN 下的假号池 —— 假号进假池，不污染真 kiro.rs。
//
// 用 nano 时间戳做 id 起点 —— in-memory 计数器重启会归零，但 credential_ledger
// 里那些老行还在，会撞 UNIQUE 约束。用 UnixNano 单调递增，重启不复用。
type DryRunPool struct {
	nextID atomic.Uint64
}

func (p *DryRunPool) BatchImport(_ context.Context, req housepool.BatchImportRequest) (*housepool.BatchImportResult, error) {
	ev := make(chan housepool.BatchImportEvent, len(req.Credentials)+1)
	sum := make(chan housepool.BatchImportSummary, 1)
	// 首次调用把种子设成 UnixNano · 之后每次拉都往上加 1
	p.nextID.CompareAndSwap(0, uint64(time.Now().UnixNano()))
	for i := range req.Credentials {
		idx := i
		cid := housepool.CredentialID(p.nextID.Add(1))
		ev <- housepool.BatchImportEvent{
			Index: &idx, Status: housepool.ImportStatusVerified, CredentialID: &cid,
			Email: req.Credentials[i].Email,
		}
	}
	close(ev)
	sum <- housepool.BatchImportSummary{
		Total: len(req.Credentials), Imported: len(req.Credentials),
		Verified: len(req.Credentials),
	}
	close(sum)
	return &housepool.BatchImportResult{
		Events: ev, Summary: sum, Err: func() error { return nil },
	}, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
