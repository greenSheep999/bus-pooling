package wallet

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

const micro = 1_000_000

func setup(t *testing.T) (*Store, string) {
	t.Helper()
	ctx := context.Background()

	d := db.NewTestDB(t)

	const pid = "p1"
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES (?, 'u1', 'u1@example.com', 'x', '2026-01-01', '2026-01-01')`, pid); err != nil {
		t.Fatalf("建乘客: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
		INSERT INTO wallet (passenger_id, balance, reserved, updated_at)
		VALUES (?, 0, 0, '2026-01-01')`, pid); err != nil {
		t.Fatalf("建钱包: %v", err)
	}

	return NewStore(d.DB), pid
}

func TestCreditThenDebit(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	e, err := s.Credit(ctx, Move{PassengerID: pid, Reason: ReasonRecharge, Amount: 100 * micro, Memo: "充值"})
	if err != nil {
		t.Fatalf("Credit: %v", err)
	}
	if e.Amount != 100*micro {
		t.Errorf("入账金额 = %d", e.Amount)
	}
	if e.BalanceAfter != 100*micro {
		t.Errorf("入账后余额 = %d", e.BalanceAfter)
	}
	if e.Seq != 1 {
		t.Errorf("首条流水 seq = %d, want 1", e.Seq)
	}

	e2, err := s.Debit(ctx, Move{PassengerID: pid, Reason: ReasonKeyCost, Amount: 30 * micro})
	if err != nil {
		t.Fatalf("Debit: %v", err)
	}
	// 出账金额在流水里是**负数**
	if e2.Amount != -30*micro {
		t.Errorf("出账金额 = %d, want %d", e2.Amount, -30*micro)
	}
	if e2.BalanceAfter != 70*micro {
		t.Errorf("出账后余额 = %d", e2.BalanceAfter)
	}
	if e2.Seq != 2 {
		t.Errorf("第二条流水 seq = %d, want 2", e2.Seq)
	}

	b, err := s.Get(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if b.Balance != 70*micro {
		t.Errorf("余额 = %d", b.Balance)
	}
}

func TestDebitRejectsOverdraft(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Credit(ctx, Move{PassengerID: pid, Reason: ReasonRecharge, Amount: 10 * micro}); err != nil {
		t.Fatal(err)
	}

	_, err := s.Debit(ctx, Move{PassengerID: pid, Reason: ReasonKeyCost, Amount: 11 * micro})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("超额扣款应报 ErrInsufficientBalance，得到 %v", err)
	}

	// 失败的扣款不能留下痕迹：余额不变、流水不多
	b, _ := s.Get(ctx, pid)
	if b.Balance != 10*micro {
		t.Fatalf("失败扣款后余额被改了: %d", b.Balance)
	}
	entries, total, err := s.List(ctx, pid, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(entries) != 1 {
		t.Fatalf("失败扣款留下了流水: total=%d", total)
	}
}

// Iss #5 DoD：并发 Debit 保证不超扣
func TestConcurrentDebitNeverOverdraws(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	// 余额只够 10 次
	const each = 10 * micro
	const funded = 100 * micro
	if _, err := s.Credit(ctx, Move{PassengerID: pid, Reason: ReasonRecharge, Amount: funded}); err != nil {
		t.Fatal(err)
	}

	// 50 个并发各扣 10 —— 只有 10 个该成功
	const goroutines = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	var okCount, insufficientCount int
	var otherErrs []error

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := s.Debit(ctx, Move{PassengerID: pid, Reason: ReasonKeyCost, Amount: each})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				okCount++
			case errors.Is(err, ErrInsufficientBalance):
				insufficientCount++
			default:
				otherErrs = append(otherErrs, err)
			}
		}()
	}
	wg.Wait()

	if len(otherErrs) > 0 {
		t.Fatalf("出现意外错误（%d 个），第一个: %v", len(otherErrs), otherErrs[0])
	}
	if want := int(funded / each); okCount != want {
		t.Errorf("成功 %d 次，应恰好 %d 次", okCount, want)
	}
	if okCount+insufficientCount != goroutines {
		t.Errorf("成功 %d + 余额不足 %d != 总数 %d", okCount, insufficientCount, goroutines)
	}

	b, err := s.Get(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if b.Balance != 0 {
		t.Errorf("最终余额 = %d，应为 0（不能超扣成负数，也不该少扣）", b.Balance)
	}
	if b.Balance < 0 {
		t.Fatal("余额被扣成负数 —— 超扣了")
	}
}

