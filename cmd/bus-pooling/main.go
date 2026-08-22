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
	crand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/api"
	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/coupon"
	"github.com/bus-pooling/bus-pooling/internal/credplain"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/deathwatch"
	"github.com/bus-pooling/bus-pooling/internal/decider"
	"github.com/bus-pooling/bus-pooling/internal/delivery/handoff"
	"github.com/bus-pooling/bus-pooling/internal/delivery/passengerpool"
	"github.com/bus-pooling/bus-pooling/internal/downstream"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/housepool/kirors"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
	"github.com/bus-pooling/bus-pooling/internal/insight"
	"github.com/bus-pooling/bus-pooling/internal/marketstock"
	"github.com/bus-pooling/bus-pooling/internal/passenger"
	"github.com/bus-pooling/bus-pooling/internal/paymentgw"
	"github.com/bus-pooling/bus-pooling/internal/pricing"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/providers/kiro"
	"github.com/bus-pooling/bus-pooling/internal/pullrecord"
	"github.com/bus-pooling/bus-pooling/internal/redeem"
	"github.com/bus-pooling/bus-pooling/internal/secrets"
	"github.com/bus-pooling/bus-pooling/internal/stockwatch"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
	"github.com/bus-pooling/bus-pooling/internal/topup"
	"github.com/bus-pooling/bus-pooling/internal/topupchannel"
	"github.com/bus-pooling/bus-pooling/internal/vendoraccount"
	"github.com/bus-pooling/bus-pooling/internal/vendorbalance"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
	"github.com/bus-pooling/bus-pooling/internal/web"
	"github.com/bus-pooling/bus-pooling/internal/webhookin"
	"github.com/bus-pooling/bus-pooling/internal/xi8"
	"github.com/google/uuid"
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
	case "xi8-backfill":
		return runXi8Backfill(ctx, cfg, args)
	case "xi8-audit":
		return runXi8Audit(ctx, cfg, args)
	case "backfill-probe-zone":
		return runBackfillProbeZone(ctx, cfg, args)
	case "backfill-stock-delta":
		return runBackfillStockDelta(ctx, cfg, args)
	case "reconcile":
		return runReconcile(ctx, cfg, args)
	case "vendor-probe":
		return runVendorProbe(ctx, cfg, args)
	case "seed-credplain":
		return runSeedCredplain(ctx, cfg, args)
	default:
		return fmt.Errorf("未知子命令 %q（支持 serve | migrate | genkey | redeem | seed-vendor | list-vendors | xi8-backfill | xi8-audit | backfill-probe-zone | backfill-stock-delta | reconcile | vendor-probe）", cmd)
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
//   - 生产：init 时跑 `seed-vendor` 一次 · env 里只留 `BP_MASTER_KEY`
//   - dev：`.dev.env` 里塞明文继续能用 · 不必先 seed
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

	// 这家 vendor 的网页账密 · 拉 /api/user/* 的 ledger 用（登录无验证码 · 可自动重登）
	kccCfg := base(cfg.Vendors.KiroAppCC, kccAPIKey, "")
	kccCfg.LoginUser = os.Getenv("BP_VENDOR_KIROAPPCC_LOGIN_USER")
	kccCfg.LoginPass = os.Getenv("BP_VENDOR_KIROAPPCC_LOGIN_PASS")

	// 这家 vendor 的网页 session token · 拉 /api/v1/* 的降价 schedule 用（登录带图形验证码 ·
	// 人工 seed · 会过期 · 空则不拉降价表 · 现价链走 api_key 不受影响）
	kdropCfg := base(cfg.Vendors.KiroDrop, kdropAPIKey, kdropWebhook)
	kdropCfg.SessionToken = os.Getenv("BP_VENDOR_KIRODROP_SESSION_TOKEN")

	err := kiro.Register(r, kiro.Config{
		Kiro91:    base(cfg.Vendors.Kiro91, k91APIKey, k91Webhook),
		KiroCEO:   base(cfg.Vendors.KiroCEO, kceoAPIKey, ""),
		KiroOOO:   base(cfg.Vendors.KiroOOO, koooAPIKey, ""),
		KiroAppIO: base(cfg.Vendors.KiroAppIO, kioAPIKey, ""),
		KiroAppCC: kccCfg,
		KiroDrop:  kdropCfg,
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
		topupchannel.Bybit:   envBool("BP_TOPUP_BYBIT_ENABLED", false),
		topupchannel.Binance: envBool("BP_TOPUP_BINANCE_ENABLED", false),
		topupchannel.USDT:    envBool("BP_TOPUP_USDT_ENABLED", false),
		topupchannel.Tron:    envBool("BP_TOPUP_TRON_ENABLED", false),
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
// startMarketStockSweeper · 每 60s 扫一次 · 释放超过 ReserveTTL(5min) 的 reserved
// 一律用 background context 起（不用 runServe 的 ctx · 那个会被信号取消 · 关服时 goroutine 自然退）
func startMarketStockSweeper(ctx context.Context, store *marketstock.Store) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := store.SweepExpired(context.Background())
				if err != nil {
					slog.Warn("marketstock sweeper 出错", "err", err)
					continue
				}
				if n > 0 {
					slog.Info("marketstock sweeper 释放超时占用", "count", n)
				}
			}
		}
	}()
}

