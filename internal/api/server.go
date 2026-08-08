package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/passenger"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

// maxBodyBytes 请求体上限（契约错误码表里的 body_too_large 是 1 MiB）
const maxBodyBytes = 1 << 20

type Server struct {
	db         *sql.DB
	passengers *passenger.Store
	wallets    *wallet.Store
	strategies *strategy.Store
	decider    *decider.Orchestrator
	// secureCookie 生产环境要 true（HTTPS）· 本地 http 调试设 false 否则 cookie 不生效
	secureCookie bool
}

// ServerDeps 装配 Server 需要的依赖。decider 允许为 nil（migrate 之类的
// 子命令不需要跑拉号，主进程 serve 才装配）
type ServerDeps struct {
	DB           *sql.DB
	Passengers   *passenger.Store
	Wallets      *wallet.Store
	Strategies   *strategy.Store
	Decider      *decider.Orchestrator
	SecureCookie bool
}

func NewServer(d ServerDeps) *Server {
	return &Server{
		db:           d.DB,
		passengers:   d.Passengers,
		wallets:      d.Wallets,
		strategies:   d.Strategies,
		decider:      d.Decider,
		secureCookie: d.SecureCookie,
	}
}

// Routes 注册所有路由。
//
// 分三类（对应契约 §鉴权）：
//   - 公开：注册 / 登录 / 健康检查
//   - 已鉴权：会话或 API key 都行
//   - 只会话：改密码 / 建 API key（防泄露的 key 自我续命）
func (s *Server) Routes(mux *http.ServeMux) {
	// 公开
	mux.Handle("POST /api/register", handler(s.handleRegister))
	mux.Handle("POST /api/login", handler(s.handleLogin))
	mux.Handle("POST /api/logout", handler(s.handleLogout))

	// 已鉴权（会话 or API key）
	mux.Handle("GET /api/me", handler(s.RequireAuth(s.handleMe)))
	mux.Handle("GET /api/me/wallet", handler(s.RequireAuth(s.handleWallet)))
	mux.Handle("GET /api/me/ledger", handler(s.RequireAuth(s.handleLedger)))
	mux.Handle("GET /api/me/api-keys", handler(s.RequireAuth(s.handleListAPIKeys)))
	mux.Handle("DELETE /api/me/api-keys/{id}", handler(s.RequireAuth(s.handleRevokeAPIKey)))
	mux.Handle("GET /api/me/strategy", handler(s.RequireAuth(s.handleGetStrategy)))
	mux.Handle("PUT /api/me/strategy", handler(s.RequireAuth(s.handlePutStrategy)))
	mux.Handle("POST /api/me/pull", handler(s.RequireAuth(s.handlePull)))

	// 只会话 —— API key 不能做这两件事
	mux.Handle("POST /api/me/password", handler(s.RequireSession(s.handleChangePassword)))
	mux.Handle("POST /api/me/api-keys", handler(s.RequireSession(s.handleCreateAPIKey)))
}

// ── 账号 ────────────────────────────────────────────

type registerRequest struct {
	Email      string `json:"email"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	InviteCode string `json:"invite_code"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) error {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if err := validateRegister(req); err != nil {
		return err
	}

	p, err := s.passengers.Register(r.Context(), passenger.RegisterInput{
		Email:      req.Email,
		Username:   req.Username,
		Password:   req.Password,
		InviteCode: req.InviteCode,
	})
	switch {
	case errors.Is(err, passenger.ErrEmailTaken):
		return ErrConflict(CodeConflict, "这个邮箱已经注册过了")
	case errors.Is(err, passenger.ErrUsernameTaken):
		return ErrConflict(CodeConflict, "这个用户名已被占用")
	case err != nil:
		return err
	}

	// 注册即登录（前端注册成功直接跳首页）
	token, expires, err := s.passengers.CreateSession(
		r.Context(), p.ID, clientIP(r), r.UserAgent(), false)
	if err != nil {
		return err
	}
	setSessionCookie(w, token, int(time.Until(expires).Seconds()), s.secureCookie)

	writeJSON(w, http.StatusCreated, profileOf(p))
	return nil
}

func validateRegister(req registerRequest) error {
	if len(req.Password) < 8 {
		return ErrBadRequest("密码至少 8 位")
	}
	if !strings.Contains(req.Email, "@") || len(req.Email) < 5 {
		return ErrBadRequest("邮箱格式不对")
	}
	if n := len([]rune(strings.TrimSpace(req.Username))); n < 2 || n > 24 {
		return ErrBadRequest("用户名 2-24 个字")
	}
	return nil
}

