package passenger

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// 邀请码（decisions §8.29 / §8.32）。
//
// **两种码职责完全不同 · 绝不能混**：
//
//	           系统邀请码                      个人邀请码
//	谁发       我方 → 社群                     每个乘客自动有
//	作用       划社群身份（解锁 vendor 真名     只给**手续费减免额度**
//	           + 免区域分项）                   （限次数）
//	invited    置 true                          **不改**（被邀请人仍是零售）
//
// **为什么个人码绝不能改 invited**（§8.29 明文）：如果个人码也解锁社群身份，
// 那任何人都能生成码让别人免区域分项 → §8.20 的整个定价分层崩掉。
//
// 修的老漏洞：register 里原来是 `invited := in.InviteCode != ""` ——
// **任何**非空码都置 invited=1。现在查 system_invite_code 白名单。

// inviteAlphabet 跟 bus 邀请码同字符集（剔除易混淆的 0/O · 1/I/L）
const inviteAlphabet = "ABCDEFGHJKMNPQRTUVWXYZ23467689"
const inviteCodeLen = 8

// feeWaiverPerInvite 每成功邀请 1 人 · 邀请人获得几次手续费减免。
// 额度上限是硬要求（§8.32）· 这个值该进配置 · TODO 挪到 config.invite。
const feeWaiverPerInvite = 3

var (
	// ErrInviteCodeInvalid 码不存在 / 已停用 / 已过期 / 用完了
	ErrInviteCodeInvalid = errors.New("passenger: 邀请码无效")
	// ErrSelfInvite 自己的码不能自己用
	ErrSelfInvite = errors.New("passenger: 不能用自己的邀请码")
	// ErrAlreadyReferred 这个账号已经被别人邀请过了（一人只能被邀一次）
	ErrAlreadyReferred = errors.New("passenger: 这个账号已经用过邀请码了")
)

// PersonalInvite 个人邀请码 + 成绩（对外展示用）。
type PersonalInvite struct {
	Code         string
	InvitedCount int
	// FeeWaiverTotal / Used 手续费减免额度（次）
	FeeWaiverTotal int
	FeeWaiverUsed  int
}

// Remaining 还剩几次减免额度。
func (p PersonalInvite) Remaining() int {
	n := p.FeeWaiverTotal - p.FeeWaiverUsed
	if n < 0 {
		return 0
	}
	return n
}

func generateInviteCode() (string, error) {
	buf := make([]byte, inviteCodeLen)
	l := big.NewInt(int64(len(inviteAlphabet)))
	for i := range buf {
		n, err := rand.Int(rand.Reader, l)
		if err != nil {
			return "", fmt.Errorf("passenger: 生成邀请码: %w", err)
		}
		buf[i] = inviteAlphabet[n.Int64()]
	}
	return string(buf), nil
}

// ErrAlreadyMember 已经是社群成员了（不能重复绑）
var ErrAlreadyMember = errors.New("passenger: 已经是社群成员")

// Referral 一条邀请记录（我邀请了谁）。
type Referral struct {
	// InviteeMasked 被邀请人的**脱敏**标识（如 `zha***@gmail.com`）
	//
	// **为什么脱敏**：邀请人不该看到别人的完整邮箱 —— 那是第三方的 PII。
	// 但他得能认出这是谁（毕竟是他邀请的），所以留前 3 位 + 域名。
	// 既能辨认又不能被拿去撞库 / 群发。
	InviteeMasked string
	// WaiverGranted 这一条给邀请人带来几次减免额度
	WaiverGranted int
	CreatedAt     time.Time
}

// maskEmail 邮箱脱敏 · `zhangsan@gmail.com` → `zha***@gmail.com`。
//
// 本地部分 ≤3 字符时只留首字符（`ab@x.com` → `a***@x.com`）·
// 否则前 3 位后面一律 `***`（不暴露原始长度 —— 长度也是信息）。
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***" // 不像邮箱 · 整个遮掉
	}
	local, domain := email[:at], email[at:]
	keep := 3
	if len(local) < keep {
		keep = 1
	}
	return local[:keep] + "***" + domain
}

