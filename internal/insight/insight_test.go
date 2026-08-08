package insight_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/insight"

	_ "modernc.org/sqlite"
)

// setup 建库 + 跑迁移 + 建乘客 + 建单人车 —— 后续每个 case 拿这个环境写测试。
type env struct {
	t   *testing.T
	db  *sql.DB
	pid string
	bus string
	st  *insight.Store
	now time.Time
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "insight.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}

	pid := "p_" + uuid7()
	bid := "b_" + uuid7()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

	// 造账户 + 钱包 + 单人车 —— 最小可用集
	tx, err := d.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	iso := now.UTC().Format("2006-01-02T15:04:05.000Z")
	_, _ = tx.Exec(`INSERT INTO passenger
	 (id, username, email, password_hash, role, status, invited, created_at, updated_at)
	 VALUES (?, ?, ?, 'x', 'user', 'active', 0, ?, ?)`,
		pid, "u"+pid[:6], pid+"@e", iso, iso)
	_, _ = tx.Exec(`INSERT INTO wallet (passenger_id, balance, reserved, updated_at)
	 VALUES (?, ?, ?, ?)`, pid, 500_000_000, 0, iso)
	_, _ = tx.Exec(`INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at)
	 VALUES (?, '测试车', 'single', ?, 'active', ?)`, bid, pid, iso)
	_, _ = tx.Exec(`INSERT INTO bus_member (bus_id, passenger_id, role, joined_at, share_pct, status)
	 VALUES (?, ?, 'owner', ?, 100, 'active')`, bid, pid, iso)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	st := insight.NewStore(d.DB).WithClock(func() time.Time { return now })
	return &env{t: t, db: d.DB, pid: pid, bus: bid, st: st, now: now}
}

// insertLedger 塞一条 wallet_ledger（不做真实扣款，只造聚合数据源）
func (e *env) insertLedger(reason string, amount int64, at time.Time, memo string) {
	e.t.Helper()
	id := "l_" + uuid7()
	iso := at.UTC().Format("2006-01-02T15:04:05.000Z")
	var seq int64
	_ = e.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM wallet_ledger WHERE passenger_id = ?`, e.pid).Scan(&seq)
	if _, err := e.db.Exec(`INSERT INTO wallet_ledger
	 (id, passenger_id, seq, reason, amount, balance_after, memo, created_at)
	 VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		id, e.pid, seq, reason, amount, memo, iso); err != nil {
		e.t.Fatalf("造 ledger: %v", err)
	}
}

// insertPullRound bus_id 空 = 单独拉号；participants_split_json 挂当前乘客
func (e *env) insertPullRound(vendor, busID string, purchased int, spend int64, at time.Time) string {
	e.t.Helper()
	id := "r_" + uuid7()
	iso := at.UTC().Format("2006-01-02T15:04:05.000Z")
	split := `{"` + e.pid + `":` + itoaSafe(purchased) + `}`
	var busField any
	if busID != "" {
		busField = busID
	}
	_, err := e.db.Exec(`INSERT INTO pull_round
	 (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
	  key_cost_total, vendor_fee_total, region_fee_total,
	  single_pull_fee_total, capability_fee_total, service_fee_total,
	  participants_split_json, status, created_at, completed_at)
	 VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0, 0, 0, 0, ?, 'completed', ?, ?)`,
		id, vendor, "co_"+id, busField, purchased, purchased, spend, split, iso, iso)
	if err != nil {
		e.t.Fatalf("造 pull_round: %v", err)
	}
	return id
}

// insertCredential 造一条 credential_ledger
func (e *env) insertCredential(vendor, group, status string, pulledAt time.Time, deadAt *time.Time, pushedAt *time.Time, ownerBus, ownerRecord string, roundID string) string {
	e.t.Helper()
	id := "c_" + uuid7()
	pAt := pulledAt.UTC().Format("2006-01-02T15:04:05.000Z")
	var dAt, pushed any
	if deadAt != nil {
		dAt = deadAt.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if pushedAt != nil {
		pushed = pushedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	var bus, rec any
	if ownerBus != "" {
		bus = ownerBus
	}
	if ownerRecord != "" {
		rec = ownerRecord
	}
	// housepool id 每条唯一 · 用全局递增计数器保证唯一
	uuid7Counter++
	kiroID := int64(uuid7Counter*1000 + int(pulledAt.UnixNano()%1000))
	_, err := e.db.Exec(`INSERT INTO credential_ledger
	 (id, kiro_rs_credential_id, owner_bus_id, owner_record_passenger_id, current_group,
	  vendor_id, source_pull_round_id, status, disabled, pulled_at, dead_at,
	  pushed_to_passengerpool_at)
	 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		id, kiroID, bus, rec, group, vendor, roundID, status, pAt, dAt, pushed)
	if err != nil {
		e.t.Fatalf("造 credential: %v", err)
	}
	return id
}

// uuid7 是测试用简易 id（长度足够避免冲突就行）
// 用原子递增计数器 —— time.Now 在同一微秒内会撞。
func uuid7() string {
	uuid7Counter++
	return time.Now().UTC().Format("20060102150405.000000000") + "_" + itoaSafe(uuid7Counter)
}

var uuid7Counter int

func itoaSafe(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
