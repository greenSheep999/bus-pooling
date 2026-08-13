package vendorview

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// LedgerStore 读写 vendor_ledger 表（migration 033）· vendor 侧积分流水。
//
// 用途：交叉对账（docs/20 §1）· 拿 vendor 自报流水跟我方 pull_round 双向核对。
// **纯内部**（CLAUDE.md §0.1）· 不出前端。
type LedgerStore struct {
	db *sql.DB
}

func NewLedgerStore(db *sql.DB) *LedgerStore {
	return &LedgerStore{db: db}
}

// UpsertLedger 幂等写入流水批量 · 同 (vendor_id, entry_id) 存在则覆盖到最新
// （balance_after / reason 归一可能随重拉修正）。
func (s *LedgerStore) UpsertLedger(ctx context.Context, vendorID string, entries []providers.VendorLedgerEntry) error {
	if s.db == nil || len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO vendor_ledger (
			vendor_id, entry_id, order_id, reason, raw_reason,
			amount_micro, balance_after, created_at, fetched_at, raw
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vendor_id, entry_id) DO UPDATE SET
			order_id      = excluded.order_id,
			reason        = excluded.reason,
			raw_reason    = excluded.raw_reason,
			amount_micro  = excluded.amount_micro,
			balance_after = excluded.balance_after,
			created_at    = excluded.created_at,
			fetched_at    = excluded.fetched_at,
			raw           = excluded.raw
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range entries {
		if e.EntryID == "" {
			continue // 无幂等键的行跳过 · adapter 应保证合成指纹
		}
		_, err := stmt.ExecContext(ctx,
			vendorID, e.EntryID, nullIfEmpty(e.OrderID),
			e.Reason, nullIfEmpty(e.RawReason),
			e.Amount.Amount, nullIfZeroInt64(e.BalanceAfter.Amount),
			e.CreatedAt.UTC().Format(time.RFC3339), now, e.Raw,
		)
		if err != nil {
			return fmt.Errorf("upsert vendor_ledger: %w", err)
		}
	}
	return tx.Commit()
}

// LedgerByOrder 按订单号拿 vendor 侧的 purchase/refund 流水（对账 join 用）。
// 返回 (purchaseMicro, refundMicro, found) —— found=false 表示 vendor 账本里没这单。
func (s *LedgerStore) LedgerByOrder(ctx context.Context, vendorID, orderID string) (purchase, refund int64, found bool, err error) {
	if s.db == nil || orderID == "" {
		return 0, 0, false, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT reason, amount_micro FROM vendor_ledger
		 WHERE vendor_id = ? AND order_id = ?
	`, vendorID, orderID)
	if err != nil {
		return 0, 0, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var reason string
		var amt int64
		if err := rows.Scan(&reason, &amt); err != nil {
			return 0, 0, false, err
		}
		found = true
		switch reason {
		case providers.LedgerPurchase:
			// 扣费存的是负值 · 对账要正的绝对额
			if amt < 0 {
				amt = -amt
			}
			purchase += amt
		case providers.LedgerRefund:
			if amt < 0 {
				amt = -amt
			}
			refund += amt
		}
	}
	return purchase, refund, found, rows.Err()
}
