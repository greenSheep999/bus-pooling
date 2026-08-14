package vendorview

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// 交叉对账（docs/23-endpoints-todo §1）· 拿我方 pull_round 跟 vendor 侧账本双向核对。
//
// **为什么**：生产已开 dry_run=false 真实扣费 · 只有我方账本时 · 被多扣 / 漏退都发现
// 不了。对账在三个层面比：
//   1. 存在性 —— 我方记了这单 · vendor 账本有没有（count 层 · 用 vendor_order · 5 家有）
//   2. 数量 —— 我方 count_purchased vs vendor_order.purchased
//   3. 金额 —— 我方 key_cost_total vs vendor_ledger 的 purchase 额（接了 ledger 的家）
//
// **纯内部**（CLAUDE.md §0.1）· 只做后台告警 / CLI · 不出前端。

// DiscrepancyKind · 对账差异类型
const (
	// DiscOrphanOurs 我方有完成的 round · vendor 账本查无此单（幻扣 / backfill 滞后）
	DiscOrphanOurs = "orphan_ours"
	// DiscCountMismatch 数量对不上（我方记 N · vendor 记 M）
	DiscCountMismatch = "count_mismatch"
	// DiscAmountMismatch 金额对不上（接了 ledger 才判）
	DiscAmountMismatch = "amount_mismatch"
	// DiscRefundMissing 我方退款了 · vendor 账本无对应退款（我方净亏）
	DiscRefundMissing = "refund_missing"
)

// Discrepancy 一条对账差异
type Discrepancy struct {
	RoundID     string
	VendorID    string
	OrderID     string
	Kind        string
	OursCount   int
	VendorCount int
	OursMicro   int64
	VendorMicro int64
	Detail      string
}

// ReconcileSummary 对账汇总
type ReconcileSummary struct {
	RoundsChecked int
	Discrepancies int
	// ByKind 各类型计数
	ByKind map[string]int
}

// Reconciler 对账器 · 只读 · 不改任何表。
type Reconciler struct {
	db     *sql.DB
	ledger *LedgerStore
}

func NewReconciler(db *sql.DB, ledger *LedgerStore) *Reconciler {
	return &Reconciler{db: db, ledger: ledger}
}

// reconRound · 从 pull_round 读出的对账所需字段
type reconRound struct {
	id            string
	vendorID      string
	vendorOrderID string
	clientOrderID string
	countPurch    int
	keyCostMicro  int64
	status        string
}

// Reconcile 跑一次对账 · 覆盖近 sinceDays 天的 pull_round（<=0 默认 30）。
//
// 只查**已成交/已退款**的 round（initiated/failed 没真扣费 · 不对账）。
func (r *Reconciler) Reconcile(ctx context.Context, sinceDays int) ([]Discrepancy, ReconcileSummary, error) {
	sum := ReconcileSummary{ByKind: map[string]int{}}
	if r.db == nil {
		return nil, sum, nil
	}
	if sinceDays <= 0 {
		sinceDays = 30
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -sinceDays).Format(time.RFC3339)

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, vendor_id, COALESCE(vendor_order_id,''), client_order_id,
		       count_purchased, key_cost_total, status
		  FROM pull_round
		 WHERE created_at >= ?
		   AND status IN ('completed','partial','refunded')
		 ORDER BY created_at DESC
	`, cutoff)
	if err != nil {
		return nil, sum, err
	}
	defer rows.Close()

	var rounds []reconRound
	for rows.Next() {
		var rr reconRound
		if err := rows.Scan(&rr.id, &rr.vendorID, &rr.vendorOrderID,
			&rr.clientOrderID, &rr.countPurch, &rr.keyCostMicro, &rr.status); err != nil {
			return nil, sum, err
		}
		rounds = append(rounds, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, sum, err
	}

	var out []Discrepancy
	for _, rr := range rounds {
		sum.RoundsChecked++
		ds := r.checkRound(ctx, rr)
		for _, d := range ds {
			out = append(out, d)
			sum.ByKind[d.Kind]++
			sum.Discrepancies++
		}
	}
	return out, sum, nil
}

// checkRound 对单个 round 做三层核对。
func (r *Reconciler) checkRound(ctx context.Context, rr reconRound) []Discrepancy {
	var out []Discrepancy

	// join 键：优先 vendor_order_id · 空则 client_order_id
	joinKey := rr.vendorOrderID
	if joinKey == "" {
		joinKey = rr.clientOrderID
	}

	// 层 1+2 · 存在性 + 数量（vendor_order · 5 家有 backfill）
	vendorPurchased, orderFound, err := r.vendorOrderPurchased(ctx, rr.vendorID, joinKey)
	if err == nil {
		if !orderFound {
			out = append(out, Discrepancy{
				RoundID: rr.id, VendorID: rr.vendorID, OrderID: joinKey,
				Kind: DiscOrphanOurs, OursCount: rr.countPurch,
				Detail: "我方记了成交 · vendor 订单历史查无此单（幻扣或 backfill 滞后）",
			})
		} else if vendorPurchased != rr.countPurch {
			out = append(out, Discrepancy{
				RoundID: rr.id, VendorID: rr.vendorID, OrderID: joinKey,
				Kind: DiscCountMismatch, OursCount: rr.countPurch, VendorCount: vendorPurchased,
				Detail: fmt.Sprintf("数量对不上：我方 %d · vendor %d", rr.countPurch, vendorPurchased),
			})
		}
	}

	// 层 3 · 金额（vendor_ledger · 接了 ledger 的家才有）
	if r.ledger != nil {
		purchaseMicro, refundMicro, ledgerFound, lerr := r.ledger.LedgerByOrder(ctx, rr.vendorID, joinKey)
		if lerr == nil && ledgerFound {
			if rr.keyCostMicro != purchaseMicro {
				out = append(out, Discrepancy{
					RoundID: rr.id, VendorID: rr.vendorID, OrderID: joinKey,
					Kind: DiscAmountMismatch, OursMicro: rr.keyCostMicro, VendorMicro: purchaseMicro,
					Detail: fmt.Sprintf("金额对不上：我方记号价 %d · vendor 扣 %d（microunit）", rr.keyCostMicro, purchaseMicro),
				})
			}
			// 我方退款了 · vendor 账本无退款 = 净亏
			if rr.status == "refunded" && refundMicro == 0 {
				out = append(out, Discrepancy{
					RoundID: rr.id, VendorID: rr.vendorID, OrderID: joinKey,
					Kind: DiscRefundMissing, OursMicro: rr.keyCostMicro,
					Detail: "我方已退款给乘客 · 但 vendor 账本无对应退款（我方净亏）",
				})
			}
		}
	}
	return out
}

// vendorOrderPurchased 查 vendor_order 里这单的 purchased 数 · found=false 表示查无此单。
func (r *Reconciler) vendorOrderPurchased(ctx context.Context, vendorID, orderID string) (purchased int, found bool, err error) {
	if orderID == "" {
		return 0, false, nil
	}
	var p sql.NullInt64
	err = r.db.QueryRowContext(ctx, `
		SELECT purchased FROM vendor_order
		 WHERE vendor_id = ? AND vendor_order_id = ?
	`, vendorID, orderID).Scan(&p)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return int(p.Int64), true, nil
}
