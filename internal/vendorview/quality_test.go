package vendorview

import (
	"testing"
	"time"
)

// TestComputeQuality_RealShape · 用生产实况数据形状验证排序 + 标签
//
// 5 种典型档位（数据来自 2026-08-11 生产快照 · 匿名）：
//
//	A · 98% uptime · 42 批 · 最新 17h 前 · stock=out（高产稳定）
//	B · 16% uptime · 34 批 · 最新 5min 前 · stock=out（高产但极不稳）
//	C · 99% uptime · 19 批 · 最新 5h 前 · stock=out（稳定中产）
//	D · 99% uptime · 0 批 · 最新 10 天前 · stock=out（稳但长期没发货）
//	E · 99% uptime · 无 dispatch 数据（只有 uptime）
//
// 用户直觉排序：A > C > B > D/E
func TestComputeQuality_RealShape(t *testing.T) {
	now := time.Date(2026, 8, 11, 8, 30, 0, 0, time.UTC)
	pct := func(n int) *int { return &n }

	cases := []struct {
		name          string
		in            qualityInput
		wantTagsAny   []string
		wantScoreOver int
	}{
		{
			"A · 高产稳定 · 但最新 17h 前",
			qualityInput{
				alive: true, uptime24hPct: pct(98), stockBucket: "out",
				hasWarranty: true, dispatchBatches: 42,
				lastDispatch:   now.Add(-17 * time.Hour),
				dataSufficient: true, now: now,
			},
			[]string{"stable", "high-volume", "active", "warranty"},
			60,
		},
		{
			"B · 高产活跃但不稳",
			qualityInput{
				alive: true, uptime24hPct: pct(16), stockBucket: "out",
				hasWarranty: true, dispatchBatches: 34,
				lastDispatch:   now.Add(-5 * time.Minute),
				dataSufficient: true, now: now,
			},
			[]string{"high-volume", "active", "warranty", "watching"},
			30,
		},
		{
			"C · 稳定中产 · 最新 5h 前",
			qualityInput{
				alive: true, uptime24hPct: pct(99), stockBucket: "out",
				hasWarranty: true, dispatchBatches: 19,
				lastDispatch:   now.Add(-5 * time.Hour),
				dataSufficient: true, now: now,
			},
			[]string{"stable", "active", "warranty"},
			40,
		},
		{
			"D · 稳但 10 天前没发货了",
			qualityInput{
				alive: true, uptime24hPct: pct(99), stockBucket: "out",
				hasWarranty: true, dispatchBatches: 0,
				lastDispatch:   now.Add(-10 * 24 * time.Hour),
				dataSufficient: true, now: now,
			},
			[]string{"stable", "warranty"},
			15,
		},
		{
			"E · 无 dispatch 数据 · 只看 uptime",
			qualityInput{
				alive: true, uptime24hPct: pct(99), stockBucket: "out",
				hasWarranty: true, dispatchBatches: 0,
				dataSufficient: true, now: now,
			},
			[]string{"stable", "warranty"},
			15,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := computeQuality(c.in)
			t.Logf("Score=%d Tags=%v", q.Score, q.Tags)
			if q.Score < c.wantScoreOver {
				t.Errorf("Score %d < %d 期望", q.Score, c.wantScoreOver)
			}
			got := map[string]bool{}
			for _, tag := range q.Tags {
				got[tag.Kind] = true
			}
			for _, want := range c.wantTagsAny {
				if !got[want] {
					t.Errorf("缺标签 %s · 实得 %v", want, q.Tags)
				}
			}
		})
	}
}

// TestComputeQuality_LeadsBoard · A 家（高产稳定）综合分应高于其他家 · 用户核心诉求
func TestComputeQuality_LeadsBoard(t *testing.T) {
	now := time.Date(2026, 8, 11, 8, 30, 0, 0, time.UTC)
	pct := func(n int) *int { return &n }

	// A · 高产稳定
	a := computeQuality(qualityInput{
		alive: true, uptime24hPct: pct(98), stockBucket: "out",
		hasWarranty: true, dispatchBatches: 42,
		lastDispatch:   now.Add(-17 * time.Hour),
		dataSufficient: true, now: now,
	})
	// B · 高产不稳
	b := computeQuality(qualityInput{
		alive: true, uptime24hPct: pct(16), stockBucket: "out",
		hasWarranty: true, dispatchBatches: 34,
		lastDispatch:   now.Add(-5 * time.Minute),
		dataSufficient: true, now: now,
	})
	// C · 稳定中产
	c := computeQuality(qualityInput{
		alive: true, uptime24hPct: pct(99), stockBucket: "out",
		hasWarranty: true, dispatchBatches: 19,
		lastDispatch:   now.Add(-5 * time.Hour),
		dataSufficient: true, now: now,
	})
	// D · 稳但长期没发货
	d := computeQuality(qualityInput{
		alive: true, uptime24hPct: pct(99), stockBucket: "out",
		hasWarranty: true, dispatchBatches: 0,
		lastDispatch:   now.Add(-10 * 24 * time.Hour),
		dataSufficient: true, now: now,
	})

	t.Logf("A=%d B=%d C=%d D=%d", a.Score, b.Score, c.Score, d.Score)

	if a.Score <= b.Score {
		t.Errorf("A (%d) 应 > B (%d) · 用户直觉排序破裂", a.Score, b.Score)
	}
	if a.Score <= c.Score {
		t.Errorf("A (%d) 应 > C (%d)", a.Score, c.Score)
	}
	if a.Score <= d.Score {
		t.Errorf("A (%d) 应 > D (%d)", a.Score, d.Score)
	}
}

// TestComputeQuality_DataInsufficient · 数据不足 · 只挂 watching · 不给正向暗示
func TestComputeQuality_DataInsufficient(t *testing.T) {
	q := computeQuality(qualityInput{alive: true, dataSufficient: false})
	if q.Score != 0 {
		t.Errorf("数据不足应 Score=0 · 得 %d", q.Score)
	}
	if len(q.Tags) != 1 || q.Tags[0].Kind != "watching" {
		t.Errorf("数据不足应只挂 watching · 得 %v", q.Tags)
	}
}

// TestComputeQuality_Dead · alive=false · 也走 watching
func TestComputeQuality_Dead(t *testing.T) {
	pct := func(n int) *int { return &n }
	q := computeQuality(qualityInput{
		alive: false, uptime24hPct: pct(99), dataSufficient: true,
	})
	if len(q.Tags) != 1 || q.Tags[0].Kind != "watching" {
		t.Errorf("挂了的应只有 watching · 得 %v", q.Tags)
	}
}
