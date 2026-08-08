package pullrecord

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

// setupDB 建一个临时库 + 跑迁移 + 塞两个乘客 + 一辆车 + 一次拉号轮次。
// 返回 db、乘客 A/B 的 id、A 车的 id、A 名下拉号轮次的 id。
func setupDB(t *testing.T) (*sql.DB, string, string, string, string) {
	t.Helper()
	ctx := context.Background()

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "pr.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}

	// 两个乘客
	pidA := "pass-a"
	pidB := "pass-b"
	for _, pid := range []string{pidA, pidB} {
		if _, err := d.DB.ExecContext(ctx, `
			INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
			VALUES (?, ?, ?, 'h', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`,
			pid, pid, pid+"@e.com"); err != nil {
			t.Fatalf("塞 passenger %s: %v", pid, err)
		}
	}
	// A 建一辆车
	busID := "bus-a"
	if _, err := d.DB.ExecContext(ctx, `
		INSERT INTO bus (id, name, kind, creator_passenger_id, status, created_at)
		VALUES (?, 'A的车', 'single', ?, 'active', '2026-01-01T00:00:00.000Z')`,
		busID, pidA); err != nil {
		t.Fatalf("塞 bus: %v", err)
	}
	if _, err := d.DB.ExecContext(ctx, `
		INSERT INTO bus_member (bus_id, passenger_id, role, joined_at, share_pct, status)
		VALUES (?, ?, 'owner', '2026-01-01T00:00:00.000Z', 100, 'active')`,
		busID, pidA); err != nil {
		t.Fatalf("塞 bus_member: %v", err)
	}

	// 一次拉号轮次（A 的）
	roundID := "round-a"
	if _, err := d.DB.ExecContext(ctx, `
		INSERT INTO pull_round
		  (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
		   key_cost_total, service_fee_total, participants_split_json, status, created_at)
		VALUES (?, '91kiro', 'coid-1', NULL, 3, 3, 60000000, 3000000, '{}', 'completed',
		        '2026-01-01T00:00:00.000Z')`, roundID); err != nil {
		t.Fatalf("塞 pull_round: %v", err)
	}
	return d.DB, pidA, pidB, busID, roundID
}

// 塞一个 record group 号（属乘客 pid，非车）
func insertCred(t *testing.T, dbConn *sql.DB, id string, pid, roundID, status string, kiroID uint64) {
	t.Helper()
	if _, err := dbConn.Exec(`
		INSERT INTO credential_ledger
		  (id, kiro_rs_credential_id, owner_bus_id, owner_record_passenger_id,
		   current_group, vendor_id, source_pull_round_id, status, disabled,
		   pulled_at, key_masked, region, credits_used)
		VALUES (?, ?, NULL, ?, ?, '91kiro', ?, ?, 0,
		        '2026-01-01T00:00:00.000Z', 'ksk_live_xxxx…abc', 'us-east-1', 100000)`,
		id, kiroID, pid, "record-"+pid, roundID, status); err != nil {
		t.Fatalf("塞 credential %s: %v", id, err)
	}
}

// List 只返存活号 · 默认不含 history
func TestList_OnlyAlive(t *testing.T) {
	dbConn, pidA, _, _, roundID := setupDB(t)
	insertCred(t, dbConn, "c1", pidA, roundID, "alive", 1)
	insertCred(t, dbConn, "c2", pidA, roundID, "dead", 2)

	s := NewStore(dbConn)
	items, total, err := s.List(context.Background(), pidA, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total=%d len=%d, 只该返 alive 那一条", total, len(items))
	}
	if items[0].ID != "c1" || items[0].Status != StatusAlive {
		t.Errorf("返回错行: %+v", items[0])
	}
}

