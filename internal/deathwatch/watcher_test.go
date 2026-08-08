package deathwatch

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// ── 测试用的内存 Pool mock ──────────────────────────────────

type mockPool struct {
	mu sync.Mutex
	// list ListCredentials 返回什么
	list []housepool.Credential
	// listErr 覆盖 list 返回；非 nil 优先
	listErr error
	// probeErr[credID] = 该号被 TestCredential 时返回的 err（nil = 探活成功）
	probeErr map[housepool.CredentialID]error
	// probed 记录被 TestCredential 调过多少次（回归"活号不被主动探"）
	probed map[housepool.CredentialID]int
}

func newMockPool() *mockPool {
	return &mockPool{
		probeErr: map[housepool.CredentialID]error{},
		probed:   map[housepool.CredentialID]int{},
	}
}

func (m *mockPool) ListCredentials(ctx context.Context, filter housepool.CredentialFilter) ([]housepool.Credential, *housepool.PoolSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, nil, m.listErr
	}
	if !filter.IncludeDisabled {
		// 契合真号池行为 —— 死号是 disabled，deathwatch 必须带这个开关
		out := make([]housepool.Credential, 0, len(m.list))
		for _, c := range m.list {
			if !c.Disabled {
				out = append(out, c)
			}
		}
		return out, nil, nil
	}
	return append([]housepool.Credential(nil), m.list...), nil, nil
}

func (m *mockPool) TestCredential(ctx context.Context, id housepool.CredentialID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.probed[id]++
	return m.probeErr[id]
}

// ── DB fixture ─────────────────────────────────────────────

// setupDB 起真 SQLite（modernc/sqlite 纯 Go）+ 跑 migration；
// 建最少的父行让 credential_ledger 的外键能过。
//
// 返回：db + passenger id + bus id + pull_round id
func setupDB(t *testing.T) (*sql.DB, string, string, string) {
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
	const busID = "b1"
	const roundID = "r1"

	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES (?, 'u1', 'u1@example.com', 'x', '2026-01-01', '2026-01-01')`, pid); err != nil {
		t.Fatalf("建乘客: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
		INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at)
		VALUES (?, 'test bus', 'single', ?, 'active', '2026-01-01')`, busID, pid); err != nil {
		t.Fatalf("建 bus: %v", err)
	}
	if _, err := d.ExecContext(ctx, `
		INSERT INTO pull_round
		  (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
		   key_cost_total, service_fee_total, participants_split_json, status, created_at)
		VALUES (?, 'kiro91', 'co1', ?, 1, 1, 0, 0, '{}', 'completed', '2026-01-01')`,
		roundID, busID); err != nil {
		t.Fatalf("建 pull_round: %v", err)
	}
	return d.DB, pid, busID, roundID
}

// insertCred 塞一行 credential_ledger（属于 bus）· 返回本地 ledger id。
func insertCred(t *testing.T, sqldb *sql.DB, ledgerID string, kiroID uint64, busID, roundID, status string, disabled bool) {
	t.Helper()
	dis := 0
	if disabled {
		dis = 1
	}
	if _, err := sqldb.ExecContext(context.Background(), `
		INSERT INTO credential_ledger
		  (id, kiro_rs_credential_id, owner_bus_id, current_group, vendor_id,
		   source_pull_round_id, status, disabled, pulled_at)
		VALUES (?, ?, ?, ?, 'kiro91', ?, ?, ?, '2026-01-01')`,
		ledgerID, kiroID, busID, "bus-"+busID, roundID, status, dis); err != nil {
		t.Fatalf("插 credential_ledger: %v", err)
	}
}

func rowDeath(t *testing.T, sqldb *sql.DB, ledgerID string) (status, deadAt, deathSource string) {
	t.Helper()
	var dead, src sql.NullString
	if err := sqldb.QueryRow(
		`SELECT status, dead_at, death_source FROM credential_ledger WHERE id = ?`,
		ledgerID).Scan(&status, &dead, &src); err != nil {
		t.Fatalf("查 credential_ledger: %v", err)
	}
	return status, dead.String, src.String
}

func newWatcher(sqldb *sql.DB, pool Pool) *Watcher {
	// 静默 logger —— 测试输出别被 info 淹了
	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Config{DB: sqldb, Pool: pool, Logger: silent})
}

// ── 判死枚举 ──────────────────────────────────────────────

