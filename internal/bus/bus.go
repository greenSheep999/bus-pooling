// Package bus 管拼车实体：建 bus、加/退成员、查号池、解散。
//
// 阶段 1a：single kind（1 人车，creator 就是唯一 owner）
// 阶段 1c：加 anon（系统撮合）+ team（邀请码组队）两条多人拼车路径
// 拉号 / 补车 / 集单是别的包的事 —— 这里只管 bus 元数据 + 成员关系。
package bus

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Kind string

const (
	KindSingle Kind = "single"
	KindAnon   Kind = "anon"
	KindTeam   Kind = "team"
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
	Strategy    Strategy
	// AnonZone · anon 撮合过滤：只有相同 zone 的 anon bus 才互相匹配（1c 新加）
	// 空 = 不限区（默认）· single / team 忽略
	AnonZone string
	// AnonMaxUnitPrice · anon 撮合价格上限（microunit）· 0 = 不限
	AnonMaxUnitPrice int64
}

// Strategy 每车一策略（decisions §8.6）· 落 bus 表同名列。
// 指针字段 nil = 不限 / 未设。
type Strategy struct {
	AutoRefillEnabled bool
	RefillWatermark   int
	RefillMinCount    *int
	PerRoundCount     *int
	MaxUnitPrice      *int64
	DailyRoundLimit   *int
	DailySpendLimit   *int64
	PreferredVendor   *string
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
	ErrNotFound         = errors.New("bus: 找不到这辆车")
	ErrNotMember        = errors.New("bus: 不是这辆车的成员")
	ErrDissolved        = errors.New("bus: 车已解散")
	ErrOwnerCantLeave   = errors.New("bus: owner 不能退出车，请解散")
	ErrBadKind          = errors.New("bus: 不支持的 kind")
	ErrAlreadyMember    = errors.New("bus: 已在车里")
	ErrBusFull          = errors.New("bus: 车已满")
	ErrInvalidInvite    = errors.New("bus: 邀请码无效")
	ErrNotOwner         = errors.New("bus: 只有 owner 能操作")
	ErrInviteExhausted  = errors.New("bus: 邀请码生成冲突过多·请重试")
)

type Store struct {
	db *sql.DB
	// maxMembers 从 config.Bus.MaxMembers 传入·建车时统一强制
	// 忽略前端 CreateInput.MaxMembers 入参·防止胡传 / 老客户端漏传
	// 0 = 装配层没传·systemMaxMembers() 兜到 defaultMaxMembers 让老 caller / 测试可跑
	maxMembers int
}

// defaultMaxMembers · Store 建时没显式设 maxMembers 的兜底
// 生产装配走 NewStoreWithConfig · 测试直接 NewStore 就用这个值
const defaultMaxMembers = 5

// NewStore · 保留旧构造 · 用 defaultMaxMembers 兜底
// 生产走 NewStoreWithConfig · 传 config.Bus.MaxMembers
func NewStore(db *sql.DB) *Store { return &Store{db: db, maxMembers: defaultMaxMembers} }

// NewStoreWithConfig · 装配层传 config.Bus.MaxMembers 进来
// max <= 0 兜到 defaultMaxMembers（Validate 里应该已经拦了·这里再一层防御）
func NewStoreWithConfig(db *sql.DB, maxMembers int) *Store {
	if maxMembers <= 0 {
		maxMembers = defaultMaxMembers
	}
	return &Store{db: db, maxMembers: maxMembers}
}

// systemMaxMembers · Store 用的 max_members 值 · 从 config 传进来
func (s *Store) systemMaxMembers() int {
	if s.maxMembers <= 0 {
		return defaultMaxMembers
	}
	return s.maxMembers
}

