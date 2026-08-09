// Package pullrecord 读一名乘客的「待派拉号记录」。
//
// 数据来源：`credential_ledger` 里 `owner_record_passenger_id = <pid>`
// 且 `status = 'alive'` 的号（也就是号池里 record group 中还没派去向的号）。
// **本包不建表** —— 拉号记录是 housepool 里的 group + credential_ledger 台账，
// 不是独立业务表。
//
// 对外形状（`web/src/types/index.ts Credential`）：
//   - 隐去 housepool 内部字段（`kiro_rs_credential_id` / `current_group` /
//     `death_source` / vendor_order_id 等 — CLAUDE.md §0.1）
//   - 只暴露乘客做决策要看的字段（活死状态 / 打码 key / 已耗额度 / 质保截止 / 推送情况）
package pullrecord

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound 找不到这条拉号记录（或不归此乘客）
var ErrNotFound = errors.New("pullrecord: 找不到这条拉号记录")

// Status 是对外简化后的号状态（CLAUDE.md §12.5 收敛）· 只有 alive / dead 两态。
// handoff 后号不在这个视图里出现（`credential_ledger.status='handed_off'` 被 SQL 过滤掉）
type Status string

const (
	StatusAlive Status = "alive"
	StatusDead  Status = "dead"
)

// Record 是一条对外的拉号记录（一个号一条 · 跟前端 Credential 类型对齐）。
//
// 对外不暴露 kiro_rs_credential_id / current_group / death_source
// / owner_record_passenger_id / vendor_order_id （CLAUDE.md §0.1 §12.6）。
type Record struct {
	ID          string     // credential_ledger.id（我方 UUID · 对外派发用这个）
	VendorID    string     // vendor 内部 id · 对外展示走 vendorLabel 映射
	Status      Status     // alive | dead
	KeyMasked   string     // ksk_live_xxxx…xxx · 只前后 + 中间省略号，永远不落明文
	Region      string     // us-east-1 / eu-central-1
	CreditsUsed int64      // microunit · 已消耗额度快照
	PulledAt    time.Time  // 号入池时间
	WarrantyUnt *time.Time // 质保截止 · null = 无质保 vendor
	DeadAt      *time.Time // 存活时为空；死了带值
	PushedAt    *time.Time // 推 passengerpool 时间 · null = 未推
	PushFailed  bool       // 推送失败过（push_error_code 非空且 attempts 已到上限）
	PushError   *PushError // 结构化推送失败详情 · nil = 从未失败或成功
	SourceRound string     // pull_round_id（用于关联到一次拉号动作 · 前端可点击回溯）
}

// PushError 是推 passengerpool 失败详情的对外形状（对齐 web `PushError` 类型）。
// 只承载**乘客视角**能看懂的字段，不含内部 push 攻击面。
type PushError struct {
	Code          string // unauthorized / not_found / conflict / timeout / …
	Status        *int   // HTTP 状态码 · nil = 没连上（超时 / DNS）
	Message       string // 给用户看的人话
	Retriable     bool
	Attempts      int
	LastAttemptAt time.Time
}

// ListOptions 列表查询选项。
type ListOptions struct {
	// IncludeHistory true = 也返回 dead 号；false = 只返 alive
	IncludeHistory bool
	Limit          int // 0 = 默认 50
	Offset         int
}