func buildDecider(
	cfg config.Config, sqldb *db.DB, reg *providers.Registry,
	enqueuer decider.StockEnqueuer,
	marketStockStore *marketstock.Store,
	credplainStore *credplain.Store,
) (*decider.Orchestrator, housepool.HousePool, decider.Rates, *pricing.SurchargeResolver, error) {
	live := !cfg.DryRun && os.Getenv("BP_ALLOW_LIVE_PULL") == "1"

	var vendor decider.VendorClient
	vendors := map[providers.VendorID]decider.VendorClient{}
	var pool decider.PoolClient
	var pubPool housepool.HousePool
	if !live {
		// mock 模式 · 装一个 default DryRunVendor(Vendor91Kiro 冒充) · api 层不指定 vendor 时用
		// 加装 6 家 DryRunVendor · 让"指定 vendor"也能 mock 走通
		vendor = &decider.DryRunVendor{VendorID: providers.Vendor91Kiro}
		for _, id := range []providers.VendorID{
			providers.Vendor91Kiro, providers.VendorKiroCEO, providers.VendorKiroOOO,
			providers.VendorKiroAppIO, providers.VendorKiroAppCC, providers.VendorKiroDrop,
		} {
			vendors[id] = &decider.DryRunVendor{VendorID: id}
		}
		// 第 7 家 · 手工池是**真实** vendor 实现（不是 mock）· dry-run 模式也用它
		// 库存/价格都在本地库 · 不打上游 API · 见 marketstock/vendor.go
		if marketStockStore != nil {
			vendors[providers.VendorKiroMarket] = marketstock.NewVendor(marketStockStore)
		}
		// **P1-g 修(2026-08-16)**: mock 模式下 housepool 若已配就装真 client ·
		// vendor 走 mock 但 group 迁移 / credential 探活等 housepool 侧操作走真。
		// 用途:手动 BatchImport 的号 + assign into_bus / push_pool 需真 housepool 同步 group。
		// 不装 · 走 DryRunPool · assign 号 group 只改本地 DB · housepool 后端 侧不动 → 车友取号权限错。
		if cfg.Housepool.BaseURL != "" && cfg.Secrets.HousepoolAdminKey != "" {
			hc, herr := httpx.New(httpx.Config{
				Timeout: cfg.HTTPX.Timeout, MaxRetries: cfg.HTTPX.MaxRetries,
				RetryBaseWait: cfg.HTTPX.RetryBaseWait,
				Proxy:         cfg.HTTPX.Proxy, NoProxy: cfg.HTTPX.NoProxy,
			})
			if herr != nil {
				return nil, nil, decider.Rates{}, nil, fmt.Errorf("mock+housepool · httpx: %w", herr)
			}
			poolClient, perr := kirors.New(kirors.Config{
				BaseURL: cfg.Housepool.BaseURL, AdminKey: cfg.Secrets.HousepoolAdminKey,
			}, hc)
			if perr != nil {
				return nil, nil, decider.Rates{}, nil, fmt.Errorf("mock+housepool · client: %w", perr)
			}
			pool = poolClient
			pubPool = poolClient
			slog.Info("housepool 已装配(mock vendor + real housepool 组合)",
				"base_url", cfg.Housepool.BaseURL)
		} else {
			pool = &decider.DryRunPool{}
			slog.Warn("housepool 未配 · 走 DryRunPool · assign 号不同步 group",
				"tip", "配 BP_HOUSEPOOL_URL + BP_HOUSEPOOL_ADMIN_KEY 让 mock+housepool 组合生效")
		}
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
			providers.VendorKiroMarket, // 我方第 7 家 · 手工池（Step 3f）
		}
		for _, id := range orderedIDs {
			if v, err := reg.Get(id); err == nil {
				vendors[id] = decider.VendorClient(v)
			}
		}
		if len(vendors) == 0 {
			return nil, nil, decider.Rates{}, nil, fmt.Errorf(
				"live 模式必须至少启用一家 vendor（config.vendors.*.enabled = true）")
		}

		// default vendor · 客户端不传 vendor_id 时用（1a-1b 手工配·1d+ 算法比价）
		defaultID := providers.VendorID(cfg.Decider.DefaultVendor)
		if defaultID == "" {
			return nil, nil, decider.Rates{}, nil, fmt.Errorf(
				"live 模式必须显式配 decider.default_vendor · 见 config.example.yaml")
		}
		v, ok := vendors[defaultID]
		if !ok {
			return nil, nil, decider.Rates{}, nil, fmt.Errorf(
				"decider.default_vendor 未启用·检查 config.vendors 配置")
		}
		hc, err := httpx.New(httpx.Config{
			Timeout: cfg.HTTPX.Timeout, MaxRetries: cfg.HTTPX.MaxRetries,
			RetryBaseWait: cfg.HTTPX.RetryBaseWait,
			Proxy:         cfg.HTTPX.Proxy, NoProxy: cfg.HTTPX.NoProxy,
		})
		if err != nil {
			return nil, nil, decider.Rates{}, nil, err
		}
		poolClient, err := kirors.New(kirors.Config{
			BaseURL: cfg.Housepool.BaseURL, AdminKey: cfg.Secrets.HousepoolAdminKey,
		}, hc)
		if err != nil {
			return nil, nil, decider.Rates{}, nil, fmt.Errorf("装配号池客户端: %w", err)
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
				return nil, nil, decider.Rates{}, nil, fmt.Errorf(
					"housepool 版本校验失败·无法拉版本·拒启动: %w", verErr)
			}
			if gotVersion != cfg.Housepool.ExpectedVersion {
				return nil, nil, decider.Rates{}, nil, fmt.Errorf(
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
		return nil, nil, decider.Rates{}, nil, fmt.Errorf(
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
		DB:      sqldb.DB,
		State:   decider.NewStore(sqldb.DB),
		Vendor:  vendor,
		Vendors: vendors,
		Pool:    pool,
		Rates:   rates,
		Pricing: pricingAdapter,
		// 估价基准读 vendor_probe.our_unit_credits（docs/10-pricing §1.4）·
		// 探针还没落过数时自动退回按快照现算
		Credits:       pricing.NewProbeCredits(sqldb.DB),
		RatesResolver: surchargeResolver,
		// 拉号并发 + 数量区间走 config.pull（§8.35 #18 · 避免并发打爆上游）
		Limits: decider.Limits{
			MaxConcurrentPerVendor:    cfg.Pull.MaxConcurrentPerVendor,
			MaxConcurrentPerPassenger: cfg.Pull.MaxConcurrentPerPassenger,
			MinCount:                  cfg.Pull.MinCount,
			MaxCount:                  cfg.Pull.MaxCount,
		},
		// 抢号链 · auto 模式缺货时挂单等补货（decisions §11.15）
		Enqueuer: enqueuer,
		// I-27 · 命中规则明细落 pull_round_surcharge(对账/申诉用) · 跟 RatesResolver 同一实例。
		HitsResolver: surchargeResolver,
		// 我方第 7 家手工池 seller · settle 里跟 credential_ledger.INSERT 同 tx 卖号
		// nil 允许（老装配 / 未接手工池）· 但号一旦是 market 来源就必须有
		MarketStock: marketStockStore,
		// I-01 · 手工池号 sold 时 · 迁明文 stash → credential_plaintext(同 tx)
		// nil 允许(cipher 未配)· 号仍能卖 · push_pool 会走 placeholder
		MarketPopper: credplainStore,
		// I-22 · 前 6 家 BatchImport 路径 · 拉号成功后同 tx 落号明文。
		// nil 允许(cipher 未配)· 号仍能卖 · push_pool / handoff 走 placeholder。
		PlaintextSaver: credplainStore,
	}), pubPool, rates, surchargeResolver, nil
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
	// 我方第 7 家 Kiro Vendor Market · 手工上架 · 库存来自 market_stock_item 表
	// 装同一个 Store 实例:decider 的 settle SellTx + api 的 admin/market/* 都用它
	marketStockStore := marketstock.NewStore(database.DB)
	if err := vendorRegistry.Register(marketstock.NewVendor(marketStockStore), true); err != nil {
		return fmt.Errorf("注册 Kiro Vendor Market: %w", err)
	}

	// credplain 提到这里(cipher 已在 562 行建)· 让 buildDecider / api.NewServer / pusher
	// 三处装配都拿同一实例(避免多份 Store 走同一把 cipher 但语义分裂)
	// nil 允许 · cipher 未配时(dev 环境无 BP_MASTER_KEY)手工池 stash 落不了 · push_pool 走 placeholder
	var credplainStore *credplain.Store
	if cipher != nil {
		credplainStore = credplain.New(database.DB, cipher)
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

	// ── 抢号链装配（decisions §11.15）─────────────────────────
	//
	// 构造顺序解环：Watcher{Firer:nil} → decider{Enqueuer:watcher} → SetFirer(decider)
	// （Watcher 要 decider 当 Firer · decider 要 Watcher 当 Enqueuer · 互相依赖）
	//
	// 两个开关都是**文件哨兵** · 运维 SSH 一条命令即时生效 · 不重启：
	//   touch <data>/TURBO_ON     → 人工强制抢（无视运营态自动判断）
	//   touch <data>/KILL_PULLS   → 急停 · 一切 Purchase 停
	dataDir := filepath.Dir(cfg.DB.Path)
	turboFlag := stockwatch.NewFileFlag(
		filepath.Join(dataDir, "TURBO_ON"), "turbo", slog.Default())
	killFlag := stockwatch.NewFileFlag(
		filepath.Join(dataDir, "KILL_PULLS"), "kill_pulls", slog.Default())
	turboFlag.Start(ctx, 5*time.Second)
	defer turboFlag.Stop(2 * time.Second)
	killFlag.Start(ctx, 5*time.Second)
	defer killFlag.Stop(2 * time.Second)

	modeMgr := stockwatch.NewModeMgr(database.DB)
	modeMgr.Start(ctx)
	defer modeMgr.Stop(2 * time.Second)

	// xi8 可买性 flag（buyable/blocked/floating）· **纯内部对账 / 参考**（看 xi8 怎么对齐上游）·
	// xi8 backfiller 每 5min 写。**不介入采购决策** —— 采购一律直接打 vendor（用户 2026-08-14 拍板）。
	xi8FlagStore := vendorview.NewFlagStore(database.DB)

	stockWatcher := stockwatch.New(stockwatch.Config{
		DB:     database.DB,
		Logger: slog.Default(),
		Mode:   modeMgr,
		Kill:   killFlag,
		Turbo:  turboFlag,
		// Firer 稍后 SetFirer 补 —— 构造环
		// **不接 xi8 guard**：能不能买以直接打 vendor 为准 · xi8 只做对账不进钱路
	})
	// TTL sweeper · 不扫的话过期挂单会让 demand 虚高 · mode 永远判 tight
	stockWatcher.StartSweeper(ctx, time.Minute)
	defer stockWatcher.StopSweeper(2 * time.Second)

	// 手工池 TTL sweeper · 5min 内没 sold 的 reserved 释放回 available（migration 047）·
	// 场景:decider 崩在 purchasing 前 / 请求上下文超时 / vendor 侧 5xx 反复重试没成
	// 不扫就永久占位 · 用户看到"缺货"但真库存还在
	startMarketStockSweeper(ctx, marketStockStore)
	slog.Info("抢号链已装配",
		"turbo_flag", turboFlag.Path(),
		"kill_flag", killFlag.Path(),
		"mode", modeMgr.Current().String())

	orch, poolClient, rates, surchargeResolver, err := buildDecider(cfg, database, vendorRegistry, stockWatcher, marketStockStore, credplainStore)
	if err != nil {
		return err
	}
	// 补上反向依赖 · 抢号链闭环（此时 Notify 才会真 fire）
	stockWatcher.SetFirer(orch)

	// vendorview 用同一份 rates（费率不进代码，走 decider.Rates 唯一入口）
	// 加上 probeStore + probeInterval · 让 StatusOverview 有历史数据可读
	probeStore := vendorview.NewProbeStore(database.DB)
	probeZoneStore := vendorview.NewProbeZoneStore(database.DB)
	const probeInterval = 60 * time.Second
	orderKeyStoreForView := vendorview.NewOrderKeyStore(database.DB)
	vendorSvc, err := vendorview.New(vendorview.Config{
		Registry:      vendorRegistry,
		Rates:         rates,
		ProbeStore:    probeStore,
		ProbeInterval: probeInterval,
		OrderKeyStore: orderKeyStoreForView,
		// 展示价换算 · USD 家不换会把展示价算成实际的 1/6.8（docs/10-pricing §1.3）
		Pricing: pricing.NewVendorViewLookup(pricing.NewStore(database.DB)),
		// Task 65 · 从 vendor_key 聚合号寿命 / 30d 存活率喂 AutoPick 打分
		// 没数据的家降级 50 常数（老行为 · 等价纯价格排序）
		Quality: vendorview.NewQualityStore(database.DB),
		// 第 7 家手工池 · Offers 端点组 Offer matrix 时读它（Step 4）
		MarketReader: newMarketReader(marketStockStore),
		// vendor 档位开关（migration 049）· 后台可 toggle · 不写代码
		PlanConfig: vendorview.NewPlanConfigStore(database.DB),
		// I-20 · 展示价跟 decider 拉号同源(surcharge_rule DB 表)· nil 时退 Rates env 兜底
		RatesResolver: surchargeResolver,
		// I-25 · offers 数量分档从 vendor_price_tier(qty_band)读入·前端切数量重算单价
		// 数据源:backfiller 每 5min 从实现 KeyTierLister 的家拉·nil = offers 不带 price_bands
		TierStore: vendorview.NewTierStore(database.DB),
	})
	if err != nil {
		return err
	}

	// **P4 · AutoPick 进 decider**（2026-08-14）：闭合"用户看到的推荐 = 真拉时用的"·
	// decider.Pull 里 VendorID 空且 preferred 也空时 · 走 vendorSvc.PickBestVendor（同一套打分）。
	// 装配顺序：orch → vendorSvc → orch.SetPicker（跟 stockwatch.SetFirer 一样解构造环）。
	orch.SetPicker(vendorSvc)

	// **P5 · 上游余额预检**（2026-08-14）：每 5min poll 一次每家 vendor 的 Balance() ·
	// decider.Pull 拉号前查缓存 · 余额<预估总额直接拒 ErrVendorInsufficient · 不发下单请求。
	// 避免 vendor 侧返 insufficient_balance 的被动失败。老装配 nil cache 走老行为。
	balanceCache := vendorbalance.New(vendorbalance.Config{
		Registry: vendorRegistry,
		Logger:   slog.Default(),
		Interval: 5 * time.Minute,
		Timeout:  8 * time.Second,
	})
	balanceCache.Start(ctx)
	defer balanceCache.Stop(3 * time.Second)
	orch.SetBalanceChecker(balanceCache)

	// vendor 状态探针 · 每 vendor 一个 goroutine · 每 60s 拨号 vendor.Stock
	// 结果写 vendor_probe · /api/vendors/status 从表读，不实时打上游
	// 探测 timeout 10s（vendor 侧偶尔慢·比原 3s 更宽松·避免误报 timeout）
	// **stock-delta 推算 restock** · 拿 OrderKeyStore 落 vendor_dispatch · 无额外 API 调用
	// 补上 4 家无 fleet 端点的 vendor 的开号事件流（decisions §11.9）
	// 复用 vendorSvc 那份 orderKeyStoreForView · 一处装配多处用
	// 管线心跳（migration 036）· Prober / Backfiller 每轮盖戳 · data-health 端点 +
	// StalenessChecker 据此发现"某条管线停更了"（token 过期 / vendor 改形状 / serve 挂）
	healthStore := vendorview.NewHealthStore(database.DB)

	prober := vendorview.NewProber(vendorview.ProberConfig{
		Registry:      vendorRegistry,
		Store:         probeStore,
		ZoneStore:     probeZoneStore, // migration 029 · 每 zone 一行 · 精确定价的权威源
		OrderKeyStore: orderKeyStoreForView,
		// 抢号链：stock-delta 推出 restock 时唤醒挂单（只在 tight / turbo 时真 fire）
		Notifier: stockWatcher,
		// pricing 标准化（docs/10-pricing §1.3 · migration 028）· 落库前把 vendor 报价换算成积分
		Pricing:     pricing.NewVendorViewLookup(pricing.NewStore(database.DB)),
		HealthStore: healthStore,
		Interval:    probeInterval,
		Timeout:     10 * time.Second,
	})
	prober.Start(ctx)
	defer prober.Stop(3 * time.Second)

	// DailyRollupper · 把 vendor_probe 明细聚合进 vendor_daily（/status 的 7d 事故读它）
	// 启动回补存量日期 · 之后每小时滚今天+昨天 · 纯内部滚动不打上游
	dailyRollupper := vendorview.NewDailyRollupper(probeStore, time.Hour, slog.Default())
	dailyRollupper.Start(ctx)
	defer dailyRollupper.Stop(3 * time.Second)

	// Backfiller · 每 5 分钟拉一次 vendor 侧全量订单 + key 历史
	// 落 vendor_order + vendor_key · 是 /api/vendors/status + /api/vendors/prices 共同数据源
	backfiller := vendorview.NewBackfiller(vendorview.BackfillerConfig{
		Registry: vendorRegistry,
		Store:    orderKeyStoreForView,
		// 交叉对账（docs/23-endpoints-todo §1）· 拉 vendor 侧流水落 vendor_ledger · 实现了 LedgerLister 的家才拉
		LedgerStore: vendorview.NewLedgerStore(database.DB),
		// 阶梯价格（docs/23-endpoints-todo）· 拉数量分档落 vendor_price_tier · 实现了 KeyTierLister 的家才拉
		TierStore:   vendorview.NewTierStore(database.DB),
		HealthStore: healthStore, // 每步盖管线心跳（migration 036）
		Interval:    5 * time.Minute,
		Timeout:     20 * time.Second,
	})
	backfiller.Start(ctx)
	defer backfiller.Stop(5 * time.Second)

	// StalenessChecker · 定时扫 pipeline_health · 陈旧管线大声打 ERROR（系统自己盯）·
	// 5min 一轮 · 首查延后一个间隔（等采集先盖上戳）
	stalenessChecker := vendorview.NewStalenessChecker(healthStore, 5*time.Minute, slog.Default())
	// 告警外发 · BP_ALERT_WEBHOOK 未设 = 只留 ERROR 日志（不外发）
	if alertURL := os.Getenv("BP_ALERT_WEBHOOK"); alertURL != "" {
		notifier := vendorview.NewWebhookNotifier(alertURL, 30*time.Minute, slog.Default())
		stalenessChecker.SetNotifier(notifier)
		slog.Info("StalenessChecker 装配告警外发", "cooldown", "30m")
	}
	stalenessChecker.Start(ctx)
	defer stalenessChecker.Stop(2 * time.Second)

	// 1f-C · 策略 Effective() 桥依赖 · 供三条自动触发路径共用(scheduler / deathwatch /
	// webhook 扫号)。装配层集中构造 · 保 strategy.Store + bus.Store + SystemDefaults 一份。
	effDepsMain := &effectiveDepsMain{
		strategies: strategy.NewStore(database.DB),
		buses:      bus.NewStoreWithConfig(database.DB, cfg.Bus.MaxMembers),
		sys: strategy.SystemDefaults{
			PerRoundCount: cfg.Pull.DefaultCount,
			DefaultZone:   strategy.ZoneAuto,
		},
	}

	// 1d · 自动补车 scheduler · 5min 扫水位低于阈值的 auto_refill bus · 走 decider.Pull
	// 补的号自动进 bus-<id> group · 下次循环看得到。**这是 1d 阶段最关键的活的部件** ——
	// 老代码只支持乘客手动点拉号 · scheduler 装上后车挂着就能自己补。
	autoRefill := bus.NewScheduler(database.DB, &autoRefillBridge{orch: orch, db: database.DB}, 5*time.Minute, slog.Default())
	// v1d-2 · 第二刀 · 装配 decider.Decide 适配器 · Scheduler 每辆车过统一决策器
	autoRefill.SetDecider(&schedulerDecideBridge{
		db:        database.DB,
		deps:      effDepsMain,
		modeMgr:   modeMgr,
		killFlag:  killFlag,
		turboFlag: turboFlag,
		probeZone: probeZoneStore,
		picker:    vendorSvc,
		logger:    slog.Default(),
	})
	// ActionEnqueue 分支 · 挂 stockwatch · 挂意图不预冻结
	autoRefill.SetEnqueuer(&schedulerEnqueueBridge{sw: stockWatcher, logger: slog.Default()})
	autoRefill.Start(ctx)
	defer autoRefill.Stop(2 * time.Second)

	// xi8 后台 backfill · 30s 拉 signals 增量 · 5min 拉 restock-log 全量
	// 服务停跑期 xi8 侧继续记 · 重启自动补 · 落 source='xi8' 不出前端（CLAUDE.md §0.1）
	// BP_XI8_API_KEY 未设置时 Start 是 no-op（本地 dev 无 xi8 也能跑）
	if xi8Key := os.Getenv("BP_XI8_API_KEY"); xi8Key != "" {
		xi8HTTP, err := httpx.New(httpx.Config{Timeout: 15 * time.Second, MaxRetries: 2})
		if err != nil {
			slog.Warn("xi8 httpx 装配失败 · 跳过 xi8 backfill", "err", err)
		} else {
			xi8Client := xi8.New(xi8Key, xi8HTTP)
			xi8Backfiller := xi8.NewBackfiller(xi8Client, orderKeyStoreForView, slog.Default())
			xi8Backfiller.SetZoneStore(probeZoneStore) // 逐 zone 单价 → 侧表 · docs/decisions §11.11
			xi8Backfiller.SetFlagStore(xi8FlagStore)   // buyable/blocked/floating → 抢号 guard · docs/23-endpoints-todo §3
			xi8Backfiller.Start(ctx, 30*time.Second, 5*time.Minute)
			defer xi8Backfiller.Stop(5 * time.Second)
			slog.Info("xi8 backfiller 已启动 · 30s signals + 5min full")
		}
	} else {
		slog.Info("BP_XI8_API_KEY 未设置 · xi8 backfiller 关闭")
	}

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
			// 号死后真调 decider.Pull 补车 · puller 用 decider.RefillAdapter 桥接
			RefillPuller: &refillPullerBridge{orch: orch},
		})
		// v1d-3 · 第三刀 · 装配 decider.Decide 适配器 · RefillTick 每条 pending_refill 过统一决策器
		// 关键:AutoRefillEnabled=false 时 · Decide 会拒 · 号死不自动补(退款照走)
		deathwatchWatcher.SetRefillDecider(&refillDecideBridge{
			db:        database.DB,
			deps:      effDepsMain,
			modeMgr:   modeMgr,
			killFlag:  killFlag,
			turboFlag: turboFlag,
			probeZone: probeZoneStore,
			picker:    vendorSvc,
			logger:    slog.Default(),
		})
		// RefillEnqueue 分支 · 挂 stockwatch · 挂意图不预冻结
		deathwatchWatcher.SetRefillEnqueuer(&refillEnqueueBridge{sw: stockWatcher, logger: slog.Default()})
	}

	// webhookin 分派器 · 收到 vendor webhook 后按事件类型走 vendor_dispatch / deathwatch / refund
	// db / dispatchStore / deathwatch 允许 nil · dispatcher 内部会 skip
	var deathwatchTrigger webhookin.SweepTrigger
	if deathwatchWatcher != nil {
		deathwatchTrigger = &deathwatchTriggerAdapter{w: deathwatchWatcher}
	}
	webhookDispatcher := webhookin.New(webhookin.Config{
		DB:            database.DB,
		DispatchStore: orderKeyStoreForView, // 复用同一份 store（同一张 vendor_dispatch 表）
		Deathwatch:    deathwatchTrigger,
		// 抢号链：**最快的信号**（vendor push 200ms-2s · 抢到号主要靠这条）
		Notifier: stockWatcher,
		// 第五刀 · vendor 新号 webhook 到时扫低水位 auto 车 · 逐辆调 Decide
		AutoScan: &webhookAutoScanBridge{
			db:           database.DB,
			deps:         effDepsMain,
			orch:         orch,
			stockWatcher: stockWatcher,
			modeMgr:      modeMgr,
			killFlag:     killFlag,
			turboFlag:    turboFlag,
			probeZone:    probeZoneStore,
			logger:       slog.Default(),
		},
		// 部分 vendor webhook 带 price/available · 顺手落 vendor_probe_zone
		// source='webhook' · 补探针间隙 + 前端 price-trend 多一路
		ProbeZone: probeZoneStore,
	})
	slog.Info("webhookin 分派器已装配")

	// webhook 静默哨兵 · 上游在开号但我方 webhook 收不到时报警。
	// 静默不会自己暴露（vendor 侧看我方永远 200 · /status 有探针兜底看不出缺口）·
	// 代价是抢号链在均衡态收不到这家信号 —— 见 webhookin/health.go。
	webhookHealth := webhookin.NewHealthChecker(webhookin.HealthConfig{
		DB:     database.DB,
		Logger: slog.Default(),
	})
	webhookHealth.Start(ctx)
	defer webhookHealth.Stop(2 * time.Second)

	// passengerpool.Pusher · 推乘客号池的双写(sprint-1e-1)。
	//
	// 装配条件：cipher 非 nil(要解密 admin_token) + downstream store 建了 + httpx 装了。
	// pool 是 nil 时也能装(pool 是 housepool · 推的对家跟 housepool 不同实例)。
	// **nil 兜底**：handler 会走 dry-run(只标 pushed_at) · 跟 1a 一致。
	//
	// **PlaintextLookup 明文缺口**(等 housepool 后端 reveal 端点 · docs/08 §12.1)：
	// 当前 housepool 后端 无 reveal 端点 · 装配层传 nil · Pusher 内部走 BP_ALLOW_PASSENGERPOOL_PLACEHOLDER
	// 兜底 · 生产禁用。**PLACEHOLDER_PLAINTEXT** · grep 定位这里·未来接了替换。
	downstreamStore := downstream.NewStore(database.DB, cipher)
	poolHTTPX, err := httpx.New(httpx.Config{
		Timeout: 15 * time.Second, MaxRetries: 0, // BatchImport 是 SSE · 不走重试
	})
	if err != nil {
		return fmt.Errorf("httpx(passengerpool): %w", err)
	}
	var pusher passengerpool.Pusher
	if downstreamStore != nil && credplainStore != nil {
		// **P0-c 修(2026-08-16)**: credplain 表 · 拉号成功那一刻明文加密缓存 ·
		// pusher 走这个查真明文 · 上游 housepool 后端 1.8.3 确认无 reveal 端点 ·
		// 手工池号 sold 时由 decider settle 从 stash 迁进来(I-01)· 老拉号靠 seed-credplain CLI。
		// credplain Get 找不到 · pusher 走 placeholder 兜底(dev mock 环境)
		pusher = passengerpool.NewPusher(passengerpool.PusherDeps{
			Downstreams:    downstreamStore,
			Plaintext:      credplain.NewLookup(credplainStore),
			PlaintextUsage: credplainStore, // 推成功后标 used_at · janitor 24h 硬删
			HTTPX:          poolHTTPX,
			DB:             database.DB,
			Logger:         slog.Default(),
		})
		slog.Info("passengerpool.Pusher 已装配 · 走 credplain 表(P0-c)")
	} else {
		slog.Warn("passengerpool.Pusher 未装配 · handler 走 dry-run", "cipher_nil", cipher == nil)
	}

	// webhookout.Dispatcher · 对外 webhook 出向(sprint-1e-2)
	//
	// 装配条件跟 pusher 类似:cipher 非 nil(解密 secret) + downstream store 建了。
	// **nil 兜底**：handleTestWebhook 走 1a 兼容分支(裸 POST) · 主链触发源判 nil 跳过。
	//
	// 四种触发源(装配后):
	//   ① decider.settle 成功 · new_keys_available
	//   ② deathwatch.markDead 车全灭 · all_keys_dead
	//   ③ deathwatch.RefundOnce 每笔退款 · warranty_refund
	//   ④ handoff.Complete / pullrecord.Assign push_pool 成功 · boarded
	//
	// **静默失败不阻塞主链** - Dispatch 是非阻塞入队 · 内部消费失败只 log · 不回滚主 tx。
	webhookOutDisp := buildWebhookout(database.DB, downstreamStore)
	// I-02 · bridge 建早 · 场景 1&2(decider.Pull) 挂 pullNotifier · 场景 3(assign into_bus)
	// 由 api 层的 AutoPushOnAssign hook 调 bridge.AutoPushOnAssign
	// pusher / downstreams nil 时静默跳过(测试 / dev 环境)
	pullBridge := &pullSuccessBridge{
		disp:        webhookOutDisp,
		pusher:      pusher,
		downstreams: downstreamStore,
		vendorView:  vendorSvc,
		db:          database.DB,
		logger:      slog.Default(),
	}

	if webhookOutDisp != nil {
		webhookOutDisp.Start(ctx)
		defer webhookOutDisp.Stop(3 * time.Second)
		// 桥接到 decider / deathwatch(handoff 桥在 handoff janitor 装配点)
		orch.SetPullNotifier(pullBridge)
		if deathwatchWatcher != nil {
			deathwatchWatcher.SetDeathNotifier(&deathBridge{disp: webhookOutDisp, db: database.DB, logger: slog.Default()})
			deathwatchWatcher.SetRefundNotifier(&refundBridge{disp: webhookOutDisp, db: database.DB})
		}
		slog.Info("webhookout.Dispatcher 已装配 · retrier 已启动 · 3 触发源已桥")
	} else {
		slog.Warn("webhookout.Dispatcher 未装配 · handleTestWebhook 走 1a 兼容分支")
		// I-02 · 即使 webhookout 未装配 · 只要 pusher + downstreams 就绪 · 自动推仍可跑
		orch.SetPullNotifier(pullBridge)
	}

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
		Downstreams:         downstreamStore,
		TopupChannels:       topupChannelRegistry(),
		PendingTopups:       topup.NewPendingStore(database.DB),
		PaymentGW:           pgw,
		PaymentGWSuccessURL: os.Getenv("BP_GW_SUCCESS_URL"),
		SecureCookie:        secureCookie,
		Promos:              cfg.Promo.Items,
		CommunityChannels:   cfg.Community.Channels,
		VendorAccounts:      vaStore,                                                                       // vendor_account 表 · webhook 验签走这里读 secret
		WebhookDispatcher:   webhookDispatcher,                                                             // vendor webhook 事件的分派器
		Health:              healthStore,                                                                   // 数据管线心跳（migration 036）· data-health 端点用
		Reconciler:          vendorview.NewReconciler(database.DB, vendorview.NewLedgerStore(database.DB)), //  对账
		AdminKey:            os.Getenv("BP_ADMIN_KEY"),
		Pusher:              pusher,         // nil 时 handler 走 dry-run · 跟 1a 一致
		WebhookOut:          webhookOutDisp, // nil 时 handleTestWebhook 走裸 POST
		// 1f-C · 策略 Effective() 系统默认值(config.pull.*)
		SysDefaults: strategy.SystemDefaults{
			PerRoundCount: cfg.Pull.DefaultCount,
			DefaultZone:   strategy.ZoneAuto,
		},
		// decisions §8.43 v2 · 优惠码服务 · topup / pull 两场景走 type 校验
		Coupons: coupon.NewStore(database.DB),
		// 我方第 7 家 Kiro Vendor Market 手工上架 · admin/market/* 路由生效
		// 需要 BP_ADMIN_KEY · 走跟 admin/data-health 同套鉴权
		// **必须跟 buildDecider 用同一个 Store 实例** —— 手工池 Reserve/Sell 是同一状态机
		MarketStock: marketStockStore,
		// I-01 · admin_market POST /admin/market/stock 时 · 明文加密写 market_stock_plaintext
		// 暂存表 · settle 时同 tx 迁到 credential_plaintext。跟 buildDecider / pusher 同实例
		Credplain: credplainStore,
		// I-29 · vendor_plan_config admin toggle · 运营改档位不改 SQL(要 BP_ADMIN_KEY)
		PlanConfigStore: vendorview.NewPlanConfigStore(database.DB),
		// I-02 · assign into_bus 场景 · handler 后台调 bridge 自动推
		AutoPushOnAssign: pullBridge.AutoPushOnAssign,
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
			// 每次卡单重试都带上当前 pending 的 passenger + notifier
			// (webhookOutDisp nil 时 Notifier nil · handoff.Complete 内部判 nil 跳过)
			deps := completeDeps
			deps.PassengerID = p.PassengerID
			if webhookOutDisp != nil {
				deps.Notifier = &janitorBoardedBridge{disp: webhookOutDisp}
			}
			return handoff.Complete(ctx, deps, p.CredentialIDs)
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

		// 自动补车 tick · 每 1min 消费 pending_refill · 装了 puller 才有意义
		if orch != nil {
			go func() {
				ticker := time.NewTicker(1 * time.Minute)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if n, err := deathwatchWatcher.RefillTick(ctx, 20); err != nil {
							slog.Warn("deathwatch refill tick 失败", "err", err)
						} else if n > 0 {
							slog.Info("deathwatch refill tick", "processed", n)
						}
					}
				}
			}()
			slog.Info("deathwatch refill tick 已启动 · 1min 一轮")
		}
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