// CreateInput 建车入参。Strategy 为 nil = 用零值（不自动补车、上限全空）。
type CreateInput struct {
	Name      string
	Kind      Kind
	CreatorID string
	Strategy  *Strategy
	// MaxMembers · anon 车最多几人（0 = 不限）· single 忽略
	MaxMembers int
	// AnonZone · anon 撮合的 zone（同 zone 才匹配）· single 忽略
	AnonZone string
	// AnonMaxUnitPrice · anon 撮合的单价上限 microunit（0 = 不限）· single 忽略
	AnonMaxUnitPrice int64
}

// Create 建一辆车（1a: single · 1c: anon · 2a: team） + creator 作为 owner 成员，一个事务。
func (s *Store) Create(ctx context.Context, in CreateInput) (*Bus, error) {
	switch in.Kind {
	case KindSingle, KindAnon, KindTeam:
	default:
		return nil, fmt.Errorf("%w: %q", ErrBadKind, in.Kind)
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

	// 1c · max_members 系统统一 · 前端不传·后端按 config.bus.max_members 读
	// 忽略入参·防止前端胡传 or 老客户端漏传
	b := &Bus{
		ID:               uuid.NewString(),
		Name:             in.Name,
		Kind:             in.Kind,
		Status:           StatusActive,
		CreatorID:        in.CreatorID,
		MaxMembers:       s.systemMaxMembers(),
		AnonZone:         in.AnonZone,
		AnonMaxUnitPrice: in.AnonMaxUnitPrice,
		CreatedAt:        time.Now().UTC(),
	}
	if in.Strategy != nil {
		b.Strategy = *in.Strategy
	}

	// 用户建的车一律带邀请码 —— single / team 行为一致（CLAUDE.md §2 一辆车就是一辆车）：
	// 1 个人时是独享·车主随时把码给朋友就变多人拼车·不需要"换类型"。
	// 只有系统建的 anon 撮合池没码（谁进由系统撮合决定·不靠码）。
	if in.Kind != KindAnon {
		code, err := s.generateUniqueInviteCode(ctx)
		if err != nil {
			return nil, err
		}
		b.InviteCode = code
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO bus
		  (id, name, kind, creator_passenger_id, status, created_at,
		   max_members, anon_zone, anon_max_unit_price, invite_code,
		   auto_refill_enabled, refill_watermark, refill_min_count,
		   per_round_count, max_unit_price,
		   daily_round_limit, daily_spend_limit, preferred_vendor)
		VALUES (?, ?, ?, ?, ?, ?,
		        ?, ?, ?, ?,
		        ?, ?, ?,
		        ?, ?,
		        ?, ?, ?)`,
		b.ID, b.Name, string(b.Kind), b.CreatorID, string(b.Status),
		formatTime(b.CreatedAt),
		nullableIntZero(b.MaxMembers),
		nullableString(&b.AnonZone),
		nullableInt64Zero(b.AnonMaxUnitPrice),
		nullableString(&b.InviteCode),
		boolToInt(b.Strategy.AutoRefillEnabled), b.Strategy.RefillWatermark,
		nullableInt(b.Strategy.RefillMinCount),
		nullableInt(b.Strategy.PerRoundCount),
		nullableInt64(b.Strategy.MaxUnitPrice),
		nullableInt(b.Strategy.DailyRoundLimit),
		nullableInt64(b.Strategy.DailySpendLimit),
		nullableString(b.Strategy.PreferredVendor)); err != nil {
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

// inviteAlphabet · 邀请码字符集·剔除易混淆字符（0/O · 1/I/L · S/5）
// 长度 8 → 32^8 ≈ 10^12 唯一空间·冲突极低。
const inviteAlphabet = "ABCDEFGHJKMNPQRTUVWXYZ23467689"
const inviteCodeLen = 8

// generateInviteCode · crypto/rand 生成 8 位可读邀请码。
func generateInviteCode() (string, error) {
	buf := make([]byte, inviteCodeLen)
	alphabetLen := big.NewInt(int64(len(inviteAlphabet)))
	for i := range buf {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", fmt.Errorf("bus: 生成邀请码: %w", err)
		}
		buf[i] = inviteAlphabet[n.Int64()]
	}
	return string(buf), nil
}

// generateUniqueInviteCode · 生成 8 位邀请码·DB UNIQUE 冲突自动重试。
// 3 次都冲突返 ErrInviteExhausted（几乎不会发生·10^12 空间 · 冲突概率极低）。
func (s *Store) generateUniqueInviteCode(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		code, err := generateInviteCode()
		if err != nil {
			return "", err
		}
		var exists int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM bus WHERE invite_code = ? AND status = 'active'`, code).Scan(&exists); err != nil {
			return "", fmt.Errorf("bus: 查邀请码冲突: %w", err)
		}
		if exists == 0 {
			return code, nil
		}
	}
	return "", ErrInviteExhausted
}

