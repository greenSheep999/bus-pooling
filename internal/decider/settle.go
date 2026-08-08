package decider

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

// settle 在**一个事务**里做完落账 + 落库 + 完成，这几件事必须原子：
//   - 把冻结转成消费（各分层各一笔 ledger）
//   - 差额释放回 balance（估价高于实扣时）
//   - 写 pull_round
//   - 写 credential_ledger 每号一行
//   - 累加当日计数
//   - 推进 pending_purchase → completed
//
// 一条也不能少 —— 少一条对账就废（比如少了 credential_ledger 那批号就成了孤号）。
func (o *Orchestrator) settle(
	ctx context.Context,
	pendingID string,
	pending Pending,
	purchase *providers.PurchaseResult,
	credIDs []housepool.CredentialID,
) (*PullResult, error) {

	// 按 vendor 实扣的**实际单价**重算加价（估价可能偏低或偏高）
	actual := int64(purchase.Purchased)
	if actual <= 0 {
		return nil, fmt.Errorf("decider: 意外的 0 成交")
	}
	unitCost := purchase.TotalCost.Amount / actual
	bd := Price(unitCost, purchase.Purchased, o.rates)
	if !bd.Verify() {
		return nil, fmt.Errorf("decider: 分项之和 != 总额（不该发生）")
	}

	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// 差额退回：估价冻结 - 实扣
	if diff := pending.ReservedAmount - bd.Total; diff > 0 {
		if err := wallet.ReleaseReservedTx(ctx, tx, pending.PassengerID, diff); err != nil {
			return nil, fmt.Errorf("decider: 释放差额: %w", err)
		}
	} else if diff < 0 {
		// 极端：实扣 > 估价（混价单）· 差额得从余额再冻一份并直接消费
		if err := wallet.ReserveTx(ctx, tx, pending.PassengerID, -diff); err != nil {
			return nil, fmt.Errorf("decider: 补冻结差额: %w", err)
		}
	}

	// 把冻结按分项 CommitReserved —— 每层一笔 ledger，前端看不到分层（映射成 spend），
	// 内部对账需要
	pullRoundID := o.newID()
	if err := o.commitBreakdown(ctx, tx, pending.PassengerID, pullRoundID, bd); err != nil {
		return nil, err
	}

	// 每日计数
	if err := wallet.BumpDailyTx(ctx, tx, pending.PassengerID, 1, bd.Total); err != nil {
		return nil, err
	}

	// pull_round
	if err := o.insertPullRound(ctx, tx, pullRoundID, pending, purchase, bd); err != nil {
		return nil, err
	}

	// credential_ledger 每号一行
	ledgerIDs, err := o.insertCredentials(ctx, tx, pending, pullRoundID, purchase, credIDs)
	if err != nil {
		return nil, err
	}

	// pending_purchase → completed（同事务的条件 UPDATE）
	if err := advancePendingTx(ctx, tx, pendingID, StatusImported, StatusCompleted,
		Fields{PullRoundID: pullRoundID}, o.now()); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// 取扣后余额（读一次库，不放事务里避免拿到旧值）
	var balance int64
	if err := o.db.QueryRowContext(ctx,
		`SELECT balance FROM wallet WHERE passenger_id = ?`,
		pending.PassengerID).Scan(&balance); err != nil {
		return nil, err
	}

	return &PullResult{
		PullRoundID:      pullRoundID,
		VendorID:         pending.VendorID,
		Purchased:        purchase.Purchased,
		CredentialIDs:    ledgerIDs,
		UnitPrice:        bd.UnitPrice,
		ServiceFee:       bd.ServiceFee,
		TotalDebit:       bd.Total,
		BalanceRemaining: balance,
	}, nil
}

// commitBreakdown 按分项落 wallet_ledger。每层非 0 才落一笔 —— 全 0 也没意义。
//
// 对外前端只看到 spend；对内每层单独一笔是**对账所需**（哪层收多少要能追）。
func (o *Orchestrator) commitBreakdown(
	ctx context.Context, tx *sql.Tx,
	passengerID, pullRoundID string,
	bd Breakdown,
) error {
	moves := []struct {
		reason wallet.Reason
		amount int64
	}{
		{wallet.ReasonKeyCost, bd.keyCost},
		{wallet.ReasonVendorFee, bd.vendorFee},
		{wallet.ReasonRegionFee, bd.regionFee},
		{wallet.ReasonSinglePullFee, bd.singlePullFee},
		{wallet.ReasonCapabilityFee, bd.capabilityFee},
		{wallet.ReasonServiceFee, bd.ServiceFee},
	}
	for _, m := range moves {
		if m.amount == 0 {
			continue
		}
		if _, err := wallet.CommitReservedTx(ctx, tx, wallet.Move{
			PassengerID: passengerID,
			Reason:      m.reason,
			Amount:      m.amount,
			RefType:     "pull_round",
			RefID:       pullRoundID,
		}); err != nil {
			return fmt.Errorf("decider: 落分项 %s: %w", m.reason, err)
		}
	}
	return nil
}

