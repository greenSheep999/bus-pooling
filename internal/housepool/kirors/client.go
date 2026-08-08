// Package kirors 是 housepool.HousePool 针对 kiro.rs 的实现。
//
// **上层不 import 这个包** —— 只 import internal/housepool（契约 §1）。
//
// 所有出向请求走 internal/httpx（proxy / timeout / retry 统一 · CLAUDE.md §7.1）。
// admin key 从 internal/secrets 拿，明文不落文件。
package kirors

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
)

// adminPrefix 所有 admin 端点的前缀（kiro.rs src/main.rs 的 .nest("/api/admin", …)）。
// Config.BaseURL 只填到域名，这个前缀由 client 内部拼（契约 §10b ⑤）。
const adminPrefix = "/api/admin"

// authHeader kiro.rs 支持 x-api-key 或 Authorization: Bearer，我方统一用前者。
const authHeader = "x-api-key"

type Config struct {
	// BaseURL 只到域名，例 "https://kiro.aibbq.xyz" —— 别带 /api/admin
	BaseURL string
	// AdminKey 明文 · 由调用方从 secrets 解出来传进来
	AdminKey string
}

type Client struct {
	baseURL  string
	adminKey string
	http     *httpx.Client
}

// 编译期确认实现了完整接口 —— 少一个方法在这里就报错，而不是运行时才发现
var _ housepool.HousePool = (*Client)(nil)

func New(cfg Config, hc *httpx.Client) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("kirors: BaseURL 不能为空")
	}
	if cfg.AdminKey == "" {
		return nil, fmt.Errorf("kirors: AdminKey 不能为空")
	}
	if hc == nil {
		return nil, fmt.Errorf("kirors: 必须传 httpx.Client（出向请求要统一走它）")
	}
	return &Client{
		baseURL:  strings.TrimRight(cfg.BaseURL, "/"),
		adminKey: cfg.AdminKey,
		http:     hc,
	}, nil
}

func (c *Client) Close() error { return nil }

// ── 请求底座 ────────────────────────────────────────

func (c *Client) urlFor(path string, query url.Values) string {
	u := c.baseURL + adminPrefix + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// do 发请求并按状态码归类错误。
//
// out 非 nil 时把响应体解到它里面。
func (c *Client) do(ctx context.Context, op, method, path string, query url.Values, body, out any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("kirors: %s 编码请求: %w", op, err)
		}
		rdr = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, c.urlFor(path, query), rdr)
	if err != nil {
		return fmt.Errorf("kirors: %s 建请求: %w", op, err)
	}
	req.Header.Set(authHeader, c.adminKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		// 网络层失败（含重试用尽）→ 号池不可用
		return &housepool.Error{Op: op, Err: housepool.ErrUnavailable, Message: err.Error()}
	}

	if err := classify(op, resp.StatusCode, resp.Body); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return fmt.Errorf("kirors: %s 解响应: %w（原文前 200 字节: %s）",
			op, err, truncate(resp.Body, 200))
	}
	return nil
}

// classify 把 HTTP 状态映射到 housepool 的 sentinel。
func classify(op string, status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}

	var we wireError
	_ = json.Unmarshal(body, &we) // 解不出来就用空文本
	msg := we.text()
	if msg == "" {
		msg = truncate(body, 200)
	}

	e := &housepool.Error{Op: op, Status: status, Message: msg}
	switch {
	case status == http.StatusNotFound:
		e.Err = housepool.ErrNotFound
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		e.Err = housepool.ErrUnauthorized
	case status == http.StatusConflict:
		e.Err = housepool.ErrConflict
	case status >= 500:
		e.Err = housepool.ErrUnavailable
	default:
		// 4xx 其它情况：是我方请求有问题，不该重试也不该当成号池挂了
		e.Err = fmt.Errorf("kirors: %s 请求被拒绝", op)
	}
	return e
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

func idPath(prefix string, id uint64, suffix string) string {
	return prefix + "/" + strconv.FormatUint(id, 10) + suffix
}

// ── Credential ──────────────────────────────────────

func (c *Client) ListCredentials(ctx context.Context, filter housepool.CredentialFilter) ([]housepool.Credential, *housepool.PoolSnapshot, error) {
	q := url.Values{}
	// kiro.rs 侧一次返全量，group 过滤在我方做 —— 池子规模（百级）下这样更简单，
	// 也避免依赖它的 query 参数语义（那个没在契约里固定）
	var wire wireCredentialList
	if err := c.do(ctx, "ListCredentials", http.MethodGet, "/credentials", q, nil, &wire); err != nil {
		return nil, nil, err
	}

	snap := toSnapshot(wire)
	out := make([]housepool.Credential, 0, len(wire.Credentials))
	for _, w := range wire.Credentials {
		cred := toCredential(w)
		if !filter.IncludeDisabled && cred.Disabled {
			continue
		}
		if len(filter.Groups) > 0 && !hasAnyGroup(cred.Groups, filter.Groups) {
			continue
		}
		out = append(out, cred)
	}
	return out, &snap, nil
}

