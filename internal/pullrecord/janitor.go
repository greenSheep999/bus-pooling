package pullrecord

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// PoolReader · janitor reconcile 只需要 GetCredential · 用窄接口方便注入 mock。
// live 装配时传 housepool.HousePool（自动满足）· 测试直接 stub。
type PoolReader interface {
	GetCredential(ctx context.Context, id housepool.CredentialID) (*housepool.Credential, error)
}

// AssignJanitor 扫 pending_assignment 卡在 initial 的行 · 尝试 reconcile · 无法 reconcile 转 need_manual。
//
// 背景（09-transactions §5 · P0-3 修复）：
// assign 是三段式（tx1 initial → tx 外 pool.UpdateCredential → tx2 completed）。
// tx1 与 pool 之间 · 或 pool 与 tx2 之间发生 SIGKILL / 网络断 · pending_assignment
// 会卡在 initial · 台账可能未改 · housepool 侧 group 可能已迁也可能未迁。
//
// **1a 收尾 reconcile 策略**（into_bus 分支实现·push_pool 仍转 need_manual）：
//
//   1. 拿 credential_id → 查 credential_ledger.kiro_rs_credential_id
//   2. pool.GetCredential(kr_id) → 查 Groups
//   3.
//      - groups 含目标 `bus-<busID>`：外部动作已完成 · 台账可能未落
//        → 走 AssignToBusTx（幂等·已 owner_bus_id 命中直接返 ErrNotFound 就当已经处理）
//        → 前推 pending_assignment=completed + 保存幂等响应
//      - groups 仍在 `record-<pid>`：外部动作没做 · delete pending_assignment
//        （幂等 key 也回收 · 同 key 可重放 = 新单）
//      - 其他情形（不在预期两个 group 中任意一个）：疑难 · 转 need_manual
//   4. pool 查询失败（网络 / auth）：不改状态 · 下轮再试
//   5. pool 未装配（DRY_RUN / mock）：转 need_manual（人工看数据判断）
//
// **push_pool 分支**：1c 才做真推 · 现在没法查 passengerpool · 直接转 need_manual。
type AssignJanitor struct {
	db         *sql.DB
	store      *Store
	pool       PoolReader
	interval   time.Duration
	stuckAfter time.Duration
	batchLimit int
	log        *slog.Logger
}

type AssignJanitorConfig struct {
	DB    *sql.DB
	Store *Store
	// Pool nil = 未装配 · 走 need_manual 兜底（1a DRY_RUN 场景常见）
	// 窄接口 PoolReader · live 装配传 housepool.HousePool（自动实现 GetCredential）
	Pool       PoolReader
	Interval   time.Duration // 默认 30s
	StuckAfter time.Duration // 默认 5min
	BatchLimit int           // 默认 50
	Logger     *slog.Logger
}

func NewAssignJanitor(cfg AssignJanitorConfig) *AssignJanitor {
	j := &AssignJanitor{
		db:         cfg.DB,
		store:      cfg.Store,
		pool:       cfg.Pool,
		interval:   cfg.Interval,
		stuckAfter: cfg.StuckAfter,
		batchLimit: cfg.BatchLimit,
		log:        cfg.Logger,
	}
	if j.interval <= 0 {
		j.interval = 30 * time.Second
	}
	if j.stuckAfter <= 0 {
		j.stuckAfter = 5 * time.Minute
	}
	if j.batchLimit <= 0 {
		j.batchLimit = 50
	}
	if j.log == nil {
		j.log = slog.Default()
	}
	return j
}

func (j *AssignJanitor) Run(ctx context.Context) {
	t := time.NewTicker(j.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = j.SweepOnce(ctx)
		}
	}
}

// SweepReport 一轮统计。
type SweepReport struct {
	Forwarded   int // reconcile 前推到 completed
	Rolledback  int // pool 未迁 · delete 允许重试
	NeedManual  int // 转 need_manual
	Failed      int
}

