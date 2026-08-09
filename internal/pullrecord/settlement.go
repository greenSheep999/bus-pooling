package pullrecord

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

// 自费拉的号派进多人车 · 按份额即时清算（decisions §8.23）。
//
// **场景**：我从「提取 key」自己掏钱拉了号 → 派进车 → 车里其他人也用。
// 我垫的钱要由其他成员按各自 share_pct 买回份额，否则没人愿意先垫钱、多人车玩不起来。
//
// ```
// 我拉号花 100 积分 · 车内 我 40% / A 30% / B 30%
// 派进车时：A 扣 30 → 我入账 30
//          B 扣 30 → 我入账 30
//          我净支出 40（= 我该承担的份额）
// ```
//
// **会计语义**：成员向派入者**买入份额**，我方是清算通道 —— **不抽成**
// （服务费在拉号时已经收过了 · 这里只是内部记账转移，不再收 vendor 费用）。
//
// **规则**（§8.23 明文，别自己发明）：
//   - 余额不足 → **只跳过那个人**，不拒绝整车（号已经是我的了·少一人分摊只是我少收回一点）
//   - 挂起的成员（§8.26）同理不参与
//   - 被跳过的人：不扣他积分、也不给他取这批号 · `share_pct` **不动**
//   - **只有一个人都参与不了才拦** —— 那时候派进去等于纯赠送
//   - **不做应收 / 账期** —— 跳过就跳过，不记债务
//   - **单人车不清算** —— 就一个人，没有分摊对象

// SettlementShare 一位成员的清算份额。
type SettlementShare struct {
	PassengerID string
	// Amount 他该付给派入者多少（microunit）
	Amount int64
	// SkipReason 空 = 正常参与 · insufficient_balance / suspended = 本次跳过
	SkipReason string
}

// Settlement 一次派进车的清算方案。
type Settlement struct {
	// Payers 实际付钱的成员
	Payers []SettlementShare
	// Skipped 本次跳过的成员（余额不足 / 已挂起）
	Skipped []SettlementShare
	// Income 派入者实际收到多少（= Payers 之和）
	Income int64
	// Lost 因为有人跳过·派入者少收多少（= Skipped 之和 · 只用于提示）
	Lost int64
	// Solo true = 单人车 · 无分摊对象 · 不清算
	Solo bool
}

// ErrNoPayableMember 一个车友都参与不了 —— 派进去等于纯赠送 · 拦住（§8.23）
var ErrNoPayableMember = fmt.Errorf("pullrecord: 没有车友能参与这次分摊")

// planSettlement 算清算方案（纯函数 · 好测）。
//
// cost 是这批号的**原始成本**（派入者当初为这些号付的钱 · microunit）。
// assignerID 是派入者（他不在 payers 里 —— 他承担自己那份）。
//
// 口径跟前端预览（web/src/components/AssignModal.tsx calcSettlement）**必须一致**：
// 每个**其他**成员付 `cost × 他的 share_pct / 100`。
func planSettlement(members []settlementMember, assignerID string, cost int64) (Settlement, error) {
	if cost <= 0 {
		return Settlement{}, fmt.Errorf("pullrecord: 清算成本必须 > 0")
	}

	var others []settlementMember
	for _, m := range members {
		if m.passengerID == assignerID {
			continue
		}
		others = append(others, m)
	}
	// 单人车 / 车里只有派入者 → 不清算
	if len(others) == 0 {
		return Settlement{Solo: true}, nil
	}

	// 排序保证结果可复现
	sort.Slice(others, func(i, j int) bool {
		return others[i].passengerID < others[j].passengerID
	})

	var st Settlement
	for _, m := range others {
		// 跟前端一致：按各自 share_pct 算·不归一化到 100
		// （派入者那份留给他自己承担·不摊给别人）
		amount := cost * int64(m.sharePct) / 100
		share := SettlementShare{PassengerID: m.passengerID, Amount: amount}

		switch {
		case m.suspended:
			share.SkipReason = "suspended"
		case amount > m.balance:
			share.SkipReason = "insufficient_balance"
		}

		if share.SkipReason != "" {
			st.Skipped = append(st.Skipped, share)
			st.Lost += amount
			continue
		}
		if amount <= 0 {
			continue // share_pct=0 的成员没份额可付
		}
		st.Payers = append(st.Payers, share)
		st.Income += amount
	}

	// 一个人都参与不了 → 拦（§8.23：那时候派进去等于纯赠送）
	if len(st.Payers) == 0 {
		return Settlement{}, ErrNoPayableMember
	}
	return st, nil
}

// settlementMember 清算用的成员快照。
type settlementMember struct {
	passengerID string
	sharePct    int
	balance     int64
	suspended   bool
}

