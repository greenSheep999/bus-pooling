package kiroappio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// publicStatusResp 本 vendor /api/status（**免 auth** · 公开）真实响应。
//
// 观察到的字段（curl 采样 2026-08-10）：
//   {
//     "auto_check": true,
//     "auto_generate": false,
//     "captcha_app_id": "199244242",
//     "captcha_enabled": true,
//     "generating": false,
//     "price": 50, "price_us": 50, "price_eu": 30,
//     "started_at": "2026-08-06T10:30:23Z",  // ISO-8601
//     "stock": 0, "stock_us": 0, "stock_eu": 0,
//     "uptime_seconds": 330642
//   }
//
// 注意：字段是 fleet-wide 视图（跟 /api/me/stock 类似结构但去掉 balance/max）。
// 没有 keys_active/keys_dead —— 本 vendor 平台不暴露这些。
type publicStatusResp struct {
	Generating    bool   `json:"generating"`
	Stock         int    `json:"stock"`
	StartedAt     string `json:"started_at"` // ISO-8601 UTC
	UptimeSeconds int64  `json:"uptime_seconds"`
}

func (a *Adapter) PublicStatus(ctx context.Context) (*providers.PublicStatusSnapshot, error) {
	// 注意：本端点**免 auth** · newReq 仍会带 API Key 头（vendor 侧忽略即可）
	req, err := a.newReq(ctx, http.MethodGet, "/api/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiroappio: public_status: http %d", resp.StatusCode)
	}

	var sr publicStatusResp
	if err := json.Unmarshal(resp.Body, &sr); err != nil {
		return nil, fmt.Errorf("kiroappio: public_status: 解析: %w", err)
	}

	out := &providers.PublicStatusSnapshot{
		VendorID:      providers.VendorKiroAppIO,
		ObservedAt:    time.Now().UTC(),
		Generating:    sr.Generating,
		KeysStock:     sr.Stock,
		UptimeSeconds: sr.UptimeSeconds,
		Raw:           resp.Body,
	}
	if sr.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339, sr.StartedAt); err == nil {
			out.StartedAt = &t
		}
	}
	return out, nil
}
