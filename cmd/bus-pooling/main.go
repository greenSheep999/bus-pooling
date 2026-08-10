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
	"strings"
	"syscall"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/api"
	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/deathwatch"
	"github.com/bus-pooling/bus-pooling/internal/web"
	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/delivery/handoff"
	"github.com/bus-pooling/bus-pooling/internal/downstream"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/housepool/kirors"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
	"github.com/bus-pooling/bus-pooling/internal/insight"
	"github.com/bus-pooling/bus-pooling/internal/passenger"
	"github.com/bus-pooling/bus-pooling/internal/paymentgw"
	"github.com/bus-pooling/bus-pooling/internal/pricing"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/providers/kiro"
	"github.com/bus-pooling/bus-pooling/internal/pullrecord"
	"github.com/bus-pooling/bus-pooling/internal/redeem"
	"github.com/bus-pooling/bus-pooling/internal/secrets"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
	"github.com/bus-pooling/bus-pooling/internal/topup"
	"github.com/bus-pooling/bus-pooling/internal/topupchannel"
	"github.com/bus-pooling/bus-pooling/internal/vendoraccount"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
	"github.com/bus-pooling/bus-pooling/internal/webhookin"
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
	case "seed-vendor":
		return runSeedVendor(ctx, cfg, args)
	case "list-vendors":
		return runListVendors(ctx, cfg)
	default:
		return fmt.Errorf("未知子命令 %q（支持 serve | migrate | genkey | redeem | seed-vendor | list-vendors）", cmd)
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
// **凭证来源优先级**（decisions §11.6 · CLAUDE.md §7.1）：
//  1. `vendor_account` 表（AES 加密 · admin CLI `seed-vendor` 写入）· **生产必用**
//  2. env `BP_VENDOR_<slug>_API_KEY / _WEBHOOK_SECRET` · **仅本地 dev 兜底**
//
// 表里存在 (vendor_id, status=active) 就用表 · 否则回落 env。这样：
//  - 生产：init 时跑 `seed-vendor` 一次 · env 里只留 `BP_MASTER_KEY`
//  - dev：`.dev.env` 里塞明文继续能用 · 不必先 seed
//
// 密钥从 db/env 走到这里就是明文了 —— 落库前 secrets 加密 · 运行期内存是明文
// （要拿它发 HTTP 头）。**别把 registry / cfg.Secrets 打进日志**。
func buildVendorRegistry(ctx context.Context, cfg config.Config, vaStore *vendoraccount.Store) (*providers.Registry, error) {
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
	// resolveCred · 表优先 · 表空回落到 env 传入的 fallback
	resolveCred := func(vendorID, envAPIKey, envWebhook string) (string, string) {
		if vaStore == nil {
			return envAPIKey, envWebhook
		}
		cred, err := vaStore.LoadActive(ctx, vendorID)
		if err != nil {
			slog.Warn("vendor_account 查表出错·回落 env", "vendor", vendorID, "err", err)
			return envAPIKey, envWebhook
		}
		if cred == nil {
			return envAPIKey, envWebhook
		}
		wh := cred.WebhookSecret
		if wh == "" {
			// vendor_account 里没存 webhook 但 env 里有 · 保留 env 值
			wh = envWebhook
		}
		return cred.APIKey, wh
	}

	k91APIKey, k91Webhook := resolveCred("kiro91", cfg.Secrets.Kiro91APIKey, cfg.Secrets.Kiro91WebhookSecret)
	kceoAPIKey, _ := resolveCred("kiroceo", cfg.Secrets.KiroCEOAPIKey, "")
	koooAPIKey, _ := resolveCred("kirooo", cfg.Secrets.KiroOOOAPIKey, "")
	kioAPIKey, _ := resolveCred("kiroappio", cfg.Secrets.KiroAppIOAPIKey, "")
	kccAPIKey, _ := resolveCred("kiroappcc", cfg.Secrets.KiroAppCCAPIKey, "")
	kdropAPIKey, kdropWebhook := resolveCred("kirodrop", cfg.Secrets.KiroDropAPIKey, cfg.Secrets.KiroDropWebhookSecret)

	err := kiro.Register(r, kiro.Config{
		Kiro91:    base(cfg.Vendors.Kiro91, k91APIKey, k91Webhook),
		KiroCEO:   base(cfg.Vendors.KiroCEO, kceoAPIKey, ""),
		KiroOOO:   base(cfg.Vendors.KiroOOO, koooAPIKey, ""),
		KiroAppIO: base(cfg.Vendors.KiroAppIO, kioAPIKey, ""),
		KiroAppCC: base(cfg.Vendors.KiroAppCC, kccAPIKey, ""),
		KiroDrop:  base(cfg.Vendors.KiroDrop, kdropAPIKey, kdropWebhook),
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// loadRequestSnapshot · P0 修（codex 三轮）· janitor 反查 gateway 时·**直接**
// 读起单时冷冻在 topup_order.gateway_request_snapshot 的 JSON 反序列化返回。
//
// **为什么不从 config 重建**：起单跟 janitor 反查之间·汇率 / channel config /
// payer_email 都可能变过·从当前 config 重建 → gateway 幂等指纹跟起单时不同 →
// gateway 会当"新单"建·而不是 replay 原单。语义错。
//
// snapshot 空（起单没走到 SaveGatewayRequestSnapshot·极少见）→ 返
// ErrGatewayFindUnavailable · janitor 走 pending_manual 兜底 · 不 expire。
func loadRequestSnapshot(orders *topup.Store) func(context.Context, string) (*paymentgw.CreatePaymentRequest, error) {
	return func(ctx context.Context, clientOrderID string) (*paymentgw.CreatePaymentRequest, error) {
		order, err := orders.FindByClientOrderID(ctx, clientOrderID)
		if err != nil {
			return nil, err
		}
		return topup.UnmarshalRequestSnapshot(order.GatewayRequestSnapshot)
	}
}

// topupChannelRegistry 装 topup 渠道注册表 · 支持 env override 开关。
//
// 每个 channel 由 `BP_TOPUP_<CHANNEL>_ENABLED=1|0` 单独开关·默认按 registry 里的
// 默认值走。未来 gateway 开启对应 rail 时·改 env 即可打开·**不改代码**。
func topupChannelRegistry() *topupchannel.Registry {
	envBool := func(name string, dflt bool) bool {
		v := os.Getenv(name)
		if v == "" {
			return dflt
		}
		return v == "1" || strings.EqualFold(v, "true")
	}
	return topupchannel.New(map[topupchannel.ID]bool{
		topupchannel.Waffo:   envBool("BP_TOPUP_WAFFO_ENABLED", true),
		topupchannel.EPUSDT:  envBool("BP_TOPUP_EPUSDT_ENABLED", false),
		topupchannel.Bybit:   envBool("BP_TOPUP_BYBIT_ENABLED", false),
		topupchannel.Binance: envBool("BP_TOPUP_BINANCE_ENABLED", false),
	})
}

// ratesFromEnv 从 env 读各层费率（basis point · 1 bp = 0.01%）。
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
	vendors := map[providers.VendorID]decider.VendorClient{}
	var pool decider.PoolClient
	var pubPool housepool.HousePool
	if !live {
		// mock 模式 · 装一个 default DryRunVendor（Vendor91Kiro 冒充） · api 层不指定 vendor 时用
		// 加装 6 家 DryRunVendor · 让"指定 vendor" 也能 mock 走通
		vendor = &decider.DryRunVendor{VendorID: providers.Vendor91Kiro}
		for _, id := range []providers.VendorID{
			providers.Vendor91Kiro, providers.VendorKiroCEO, providers.VendorKiroOOO,
			providers.VendorKiroAppIO, providers.VendorKiroAppCC, providers.VendorKiroDrop,
		} {
			vendors[id] = &decider.DryRunVendor{VendorID: id}
		}
		pool = &decider.DryRunPool{}
		if !cfg.DryRun {
			slog.Warn("拉号走 mock · 要接真链路请显式设 BP_ALLOW_LIVE_PULL=1")
		}
	} else {
		// live 模式 · 所有已注册 vendor 平等参与（**P1-4 修**：不再硬编偏向任何一家）
		// **任意一家启用即可 live**。default vendor 由**config 显式指定**·
		// 未指定 = fail（不允许隐式偏向任何一家）。
		orderedIDs := []providers.VendorID{
			providers.Vendor91Kiro, providers.VendorKiroCEO, providers.VendorKiroOOO,
			providers.VendorKiroAppIO, providers.VendorKiroAppCC, providers.VendorKiroDrop,
		}
		for _, id := range orderedIDs {
			if v, err := reg.Get(id); err == nil {
				vendors[id] = decider.VendorClient(v)
			}
		}
		if len(vendors) == 0 {
			return nil, nil, decider.Rates{}, fmt.Errorf(
				"live 模式必须至少启用一家 vendor（config.vendors.*.enabled = true）")
		}

		// default vendor · 客户端不传 vendor_id 时用（1a-1b 手工配·1d+ 算法比价）
		defaultID := providers.VendorID(cfg.Decider.DefaultVendor)
		if defaultID == "" {
			return nil, nil, decider.Rates{}, fmt.Errorf(
				"live 模式必须显式配 decider.default_vendor · 见 config.example.yaml")
		}
		v, ok := vendors[defaultID]
		if !ok {
			return nil, nil, decider.Rates{}, fmt.Errorf(
				"decider.default_vendor 未启用·检查 config.vendors 配置")
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
		// live 模式下强烈建议配·防 housepool 契约漂移后我方误发请求
		// **注意**：这里比的是**语义版本**（CARGO_PKG_VERSION）·不是 commit SHA。
		// 真绑 build sha 需 housepool 加 endpoint · 上游未提供。
		if cfg.Housepool.ExpectedVersion != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			gotVersion, verErr := poolClient.GetVersion(ctx)
			cancel()
			if verErr != nil {
				return nil, nil, decider.Rates{}, fmt.Errorf(
					"housepool 版本校验失败·无法拉版本·拒启动: %w", verErr)
			}
			if gotVersion != cfg.Housepool.ExpectedVersion {
				return nil, nil, decider.Rates{}, fmt.Errorf(
					"housepool 版本对不上·期望 %q·实际 %q·契约可能已漂移·拒启动",
					cfg.Housepool.ExpectedVersion, gotVersion)
			}
			slog.Info("housepool 版本校验通过", "version", gotVersion)
		}
		vendor = v
		pool = poolClient
		pubPool = poolClient
		enabledIDs := make([]providers.VendorID, 0, len(vendors))
		for id := range vendors {
			enabledIDs = append(enabledIDs, id)
		}
		slog.Warn("拉号走 LIVE 链路 · 会产生真扣款",
			"enabled_vendors", enabledIDs, "default", v.ID())
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

	// 1b P1-2A · vendor_pricing 表适配·空表时全走 fallback（CNY 1:1 · 兼容 1a）
	pricingAdapter := pricing.NewDeciderAdapter(pricing.NewStore(sqldb.DB))
	// 1b P1-2B · surcharge_rule 表适配·空表时走 env rates 兜底
	surchargeResolver := pricing.NewSurchargeResolver(pricing.SurchargeResolverConfig{
		Store:       pricing.NewSurchargeStore(sqldb.DB),
		EnvFallback: rates,
	})

	return decider.New(decider.Config{
		DB:            sqldb.DB,
		State:         decider.NewStore(sqldb.DB),
		Vendor:        vendor,
		Vendors:       vendors,
		Pool:          pool,
		Rates:         rates,
		Pricing:       pricingAdapter,
		RatesResolver: surchargeResolver,
		// 拉号并发 + 数量区间走 config.pull（§8.35 #18 · 避免并发打爆上游）
		Limits: decider.Limits{
			MaxConcurrentPerVendor:    cfg.Pull.MaxConcurrentPerVendor,
			MaxConcurrentPerPassenger: cfg.Pull.MaxConcurrentPerPassenger,
			MinCount:                  cfg.Pull.MinCount,
			MaxCount:                  cfg.Pull.MaxCount,
		},
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

	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	// vendor_account 加密凭证存 · nil-safe（本地 dev 主密钥不装配时用 nil · registry 会回落 env）
	var vaStore *vendoraccount.Store
	if cfg.Secrets.MasterKey != "" {
		cipher, err := secrets.New(cfg.Secrets.MasterKey)
		if err != nil {
			return fmt.Errorf("secrets cipher 装配: %w", err)
		}
		vaStore = vendoraccount.NewStore(database.DB, cipher)
		if ids, err := vaStore.ListActiveVendorIDs(ctx); err == nil && len(ids) > 0 {
			slog.Info("vendor_account 已 seed", "vendors", ids)
		} else if err == nil {
			slog.Info("vendor_account 表空 · vendor 凭证回落到 env (BP_VENDOR_*_API_KEY)")
		}
	}

	// vendor registry —— 业务层只认 providers.Registry，装配是 main 的事（契约 §10）
	vendorRegistry, err := buildVendorRegistry(ctx, cfg, vaStore)
	if err != nil {
		return err
	}
	for _, e := range vendorRegistry.All() {
		slog.Info("vendor 已注册", "vendor", e.VendorID, "enabled", e.Enabled)
	}

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
	// 加上 probeStore + probeInterval · 让 StatusOverview 有历史数据可读
	probeStore := vendorview.NewProbeStore(database.DB)
	const probeInterval = 60 * time.Second
	orderKeyStoreForView := vendorview.NewOrderKeyStore(database.DB)
	vendorSvc, err := vendorview.New(vendorview.Config{
		Registry:      vendorRegistry,
		Rates:         rates,
		ProbeStore:    probeStore,
		ProbeInterval: probeInterval,
		OrderKeyStore: orderKeyStoreForView,
	})
	if err != nil {
		return err
	}

	// vendor 状态探针 · 每 vendor 一个 goroutine · 每 60s 拨号 vendor.Stock
	// 结果写 vendor_probe · /api/vendors/status 从表读，不实时打上游
	// 探测 timeout 10s（vendor 侧偶尔慢·比原 3s 更宽松·避免误报 timeout）
	prober := vendorview.NewProber(vendorview.ProberConfig{
		Registry: vendorRegistry,
		Store:    probeStore,
		Interval: probeInterval,
		Timeout:  10 * time.Second,
	})
	prober.Start(ctx)
	defer prober.Stop(3 * time.Second)

	// Backfiller · 每 5 分钟拉一次 vendor 侧全量订单 + key 历史
	// 落 vendor_order + vendor_key · 是 /api/vendors/status + /api/vendors/prices 共同数据源
	orderKeyStore := vendorview.NewOrderKeyStore(database.DB)
	backfiller := vendorview.NewBackfiller(vendorview.BackfillerConfig{
		Registry: vendorRegistry,
		Store:    orderKeyStore,
		Interval: 5 * time.Minute,
		Timeout:  20 * time.Second,
	})
	backfiller.Start(ctx)
	defer backfiller.Stop(5 * time.Second)

	handoffs := handoff.NewStore(database.DB, 0) // 0 = 默认 TTL

	// paymentgw client · 三个环境变量都要有才装配·任缺其一走 dev mock 路径
	// BP_GW_BASE / BP_GW_TOKEN / BP_GW_SETTLEMENT_SECRET
	// BP_GW_SUCCESS_URL 可选·hosted checkout 完成后回跳我方前端 URL
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

	// deathwatch 只在真号池 live 起时建（mock 模式没 pool 可探）· goroutine 后面 Run
	// **提前建**（原来在 apiSrv 后建）· 因为 webhookin.Dispatcher 需要它做 SweepTrigger ·
	// 而 apiSrv 又要 dispatcher。顺序：pool → deathwatch → webhookin → apiSrv → go w.Run
	var deathwatchWatcher *deathwatch.Watcher
	if poolClient != nil {
		deathwatchWatcher = deathwatch.New(deathwatch.Config{
			DB:      database.DB,
			Pool:    poolClient,
			Refunds: deathwatch.NewSQLRefundStore(database.DB),
		})
	}

	// webhookin 分派器 · 收到 vendor webhook 后按事件类型走 vendor_dispatch / deathwatch / refund
	// db / dispatchStore / deathwatch 允许 nil · dispatcher 内部会 skip
	var deathwatchTrigger webhookin.SweepTrigger
	if deathwatchWatcher != nil {
		deathwatchTrigger = &deathwatchTriggerAdapter{w: deathwatchWatcher}
	}
	webhookDispatcher := webhookin.New(webhookin.Config{
		DB:            database.DB,
		DispatchStore: orderKeyStore, // 复用 backfiller 的 store（同一张 vendor_dispatch 表）
		Deathwatch:    deathwatchTrigger,
	})
	slog.Info("webhookin 分派器已装配")

	apiSrv := api.NewServer(api.ServerDeps{
		DB:                  database.DB,
		Passengers:          passenger.NewStore(database.DB),
		Wallets:             wallet.NewStore(database.DB),
		Strategies:          strategy.NewStore(database.DB),
		Buses:               bus.NewStoreWithConfig(database.DB, cfg.Bus.MaxMembers),
		Decider:             orch,
		Redeems:             redeem.NewStore(database.DB),
		Topups:              topup.NewStore(database.DB),
		PullRecords:         pullrecord.NewStore(database.DB),
		Handoffs:            handoffs,
		Pool:                poolClient, // 可能为 nil（mock 模式）· handler 有 nil 兜底
		VendorView:          vendorSvc,
		Insights:            insight.NewStore(database.DB),
		Downstreams:         downstream.NewStore(database.DB, cipher),
		TopupChannels:       topupChannelRegistry(),
		PendingTopups:       topup.NewPendingStore(database.DB),
		PaymentGW:           pgw,
		PaymentGWSuccessURL: os.Getenv("BP_GW_SUCCESS_URL"),
		SecureCookie:        secureCookie,
		Promos:              cfg.Promo.Items,
		CommunityChannels:   cfg.Community.Channels,
		VendorAccounts:      vaStore,           // vendor_account 表 · webhook 验签走这里读 secret
		WebhookDispatcher:   webhookDispatcher, // vendor webhook 事件的分派器
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
	// pool 非 nil 时（live 模式）·装完整 completeFn 让 confirmed 卡单能重试 DELETE。
	// completeFn 走 handoff.Complete · 跟 api.completeHandoff 共用同一份实现
	// （消 Standards duplication · 修 P1-A：不再有静默跳过 / 忽略 DB 错误）
	handoffJanitorCfg := handoff.JanitorConfig{Store: handoffs}
	if poolClient != nil {
		completeDeps := handoff.CompleteDeps{DB: database.DB, Pool: poolClient}
		handoffJanitorCfg.CompleteFn = func(ctx context.Context, p handoff.Pending) error {
			return handoff.Complete(ctx, completeDeps, p.CredentialIDs)
		}
	}
	handoffJanitor := handoff.NewJanitor(handoffJanitorCfg)
	go handoffJanitor.Run(ctx)
	slog.Info("handoff janitor 已启动")

	// assign janitor · 扫 pending_assignment 卡在 initial 太久的行（09-transactions §5）
	// tx1 落 initial + pool.UpdateCredential + tx2 completed 是三段·中间崩会留 initial。
	// 阶段 1a 简化：转 need_manual 让运营查·1c 加自动 reconcile。
	// assign janitor · into_bus 分支支持 reconcile pool groups（1a 收尾 · P0-3）
	// pool 已迁 → 前推 completed · 未迁 → 回滚允许重试 · 疑难 → need_manual
	// pool 未装配（mock 模式）时自动降级为"直接 need_manual"·保守
	assignJanitor := pullrecord.NewAssignJanitor(pullrecord.AssignJanitorConfig{
		DB:    database.DB,
		Store: pullrecord.NewStore(database.DB),
		Pool:  poolClient, // 可能 nil · janitor 内部会兜底
	})
	go assignJanitor.Run(ctx)
	slog.Info("assign janitor 已启动")

	// topup janitor · 扫 pending_topup 卡在中间态的行（1b P1-C · 09-transactions §6）
	// webhook 是主推进 · janitor 是兜底：gateway 崩溃 / 我方 MarkPaid 失败时能恢复
	topupStore := topup.NewStore(database.DB)
	topupJanCfg := topup.JanitorConfig{
		Orders:    topupStore,
		Pending:   topup.NewPendingStore(database.DB),
		Completer: topupStore,
	}
	// P0-3 · gateway 装配时·让 janitor 也能主动 poll · 覆盖 webhook 丢失场景
	if pgw != nil {
		topupJanCfg.Poller = &topup.GatewayPollerAdapter{
			Client: pgw,
			// P0 修（codex 三轮）：反查用起单时冷冻的 CreatePaymentRequest 快照重新 POST·
			// 保证幂等指纹跟初次一致（同 client_order_id·gateway 幂等表命中 → 200 replay）。
			// 从当前 config 重建是错的 —— 汇率 / channel config / payer_email 都可能变了·
			// 会命中新单而不是 replay。
			LoadRequestSnapshot: loadRequestSnapshot(topupStore),
		}
	}
	topupJanitor := topup.NewJanitor(topupJanCfg)
	go topupJanitor.Run(ctx)
	slog.Info("topup janitor 已启动")

	// deathwatch 已经在起 apiSrv 之前建好了（deathwatchWatcher 变量）· 这里只 Run
	if deathwatchWatcher != nil {
		go deathwatchWatcher.Run(ctx)
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

	// SPA 静态资源兜底 · 优先级最低 · mux 里 /api /healthz 都在前面 ·
	// 未匹配的走 web.Handler() 内嵌 dist（Docker 里 web-build stage 打进来）
	mux.Handle("/", web.Handler())

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

// deathwatchTriggerAdapter · 把 *deathwatch.Watcher 转成 webhookin.SweepTrigger。
//
// 两个包各自定义了 SweepReport / RefundReport（防包依赖循环）· 字段一样但类型不同 ·
// 这里做字段拷贝翻译。
type deathwatchTriggerAdapter struct {
	w *deathwatch.Watcher
}

func (a *deathwatchTriggerAdapter) SweepOnce(ctx context.Context) webhookin.SweepReport {
	r := a.w.SweepOnce(ctx)
	return webhookin.SweepReport{
		Scanned:    r.Scanned,
		MarkedDead: r.MarkedDead,
		Errors:     r.Errors,
	}
}

func (a *deathwatchTriggerAdapter) RefundOnce(ctx context.Context, limit int) webhookin.RefundReport {
	r := a.w.RefundOnce(ctx, limit)
	return webhookin.RefundReport{
		Scanned:  r.Scanned,
		Refunded: r.Refunded,
		Errors:   r.Errors,
	}
}
