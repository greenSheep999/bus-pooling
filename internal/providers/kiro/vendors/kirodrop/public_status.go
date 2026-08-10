package kirodrop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// publicStatusResp kirodrop /api/status（需 auth · X-API-Key）真实响应。
//
// 观察到的字段（curl 采样 2026-08-10）：
//   {
//     "community_qr_urls": [...],
//     "generating": false,
//     "keys_active": 17,
//     "keys_dead": 29,
//     "keys_stock": 0,
//     "region": "us-east-1"
//   }
type publicStatusResp struct {
	Generating bool   `json:"generating"`
	KeysActive int    `json:"keys_active"`
	KeysDead   int    `json:"keys_dead"`
	KeysStock  int    `json:"keys_stock"`
	Region     string `json:"region"`
}

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
		return nil, fmt.Errorf("kirodrop public_status: http %d", resp.StatusCode)
	}

	var sr publicStatusResp
	if err := json.Unmarshal(resp.Body, &sr); err != nil {
		return nil, fmt.Errorf("kirodrop public_status: 解析: %w", err)
	}

	return &providers.PublicStatusSnapshot{
		VendorID:   providers.VendorKiroDrop,
		ObservedAt: time.Now().UTC(),
		Generating: sr.Generating,
		KeysActive: sr.KeysActive,
		KeysDead:   sr.KeysDead,
		KeysStock:  sr.KeysStock,
		Raw:        resp.Body,
	}, nil
}
