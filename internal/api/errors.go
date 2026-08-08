// Package api 是 HTTP 层：路由 / 中间件 / 请求响应编解码。
//
// 两条铁律（CLAUDE.md §12）：
//   - 返回体和 message **绝不出现内部术语**（housepool / record group / provider / 内部状态枚举）
//   - 状态对外收敛（§12.5）—— 内部多态在这一层映射成用户能看懂的少数几个
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Code 是稳定的错误标识 —— 客户端按它分派，**不按 message**（message 会改）。
// 全表见 docs/05-api-contract.md §错误码全表。
type Code string

const (
	CodeBadJSON             Code = "bad_json"
	CodeBadIdempotencyKey   Code = "bad_idempotency_key"
	CodeBadRequest          Code = "bad_request"
	CodeUnauthenticated     Code = "unauthenticated"
	CodeInvalidAPIKey       Code = "invalid_api_key"
	CodeInsufficientBalance Code = "insufficient_balance"
	CodeDisabled            Code = "disabled"
	CodeSessionRequired     Code = "session_required"
	CodeNotFound            Code = "not_found"
	CodeConflict            Code = "conflict"
	CodeIdempotencyConflict Code = "idempotency_conflict"
	CodeBodyTooLarge        Code = "body_too_large"
	CodeRateLimited         Code = "rate_limited"
	CodeInternal            Code = "internal"
)

// Error 是统一的错误响应形状。
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	// RetryAfter 秒 · 只在限流 / 上游临时不可用时给
	RetryAfter int `json:"retry_after,omitempty"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// Fail 构造一个带 HTTP 状态的错误。
type Fail struct {
	Status int
	Err    *Error
}

func (f *Fail) Error() string { return f.Err.Error() }

func newFail(status int, code Code, msg string) *Fail {
	return &Fail{Status: status, Err: &Error{Code: code, Message: msg}}
}

// 常用错误的构造器。message 一律是**中文人话且不含内部术语**。
func ErrBadJSON(msg string) *Fail {
	if msg == "" {
		msg = "请求内容格式不对"
	}
	return newFail(http.StatusBadRequest, CodeBadJSON, msg)
}

func ErrBadRequest(msg string) *Fail {
	return newFail(http.StatusBadRequest, CodeBadRequest, msg)
}

func ErrUnauthenticated() *Fail {
	return newFail(http.StatusUnauthorized, CodeUnauthenticated, "请先登录")
}

func ErrInvalidAPIKey() *Fail {
	return newFail(http.StatusUnauthorized, CodeInvalidAPIKey, "API key 无效或已吊销")
}

func ErrDisabled() *Fail {
	return newFail(http.StatusForbidden, CodeDisabled, "账号已停用")
}

// ErrSessionRequired 用于「只能浏览器登录做」的操作（改密码 / 建 API key）。
// 目的是防止泄露的 key 被用来换新 key 把主人锁在门外。
func ErrSessionRequired() *Fail {
	return newFail(http.StatusForbidden, CodeSessionRequired,
		"这个操作需要登录后在网页上做，不能用 API key")
}

func ErrNotFound(msg string) *Fail {
	if msg == "" {
		msg = "找不到这个资源"
	}
	return newFail(http.StatusNotFound, CodeNotFound, msg)
}

func ErrConflict(code Code, msg string) *Fail {
	return newFail(http.StatusConflict, code, msg)
}

func ErrInsufficientBalance(msg string) *Fail {
	if msg == "" {
		msg = "积分不足"
	}
	return newFail(http.StatusPaymentRequired, CodeInsufficientBalance, msg)
}

func ErrInternal() *Fail {
	// 内部错误**不把细节给客户端** —— 细节进日志
	return newFail(http.StatusInternalServerError, CodeInternal, "服务出了点问题，请稍后再试")
}

// writeJSON 编码并写响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// header 已经写出去了，这里只能记日志
		slog.Error("写响应失败", "err", err)
	}
}

// writeFail 写错误响应。非 *Fail 的错误一律当 500 并把细节留在日志里。
func writeFail(w http.ResponseWriter, r *http.Request, err error) {
	if f, ok := err.(*Fail); ok {
		if f.Err.RetryAfter > 0 {
			w.Header().Set("Retry-After", itoa(f.Err.RetryAfter))
		}
		// 4xx 是客户端问题，记 info 就够；5xx 才是我们的问题
		lvl := slog.LevelInfo
		if f.Status >= 500 {
			lvl = slog.LevelError
		}
		slog.Log(r.Context(), lvl, "请求失败",
			"method", r.Method, "path", r.URL.Path,
			"status", f.Status, "code", f.Err.Code)
		writeJSON(w, f.Status, f.Err)
		return
	}

	slog.Error("未处理的错误", "method", r.Method, "path", r.URL.Path, "err", err)
	e := ErrInternal()
	writeJSON(w, e.Status, e.Err)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// handler 是内部用的 handler 形状 —— 返回 error 让中间件统一处理，
// 而不是每个 handler 各自 writeFail（那样容易漏写 return 导致写两次响应）。
type handler func(http.ResponseWriter, *http.Request) error

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		writeFail(w, r, err)
	}
}
