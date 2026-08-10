package kiro91

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// 本 vendor `/api/my/rounds` 是**平台整轮开号视图**（不是账户视角）·
// 每条 `visibility:"public", scope:"platform", is_mine:false` · 全网可见。
//
// 观察样本（2026-08-10）：
//
//	{
//	  "rounds": [{
//	    "id": "48ff650ff1d1e21fbb94aed21c7ad86f",
//	    "visibility": "public", "scope": "platform", "state": "dead",
//	    "keys_total": 19, "unit_price": 50,
//	    "allocated_at": "2026-08-10T08:56:03Z",
//	    "launched_at":  "2026-08-10T08:56:03Z",
//	    "died_at":      "2026-08-10T10:11:21Z",
//	    "regions": "eu-central-1,us-east-1",
//	    "launched_by": "manual", "is_mine": false
//	  }, ...]
//	}
//
// `state` ∈ {live, dead, failed} · alive/dead 我方按 state 判定：
//   - live  → alive = keys_total, dead = 0
//   - dead  → alive = 0, dead = keys_total（全批已挂）
//   - failed → alive = dead = 0（开号失败 · 没号可派）
//
// **不用 `/api/my/gen-logs`**——那个端点账户视角返 `{logs:null}`（我方账户无购买）·
// `/api/my/rounds` 是平台视图 · 一定有数据。

type roundsResp struct {
	Rounds []roundItem `json:"rounds"`
}

type roundItem struct {
	ID          string `json:"id"`
	State       string `json:"state"`
	KeysTotal   int    `json:"keys_total"`
	LaunchedAt  string `json:"launched_at"`
	DiedAt      string `json:"died_at"`
	Regions     string `json:"regions"`     // "eu-central-1,us-east-1" · 逗号分隔
	Visibility  string `json:"visibility"`  // public / private
	Scope       string `json:"scope"`       // platform / user
	IsMine      bool   `json:"is_mine"`
	LaunchedBy  string `json:"launched_by"` // manual / auto
}

func (a *Adapter) ListDispatches(ctx context.Context, limit int) ([]providers.VendorDispatch, error) {
	req, err := a.newReq(ctx, http.MethodGet, "/api/my/rounds", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiro91: rounds: http %d", resp.StatusCode)
	}
	var rr roundsResp
	if err := json.Unmarshal(resp.Body, &rr); err != nil {
		return nil, fmt.Errorf("kiro91: rounds 解析: %w", err)
	}

	out := make([]providers.VendorDispatch, 0, len(rr.Rounds))
	for _, r := range rr.Rounds {
		// 只要平台视图 · 跳过账户私有轮（虽然当前样本里都是 public · 保险起见）
		if r.Visibility != "" && r.Visibility != "public" {
			continue
		}
		t := parseHistTime(r.LaunchedAt)
		if t.IsZero() {
			continue
		}
		alive, dead := 0, 0
		switch r.State {
		case "live":
			alive = r.KeysTotal
		case "dead":
			dead = r.KeysTotal
		}
		var deadAt time.Time
		if r.DiedAt != "" {
			deadAt = parseHistTime(r.DiedAt)
		}
		// 多区批次 · 记第一个 region（或空 · fleet 视图不 care 分区细粒度）
		region := ""
		if r.Regions != "" {
			region = strings.SplitN(r.Regions, ",", 2)[0]
		}

		raw, _ := json.Marshal(r)
		out = append(out, providers.VendorDispatch{
			DispatchKey:  r.ID,
			Region:       region,
			DispatchedAt: t,
			Count:        r.KeysTotal,
			Alive:        alive,
			Dead:         dead,
			DeadAt:       deadAt,
			Status:       r.State,
			Raw:          raw,
		})
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