// Iss #5 DoD：ledger seq 严格递增（并发下也不能重复或跳号）
func TestConcurrentLedgerSeqIsStrictlyIncreasing(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Credit(ctx, Move{PassengerID: pid, Reason: ReasonRecharge, Amount: 1000 * micro}); err != nil {
		t.Fatal(err)
	}

	const n = 40
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = s.Debit(ctx, Move{PassengerID: pid, Reason: ReasonServiceFee, Amount: 1 * micro})
		}()
	}
	wg.Wait()

	entries, total, err := s.List(ctx, pid, ListOptions{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if total != n+1 { // n 次扣款 + 1 次充值
		t.Fatalf("流水 %d 条，应为 %d", total, n+1)
	}

	// List 是按 seq DESC 返回的
	seen := map[int64]bool{}
	prev := int64(-1)
	for _, e := range entries {
		if seen[e.Seq] {
			t.Fatalf("seq %d 重复", e.Seq)
		}
		seen[e.Seq] = true
		if prev != -1 && e.Seq >= prev {
			t.Fatalf("seq 不是严格递减（DESC 序）: %d 出现在 %d 之后", e.Seq, prev)
		}
		prev = e.Seq
	}
	// 应该是连续的 1..n+1，不跳号
	for i := int64(1); i <= int64(n+1); i++ {
		if !seen[i] {
			t.Errorf("seq %d 缺失（跳号了）", i)
		}
	}
}

func TestReserveCommitRelease(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Credit(ctx, Move{PassengerID: pid, Reason: ReasonRecharge, Amount: 100 * micro}); err != nil {
		t.Fatal(err)
	}

	// 冻结 50：balance 减、reserved 增、**不记流水**（钱还没花掉）
	if err := s.Reserve(ctx, pid, 50*micro); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	b, _ := s.Get(ctx, pid)
	if b.Balance != 50*micro || b.Reserved != 50*micro {
		t.Fatalf("冻结后 balance=%d reserved=%d", b.Balance, b.Reserved)
	}
	_, total, _ := s.List(ctx, pid, ListOptions{})
	if total != 1 {
		t.Fatalf("Reserve 不该记流水，现在有 %d 条", total)
	}

	// 实际成交 30 → commit 30
	if _, err := s.CommitReserved(ctx, Move{
		PassengerID: pid, Reason: ReasonKeyCost, Amount: 30 * micro,
	}); err != nil {
		t.Fatalf("CommitReserved: %v", err)
	}
	// 差额 20 退回
	if err := s.ReleaseReserved(ctx, pid, 20*micro); err != nil {
		t.Fatalf("ReleaseReserved: %v", err)
	}

	b, _ = s.Get(ctx, pid)
	if b.Reserved != 0 {
		t.Errorf("结算后 reserved = %d，应为 0", b.Reserved)
	}
	// 100 - 30 = 70
	if b.Balance != 70*micro {
		t.Errorf("结算后 balance = %d，应为 %d", b.Balance, 70*micro)
	}
}

func TestReserveRejectsInsufficient(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Credit(ctx, Move{PassengerID: pid, Reason: ReasonRecharge, Amount: 10 * micro}); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, pid, 11*micro); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("冻结超额应报 ErrInsufficientBalance，得到 %v", err)
	}
}

func TestCommitMoreThanReservedFails(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Credit(ctx, Move{PassengerID: pid, Reason: ReasonRecharge, Amount: 100 * micro}); err != nil {
		t.Fatal(err)
	}
	if err := s.Reserve(ctx, pid, 10*micro); err != nil {
		t.Fatal(err)
	}
	_, err := s.CommitReserved(ctx, Move{PassengerID: pid, Reason: ReasonKeyCost, Amount: 20 * micro})
	if !errors.Is(err, ErrInsufficientReserved) {
		t.Fatalf("结算超过冻结额应报 ErrInsufficientReserved，得到 %v", err)
	}
}

