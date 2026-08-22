package vendorview

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// StatusOverview 是 GET /api/vendors/status 的响应形状 · **公开端点**（不要求登录）。
//
// **强制匿名**：`vendor.DisplayName()` 的真名一律不出。用 `anonLabelOf`（"AWS-Q Kiro
// Vendor 01" 等编号）+ `anonIDOf`（稳定 6 位 hash）。真名只在**登录后**的 /stock
// / /prices 端点通过 `viewerOf(p, r).Invited==true` 才能看到（decisions §10.4）。
//
// 不出：单价 / 具体寿命秒数 / 内部 vendor_id / balance / DisplayName 真名。
//
// 字段选择遵循"档位化脱敏"：
//   - stock_bucket → many / low / out（不给具体数字·避免竞品拿去做实时供应链情报）
//   - lifespan_bucket → long / mid / short / unknown（同上）
//   - uptime_24h_pct / stockout_24h_minutes → 数字可给（是产品可靠性承诺）
type StatusOverview struct {
	// ProbedAt 最后一次探测时间 · 让访客知道数据新鲜度
	ProbedAt string            `json:"probed_at,omitempty"`
	Vendors  []VendorStatusRow `json:"vendors"`
}

// VendorStatusRow 一家 vendor 的对外脱敏状态行 · 永远匿名。
type VendorStatusRow struct {
	// AnonID 稳定的 6 位短 hash（vendor_id → sha256 → 前 6 位）
	// 前端拿它做 key + 拉这家的 trend · 关联但不泄露内部 id
	AnonID string `json:"anon_id"`
	// AnonLabel "AWS-Q Kiro Vendor 01" 等编号 · 不出真品牌名
	AnonLabel string `json:"anon_label"`

	// PublicStatus 出 vendor 自报的 fleet 累计数据（keys_active / keys_dead 等）
	// 支持的 vendor（多家 vendor）才有 · 其他 vendor 这个字段 nil
	PublicStatus *PublicStatusOut `json:"public_status,omitempty"`

	// Alive 最后一次探测是否活着（vendor.Stock 返回没报错即活）
	Alive bool `json:"alive"`
	// ErrorKind alive=false 时给个短标签（timeout / auth / http_5xx / http_4xx / other）
	ErrorKind string `json:"error_kind,omitempty"`

	// StockBucket 库存档位 · many (>=20) / low (1-19) / out (0) / unknown（探不到时）
	StockBucket string `json:"stock_bucket"`
	// RegionCount 覆盖区域数（比如 "3 区可用"）· 不列具体区名
	RegionCount int `json:"region_count"`

	// HasWarranty 是否提供质保（产品卖点·可以露）
	HasWarranty bool `json:"has_warranty"`
	// WarrantyMinutes 质保时长（15 分钟这种·可以露）
	WarrantyMinutes int `json:"warranty_minutes,omitempty"`
	// MaxPerOrder 单次最多购买数（产品可预期性·可以露）
	MaxPerOrder int `json:"max_per_order,omitempty"`

	// Uptime24hPct 过去 24h 存活百分比（0-100 整数）· 探测样本 < 10 时给 nil
	Uptime24hPct *int `json:"uptime_24h_pct,omitempty"`
	// Stockout24hMinutes 过去 24h 库存为 0 的累计分钟数
	Stockout24hMinutes int `json:"stockout_24h_minutes,omitempty"`

	// LifespanBucket 平均寿命档位（未来接入 KeyHealth 探测后填）
	// long (>1h) / mid (10min-1h) / short (<10min) / unknown
	LifespanBucket string `json:"lifespan_bucket,omitempty"`

	// Incidents7d 过去 7 天的 incident 日期列表（YYYY-MM-DD）· 空 = 无事故
	Incidents7d []string `json:"incidents_7d,omitempty"`

	// History 从 vendor 侧 backfill 的真实历史（订单数 · key 数 · 平均寿命）
	// vendor 支持 OrderHistoryLister/KeyHistoryLister 才有 · 否则 nil
	History *HistoryOut `json:"history,omitempty"`

	// Dispatch vendor **平台全网**最近开号节奏（fleet-wide）· 6 家只要有 FleetLister 都能填
	// 这是每张卡上"过去 X 时间 vendor 发了多少批 · 平均多久一批 · 累计多少 key"的硬数据
	Dispatch *DispatchOut `json:"dispatch,omitempty"`

	// Quality 综合质量 · 排序 + 标签（Score 不出 · Tags 出）· 见 quality.go
	Quality VendorQuality `json:"quality"`
}