// List history=1 时含死号
func TestList_IncludeHistory(t *testing.T) {
	dbConn, pidA, _, _, roundID := setupDB(t)
	insertCred(t, dbConn, "c1", pidA, roundID, "alive", 1)
	insertCred(t, dbConn, "c2", pidA, roundID, "dead", 2)

	s := NewStore(dbConn)
	items, total, err := s.List(context.Background(), pidA,
		ListOptions{IncludeHistory: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total=%d len=%d，含 history 应该返 2 条", total, len(items))
	}
}

// handed_off 号从 record 视图消失（handoff 后不再列）
func TestList_HandedOffHidden(t *testing.T) {
	dbConn, pidA, _, _, roundID := setupDB(t)
	insertCred(t, dbConn, "c1", pidA, roundID, "alive", 1)
	insertCred(t, dbConn, "c2", pidA, roundID, "handed_off", 2)

	s := NewStore(dbConn)
	items, total, _ := s.List(context.Background(), pidA,
		ListOptions{IncludeHistory: true})
	if total != 1 || len(items) != 1 {
		t.Fatalf("handed_off 号该隐藏，total=%d", total)
	}
	if items[0].ID != "c1" {
		t.Errorf("剩下的应该是 c1，得到 %s", items[0].ID)
	}
}

// 跨乘客隔离：B 看不到 A 的号
func TestList_TenantIsolation(t *testing.T) {
	dbConn, pidA, pidB, _, roundID := setupDB(t)
	insertCred(t, dbConn, "c1", pidA, roundID, "alive", 1)

	s := NewStore(dbConn)
	items, total, _ := s.List(context.Background(), pidB, ListOptions{})
	if total != 0 || len(items) != 0 {
		t.Errorf("B 看到了 A 的号：%+v", items)
	}
}

// Get 校验归属：非本人的号返 ErrNotFound
func TestGet_TenantIsolation(t *testing.T) {
	dbConn, pidA, pidB, _, roundID := setupDB(t)
	insertCred(t, dbConn, "c1", pidA, roundID, "alive", 1)

	s := NewStore(dbConn)
	if _, err := s.Get(context.Background(), "c1", pidB); err != ErrNotFound {
		t.Errorf("B 拿 A 的号应 ErrNotFound，得到 %v", err)
	}
}

// GetOwnerships 批量校验
func TestGetOwnerships(t *testing.T) {
	dbConn, pidA, _, _, roundID := setupDB(t)
	insertCred(t, dbConn, "c1", pidA, roundID, "alive", 1)
	insertCred(t, dbConn, "c2", pidA, roundID, "dead", 2)       // 死号也算归他
	insertCred(t, dbConn, "c3", pidA, roundID, "handed_off", 3) // handed_off 不算

	s := NewStore(dbConn)
	own, err := s.GetOwnerships(context.Background(),
		[]string{"c1", "c2", "c3", "does-not-exist"}, pidA)
	if err != nil {
		t.Fatal(err)
	}
	if !own["c1"] || !own["c2"] {
		t.Errorf("c1 c2 应属于 A: %+v", own)
	}
	if own["c3"] {
		t.Errorf("handed_off 的 c3 不该算属于（已交出）")
	}
	if own["does-not-exist"] {
		t.Errorf("不存在的不该算属于")
	}
}

// AssignToBus 把 record 号搬进车
func TestAssignToBus(t *testing.T) {
	dbConn, pidA, _, busID, roundID := setupDB(t)
	insertCred(t, dbConn, "c1", pidA, roundID, "alive", 1)
	insertCred(t, dbConn, "c2", pidA, roundID, "alive", 2)

	s := NewStore(dbConn)
	if err := s.AssignToBus(context.Background(), []string{"c1", "c2"}, pidA, busID); err != nil {
		t.Fatalf("派进车: %v", err)
	}
	// 派完后 record 视图应该空了
	items, _, _ := s.List(context.Background(), pidA, ListOptions{})
	if len(items) != 0 {
		t.Errorf("派进车后 record 视图应空，剩 %d 条", len(items))
	}
	// credential_ledger 里号已改归属
	var busOwner sql.NullString
	var recOwner sql.NullString
	var grp string
	if err := dbConn.QueryRow(`
		SELECT owner_bus_id, owner_record_passenger_id, current_group
		  FROM credential_ledger WHERE id = 'c1'`).Scan(&busOwner, &recOwner, &grp); err != nil {
		t.Fatal(err)
	}
	if !busOwner.Valid || busOwner.String != busID {
		t.Errorf("owner_bus_id = %+v, want %s", busOwner, busID)
	}
	if recOwner.Valid {
		t.Errorf("owner_record_passenger_id 应清空, 得到 %+v", recOwner)
	}
	if grp != "bus-"+busID {
		t.Errorf("current_group = %s, want bus-%s", grp, busID)
	}
}

// AssignToBus 拒绝非本人的号（整批回滚）
func TestAssignToBus_RejectsNonOwned(t *testing.T) {
	dbConn, pidA, pidB, busID, roundID := setupDB(t)
	insertCred(t, dbConn, "c1", pidA, roundID, "alive", 1)
	insertCred(t, dbConn, "c2", pidB, roundID, "alive", 2) // B 的号

	s := NewStore(dbConn)
	if err := s.AssignToBus(context.Background(), []string{"c1", "c2"}, pidA, busID); err != ErrNotFound {
		t.Errorf("非本人号该 ErrNotFound，得到 %v", err)
	}
	// 事务回滚：c1 应仍在 record group（不允许"部分成功"）
	var grp string
	if err := dbConn.QueryRow(`SELECT current_group FROM credential_ledger WHERE id = 'c1'`).Scan(&grp); err != nil {
		t.Fatal(err)
	}
	if grp != "record-"+pidA {
		t.Errorf("c1 应回滚成 record group，得到 %s", grp)
	}
}

// MarkPushed 只写时间戳，不动归属
func TestMarkPushed(t *testing.T) {
	dbConn, pidA, _, _, roundID := setupDB(t)
	insertCred(t, dbConn, "c1", pidA, roundID, "alive", 1)

	s := NewStore(dbConn)
	if err := s.MarkPushed(context.Background(), []string{"c1"}, pidA); err != nil {
		t.Fatalf("MarkPushed: %v", err)
	}
	var pushed sql.NullString
	var grp string
	if err := dbConn.QueryRow(`
		SELECT pushed_to_passengerpool_at, current_group
		  FROM credential_ledger WHERE id = 'c1'`).Scan(&pushed, &grp); err != nil {
		t.Fatal(err)
	}
	if !pushed.Valid || pushed.String == "" {
		t.Errorf("pushed_to_passengerpool_at 应有值")
	}
	// 归属不变
	if grp != "record-"+pidA {
		t.Errorf("current_group 不该动，得到 %s", grp)
	}
}