// ListReferrals 我邀请过的人（倒序 · 最新在前）。
//
// 只返脱敏标识 —— 不出被邀请人的 id / 完整邮箱 / 用户名（第三方 PII）。
func (s *Store) ListReferrals(
	ctx context.Context, inviterID string, limit int,
) ([]Referral, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.email, r.created_at
		  FROM invite_referral r
		  JOIN passenger p ON p.id = r.invitee_passenger_id
		 WHERE r.inviter_passenger_id = ?
		 ORDER BY r.created_at DESC
		 LIMIT ?`, inviterID, limit)
	if err != nil {
		return nil, fmt.Errorf("passenger: 查邀请记录: %w", err)
	}
	defer rows.Close()

	var out []Referral
	for rows.Next() {
		var email, createdAt string
		if err := rows.Scan(&email, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, Referral{
			InviteeMasked: maskEmail(email),
			WaiverGranted: feeWaiverPerInvite,
			CreatedAt:     parseTime(createdAt),
		})
	}
	return out, rows.Err()
}

// BindSystemCode 已注册用户补绑社群码 · 拿社群身份。
//
// **为什么需要这个**：社群码原来只能注册时填。但用户往往先注册、后来才进社群拿到码 ——
// 没有补绑入口他只能注销重注册。
//
// **安全要点**（这个操作给的是定价特权 · §8.20）：
//   - 只认 `system_invite_code` 白名单里的码（跟注册走同一个守门人）
//   - **一个账号只能绑一次** —— 已经 invited=1 的直接拒（防止刷计数）
//   - 条件 UPDATE（`WHERE invited = 0`）· 并发下只有一个能成
//   - 码的 used_count 跟身份变更**同事务** —— 不然会出现"码计数了但身份没给"
func (s *Store) BindSystemCode(ctx context.Context, passengerID, code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return ErrInviteCodeInvalid
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("passenger: 开事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ok, err := isSystemInviteCode(ctx, tx, code)
	if err != nil {
		return err
	}
	if !ok {
		// 无效 / 停用 / 过期 / 用满 —— 都归一个错（不告诉用户具体哪种·防枚举）
		return ErrInviteCodeInvalid
	}

	// 条件 UPDATE：只有还没绑的才能绑（并发安全 + 防重复刷计数）
	res, err := tx.ExecContext(ctx, `
		UPDATE passenger
		   SET invited = 1, invite_code_used = ?, updated_at = ?
		 WHERE id = ? AND invited = 0`,
		code, formatTime(time.Now().UTC()), passengerID)
	if err != nil {
		return fmt.Errorf("passenger: 绑社群码: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAlreadyMember
	}

	if err := bumpSystemCodeUseTx(ctx, tx, code); err != nil {
		return err
	}
	return tx.Commit()
}

// EnsurePersonalCode 拿这个乘客的个人邀请码 · 没有就生成（幂等）。
//
// 老账号（1c 之前注册的）读到就补 —— 不做批量 migration。
func (s *Store) EnsurePersonalCode(ctx context.Context, passengerID string) (PersonalInvite, error) {
	out, err := s.getPersonalCode(ctx, passengerID)
	if err == nil {
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PersonalInvite{}, err
	}

	// 没有 → 生成（唯一性靠主键 · 冲突重试 3 次）
	now := formatTime(time.Now().UTC())
	for i := 0; i < 3; i++ {
		code, gerr := generateInviteCode()
		if gerr != nil {
			return PersonalInvite{}, gerr
		}
		_, ierr := s.db.ExecContext(ctx, `
			INSERT INTO personal_invite_code
			  (code, passenger_id, invited_count, fee_waiver_total, fee_waiver_used,
			   created_at, updated_at)
			VALUES (?, ?, 0, 0, 0, ?, ?)`,
			code, passengerID, now, now)
		if ierr == nil {
			return PersonalInvite{Code: code}, nil
		}
		// passenger_id UNIQUE 撞了 = 并发已插 · 读回来用它的
		if got, gerr2 := s.getPersonalCode(ctx, passengerID); gerr2 == nil {
			return got, nil
		}
		// code 主键撞了 · 换一个再试
	}
	return PersonalInvite{}, fmt.Errorf("passenger: 生成个人邀请码冲突过多")
}

func (s *Store) getPersonalCode(ctx context.Context, passengerID string) (PersonalInvite, error) {
	var out PersonalInvite
	err := s.db.QueryRowContext(ctx, `
		SELECT code, invited_count, fee_waiver_total, fee_waiver_used
		  FROM personal_invite_code WHERE passenger_id = ?`, passengerID).
		Scan(&out.Code, &out.InvitedCount, &out.FeeWaiverTotal, &out.FeeWaiverUsed)
	return out, err
}

// isSystemInviteCode 查这个码是否在系统邀请码白名单里（决定 invited 身份）。
//
// **只有白名单里的码才置 invited=1** —— 这是 §8.20 定价分层的守门人。
// 码不存在 / 停用 / 过期 / 用满 → false（不报错 —— 用户可能是填了个人码）。
func isSystemInviteCode(ctx context.Context, tx *sql.Tx, code string) (bool, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return false, nil
	}
	var maxUses sql.NullInt64
	var usedCount int
	var expiresAt sql.NullString
	var disabled int
	err := tx.QueryRowContext(ctx, `
		SELECT max_uses, used_count, expires_at, disabled
		  FROM system_invite_code WHERE code = ?`, code).
		Scan(&maxUses, &usedCount, &expiresAt, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("passenger: 查系统邀请码: %w", err)
	}
	if disabled != 0 {
		return false, nil
	}
	if maxUses.Valid && int64(usedCount) >= maxUses.Int64 {
		return false, nil
	}
	if expiresAt.Valid && expiresAt.String != "" {
		if until, perr := time.Parse(timeLayout, expiresAt.String); perr == nil {
			if !time.Now().UTC().Before(until) {
				return false, nil
			}
		}
	}
	return true, nil
}

// bumpSystemCodeUseTx 系统码用一次 · 计数 +1（在注册那个事务里）。
func bumpSystemCodeUseTx(ctx context.Context, tx *sql.Tx, code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE system_invite_code SET used_count = used_count + 1 WHERE code = ?`, code)
	if err != nil {
		return fmt.Errorf("passenger: 系统邀请码计数: %w", err)
	}
	return nil
}

