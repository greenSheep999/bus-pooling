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
