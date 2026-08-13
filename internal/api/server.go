package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/delivery/handoff"
	"github.com/bus-pooling/bus-pooling/internal/downstream"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/insight"
	"github.com/bus-pooling/bus-pooling/internal/passenger"
	"github.com/bus-pooling/bus-pooling/internal/paymentgw"
	"github.com/bus-pooling/bus-pooling/internal/pullrecord"
	"github.com/bus-pooling/bus-pooling/internal/redeem"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
	"github.com/bus-pooling/bus-pooling/internal/topup"
	"github.com/bus-pooling/bus-pooling/internal/topupchannel"
	"github.com/bus-pooling/bus-pooling/internal/vendoraccount"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
	"github.com/bus-pooling/bus-pooling/internal/webhookin"
)

// maxBodyBytes 请求体上限（契约错误码表里的 body_too_large 是 1 MiB）
const maxBodyBytes = 1 << 20

type Server struct {
	db          *sql.DB
	passengers  *passenger.Store
	wallets     *wallet.Store
	strategies  *strategy.Store
	buses       *bus.Store
	decider     *decider.Orchestrator
	redeems     *redeem.Store
	topups      *topup.Store
	pullRecords *pullrecord.Store
	handoffs    *handoff.Store
	pool        housepool.HousePool
	vendorView  *vendorview.Service
	insights    *insight.Store
	downstreams *downstream.Store
	// topupChannels · topup 渠道注册表（三维属性 · enabled 开关 · 见 topupchannel 包）
	topupChannels *topupchannel.Registry
	// pendingTopups · pending_topup 状态机 · webhook 主推进 · janitor 兜底（1b P1-C）
	pendingTopups *topup.PendingStore
	// paymentGW 接 404bus-payment-gateway·nil = 未装配（走 dev mock 端点）
	paymentGW *paymentgw.Client
	// paymentGWSuccessURL hosted checkout 完成后回跳的前端 URL·可选
	paymentGWSuccessURL string
	// secureCookie 生产环境要 true（HTTPS）· 本地 http 调试设 false 否则 cookie 不生效
	secureCookie bool
	// promos 顶部跑马灯活动位（config.promo.items）· 空 = 不显示跑马灯
	promos []config.PromoItem
	// communityChannels 社群渠道链接（config.community.channels）· 空 = 前端展示占位
	communityChannels []config.CommunityChannel
	// vaStore · vendor_account 表 · webhook receiver 从这里读 webhook_secret 明文
	// （AES 解密后的·内存里·永不落 log）· nil 时 fallback 到 env
	vaStore *vendoraccount.Store
	// webhookDispatcher · webhookin 分派器 · 收到 vendor webhook 后处理 event
	// nil 时 receiver 只 log 不分派（保留旧行为兼容测试 / 旧部署）
	webhookDispatcher *webhookin.Dispatcher
	// health · 数据管线心跳（migration 036）· admin data-health 端点读它 · 可 nil
	health *vendorview.HealthStore
	// adminKey · BP_ADMIN_KEY · 非空才挂 /api/admin/* · 且请求要带 X-Admin-Key 匹配
	adminKey string
}

// ServerDeps 装配 Server 需要的依赖。decider 允许为 nil（migrate 之类的
// 子命令不需要跑拉号，主进程 serve 才装配）
type ServerDeps struct {
	DB                  *sql.DB
	Passengers          *passenger.Store
	Wallets             *wallet.Store
	Strategies          *strategy.Store
	Buses               *bus.Store
	Decider             *decider.Orchestrator
	Redeems             *redeem.Store
	Topups              *topup.Store
	PullRecords         *pullrecord.Store
	Handoffs            *handoff.Store
	Pool                housepool.HousePool
	VendorView          *vendorview.Service
	Insights            *insight.Store
	Downstreams         *downstream.Store
	TopupChannels       *topupchannel.Registry
	PendingTopups       *topup.PendingStore
	PaymentGW           *paymentgw.Client
	PaymentGWSuccessURL string
	SecureCookie        bool
	// Promos 跑马灯配置（config.promo.items）
	Promos []config.PromoItem
	// CommunityChannels 社群渠道配置（config.community.channels）
	CommunityChannels []config.CommunityChannel
	// VendorAccounts vendor_account 表 · webhook receiver 验签时 · 从表读 vendor
	// 的 webhook_secret（AES 解密后的明文）· 允许 nil（旧集成走 env fallback）
	VendorAccounts *vendoraccount.Store
	// WebhookDispatcher webhookin 分派器 · 允许 nil（老装配路径 · receiver 只 log 不分派）
	WebhookDispatcher *webhookin.Dispatcher
	// Health 数据管线心跳（migration 036）· admin data-health 端点用 · 允许 nil
	Health *vendorview.HealthStore
	// AdminKey BP_ADMIN_KEY · 非空才挂 /api/admin/* 运维端点（X-Admin-Key 头校验）
	AdminKey string
}