// applyPersonalCodeTx 被邀请人注册时用了某人的个人码 · 记推荐关系 + 给邀请人加额度。
//
// ⚠️ **只在 Register 里调** —— 不要加"补绑好友码"的接口（decisions §8.38 已定：
// 个人码不可补绑 · 专属邀请码才可以）。
//
// 规则：
//   - 一个被邀请人**只能被邀一次**（invite_referral 主键约束）
//   - **不能自己邀自己**（CHECK 约束 + 这里显式挡）
//   - **不改被邀请人的 invited**（§8.29 铁律：个人码不划社群身份）
//   - 邀请人每拉来 1 人得 feeWaiverPerInvite 次手续费减免额度
//
// 码无效时**静默跳过**（返 nil）—— 用户填错码不该让注册失败。
func applyPersonalCodeTx(
	ctx context.Context, tx *sql.Tx, inviteeID, code string, now time.Time,
) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil
	}
	var inviterID string
	err := tx.QueryRowContext(ctx,
		`SELECT passenger_id FROM personal_invite_code WHERE code = ?`, code).Scan(&inviterID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // 不是个人码（可能是系统码或填错了）· 静默跳过
	}
	if err != nil {
		return fmt.Errorf("passenger: 查个人邀请码: %w", err)
	}
	if inviterID == inviteeID {
		return nil // 自己邀自己 · 静默忽略（注册时不该因为这个失败）
	}

	nowStr := formatTime(now)
	// 记推荐关系 · 主键冲突 = 已被邀过 · 忽略
	res, err := tx.ExecContext(ctx, `
		INSERT INTO invite_referral
		  (invitee_passenger_id, inviter_passenger_id, code, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (invitee_passenger_id) DO NOTHING`,
		inviteeID, inviterID, code, nowStr)
	if err != nil {
		return fmt.Errorf("passenger: 记推荐关系: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // 已经被邀过了 · 不重复给额度
	}

	// 邀请人 +1 邀请数 + 减免额度
	if _, err := tx.ExecContext(ctx, `
		UPDATE personal_invite_code
		   SET invited_count = invited_count + 1,
		       fee_waiver_total = fee_waiver_total + ?,
		       updated_at = ?
		 WHERE passenger_id = ?`,
		feeWaiverPerInvite, nowStr, inviterID); err != nil {
		return fmt.Errorf("passenger: 给邀请人加额度: %w", err)
	}
	return nil
}
