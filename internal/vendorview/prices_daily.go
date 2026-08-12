package vendorview

import (
	"context"
	"database/sql"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ResolveAnonID · 从对外的 anon_id（6 位 hash）还原到内部 vendor_id。
// 找不到返 ("", false) · api 层判 404。
//
// 复用 anonIDOf 生成规则（status_view.go）· 遍历 6 家找匹配。
func (s *Service) ResolveAnonID(anonID string) (string, bool) {
	if s.registry == nil {
		return "", false
	}
	for _, e := range s.registry.Enabled() {
		if anonIDOf(e.Vendor.ID()) == anonID {
			return string(e.Vendor.ID()), true
		}
	}
	return "", false
}

// DailyPrices · Prices 每日高低查询 · api handler 用。
// 委托到 ProbeStore.DailyPricesForVendor · Service 层做门面转发。
func (s *Service) DailyPrices(ctx context.Context, vendorID string, days int) ([]DailyPricePoint, error) {
	if s.probeStore == nil {
		return nil, nil
	}
	return s.probeStore.DailyPricesForVendor(ctx, vendorID, days)
}

var _ providers.VendorID // silence import when unused (安全护栏)

// DailyPricePoint · 单 vendor 单日的单价聚合（min/max/avg/samples）。
//
// 从 vendor_probe.sample_price_micro 每日 group 出来（Prober 60s 落一条）·
// samples 少的日子（<10）min/max 波动大 · 前端可自己判断是否显示。
type DailyPricePoint struct {
	Date       string `json:"date"`                // "2026-08-12"
	MinPrice   int64  `json:"min_price"`           // microunit · 单价链最外层前
	MaxPrice   int64  `json:"max_price"`
	AvgPrice   int64  `json:"avg_price"`
	SampleN    int    `json:"sample_n"`            // 当日探针成功样本数
	FirstSeen  string `json:"first_seen_at"`
	LastSeen   string `json:"last_seen_at"`
}

// DailyPricesForVendor · 拉单 vendor 过去 days 天的每日高低。
//
// 用于 /prices 页动态曲线 · 前端画每天蜡烛图（low-high 区间 + avg 点）。
// samples 少的日子返 0 值 · 由前端决定要不要画（视觉降级）。
func (s *ProbeStore) DailyPricesForVendor(ctx context.Context, vendorID string, days int) ([]DailyPricePoint, error) {
	if s.db == nil {
		return nil, nil
	}
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)

	rows, err := s.db.QueryContext(ctx, `
		SELECT DATE(probed_at) as day,
		       MIN(sample_price_micro) as min_p,
		       MAX(sample_price_micro) as max_p,
		       AVG(sample_price_micro) as avg_p,
		       COUNT(*) as n,
		       MIN(probed_at) as first_seen,
		       MAX(probed_at) as last_seen
		  FROM vendor_probe
		 WHERE vendor_id = ?
		   AND sample_price_micro IS NOT NULL AND sample_price_micro > 0
		   AND probed_at >= ?
		 GROUP BY day
		 ORDER BY day ASC`, vendorID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DailyPricePoint
	for rows.Next() {
		var (
			p              DailyPricePoint
			minP, maxP     sql.NullInt64
			avgP           sql.NullFloat64
			firstS, lastS  sql.NullString
		)
		if err := rows.Scan(&p.Date, &minP, &maxP, &avgP, &p.SampleN, &firstS, &lastS); err != nil {
			return nil, err
		}
		p.MinPrice = minP.Int64
		p.MaxPrice = maxP.Int64
		p.AvgPrice = int64(avgP.Float64 + 0.5)
		p.FirstSeen = firstS.String
		p.LastSeen = lastS.String
		out = append(out, p)
	}
	return out, rows.Err()
}