// refillPullerBridge · 把 orch 转成 deathwatch.RefillPuller
//
// 直接调 orch.Pull · 内部处理 err 分类（缺货保 pending / 硬错重试）。
// 参考 decider/refill_puller.go 的 RefillAdapter · 但那份用 refillReq · 我们这里用
// deathwatch.RefillRequest 直接对接（避免 refillReq/RefillRequest 两个 struct）。
type refillPullerBridge struct {
	orch *decider.Orchestrator
}

func (b *refillPullerBridge) Refill(ctx context.Context, req deathwatch.RefillRequest) (bool, error) {
	if b.orch == nil {
		return false, nil
	}
	_, err := b.orch.Pull(ctx, decider.PullInput{
		PassengerID:  req.PassengerID,
		BusID:        req.BusID,
		Count:        req.Count,
		VendorID:     providers.VendorID(req.VendorID),
		MaxUnitPrice: req.MaxUnitPrice, // 第三刀 · Decide 输出的护栏
	})
	if err == nil {
		return true, nil
	}
	// 缺货 / 限流 · 保 pending · 不算失败
	if errors.Is(err, decider.ErrNoStock) || errors.Is(err, decider.ErrRateLimited) {
		return false, nil
	}
	return false, err
}

// refillDecideBridge · 第三刀 · 把 decider.Decide 接进 deathwatch.RefillTick
//
// 负责:
//  1. 调 strategy.Effective 拿车级/全局取严后的策略字段(1f-C · §4.3.4)
//  2. 组装 DecideInput(source=death_refill · 单车视角)
//  3. 调 decider.Decide
//  4. 输出翻译成 deathwatch.RefillVerdict
//
// 只处理有 bus_id 的 pending_refill · 无 bus_id(record group 单独号)直接放行。
//
// **1f-C** · 手工拼 bus 表字段 + passenger_strategy_default 已经改用
// strategy.Effective() · 桥自己不再直接读策略字段。
type refillDecideBridge struct {
	db        *sql.DB
	deps      *effectiveDepsMain // 1f-C · strategy.Effective() 依赖
	modeMgr   *stockwatch.ModeMgr
	killFlag  *stockwatch.FileFlag
	turboFlag *stockwatch.FileFlag
	probeZone *vendorview.ProbeZoneStore
	picker    *vendorview.Service // Enqueue 分支 vendor 空时兜底选一家
	logger    *slog.Logger
}

