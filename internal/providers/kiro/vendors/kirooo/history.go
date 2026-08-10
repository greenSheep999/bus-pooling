package kirooo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// kirooo 的 /api/my/purchase-orders 返数组 · /api/my/keys?history=1 返 {count, keys[], active, suspect}
// 全量拉 · 无分页 · 每次都从头拿完整历史（vendor 侧量不大）

type orderRow struct {
	ClientOrderID string `json:"client_order_id"`
	CreatedAt     string `json:"created_at"` // "2026-08-09 17:14:05"（本地格式）
	Purchased     int    `json:"purchased"`
	Requested     int    `json:"requested"`
	Source        string `json:"source"`
}

type keysWrap struct {
	Active  int      `json:"active"`
	Count   int      `json:"count"`
	Suspect int      `json:"suspect"`
	Keys    []keyRow `json:"keys"`
}

type keyRow struct {
	ID            int64  `json:"id"`
	Key           string `json:"key"`
	Region        string `json:"region"`
	Status        string `json:"status"` // active / dead / suspect
	OrderID       string `json:"order_id"`
	MasterID      string `json:"master_id"`
	CreatedAt     string `json:"created_at"`
	DispatchedAt  string `json:"dispatched_at"`
	DeadReason    string `json:"dead_reason"`
	LastProbe     string `json:"last_probe"`
	CurrentUsage  int    `json:"current_usage"`
	UsageLimit    int    `json:"usage_limit"`
	UsageRate     int    `json:"usage_rate"`
	ListingPrice  int64  `json:"listing_price"`
	OnSale        bool   `json:"on_sale"`
}

const kiroooTimeLayout = "2006-01-02 15:04:05"

func parseKiroooTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(kiroooTimeLayout, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return k
	}
	return k[:8] + "****"
}

// ListOrders 拉全部历史订单 · vendor 侧无分页 · NextCursor 恒空
func (a *Adapter) ListOrders(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorOrder], error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/purchase-orders", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kirooo purchase-orders: http %d", resp.StatusCode)
	}

	var rows []orderRow
	if err := json.Unmarshal(resp.Body, &rows); err != nil {
		return nil, fmt.Errorf("kirooo purchase-orders 解析: %w", err)
	}

	out := make([]providers.VendorOrder, 0, len(rows))
	for _, r := range rows {
		raw, _ := json.Marshal(r)
		out = append(out, providers.VendorOrder{
			VendorOrderID: r.ClientOrderID,
			CreatedAt:     parseKiroooTime(r.CreatedAt),
			Purchased:     r.Purchased,
			Requested:     r.Requested,
			Source:        r.Source,
			Raw:           raw,
		})
	}
	return &providers.HistoryPage[providers.VendorOrder]{Items: out}, nil
}

// ListKeys 拉全部 key 生命周期（含已挂）· 无分页
func (a *Adapter) ListKeys(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorKey], error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/keys?history=1", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kirooo keys: http %d", resp.StatusCode)
	}

	var wrap keysWrap
	if err := json.Unmarshal(resp.Body, &wrap); err != nil {
		return nil, fmt.Errorf("kirooo keys 解析: %w", err)
	}

	out := make([]providers.VendorKey, 0, len(wrap.Keys))
	for _, k := range wrap.Keys {
		raw, _ := json.Marshal(k)
		vk := providers.VendorKey{
			VendorKeyID:   fmt.Sprintf("%d", k.ID),
			OrderID:       k.OrderID,
			KeyMasked:     maskKey(k.Key),
			Region:        k.Region,
			Status:        k.Status,
			CreatedAt:     parseKiroooTime(k.CreatedAt),
			DispatchedAt:  parseKiroooTime(k.DispatchedAt),
			DeadReason:    k.DeadReason,
			LastProbeAt:   parseKiroooTime(k.LastProbe),
			CurrentUsage:  k.CurrentUsage,
			UsageLimit:    k.UsageLimit,
			Raw:           raw,
		}
		// 挂的：kirooo 用 status=dead + dead_reason 表达 · 没单独 dead_at 字段
		// 用 last_probe 当近似 dead_at（vendor 最后一次探测发现挂了的时刻）
		if k.Status == "dead" {
			vk.DeadAt = parseKiroooTime(k.LastProbe)
		}
		out = append(out, vk)
	}
	return &providers.HistoryPage[providers.VendorKey]{Items: out}, nil
}