// DispatchOut vendor 平台的发货节奏 · 上线一秒到手
type DispatchOut struct {
	// TotalBatches vendor 在表里的总批次数（我方从上线开始积累 · 上线越久越多）
	TotalBatches int `json:"total_batches"`
	// TotalKeysDispatched 累计发过多少个 key
	TotalKeysDispatched int `json:"total_keys_dispatched"`
	// AvgIntervalMin 平均每批间隔分钟数（<=0 = 数据不足）
	AvgIntervalMin float64 `json:"avg_interval_min,omitempty"`
	// LastDispatchAt 最新一批时间 · RFC3339 UTC
	LastDispatchAt string `json:"last_dispatch_at,omitempty"`
}

// HistoryOut 从 vendor_order + vendor_key 表汇总的真实历史（脱敏 · 无价格）。
// 这些是 vendor 侧**已经存在**的历史 · 我方 backfill 拉过来复用 · 上线一秒就能显示。
type HistoryOut struct {
	TotalOrders int `json:"total_orders"`
	TotalKeys   int `json:"total_keys"`
	ActiveKeys  int `json:"active_keys"`
	DeadKeys    int `json:"dead_keys"`
	// AvgLifespanSec 平均寿命秒数 · 前端要不要展示具体数字自己判断（也能按档位化）
	AvgLifespanSec int64 `json:"avg_lifespan_sec,omitempty"`
	// FirstOrderAt / LastOrderAt · vendor 侧第一单 / 最新一单时间
	FirstOrderAt string `json:"first_order_at,omitempty"`
	LastOrderAt  string `json:"last_order_at,omitempty"`
}

// PublicStatusOut vendor 自报的 fleet 累计数据 · 已脱敏（无价格）· 内嵌到 Row。
//
// keys_active / keys_dead / keys_total 是 vendor 侧**平台历史累计**，比我方 60s
// 探测积累的数据丰富得多 · 上线首日就能看到"这家 vendor 卖过多少号 / 多少还活着"。
//
// 未来可以在 status_view 里做 rolling 数值对比：keys_active 昨天 vs 今天 = 净增。
type PublicStatusOut struct {
	KeysActive    *int   `json:"keys_active,omitempty"`
	KeysAlive     *int   `json:"keys_alive,omitempty"`
	KeysDead      *int   `json:"keys_dead,omitempty"`
	KeysStock     *int   `json:"keys_stock,omitempty"`
	KeysSuspect   *int   `json:"keys_suspect,omitempty"`
	KeysTotal     *int   `json:"keys_total,omitempty"`
	Generating    *bool  `json:"generating,omitempty"`
	UptimeSeconds *int64 `json:"uptime_seconds,omitempty"`
	// StartedAt vendor 平台的启动时间（RFC3339 UTC）· 用于前端显示"运行 X 天"
	StartedAt string `json:"started_at,omitempty"`
}

