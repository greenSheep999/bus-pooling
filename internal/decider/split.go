package decider

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// 多人拼车的费用分摊（decisions §8.18 / §8.23 / §8.26）。
//
// **规则**（跟 decisions 一致 · 别自己发明）：
//   1. 只有 `status='active'` 的成员参与分摊（`suspended` 的不参与·也不给号 · §8.26）
//   2. 余额不够自己那份的成员 → **本次跳过**（不扣他钱 · 不记债务 · §8.23 已否决记账期）
//      · `skipped_count += 1` · 连续到 3 次自动挂起（§8.26）
//   3. 跳过之后剩下的人**重新归一化**分摊 —— 但只在他们各自还付得起时才成立·
//      否则继续踢·直到剩下的人都付得起（收敛循环）
//   4. 发起人付不起自己那份 → 整轮失败（不能让别人替他垫 · §8.18 不许悄悄让人多掏钱）
//   5. 所有人都付不起 → 整轮失败（余额不足）
//
// **为什么要重新归一化**：车里 3 人各 33%，一人余额不足被跳过，剩 2 人如果还按 33% 付，
// 总额就凑不齐（66% ≠ 100%）。剩下的人得按 50/50 分完整轮的钱。
// 这不违反"不许悄悄让人多掏钱"—— 每轮拉号是**发起人主动触发**的一次性行为，
// 参与的人本来就是按"这轮实际参与人数"分摊，不是改 bus_member.share_pct 那种长期变更。

// Participant 一位参与分摊的成员及其应付额。
type Participant struct {
	PassengerID string
	// Amount 这轮该他付多少（microunit）· 之和 == 轮总额
	Amount int64
	// Keys 这轮分给他几个号（按 Amount 占比 · 用于 participants_split_json）
	Keys int
}

// SkippedMember 被跳过的成员及原因（对外映射成"本次跳过"·不暴露内部细节）。
type SkippedMember struct {
	PassengerID string
	Reason      string // insufficient_balance | suspended
}

// SplitPlan 一轮拉号的分摊方案。
type SplitPlan struct {
	Participants []Participant
	Skipped      []SkippedMember
	Total        int64
}

// AmountFor 查某人应付额 · 不在方案里返 0。
func (p SplitPlan) AmountFor(passengerID string) int64 {
	for _, x := range p.Participants {
		if x.PassengerID == passengerID {
			return x.Amount
		}
	}
	return 0
}

// SplitMap 给 reserve_split_json 落库用。
func (p SplitPlan) SplitMap() map[string]int64 {
	out := make(map[string]int64, len(p.Participants))
	for _, x := range p.Participants {
		out[x.PassengerID] = x.Amount
	}
	return out
}

// KeysMap 给 participants_split_json 落库用（{passenger_id: 号数}）。
func (p SplitPlan) KeysMap() map[string]int {
	out := make(map[string]int, len(p.Participants))
	for _, x := range p.Participants {
		out[x.PassengerID] = x.Keys
	}
	return out
}

// splitMember 内部用 · 成员的分摊输入。
type splitMember struct {
	passengerID string
	sharePct    int
	balance     int64
	suspended   bool
}

