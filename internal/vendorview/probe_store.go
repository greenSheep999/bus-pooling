package vendorview

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ProbeStore 负责读写 vendor_probe 和 vendor_daily 两张表。
//
// 分层原因：vendorview 里的其他文件（AutoPick / Prices / Stock）是**无状态视图**，
// 直接调 registry。probe 相关是**有状态**（写探测样本 + 读历史聚合），需要 db。
// 塞在同一个包里因为语义都是 "vendor 观测"（CLAUDE.md §4 · 不能新加包）。
type ProbeStore struct {
	db *sql.DB
}

// NewProbeStore db 允许 nil —— 未装配 db 时探针关闭，Status endpoint 会返回空
// （测试 / 只跑 API 层的场景走这条路径）。
func NewProbeStore(db *sql.DB) *ProbeStore {
	return &ProbeStore{db: db}
}

// ProbeSample 一次探测的结果 · 对应 vendor_probe 一行。
//
// 从 providers.StockSnapshot + Capability + 错误信息中提取，poller 里填。
type ProbeSample struct {
	VendorID          string
	ProbedAt          time.Time // UTC
	Alive             bool
	LatencyMs         int
	StockTotal        int
	StockByRegion     []RegionStock // 落 JSON 存
	WarrantyMinutes   int
	MaxPerOrder       int
	SamplePriceMicro  int64  // **DEPRECATED**（migration 028）· 沿用旧行为 · 值 = vendor 原始报价（可能任币种混着 · 语义不再准）
	SamplePriceRegion string // 采样的 zone id · 内部字段
	ErrorKind         string // 空 = 成功
	RawSnapshot       []byte // 完整 StockSnapshot JSON

	// ── pricing 标准化（docs/10-pricing §1.2 · migration 028）──
	//
	// 上游原样字段 · 拿到就存 · 没有则零值（SQL 层用 nullIfZero 转 NULL）：
	VendorCurrency     string  // credit / CNY / USD
	VendorUnitRaw      int64   // microunit · vendor 报价原值
	VendorExchangeRate float64 // vendor 侧汇率（UI 有 · API 无 · 保留字段）
	VendorPriceUSDRaw  int64   // USD 原值 microunit（部分 vendor 单独 USD 字段时填）
	VendorPriceCNYRaw  int64   // CNY 原值 microunit（UI 有 · API 无 · 保留字段）
	// 我方计算 · 唯一权威积分（docs/10-pricing §1.3 换算路径）：
	OurUnitCredits int64  // ★ microunit · 1_000_000 = 1 积分 = 1 RMB
	OurUnitSource  string // vendor_native / computed_from_usd / fallback_last_rate
	OurComputedAt  time.Time

	// PublicStatus 相关字段 · vendor 自报的 fleet 累计数据（可选 · vendor 不支持时全 nil）
	// 独立于 Alive/StockTotal —— 探针会同时打 Stock 和 PublicStatus 两个端点
	PSKeysActive    *int  // vendor 侧当前活跃 key
	PSKeysAlive     *int  // 部分 vendor 才有 · active + suspect
	PSKeysDead      *int  // vendor 侧当前失效 key
	PSKeysStock     *int  // vendor 侧当前可购买库存
	PSKeysSuspect   *int  // 部分 vendor 才有
	PSKeysTotal     *int  // 历史累计
	PSGenerating    *bool // vendor 是否正在生成新 key
	PSStartedAt     *time.Time
	PSUptimeSeconds *int64 // vendor 自报运行时长
	PSRaw           []byte // 原始 /api/status 响应
	PSErrorKind     string // PublicStatus 端点独立错误
}

// RegionStock 落 stock_by_region 字段的一条 entry
type RegionStock struct {
	// Zone · 归一后的地区标识（us / eu / general · providers.ZoneOf 出口）·
	// **stock-delta 对比的键用它** —— 部分 vendor 不返 region 原文（Region 恒空）·
	// 拿 Region 当键会让多 zone 塌成一条 · 整区的 delta 被漏掉。
	Zone           string `json:"zone"`
	Region         string `json:"region"`
	Available      int    `json:"available"`
	UnitPriceMicro int64  `json:"unit_price_micro,omitempty"`
}

