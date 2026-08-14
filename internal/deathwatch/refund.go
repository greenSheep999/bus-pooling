package deathwatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

// 质保退款（00 §7.5 规则 B）。
//
// **跟随上游**：只有上游 vendor 在它的质保窗口内退了钱给我方账户，我方才退乘客积分。
// 上游过窗口不退 → 我方也不退。我方**不承诺**任何可用时长。
//
// **只退号价**：
//   - ✅ 退 key_cost（号价 pass-through · 上游退了我方才有钱退）
//   - ❌ 不退 service_fee（服务已交付 —— 拉号动作发生了，无论号后来死没死）
//   - ❌ 不退 channel_fee（已付给支付通道 · 我方无回收路径）
//   - ❌ 不退 vendor_fee / region_fee / single_pull_fee / capability_fee（都是我方服务分项）
//
// **多人车按当初那轮的分摊比例退**，不按现在的 share_pct：
//   成员可能变过、有人可能已经退出。钱是谁付的就退给谁 —— 已退出的成员照样退
//   （他当初真金白银付了，号死了那份该还他）。账号注销的不退（没地方退）。
//
// **只退积分**·绝不走支付通道反向退款（规则 B 明文）。

// RefundStore 质保退款要用的库操作（抽出来好测）。
type RefundStore interface {
	// FindRefundable 找已判死 + 上游已退款 + 我方还没退乘客的号
	FindRefundable(ctx context.Context, limit int) ([]RefundCandidate, error)
	// MarkRefunded 标记这个号的质保退款已处理（幂等锚点）
	MarkRefunded(ctx context.Context, tx *sql.Tx, credentialLedgerID string, at time.Time) error
}

// RefundCandidate 一个待退款的死号。
type RefundCandidate struct {
	CredentialLedgerID string
	PullRoundID        string
	// KeyCostShare 这个号对应的号价（整轮 key_cost / 整轮号数）· microunit
	KeyCostShare int64
	// ParticipantsSplit 当初那轮 {passenger_id: 号数}
	ParticipantsSplit map[string]int
	// OwnerBusID 空 = 单独拉号（record group）· 非空 = 车里的号
	OwnerBusID string
	// OwnerRecordPassengerID 单独拉号时的归属人
	OwnerRecordPassengerID string
}

// RefundReport 一轮退款扫描的结果。
type RefundReport struct {
	Scanned  int
	Refunded int
	Skipped  int
	Errors   int
	// TotalCredits 本轮退出去的总积分（microunit）
	TotalCredits int64
}

