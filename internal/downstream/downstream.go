// Package downstream 存乘客的两份下游配置：
//
//   - passengerpool 地址 + admin token（我方双写号池时用这份 token 调他的 housepool）
//   - webhook 地址 + 签名 secret（我方推事件时用这份 secret 做 HMAC-SHA256）
//
// 铁律（CLAUDE.md §11 + docs/06-db-schema §3）：
//
//   - **明文永不落库**：token / secret 全走 internal/secrets 加密
//   - 对外**永远只回 mask**（`kiro_admin_••…{last4}` / `whsec_••…{last4}`）
//   - 明文只在两处出现：PUT passengerpool 收到入参那一瞬间 + 轮换 secret 时返回一次
package downstream

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/secrets"
)

var (
	// ErrNotFound 该乘客还没配过下游 —— **不是错误**，Store.Get 会返 zero-value + ErrNotFound，
	// 让 handler 决定是返"未配置"还是走默认。
	ErrNotFound = errors.New("downstream: 该乘客未配置下游")
)

// Config 是一份完整下游配置。指针字段 nil = 未配置。
//
// 加密字段（*Encrypted）保持 blob 形态在 Store 内流转 —— 只在必要处解密
// （比如真发 webhook / 真调 passengerpool 时），减少明文出现的时间窗口。
type Config struct {
	PassengerID string

	// passengerpool
	PassengerpoolURL             string
	PassengerpoolTokenEncrypted  []byte // AES-GCM · 内部字段
	PassengerpoolTokenConfigured bool   // 是否已配 token（避免解密只为判空）

	// webhook
	WebhookURL              string
	WebhookSecretEncrypted  []byte
	WebhookSecretConfigured bool
	WebhookEnabled          bool     // migration 038 · UI 顶部开关 · 未配 URL/secret 时 UI 视为 false
	WebhookEvents           []string // migration 038 · nil = 未配(全订阅兜底) · 用户勾了什么就是什么

	// 4 条推送策略（decisions §8.25 · 前端「我的号池」页）
	PushOnPull     bool
	ResyncOnDead   bool
	RetryOnFailure bool
	BusOnly        bool

	UpdatedAt time.Time
}

// Defaults 是从没配过时的那份。
//
// 4 条推送策略默认全开 · bus_only 关（跟 001_init.sql 的 DEFAULT 一致）。
// webhook 默认启用(需要有 URL + secret 才真发)· events=nil 视为全订阅 4 事件(dispatcher 内部兜底)。
func Defaults(passengerID string) Config {
	return Config{
		PassengerID:    passengerID,
		PushOnPull:     true,
		ResyncOnDead:   true,
		RetryOnFailure: true,
		BusOnly:        false,
		WebhookEnabled: true,
		WebhookEvents:  nil,
	}
}

type Store struct {
	db     *sql.DB
	cipher *secrets.Cipher
}

// NewStore 建 Store · cipher 必须非 nil（下游配置只有加密才能安全落库）。
func NewStore(db *sql.DB, cipher *secrets.Cipher) *Store {
	return &Store{db: db, cipher: cipher}
}

