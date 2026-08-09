// Package handoff 走 pending_handoff 状态机（09-transactions §4 · P0-3 三段式）。
//
// 三步分工：
//
//	① IssueToken  · POST /me/handoff              → status: token_issued
//	② Fulfill     · GET  /me/handoff/{token}      → status: fulfilled  · 每次实时从 housepool 读明文
//	③ Confirm     · POST /me/handoff/{token}/confirm → 触发 DELETE + 台账 handed_off → status: completed
//
// 明文**永不落库**（我方 DB 无 plaintext 字段） · 断线重试靠 token TTL 内可反复 GET 明文。
// janitor 兜底：token_issued / fulfilled 过 5 min 未推进 → expired* → 号仍在池里可重来。
package handoff

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TokenTTL 是 token 的有效期（09-transactions §4 · 默认 5 min）。
// 装配时可以覆盖，测试里也用它注入较短的时长。
const TokenTTL = 5 * time.Minute

// Status 是 pending_handoff 的状态（DB CHECK 约束里的取值）。
type Status string

const (
	StatusTokenIssued         Status = "token_issued"
	StatusFulfilled           Status = "fulfilled"
	StatusConfirmed           Status = "confirmed"
	StatusCompleted           Status = "completed"
	StatusExpired             Status = "expired"
	StatusExpiredAfterFulfill Status = "expired_after_fulfill"
	StatusNeedManual          Status = "need_manual"

	// StatusPlaceholderDelivered 联调路径专用（BP_ALLOW_HANDOFF_PLACEHOLDER=1）·
	// fulfill 返占位字符串时推到这个状态·**不允许**接下来走真明文 confirm DELETE 分支。
	StatusPlaceholderDelivered Status = "placeholder_delivered"
	// StatusConfirmedPlaceholder 占位路径 confirm 后的终态·**不做**任何外部动作·
	// 号仍在 pool 里·避免降级路径删真号。
	StatusConfirmedPlaceholder Status = "confirmed_placeholder"
)

var (
	// ErrTokenExpired token 已过期或不存在（对外统一映射成 404 token_expired）
	ErrTokenExpired = errors.New("handoff: token 已过期")
	// ErrCredentialNotOwned 传入的 credential 不归此乘客（或已 handoff）
	ErrCredentialNotOwned = errors.New("handoff: 号不归此乘客")
	// ErrStaleTransition 状态不匹配 · 并发保护，非 bug
	ErrStaleTransition = errors.New("handoff: 状态已被别人推进")
	// ErrEmptyCredentials 没传号
	ErrEmptyCredentials = errors.New("handoff: 至少要传一个 credential_id")
)

// Pending 是一行 pending_handoff。
type Pending struct {
	ID            string
	PassengerID   string
	DownloadToken string
	CredentialIDs []string
	Status        Status
	FulfillCount  int
	FulfilledAt   *time.Time
	ConfirmedAt   *time.Time
	CompletedAt   *time.Time
	ExpiresAt     time.Time
	Error         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Store 管 pending_handoff 表读写。
//
// 不管 housepool DELETE / credential_ledger 状态更新 —— 那是 handler 的事（先做外部
// 动作，再改本地状态 · CLAUDE.md §7.1）。本包只负责状态机推进。
type Store struct {
	db  *sql.DB
	ttl time.Duration
	// now / newToken 可注入，测试用来控时钟和 token 生成
	now      func() time.Time
	newID    func() string
	newToken func() (string, error)
}

// NewStore 建 Store。ttl 传 0 = 用默认 5 min。
func NewStore(db *sql.DB, ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = TokenTTL
	}
	return &Store{
		db:       db,
		ttl:      ttl,
		now:      func() time.Time { return time.Now().UTC() },
		newID:    uuid.NewString,
		newToken: newHexToken,
	}
}

// IssueTokenInput 是 ① 发 token 的入参。
type IssueTokenInput struct {
	PassengerID   string
	CredentialIDs []string
}

