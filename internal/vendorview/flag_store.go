package vendorview

import (
	"context"
	"database/sql"
	"time"
)

// FlagStore 读写 xi8_vendor_flags 表（migration 034）· 聚合源的 buyable/blocked/floating。
//
// 用途：抢号 fire-guard（docs/20 §3）· blocked 的别 fire。**纯内部**（CLAUDE.md §0.1）。
type FlagStore struct {
	db *sql.DB
	// staleAfter · flag 超过这个时长没更新就不信（默认 30min）。
	// xi8 backfiller 5min 拉一轮 · 30min 没更新说明 xi8 挂了或没数据 · 这时 fail-open。
	staleAfter time.Duration
}

func NewFlagStore(db *sql.DB) *FlagStore {
	return &FlagStore{db: db, staleAfter: 30 * time.Minute}
}

// VendorFlag · 一个 vendor+zone 的最新 flag 快照
type VendorFlag struct {
	VendorID    string
	Zone        string
	Buyable     bool
	Blocked     bool
	BlockReason string
	Floating    bool
	PriceFen    int
}

// UpsertFlags 幂等写最新快照 · 同 (vendor_id, zone) 覆盖。
func (s *FlagStore) UpsertFlags(ctx context.Context, flags []VendorFlag) error {
	if s.db == nil || len(flags) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO xi8_vendor_flags
			(vendor_id, zone, buyable, blocked, block_reason, floating, price_fen, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vendor_id, zone) DO UPDATE SET
			buyable      = excluded.buyable,
			blocked      = excluded.blocked,
			block_reason = excluded.block_reason,
			floating     = excluded.floating,
			price_fen    = excluded.price_fen,
			updated_at   = excluded.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, f := range flags {
		if _, err := stmt.ExecContext(ctx,
			f.VendorID, f.Zone, b2i(f.Buyable), b2i(f.Blocked),
			nullIfEmpty(f.BlockReason), b2i(f.Floating),
			nullIfZero(f.PriceFen), now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// IsBlocked · 抢号 fire 前查 · 实现 stockwatch.BlockGuard。
//
// 返回 blocked=true 只在：**有新鲜数据** 且 blocked=1。
//   - 查不到该 vendor+zone（xi8 不覆盖 / 没数据）→ 不拦（fail-open）
//   - 数据太旧（> staleAfter）→ 不拦（xi8 可能挂了 · 别误伤）
//
// zone 空 = 按 vendor 任一 zone blocked 就算（保守：任一区停售就别抢该 vendor）。
func (s *FlagStore) IsBlocked(ctx context.Context, vendorID, zone string) (blocked bool, reason string) {
	if s.db == nil || vendorID == "" {
		return false, ""
	}
	cutoff := time.Now().UTC().Add(-s.staleAfter).Format(time.RFC3339)

	var q string
	var args []any
	if zone == "" {
		q = `SELECT block_reason FROM xi8_vendor_flags
		      WHERE vendor_id = ? AND blocked = 1 AND updated_at >= ? LIMIT 1`
		args = []any{vendorID, cutoff}
	} else {
		q = `SELECT block_reason FROM xi8_vendor_flags
		      WHERE vendor_id = ? AND zone = ? AND blocked = 1 AND updated_at >= ? LIMIT 1`
		args = []any{vendorID, zone, cutoff}
	}
	var r sql.NullString
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&r)
	if err != nil {
		return false, "" // ErrNoRows 或查询错 · 一律不拦（fail-open）
	}
	return true, r.String
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
