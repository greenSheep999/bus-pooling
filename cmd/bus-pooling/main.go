// Command bus-pooling 是服务入口。
//
// 子命令：
//
//	serve                     起 HTTP 服务（默认）
//	migrate up                应用未应用的迁移
//	migrate down [n]          回滚最近 n 个（默认 1）
//	migrate status            看哪些已应用
//	genkey                    生成一个新的 AES 主密钥（部署时用一次）
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/api"
	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/deathwatch"
	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/delivery/handoff"
	"github.com/bus-pooling/bus-pooling/internal/downstream"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/housepool/kirors"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
	"github.com/bus-pooling/bus-pooling/internal/insight"
	"github.com/bus-pooling/bus-pooling/internal/passenger"
	"github.com/bus-pooling/bus-pooling/internal/paymentgw"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/providers/kiro"
	"github.com/bus-pooling/bus-pooling/internal/pullrecord"
	"github.com/bus-pooling/bus-pooling/internal/redeem"
	"github.com/bus-pooling/bus-pooling/internal/secrets"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
	"github.com/bus-pooling/bus-pooling/internal/topup"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

func main() {
	var cfgPath string
	fs := flag.NewFlagSet("bus-pooling", flag.ExitOnError)
	fs.StringVar(&cfgPath, "config", os.Getenv("BP_CONFIG"), "配置文件路径（可空，用默认值）")
	_ = fs.Parse(os.Args[1:])

	args := fs.Args()
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(cmd, cfgPath, args); err != nil {
		logger.Error("启动失败", "cmd", cmd, "err", err)
		os.Exit(1)
	}
}

func run(cmd, cfgPath string, args []string) error {
	// genkey 不需要配置也不需要数据库
	if cmd == "genkey" {
		key, err := secrets.GenerateKeyHex()
		if err != nil {
			return err
		}
		fmt.Printf("%s=%s\n", config.EnvMasterKey, key)
		return nil
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "migrate":
		return runMigrate(ctx, cfg, args)
	case "serve":
		return runServe(ctx, cfg)
	case "redeem":
		return runRedeem(ctx, cfg, args)
	default:
		return fmt.Errorf("未知子命令 %q（支持 serve | migrate | genkey | redeem）", cmd)
	}
}

func runMigrate(ctx context.Context, cfg config.Config, args []string) error {
	sub := "up"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}

	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	switch sub {
	case "up":
		ran, err := database.MigrateUp(ctx, cfg.DB.MigrationsDir)
		if err != nil {
			return err
		}
		if len(ran) == 0 {
			slog.Info("没有待应用的迁移")
			return nil
		}
		for _, m := range ran {
			slog.Info("已应用迁移", "version", m.Version, "name", m.Name)
		}
		return nil

	case "down":
		n := 1
		if len(args) > 0 {
			parsed, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("down 的参数要是数字: %w", err)
			}
			n = parsed
		}
		ran, err := database.MigrateDown(ctx, cfg.DB.MigrationsDir, n)
		if err != nil {
			return err
		}
		for _, m := range ran {
			slog.Info("已回滚迁移", "version", m.Version, "name", m.Name)
		}
		return nil

	case "status":
		all, applied, err := database.MigrateStatus(ctx, cfg.DB.MigrationsDir)
		if err != nil {
			return err
		}
		for _, m := range all {
			mark := "待应用"
			if applied[m.Version] {
				mark = "已应用"
			}
			fmt.Printf("%-6s %03d_%s\n", mark, m.Version, m.Name)
		}
		return nil

	default:
		return fmt.Errorf("未知 migrate 子命令 %q（支持 up | down | status）", sub)
	}
}

