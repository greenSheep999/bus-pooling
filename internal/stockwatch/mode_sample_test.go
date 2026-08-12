package stockwatch

import (
	"context"
	"log/slog"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

// ModeMgr.sample 必须查 pending_purchase（真实拉号链路）· 不是 pull_intent。
//
// 这个测试是**回归哨兵**：初版查了 pull_intent · 而生产代码从没写过那张表
// （实际走 pending_purchase 状态机 · coalescer 的 Anon/Team 还是 stub）·
// 结果 demand 恒 0 · mode 永远锁 cool · 抢号链一次都不 fire。
func TestModeMgr_Sample_ReadsPendingPurchase(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)
	ctx := context.Background()

	m := NewModeMgr(database.DB)

	// 没需求没库存 → cool
	m.sample(ctx)
	if got := m.Current(); got != ModeCool {
		t.Fatalf("零需求应 cool · 得 %s", got)
	}

	// 塞 3 条活跃 pending_purchase（有需求 · 无库存样本）
	seedIdempotency(t, database, "idem-a")
	seedIdempotency(t, database, "idem-b")
	seedIdempotency(t, database, "idem-c")
	for i, st := range []string{"initial", "reserved", "purchasing"} {
		seedPendingPurchase(t, database, string(rune('a'+i)), st)
	}

	m.sample(ctx)
	// demand=3 · supply<1 补 1 → ratio=3 > 2 → tight
	if got := m.Current(); got != ModeTight {
		t.Fatalf("3 条活跃挂单 + 零库存应 tight · 得 %s", got)
	}

	// 把它们推进到已完成状态 → 不再算需求
	_, err := database.ExecContext(ctx,
		`UPDATE pending_purchase SET status = 'completed'`)
	if err != nil {
		t.Fatal(err)
	}
	m.sample(ctx)
	if got := m.Current(); got != ModeCool {
		t.Fatalf("全部 completed 后应回 cool · 得 %s", got)
	}
}

// stock_watcher 里 watching 的也算需求 —— 它们已退出主流程 · 不在 pending_purchase 里
func TestModeMgr_Sample_CountsWatchingAsDemand(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)
	ctx := context.Background()

	w := New(Config{DB: database.DB, Firer: &mockFirer{}, Logger: slog.Default()})
	m := NewModeMgr(database.DB)

	m.sample(ctx)
	if got := m.Current(); got != ModeCool {
		t.Fatalf("起始应 cool · 得 %s", got)
	}

	// 挂 3 条 watching · pending_purchase 里一条都没有
	enqueueOne(t, w, "kiroceo", "o1")
	enqueueOne(t, w, "kiroceo", "o2")
	enqueueOne(t, w, "kiroceo", "o3")

	m.sample(ctx)
	if got := m.Current(); got != ModeTight {
		t.Fatalf("3 条 watching 应算需求 → tight · 得 %s", got)
	}
}

// 有充足库存样本时 · 同样的需求量应落到更松的档
func TestModeMgr_Sample_SupplyLowersMode(t *testing.T) {
	database := openTestDB(t)
	seedPassenger(t, database)
	ctx := context.Background()

	w := New(Config{DB: database.DB, Firer: &mockFirer{}, Logger: slog.Default()})
	m := NewModeMgr(database.DB)

	// 1 条需求
	enqueueOne(t, w, "kiroceo", "o1")

	// 塞库存样本：一家 vendor 均值 10 个 · demand=1 → ratio=0.1 → cool
	// 每条错开 10 秒（避开 (vendor_id, probed_at) 主键冲突）· 都在 5min 窗内
	for i := 0; i < 3; i++ {
		seedProbe(t, database, "kiroceo", 10, i*10)
	}
	m.sample(ctx)
	if got := m.Current(); got != ModeCool {
		t.Fatalf("1 需求 vs 10 库存应 cool · 得 %s", got)
	}
}

// ── seed 工具 ──

// seedIdempotency · pending_purchase 有 idempotency_record FK
func seedIdempotency(t *testing.T, database *db.DB, id string) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO idempotency_record
		   (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		 VALUES (?, 'p1', 'POST', '/api/me/pull', ?, ?, '2026-01-01')`,
		id, "key-"+id, "fp-"+id); err != nil {
		t.Fatalf("seed idempotency: %v", err)
	}
}

// seedPendingPurchase · 建一条指定状态的挂单 · suffix 保证 client_order_id 唯一
func seedPendingPurchase(t *testing.T, database *db.DB, suffix, status string) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO pending_purchase
		   (id, idempotency_record_id, passenger_id, target_group, vendor_id,
		    client_order_id, count_requested, reserved_amount, status,
		    created_at, updated_at)
		 VALUES (?, ?, 'p1', 'record-p1', 'kiroceo', ?, 1, 20000000, ?,
		         '2026-01-01', '2026-01-01')`,
		"pp-"+suffix, "idem-"+suffix, "co-"+suffix, status); err != nil {
		t.Fatalf("seed pending_purchase(%s): %v", status, err)
	}
}

// seedProbe · 建一条探针样本 · 让 supply 有值
//
// probed_at 是 (vendor_id, probed_at) 主键的一半 · 同毫秒插两条会撞
// （`strftime('%f')` 只到毫秒）· 用 secondsAgo 错开 · 同时保证落在 5min 窗口内。
func seedProbe(t *testing.T, database *db.DB, vendorID string, stock, secondsAgo int) {
	t.Helper()
	if _, err := database.ExecContext(context.Background(),
		`INSERT INTO vendor_probe (vendor_id, probed_at, alive, stock_total)
		 VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now', ?), 1, ?)`,
		vendorID, "-"+itoa(secondsAgo)+" seconds", stock); err != nil {
		t.Fatalf("seed probe: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
