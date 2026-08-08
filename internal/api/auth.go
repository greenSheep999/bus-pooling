package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/passenger"
)

// AuthMode 记录这次请求是怎么通过鉴权的 —— 决定能不能做「只允许会话」的操作。
type AuthMode string

const (
	AuthSession AuthMode = "session"
	AuthAPIKey  AuthMode = "api_key"
)

type ctxKey int

const (
	ctxKeyPassenger ctxKey = iota
	ctxKeyAuthMode
	ctxKeySessionToken
)

// SessionCookieName 会话 cookie 名。
const SessionCookieName = "bp_session"

// callerFrom 取当前请求的乘客。没有就是没过鉴权（中间件应该已经拦掉了）。
func callerFrom(ctx context.Context) (*passenger.Passenger, bool) {
	p, ok := ctx.Value(ctxKeyPassenger).(*passenger.Passenger)
	return p, ok
}

func authModeFrom(ctx context.Context) AuthMode {
	m, _ := ctx.Value(ctxKeyAuthMode).(AuthMode)
	return m
}

func sessionTokenFrom(ctx context.Context) string {
	t, _ := ctx.Value(ctxKeySessionToken).(string)
	return t
}

// mustCaller 在已鉴权的 handler 里取乘客。没有就是路由挂错了（漏了中间件）。
func mustCaller(r *http.Request) (*passenger.Passenger, error) {
	p, ok := callerFrom(r.Context())
	if !ok {
		return nil, ErrUnauthenticated()
	}
	return p, nil
}

// RequireAuth 接受两种入口：会话 cookie 或 API key。
//
// 顺序：先看 API key（脚本调用更明确），再看 cookie。
func (s *Server) RequireAuth(next handler) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()

		if raw := apiKeyFromRequest(r); raw != "" {
			p, err := s.passengers.APIKeyOwner(ctx, raw)
			switch {
			case errors.Is(err, passenger.ErrAPIKeyInvalid):
				return ErrInvalidAPIKey()
			case errors.Is(err, passenger.ErrAccountDisabled):
				return ErrDisabled()
			case err != nil:
				return err
			}
			ctx = context.WithValue(ctx, ctxKeyPassenger, p)
			ctx = context.WithValue(ctx, ctxKeyAuthMode, AuthAPIKey)
			return next(w, r.WithContext(ctx))
		}

		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			return ErrUnauthenticated()
		}
		p, err := s.passengers.SessionOwner(ctx, cookie.Value)
		switch {
		case errors.Is(err, passenger.ErrSessionInvalid), errors.Is(err, passenger.ErrNotFound):
			return ErrUnauthenticated()
		case errors.Is(err, passenger.ErrAccountDisabled):
			return ErrDisabled()
		case err != nil:
			return err
		}
		ctx = context.WithValue(ctx, ctxKeyPassenger, p)
		ctx = context.WithValue(ctx, ctxKeyAuthMode, AuthSession)
		ctx = context.WithValue(ctx, ctxKeySessionToken, cookie.Value)
		return next(w, r.WithContext(ctx))
	}
}

// RequireSession 只放会话鉴权过的请求。
//
// 用在改密码和建 API key 上（05-api-contract §鉴权）：如果 API key 能建新 key
// 或改密码，泄露一把 key 就等于永久接管账号 —— 主人反而会被锁在门外。
func (s *Server) RequireSession(next handler) handler {
	return s.RequireAuth(func(w http.ResponseWriter, r *http.Request) error {
		if authModeFrom(r.Context()) != AuthSession {
			return ErrSessionRequired()
		}
		return next(w, r)
	})
}

// apiKeyFromRequest 支持两种写法（契约 §鉴权）：
//
//	X-API-Key: usr-<hex>
//	Authorization: Bearer usr-<hex>
func apiKeyFromRequest(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-API-Key")); v != "" {
		return v
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

// setSessionCookie 下发会话 cookie。
//
// HttpOnly 防 XSS 偷 token；SameSite=Lax 挡掉大部分 CSRF（写操作还会另外要求
// 幂等键，进一步降低被诱导重放的风险）。
func setSessionCookie(w http.ResponseWriter, token string, maxAge int, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
