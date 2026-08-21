package credplain

// marketstage.go · 手工池号明文暂存 · issues-log I-01
//
// **为什么单独一段**:普通拉号路径 · 明文入库时已经有 credential_ledger.id ·
// 直接用 Save() 就够了。手工池不一样 —— admin 塞号那一刻只有 kiro_rs_credential_id ·
// ledger.id 要等用户拉号 sold 时才产生。中间那段"号在 available 池等买家"期间 ·
// 明文没地方存 · 只能落到这张暂存表 · 用 kiro_rs_credential_id 做主键。
//
// 生命周期:
//   admin POST /admin/market/stock  →  StashByKiroRS   (塞暂存表)
//   用户拉号 · decider settle sold  →  PopToCredplain  (读暂存 + 写 credplain + 删暂存 · 同 tx)
//   号一直没卖 · 7d TTL 到          →  PurgeStash      (janitor 清)
//
// 明文这一路都是加密态存放(AES-GCM) · 跟 credplain 主表复用同一把 cipher。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// StashTTL · 暂存表默认过期 · 号一直没卖 · janitor 到期清 → 该号 push_pool 走 placeholder(跟当前行为一致)
const StashTTL = 7 * 24 * time.Hour

// StashInput · 塞暂存表的入参 · kiro_rs_credential_id + 明文四件套之一
type StashInput struct {
	KiroRSCredentialID uint64
	AuthMethod         AuthMethod
	RefreshToken       string
	AccessToken        string
	KiroAPIKey         string
	Email              string
}

// PopToCredplainTx · settle 手工池 sold 分支调 · 读暂存 → 写正式 credplain → 删暂存
//
// **必须跟 credential_ledger INSERT 同 tx**（跟 SellTx 一样的理由）· 否则崩溃后:
//   - 暂存已删 · credplain 未写 · ledger 未 INSERT · 死锁不可恢复
//   - 暂存已删 · credplain 已写 · ledger 未 INSERT · credplain 指向不存在的 ledger.id
// 同 tx 保证:三者要么全 commit · 要么全 rollback。
//
// 拿不到暂存(admin 老导入的号 · 或 TTL 过期)不 fatal —— 号仍能卖 · 只是 push_pool
// 会走 placeholder(跟 I-01 修复前行为一致 · 不新增回归)。返 ErrNotFound 让调用方决定。
func (s *Store) PopToCredplainTx(
	ctx context.Context,
	tx *sql.Tx,
	kiroRSCredentialID uint64,
	credentialID string,
) error {
	if s == nil || s.cipher == nil {
		return errors.New("credplain: Store 未装配 cipher")
	}
	if kiroRSCredentialID == 0 || credentialID == "" {
		return errors.New("credplain: PopToCredplainTx 缺参数")
	}

	// 1. 读暂存 · 加密块直接搬 · 不解密再加密（省一趟 · 且 cipher.Encrypt 每次 nonce 不同 · 数据一致性 OK）
	var (
		authMethod                string
		rtEnc, atEnc, akEnc       []byte
		emailNull                 sql.NullString
	)
	err := tx.QueryRowContext(ctx, `
		SELECT auth_method,
		       refresh_token_encrypted, access_token_encrypted, kiro_api_key_encrypted,
		       email
		  FROM market_stock_plaintext
		 WHERE kiro_rs_credential_id = ?`, kiroRSCredentialID).
		Scan(&authMethod, &rtEnc, &atEnc, &akEnc, &emailNull)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("credplain: 读 stash: %w", err)
	}

	// 2. 写正式 credplain · 加密块直接搬 · TTL 从此刻开始 24h
	now := time.Now().UTC()
	exp := now.Add(DefaultTTL)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO credential_plaintext
		  (credential_id, auth_method,
		   refresh_token_encrypted, access_token_encrypted, kiro_api_key_encrypted,
		   email, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(credential_id) DO UPDATE SET
		  auth_method             = excluded.auth_method,
		  refresh_token_encrypted = excluded.refresh_token_encrypted,
		  access_token_encrypted  = excluded.access_token_encrypted,
		  kiro_api_key_encrypted  = excluded.kiro_api_key_encrypted,
		  email                   = excluded.email,
		  expires_at              = excluded.expires_at,
		  used_at                 = NULL`,
		credentialID, authMethod,
		rtEnc, atEnc, akEnc,
		nullIfSQLNull(emailNull), now.Format(time.RFC3339Nano), exp.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("credplain: 写 credplain: %w", err)
	}

	// 3. 删暂存 · 一号一次 · 避免残留
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM market_stock_plaintext WHERE kiro_rs_credential_id = ?`,
		kiroRSCredentialID); err != nil {
		return fmt.Errorf("credplain: 删 stash: %w", err)
	}
	return nil
}

