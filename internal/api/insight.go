package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/insight"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
)

// 聚合读端点 · 首页 / 数据 tab / 活动流 / 价格走势
// 响应形状对齐 web/src/types/index.ts —— 前端已按那份跑通，直接映射即可。
//
// 内部术语（vendor_id / bus_id）在响应里是**元数据**：
//   - vendor_id：前端拿去查颜色 / 名字（VENDOR_NAME 映射）· 不是"内部术语泄漏"
//   - bus_id：前端跳转链接 · 是稳定引用
// 分项链分层字段（key_cost / vendor_fee / …）**永远不出响应体**（CLAUDE.md §0.1）。
//
// 用**独立接口**装依赖（不直接吃 *insight.Store）· 好处：
//   - handler_test 里可以喂 mock，不依赖 Server 装配（不用等 ServerDeps 加字段）
//   - 装配阶段：ServerDeps 加 *insight.Store，方法名一致即满足接口

// insightReader 读侧的三个聚合能力。
type insightReader interface {
	Overview(ctx context.Context, passengerID string) (*insight.Overview, error)
	Trend(ctx context.Context, passengerID string, metric insight.TrendMetric,
		days int, scope insight.TrendScope) ([]insight.TrendPoint, error)
	Activities(ctx context.Context, passengerID string, page, pageSize int) ([]insight.Activity, int, error)
}

// busChecker 用于 trend 端点校验 bus 归属 —— 别人的车不能查。
// 由 *bus.Store 实现（方法签名一致）。
type busChecker interface {
	GetForPassenger(ctx context.Context, busID, passengerID string) (*bus.Bus, error)
}

// handleOverviewWith 构造 GET /api/me/overview 的 handler。
func handleOverviewWith(rdr insightReader) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		p, err := mustCaller(r)
		if err != nil {
			return err
		}
		out, err := rdr.Overview(r.Context(), p.ID)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, out)
		return nil
	}
}

// handleTrendWith 构造 GET /api/me/trend 的 handler。
//
//	range ∈ {today, 7d, 30d(默认), 90d}  ·  metric ∈ {credits(默认), pulls, lifespan, usage}
//	scope 可选 bus_id 或 vendor（二选一 · 同时传 400）
func handleTrendWith(rdr insightReader, buses busChecker) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		p, err := mustCaller(r)
		if err != nil {
			return err
		}
		q := r.URL.Query()
		metric := insight.TrendMetric(q.Get("metric"))
		if metric == "" {
			metric = insight.TrendCredits
		}
		switch metric {
		case insight.TrendCredits, insight.TrendPulls, insight.TrendLifespan, insight.TrendUsage:
		default:
			return ErrBadRequest("metric 只能是 credits / pulls / lifespan / usage")
		}

		days := daysFromRange(q.Get("range"))

		scope := insight.TrendScope{
			BusID:    strings.TrimSpace(q.Get("bus_id")),
			VendorID: strings.TrimSpace(q.Get("vendor")),
		}
		if scope.BusID != "" && scope.VendorID != "" {
			return ErrBadRequest("bus_id 和 vendor 不能同时传")
		}
		// bus scope 校验归属 —— 别人的车不能查
		if scope.BusID != "" && buses != nil {
			if _, err := buses.GetForPassenger(r.Context(), scope.BusID, p.ID); err != nil {
				return ErrNotFound("找不到这辆车")
			}
		}

		points, err := rdr.Trend(r.Context(), p.ID, metric, days, scope)
		if err != nil {
			return err
		}
		if points == nil {
			points = []insight.TrendPoint{}
		}
		writeJSON(w, http.StatusOK, points)
		return nil
	}
}