// StatusOverview 汇聚所有 enabled vendor 的公开状态。
// 数据来源：
//   - 最近一次 vendor_probe（当前是否活、库存、质保）
//   - 24h 聚合（vendor_probe 过滤 last-24h · 存活率 + stockout 分钟）
//   - 7d incident 日期（vendor_daily.incident_flag=1）
//   - windowHours 窗口的 dispatch 批次（决定 Volume/Freshness 标签评估窗口）
//
// windowHours <=0 默认 168（7d）· 上限 720（30d）· 保持跟 DispatchEvents 一致。
//
// 探针未启动 / db 为 nil 时返回空 · 前端展示"数据采集中"占位。
func (s *Service) StatusOverview(ctx context.Context, windowHours int) StatusOverview {
	if s.probeStore == nil || s.registry == nil {
		return StatusOverview{}
	}
	if windowHours <= 0 {
		windowHours = 168
	}
	if windowHours > 720 {
		windowHours = 720
	}

	entries := s.registry.Enabled()
	rows := make([]VendorStatusRow, 0, len(entries))
	var latestProbe time.Time

	for _, e := range entries {
		row := s.rowFor(ctx, e.Vendor, windowHours)
		rows = append(rows, row)
	}

	// 排序：Quality.Score 降序 · alive 优先兜底 · anon_label 稳定序
	// **不再按 uptime 单维度排** —— 高产 + 稳定的家会被 quality 综合分抬到前面
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Alive != rows[j].Alive {
			return rows[i].Alive
		}
		if rows[i].Quality.Score != rows[j].Quality.Score {
			return rows[i].Quality.Score > rows[j].Quality.Score
		}
		return rows[i].AnonLabel < rows[j].AnonLabel
	})

	out := StatusOverview{Vendors: rows}
	if !latestProbe.IsZero() {
		out.ProbedAt = latestProbe.UTC().Format(time.RFC3339)
	}
	return out
}