// loadMembersForSettlement 事务内读车成员 + 余额（判定和扣款要看同一快照·防并发超扣）。
func loadMembersForSettlement(
	ctx context.Context, tx *sql.Tx, busID string,
) ([]settlementMember, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT bm.passenger_id, bm.share_pct, bm.status, COALESCE(w.balance, 0)
		  FROM bus_member bm
		  LEFT JOIN wallet w ON w.passenger_id = bm.passenger_id
		 WHERE bm.bus_id = ? AND bm.left_at IS NULL`, busID)
	if err != nil {
		return nil, fmt.Errorf("pullrecord: 读车成员: %w", err)
	}
	defer rows.Close()

	var out []settlementMember
	for rows.Next() {
		var m settlementMember
		var status string
		if err := rows.Scan(&m.passengerID, &m.sharePct, &status, &m.balance); err != nil {
			return nil, err
		}
		m.suspended = status == "suspended"
		out = append(out, m)
	}
	return out, rows.Err()
}

// creditsCostOf 查这批号当初花了多少（microunit）。
//
// 从 credential_ledger → pull_round 反查：每号摊到的号价 = 整轮号价 / 整轮号数。
// **只算号价**（key_cost）—— 服务费在拉号时已收·不该让车友再买一次（§8.23
// "不再收 vendor 费用 · 派发只是内部记账转移"）。
func creditsCostOf(
	ctx context.Context, tx *sql.Tx, credentialIDs []string,
) (int64, error) {
	if len(credentialIDs) == 0 {
		return 0, nil
	}
	var total int64
	for _, cid := range credentialIDs {
		var keyCostTotal int64
		var countPurchased int
		err := tx.QueryRowContext(ctx, `
			SELECT pr.key_cost_total, pr.count_purchased
			  FROM credential_ledger cl
			  JOIN pull_round pr ON pr.id = cl.source_pull_round_id
			 WHERE cl.id = ?`, cid).Scan(&keyCostTotal, &countPurchased)
		if err != nil {
			if err == sql.ErrNoRows {
				continue // 找不到来源轮次（不该发生）· 这号按 0 成本算
			}
			return 0, fmt.Errorf("pullrecord: 查号成本(%s): %w", cid, err)
		}
		if countPurchased > 0 {
			total += keyCostTotal / int64(countPurchased)
		}
	}
	return total, nil
}

// SettleAssignToBusTx 在派进车的**同一事务**里做清算（§8.23 明文要求同事务 ——
// 避免"号进车了但钱没结"）。
//
// 返回清算方案给 handler 落 assign_event / 回响应。
// busID 对应的车只有派入者一人时返 Solo=true · 不动钱。
func SettleAssignToBusTx(
	ctx context.Context, tx *sql.Tx,
	credentialIDs []string, assignerID, busID string,
) (Settlement, error) {
	members, err := loadMembersForSettlement(ctx, tx, busID)
	if err != nil {
		return Settlement{}, err
	}
	// 单人车快路径 —— 不查成本·不动钱
	if len(members) <= 1 {
		return Settlement{Solo: true}, nil
	}

	cost, err := creditsCostOf(ctx, tx, credentialIDs)
	if err != nil {
		return Settlement{}, err
	}
	if cost <= 0 {
		// 号价查不到（DryRun 号 / 数据不全）· 不清算但允许派进车
		return Settlement{Solo: true}, nil
	}

	st, err := planSettlement(members, assignerID, cost)
	if err != nil {
		return Settlement{}, err
	}
	if st.Solo {
		return st, nil
	}

	// 逐人扣 → 派入者收。两个方向都落 ledger（对账要能追）。
	refID := credentialIDs[0] // 一批号用第一个当引用锚点
	for _, p := range st.Payers {
		if _, err := wallet.ApplyTx(ctx, tx, wallet.Move{
			PassengerID: p.PassengerID,
			Reason:      wallet.ReasonShareExpense,
			Amount:      p.Amount,
			RefType:     "credential_assign",
			RefID:       refID,
			Memo:        "买入拼车号份额",
		}, -1); err != nil {
			// planSettlement 已按同事务快照筛过余额 · 走到这儿是并发扣走了
			return Settlement{}, fmt.Errorf("pullrecord: 清算扣款(%s): %w", p.PassengerID, err)
		}
	}
	if st.Income > 0 {
		if _, err := wallet.ApplyTx(ctx, tx, wallet.Move{
			PassengerID: assignerID,
			Reason:      wallet.ReasonShareIncome,
			Amount:      st.Income,
			RefType:     "credential_assign",
			RefID:       refID,
			Memo:        "车友分摊回款",
		}, +1); err != nil {
			return Settlement{}, fmt.Errorf("pullrecord: 清算入账: %w", err)
		}
	}

	// 自检：收支必须相等（我方不抽成 · §8.23）
	var paid int64
	for _, p := range st.Payers {
		paid += p.Amount
	}
	if paid != st.Income {
		return Settlement{}, fmt.Errorf(
			"pullrecord: 清算收支不平 · 付出 %d 收到 %d（不该发生）", paid, st.Income)
	}
	return st, nil
}
