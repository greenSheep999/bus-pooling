package main

// bus-pooling xi8-backfill · 一次性回填 xi8 restock-log + signals 到 vendor_dispatch。
//
// 用法：
//   bus-pooling xi8-backfill                # 默认 limit=500
//   bus-pooling xi8-backfill --limit=200
//
// bus-pooling xi8-audit · 对账 vendor_self vs xi8 · 输出漏采差异表。
//
// 前提：BP_XI8_API_KEY 环境变量存在 · 或已 `seed-vendor xi8 --api-key=...`。
// 落库源标 source='xi8' · 前端读路径不动（只查 vendor_self · CLAUDE.md §0.1）。

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/httpx"
	"github.com/bus-pooling/bus-pooling/internal/secrets"
	"github.com/bus-pooling/bus-pooling/internal/vendoraccount"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
	"github.com/bus-pooling/bus-pooling/internal/xi8"
)

func runXi8Backfill(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("xi8-backfill", flag.ExitOnError)
	var limit int
	fs.IntVar(&limit, "limit", 500, "拉多少条 · 上限 500（服务端约束）")
	_ = fs.Parse(args)

	apiKey := loadXi8APIKey(ctx, cfg)
	if apiKey == "" {
		return errors.New("xi8-backfill: 缺 API key · 设 BP_XI8_API_KEY env 或 `seed-vendor xi8 --api-key=X`")
	}

	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	hc, err := httpx.New(httpx.Config{Timeout: 15 * time.Second, MaxRetries: 2})
	if err != nil {
		return err
	}
	client := xi8.New(apiKey, hc)
	store := vendorview.NewOrderKeyStore(database.DB)
	bf := xi8.NewBackfiller(client, store, slog.Default())

	sigs, restocks, err := bf.RunOnce(ctx, limit)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "xi8-backfill: 完成 · signals=%d · restock-log=%d · 落 vendor_dispatch source='xi8'\n",
		sigs, restocks)
	return nil
}

// runXi8Audit · 对账 CLI · 输出 vendor_self vs xi8 漏采差异
func runXi8Audit(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("xi8-audit", flag.ExitOnError)
	var windowHours int
	fs.IntVar(&windowHours, "window", 48, "对账时间窗（小时 · 默认 48h）")
	_ = fs.Parse(args)

	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	cutoff := time.Now().UTC().Add(-time.Duration(windowHours) * time.Hour).Format(time.RFC3339)

	rows, err := database.DB.QueryContext(ctx, `
		SELECT vendor_id, source, COUNT(*) as batches, SUM(count) as keys,
		       MIN(dispatched_at), MAX(dispatched_at)
		  FROM vendor_dispatch
		 WHERE dispatched_at >= ?
		 GROUP BY vendor_id, source
		 ORDER BY vendor_id, source`, cutoff)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Fprintf(os.Stderr, "xi8-audit · 过去 %dh 各源批次统计（%s ~ now）\n\n", windowHours, cutoff)
	fmt.Fprintf(os.Stderr, "%-12s %-12s %8s %8s  %-20s %-20s\n",
		"vendor", "source", "batches", "keys", "min_time", "max_time")
	fmt.Fprintln(os.Stderr, "─────────────────────────────────────────────────────────────────────────────")

	type row struct {
		vendor, source, minT, maxT string
		batches, keys              int
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.vendor, &r.source, &r.batches, &r.keys, &r.minT, &r.maxT); err != nil {
			return err
		}
		all = append(all, r)
		fmt.Fprintf(os.Stderr, "%-12s %-12s %8d %8d  %-20s %-20s\n",
			r.vendor, r.source, r.batches, r.keys, r.minT, r.maxT)
	}

	// 简易差异检测：同一 vendor · vendor_self 批次数 < xi8 一半 → 有漏采嫌疑
	fmt.Fprintln(os.Stderr, "\n漏采嫌疑（vendor_self 批次 < xi8 * 0.5）：")
	perVendor := make(map[string]map[string]int)
	for _, r := range all {
		if perVendor[r.vendor] == nil {
			perVendor[r.vendor] = make(map[string]int)
		}
		perVendor[r.vendor][r.source] = r.batches
	}
	suspects := 0
	for v, srcs := range perVendor {
		self := srcs["vendor_self"]
		xi := srcs["xi8"]
		if xi > 0 && float64(self) < float64(xi)*0.5 {
			fmt.Fprintf(os.Stderr, "  %s · vendor_self=%d < xi8=%d · 检查探针 / fleet 端点\n", v, self, xi)
			suspects++
		}
	}
	if suspects == 0 {
		fmt.Fprintln(os.Stderr, "  （无 · vendor_self 覆盖良好）")
	}
	return nil
}

// loadXi8APIKey · 三级 fallback：env → vendor_account 表 → 空
func loadXi8APIKey(ctx context.Context, cfg config.Config) string {
	// 1. env 优先（dev 便捷）
	if k := os.Getenv("BP_XI8_API_KEY"); k != "" {
		return k
	}
	// 2. vendor_account 表（生产走 seed-vendor CLI 塞过）
	if cfg.Secrets.MasterKey == "" || cfg.DB.Path == "" {
		return ""
	}
	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return ""
	}
	defer database.Close()
	cipher, err := secrets.New(cfg.Secrets.MasterKey)
	if err != nil {
		return ""
	}
	store := vendoraccount.NewStore(database.DB, cipher)
	cred, err := store.LoadActive(ctx, "xi8")
	if err != nil || cred == nil {
		return ""
	}
	return cred.APIKey
}
