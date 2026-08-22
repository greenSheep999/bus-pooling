// Package insight 聚合读取乘客维度的 KPI / 趋势 / 活动流。
//
// 纯读，不写任何表。所有查询都跟 passenger_id 挂钩（多租户隔离靠这里）。
// 关键设计：**只暴露聚合后的对外形状** —— 内部 reason / status / vendor id
// 都在这一层收敛（CLAUDE.md §12.5），handler 直接下发结果不再翻译。
package insight

import (
	"database/sql"
	"time"
)

// Store 是 insight 的 SQL 门面。构造时只吃 *sql.DB —— 不依赖别的业务包，
// 因为聚合就是跨包读，走对方的 Go API 会造出一堆 N+1 查询。
type Store struct {
	db *sql.DB
	// now 让测试能注入固定时间（今日/昨日/本月的分割靠它）
	now func() time.Time
}

// NewStore 建 Store。生产用 time.Now.UTC；测试可用 WithClock 覆盖。
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// WithClock 换掉时钟（测试用）。
func (s *Store) WithClock(fn func() time.Time) *Store {
	s.now = fn
	return s
}

// ── 对外类型 · 跟 web/src/types/index.ts 一一对应 ──────

// Money 是 microunit 整数（1 积分 = 1_000_000）。
type Money = int64

// KPI 首页 4 指标卡（Overview.kpi）。
type KPI struct {
	Balance            Money `json:"balance"`
	BalanceDeltaTopup  Money `json:"balance_delta_topup"`
	BalanceDeltaSpend  Money `json:"balance_delta_spend"`
	SpendToday         Money `json:"spend_today"`
	SpendYesterday     Money `json:"spend_yesterday"`
	PullTotal          int   `json:"pull_total"`
	PullThisMonth      int   `json:"pull_this_month"`
	AliveCount         int   `json:"alive_count"`
	DeadCount          int   `json:"dead_count"`
	PendingRefill      int   `json:"pending_refill"`
	// nil = 还没有号死过（全是活号）· 前端显"暂无"而非误导性的 0 秒
	// （口径同 busResponse.AvgLifespanSeconds · 别让 Overview 显 0h 而车详情显 —）
	AvgLifespanSeconds *int64 `json:"avg_lifespan_seconds"`
}

// BusesSummary Overview 的车汇总块。
type BusesSummary struct {
	BusCount         int             `json:"bus_count"`
	TotalCredentials int             `json:"total_credentials"`
	RefillCount      int             `json:"refill_count"`
	CoalesceRate     float64         `json:"coalesce_rate"`
	Items            []BusSummaryRow `json:"items"`
}

// BusSummaryRow Overview.buses.items[] 一行。
type BusSummaryRow struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"` // owner | member
	Alive int    `json:"alive"`
	Dead  int    `json:"dead"`
	Spend Money  `json:"spend"`
}

// ExtractSummary Overview 的号池待派 / 已派汇总。
type ExtractSummary struct {
	CountToday       int              `json:"count_today"`
	TotalCredentials int              `json:"total_credentials"`
	Pending          int              `json:"pending"`
	Spend            Money            `json:"spend"`
	ByDestination    []DestinationRow `json:"by_destination"`
}

// DestinationRow 号池按去向的分布。destination ∈ {pending, into_bus, push_pool}
// —— handoff 不在这里（号已离开系统，见 fixtures.ts 注释）。
type DestinationRow struct {
	Destination string `json:"destination"`
	Count       int    `json:"count"`
}

// Overview 首页大响应。
type Overview struct {
	KPI     KPI            `json:"kpi"`
	Buses   BusesSummary   `json:"buses"`
	Extract ExtractSummary `json:"extract"`
}

// TrendPoint 时序单点。
type TrendPoint struct {
	Date  string  `json:"date"`  // YYYY-MM-DD
	Value float64 `json:"value"` // 具体度量决定单位
}

// TrendMetric 支持的度量类型。lifespan 为平均寿命（小时数），
// pulls 为拉号轮次数，credits 为花掉的积分（正数 · 支出的绝对值）。
type TrendMetric string

const (
	TrendCredits  TrendMetric = "credits"
	TrendPulls    TrendMetric = "pulls"
	TrendLifespan TrendMetric = "lifespan"
	// TrendUsage 号的**实际用量**(号池 5min 采样的 current_usage)· 跟 credits 不是一回事:
	//   credits = 买号花的钱(我方扣的积分) · usage = 号在上游被用掉的额度
	// 「用量趋势」这个图原来三条线全是花费/拉号/补车 · 压根没有用量 —— 这条补上。
	TrendUsage TrendMetric = "usage"
)

// TrendScope 可选过滤 —— bus 或 vendor（二选一，不同时传，handler 层拦掉）。
type TrendScope struct {
	BusID    string
	VendorID string
}

// Activity 活动流一条。kind + source/target 是结构化视图，summary 兜底文本。
type Activity struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Source     string  `json:"source,omitempty"`
	Target     string  `json:"target,omitempty"`
	TargetKind string  `json:"target_kind,omitempty"`
	Count      int     `json:"count,omitempty"`
	CountUnit  string  `json:"count_unit,omitempty"`
	Summary    string  `json:"summary"`
	// SummaryCode 兜底文案的**机器码**（memo 为空时才有）· 前端按码出 i18n ·
	// Summary 非空时优先用 Summary（那是运营写的 memo 原文 · 是数据）
	SummaryCode string `json:"summary_code,omitempty"`
	Amount     *Money  `json:"amount"`
	CreatedAt  string  `json:"created_at"`
	Link       *string `json:"link"`
}

// ActivityKind 允许的 kind 集合（前端 chip 分派 · 都是对外术语）。
const (
	ActivityIntoBus = "into_bus"
	ActivityExtract = "extract"
	ActivityRefill  = "refill"
	ActivityDead    = "dead"
	ActivityTopup   = "topup"
	ActivityRedeem  = "redeem"
	ActivityPush    = "push"
	ActivityHandoff = "handoff"
)
