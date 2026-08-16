package vendorview

import (
	"context"
	"database/sql"
	"sort"
	"time"
)

// HealthStore 读写 pipeline_health · 各数据管线的"最后成功/失败"心跳（migration 036）。
//
// 每条管线每轮跑完调 Mark 盖戳（成功刷 last_ok_at · 失败刷 last_err）· StalenessChecker
// 和 admin 端点读 Report 判断"谁多久没成功了"。**纯运维可观测**（CLAUDE.md §0.1）。
type HealthStore struct {
	db *sql.DB
}

func NewHealthStore(db *sql.DB) *HealthStore {
	return &HealthStore{db: db}
}

// PipelineHealthRow 一条管线的健康心跳（vendor_id=” = 全局管线）。
type PipelineHealthRow struct {
	VendorID  string
	Pipeline  string
	LastOKAt  time.Time // 零值 = 从没成功过
	LastErr   string
	LastErrAt time.Time
	UpdatedAt time.Time
}

// pipelineMaxAge 各管线预期"最久多没成功就算陈旧"（= 间隔 × 约 3 冗余）。
//   - probe 60s 循环 → 3min 没成功 = 有问题
//   - backfill 家族 5min 循环 → 15min 没成功 = 有问题
var pipelineMaxAge = map[string]time.Duration{
	"probe":      3 * time.Minute,
	"orders":     15 * time.Minute,
	"keys":       15 * time.Minute,
	"dispatch":   15 * time.Minute,
	"ledger":     15 * time.Minute,
	"qty_tiers":  15 * time.Minute,
	"time_decay": 15 * time.Minute,
}

const defaultPipelineMaxAge = 15 * time.Minute

// MaxAge 返回该管线的陈旧阈值。
func MaxAge(pipeline string) time.Duration {
	if d, ok := pipelineMaxAge[pipeline]; ok {
		return d
	}
	return defaultPipelineMaxAge
}

// Stale 判断这条心跳是否陈旧（从没成功过也算陈旧）。
func (r PipelineHealthRow) Stale(now time.Time) bool {
	if r.LastOKAt.IsZero() {
		return true
	}
	return now.Sub(r.LastOKAt) > MaxAge(r.Pipeline)
}

// Mark 盖一次心跳戳：err==nil 刷 last_ok_at · err!=nil 刷 last_err/last_err_at。
//
// 用 COALESCE 只动本次这一侧 —— 成功不抹掉"上次为啥挂过"的 last_err · 失败不抹掉
// last_ok_at（好判断"挂了多久 / 上次成功多久前"）。
func (s *HealthStore) Mark(ctx context.Context, vendorID, pipeline string, markErr error) error {
	if s == nil || s.db == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var okAt, errStr, errAt any
	if markErr == nil {
		okAt = now
	} else {
		msg := markErr.Error()
		if len(msg) > 300 {
			msg = msg[:300]
		}
		errStr = msg
		errAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pipeline_health (vendor_id, pipeline, last_ok_at, last_err, last_err_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(vendor_id, pipeline) DO UPDATE SET
			last_ok_at  = COALESCE(excluded.last_ok_at,  pipeline_health.last_ok_at),
			last_err    = COALESCE(excluded.last_err,    pipeline_health.last_err),
			last_err_at = COALESCE(excluded.last_err_at, pipeline_health.last_err_at),
			updated_at  = excluded.updated_at
	`, vendorID, pipeline, okAt, errStr, errAt, now)
	return err
}

// Report 读全部心跳 · 按 vendor_id + pipeline 排序（稳定输出）。
func (s *HealthStore) Report(ctx context.Context) ([]PipelineHealthRow, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT vendor_id, pipeline, last_ok_at, last_err, last_err_at, updated_at
		  FROM pipeline_health
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PipelineHealthRow
	for rows.Next() {
		var r PipelineHealthRow
		var okAt, errStr, errAt, updAt sql.NullString
		if err := rows.Scan(&r.VendorID, &r.Pipeline, &okAt, &errStr, &errAt, &updAt); err != nil {
			return nil, err
		}
		r.LastOKAt = parseRFC3339(okAt.String)
		r.LastErr = errStr.String
		r.LastErrAt = parseRFC3339(errAt.String)
		r.UpdatedAt = parseRFC3339(updAt.String)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].VendorID != out[j].VendorID {
			return out[i].VendorID < out[j].VendorID
		}
		return out[i].Pipeline < out[j].Pipeline
	})
	return out, nil
}

func parseRFC3339(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
