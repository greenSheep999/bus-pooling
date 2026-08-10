package kiroappcc

import (
	"context"
	"encoding/json"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// kiroappcc 没独立"最近开号"端点 · 直接从 /openapi/orders 里抽 · 一单 = 一批
// orders 已经在 ListOrders() 拉过 · 复用同一份 vendor 请求。
func (a *Adapter) ListDispatches(ctx context.Context, limit int) ([]providers.VendorDispatch, error) {
	page, err := a.ListOrders(ctx, "")
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, nil
	}
	out := make([]providers.VendorDispatch, 0, len(page.Items))
	for _, o := range page.Items {
		if o.CreatedAt.IsZero() {
			continue
		}
		// 从 Raw 里挖 dead 状态（kiroappcc 一单一 key · 用 order 的 probeState / probeTerminalAt 判定）
		alive, dead := 0, 0
		status := "done"
		if len(o.Raw) > 0 {
			var probe struct {
				ProbeState      string `json:"probeState"`
				ProbeTerminalAt string `json:"probeTerminalAt"`
			}
			if json.Unmarshal(o.Raw, &probe) == nil {
				if probe.ProbeState == "dead" {
					dead = o.Purchased
					status = "dead"
				} else if probe.ProbeState == "alive" {
					alive = o.Purchased
				}
			}
		}
		out = append(out, providers.VendorDispatch{
			DispatchKey:  o.VendorOrderID,
			DispatchedAt: o.CreatedAt,
			Count:        o.Purchased,
			Alive:        alive,
			Dead:         dead,
			Status:       status,
			Raw:          o.Raw,
		})
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
