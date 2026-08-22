package api

import (
	"database/sql"
	"time"
)

// lifespanOf 号活了多久（秒）· 死号算到 dead_at · 活号算到现在
//
// 两个端点（车内号列表 / 派发历史）都要这个数 · 抽出来免得算法漂移。
// 解析失败或时长为负一律返 0（前端显示 "-"）· 不要猜。
func lifespanOf(pulledAt string, deadAt sql.NullString) int64 {
	if pulledAt == "" {
		return 0
	}
	t0, err := time.Parse(time.RFC3339, pulledAt)
	if err != nil {
		return 0
	}
	end := time.Now().UTC()
	if deadAt.Valid && deadAt.String != "" {
		if t1, err := time.Parse(time.RFC3339, deadAt.String); err == nil {
			end = t1
		}
	}
	if d := end.Sub(t0); d > 0 {
		return int64(d.Seconds())
	}
	return 0
}

// lifespanAt 到指定时刻为止的存活秒数（"派发那一刻的寿命快照"语义）。
//
// 跟 lifespanOf 的区别：end 不取 now 而取 atRFC3339（如派发时间）——
// 派发历史的 "Alive at dispatch" 列要的是**派发时**号活了多久 · 用 now 会让
// 这个数随时间自己长 · 跟列名语义打架。号在 at 之前就死了则截到 dead_at。
func lifespanAt(pulledAt string, deadAt sql.NullString, atRFC3339 string) int64 {
	if pulledAt == "" {
		return 0
	}
	t0, err := time.Parse(time.RFC3339, pulledAt)
	if err != nil {
		return 0
	}
	end, err := time.Parse(time.RFC3339, atRFC3339)
	if err != nil {
		// 派发时间解析不了退回"现在"（跟 lifespanOf 同兜底 · 不静默归零）
		end = time.Now().UTC()
	}
	if deadAt.Valid && deadAt.String != "" {
		if t1, err := time.Parse(time.RFC3339, deadAt.String); err == nil && t1.Before(end) {
			end = t1
		}
	}
	if d := end.Sub(t0); d > 0 {
		return int64(d.Seconds())
	}
	return 0
}
