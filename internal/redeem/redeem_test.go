package redeem

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

func setup(t *testing.T) (*Store, *wallet.Store, *db.DB) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "redeem.db")
	d, err := db.Open(ctx, path)
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return NewStore(d.DB), wallet.NewStore(d.DB), d
}

// seedPassenger 造一个乘客 + 空钱包（redeem_code 有 FK 到 passenger.used_by）。
func seedPassenger(t *testing.T, d *db.DB, id string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger (id, email, username, password_hash, created_at, updated_at)
		VALUES (?, ?, ?, 'x', ?, ?)`,
		id, id+"@example.com", id, now, now); err != nil {
		t.Fatalf("插入 passenger: %v", err)
	}
	if _, err := d.ExecContext(ctx,
		`INSERT INTO wallet (passenger_id, balance, reserved, updated_at) VALUES (?, 0, 0, ?)`,
		id, now); err != nil {
		t.Fatalf("插入 wallet: %v", err)
	}
}

func TestConsumeUnused(t *testing.T) {
	s, w, d := setup(t)
	seedPassenger(t, d, "p1")

	if err := s.Seed(context.Background(), "krc-hello", 50_000_000, "test", nil); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 小写输入 + 空白 · 内部要 Normalize
	res, err := s.Consume(context.Background(), "p1", "  krc-hello  ")
	if err != nil {
		t.Fatalf("消费: %v", err)
	}
	if res.Credits != 50_000_000 {
		t.Errorf("Credits = %d", res.Credits)
	}
	if res.Replayed {
		t.Error("首次消费不该 Replayed")
	}
	if res.BalanceAfter != 50_000_000 {
		t.Errorf("BalanceAfter = %d", res.BalanceAfter)
	}

	bal, err := w.Get(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if bal.Balance != 50_000_000 {
		t.Errorf("wallet balance = %d", bal.Balance)
	}
}

func TestConsumeReplaySamePassenger(t *testing.T) {
	s, w, d := setup(t)
	seedPassenger(t, d, "p1")
	if err := s.Seed(context.Background(), "AAA", 20_000_000, "", nil); err != nil {
		t.Fatal(err)
	}

	// 第一次
	if _, err := s.Consume(context.Background(), "p1", "AAA"); err != nil {
		t.Fatalf("首次: %v", err)
	}
	// 第二次 · 同乘客 · 应 Replayed=true 不重复入账
	res2, err := s.Consume(context.Background(), "p1", "AAA")
	if err != nil {
		t.Fatalf("重放: %v", err)
	}
	if !res2.Replayed {
		t.Error("重放该 Replayed=true")
	}
	if res2.Credits != 20_000_000 {
		t.Errorf("重放 Credits = %d", res2.Credits)
	}
	// 钱包只应加了一次
	bal, _ := w.Get(context.Background(), "p1")
	if bal.Balance != 20_000_000 {
		t.Errorf("重放后余额 = %d，应为 20000000（不重复入账）", bal.Balance)
	}
}

func TestConsumeClaimedByOther(t *testing.T) {
	s, _, d := setup(t)
	seedPassenger(t, d, "p1")
	seedPassenger(t, d, "p2")
	if err := s.Seed(context.Background(), "BBB", 10_000_000, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consume(context.Background(), "p1", "BBB"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Consume(context.Background(), "p2", "BBB")
	if !errors.Is(err, ErrClaimedByOther) {
		t.Errorf("err = %v，应为 ErrClaimedByOther", err)
	}
}

func TestConsumeNotFound(t *testing.T) {
	s, _, d := setup(t)
	seedPassenger(t, d, "p1")
	_, err := s.Consume(context.Background(), "p1", "NOPE")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v，应为 ErrNotFound", err)
	}
}

func TestConsumeExpired(t *testing.T) {
	s, _, d := setup(t)
	seedPassenger(t, d, "p1")
	past := time.Now().UTC().Add(-time.Hour)
	if err := s.Seed(context.Background(), "OLD", 10_000_000, "", &past); err != nil {
		t.Fatal(err)
	}
	_, err := s.Consume(context.Background(), "p1", "OLD")
	if !errors.Is(err, ErrExpired) {
		t.Errorf("err = %v，应为 ErrExpired", err)
	}
}

func TestConsumeEmpty(t *testing.T) {
	s, _, _ := setup(t)
	_, err := s.Consume(context.Background(), "p1", "   ")
	if !errors.Is(err, ErrEmptyCode) {
		t.Errorf("err = %v，应为 ErrEmptyCode", err)
	}
}