// IssueToken ① 发 token · 写 pending_handoff 行 status=token_issued。
//
// **归属校验**：CredentialIDs 必须都是**该乘客名下的活号**（record group 里未派或
// 已进车的都可以）· 不然整批拒绝，避免"我把别人的号 handoff 出去"。
//
// 返回的 token 是 32 位十六进制 · 明文 · 后续两步都用它当鉴权。
func (s *Store) IssueToken(ctx context.Context, in IssueTokenInput) (*Pending, error) {
	if len(in.CredentialIDs) == 0 {
		return nil, ErrEmptyCredentials
	}
	if err := s.checkOwnership(ctx, in.CredentialIDs, in.PassengerID); err != nil {
		return nil, err
	}

	token, err := s.newToken()
	if err != nil {
		return nil, fmt.Errorf("handoff: 生成 token: %w", err)
	}

	// 顺便把这些号在 housepool 侧标 disabled=true —— 但那步在 handler 里做（先外后内）。
	// 本方法只落 pending_handoff 行。
	credIDsJSON, err := json.Marshal(in.CredentialIDs)
	if err != nil {
		return nil, fmt.Errorf("handoff: 编码 credential_ids: %w", err)
	}

	now := s.now()
	expires := now.Add(s.ttl)
	p := Pending{
		ID:          s.newID(),
		PassengerID: in.PassengerID,
		// Pending.DownloadToken 保留**明文** —— 只此一次返给 handler，之后 API 层
		// 从 URL 拿明文再 hash 查 · 明文永远不落库
		DownloadToken: token,
		CredentialIDs: in.CredentialIDs,
		Status:        StatusTokenIssued,
		ExpiresAt:     expires,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	// 落库存 hash · 跟 session / API key 一样标准（review 指出 token 明文存是漏洞）
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO pending_handoff
		  (id, passenger_id, download_token, credential_ids_json, status,
		   fulfill_count, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		p.ID, p.PassengerID, hashToken(token), string(credIDsJSON),
		string(p.Status), formatTime(expires), formatTime(now), formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("handoff: 写 pending_handoff: %w", err)
	}
	return &p, nil
}

// hashToken 对 handoff download_token 做 sha256 · 落库时用。
// 跟 passenger.hashAPIKey / session hash 用同一手法。
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GetByToken 按 token 读一行 · 校验 TTL（过期直接返 ErrTokenExpired）。
//
// 也用于 Fulfill / Confirm 前的一次性查行。
func (s *Store) GetByToken(ctx context.Context, token string) (*Pending, error) {
	p, err := s.getByTokenNoTTL(ctx, token)
	if err != nil {
		return nil, err
	}
	// 已终态的 completed / need_manual 直接按状态返回（不校验 TTL）
	// 但 token_issued / fulfilled 状态下如果过了 expires_at，视为过期
	if p.Status == StatusTokenIssued || p.Status == StatusFulfilled {
		if s.now().After(p.ExpiresAt) {
			return nil, ErrTokenExpired
		}
	}
	return p, nil
}

func (s *Store) getByTokenNoTTL(ctx context.Context, token string) (*Pending, error) {
	// 用 hash 查（落库是 hash）· scanRow 已经不把 hash 塞进 Pending.DownloadToken。
	row := s.db.QueryRowContext(ctx, selectPending+` WHERE download_token = ?`, hashToken(token))
	p, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTokenExpired
	}
	if err != nil {
		return nil, fmt.Errorf("handoff: 读 pending_handoff: %w", err)
	}
	return &p, nil
}