// 并发冻结也不能把余额冻成负数
func TestConcurrentReserveNeverOverdraws(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	const each = 10 * micro
	const funded = 50 * micro
	if _, err := s.Credit(ctx, Move{PassengerID: pid, Reason: ReasonRecharge, Amount: funded}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Reserve(ctx, pid, each); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if want := int(funded / each); ok != want {
		t.Errorf("冻结成功 %d 次，应为 %d", ok, want)
	}
	b, _ := s.Get(ctx, pid)
	if b.Balance != 0 || b.Reserved != funded {
		t.Errorf("balance=%d reserved=%d，应为 0 / %d", b.Balance, b.Reserved, funded)
	}
}

func TestRejectsNonPositiveAmount(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	for _, amt := range []int64{0, -1} {
		if _, err := s.Credit(ctx, Move{PassengerID: pid, Reason: ReasonRecharge, Amount: amt}); !errors.Is(err, ErrNonPositiveAmount) {
			t.Errorf("Credit(%d) 应报 ErrNonPositiveAmount，得到 %v", amt, err)
		}
		if _, err := s.Debit(ctx, Move{PassengerID: pid, Reason: ReasonKeyCost, Amount: amt}); !errors.Is(err, ErrNonPositiveAmount) {
			t.Errorf("Debit(%d) 应报 ErrNonPositiveAmount，得到 %v", amt, err)
		}
	}
}

func TestListFilterAndPaging(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	if _, err := s.Credit(ctx, Move{PassengerID: pid, Reason: ReasonRecharge, Amount: 100 * micro}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Debit(ctx, Move{PassengerID: pid, Reason: ReasonServiceFee, Amount: 1 * micro}); err != nil {
			t.Fatal(err)
		}
	}

	// 按 reason 筛
	entries, total, err := s.List(ctx, pid, ListOptions{Reason: ReasonServiceFee})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(entries) != 3 {
		t.Fatalf("筛 service_fee 得到 %d 条（total %d），应为 3", len(entries), total)
	}

	// 分页
	page1, total, err := s.List(ctx, pid, ListOptions{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("总数 = %d，应为 4", total)
	}
	if len(page1) != 2 {
		t.Fatalf("第一页 %d 条，应为 2", len(page1))
	}
	page2, _, err := s.List(ctx, pid, ListOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 {
		t.Fatalf("第二页 %d 条，应为 2", len(page2))
	}
	if page1[0].Seq == page2[0].Seq {
		t.Fatal("两页返回了同一条")
	}
}

func TestUnknownWallet(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()

	if _, err := s.Get(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("查不存在的钱包应报 ErrNotFound，得到 %v", err)
	}
	if _, err := s.Debit(ctx, Move{PassengerID: "nobody", Reason: ReasonKeyCost, Amount: micro}); !errors.Is(err, ErrNotFound) {
		t.Errorf("扣不存在的钱包应报 ErrNotFound，得到 %v", err)
	}
}

func TestTodayUsage(t *testing.T) {
	s, pid := setup(t)
	ctx := context.Background()

	// 没记录时返回 0 而不是报错
	u, err := s.TodayUsage(ctx, pid)
	if err != nil {
		t.Fatalf("TodayUsage: %v", err)
	}
	if u.Rounds != 0 || u.Spend != 0 {
		t.Fatalf("初始用量应为 0，得到 %+v", u)
	}

	// 累加两次，验证 upsert
	err = s.inTx(ctx, func(tx *sql.Tx) error { return BumpDailyTx(ctx, tx, pid, 1, 20*micro) })
	if err != nil {
		t.Fatalf("BumpDailyTx: %v", err)
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error { return BumpDailyTx(ctx, tx, pid, 2, 5*micro) })
	if err != nil {
		t.Fatalf("BumpDailyTx 第二次: %v", err)
	}

	u, err = s.TodayUsage(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if u.Rounds != 3 {
		t.Errorf("轮数 = %d，应为 3", u.Rounds)
	}
	if u.Spend != 25*micro {
		t.Errorf("消费 = %d，应为 %d", u.Spend, 25*micro)
	}
}