// buildVendorRegistry 把配置 + 密钥拼成 vendor registry。
//
// 密钥从 env 走到这里就是明文了 —— `internal/secrets` 管的是**落库**的加密，
// 运行期内存里是明文（要拿它发 HTTP 头）。所以别把 registry 或 cfg.Secrets 打进日志。
func buildVendorRegistry(cfg config.Config) (*providers.Registry, error) {
	r := providers.NewRegistry()
	// 通用：所有 vendor 共用 httpx 超时/代理（CLAUDE.md §7.1 出向统一）
	base := func(v config.Vendor, apiKey, webhookSecret string) kiro.VendorConfig {
		return kiro.VendorConfig{
			Enabled: v.Enabled, BaseURL: v.BaseURL,
			APIKey: apiKey, WebhookSecret: webhookSecret,
			Timeout: cfg.HTTPX.Timeout, MaxRetries: cfg.HTTPX.MaxRetries,
			ProxyURL: cfg.HTTPX.Proxy, NoProxy: cfg.HTTPX.NoProxy,
		}
	}
	err := kiro.Register(r, kiro.Config{
		Kiro91:    base(cfg.Vendors.Kiro91, cfg.Secrets.Kiro91APIKey, cfg.Secrets.Kiro91WebhookSecret),
		KiroCEO:   base(cfg.Vendors.KiroCEO, cfg.Secrets.KiroCEOAPIKey, ""),
		KiroOOO:   base(cfg.Vendors.KiroOOO, cfg.Secrets.KiroOOOAPIKey, ""),
		KiroAppIO: base(cfg.Vendors.KiroAppIO, cfg.Secrets.KiroAppIOAPIKey, ""),
		KiroAppCC: base(cfg.Vendors.KiroAppCC, cfg.Secrets.KiroAppCCAPIKey, ""),
		KiroDrop:  base(cfg.Vendors.KiroDrop, cfg.Secrets.KiroDropAPIKey, cfg.Secrets.KiroDropWebhookSecret),
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ratesFromEnv 从 env 读加价链各层费率（basis point · 1 bp = 0.01%）。
// **具体率不进代码 · §8.20** —— 生产由 ops 通过 env 注入（未来后台配置）。
func ratesFromEnv() decider.Rates {
	bp := func(name string) decider.Rate {
		v := os.Getenv(name)
		if v == "" {
			return 0
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			slog.Warn("费率 env 值非法，按 0 处理", "env", name, "value", v)
			return 0
		}
		return decider.Rate(n)
	}
	return decider.Rates{
		VendorMarkup: bp("BP_RATE_VENDOR_BP"),
		RegionMarkup: bp("BP_RATE_REGION_BP"),
		SinglePull:   bp("BP_RATE_SINGLE_PULL_BP"),
		Capability:   bp("BP_RATE_CAPABILITY_BP"),
		Service:      bp("BP_RATE_SERVICE_BP"),
	}
}

// buildDecider 装配拉号编排器 + 返回同一份 pool（deathwatch / vendorview
// 都要复用这一份 —— 别在两处各建一份 kirors.Client）。
//
// **默认走内存 mock**（`DryRunVendor` + `DryRunPool`）—— vendor 侧是真积分，
// 阶段 1a 只跑通接口。切真链路需要**同时**：
//   - `cfg.DryRun == false`
//   - env `BP_ALLOW_LIVE_PULL=1`（第二把锁，防意外配错）
//
// 单靠 `DRY_RUN=false` 不够 —— 那个变量在很多地方影响行为，一处误配会全线通到真扣款。
//
// live 模式下返回的 pool 是 *kirors.Client（同时满足 decider.PoolClient 和
// housepool.HousePool 两个接口）；mock 模式下 pool 是 nil（deathwatch / handoff
// 都会防 nil），api handler 用的 housepool.HousePool 也是 nil，vendorview
// 只用 registry 不用 pool。
func buildDecider(cfg config.Config, sqldb *db.DB, reg *providers.Registry) (*decider.Orchestrator, housepool.HousePool, decider.Rates, error) {
	live := !cfg.DryRun && os.Getenv("BP_ALLOW_LIVE_PULL") == "1"

	var vendor decider.VendorClient
	var pool decider.PoolClient
	var pubPool housepool.HousePool
	if !live {
		vendor = &decider.DryRunVendor{VendorID: providers.Vendor91Kiro}
		pool = &decider.DryRunPool{}
		if !cfg.DryRun {
			slog.Warn("拉号走 mock · 要接真链路请显式设 BP_ALLOW_LIVE_PULL=1")
		}
	} else {
		v, err := reg.Get(providers.Vendor91Kiro)
		if err != nil {
			return nil, nil, decider.Rates{}, fmt.Errorf("live 模式但未启用 kiro91 vendor: %w", err)
		}
		hc, err := httpx.New(httpx.Config{
			Timeout: cfg.HTTPX.Timeout, MaxRetries: cfg.HTTPX.MaxRetries,
			RetryBaseWait: cfg.HTTPX.RetryBaseWait,
			Proxy:         cfg.HTTPX.Proxy, NoProxy: cfg.HTTPX.NoProxy,
		})
		if err != nil {
			return nil, nil, decider.Rates{}, err
		}
		poolClient, err := kirors.New(kirors.Config{
			BaseURL: cfg.Housepool.BaseURL, AdminKey: cfg.Secrets.HousepoolAdminKey,
		}, hc)
		if err != nil {
			return nil, nil, decider.Rates{}, fmt.Errorf("装配号池客户端: %w", err)
		}
		// expected_version 真校验（Iss #13）· 空 = 不校验
		// live 模式下强烈建议配·防 kiro.rs 契约漂移后我方误发请求
		// **注意**：这里比的是**语义版本**（CARGO_PKG_VERSION）·不是 commit SHA。
		// 真绑 build sha 需 kiro.rs 加 endpoint · 上游未提供。
		if cfg.Housepool.ExpectedVersion != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			gotVersion, verErr := poolClient.GetVersion(ctx)
			cancel()
			if verErr != nil {
				return nil, nil, decider.Rates{}, fmt.Errorf(
					"kiro.rs 版本校验失败·无法拉版本·拒启动: %w", verErr)
			}
			if gotVersion != cfg.Housepool.ExpectedVersion {
				return nil, nil, decider.Rates{}, fmt.Errorf(
					"kiro.rs 版本对不上·期望 %q·实际 %q·契约可能已漂移·拒启动",
					cfg.Housepool.ExpectedVersion, gotVersion)
			}
			slog.Info("kiro.rs 版本校验通过", "version", gotVersion)
		}
		vendor = v
		pool = poolClient
		pubPool = poolClient
		slog.Warn("拉号走 LIVE 链路 · 会产生真扣款", "vendor", providers.Vendor91Kiro)
	}

	// Rates 从环境 basis point 读（1 bp = 0.01%）· 生产前 ops 从后台配置注入
	rates := ratesFromEnv()
	// live 模式下**零费率拒启动** —— 那意味着号价 pass-through 没有收入，
	// 是配错不是设计。DRY_RUN 允许零，方便本地跑通闭环。
	if live && rates == (decider.Rates{}) {
		return nil, nil, decider.Rates{}, fmt.Errorf(
			"live 模式必须显式配费率：BP_RATE_SERVICE_BP=<n> 等 · 零费率是配错防身",
		)
	}

	return decider.New(decider.Config{
		DB:     sqldb.DB,
		State:  decider.NewStore(sqldb.DB),
		Vendor: vendor,
		Pool:   pool,
		Rates:  rates,
	}), pubPool, rates, nil
}

func runServe(ctx context.Context, cfg config.Config) error {
	// serve 需要主密钥（vendor 凭证 / 号池 token 都要解密）
	if err := cfg.RequireSecrets(); err != nil {
		return err
	}
	cipher, err := secrets.New(cfg.Secrets.MasterKey)
	if err != nil {
		return err
	}
	if _, err := httpx.New(httpx.Config{
		Timeout:       cfg.HTTPX.Timeout,
		MaxRetries:    cfg.HTTPX.MaxRetries,
		RetryBaseWait: cfg.HTTPX.RetryBaseWait,
		Proxy:         cfg.HTTPX.Proxy,
		NoProxy:       cfg.HTTPX.NoProxy,
	}); err != nil {
		return err
	}

	// vendor registry —— 业务层只认 providers.Registry，装配是 main 的事（契约 §10）
	vendorRegistry, err := buildVendorRegistry(cfg)
	if err != nil {
		return err
	}
	for _, e := range vendorRegistry.All() {
		slog.Info("vendor 已注册", "vendor", e.VendorID, "enabled", e.Enabled)
	}

	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	// 起服务前确认 schema 是最新的 —— 少一张表就跑不起来，早失败好过运行时 500
	all, applied, err := database.MigrateStatus(ctx, cfg.DB.MigrationsDir)
	if err != nil {
		return err
	}
	for _, m := range all {
		if !applied[m.Version] {
			return fmt.Errorf("有未应用的迁移（%03d_%s）· 先跑 `bus-pooling migrate up`", m.Version, m.Name)
		}
	}

	mux := http.NewServeMux()

	// secureCookie：生产走 HTTPS 要 true；本地 http 调试必须 false，否则浏览器不存 cookie
	secureCookie := os.Getenv("BP_INSECURE_COOKIE") == ""
	orch, poolClient, rates, err := buildDecider(cfg, database, vendorRegistry)
	if err != nil {
		return err
	}

	// vendorview 用同一份 rates（费率不进代码，走 decider.Rates 唯一入口）
	vendorSvc, err := vendorview.New(vendorview.Config{
		Registry: vendorRegistry,
		Rates:    rates,
	})
	if err != nil {
		return err
	}

	handoffs := handoff.NewStore(database.DB, 0) // 0 = 默认 TTL

	// paymentgw client · 三个环境变量都要有才装配·任缺其一走 dev mock 路径
	// BP_GW_BASE / BP_GW_TOKEN / BP_GW_SETTLEMENT_SECRET
	// BP_GW_SUCCESS_URL 可选·waffo checkout 完成后回跳我方前端 URL
	var pgw *paymentgw.Client
	if base, tok, sec := os.Getenv("BP_GW_BASE"), os.Getenv("BP_GW_TOKEN"), os.Getenv("BP_GW_SETTLEMENT_SECRET"); base != "" && tok != "" && sec != "" {
		c, err := paymentgw.New(paymentgw.Config{
			BaseURL: base, BearerToken: tok, SettlementSecret: sec,
		})
		if err != nil {
			return fmt.Errorf("paymentgw 装配: %w", err)
		}
		pgw = c
		slog.Info("payment gateway 已装配", "base", base)
	} else {
		slog.Warn("payment gateway 未装配（BP_GW_BASE/TOKEN/SETTLEMENT_SECRET 缺失）· 走 dev mock topup 路径")
	}

	apiSrv := api.NewServer(api.ServerDeps{
		DB:                  database.DB,
		Passengers:          passenger.NewStore(database.DB),
		Wallets:             wallet.NewStore(database.DB),
		Strategies:          strategy.NewStore(database.DB),
		Buses:               bus.NewStore(database.DB),
		Decider:             orch,
		Redeems:             redeem.NewStore(database.DB),
		Topups:              topup.NewStore(database.DB),
		PullRecords:         pullrecord.NewStore(database.DB),
		Handoffs:            handoffs,
		Pool:                poolClient, // 可能为 nil（mock 模式）· handler 有 nil 兜底
		VendorView:          vendorSvc,
		Insights:            insight.NewStore(database.DB),
		Downstreams:         downstream.NewStore(database.DB, cipher),
		PaymentGW:           pgw,
		PaymentGWSuccessURL: os.Getenv("BP_GW_SUCCESS_URL"),
		SecureCookie:        secureCookie,
	})
	apiSrv.Routes(mux)

	// janitor 后台扫超时单 · orch 为 nil 时跳过（不装配拉号就没超时可扫）
	if orch != nil {
		janitor := decider.NewJanitor(decider.JanitorConfig{
			Orchestrator: orch, State: decider.NewStore(database.DB),
		})
		go janitor.Run(ctx)
		slog.Info("janitor 已启动")
	}

	// handoff janitor · 扫过期 token + 卡在 confirmed 的行
	// pool 非 nil 时（live 模式）·装完整 completeFn 让 confirmed 卡单能重试 DELETE
	handoffJanitorCfg := handoff.JanitorConfig{Store: handoffs}
	if poolClient != nil {
		handoffJanitorCfg.Pool = poolClient
		// completeFn · 重试 completeHandoff 的外部动作
		// 简化：只重试 pool.DeleteCredential（依赖 credential_ledger 存 kiro_rs_credential_id）
		// 真的完整版应该跟 api.completeHandoff 共享逻辑·1c 抽公用包时统一
		handoffJanitorCfg.CompleteFn = func(ctx context.Context, p handoff.Pending) error {
			for _, cid := range p.CredentialIDs {
				var krID uint64
				err := database.DB.QueryRowContext(ctx,
					`SELECT kiro_rs_credential_id FROM credential_ledger WHERE id = ? AND kiro_rs_credential_id IS NOT NULL`,
					cid).Scan(&krID)
				if err != nil {
					continue // credential 没 krID·跳过
				}
				if err := poolClient.DeleteCredential(ctx, housepool.CredentialID(krID)); err != nil {
					return fmt.Errorf("重试 DeleteCredential(cred=%s krID=%d): %w", cid, krID, err)
				}
				// 台账标 handed_off
				_, _ = database.DB.ExecContext(ctx,
					`UPDATE credential_ledger SET status='handed_off' WHERE id = ?`, cid)
			}
			return nil
		}
	}
	handoffJanitor := handoff.NewJanitor(handoffJanitorCfg)
	go handoffJanitor.Run(ctx)
	slog.Info("handoff janitor 已启动")

	// deathwatch 只在真号池 live 起时跑（mock 模式没 pool 可探）
	if poolClient != nil {
		w := deathwatch.New(deathwatch.Config{DB: database.DB, Pool: poolClient})
		go w.Run(ctx)
		slog.Info("deathwatch 已启动")
	}

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := database.PingContext(r.Context()); err != nil {
			http.Error(w, `{"code":"internal","message":"服务暂时不可用"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("服务启动", "addr", cfg.Server.Addr, "db", cfg.DB.Path, "dry_run", cfg.DryRun)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("收到退出信号，开始优雅关闭")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
