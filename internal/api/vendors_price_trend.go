package api

// 涨价历史端点 · GET /api/vendors/{anon_id}/price-trend
//
// **公开** · 匿名（用 anon_id 不用 vendor_id）· 前端 vendors 页画价格走势。
//
// **数据源**：vendor_probe_zone（多 source 合并 · 已经在跑）
// **窗口**：?window=Nd（默认 7d · 上限 30d · 太久数据点太密画不动）
// **粒度**：60s 采样过粗 · 按小时聚合返（Nd = N*24 数据点）· 让前端一次拉完 · 不再滚
//
// **响应**：每个 zone 一个 series · 每 series 三个 source 分开返（让运维看 vendor_self 是不是断流）·
// 前端画多线图 / 主图 vendor_self · xi8/xi8_notif 淡化。

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

type priceTrendResp struct {
	AnonID  string           `json:"anon_id"`
	WindowH int              `json:"window_hours"`
	Zones   []priceTrendZone `json:"zones"`
}

type priceTrendZone struct {
	Zone   string                       `json:"zone"`
	Series map[string][]priceTrendPoint `json:"series"` // source → 点列表
}

type priceTrendPoint struct {
	// HourUTC · RFC3339 · 该小时的整点时刻
	HourUTC string `json:"hour_utc"`
	// AvgCredits · 该小时内该 vendor + zone + source 的平均积分单价（microunit）
	AvgCredits int64 `json:"avg_credits"`
	// Samples · 该小时的样本数（探针 60s 一轮 · 满小时 60 样本）
	Samples int `json:"samples"`
}

func (s *Server) handlePriceTrend(w http.ResponseWriter, r *http.Request) error {
	if s.vendorView == nil {
		return ErrNotFound("vendor 服务未装配")
	}
	anonID := r.PathValue("anon_id")
	if anonID == "" {
		return ErrBadRequest("缺少 anon_id")
	}
	windowH := atoiDefault(r.URL.Query().Get("window_hours"), 168) // 默认 7 天
	if windowH < 24 {
		windowH = 24
	}
	if windowH > 720 { // 30 天上限
		windowH = 720
	}

	// anon_id → vendor_id（内部）
	vendorID := s.vendorView.VendorIDForAnon(anonID)
	if vendorID == "" {
		return ErrNotFound("找不到这家 vendor")
	}

	zones, err := loadPriceTrend(r.Context(), s.db, vendorID, windowH)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, priceTrendResp{
		AnonID:  anonID,
		WindowH: windowH,
		Zones:   zones,
	})
	return nil
}

// loadPriceTrend · 按 zone × source 分组 · 按小时聚合
func loadPriceTrend(ctx context.Context, db *sql.DB, vendorID string, windowH int) ([]priceTrendZone, error) {
	if db == nil {
		return nil, nil
	}
	cutoff := time.Now().Add(-time.Duration(windowH) * time.Hour).UTC().Format(time.RFC3339)

	// SQLite 按小时聚合：SUBSTR(probed_at,1,13) 取 YYYY-MM-DDTHH 前 13 字符
	rows, err := db.QueryContext(ctx, `
		SELECT zone, source,
		       SUBSTR(probed_at, 1, 13) || ':00:00Z' AS hour_utc,
		       CAST(AVG(our_unit_credits) AS INTEGER) AS avg_credits,
		       COUNT(*) AS samples
		  FROM vendor_probe_zone
		 WHERE vendor_id = ?
		   AND probed_at >= ?
		   AND our_unit_credits > 0
		 GROUP BY zone, source, hour_utc
		 ORDER BY zone, source, hour_utc`,
		vendorID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 按 zone 聚成 map · 每个 zone 再按 source 聚
	byZone := make(map[string]map[string][]priceTrendPoint)
	zoneOrder := make([]string, 0, 2)
	for rows.Next() {
		var zone, source, hour string
		var avg int64
		var samples int
		if err := rows.Scan(&zone, &source, &hour, &avg, &samples); err != nil {
			return nil, err
		}
		if _, ok := byZone[zone]; !ok {
			byZone[zone] = make(map[string][]priceTrendPoint)
			zoneOrder = append(zoneOrder, zone)
		}
		byZone[zone][source] = append(byZone[zone][source], priceTrendPoint{
			HourUTC: hour, AvgCredits: avg, Samples: samples,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]priceTrendZone, 0, len(zoneOrder))
	for _, z := range zoneOrder {
		out = append(out, priceTrendZone{Zone: z, Series: byZone[z]})
	}
	return out, nil
}