func (b *refillDecideBridge) Decide(ctx context.Context, req deathwatch.RefillRequest) deathwatch.RefillVerdict {
	// 单独号(record group · 无 bus_id)不过 Decide · 直接 Pull(号死退款是天赋)
	if req.BusID == "" {
		return deathwatch.RefillVerdict{
			Action:     deathwatch.RefillPull,
			PullCount:  req.Count,
			PullVendor: req.VendorID,
		}
	}

	// 1f-C · 策略优先级铁律 · 所有字段从 strategy.Effective() 拿(§4.3.4)。
	// req.PassengerID 已在 pending_refill 里 · 直接用。
	eff, err := strategy.Effective(ctx, b.deps, req.PassengerID, req.BusID, nil)
	if err != nil {
		b.logger.Warn("refillDecideBridge: Effective 失败·保守跳过",
			"bus", req.BusID, "err", err)
		return deathwatch.RefillVerdict{Action: deathwatch.RefillReject, Reason: "effective_lookup_failed"}
	}

	// 全局跨车调度护栏检查(migration 040 · CLAUDE §1.5) · 自动补车路径
	if reason, deny := autoRefillGuardrailsDeny(ctx, b.db, eff, req.PassengerID, req.VendorID); deny {
		b.logger.Info("refillDecideBridge: 护栏拒", "bus", req.BusID, "reason", reason)
		return deathwatch.RefillVerdict{Action: deathwatch.RefillReject, Reason: reason}
	}

	// 读车里活号按 vendor 分组(车级快照 · 不是策略字段)
	aliveByVendor := make(map[string]int)
	rows, err := b.db.QueryContext(ctx,
		`SELECT vendor_id, COUNT(*)
		   FROM credential_ledger
		  WHERE owner_bus_id = ? AND status = 'alive'
		  GROUP BY vendor_id`, req.BusID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var vid string
			var n int
			if err := rows.Scan(&vid, &n); err == nil && vid != "" {
				aliveByVendor[vid] = n
			}
		}
	}

	// 读 vendor 单价
	prices := make(map[string]decider.VendorPriceSnapshot, len(aliveByVendor))
	if b.probeZone != nil {
		for vid := range aliveByVendor {
			credits, at, ok := b.probeZone.LatestZoneCredits(ctx, vid, providers.Zone(""))
			if !ok {
				prices[vid] = decider.VendorPriceSnapshot{}
				continue
			}
			stale := time.Since(at) > 2*time.Minute
			prices[vid] = decider.VendorPriceSnapshot{
				UnitPriceMicro: credits,
				ObservedAt:     at,
				Stale:          stale,
			}
		}
	}

	mode := ""
	if b.modeMgr != nil {
		mode = b.modeMgr.Current().String()
	}
	kill := b.killFlag != nil && b.killFlag.Engaged()
	turbo := b.turboFlag != nil && b.turboFlag.Engaged()

	// EffectiveStrategy 的 MaxUnitPrice 已是全局∧车级取严后的最终值 · 直接传 ·
	// decider.Decide 的 BusMaxUnitPrice / PassengerMaxPrice 语义是分层输入 ·
	// 但既然 Effective 已经取严 · 分层再传只会重算一次同样的 min · 把最终值
	// 传成 BusMaxUnitPrice · PassengerMaxPrice=0 让 Decide 那边保持行为一致。
	minCountVal := 0
	if eff.RefillMinCount != nil {
		minCountVal = *eff.RefillMinCount
	}

	out, err := decider.Decide(ctx, decider.DecideInput{
		Source:            decider.SourceDeathRefill,
		BusID:             req.BusID,
		AliveByVendor:     aliveByVendor,
		AutoRefillEnabled: eff.AutoRefillEnabled,
		RefillWatermark:   eff.RefillWatermark,
		RefillMinCount:    minCountVal,
		BusMaxUnitPrice:   eff.MaxUnitPrice, // Effective 已取严 · 直接传
		PassengerMaxPrice: 0,                // 已进 BusMaxUnitPrice · 不重复
		PreferredVendor:   eff.PreferredVendor,
		Mode:              mode,
		KillPulls:         kill,
		Turbo:             turbo,
		PricesByVendor:    prices,
	})
	if err != nil {
		b.logger.Warn("refillDecideBridge: Decide 报错", "bus", req.BusID, "err", err)
		return deathwatch.RefillVerdict{Action: deathwatch.RefillReject, Reason: err.Error()}
	}

	verdict := deathwatch.RefillVerdict{Reason: out.RejectReason}
	// count · pending_refill 里的通常是补该辆死号一个 · minCount 兜底 · 两条分支共用
	count := req.Count
	if count < 1 {
		count = 1
	}
	if minCountVal > count {
		count = minCountVal
	}
	// vendor · pending_refill 明说的 vendor 优先(号死通常想补同家) · 兜底走 Effective 挑好的
	chosenVendor := req.VendorID
	if chosenVendor == "" {
		chosenVendor = eff.PreferredVendor
	}
	if chosenVendor == "" && b.picker != nil {
		if pv, _, ok := b.picker.PickBestVendor(ctx, ""); ok {
			chosenVendor = string(pv)
		}
	}
	maxPrice := eff.MaxUnitPrice

	switch out.Verdict {
	case decider.VerdictReject:
		verdict.Action = deathwatch.RefillReject
	case decider.VerdictEnqueue:
		verdict.Action = deathwatch.RefillEnqueue
		verdict.PullCount = count
		verdict.PullVendor = chosenVendor
		verdict.PullMaxPrice = maxPrice
		if chosenVendor == "" {
			// vendor 三级都空 · Enqueue 会因 VendorID="" 挂不上 · 保守转 Reject
			verdict.Action = deathwatch.RefillReject
			verdict.Reason = "no_vendor_for_enqueue · 三级降级都空"
		}
	case decider.VerdictPull:
		verdict.Action = deathwatch.RefillPull
		verdict.PullCount = count
		verdict.PullVendor = chosenVendor
		verdict.PullMaxPrice = maxPrice
	}
	return verdict
}

