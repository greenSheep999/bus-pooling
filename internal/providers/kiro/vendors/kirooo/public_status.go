package kirooo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// publicStatusResp 本 vendor /api/status（需 auth · X-API-Key）真实响应。
//
// 观察到的字段（curl 采样 2026-08-10）：
//   {
//     "announce": {enabled, text, level, updated_at, updated_by},
//     "auto_mode": false,
//     "generating": false,
//     "keys_active": 446,
//     "keys_alive": 1187,
//     "keys_dead": 515,
//     "keys_stock": 11,
//     "keys_suspect": 283,
//     "keys_total": 1985,
//     "started_at": "2026-08-10 13:38:29",
//     "uptime_seconds": ...
//   }
type publicStatusResp struct {
	Generating    bool   `json:"generating"`
	KeysActive    int    `json:"keys_active"`
	KeysAlive     int    `json:"keys_alive"`
	KeysDead      int    `json:"keys_dead"`
	KeysStock     int    `json:"keys_stock"`
	KeysSuspect   int    `json:"keys_suspect"`
	KeysTotal     int    `json:"keys_total"`
	StartedAt     string `json:"started_at"` // "2026-08-10 13:38:29" · 非 RFC3339
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// PublicStatus vendor 平台自报的累计状态（比 Stock 更丰富）。
// 用于 /api/vendors/status 公开页展示 keys_active/keys_dead 之类的历史累计数据。
func (a *Adapter) PublicStatus(ctx context.Context) (*providers.PublicStatusSnapshot, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kirooo: public_status: http %d", resp.StatusCode)
	}

	var sr publicStatusResp
	if err := json.Unmarshal(resp.Body, &sr); err != nil {
		return nil, fmt.Errorf("kirooo: public_status: 解析: %w", err)
	}

	out := &providers.PublicStatusSnapshot{
		VendorID:      providers.VendorKiroOOO,
		ObservedAt:    time.Now().UTC(),
		Generating:    sr.Generating,
		KeysActive:    sr.KeysActive,
		KeysAlive:     sr.KeysAlive,
		KeysDead:      sr.KeysDead,
		KeysStock:     sr.KeysStock,
		KeysSuspect:   sr.KeysSuspect,
		KeysTotal:     sr.KeysTotal,
		UptimeSeconds: sr.UptimeSeconds,
		Raw:           resp.Body,
	}
	// started_at 格式 "2006-01-02 15:04:05" · 部分场景可能是 UTC 部分本地 · 尽力 parse
	if sr.StartedAt != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", sr.StartedAt); err == nil {
			out.StartedAt = &t
		}
	}
	return out, nil
}
