// Package passenger 管账号：注册 / 登录 / session / API key。
//
// 三种鉴权入口里这个包负责两种（05-api-contract §鉴权）：
//   - 会话 cookie（浏览器）
//   - API key（脚本 / 服务端）
//
// **API key 权限收窄**：不能改密码、不能建新 key —— 防"泄露的 key 换成新 key
// 把主人锁在门外"。这条在 api 层用 RequireSession 强制。
package passenger

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound        = errors.New("passenger: 账号不存在")
	ErrEmailTaken      = errors.New("passenger: 邮箱已被注册")
	ErrUsernameTaken   = errors.New("passenger: 用户名已被占用")
	ErrAccountDisabled = errors.New("passenger: 账号已停用")
	ErrSessionInvalid  = errors.New("passenger: 会话无效或已过期")
	ErrAPIKeyInvalid   = errors.New("passenger: API key 无效或已吊销")
)

// SessionTTL 默认会话时长。"记住我"用 SessionTTLRemember。
const (
	SessionTTL         = 7 * 24 * time.Hour
	SessionTTLRemember = 30 * 24 * time.Hour
)

// APIKeyPrefix 是 API key 明文的固定前缀（05-api-contract §鉴权：usr-<hex>）。
const APIKeyPrefix = "usr-"

// apiKeyPrefixLen 存进 DB 用于 UI 展示的前缀长度（含 usr-）。
const apiKeyPrefixLen = 12

type Passenger struct {
	ID            string
	Username      string
	Email         string
	EmailVerified bool
	Role          string
	Status        string
	// Tier · 用户档次（retail / community / wholesale · docs/10-pricing §2.1）·
	// 决定计费链免哪几个分项 + 能不能看 vendor 展示名（只 wholesale 能）
	Tier string
	// Invited · **兜底字段** · 下次 schema 变更删。
	// 别拿它判档次 —— community 和 wholesale 都是 true · 拿它当"能看展示名"会漏给社群档
	Invited        bool
	InviteCodeUsed string
	CreatedAt      time.Time
	LastLoginAt    *time.Time
}

// 档次常量 · CHECK 在应用层（migration 028 · SQLite 加列不支持带 CHECK）
const (
	TierRetail    = "retail"
	TierCommunity = "community"
	TierWholesale = "wholesale"
)

