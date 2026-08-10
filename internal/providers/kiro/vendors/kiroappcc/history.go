package kiroappcc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// kiroappcc /openapi/orders 结构跟其他 vendor 完全不同 · 一条 order 内嵌 key 信息 ·
// 我方拆成 vendor_order（订单元数据）+ vendor_key（内嵌的 key） 两条记录。
//
// 观察到的真实结构（curl 采样）：
//   [{
//     "id": 1473,
//     "orderNo": "KV-20260801-PS2KL8",
//     "kiroApiKey": "ksk_4KJqQsln1jQDuRXF4v398HpJGPudSXin",
//     "pointsCost": 25,
//     "claimedAt": "2026-08-01T11:38:39.291857916+00:00",
//     "warrantyUntil": "2026-08-01T11:48:39.291858396+00:00",
//     "warrantyStatus": "expired",
//     "usageSnapshot": "{\"checkedAt\":\"...\",\"currentUsage\":198.25,\"usageLimit\":10000.0,\"subscriptionTitle\":\"KIRO POWER\"}",
//     "warrantyMaxUsageObserved": 126.62,
//     "probeState": "dead",
//     "probeTerminalAt": "2026-08-01T12:57:50.733348075+00:00",  // 挂的时刻
//     "probeTerminalReason": "权限不足…"
//   }, ...]

type ccOrder struct {
	ID                       int64   `json:"id"`
	OrderNo                  string  `json:"orderNo"`
	UserID                   int64   `json:"userId"`
	KiroApiKey               string  `json:"kiroApiKey"`
	PointsCost               int64   `json:"pointsCost"`
	ClaimedAt                string  `json:"claimedAt"` // ISO-8601
	WarrantyUntil            string  `json:"warrantyUntil"`
	WarrantyStatus           string  `json:"warrantyStatus"` // active / expired
	RefundedAt               *string `json:"refundedAt"`
	UsageSnapshot            string  `json:"usageSnapshot"` // JSON as string
	WarrantyMaxUsageObserved float64 `json:"warrantyMaxUsageObserved"`
	ProbeState               string  `json:"probeState"` // alive / dead
	ProbeTerminalAt          *string `json:"probeTerminalAt"`
	ProbeTerminalReason      string  `json:"probeTerminalReason"`
}

// usageSnapshot 内嵌 JSON 的字段（vendor 侧字符串化了）
type usageSnap struct {
	CheckedAt         string  `json:"checkedAt"`
	CurrentUsage      float64 `json:"currentUsage"`
	UsageLimit        float64 `json:"usageLimit"`
	SubscriptionTitle string  `json:"subscriptionTitle"`
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// vendor 出的是"2026-08-01T11:38:39.291857916+00:00"（RFC3339 带纳秒 · time.Parse 认）
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func parseTimePtr(p *string) time.Time {
	if p == nil {
		return time.Time{}
	}
	return parseTime(*p)
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return k
	}
	return k[:8] + "****"
}

// ListOrders kiroappcc 一次全量返 · 没分页
func (a *Adapter) ListOrders(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorOrder], error) {
	req, err := a.newReq(ctx, http.MethodGet, "/openapi/orders", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiroappcc orders: http %d", resp.StatusCode)
	}
	var rows []ccOrder
	if err := json.Unmarshal(resp.Body, &rows); err != nil {
		return nil, fmt.Errorf("kiroappcc orders 解析: %w", err)
	}

	out := make([]providers.VendorOrder, 0, len(rows))
	for _, r := range rows {
		raw, _ := json.Marshal(r)
		out = append(out, providers.VendorOrder{
			VendorOrderID: r.OrderNo, // 用 OrderNo 而非 ID · 可读性好
			CreatedAt:     parseTime(r.ClaimedAt),
			Purchased:     1, // kiroappcc 一单一 key · 恒为 1
			Requested:     1,
			UnitPrice: providers.Money{
				Amount: r.PointsCost * 1_000_000, // pointsCost 是整数 · 转成 microunit（跟其他 vendor 对齐）
			},
			TotalCost: providers.Money{Amount: r.PointsCost * 1_000_000},
			Source:    "api",
			Raw:       raw,
		})
	}
	return &providers.HistoryPage[providers.VendorOrder]{Items: out}, nil
}

// ListKeys kiroappcc 没独立 keys 端点 · 从 orders 里抽 key
func (a *Adapter) ListKeys(ctx context.Context, cursor string) (*providers.HistoryPage[providers.VendorKey], error) {
	req, err := a.newReq(ctx, http.MethodGet, "/openapi/orders", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiroappcc keys: http %d", resp.StatusCode)
	}
	var rows []ccOrder
	if err := json.Unmarshal(resp.Body, &rows); err != nil {
		return nil, fmt.Errorf("kiroappcc keys 解析: %w", err)
	}

	out := make([]providers.VendorKey, 0, len(rows))
	for _, r := range rows {
		raw, _ := json.Marshal(r)
		var us usageSnap
		if r.UsageSnapshot != "" {
			_ = json.Unmarshal([]byte(r.UsageSnapshot), &us)
		}
		// probeState: alive / dead · 收敛成 active/dead
		status := "unknown"
		switch r.ProbeState {
		case "alive":
			status = "active"
		case "dead":
			status = "dead"
		}

		vk := providers.VendorKey{
			VendorKeyID:   fmt.Sprintf("%d", r.ID),
			OrderID:       r.OrderNo,
			KeyMasked:     maskKey(r.KiroApiKey),
			Status:        status,
			CreatedAt:     parseTime(r.ClaimedAt),
			DispatchedAt:  parseTime(r.ClaimedAt),
			DeadAt:        parseTimePtr(r.ProbeTerminalAt),
			DeadReason:    r.ProbeTerminalReason,
			WarrantyUntil: parseTime(r.WarrantyUntil),
			CurrentUsage:  int(us.CurrentUsage),
			UsageLimit:    int(us.UsageLimit),
			UnitPrice: providers.Money{
				Amount: r.PointsCost * 1_000_000,
			},
			Raw: raw,
		}
		out = append(out, vk)
	}
	return &providers.HistoryPage[providers.VendorKey]{Items: out}, nil
}
