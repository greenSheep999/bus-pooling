package passenger

import (
	"context"
	"strings"
	"testing"
)

// 邮箱脱敏 —— 邀请人不该拿到第三方的完整邮箱（PII）
func TestMaskEmail(t *testing.T) {
	cases := []struct{ in, want string }{
		{"zhangsan@gmail.com", "zha***@gmail.com"},
		{"abcdef@example.org", "abc***@example.org"},
		// 本地部分短 → 只留首字符
		{"ab@x.com", "a***@x.com"},
		{"a@x.com", "a***@x.com"},
		// 不像邮箱 → 整个遮掉
		{"notanemail", "***"},
		{"", "***"},
	}
	for _, c := range cases {
		if got := maskEmail(c.in); got != c.want {
			t.Errorf("maskEmail(%q) = %q · want %q", c.in, got, c.want)
		}
	}
}

// 脱敏后**不能**还能看出原始本地部分的长度（长度也是信息）
func TestMaskEmail_HidesLength(t *testing.T) {
	short := maskEmail("abcd@x.com")
	long := maskEmail("abcdefghijklmnop@x.com")
	if short != long {
		t.Errorf("不同长度的本地部分脱敏后该一样（不泄漏长度）· %q vs %q", short, long)
	}
}

// 脱敏结果里不该含完整的原始 local part
func TestMaskEmail_NoFullLocalLeak(t *testing.T) {
	got := maskEmail("secretuser@gmail.com")
	if strings.Contains(got, "secretuser") {
		t.Errorf("脱敏没生效·泄漏了完整用户名: %q", got)
	}
}

// 邀请记录：邀了 2 人 → 2 条记录 · 倒序（最新在前）· 只给脱敏标识
func TestListReferrals(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	inviter, err := s.Register(ctx, RegisterInput{
		Email: "inviter@example.com", Username: "inviter", Password: "password123",
	})
	if err != nil {
		t.Fatal(err)
	}
	pi, err := s.EnsurePersonalCode(ctx, inviter.ID)
	if err != nil {
		t.Fatal(err)
	}

	// 两个朋友用他的码注册
	for _, e := range []string{"friendone@gmail.com", "friendtwo@outlook.com"} {
		if _, err := s.Register(ctx, RegisterInput{
			Email: e, Username: strings.Split(e, "@")[0], Password: "password123",
			InviteCode: pi.Code,
		}); err != nil {
			t.Fatal(err)
		}
	}

	refs, err := s.ListReferrals(ctx, inviter.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("邀请记录数 = %d · want 2", len(refs))
	}
	for _, r := range refs {
		// 只给脱敏标识
		if !strings.Contains(r.InviteeMasked, "***") {
			t.Errorf("被邀请人该脱敏 · got=%q", r.InviteeMasked)
		}
		// 绝不能出现完整邮箱
		if strings.Contains(r.InviteeMasked, "friendone@") ||
			strings.Contains(r.InviteeMasked, "friendtwo@") {
			t.Errorf("泄漏了完整邮箱 · %q", r.InviteeMasked)
		}
		if r.WaiverGranted != feeWaiverPerInvite {
			t.Errorf("每条该带 %d 次额度 · got=%d", feeWaiverPerInvite, r.WaiverGranted)
		}
		if r.CreatedAt.IsZero() {
			t.Error("注册时间没解析出来")
		}
	}
}

// 没邀请过人 → 空列表（不是错误）
func TestListReferrals_Empty(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p, _ := s.Register(ctx, RegisterInput{
		Email: "lonely@example.com", Username: "lonely", Password: "password123",
	})
	refs, err := s.ListReferrals(ctx, p.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 0 {
		t.Errorf("没邀请过该返空 · got=%d 条", len(refs))
	}
}

// 只返自己邀请的人（不能看到别人的邀请记录）
func TestListReferrals_ScopedToInviter(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	a, _ := s.Register(ctx, RegisterInput{
		Email: "a@example.com", Username: "aaa", Password: "password123",
	})
	b, _ := s.Register(ctx, RegisterInput{
		Email: "b@example.com", Username: "bbb", Password: "password123",
	})
	aCode, _ := s.EnsurePersonalCode(ctx, a.ID)
	_, _ = s.EnsurePersonalCode(ctx, b.ID)

	// C 用 A 的码注册
	if _, err := s.Register(ctx, RegisterInput{
		Email: "c@example.com", Username: "ccc", Password: "password123",
		InviteCode: aCode.Code,
	}); err != nil {
		t.Fatal(err)
	}

	aRefs, _ := s.ListReferrals(ctx, a.ID, 50)
	bRefs, _ := s.ListReferrals(ctx, b.ID, 50)
	if len(aRefs) != 1 {
		t.Errorf("A 该有 1 条 · got=%d", len(aRefs))
	}
	if len(bRefs) != 0 {
		t.Errorf("B 没邀请过人 · 不该看到别人的记录 · got=%d 条", len(bRefs))
	}
}
