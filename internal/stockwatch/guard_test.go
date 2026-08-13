package stockwatch

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
)

// mockGuard · 可控的 blocked 判定
type mockGuard struct {
	blocked bool
	reason  string
	calls   int
}

func (m *mockGuard) IsBlocked(_ context.Context, _, _ string) (bool, string) {
	m.calls++
	return m.blocked, m.reason
}

// blocked → 不 fire（docs/20 §3）
func TestNotify_BlockedGuard_Skips(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	firer := &mockFirer{}
	guard := &mockGuard{blocked: true, reason: "成本可疑"}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default(), Guard: guard})

	enqueueOne(t, w, "kiroceo", "order-blocked")
	_ = w.Notify(context.Background(), NotifyParams{VendorID: "kiroceo", Source: "webhook"})

	if len(firer.firedIDs()) != 0 {
		t.Fatalf("blocked 应不 fire · 得 %d 次", len(firer.firedIDs()))
	}
	if guard.calls == 0 {
		t.Error("guard 应被查过")
	}
}

// not blocked → 正常 fire
func TestNotify_NotBlocked_Fires(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	firer := &mockFirer{}
	guard := &mockGuard{blocked: false}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default(), Guard: guard})

	enqueueOne(t, w, "kiroceo", "order-ok")
	_ = w.Notify(context.Background(), NotifyParams{VendorID: "kiroceo", Source: "webhook"})

	if len(firer.firedIDs()) != 1 {
		t.Fatalf("not blocked 应 fire · 得 %d 次", len(firer.firedIDs()))
	}
}

// turbo 强制时绕过 guard（人工意志最高）
func TestNotify_TurboBypassesGuard(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	dir := t.TempDir()
	turbo := NewFileFlag(filepath.Join(dir, "TURBO_ON"), "turbo", slog.Default())
	_ = turbo.Engage()

	firer := &mockFirer{}
	guard := &mockGuard{blocked: true, reason: "停售"}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default(), Turbo: turbo, Guard: guard})

	enqueueOne(t, w, "kiroceo", "order-turbo-guard")
	_ = w.Notify(context.Background(), NotifyParams{VendorID: "kiroceo", Source: "webhook"})

	if len(firer.firedIDs()) != 1 {
		t.Fatalf("turbo 应绕过 guard 强制 fire · 得 %d 次", len(firer.firedIDs()))
	}
}

// guard nil → 不拦（老装配兼容）
func TestNotify_NilGuard_Fires(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default()}) // 无 Guard

	enqueueOne(t, w, "kiroceo", "order-noguard")
	_ = w.Notify(context.Background(), NotifyParams{VendorID: "kiroceo", Source: "webhook"})

	if len(firer.firedIDs()) != 1 {
		t.Fatalf("guard nil 应正常 fire · 得 %d 次", len(firer.firedIDs()))
	}
}

// 急停优先级高于 guard 放行（kill 开着 · 即便 not blocked 也不 fire）
func TestNotify_KillBeatsGuard(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	dir := t.TempDir()
	kill := NewFileFlag(filepath.Join(dir, "KILL_PULLS"), "kill", slog.Default())
	_ = kill.Engage()

	firer := &mockFirer{}
	guard := &mockGuard{blocked: false}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default(), Kill: kill, Guard: guard})

	enqueueOne(t, w, "kiroceo", "order-kill-guard")
	_ = w.Notify(context.Background(), NotifyParams{VendorID: "kiroceo", Source: "webhook"})

	if len(firer.firedIDs()) != 0 {
		t.Fatal("急停应最高优先 · 不 fire")
	}
	if guard.calls != 0 {
		t.Error("急停时不该走到 guard（提前返回）")
	}
}
