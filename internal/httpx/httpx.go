// Package httpx 统一所有出向 HTTP（CLAUDE.md §7.1：proxy / timeout / no_proxy 统一）。
//
// vendor 和 housepool 的 client 都必须走这里，不要各自 new http.Client —— 否则
// 代理配置和重试策略会各处不一致，排查上游问题时说不清是谁的行为。
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Timeout       time.Duration
	MaxRetries    int
	RetryBaseWait time.Duration
	Proxy         string
	NoProxy       string
}

type Client struct {
	hc  *http.Client
	cfg Config
	// sleep 可注入，测试里不用真的等
	sleep func(time.Duration)
}

func New(cfg Config) (*Client, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.RetryBaseWait <= 0 {
		cfg.RetryBaseWait = 500 * time.Millisecond
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Proxy != "" {
		pu, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("httpx: 代理地址非法 %q: %w", cfg.Proxy, err)
		}
		noProxy := parseNoProxy(cfg.NoProxy)
		tr.Proxy = func(req *http.Request) (*url.URL, error) {
			if matchNoProxy(req.URL.Hostname(), noProxy) {
				return nil, nil
			}
			return pu, nil
		}
	}

	return &Client{
		hc:    &http.Client{Timeout: cfg.Timeout, Transport: tr},
		cfg:   cfg,
		sleep: time.Sleep,
	}, nil
}

func parseNoProxy(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}

func matchNoProxy(host string, noProxy []string) bool {
	host = strings.ToLower(host)
	for _, np := range noProxy {
		if host == np || strings.HasSuffix(host, "."+strings.TrimPrefix(np, ".")) {
			return true
		}
	}
	return false
}

// Response 是读完 body 的响应 —— 调用方不用管 Close，也就不会漏。
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	// Attempts 实际请求了几次（1 = 一次成功）· 排查上游抖动时有用
	Attempts int
}

// Do 发请求，按策略重试。
//
// 重试条件（**只重试语义上安全的**）：
//   - 网络错误 / 超时
//   - 429（看 Retry-After）
//   - 5xx
//
// **不重试** 4xx（除 429）—— 那是请求本身的问题，重试改变不了结果。
// 幂等性由调用方保证（拉号带 client_order_id，见 09-transactions §2）。
func (c *Client) Do(ctx context.Context, req *http.Request) (*Response, error) {
	var lastErr error

	for attempt := 1; attempt <= c.cfg.MaxRetries+1; attempt++ {
		// 每次重试都要能重放 body
		if req.Body != nil && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("httpx: 重放请求体: %w", err)
			}
			req.Body = body
		}

		resp, err := c.hc.Do(req.WithContext(ctx))
		if err != nil {
			lastErr = err
			// ctx 取消/超时就别重试了 —— 上层已经不等了
			if ctx.Err() != nil {
				return nil, fmt.Errorf("httpx: %w", ctx.Err())
			}
			if attempt <= c.cfg.MaxRetries {
				if werr := c.wait(ctx, attempt, 0); werr != nil {
					return nil, werr
				}
				continue
			}
			return nil, fmt.Errorf("httpx: 请求失败（试了 %d 次）: %w", attempt, lastErr)
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt <= c.cfg.MaxRetries {
				if werr := c.wait(ctx, attempt, 0); werr != nil {
					return nil, werr
				}
				continue
			}
			return nil, fmt.Errorf("httpx: 读响应体: %w", readErr)
		}

		out := &Response{
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Body:       body,
			Attempts:   attempt,
		}

		if shouldRetry(resp.StatusCode) && attempt <= c.cfg.MaxRetries {
			if werr := c.wait(ctx, attempt, retryAfter(resp.Header)); werr != nil {
				return nil, werr
			}
			continue
		}
		return out, nil
	}

	return nil, fmt.Errorf("httpx: 重试用尽: %w", lastErr)
}

// Stream 发请求并把 **未读的** 响应交给调用方，用于 SSE / 长流。
//
// 跟 Do 的区别：Do 会把 body 整体读完再返回，流式响应下那意味着「等流结束才拿到
// 第一个字节」—— 进度事件全白等，长流还可能撞上 client timeout。
//
// **调用方负责 resp.Body.Close()**。
//
// **不重试** —— 流可能已经产生了副作用（BatchImport 已导进去几个号），重放会重复导入。
// 幂等性由调用方保证。
func (c *Client) Stream(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	// 流式响应不能有整体 timeout（长导入会被砍断）· 超时靠 ctx 控制
	streamClient := &http.Client{Transport: c.hc.Transport}
	resp, err := streamClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("httpx: 建流失败: %w", err)
	}
	return resp, nil
}

func shouldRetry(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// retryAfter 解析 Retry-After（秒数形式）· 0 = 没有或不合法
func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// wait 指数退避 · 上游给了 Retry-After 就听它的
func (c *Client) wait(ctx context.Context, attempt int, hint time.Duration) error {
	d := hint
	if d <= 0 {
		d = time.Duration(float64(c.cfg.RetryBaseWait) * math.Pow(2, float64(attempt-1)))
	}
	// 别让退避超过整体超时太多
	if max := 30 * time.Second; d > max {
		d = max
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("httpx: 等待重试时 %w", ctx.Err())
	case <-timeAfter(d, c.sleep):
		return nil
	}
}

// timeAfter 把可注入的 sleep 适配成 channel，方便测试
func timeAfter(d time.Duration, sleep func(time.Duration)) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		sleep(d)
		close(ch)
	}()
	return ch
}

var ErrNilRequest = errors.New("httpx: request 为空")