// FindByInviteCode · 按邀请码找活跃车。
//
// **不筛 kind** —— 用户建的车（single / team）都有码·都能被加入。
// 1 人独享的车拿到码的人一进来就变多人拼车·这是正常路径不是异常。
// 邀请码大小写不敏感（存的是大写·查询前 upper）· 空返 ErrInvalidInvite。
func (s *Store) FindByInviteCode(ctx context.Context, code string) (*Bus, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, ErrInvalidInvite
	}
	b, err := s.scanBus(s.db.QueryRowContext(ctx,
		selectBus+` WHERE invite_code = ? AND status = 'active'`, code))
	if errors.Is(err, ErrNotFound) {
		return nil, ErrInvalidInvite
	}
	return b, err
}

// JoinByInviteCode · 用邀请码加入一辆车。
//
// 语义：
//   - 邀请码无效 / 车已解散 → ErrInvalidInvite（不区分·避免枚举攻击）
//   - 车已满 → ErrBusFull · 已成员 → ErrAlreadyMember（handler 幂等处理）
//   - 加入本身**不触发拉号**· 只写 bus_member
//   - SharePct 按成员均分（100 / N）· 1 人独享的车进来第 2 个人就变 50/50
//
// 返回 bus 本身·让 handler 可以直接返车详情。
func (s *Store) JoinByInviteCode(ctx context.Context, code, passengerID string) (*Bus, error) {
	b, err := s.FindByInviteCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if err := s.Join(ctx, b.ID, passengerID); err != nil {
		return nil, err
	}
	// join 后 members / share_pct 变了 · 重读一次
	return s.Get(ctx, b.ID)
}