func hasAnyGroup(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if h == w {
				return true
			}
		}
	}
	return false
}

func (c *Client) GetCredential(ctx context.Context, id housepool.CredentialID) (*housepool.Credential, error) {
	// kiro.rs 没有单号 GET —— 从列表里挑（契约 §11 的端点表里 GetCredential 对应
	// GET /credentials/{id}，但源码里那个路由只有 delete/put，所以走列表过滤）
	creds, _, err := c.ListCredentials(ctx, housepool.CredentialFilter{IncludeDisabled: true})
	if err != nil {
		return nil, err
	}
	for i := range creds {
		if creds[i].ID == id {
			return &creds[i], nil
		}
	}
	return nil, &housepool.Error{
		Op: "GetCredential", Status: http.StatusNotFound, Err: housepool.ErrNotFound,
		Message: fmt.Sprintf("号池里没有 id=%d", id),
	}
}

func (c *Client) UpdateCredential(ctx context.Context, id housepool.CredentialID, patch housepool.CredentialPatch) error {
	body := wireUpdateCredentialRequest{
		Email:            patch.Email,
		ProxyURL:         patch.ProxyURL,
		ProxyUsername:    patch.ProxyUsername,
		ProxyPassword:    patch.ProxyPassword,
		Groups:           patch.Groups,
		SourceChannel:    patch.SourceChannel,
		ConcurrencyLimit: patch.ConcurrencyLimit,
	}
	return c.do(ctx, "UpdateCredential", http.MethodPut,
		idPath("/credentials", uint64(id), ""), nil, body, nil)
}

func (c *Client) SetDisabled(ctx context.Context, id housepool.CredentialID, disabled bool) error {
	// body 只有 disabled —— 传不进自定义 reason，kiro.rs 一律写 Manual（契约 §10b ④）
	return c.do(ctx, "SetDisabled", http.MethodPost,
		idPath("/credentials", uint64(id), "/disabled"), nil,
		wireSetDisabledRequest{Disabled: disabled}, nil)
}

func (c *Client) SetDisabledBatch(ctx context.Context, ids []housepool.CredentialID, disabled bool) error {
	if len(ids) == 0 {
		return nil
	}
	return c.do(ctx, "SetDisabledBatch", http.MethodPost,
		"/credentials/batch/disabled", nil,
		wireBatchSetDisabledRequest{IDs: rawIDs(ids), Disabled: disabled}, nil)
}

func (c *Client) DeleteCredential(ctx context.Context, id housepool.CredentialID) error {
	return c.do(ctx, "DeleteCredential", http.MethodDelete,
		idPath("/credentials", uint64(id), ""), nil, nil, nil)
}

func (c *Client) DeleteCredentialBatch(ctx context.Context, ids []housepool.CredentialID) error {
	if len(ids) == 0 {
		return nil
	}
	return c.do(ctx, "DeleteCredentialBatch", http.MethodPost,
		"/credentials/batch/delete", nil,
		wireBatchDeleteRequest{IDs: rawIDs(ids)}, nil)
}

func rawIDs(ids []housepool.CredentialID) []uint64 {
	out := make([]uint64, len(ids))
	for i, id := range ids {
		out[i] = uint64(id)
	}
	return out
}

func (c *Client) GetBalance(ctx context.Context, id housepool.CredentialID) (*housepool.Balance, error) {
	var wire wireBalance
	if err := c.do(ctx, "GetBalance", http.MethodGet,
		idPath("/credentials", uint64(id), "/balance"), nil, nil, &wire); err != nil {
		return nil, err
	}
	b := toBalance(wire)
	return &b, nil
}

// TestCredential 探活。
//
// **这是唯一可靠的判死手段**（契约 §DisabledReason 判据）：返回 error 即判死。
// 对 disabled 的号也能调 —— kiro.rs 的 prepare_request_token 不看 disabled。
func (c *Client) TestCredential(ctx context.Context, id housepool.CredentialID) error {
	return c.do(ctx, "TestCredential", http.MethodPost,
		idPath("/credentials", uint64(id), "/test"), nil, nil, nil)
}

