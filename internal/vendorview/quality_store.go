package vendorview

// quality_store · 从 vendor_key 表聚合每家 vendor 的号寿命/存活率（Task 65 · 2026-08-14）
//
// **背景**：AutoPick 打分公式 aliveRate/100*0.6 + (1-price/max)*0.4 · aliveRate 老代码
// 恒 50.0（数据没采集）· 等价纯价格排序 —— 用户点名说"号寿命/用量卡着比价 fallback"。
//
// **现在**：vendor_key 表有 created_at + dead_at + status（migration 023 起就有 · 但没人聚合）·
// 加个聚合读方法喂进 AutoPick。
//
// **聚合窗口**：30d · 太短单一坏号影响过大 · 太长陈旧值拖分。
//
// **两个指标**：
//   AvgLifespanSeconds · 死号的平均寿命（dead_at - created_at）· 越高越好
//   AliveRate30d       · 30d 内产出的号里此刻仍 alive 的比例（0-100）· 越高越好
//
// **数据不足时**（vendor_key 空 / 死号 <3）：返 (0, 0, false) · 上层降级 50/0 兜底。

import (
	"context"
	"database/sql"
	"time"
)

// QualityStore · 号质量聚合读
type QualityStore struct {
	db *sql.DB
}

func NewQualityStore(db *sql.DB) *QualityStore {
	return &QualityStore{db: db}
}

// QualityStats · 单家 vendor 的号质量聚合
type QualityStats struct {
	VendorID           string
	AvgLifespanSeconds int64
	AliveRate30d       int // 0-100
	// AvgCreditsUsed 死号平均已耗额度（vendor_key.current_usage · 原始 0-10k 档 · 非 microunit）·
	// 语义 = "这家的号平均烧到多少积分才挂" · 越大越耐用（Overview 耐用度 Meter）
	AvgCreditsUsed int64
	// WarrantyDeaths 死在质保期内的号数（dead_at <= warranty_until）· Overview「保修」列
	WarrantyDeaths int
	// SampleSize 参与聚合的死号数 · < 3 视为数据不足（返 ok=false）
	SampleSize int
}

// Get · 拿某家 vendor 30d 内号质量。数据不足返 (nil, false, nil)（不是错）。
//
// SQL 只查 vendor_key · 不 join · 快。窗口硬编码 30d · 后续可参数化。
func (s *QualityStore) Get(ctx context.Context, vendorID string) (*QualityStats, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, nil
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)

	// 一次 SQL 拿死号平均寿命 + 死号数量 + alive 数量 + 死号平均耗额 + 质保期内死亡数
	// COALESCE 保护空表（AVG 恒空 · SUM 也空）
	// **CAST 是必需的** —— SQLite julianday * 86400 是 float · 转 INTEGER 避免精度漂
	q := `
	SELECT
	  COALESCE(CAST(AVG(
	    CASE WHEN status = 'dead' AND dead_at IS NOT NULL
	         THEN (julianday(dead_at) - julianday(created_at)) * 86400
	    END
	  ) AS INTEGER), 0) AS avg_lifespan_seconds,
	  COALESCE(CAST(AVG(
	    CASE WHEN status = 'dead' THEN current_usage END
	  ) AS INTEGER), 0) AS avg_credits_used,
	  SUM(CASE WHEN status = 'dead'
	           AND dead_at IS NOT NULL AND warranty_until IS NOT NULL
	           AND dead_at <= warranty_until THEN 1 ELSE 0 END) AS warranty_deaths,
	  SUM(CASE WHEN status = 'dead' THEN 1 ELSE 0 END) AS dead_count,
	  SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) AS alive_count,
	  COUNT(*) AS total
	  FROM vendor_key
	 WHERE vendor_id = ?
	   AND created_at >= ?
	`
	var avgLife, avgCredits int64
	var warrantyDeaths, dead, alive, total int
	err := s.db.QueryRowContext(ctx, q, vendorID, cutoff).
		Scan(&avgLife, &avgCredits, &warrantyDeaths, &dead, &alive, &total)
	if err != nil {
		return nil, false, err
	}
	// 至少要有 3 个死号才有意义（1 个死号平均寿命是它自己 · 抖动大）
	if dead < 3 {
		return nil, false, nil
	}
	rate := 0
	if total > 0 {
		rate = alive * 100 / total
	}
	return &QualityStats{
		VendorID:           vendorID,
		AvgLifespanSeconds: avgLife,
		AliveRate30d:       rate,
		AvgCreditsUsed:     avgCredits,
		WarrantyDeaths:     warrantyDeaths,
		SampleSize:         dead,
	}, true, nil
}
