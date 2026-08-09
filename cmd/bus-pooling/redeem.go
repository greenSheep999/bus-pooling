// redeem CLI 子命令 · 批量生成兑换码
//
// 生产管理面板做出来前·运营手工生成一批 CDK 分发到社群·乘客用
// POST /api/me/redeem 消费到账（redeem.Store.Consume 已实现·幂等·事务化）。
//
// 用法：
//
//	bp redeem gen -count 20 -credits 100 -memo 'kiro-bus 社群 8 月券' -prefix KIRO -expires-days 30
//	→ 输出到 stdout 每行一码·配 memo。可用 > codes.txt 存档。
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/redeem"
)

func runRedeem(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法：bp redeem gen -count <n> -credits <微单位> [-memo <str>] [-prefix <str>] [-expires-days <n>]")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "gen":
		return redeemGen(ctx, cfg, rest)
	default:
		return fmt.Errorf("redeem 子命令未知 %q（支持 gen）", sub)
	}
}

func redeemGen(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("bp redeem gen", flag.ContinueOnError)
	count := fs.Int("count", 1, "生成多少条")
	credits := fs.Int64("credits", 0, "每条面值（微单位·1 积分 = 1_000_000）")
	memo := fs.String("memo", "", "备注（可选·会出现在 wallet_ledger.memo · 别写敏感信息）")
	prefix := fs.String("prefix", "KIRO", "码前缀（大写字母数字）· 例：KIRO / GIFT")
	expiresDays := fs.Int("expires-days", 0, "过期天数（0 = 永不过期）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *count <= 0 || *count > 10000 {
		return fmt.Errorf("count 必须 1..10000")
	}
	if *credits <= 0 {
		return fmt.Errorf("credits 必须 > 0（单位微单位·1 积分 = 1_000_000）")
	}
	if strings.TrimSpace(*prefix) == "" {
		*prefix = "KIRO"
	}
	*prefix = strings.ToUpper(strings.ReplaceAll(*prefix, "-", ""))

	// DB 直连 · 不需要拉起完整 server
	d, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer func() { _ = d.Close() }()

	store := redeem.NewStore(d.DB)

	var expiresAt *time.Time
	if *expiresDays > 0 {
		t := time.Now().UTC().AddDate(0, 0, *expiresDays)
		expiresAt = &t
	}

	fmt.Fprintf(os.Stderr, "== 生成 %d 条 · 面值 %d 微单位 · prefix %s · %s 过期 ==\n",
		*count, *credits, *prefix, expiryDesc(expiresAt))

	// stdout 只输出干净的 code · 方便 pipeline 到 CSV / 社群机器人
	success, failed := 0, 0
	for i := 0; i < *count; i++ {
		code := genCode(*prefix)
		if err := store.Seed(ctx, code, *credits, *memo, expiresAt); err != nil {
			fmt.Fprintf(os.Stderr, "!! 第 %d 条失败 (%s)：%v\n", i+1, code, err)
			failed++
			continue
		}
		fmt.Println(code)
		success++
	}
	fmt.Fprintf(os.Stderr, "== 成功 %d · 失败 %d ==\n", success, failed)
	if failed > 0 {
		return fmt.Errorf("%d 条生成失败", failed)
	}
	return nil
}

// genCode 生成 "PREFIX-XXXX-XXXX-XXXX" 格式的一次性码
// hex 12 字节 · 分 3 段 · 大写便于人工输入
func genCode(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	h := strings.ToUpper(hex.EncodeToString(b[:]))
	return fmt.Sprintf("%s-%s-%s-%s", prefix, h[0:4], h[4:8], h[8:12])
}

func expiryDesc(t *time.Time) string {
	if t == nil {
		return "永不"
	}
	return t.Format("2006-01-02")
}
