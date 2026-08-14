// Package kirors 是 passengerpool.Pusher 走 housepool 后端 协议的**一次性**客户端。
//
// **上层不 import 这个包** —— 只 import internal/delivery/passengerpool(接口层)。
//
// 关键差别(跟 housepool/kirors)：
//   - admin_token 一次性 · New(cfg) 后调完 Push 就 Close 扔掉
//   - Client struct **不保存** adminKey 字段(每次请求前从传入 cfg.AdminKey 现取)
//   - 只实现 BatchImport(SSE 流) + Ping · 不实现 List / Update / Delete / Group
//   - 对家协议错分类走 passengerpool.ErrorKind(不复用 housepool.Error)
package kirors

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/httpx"
)

// adminPrefix 跟 housepool 一致 · 对家(乘客自建)housepool 后端 也用同一个路径。
const adminPrefix = "/api/admin"

// authHeader 对家一律 x-api-key(跟 housepool client 保持一致 · 别引第二种鉴权方式)。
const authHeader = "x-api-key"

// Config 是构造一次性 Client 的入参。
//
// **BaseURL** 只到域名 · 别带 /api/admin (跟 housepool 契约 §10b ⑤ 一致)
// **AdminKey** 明文 · 由 Pusher 主流程从 downstream.DecryptPassengerpoolToken 现取。
type Config struct {
	BaseURL  string
	AdminKey string
}

// Client 是一次性 housepool 后端 客户端。
//
// **不保存 adminKey 字段** —— 每次请求从入参 cfg.AdminKey 通过 req header 传 ·
// 用完 GC 掉 client · 减少明文在内存里的时间窗口。
//
// httpx.Client 长期复用没问题(它是 transport 池 · 不带凭证)。
type Client struct {
	baseURL string
	// adminKey 只在 Client 实例内短暂持有 · Close() 后清空 · Pusher 端 defer Close。
	// **不 log · 不 marshal · 不出 String()**。
	adminKey string
	http     *httpx.Client
}

// New 建一个一次性 Client。
//
// **调用方 defer Close()** —— 别忘了扔明文。
func New(cfg Config, hc *httpx.Client) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("kirors(passengerpool): BaseURL 不能为空")
	}
	if cfg.AdminKey == "" {
		return nil, fmt.Errorf("kirors(passengerpool): AdminKey 不能为空")
	}
	if hc == nil {
		return nil, fmt.Errorf("kirors(passengerpool): 必须传 httpx.Client")
	}
	return &Client{
		baseURL:  strings.TrimRight(cfg.BaseURL, "/"),
		adminKey: cfg.AdminKey,
		http:     hc,
	}, nil
}

// Close 清空 adminKey 明文引用 · 之后再调 Client 方法会 fail(拒服务)。
//
// 幂等 · Pusher 端 defer Close · 主流程正常返回也调 · 保证明文最短生命周期。
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.adminKey = ""
}