// Store 从 credential_ledger 读拉号记录。**只读** —— 派去向、handoff 等状态推进在
// 别的包（delivery/handoff、api/pullrecord.go handler）里走独立事务。
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// List 分页列一名乘客名下所有待派号。
//
// 返回 items + total（用于前端分页展示）· 按 pulled_at DESC 排序（新号在前）。
func (s *Store) List(ctx context.Context, passengerID string, opt ListOptions) ([]Record, int, error) {
	if opt.Limit <= 0 {
		opt.Limit = 50
	}
	if opt.Limit > 500 {
		opt.Limit = 500
	}

	// 未 handoff 且属于该乘客的 record group 号（owner_record_passenger_id 非 null）
	// **不包含** owner_bus_id 非 null 的号（那些属于车，不叫拉号记录）
	// **不包含** status='handed_off' 的号（handoff 后不再列出 · 台账行仍在库供追溯）
	where := `WHERE owner_record_passenger_id = ? AND owner_bus_id IS NULL AND status != 'handed_off'`
	args := []any{passengerID}
	if !opt.IncludeHistory {
		where += ` AND status = 'alive'`
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM credential_ledger `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("pullrecord: 计数: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, selectRecord+where+
		` ORDER BY pulled_at DESC LIMIT ? OFFSET ?`,
		append(args, opt.Limit, opt.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("pullrecord: 查询: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// Get 读一条拉号记录 · 校验归属（不属于该乘客 → ErrNotFound）。
//
// **不用 ErrForbidden** —— 那会泄漏"记录存在但你不是主人"的信息。
func (s *Store) Get(ctx context.Context, recordID, passengerID string) (*Record, error) {
	row := s.db.QueryRowContext(ctx, selectRecord+
		` WHERE id = ? AND owner_record_passenger_id = ? AND owner_bus_id IS NULL AND status != 'handed_off'`,
		recordID, passengerID)
	r, err := scanSingle(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// querier 兼容 *sql.DB / *sql.Tx —— GetOwnerships 在 tx1 内被调用时必须走同一 tx ·
// 避免 SQLite IMMEDIATE 事务持有 writer 时另一路读走另一连接卡死（driver 池化连接·
// 写锁未释放前另一连接会等 busy_timeout）。
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// GetOwnerships 批量校验一批 credential_id 是否都归此乘客（派去向 / handoff 前做归属校验）。
//
// 返回 map[credential_id]bool · true = 属于该乘客的待派号 · false = 不属于或已 handoff
// 未在结果里出现的 id 视为"不存在"（等价 false）。
func (s *Store) GetOwnerships(ctx context.Context, credentialIDs []string, passengerID string) (map[string]bool, error) {
	return getOwnerships(ctx, s.db, credentialIDs, passengerID)
}

// GetOwnershipsTx 事务版·跟 GetOwnerships 语义一致·让 tx1 里做归属校验不死锁。
func GetOwnershipsTx(ctx context.Context, tx *sql.Tx, credentialIDs []string, passengerID string) (map[string]bool, error) {
	return getOwnerships(ctx, tx, credentialIDs, passengerID)
}

func getOwnerships(ctx context.Context, q querier, credentialIDs []string, passengerID string) (map[string]bool, error) {
	out := make(map[string]bool, len(credentialIDs))
	if len(credentialIDs) == 0 {
		return out, nil
	}
	// 构建 IN 占位符
	placeholders := ""
	args := make([]any, 0, len(credentialIDs)+1)
	args = append(args, passengerID)
	for i, id := range credentialIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	// 两种归属都算 own：
	// 1. record group 里未派的号（owner_record_passenger_id）
	// 2. 已进车的号（owner_bus_id → bus → creator 或 bus_member）· 阶段 1a 单人车
	//    简化：只放行 record group 归属；进车号的 handoff 走 bus 权限，这里不管
	// 本方法专给"派去向" / "handoff" 用：目标是 record group 里的未派号
	rows, err := q.QueryContext(ctx, `
		SELECT id FROM credential_ledger
		 WHERE owner_record_passenger_id = ?
		   AND owner_bus_id IS NULL
		   AND status != 'handed_off'
		   AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("pullrecord: 校验归属: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// AssignToBus 把一批号从 record group 迁到 bus group（派去向 · into_bus）。
//
// 只改 `owner_bus_id / owner_record_passenger_id / current_group` 三个字段；
// **不改 housepool 侧的 group** —— 那是 handler 或调用方的责任（先做外部动作再改本地状态 ·
// CLAUDE.md §7.1）。这里只是台账落地。
//
// 单事务保证多号一起迁 · 若某号不归此乘客则整批回滚 → ErrNotFound。
func (s *Store) AssignToBus(ctx context.Context, credentialIDs []string, passengerID, busID string) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		return AssignToBusTx(ctx, tx, credentialIDs, passengerID, busID)
	})
}

