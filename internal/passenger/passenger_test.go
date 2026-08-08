package passenger

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func setup(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return NewStore(d.DB)
}

func register(t *testing.T, s *Store, email, username, password string) *Passenger {
	t.Helper()
	p, err := s.Register(context.Background(), RegisterInput{
		Email: email, Username: username, Password: password,
	})
	if err != nil {
		t.Fatalf("注册: %v", err)
	}
	return p
}

// ── 密码 ────────────────────────────────────────────

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash 应是 argon2id PHC 格式，得到 %q", hash)
	}
	if err := VerifyPassword("correct horse battery staple", hash); err != nil {
		t.Fatalf("正确密码应通过: %v", err)
	}
	if err := VerifyPassword("wrong", hash); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("错密码应报 ErrWrongPassword，得到 %v", err)
	}
}

// 同一密码两次 hash 必须不同（salt 随机）
func TestPasswordHashIsSalted(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("两次 hash 相同 —— salt 没起作用")
	}
	// 但两个都能验证通过
	if err := VerifyPassword("same", a); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword("same", b); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	cases := []string{
		"", "notahash", "$argon2id$", "$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=99$m=65536,t=3,p=2$c2FsdA$aGFzaA", // 版本不对
		"$argon2id$v=19$bad-params$c2FsdA$aGFzaA",
	}
	for _, h := range cases {
		if err := VerifyPassword("x", h); err == nil {
			t.Errorf("畸形 hash %q 应该报错", h)
		}
	}
}

// ── 注册 ────────────────────────────────────────────

func TestRegisterCreatesWallet(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "a@example.com", "alice", "password123")

	if p.ID == "" {
		t.Fatal("没返回 id")
	}
	if p.Invited {
		t.Error("没填邀请码不该是 invited")
	}

	// 钱包必须跟账号同生 —— 否则后面每个流程都要处理"钱包不存在"这个特例
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(1) FROM wallet WHERE passenger_id = ?`, p.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("注册没建钱包")
	}
}

func TestRegisterWithInviteCodeSetsInvited(t *testing.T) {
	s := setup(t)
	p, err := s.Register(context.Background(), RegisterInput{
		Email: "b@example.com", Username: "bob", Password: "password123",
		InviteCode: "KIRO-VIP",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Invited {
		t.Fatal("填了邀请码应该是 invited（决定看真名 + 免区域附加费）")
	}
	if p.InviteCodeUsed != "KIRO-VIP" {
		t.Errorf("invite_code_used = %q", p.InviteCodeUsed)
	}
}

func TestRegisterRejectsDuplicates(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	register(t, s, "dup@example.com", "dupuser", "password123")

	_, err := s.Register(ctx, RegisterInput{
		Email: "dup@example.com", Username: "other", Password: "password123"})
	if !errors.Is(err, ErrEmailTaken) {
		t.Errorf("重复邮箱应报 ErrEmailTaken，得到 %v", err)
	}

	_, err = s.Register(ctx, RegisterInput{
		Email: "other@example.com", Username: "dupuser", Password: "password123"})
	if !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("重复用户名应报 ErrUsernameTaken，得到 %v", err)
	}
}

func TestRegisterNormalizesEmail(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "  MiXeD@Example.COM  ", "mixed", "password123")
	if p.Email != "mixed@example.com" {
		t.Fatalf("邮箱没规范化: %q", p.Email)
	}
	// 大写形式也该被认作重复
	_, err := s.Register(ctx, RegisterInput{
		Email: "MIXED@EXAMPLE.COM", Username: "another", Password: "password123"})
	if !errors.Is(err, ErrEmailTaken) {
		t.Errorf("大小写不同的同一邮箱应算重复，得到 %v", err)
	}
}

// ── 登录 ────────────────────────────────────────────

func TestAuthenticateByEmailOrUsername(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	register(t, s, "c@example.com", "carol", "password123")

	for _, account := range []string{"c@example.com", "carol", "CAROL", "C@EXAMPLE.COM"} {
		if _, err := s.Authenticate(ctx, account, "password123"); err != nil {
			t.Errorf("用 %q 登录失败: %v", account, err)
		}
	}
}

// 账号不存在和密码错必须返回同一个错误 —— 否则接口成了账号枚举器
func TestAuthenticateDoesNotLeakAccountExistence(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	register(t, s, "d@example.com", "dave", "password123")

	_, errWrongPw := s.Authenticate(ctx, "d@example.com", "wrong")
	_, errNoAcct := s.Authenticate(ctx, "nobody@example.com", "whatever")

	if !errors.Is(errWrongPw, ErrWrongPassword) {
		t.Fatalf("密码错应报 ErrWrongPassword，得到 %v", errWrongPw)
	}
	if !errors.Is(errNoAcct, ErrWrongPassword) {
		t.Fatalf("账号不存在也应报 ErrWrongPassword（防枚举），得到 %v", errNoAcct)
	}
}

func TestAuthenticateRejectsDisabledAccount(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "e@example.com", "eve", "password123")

	if _, err := s.db.ExecContext(ctx,
		`UPDATE passenger SET status = 'disabled' WHERE id = ?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, "e@example.com", "password123"); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("停用账号应报 ErrAccountDisabled，得到 %v", err)
	}
}