// rowFor 构造单家 vendor 的行 · 静默处理错误（探针数据缺失是正常状态）
//
// **不出 v.DisplayName() 真名** —— 走 anonLabelOf 编号 + anonIDOf 短哈希
//
// windowHours 影响 Quality 里 Volume / Freshness 的评估窗口 · 与前端时间 tab 联动
func (s *Service) rowFor(ctx context.Context, v providers.Vendor, windowHours int) VendorStatusRow {
	vid := string(v.ID())
	cap := v.Capability()

	row := VendorStatusRow{
		AnonID:          anonIDOf(v.ID()),
		AnonLabel:       anonLabelOf(v.ID()),
		HasWarranty:     cap.HasWarranty,
		WarrantyMinutes: cap.WarrantyMinutes,
		MaxPerOrder:     cap.MaxPerOrder,
		StockBucket:     "unknown",
		LifespanBucket:  "unknown",
	}

	// 最近一次探测 · 决定 alive + stock_bucket + region_count + public_status
	latest, err := s.probeStore.LatestProbe(ctx, vid)
	if err == nil && latest != nil {
		row.Alive = latest.Alive
		row.ErrorKind = latest.ErrorKind
		row.RegionCount = len(latest.StockByRegion)
		// availableFromProbe 带分区求和兜底 —— 只报分区库存（stock_total 恒 0）的家
		// 直接拿 StockTotal 会永远判成 out（Overview 那边一直是对的 · 两页曾打架）
		row.StockBucket = bucketStock(availableFromProbe(latest), latest.Alive)
		// 探测到的 warranty / max 覆盖 capability（capability 是静态·探测是运行时）
		if latest.WarrantyMinutes > 0 {
			row.WarrantyMinutes = latest.WarrantyMinutes
			row.HasWarranty = true
		}
		if latest.MaxPerOrder > 0 {
			row.MaxPerOrder = latest.MaxPerOrder
		}

		// PublicStatus · vendor 自报的 fleet 累计数据（只在 vendor 支持时才有）
		// 至少一个字段非 nil 才出 · 全 nil 时省略这个整块（omitempty）
		if latest.PSKeysActive != nil || latest.PSKeysDead != nil ||
			latest.PSKeysStock != nil || latest.PSGenerating != nil {
			ps := &PublicStatusOut{
				KeysActive:    latest.PSKeysActive,
				KeysAlive:     latest.PSKeysAlive,
				KeysDead:      latest.PSKeysDead,
				KeysStock:     latest.PSKeysStock,
				KeysSuspect:   latest.PSKeysSuspect,
				KeysTotal:     latest.PSKeysTotal,
				Generating:    latest.PSGenerating,
				UptimeSeconds: latest.PSUptimeSeconds,
			}
			if latest.PSStartedAt != nil {
				ps.StartedAt = latest.PSStartedAt.UTC().Format(time.RFC3339)
			}
			row.PublicStatus = ps
		}
	}

	// Dispatch · vendor 平台"最近开号"节奏（fleet-wide · 6 家都能有）
	//
	// 数据源优先级：
	//  1. FleetLister 拉的官方 gen-logs（多家 vendor 有 · 最准）
	//  2. 从 vendor_probe 的 PS 字段增量推（多家 vendor · 兜底）
	//
	// 3 家没 FleetLister 的 vendor 上游穷举过所有 gen-logs / stats / timeline 端点
	// 都是 404 · 只能用 60s 探针跨采样对比推 batch · 精度差 · 但比"数据采集中"强。
	if s.orderKeyStore != nil {
		ds, err := s.orderKeyStore.DispatchSummary(ctx, vid, 20)
		if err == nil && ds != nil && ds.TotalBatches > 0 {
			d := &DispatchOut{
				TotalBatches:        ds.TotalBatches,
				TotalKeysDispatched: ds.TotalKeysDispatched,
				AvgIntervalMin:      ds.AvgIntervalMin,
			}
			if !ds.LastDispatchAt.IsZero() {
				d.LastDispatchAt = ds.LastDispatchAt.UTC().Format(time.RFC3339)
			}
			row.Dispatch = d
		}
	}
	// FleetLister 无数据 · 从探针增量推
	if row.Dispatch == nil && s.probeStore != nil {
		derived, err := s.probeStore.DeriveDispatchSummary(ctx, vid, 24*7)
		if err == nil && !derived.IsEmpty() {
			d := &DispatchOut{
				TotalBatches:        derived.TotalBatches,
				TotalKeysDispatched: derived.TotalKeysDispatched,
				AvgIntervalMin:      derived.AvgIntervalMin,
			}
			if !derived.LastDispatchAt.IsZero() {
				d.LastDispatchAt = derived.LastDispatchAt.UTC().Format(time.RFC3339)
			}
			row.Dispatch = d
		}
	}

	// Backfill 历史 · 从 vendor_order + vendor_key 拿实实在在的历史累计
	// 不管 alive 状态 · 有数据就出（vendor 侧的历史 · 我方零起步就能显示）
	if s.orderKeyStore != nil {
		summary, err := s.orderKeyStore.HistorySummary(ctx, vid)
		if err == nil && summary != nil && (summary.TotalOrders > 0 || summary.TotalKeys > 0) {
			out := &HistoryOut{
				TotalOrders:    summary.TotalOrders,
				TotalKeys:      summary.TotalKeys,
				ActiveKeys:     summary.ActiveKeys,
				DeadKeys:       summary.DeadKeys,
				AvgLifespanSec: summary.AvgLifespanSec,
			}
			if !summary.FirstOrderAt.IsZero() {
				out.FirstOrderAt = summary.FirstOrderAt.UTC().Format(time.RFC3339)
			}
			if !summary.LastOrderAt.IsZero() {
				out.LastOrderAt = summary.LastOrderAt.UTC().Format(time.RFC3339)
			}
			row.History = out

			// Lifespan bucket 从平均寿命反推档位 · 覆盖 latest（更可信）
			if summary.AvgLifespanSec > 3600 {
				row.LifespanBucket = "long"
			} else if summary.AvgLifespanSec > 600 {
				row.LifespanBucket = "mid"
			} else if summary.AvgLifespanSec > 0 {
				row.LifespanBucket = "short"
			}
		}
	}

	// 24h uptime · 样本少于 10 时不给（数据不可信）
	pct, samples, err := s.probeStore.Uptime24h(ctx, vid)
	if err == nil && samples >= 10 {
		p := int(pct*100 + 0.5)
		row.Uptime24hPct = &p
	}

	// 24h stockout 分钟数
	stockout, err := s.probeStore.StockoutMinutes24h(ctx, vid, int(s.probeInterval.Seconds()))
	if err == nil {
		row.Stockout24hMinutes = stockout
	}

	// 7d incidents
	incidents, err := s.probeStore.Incidents7d(ctx, vid)
	if err == nil {
		row.Incidents7d = incidents
	}

	// ─── Quality · 综合分 + 标签 ─────────────────────────────
	//
	// 需要窗口内的 dispatch 数据来算 Volume/Freshness · 复用已有 DispatchesSince
	// （已优先从 vendor_dispatch 表拿 · 表空 fallback 探针 deriver · 跟 events 端点同源）
	qi := qualityInput{
		alive:        row.Alive,
		uptime24hPct: row.Uptime24hPct,
		stockBucket:  row.StockBucket,
		hasWarranty:  row.HasWarranty,
		now:          time.Now(),
	}

	// 窗口内 dispatch 批次 · 优先 vendor 自报 · 兜底探针 deriver（跟 DispatchEvents 同源）
	var windowBatches int
	var latestDispatch time.Time
	if s.orderKeyStore != nil {
		if ds, err := s.orderKeyStore.DispatchesSince(ctx, vid, windowHours, 500); err == nil {
			windowBatches = len(ds)
			for _, d := range ds {
				if d.DispatchedAt.After(latestDispatch) {
					latestDispatch = d.DispatchedAt
				}
			}
		}
	}
	// 没自报 · 探针推
	if windowBatches == 0 && s.probeStore != nil {
		if derived, err := s.probeStore.DeriveDispatchEvents(ctx, vid, windowHours, 500); err == nil {
			windowBatches = len(derived)
			for _, d := range derived {
				if d.At.After(latestDispatch) {
					latestDispatch = d.At
				}
			}
		}
	}
	qi.dispatchBatches = windowBatches
	qi.lastDispatch = latestDispatch

	// 数据是否足够 · uptime 有样本 或 有任何 dispatch 都算够
	qi.dataSufficient = row.Uptime24hPct != nil || windowBatches > 0

	row.Quality = computeQuality(qi)

	return row
}