// autoRefillBridge · 1d · 把 orch 转成 bus.AutoRefiller
//
// 跟 refillPullerBridge 平行 · 只是接口签名不同（bus 包不 import deathwatch · 单独一个）。
// scheduler 5min 一轮 · 常见 err（余额不足 / 缺货 / 上限）都被吞成 nil · 下轮再试。
//
// **P0 fix(2026-08-15)**: 之前 scheduler 生成 32-hex idem 直接传给 decider.Pull ·
// 但 pending_purchase.idempotency_record_id FK → idempotency_record.id · idem 从未落表 ·
// 每 5min FK 787 崩一次 · 补车整链断。修法:落 idempotency_record 一行 · 传 UUID id。
type autoRefillBridge struct {
	orch *decider.Orchestrator
	db   *sql.DB
}

func (b *autoRefillBridge) Refill(ctx context.Context, req bus.AutoRefillRequest) error {
	if b.orch == nil {
		return nil
	}
	recordID, err := b.ensureRefillIdemRecord(ctx, req)
	if err != nil {
		return fmt.Errorf("autoRefillBridge: 落 idempotency_record: %w", err)
	}
	_, err = b.orch.Pull(ctx, decider.PullInput{
		PassengerID:         req.InitiatorPassengerID,
		BusID:               req.BusID,
		Count:               req.Count,
		IdempotencyRecordID: recordID,
		VendorID:            providers.VendorID(req.PreferredVendor),
		MaxUnitPrice:        req.MaxUnitPrice,
	})
	return err
}

