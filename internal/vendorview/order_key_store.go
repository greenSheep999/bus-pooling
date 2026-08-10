package vendorview

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// OrderKeyStore 负责读写 vendor_order + vendor_key 两张表。
//
// 数据是 backfill 拉进来的 vendor 侧历史（订单 + key 生命周期）·
// 是 /status 页（脱敏画图）+ /prices 页（价格分析）的共同数据源。
type OrderKeyStore struct {
	db *sql.DB
}

func NewOrderKeyStore(db *sql.DB) *OrderKeyStore {
	return &OrderKeyStore{db: db}
}

// UpsertOrders 幂等写入订单批量 · 同 (vendor_id, vendor_order_id) 存在则覆盖。
// backfill 每次全量拉一遍 · 拿到最新的 warranty / probe 状态。
func (s *OrderKeyStore) UpsertOrders(ctx context.Context, vendorID string, orders []providers.VendorOrder) error {
	if s.db == nil || len(orders) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vendor_order (
			vendor_id, vendor_order_id, created_at, purchased, requested,
			unit_price_micro, total_cost_micro, source, fetched_at, raw
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vendor_id, vendor_order_id) DO UPDATE SET
			purchased        = excluded.purchased,
			requested        = excluded.requested,
			unit_price_micro = excluded.unit_price_micro,
			total_cost_micro = excluded.total_cost_micro,
			source           = excluded.source,
			fetched_at       = excluded.fetched_at,
			raw              = excluded.raw
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, o := range orders {
		_, err := stmt.ExecContext(ctx,
			vendorID, o.VendorOrderID, o.CreatedAt.UTC().Format(time.RFC3339),
			o.Purchased, nullIfZero(o.Requested),
			nullIfZeroInt64(o.UnitPrice.Amount),
			nullIfZeroInt64(o.TotalCost.Amount),
			nullIfEmpty(o.Source),
			now, o.Raw,
		)
		if err != nil {
			return fmt.Errorf("upsert vendor_order: %w", err)
		}
	}
	return tx.Commit()
}

// UpsertKeys 幂等写入 key 生命周期 · 同 (vendor_id, vendor_key_id) 存在则覆盖。
// 重要字段（status / dead_at / current_usage）随每次 backfill 更新到最新。
func (s *OrderKeyStore) UpsertKeys(ctx context.Context, vendorID string, keys []providers.VendorKey) error {
	if s.db == nil || len(keys) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vendor_key (
			vendor_id, vendor_key_id, order_id, key_masked, region, status,
			created_at, dispatched_at, dead_at, dead_reason,
			last_probe_at, current_usage, usage_limit, warranty_until,
			unit_price_micro, fetched_at, raw
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vendor_id, vendor_key_id) DO UPDATE SET
			order_id         = excluded.order_id,
			key_masked       = excluded.key_masked,
			region           = excluded.region,
			status           = excluded.status,
			dispatched_at    = excluded.dispatched_at,
			dead_at          = excluded.dead_at,
			dead_reason      = excluded.dead_reason,
			last_probe_at    = excluded.last_probe_at,
			current_usage    = excluded.current_usage,
			usage_limit      = excluded.usage_limit,
			warranty_until   = excluded.warranty_until,
			unit_price_micro = excluded.unit_price_micro,
			fetched_at       = excluded.fetched_at,
			raw              = excluded.raw
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, k := range keys {
		_, err := stmt.ExecContext(ctx,
			vendorID, k.VendorKeyID,
			nullIfEmpty(k.OrderID), nullIfEmpty(k.KeyMasked),
			nullIfEmpty(k.Region), k.Status,
			k.CreatedAt.UTC().Format(time.RFC3339),
			nullTimeStr(k.DispatchedAt), nullTimeStr(k.DeadAt),
			nullIfEmpty(k.DeadReason), nullTimeStr(k.LastProbeAt),
			nullIfZero(k.CurrentUsage), nullIfZero(k.UsageLimit),
			nullTimeStr(k.WarrantyUntil),
			nullIfZeroInt64(k.UnitPrice.Amount),
			now, k.Raw,
		)
		if err != nil {
			return fmt.Errorf("upsert vendor_key: %w", err)
		}
	}
	return tx.Commit()
}