type loginRequest struct {
	// Account 邮箱或用户名
	Account  string `json:"account"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.Account == "" || req.Password == "" {
		return ErrBadRequest("请填账号和密码")
	}

	p, err := s.passengers.Authenticate(r.Context(), req.Account, req.Password)
	switch {
	case errors.Is(err, passenger.ErrWrongPassword):
		// 不区分"账号不存在"和"密码错" —— 否则接口成了账号枚举器
		return &Fail{Status: http.StatusUnauthorized,
			Err: &Error{Code: CodeUnauthenticated, Message: "账号或密码不对"}}
	case errors.Is(err, passenger.ErrAccountDisabled):
		return ErrDisabled()
	case err != nil:
		return err
	}

	token, expires, err := s.passengers.CreateSession(
		r.Context(), p.ID, clientIP(r), r.UserAgent(), req.Remember)
	if err != nil {
		return err
	}
	setSessionCookie(w, token, int(time.Until(expires).Seconds()), s.secureCookie)

	writeJSON(w, http.StatusOK, profileOf(p))
	return nil
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) error {
	// 登出不要求鉴权成功 —— 已经过期的会话也该能"登出"（清 cookie）
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
		if err := s.passengers.RevokeSession(r.Context(), c.Value); err != nil {
			return err
		}
	}
	clearSessionCookie(w, s.secureCookie)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// profileResponse 是 GET /api/me 的形状（05-api-contract §1）。
// 注意**不含余额** —— 余额只在 /api/me/wallet，避免两处返回同一个数字导致不一致。
type profileResponse struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	CreatedAt     string `json:"created_at"`
	Invited       bool   `json:"invited"`
}

func profileOf(p *passenger.Passenger) profileResponse {
	return profileResponse{
		ID: p.ID, Username: p.Username, Email: p.Email,
		EmailVerified: p.EmailVerified,
		CreatedAt:     p.CreatedAt.Format(time.RFC3339),
		Invited:       p.Invited,
	}
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, profileOf(p))
	return nil
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if len(req.NewPassword) < 8 {
		return ErrBadRequest("新密码至少 8 位")
	}

	err = s.passengers.ChangePassword(r.Context(), p.ID, req.OldPassword, req.NewPassword)
	switch {
	case errors.Is(err, passenger.ErrWrongPassword):
		return ErrBadRequest("当前密码不对")
	case err != nil:
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// ── API key ────────────────────────────────────────

type apiKeyResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	Revoked    bool    `json:"revoked"`
}

func apiKeyOf(k passenger.APIKey) apiKeyResponse {
	out := apiKeyResponse{
		ID: k.ID, Name: k.Name, Prefix: k.Prefix,
		CreatedAt: k.CreatedAt.Format(time.RFC3339), Revoked: k.Revoked,
	}
	if k.LastUsedAt != nil {
		s := k.LastUsedAt.Format(time.RFC3339)
		out.LastUsedAt = &s
	}
	return out
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	keys, err := s.passengers.ListAPIKeys(r.Context(), p.ID)
	if err != nil {
		return err
	}
	items := make([]apiKeyResponse, 0, len(keys))
	for _, k := range keys {
		items = append(items, apiKeyOf(k))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	plaintext, key, err := s.passengers.CreateAPIKey(r.Context(), p.ID, req.Name)
	if err != nil {
		return err
	}
	// 明文**只此一次** —— 之后任何端点都拿不到（契约 §2）
	writeJSON(w, http.StatusCreated, map[string]any{
		"key":  plaintext,
		"item": apiKeyOf(*key),
	})
	return nil
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	id := r.PathValue("id")
	if id == "" {
		return ErrBadRequest("缺少 key id")
	}

	err = s.passengers.RevokeAPIKey(r.Context(), p.ID, id)
	switch {
	case errors.Is(err, passenger.ErrNotFound):
		return ErrNotFound("找不到这个 API key")
	case err != nil:
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// ── 钱包 ────────────────────────────────────────────

func (s *Server) handleWallet(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}
	b, err := s.wallets.Get(r.Context(), p.ID)
	if errors.Is(err, wallet.ErrNotFound) {
		return ErrNotFound("找不到钱包")
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"balance":    b.Balance,
		"reserved":   b.Reserved,
		"updated_at": b.Updated.Format(time.RFC3339),
	})
	return nil
}

func (s *Server) handleLedger(w http.ResponseWriter, r *http.Request) error {
	p, err := mustCaller(r)
	if err != nil {
		return err
	}

	page := atoiDefault(r.URL.Query().Get("page"), 1)
	pageSize := atoiDefault(r.URL.Query().Get("page_size"), 50)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 50
	}

	entries, total, err := s.wallets.List(r.Context(), p.ID, wallet.ListOptions{
		Reasons: internalReasonsFor(r.URL.Query().Get("type")),
		Limit:   pageSize,
		Offset:  (page - 1) * pageSize,
	})
	if err != nil {
		return err
	}

	items := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		items = append(items, map[string]any{
			"id":            e.ID,
			"type":          publicLedgerType(e.Reason),
			"amount":        e.Amount,
			"balance_after": e.BalanceAfter,
			"memo":          e.Memo,
			"created_at":    e.CreatedAt.Format(time.RFC3339),
		})
	}

	pages := (total + pageSize - 1) / pageSize
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
	})
	return nil
}

// ── 工具 ────────────────────────────────────────────

// decodeJSON 解请求体。拒绝未知字段 —— 客户端拼错字段名时早报错，
// 而不是静默忽略然后行为跟预期不一致（契约错误码 bad_json）。
func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return ErrBadJSON("请求内容为空")
	}
	limited := http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	dec := json.NewDecoder(limited)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return &Fail{Status: http.StatusRequestEntityTooLarge,
				Err: &Error{Code: CodeBodyTooLarge, Message: "请求内容太大"}}
		}
		return ErrBadJSON("")
	}
	// 只允许一个 JSON 文档
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return ErrBadJSON("请求内容格式不对")
	}
	return nil
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// clientIP 尽力取真实来源 IP（审计用，不做鉴权依据 —— header 可伪造）
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i > 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return strings.TrimSpace(v)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
