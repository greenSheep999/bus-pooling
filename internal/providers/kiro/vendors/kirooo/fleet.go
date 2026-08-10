package kirooo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// kirooo /api/my/stock/regions 返 regions[].dispatches[] 结构 · 用来当 fleet-wide 开号历史。
//
// 观察到的响应：
//   {
//     "fleet_active": true,
//     "fleet_now": "2026-08-10 20:39:39",
//     "fleet_started_at": "2026-08-10 20:35:26",
//     "regions": [
//       {
//         "region": "us-east-1",
//         "label": "美国区",
//         "unit_price": 80,
//         "stock": 0,
//         "dispatches": [
//           {"time": "2026-08-10 20:25:00", "delivered": 10, "alive": 0, "dead": 0, "dead_at": "", "running": true},
//           {"time": "2026-08-10 19:28:00", "delivered": 0, "alive": 0, "dead": 10, "dead_at": "2026-08-10 20:39:01", "running": false},
//           ...
//         ]
//       },
//       { "region": "eu-central-1", ... }
//     ]
//   }

type regionsResp struct {
	FleetActive    bool          `json:"fleet_active"`
	FleetStartedAt string        `json:"fleet_started_at"`
	Regions        []regionEntry `json:"regions"`
}

type regionEntry struct {
	Region     string          `json:"region"`
	Label      string          `json:"label"`
	UnitPrice  int64           `json:"unit_price"`
	Stock      int             `json:"stock"`
	Dispatches []dispatchEntry `json:"dispatches"`
}

type dispatchEntry struct {
	Time      string `json:"time"`
	Delivered int    `json:"delivered"`
	Alive     int    `json:"alive"`
	Dead      int    `json:"dead"`
	DeadAt    string `json:"dead_at"`
	Running   bool   `json:"running"`
}

// ListDispatches 拉 fleet-wide 最近开号 · limit 忽略（vendor 一次返完）
func (a *Adapter) ListDispatches(ctx context.Context, limit int) ([]providers.VendorDispatch, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/stock/regions", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kirooo dispatches: http %d", resp.StatusCode)
	}
	var rr regionsResp
	if err := json.Unmarshal(resp.Body, &rr); err != nil {
		return nil, fmt.Errorf("kirooo dispatches 解析: %w", err)
	}

	var out []providers.VendorDispatch
	for _, r := range rr.Regions {
		for _, d := range r.Dispatches {
			t := parseKiroooTime(d.Time)
			if t.IsZero() {
				continue
			}
			raw, _ := json.Marshal(d)
			status := "running"
			if !d.Running {
				status = "done"
			}
			if d.Dead > 0 && d.Alive == 0 && d.Delivered == 0 {
				status = "dead"
			}
			vd := providers.VendorDispatch{
				// dispatch_key 组合 region + time · 同 region 内 time 唯一
				DispatchKey:  r.Region + "@" + d.Time,
				Region:       r.Region,
				DispatchedAt: t,
				Count:        d.Delivered + d.Alive + d.Dead,
				Alive:        d.Alive,
				Dead:         d.Dead,
				DeadAt:       parseKiroooTime(d.DeadAt),
				Status:       status,
				Raw:          raw,
			}
			// Count 兜底：有的批次 delivered=0 但 dead=10（已全挂），Count 应 = alive+dead
			if vd.Count == 0 {
				vd.Count = d.Delivered
				if vd.Count == 0 {
					vd.Count = d.Alive + d.Dead
				}
			}
			out = append(out, vd)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// 保证接口断言不失败
var _ = time.Time{}
