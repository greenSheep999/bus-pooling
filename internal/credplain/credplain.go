// Package credplain · 手上号明文加密缓存 · decisions §12.5 扩展(§199)
//
// 上游 housepool 后端 后端**未提供 reveal 端点** · 拉号成功那一刻拿到明文后
// bus-pooling 必须自己存一份(加密) · 否则 push_pool / handoff 都没明文可导出。
//
// TTL 24h · used_at 后 24h · janitor 定时清 · 走 AES-GCM 加密(复用 internal/secrets)。
package credplain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/secrets"
)

// AuthMethod · 认证方式 · 落 credential_plaintext.auth_method
type AuthMethod string

const (
	AuthRefreshToken AuthMethod = "refresh_token"
	AuthAPIKey       AuthMethod = "api_key"
	AuthBearer       AuthMethod = "bearer"
)

// DefaultTTL · 明文缓存过期时间 · 24h 是"给用户操作缓冲 + 不给攻击者太长窗口"的折中
const DefaultTTL = 24 * time.Hour

// Plaintext · 一份号的明文
type Plaintext struct {
	CredentialID string
	AuthMethod   AuthMethod
	RefreshToken string // AuthRefreshToken 用
	AccessToken  string
	KiroAPIKey   string // AuthAPIKey 用
	Email        string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	UsedAt       *time.Time
}

// Store · 明文加密缓存服务
type Store struct {
	db     *sql.DB
	cipher *secrets.Cipher
	ttl    time.Duration
}

// New · 建 Store · cipher 必须非 nil(明文一律加密)
func New(db *sql.DB, cipher *secrets.Cipher) *Store {
	return &Store{db: db, cipher: cipher, ttl: DefaultTTL}
}

// SaveInput · Save 的入参 · 按 AuthMethod 只填相应字段
type SaveInput struct {
	CredentialID string
	AuthMethod   AuthMethod
	RefreshToken string
	AccessToken  string
	KiroAPIKey   string
	Email        string
}

// Save · 存一份加密明文 · 已存在则覆盖(用于 refresh_token 转轮等场景)
func (s *Store) Save(ctx context.Context, in SaveInput) error {
	if s == nil || s.cipher == nil {
		return errors.New("credplain: Store 未装配 cipher")
	}
	if in.CredentialID == "" {
		return errors.New("credplain: credential_id 不能空")
	}
	switch in.AuthMethod {
	case AuthRefreshToken:
		if in.RefreshToken == "" {
			return errors.New("credplain: refresh_token 方法需 RefreshToken 非空")
		}
	case AuthAPIKey:
		if in.KiroAPIKey == "" {
			return errors.New("credplain: api_key 方法需 KiroAPIKey 非空")
		}
	case AuthBearer:
		if in.AccessToken == "" {
			return errors.New("credplain: bearer 方法需 AccessToken 非空")
		}
	default:
		return fmt.Errorf("credplain: 未知 auth_method %q", in.AuthMethod)
	}

	var (
		rtEnc []byte
		atEnc []byte
		akEnc []byte
		err   error
	)
	if in.RefreshToken != "" {
		if rtEnc, err = s.cipher.Encrypt([]byte(in.RefreshToken)); err != nil {
			return fmt.Errorf("credplain: 加密 refresh_token: %w", err)
		}
	}
	if in.AccessToken != "" {
		if atEnc, err = s.cipher.Encrypt([]byte(in.AccessToken)); err != nil {
			return fmt.Errorf("credplain: 加密 access_token: %w", err)
		}
	}
	if in.KiroAPIKey != "" {
		if akEnc, err = s.cipher.Encrypt([]byte(in.KiroAPIKey)); err != nil {
			return fmt.Errorf("credplain: 加密 kiro_api_key: %w", err)
		}
	}

	now := time.Now().UTC()
	exp := now.Add(s.ttl)

	_, err = s.db.ExecContext(ctx, `
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
		in.CredentialID, string(in.AuthMethod),
		rtEnc, atEnc, akEnc,
		nullIfEmpty(in.Email), now.Format(time.RFC3339Nano), exp.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("credplain: 落库: %w", err)
	}
	return nil
}

// Get · 拿一份明文 · 过期 or used_at 后 24h 返 ErrNotFound
var ErrNotFound = errors.New("credplain: 找不到明文(未存 · 已过期 · 或用完清理)")

func (s *Store) Get(ctx context.Context, credentialID string) (*Plaintext, error) {
	if s == nil || s.cipher == nil {
		return nil, errors.New("credplain: Store 未装配")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT credential_id, auth_method,
		       refresh_token_encrypted, access_token_encrypted, kiro_api_key_encrypted,
		       COALESCE(email, ''), created_at, expires_at, used_at
		  FROM credential_plaintext
		 WHERE credential_id = ?`, credentialID)

	var (
		p            Plaintext
		method       string
		rtEnc, atEnc []byte
		akEnc        []byte
		createdAtStr string
		expiresAtStr string
		usedAtStr    sql.NullString
	)
	err := row.Scan(&p.CredentialID, &method, &rtEnc, &atEnc, &akEnc,
		&p.Email, &createdAtStr, &expiresAtStr, &usedAtStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("credplain: 读: %w", err)
	}
	p.AuthMethod = AuthMethod(method)
	if p.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtStr); err != nil {
		return nil, fmt.Errorf("credplain: 解析 created_at: %w", err)
	}
	if p.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAtStr); err != nil {
		return nil, fmt.Errorf("credplain: 解析 expires_at: %w", err)
	}
	if usedAtStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, usedAtStr.String)
		p.UsedAt = &t
	}

	// 过期判 · TTL 到 或 used_at 后 24h
	now := time.Now().UTC()
	if now.After(p.ExpiresAt) {
		return nil, ErrNotFound
	}
	if p.UsedAt != nil && now.After(p.UsedAt.Add(24*time.Hour)) {
		return nil, ErrNotFound
	}

	// 解密对应字段
	if len(rtEnc) > 0 {
		b, err := s.cipher.Decrypt(rtEnc)
		if err != nil {
			return nil, fmt.Errorf("credplain: 解密 refresh_token: %w", err)
		}
		p.RefreshToken = string(b)
	}
	if len(atEnc) > 0 {
		b, err := s.cipher.Decrypt(atEnc)
		if err != nil {
			return nil, fmt.Errorf("credplain: 解密 access_token: %w", err)
		}
		p.AccessToken = string(b)
	}
	if len(akEnc) > 0 {
		b, err := s.cipher.Decrypt(akEnc)
		if err != nil {
			return nil, fmt.Errorf("credplain: 解密 kiro_api_key: %w", err)
		}
		p.KiroAPIKey = string(b)
	}
	return &p, nil
}

// MarkUsed · handoff / push_pool 成功后标 used_at · 24h 后 janitor 硬删
func (s *Store) MarkUsed(ctx context.Context, credentialID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE credential_plaintext
		   SET used_at = ?
		 WHERE credential_id = ? AND used_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), credentialID)
	return err
}

// Purge · janitor 用 · 删过期或 used_at 后 24h 的行 · 返删除数
func (s *Store) Purge(ctx context.Context) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	cutUsed := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM credential_plaintext
		 WHERE expires_at <= ?
		    OR (used_at IS NOT NULL AND used_at <= ?)`, now, cutUsed)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
