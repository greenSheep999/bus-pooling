// Package bus 管拼车实体：建 bus、加/退成员、查号池、解散。
//
// 阶段 1a 只做 single kind（1 人车，creator 就是唯一 owner）。anon / team 在 1c / 2a。
// 拉号 / 补车 / 集单是别的包的事 —— 这里只管 bus 元数据 + 成员关系。
package bus

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Kind string

const (
	KindSingle Kind = "single"
	KindAnon   Kind = "anon" // 1c 才做
	KindTeam   Kind = "team" // 2a 才做
)

type Status string

const (
	StatusActive    Status = "active"
	StatusDissolved Status = "dissolved"
)

// Bus 是一辆车的元数据（对内视角，跟 web/src/types/index.ts 的 Bus 有映射）。
type Bus struct {
	ID          string
	Name        string
	Kind        Kind
	Status      Status
	CreatorID   string
	InviteCode  string // team 才有 · single 恒空
	MaxMembers  int    // 0 = 不限
	CreatedAt   time.Time
	DissolvedAt *time.Time
}

// Member 是车里一个成员的行。
type Member struct {
	BusID       string
	PassengerID string
	Role        string // owner | member
	JoinedAt    time.Time
	LeftAt      *time.Time
	SharePct    int    // 单人车恒 100
	Status      string // active | suspended
}

