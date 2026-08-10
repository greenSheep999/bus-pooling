package kiroceo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 本 vendor /api/my/gen-logs 返 fleet-wide "最近开号"（简约版）：
//
//   {"avg_interval_min":15.73,
//    "items":[
//      {"created_at":"2026-08-10 20:40:35","count":10,"status":"running"},
//      {"created_at":"2026-08-10 20:21:37","count":10,"status":"done"},
//      ...
//    ]}
//
// 只有 time + count + status · 没有 alive/dead 细分。够画"每 X 分钟一批，每批 N 个"。

type genLogsResp struct {
	AvgIntervalMin float64      `json:"avg_interval_min"`
	Items          []genLogItem `json:"items"`
}

type genLogItem struct {
	CreatedAt string `json:"created_at"`
	Count     int    `json:"count"`
	Status    string `json:"status"`
}

func (a *Adapter) ListDispatches(ctx context.Context, limit int) ([]providers.VendorDispatch, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/gen-logs", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiroceo: dispatches: http %d", resp.StatusCode)
	}
	var gr genLogsResp
	if err := json.Unmarshal(resp.Body, &gr); err != nil {
		return nil, fmt.Errorf("kiroceo: dispatches 解析: %w", err)
	}

	out := make([]providers.VendorDispatch, 0, len(gr.Items))
	for _, it := range gr.Items {
		t := parseHistTime(it.CreatedAt)
		if t.IsZero() {
			continue
		}
		raw, _ := json.Marshal(it)
		out = append(out, providers.VendorDispatch{
			DispatchKey:  it.CreatedAt, // vendor 侧稳定 · 全网 gen-log 唯一
			DispatchedAt: t,
			Count:        it.Count,
			Status:       it.Status,
			Raw:          raw,
		})
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