// SweepOnce 扫一轮 · 供测试直接调用。返回本轮更新的行数（Forwarded + Rolledback + NeedManual）+ err。
func (j *AssignJanitor) SweepOnce(ctx context.Context) (int, error) {
	r, err := j.sweepReport(ctx)
	return r.Forwarded + r.Rolledback + r.NeedManual, err
}

func (j *AssignJanitor) sweepReport(ctx context.Context) (SweepReport, error) {
	var r SweepReport
	cutoff := time.Now().UTC().Add(-j.stuckAfter).Format(timeLayout)
	rows, err := j.db.QueryContext(ctx, `
		SELECT id, passenger_id, credential_id, target, target_bus_id,
		       idempotency_record_id, updated_at
		  FROM pending_assignment
		 WHERE status = 'initial' AND updated_at < ?
		 LIMIT ?`, cutoff, j.batchLimit)
	if err != nil {
		j.log.Error("assign janitor 查 stuck initial 失败", "err", err)
		return r, err
	}
	defer rows.Close()

	type stuckRow struct {
		id, pid, cid, tgt, idemID string
		busID                     sql.NullString
	}
	var stuck []stuckRow
	for rows.Next() {
		var s stuckRow
		var ua string
		if err := rows.Scan(&s.id, &s.pid, &s.cid, &s.tgt, &s.busID, &s.idemID, &ua); err != nil {
			return r, err
		}
		stuck = append(stuck, s)
	}
	if err := rows.Err(); err != nil {
		return r, err
	}

	for _, s := range stuck {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		outcome, ferr := j.reconcile(ctx, s.id, s.pid, s.cid, s.tgt, s.busID.String, s.idemID)
		switch outcome {
		case "forwarded":
			r.Forwarded++
		case "rolledback":
			r.Rolledback++
		case "need_manual":
			r.NeedManual++
		case "retry":
			// 不算本轮解决·下轮继续
		default:
			r.Failed++
			if ferr != nil {
				j.log.Warn("assign janitor reconcile 未处理·下轮重试",
					"id", s.id, "outcome", outcome, "err", ferr)
			}
		}
	}
	return r, nil
}

