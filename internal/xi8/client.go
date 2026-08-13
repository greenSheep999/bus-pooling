// Package xi8 · 内部数据源 client · 用于对账 vendor_self + 补历史空窗。
//
// **不是 vendor · 不出前端 · 不注册为 provider**（`CLAUDE.md §0.1`）·
// 只在后端跑对账 CLI + 一次性 backfill 用。credentials 走 vendor_account 表加密存储。
//
// 端点（实证 8-11）：
//   - GET /api/restock-log?limit=N · 出库历史（轮询推算 · 时间偏晚 ~8min · 覆盖多天）
//   - GET /api/vendors                · 5 家聚合 vendor 的实时库存 + 价格 + 质保 + 人工评级
//   - GET /api/signals?limit=N        · webhook 验签过的上货信号原文（含 vendor_order_id · 最准）
//   - GET /api/stock                  · 实时 stock（我们用不上 · 探针已有）
//
// 时间字段一律 ISO with `+08:00` · 解 RFC3339 后 .UTC() 才对。
package xi8

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bus-pooling/bus-pooling/internal/httpx"
)

// Client · xi8 API 只读 client。
type Client struct {
	baseURL string
	apiKey  string
	http    *httpx.Client
}

// New 装配 · apiKey 空返 nil（上层判断 xi8 未接入 · 跳过对账）
func New(apiKey string, hc *httpx.Client) *Client {
	if apiKey == "" || hc == nil {
		return nil
	}
	return &Client{
		baseURL: "https://xi8.cc",
		apiKey:  apiKey,
		http:    hc,
	}
}

func (c *Client) do(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("xi8: %s http %d · body %s", path, resp.StatusCode, string(resp.Body))
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return fmt.Errorf("xi8: %s 解析: %w", path, err)
	}
	return nil
}

// ListRestockLog · 拉出库历史 · limit ≤ 500（服务端上限）
func (c *Client) ListRestockLog(ctx context.Context, limit int) (*RestockLogResp, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	var out RestockLogResp
	if err := c.do(ctx, fmt.Sprintf("/api/restock-log?limit=%d", limit), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListVendors · 5 家实时状态 + 质保 + 评级
func (c *Client) ListVendors(ctx context.Context) (*VendorsResp, error) {
	var out VendorsResp
	if err := c.do(ctx, "/api/vendors", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSignals · webhook 验签过的原始信号 · 含 vendor_order_id · 最精准
func (c *Client) ListSignals(ctx context.Context, limit int) (*SignalsResp, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var out SignalsResp
	if err := c.do(ctx, fmt.Sprintf("/api/signals?limit=%d", limit), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListNotifications · 站内通知 · **唯一带历史价格的端点**（`price_fen` + `old_price_fen`）
//
// 上游其他端点都不给历史价：`/signals` 和 `/restock-log` 只有数量和批次号 ·
// `/vendors` 和 `/stock` 是实时快照。想补探针上线前的价格只有这条路。
//
// **服务端限制**（实测 2026-08-13）：
//   - limit 硬顶 100 · 传 500/1000/5000 都只返 100
//   - **只 `since_id` 生效**（返 id > 该值的）· `page` / `offset` / `cursor` /
//     `before_id` / `until_id` / `max_id` 全部无效 · 一律返首页
//   - 所以 API 层最多拿最近 100 条（约 2 天）· 更早的只有网页版能翻（需 cookie）
//
// sinceID <= 0 时不带该参数 · 拿最近的一批。
func (c *Client) ListNotifications(ctx context.Context, limit, sinceID int) (*NotificationsResp, error) {
	if limit <= 0 || limit > 100 {
		limit = 100 // 服务端硬顶 · 传更大无意义
	}
	path := fmt.Sprintf("/api/my/notifications?limit=%d", limit)
	if sinceID > 0 {
		path += fmt.Sprintf("&since_id=%d", sinceID)
	}
	var out NotificationsResp
	if err := c.do(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