// InsertProbe 落一条探测样本。db==nil 时是 no-op（便于测试）。
func (s *ProbeStore) InsertProbe(ctx context.Context, p ProbeSample) error {
	if s.db == nil {
		return nil
	}
	regionsJSON, _ := json.Marshal(p.StockByRegion)
	// PublicStatus 字段 · vendor 不支持时全 nil
	var (
		psStartedAt sql.NullString
		psGen       sql.NullInt64
	)
	if p.PSStartedAt != nil {
		psStartedAt = sql.NullString{String: p.PSStartedAt.UTC().Format(time.RFC3339), Valid: true}
	}
	if p.PSGenerating != nil {
		if *p.PSGenerating {
			psGen = sql.NullInt64{Int64: 1, Valid: true}
		} else {
			psGen = sql.NullInt64{Int64: 0, Valid: true}
		}
	}
	// 标准化字段（docs/10-pricing §1.3 · migration 028）· ComputedAt 空则用 ProbedAt
	computedAt := p.OurComputedAt
	if computedAt.IsZero() {
		computedAt = p.ProbedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vendor_probe (
			vendor_id, probed_at, alive, latency_ms, stock_total, stock_by_region,
			warranty_minutes, max_per_order, sample_price_micro, sample_price_region,
			error_kind, raw_snapshot,
			ps_keys_active, ps_keys_alive, ps_keys_dead, ps_keys_stock,
			ps_keys_suspect, ps_keys_total, ps_generating,
			ps_started_at, ps_uptime_seconds, ps_raw, ps_error_kind,
			vendor_currency, vendor_unit_raw, vendor_exchange_rate,
			vendor_price_usd_raw, vendor_price_cny_raw,
			our_unit_credits, our_unit_source, our_computed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		          ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		p.VendorID, p.ProbedAt.UTC().Format(time.RFC3339Nano),
		boolToInt(p.Alive), nullIfZero(p.LatencyMs), nullIfZero(p.StockTotal),
		string(regionsJSON), nullIfZero(p.WarrantyMinutes), nullIfZero(p.MaxPerOrder),
		nullIfZeroInt64(p.SamplePriceMicro), nullIfEmpty(p.SamplePriceRegion),
		nullIfEmpty(p.ErrorKind), p.RawSnapshot,
		nullIfNilInt(p.PSKeysActive), nullIfNilInt(p.PSKeysAlive),
		nullIfNilInt(p.PSKeysDead), nullIfNilInt(p.PSKeysStock),
		nullIfNilInt(p.PSKeysSuspect), nullIfNilInt(p.PSKeysTotal),
		psGen, psStartedAt, nullIfNilInt64(p.PSUptimeSeconds), p.PSRaw,
		nullIfEmpty(p.PSErrorKind),
		// migration 028 · pricing 标准化字段
		nullIfEmpty(p.VendorCurrency), nullIfZeroInt64(p.VendorUnitRaw),
		nullIfZeroFloat64(p.VendorExchangeRate),
		nullIfZeroInt64(p.VendorPriceUSDRaw), nullIfZeroInt64(p.VendorPriceCNYRaw),
		nullIfZeroInt64(p.OurUnitCredits), nullIfEmpty(p.OurUnitSource),
		computedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert vendor_probe: %w", err)
	}
	return nil
}

// LatestProbe 拿指定 vendor 的最近一条探测（用于 /status 页的"当前状态"字段）。
// 没样本时返回 nil, nil（不算错误 · 刚上线 / 长期没探到的正常情况）。
//
// 同时读 PublicStatus 字段（vendor 自报的 keys_active/keys_dead/keys_stock 等） ·
// 不支持 PublicStatus 的 vendor 这些字段 NULL，返回时保持 nil 指针。
func (s *ProbeStore) LatestProbe(ctx context.Context, vendorID string) (*ProbeSample, error) {
	if s.db == nil {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT probed_at, alive, latency_ms, stock_total, stock_by_region,
		       warranty_minutes, max_per_order, error_kind,
		       ps_keys_active, ps_keys_alive, ps_keys_dead, ps_keys_stock,
		       ps_keys_suspect, ps_keys_total, ps_generating,
		       ps_started_at, ps_uptime_seconds, ps_error_kind
		  FROM vendor_probe
		 WHERE vendor_id = ?
		 ORDER BY probed_at DESC
		 LIMIT 1
	`, vendorID)

	var (
		probedAt        string
		alive           int
		latencyMs       sql.NullInt64
		stockTotal      sql.NullInt64
		stockByRegion   sql.NullString
		warrantyMinutes sql.NullInt64
		maxPerOrder     sql.NullInt64
		errorKind       sql.NullString
		psActive        sql.NullInt64
		psAlive         sql.NullInt64
		psDead          sql.NullInt64
		psStock         sql.NullInt64
		psSuspect       sql.NullInt64
		psTotal         sql.NullInt64
		psGen           sql.NullInt64
		psStartedAt     sql.NullString
		psUptimeSec     sql.NullInt64
		psErrorKind     sql.NullString
	)
	if err := row.Scan(
		&probedAt, &alive, &latencyMs, &stockTotal, &stockByRegion,
		&warrantyMinutes, &maxPerOrder, &errorKind,
		&psActive, &psAlive, &psDead, &psStock,
		&psSuspect, &psTotal, &psGen,
		&psStartedAt, &psUptimeSec, &psErrorKind,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	t, _ := time.Parse(time.RFC3339Nano, probedAt)
	out := &ProbeSample{
		VendorID:        vendorID,
		ProbedAt:        t,
		Alive:           alive == 1,
		LatencyMs:       int(latencyMs.Int64),
		StockTotal:      int(stockTotal.Int64),
		WarrantyMinutes: int(warrantyMinutes.Int64),
		MaxPerOrder:     int(maxPerOrder.Int64),
		ErrorKind:       errorKind.String,
		PSErrorKind:     psErrorKind.String,
	}
	if stockByRegion.Valid && stockByRegion.String != "" {
		var regions []RegionStock
		if err := json.Unmarshal([]byte(stockByRegion.String), &regions); err == nil {
			out.StockByRegion = regions
		}
	}
	if psActive.Valid {
		v := int(psActive.Int64)
		out.PSKeysActive = &v
	}
	if psAlive.Valid {
		v := int(psAlive.Int64)
		out.PSKeysAlive = &v
	}
	if psDead.Valid {
		v := int(psDead.Int64)
		out.PSKeysDead = &v
	}
	if psStock.Valid {
		v := int(psStock.Int64)
		out.PSKeysStock = &v
	}
	if psSuspect.Valid {
		v := int(psSuspect.Int64)
		out.PSKeysSuspect = &v
	}
	if psTotal.Valid {
		v := int(psTotal.Int64)
		out.PSKeysTotal = &v
	}
	if psGen.Valid {
		b := psGen.Int64 == 1
		out.PSGenerating = &b
	}
	if psStartedAt.Valid && psStartedAt.String != "" {
		if pt, err := time.Parse(time.RFC3339, psStartedAt.String); err == nil {
			out.PSStartedAt = &pt
		}
	}
	if psUptimeSec.Valid {
		v := psUptimeSec.Int64
		out.PSUptimeSeconds = &v
	}
	return out, nil
}

// Uptime24h 返回 vendor 过去 24 小时的存活率（0.0-1.0）+ 样本数。
// 样本数 0 时返回 (0, 0) —— 上层判断是"没数据"而不是"100% 死"。
func (s *ProbeStore) Uptime24h(ctx context.Context, vendorID string) (pct float64, samples int, err error) {
	if s.db == nil {
		return 0, 0, nil
	}
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	row := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FILTER (WHERE alive = 1), COUNT(*)
		  FROM vendor_probe
		 WHERE vendor_id = ? AND probed_at >= ?
	`, vendorID, cutoff)
	var aliveCount, totalCount int
	if err := row.Scan(&aliveCount, &totalCount); err != nil {
		return 0, 0, err
	}
	if totalCount == 0 {
		return 0, 0, nil
	}
	return float64(aliveCount) / float64(totalCount), totalCount, nil
}

// StockoutMinutes24h 计算过去 24h 内库存 <= 0 的探测数 * 探测间隔（分钟）。
// 探测间隔默认 60s（从 config 传进来即可 · 目前先写死）。
func (s *ProbeStore) StockoutMinutes24h(ctx context.Context, vendorID string, probeIntervalSec int) (int, error) {
	if s.db == nil {
		return 0, nil
	}
	if probeIntervalSec <= 0 {
		probeIntervalSec = 60
	}
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	row := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM vendor_probe
		 WHERE vendor_id = ?
		   AND probed_at >= ?
		   AND alive = 1
		   AND (stock_total IS NULL OR stock_total <= 0)
	`, vendorID, cutoff)
	var stockoutSamples int
	if err := row.Scan(&stockoutSamples); err != nil {
		return 0, err
	}
	return stockoutSamples * probeIntervalSec / 60, nil
}

// Incidents7d 返回过去 7 天内 incident_flag=1 的日期（YYYY-MM-DD）。
func (s *ProbeStore) Incidents7d(ctx context.Context, vendorID string) ([]string, error) {
	return s.Incidents7dFrom(ctx, vendorID, time.Now())
}

// Incidents7dFrom 同 Incidents7d · 但以传入时刻为"今天"（测试注入 / 回放用）。
func (s *ProbeStore) Incidents7dFrom(ctx context.Context, vendorID string, now time.Time) ([]string, error) {
	if s.db == nil {
		return nil, nil
	}
	cutoff := now.UTC().AddDate(0, 0, -7).Format("2006-01-02")
	rows, err := s.db.QueryContext(ctx, `
		SELECT date FROM vendor_daily
		 WHERE vendor_id = ? AND date >= ? AND incident_flag = 1
		 ORDER BY date DESC
	`, vendorID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// TrendBucket 一个时间桶的聚合点（用于 /status 页 sparkline）。
type TrendBucket struct {
	// BucketStart 桶起点（UTC · RFC3339）· 前端 x 轴取值
	BucketStart string
	// AlivePct 0.0-1.0 · 桶内 alive 探测占比
	AlivePct float64
	// StockAvg 桶内库存均值（探测样本 stock_total 平均值）
	StockAvg float64
	// Samples 桶内探测样本数（<3 时前端可选不画）
	Samples int
}

// TrendBuckets 按 bucketMinutes 分桶聚合 vendor 过去 windowHours 小时的样本。
// 常规调用：TrendBuckets(ctx, vendorID, 24, 15) → 24h/15min → 96 个点。
//
// 用 SQLite 的 CAST(strftime('%s', probed_at)/60/N) 做时间桶 · 保证连续性。
// 无样本的桶会**缺失** —— 前端画折线时按 BucketStart 时间跳过。
func (s *ProbeStore) TrendBuckets(ctx context.Context, vendorID string, windowHours, bucketMinutes int) ([]TrendBucket, error) {
	if s.db == nil {
		return nil, nil
	}
	if windowHours <= 0 {
		windowHours = 24
	}
	if bucketMinutes <= 0 {
		bucketMinutes = 15
	}
	cutoff := time.Now().UTC().Add(-time.Duration(windowHours) * time.Hour).Format(time.RFC3339Nano)
	// 秒到桶：桶号 = floor(unix_seconds / (bucketMinutes*60))
	// 用桶号 * bucket 秒数 反算回 UTC 时间戳，得到桶起点
	bucketSeconds := bucketMinutes * 60
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			CAST(strftime('%s', probed_at) AS INTEGER) / ? AS bucket_no,
			AVG(CAST(alive AS REAL))                       AS alive_pct,
			AVG(COALESCE(stock_total, 0))                  AS stock_avg,
			COUNT(*)                                       AS samples
		  FROM vendor_probe
		 WHERE vendor_id = ? AND probed_at >= ?
		 GROUP BY bucket_no
		 ORDER BY bucket_no ASC
	`, bucketSeconds, vendorID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrendBucket
	for rows.Next() {
		var (
			bucketNo int64
			alivePct float64
			stockAvg float64
			samples  int
		)
		if err := rows.Scan(&bucketNo, &alivePct, &stockAvg, &samples); err != nil {
			return nil, err
		}
		bucketStart := time.Unix(bucketNo*int64(bucketSeconds), 0).UTC()
		out = append(out, TrendBucket{
			BucketStart: bucketStart.Format(time.RFC3339),
			AlivePct:    alivePct,
			StockAvg:    stockAvg,
			Samples:     samples,
		})
	}
	return out, rows.Err()
}

// PurgeProbeOlderThan 删除 vendor_probe 中 probed_at < cutoff 的行（保留策略）。
// 默认 30 天前的删掉。janitor 每天调一次即可。
func (s *ProbeStore) PurgeProbeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	if s.db == nil {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM vendor_probe WHERE probed_at < ?
	`, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ── 小工具 ──

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfZero(v int) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}

func nullIfZeroInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullIfEmpty(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullIfNilInt(p *int) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}

func nullIfNilInt64(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func nullIfZeroFloat64(v float64) sql.NullFloat64 {
	if v == 0 {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: v, Valid: true}
}