func (o *Orchestrator) insertPullRound(
	ctx context.Context, tx *sql.Tx,
	id string, pending Pending,
	purchase *providers.PurchaseResult, bd Breakdown,
) error {
	rawResp, _ := json.Marshal(purchase.Raw)
	// 参与方分摊：1a 只做 single bus，全归本人
	split, _ := json.Marshal(map[string]int{pending.PassengerID: purchase.Purchased})

	cols := bd.roundColumns()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO pull_round
		  (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
		   key_cost_total, vendor_fee_total, region_fee_total,
		   single_pull_fee_total, capability_fee_total, service_fee_total,
		   participants_split_json, status, vendor_response_json, vendor_order_id,
		   created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, pending.VendorID, pending.ClientOrderID,
		nullIfEmpty(pending.BusID),
		pending.CountRequested, purchase.Purchased,
		cols[0], cols[1], cols[2], cols[3], cols[4], cols[5],
		string(split), "completed", string(rawResp),
		purchase.VendorOrderID,
		formatTime(o.now()), formatTime(o.now()))
	if err != nil {
		return fmt.Errorf("decider: 写 pull_round: %w", err)
	}
	return nil
}

// insertCredentials 每号一行 credential_ledger，返回我方 UUID 列表（对外派发用这个）。
func (o *Orchestrator) insertCredentials(
	ctx context.Context, tx *sql.Tx,
	pending Pending, pullRoundID string,
	purchase *providers.PurchaseResult,
	credIDs []housepool.CredentialID,
) ([]string, error) {
	// credIDs 长度可能 < purchase.Keys（号池部分失败）· 按短的走
	n := len(credIDs)
	if n > purchase.Purchased {
		n = purchase.Purchased
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := o.newID()
		var warrantyUntil any
		if purchase.Keys[i].WarrantyUntil != nil {
			warrantyUntil = formatTime(*purchase.Keys[i].WarrantyUntil)
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO credential_ledger
			  (id, kiro_rs_credential_id, owner_bus_id, owner_record_passenger_id,
			   current_group, vendor_id, vendor_order_id, source_pull_round_id,
			   status, disabled, pulled_at, warranty_until)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'alive', 0, ?, ?)`,
			id, uint64(credIDs[i]),
			nullIfEmpty(pending.BusID),
			nullIfEmptyIfNoBus(pending.BusID, pending.PassengerID),
			pending.TargetGroup, pending.VendorID, purchase.VendorOrderID, pullRoundID,
			formatTime(o.now()), warrantyUntil)
		if err != nil {
			return nil, fmt.Errorf("decider: 写 credential_ledger[%d]: %w", i, err)
		}
		out = append(out, id)
	}
	return out, nil
}

// nullIfEmptyIfNoBus 只有单独拉号（BusID 空）时才把 owner_record_passenger_id 填上。
// schema 有 CHECK：号要么属车、要么属乘客的拉号记录，不能同时。
func nullIfEmptyIfNoBus(busID, passengerID string) any {
	if busID != "" {
		return nil
	}
	return passengerID
}

// advancePendingTx 事务内的条件推进（跟 Store.advance 逻辑同，但用现成 tx）。
func advancePendingTx(
	ctx context.Context, tx *sql.Tx,
	id string, from, to Status, set Fields, now time.Time,
) error {
	query := `UPDATE pending_purchase SET status = ?, updated_at = ?`
	args := []any{string(to), formatTime(now)}
	if set.VendorOrderID != "" {
		query += `, vendor_order_id = ?`
		args = append(args, set.VendorOrderID)
	}
	if set.PullRoundID != "" {
		query += `, pull_round_id = ?`
		args = append(args, set.PullRoundID)
	}
	if set.Error != "" {
		query += `, error = ?`
		args = append(args, set.Error)
	}
	query += ` WHERE id = ? AND status = ?`
	args = append(args, id, string(from))

	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleTransition
	}
	return nil
}