// bucketStock 库存量档位化 · 避免精确数字被竞品拿去做实时供应链情报
func bucketStock(total int, alive bool) string {
	if !alive {
		return "unknown"
	}
	switch {
	case total <= 0:
		return "out"
	case total < 20:
		return "low"
	default:
		return "many"
	}
}

// StatusTrend 单家 vendor 过去 windowHours 小时的 sparkline 数据（脱敏后）。
//
// 只出 alive_pct + stock_bucket 序列 —— **不出具体库存数字**，前端画个大致轮廓即可。
// 用 anonID 查真 vendor_id · 不再暴露内部 id。
type StatusTrend struct {
	AnonID    string `json:"anon_id"`
	AnonLabel string `json:"anon_label"`
	Window    string `json:"window"` // "24h"
	// Source · 数据来源 · backfill = vendor 侧真历史 · probe = 我方探针积累 · empty = 都没
	// 前端按 source 决定画什么曲线（backfill 画 keys_born/died；probe 画 uptime）
	Source string             `json:"source"`
	Points []StatusTrendPoint `json:"points"` // 按时间升序
}

// StatusTrendPoint 一个桶的脱敏数据点。
//
// 有 backfill 数据（vendor 侧真历史）时 · 桶是 1 小时 · keys_born + keys_died
// 没有 backfill 数据时 · 回落到探针 15min 桶 · 只有 uptime_pct + stock_bucket
type StatusTrendPoint struct {
	// T 桶起点（RFC3339 UTC）· x 轴
	T string `json:"t"`

	// —— 探针数据（老维度 · vendor 无 backfill 时才有）——
	UptimePct   int    `json:"uptime_pct,omitempty"`
	StockBucket string `json:"stock_bucket,omitempty"`
	Samples     int    `json:"samples,omitempty"`

	// —— Backfill 数据（vendor 侧真历史 · 优先展示）——
	// KeysBorn 桶内新发的 key 数
	KeysBorn int `json:"keys_born,omitempty"`
	// KeysDied 桶内挂掉的 key 数
	KeysDied int `json:"keys_died,omitempty"`
	// AvgLifespanSec 桶内挂掉的 key 平均寿命秒数
	AvgLifespanSec int64 `json:"avg_lifespan_sec,omitempty"`
}