// AssignToBusTx 是外部事务的版本 · 让 handler 把 assign + pending_assignment + 幂等响应
// 合成一个事务·任何一步失败整批回滚（跟文档 09-transactions §5 定义的 assign 状态机一致）。
func AssignToBusTx(ctx context.Context, tx *sql.Tx, credentialIDs []string, passengerID, busID string) error {
	if len(credentialIDs) == 0 {
		return nil
	}
	for _, cid := range credentialIDs {
		res, err := tx.ExecContext(ctx, `
			UPDATE credential_ledger
			   SET owner_bus_id = ?,
			       owner_record_passenger_id = NULL,
			       current_group = ?
			 WHERE id = ?
			   AND owner_record_passenger_id = ?
			   AND owner_bus_id IS NULL
			   AND status != 'handed_off'`,
			busID, "bus-"+busID, cid, passengerID)
		if err != nil {
			return fmt.Errorf("pullrecord: 派进车 %s: %w", cid, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// LookupKiroRSCredentialIDs 拿一批本地 credential.id 对应的 housepool 侧 CredentialID。
//
// 用来在 assign into_bus 时先调 housepool.UpdateCredential 把 group 迁到 bus-{id} ·
// **先外后内**（CLAUDE.md §7.1）· 外部动作成功后台账才更新 owner_bus_id。
//
// 返回 map 只含**有 kiro_rs_credential_id 的行**（DryRun 拉的号 kiro_rs_id 可能为空）·
// 调用方自己决定"外部动作不做也接受"还是"要求全部有 id 才继续"。
func (s *Store) LookupKiroRSCredentialIDs(ctx context.Context, credentialIDs []string, passengerID string) (map[string]uint64, error) {
	out := make(map[string]uint64, len(credentialIDs))
	if len(credentialIDs) == 0 {
		return out, nil
	}
	placeholders := ""
	args := make([]any, 0, len(credentialIDs)+1)
	args = append(args, passengerID)
	for i, id := range credentialIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kiro_rs_credential_id
		  FROM credential_ledger
		 WHERE owner_record_passenger_id = ?
		   AND owner_bus_id IS NULL
		   AND status != 'handed_off'
		   AND kiro_rs_credential_id IS NOT NULL
		   AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("pullrecord: 查 kiro_rs id: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   string
			krID uint64
		)
		if err := rows.Scan(&id, &krID); err != nil {
			return nil, err
		}
		out[id] = krID
	}
	return out, rows.Err()
}

// LookupKiroRSByID **不校验归属** · 只按 credential.id 查 kiro_rs_credential_id。
//
// 用途：janitor 的 reconcile 场景 —— credential 归属可能已变（owner_record_passenger_id=NULL
// 或已迁到别的 bus）·主 lookup 会漏。janitor 只关心"这号在池里的 kr_id 是啥"·
// 归属校验交给 janitor 自己按 pending_assignment 里的 (passenger_id, target_bus_id) 做。
func (s *Store) LookupKiroRSByID(ctx context.Context, credentialIDs []string) (map[string]uint64, error) {
	out := make(map[string]uint64, len(credentialIDs))
	if len(credentialIDs) == 0 {
		return out, nil
	}
	placeholders := ""
	args := make([]any, 0, len(credentialIDs))
	for i, id := range credentialIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kiro_rs_credential_id
		  FROM credential_ledger
		 WHERE kiro_rs_credential_id IS NOT NULL
		   AND id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("pullrecord: 查 kiro_rs id（按 id）: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   string
			krID uint64
		)
		if err := rows.Scan(&id, &krID); err != nil {
			return nil, err
		}
		out[id] = krID
	}
	return out, rows.Err()
}

// MarkPushed 标一批号"已推 passengerpool"（派去向 · push_pool）。
//
// **阶段 1a 只做本地标记**（真的推送在 1c）· 号仍留在 record group · 不改归属。
// 只写 `pushed_to_passengerpool_at`（decisions §8.24 双写台账）。
func (s *Store) MarkPushed(ctx context.Context, credentialIDs []string, passengerID string) error {
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		return MarkPushedTx(ctx, tx, credentialIDs, passengerID)
	})
}