// handleActivitiesWith 构造 GET /api/me/activities 的 handler。
//
// **§0.1 · vendor 匿名化必须在服务端做** —— activities 的 Source 直接是 vendor_id ·
// Summary 里也拼了真名（insight/activities.go）· 不匿名化会漏给 retail / community 档。
func (s *Server) handleActivitiesWith(rdr insightReader) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		p, err := mustCaller(r)
		if err != nil {
			return err
		}
		q := r.URL.Query()
		page := atoiDefault(q.Get("page"), 1)
		pageSize := atoiDefault(q.Get("page_size"), 20)
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 200 {
			pageSize = 20
		}
		items, total, err := rdr.Activities(r.Context(), p.ID, page, pageSize)
		if err != nil {
			return err
		}
		if items == nil {
			items = []insight.Activity{}
		}
		// § 0.1 · 匿名化 · 每条 activity 里的 vendor 真名 → 匿名 label
		// wholesale 档看真名 · 其他档看 "AWS-Q Kiro Vendor NN"
		if s.vendorView != nil {
			viewer := viewerOf(p, r)
			for i := range items {
				items[i] = anonymizeActivity(items[i], s.vendorView, viewer)
			}
		}
		pages := (total + pageSize - 1) / pageSize
		writeJSON(w, http.StatusOK, map[string]any{
			"items": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
		})
		return nil
	}
}

// anonymizeActivity · 单条 activity 的 vendor 名 → 匿名 label 转换
// 判据:Source 长得像 vendor_id(在 vendorview.AnonIDFor 能找到编号)· 就替换成 label
// Summary 用 strings.Replace 把真名替换掉
func anonymizeActivity(a insight.Activity, vsvc *vendorview.Service, v vendorview.Viewer) insight.Activity {
	if vsvc == nil {
		return a
	}
	// Source 可能是 vendor_id · 也可能是 bus_id 或别的东西 · 只在能匿名化时替换
	if a.Source != "" {
		anonLabel := vsvc.LabelFor(a.Source, v)
		if anonLabel != "" && anonLabel != a.Source {
			// 先替换 Summary 里的真名(Summary 里可能出现"<vendor 真名> · 拉 1 个")
			a.Summary = strings.ReplaceAll(a.Summary, a.Source, anonLabel)
			a.Source = anonLabel
		}
	}
	return a
}

// vendorPriceTrendResp 单家 vendor 的价格走势响应块。字段跟 types.ts 的
// VendorPriceTrend 完全对齐（含派生汇总）· 前端不做二次派生。
//
// 1a 阶段暂无数据源 · 装配好这一层的类型，等 1d 采集到 vendor_round 后填。
type vendorPriceTrendResp struct {
	VendorID          string            `json:"vendor_id"`
	VendorLabel       string            `json:"vendor_label"`
	Zone              *string           `json:"zone"`
	Days              []vendorDayRounds `json:"days"`
	CurrentPrice      int64             `json:"current_price"`
	PriceHigh         int64             `json:"price_high"`
	PriceLow          int64             `json:"price_low"`
	PriceAvg          int64             `json:"price_avg"`
	TotalRounds       int               `json:"total_rounds"`
	AvgRoundsPerDay   float64           `json:"avg_rounds_per_day"`
	Change30dPct      float64           `json:"change_30d_pct"`
	NoServiceDays     int               `json:"no_service_days"`
	LongestStreakDays int               `json:"longest_streak_days"`
	InStockNow        bool              `json:"in_stock_now"`
}

type vendorDayRounds struct {
	Date   string             `json:"date"`
	Rounds []vendorPriceRound `json:"rounds"`
}

type vendorPriceRound struct {
	Time      string  `json:"time"`
	Zone      *string `json:"zone"`
	UnitPrice int64   `json:"unit_price"`
	KeysCount int     `json:"keys_count"`
}

// handleVendorPrices GET /api/vendors/prices —— 价格走势页。
//
// **1a 阶段无数据源**（`vendor_round` 表 1d 才建）· 返回**形状完整但 trends 为空数组**，
// 前端会渲染空态 · 前端契约 §9 已说明"1b 返 501 可接受"，我们比 501 更好：返 200 空数组，
// 页面正常渲染只是没线。
//
// 端点仍走鉴权 —— 因为价格是按身份定价的（社群 vs 散客），未鉴权拿不到定价上下文。
func handleVendorPrices(w http.ResponseWriter, r *http.Request) error {
	if _, err := mustCaller(r); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"trends": []vendorPriceTrendResp{},
	})
	return nil
}

// daysFromRange 把 range 参数映射成天数。
//
//	today = 1  ·  7d = 7  ·  30d(默认) = 30  ·  90d = 90
func daysFromRange(v string) int {
	switch v {
	case "today":
		return 1
	case "7d":
		return 7
	case "90d":
		return 90
	case "30d", "":
		return 30
	}
	return 30
}