func (c *Client) urlFor(path string, query url.Values) string {
	u := c.baseURL + adminPrefix + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// Ping 用 /groups 端点探连通 · 跟 housepool 一致。
//
// 独立方法给"测试对家 URL"入口用 · 但**不 log adminKey**。
func (c *Client) Ping(ctx context.Context) error {
	if c.adminKey == "" {
		return errClosed()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor("/groups", nil), nil)
	if err != nil {
		return fmt.Errorf("kirors(passengerpool): 建 Ping 请求: %w", err)
	}
	req.Header.Set(authHeader, c.adminKey)
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return classifyNetwork("Ping", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return classifyStatus("Ping", resp.StatusCode, resp.Body)
}

// BatchImportResult 是一次导入的结果 · 客户端已经消费完 SSE。
//
// 每号一个状态 · Verified/Duplicate 视为成功 · Failed 视为失败(带错误文本)。
// **不返 credentialId** —— 我方对家的号 id 不需要落库(单向复制)。
type BatchImportResult struct {
	// PerIndex[i] 对应请求 Credentials[i] 的结果
	PerIndex []EventResult
	// Summary 对家给的最终统计(可能跟 PerIndex 计数不一致 · 以 Summary 为准)
	Summary SummaryResult
}

// EventResult 是 SSE 一条事件抽出的结果(外部可见 · 上层按 Index 映射回原批)。
type EventResult struct {
	Index    int
	Status   string // verified | duplicate | failed
	Error    string
	Verified bool
}

// SummaryResult 是流末 summary 事件(对家给的最终统计)。
type SummaryResult struct {
	Total      int
	Imported   int
	Verified   int
	Duplicate  int
	Failed     int
	RolledBack int
}

// StreamError 是 SSE 流层的错误(网络断 / 5xx / 401)· 上层归错分类用。
//
// 独立类型让上层不用依赖 passengerpool 包(依赖循环)· Pusher 端 As 判类后映射到 ErrorKind。
type StreamError struct {
	Op string
	// Status HTTP 状态码 · 0 = 网络层错误
	Status int
	// Message 对家给的原文(如果有 · 已 truncate 到 200)
	Message string
	// Kind 内部分类 · 上层直接照抄成 passengerpool.ErrorKind
	Kind string
	// Cause 底层网络错误(仅调试用)
	Cause error
}

func (e *StreamError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("kirors(passengerpool): %s(HTTP %d): %s", e.Op, e.Status, e.Message)
	}
	return fmt.Sprintf("kirors(passengerpool): %s: %s", e.Op, e.Message)
}

func (e *StreamError) Unwrap() error { return e.Cause }

// BatchImport 把一批号推给对家 · 消费完整 SSE 流后返回。
//
// **一次性调完就 Close** · 明文只在 header 里活到 http 发送完成。
//
// 对家协议错走 SSE 解析 · 归到 wire.Error 字段 · Pusher 端映射到 passengerpool.ErrorKind。
// HTTP 层错误(401 / 5xx / 超时 / SSE 断流)走 *StreamError 分类。
func (c *Client) BatchImport(ctx context.Context, creds []ImportInput) (*BatchImportResult, error) {
	if c.adminKey == "" {
		return nil, errClosed()
	}
	if len(creds) == 0 {
		return nil, fmt.Errorf("kirors(passengerpool): BatchImport 至少要一个凭证")
	}

	body := wireBatchImportRequest{
		Credentials: make([]wireImportCredential, 0, len(creds)),
	}
	for _, c := range creds {
		body.Credentials = append(body.Credentials, toImportCredential(c))
	}
	// verify=true · 对家会走一遍探活(refresh_token / access_token 有效性) ·
	// 减少"号已进对家池但一上就是死的"的场景。**不做**可配·永远开。
	trueVal := true
	body.Verify = &trueVal

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("kirors(passengerpool): 编码 BatchImport: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.urlFor("/credentials/batch-import", nil), bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("kirors(passengerpool): 建 BatchImport 请求: %w", err)
	}
	req.Header.Set(authHeader, c.adminKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// SSE 走 Stream · **不走 Do**(会等流结束才返回 · 拿不到中间进度)
	resp, err := c.http.Stream(ctx, req)
	if err != nil {
		return nil, classifyNetwork("BatchImport", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, classifyStatus("BatchImport", resp.StatusCode, b)
	}
	defer resp.Body.Close()

	return readSSE(resp.Body)
}

// readSSE 同步消费完整 SSE 流(跟 housepool/kirors/client.go 的 readSSE 结构一致)。
//
// **同步**而非 goroutine —— Pusher.Push 只关心最终结果 · 不需要中间进度 ·
// 上层复用 Client 的 defer Close 保证明文被扔掉 · 用 goroutine 会跟 Close 时机赛跑。
func readSSE(r io.Reader) (*BatchImportResult, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	out := &BatchImportResult{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue // 空行是事件分隔 · 冒号开头是注释 / 心跳
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue // event: / id: 字段我方不用
		}
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			continue
		}

		var w wireBatchImportEvent
		if err := json.Unmarshal([]byte(data), &w); err != nil {
			continue // 单条解不开就跳过 · 别让整批失败
		}
		if w.Status == "summary" && w.Summary != nil {
			out.Summary = fromSummary(*w.Summary)
			continue
		}
		out.PerIndex = append(out.PerIndex, fromEvent(w))
	}
	if err := sc.Err(); err != nil {
		return out, &StreamError{
			Op:      "BatchImport",
			Kind:    "stream_broken",
			Message: "对家导入流中断: " + err.Error(),
			Cause:   err,
		}
	}
	return out, nil
}

// KindUnauthorized / KindNotFound / … 是 StreamError.Kind 的取值。
// 上层照抄成 passengerpool.ErrorKind · 打破依赖循环。
const (
	KindUnauthorized = "unauthorized"
	KindNotFound     = "not_found"
	KindConflict     = "conflict"
	KindTimeout      = "timeout"
	KindBadRequest   = "bad_request"
	KindStreamBroken = "stream_broken"
)

// classifyStatus 把 HTTP 错误状态映射成 StreamError。
//
// **不带对家协议名 / 内部术语**·CLAUDE.md §0.1。
func classifyStatus(op string, status int, body []byte) error {
	msg := extractMessage(body)
	if msg == "" {
		msg = truncate(body, 200)
	}
	var kind string
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		kind = KindUnauthorized
	case status == http.StatusNotFound:
		kind = KindNotFound
	case status == http.StatusConflict:
		kind = KindConflict
	case status == http.StatusRequestTimeout, status >= 500:
		kind = KindTimeout
	default:
		kind = KindBadRequest
	}
	return &StreamError{
		Op:      op,
		Kind:    kind,
		Status:  status,
		Message: msg,
	}
}

// classifyNetwork 把 httpx 层的网络错误(超时 / DNS / 重试用尽)映射成 timeout。
func classifyNetwork(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// ctx 取消 / deadline · 判成 timeout(可重试)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &StreamError{Op: op, Kind: KindTimeout, Message: msg, Cause: err}
	}
	return &StreamError{Op: op, Kind: KindTimeout, Message: msg, Cause: err}
}

// extractMessage 从对家错误响应体里挑一条能给用户看的 message。
func extractMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var we wireError
	if err := json.Unmarshal(body, &we); err == nil {
		return we.text()
	}
	return ""
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// errClosed 明文已扔 · 拒绝再服务。
func errClosed() error {
	return &StreamError{
		Op:      "any",
		Kind:    KindBadRequest,
		Message: "客户端已 Close · admin_token 明文已扔",
	}
}

