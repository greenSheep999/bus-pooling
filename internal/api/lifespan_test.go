package api

import (
	"database/sql"
	"testing"
	"time"
)

func TestLifespanOf(t *testing.T) {
	now := time.Now().UTC()
	pulled := now.Add(-2 * time.Hour).Format(time.RFC3339)
	dead := now.Add(-30 * time.Minute).Format(time.RFC3339)

	t.Run("死号算到 dead_at", func(t *testing.T) {
		got := lifespanOf(pulled, sql.NullString{String: dead, Valid: true})
		// 2h - 30min = 1.5h = 5400s（容 2s 抖动）
		if got < 5398 || got > 5402 {
			t.Errorf("got %d · 期望 ≈5400", got)
		}
	})

	t.Run("活号算到现在", func(t *testing.T) {
		got := lifespanOf(pulled, sql.NullString{})
		if got < 7198 || got > 7202 {
			t.Errorf("got %d · 期望 ≈7200", got)
		}
	})

	t.Run("pulled_at 空返 0", func(t *testing.T) {
		if got := lifespanOf("", sql.NullString{}); got != 0 {
			t.Errorf("got %d · 期望 0", got)
		}
	})

	t.Run("时间格式坏返 0 而不是猜", func(t *testing.T) {
		if got := lifespanOf("not-a-time", sql.NullString{}); got != 0 {
			t.Errorf("got %d · 期望 0", got)
		}
	})

	t.Run("dead_at 早于 pulled_at 返 0 不返负", func(t *testing.T) {
		early := now.Add(-3 * time.Hour).Format(time.RFC3339)
		if got := lifespanOf(pulled, sql.NullString{String: early, Valid: true}); got != 0 {
			t.Errorf("got %d · 期望 0（负时长不该外泄）", got)
		}
	})
}