// Suspended → 5 分钟内标 dead + death_source
// （5 分钟是循环间隔；单测直接调 SweepOnce 就能验"一轮内标死"）
func TestSuspendedMarksDead(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 1001, busID, roundID, "alive", true)

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 1001, Disabled: true, DisabledReason: housepool.ReasonSuspended},
	}

	w := newWatcher(sqldb, pool)
	rep := w.SweepOnce(context.Background())

	if rep.MarkedDead != 1 {
		t.Fatalf("MarkedDead = %d, want 1", rep.MarkedDead)
	}
	if rep.Probed != 0 {
		t.Errorf("明确失效枚举不应走 TestCredential，Probed = %d", rep.Probed)
	}
	status, deadAt, src := rowDeath(t, sqldb, "c1")
	if status != "dead" {
		t.Errorf("status = %q, want dead", status)
	}
	if deadAt == "" {
		t.Error("dead_at 未写入")
	}
	if src != "housepool_probe" {
		t.Errorf("death_source = %q, want housepool_probe", src)
	}
	if pool.probed[1001] != 0 {
		t.Errorf("Suspended 不该被主动探活，被调 %d 次", pool.probed[1001])
	}
}

// QuotaExceeded / InvalidRefreshToken 同 Suspended 走判死分支。
func TestOtherDeadReasonsMarkDead(t *testing.T) {
	for _, reason := range []string{housepool.ReasonQuotaExceeded, housepool.ReasonInvalidRefreshToken} {
		t.Run(reason, func(t *testing.T) {
			sqldb, _, busID, roundID := setupDB(t)
			insertCred(t, sqldb, "c1", 1001, busID, roundID, "alive", true)

			pool := newMockPool()
			pool.list = []housepool.Credential{
				{ID: 1001, Disabled: true, DisabledReason: reason},
			}
			w := newWatcher(sqldb, pool)
			rep := w.SweepOnce(context.Background())
			if rep.MarkedDead != 1 {
				t.Fatalf("MarkedDead = %d, want 1", rep.MarkedDead)
			}
			status, _, _ := rowDeath(t, sqldb, "c1")
			if status != "dead" {
				t.Errorf("status = %q, want dead", status)
			}
		})
	}
}

// ── 回归：Manual 号一直不被判死 ─────────────────────────────
//
// record-<pid> 里待派的号、handoff 待确认的号、成员挂起的号 —— 全是我方主动
// disable 写的，reason 是 Manual。deathwatch 误判它们会导致：
//   - 用户 handoff 明文都取到了，号却已被我方标死
//   - 拉号后待派号"上架前"就死了，退款链条乱飞
//
// 这条测试是 Iss #12 DoD 的核心回归。
func TestManualIsNeverMarkedDead(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c-manual", 2001, busID, roundID, "alive", true)

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 2001, Disabled: true, DisabledReason: housepool.ReasonManual},
	}
	// 兜底：就算 deathwatch 出 bug 去探活，也让探活失败 —— 那样测试会证明"探活分支被误走了"
	pool.probeErr[2001] = errors.New("shouldn't be probed")

	w := newWatcher(sqldb, pool)
	rep := w.SweepOnce(context.Background())

	if rep.MarkedDead != 0 {
		t.Fatalf("Manual 号被判死 · MarkedDead = %d，违反 Iss #12 DoD", rep.MarkedDead)
	}
	if rep.Probed != 0 {
		t.Errorf("Manual 不该走探活分支，Probed = %d", rep.Probed)
	}
	if pool.probed[2001] != 0 {
		t.Errorf("Manual 不该调 TestCredential，被调 %d 次", pool.probed[2001])
	}
	if rep.Skipped != 1 {
		t.Errorf("Manual 应记 Skipped，Skipped = %d", rep.Skipped)
	}
	status, deadAt, src := rowDeath(t, sqldb, "c-manual")
	if status != "alive" {
		t.Errorf("status = %q, Manual 号必须保持 alive", status)
	}
	if deadAt != "" || src != "" {
		t.Errorf("dead_at/death_source 不该写入，得到 %q / %q", deadAt, src)
	}
}

// ── TestCredential 复核路径 ─────────────────────────────

// TooManyFailures / AutoThrottled 等 → 走 TestCredential 复核。
// 复核成功 → 不判死（号池自愈中）。
func TestProbeSuccessKeepsAlive(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 3001, busID, roundID, "alive", true)

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 3001, Disabled: true, DisabledReason: housepool.ReasonTooManyFailures},
	}
	// probeErr[3001] 未设 → 探活返回 nil → 活着

	w := newWatcher(sqldb, pool)
	rep := w.SweepOnce(context.Background())

	if rep.Probed != 1 {
		t.Fatalf("Probed = %d, want 1（应走复核分支）", rep.Probed)
	}
	if rep.MarkedDead != 0 {
		t.Errorf("复核成功不应判死，MarkedDead = %d", rep.MarkedDead)
	}
	status, _, _ := rowDeath(t, sqldb, "c1")
	if status != "alive" {
		t.Errorf("status = %q, want alive", status)
	}
}