// ensureRefillIdemRecord · scheduler 每轮 idem 是 32-hex · 转成 idempotency_record 行
//
// method/path 用虚拟值 `POST /internal/auto-refill` · 跟真 HTTP 路径隔开 · 幂等键仍是 req.IdempotencyRecordID
// (32-hex · scheduler 每轮新生成) · 每 5min 一新键 · 不撞 API 层用户请求。
// INSERT OR IGNORE 保证多进程/重启后同 key 不重复落。
func (b *autoRefillBridge) ensureRefillIdemRecord(ctx context.Context, req bus.AutoRefillRequest) (string, error) {
	if b.db == nil {
		return "", errors.New("autoRefillBridge.db 未装配")
	}
	// scheduler idem 是唯一入参 · body 空(无请求 body) · fingerprint 用 idem 兜底
	// (idempotency_record.request_fingerprint NOT NULL · 空串会撞 CHECK · 用 idem 保唯一)
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	const method = "POST"
	const path = "/internal/auto-refill"
	res, err := b.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO idempotency_record
		  (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, req.InitiatorPassengerID, method, path, req.IdempotencyRecordID, req.IdempotencyRecordID, now)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return id, nil
	}
	// 已存在 · 反查该 idem 对应的 id
	var existing string
	err = b.db.QueryRowContext(ctx, `
		SELECT id FROM idempotency_record
		 WHERE passenger_id = ? AND path = ? AND idempotency_key = ?`,
		req.InitiatorPassengerID, path, req.IdempotencyRecordID).Scan(&existing)
	if err != nil {
		return "", err
	}
	return existing, nil
}

// schedulerDecideBridge · 第二刀 · 把 decider.Decide 接进 bus.Scheduler
//
// 负责:
//  1. 调 strategy.Effective 拿车级/全局取严后的策略字段(1f-C · §4.3.4)
//  2. 从 bus.SchedulerCandidate.AliveByVendor + ModeMgr + vendor_probe_zone 组装
//     decider.DecideInput
//  3. 调 decider.Decide
//  4. 输出翻译成 bus.SchedulerVerdict
//
// 装配层给 SchedulerDecider 注入 · nil-safe(bus.Scheduler 未装配 decider 时走老路径)。
type schedulerDecideBridge struct {
	db        *sql.DB
	deps      *effectiveDepsMain // 1f-C · strategy.Effective() 依赖
	modeMgr   *stockwatch.ModeMgr
	killFlag  *stockwatch.FileFlag
	turboFlag *stockwatch.FileFlag
	probeZone *vendorview.ProbeZoneStore // 读 vendor 单价新鲜度
	picker    *vendorview.Service        // Enqueue 分支 vendor 空时兜底选一家
	logger    *slog.Logger
}

