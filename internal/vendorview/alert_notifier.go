package vendorview

// 告警外发 · 陈旧管线不止 ERROR 日志 · 主动 POST 到 webhook。
//
// **为什么要有这个**：日志里 ERROR 靠人 tail · 半夜没人看。把 stale 事件
// POST 到一个 webhook（企业微信 / 钉钉 / Slack / 自建告警网关），配上
// 去重 + 冷却 · 才是"系统自己盯"的完整闭环。
//
// **去重口径**：(vendor, pipeline) 双键 · 同一条管线冷却窗口内只发一次告警 ·
// 恢复后清零。防止陈旧 5 分钟就 5 条告警轰炸。

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// AlertNotifier 陈旧管线告警外发接口 · nil-safe（可不装配）。
type AlertNotifier interface {
	// Notify 把一批陈旧管线告警外发。now 是本轮检查时间。
	// 实现应自行处理去重 / 冷却 / 失败静默（不能反过来把 checker 打挂）。
	Notify(ctx context.Context, rows []PipelineHealthRow, now time.Time)
	// NotifyRecovered 通知这批管线（vendor+pipeline）已恢复新鲜 · 让实现方清冷却窗。
	NotifyRecovered(ctx context.Context, keys []AlertKey, now time.Time)
}

// AlertKey (vendor_id, pipeline) 二元组 · vendor_id="" = 全局管线。
type AlertKey struct {
	VendorID string
	Pipeline string
}

// AlertPayload webhook POST body · 简单 JSON · 让下游随便接。
type AlertPayload struct {
	Type      string `json:"type"`                  // "stale" | "recovered"
	At        string `json:"at"`                    // RFC3339 · 事件时间
	Vendor    string `json:"vendor"`                // 空 = 全局
	Pipeline  string `json:"pipeline"`              // probe / orders / keys / ...
	LastOK    string `json:"last_ok,omitempty"`     // 上次成功时间 · 空=从未
	LastOKAge string `json:"last_ok_age,omitempty"` // 人可读时长
	Threshold string `json:"threshold"`             // 陈旧阈值
	LastErr   string `json:"last_err,omitempty"`
}

// WebhookNotifier 把 stale/recovered 事件 POST 到一个 URL。
//
// **冷却窗**：默认 30min · 同一条管线在窗口内只 POST 一次 stale 告警。
// **恢复通知**：只要之前发过 stale · 恢复时必发 recovered · 不受冷却窗限制。
type WebhookNotifier struct {
	url      string
	client   *http.Client
	cooldown time.Duration
	logger   *slog.Logger

	mu       sync.Mutex
	lastSent map[AlertKey]time.Time // 每条管线上次发 stale 的时间 · 用于冷却
	active   map[AlertKey]struct{}  // 当前处于告警中的管线 · 用于判断"恢复"
}

// NewWebhookNotifier · url 空 = 返 nil（不装配告警）· 让调用方 nil-safe 走过。
func NewWebhookNotifier(url string, cooldown time.Duration, logger *slog.Logger) *WebhookNotifier {
	if url == "" {
		return nil
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &WebhookNotifier{
		url:      url,
		client:   &http.Client{Timeout: 8 * time.Second},
		cooldown: cooldown,
		logger:   logger,
		lastSent: make(map[AlertKey]time.Time),
		active:   make(map[AlertKey]struct{}),
	}
}

// Notify · 遍历陈旧行 · 过冷却窗则外发 · 记录到 active。
func (n *WebhookNotifier) Notify(ctx context.Context, rows []PipelineHealthRow, now time.Time) {
	if n == nil {
		return
	}
	n.mu.Lock()
	toSend := make([]PipelineHealthRow, 0, len(rows))
	seen := make(map[AlertKey]struct{}, len(rows))
	for _, r := range rows {
		k := AlertKey{VendorID: r.VendorID, Pipeline: r.Pipeline}
		seen[k] = struct{}{}
		n.active[k] = struct{}{}
		if last, ok := n.lastSent[k]; ok && now.Sub(last) < n.cooldown {
			continue
		}
		n.lastSent[k] = now
		toSend = append(toSend, r)
	}
	n.mu.Unlock()

	for _, r := range toSend {
		n.post(ctx, buildStalePayload(r, now))
	}
}

// NotifyRecovered · 之前告警过 · 这轮变新鲜 · 发一次 recovered 且清 active。
func (n *WebhookNotifier) NotifyRecovered(ctx context.Context, keys []AlertKey, now time.Time) {
	if n == nil {
		return
	}
	n.mu.Lock()
	toSend := make([]AlertKey, 0, len(keys))
	for _, k := range keys {
		if _, was := n.active[k]; !was {
			continue
		}
		delete(n.active, k)
		delete(n.lastSent, k) // 清冷却窗 · 下次陈旧立即能报
		toSend = append(toSend, k)
	}
	n.mu.Unlock()

	for _, k := range toSend {
		n.post(ctx, AlertPayload{
			Type: "recovered", At: now.UTC().Format(time.RFC3339),
			Vendor: k.VendorID, Pipeline: k.Pipeline,
		})
	}
}

func buildStalePayload(r PipelineHealthRow, now time.Time) AlertPayload {
	p := AlertPayload{
		Type:      "stale",
		At:        now.UTC().Format(time.RFC3339),
		Vendor:    r.VendorID,
		Pipeline:  r.Pipeline,
		Threshold: MaxAge(r.Pipeline).String(),
		LastErr:   r.LastErr,
	}
	if !r.LastOKAt.IsZero() {
		p.LastOK = r.LastOKAt.UTC().Format(time.RFC3339)
		p.LastOKAge = now.Sub(r.LastOKAt).Round(time.Second).String()
	}
	return p
}

// post · 单次外发 · 失败只 WARN · 绝不打挂 checker。
func (n *WebhookNotifier) post(ctx context.Context, p AlertPayload) {
	body, err := json.Marshal(p)
	if err != nil {
		n.logger.Warn("AlertNotifier: 序列化失败", "err", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		n.logger.Warn("AlertNotifier: 构造请求失败", "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bus-pooling-alert/1.0")

	resp, err := n.client.Do(req)
	if err != nil {
		n.logger.Warn("AlertNotifier: POST 失败", "err", err, "type", p.Type, "pipeline", p.Pipeline)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		n.logger.Warn("AlertNotifier: 下游返错", "status", resp.StatusCode,
			"pipeline", p.Pipeline, "body", string(buf))
		return
	}
	n.logger.Info("AlertNotifier: 已外发", "type", p.Type,
		"vendor", vendorOrGlobal(p.Vendor), "pipeline", p.Pipeline)
}