// UpsertDispatches 幂等写入"最近开号"批次 · 同 (vendor_id, dispatch_key) 存在则覆盖
// alive/dead 是变动字段 · 每次 backfill 覆盖到最新（vendor 侧探测更新了 alive/dead 数）
func (s *OrderKeyStore) UpsertDispatches(ctx context.Context, vendorID string, ds []providers.VendorDispatch) error {
	if s.db == nil || len(ds) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vendor_dispatch (
			vendor_id, dispatch_key, region, dispatched_at,
			count, alive, dead, dead_at, status, fetched_at, raw
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vendor_id, dispatch_key) DO UPDATE SET
			region        = excluded.region,
			dispatched_at = excluded.dispatched_at,
			count         = excluded.count,
			alive         = excluded.alive,
			dead          = excluded.dead,
			dead_at       = excluded.dead_at,
			status        = excluded.status,
			fetched_at    = excluded.fetched_at,
			raw           = excluded.raw
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, d := range ds {
		_, err := stmt.ExecContext(ctx,
			vendorID, d.DispatchKey,
			nullIfEmpty(d.Region),
			d.DispatchedAt.UTC().Format(time.RFC3339),
			d.Count, nullIfZero(d.Alive), nullIfZero(d.Dead),
			nullTimeStr(d.DeadAt), nullIfEmpty(d.Status),
			now, d.Raw,
		)
		if err != nil {
			return fmt.Errorf("upsert vendor_dispatch: %w", err)
		}
	}
	return tx.Commit()
}

// DispatchSummary vendor 平台"最近开号"汇总 · 用于 status 页每张卡上一行硬数据
type DispatchSummary struct {
	// RecentBatches 最近 N 批（默认 20 · 按时间倒序）
	RecentBatches []providers.VendorDispatch
	// TotalBatches 表里这家 vendor 的总批次数
	TotalBatches int
	// AvgIntervalMin 相邻批次的平均间隔（分钟）· 只在 batches>=2 时算
	AvgIntervalMin float64
	// LastDispatchAt 最新一批时间
	LastDispatchAt time.Time
	// TotalKeysDispatched 所有批次 count 累加（fleet-wide 累计）
	TotalKeysDispatched int
}