func (b *schedulerDecideBridge) Decide(ctx context.Context, busID string, cand bus.SchedulerCandidate) bus.SchedulerVerdict {
	// 1f-C · 拿 passenger owner · 走 Effective(cand.OwnerID 是 creator_passenger_id)
	eff, err := strategy.Effective(ctx, b.deps, cand.OwnerID, busID, nil)
	if err != nil {
		b.logger.Warn("schedulerDecideBridge: Effective 失败·保守跳过",
			"bus", busID, "err", err)
		return bus.SchedulerVerdict{Action: bus.ActionReject, Reason: "effective_lookup_failed"}
	}

	// 全局跨车调度护栏(migration 040 · CLAUDE §1.5) · vendor 空传("")· 仅判钱包/预算
	if reason, deny := autoRefillGuardrailsDeny(ctx, b.db, eff, cand.OwnerID, ""); deny {
		b.logger.Info("schedulerDecideBridge: 护栏拒", "bus", busID, "reason", reason)
		return bus.SchedulerVerdict{Action: bus.ActionReject, Reason: reason}
	}

	// 读 vendor 当前单价(freshness 判 stale)· 只查 cand.AliveByVendor 里的那几家
	prices := make(map[string]decider.VendorPriceSnapshot, len(cand.AliveByVendor))
	if b.probeZone != nil {
		for vid := range cand.AliveByVendor {
			credits, at, ok := b.probeZone.LatestZoneCredits(ctx, vid, providers.Zone(""))
			if !ok {
				prices[vid] = decider.VendorPriceSnapshot{}
				continue
			}
			stale := time.Since(at) > 2*time.Minute
			prices[vid] = decider.VendorPriceSnapshot{
				UnitPriceMicro: credits,
				ObservedAt:     at,
				Stale:          stale,
			}
		}
	}

	mode := ""
	if b.modeMgr != nil {
		mode = b.modeMgr.Current().String()
	}
	kill := b.killFlag != nil && b.killFlag.Engaged()
	turbo := b.turboFlag != nil && b.turboFlag.Engaged()

	minCountVal := 0
	if eff.RefillMinCount != nil {
		minCountVal = *eff.RefillMinCount
	}

	out, err := decider.Decide(ctx, decider.DecideInput{
		Source:            decider.SourceScheduler,
		BusID:             busID,
		AliveByVendor:     cand.AliveByVendor,
		AutoRefillEnabled: eff.AutoRefillEnabled, // Effective 里已含全局 fallback
		RefillWatermark:   eff.RefillWatermark,
		RefillMinCount:    minCountVal,
		BusMaxUnitPrice:   eff.MaxUnitPrice, // 已取严 · 直接传
		PassengerMaxPrice: 0,                // 已进 BusMaxUnitPrice
		PreferredVendor:   eff.PreferredVendor,
		Mode:              mode,
		KillPulls:         kill,
		Turbo:             turbo,
		PricesByVendor:    prices,
	})
	if err != nil {
		b.logger.Warn("schedulerDecideBridge: Decide 报错", "bus", busID, "err", err)
		return bus.SchedulerVerdict{Action: bus.ActionReject, Reason: err.Error()}
	}

	verdict := bus.SchedulerVerdict{Reason: out.RejectReason}
	// Pull / Enqueue 共用的参数(§5 Step 5)
	//
	// **1f-C 铁律**(§4.3.4)· gap 用 eff.RefillWatermark(Effective 已合并全局) ·
	// **不用** cand.Watermark(loadCandidates 返的车级原始值 · 未合并全局)。
	gap := eff.RefillWatermark - sumAlive(cand.AliveByVendor)
	count := minCountVal
	if count <= 0 {
		count = gap
	}
	if count < 1 {
		count = 1
	}
	maxPrice := eff.MaxUnitPrice
	// vendor · Effective 已按 request→车级→全局 挑好 · 空(AutoPick)时走 picker
	chosenVendor := eff.PreferredVendor
	if chosenVendor == "" && b.picker != nil {
		if pv, _, ok := b.picker.PickBestVendor(ctx, ""); ok {
			chosenVendor = string(pv)
		}
	}

	switch out.Verdict {
	case decider.VerdictReject:
		verdict.Action = bus.ActionReject
	case decider.VerdictEnqueue:
		verdict.Action = bus.ActionEnqueue
		verdict.PullCount = count
		verdict.PullVendor = chosenVendor
		verdict.PullMaxPrice = maxPrice
		if chosenVendor == "" {
			// vendor 三级都空 · Enqueue 会因 VendorID="" 挂不上 · 保守转 Reject
			verdict.Action = bus.ActionReject
			verdict.Reason = "no_vendor_for_enqueue · 三级降级都空"
		}
	case decider.VerdictPull:
		verdict.Action = bus.ActionPull
		verdict.PullCount = count
		// Pull 分支 vendor 允许空(decider.Pull 内部会再 AutoPick 一次) · 但填上更精确
		verdict.PullVendor = chosenVendor
		verdict.PullMaxPrice = maxPrice
	}
	return verdict
}

// sumAlive · 整车 alive
func sumAlive(m map[string]int) int {
	s := 0
	for _, v := range m {
		s += v
	}
	return s
}

// pickVendorForEnqueue / strictestMaxPrice · 1f-C 已删除 · 三级 vendor 降级 +
// 硬上限取严都由 strategy.Effective() 统一算(§4.3.4)。桥拿到的 EffectiveStrategy
// 里 PreferredVendor 空就调 picker · MaxUnitPrice 已经取严 —— 不再需要 wrap。

// webhookAutoScanBridge · 第五刀 · vendor 新号 webhook 到时 · 扫低水位 auto 车 · 逐辆调 Decide。
//
// 逻辑:
//  1. 查所有 auto_refill_enabled=1 且 alive < watermark 的 bus(排除已挂 stockwatch 的)
//     ** 候选筛 SQL 用 bus 表原始字段(auto_refill_enabled=1) —— 这不是决策 · 是过滤 ·
//     真正的字段生效值由后面 Effective() 逐辆决定(1f-C · §4.3.4)。
//  2. 逐辆调 strategy.Effective + decider.Decide(source=webhook)
//  3. Verdict=Pull → 调 orch.Pull(vendor 指定为 webhook 到的那家)
//  4. Verdict=Enqueue → 交给 stockwatch 挂单
//  5. Verdict=Reject → 静默
//
// 出错只 log · 不影响 webhook 主流程返 200。
type webhookAutoScanBridge struct {
	db           *sql.DB
	deps         *effectiveDepsMain // 1f-C · strategy.Effective() 依赖
	orch         *decider.Orchestrator
	stockWatcher *stockwatch.Watcher // Enqueue 目标
	modeMgr      *stockwatch.ModeMgr
	killFlag     *stockwatch.FileFlag
	turboFlag    *stockwatch.FileFlag
	probeZone    *vendorview.ProbeZoneStore
	logger       *slog.Logger
}

func (b *webhookAutoScanBridge) OnNewKeys(ctx context.Context, vendorID, zone string, newKeys int) {
	if b.orch == nil || b.db == nil {
		return
	}
	// 查候选:所有 active 车 · 排除已经在 stock_watcher watching 的 bus
	// (那些走 stockwatch fire 路径)
	//
	// **1f-C 铁律**(§4.3.4)· SQL **只**做 store 基础过滤(active + 未挂 stockwatch) ·
	// **不**做 auto/watermark 的 IFNULL 合并 —— 那些字段的生效值由后面 Effective()
	// 逐车重算。低水位判断也放到 decideAndAct 里 · 拿 eff.RefillWatermark 比 alive。
	//
	// **性能取舍**:候选池从"筛过 auto+watermark 的车"扩到"全部 active 车" ·
	// v1 期用户量下每次 webhook 到 · 扫全表秒级完成 · 换回 §4.3.4 单入口合规。
	rows, err := b.db.QueryContext(ctx, `
		SELECT b.id, b.creator_passenger_id,
		       COALESCE(SUM(CASE WHEN cl.status = 'alive' THEN 1 ELSE 0 END), 0) AS alive
		  FROM bus b
		  LEFT JOIN credential_ledger cl ON cl.owner_bus_id = b.id
		 WHERE b.status = 'active'
		   AND NOT EXISTS (
		     SELECT 1 FROM stock_watcher sw
		      WHERE sw.bus_id = b.id AND sw.status = 'watching'
		   )
		 GROUP BY b.id`)
	if err != nil {
		b.logger.Warn("webhookAutoScanBridge: 扫候选失败", "err", err)
		return
	}
	defer rows.Close()

	mode := ""
	if b.modeMgr != nil {
		mode = b.modeMgr.Current().String()
	}
	kill := b.killFlag != nil && b.killFlag.Engaged()
	turbo := b.turboFlag != nil && b.turboFlag.Engaged()

	touched := 0
	for rows.Next() {
		var (
			busID, ownerID string
			aliveTotal     int
		)
		if err := rows.Scan(&busID, &ownerID, &aliveTotal); err != nil {
			continue
		}
		touched++
		b.decideAndAct(ctx, busID, ownerID, aliveTotal, vendorID, mode, kill, turbo)
	}
	if touched > 0 {
		b.logger.Info("webhookAutoScanBridge: 扫 active auto 车",
			"vendor", vendorID, "zone", zone, "new_keys", newKeys, "touched", touched)
	}
}

