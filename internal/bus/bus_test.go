package bus

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func setup(t *testing.T) (*Store, string, string) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
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

// 阶段 1a 只支持 single —— anon/team 显式拒绝
func TestCreateRejectsAnonAndTeam(t *testing.T) {
	s, pid, _ := setup(t)
	for _, k := range []Kind{KindAnon, KindTeam, "garbage"} {
		if _, err := s.Create(context.Background(),
			CreateInput{Name: "x", Kind: k, CreatorID: pid}); !errors.Is(err, ErrBadKind) {
			t.Errorf("kind=%q 应返回 ErrBadKind，得到 %v", k, err)
		}
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
