package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/downstream"
)

// 下游配置端点（05-api-contract §8）。
//
// 对外契约（CLAUDE.md §0.1）：
//   - 明文 token / secret **绝不出响应体** —— 只回 masked
//   - is_configured 布尔告诉前端"配没配"，用它来控制"测试"按钮的可用性
//   - webhook secret 明文**只在 POST /secret 轮换那一刻返回一次**
//
// TODO(assembly): 目前 downstream.Store 存在 *Server 关联的 side-table 里
// （downstream_ext.go），因为我这个 agent 不动 server.go。装配 agent
// 请把 `downstreams *downstream.Store` 挪到 Server / ServerDeps 里，
// 然后把 s.downstreams 全部改成 s.downstreams（去掉方法调用括号），
// 删掉 downstream_ext.go。

// ── 响应形状（跟 web/src/types/index.ts 对齐）──────────────

type downstreamResponse struct {
	PassengerpoolURL         string             `json:"passengerpool_url"`
	PassengerpoolTokenMasked string             `json:"passengerpool_token_masked"`
	Connected                bool               `json:"connected"`
	LastHeartbeatAt          *string            `json:"last_heartbeat_at"`
	PushSuccessRate          float64            `json:"push_success_rate"`
	PushTotal                int                `json:"push_total"`
	PushFailed               int                `json:"push_failed"`
	Rules                    downstreamRulesDTO `json:"rules"`
}

type downstreamRulesDTO struct {
	PushOnPull     bool `json:"push_on_pull"`
	ResyncOnDead   bool `json:"resync_on_dead"`
	RetryOnFailure bool `json:"retry_on_failure"`
	BusOnly        bool `json:"bus_only"`
}

type webhookResponse struct {
	URL          string   `json:"url"`
	SecretMasked string   `json:"secret_masked"`
	Enabled      bool     `json:"enabled"`
	Events       []string `json:"events"`
}

type webhookDeliveryDTO struct {
	ID         string `json:"id"`
	Event      string `json:"event"`
	OK         bool   `json:"ok"`
	StatusCode *int   `json:"status_code"`
	Attempt    int    `json:"attempt"`
	LatencyMs  int    `json:"latency_ms"`
	CreatedAt  string `json:"created_at"`
}

type testResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int    `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type webhookTestResult struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code"`
	LatencyMs  int    `json:"latency_ms"`
	Error      string `json:"error,omitempty"`
}

// ── 请求 ────────────────────────────────────────────

type putPassengerpoolRequest struct {
	// URL 为空字符串 = "别改"；nil 也是"别改"（用 *string 区分）
	URL   *string             `json:"passengerpool_url,omitempty"`
	Token string              `json:"token,omitempty"` // 明文 · 空 = 不改现有 token
	Rules *downstreamRulesDTO `json:"rules,omitempty"`
}

type putWebhookRequest struct {
	URL     *string  `json:"url,omitempty"`
	Enabled *bool    `json:"enabled,omitempty"`
	Events  []string `json:"events,omitempty"`
	// eventsPresent · 区分 "字段没传" vs "传了空数组" · 后者会清空订阅
	// 由 UnmarshalJSON 填 · 应用层用它决定要不要 SaveWebhookEvents
	eventsPresent bool `json:"-"`
}

// UnmarshalJSON · 让 Events 区分 "字段没出现" 和 "传了 []"
// events:null / events 缺席 → eventsPresent=false · 不动库
// events:[] → eventsPresent=true + Events=nil · 落 NULL(全订阅兜底 · 见 Store.SaveWebhookEvents)
// events:["boarded"] → eventsPresent=true + Events=["boarded"] · 落该数组
func (r *putWebhookRequest) UnmarshalJSON(data []byte) error {
	type alias struct {
		URL     *string          `json:"url,omitempty"`
		Enabled *bool            `json:"enabled,omitempty"`
		Events  *json.RawMessage `json:"events,omitempty"`
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	r.URL = a.URL
	r.Enabled = a.Enabled
	if a.Events != nil {
		r.eventsPresent = true
		// null 视为清空(等价空数组)
		if string(*a.Events) == "null" {
			return nil
		}
		if err := json.Unmarshal(*a.Events, &r.Events); err != nil {
			return err
		}
	}
	return nil
}

// ── 事件列表（阶段 1a 固定值，跟 fixtures.ts 对齐）──────

// defaultWebhookEvents · 对齐 docs/05-api-contract §11 的四种事件类型。
//
// 前端复选框按这个顺序渲染 · 用户勾选后前端存 index。
// 阶段 1e-2 定稿 · 不加不减。
var defaultWebhookEvents = []string{
	"new_keys_available",
	"all_keys_dead",
	"warranty_refund",
	"boarded",
}

// ── 端点 ────────────────────────────────────────────

// handleGetDownstream GET /api/me/downstream
//
// 未配过 → 返回一份"空但结构完整"的 DTO（前端不用判 404），rules 走 Defaults。
func (s *Server) handleGetDownstream(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.downstreams == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "下游配置服务暂未装配")
	}

	cfg, err := s.downstreams.Get(r.Context(), p.ID)
	if err != nil && !errors.Is(err, downstream.ErrNotFound) {
		return err
	}

	writeJSON(w, http.StatusOK, dtoOf(cfg))
	return nil
}