// RemoveMember 车主移除一位成员 · 剩下的人 share_pct 重算（decisions §8.18 / §8.26）。
//
// **跟"挂起"的区别**（UI 上要说清·否则车主会误用）：
//   - 挂起：`share_pct` 保留 · 可逆 · 他充值就回来
//   - 移除：`share_pct` **要重算分给其他人** · 不可逆
//
// 移除**不退**他已经花的钱（用户明确：提前下车不退）。历史轮次的
// `participants_split_json` 不动 —— 那些号死了走质保退款还是退给他（§8.35 #19）。
//
// 权限：只 owner 能移除 · 不能移除自己（要退出走解散）。
//
// **车主有权直接移除**（§8.36 覆盖了 §8.18 原先的"全员确认"要求）：
// 车是车主建的、邀请码是车主发的·成员构成归他处置。全员确认在真实场景里会卡死 ——
// 移除的典型原因是"这人欠钱不还"，等他点同意等不到。
func (s *Store) RemoveMember(ctx context.Context, busID, callerID, targetID string) error {
	if targetID == callerID {
		return fmt.Errorf("%w: 不能移除自己（要退出请解散车）", ErrNotOwner)
	}
	b, err := s.Get(ctx, busID)
	if err != nil {
		return err
	}
	if b.CreatorID != callerID {
		return ErrNotOwner
	}
	if b.Status != StatusActive {
		return ErrDissolved
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 标记退出（不删行 —— 留历史·质保退款要按当初的 split 找到他）
	res, err := tx.ExecContext(ctx, `
		UPDATE bus_member SET left_at = ?
		 WHERE bus_id = ? AND passenger_id = ? AND left_at IS NULL`,
		formatTime(time.Now().UTC()), busID, targetID)
	if err != nil {
		return fmt.Errorf("bus: 移除成员: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotMember
	}

	// 剩下的人重算 share_pct（成员变化 → 重新计算分配）
	if err := recalcSharesTx(ctx, tx, busID); err != nil {
		return err
	}
	return tx.Commit()
}

// recalcSharesTx 把剩余活跃成员的 share_pct 重算成均分 · 余数给 owner。
//
// 为什么均分而不按原比例缩放：原比例是历史协商结果·少了一个人之后按什么比例
// 分没有客观答案。均分是最不需要解释的规则（谁也不多占）。车主想要别的比例
// 得走单独的"改分摊比例"流程 —— 那个**仍然该全员确认**（纯改钱的分配·跟移除不同）·
// 阶段 2+ 再说（§8.36）。
func recalcSharesTx(ctx context.Context, tx *sql.Tx, busID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT passenger_id, role FROM bus_member
		 WHERE bus_id = ? AND left_at IS NULL
		 ORDER BY joined_at`, busID)
	if err != nil {
		return fmt.Errorf("bus: 读剩余成员: %w", err)
	}
	type m struct{ id, role string }
	var members []m
	for rows.Next() {
		var x m
		if err := rows.Scan(&x.id, &x.role); err != nil {
			rows.Close()
			return err
		}
		members = append(members, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(members) == 0 {
		return nil // 空车（不该发生 · owner 走解散）
	}

	per := 100 / len(members)
	remainder := 100 - per*len(members)
	for _, x := range members {
		pct := per
		if x.role == "owner" {
			pct += remainder // 余数给 owner
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE bus_member SET share_pct = ?
			 WHERE bus_id = ? AND passenger_id = ? AND left_at IS NULL`,
			pct, busID, x.id); err != nil {
			return fmt.Errorf("bus: 重算 share_pct: %w", err)
		}
	}
	return nil
}

