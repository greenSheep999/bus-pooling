package decider

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func setupDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	ctx := context.Background()

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}

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
	// pending_purchase.idempotency_record_id 有外键
	if _, err := d.ExecContext(ctx, `
		INSERT INTO idempotency_record
		  (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		VALUES ('idem1', ?, 'POST', '/api/me/pull', 'k1', 'fp1', '2026-01-01')`, pid); err != nil {
		t.Fatalf("建幂等记录: %v", err)
	}
	return d.DB, pid
}

func newPending(pid string) Pending {
	return Pending{
		IdempotencyRecordID: "idem1",
		PassengerID:         pid,
		TargetGroup:         "record-" + pid,
		VendorID:            "kiro91",
		ClientOrderID:       "a1b2c3d4e5f60718293a4b5c6d7e8f90",
		CountRequested:      2,
		ReservedAmount:      50 * micro,
	}
}

// 完整推进一遍 5 个状态（Iss #9 DoD 第 1 条）。
func TestFullHappyPathTransitions(t *testing.T) {
	sqldb, pid := setupDB(t)
	s := NewStore(sqldb)
	ctx := context.Background()

	id, err := s.Create(ctx, newPending(pid))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusInitial {
		t.Fatalf("初始状态 = %q，want %q", got.Status, StatusInitial)
	}

	steps := []struct{ from, to Status }{
		{StatusInitial, StatusReserved},
		{StatusReserved, StatusPurchasing},
		{StatusPurchasing, StatusPurchased},
		{StatusPurchased, StatusImported},
		{StatusImported, StatusCompleted},
	}
	for _, st := range steps {
		if err := s.Advance(ctx, id, st.from, st.to); err != nil {
			t.Fatalf("推进 %s→%s: %v", st.from, st.to, err)
		}
		cur, _ := s.Get(ctx, id)
		if cur.Status != st.to {
			t.Fatalf("推进后状态 = %q，want %q", cur.Status, st.to)
		}
	}
}

// 条件 UPDATE 是并发保护的核心：状态不匹配时必须拒绝推进。
//
// 少了这个保护，janitor 和请求线程会同时往下走 —— 同一笔单扣两次款、导两次号。
func TestAdvanceRejectsStaleTransition(t *testing.T) {
	sqldb, pid := setupDB(t)
	s := NewStore(sqldb)
	ctx := context.Background()

	id, _ := s.Create(ctx, newPending(pid))
	if err := s.Advance(ctx, id, StatusInitial, StatusReserved); err != nil {
		t.Fatalf("首次推进: %v", err)
	}

	// 再用 initial 作为 from 推进 —— 应被拒
	err := s.Advance(ctx, id, StatusInitial, StatusReserved)
	if !errors.Is(err, ErrStaleTransition) {
		t.Fatalf("重复推进应返回 ErrStaleTransition，得到 %v", err)
	}
	// 错误信息要能看出实际状态，否则线上排查只能猜
	if err != nil && !contains(err.Error(), "reserved") {
		t.Errorf("错误信息该带上实际状态: %v", err)
	}
}

// 并发推进只能有一个赢（Iss #9 DoD 第 4 条的状态机那半边）。
func TestConcurrentAdvanceOnlyOneWins(t *testing.T) {
	sqldb, pid := setupDB(t)
	s := NewStore(sqldb)
	ctx := context.Background()

	id, _ := s.Create(ctx, newPending(pid))

	const n = 8
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = s.Advance(ctx, id, StatusInitial, StatusReserved)
		}(i)
	}
	wg.Wait()

	won := 0
	for _, err := range results {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrStaleTransition):
			// 正常 —— 别人赢了
		default:
			t.Errorf("意外错误: %v", err)
		}
	}
	if won != 1 {
		t.Errorf("有 %d 个 goroutine 推进成功，必须恰好 1 个", won)
	}
}

// AdvanceWith 的字段必须跟状态推进在同一条 UPDATE 里。
// 分两条写的话，崩在中间会留下「状态是 purchased 但没有 vendor_order_id」的行，
// 恢复时不知道补拉哪个订单。
func TestAdvanceWithSetsFieldsAtomically(t *testing.T) {
	sqldb, pid := setupDB(t)
	s := NewStore(sqldb)
	ctx := context.Background()

	id, _ := s.Create(ctx, newPending(pid))
	_ = s.Advance(ctx, id, StatusInitial, StatusReserved)
	_ = s.Advance(ctx, id, StatusReserved, StatusPurchasing)

	if err := s.AdvanceWith(ctx, id, StatusPurchasing, StatusPurchased, Fields{
		VendorOrderID: "ord-777",
	}); err != nil {
		t.Fatalf("AdvanceWith: %v", err)
	}

	got, _ := s.Get(ctx, id)
	if got.Status != StatusPurchased {
		t.Errorf("状态 = %q", got.Status)
	}
	if got.VendorOrderID != "ord-777" {
		t.Errorf("VendorOrderID = %q，应跟状态一起写入", got.VendorOrderID)
	}

	// 状态不匹配时字段也不该被写
	if err := s.AdvanceWith(ctx, id, StatusPurchasing, StatusPurchased, Fields{
		VendorOrderID: "ord-should-not-stick",
	}); !errors.Is(err, ErrStaleTransition) {
		t.Fatalf("应拒绝，得到 %v", err)
	}
	got, _ = s.Get(ctx, id)
	if got.VendorOrderID != "ord-777" {
		t.Errorf("推进被拒时字段不该被改，得到 %q", got.VendorOrderID)
	}
}

func TestGetNotFound(t *testing.T) {
	sqldb, _ := setupDB(t)
	s := NewStore(sqldb)
	if _, err := s.Get(context.Background(), "nope"); !errors.Is(err, ErrPendingNotFound) {
		t.Errorf("应返回 ErrPendingNotFound，得到 %v", err)
	}
}