// ── 统一开号事件流（6 家同一形状 · 前端只画一种图）─────────────
//
// **为什么加这个**：老的 StatusTrend 按 source 返两种 schema（backfill 给
// keys_born/keys_died · probe 给 uptime_pct）· 前端只能 if/else 画两种图 ·
// 用户看到"有的柱有的线"根本没法横向比。契约层就该统一。
//
// 统一口径：**一条事件 = 上游一次开号**。
//   - 有 fleet 端点的 vendor → 读 vendor_dispatch 表（vendor 自报 · Derived=false）
//   - 没有的 vendor         → 探针增量推同形状事件（Derived=true · 前端注明"观测推算"）
//
// 脱敏：不出真 vendor 名 · 不出价格 · region 只出"美区/欧区/全区"这种人话。

// DispatchEventsOut 是 GET /api/vendors/status/{anon_id}/events 的响应。
type DispatchEventsOut struct {
	AnonID    string `json:"anon_id"`
	AnonLabel string `json:"anon_label"`
	Window    string `json:"window"` // "168h"
	// Source · vendor = 上游自报的批次（准）· observed = 我方探针增量推算 · empty = 无数据
	// **前端不按 source 换图形** —— 只用来显示一句数据来源说明。
	Source string `json:"source"`
	// Events 按时间倒序（最新在前）· 前端画图时自己反转
	Events []DispatchEvent `json:"events"`
	// Summary 窗口内的汇总（前端不用自己 reduce）
	Summary DispatchEventsSummary `json:"summary"`
}

// DispatchEventsSummary 窗口内汇总。
type DispatchEventsSummary struct {
	Batches        int     `json:"batches"`
	Keys           int     `json:"keys"`
	AvgIntervalMin float64 `json:"avg_interval_min,omitempty"`
	// AliveNow 窗口内还活着的号（vendor 自报 alive 字段的和 · observed 源没有）
	AliveNow  int `json:"alive_now,omitempty"`
	DeadTotal int `json:"dead_total,omitempty"`
}

// DispatchEvent 一次开号 · 6 家同一形状。
type DispatchEvent struct {
	// At 发出时刻 · RFC3339 UTC
	At string `json:"at"`
	// Count 这批发了几个号
	Count int `json:"count"`
	// Region 人话区域名（"美区" / "欧区" / "" = 不分区）· **不出 us-east-1 这种内部 id**
	Region string `json:"region,omitempty"`
	// Alive / Dead vendor 自报的存活情况（observed 源没有 · 为 0）
	Alive int `json:"alive,omitempty"`
	Dead  int `json:"dead,omitempty"`
	// Status 收敛后的三态：running（在架）/ done（已发完）/ dead（全挂）
	// **内部 vendor 状态字符串不出去**（CLAUDE.md §12.5）
	Status string `json:"status,omitempty"`
	// DeadAt 全批挂完的时刻（有则填）
	DeadAt string `json:"dead_at,omitempty"`
	// Derived true = 我方探针增量推算 · false = vendor 自报
	Derived bool `json:"derived,omitempty"`
}

