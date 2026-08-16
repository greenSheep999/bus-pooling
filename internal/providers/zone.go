package providers

import "strings"

// ZoneOf · 把任意 region / zone 字符串归一到 Zone 枚举（docs/22-buy-race 缺口 5 · 抢号链 region 命名口径）
//
// **背景**：三处对 region 用了三套字面量：
//   - enqueue 时 · decider 传 in.Zone（"us" / "eu"）
//   - webhook 收到时 · vendor 发的 zone 字段（通常也是 "us"）
//   - 探针 delta 时 · vendor 的 region 名（"us-east-1" / "eu-central-1" / "us-east-1-dryrun"）
//
// stock_watcher.region 语义定死为 zone 名（"us" / "eu"）· Notify 前用本函数归一 · 一致 SQL 匹配。
//
// 匹配规则（case-insensitive）：
//
//	"general"              → ZoneGeneral（不分区 vendor 的唯一一区 · 原样保留）
//	包含 "us" / "美"        → ZoneUS
//	包含 "eu" / "欧"        → ZoneEU
//	其他                    → Zone("") 空 · 让 SQL "region IS NULL OR region=''" 分支放行
//
// **为什么 general 要单独判**（2026-08-13 修）：不分区的 vendor 用 ZoneGeneral 当
// 唯一一区。若归一成空串 · 它的侧表行 zone 列就是空 · PricedFor 按 zone 查匹配不到 ·
// 等于这家 vendor 在定价链上"不存在"。
func ZoneOf(region string) Zone {
	r := strings.ToLower(strings.TrimSpace(region))
	if r == "" {
		return ""
	}
	if r == string(ZoneGeneral) {
		return ZoneGeneral
	}
	if strings.Contains(r, "us") || strings.Contains(r, "美") {
		return ZoneUS
	}
	if strings.Contains(r, "eu") || strings.Contains(r, "欧") {
		return ZoneEU
	}
	return ""
}