// MarkFulfilled ② fulfill 后调 · 推进到 fulfilled + 记 fulfill_count + fulfilled_at。
//
// **允许多次调用** —— TTL 内客户端可反复 GET 明文（断线重试）。
// 已经是 fulfilled 状态时不再改 fulfilled_at（那是**首次** fulfill 的时刻），
// 只累加 fulfill_count；不改状态。
func (s *Store) MarkFulfilled(ctx context.Context, id string) error {
	now := s.now()
	// 首次：token_issued → fulfilled  · 记 fulfilled_at
	// 重复：fulfilled → fulfilled     · 只累加 fulfill_count
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_handoff
		   SET status = 'fulfilled',
		       fulfilled_at = COALESCE(fulfilled_at, ?),
		       fulfill_count = fulfill_count + 1,
		       updated_at = ?
		 WHERE id = ? AND status IN ('token_issued', 'fulfilled')`,
		formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("handoff: 推进 fulfilled: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleTransition
	}
	return nil
}

// MarkConfirmed ③ confirm 前推进 fulfilled → confirmed。
//
// **必须**先由 handler 做完 housepool DELETE + credential_ledger 更新，再调本方法
// 落 status=completed（用 MarkCompleted）。本方法是"你保证接下来会执行外部动作"的锁。
//
// 幂等：已经 confirmed / completed 状态时不算错（返回 nil），避免客户端重放报错。
func (s *Store) MarkConfirmed(ctx context.Context, id string) error {
	now := s.now()
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_handoff
		   SET status = 'confirmed',
		       confirmed_at = COALESCE(confirmed_at, ?),
		       updated_at = ?
		 WHERE id = ? AND status = 'fulfilled'`,
		formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("handoff: 推进 confirmed: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 已 confirmed / completed → 幂等静默返回；其它状态才是错
		p, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		if p.Status == StatusConfirmed || p.Status == StatusCompleted {
			return nil
		}
		return ErrStaleTransition
	}
	return nil
}

// MarkPlaceholderDelivered · **仅联调路径** · 占位字符串给出去时用。
//
// 跟 MarkFulfilled 的关键差别：目标状态是 placeholder_delivered · **不是**
// fulfilled。这样后续 confirm handler 一看状态就知道号是假的·必须走
// MarkConfirmedPlaceholder 而不能走真 DELETE 分支。
//
// **绝不能**在生产（BP_HANDOFF_TRUE_PLAINTEXT=1）路径下调这个方法。
func (s *Store) MarkPlaceholderDelivered(ctx context.Context, id string) error {
	now := s.now()
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_handoff
		   SET status = 'placeholder_delivered',
		       fulfilled_at = COALESCE(fulfilled_at, ?),
		       fulfill_count = fulfill_count + 1,
		       updated_at = ?
		 WHERE id = ? AND status IN ('token_issued', 'placeholder_delivered')`,
		formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("handoff: 推进 placeholder_delivered: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleTransition
	}
	return nil
}

// MarkConfirmedPlaceholder · **仅联调路径** · placeholder_delivered → confirmed_placeholder。
//
// **不做**任何外部动作·号仍在 pool 里。跟 MarkConfirmed + MarkCompleted 不同：
// - MarkConfirmed → completeHandoff（外部 DELETE） → MarkCompleted
// - MarkConfirmedPlaceholder → 什么都不做 → 终态
//
// 保证降级路径永远不会真删号。
func (s *Store) MarkConfirmedPlaceholder(ctx context.Context, id string) error {
	now := s.now()
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_handoff
		   SET status = 'confirmed_placeholder',
		       confirmed_at = COALESCE(confirmed_at, ?),
		       completed_at = COALESCE(completed_at, ?),
		       updated_at = ?
		 WHERE id = ? AND status = 'placeholder_delivered'`,
		formatTime(now), formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("handoff: 推进 confirmed_placeholder: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		p, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		if p.Status == StatusConfirmedPlaceholder {
			return nil
		}
		return ErrStaleTransition
	}
	return nil
}