func TestAuthenticateUpdatesLastLogin(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "f@example.com", "frank", "password123")
	if p.LastLoginAt != nil {
		t.Fatal("刚注册不该有 last_login_at")
	}

	got, err := s.Authenticate(ctx, "f@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastLoginAt == nil {
		t.Fatal("登录后应记录 last_login_at")
	}
}

// ── 改密码 ──────────────────────────────────────────

func TestChangePassword(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "g@example.com", "grace", "oldpassword")

	if err := s.ChangePassword(ctx, p.ID, "wrong", "newpassword"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("旧密码不对应报错，得到 %v", err)
	}
	if err := s.ChangePassword(ctx, p.ID, "oldpassword", "newpassword"); err != nil {
		t.Fatalf("改密码: %v", err)
	}

	if _, err := s.Authenticate(ctx, "g@example.com", "oldpassword"); !errors.Is(err, ErrWrongPassword) {
		t.Error("旧密码改后还能登录")
	}
	if _, err := s.Authenticate(ctx, "g@example.com", "newpassword"); err != nil {
		t.Errorf("新密码登录失败: %v", err)
	}
}

// ── Session ────────────────────────────────────────

func TestSessionLifecycle(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "h@example.com", "henry", "password123")

	token, expires, err := s.CreateSession(ctx, p.ID, "1.2.3.4", "test-ua", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 64 { // 32 字节 hex
		t.Errorf("token 长度 = %d", len(token))
	}
	if d := time.Until(expires); d > SessionTTL+time.Minute || d < SessionTTL-time.Minute {
		t.Errorf("默认有效期 %v，应约 %v", d, SessionTTL)
	}

	owner, err := s.SessionOwner(ctx, token)
	if err != nil {
		t.Fatalf("用 token 查账号: %v", err)
	}
	if owner.ID != p.ID {
		t.Errorf("查到的是别人: %s", owner.ID)
	}

	if err := s.RevokeSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionOwner(ctx, token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("吊销后应无效，得到 %v", err)
	}
}

// 明文 token 不能落库 —— 库被读走也不该能直接用
func TestSessionTokenNotStoredInPlaintext(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "i@example.com", "iris", "password123")
	token, _, err := s.CreateSession(ctx, p.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(1) FROM session WHERE id = ?`, token).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("session 表里存了明文 token")
	}
	// 存的应该是 hash
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(1) FROM session WHERE id = ?`, hashToken(token)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("session 表里没找到 token 的 hash")
	}
}