func TestFindByClientOrderID(t *testing.T) {
	sqldb, pid := setupDB(t)
	s := NewStore(sqldb)
	ctx := context.Background()

	id, _ := s.Create(ctx, newPending(pid))
	got, err := s.FindByClientOrderID(ctx, "kiro91", "a1b2c3d4e5f60718293a4b5c6d7e8f90")
	if err != nil {
		t.Fatalf("FindByClientOrderID: %v", err)
	}
	if got.ID != id {
		t.Errorf("找到的是 %q，want %q", got.ID, id)
	}
}

// FindStale 按 updated_at 扫超时单。
//
// **时间格式必须定宽** —— RFC3339Nano 会省掉尾随零，导致
// `"…00:00Z"` 跟 `"…00:00.123Z"` 的字符串比较结果反过来（'Z' > '.'），
// 于是该恢复的单子被静默漏掉。这个测试专门覆盖整秒边界。
func TestFindStaleUsesWidthStableTimeFormat(t *testing.T) {
	sqldb, pid := setupDB(t)
	s := NewStore(sqldb)
	ctx := context.Background()

	id, _ := s.Create(ctx, newPending(pid))
	_ = s.Advance(ctx, id, StatusInitial, StatusReserved)

	// 关键是让 **row 的时间戳落在整秒、而 cutoff 落在同一秒的中间** ——
	// 只有这时变宽格式的字符串比较才会出错（比到 'Z' vs '.' 那一位）。
	// 隔了好几秒的话前面的位就分出大小了，照不出问题（我第一版就是这么写的，没抓到）。
	rowTime := time.Now().UTC().Add(-10 * time.Second).Truncate(time.Second)
	if _, err := sqldb.ExecContext(ctx,
		`UPDATE pending_purchase SET updated_at = ? WHERE id = ?`,
		formatTime(rowTime), id); err != nil {
		t.Fatal(err)
	}
	// 让 FindStale 算出的 cutoff ≈ rowTime + 500ms（同一秒内，带小数）
	olderThan := time.Since(rowTime.Add(500 * time.Millisecond))

	stale, err := s.FindStale(ctx, StatusReserved, olderThan, 10)
	if err != nil {
		t.Fatalf("FindStale: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("找到 %d 条超时单，want 1 —— 整秒时间戳被漏掉，说明时间格式是变宽的"+
			"（RFC3339Nano 省掉尾随零，'Z' > '.' 让比较结果反过来）", len(stale))
	}
	if stale[0].ID != id {
		t.Errorf("找到的是 %q", stale[0].ID)
	}
}

// 没超时的不该被扫出来 —— 否则 janitor 会去动正在处理中的单子。
func TestFindStaleSkipsFresh(t *testing.T) {
	sqldb, pid := setupDB(t)
	s := NewStore(sqldb)
	ctx := context.Background()

	id, _ := s.Create(ctx, newPending(pid))
	_ = s.Advance(ctx, id, StatusInitial, StatusReserved)

	stale, err := s.FindStale(ctx, StatusReserved, time.Hour, 10)
	if err != nil {
		t.Fatalf("FindStale: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("刚推进的单子不该算超时，得到 %d 条", len(stale))
	}
}

// FindStale 只返回指定状态的 —— 各状态的超时阈值不同（§2 关键约定 5）。
func TestFindStaleFiltersByStatus(t *testing.T) {
	sqldb, pid := setupDB(t)
	s := NewStore(sqldb)
	ctx := context.Background()

	mk := func(coid string, to Status) string {
		p := newPending(pid)
		p.ClientOrderID = coid
		id, err := s.Create(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		_ = s.Advance(ctx, id, StatusInitial, StatusReserved)
		if to != StatusReserved {
			_ = s.Advance(ctx, id, StatusReserved, to)
		}
		old := time.Now().UTC().Add(-time.Hour)
		if _, err := sqldb.ExecContext(ctx,
			`UPDATE pending_purchase SET updated_at = ? WHERE id = ?`, formatTime(old), id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	reservedID := mk("11111111111111111111111111111111", StatusReserved)
	mk("22222222222222222222222222222222", StatusPurchasing)

	stale, err := s.FindStale(ctx, StatusReserved, time.Minute, 10)
	if err != nil {
		t.Fatalf("FindStale: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != reservedID {
		t.Errorf("只该返回 reserved 的那条，得到 %d 条", len(stale))
	}
}

// 单独拉号时 bus_id 为 NULL，读出来应是空串而不是报错。
func TestNullBusIDReadsAsEmpty(t *testing.T) {
	sqldb, pid := setupDB(t)
	s := NewStore(sqldb)
	ctx := context.Background()

	id, _ := s.Create(ctx, newPending(pid)) // BusID 未设
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BusID != "" {
		t.Errorf("BusID = %q，单独拉号应为空", got.BusID)
	}
	if got.TargetGroup != "record-"+pid {
		t.Errorf("TargetGroup = %q", got.TargetGroup)
	}
}

// 同一个 (vendor, client_order_id) 不能建两笔 —— 那是 vendor 侧的幂等锚，
// 重复会让对账时分不清是哪一笔。
func TestDuplicateClientOrderIDRejected(t *testing.T) {
	sqldb, pid := setupDB(t)
	s := NewStore(sqldb)
	ctx := context.Background()

	if _, err := s.Create(ctx, newPending(pid)); err != nil {
		t.Fatalf("首次 Create: %v", err)
	}
	if _, err := s.Create(ctx, newPending(pid)); err == nil {
		t.Error("同 (vendor, client_order_id) 重复建应报错（UNIQUE 约束）")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