// MarkCompleted 落 status=completed（housepool DELETE + credential_ledger 更新已做完时调）。
//
// 幂等：已经 completed 时不再改 completed_at（保留首次时间）。
func (s *Store) MarkCompleted(ctx context.Context, id string) error {
	now := s.now()
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_handoff
		   SET status = 'completed',
		       completed_at = COALESCE(completed_at, ?),
		       updated_at = ?
		 WHERE id = ? AND status IN ('confirmed', 'completed')`,
		formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("handoff: 推进 completed: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleTransition
	}
	return nil
}

// MarkExpired 兜底把 token_issued → expired（或 fulfilled → expired_after_fulfill）。
// janitor 用。
func (s *Store) MarkExpired(ctx context.Context, id string, from Status) error {
	var to Status
	switch from {
	case StatusTokenIssued:
		to = StatusExpired
	case StatusFulfilled:
		to = StatusExpiredAfterFulfill
	default:
		return fmt.Errorf("handoff: 状态 %s 不能标 expired", from)
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_handoff SET status = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		string(to), formatTime(s.now()), id, string(from))
	if err != nil {
		return fmt.Errorf("handoff: 推进 expired: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStaleTransition
	}
	return nil
}

// Get 按 id 读一行。**不校验 TTL** —— 内部推进用。
func (s *Store) Get(ctx context.Context, id string) (*Pending, error) {
	row := s.db.QueryRowContext(ctx, selectPending+` WHERE id = ?`, id)
	p, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTokenExpired
	}
	if err != nil {
		return nil, fmt.Errorf("handoff: 读 pending_handoff: %w", err)
	}
	return &p, nil
}

// FindStale 找卡在某状态且过期的行 · janitor 用。
//
// token_issued / fulfilled 才是"可能过期"的状态 —— completed / expired* 都是终态不看。
func (s *Store) FindStale(ctx context.Context, status Status, limit int) ([]Pending, error) {
	if status != StatusTokenIssued && status != StatusFulfilled {
		return nil, fmt.Errorf("handoff: 不能扫状态 %s（不过期）", status)
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		selectPending+` WHERE status = ? AND expires_at < ? ORDER BY expires_at LIMIT ?`,
		string(status), formatTime(s.now()), limit)
	if err != nil {
		return nil, fmt.Errorf("handoff: 扫过期行: %w", err)
	}
	defer rows.Close()

	var out []Pending
	for rows.Next() {
		p, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FindStuckConfirmed 找卡在 confirmed 但没推到 completed 的行·超时阈值 stuckAfter。
//
// 触发场景：confirm handler 已推状态 confirmed·但接下来的 housepool DELETE
// 失败（网络断 · 崩溃 · kiro.rs 503）· pending_handoff 卡在 confirmed·
// **号还在 pool 里没删** —— janitor 定期扫这些·重试 completeHandoff 或转 need_manual。
//
// 用 confirmed_at 而不是 expires_at 判"卡了多久"（confirmed 之后已经不看 TTL）。
func (s *Store) FindStuckConfirmed(ctx context.Context, stuckAfter time.Duration, limit int) ([]Pending, error) {
	if limit <= 0 {
		limit = 50
	}
	cutoff := s.now().Add(-stuckAfter)
	rows, err := s.db.QueryContext(ctx,
		selectPending+` WHERE status = 'confirmed' AND confirmed_at IS NOT NULL AND confirmed_at < ? ORDER BY confirmed_at LIMIT ?`,
		formatTime(cutoff), limit)
	if err != nil {
		return nil, fmt.Errorf("handoff: 扫 stuck confirmed: %w", err)
	}
	defer rows.Close()

	var out []Pending
	for rows.Next() {
		p, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MarkNeedManual 把 pending_handoff 标 need_manual（human 介入）· janitor 重试若干次后调。
func (s *Store) MarkNeedManual(ctx context.Context, id, reason string) error {
	now := s.now()
	_, err := s.db.ExecContext(ctx, `
		UPDATE pending_handoff
		   SET status = 'need_manual', error = ?, updated_at = ?
		 WHERE id = ? AND status != 'need_manual'`,
		reason, formatTime(now), id)
	if err != nil {
		return fmt.Errorf("handoff: 标 need_manual: %w", err)
	}
	return nil
}

// checkOwnership 批量校验 credential_id 是否都归此乘客。
//
// 归属规则：
//   - owner_record_passenger_id = passengerID · owner_bus_id IS NULL（record group 未派号）
//   - 或号在乘客参与的车里（owner_bus_id 属车 · 该乘客是 bus_member 中的活跃成员）
//
// 只要有一个不满足，整批拒绝（ErrCredentialNotOwned）—— handoff 是不可逆动作，
// 宁可让客户端重试少一个号，也不能"部分放行"。
func (s *Store) checkOwnership(ctx context.Context, ids []string, passengerID string) error {
	if len(ids) == 0 {
		return ErrEmptyCredentials
	}
	placeholders := ""
	args := make([]any, 0, len(ids)+3)
	args = append(args, passengerID, passengerID, passengerID)
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	// 三条件 OR：
	// 1. 号在 record group 归该乘客（owner_record_passenger_id）
	// 2. 号在车里 · 该乘客是车主（bus.creator_passenger_id）
	// 3. 号在车里 · 该乘客是活跃成员（bus_member.left_at IS NULL）
	//
	// 阶段 1a 单人车里 (2) 恒等价 (3)；提前把两条都写清避免 1c 多人车时又忘补
	query := `
		SELECT COUNT(1) FROM credential_ledger c
		 WHERE c.status != 'handed_off'
		   AND c.id IN (` + placeholders + `)
		   AND (
		     c.owner_record_passenger_id = ?
		     OR c.owner_bus_id IN (SELECT id FROM bus WHERE creator_passenger_id = ?)
		     OR c.owner_bus_id IN (
		         SELECT bus_id FROM bus_member
		          WHERE passenger_id = ? AND left_at IS NULL
		     )
		   )`
	// 交换：先 placeholders 再三条件 - 上面已经组好 args 顺序（passenger×3, ids...）
	// 但 query 里的顺序应是 ids... 后 passenger×3。重新调整 args 顺序：
	sortedArgs := make([]any, 0, len(ids)+3)
	for _, id := range ids {
		sortedArgs = append(sortedArgs, id)
	}
	sortedArgs = append(sortedArgs, passengerID, passengerID, passengerID)

	var n int
	if err := s.db.QueryRowContext(ctx, query, sortedArgs...).Scan(&n); err != nil {
		return fmt.Errorf("handoff: 校验归属: %w", err)
	}
	if n != len(ids) {
		return ErrCredentialNotOwned
	}
	return nil
}

// ── SQL / 扫描 ──────────────────────────────────────

const selectPending = `SELECT
	id,
	passenger_id,
	download_token,
	credential_ids_json,
	status,
	fulfill_count,
	fulfilled_at,
	confirmed_at,
	completed_at,
	expires_at,
	COALESCE(error, ''),
	created_at,
	updated_at
FROM pending_handoff `

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(r rowScanner) (Pending, error) {
	var p Pending
	var status, credsJSON, expiresAt, createdAt, updatedAt string
	var fulfilledAt, confirmedAt, completedAt sql.NullString
	// download_token 列里存的是 hash · 扔进本地变量避免污染 Pending.DownloadToken
	// （明文只在 IssueToken 那一刻存在，此处永远不该"知道"明文）
	var storedTokenHash string
	if err := r.Scan(&p.ID, &p.PassengerID, &storedTokenHash, &credsJSON,
		&status, &p.FulfillCount, &fulfilledAt, &confirmedAt, &completedAt,
		&expiresAt, &p.Error, &createdAt, &updatedAt); err != nil {
		return p, err
	}
	_ = storedTokenHash // 只用来吸收扫描列，不外传
	p.Status = Status(status)
	if err := json.Unmarshal([]byte(credsJSON), &p.CredentialIDs); err != nil {
		return p, fmt.Errorf("handoff: 解 credential_ids: %w", err)
	}
	p.ExpiresAt = parseTime(expiresAt)
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	if fulfilledAt.Valid {
		t := parseTime(fulfilledAt.String)
		p.FulfilledAt = &t
	}
	if confirmedAt.Valid {
		t := parseTime(confirmedAt.String)
		p.ConfirmedAt = &t
	}
	if completedAt.Valid {
		t := parseTime(completedAt.String)
		p.CompletedAt = &t
	}
	return p, nil
}

// newHexToken 生成 32 位十六进制 token（16 字节随机 → hex）· 跟 X-Idempotency-Key 一致的格式
func newHexToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

const timeLayout = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) time.Time {
	for _, l := range []string{timeLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
