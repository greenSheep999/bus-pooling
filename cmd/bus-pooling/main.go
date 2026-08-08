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

	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
	"github.com/bus-pooling/bus-pooling/internal/secrets"
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
	default:
		return fmt.Errorf("未知子命令 %q（支持 serve | migrate | genkey）", cmd)
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

func runServe(ctx context.Context, cfg config.Config) error {
	// serve 需要主密钥（vendor 凭证 / 号池 token 都要解密）
	if err := cfg.RequireSecrets(); err != nil {
		return err
	}
	if _, err := secrets.New(cfg.Secrets.MasterKey); err != nil {
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
	// 业务路由在 Iss #4 起逐个加（internal/api）· 现在只有存活探针
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