// DispatchEvents 返统一形状的开号事件流。
//
// windowHours <= 0 默认 168（7 天）· 上限 720（30 天）。
// limit <= 0 默认 200 条。
func (s *Service) DispatchEvents(ctx context.Context, anonID string, windowHours, limit int) (*DispatchEventsOut, error) {
	if s.registry == nil {
		return nil, nil
	}
	if windowHours <= 0 {
		windowHours = 168
	}
	if windowHours > 720 {
		windowHours = 720
	}
	if limit <= 0 {
		limit = 200
	}

	var found providers.Vendor
	for _, e := range s.registry.Enabled() {
		if anonIDOf(e.Vendor.ID()) == anonID {
			found = e.Vendor
			break
		}
	}
	if found == nil {
		return nil, nil
	}
	vid := string(found.ID())

	out := &DispatchEventsOut{
		AnonID:    anonID,
		AnonLabel: anonLabelOf(found.ID()),
		Window:    fmt.Sprintf("%dh", windowHours),
		Source:    "empty",
		Events:    []DispatchEvent{},
	}

	// ① vendor 自报的批次（vendor_dispatch 表）
	//
	// **自动扩窗**：请求窗口内没数据时 · 扩到 720h（30 天）再查一次。
	// 理由：有的上游最近几天没开号（但历史有 18 批）· 死守 168h 会让卡片显示
	// "无记录" · 用户以为我方没采到数据 —— 其实是上游本来就没动静。扩窗后
	// 卡片能显示真实历史 · Window 字段会回报实际用的窗口。
	if s.orderKeyStore != nil {
		ds, err := s.orderKeyStore.DispatchesSince(ctx, vid, windowHours, limit)
		if err == nil && len(ds) == 0 && windowHours < 720 {
			if wider, e := s.orderKeyStore.DispatchesSince(ctx, vid, 720, limit); e == nil && len(wider) > 0 {
				ds = wider
				out.Window = "720h"
			}
		}
		if err == nil && len(ds) > 0 {
			out.Source = "vendor"
			for _, d := range ds {
				ev := DispatchEvent{
					At:     d.DispatchedAt.UTC().Format(time.RFC3339),
					Count:  d.Count,
					Region: regionLabel(d.Region),
					Alive:  d.Alive,
					Dead:   d.Dead,
					Status: dispatchStatus(d.Status, d.Dead, d.Count),
				}
				if !d.DeadAt.IsZero() {
					ev.DeadAt = d.DeadAt.UTC().Format(time.RFC3339)
				}
				out.Events = append(out.Events, ev)
			}
			out.Summary = summarizeEvents(out.Events)
			return out, nil
		}
	}

	// ② 探针增量推算（没 fleet 端点的 vendor · 形状跟 ① 完全一致）
	if s.probeStore != nil {
		derived, err := s.probeStore.DeriveDispatchEvents(ctx, vid, windowHours, limit)
		if err == nil && len(derived) > 0 {
			out.Source = "observed"
			for _, d := range derived {
				out.Events = append(out.Events, DispatchEvent{
					At:      d.At.UTC().Format(time.RFC3339),
					Count:   d.Count,
					Derived: true,
				})
			}
			out.Summary = summarizeEvents(out.Events)
			return out, nil
		}
	}

	return out, nil
}

// summarizeEvents 从倒序事件列表算汇总（相邻间隔用倒序差）。
func summarizeEvents(events []DispatchEvent) DispatchEventsSummary {
	sum := DispatchEventsSummary{Batches: len(events)}
	for _, e := range events {
		sum.Keys += e.Count
		sum.AliveNow += e.Alive
		sum.DeadTotal += e.Dead
	}
	if len(events) >= 2 {
		var totalSec float64
		var pairs int
		for i := 0; i < len(events)-1; i++ {
			newer, err1 := time.Parse(time.RFC3339, events[i].At)
			older, err2 := time.Parse(time.RFC3339, events[i+1].At)
			if err1 != nil || err2 != nil {
				continue
			}
			if gap := newer.Sub(older).Seconds(); gap > 0 {
				totalSec += gap
				pairs++
			}
		}
		if pairs > 0 {
			sum.AvgIntervalMin = totalSec / float64(pairs) / 60
		}
	}
	return sum
}