var (
	ErrNotFound       = errors.New("bus: 找不到这辆车")
	ErrNotMember      = errors.New("bus: 不是这辆车的成员")
	ErrDissolved      = errors.New("bus: 车已解散")
	ErrOwnerCantLeave = errors.New("bus: owner 不能退出车，请解散")
	ErrBadKind        = errors.New("bus: 不支持的 kind")
	ErrAlreadyMember  = errors.New("bus: 已在车里")
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// CreateInput 建车入参。
type CreateInput struct {
	Name      string
	Kind      Kind
	CreatorID string
}

// Create 建一辆 single 车 + creator 作为 owner 成员，一个事务。
func (s *Store) Create(ctx context.Context, in CreateInput) (*Bus, error) {
	if in.Kind != KindSingle {
		return nil, fmt.Errorf("%w: %q（阶段 1a 只支持 single）", ErrBadKind, in.Kind)
	}
	if in.Name == "" {
		return nil, fmt.Errorf("bus: 车名不能为空")
	}
	if n := len([]rune(in.Name)); n > 40 {
		return nil, fmt.Errorf("bus: 车名不能超过 40 字")
	}
	if in.CreatorID == "" {
		return nil, fmt.Errorf("bus: 缺 creator")
	}

	b := &Bus{
		ID:        uuid.NewString(),
		Name:      in.Name,
		Kind:      in.Kind,
		Status:    StatusActive,
		CreatorID: in.CreatorID,
		CreatedAt: time.Now().UTC(),
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO bus
		  (id, name, kind, creator_passenger_id, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		b.ID, b.Name, string(b.Kind), b.CreatorID, string(b.Status),
		formatTime(b.CreatedAt)); err != nil {
		return nil, fmt.Errorf("bus: 建车: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO bus_member
		  (bus_id, passenger_id, role, joined_at, share_pct, status)
		VALUES (?, ?, 'owner', ?, 100, 'active')`,
		b.ID, b.CreatorID, formatTime(b.CreatedAt)); err != nil {
		return nil, fmt.Errorf("bus: 加 owner: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return b, nil
}

// Get 按 id 读一辆车。**不校验乘客归属** —— 那是上层 handler 的事。
func (s *Store) Get(ctx context.Context, id string) (*Bus, error) {
	return s.scanBus(s.db.QueryRowContext(ctx, selectBus+` WHERE id = ?`, id))
}

// GetForPassenger 读乘客参与的一辆车。**不是成员就返回 ErrNotMember** ——
// 别用 ErrNotFound，那会泄漏"这辆车存在但你不是成员"的信息。
func (s *Store) GetForPassenger(ctx context.Context, id, passengerID string) (*Bus, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	member, err := s.isActiveMember(ctx, id, passengerID)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrNotMember
	}
	return b, nil
}

// ListForPassenger 列出乘客参与的所有活跃 bus，按创建时间倒序。
// 解散的车**不返回**（前端要看历史另开端点）。
func (s *Store) ListForPassenger(ctx context.Context, passengerID string) ([]Bus, error) {
	rows, err := s.db.QueryContext(ctx, selectBus+`
		WHERE id IN (
			SELECT bus_id FROM bus_member
			 WHERE passenger_id = ? AND left_at IS NULL
		) AND status = 'active'
		ORDER BY created_at DESC`, passengerID)
	if err != nil {
		return nil, fmt.Errorf("bus: 列车: %w", err)
	}
	defer rows.Close()
	var out []Bus
	for rows.Next() {
		b, err := scanBusRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// Leave 成员退出。**owner 不能退**（只能解散车 · 单人车 owner = 唯一成员）。
func (s *Store) Leave(ctx context.Context, busID, passengerID string) error {
	b, err := s.Get(ctx, busID)
	if err != nil {
		return err
	}
	if b.Status == StatusDissolved {
		return ErrDissolved
	}
	if b.CreatorID == passengerID {
		return ErrOwnerCantLeave
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE bus_member SET left_at = ?
		 WHERE bus_id = ? AND passenger_id = ? AND left_at IS NULL`,
		formatTime(time.Now().UTC()), busID, passengerID)
	if err != nil {
		return fmt.Errorf("bus: 退车: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotMember
	}
	return nil
}

// Dissolve 解散车。只有 creator（1a 单人车里 = 唯一成员）能解散。
//
// **不删号池里的号** —— 那走 pending_dissolution 状态机（09-transactions §5.5），
// 阶段 1a 简化：解散只标状态，号留在原 group（后续 1c 完善迁移逻辑）。
func (s *Store) Dissolve(ctx context.Context, busID, passengerID string) error {
	b, err := s.Get(ctx, busID)
	if err != nil {
		return err
	}
	if b.Status == StatusDissolved {
		return ErrDissolved
	}
	if b.CreatorID != passengerID {
		return ErrNotMember
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE bus SET status = 'dissolved', dissolved_at = ?
		 WHERE id = ? AND status = 'active'`,
		formatTime(now), busID)
	if err != nil {
		return fmt.Errorf("bus: 解散: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDissolved
	}
	return nil
}

// Members 列车里所有活跃成员（按加入时间正序）。
func (s *Store) Members(ctx context.Context, busID string) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT bus_id, passenger_id, role, joined_at, left_at, share_pct, status
		  FROM bus_member
		 WHERE bus_id = ? AND left_at IS NULL
		 ORDER BY joined_at`, busID)
	if err != nil {
		return nil, fmt.Errorf("bus: 列成员: %w", err)
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		var joinedAt string
		var leftAt sql.NullString
		if err := rows.Scan(&m.BusID, &m.PassengerID, &m.Role, &joinedAt, &leftAt,
			&m.SharePct, &m.Status); err != nil {
			return nil, err
		}
		m.JoinedAt = parseTime(joinedAt)
		if leftAt.Valid {
			t := parseTime(leftAt.String)
			m.LeftAt = &t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) isActiveMember(ctx context.Context, busID, passengerID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(1) FROM bus_member
		 WHERE bus_id = ? AND passenger_id = ? AND left_at IS NULL`,
		busID, passengerID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("bus: 查成员: %w", err)
	}
	return n > 0, nil
}

// ── SQL / 扫描 ──────────────────────────────────────

const selectBus = `SELECT id, name, kind, status, creator_passenger_id,
                          COALESCE(invite_code, ''), COALESCE(max_members, 0),
                          created_at, dissolved_at
                     FROM bus`

func (s *Store) scanBus(row *sql.Row) (*Bus, error) {
	b := &Bus{}
	var kind, status, createdAt string
	var dissolvedAt sql.NullString
	err := row.Scan(&b.ID, &b.Name, &kind, &status, &b.CreatorID,
		&b.InviteCode, &b.MaxMembers, &createdAt, &dissolvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bus: 读车: %w", err)
	}
	b.Kind = Kind(kind)
	b.Status = Status(status)
	b.CreatedAt = parseTime(createdAt)
	if dissolvedAt.Valid {
		t := parseTime(dissolvedAt.String)
		b.DissolvedAt = &t
	}
	return b, nil
}

func scanBusRow(rows *sql.Rows) (*Bus, error) {
	b := &Bus{}
	var kind, status, createdAt string
	var dissolvedAt sql.NullString
	if err := rows.Scan(&b.ID, &b.Name, &kind, &status, &b.CreatorID,
		&b.InviteCode, &b.MaxMembers, &createdAt, &dissolvedAt); err != nil {
		return nil, err
	}
	b.Kind = Kind(kind)
	b.Status = Status(status)
	b.CreatedAt = parseTime(createdAt)
	if dissolvedAt.Valid {
		t := parseTime(dissolvedAt.String)
		b.DissolvedAt = &t
	}
	return b, nil
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
