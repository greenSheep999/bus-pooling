package vendorview

import (
	"context"
	"database/sql"
	"time"
)

// DerivedDispatchSummary 从 vendor_probe 里推出来的"合成"发货节奏。
//
// 用途：3 家 vendor（多家 vendor）上游没暴露 fleet-wide
// gen-logs / orders 端点（穷举过所有 /api/*、/api/my/*、/api/public/* 无果），
// FleetLister 抓不到数据。但我方每 60s 打一次探针，PS 字段（keys_active +
// keys_dead + keys_stock 或 stock_total）会随 vendor 持续发号累计上涨。
//
// 相邻两次探针的**正向增量**就是一次 dispatch batch 的近似值：
//   - 增量 = 这两个探针之间上游发出的新号数（净观测值 · 忽略在窗口内立即挂掉的号）
//   - 时间 = 后一次探针时刻（±probe interval 的采样误差）
//
// 精度不如 vendor 官方 gen-logs（会漏掉"发出后又快速挂完"的 batch），
// 但比"数据采集中"占位强得多——上线 24h 就能画出真实节奏曲线。
type DerivedDispatchSummary struct {
	TotalBatches        int
	TotalKeysDispatched int
	AvgIntervalMin      float64
	LastDispatchAt      time.Time
}

// DeriveDispatchSummary 扫最近 windowHours 内的探针 · 相邻正增量推 batch。
//
// 信号源优先级：
//  1. PS 字段总和（ps_keys_active + ps_keys_dead + ps_keys_stock） · 最准 ·
//     多家 vendor 有；本 vendor 我们已经有 FleetLister 数据，走不到这里。
//  2. stock_total（我方账户视角） · 弱信号 · 只反映"我方能买到几个"·
//     vendor 有配额时会低估——但至少能捕获库存补货动作。
//
// windowHours <= 0 时默认 168（7 天） · 首日上线用大窗口把冷启动 24h 前的数据也吃进。
func (s *ProbeStore) DeriveDispatchSummary(
	ctx context.Context, vendorID string, windowHours int,
) (*DerivedDispatchSummary, error) {
	if s.db == nil {
		return &DerivedDispatchSummary{}, nil
	}
	if windowHours <= 0 {
		windowHours = 24 * 7
	}
	cutoff := time.Now().UTC().Add(-time.Duration(windowHours) * time.Hour).Format(time.RFC3339Nano)

	// 用 COALESCE 把 NULL 归零 · 优先取 PS 双项和（active + dead 是"曾生成的所有 key" ·
	// vendor 通常同时更新这两个 · 单独 stock 波动是随卖随空 · 不是 dispatch 信号）·
	// fallback stock_total。排除 alive=0 的样本（vendor 不通时 PS 也不可信）。
	//
	// has_ps 判定：ps_keys_active 或 ps_keys_dead 至少一个非空才当有 PS 数据 ·
	// 单独 ps_keys_stock 变化不可靠（时段性库存 · 会随卖随空反弹）。
	rows, err := s.db.QueryContext(ctx, `
		SELECT probed_at,
		       COALESCE(ps_keys_active, 0) + COALESCE(ps_keys_dead, 0) AS ps_sum,
		       COALESCE(stock_total, 0)                                AS stock,
		       CASE WHEN ps_keys_active IS NOT NULL OR ps_keys_dead IS NOT NULL
		            THEN 1 ELSE 0 END                                  AS has_ps
		  FROM vendor_probe
		 WHERE vendor_id = ? AND probed_at >= ? AND alive = 1
		 ORDER BY probed_at ASC
	`, vendorID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 状态机：跟踪上一采样的信号值 · 差 > 0 = 一次 dispatch。
	// 信号在中途"重置"（例如 vendor 清 dead 或平台重启导致 ps_sum 归零）时 · 忽略负差 ·
	// 用当前值当新基线继续跟。
	var (
		hasPrev       bool
		prevSignal    int
		prevBatchTime time.Time

		out          = &DerivedDispatchSummary{}
		gapTotalSec  float64
		gapCount     int
	)

	for rows.Next() {
		var (
			probedAt string
			psSum    int
			stock    int
			hasPS    int
		)
		if err := rows.Scan(&probedAt, &psSum, &stock, &hasPS); err != nil {
			return nil, err
		}

		// 信号选择：本采样能拿到 PS 就用 PS · 否则用 stock（弱信号）
		signal := stock
		if hasPS == 1 {
			signal = psSum
		}

		if !hasPrev {
			prevSignal = signal
			hasPrev = true
			continue
		}

		delta := signal - prevSignal
		if delta > 0 {
			t, _ := time.Parse(time.RFC3339Nano, probedAt)
			out.TotalBatches++
			out.TotalKeysDispatched += delta
			if !prevBatchTime.IsZero() {
				gap := t.Sub(prevBatchTime).Seconds()
				if gap > 0 {
					gapTotalSec += gap
					gapCount++
				}
			}
			prevBatchTime = t
			if t.After(out.LastDispatchAt) {
				out.LastDispatchAt = t
			}
		}
		// 无论正负都更新基线（负数视为 vendor 侧回收/重置 · 不当 batch 记）
		prevSignal = signal
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if gapCount > 0 {
		out.AvgIntervalMin = gapTotalSec / float64(gapCount) / 60
	}
	return out, nil
}

// nilIfEmpty 用于 status_view 判断是否要嵌 DispatchOut · 空返 nil
func (d *DerivedDispatchSummary) IsEmpty() bool {
	return d == nil || d.TotalBatches == 0
}

// DeriveDispatchEvents 跟 DeriveDispatchSummary 同一套增量算法 · 但返**逐条事件**
// 而不是汇总 —— 让没有 fleet 端点的 vendor 也能提供跟 vendor_dispatch 表**同形状**
// 的事件流（对外契约统一 · 前端只画一种图 · 见 status_view.DispatchEvent）。
//
// 每条事件 = 一次观测到的正向增量：
//   - At    = 后一次探针的时刻（±probe interval 采样误差）
//   - Count = 增量值（这段时间新出现的号数）
//
// 精度说明：探针间隔内"发出又挂完"的批次会被漏掉 · 增量也可能把两批合成一批。
// 所以这条路径产出的事件标 Derived=true · 前端会注明"观测推算"。
func (s *ProbeStore) DeriveDispatchEvents(
	ctx context.Context, vendorID string, windowHours, limit int,
) ([]DerivedEvent, error) {
	if s.db == nil {
		return nil, nil
	}
	if windowHours <= 0 {
		windowHours = 24 * 7
	}
	cutoff := time.Now().UTC().Add(-time.Duration(windowHours) * time.Hour).Format(time.RFC3339Nano)

	rows, err := s.db.QueryContext(ctx, `
		SELECT probed_at,
		       COALESCE(ps_keys_active, 0) + COALESCE(ps_keys_dead, 0) AS ps_sum,
		       COALESCE(stock_total, 0)                                AS stock,
		       CASE WHEN ps_keys_active IS NOT NULL OR ps_keys_dead IS NOT NULL
		            THEN 1 ELSE 0 END                                  AS has_ps
		  FROM vendor_probe
		 WHERE vendor_id = ? AND probed_at >= ? AND alive = 1
		 ORDER BY probed_at ASC
	`, vendorID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var (
		hasPrev    bool
		prevSignal int
		out        []DerivedEvent
	)
	for rows.Next() {
		var (
			probedAt string
			psSum    int
			stock    int
			hasPS    int
		)
		if err := rows.Scan(&probedAt, &psSum, &stock, &hasPS); err != nil {
			return nil, err
		}
		signal := stock
		if hasPS == 1 {
			signal = psSum
		}
		if !hasPrev {
			prevSignal = signal
			hasPrev = true
			continue
		}
		if delta := signal - prevSignal; delta > 0 {
			t, _ := time.Parse(time.RFC3339Nano, probedAt)
			out = append(out, DerivedEvent{At: t, Count: delta})
		}
		prevSignal = signal
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 倒序（最新在前）· 跟 vendor_dispatch 查询顺序一致
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// DerivedEvent 探针推出来的一次开号观测 · 只有时间和数量
// （alive/dead/region 探针拿不到 · status_view 转换时留空）。
type DerivedEvent struct {
	At    time.Time
	Count int
}

// 类型断言防止 database/sql 未使用告警
var _ = sql.ErrNoRows