// loadBusMembersForSplit 读这辆车的活跃成员 + 各自余额 + share_pct。
//
// 事务内读 —— 分摊判定和扣款必须看同一个余额快照，不然并发下会超扣。
func loadBusMembersForSplit(
	ctx context.Context, tx *sql.Tx, busID string,
) ([]splitMember, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT bm.passenger_id, bm.share_pct, bm.status,
		       COALESCE(w.balance, 0)
		  FROM bus_member bm
		  LEFT JOIN wallet w ON w.passenger_id = bm.passenger_id
		 WHERE bm.bus_id = ? AND bm.left_at IS NULL
		 ORDER BY bm.joined_at`, busID)
	if err != nil {
		return nil, fmt.Errorf("decider: 读车成员: %w", err)
	}
	defer rows.Close()

	var out []splitMember
	for rows.Next() {
		var m splitMember
		var status string
		if err := rows.Scan(&m.passengerID, &m.sharePct, &status, &m.balance); err != nil {
			return nil, err
		}
		m.suspended = status == "suspended"
		out = append(out, m)
	}
	return out, rows.Err()
}

// planSplit 算这一轮谁付多少 · 纯函数（好测）。
//
// initiatorID 是发起拉号的人 —— 他付不起自己那份就整轮失败（规则 4）。
// total 是这轮要付的总额（microunit）· keys 是这轮号数。
func planSplit(
	members []splitMember, initiatorID string, total int64, keys int,
) (SplitPlan, error) {
	if total <= 0 {
		return SplitPlan{}, fmt.Errorf("decider: 分摊总额必须 > 0")
	}

	var skipped []SkippedMember
	// 挂起的先出局（§8.26 · 不参与分摊也不给号）
	var pool []splitMember
	for _, m := range members {
		if m.suspended {
			skipped = append(skipped, SkippedMember{m.passengerID, "suspended"})
			continue
		}
		pool = append(pool, m)
	}
	if len(pool) == 0 {
		return SplitPlan{}, ErrNoPayableMember
	}

	// 收敛循环：按当前 pool 归一化分摊 → 谁付不起就踢掉 → 重算
	// 最多循环 len(pool) 次（每轮至少踢一个）· 不会死循环
	for {
		amounts, err := normalizeAmounts(pool, total)
		if err != nil {
			return SplitPlan{}, err
		}

		// 找付不起的人
		var broke []int
		for i, m := range pool {
			if m.balance < amounts[i] {
				broke = append(broke, i)
			}
		}
		if len(broke) == 0 {
			// 都付得起 · 成方案
			return buildPlan(pool, amounts, skipped, total, keys)
		}

		// 发起人付不起 → 整轮失败（不能让别人替他垫）
		for _, i := range broke {
			if pool[i].passengerID == initiatorID {
				return SplitPlan{}, ErrInitiatorInsufficient
			}
		}

		// 踢掉付不起的（从后往前删·避免索引错位）
		sort.Sort(sort.Reverse(sort.IntSlice(broke)))
		for _, i := range broke {
			skipped = append(skipped, SkippedMember{
				pool[i].passengerID, "insufficient_balance",
			})
			pool = append(pool[:i], pool[i+1:]...)
		}
		if len(pool) == 0 {
			return SplitPlan{}, ErrNoPayableMember
		}
	}
}

// normalizeAmounts 按 share_pct 归一化分摊 total · 返回跟 pool 同序的应付额。
//
// 余数给 share_pct 最大的人（差几个 microunit 不值得引入小数）。
// 所有人 share_pct 都是 0 时按人数均分（不该发生·但别炸）。
func normalizeAmounts(pool []splitMember, total int64) ([]int64, error) {
	if len(pool) == 0 {
		return nil, ErrNoPayableMember
	}
	sumPct := 0
	for _, m := range pool {
		sumPct += m.sharePct
	}

	amounts := make([]int64, len(pool))
	var allocated int64
	if sumPct <= 0 {
		// 均分兜底
		per := total / int64(len(pool))
		for i := range amounts {
			amounts[i] = per
			allocated += per
		}
	} else {
		for i, m := range pool {
			a := total * int64(m.sharePct) / int64(sumPct)
			amounts[i] = a
			allocated += a
		}
	}

	// 余数补给 share_pct 最大的（并列取第一个）
	if rem := total - allocated; rem > 0 {
		maxIdx := 0
		for i, m := range pool {
			if m.sharePct > pool[maxIdx].sharePct {
				maxIdx = i
			}
			_ = m
		}
		amounts[maxIdx] += rem
	}
	return amounts, nil
}

// buildPlan 把归一化结果转成 SplitPlan（含按金额占比分号数）。
func buildPlan(
	pool []splitMember, amounts []int64,
	skipped []SkippedMember, total int64, keys int,
) (SplitPlan, error) {
	parts := make([]Participant, len(pool))
	for i, m := range pool {
		parts[i] = Participant{PassengerID: m.passengerID, Amount: amounts[i]}
	}

	// 号数按金额占比分 · 余数给出钱最多的人
	if keys > 0 && total > 0 {
		assigned := 0
		for i := range parts {
			n := int(int64(keys) * parts[i].Amount / total)
			parts[i].Keys = n
			assigned += n
		}
		if rem := keys - assigned; rem > 0 {
			maxIdx := 0
			for i := range parts {
				if parts[i].Amount > parts[maxIdx].Amount {
					maxIdx = i
				}
			}
			parts[maxIdx].Keys += rem
		}
	}

	// 自检：分摊之和必须等于总额（钱不能凭空多也不能少）
	var sum int64
	for _, p := range parts {
		sum += p.Amount
	}
	if sum != total {
		return SplitPlan{}, fmt.Errorf(
			"decider: 分摊之和 %d != 总额 %d（不该发生）", sum, total)
	}

	return SplitPlan{Participants: parts, Skipped: skipped, Total: total}, nil
}
