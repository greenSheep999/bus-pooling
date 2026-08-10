package kiroceo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 本 vendor 跟 本 vendor 端点格式几乎一样（vendor 是同一家的 fork） · 复用相同解析。

type orderRow struct {
	ClientOrderID string `json:"client_order_id"`
	CreatedAt     string `json:"created_at"`
	Purchased     int    `json:"purchased"`
	Requested     int    `json:"requested"`
	Source        string `json:"source"`
}

type keysWrap struct {
	Active int      `json:"active"`
	Count  int      `json:"count"`
	Keys   []keyRow `json:"keys"`
}

type keyRow struct {
	ID           int64  `json:"id"`
	Key          string `json:"key"`
	Region       string `json:"region"`
	Status       string `json:"status"`
	OrderID      string `json:"order_id"`
	CreatedAt    string `json:"created_at"`
	DispatchedAt string `json:"dispatched_at"`
	DeadReason   string `json:"dead_reason"`
	LastProbe    string `json:"last_probe"`
	CurrentUsage int    `json:"current_usage"`
	UsageLimit   int    `json:"usage_limit"`
}

const timeLayout = "2006-01-02 15:04:05"

func parseHistTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(timeLayout, s); err == nil {
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
		return nil, fmt.Errorf("kiroceo: purchase-orders: http %d", resp.StatusCode)
	}
	var rows []orderRow
	if err := json.Unmarshal(resp.Body, &rows); err != nil {
		return nil, fmt.Errorf("kiroceo: orders 解析: %w", err)
	}
	out := make([]providers.VendorOrder, 0, len(rows))
	for _, r := range rows {
		raw, _ := json.Marshal(r)
		out = append(out, providers.VendorOrder{
			VendorOrderID: r.ClientOrderID,
			CreatedAt:     parseHistTime(r.CreatedAt),
			Purchased:     r.Purchased,
			Requested:     r.Requested,
			Source:        r.Source,
			Raw:           raw,
		})
	}
	return &providers.HistoryPage[providers.VendorOrder]{Items: out}, nil
}

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
		return nil, fmt.Errorf("kiroceo: keys: http %d", resp.StatusCode)
	}
	var wrap keysWrap
	if err := json.Unmarshal(resp.Body, &wrap); err != nil {
		return nil, fmt.Errorf("kiroceo: keys 解析: %w", err)
	}
	out := make([]providers.VendorKey, 0, len(wrap.Keys))
	for _, k := range wrap.Keys {
		raw, _ := json.Marshal(k)
		vk := providers.VendorKey{
			VendorKeyID:  fmt.Sprintf("%d", k.ID),
			OrderID:      k.OrderID,
			KeyMasked:    maskKey(k.Key),
			Region:       k.Region,
			Status:       k.Status,
			CreatedAt:    parseHistTime(k.CreatedAt),
			DispatchedAt: parseHistTime(k.DispatchedAt),
			DeadReason:   k.DeadReason,
			LastProbeAt:  parseHistTime(k.LastProbe),
			CurrentUsage: k.CurrentUsage,
			UsageLimit:   k.UsageLimit,
			Raw:          raw,
		}
		if k.Status == "dead" {
			vk.DeadAt = parseHistTime(k.LastProbe)
		}
		out = append(out, vk)
	}
	return &providers.HistoryPage[providers.VendorKey]{Items: out}, nil
}
