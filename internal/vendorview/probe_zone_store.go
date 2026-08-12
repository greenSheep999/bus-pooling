package vendorview

// probe_zone_store · vendor_probe_zone 侧表读写（migration 029）
//
// **为什么侧表**：vendor_probe 每探针一行 · 8 新列采样首个 zone · US/EU 差价压平。
// 侧表逐 zone 落 · PricedFor(vendor, region) 精确到区（docs/18 §5 未收口补齐）。

import (
	"context"
	"database/sql"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ProbeZoneSample · 侧表一行 · 一个 zone 的定价快照
type ProbeZoneSample struct {
	VendorID       string
	ProbedAt       time.Time
	Zone           string // 归一后的 · providers.ZoneOf(raw) · us / eu / ''
	Region         string // vendor 原文 · 便于对账
	Available      int
	VendorCurrency string
	VendorUnitRaw  int64
	OurUnitCredits int64
	OurUnitSource  string
}

// ProbeZoneStore
type ProbeZoneStore struct{ db *sql.DB }

func NewProbeZoneStore(db *sql.DB) *ProbeZoneStore { return &ProbeZoneStore{db: db} }

// InsertBatch · 一次探针一批 zone · 一个事务落库 · 幂等（PRIMARY KEY 冲突走 REPLACE）
//
// 为什么 REPLACE 而不是 IGNORE：探针可能因为超时/重试拿到不同的价 · 后写的更权威。
func (s *ProbeZoneStore) InsertBatch(ctx context.Context, zones []ProbeZoneSample) error {
	if s == nil || s.db == nil || len(zones) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO vendor_probe_zone
		  (vendor_id, probed_at, zone, region,
		   available, vendor_currency, vendor_unit_raw,
		   our_unit_credits, our_unit_source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, z := range zones {
		if _, err := stmt.ExecContext(ctx,
			z.VendorID,
			z.ProbedAt.UTC().Format(time.RFC3339Nano),
			z.Zone,
			nullIfEmpty(z.Region),
			nullIfZeroInt64(int64(z.Available)),
			nullIfEmpty(z.VendorCurrency),
			nullIfZeroInt64(z.VendorUnitRaw),
			nullIfZeroInt64(z.OurUnitCredits),
			nullIfEmpty(z.OurUnitSource),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LatestZoneCredits · 读该 vendor + zone 最近一条有价的积分
//
// **zone 参数是归一后的**（"us" / "eu" / ""）· 上游要先过 providers.ZoneOf。
// zone 空表示"任意" —— 找该 vendor 最近一条有价的（跨 zone · 保 PricedFor 老兼容）。
func (s *ProbeZoneStore) LatestZoneCredits(
	ctx context.Context, vendorID string, zone providers.Zone,
) (credits int64, probedAt time.Time, found bool) {
	if s == nil || s.db == nil {
		return 0, time.Time{}, false
	}
	var (
		q    string
		args []any
	)
	if zone == "" {
		q = `SELECT our_unit_credits, probed_at FROM vendor_probe_zone
		      WHERE vendor_id = ? AND our_unit_credits IS NOT NULL AND our_unit_credits > 0
		      ORDER BY probed_at DESC LIMIT 1`
		args = []any{vendorID}
	} else {
		q = `SELECT our_unit_credits, probed_at FROM vendor_probe_zone
		      WHERE vendor_id = ? AND zone = ?
		        AND our_unit_credits IS NOT NULL AND our_unit_credits > 0
		      ORDER BY probed_at DESC LIMIT 1`
		args = []any{vendorID, string(zone)}
	}

	var c sql.NullInt64
	var at string
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&c, &at); err != nil {
		return 0, time.Time{}, false
	}
	if !c.Valid || c.Int64 <= 0 {
		return 0, time.Time{}, false
	}
	t, _ := time.Parse(time.RFC3339Nano, at)
	return c.Int64, t, true
}

// PurgeOlderThan · 跟 vendor_probe 那个的保留策略对齐 · janitor 每天调
func (s *ProbeZoneStore) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM vendor_probe_zone WHERE probed_at < ?`,
		cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