// reconcile 单行处理 · 返回 outcome:
//   - "forwarded" · pool 已迁·前推 completed
//   - "rolledback" · pool 未迁·delete 允许重试
//   - "need_manual" · 疑难或 pool 未装配·转 need_manual
//   - "retry" · pool 查询失败·下轮再试
//   - "" · 意外错误
func (j *AssignJanitor) reconcile(ctx context.Context, paID, pid, cid, tgt, busID, idemID string) (string, error) {
	// push_pool · 1c 才做真推 · 现在没法查 passengerpool · need_manual
	if tgt == "to-passengerpool" {
		return "need_manual", j.markNeedManual(ctx, paID, "push_pool 无法自动 reconcile · 1c 补")
	}
	if tgt != "to-bus" {
		return "need_manual", j.markNeedManual(ctx, paID, "未知 target · "+tgt)
	}
	if busID == "" {
		return "need_manual", j.markNeedManual(ctx, paID, "into_bus 缺 target_bus_id")
	}

	// pool 未装配（DRY_RUN / mock）· 走 need_manual · 保守
	if j.pool == nil || j.store == nil {
		return "need_manual", j.markNeedManual(ctx, paID, "pool / store 未装配 · 无法自动 reconcile")
	}

	// 拿 kr_id
	// **P0-1 修**：用 LookupKiroRSByID 不校验归属 · 分叉场景 owner_record_passenger_id
	// 已变化时也能拿到 kr_id · 走到真正的分叉判定逻辑
	krIDs, err := j.store.LookupKiroRSByID(ctx, []string{cid})
	if err != nil {
		return "retry", fmt.Errorf("lookup kr_id: %w", err)
	}
	krID, ok := krIDs[cid]
	if !ok {
		// 号本地没记 kr_id（DryRun 拉的 · 或数据异常）· 转人工
		return "need_manual", j.markNeedManual(ctx, paID,
			"credential 缺 kiro_rs_credential_id · 无法查 pool group")
	}

	// 查 pool group
	cred, err := j.pool.GetCredential(ctx, housepool.CredentialID(krID))
	if err != nil {
		// pool 未响应 · 下轮再试（不 markNeedManual · 网络抖动别浪费）
		return "retry", fmt.Errorf("pool.GetCredential: %w", err)
	}

	targetGroup := "bus-" + busID
	recordGroup := "record-" + pid
	inTarget := containsGroup(cred.Groups, targetGroup)
	inRecord := containsGroup(cred.Groups, recordGroup)

	switch {
	case inTarget:
		// pool 已迁到我们的 target · 前推 completed（幂等）
		if err := j.forward(ctx, paID, []string{cid}, pid, busID, idemID); err != nil {
			// forward 里可能检测到分叉 · 转 need_manual
			if strings.Contains(err.Error(), "分叉") {
				return "need_manual", j.markNeedManual(ctx, paID, err.Error())
			}
			return "retry", err
		}
		j.log.Info("assign janitor · 前推 completed（pool 已迁）",
			"id", paID, "credential", cid, "bus", busID)
		return "forwarded", nil

	case inRecord && !inTarget:
		// pool 未迁·delete pending_assignment（同 idempotency key 可重放 = 新单）
		// idempotency_record 也删（避免 hit stale）· FK 顺序：pending_assignment 先
		if err := j.rollback(ctx, paID, idemID); err != nil {
			return "retry", err
		}
		j.log.Info("assign janitor · 回滚 initial（pool 未迁）",
			"id", paID, "credential", cid)
		return "rolledback", nil

	default:
		// 号不在预期两个 group 里 · 可能 handoff / dead / 其他人为操作 · 转人工
		return "need_manual", j.markNeedManual(ctx, paID,
			fmt.Sprintf("credential 不在 target(%s) 也不在 record(%s) · 实际 groups=%v",
				targetGroup, recordGroup, cred.Groups))
	}
}