// regionLabel 内部 region id → 对外人话（CLAUDE.md §12.6 · 不出 us-east-1）。
func regionLabel(region string) string {
	switch {
	case region == "":
		return ""
	case strings.HasPrefix(region, "us"):
		return "美区"
	case strings.HasPrefix(region, "eu"):
		return "欧区"
	}
	return "其他区"
}

// dispatchStatus 收敛 vendor 自报状态到三态（CLAUDE.md §12.5）。
//
// vendor 侧字符串五花八门（running / done / error / dead / ...）·
// 对外只留：running（在架）/ done（已发完）/ dead（全挂）。
func dispatchStatus(raw string, dead, count int) string {
	if dead > 0 && count > 0 && dead >= count {
		return "dead"
	}
	switch raw {
	case "running":
		return "running"
	case "dead":
		return "dead"
	}
	return "done"
}

// StatusTrend 找到匿名 id 对应的 vendor，返窗口内的桶数据。
//
// 优先用 backfill 的 key 生命周期（真历史 · 1 小时桶）· vendor 没 backfill 时
// fallback 到探针数据（15min 桶）· 未知 anon_id 返 nil, nil（404）。
func (s *Service) StatusTrend(ctx context.Context, anonID string, windowHours int) (*StatusTrend, error) {
	if s.registry == nil {
		return nil, nil
	}
	if windowHours <= 0 {
		windowHours = 24
	}
	// 反查真 vendor
	var found providers.Vendor
	for _, e := range s.registry.Enabled() {
		if anonIDOf(e.Vendor.ID()) == anonID {
			found = e.Vendor
			break
		}
	}
	if found == nil {
		return nil, nil
	}
	vid := string(found.ID())

	// 优先：backfill 的 key 生命周期（真历史）
	// 如果指定窗口内没数据 · 自动扩到 720h（30 天）· 让首日 vendor 一秒有图
	if s.orderKeyStore != nil {
		buckets, err := s.orderKeyStore.KeyLifecycleBuckets(ctx, vid, windowHours, 60)
		if err == nil && len(buckets) == 0 && windowHours < 720 {
			// 扩窗一次到 30 天再查
			buckets, err = s.orderKeyStore.KeyLifecycleBuckets(ctx, vid, 720, 60)
		}
		if err == nil && len(buckets) > 0 {
			points := make([]StatusTrendPoint, 0, len(buckets))
			for _, b := range buckets {
				points = append(points, StatusTrendPoint{
					T:              b.BucketStart,
					KeysBorn:       b.KeysBorn,
					KeysDied:       b.KeysDied,
					AvgLifespanSec: b.AvgLifespanSec,
				})
			}
			return &StatusTrend{
				AnonID:    anonID,
				AnonLabel: anonLabelOf(found.ID()),
				Window:    fmt.Sprintf("%dh", windowHours),
				Source:    "backfill",
				Points:    points,
			}, nil
		}
	}

	// Fallback：探针数据（vendor 没 backfill 端点时用）
	if s.probeStore != nil {
		buckets, err := s.probeStore.TrendBuckets(ctx, vid, windowHours, 15)
		if err == nil && len(buckets) > 0 {
			points := make([]StatusTrendPoint, 0, len(buckets))
			for _, b := range buckets {
				points = append(points, StatusTrendPoint{
					T:           b.BucketStart,
					UptimePct:   int(b.AlivePct*100 + 0.5),
					StockBucket: bucketStock(int(b.StockAvg+0.5), b.AlivePct > 0),
					Samples:     b.Samples,
				})
			}
			return &StatusTrend{
				AnonID:    anonID,
				AnonLabel: anonLabelOf(found.ID()),
				Window:    fmt.Sprintf("%dh", windowHours),
				Source:    "probe",
				Points:    points,
			}, nil
		}
	}

	// 都没数据 · 返一个空 trend · 前端"数据采集中"
	return &StatusTrend{
		AnonID:    anonID,
		AnonLabel: anonLabelOf(found.ID()),
		Window:    fmt.Sprintf("%dh", windowHours),
		Source:    "empty",
		Points:    nil,
	}, nil
}
