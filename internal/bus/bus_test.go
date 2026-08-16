package bus

import (
	"context"
	"errors"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func setup(t *testing.T) (*Store, string, string) {
	t.Helper()
	ctx := context.Background()
	d := db.NewTestDB(t)
	for _, pid := range []string{"p1", "p2"} {
		if _, err := d.ExecContext(ctx, `
			INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
			VALUES (?, ?, ?, 'x', '2026-01-01', '2026-01-01')`,
			pid, pid, pid+"@x.com"); err != nil {
			t.Fatal(err)
		}
	}
	return NewStore(d.DB), "p1", "p2"
}

func TestCreateSingleBus(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()

	b, err := s.Create(ctx, CreateInput{Name: "我的号池", Kind: KindSingle, CreatorID: pid})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.Kind != KindSingle || b.Status != StatusActive {
		t.Errorf("kind=%q status=%q", b.Kind, b.Status)
	}
	if b.CreatorID != pid {
		t.Errorf("creator = %q", b.CreatorID)
	}

	// creator 应被自动加成 owner
	members, err := s.Members(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 {
		t.Fatalf("成员数 = %d，want 1", len(members))
	}
	if members[0].Role != "owner" || members[0].SharePct != 100 {
		t.Errorf("owner role/share = %q/%d", members[0].Role, members[0].SharePct)
	}
}

// 1c · single / anon / team 都允许 · 只挡未知 kind
func TestCreateRejectsGarbageKind(t *testing.T) {
	s, pid, _ := setup(t)
	if _, err := s.Create(context.Background(),
		CreateInput{Name: "x", Kind: "garbage", CreatorID: pid}); !errors.Is(err, ErrBadKind) {
		t.Errorf("garbage kind 应返 ErrBadKind · got=%v", err)
	}
}

// 1c · anon 车能建 · MaxMembers 由 Store（config.Bus.MaxMembers）统一强制·忽略入参
func TestCreateAnonAllowed(t *testing.T) {
	s, pid, _ := setup(t)
	// 用 max=3 的 Store 覆盖·验证 config 生效
	s3 := NewStoreWithConfig(s.db, 3)
	b, err := s3.Create(context.Background(), CreateInput{
		Name:             "拼车",
		Kind:             KindAnon,
		CreatorID:        pid,
		MaxMembers:       999, // 前端胡传·后端应忽略
		AnonZone:         "us",
		AnonMaxUnitPrice: 30_000_000,
	})
	if err != nil {
		t.Fatalf("建 anon 车失败: %v", err)
	}
	if b.Kind != KindAnon || b.AnonZone != "us" {
		t.Errorf("anon 基础属性丢失: %+v", b)
	}
	if b.MaxMembers != 3 {
		t.Errorf("MaxMembers 应从 config 走·不听前端 · got=%d want=3", b.MaxMembers)
	}
}

func TestCreateValidatesInput(t *testing.T) {
	s, pid, _ := setup(t)
	cases := []struct {
		name string
		in   CreateInput
	}{
		{"空车名", CreateInput{Name: "", Kind: KindSingle, CreatorID: pid}},
		{"缺 creator", CreateInput{Name: "x", Kind: KindSingle}},
		{"车名过长", CreateInput{Name: string(make([]byte, 200)), Kind: KindSingle, CreatorID: pid}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Create(context.Background(), tc.in); err == nil {
				t.Error("应报错")
			}
		})
	}
}

// GetForPassenger 对**非成员**返 ErrNotMember 而不是 ErrNotFound ——
// 后者会泄漏"车存在"信息
func TestGetForPassengerRejectsNonMember(t *testing.T) {
	s, pid, other := setup(t)
	b, _ := s.Create(context.Background(),
		CreateInput{Name: "x", Kind: KindSingle, CreatorID: pid})

	if _, err := s.GetForPassenger(context.Background(), b.ID, other); !errors.Is(err, ErrNotMember) {
		t.Errorf("非成员应返回 ErrNotMember，得到 %v", err)
	}
}

func TestGetForPassengerAllowsMember(t *testing.T) {
	s, pid, _ := setup(t)
	b, _ := s.Create(context.Background(),
		CreateInput{Name: "x", Kind: KindSingle, CreatorID: pid})

	got, err := s.GetForPassenger(context.Background(), b.ID, pid)
	if err != nil {
		t.Fatalf("GetForPassenger: %v", err)
	}
	if got.ID != b.ID {
		t.Errorf("id = %q", got.ID)
	}
}

func TestGetNotFound(t *testing.T) {
	s, _, _ := setup(t)
	if _, err := s.Get(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("应返回 ErrNotFound，得到 %v", err)
	}
}

// ListForPassenger 只返自己参与的活跃车
func TestListForPassengerScopedToMemberAndActive(t *testing.T) {
	s, pid, other := setup(t)
	ctx := context.Background()

	b1, _ := s.Create(ctx, CreateInput{Name: "我的 A", Kind: KindSingle, CreatorID: pid})
	_, _ = s.Create(ctx, CreateInput{Name: "别人的", Kind: KindSingle, CreatorID: other})
	b3, _ := s.Create(ctx, CreateInput{Name: "我的 B 要解散", Kind: KindSingle, CreatorID: pid})
	if err := s.Dissolve(ctx, b3.ID, pid); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListForPassenger(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != b1.ID {
		t.Errorf("应只返回 A（活跃 · 属我）· 得到 %+v", got)
	}
}

// 单人车 owner 不能"退出"，只能解散
func TestOwnerCannotLeave(t *testing.T) {
	s, pid, _ := setup(t)
	b, _ := s.Create(context.Background(),
		CreateInput{Name: "x", Kind: KindSingle, CreatorID: pid})

	if err := s.Leave(context.Background(), b.ID, pid); !errors.Is(err, ErrOwnerCantLeave) {
		t.Errorf("owner 退车应返回 ErrOwnerCantLeave，得到 %v", err)
	}
}

// 非成员"退出"应返 ErrNotMember
func TestLeaveNotMember(t *testing.T) {
	s, pid, other := setup(t)
	b, _ := s.Create(context.Background(),
		CreateInput{Name: "x", Kind: KindSingle, CreatorID: pid})

	if err := s.Leave(context.Background(), b.ID, other); !errors.Is(err, ErrNotMember) {
		t.Errorf("非成员退车应返回 ErrNotMember，得到 %v", err)
	}
}

func TestDissolveByCreator(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()
	b, _ := s.Create(ctx, CreateInput{Name: "x", Kind: KindSingle, CreatorID: pid})

	if err := s.Dissolve(ctx, b.ID, pid); err != nil {
		t.Fatalf("Dissolve: %v", err)
	}
	got, _ := s.Get(ctx, b.ID)
	if got.Status != StatusDissolved {
		t.Errorf("status = %q", got.Status)
	}
	if got.DissolvedAt == nil {
		t.Error("dissolved_at 应有值")
	}

	// 重复解散
	if err := s.Dissolve(ctx, b.ID, pid); !errors.Is(err, ErrDissolved) {
		t.Errorf("重复解散应返回 ErrDissolved，得到 %v", err)
	}
}

// 非 creator 解散应被拒
func TestDissolveByNonCreatorRejected(t *testing.T) {
	s, pid, other := setup(t)
	b, _ := s.Create(context.Background(),
		CreateInput{Name: "x", Kind: KindSingle, CreatorID: pid})

	if err := s.Dissolve(context.Background(), b.ID, other); !errors.Is(err, ErrNotMember) {
		t.Errorf("非 creator 解散应被拒，得到 %v", err)
	}
	// 状态不该被改
	got, _ := s.Get(context.Background(), b.ID)
	if got.Status != StatusActive {
		t.Errorf("被非 creator 尝试解散后状态 = %q", got.Status)
	}
}

// ── 1c team · 邀请码组队 ──

// 建 team 车时自动生成邀请码 · 8 位 · 字符集在白名单内。
func TestCreateTeamGeneratesInviteCode(t *testing.T) {
	s, pid, _ := setup(t)
	b, err := s.Create(context.Background(), CreateInput{
		Name: "程序员拼车", Kind: KindTeam, CreatorID: pid, MaxMembers: 3,
	})
	if err != nil {
		t.Fatalf("建 team 车: %v", err)
	}
	if b.Kind != KindTeam {
		t.Errorf("kind=%q · want=team", b.Kind)
	}
	if len(b.InviteCode) != inviteCodeLen {
		t.Errorf("邀请码长度 = %d · want=%d", len(b.InviteCode), inviteCodeLen)
	}
	for _, ch := range b.InviteCode {
		if !contains(inviteAlphabet, byte(ch)) {
			t.Errorf("邀请码 %q 含非白名单字符 %q", b.InviteCode, ch)
		}
	}
}

func contains(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}

// FindByInviteCode + JoinByInviteCode 完整链路
func TestJoinByInviteCode(t *testing.T) {
	s, pid, other := setup(t)
	b, err := s.Create(context.Background(), CreateInput{
		Name: "friends", Kind: KindTeam, CreatorID: pid, MaxMembers: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	joined, err := s.JoinByInviteCode(context.Background(), b.InviteCode, other)
	if err != nil {
		t.Fatalf("JoinByInviteCode: %v", err)
	}
	if joined.ID != b.ID {
		t.Errorf("加入的车 id 不对 · got=%q · want=%q", joined.ID, b.ID)
	}
	members, _ := s.Members(context.Background(), b.ID)
	if len(members) != 2 {
		t.Errorf("成员数 = %d · want=2", len(members))
	}
}

// 邀请码大小写不敏感（存大写 · 用户输小写也能找到）
func TestJoinByInviteCode_CaseInsensitive(t *testing.T) {
	s, pid, other := setup(t)
	b, _ := s.Create(context.Background(), CreateInput{
		Name: "friends", Kind: KindTeam, CreatorID: pid, MaxMembers: 3,
	})
	// 转小写调
	lower := ""
	for i := 0; i < len(b.InviteCode); i++ {
		ch := b.InviteCode[i]
		if ch >= 'A' && ch <= 'Z' {
			ch = ch + 32
		}
		lower = lower + string(ch)
	}
	if _, err := s.JoinByInviteCode(context.Background(), lower, other); err != nil {
		t.Errorf("小写邀请码应可用 · got=%v", err)
	}
}

// 邀请码无效 → ErrInvalidInvite（不是 ErrNotFound · 避免枚举攻击靠含义区分）
func TestJoinByInviteCode_InvalidReturnsInvalidInvite(t *testing.T) {
	s, _, other := setup(t)
	if _, err := s.JoinByInviteCode(context.Background(), "AAAA0000", other); !errors.Is(err, ErrInvalidInvite) {
		t.Errorf("无效邀请码应返 ErrInvalidInvite · got=%v", err)
	}
	if _, err := s.JoinByInviteCode(context.Background(), "", other); !errors.Is(err, ErrInvalidInvite) {
		t.Errorf("空邀请码应返 ErrInvalidInvite · got=%v", err)
	}
}

// 车满 → ErrBusFull
// 1c · max_members 从 config.Bus.MaxMembers 传入·测试用 NewStoreWithConfig(db, 2) 建 max=2 车
func TestJoinByInviteCode_BusFull(t *testing.T) {
	s, pid, other := setup(t)
	// 用 max=2 的 Store 覆盖·avoid 走 default 5
	s2 := NewStoreWithConfig(s.db, 2)
	b, err := s2.Create(context.Background(), CreateInput{
		Name: "small-team", Kind: KindTeam, CreatorID: pid,
	})
	if err != nil {
		t.Fatal(err)
	}
	// owner + 1 → 满 · 第 3 位应满
	if _, err := s2.JoinByInviteCode(context.Background(), b.InviteCode, other); err != nil {
		t.Fatalf("第 2 位 join 应成功: %v", err)
	}
	if _, err := s.db.ExecContext(context.Background(), `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES ('p_overflow', 'overflow', 'overflow@x.com', 'x', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.JoinByInviteCode(context.Background(), b.InviteCode, "p_overflow"); !errors.Is(err, ErrBusFull) {
		t.Errorf("满车（2/2）第 3 位应返 ErrBusFull · got=%v", err)
	}
}

// 车已解散 → ErrInvalidInvite（跟无效码同响应·避免枚举）
func TestJoinByInviteCode_DissolvedBusNotDisclosed(t *testing.T) {
	s, pid, other := setup(t)
	b, _ := s.Create(context.Background(), CreateInput{
		Name: "gone", Kind: KindTeam, CreatorID: pid, MaxMembers: 3,
	})
	if err := s.Dissolve(context.Background(), b.ID, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.JoinByInviteCode(context.Background(), b.InviteCode, other); !errors.Is(err, ErrInvalidInvite) {
		t.Errorf("解散车的邀请码应返 ErrInvalidInvite（不泄漏车存在） · got=%v", err)
	}
}

// RegenerateInviteCode · owner 换码 · 旧码立即失效
func TestRegenerateInviteCode_OldCodeInvalidated(t *testing.T) {
	s, pid, other := setup(t)
	b, _ := s.Create(context.Background(), CreateInput{
		Name: "rotate", Kind: KindTeam, CreatorID: pid, MaxMembers: 5,
	})
	oldCode := b.InviteCode

	newCode, err := s.RegenerateInviteCode(context.Background(), b.ID, pid)
	if err != nil {
		t.Fatalf("RegenerateInviteCode: %v", err)
	}
	if newCode == oldCode {
		t.Errorf("换码结果跟旧码相同 · 应换新的")
	}
	// 旧码不能再加入
	if _, err := s.JoinByInviteCode(context.Background(), oldCode, other); !errors.Is(err, ErrInvalidInvite) {
		t.Errorf("旧码换掉后应 invalid · got=%v", err)
	}
	// 新码可以
	if _, err := s.JoinByInviteCode(context.Background(), newCode, other); err != nil {
		t.Errorf("新码应可用 · got=%v", err)
	}
}

// 非 owner 不能换邀请码 → ErrNotOwner
func TestRegenerateInviteCode_NonOwnerRejected(t *testing.T) {
	s, pid, other := setup(t)
	b, _ := s.Create(context.Background(), CreateInput{
		Name: "guard", Kind: KindTeam, CreatorID: pid, MaxMembers: 3,
	})
	if _, err := s.RegenerateInviteCode(context.Background(), b.ID, other); !errors.Is(err, ErrNotOwner) {
		t.Errorf("非 owner 换码应返 ErrNotOwner · got=%v", err)
	}
}

// 用户建的车（single / team）都能换邀请码 · 只有系统 anon 撮合池不能
func TestRegenerateInviteCode_UserBusesAllowedAnonRejected(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()

	// single 车能换码（single 跟 team 行为一致 · CLAUDE.md §2）
	single, err := s.Create(ctx, CreateInput{Name: "solo", Kind: KindSingle, CreatorID: pid})
	if err != nil {
		t.Fatal(err)
	}
	if single.InviteCode == "" {
		t.Error("single 车建出来就该有邀请码（1 人独享·随时可邀人）")
	}
	newCode, err := s.RegenerateInviteCode(ctx, single.ID, pid)
	if err != nil {
		t.Errorf("single 车应能换码 · got=%v", err)
	}
	if newCode == single.InviteCode {
		t.Error("换码后应是新码")
	}

	// anon 撮合池没码可换
	anon, _ := s.Create(ctx, CreateInput{Name: "share", Kind: KindAnon, CreatorID: pid})
	if anon.InviteCode != "" {
		t.Errorf("anon 撮合池不该有邀请码 · got=%q", anon.InviteCode)
	}
	if _, err := s.RegenerateInviteCode(ctx, anon.ID, pid); !errors.Is(err, ErrBadKind) {
		t.Errorf("anon 车换码应返 ErrBadKind · got=%v", err)
	}
}

// single 车能被邀请码加入 —— 1 人独享的车加了人就是多人拼车（不是错误路径）
func TestJoinByInviteCode_SingleBusBecomesMulti(t *testing.T) {
	s, pid, other := setup(t)
	ctx := context.Background()
	b, err := s.Create(ctx, CreateInput{Name: "我的车", Kind: KindSingle, CreatorID: pid})
	if err != nil {
		t.Fatal(err)
	}
	joined, err := s.JoinByInviteCode(ctx, b.InviteCode, other)
	if err != nil {
		t.Fatalf("single 车应能用邀请码加人 · got=%v", err)
	}
	members, _ := s.Members(ctx, joined.ID)
	if len(members) != 2 {
		t.Errorf("加人后成员数 = %d · want=2", len(members))
	}
	// share_pct 应该重算成均分
	for _, m := range members {
		if m.SharePct != 50 {
			t.Errorf("2 人车 share_pct 应 50 · got=%d (%s)", m.SharePct, m.Role)
		}
	}
}

// EnsureInviteCode · 老数据（没码的车）读时自愈补码 · 幂等
func TestEnsureInviteCode_BackfillsAndIsIdempotent(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()
	b, _ := s.Create(ctx, CreateInput{Name: "老车", Kind: KindSingle, CreatorID: pid})
	// 模拟 1c 之前的老数据：把码清掉
	if _, err := s.db.ExecContext(ctx, `UPDATE bus SET invite_code = NULL WHERE id = ?`, b.ID); err != nil {
		t.Fatal(err)
	}
	stale, _ := s.Get(ctx, b.ID)
	if stale.InviteCode != "" {
		t.Fatal("前置条件没生效")
	}

	if err := s.EnsureInviteCode(ctx, stale); err != nil {
		t.Fatalf("EnsureInviteCode: %v", err)
	}
	if stale.InviteCode == "" {
		t.Fatal("补码后 InviteCode 仍为空")
	}
	first := stale.InviteCode

	// 落库了吗
	reread, _ := s.Get(ctx, b.ID)
	if reread.InviteCode != first {
		t.Errorf("补的码没落库 · db=%q · want=%q", reread.InviteCode, first)
	}
	// 幂等：再调一次不换码
	if err := s.EnsureInviteCode(ctx, reread); err != nil {
		t.Fatal(err)
	}
	if reread.InviteCode != first {
		t.Errorf("EnsureInviteCode 应幂等·不该换码 · got=%q want=%q", reread.InviteCode, first)
	}
}

// anon 撮合池不补码
func TestEnsureInviteCode_SkipsAnon(t *testing.T) {
	s, pid, _ := setup(t)
	ctx := context.Background()
	anon, _ := s.Create(ctx, CreateInput{Name: "撮合池", Kind: KindAnon, CreatorID: pid})
	if err := s.EnsureInviteCode(ctx, anon); err != nil {
		t.Fatal(err)
	}
	if anon.InviteCode != "" {
		t.Errorf("anon 池不该补码 · got=%q", anon.InviteCode)
	}
}

// 邀请码字符集不含易混淆字符（0/O / 1/I/L）
func TestInviteAlphabet_NoConfusingChars(t *testing.T) {
	forbidden := "0O1IL"
	for i := 0; i < len(forbidden); i++ {
		if contains(inviteAlphabet, forbidden[i]) {
			t.Errorf("邀请码字符集不该含易混字符 %q", forbidden[i])
		}
	}
}