// forward · pool 已迁·跑 AssignToBusTx + 推 completed + 保存幂等响应。
//
// **P0-1 修**（审计发现）：以前把 AssignToBusTx 的 ErrNotFound 当"已处理"直接推 completed·
// 无法识别 **credential 已被别的 assign 迁走**的分叉场景。
// 现在：先 SELECT credential_ledger.owner_bus_id 判断：
//   - IS NULL / 恰好 = busID：正常路径·执行 AssignToBusTx
//   - = 其他 bus id：**分叉** · 转 need_manual · 让运营查
//
// 幂等：AssignToBusTx 落到 owner_bus_id = busID 后 · 再跑一次 WHERE owner_bus_id IS NULL
// 返 ErrNotFound · 视 owner_bus_id 是否已 = busID 决定当"已处理"还是"分叉"。
func (j *AssignJanitor) forward(ctx context.Context, paID string, cids []string, pid, busID, idemID string) error {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 先看 credential 当前的 owner_bus_id · 分叉早发现。
	//
	// **P0-1 修**（审计发现）：不能加 `AND owner_record_passenger_id = ?` 过滤·
	// 因为完成派单后 owner_record_passenger_id 会被置 NULL·加了这条件会 sql.ErrNoRows·
	// 分叉分支根本走不到。改成只按 id 查·同时也校验 owner_record_passenger_id
	// 状态（=pid 说明还没迁 · =NULL 说明已迁·再看 owner_bus_id 是哪辆车）。
	for _, cid := range cids {
		var (
			currentBus  sql.NullString
			currentRec  sql.NullString
		)
		err := tx.QueryRowContext(ctx,
			`SELECT owner_bus_id, owner_record_passenger_id FROM credential_ledger WHERE id = ?`,
			cid).Scan(&currentBus, &currentRec)
		if err != nil {
			return fmt.Errorf("查 credential_ledger: %w", err)
		}
		// 分叉判定：
		//   1) owner_bus_id 已经指向另一辆车 → 分叉（无论 owner_record_passenger_id 值如何）
		//   2) owner_bus_id 空 · owner_record_passenger_id 不等于本次 pid → 归属被人改了 · 分叉
		if currentBus.Valid && currentBus.String != "" && currentBus.String != busID {
			return fmt.Errorf("credential %s 分叉：台账 owner_bus_id=%q · pending 目标=%q",
				cid, currentBus.String, busID)
		}
		if (!currentBus.Valid || currentBus.String == "") &&
			(!currentRec.Valid || currentRec.String != pid) {
			return fmt.Errorf("credential %s 分叉：归属丢失 · owner_record_passenger_id=%q · pending 主 =%q",
				cid, currentRec.String, pid)
		}
	}

	if err := AssignToBusTx(ctx, tx, cids, pid, busID); err != nil {
		if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("AssignToBusTx: %w", err)
		}
		// ErrNotFound = WHERE owner_record_passenger_id + owner_bus_id IS NULL 命中 0 行
		// 上面已经确认 owner_bus_id 要么 IS NULL 要么 = busID · 走到这里就是已 = busID · 幂等 pass。
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE pending_assignment
		   SET status = 'completed', updated_at = ?
		 WHERE id = ? AND status = 'initial'`,
		time.Now().UTC().Format(timeLayout), paID)
	if err != nil {
		return fmt.Errorf("推 completed: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 已被其他 forward 抢先·不当错
		return tx.Commit()
	}

	// 更新 idempotency_record.response_body（保存 janitor 兜底后的成功响应）
	// 让原客户端重放同 key 时也能拿到一致的 200 body
	respBody := []byte(fmt.Sprintf(`{"assigned":%d,"errors":[]}`, len(cids)))
	if _, err := tx.ExecContext(ctx, `
		UPDATE idempotency_record
		   SET response_status = 200, response_body = ?, first_completed_at = ?
		 WHERE id = ? AND first_completed_at IS NULL`,
		respBody, time.Now().UTC().Format(timeLayout), idemID); err != nil {
		return fmt.Errorf("回填幂等响应: %w", err)
	}
	return tx.Commit()
}

// rollback · pool 未迁·删 pending_assignment + idempotency_record（允许同 key 重放）。
func (j *AssignJanitor) rollback(ctx context.Context, paID, idemID string) error {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM pending_assignment WHERE id = ? AND status = 'initial'`, paID); err != nil {
		return fmt.Errorf("delete pending_assignment: %w", err)
	}
	// idempotency_record 删除·让同 key 重放当新单处理
	// **前提**：response_status IS NULL（fresh 或未落响应体）· 已完成不删
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM idempotency_record WHERE id = ? AND response_status IS NULL`, idemID); err != nil {
		return fmt.Errorf("delete idempotency_record: %w", err)
	}
	return tx.Commit()
}

// markNeedManual · 转 need_manual · 记 error 让运营查。
func (j *AssignJanitor) markNeedManual(ctx context.Context, paID, reason string) error {
	_, err := j.db.ExecContext(ctx, `
		UPDATE pending_assignment
		   SET status = 'need_manual', error = ?, updated_at = ?
		 WHERE id = ? AND status = 'initial'`,
		reason, time.Now().UTC().Format(timeLayout), paID)
	if err != nil {
		j.log.Error("assign janitor 转 need_manual 失败", "id", paID, "err", err)
		return err
	}
	j.log.Warn("assign janitor · pending_assignment → need_manual",
		"id", paID, "reason", reason)
	return nil
}

// containsGroup · slice 匹配·groups 通常 1-2 个·线性够用。
func containsGroup(groups []string, want string) bool {
	for _, g := range groups {
		if strings.EqualFold(g, want) {
			return true
		}
	}
	return false
}
