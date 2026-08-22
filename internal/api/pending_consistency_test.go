package api

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// 「待派」口径必须三处一致 —— 否则 Overview 卡片数字跟点进去的列表条数对不上
// （车主 2026-08-17 报的 bug：卡片说 1 个 · 列表列 2 条）。
//
// 三处:
//   - internal/insight/overview.go  · Overview「提取 key」卡片的 pending 桶
//   - internal/api/pullrecord.go    · 提取页待派列表
//   - internal/api/events.go        · 提取历史每轮的 pending_count
//
// 判据（三处都必须满足 · 且**都不能排已推池**）:
//   owner_record_passenger_id 有值  AND  owner_bus_id IS NULL  AND  status != 'handed_off'
//
// 为什么已推池仍算待派:push_pool 是双写（docs/15 §14.3）· housepool 副本还在 ·
// 号还属于这个乘客 · 还能再派去别处。
//
// **这是静态源码检查** —— 不查运行时行为 · 只保证三处 SQL 的过滤条件没跑偏。
// 改了任一处的口径 · 这条测试会红 · 提醒你同步另两处。
func TestPendingCriteriaConsistentAcrossEndpoints(t *testing.T) {
	files := map[string]string{
		"insight/overview.go": "../insight/overview.go",
		"api/pullrecord.go":   "pullrecord.go",
		"api/events.go":       "events.go",
	}

	// 已推池**不该**出现在待派判据里
	pushedFilter := regexp.MustCompile(`pushed_to_passengerpool_at IS NULL`)

	for name, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		text := string(src)

		// 抽出"待派"相关的那段（各文件标记不同 · 用共同的三个条件定位）
		if !strings.Contains(text, "owner_record_passenger_id") {
			t.Errorf("%s: 找不到 owner_record_passenger_id —— 待派判据是不是被改没了?", name)
			continue
		}
		if !strings.Contains(text, "owner_bus_id IS NULL") {
			t.Errorf("%s: 待派判据缺 owner_bus_id IS NULL", name)
		}
		if !strings.Contains(text, "handed_off") {
			t.Errorf("%s: 待派判据缺 status != 'handed_off'", name)
		}

		// 关键:待派那一段不许用"未推池"当条件
		//
		// 只看**待派判据本身**（owner_record_passenger_id 那行往后到该条件块结束）——
		// 不能整段扫:同一个 SQL 里 into_bus 桶用 `pushed_to_passengerpool_at IS NULL`
		// 是**对的**（保 4 桶互斥）· 扫太宽会误伤它。
		for _, block := range pendingBlocks(text) {
			if pushedFilter.MatchString(block) {
				t.Errorf("%s: 待派判据里出现 `pushed_to_passengerpool_at IS NULL` ——\n"+
					"已推池的号仍算待派（push_pool 是双写 · docs/15 §14.3）· "+
					"加了这条会让本处数字比另两处少 · 卡片跟列表对不上。\n片段:\n%s",
					name, strings.TrimSpace(block))
			}
		}
	}
}

// pendingBlocks 抽"待派判据"那一段:从含 owner_record_passenger_id 的行开始 ·
// 到该条件块结束（遇到 THEN / AS xxx_count / 空行 / 下一个 WHEN 就停）。
//
// **不整段扫** —— 同一个 SQL 里别的桶（into_bus）用"未推池"是对的 · 扫宽了会误报。
func pendingBlocks(text string) []string {
	lines := strings.Split(text, "\n")
	var out []string
	for i, l := range lines {
		if !strings.Contains(l, "owner_record_passenger_id") {
			continue
		}
		var b []string
		b = append(b, l)
		for j := i + 1; j < len(lines) && j < i+8; j++ {
			nxt := lines[j]
			// 条件块边界
			if strings.Contains(nxt, "THEN") || strings.Contains(nxt, "_count") ||
				strings.Contains(nxt, "WHEN ") || strings.TrimSpace(nxt) == "" {
				b = append(b, nxt)
				break
			}
			b = append(b, nxt)
		}
		out = append(out, strings.Join(b, "\n"))
	}
	return out
}
