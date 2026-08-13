// vendor-probe · 只读探测一个 vendor 端点 · 打印真实响应（脱敏）。
//
// **为什么有这条命令**（institutionalize "别猜形状" 的纪律）：
// §19.3 那批"高价值未接"端点 · 档案只有字段名猜测 · 没实测 JSON。照猜写 adapter
// 是本项目反复踩的坑（某家 webhook 100% 丢 / ledger 形状不明）。写 adapter 前
// **先用这条抓到真实响应** · 对着真形状写 · 不对着文档猜。
//
// **只读铁律**：只发 GET · 只准打**明确无副作用**的端点（ledger / stock/rounds /
// key-price-tiers / credits 这类报价+流水）。**严禁**打 purchase / claim / reservation
// 等可能占库存 / 扣费的端点 —— 名字带 reservation 的先人工确认语义再说。
//
// 用法（本地 · 先 source .dev.env 拿 key）：
//   set -a; . ./.dev.env; set +a
//   go run ./cmd/bus-pooling vendor-probe <slug> <path>
//
// 输出：HTTP 状态 + 脱敏后的 body（key/token 前缀一律打码）。

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/config"
)

// vendorProbeAuth · 各家鉴权方式（跟 adapter 的 newReq 保持一致）
var vendorProbeAuth = map[string]struct {
	baseURL func(config.Config) string
	keyEnv  string
	bearer  bool // true=Authorization Bearer · false=X-API-Key
}{
	"kiro91":    {func(c config.Config) string { return c.Vendors.Kiro91.BaseURL }, "BP_VENDOR_KIRO91_API_KEY", false},
	"kiroceo":   {func(c config.Config) string { return c.Vendors.KiroCEO.BaseURL }, "BP_VENDOR_KIROCEO_API_KEY", false},
	"kirooo":    {func(c config.Config) string { return c.Vendors.KiroOOO.BaseURL }, "BP_VENDOR_KIROOOO_API_KEY", false},
	"kiroappio": {func(c config.Config) string { return c.Vendors.KiroAppIO.BaseURL }, "BP_VENDOR_KIROAPPIO_API_KEY", false},
	"kiroappcc": {func(c config.Config) string { return c.Vendors.KiroAppCC.BaseURL }, "BP_VENDOR_KIROAPPCC_API_KEY", true},
	"kirodrop":  {func(c config.Config) string { return c.Vendors.KiroDrop.BaseURL }, "BP_VENDOR_KIRODROP_API_KEY", false},
}

// 危险端点关键词 · 命中直接拒（防手滑占库存/扣费）
var probeDangerWords = []string{"purchase", "claim", "redeem", "reservation", "order", "buy"}

func runVendorProbe(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("用法：vendor-probe <slug> <path>（只读 GET · path 例 /api/my/ledger）")
	}
	slug, path := args[0], args[1]
	auth, ok := vendorProbeAuth[slug]
	if !ok {
		return fmt.Errorf("未知 vendor slug %q · 支持：%s", slug, strings.Join(probeSlugs(), " "))
	}
	// 只读闸：拒危险端点（除非显式 --force · 但即便 force 也只发 GET）
	lower := strings.ToLower(path)
	for _, w := range probeDangerWords {
		if strings.Contains(lower, w) && !hasFlag(args, "--force") {
			return fmt.Errorf("端点含危险词 %q · 可能占库存/扣费 · 确认无副作用后加 --force（本命令只发 GET · 但仍需你判断语义）", w)
		}
	}

	base := auth.baseURL(cfg)
	if base == "" {
		return fmt.Errorf("vendor %s base_url 空 · 检查 config.yaml", slug)
	}
	key := os.Getenv(auth.keyEnv)
	if key == "" {
		return fmt.Errorf("%s 为空 · 先 source .dev.env（set -a; . ./.dev.env; set +a）", auth.keyEnv)
	}

	url := strings.TrimRight(base, "/") + path
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if auth.bearer {
		req.Header.Set("Authorization", "Bearer "+key)
	} else {
		req.Header.Set("X-API-Key", key)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 8192)
	for {
		n, rerr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if rerr != nil || len(buf) > 512*1024 {
			break
		}
	}

	fmt.Fprintf(os.Stderr, "GET %s\nHTTP %d · %d bytes\n\n", url, resp.StatusCode, len(buf))
	fmt.Println(maskSecrets(string(buf)))
	return nil
}

// probeSlugs · 支持的 slug 列表 · 从鉴权表动态取（不硬编码进字符串 · 过 lint）
func probeSlugs() []string {
	out := make([]string, 0, len(vendorProbeAuth))
	for s := range vendorProbeAuth {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// maskSecrets · 把明文 key / token 打码（§8.1 · 绝不让密钥落终端日志/文件）。
// 覆盖常见前缀：sk- / usr- / ksk- / Bearer · 以及长 hex/base64 串。
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(sk|usr|ksk|key|tok|Bearer)[-_ ][A-Za-z0-9_\-]{8,}`),
	regexp.MustCompile(`"(key|api_key|token|secret|password)"\s*:\s*"[^"]{6,}"`),
}

func maskSecrets(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "<redacted>")
	}
	return s
}
