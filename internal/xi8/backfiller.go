package xi8

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// DispatchStore · 我方 vendor_dispatch 落库接口（避免循环依赖 · 上层传 vendorview.OrderKeyStore）
type DispatchStore interface {
	UpsertDispatches(ctx context.Context, vendorID, source string, ds []providers.VendorDispatch) error
}

// Backfiller · xi8 一次性回填 · 只做后端对账 + 历史空窗填。
//
// 跟 vendorview.Backfiller 分工：
//   - vendorview.Backfiller · 拉 vendor 自家 fleet 端点 · source=vendor_self · 5min 一轮
//   - xi8.Backfiller         · 拉 xi8 restock-log + signals · source=xi8 · 手动 CLI 触发 or 定时
//
// 前端读路径全部只查 source=vendor_self · xi8 行只做**内部对账 + 历史空窗填**（CLAUDE.md §0.1）。
type Backfiller struct {
	client *Client
	store  DispatchStore
	logger *slog.Logger
}

func NewBackfiller(client *Client, store DispatchStore, logger *slog.Logger) *Backfiller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Backfiller{client: client, store: store, logger: logger}
}

// RunOnce · 拉一次 xi8 数据 · 落 vendor_dispatch source='xi8'。
//
// 步骤：
//  1. 先拉 signals（有 vendor_order_id · 最准 · 优先）· dispatch_key = "xi8-sig-{order_id}"
//  2. 再拉 restock-log（推算 · 兜底 · 覆盖 signals 没接的 vendor）· dispatch_key = "xi8-log-{vendor}-{region}-{iso}"
//  3. 同一 dispatch_key 幂等 upsert · 反复跑不重
//
// 返 (signalsCount, restocksCount) · 便于 CLI 打进度。
func (b *Backfiller) RunOnce(ctx context.Context, limit int) (int, int, error) {
	if b.client == nil {
		return 0, 0, fmt.Errorf("xi8: client nil · 未 seed API key")
	}

	// 1. signals（精准源 · 有 vendor_order_id）
	sigCount := 0
	if signals, err := b.client.ListSignals(ctx, limit); err == nil {
		byVendor := make(map[string][]providers.VendorDispatch)
		for _, s := range signals.Signals {
			slug := VendorSlugForXi8ID(s.VendorID)
			if slug == "" {
				b.logger.Warn("xi8: signal 未映射 vendor · 跳过", "xi8_vendor_id", s.VendorID, "name", s.Name)
				continue
			}
			t, err := parseXi8Time(s.At.ISO)
			if err != nil {
				continue
			}
			raw, _ := json.Marshal(s)
			d := providers.VendorDispatch{
				DispatchKey:  "xi8-sig-" + s.VendorOrderID,
				Region:       xi8RegionToOurs(joinRegions(s.Regions)),
				DispatchedAt: t,
				Count:        s.Count,
				Alive:        s.Count,
				Status:       "running",
				Raw:          raw,
			}
			byVendor[slug] = append(byVendor[slug], d)
			sigCount++
		}
		for slug, ds := range byVendor {
			if err := b.store.UpsertDispatches(ctx, slug, "xi8", ds); err != nil {
				b.logger.Warn("xi8: upsert signals 失败", "vendor", slug, "err", err)
			}
		}
	} else {
		b.logger.Warn("xi8: 拉 signals 失败 · 继续 restock-log", "err", err)
	}

	// 2. restock-log（推算源 · 覆盖广 · 兜底）
	rsCount := 0
	logs, err := b.client.ListRestockLog(ctx, limit)
	if err != nil {
		return sigCount, 0, fmt.Errorf("xi8: 拉 restock-log: %w", err)
	}
	byVendor := make(map[string][]providers.VendorDispatch)
	for _, r := range logs.Rows {
		slug := VendorSlugForXi8ID(r.VendorID)
		if slug == "" {
			b.logger.Warn("xi8: restock 未映射 vendor · 跳过", "xi8_vendor_id", r.VendorID, "name", r.Name)
			continue
		}
		t, err := parseXi8Time(r.At.ISO)
		if err != nil {
			continue
		}
		raw, _ := json.Marshal(r)
		d := providers.VendorDispatch{
			DispatchKey:  fmt.Sprintf("xi8-log-%s-%s", xi8RegionToOurs(r.Region), t.UTC().Format("20060102T150405Z")),
			Region:       xi8RegionToOurs(r.Region),
			DispatchedAt: t,
			Count:        r.Added,
			Alive:        r.Added,
			Status:       "running",
			Raw:          raw,
		}
		byVendor[slug] = append(byVendor[slug], d)
		rsCount++
	}
	for slug, ds := range byVendor {
		if err := b.store.UpsertDispatches(ctx, slug, "xi8", ds); err != nil {
			b.logger.Warn("xi8: upsert restock-log 失败", "vendor", slug, "err", err)
		}
	}
	b.logger.Info("xi8: backfill 完成", "signals", sigCount, "restocks", rsCount)
	return sigCount, rsCount, nil
}

// parseXi8Time · xi8 iso 字段带 +08:00 · Parse 后 .UTC() 才对。
func parseXi8Time(iso string) (time.Time, error) {
	if iso == "" {
		return time.Time{}, fmt.Errorf("empty iso")
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// xi8RegionToOurs · xi8 用 "us" / "eu" · 我方用 "us-east-1" / "eu-central-1"
// 未来若某家 vendor 有更多 region · 需扩这个映射。
func xi8RegionToOurs(r string) string {
	switch r {
	case "us":
		return "us-east-1"
	case "eu":
		return "eu-central-1"
	default:
		return r
	}
}

func joinRegions(rs []string) string {
	if len(rs) == 0 {
		return ""
	}
	return rs[0] // signals 通常一个批次一个 region · 取第一个
}