func (c *Client) RefreshToken(ctx context.Context, id housepool.CredentialID) error {
	return c.do(ctx, "RefreshToken", http.MethodPost,
		idPath("/credentials", uint64(id), "/refresh"), nil, nil, nil)
}

// ── BatchImport（SSE） ──────────────────────────────

// BatchImport 走 SSE 流。
//
// kiro.rs 返回的是**一个**流，summary 是流里 status=="summary" 的最后一个事件
// （契约 §10b ③）。我方拆成两个 channel 让上层用起来清楚。
//
// 上层用法：
//
//	res, err := hp.BatchImport(ctx, req)
//	for ev := range res.Events { ... }        // 读到关闭
//	sum := <-res.Summary                      // 取一次
//	if err := res.Err(); err != nil { ... }   // 流是否中断
func (c *Client) BatchImport(ctx context.Context, req housepool.BatchImportRequest) (*housepool.BatchImportResult, error) {
	if len(req.Credentials) == 0 {
		return nil, fmt.Errorf("kirors: BatchImport 至少要一个凭证")
	}

	body := wireBatchImportRequest{
		Credentials: make([]wireImportCredential, 0, len(req.Credentials)),
	}
	for _, cr := range req.Credentials {
		body.Credentials = append(body.Credentials, toImportCredential(cr))
	}
	if req.Concurrency > 0 {
		body.Concurrency = &req.Concurrency
	}
	// Verify 显式传 —— 默认值语义由号池定，我方别猜
	verify := req.Verify
	body.Verify = &verify

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("kirors: BatchImport 编码请求: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.urlFor("/credentials/batch-import", nil), bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("kirors: BatchImport 建请求: %w", err)
	}
	httpReq.Header.Set(authHeader, c.adminKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	// SSE **不走 httpx.Do** —— 那个会把 body 整体读完（流式响应会一直读到结束，
	// 拿不到中间进度，而且长流可能撞上 client timeout）。这里直接用底层 client 流式读。
	resp, err := c.http.Stream(ctx, httpReq)
	if err != nil {
		return nil, &housepool.Error{Op: "BatchImport", Err: housepool.ErrUnavailable, Message: err.Error()}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, classify("BatchImport", resp.StatusCode, b)
	}

	events := make(chan housepool.BatchImportEvent, 16)
	summary := make(chan housepool.BatchImportSummary, 1)
	var streamErr error

	go func() {
		defer resp.Body.Close()
		defer close(events)
		defer close(summary)
		streamErr = readSSE(resp.Body, events, summary)
	}()

	return &housepool.BatchImportResult{
		Events:  events,
		Summary: summary,
		Err:     func() error { return streamErr },
	}, nil
}

// readSSE 解 SSE 流。格式是 `data: {json}\n\n`。
func readSSE(r io.Reader, events chan<- housepool.BatchImportEvent, summary chan<- housepool.BatchImportSummary) error {
	sc := bufio.NewScanner(r)
	// 单条事件可能比默认 64KiB 大（凭证多时的 summary）
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue // 空行是事件分隔，冒号开头是注释/心跳
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue // event: / id: 之类的字段我方不用
		}
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			continue
		}

		var w wireBatchImportEvent
		if err := json.Unmarshal([]byte(data), &w); err != nil {
			// 单条解不开就跳过，别让整个导入失败 —— 其它号还在正常导
			continue
		}

		if w.Status == string(housepool.ImportStatusSummary) {
			if w.Summary != nil {
				summary <- toBatchImportSummary(*w.Summary)
			}
			continue
		}
		events <- toBatchImportEvent(w)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("kirors: 读导入流中断: %w", err)
	}
	return nil
}

// ── Group ───────────────────────────────────────────

func (c *Client) ListGroups(ctx context.Context) ([]housepool.Group, error) {
	var wire wireGroupList
	if err := c.do(ctx, "ListGroups", http.MethodGet, "/groups", nil, nil, &wire); err != nil {
		return nil, err
	}
	out := make([]housepool.Group, 0, len(wire.Groups))
	for _, w := range wire.Groups {
		out = append(out, toGroup(w))
	}
	return out, nil
}

func (c *Client) CreateGroup(ctx context.Context, req housepool.GroupRequest) (*housepool.Group, error) {
	body := wireCreateGroupRequest{Name: req.Name}
	if req.Description != "" {
		body.Description = &req.Description
	}
	if req.CacheMode != "" {
		body.CacheMode = &req.CacheMode
	}
	if req.CacheMetering != "" {
		body.CacheMetering = &req.CacheMetering
	}
	if req.CompactThreshold > 0 {
		body.CompactThreshold = &req.CompactThreshold
	}

	var wire wireGroup
	if err := c.do(ctx, "CreateGroup", http.MethodPost, "/groups", nil, body, &wire); err != nil {
		return nil, err
	}
	// 有些实现建完只返 204 —— 那时 wire 是空的，用请求值兜住
	if wire.Name == "" {
		wire.Name = req.Name
	}
	g := toGroup(wire)
	return &g, nil
}