// MarkPushedTx 外部事务版·跟 AssignToBusTx 一样让 handler 合并事务。
func MarkPushedTx(ctx context.Context, tx *sql.Tx, credentialIDs []string, passengerID string) error {
	if len(credentialIDs) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(timeLayout)
	for _, cid := range credentialIDs {
		res, err := tx.ExecContext(ctx, `
			UPDATE credential_ledger
			   SET pushed_to_passengerpool_at = ?
			 WHERE id = ?
			   AND owner_record_passenger_id = ?
			   AND owner_bus_id IS NULL
			   AND status != 'handed_off'`,
			now, cid, passengerID)
		if err != nil {
			return fmt.Errorf("pullrecord: 标推送 %s: %w", cid, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// withTx 事务小工具·省得每个 method 都写 begin/rollback/commit。
func withTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// ── SQL / 扫描 ──────────────────────────────────────

// 选择列表：只选**对外**要的字段，vendor_order_id / kiro_rs_credential_id /
// current_group / death_source **绝不出现在扫描列表里**（防止一时手滑把它们暴露）。
const selectRecord = `SELECT
	id,
	COALESCE(vendor_id, ''),
	status,
	COALESCE(key_masked, ''),
	COALESCE(region, ''),
	COALESCE(credits_used, 0),
	pulled_at,
	warranty_until,
	dead_at,
	pushed_to_passengerpool_at,
	push_attempts,
	COALESCE(push_error_code, ''),
	push_error_status,
	COALESCE(push_error_message, ''),
	COALESCE(push_error_retriable, 0),
	push_last_attempt_at,
	source_pull_round_id
FROM credential_ledger `

// 通用扫描
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(r rowScanner) (Record, error) {
	var rec Record
	var status, pulledAt, sourceRound string
	var warranty, deadAt, pushedAt, pushLast sql.NullString
	var pushCode, pushMsg string
	var pushStatus sql.NullInt64
	var pushAttempts int
	var pushRetriable int
	err := r.Scan(&rec.ID, &rec.VendorID, &status,
		&rec.KeyMasked, &rec.Region, &rec.CreditsUsed,
		&pulledAt, &warranty, &deadAt, &pushedAt,
		&pushAttempts, &pushCode, &pushStatus, &pushMsg, &pushRetriable, &pushLast,
		&sourceRound)
	if err != nil {
		return rec, err
	}
	rec.Status = Status(status)
	rec.PulledAt = parseTime(pulledAt)
	rec.SourceRound = sourceRound
	if warranty.Valid {
		t := parseTime(warranty.String)
		rec.WarrantyUnt = &t
	}
	if deadAt.Valid {
		t := parseTime(deadAt.String)
		rec.DeadAt = &t
	}
	if pushedAt.Valid {
		t := parseTime(pushedAt.String)
		rec.PushedAt = &t
	}
	// 判"推送失败" · 有 code 就是失败过（不看 attempts —— 一次也是失败）
	if pushCode != "" {
		rec.PushFailed = true
		pe := &PushError{
			Code:      pushCode,
			Message:   pushMsg,
			Retriable: pushRetriable != 0,
			Attempts:  pushAttempts,
		}
		if pushStatus.Valid {
			st := int(pushStatus.Int64)
			pe.Status = &st
		}
		if pushLast.Valid {
			pe.LastAttemptAt = parseTime(pushLast.String)
		}
		rec.PushError = pe
	}
	return rec, nil
}

func scanSingle(row *sql.Row) (Record, error) {
	return scanRow(row)
}

// timeLayout 跟其它包（wallet / bus / decider）一致，避免解析层不齐
const timeLayout = "2006-01-02T15:04:05.000Z"

func parseTime(s string) time.Time {
	for _, l := range []string{timeLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
