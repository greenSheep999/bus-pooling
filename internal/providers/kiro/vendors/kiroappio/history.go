package kiroappio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 本 vendor 用 /api/me/orders + /api/me/keys · 分页格式 {items[], page, page_size, pages, total}
// cursor 用 page number 字符串 · 空 = page=1

type paged[T any] struct {
	Items    []T `json:"items"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Pages    int `json:"pages"`
	Total    int `json:"total"`
}

type ioOrder struct {
	ID            string `json:"id"`
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id"`
	CreatedAt     string `json:"created_at"`
	Purchased     int    `json:"purchased"`
	Requested     int    `json:"requested"`
	Source        string `json:"source"`
	UnitPrice     int64  `json:"unit_price"`
	TotalCredits  int64  `json:"total_credits"`
}

type ioKey struct {
	ID            string `json:"id"`
	Key           string `json:"key"`
	Region        string `json:"region"`
	Status        string `json:"status"`
	OrderID       string `json:"order_id"`
	CreatedAt     string `json:"created_at"`
	DispatchedAt  string `json:"dispatched_at"`
	DeadAt        string `json:"dead_at"`
	DeadReason    string `json:"dead_reason"`
	LastProbe     string `json:"last_probe"`
	CurrentUsage  int    `json:"current_usage"`
	UsageLimit    int    `json:"usage_limit"`
	UnitPrice     int64  `json:"unit_price"`
}

func parseHistTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
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
	page := 1
	if cursor != "" {
		if p, err := strconv.Atoi(cursor); err == nil && p > 0 {
			page = p
		}
	}
	url := fmt.Sprintf("/api/me/orders?page=%d&page_size=100", page)
	req, err := a.newReq(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiroappio: orders: http %d", resp.StatusCode)
	}
	var p paged[ioOrder]
	if err := json.Unmarshal(resp.Body, &p); err != nil {
		return nil, fmt.Errorf("kiroappio: orders 解析: %w", err)
	}

	out := make([]providers.VendorOrder, 0, len(p.Items))
	for _, r := range p.Items {
		raw, _ := json.Marshal(r)
		vid := r.OrderID
		if vid == "" {
			vid = r.ID
		}
		if vid == "" {
			vid = r.ClientOrderID
		}
		out = append(out, providers.VendorOrder{
			VendorOrderID: vid,
			CreatedAt:     parseHistTime(r.CreatedAt),
			Purchased:     r.Purchased,
			Requested:     r.Requested,
			UnitPrice:     providers.Money{Amount: r.UnitPrice * 1_000_000},
			TotalCost:     providers.Money{Amount: r.TotalCredits * 1_000_000},
			Source:        r.Source,
			Raw:           raw,
		})
	}
	next := ""
	if page < p.Pages {
		next = strconv.Itoa(page + 1)
	}
	return &providers.HistoryPage[providers.VendorOrder]{Items: out, NextCursor: next}, nil
}

func (a *Adapter) ListKeys(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorKey], error) {
	page := 1
	if cursor != "" {
		if p, err := strconv.Atoi(cursor); err == nil && p > 0 {
			page = p
		}
	}
	url := fmt.Sprintf("/api/me/keys?page=%d&page_size=100", page)
	req, err := a.newReq(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiroappio: keys: http %d", resp.StatusCode)
	}
	var p paged[ioKey]
	if err := json.Unmarshal(resp.Body, &p); err != nil {
		return nil, fmt.Errorf("kiroappio: keys 解析: %w", err)
	}

	out := make([]providers.VendorKey, 0, len(p.Items))
	for _, k := range p.Items {
		raw, _ := json.Marshal(k)
		vk := providers.VendorKey{
			VendorKeyID:  k.ID,
			OrderID:      k.OrderID,
			KeyMasked:    maskKey(k.Key),
			Region:       k.Region,
			Status:       k.Status,
			CreatedAt:    parseHistTime(k.CreatedAt),
			DispatchedAt: parseHistTime(k.DispatchedAt),
			DeadAt:       parseHistTime(k.DeadAt),
			DeadReason:   k.DeadReason,
			LastProbeAt:  parseHistTime(k.LastProbe),
			CurrentUsage: k.CurrentUsage,
			UsageLimit:   k.UsageLimit,
			UnitPrice:    providers.Money{Amount: k.UnitPrice * 1_000_000},
			Raw:          raw,
		}
		out = append(out, vk)
	}
	next := ""
	if page < p.Pages {
		next = strconv.Itoa(page + 1)
	}
	return &providers.HistoryPage[providers.VendorKey]{Items: out, NextCursor: next}, nil
}