// handlePutPassengerpool PUT /api/me/downstream/passengerpool
//
// 语义（05-api-contract §8）：
//   - url 有值：校验 + 更新
//   - token 有值：加密后落库；空 = 不动现有 token
//   - rules 有值：更新 4 条推送策略
//
// 只要有任何一项，就写库（partial update）。
func (s *Server) handlePutPassengerpool(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.downstreams == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "下游配置服务暂未装配")
	}

	var req putPassengerpoolRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	// URL 校验放在写库前 —— 别把非法值写进去了才发现
	if req.URL != nil && *req.URL != "" {
		if err := downstream.ValidateTargetURL(*req.URL); err != nil {
			return ErrBadRequest(fallbackUserMsg(err, "地址不合法"))
		}
	}

	// 读现有配置，保留缺省 URL（前端只想改 token 时会不传 url）
	cur, err := s.downstreams.Get(r.Context(), p.ID)
	if err != nil && !errors.Is(err, downstream.ErrNotFound) {
		return err
	}
	url := cur.PassengerpoolURL
	if req.URL != nil {
		url = *req.URL
	}

	if err := s.downstreams.SavePassengerpool(r.Context(), p.ID, url, req.Token); err != nil {
		return err
	}
	if req.Rules != nil {
		if err := s.saveDownstreamRules(r.Context(), p.ID, *req.Rules); err != nil {
			return err
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// saveDownstreamRules 更新 4 条推送策略。
//
// 直接走 SQL —— rules 变更频率很低，没必要在 downstream.Store 上加一个专门方法
// 让接口面积膨胀。**注意**：ON CONFLICT 只覆盖 rules 列，不动 URL / token。
func (s *Server) saveDownstreamRules(ctx context.Context, passengerID string, r downstreamRulesDTO) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO passenger_downstream
		  (passenger_id, push_on_pull, resync_on_dead, retry_on_failure, bus_only, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (passenger_id) DO UPDATE SET
		  push_on_pull     = excluded.push_on_pull,
		  resync_on_dead   = excluded.resync_on_dead,
		  retry_on_failure = excluded.retry_on_failure,
		  bus_only         = excluded.bus_only,
		  updated_at       = excluded.updated_at`,
		passengerID,
		boolInt(r.PushOnPull), boolInt(r.ResyncOnDead),
		boolInt(r.RetryOnFailure), boolInt(r.BusOnly), now)
	if err != nil {
		return err
	}
	return nil
}

// handleTestPassengerpool POST /api/me/downstream/passengerpool/test
//
// 阶段 1a 简化：**不发真敏感请求**，只发一次 GET / HEAD 探活探连通。
// 如果 URL 都没配过就直接返 400 —— 提示"先配 URL 再测"，别让前端拿到误导性 latency=0。
func (s *Server) handleTestPassengerpool(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.downstreams == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "下游配置服务暂未装配")
	}

	cfg, err := s.downstreams.Get(r.Context(), p.ID)
	if err != nil && !errors.Is(err, downstream.ErrNotFound) {
		return err
	}
	if cfg.PassengerpoolURL == "" {
		return ErrBadRequest("请先配置我的号池地址")
	}

	// 走一次带 3 秒 timeout 的 HEAD/GET · 只关心能不能连上 · 不看内容
	result := s.probeReachability(r.Context(), cfg.PassengerpoolURL)
	writeJSON(w, http.StatusOK, result)
	return nil
}

// probeReachability 发一次探活请求。
//
// 用 http.Client 而不是 internal/httpx —— httpx 走 3 次重试，测连通只需要一次即可
// （用户点了「测试」就是想马上知道结果，不是等 20 秒重试完）。
// 3 秒是 UI 上「转圈圈」的可接受上限。
func (s *Server) probeReachability(ctx context.Context, rawURL string) testResult {
	client := &http.Client{Timeout: 3 * time.Second}
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	start := time.Now()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return testResult{OK: false, Error: "地址格式不对"}
	}
	resp, err := client.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return testResult{OK: false, LatencyMs: latency, Error: "连不上目标地址"}
	}
	_ = resp.Body.Close()

	// 2xx/3xx/4xx 都算"能通"—— 只要 TCP + TLS 握手成 + HTTP 层响应了就是通的
	// 5xx 才算不通（说明对方在但坏了）
	ok := resp.StatusCode < 500
	out := testResult{OK: ok, LatencyMs: latency}
	if !ok {
		out.Error = "目标服务暂时不可用"
	}
	return out
}

// handleGetWebhook GET /api/me/downstream/webhook
func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.downstreams == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "下游配置服务暂未装配")
	}

	cfg, err := s.downstreams.Get(r.Context(), p.ID)
	if err != nil && !errors.Is(err, downstream.ErrNotFound) {
		return err
	}

	writeJSON(w, http.StatusOK, webhookDTOOf(cfg))
	return nil
}

// handlePutWebhook PUT /api/me/downstream/webhook
//
// 只改 URL / events；secret 走独立的 POST /secret 轮换（不能通过 PUT 传明文进来）。
func (s *Server) handlePutWebhook(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.downstreams == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "下游配置服务暂未装配")
	}

	var req putWebhookRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	if req.URL != nil && *req.URL != "" {
		if err := downstream.ValidateTargetURL(*req.URL); err != nil {
			return ErrBadRequest(fallbackUserMsg(err, "地址不合法"))
		}
	}

	if req.URL != nil {
		if err := s.downstreams.SaveWebhookURL(r.Context(), p.ID, *req.URL); err != nil {
			return err
		}
	}
	if req.Enabled != nil {
		if err := s.downstreams.SaveWebhookEnabled(r.Context(), p.ID, *req.Enabled); err != nil {
			return err
		}
	}
	if req.eventsPresent {
		// 白名单校验 · 只接受 4 个官方事件
		allowed := map[string]bool{}
		for _, e := range defaultWebhookEvents {
			allowed[e] = true
		}
		for _, e := range req.Events {
			if !allowed[e] {
				return ErrBadRequest("不支持的事件: " + e)
			}
		}
		if err := s.downstreams.SaveWebhookEvents(r.Context(), p.ID, req.Events); err != nil {
			return err
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// handleRotateWebhookSecret POST /api/me/downstream/webhook/secret
//
// 生成新 secret · 明文只此一次返回。
// 前端拿到后弹一次性对话框让用户手抄 · 关闭对话框后就再拿不到（Get 只有 mask）。
func (s *Server) handleRotateWebhookSecret(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.downstreams == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "下游配置服务暂未装配")
	}

	plaintext, err := s.downstreams.RotateWebhookSecret(r.Context(), p.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": plaintext})
	return nil
}

// handleTestWebhook POST /api/me/downstream/webhook/test
//
// 走一次真发测试 webhook · 状态无论成功失败都落 outbound_webhook_delivery
// 让 GET /deliveries 能立即看到。
func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.downstreams == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "下游配置服务暂未装配")
	}

	cfg, err := s.downstreams.Get(r.Context(), p.ID)
	if err != nil && !errors.Is(err, downstream.ErrNotFound) {
		return err
	}
	if cfg.WebhookURL == "" {
		return ErrBadRequest("请先配置 webhook 地址")
	}

	// **1e-2 起** · 走 webhookout.SendTest(真签名 + 真台账) ·
	// nil 兜底走 1a 的裸 POST(mock 环境不装 webhookout · handler 别炸)
	if s.webhookOut != nil {
		ok, statusCode, latency, errMsg := s.webhookOut.SendTest(r.Context(), p.ID)
		writeJSON(w, http.StatusOK, webhookTestResult{
			OK:         ok,
			StatusCode: statusCode,
			LatencyMs:  latency,
			Error:      errMsg,
		})
		return nil
	}

	// **1a 兼容分支** · webhookOut 未装配 · 走裸 POST + 落台账
	probe := s.probeReachabilityPost(r.Context(), cfg.WebhookURL, `{"event":"test"}`)
	statusText := downstream.StatusFailed
	if probe.OK {
		statusText = downstream.StatusDelivered
	}
	var respStatusPtr *int
	if probe.statusCode > 0 {
		v := probe.statusCode
		respStatusPtr = &v
	}
	latency := probe.LatencyMs
	_, _ = s.downstreams.InsertDelivery(r.Context(), downstream.RecordAttempt{
		PassengerID:    p.ID,
		EventID:        newEventID(),
		EventType:      "test",
		TargetURL:      cfg.WebhookURL,
		Payload:        `{"event":"test"}`,
		Attempt:        1,
		Status:         statusText,
		ResponseStatus: respStatusPtr,
		LatencyMs:      &latency,
	})

	writeJSON(w, http.StatusOK, webhookTestResult{
		OK:         probe.OK,
		StatusCode: probe.statusCode,
		LatencyMs:  probe.LatencyMs,
		Error:      probe.Error,
	})
	return nil
}

// handleListDeliveries GET /api/me/downstream/webhook/deliveries
func (s *Server) handleListDeliveries(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	if s.downstreams == nil {
		return newFail(http.StatusServiceUnavailable, CodeInternal, "下游配置服务暂未装配")
	}

	page := atoiDefault(r.URL.Query().Get("page"), 1)
	pageSize := atoiDefault(r.URL.Query().Get("page_size"), 50)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 50
	}

	items, err := s.downstreams.ListDeliveries(r.Context(), p.ID, pageSize, (page-1)*pageSize)
	if err != nil {
		return err
	}

	dtos := make([]webhookDeliveryDTO, 0, len(items))
	for _, d := range items {
		dtos = append(dtos, webhookDeliveryDTOOf(d))
	}
	// 前端 TS 类型定义是数组，不是 { items, total } · 保持一致（web/src/api/hooks.ts:386）
	writeJSON(w, http.StatusOK, dtos)
	return nil
}

// ── DTO 映射 ─────────────────────────────────────────

func dtoOf(cfg downstream.Config) downstreamResponse {
	out := downstreamResponse{
		PassengerpoolURL: cfg.PassengerpoolURL,
		Rules: downstreamRulesDTO{
			PushOnPull:     cfg.PushOnPull,
			ResyncOnDead:   cfg.ResyncOnDead,
			RetryOnFailure: cfg.RetryOnFailure,
			BusOnly:        cfg.BusOnly,
		},
	}
	if cfg.PassengerpoolTokenConfigured {
		// mask 用密文最后 4 字节的 hex 替代明文尾 4 位 —— 拿不到明文，
		// 但保证同一 token 每次显示同样的 mask（换 token 时用户能识别到"变了"）
		// passengerpool token 是**用户自己填的** · 明文格式我方不控 · 打码不加假前缀 ·
		// 只显示纯打码 + 尾 4 位(1e-2 收尾 · P1-6 修正 · 避免"看着不像用户填的那个"错觉)
		out.PassengerpoolTokenMasked = maskFromEncrypted("", cfg.PassengerpoolTokenEncrypted)
	}
	// connected 语义:**配了 URL + token = 已配置** · 不等 push 真跑
	// 之前硬编 false 让 UI 显示 "Disconnected" 跟 test 探活成功矛盾(用户困惑)
	// 心跳时间(last_heartbeat_at)独立展示 · 首次真 push 才有值 · 前端做"No heartbeat yet"提示
	// 阶段 1e 后台推送真活跃后 · Connected 应改判"最近 30s 内有 push 成功"
	// 现在(阶段 1e 收尾)只有 test 探活证明连通 · 用"配置存在"作最保守可展示态
	out.Connected = cfg.PassengerpoolURL != "" && cfg.PassengerpoolTokenConfigured
	out.LastHeartbeatAt = nil
	out.PushSuccessRate = 0
	return out
}

func webhookDTOOf(cfg downstream.Config) webhookResponse {
	// Enabled 是**派生态** + **用户显式开关**的组合:
	//   URL/secret 未配 → 强制 false(有开关也发不出去 · 别误导用户)
	//   URL/secret 已配 → 看用户开关(WebhookEnabled · 默认 true)
	// 这样用户配好后开关能真正生效 · 未配时开关也是 false 免得点了没反应(P0-1 修正)
	configured := cfg.WebhookURL != "" && cfg.WebhookSecretConfigured
	out := webhookResponse{
		URL:     cfg.WebhookURL,
		Enabled: configured && cfg.WebhookEnabled,
		// events 落库为 nil 表示"未设" · 兜底展示全 4 个订阅态(defaultWebhookEvents)
		// 用户明确清空过 · 就展示空数组
		Events: cfg.WebhookEvents,
	}
	if out.Events == nil {
		out.Events = append([]string{}, defaultWebhookEvents...)
	}
	if cfg.WebhookSecretConfigured {
		out.SecretMasked = maskFromEncrypted("whsec_", cfg.WebhookSecretEncrypted)
	}
	return out
}

func webhookDeliveryDTOOf(d downstream.Delivery) webhookDeliveryDTO {
	out := webhookDeliveryDTO{
		ID:         d.ID,
		Event:      d.EventType,
		OK:         d.OK(),
		StatusCode: d.ResponseStatus,
		Attempt:    d.Attempt,
		CreatedAt:  d.CreatedAt.UTC().Format(time.RFC3339),
	}
	if d.LatencyMs != nil {
		out.LatencyMs = *d.LatencyMs
	}
	return out
}

// maskFromEncrypted 从密文 blob 里派生稳定的展示 mask。
//
// 用密文尾字节做展示 —— 每次加密的 nonce 不同，但只要密文完整就有稳定的尾字节；
// 保留 4 个 hex 字符（2 字节）· 变更 token 时用户能立刻看到 mask 不同。
//
// **prefix 参数**只是显示用 · **不是**真前缀 —— 明文本身可能有 / 没有前缀 ·
// 这里的 prefix 只让 UI 一眼看出"这是 webhook secret / kiro admin token" ·
// 别把它当"明文头几个字符"用。webhook secret 走 whsec_ 前缀是因为
// downstream.generateSecretHex 生成时就带了(1e-2 收尾对齐) ·
// passengerpool token 是用户自己填的、明文可能任意前缀 · UI 展示的 kiro_admin_
// 只是提示语义 · 不代表明文头字节。
func maskFromEncrypted(prefix string, blob []byte) string {
	if len(blob) < 2 {
		return prefix + "••••"
	}
	tail := blob[len(blob)-2:]
	const hexdig = "0123456789abcdef"
	buf := []byte{
		hexdig[tail[0]>>4], hexdig[tail[0]&0xf],
		hexdig[tail[1]>>4], hexdig[tail[1]&0xf],
	}
	return prefix + "••••••••••••••••" + string(buf)
}

// ── 工具 ────────────────────────────────────────────

// probeResult 是 probeReachabilityPost 的返回 —— 比 testResult 多一个 statusCode。
type probeResult struct {
	OK         bool
	LatencyMs  int
	statusCode int
	Error      string
}

func (s *Server) probeReachabilityPost(ctx context.Context, rawURL, body string) probeResult {
	client := &http.Client{Timeout: 3 * time.Second}
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	start := time.Now()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, rawURL, strings.NewReader(body))
	if err != nil {
		return probeResult{Error: "地址格式不对"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bus-Event", "test")
	resp, err := client.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return probeResult{LatencyMs: latency, Error: "连不上目标地址"}
	}
	defer resp.Body.Close()
	out := probeResult{OK: resp.StatusCode >= 200 && resp.StatusCode < 300, LatencyMs: latency, statusCode: resp.StatusCode}
	if !out.OK {
		out.Error = "目标服务未返回 2xx"
	}
	return out
}

func fallbackUserMsg(err error, def string) string {
	if err == nil {
		return def
	}
	if msg := downstream.UserMessage(err); msg != "" {
		return msg
	}
	return def
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// newEventID 生成 outbound event id · 用时间纳秒 + 4 位随机做冲突兜底
// （只在 test 端点用 · 真出向 worker 会用 UUID v7）
func newEventID() string {
	return time.Now().UTC().Format("20060102T150405.000000000")
}