// TestCredential 返 error → 判死。
func TestProbeFailureMarksDead(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 3002, busID, roundID, "alive", true)

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 3002, Disabled: true, DisabledReason: housepool.ReasonAutoThrottled},
	}
	pool.probeErr[3002] = errors.New("upstream 401")

	w := newWatcher(sqldb, pool)
	rep := w.SweepOnce(context.Background())

	if rep.Probed != 1 {
		t.Fatalf("Probed = %d, want 1", rep.Probed)
	}
	if rep.MarkedDead != 1 {
		t.Fatalf("MarkedDead = %d, want 1（探活失败必须判死）", rep.MarkedDead)
	}
	status, _, src := rowDeath(t, sqldb, "c1")
	if status != "dead" {
		t.Errorf("status = %q, want dead", status)
	}
	if src != "housepool_probe" {
		t.Errorf("death_source = %q, want housepool_probe", src)
	}
}

// disabled=false 的号 → 本轮不做处理（不主动探活活号 · 打爆号池的最快路径）。
func TestEnabledCredentialsSkipped(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 4001, busID, roundID, "alive", false)

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 4001, Disabled: false},
	}
	w := newWatcher(sqldb, pool)
	rep := w.SweepOnce(context.Background())

	if rep.Probed != 0 {
		t.Errorf("活号不该被主动探，Probed = %d", rep.Probed)
	}
	if rep.MarkedDead != 0 {
		t.Errorf("活号不该判死，MarkedDead = %d", rep.MarkedDead)
	}
}

// ── 幂等 / 边界 ────────────────────────────────────────────

// 已经 dead 的号再次被扫到 → AlreadyDead，不重复写 dead_at。
func TestAlreadyDeadIsIdempotent(t *testing.T) {
	sqldb, _, busID, roundID := setupDB(t)
	// 先手工建成 dead
	insertCred(t, sqldb, "c1", 5001, busID, roundID, "dead", true)
	// 手工写 dead_at 让我们能断言"没被改"
	if _, err := sqldb.Exec(`UPDATE credential_ledger SET dead_at='2026-01-02T00:00:00.000Z', death_source='housepool_probe' WHERE id=?`, "c1"); err != nil {
		t.Fatal(err)
	}

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 5001, Disabled: true, DisabledReason: housepool.ReasonSuspended},
	}
	w := newWatcher(sqldb, pool)
	rep := w.SweepOnce(context.Background())

	if rep.AlreadyDead != 1 {
		t.Errorf("AlreadyDead = %d, want 1", rep.AlreadyDead)
	}
	if rep.MarkedDead != 0 {
		t.Errorf("MarkedDead = %d, want 0（终态不应再改）", rep.MarkedDead)
	}
	_, deadAt, _ := rowDeath(t, sqldb, "c1")
	if deadAt != "2026-01-02T00:00:00.000Z" {
		t.Errorf("dead_at 被覆盖了：%q", deadAt)
	}
}

// 号池有但账里没 → UnknownCredential + 记 warn，不 fail 整轮。
func TestUnknownCredentialLogged(t *testing.T) {
	sqldb, _, _, _ := setupDB(t)

	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 9999, Disabled: true, DisabledReason: housepool.ReasonSuspended},
	}
	w := newWatcher(sqldb, pool)
	rep := w.SweepOnce(context.Background())

	if rep.UnknownCredential != 1 {
		t.Errorf("UnknownCredential = %d, want 1", rep.UnknownCredential)
	}
	if rep.MarkedDead != 0 {
		t.Errorf("找不到账不该记 MarkedDead")
	}
}

// ListCredentials 失败 → 整轮返回，不 panic。
func TestListErrorSurfaces(t *testing.T) {
	sqldb, _, _, _ := setupDB(t)

	pool := newMockPool()
	pool.listErr = errors.New("housepool 500")
	w := newWatcher(sqldb, pool)
	rep := w.SweepOnce(context.Background())

	if rep.Errors != 1 {
		t.Errorf("Errors = %d, want 1", rep.Errors)
	}
	if rep.Scanned != 0 {
		t.Errorf("Scanned = %d, list 失败不该算扫过", rep.Scanned)
	}
}

// Run: ctx 取消后循环立即退出。
func TestRunExitsOnContextCancel(t *testing.T) {
	sqldb, _, _, _ := setupDB(t)
	pool := newMockPool()
	w := New(Config{DB: sqldb, Pool: pool, Interval: time.Hour, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Run 里第一轮同步跑 SweepOnce · 然后进 ticker · cancel 触发退出
	cancel()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("Run 未在 ctx 取消后 2s 内退出")
	}
}