// PurgeStash · janitor 清 market_stock_plaintext 过期行 · 返删除数
//
// 号一直在 available 状态没卖 · 7d TTL 到期就删 · push_pool 走 placeholder(自然兜底)。
func (s *Store) PurgeStash(ctx context.Context) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM market_stock_plaintext WHERE expires_at <= ?`, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ── 私工具 ─────────────────────────

func validateAuth(m AuthMethod, rt, at, ak string) error {
	switch m {
	case AuthRefreshToken:
		if rt == "" {
			return errors.New("credplain: refresh_token 方法需 RefreshToken 非空")
		}
	case AuthAPIKey:
		if ak == "" {
			return errors.New("credplain: api_key 方法需 KiroAPIKey 非空")
		}
	case AuthBearer:
		if at == "" {
			return errors.New("credplain: bearer 方法需 AccessToken 非空")
		}
	default:
		return fmt.Errorf("credplain: 未知 auth_method %q", m)
	}
	return nil
}

func (s *Store) encryptAll(rt, at, ak string) (rtEnc, atEnc, akEnc []byte, err error) {
	if rt != "" {
		if rtEnc, err = s.cipher.Encrypt([]byte(rt)); err != nil {
			return nil, nil, nil, fmt.Errorf("credplain: 加密 refresh_token: %w", err)
		}
	}
	if at != "" {
		if atEnc, err = s.cipher.Encrypt([]byte(at)); err != nil {
			return nil, nil, nil, fmt.Errorf("credplain: 加密 access_token: %w", err)
		}
	}
	if ak != "" {
		if akEnc, err = s.cipher.Encrypt([]byte(ak)); err != nil {
			return nil, nil, nil, fmt.Errorf("credplain: 加密 kiro_api_key: %w", err)
		}
	}
	return
}

func nullIfSQLNull(s sql.NullString) any {
	if s.Valid && s.String != "" {
		return s.String
	}
	return nil
}

// StashByKiroRS · admin_market 塞号时调 · 明文加密写 market_stock_plaintext
//
// ON CONFLICT 覆盖 —— 同一 kiro_rs_credential_id 重复导入(admin 重跑)时 · 用新明文覆盖旧的。
func (s *Store) StashByKiroRS(ctx context.Context, in StashInput) error {
	if s == nil || s.cipher == nil {
		return errors.New("credplain: Store 未装配 cipher")
	}
	if in.KiroRSCredentialID == 0 {
		return errors.New("credplain: kiro_rs_credential_id 不能 0")
	}
	if err := validateAuth(in.AuthMethod, in.RefreshToken, in.AccessToken, in.KiroAPIKey); err != nil {
		return err
	}

	rtEnc, atEnc, akEnc, err := s.encryptAll(in.RefreshToken, in.AccessToken, in.KiroAPIKey)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	exp := now.Add(StashTTL)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO market_stock_plaintext
		  (kiro_rs_credential_id, auth_method,
		   refresh_token_encrypted, access_token_encrypted, kiro_api_key_encrypted,
		   email, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(kiro_rs_credential_id) DO UPDATE SET
		  auth_method             = excluded.auth_method,
		  refresh_token_encrypted = excluded.refresh_token_encrypted,
		  access_token_encrypted  = excluded.access_token_encrypted,
		  kiro_api_key_encrypted  = excluded.kiro_api_key_encrypted,
		  email                   = excluded.email,
		  expires_at              = excluded.expires_at`,
		in.KiroRSCredentialID, string(in.AuthMethod),
		rtEnc, atEnc, akEnc,
		nullIfEmpty(in.Email), now.Format(time.RFC3339Nano), exp.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("credplain: 落 stash: %w", err)
	}
	return nil
}