// Get 读一份配置。没有 = 返回 Defaults + ErrNotFound（handler 自己判决用不用默认值）。
func (s *Store) Get(ctx context.Context, passengerID string) (Config, error) {
	out := Defaults(passengerID)
	var (
		poolURL       sql.NullString
		poolToken     []byte
		hookURL       sql.NullString
		hookSecret    []byte
		pushOnPull    int64
		resyncOnDead  int64
		retryOnFail   int64
		busOnly       int64
		hookEnabled   int64
		hookEventsRaw sql.NullString
		updatedAtRaw  string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT passengerpool_url, secret_passengerpool_token_encrypted,
		       webhook_url, secret_webhook_secret_encrypted,
		       push_on_pull, resync_on_dead, retry_on_failure, bus_only,
		       webhook_enabled, webhook_events_json,
		       updated_at
		  FROM passenger_downstream
		 WHERE passenger_id = ?`, passengerID).
		Scan(&poolURL, &poolToken, &hookURL, &hookSecret,
			&pushOnPull, &resyncOnDead, &retryOnFail, &busOnly,
			&hookEnabled, &hookEventsRaw, &updatedAtRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return Config{}, fmt.Errorf("downstream: 读配置: %w", err)
	}

	if poolURL.Valid {
		out.PassengerpoolURL = poolURL.String
	}
	if len(poolToken) > 0 {
		out.PassengerpoolTokenEncrypted = poolToken
		out.PassengerpoolTokenConfigured = true
	}
	if hookURL.Valid {
		out.WebhookURL = hookURL.String
	}
	if len(hookSecret) > 0 {
		out.WebhookSecretEncrypted = hookSecret
		out.WebhookSecretConfigured = true
	}
	out.PushOnPull = pushOnPull != 0
	out.ResyncOnDead = resyncOnDead != 0
	out.RetryOnFailure = retryOnFail != 0
	out.BusOnly = busOnly != 0
	out.WebhookEnabled = hookEnabled != 0
	if hookEventsRaw.Valid && hookEventsRaw.String != "" {
		var evs []string
		if jsonErr := json.Unmarshal([]byte(hookEventsRaw.String), &evs); jsonErr == nil {
			out.WebhookEvents = evs
		}
	}
	out.UpdatedAt = parseTime(updatedAtRaw)
	return out, nil
}

// SavePassengerpool 更新 passengerpool url + token。
//
// **token 为空**：只更新 url，保留原 token（前端只想改 URL 的场景）。
// **token 非空**：加密后覆盖，明文用后即弃 —— 不 log、不返回、不落任何非 blob 字段。
func (s *Store) SavePassengerpool(ctx context.Context, passengerID, url, plaintextToken string) error {
	if s.cipher == nil {
		return errors.New("downstream: cipher 未装配")
	}

	var encrypted []byte
	if plaintextToken != "" {
		var err error
		encrypted, err = s.cipher.EncryptString(plaintextToken)
		if err != nil {
			return fmt.Errorf("downstream: 加密 token: %w", err)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	return s.upsertPassengerpool(ctx, passengerID, url, encrypted, now, plaintextToken != "")
}

// upsertPassengerpool 是 SavePassengerpool 的落库半段。
//
// 两种情形分开 SQL：token 有 → 覆盖 · token 无 → 只覆盖 url。避免"传空 token
// 意外把已配好的 token 清掉"（前端只想改 url 时反而更危险）。
func (s *Store) upsertPassengerpool(
	ctx context.Context,
	passengerID, url string,
	encryptedToken []byte,
	now string,
	writeToken bool,
) error {
	if writeToken {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO passenger_downstream
			  (passenger_id, passengerpool_url, secret_passengerpool_token_encrypted, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (passenger_id) DO UPDATE SET
			  passengerpool_url                    = excluded.passengerpool_url,
			  secret_passengerpool_token_encrypted = excluded.secret_passengerpool_token_encrypted,
			  updated_at                           = excluded.updated_at`,
			passengerID, nullIfEmpty(url), encryptedToken, now)
		if err != nil {
			return fmt.Errorf("downstream: 写 passengerpool: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO passenger_downstream
		  (passenger_id, passengerpool_url, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT (passenger_id) DO UPDATE SET
		  passengerpool_url = excluded.passengerpool_url,
		  updated_at        = excluded.updated_at`,
		passengerID, nullIfEmpty(url), now)
	if err != nil {
		return fmt.Errorf("downstream: 写 passengerpool url: %w", err)
	}
	return nil
}

// SaveWebhookURL 更新 webhook 地址（不动 secret）。
func (s *Store) SaveWebhookURL(ctx context.Context, passengerID, url string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO passenger_downstream
		  (passenger_id, webhook_url, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT (passenger_id) DO UPDATE SET
		  webhook_url = excluded.webhook_url,
		  updated_at  = excluded.updated_at`,
		passengerID, nullIfEmpty(url), now)
	if err != nil {
		return fmt.Errorf("downstream: 写 webhook url: %w", err)
	}
	return nil
}

// SaveWebhookEnabled 更新 webhook 启用开关(1e-2 补 · P0-1)。
//
// **前端 UI 顶部的启用开关** · 之前只在前端 state 里改 · 后端 PUT 直接扔字段 ·
// 现在真落库 · dispatcher 派发前查这个字段决定发不发。
func (s *Store) SaveWebhookEnabled(ctx context.Context, passengerID string, enabled bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	flag := int64(0)
	if enabled {
		flag = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO passenger_downstream
		  (passenger_id, webhook_enabled, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT (passenger_id) DO UPDATE SET
		  webhook_enabled = excluded.webhook_enabled,
		  updated_at      = excluded.updated_at`,
		passengerID, flag, now)
	if err != nil {
		return fmt.Errorf("downstream: 写 webhook enabled: %w", err)
	}
	return nil
}

// SaveWebhookEvents 更新订阅事件白名单(1e-2 补 · P0-2)。
//
// **events == nil 或空数组** → 写 NULL(视为"未设置" · dispatcher 兜底当全订阅)。
// 用户主动清空所有事件 · 落 NULL 语义等同"跟没设一样" —— 若用户想彻底不收 ·
// 应该关 webhook_enabled 而不是清空 events(那样测试事件也能收到 · 更符合心智)。
// 白名单校验(必须是 4 个事件之一) · 在 api 层做 · Store 只落值。
func (s *Store) SaveWebhookEvents(ctx context.Context, passengerID string, events []string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var raw any = nil
	if len(events) > 0 {
		b, err := json.Marshal(events)
		if err != nil {
			return fmt.Errorf("downstream: 编码 webhook events: %w", err)
		}
		raw = string(b)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO passenger_downstream
		  (passenger_id, webhook_events_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT (passenger_id) DO UPDATE SET
		  webhook_events_json = excluded.webhook_events_json,
		  updated_at          = excluded.updated_at`,
		passengerID, raw, now)
	if err != nil {
		return fmt.Errorf("downstream: 写 webhook events: %w", err)
	}
	return nil
}

// RotateWebhookSecret 生成一份新 secret · 加密落库 · 返回明文（**只返一次**）。
//
// 旧 secret 立即失效 —— 调用方必须提示用户"手抄一份，之后再也拿不到"。
func (s *Store) RotateWebhookSecret(ctx context.Context, passengerID string) (plaintext string, err error) {
	if s.cipher == nil {
		return "", errors.New("downstream: cipher 未装配")
	}

	plaintext, err = generateSecretHex()
	if err != nil {
		return "", err
	}

	encrypted, err := s.cipher.EncryptString(plaintext)
	if err != nil {
		return "", fmt.Errorf("downstream: 加密 webhook secret: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO passenger_downstream
		  (passenger_id, secret_webhook_secret_encrypted, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT (passenger_id) DO UPDATE SET
		  secret_webhook_secret_encrypted = excluded.secret_webhook_secret_encrypted,
		  updated_at                      = excluded.updated_at`,
		passengerID, encrypted, now)
	if err != nil {
		return "", fmt.Errorf("downstream: 写 webhook secret: %w", err)
	}
	return plaintext, nil
}

// DecryptPassengerpoolToken 拿明文 token（**只在真调 passengerpool 时用**）。
//
// handler 不该直接调这个 —— 只在推送流水线里用。列在 Store 上是因为 secrets 是私有依赖。
func (s *Store) DecryptPassengerpoolToken(encrypted []byte) (string, error) {
	if s.cipher == nil {
		return "", errors.New("downstream: cipher 未装配")
	}
	return s.cipher.DecryptString(encrypted)
}

// DecryptWebhookSecret 拿明文 secret（**只在真签 webhook 时用**）。
func (s *Store) DecryptWebhookSecret(encrypted []byte) (string, error) {
	if s.cipher == nil {
		return "", errors.New("downstream: cipher 未装配")
	}
	return s.cipher.DecryptString(encrypted)
}

// ── 工具 ────────────────────────────────────────────

// WebhookSecretPrefix · secret 明文的固定前缀。跟 Stripe / GitHub 的做法一致
// (Stripe: whsec_ · GitHub: ghs_) —— 用户在自己代码里看到前缀就知道"这是 webhook
// 签名密钥" · 用错地方(比如粘到 passengerpool token 栏)时能一眼识别。
//
// **落库存整串**(含前缀) · 打码 / 明文格式一致 · 别再在 api 层人为拼前缀 ·
// 那样会造成明文态跟打码态"看着不是一个东西"的错觉(1e 收尾 bug 修正)。
const WebhookSecretPrefix = "whsec_"

// generateSecretHex 生成 32 字节 = 64 位 hex 的 webhook secret · 前缀 whsec_。
// 跟 vendor 侧 HMAC 长度一致（SHA-256）· 前缀跟行业规范(Stripe / GitHub)对齐。
func generateSecretHex() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("downstream: 生成 secret: %w", err)
	}
	return WebhookSecretPrefix + hex.EncodeToString(buf), nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
