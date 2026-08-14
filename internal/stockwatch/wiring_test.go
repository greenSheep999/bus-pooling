package stockwatch

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
)

// 抢号链闭环 · 装配顺序（构造环）验证。
//
// 这个测试锁死 main.go 的装配契约：
//
//	Watcher{Firer:nil} → decider{Enqueuer:watcher} → watcher.SetFirer(decider)
//
// 如果哪天有人把 SetFirer 那行删了 · Notify 会静默什么都不做（firer==nil 直接 return）·
// 抢号链看起来"装配好了"但一次都不 fire —— 这种 bug 从日志上看不出来。
func TestWiring_SetFirer_ClosesLoop(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)
	ctx := context.Background()

	// ① Watcher 先建 · Firer 为 nil（模拟 main.go 第一步）
	w := New(Config{DB: database.DB, Logger: slog.Default()})
	if w == nil {
		t.Fatal("New 不该返 nil")
	}

	id := enqueueOne(t, w, "kiroceo", "wiring-1")

	// Firer 还没设 · Notify 应该什么都不干（不 panic · 不推进状态）
	if err := w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Source: "webhook"}); err != nil {
		t.Fatalf("Firer 未设时 Notify 不该报错: %v", err)
	}
	var status string
	_ = database.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE id = ?`, id).Scan(&status)
	if status != "watching" {
		t.Fatalf("Firer 未设时状态不该动 · 得 %q", status)
	}

	// ② 补上 Firer（模拟 main.go 第三步 SetFirer）
	firer := &mockFirer{}
	w.SetFirer(firer)

	// ③ 现在 Notify 应该真 fire
	if err := w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Source: "webhook"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if got := firer.firedIDs(); len(got) != 1 || got[0] != id {
		t.Fatalf("SetFirer 后应真 fire · 得 %v", got)
	}
	_ = database.QueryRowContext(ctx,
		`SELECT status FROM stock_watcher WHERE id = ?`, id).Scan(&status)
	if status != "fulfilled" {
		t.Fatalf("fire 成功应 fulfilled · 得 %q", status)
	}
}

// SetFirer nil-safe（装配路径上 Watcher 可能是 nil · 比如 DB 没装配）
func TestWiring_SetFirer_NilSafe(t *testing.T) {
	var w *Watcher
	w.SetFirer(&mockFirer{}) // 不该 panic
	if err := w.Notify(context.Background(), NotifyParams{VendorID: "x"}); err != nil {
		t.Fatalf("nil Watcher 的 Notify 应静默返 nil · 得 %v", err)
	}
}

// 三源竞争 · 只有一个能 fire 成功（conditional UPDATE 幂等）
//
// 场景：vendor 上货后 webhook（2s）+ 探针 delta（30s）+ xi8 signal（8min）可能都打过来 ·
// 挂单只能被 fire 一次 —— 否则同一个用户会被扣两次钱。
func TestWiring_ThreeSources_OnlyOneFires(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)
	ctx := context.Background()

	// turbo 开着 · 保证三个 source 都过 mode 门（专测幂等 · 不测 mode）
	dir := t.TempDir()
	turbo := NewFileFlag(filepath.Join(dir, "TURBO_ON"), "turbo", slog.Default())
	_ = turbo.Engage()

	firer := &mockFirer{}
	w := New(Config{DB: database.DB, Firer: firer, Logger: slog.Default(), Turbo: turbo})

	id := enqueueOne(t, w, "kiroceo", "race-1")

	// 两个信号源依次打过来 + 一个 manual（真实场景是并发·这里串行也能验幂等：
	// 第一个把状态推到 fulfilled · 后两个的 conditional UPDATE 匹配不到 watching）
	for _, src := range []string{"webhook", "stock_delta", "manual"} {
		if err := w.Notify(ctx, NotifyParams{VendorID: "kiroceo", Source: src}); err != nil {
			t.Fatalf("Notify(%s): %v", src, err)
		}
	}

	if got := firer.firedIDs(); len(got) != 1 {
		t.Fatalf("三源竞争只应 fire 一次（否则重复扣款）· 实际 %d 次: %v", len(got), got)
	}
	var fireCount int
	_ = database.QueryRowContext(ctx,
		`SELECT fire_count FROM stock_watcher WHERE id = ?`, id).Scan(&fireCount)
	if fireCount != 1 {
		t.Fatalf("fire_count 应 1 · 得 %d", fireCount)
	}
}