// RegenerateInviteCode · owner 主动换邀请码 · 旧码立即失效。
// 场景：邀请码泄漏 · 或想拒绝没入车的老邀请。
//
// 用户建的车（single / team）都能换 · 只有系统 anon 撮合池没码可换。
func (s *Store) RegenerateInviteCode(ctx context.Context, busID, callerID string) (string, error) {
	b, err := s.Get(ctx, busID)
	if err != nil {
		return "", err
	}
	if b.Kind == KindAnon {
		return "", fmt.Errorf("%w: 系统撮合池没有邀请码", ErrBadKind)
	}
	if b.CreatorID != callerID {
		return "", ErrNotOwner
	}
	if b.Status == StatusDissolved {
		return "", ErrDissolved
	}
	code, err := s.generateUniqueInviteCode(ctx)
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE bus SET invite_code = ? WHERE id = ?`, code, busID); err != nil {
		return "", fmt.Errorf("bus: 更新邀请码: %w", err)
	}
	return code, nil
}

// EnsureInviteCode · 读车时补一个邀请码（老数据自愈 · 不做 migration）。
//
// 1c 之前建的车（kind='single'）没生成过邀请码·但模型上它跟 team 一样能加人。
// 与其批量改历史数据·不如**读到没码的车时补一个**：幂等 · 只写一次 · 无副作用。
// anon 撮合池不补（它不靠码）· 已有码的不动。
func (s *Store) EnsureInviteCode(ctx context.Context, b *Bus) error {
	if b == nil || b.Kind == KindAnon || b.InviteCode != "" || b.Status != StatusActive {
		return nil
	}
	code, err := s.generateUniqueInviteCode(ctx)
	if err != nil {
		return err
	}
	// 条件 UPDATE：只在还是空的时候写·并发下不会互相覆盖
	res, err := s.db.ExecContext(ctx,
		`UPDATE bus SET invite_code = ?
		  WHERE id = ? AND (invite_code IS NULL OR invite_code = '')`, code, b.ID)
	if err != nil {
		return fmt.Errorf("bus: 补邀请码: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 别人抢先补了 · 读回来用它的
		fresh, gerr := s.Get(ctx, b.ID)
		if gerr != nil {
			return nil // 补不上不算错·下次读再试
		}
		b.InviteCode = fresh.InviteCode
		return nil
	}
	b.InviteCode = code
	return nil
}

// Get 按 id 读一辆车。**不校验乘客归属** —— 那是上层 handler 的事。
func (s *Store) Get(ctx context.Context, id string) (*Bus, error) {
	return s.scanBus(s.db.QueryRowContext(ctx, selectBus+` WHERE id = ?`, id))
}

// Join 一位乘客加入一辆车（1c）。
//
// **任何 kind 都能加人** —— 一辆车就是一辆车（CLAUDE.md §2）：
// 1 人时是独享·别人进来就变多人拼车·不存在"这车不能加人"这回事。
// single 是历史 kind 值·行为跟 team 完全一致·不做区分。
//
// 语义：
//   - 已在车里（active 成员）返 ErrAlreadyMember（handler 可幂等处理）
//   - 车已解散返 ErrDissolved · 车满返 ErrBusFull
//   - 加入本身**不触发拉号**（04-scenarios §A4）· 只写 bus_member
//   - SharePct 简化：所有成员均分 · 加入时批量重算（100 / N）
//     · 剩余精度用整数除法舍掉·A4 不精算·1c-2 加精细化摊分
func (s *Store) Join(ctx context.Context, busID, passengerID string) error {
	b, err := s.Get(ctx, busID)
	if err != nil {
		return err
	}
	if b.Status != StatusActive {
		return ErrDissolved
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 已是 active 成员？返 ErrAlreadyMember
	var existing int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(1) FROM bus_member
		 WHERE bus_id = ? AND passenger_id = ? AND left_at IS NULL`,
		busID, passengerID).Scan(&existing); err != nil {
		return fmt.Errorf("bus: 查成员: %w", err)
	}
	if existing > 0 {
		return ErrAlreadyMember
	}

	// 车满检查
	var active int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(1) FROM bus_member
		 WHERE bus_id = ? AND left_at IS NULL`, busID).Scan(&active); err != nil {
		return fmt.Errorf("bus: 查成员数: %w", err)
	}
	if b.MaxMembers > 0 && active >= b.MaxMembers {
		return ErrBusFull
	}

	// 均分 share_pct = 100 / (active+1) · 剩余给 owner（简化）
	newActive := active + 1
	newShare := 100 / newActive
	now := formatTime(time.Now().UTC())
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO bus_member (bus_id, passenger_id, role, joined_at, share_pct, status)
		VALUES (?, ?, 'member', ?, ?, 'active')`,
		busID, passengerID, now, newShare); err != nil {
		return fmt.Errorf("bus: 加成员: %w", err)
	}
	// 重算全体 share_pct（简单：均分 · 剩余给 owner）
	remainder := 100 - newShare*newActive
	if _, err := tx.ExecContext(ctx, `
		UPDATE bus_member SET share_pct = ?
		 WHERE bus_id = ? AND left_at IS NULL AND role != 'owner'`,
		newShare, busID); err != nil {
		return fmt.Errorf("bus: 重算成员 share: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE bus_member SET share_pct = ?
		 WHERE bus_id = ? AND left_at IS NULL AND role = 'owner'`,
		newShare+remainder, busID); err != nil {
		return fmt.Errorf("bus: 重算 owner share: %w", err)
	}
	return tx.Commit()
}

// MatchOptions · anon bus 撮合条件。
//
// 语义：找一辆**已存在的活跃 anon bus** 满足：
//   1. status = active
//   2. anon_zone 匹配（空 = 不限）
//   3. anon_max_unit_price >= 请求上限（保护成员利益 · 上限更低的车不匹配）
//   4. 未满（active 成员数 < max_members · max_members=0 视为不限）
//   5. 该乘客还没加入过（不重复算成员）
//
// 返回**最老**的一辆（先建先满 · 让新用户先填满旧车再开新车）· 找不到返 ErrNoMatch。
type MatchOptions struct {
	PassengerID  string
	Zone         string // 空 = 不限
	MaxUnitPrice int64  // 0 = 不限
}

var ErrNoMatch = errors.New("bus: 没有匹配的 anon 车")

func (s *Store) FindMatchable(ctx context.Context, opt MatchOptions) (*Bus, error) {
	// 一次 SQL 查完 · 用 sub-query 过滤 "该乘客未加入" 且 "未满"
	// **不能** 在外层 rows 迭代里跑 QueryRow —— SQLite 单 writer 场景会争锁·驱动池化连接
	// 时另一读连接被 tx 阻塞 → 死锁。合成一句 SQL 交给 SQLite 一起决议。
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.id, b.name, b.kind, b.status, b.creator_passenger_id,
		       COALESCE(b.invite_code, ''), COALESCE(b.max_members, 0),
		       b.created_at, b.dissolved_at,
		       COALESCE(b.anon_zone, ''), COALESCE(b.anon_max_unit_price, 0),
		       b.auto_refill_enabled, b.refill_watermark, b.refill_min_count,
		       b.per_round_count, b.max_unit_price,
		       b.daily_round_limit, b.daily_spend_limit, b.preferred_vendor
		  FROM bus b
		 WHERE b.kind = 'anon' AND b.status = 'active'
		   AND (? = '' OR b.anon_zone = ? OR b.anon_zone = '')
		   AND (? = 0 OR b.anon_max_unit_price >= ? OR b.anon_max_unit_price = 0)
		   AND NOT EXISTS (
		     SELECT 1 FROM bus_member m
		      WHERE m.bus_id = b.id AND m.passenger_id = ? AND m.left_at IS NULL
		   )
		   AND (
		     b.max_members = 0 OR b.max_members IS NULL OR
		     (SELECT count(1) FROM bus_member m2
		       WHERE m2.bus_id = b.id AND m2.left_at IS NULL) < b.max_members
		   )
		 ORDER BY b.created_at ASC
		 LIMIT 1`,
		opt.Zone, opt.Zone, opt.MaxUnitPrice, opt.MaxUnitPrice, opt.PassengerID)
	if err != nil {
		return nil, fmt.Errorf("bus: 撮合查询: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrNoMatch
	}
	b := &Bus{}
	if err := scanBusFields(rows, b); err != nil {
		return nil, err
	}
	return b, nil
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

// Rename 改车名 · 只有 creator 能改（1a 单人车 = 唯一成员）。
func (s *Store) Rename(ctx context.Context, busID, passengerID, newName string) error {
	if newName == "" {
		return fmt.Errorf("bus: 车名不能为空")
	}
	if n := len([]rune(newName)); n > 40 {
		return fmt.Errorf("bus: 车名不能超过 40 字")
	}
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
	res, err := s.db.ExecContext(ctx,
		`UPDATE bus SET name = ? WHERE id = ? AND status = 'active'`,
		newName, busID)
	if err != nil {
		return fmt.Errorf("bus: 改名: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDissolved
	}
	return nil
}

// UpdateStrategy 改车级策略。只有 creator 能改（1a 单人车规则）。
// 传入 Strategy 整体替换（前端每次 PUT 完整对象）· nil 字段视为清空。
func (s *Store) UpdateStrategy(ctx context.Context, busID, passengerID string, st Strategy) error {
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
	res, err := s.db.ExecContext(ctx, `
		UPDATE bus SET auto_refill_enabled = ?, refill_watermark = ?, refill_min_count = ?,
		               per_round_count = ?, max_unit_price = ?,
		               daily_round_limit = ?, daily_spend_limit = ?, preferred_vendor = ?
		 WHERE id = ? AND status = 'active'`,
		boolToInt(st.AutoRefillEnabled), st.RefillWatermark, nullableInt(st.RefillMinCount),
		nullableInt(st.PerRoundCount), nullableInt64(st.MaxUnitPrice),
		nullableInt(st.DailyRoundLimit), nullableInt64(st.DailySpendLimit),
		nullableString(st.PreferredVendor), busID)
	if err != nil {
		return fmt.Errorf("bus: 改策略: %w", err)
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
                          created_at, dissolved_at,
                          COALESCE(anon_zone, ''), COALESCE(anon_max_unit_price, 0),
                          auto_refill_enabled, refill_watermark, refill_min_count,
                          per_round_count, max_unit_price,
                          daily_round_limit, daily_spend_limit, preferred_vendor
                     FROM bus`

func (s *Store) scanBus(row *sql.Row) (*Bus, error) {
	b := &Bus{}
	if err := scanBusFields(row, b); err != nil {
		return nil, err
	}
	return b, nil
}

// scanner 让 Row 和 Rows 都能塞进 scanBusFields。
type scanner interface {
	Scan(dest ...any) error
}

func scanBusFields(sc scanner, b *Bus) error {
	var kind, status, createdAt string
	var dissolvedAt sql.NullString
	var autoRefill int
	var refillMinCount, perRoundCount, dailyRoundLimit sql.NullInt64
	var maxUnitPrice, dailySpendLimit sql.NullInt64
	var preferredVendor sql.NullString
	err := sc.Scan(&b.ID, &b.Name, &kind, &status, &b.CreatorID,
		&b.InviteCode, &b.MaxMembers, &createdAt, &dissolvedAt,
		&b.AnonZone, &b.AnonMaxUnitPrice,
		&autoRefill, &b.Strategy.RefillWatermark, &refillMinCount,
		&perRoundCount, &maxUnitPrice,
		&dailyRoundLimit, &dailySpendLimit, &preferredVendor)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("bus: 读车: %w", err)
	}
	b.Kind = Kind(kind)
	b.Status = Status(status)
	b.CreatedAt = parseTime(createdAt)
	if dissolvedAt.Valid {
		t := parseTime(dissolvedAt.String)
		b.DissolvedAt = &t
	}
	b.Strategy.AutoRefillEnabled = autoRefill != 0
	if refillMinCount.Valid {
		v := int(refillMinCount.Int64)
		b.Strategy.RefillMinCount = &v
	}
	if perRoundCount.Valid {
		v := int(perRoundCount.Int64)
		b.Strategy.PerRoundCount = &v
	}
	if maxUnitPrice.Valid {
		v := maxUnitPrice.Int64
		b.Strategy.MaxUnitPrice = &v
	}
	if dailyRoundLimit.Valid {
		v := int(dailyRoundLimit.Int64)
		b.Strategy.DailyRoundLimit = &v
	}
	if dailySpendLimit.Valid {
		v := dailySpendLimit.Int64
		b.Strategy.DailySpendLimit = &v
	}
	if preferredVendor.Valid {
		s := preferredVendor.String
		b.Strategy.PreferredVendor = &s
	}
	return nil
}

func scanBusRow(rows *sql.Rows) (*Bus, error) {
	b := &Bus{}
	if err := scanBusFields(rows, b); err != nil {
		return nil, err
	}
	return b, nil
}

// ── null helpers ────────────────────────────────────

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
func nullableString(p *string) any {
	if p == nil {
		return nil
	}
	if *p == "" {
		return nil
	}
	return *p
}

// nullableIntZero · 0 存 NULL（跟 nullableInt 不同：不用指针·适合 CreateInput 的值类型字段）
func nullableIntZero(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

// nullableInt64Zero · 0 存 NULL
func nullableInt64Zero(v int64) any {
	if v == 0 {
		return nil
	}
	return v
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
