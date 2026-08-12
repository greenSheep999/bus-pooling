package providers

import "strings"

// ZoneOf · 把任意 region / zone 字符串归一到 Zone 枚举（docs/16 缺口 5 · 抢号链 region 命名口径）
//
// **背景**：三处对 region 用了三套字面量：
//   - enqueue 时 · decider 传 in.Zone（"us" / "eu"）
//   - webhook 收到时 · vendor 发的 zone 字段（通常也是 "us"）
//   - 探针 delta 时 · vendor 的 region 名（"us-east-1" / "eu-central-1" / "us-east-1-dryrun"）
//
// stock_watcher.region 语义定死为 zone 名（"us" / "eu"）· Notify 前用本函数归一 · 一致 SQL 匹配。
//
// 匹配规则（case-insensitive）：
//   包含 "us" / "美"        → ZoneUS
//   包含 "eu" / "欧"        → ZoneEU
//   其他                    → Zone("") 空 · 让 SQL "region IS NULL OR region=''" 分支放行
func ZoneOf(region string) Zone {
	r := strings.ToLower(strings.TrimSpace(region))
	if r == "" {
		return ""
	}
	if strings.Contains(r, "us") || strings.Contains(r, "美") {
		return ZoneUS
	}
	if strings.Contains(r, "eu") || strings.Contains(r, "欧") {
		return ZoneEU
	}
	return ""
}
