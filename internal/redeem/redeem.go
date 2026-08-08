// Package redeem 管兑换码的消费。
//
// 一码一行；status=unused → used 是一次性的（used_by / used_at 落定后不可再消费）。
// 消费流程放在一个事务里：UPDATE ... WHERE status='unused' → 影响 1 行才继续入账。
// 并发下靠 SQLite BEGIN IMMEDIATE 串行，靠条件 UPDATE 挡重复。
package redeem

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

var (
	ErrNotFound       = errors.New("redeem: 兑换码不存在")
	ErrUsed           = errors.New("redeem: 兑换码已被使用")
	ErrExpired        = errors.New("redeem: 兑换码已过期")
	ErrEmptyCode      = errors.New("redeem: 兑换码为空")
	ErrClaimedByOther = errors.New("redeem: 兑换码已被其他账号使用")
)

type Code struct {
	Code      string
	Credits   int64
	Status    string
	UsedBy    string
	UsedAt    time.Time
	ExpiresAt time.Time
	Memo      string
	CreatedAt time.Time
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Result 是消费成功的结果 —— replay=true 表示这个乘客之前已经用同一个码
// 兑过（幂等重放），credits 依然回填。
type Result struct {
	Credits      int64
	BalanceAfter int64
	LedgerID     string
	Replayed     bool
}

// Normalize 把输入的码规整成入库形式：去空白 + 转大写。
// mock / 手输时用户经常打空格或小写，落库前统一。
func Normalize(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

// Consume 在一次事务里做三件事：
//  1. 锁定并标记 redeem_code
//  2. 给乘客钱包 Credit（reason=redeem）
//  3. 返回入账结果
//
// 并发下的行为：
//   - 两个乘客同时兑同一个码 → SQLite 串行 → 第二个进事务看到 status='used' → ErrUsed
//   - 同一乘客重放（网络重试） → 幂等由 API 层的 X-Idempotency-Key 兜底；这里
//     也做兜底：查到 used_by = 当前乘客 → Replayed=true 并返回原积分（不重复入账）
func (s *Store) Consume(ctx context.Context, passengerID, rawCode string) (Result, error) {
	code := Normalize(rawCode)
	if code == "" {
		return Result{}, ErrEmptyCode
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("redeem: 开事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		credits   int64
		status    string
		usedBy    sql.NullString
		expiresAt sql.NullString
	)
	err = tx.QueryRowContext(ctx,
		`SELECT credits, status, used_by, expires_at FROM redeem_code WHERE code = ?`,
		code).Scan(&credits, &status, &usedBy, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, fmt.Errorf("redeem: 查码: %w", err)
	}

	// 过期检查（不改 DB 状态 —— 后台清扫任务再统一改）
	if expiresAt.Valid && expiresAt.String != "" {
		if t, perr := time.Parse(time.RFC3339Nano, expiresAt.String); perr == nil && !t.IsZero() && t.Before(time.Now().UTC()) {
			return Result{}, ErrExpired
		}
	}
	if status == "expired" {
		return Result{}, ErrExpired
	}

	// 幂等重放：同一乘客二次兑同一个码 —— 返 replay，不再入账
	if status == "used" {
		if usedBy.Valid && usedBy.String == passengerID {
			// 找上次入账那条流水，把当前余额也回一并返回
			ledgerID, balAfter, err := findRedeemLedger(ctx, tx, passengerID, code)
			if err != nil {
				return Result{}, err
			}
			if err := tx.Commit(); err != nil {
				return Result{}, fmt.Errorf("redeem: 提交（replay）: %w", err)
			}
			return Result{
				Credits: credits, BalanceAfter: balAfter,
				LedgerID: ledgerID, Replayed: true,
			}, nil
		}
		return Result{}, ErrClaimedByOther
	}

	// 条件 UPDATE：只在 status='unused' 时才改成 used —— 并发下第二个走这里会返 0 行
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `
		UPDATE redeem_code
		   SET status = 'used', used_by = ?, used_at = ?
		 WHERE code = ? AND status = 'unused'`,
		passengerID, now, code)
	if err != nil {
		return Result{}, fmt.Errorf("redeem: 标记 used: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 走到这的话就是并发下被别人抢了 —— 重读一次给出正确的 sentinel
		var raceStatus string
		var raceBy sql.NullString
		if err := tx.QueryRowContext(ctx,
			`SELECT status, used_by FROM redeem_code WHERE code = ?`, code).
			Scan(&raceStatus, &raceBy); err != nil {
			return Result{}, err
		}
		if raceStatus == "used" && raceBy.Valid && raceBy.String == passengerID {
			ledgerID, balAfter, err := findRedeemLedger(ctx, tx, passengerID, code)
			if err != nil {
				return Result{}, err
			}
			if err := tx.Commit(); err != nil {
				return Result{}, fmt.Errorf("redeem: 提交（replay）: %w", err)
			}
			return Result{
				Credits: credits, BalanceAfter: balAfter,
				LedgerID: ledgerID, Replayed: true,
			}, nil
		}
		return Result{}, ErrUsed
	}

	entry, err := wallet.ApplyTx(ctx, tx, wallet.Move{
		PassengerID: passengerID,
		Reason:      wallet.ReasonRedeem,
		Amount:      credits,
		RefType:     "redeem_code",
		RefID:       code,
		Memo:        "兑换码 " + code,
	}, +1)
	if err != nil {
		return Result{}, err
	}
	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("redeem: 提交: %w", err)
	}
	return Result{
		Credits: credits, BalanceAfter: entry.BalanceAfter,
		LedgerID: entry.ID, Replayed: false,
	}, nil
}

// findRedeemLedger 找乘客用某个码的入账流水，用来 replay 时把 balance_after 回填。
// 找不到时用当前余额兜底（不至于因为一条流水丢失就整个失败）。
func findRedeemLedger(ctx context.Context, tx *sql.Tx, passengerID, code string) (string, int64, error) {
	var id string
	var balAfter int64
	err := tx.QueryRowContext(ctx, `
		SELECT id, balance_after FROM wallet_ledger
		 WHERE passenger_id = ? AND reason = 'redeem' AND ref_type = 'redeem_code' AND ref_id = ?
		 ORDER BY seq DESC LIMIT 1`,
		passengerID, code).Scan(&id, &balAfter)
	if errors.Is(err, sql.ErrNoRows) {
		// 兜底：读当前余额
		if err := tx.QueryRowContext(ctx,
			`SELECT balance FROM wallet WHERE passenger_id = ?`, passengerID).Scan(&balAfter); err != nil {
			return "", 0, fmt.Errorf("redeem: 兜底读余额: %w", err)
		}
		return "", balAfter, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("redeem: 找入账流水: %w", err)
	}
	return id, balAfter, nil
}

// Seed 后台生成一条兑换码（测试 / 后台批量导入用）。生产管理面板走单独端点。
func (s *Store) Seed(ctx context.Context, code string, credits int64, memo string, expiresAt *time.Time) error {
	code = Normalize(code)
	if code == "" {
		return ErrEmptyCode
	}
	if credits <= 0 {
		return fmt.Errorf("redeem: credits 必须 > 0")
	}
	var expStr any
	if expiresAt != nil {
		expStr = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO redeem_code (code, credits, status, memo, expires_at, created_at)
		VALUES (?, ?, 'unused', ?, ?, ?)`,
		code, credits, nullIfEmpty(memo), expStr,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("redeem: 插入: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
