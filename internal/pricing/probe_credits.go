package pricing

// probe_credits · 读 vendor_probe_zone.our_unit_credits（migration 029 · docs/18 §1.4 权威源）·
// 侧表缺数据时退回主表 vendor_probe.our_unit_credits（首个 zone 采样 · 单 zone vendor 够用）
//
// **fallback 链**（按 source 权威度 · 每级内部再按 zone 精确度）：
//   1. 侧表 · 按 zone 精确匹配 · source=vendor_self（我方直连上游 · 最权威）
//   2. 侧表 · 按 zone 精确匹配 · source=xi8（聚合源实时快照 · 补上游单端只给一区的空缺）
//   3. 侧表 · 按 zone 精确匹配 · source=xi8_notif（聚合源历史通知 · 补探针上线前的空窗）
//   4-6. 同上三级 · 但跨 zone 取最近一条（单 zone vendor / 指定 zone 无价时）
//   7. 侧表无 source 过滤（migration 030 之前的老数据）
//   8. `vendor_probe` 主表（029 刚上 / 老装配 / 侧表未回填的历史窗口）
//
// **schema 说明**：`vendor_probe_zone` 主键 (vendor_id, probed_at, zone) · 一探针 N 行 ·
// 主表 (vendor_id, probed_at) 保留 · 首 zone 采样兼容老读取。

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ProbeCredits · vendor_probe / vendor_probe_zone 的积分读取器
type ProbeCredits struct{ db *sql.DB }

func NewProbeCredits(db *sql.DB) *ProbeCredits { return &ProbeCredits{db: db} }

// LatestCredits · 该 vendor + zone 最近一条有效积分单价（microunit）+ 探测时刻。
//
// zone 参数：归一后的值（"us" / "eu" / ""）· 上游要先过 providers.ZoneOf。
// zone 空 = 跨 zone 找该 vendor 最近一条（单 zone vendor / 老调用点兼容）。
//
// found=false 的两种情况（调用方自己决定怎么兜）：
//   - 从没探到过（刚接入 / 长期断线）
//   - 探到的都是 0（vendor 一直缺货 · 没单价可采）
func (p *ProbeCredits) LatestCredits(
	ctx context.Context, vendorID string, zone string,
) (credits int64, probedAt time.Time, found bool) {
	if p == nil || p.db == nil {
		return 0, time.Time{}, false
	}

	// ① 侧表按 zone 精确匹配 · 优先 vendor_self（migration 030）
	if zone != "" {
		if c, at, ok := p.lookupZone(ctx, vendorID, zone, "vendor_self"); ok {
			return c, at, true
		}
		if c, at, ok := p.lookupZone(ctx, vendorID, zone, "xi8"); ok {
			return c, at, true
		}
		if c, at, ok := p.lookupZone(ctx, vendorID, zone, "xi8_notif"); ok {
			return c, at, true
		}
	}
	// ② 侧表跨 zone 最近一条（vendor_self 优先 · 再聚合源实时 · 再聚合源历史）
	if c, at, ok := p.lookupZone(ctx, vendorID, "", "vendor_self"); ok {
		return c, at, true
	}
	if c, at, ok := p.lookupZone(ctx, vendorID, "", "xi8"); ok {
		return c, at, true
	}
	if c, at, ok := p.lookupZone(ctx, vendorID, "", "xi8_notif"); ok {
		return c, at, true
	}
	// ③ 侧表无 source 过滤（migration 030 前老数据）
	if c, at, ok := p.lookupZone(ctx, vendorID, "", ""); ok {
		return c, at, true
	}
	// ④ 主表首 zone 采样（029 刚上 · 探针还没跑完一轮 · 或历史行侧表未回填）
	return p.lookupMain(ctx, vendorID)
}

// lookupZone · 侧表 · zone 空表示不加 zone 过滤 · source 空表示不加 source 过滤
func (p *ProbeCredits) lookupZone(ctx context.Context, vendorID, zone, source string) (int64, time.Time, bool) {
	q := `SELECT our_unit_credits, probed_at FROM vendor_probe_zone
	       WHERE vendor_id = ? AND our_unit_credits IS NOT NULL AND our_unit_credits > 0`
	args := []any{vendorID}
	if zone != "" {
		q += ` AND zone = ?`
		args = append(args, zone)
	}
	if source != "" {
		q += ` AND source = ?`
		args = append(args, source)
	}
	q += ` ORDER BY probed_at DESC LIMIT 1`
	var c sql.NullInt64
	var at string
	err := p.db.QueryRowContext(ctx, q, args...).Scan(&c, &at)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, time.Time{}, false
		}
		return 0, time.Time{}, false
	}
	if !c.Valid || c.Int64 <= 0 {
		return 0, time.Time{}, false
	}
	t, _ := time.Parse(time.RFC3339Nano, at)
	return c.Int64, t, true
}

// lookupMain · 主表 · 侧表没数据时的兜底（首 zone 采样 · 单 zone vendor 也 OK）
func (p *ProbeCredits) lookupMain(ctx context.Context, vendorID string) (int64, time.Time, bool) {
	var c sql.NullInt64
	var at string
	err := p.db.QueryRowContext(ctx,
		`SELECT our_unit_credits, probed_at FROM vendor_probe
		  WHERE vendor_id = ? AND our_unit_credits IS NOT NULL AND our_unit_credits > 0
		  ORDER BY probed_at DESC LIMIT 1`, vendorID).Scan(&c, &at)
	if err != nil {
		return 0, time.Time{}, false
	}
	if !c.Valid || c.Int64 <= 0 {
		return 0, time.Time{}, false
	}
	t, _ := time.Parse(time.RFC3339Nano, at)
	return c.Int64, t, true
}