func NewServer(d ServerDeps) *Server {
	// **装配硬约束**（P0 修）：paymentGW 装了但 pendingTopups 没装 = 起单会写不了状态机 ·
	// 走到 CreatePayment 后崩溃就丢单。启动阶段 panic · 让运维立刻发现。
	if d.PaymentGW != nil && d.PendingTopups == nil {
		panic("api: 装配了 PaymentGW 但缺 PendingTopups · gateway_creating 状态无法落库 · 会丢单")
	}
	return &Server{
		db:                  d.DB,
		passengers:          d.Passengers,
		wallets:             d.Wallets,
		strategies:          d.Strategies,
		buses:               d.Buses,
		decider:             d.Decider,
		redeems:             d.Redeems,
		topups:              d.Topups,
		pullRecords:         d.PullRecords,
		handoffs:            d.Handoffs,
		pool:                d.Pool,
		vendorView:          d.VendorView,
		insights:            d.Insights,
		downstreams:         d.Downstreams,
		topupChannels:       d.TopupChannels,
		pendingTopups:       d.PendingTopups,
		paymentGW:           d.PaymentGW,
		paymentGWSuccessURL: d.PaymentGWSuccessURL,
		secureCookie:        d.SecureCookie,
		promos:              d.Promos,
		communityChannels:   d.CommunityChannels,
		vaStore:             d.VendorAccounts,
		webhookDispatcher:   d.WebhookDispatcher,
		health:              d.Health,
		adminKey:            d.AdminKey,
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
	mux.Handle("GET /api/me/invite", handler(s.RequireAuth(s.handleGetMyInvite)))
	// 补绑社群码 · 已注册用户拿社群身份（decisions §8.29）
	mux.Handle("POST /api/me/community-code", handler(s.RequireAuth(s.handleBindSystemCode)))
	mux.Handle("GET /api/me/strategy", handler(s.RequireAuth(s.handleGetStrategy)))
	mux.Handle("PUT /api/me/strategy", handler(s.RequireAuth(s.handlePutStrategy)))
	mux.Handle("POST /api/me/pull", handler(s.RequireAuth(s.handlePull)))
	mux.Handle("POST /api/me/pull/estimate", handler(s.RequireAuth(s.handleEstimate)))

	// bus（阶段 1a 只 single kind）
	mux.Handle("POST /api/me/buses", handler(s.RequireAuth(s.handleCreateBus)))
	mux.Handle("GET /api/me/buses", handler(s.RequireAuth(s.handleListBuses)))
	mux.Handle("GET /api/me/buses/{bus_id}", handler(s.RequireAuth(s.handleGetBus)))
	mux.Handle("POST /api/me/buses/{bus_id}/leave", handler(s.RequireAuth(s.handleLeaveBus)))
	mux.Handle("PUT /api/me/buses/{bus_id}", handler(s.RequireAuth(s.handleUpdateBus)))
	mux.Handle("PUT /api/me/buses/{bus_id}/strategy", handler(s.RequireAuth(s.handleUpdateBusStrategy)))
	mux.Handle("DELETE /api/me/buses/{bus_id}", handler(s.RequireAuth(s.handleDissolveBus)))
	mux.Handle("POST /api/me/buses/{bus_id}/pull", handler(s.RequireAuth(s.handleBusPull)))
	// 1c-1 · 匿名撮合 · POST /api/me/buses/anon/match 找一辆现有 anon bus 或返 no_match
	// POST /api/me/buses/{bus_id}/join 显式加入某辆 anon bus（撮合后前端调）
	mux.Handle("POST /api/me/buses/anon/match", handler(s.RequireAuth(s.handleMatchAnonBus)))
	mux.Handle("POST /api/me/buses/{bus_id}/join", handler(s.RequireAuth(s.handleJoinBus)))
	// team 邀请码入口（1c · CLAUDE.md §7 阶段表 · 邀请码 + 搭车一起上）
	mux.Handle("POST /api/me/buses/join-by-invite", handler(s.RequireAuth(s.handleJoinByInvite)))
	mux.Handle("POST /api/me/buses/{bus_id}/invite-code", handler(s.RequireAuth(s.handleRegenInviteCode)))
	// 移除成员 · 剩下的人 share_pct 重算（decisions §8.18）
	mux.Handle("DELETE /api/me/buses/{bus_id}/members/{pid}", handler(s.RequireAuth(s.handleRemoveMember)))
	mux.Handle("GET /api/me/buses/{bus_id}/credentials", handler(s.RequireAuth(s.handleBusCredentials)))
	mux.Handle("GET /api/me/buses/{bus_id}/pulls", handler(s.RequireAuth(s.handleBusPulls)))
	// 成员维度统计（decisions §8.19 · 1c 多人拼车落地后开放）
	mux.Handle("GET /api/me/buses/{bus_id}/member-stats", handler(s.RequireAuth(s.handleBusMemberStats)))

	// 拉号记录 · 派去向（进车 / 推池）· handoff 三段式（05-api-contract §5 / §5b）
	mux.Handle("GET /api/me/pull-records", handler(s.RequireAuth(s.handleListPullRecords)))
	mux.Handle("GET /api/me/pull-records/{record_id}", handler(s.RequireAuth(s.handleGetPullRecord)))
	mux.Handle("POST /api/me/pull-records/assign", handler(s.RequireAuth(s.handleAssign)))
	mux.Handle("POST /api/me/handoff", handler(s.RequireAuth(s.handleHandoffInit)))
	mux.Handle("GET /api/me/handoff/{token}", handler(s.RequireAuth(s.handleHandoffFulfill)))
	mux.Handle("POST /api/me/handoff/{token}/confirm", handler(s.RequireAuth(s.handleHandoffConfirm)))
	// 事件流（前端「提取历史 / 派发历史」tab）
	mux.Handle("GET /api/me/pull/events", handler(s.RequireAuth(s.handleListPullEvents)))
	mux.Handle("GET /api/me/assign/events", handler(s.RequireAuth(s.handleListAssignEvents)))

	// 兑换码 + 充值（05-api-contract §3）
	mux.Handle("POST /api/me/redeem", handler(s.RequireAuth(s.handleRedeem)))
	// 充值渠道注册表 · 前端确认窗按这个渲染（含 disabled 显示"即将开放"占位）
	// 无鉴权：注册前也可看看有哪些渠道
	mux.Handle("GET /api/topup/channels", handler(s.handleListTopupChannels))
	// 跑马灯活动位 · **公开**（landing / 登录页也要显示）
	mux.Handle("GET /api/promos", handler(s.handleListPromos))
	mux.Handle("GET /api/community/channels", handler(s.handleListCommunityChannels))
	mux.Handle("POST /api/me/topup", handler(s.RequireAuth(s.handleCreateTopup)))
	mux.Handle("GET /api/me/topup/{order_id}", handler(s.RequireAuth(s.handleGetTopupOrder)))
	mux.Handle("GET /api/me/topup-orders", handler(s.RequireAuth(s.handleListTopupOrders)))
	// dev 内部端点 · **仅在 BP_ENABLE_DEV_TOPUP=1 时挂**（P0：任何用户能给自己充钱 · 生产禁用）。
	// 接了真支付网关后**删掉**·改成签名校验的 settlement webhook。
	if os.Getenv("BP_ENABLE_DEV_TOPUP") == "1" {
		mux.Handle("POST /api/internal/topup/{order_id}/paid", handler(s.RequireAuth(s.handleDevMarkTopupPaid)))
	}

	// 404bus-payment-gateway settlement 回调
	//
	// 挂点跟 gateway CLI -add-client 时提供的 settlement_url 一致。
	// 无鉴权（用签名验证）· 不加 RequireAuth · 契约要求接收后立刻记录 event_id
	// 再返 2xx（慢的活异步做），阶段 1a 直接同步做完（MarkPaid 是本地 SQL·<10ms）。
	if s.paymentGW != nil {
		mux.Handle("POST /api/hooks/paymentgw/settlement", handler(s.handleGatewaySettlement))
	}

	// vendor webhook 接收 · 6 家 kiro 系 vendor 一个统一端点
	//
	// 阶段 1a 只：验签（有 HMAC 那两家）+ log + 返 200 · 事件在 1d 真处理
	// 目的：vendor 侧不再刷我方 404。无鉴权（用 HMAC / URL secret 验证）
	mux.Handle("POST /api/webhooks/vendor/{vendor_id}", handler(s.handleVendorWebhook))

	// vendors 只读（05-api-contract §9）· 全部要鉴权
	mux.Handle("GET /api/vendors/status", handler(s.handleVendorsStatus))                         // 公开
	mux.Handle("GET /api/vendors/status/{anon_id}/trend", handler(s.handleVendorStatusTrend))     // 公开 · 老契约（按 source 两种 schema）
	mux.Handle("GET /api/vendors/status/{anon_id}/events", handler(s.handleVendorDispatchEvents)) // 公开 · 统一事件流 · /status 页用这个
	mux.Handle("GET /api/vendors/stock", handler(s.RequireAuth(s.handleVendorsStock)))
	mux.Handle("GET /api/vendors/prices", handler(s.RequireAuth(s.handleVendorsPrices)))
	mux.Handle("GET /api/vendors/stats", handler(s.RequireAuth(s.handleVendorsStats)))
	mux.Handle("GET /api/vendors/auto-pick", handler(s.RequireAuth(s.handleVendorsAutoPick)))
	mux.Handle("GET /api/vendors/{vendor_id}/stock", handler(s.RequireAuth(s.handleVendorStock)))
	mux.Handle("GET /api/vendors/{vendor_id}/history", handler(s.RequireAuth(s.handleVendorHistory)))
	mux.Handle("GET /api/vendors/{vendor_id}/prices/daily", handler(s.RequireAuth(s.handleVendorPricesDaily)))

	// 运维：数据管线新鲜度自检 · 仅 BP_ADMIN_KEY 非空 + 心跳已装配才挂（X-Admin-Key 头校验）·
	// 把 /healthz「HTTP 活着」升级到「数据在更新」· 纯运维视角 · 不给乘客前端（§0.1）
	if s.adminKey != "" && s.health != nil {
		mux.Handle("GET /api/admin/data-health", handler(s.requireAdmin(s.handleDataHealth)))
	}

	// 首页 / 数据 tab / 活动流（05-api-contract §9b）
	mux.Handle("GET /api/me/overview", handler(s.RequireAuth(handleOverviewWith(s.insights))))
	mux.Handle("GET /api/me/trend", handler(s.RequireAuth(handleTrendWith(s.insights, s.buses))))
	mux.Handle("GET /api/me/activities", handler(s.RequireAuth(handleActivitiesWith(s.insights))))

	// 下游配置（05-api-contract §8）
	mux.Handle("GET /api/me/downstream", handler(s.RequireAuth(s.handleGetDownstream)))
	mux.Handle("PUT /api/me/downstream/passengerpool", handler(s.RequireAuth(s.handlePutPassengerpool)))
	mux.Handle("POST /api/me/downstream/passengerpool/test", handler(s.RequireAuth(s.handleTestPassengerpool)))
	mux.Handle("GET /api/me/downstream/webhook", handler(s.RequireAuth(s.handleGetWebhook)))
	mux.Handle("PUT /api/me/downstream/webhook", handler(s.RequireAuth(s.handlePutWebhook)))
	mux.Handle("POST /api/me/downstream/webhook/secret", handler(s.RequireAuth(s.handleRotateWebhookSecret)))
	mux.Handle("POST /api/me/downstream/webhook/test", handler(s.RequireAuth(s.handleTestWebhook)))
	mux.Handle("GET /api/me/downstream/webhook/deliveries", handler(s.RequireAuth(s.handleListDeliveries)))

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
	// 前端 TS: ApiKey[]（纯数组 · 非分页）· 契约以 TS 为准
	writeJSON(w, http.StatusOK, items)
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

// derefInt64 · nil 指针取 0。用于把 strategy 的"nil = 不限"语义转成 decider 的
// "0 = 不限"语义（两边约定不同 · 这里是唯一的转换点）。
func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
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