func (c *Client) UpdateGroup(ctx context.Context, name string, req housepool.GroupRequest) error {
	body := wireUpdateGroupRequest{}
	// 改名：req.Name 跟当前名不同才传 newName
	if req.Name != "" && req.Name != name {
		body.NewName = &req.Name
	}
	if req.Description != "" {
		body.Description = &req.Description
	}
	if req.CacheMode != "" {
		body.CacheMode = &req.CacheMode
	}
	if req.CacheMetering != "" {
		body.CacheMetering = &req.CacheMetering
	}
	if req.CompactThreshold > 0 {
		body.CompactThreshold = &req.CompactThreshold
	}
	return c.do(ctx, "UpdateGroup", http.MethodPatch,
		"/groups/"+url.PathEscape(name), nil, body, nil)
}

func (c *Client) DeleteGroup(ctx context.Context, name string) error {
	return c.do(ctx, "DeleteGroup", http.MethodDelete,
		"/groups/"+url.PathEscape(name), nil, nil, nil)
}

// ── Ping ────────────────────────────────────────────

// Ping 用 /groups 当探针 —— 它够轻且要求鉴权通过，能同时验"连得上"和"key 对"。
func (c *Client) Ping(ctx context.Context) error {
	var wire wireGroupList
	return c.do(ctx, "Ping", http.MethodGet, "/groups", nil, nil, &wire)
}

// ── 1a 不实现的（ClientKey / Stats / 并发） ─────────
//
// 留在接口里是因为 interface 要完整；实现推到用得上的 sprint：
// ClientKey → 1e（推 passengerpool 才需要发 key）· Stats → 1d（采数据）

func (c *Client) ListClientKeys(context.Context, housepool.ClientKeyFilter) ([]housepool.ClientKey, error) {
	return nil, notYet("ListClientKeys", "1e")
}

func (c *Client) CreateClientKey(context.Context, housepool.ClientKeyRequest) (*housepool.ClientKey, error) {
	return nil, notYet("CreateClientKey", "1e")
}

func (c *Client) RotateClientKey(context.Context, housepool.ClientKeyID) (*housepool.ClientKey, error) {
	return nil, notYet("RotateClientKey", "1e")
}

func (c *Client) UpdateClientKey(context.Context, housepool.ClientKeyID, housepool.ClientKeyRequest) error {
	return notYet("UpdateClientKey", "1e")
}

func (c *Client) DeleteClientKey(context.Context, housepool.ClientKeyID) error {
	return notYet("DeleteClientKey", "1e")
}

func (c *Client) SetClientKeyDisabled(context.Context, housepool.ClientKeyID, bool) error {
	return notYet("SetClientKeyDisabled", "1e")
}

func (c *Client) StatsOverview(context.Context) (*housepool.StatsOverview, error) {
	return nil, notYet("StatsOverview", "1d")
}

func (c *Client) StatsByCredential(context.Context, housepool.StatsOptions) ([]housepool.CredentialStats, error) {
	return nil, notYet("StatsByCredential", "1d")
}

func (c *Client) StatsByModel(context.Context, housepool.StatsOptions) ([]housepool.ModelStats, error) {
	return nil, notYet("StatsByModel", "1d")
}

func (c *Client) StatsTimeSeries(context.Context, housepool.StatsOptions) ([]housepool.TimeSeriesPoint, error) {
	return nil, notYet("StatsTimeSeries", "1d")
}

// GetConcurrency 号池没有这个端点（契约 §7）· 恒返回 ErrNotSupported。
// 上层拿到它应该把 concurrency_avg 填 null，UI 显示 "—"。
func (c *Client) GetConcurrency(context.Context, housepool.CredentialID) (*housepool.Concurrency, error) {
	return nil, &housepool.Error{
		Op: "GetConcurrency", Err: housepool.ErrNotSupported,
		Message: "号池未提供并发查询端点（见 08-housepool-contract §7）",
	}
}

func notYet(op, sprint string) error {
	return &housepool.Error{
		Op: op, Err: housepool.ErrNotSupported,
		Message: fmt.Sprintf("阶段 1a 未实现（计划 %s）", sprint),
	}
}
