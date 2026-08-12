package stockwatch

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// decideMode 纯函数 · 阈值边界测
func TestDecideMode_Thresholds(t *testing.T) {
	cases := []struct {
		name   string
		demand int
		supply float64
		want   Mode
	}{
		{"零需求", 0, 100, ModeCool},
		{"零供应零需求", 0, 0, ModeCool},
		{"零供应有需求", 5, 0, ModeTight}, // supply < 1 补 1 · ratio = 5 > 2
		{"低压", 1, 10, ModeCool},        // 0.1 ≤ 0.3
		{"cool 边界 ratio=0.3", 3, 10, ModeCool},
		{"进 balance ratio=0.31", 31, 100, ModeBalance},
		{"balance 中段", 100, 100, ModeBalance},
		{"balance 边界 ratio=2", 200, 100, ModeBalance},
		{"进 tight ratio=2.1", 21, 10, ModeTight},
		{"高压紧张", 100, 10, ModeTight},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideMode(tc.demand, tc.supply)
			if got != tc.want {
				t.Errorf("decideMode(%d, %.1f) = %s · 期 %s", tc.demand, tc.supply, got, tc.want)
			}
		})
	}
}

// ShouldProberFire · 只 tight 态时 true
func TestModeMgr_ShouldProberFire(t *testing.T) {
	m := &ModeMgr{}
	m.current.Store(int32(ModeCool))
	if m.ShouldProberFire() {
		t.Fatal("cool 时探针不应 fire")
	}
	m.current.Store(int32(ModeBalance))
	if m.ShouldProberFire() {
		t.Fatal("balance 时探针不应 fire · 只观测")
	}
	m.current.Store(int32(ModeTight))
	if !m.ShouldProberFire() {
		t.Fatal("tight 时探针必须 fire")
	}
}

// ShouldWebhookFire · tight + balance 都 fire · cool 不 fire
func TestModeMgr_ShouldWebhookFire(t *testing.T) {
	m := &ModeMgr{}
	m.current.Store(int32(ModeCool))
	if m.ShouldWebhookFire() {
		t.Fatal("cool 时 webhook 不应 fire · 库存充足")
	}
	m.current.Store(int32(ModeBalance))
	if !m.ShouldWebhookFire() {
		t.Fatal("balance 时 webhook 应 fire")
	}
	m.current.Store(int32(ModeTight))
	if !m.ShouldWebhookFire() {
		t.Fatal("tight 时 webhook 必须 fire")
	}
}

// FileFlag · 文件存在性开关基础
func TestFileFlag_TogglesByFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TURBO_ON")

	f := NewFileFlag(path, "turbo", slog.Default())
	if f.Engaged() {
		t.Fatal("文件不存在时应 OFF")
	}
	// 建文件 → 开
	if err := f.Engage(); err != nil {
		t.Fatalf("Engage: %v", err)
	}
	if !f.Engaged() {
		t.Fatal("Engage 后应 ON")
	}
	// 删文件 → 关
	if err := f.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if f.Engaged() {
		t.Fatal("Release 后应 OFF")
	}
}

// FileFlag · 外部手动 touch 文件 · refresh 后感知（模拟运维 SSH touch）
func TestFileFlag_ExternalTouch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "KILL_PULLS")
	f := NewFileFlag(path, "kill", slog.Default())
	if f.Engaged() {
		t.Fatal("初始应 OFF")
	}
	// 外部创建（模拟 ssh vps22 'touch ...'）
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f.refresh()
	if !f.Engaged() {
		t.Fatal("外部 touch 后 refresh 应感知 ON")
	}
	// 外部删除
	_ = os.Remove(path)
	f.refresh()
	if f.Engaged() {
		t.Fatal("外部删除后 refresh 应感知 OFF")
	}
}

// FileFlag · path 空返 nil · nil-safe
func TestFileFlag_NilSafe(t *testing.T) {
	f := NewFileFlag("", "noop", slog.Default())
	if f != nil {
		t.Fatal("path 空应返 nil")
	}
	if f.Engaged() {
		t.Fatal("nil 不应 engaged")
	}
	if got := f.Path(); got != "" {
		t.Fatalf("nil Path 应空 · 得 %q", got)
	}
	// Engage / Release / Start / Stop 不 panic
	_ = f.Engage()
	_ = f.Release()
	f.Start(context.Background(), time.Second)
	f.Stop(time.Second)
}

