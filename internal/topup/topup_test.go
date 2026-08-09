package topup

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
	path := filepath.Join(t.TempDir(), "topup.db")
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

func TestBreakdownFor(t *testing.T) {
	// 想充 100 积分 → 手续费 5 → paid 105（CLAUDE.md §1.4）
	b := BreakdownFor(100_000_000)
	if b.Credits != 100_000_000 {
		t.Errorf("credits = %d", b.Credits)
	}
	if b.ChannelFee != 5_000_000 {
		t.Errorf("channel_fee = %d，应 5000000（5%%）", b.ChannelFee)
	}
	if b.Paid != 105_000_000 {
		t.Errorf("paid = %d，应 105000000", b.Paid)
	}
}

func TestCreateOrderPending(t *testing.T) {
	s, _, d := setup(t)
	seedPassenger(t, d, "p1")

	o, err := s.CreateOrder(context.Background(), "p1", "waffo", 100_000_000, "https://waffo.example/x", 15*time.Minute)
	if err != nil {
		t.Fatalf("起单: %v", err)
	}
	if o.Status != StatusPending {
		t.Errorf("status = %q", o.Status)
	}
	if o.Paid != 105_000_000 {
		t.Errorf("paid = %d", o.Paid)
	}
	if o.Credits != 100_000_000 {
		t.Errorf("credits = %d", o.Credits)
	}
	if o.ChannelFee != 5_000_000 {
		t.Errorf("fee = %d", o.ChannelFee)
	}
}

func TestCreateOrderRejectsInvalid(t *testing.T) {
	s, _, d := setup(t)
	seedPassenger(t, d, "p1")
	if _, err := s.CreateOrder(context.Background(), "p1", "waffo", 0, "x", 0); !errors.Is(err, ErrInvalidAmount) {
		t.Errorf("credits=0 应报 ErrInvalidAmount, got %v", err)
	}
	if _, err := s.CreateOrder(context.Background(), "p1", "alipay", 100, "x", 0); !errors.Is(err, ErrUnsupportedChannel) {
		t.Errorf("bad channel 应报 ErrUnsupportedChannel, got %v", err)
	}
}

func TestMarkPaidCreditsTwoLedgerEntries(t *testing.T) {
	s, w, d := setup(t)
	seedPassenger(t, d, "p1")

	o, err := s.CreateOrder(context.Background(), "p1", "waffo", 100_000_000, "u", 0)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.MarkPaid(context.Background(), o.ID)
	if err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	if out.Status != StatusPaid {
		t.Errorf("status = %q", out.Status)
	}
	if out.WalletLedgerID == "" {
		t.Error("反填 wallet_ledger_id 空")
	}

	// 钱包应 +100（recharge 105 - channel_fee 5）
	bal, _ := w.Get(context.Background(), "p1")
	if bal.Balance != 100_000_000 {
		t.Errorf("余额 = %d，应 100000000（净 +credits）", bal.Balance)
	}

	// 流水应恰好两条：recharge + channel_fee
	entries, total, err := w.List(context.Background(), "p1", wallet.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(entries) != 2 {
		t.Fatalf("流水条数 = %d，应为 2", total)
	}
	byReason := map[wallet.Reason]int64{}
	for _, e := range entries {
		byReason[e.Reason] = e.Amount
	}
	if byReason[wallet.ReasonRecharge] != 105_000_000 {
		t.Errorf("recharge 金额 = %d", byReason[wallet.ReasonRecharge])
	}
	if byReason[wallet.ReasonChannelFee] != -5_000_000 {
		t.Errorf("channel_fee 金额 = %d", byReason[wallet.ReasonChannelFee])
	}
}

func TestMarkPaidIdempotent(t *testing.T) {
	s, w, d := setup(t)
	seedPassenger(t, d, "p1")
	o, _ := s.CreateOrder(context.Background(), "p1", "waffo", 100_000_000, "u", 0)

	// 两次 MarkPaid · 第二次应幂等，不重复入账
	if _, err := s.MarkPaid(context.Background(), o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkPaid(context.Background(), o.ID); err != nil {
		t.Fatalf("二次 MarkPaid: %v", err)
	}
	bal, _ := w.Get(context.Background(), "p1")
	if bal.Balance != 100_000_000 {
		t.Errorf("重放 MarkPaid 后余额 = %d，应 100000000（不重复入账）", bal.Balance)
	}
}

func TestMarkPaidNotFound(t *testing.T) {
	s, _, _ := setup(t)
	_, err := s.MarkPaid(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestGetChecksOwner(t *testing.T) {
	s, _, d := setup(t)
	seedPassenger(t, d, "p1")
	seedPassenger(t, d, "p2")
	o, _ := s.CreateOrder(context.Background(), "p1", "waffo", 100_000_000, "u", 0)

	_, err := s.Get(context.Background(), "p2", o.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("串号访问应 ErrForbidden, got %v", err)
	}
	got, err := s.Get(context.Background(), "p1", o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != o.ID {
		t.Errorf("id = %q", got.ID)
	}
}

func TestListSelfOnly(t *testing.T) {
	s, _, d := setup(t)
	seedPassenger(t, d, "p1")
	seedPassenger(t, d, "p2")
	if _, err := s.CreateOrder(context.Background(), "p1", "waffo", 100_000_000, "u", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOrder(context.Background(), "p2", "waffo", 200_000_000, "u", 0); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.List(context.Background(), "p1", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total = %d，len = %d", total, len(items))
	}
	if items[0].Credits != 100_000_000 {
		t.Errorf("p1 单只应看到自己那张")
	}
}

func TestExpirePending(t *testing.T) {
	s, _, d := setup(t)
	seedPassenger(t, d, "p1")
	// 过期时间设成过去 · 1 纳秒 TTL
	o, _ := s.CreateOrder(context.Background(), "p1", "waffo", 100_000_000, "u", time.Nanosecond)
	time.Sleep(2 * time.Millisecond)
	n, err := s.ExpirePending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Errorf("ExpirePending = %d，应至少 1", n)
	}
	got, _ := s.Get(context.Background(), "p1", o.ID)
	if got.Status != StatusExpired {
		t.Errorf("过期后 status = %q", got.Status)
	}
}