// DispatchSummary 汇总。
func (s *OrderKeyStore) DispatchSummary(ctx context.Context, vendorID string, recentN int) (*DispatchSummary, error) {
	if s.db == nil {
		return nil, nil
	}
	if recentN <= 0 {
		recentN = 20
	}

	out := &DispatchSummary{}
	// 总数 + total count + last dispatch
	var lastStr sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(count), 0), COALESCE(MAX(dispatched_at), '')
		  FROM vendor_dispatch WHERE vendor_id = ?
	`, vendorID).Scan(&out.TotalBatches, &out.TotalKeysDispatched, &lastStr)
	if err != nil {
		return nil, err
	}
	if lastStr.String != "" {
		if t, e := time.Parse(time.RFC3339, lastStr.String); e == nil {
			out.LastDispatchAt = t
		}
	}
	if out.TotalBatches == 0 {
		return out, nil
	}

	// 最近 N 批
	rows, err := s.db.QueryContext(ctx, `
		SELECT dispatch_key, region, dispatched_at, count,
		       COALESCE(alive, 0), COALESCE(dead, 0), COALESCE(dead_at, ''),
		       COALESCE(status, '')
		  FROM vendor_dispatch
		 WHERE vendor_id = ?
		 ORDER BY dispatched_at DESC
		 LIMIT ?
	`, vendorID, recentN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d providers.VendorDispatch
		var region sql.NullString
		var dispatchedAt, deadAt, status string
		if err := rows.Scan(
			&d.DispatchKey, &region, &dispatchedAt, &d.Count,
			&d.Alive, &d.Dead, &deadAt, &status,
		); err != nil {
			return nil, err
		}
		if region.Valid {
			d.Region = region.String
		}
		if t, e := time.Parse(time.RFC3339, dispatchedAt); e == nil {
			d.DispatchedAt = t
		}
		if deadAt != "" {
			if t, e := time.Parse(time.RFC3339, deadAt); e == nil {
				d.DeadAt = t
			}
		}
		d.Status = status
		out.RecentBatches = append(out.RecentBatches, d)
	}

	// 平均间隔：从表里查最近 20 批，用相邻差
	if len(out.RecentBatches) >= 2 {
		var totalSec float64
		for i := 0; i < len(out.RecentBatches)-1; i++ {
			// batches 是倒序 · [i] 更新 · [i+1] 更旧
			gap := out.RecentBatches[i].DispatchedAt.Sub(out.RecentBatches[i+1].DispatchedAt).Seconds()
			if gap > 0 {
				totalSec += gap
			}
		}
		if pairs := float64(len(out.RecentBatches) - 1); pairs > 0 {
			out.AvgIntervalMin = totalSec / pairs / 60
		}
	}

	return out, rows.Err()
}

// KeyLifecycleBucket 一小时（或 windowMinutes）时间桶的聚合 · 用于 /status 页画图。
type KeyLifecycleBucket struct {
	BucketStart   string // RFC3339 UTC · 桶起点
	KeysBorn      int    // 桶内新发的 key 数（按 CreatedAt 落桶）
	KeysDied      int    // 桶内挂掉的 key 数（按 DeadAt 落桶）
	AvgLifespanSec int64 // 桶内挂掉的 key 平均寿命秒数（DeadAt - CreatedAt · 只对 KeysDied 算）
}

// KeyLifecycleBuckets 按 bucketMinutes 分桶聚合 vendor 过去 windowHours 的 key 生命周期。
// 用于 /status 页画"每小时新发多少 · 挂多少 · 平均寿命"曲线。
func (s *OrderKeyStore) KeyLifecycleBuckets(
	ctx context.Context, vendorID string, windowHours, bucketMinutes int,
) ([]KeyLifecycleBucket, error) {
	if s.db == nil {
		return nil, nil
	}
	if windowHours <= 0 {
		windowHours = 24
	}
	if bucketMinutes <= 0 {
		bucketMinutes = 60
	}
	cutoff := time.Now().UTC().Add(-time.Duration(windowHours) * time.Hour).Format(time.RFC3339)
	bucketSeconds := bucketMinutes * 60

	// 分两个 CTE · 一个统计 born · 一个统计 died · 再 outer join
	rows, err := s.db.QueryContext(ctx, `
		WITH born AS (
			SELECT CAST(strftime('%s', created_at) AS INTEGER) / ? AS bucket_no,
			       COUNT(*) AS n
			  FROM vendor_key
			 WHERE vendor_id = ? AND created_at >= ?
			 GROUP BY bucket_no
		),
		died AS (
			SELECT CAST(strftime('%s', dead_at) AS INTEGER) / ? AS bucket_no,
			       COUNT(*) AS n,
			       AVG(strftime('%s', dead_at) - strftime('%s', created_at)) AS avg_life
			  FROM vendor_key
			 WHERE vendor_id = ? AND dead_at IS NOT NULL AND dead_at >= ?
			 GROUP BY bucket_no
		),
		all_buckets AS (
			SELECT bucket_no FROM born
			UNION
			SELECT bucket_no FROM died
		)
		SELECT ab.bucket_no,
		       COALESCE(b.n, 0) AS born,
		       COALESCE(d.n, 0) AS died,
		       COALESCE(d.avg_life, 0) AS avg_life
		  FROM all_buckets ab
		  LEFT JOIN born b ON ab.bucket_no = b.bucket_no
		  LEFT JOIN died d ON ab.bucket_no = d.bucket_no
		 ORDER BY ab.bucket_no ASC
	`, bucketSeconds, vendorID, cutoff, bucketSeconds, vendorID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []KeyLifecycleBucket
	for rows.Next() {
		var (
			bucketNo int64
			born     int
			died     int
			avgLife  float64
		)
		if err := rows.Scan(&bucketNo, &born, &died, &avgLife); err != nil {
			return nil, err
		}
		bucketStart := time.Unix(bucketNo*int64(bucketSeconds), 0).UTC()
		out = append(out, KeyLifecycleBucket{
			BucketStart:    bucketStart.Format(time.RFC3339),
			KeysBorn:       born,
			KeysDied:       died,
			AvgLifespanSec: int64(avgLife),
		})
	}
	return out, rows.Err()
}

// VendorHistorySummary 单家 vendor 的历史汇总（累计 · 独立于 24h 窗口）。
type VendorHistorySummary struct {
	TotalOrders     int
	TotalKeys       int
	ActiveKeys      int
	DeadKeys        int
	AvgLifespanSec  int64 // 已挂 key 的平均寿命
	LastOrderAt     time.Time
	FirstOrderAt    time.Time
}

// HistorySummary 单家 vendor 累计视图。
func (s *OrderKeyStore) HistorySummary(ctx context.Context, vendorID string) (*VendorHistorySummary, error) {
	if s.db == nil {
		return nil, nil
	}
	out := &VendorHistorySummary{}
	// order 层
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(MIN(created_at), ''), COALESCE(MAX(created_at), '')
		  FROM vendor_order WHERE vendor_id = ?
	`, vendorID).Scan(&out.TotalOrders, new(string), new(string))
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	var firstStr, lastStr string
	_ = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MIN(created_at), ''), COALESCE(MAX(created_at), '')
		  FROM vendor_order WHERE vendor_id = ?
	`, vendorID).Scan(&firstStr, &lastStr)
	if firstStr != "" {
		if t, e := time.Parse(time.RFC3339, firstStr); e == nil {
			out.FirstOrderAt = t
		}
	}
	if lastStr != "" {
		if t, e := time.Parse(time.RFC3339, lastStr); e == nil {
			out.LastOrderAt = t
		}
	}
	// key 层：总数 / active / dead / avg_life
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END),
		       SUM(CASE WHEN status = 'dead'   THEN 1 ELSE 0 END),
		       COALESCE(AVG(CASE WHEN dead_at IS NOT NULL
		           THEN strftime('%s', dead_at) - strftime('%s', created_at) END), 0)
		  FROM vendor_key WHERE vendor_id = ?
	`, vendorID).Scan(&out.TotalKeys, &out.ActiveKeys, &out.DeadKeys, &out.AvgLifespanSec)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return out, nil
}

// ── 小工具 ──

func nullTimeStr(t time.Time) sql.NullString {
	if t.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(time.RFC3339), Valid: true}
}