// Notify · 急停开关开着 · 全 skip · firer 不被调用
func TestNotify_KillSwitch_Skips(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	dir := t.TempDir()
	kill := NewFileFlag(filepath.Join(dir, "KILL_PULLS"), "kill", slog.Default())
	_ = kill.Engage() // 开启急停

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default(), Kill: kill})

	ctx := context.Background()
	id := enqueueOne(t, w, "kiroceo", "order-int-kill")
	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Count: 5, Source: "webhook"})

	if len(firer.firedIDs()) != 0 {
		t.Fatal("急停时 firer 不应被调用")
	}
	// 挂单状态保留 · 没被推进
	var status string
	_ = database.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE id = ?`, id).Scan(&status)
	if status != "watching" {
		t.Fatalf("急停时挂单应保持 watching · 得 %q", status)
	}
}

// Notify · turbo 开着 · 无视 mode=cool · 强制 fire（人工干预场景）
func TestNotify_Turbo_OverridesCoolMode(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	dir := t.TempDir()
	turbo := NewFileFlag(filepath.Join(dir, "TURBO_ON"), "turbo", slog.Default())
	_ = turbo.Engage()

	mode := &ModeMgr{}
	mode.current.Store(int32(ModeCool)) // 自动判断是 cool（平时不抢）

	firer := &mockFirer{}
	w := New(Config{
		DB: database.DB, Firer: firer, Logger: slog.Default(),
		Mode: mode, Turbo: turbo,
	})

	ctx := context.Background()
	enqueueOne(t, w, "kiroceo", "order-int-turbo")

	// stock_delta 源 · mode=cool 平时会 skip · 但 turbo 开着应强制 fire
	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Source: "stock_delta"})
	if len(firer.firedIDs()) != 1 {
		t.Fatalf("turbo 开着应强制 fire（无视 cool）· 得 %d 次", len(firer.firedIDs()))
	}
}

// Notify · 急停优先级高于 turbo（两个都开 · 急停胜）
func TestNotify_KillBeatsTurbo(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	dir := t.TempDir()
	kill := NewFileFlag(filepath.Join(dir, "KILL_PULLS"), "kill", slog.Default())
	turbo := NewFileFlag(filepath.Join(dir, "TURBO_ON"), "turbo", slog.Default())
	_ = kill.Engage()
	_ = turbo.Engage()

	firer := &mockFirer{}
	w := New(Config{
		DB: database.DB, Firer: firer, Logger: slog.Default(),
		Kill: kill, Turbo: turbo,
	})

	ctx := context.Background()
	enqueueOne(t, w, "kiroceo", "order-int-both")
	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Source: "stock_delta"})

	if len(firer.firedIDs()) != 0 {
		t.Fatal("急停优先级应高于 turbo · 不该 fire")
	}
}

// Notify · mode=cool 时 stock_delta / webhook 都 skip
func TestNotify_ModeCool_Skips(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	firer := &mockFirer{}
	mode := &ModeMgr{}
	mode.current.Store(int32(ModeCool))
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default(), Mode: mode})

	ctx := context.Background()
	enqueueOne(t, w, "kiroceo", "order-int-cool")

	// stock_delta 源 · cool 应 skip
	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Source: "stock_delta"})
	if len(firer.firedIDs()) != 0 {
		t.Fatal("cool 态 · stock_delta 不应 fire")
	}
	// webhook 源 · cool 也 skip
	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Source: "webhook"})
	if len(firer.firedIDs()) != 0 {
		t.Fatal("cool 态 · webhook 也不应 fire")
	}
}

// Notify · mode=balance 时 webhook fire · stock_delta skip
func TestNotify_ModeBalance_OnlyWebhookFires(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)

	firer := &mockFirer{}
	mode := &ModeMgr{}
	mode.current.Store(int32(ModeBalance))
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default(), Mode: mode})

	ctx := context.Background()
	enqueueOne(t, w, "kiroceo", "order-int-bal")

	// stock_delta 应 skip
	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Source: "stock_delta"})
	if len(firer.firedIDs()) != 0 {
		t.Fatal("balance 态 · stock_delta 不应 fire")
	}
	// webhook 应 fire
	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Source: "webhook"})
	if len(firer.firedIDs()) != 1 {
		t.Fatalf("balance 态 · webhook 应 fire 一次 · 得 %d", len(firer.firedIDs()))
	}
}

// Notify · mode=tight 时探针 + webhook 都 fire
func TestNotify_ModeTight_BothFire(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)
	seedPassenger(t, database)

	firer := &mockFirer{}
	mode := &ModeMgr{}
	mode.current.Store(int32(ModeTight))
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default(), Mode: mode})

	ctx := context.Background()
	enqueueOne(t, w, "kiroceo", "order-int-tight-a")
	enqueueOne(t, w, "kiroceo", "order-int-tight-b")

	// stock_delta 应 fire 队列（fire 完 a 就试 b · a 成功 fulfilled · 继续 b）
	_ = w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Source: "stock_delta"})
	// 两条都应 fire 到
	if got := len(firer.firedIDs()); got != 2 {
		t.Fatalf("tight 态 · 两条应都 fire · 得 %d", got)
	}
}