// decideAndAct · 单车决策 + 动作(1f-C · 策略字段从 Effective 拿)
func (b *webhookAutoScanBridge) decideAndAct(
	ctx context.Context, busID, ownerID string,
	aliveTotal int, webhookVendor string,
	mode string, kill, turbo bool,
) {
	// 1f-C · 走 Effective · 拿最终策略字段(§4.3.4)
	eff, err := strategy.Effective(ctx, b.deps, ownerID, busID, nil)
	if err != nil {
		b.logger.Warn("webhookAutoScanBridge: Effective 失败·跳过",
			"bus", busID, "err", err)
		return
	}

	// **1f-C 铁律**(§4.3.4)· "auto 关 / watermark 0 / alive 已满"这三个决策口径
	// 从 Effective 生效值判定 · 不从 SQL 候选筛。
	if !eff.AutoRefillEnabled || eff.RefillWatermark <= 0 || aliveTotal >= eff.RefillWatermark {
		return
	}

	// 全局跨车调度护栏(migration 040 · CLAUDE §1.5)· webhook 触发有 vendor
	if reason, deny := autoRefillGuardrailsDeny(ctx, b.db, eff, ownerID, webhookVendor); deny {
		b.logger.Info("webhookAutoScanBridge: 护栏拒", "bus", busID, "vendor", webhookVendor, "reason", reason)
		return
	}

	// 读车里活号按 vendor 分组
	aliveByVendor := make(map[string]int)
	arows, _ := b.db.QueryContext(ctx,
		`SELECT vendor_id, COUNT(*) FROM credential_ledger
		 WHERE owner_bus_id = ? AND status = 'alive' GROUP BY vendor_id`, busID)
	if arows != nil {
		for arows.Next() {
			var v string
			var n int
			if err := arows.Scan(&v, &n); err == nil && v != "" {
				aliveByVendor[v] = n
			}
		}
		arows.Close()
	}

	// vendor 价格数据
	prices := make(map[string]decider.VendorPriceSnapshot, len(aliveByVendor))
	if b.probeZone != nil {
		for vid := range aliveByVendor {
			credits, at, ok := b.probeZone.LatestZoneCredits(ctx, vid, providers.Zone(""))
			if !ok {
				prices[vid] = decider.VendorPriceSnapshot{}
				continue
			}
			prices[vid] = decider.VendorPriceSnapshot{
				UnitPriceMicro: credits,
				ObservedAt:     at,
				Stale:          time.Since(at) > 2*time.Minute,
			}
		}
	}

	minCountVal := 0
	if eff.RefillMinCount != nil {
		minCountVal = *eff.RefillMinCount
	}

	out, err := decider.Decide(ctx, decider.DecideInput{
		Source:            decider.SourceWebhook,
		BusID:             busID,
		AliveByVendor:     aliveByVendor,
		AutoRefillEnabled: eff.AutoRefillEnabled,
		RefillWatermark:   eff.RefillWatermark,
		RefillMinCount:    minCountVal,
		BusMaxUnitPrice:   eff.MaxUnitPrice, // 已取严
		PassengerMaxPrice: 0,
		PreferredVendor:   eff.PreferredVendor,
		Mode:              mode,
		KillPulls:         kill,
		Turbo:             turbo,
		PricesByVendor:    prices,
	})
	if err != nil {
		return
	}

	switch out.Verdict {
	case decider.VerdictReject:
		return
	case decider.VerdictEnqueue:
		// 挂 stockwatch · 挂意图不预冻结
		gap := eff.RefillWatermark - aliveTotal
		count := minCountVal
		if count <= 0 {
			count = gap
		}
		if count < 1 {
			count = 1
		}
		// vendor · webhook 推的最优先(反正是刚来货) · 兜底 Effective 挑好的
		v := webhookVendor
		if v == "" {
			v = eff.PreferredVendor
			if v == "" {
				b.logger.Info("webhookAutoScanBridge: 无 vendor 可用 · skip Enqueue", "bus", busID)
				return
			}
		}
		cid, err := newAutoScanIdemID()
		if err != nil {
			return
		}
		_, eerr := b.stockWatcher.Enqueue(ctx, stockwatch.EnqueueParams{
			PassengerID:   ownerID,
			BusID:         busID,
			TargetGroup:   "bus-" + busID,
			VendorID:      v,
			Region:        "",
			ClientOrderID: cid,
			Count:         count,
			MaxUnitPrice:  eff.MaxUnitPrice,
			// ReservedAmount = 0 · 挂意图不预冻结
		})
		if eerr != nil {
			b.logger.Info("webhookAutoScanBridge: 挂 stockwatch 失败", "bus", busID, "err", eerr)
			return
		}
		b.logger.Info("webhookAutoScanBridge: Decide→Enqueue",
			"bus", busID, "vendor", v, "count", count)
		return
	case decider.VerdictPull:
		gap := eff.RefillWatermark - aliveTotal
		count := minCountVal
		if count <= 0 {
			count = gap
		}
		if count < 1 {
			return
		}
		idem, err := newAutoScanIdemID()
		if err != nil {
			return
		}
		// vendor 优先用 webhook 推的那家(反正是刚来货的)· 兜底 Effective 挑好的
		v := webhookVendor
		if v == "" {
			v = eff.PreferredVendor
		}
		b.logger.Info("webhookAutoScanBridge: Decide→Pull",
			"bus", busID, "vendor", v, "count", count)
		_, perr := b.orch.Pull(ctx, decider.PullInput{
			PassengerID:         ownerID,
			BusID:               busID,
			Count:               count,
			VendorID:            providers.VendorID(v),
			MaxUnitPrice:        eff.MaxUnitPrice,
			IdempotencyRecordID: idem,
		})
		if perr != nil {
			b.logger.Info("webhookAutoScanBridge: Pull 失败·下次 webhook 再试",
				"bus", busID, "err", perr)
		}
	}
}

// newAutoScanIdemID · 第五刀 webhook 扫每次生成幂等键
func newAutoScanIdemID() (string, error) {
	b := make([]byte, 16)
	if _, err := crand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// schedulerEnqueueBridge · bus.Scheduler → stockwatch.Enqueue 桥
//
// **挂意图不预冻结** —— 跟 maybeEnqueueOnNoStock 一致 · fire 时走 decider.Pull 完整钱包事务。
type schedulerEnqueueBridge struct {
	sw     *stockwatch.Watcher
	logger *slog.Logger
}

func (b *schedulerEnqueueBridge) Enqueue(ctx context.Context, req bus.AutoEnqueueRequest) error {
	if b.sw == nil {
		return errors.New("scheduler enqueue: stockwatch 未装配")
	}
	cid, err := newAutoScanIdemID()
	if err != nil {
		return err
	}
	_, err = b.sw.Enqueue(ctx, stockwatch.EnqueueParams{
		PassengerID:   req.InitiatorPassengerID,
		BusID:         req.BusID,
		TargetGroup:   "bus-" + req.BusID,
		VendorID:      req.PreferredVendor,
		ClientOrderID: cid,
		Count:         req.Count,
		MaxUnitPrice:  req.MaxUnitPrice,
		// ReservedAmount = 0 · 挂意图不预冻结
	})
	return err
}

// refillEnqueueBridge · deathwatch.RefillTick → stockwatch.Enqueue 桥
//
// **挂意图不预冻结** —— 跟 scheduler 一致 · pending_refill 挂完标 fulfilled(note=enqueued_to_stockwatch)。
type refillEnqueueBridge struct {
	sw     *stockwatch.Watcher
	logger *slog.Logger
}

func (b *refillEnqueueBridge) Enqueue(ctx context.Context, req deathwatch.RefillEnqueueRequest) error {
	if b.sw == nil {
		return errors.New("refill enqueue: stockwatch 未装配")
	}
	cid, err := newAutoScanIdemID()
	if err != nil {
		return err
	}
	target := "bus-" + req.BusID
	if req.BusID == "" {
		target = "record-" + req.PassengerID
	}
	_, err = b.sw.Enqueue(ctx, stockwatch.EnqueueParams{
		PassengerID:   req.PassengerID,
		BusID:         req.BusID,
		TargetGroup:   target,
		VendorID:      req.PreferredVendor,
		ClientOrderID: cid,
		Count:         req.Count,
		MaxUnitPrice:  req.MaxUnitPrice,
	})
	return err
}

// autoRefillGuardrailsDeny · 全局跨车调度护栏检查(migration 040 · CLAUDE §1.5)
//
// **只对自动补车链路生效** —— 手动拉号(请求带 override)**不受此约束** ·
// 见 15 §4.3.4 "手动拉号不受 auto-only guardrail 拦"。
//
// 三个护栏:
//  1. AutoRefillVendorAllowlist · 空表示不限 · 有值则被调度的 vendor 必须在列表里
//  2. AutoRefillMinWalletReserve · 钱包余额 < 该值 · 全部 auto 车暂停自动补
//  3. AutoRefillDailyBudget · 该乘客今日 auto 花费已 ≥ 该值 · 暂停自动补
//
// 返 (reason, deny) · deny=true 则调用方 return · 不进 Decide 后续步骤。
// reason 会进 log(不出用户视野 · CLAUDE §0.1)。
func autoRefillGuardrailsDeny(
	ctx context.Context,
	db *sql.DB,
	eff strategy.EffectiveStrategy,
	passengerID string,
	vendorID string, // 空 = 未定 vendor(仅 min_wallet_reserve / daily_budget 生效)
) (string, bool) {
	// 1. vendor 白名单
	if vendorID != "" && len(eff.AutoRefillVendorAllowlist) > 0 {
		allowed := false
		for _, v := range eff.AutoRefillVendorAllowlist {
			if v == vendorID {
				allowed = true
				break
			}
		}
		if !allowed {
			return "vendor_not_in_allowlist", true
		}
	}
	// 2. 钱包保护线
	if eff.AutoRefillMinWalletReserve > 0 {
		var balance int64
		if err := db.QueryRowContext(ctx,
			`SELECT balance FROM wallet WHERE passenger_id = ?`, passengerID).Scan(&balance); err == nil {
			if balance < eff.AutoRefillMinWalletReserve {
				return "wallet_below_reserve", true
			}
		}
	}
	// 3. 每日预算 · 今日 auto 花费累加(wallet_ledger.type='spend' AND kind='auto')
	if eff.AutoRefillDailyBudget > 0 {
		// 今日 = UTC 起始
		var spentToday int64
		day := time.Now().UTC().Format("2006-01-02")
		if err := db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(-amount), 0)
			  FROM wallet_ledger
			 WHERE passenger_id = ? AND type = 'spend'
			   AND substr(created_at, 1, 10) = ?`,
			passengerID, day).Scan(&spentToday); err == nil {
			if spentToday >= eff.AutoRefillDailyBudget {
				return "daily_budget_reached", true
			}
		}
	}
	return "", false
}
