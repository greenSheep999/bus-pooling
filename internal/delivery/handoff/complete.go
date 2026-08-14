package handoff

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// Complete 做 confirm 之后的外部 + 内部收尾（09-transactions §4）：
//  1. housepool DELETE（幂等：404 视为成功·可能是之前 confirm 就删过了）
//  2. credential_ledger.status='handed_off' + handed_off_at
//
// **顺序不能反**（CLAUDE.md §7.1）—— 号池删了本地才能标 handed_off。
// 反过来则可能"本地已 handed_off · 池里号还在"。
//
// 两个调用点共享（消 Standards duplication）：
//   - api handler（confirm 分支）· 用户主动 confirm 时调
//   - janitor completeFn（重试卡在 confirmed 的行）· 崩溃后异步兜时调
//
// 之前两份实现·janitor 版有几个 bug（审计发现）：
//   - 找不到 kiro_rs_credential_id 直接 continue+返 nil → janitor 标 completed 但号还在
//   - `UPDATE credential_ledger` 错误被忽略 · 台账没改也算成功
//   - 不处理 housepool 404 · 重试永远失败
// 现在都由 Complete 统一处理。
type CompleteDeps struct {
	DB   *sql.DB
	Pool housepool.HousePool // 允许 nil（DRY_RUN） · 那时只更新台账·不做 housepool DELETE
	// Notifier · 1e-2 · handed_off 落定后主动喊一声 · 装配层桥到 webhookout。
	// nil = 不通知(1a 兼容 · 测试 mock 也可 nil)
	Notifier BoardedNotifier
	// PassengerID · handoff 归属的乘客 · Notifier 派发时用
	PassengerID string
}

// BoardedNotifier · 号已交付事件的通知接口(避免 handoff → webhookout 硬依赖)。
//
// 装配层实现: webhookout.Dispatcher.Dispatch(EventBoarded, BoardedPayload{...})。
// **失败不影响主链** — Complete 已经 tx commit 了才 call Notifier。
type BoardedNotifier interface {
	NotifyBoarded(ctx context.Context, passengerID string, credentialIDs []string, route string)
}

// Complete 收尾一批 credential。返回 error 就说明还没完成·调用方决定是要重试还是转 need_manual。
//
// 空 credIDs 是合法输入（当次没号要交）· 直接返 nil。
func Complete(ctx context.Context, deps CompleteDeps, credIDs []string) error {
	if len(credIDs) == 0 {
		return nil
	}

	// 拿每号的 kiro_rs_credential_id（可能为空 · pool 侧没这个号）
	metas, err := selectCompleteMeta(ctx, deps.DB, credIDs)
	if err != nil {
		return fmt.Errorf("handoff.Complete: 查号 meta: %w", err)
	}

	// 校验：**每个** credential 都要有 meta 行（防某号在台账里没）
	if len(metas) != len(credIDs) {
		return fmt.Errorf("handoff.Complete: %d 号里只有 %d 有台账·差 %d 号",
			len(credIDs), len(metas), len(credIDs)-len(metas))
	}

	// housepool DELETE（有 pool 才做·没 pool = DRY_RUN·只更新台账）
	//
	// credential_ledger.kiro_rs_credential_id 是 NOT NULL·所以 m.kiroRSID 一定 > 0。
	// 之前的 P1-A bug：janitor 版本对 NULL 静默跳过·但 schema 根本不允许 NULL·
	// 那段代码是死代码 · 却让 janitor 在其他不该跳过的时候（比如 SQL 错误）也返 nil。
	if deps.Pool != nil {
		for _, m := range metas {
			if err := deps.Pool.DeleteCredential(ctx, housepool.CredentialID(m.kiroRSID)); err != nil {
				// housepool 已删（404）视为成功 —— 幂等重试路径
				if errors.Is(err, housepool.ErrNotFound) {
					continue
				}
				return fmt.Errorf("handoff.Complete: pool DELETE krID=%d: %w", m.kiroRSID, err)
			}
		}
	}

	// 台账 · 全部 credential 一次 tx 标 handed_off · 任意一条失败整批回滚
	tx, err := deps.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("handoff.Complete: 开 tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, cid := range credIDs {
		res, err := tx.ExecContext(ctx, `
			UPDATE credential_ledger
			   SET status = 'handed_off', handed_off_at = ?
			 WHERE id = ? AND status != 'handed_off'`, now, cid)
		if err != nil {
			return fmt.Errorf("handoff.Complete: 标 handed_off cred=%s: %w", cid, err)
		}
		// n==0 表示已经是 handed_off（幂等）· 不算错
		_, _ = res.RowsAffected()
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// 通知对外 webhook · 主 tx 成功后异步喊一声 · 失败不 rollback
	if deps.Notifier != nil && deps.PassengerID != "" {
		deps.Notifier.NotifyBoarded(ctx, deps.PassengerID, credIDs, "handoff")
	}
	return nil
}

type completeMeta struct {
	credentialID string
	kiroRSID     uint64
}

func selectCompleteMeta(ctx context.Context, db *sql.DB, credIDs []string) ([]completeMeta, error) {
	if len(credIDs) == 0 {
		return nil, nil
	}
	placeholders := ""
	args := make([]any, 0, len(credIDs))
	for i, id := range credIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(kiro_rs_credential_id, 0)
		  FROM credential_ledger
		 WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []completeMeta
	for rows.Next() {
		var m completeMeta
		if err := rows.Scan(&m.credentialID, &m.kiroRSID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