func TestSessionRememberExtendsTTL(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "j@example.com", "jack", "password123")

	_, expires, err := s.CreateSession(ctx, p.ID, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(expires); d < SessionTTLRemember-time.Minute {
		t.Errorf("记住我有效期 %v，应约 %v", d, SessionTTLRemember)
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "k@example.com", "kate", "password123")
	token, _, err := s.CreateSession(ctx, p.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}

	// 手动把过期时间推到过去
	if _, err := s.db.ExecContext(ctx,
		`UPDATE session SET expires_at = ? WHERE id = ?`,
		formatTime(time.Now().UTC().Add(-time.Hour)), hashToken(token)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionOwner(ctx, token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("过期会话应无效，得到 %v", err)
	}
}

func TestSessionRejectsGarbageToken(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, tok := range []string{"", "not-a-token", strings.Repeat("a", 64)} {
		if _, err := s.SessionOwner(ctx, tok); !errors.Is(err, ErrSessionInvalid) {
			t.Errorf("垃圾 token %q 应无效，得到 %v", tok, err)
		}
	}
}

// ── API key ────────────────────────────────────────

func TestAPIKeyLifecycle(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "l@example.com", "leo", "password123")

	plaintext, key, err := s.CreateAPIKey(ctx, p.ID, "CI 脚本")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plaintext, APIKeyPrefix) {
		t.Errorf("明文应以 %q 开头，得到 %q", APIKeyPrefix, plaintext)
	}
	if key.Prefix != plaintext[:len(key.Prefix)] {
		t.Errorf("存的 prefix %q 不是明文的前缀", key.Prefix)
	}

	owner, err := s.APIKeyOwner(ctx, plaintext)
	if err != nil {
		t.Fatalf("用 key 查账号: %v", err)
	}
	if owner.ID != p.ID {
		t.Error("查到的是别人")
	}

	keys, err := s.ListAPIKeys(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Name != "CI 脚本" {
		t.Fatalf("列表不对: %+v", keys)
	}
	if keys[0].LastUsedAt == nil {
		t.Error("用过之后应记 last_used_at")
	}

	if err := s.RevokeAPIKey(ctx, p.ID, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.APIKeyOwner(ctx, plaintext); !errors.Is(err, ErrAPIKeyInvalid) {
		t.Fatalf("吊销后应无效，得到 %v", err)
	}

	// 吊销**不删行** —— 台账留痕
	keys, _ = s.ListAPIKeys(ctx, p.ID)
	if len(keys) != 1 {
		t.Fatal("吊销把行删了，应该保留（台账留痕）")
	}
	if !keys[0].Revoked {
		t.Error("吊销后 Revoked 应为 true")
	}
}

func TestAPIKeyPlaintextNotStored(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "m@example.com", "mia", "password123")
	plaintext, _, err := s.CreateAPIKey(ctx, p.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(1) FROM passenger_api_key WHERE key_hash = ?`, plaintext).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("api key 表里存了明文")
	}
}

// 不能吊销别人的 key
func TestRevokeAPIKeyIsScopedToOwner(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	a := register(t, s, "n@example.com", "nina", "password123")
	b := register(t, s, "o@example.com", "oscar", "password123")

	_, key, err := s.CreateAPIKey(ctx, a.ID, "alice 的")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAPIKey(ctx, b.ID, key.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("吊销别人的 key 应报 ErrNotFound，得到 %v", err)
	}
	// 确认没被吊销
	keys, _ := s.ListAPIKeys(ctx, a.ID)
	if keys[0].Revoked {
		t.Fatal("别人成功吊销了我的 key")
	}
}

func TestAPIKeyRejectsGarbage(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, k := range []string{"", "nope", "usr-deadbeef", "Bearer usr-x"} {
		if _, err := s.APIKeyOwner(ctx, k); !errors.Is(err, ErrAPIKeyInvalid) {
			t.Errorf("垃圾 key %q 应无效，得到 %v", k, err)
		}
	}
}

func TestDisabledAccountBlocksBothAuthPaths(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	p := register(t, s, "q@example.com", "quinn", "password123")

	token, _, err := s.CreateSession(ctx, p.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, _, err := s.CreateAPIKey(ctx, p.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE passenger SET status = 'disabled' WHERE id = ?`, p.ID); err != nil {
		t.Fatal(err)
	}

	// 停用后两条鉴权路径都要挡住 —— 只挡一条等于没挡
	if _, err := s.SessionOwner(ctx, token); !errors.Is(err, ErrAccountDisabled) {
		t.Errorf("停用后会话应被挡，得到 %v", err)
	}
	if _, err := s.APIKeyOwner(ctx, plaintext); !errors.Is(err, ErrAccountDisabled) {
		t.Errorf("停用后 API key 应被挡，得到 %v", err)
	}
}