// planRefund 算这个死号该退给谁多少（纯函数 · 好测）。
//
// 按当初那轮的号数占比分 keyCostShare。余数给分到号最多的人。
// 返回 nil 表示没人可退（split 空 + 没有 record 归属人）。
func planRefund(c RefundCandidate) map[string]int64 {
	if c.KeyCostShare <= 0 {
		return nil
	}

	// 单独拉号：全退归属人
	if len(c.ParticipantsSplit) == 0 {
		if c.OwnerRecordPassengerID == "" {
			return nil
		}
		return map[string]int64{c.OwnerRecordPassengerID: c.KeyCostShare}
	}

	// 多人车：按当初那轮的号数占比退
	totalKeys := 0
	for _, n := range c.ParticipantsSplit {
		totalKeys += n
	}
	if totalKeys <= 0 {
		return nil
	}

	// 排序保证余数分配可复现
	ids := make([]string, 0, len(c.ParticipantsSplit))
	for id := range c.ParticipantsSplit {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make(map[string]int64, len(ids))
	var allocated int64
	maxID, maxKeys := "", -1
	for _, id := range ids {
		n := c.ParticipantsSplit[id]
		if n <= 0 {
			continue
		}
		amt := c.KeyCostShare * int64(n) / int64(totalKeys)
		out[id] = amt
		allocated += amt
		if n > maxKeys {
			maxID, maxKeys = id, n
		}
	}
	if len(out) == 0 {
		return nil
	}
	// 余数给分到号最多的人
	if rem := c.KeyCostShare - allocated; rem > 0 && maxID != "" {
		out[maxID] += rem
	}
	return out
}

// RefundOnce 扫一批待退款的死号 · 逐个退。
//
// 每个号一个事务：退款 ledger + 标记已退款原子提交。
// 一个号失败不影响其他号（各自事务）。
func (w *Watcher) RefundOnce(ctx context.Context, limit int) RefundReport {
	var rep RefundReport
	if w.refunds == nil {
		return rep
	}
	cands, err := w.refunds.FindRefundable(ctx, limit)
	if err != nil {
		w.log.Error("质保退款扫描失败", "err", err)
		rep.Errors++
		return rep
	}
	rep.Scanned = len(cands)

	for _, c := range cands {
		plan := planRefund(c)
		if len(plan) == 0 {
			// 没人可退（数据不全）· 也标记处理过·免得每轮重扫
			if err := w.markRefundedStandalone(ctx, c.CredentialLedgerID); err != nil {
				w.log.Warn("标记无可退号失败", "cred", c.CredentialLedgerID, "err", err)
				rep.Errors++
			} else {
				rep.Skipped++
			}
			continue
		}
		credited, err := w.refundOne(ctx, c, plan)
		if err != nil {
			w.log.Error("质保退款失败", "cred", c.CredentialLedgerID, "err", err)
			rep.Errors++
			continue
		}
		rep.Refunded++
		rep.TotalCredits += credited
	}
	return rep
}

// refundOne 一个死号的退款 · 单事务。
func (w *Watcher) refundOne(
	ctx context.Context, c RefundCandidate, plan map[string]int64,
) (int64, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	// **先抢锁再入账**：条件 UPDATE（warranty_refunded_at IS NULL）就是幂等锚点。
	// 抢不到说明并发已退过 —— 直接回滚，绝不重复给钱。
	// 顺序反了（先入账后标记）的话，标记失败回滚虽然也不会双给，但白做一遍活。
	if err := w.refunds.MarkRefunded(ctx, tx, c.CredentialLedgerID, w.now().UTC()); err != nil {
		if errors.Is(err, ErrAlreadyRefunded) {
			return 0, nil // 幂等成功·不算错
		}
		return 0, err
	}

	// 排序保证落 ledger 顺序确定
	ids := make([]string, 0, len(plan))
	for id := range plan {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var credited int64
	for _, pid := range ids {
		amt := plan[pid]
		if amt <= 0 {
			continue
		}
		// 账号注销的不退（用户明确：没地方退）· wallet 行不存在就跳过这个人
		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM wallet WHERE passenger_id = ?`, pid).Scan(&exists); err != nil {
			return 0, err
		}
		if exists == 0 {
			w.log.Info("质保退款跳过（钱包不存在·可能已注销）", "passenger", pid)
			continue
		}
		// sign=+1 = 加余额（wallet.ApplyTx 的 credit 方向）
		if _, err := wallet.ApplyTx(ctx, tx, wallet.Move{
			PassengerID: pid,
			Reason:      wallet.ReasonWarrantyRefund,
			Amount:      amt,
			RefType:     "credential_ledger",
			RefID:       c.CredentialLedgerID,
		}, +1); err != nil {
			return 0, fmt.Errorf("退款入账(%s): %w", pid, err)
		}
		// 退款也让余额变多 → 清欠费状态 + 自己解挂（§8.26）
		if err := wallet.ClearOverdueStateTx(ctx, tx, pid); err != nil {
			return 0, err
		}
		credited += amt
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	w.log.Info("质保退款完成",
		"cred", c.CredentialLedgerID, "credits", credited, "people", len(ids))

	// 1e-2 · 通知对外 webhook · 每退款一个 passenger 一条(账户视角)
	// 主 tx 已 commit 才通知 · 失败不 rollback
	if w.refundNotifier != nil {
		for _, pid := range ids {
			amt := plan[pid]
			if amt <= 0 {
				continue
			}
			w.refundNotifier.OnRefundIssued(ctx, pid, amt, c.CredentialLedgerID, c.OwnerBusID)
		}
	}
	return credited, nil
}

// markRefundedStandalone 没人可退时也要标记·免得每轮重复扫。
func (w *Watcher) markRefundedStandalone(ctx context.Context, credLedgerID string) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := w.refunds.MarkRefunded(ctx, tx, credLedgerID, w.now().UTC()); err != nil {
		if errors.Is(err, ErrAlreadyRefunded) {
			return nil // 并发已标 · 幂等成功
		}
		return err
	}
	return tx.Commit()
}

// ErrAlreadyRefunded 这个号的质保退款已经处理过（并发保护命中 · 不是错）。
var ErrAlreadyRefunded = errors.New("deathwatch: 质保退款已处理")

// SQLRefundStore 是 RefundStore 的 SQLite 实现。
type SQLRefundStore struct{ db *sql.DB }

func NewSQLRefundStore(db *sql.DB) *SQLRefundStore { return &SQLRefundStore{db: db} }

// FindRefundable 找该退款的死号。
//
// 条件（全部满足才退）：
//   1. 号已判死（status='dead'）
//   2. **上游已退款**（pull_round.status='refunded' —— vendor webhook / 轮询确认过）
//   3. 我方还没退过（warranty_refunded_at IS NULL · 幂等）
//   4. 在质保窗口内（warranty_until 为空视为"上游说退就退"）
func (s *SQLRefundStore) FindRefundable(ctx context.Context, limit int) ([]RefundCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT cl.id, cl.source_pull_round_id,
		       COALESCE(cl.owner_bus_id, ''),
		       COALESCE(cl.owner_record_passenger_id, ''),
		       pr.key_cost_total, pr.count_purchased, pr.participants_split_json
		  FROM credential_ledger cl
		  JOIN pull_round pr ON pr.id = cl.source_pull_round_id
		 WHERE cl.status = 'dead'
		   AND cl.warranty_refunded_at IS NULL
		   AND pr.status = 'refunded'
		 ORDER BY cl.dead_at
		 LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("deathwatch: 扫待退款号: %w", err)
	}
	defer rows.Close()

	var out []RefundCandidate
	for rows.Next() {
		var c RefundCandidate
		var keyCostTotal int64
		var countPurchased int
		var splitJSON string
		if err := rows.Scan(&c.CredentialLedgerID, &c.PullRoundID,
			&c.OwnerBusID, &c.OwnerRecordPassengerID,
			&keyCostTotal, &countPurchased, &splitJSON); err != nil {
			return nil, err
		}
		if countPurchased <= 0 {
			continue
		}
		// 这个号摊到的号价 = 整轮号价 / 整轮号数
		c.KeyCostShare = keyCostTotal / int64(countPurchased)
		if err := json.Unmarshal([]byte(splitJSON), &c.ParticipantsSplit); err != nil {
			c.ParticipantsSplit = nil // 坏数据 → 退回 record 归属人语义
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkRefunded 标记已退款（幂等锚点 · 条件 UPDATE 防重复退）。
func (s *SQLRefundStore) MarkRefunded(
	ctx context.Context, tx *sql.Tx, credentialLedgerID string, at time.Time,
) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE credential_ledger
		   SET warranty_refunded_at = ?
		 WHERE id = ? AND warranty_refunded_at IS NULL`,
		formatTime(at), credentialLedgerID)
	if err != nil {
		return fmt.Errorf("deathwatch: 标记已退款: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 已经被别的并发退过 —— 幂等成功，但要让 caller 知道别重复入账
		return ErrAlreadyRefunded
	}
	return nil
}
