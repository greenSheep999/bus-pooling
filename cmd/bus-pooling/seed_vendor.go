package main

// bus-pooling seed-vendor · admin CLI · 明文 API key + webhook secret 加密写 vendor_account 表。
//
// 用法：
//   bus-pooling seed-vendor <vendor_id>              # 交互式 · 从 stdin 读明文
//   bus-pooling seed-vendor <vendor_id> --api-key=X  # 一次性 · 从 flag 读（部署脚本用）
//   bus-pooling seed-vendor <vendor_id> --api-key=X --webhook-secret=Y --label=aux
//
// vendor_id：术语铁律 §1.1 的 6 家 vendor slug 之一
//
// 部署流程：
//   1. env 里放 BP_MASTER_KEY（AES 主密钥 · 用 bus-pooling genkey 生成）
//   2. docker exec kirobus bus-pooling seed-vendor <slug> --api-key=<PLAINTEXT>
//   3. 服务启动装配 adapter 时从表读 · env 里不需要放 API key
//
// list-vendors 子命令：只列出哪些 vendor 已 seed，不解密（避免 CLI 输出泄漏明文）。

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/secrets"
	"github.com/bus-pooling/bus-pooling/internal/vendoraccount"
)

// 6 家 vendor slug 白名单 · 拒绝 seed 错拼写
var knownVendorSlugs = map[string]bool{
	"kiro91":    true,
	"kiroceo":   true,
	"kirooo":    true,
	"kiroappio": true,
	"kiroappcc": true,
	"kirodrop":  true,
}

func runSeedVendor(ctx context.Context, cfg config.Config, args []string) error {
	if cfg.Secrets.MasterKey == "" {
		return errors.New("seed-vendor: BP_MASTER_KEY 空 · 先 bus-pooling genkey 生成 · 塞进 env")
	}
	if len(args) < 1 {
		return errors.New("seed-vendor: 需要 vendor_id 参数（见 术语铁律 §1.1 · 6 家 vendor slug 之一）")
	}
	vendorID := args[0]
	if !knownVendorSlugs[vendorID] {
		return fmt.Errorf("seed-vendor: 未知的 vendor_id %q（术语铁律 §1.1）", vendorID)
	}

	// flag 解析（vendor_id 后面）
	fs := flag.NewFlagSet("seed-vendor", flag.ExitOnError)
	var (
		apiKey        string
		webhookSecret string
		label         string
		authScheme    string
	)
	fs.StringVar(&apiKey, "api-key", "", "vendor API key 明文（不传则交互式从 stdin 读·或用 SEED_API_KEY env）")
	fs.StringVar(&webhookSecret, "webhook-secret", "", "webhook 签名密钥明文（不传则用 SEED_WEBHOOK_SECRET env · 空 = 保留旧值）")
	fs.StringVar(&label, "label", "default", "凭证别名（默认 default · 主备号时用）")
	fs.StringVar(&authScheme, "auth-scheme", "api_key", "认证方式 · api_key | bearer | cookie")
	_ = fs.Parse(args[1:])

	// env fallback · 让部署脚本能 `docker exec -e SEED_WEBHOOK_SECRET=X kirobus seed-vendor <slug>`
	// 敏感值不进 shell 命令行·不进 ps 输出。flag 优先·env 兜底。
	if apiKey == "" {
		apiKey = os.Getenv("SEED_API_KEY")
	}
	if webhookSecret == "" {
		webhookSecret = os.Getenv("SEED_WEBHOOK_SECRET")
	}

	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return fmt.Errorf("seed-vendor: 打开 DB: %w", err)
	}
	defer database.Close()

	// 迁移检查 · vendor_account 表得存在
	if _, err := database.DB.ExecContext(ctx,
		"SELECT 1 FROM vendor_account LIMIT 0"); err != nil {
		return fmt.Errorf("seed-vendor: vendor_account 表不存在 · 先跑 bus-pooling migrate up: %w", err)
	}

	cipher, err := secrets.New(cfg.Secrets.MasterKey)
	if err != nil {
		return fmt.Errorf("seed-vendor: cipher 装配: %w", err)
	}
	store := vendoraccount.NewStore(database.DB, cipher)

	// 先读旧凭证 · 让新传的字段"打补丁"式覆盖 · 没传的保留旧值。
	// 场景：先 seed api_key 部署跑起来 · 再回 vendor 后台配 webhook 拿到 secret ·
	// 只跑 seed-vendor <slug> --webhook-secret=X · 不该把 api_key 冲掉。
	var existing vendoraccount.Credential
	if cur, err := store.LoadActive(ctx, vendorID); err == nil && cur != nil {
		existing = *cur
	}

	// 未传 --api-key 且旧的空 → 交互问；旧的有 → 保留
	if apiKey == "" && existing.APIKey == "" {
		fmt.Fprintf(os.Stderr, "输入 %s 的 API key（明文回车提交 · Ctrl-C 取消）：", vendorID)
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("seed-vendor: 读 stdin: %w", err)
		}
		apiKey = strings.TrimRight(line, "\r\n")
	}

	cred := vendoraccount.Credential{
		APIKey:        firstNonEmpty(apiKey, existing.APIKey),
		WebhookSecret: firstNonEmpty(webhookSecret, existing.WebhookSecret),
	}
	if cred.APIKey == "" {
		return errors.New("seed-vendor: API key 不能空（新 seed 至少要给 --api-key）")
	}
	if err := store.Upsert(ctx, vendorID, label, authScheme, cred); err != nil {
		return err
	}

	updated := []string{}
	if apiKey != "" {
		updated = append(updated, "api_key")
	}
	if webhookSecret != "" {
		updated = append(updated, "webhook_secret")
	}
	// 输出**不含明文** · 只报"哪些字段被覆盖"
	fmt.Fprintf(os.Stderr, "OK · %s (label=%s) 已加密写入 vendor_account · 本次更新字段 %v\n",
		vendorID, label, updated)
	return nil
}

// firstNonEmpty · 传参覆盖 · 但空传值保留旧的
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// runListVendors 只列出哪些家已 seed · 不解密 · 输出安全
func runListVendors(ctx context.Context, cfg config.Config) error {
	if cfg.Secrets.MasterKey == "" {
		return errors.New("list-vendors: BP_MASTER_KEY 空")
	}
	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	cipher, err := secrets.New(cfg.Secrets.MasterKey)
	if err != nil {
		return err
	}
	store := vendoraccount.NewStore(database.DB, cipher)
	ids, err := store.ListActiveVendorIDs(ctx)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stderr, "（vendor_account 表空 · 未 seed 任何 vendor · 服务会 fallback 到 env）")
		return nil
	}
	fmt.Fprintln(os.Stderr, "已 seed 的 vendor（明文不显示 · 仅列 slug）：")
	for _, id := range ids {
		fmt.Fprintln(os.Stdout, id)
	}
	return nil
}
