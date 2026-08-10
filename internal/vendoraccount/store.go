// Package vendoraccount · vendor 敏感凭证的加密存取。
//
// 表：`vendor_account`（migrations/001_init.sql §17）
//   - `secret_credentials_encrypted BLOB` · AES-GCM · 明文永不落库
//   - `auth_scheme` · api_key | bearer | cookie · 决定 payload 里字段名
//   - `status` · active / disabled · 只装 active 的
//
// 生产部署流程（跟 config 分离 · decisions §11.6 前后约定）：
//  1. env 只放 `BP_MASTER_KEY`（AES 主密钥）
//  2. `bus-pooling seed-vendor <vendor_id>` CLI 明文 → 加密 → 写表
//  3. 服务启动装配 adapter 时 · 从表读 → 解密 → 内存塞给 adapter
//  4. **不再从 `BP_VENDOR_*_API_KEY` env 读**（本包 Load 优先·env 只当 fallback）
//
// 本地 dev 兼容：表空 · 装配处 fallback 到 env·让开发者不必 seed 就跑
package vendoraccount

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/secrets"
	"github.com/google/uuid"
)

// Credential 是解密后的明文凭证 · 内存里的形状。
//
// 一家 vendor 最多需要两把 secret：
//   - APIKey · 出向调 vendor API 用
//   - WebhookSecret · 校验 vendor 推给我方的签名用（无 webhook 的家为空）
//
// 落库前用 JSON 序列化 → AES 加密 → BLOB。
type Credential struct {
	APIKey        string `json:"api_key"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

// Store · vendor_account 表读写门面。
type Store struct {
	db     *sql.DB
	cipher *secrets.Cipher
}

func NewStore(db *sql.DB, cipher *secrets.Cipher) *Store {
	return &Store{db: db, cipher: cipher}
}

// Upsert 加密明文 · 写表 · 存在则覆盖（按 vendor_id + label · label 空默认 default）。
//
// authScheme 用现表约束：`api_key | bearer | cookie`。
// label 允许一家 vendor 存多份凭证（未来主备号）· 目前 seed CLI 传 "default"。
func (s *Store) Upsert(ctx context.Context, vendorID, label, authScheme string, cred Credential) error {
	if s.cipher == nil {
		return errors.New("vendoraccount: cipher 未装配（BP_MASTER_KEY 空）")
	}
	if vendorID == "" {
		return errors.New("vendoraccount: vendor_id 不能空")
	}
	if label == "" {
		label = "default"
	}
	payload, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("vendoraccount: 序列化: %w", err)
	}
	blob, err := s.cipher.Encrypt(payload)
	if err != nil {
		return fmt.Errorf("vendoraccount: 加密: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	// 尝试更新已有记录（同 vendor_id + label · 状态置回 active）
	res, err := s.db.ExecContext(ctx, `
		UPDATE vendor_account
		   SET auth_scheme = ?, secret_credentials_encrypted = ?,
		       status = 'active', updated_at = ?
		 WHERE vendor_id = ? AND COALESCE(label,'default') = ?
	`, authScheme, blob, now, vendorID, label)
	if err != nil {
		return fmt.Errorf("vendoraccount: update: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	// 无老记录 · 插新的
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO vendor_account (id, vendor_id, label, auth_scheme,
		                            secret_credentials_encrypted, status,
		                            created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'active', ?, ?)
	`, uuid.NewString(), vendorID, label, authScheme, blob, now, now)
	if err != nil {
		return fmt.Errorf("vendoraccount: insert: %w", err)
	}
	return nil
}

// LoadActive 拿指定 vendor 的当前活跃凭证 · label 优先 "default"。
//
// 找不到 · 或表空 · 返 (nil, nil) —— 上层拿 nil 判断"该 fallback env 了"。
func (s *Store) LoadActive(ctx context.Context, vendorID string) (*Credential, error) {
	if s.db == nil || s.cipher == nil {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT secret_credentials_encrypted
		  FROM vendor_account
		 WHERE vendor_id = ? AND status = 'active'
		 ORDER BY CASE WHEN COALESCE(label,'default')='default' THEN 0 ELSE 1 END,
		          updated_at DESC
		 LIMIT 1
	`, vendorID)
	var blob []byte
	if err := row.Scan(&blob); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("vendoraccount: scan: %w", err)
	}
	plain, err := s.cipher.Decrypt(blob)
	if err != nil {
		return nil, fmt.Errorf("vendoraccount: 解密（主密钥可能不对）: %w", err)
	}
	var cred Credential
	if err := json.Unmarshal(plain, &cred); err != nil {
		return nil, fmt.Errorf("vendoraccount: 反序列化: %w", err)
	}
	return &cred, nil
}

// ListActiveVendorIDs · 只做启动时 log · 报告哪些家已经 seed 好了。
func (s *Store) ListActiveVendorIDs(ctx context.Context) ([]string, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT vendor_id FROM vendor_account WHERE status = 'active' ORDER BY vendor_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Disable 软删除 · 保留历史（不真 DELETE 便于溯源）。
func (s *Store) Disable(ctx context.Context, vendorID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE vendor_account SET status = 'disabled', updated_at = ?
		 WHERE vendor_id = ?
	`, time.Now().UTC().Format(time.RFC3339), vendorID)
	return err
}