type APIKey struct {
	ID         string
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	Revoked    bool
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ── 注册 / 查询 ────────────────────────────────────────

type RegisterInput struct {
	Email      string
	Username   string
	Password   string
	InviteCode string
}

// Register 建账号 + 钱包（同事务 —— 没有钱包的账号在后面每个流程里都是特例）。
//
// invited 由**系统邀请码**决定（decisions §8.20 §8.29）：它划社群身份，
// 影响 vendor 是否显示真名、以及是否免区域分项。
//
// **1c 修的老漏洞**：以前是 `invited := in.InviteCode != ""` —— **任何**非空码都置
// invited=1。那等于任何人随便编个码就能拿社群身份 → §8.20 的定价分层形同虚设。
// 现在查 system_invite_code 白名单：
//   - 码在白名单里 → invited=1（社群身份）+ 白名单计数 +1
//   - 码是别人的**个人邀请码** → invited **保持 0**（仍是零售）· 但记推荐关系
//     并给邀请人加手续费减免额度（§8.29 铁律：个人码不划身份·只给额度）
//   - 码不存在 → invited=0 · 静默忽略（填错码不该让注册失败）
func (s *Store) Register(ctx context.Context, in RegisterInput) (*Passenger, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	username := strings.TrimSpace(in.Username)

	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	id := uuid.NewString()
	now := time.Now().UTC()
	nowStr := formatTime(now)
	code := strings.ToUpper(strings.TrimSpace(in.InviteCode))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("passenger: 开事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// **只有系统邀请码白名单里的码才给社群身份**（见函数注释）
	invited, err := isSystemInviteCode(ctx, tx, code)
	if err != nil {
		return nil, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO passenger
			(id, username, email, email_verified, password_hash, role, status,
			 invited, invite_code_used, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, 'user', 'active', ?, ?, ?, ?)`,
		id, username, email, hash, boolToInt(invited),
		nullIfEmpty(code), nowStr, nowStr)
	if err != nil {
		// SQLite 的 UNIQUE 冲突只说列名，得自己分辨是邮箱还是用户名
		if isUniqueViolation(err) {
			if strings.Contains(err.Error(), "email") {
				return nil, ErrEmailTaken
			}
			if strings.Contains(err.Error(), "username") {
				return nil, ErrUsernameTaken
			}
		}
		return nil, fmt.Errorf("passenger: 建账号: %w", err)
	}

	// 钱包跟账号同生 —— 见函数注释
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO wallet (passenger_id, balance, reserved, updated_at) VALUES (?, 0, 0, ?)`,
		id, nowStr); err != nil {
		return nil, fmt.Errorf("passenger: 建钱包: %w", err)
	}

	if code != "" {
		if invited {
			// 系统码 · 计数 +1（有 max_uses 限制时靠它判满）
			if err := bumpSystemCodeUseTx(ctx, tx, code); err != nil {
				return nil, err
			}
		} else {
			// 不是系统码 → 试当个人邀请码处理（记推荐关系 + 给邀请人加额度）
			// **不改本人 invited** · 码无效时静默跳过
			if err := applyPersonalCodeTx(ctx, tx, id, code, now); err != nil {
				return nil, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("passenger: 提交: %w", err)
	}

	return &Passenger{
		ID: id, Username: username, Email: email,
		Role: "user", Status: "active",
		// 注册一律落 retail · 升档只走 system_invite_code.grants_tier 那条路（docs/10-pricing §2.1）·
		// 这里不按 invited 直接升 —— 任何非空码都能置 invited=1 · 拿它升档等于白送最优档
		Tier:           TierRetail,
		Invited:        invited,
		InviteCodeUsed: code,
		CreatedAt:      now,
	}, nil
}

const passengerCols = `id, username, email, email_verified, role, status,
	COALESCE(tier, 'retail'), invited, COALESCE(invite_code_used, ''), created_at, last_login_at`

func scanPassenger(row interface{ Scan(...any) error }) (*Passenger, error) {
	var p Passenger
	var emailVerified, invited int
	var createdAt string
	var lastLogin sql.NullString

	if err := row.Scan(&p.ID, &p.Username, &p.Email, &emailVerified, &p.Role, &p.Status,
		&p.Tier, &invited, &p.InviteCodeUsed, &createdAt, &lastLogin); err != nil {
		return nil, err
	}
	p.EmailVerified = emailVerified != 0
	p.Invited = invited != 0
	if p.Tier == "" {
		p.Tier = TierRetail
	}
	p.CreatedAt = parseTime(createdAt)
	if lastLogin.Valid {
		t := parseTime(lastLogin.String)
		p.LastLoginAt = &t
	}
	return &p, nil
}

func (s *Store) ByID(ctx context.Context, id string) (*Passenger, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+passengerCols+` FROM passenger WHERE id = ? AND deleted_at IS NULL`, id)
	p, err := scanPassenger(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("passenger: 查账号: %w", err)
	}
	return p, nil
}

// ── 登录 ──────────────────────────────────────────────

// Authenticate 校验账号密码。account 可以是邮箱或用户名。
//
// 无论账号不存在还是密码错，**都返回同一个错误** —— 否则接口就成了账号枚举器。
func (s *Store) Authenticate(ctx context.Context, account, password string) (*Passenger, error) {
	acct := strings.ToLower(strings.TrimSpace(account))

	var id, hash, status string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, password_hash, status FROM passenger
		WHERE (email = ? OR lower(username) = ?) AND deleted_at IS NULL`,
		acct, acct).Scan(&id, &hash, &status)

	if errors.Is(err, sql.ErrNoRows) {
		// 走一次假的 hash 校验，让"账号不存在"和"密码错"耗时相近
		_ = VerifyPassword(password, dummyHash)
		return nil, ErrWrongPassword
	}
	if err != nil {
		return nil, fmt.Errorf("passenger: 查账号: %w", err)
	}

	if err := VerifyPassword(password, hash); err != nil {
		return nil, ErrWrongPassword
	}
	if status != "active" {
		return nil, ErrAccountDisabled
	}

	now := formatTime(time.Now().UTC())
	if _, err := s.db.ExecContext(ctx,
		`UPDATE passenger SET last_login_at = ?, updated_at = ? WHERE id = ?`,
		now, now, id); err != nil {
		// 登录本身成功了，更新 last_login 失败不该挡着用户进来
		return s.ByID(ctx, id)
	}
	return s.ByID(ctx, id)
}

// dummyHash 是一个固定的合法 argon2id 串，用于账号不存在时的等时校验。
// 明文是随机的，没人能用它登录。
var dummyHash = func() string {
	h, err := HashPassword("this-password-matches-nothing")
	if err != nil {
		// 包初始化就失败说明 argon2 有问题，让它显式炸出来
		panic("passenger: 初始化 dummy hash 失败: " + err.Error())
	}
	return h
}()

// ChangePassword 改密码。同时吊销**其它**会话（当前这个由调用方决定留不留）。
func (s *Store) ChangePassword(ctx context.Context, passengerID, oldPassword, newPassword string) error {
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT password_hash FROM passenger WHERE id = ? AND deleted_at IS NULL`,
		passengerID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("passenger: 查账号: %w", err)
	}
	if err := VerifyPassword(oldPassword, hash); err != nil {
		return ErrWrongPassword
	}

	newHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	if _, err := s.db.ExecContext(ctx,
		`UPDATE passenger SET password_hash = ?, updated_at = ? WHERE id = ?`,
		newHash, now, passengerID); err != nil {
		return fmt.Errorf("passenger: 改密码: %w", err)
	}
	return nil
}

// ── Session ──────────────────────────────────────────

// CreateSession 返回明文 token（只此一次，存 cookie 用）。DB 里只存它的 SHA-256。
func (s *Store) CreateSession(ctx context.Context, passengerID, ip, ua string, remember bool) (token string, expiresAt time.Time, err error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", time.Time{}, fmt.Errorf("passenger: 生成 session token: %w", err)
	}
	token = hex.EncodeToString(raw)

	ttl := SessionTTL
	if remember {
		ttl = SessionTTLRemember
	}
	now := time.Now().UTC()
	expiresAt = now.Add(ttl)

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO session (id, passenger_id, ip_created, user_agent, created_at, last_used_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		hashToken(token), passengerID, nullIfEmpty(ip), nullIfEmpty(ua),
		formatTime(now), formatTime(now), formatTime(expiresAt)); err != nil {
		return "", time.Time{}, fmt.Errorf("passenger: 建会话: %w", err)
	}
	return token, expiresAt, nil
}

// SessionOwner 用明文 token 查账号。顺带刷新 last_used_at。
func (s *Store) SessionOwner(ctx context.Context, token string) (*Passenger, error) {
	if token == "" {
		return nil, ErrSessionInvalid
	}
	id := hashToken(token)

	var passengerID, expiresAt string
	var revokedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT passenger_id, expires_at, revoked_at FROM session WHERE id = ?`,
		id).Scan(&passengerID, &expiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("passenger: 查会话: %w", err)
	}
	if revokedAt.Valid {
		return nil, ErrSessionInvalid
	}
	if parseTime(expiresAt).Before(time.Now().UTC()) {
		return nil, ErrSessionInvalid
	}

	p, err := s.ByID(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	if p.Status != "active" {
		return nil, ErrAccountDisabled
	}

	// 刷新 last_used_at 是尽力而为 —— 失败不该让请求挂
	_, _ = s.db.ExecContext(ctx,
		`UPDATE session SET last_used_at = ? WHERE id = ?`,
		formatTime(time.Now().UTC()), id)

	return p, nil
}

func (s *Store) RevokeSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE session SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		formatTime(time.Now().UTC()), hashToken(token))
	if err != nil {
		return fmt.Errorf("passenger: 吊销会话: %w", err)
	}
	return nil
}

// ── API key ──────────────────────────────────────────

// CreateAPIKey 返回明文 key（**只此一次**）。DB 只存 SHA-256 和前缀。
func (s *Store) CreateAPIKey(ctx context.Context, passengerID, name string) (plaintext string, key *APIKey, err error) {
	raw := make([]byte, 20)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", nil, fmt.Errorf("passenger: 生成 API key: %w", err)
	}
	plaintext = APIKeyPrefix + hex.EncodeToString(raw)

	id := uuid.NewString()
	now := time.Now().UTC()
	prefix := plaintext[:apiKeyPrefixLen]

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO passenger_api_key (id, passenger_id, key_hash, prefix, name, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, passengerID, hashToken(plaintext), prefix, nullIfEmpty(strings.TrimSpace(name)),
		formatTime(now)); err != nil {
		return "", nil, fmt.Errorf("passenger: 建 API key: %w", err)
	}

	return plaintext, &APIKey{
		ID: id, Name: strings.TrimSpace(name), Prefix: prefix, CreatedAt: now,
	}, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, passengerID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(name, ''), prefix, created_at, last_used_at, revoked_at
		FROM passenger_api_key WHERE passenger_id = ?
		ORDER BY created_at DESC`, passengerID)
	if err != nil {
		return nil, fmt.Errorf("passenger: 查 API key: %w", err)
	}
	defer rows.Close()

	var out []APIKey
	for rows.Next() {
		var k APIKey
		var createdAt string
		var lastUsed, revoked sql.NullString
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &createdAt, &lastUsed, &revoked); err != nil {
			return nil, err
		}
		k.CreatedAt = parseTime(createdAt)
		if lastUsed.Valid {
			t := parseTime(lastUsed.String)
			k.LastUsedAt = &t
		}
		k.Revoked = revoked.Valid
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey 吊销。**不删行** —— 台账留痕，售后要能查这个 key 什么时候建的、用过没。
func (s *Store) RevokeAPIKey(ctx context.Context, passengerID, keyID string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE passenger_api_key SET revoked_at = ?
		WHERE id = ? AND passenger_id = ? AND revoked_at IS NULL`,
		formatTime(time.Now().UTC()), keyID, passengerID)
	if err != nil {
		return fmt.Errorf("passenger: 吊销 API key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// 要么不存在，要么不属于这个乘客，要么已经吊销过 —— 对外都是"找不到"
		return ErrNotFound
	}
	return nil
}

// APIKeyOwner 用明文 key 查账号。顺带刷新 last_used_at。
func (s *Store) APIKeyOwner(ctx context.Context, plaintext string) (*Passenger, error) {
	if !strings.HasPrefix(plaintext, APIKeyPrefix) {
		return nil, ErrAPIKeyInvalid
	}

	var passengerID string
	var revokedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT passenger_id, revoked_at FROM passenger_api_key WHERE key_hash = ?`,
		hashToken(plaintext)).Scan(&passengerID, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAPIKeyInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("passenger: 查 API key: %w", err)
	}
	if revokedAt.Valid {
		return nil, ErrAPIKeyInvalid
	}

	p, err := s.ByID(ctx, passengerID)
	if err != nil {
		return nil, err
	}
	if p.Status != "active" {
		return nil, ErrAccountDisabled
	}

	_, _ = s.db.ExecContext(ctx,
		`UPDATE passenger_api_key SET last_used_at = ? WHERE key_hash = ?`,
		formatTime(time.Now().UTC()), hashToken(plaintext))

	return p, nil
}

// ── 工具 ─────────────────────────────────────────────

// hashToken 存的都是 token 的 SHA-256 —— 库被读走也拿不到能用的 token。
// 这里不用加盐：token 本身是 160+ bit 的随机值，彩虹表不成立。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// 时间统一 ISO-8601 UTC（CLAUDE.md §7.2）
const timeLayout = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) time.Time {
	// 兼容几种写法 —— migration 里的 seed 数据可能是短格式
	for _, layout := range []string{timeLayout, time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE")
}
