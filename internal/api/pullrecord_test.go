package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/bus"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/delivery/handoff"
	"github.com/bus-pooling/bus-pooling/internal/passenger"
	"github.com/bus-pooling/bus-pooling/internal/pullrecord"
	"github.com/bus-pooling/bus-pooling/internal/strategy"
	"github.com/bus-pooling/bus-pooling/internal/wallet"
)

// prEnv 是拉号记录 / handoff 端点专用的 env（不带 decider · 也不需要真号池）。
//
// api_test.go 的 newEnv / newEnvWithDecider 不装 pullRecords + handoffs 字段，
// 这里独立起一个（避免改公共 helper 影响别的模块的测试）。
type prEnv struct {
	srv         *httptest.Server
	db          *db.DB
	wallets     *wallet.Store
	pullRecords *pullrecord.Store
	handoffs    *handoff.Store
}

func newPREnv(t *testing.T) *prEnv {
	return newPREnvWithPool(t, nil)
}

// newPREnvWithPool 装配一个 prEnv · 可选带 mock housepool
// pool != nil 时 · assign into_bus 会调 pool.UpdateCredential 迁 group
func newPREnvWithPool(t *testing.T, pool *fullMockPool) *prEnv {
	t.Helper()

	d := db.NewTestDB(t)

	wallets := wallet.NewStore(d.DB)
	prs := pullrecord.NewStore(d.DB)
	hf := handoff.NewStore(d.DB, 0)

	deps := ServerDeps{
		DB:           d.DB,
		Passengers:   passenger.NewStore(d.DB),
		Wallets:      wallets,
		Strategies:   strategy.NewStore(d.DB),
		Buses:        bus.NewStore(d.DB),
		PullRecords:  prs,
		Handoffs:     hf,
		SecureCookie: false,
	}
	if pool != nil {
		deps.Pool = pool
	}

	mux := http.NewServeMux()
	NewServer(deps).Routes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		_ = d.Close()
	})
	return &prEnv{srv: srv, db: d, wallets: wallets, pullRecords: prs, handoffs: hf}
}

func (e *prEnv) toTestEnv() *testEnv {
	return &testEnv{srv: e.srv, db: e.db, wallets: e.wallets}
}

// 塞一个 record group 号（属乘客 pid）
func (e *prEnv) insertRecordCred(t *testing.T, id, pid, roundID, status string, kiroID uint64) {
	t.Helper()
	if _, err := e.db.DB.Exec(`
		INSERT INTO credential_ledger
		  (id, kiro_rs_credential_id, owner_bus_id, owner_record_passenger_id,
		   current_group, vendor_id, source_pull_round_id, status, disabled, pulled_at,
		   key_masked, region, credits_used)
		VALUES (?, ?, NULL, ?, ?, '91kiro', ?, ?, 0,
		        '2026-01-01T00:00:00.000Z', ?, 'us-east-1', 100000)`,
		id, kiroID, pid, "record-"+pid, roundID, status, "ksk_live_..."+id); err != nil {
		t.Fatalf("塞 credential %s: %v", id, err)
	}
}

// 塞一次拉号轮次
func (e *prEnv) insertRound(t *testing.T, id string) {
	t.Helper()
	if _, err := e.db.DB.Exec(`
		INSERT INTO pull_round
		  (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
		   key_cost_total, service_fee_total, participants_split_json, status, created_at)
		VALUES (?, '91kiro', ?, NULL, 3, 3, 60000000, 3000000, '{}', 'completed',
		        '2026-01-01T00:00:00.000Z')`, id, id); err != nil {
		t.Fatalf("塞 pull_round: %v", err)
	}
}

// ── 拉号记录列表 ────────────────────────────────

// GET /api/me/pull-records：只返存活号
func TestPullRecordsList_OnlyAlive(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "pr@e.com", "pruser", "password123")
	pid := passengerIDOf(t, base, "pr@e.com")

	e.insertRound(t, "round-1")
	e.insertRecordCred(t, "c1", pid, "round-1", "alive", 1)
	e.insertRecordCred(t, "c2", pid, "round-1", "dead", 2)

	withKey := func(r *http.Request) { r.Header.Set("X-API-Key", key) }

	status, body := base.do(t, "GET", "/api/me/pull-records", nil, withKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[struct {
		Items []map[string]json.RawMessage `json:"items"`
		Total int                          `json:"total"`
	}](t, body)
	if got.Total != 1 || len(got.Items) != 1 {
		t.Fatalf("total=%d len=%d，只该返 alive 那一条", got.Total, len(got.Items))
	}

	// 响应体不能有内部字段（CLAUDE.md §0.1）
	item := got.Items[0]
	banned := []string{"kiro_rs_credential_id", "current_group", "death_source",
		"owner_bus_id", "owner_record_passenger_id"}
	for _, k := range banned {
		if _, leaked := item[k]; leaked {
			t.Errorf("拉号记录响应泄漏内部字段 %q", k)
		}
	}
	// 必须有的字段
	for _, k := range []string{"id", "vendor_id", "status", "key_masked",
		"region", "credits_used", "pulled_at"} {
		if _, ok := item[k]; !ok {
			t.Errorf("拉号记录响应缺字段 %q", k)
		}
	}
}

// GET /api/me/pull-records?history=1：含死号
func TestPullRecordsList_History(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "pr@e.com", "pruser", "password123")
	pid := passengerIDOf(t, base, "pr@e.com")

	e.insertRound(t, "round-1")
	e.insertRecordCred(t, "c1", pid, "round-1", "alive", 1)
	e.insertRecordCred(t, "c2", pid, "round-1", "dead", 2)

	status, body := base.do(t, "GET", "/api/me/pull-records?history=1", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[struct {
		Total int `json:"total"`
	}](t, body)
	if got.Total != 2 {
		t.Errorf("total=%d, 含 history 应该 2 条", got.Total)
	}
}

// 跨乘客不能看
func TestPullRecordsList_Isolation(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	keyA := seedWithAPIKey(t, base, "a@e.com", "aa", "password123")
	seedWithAPIKey(t, base, "b@e.com", "bb", "password123")
	pidB := passengerIDOf(t, base, "b@e.com")

	e.insertRound(t, "round-b")
	e.insertRecordCred(t, "cb1", pidB, "round-b", "alive", 100)

	// A 看应该是 0
	_, body := base.do(t, "GET", "/api/me/pull-records", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", keyA) })
	got := decode[struct {
		Total int `json:"total"`
	}](t, body)
	if got.Total != 0 {
		t.Errorf("A 看到了 B 的号，total=%d", got.Total)
	}
}

// GET /api/me/pull-records/{id} 单条详情
func TestPullRecordGet_Ok(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "pr@e.com", "pruser", "password123")
	pid := passengerIDOf(t, base, "pr@e.com")

	e.insertRound(t, "round-1")
	e.insertRecordCred(t, "c1", pid, "round-1", "alive", 1)

	status, body := base.do(t, "GET", "/api/me/pull-records/c1", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	got := decode[map[string]json.RawMessage](t, body)
	if _, ok := got["id"]; !ok {
		t.Errorf("详情缺 id 字段")
	}
}

// GET 单条 · 非本人号返 404（不泄漏"存在但你不是主人"）
func TestPullRecordGet_TenantIsolation(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	keyA := seedWithAPIKey(t, base, "a@e.com", "aa", "password123")
	seedWithAPIKey(t, base, "b@e.com", "bb", "password123")
	pidB := passengerIDOf(t, base, "b@e.com")

	e.insertRound(t, "round-b")
	e.insertRecordCred(t, "cb1", pidB, "round-b", "alive", 100)

	status, _ := base.do(t, "GET", "/api/me/pull-records/cb1", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", keyA) })
	if status != http.StatusNotFound {
		t.Errorf("A 拿 B 的号该 404，得到 %d", status)
	}
}

// ── 派去向 · assign ────────────────────────────

// assign · into_bus 成功：号从 record 迁到车
func TestAssign_IntoBus(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "pr@e.com", "pruser", "password123")
	pid := passengerIDOf(t, base, "pr@e.com")

	// 建车
	sc, cb := base.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "我的号池", "kind": "single"},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	if sc != http.StatusCreated {
		t.Fatalf("建车: status=%d body=%s", sc, cb)
	}
	var busResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(cb, &busResp)

	e.insertRound(t, "round-1")
	e.insertRecordCred(t, "c1", pid, "round-1", "alive", 1)
	e.insertRecordCred(t, "c2", pid, "round-1", "alive", 2)

	status, body := base.do(t, "POST", "/api/me/pull-records/assign",
		map[string]any{
			"credential_ids": []string{"c1", "c2"},
			"destination":    "into_bus",
			"bus_id":         busResp.ID,
		},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) },
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", "00000000000000000000000000000001") })
	if status != http.StatusOK {
		t.Fatalf("assign 失败: status=%d body=%s", status, body)
	}
	got := decode[map[string]any](t, body)
	if got["assigned"].(float64) != 2 {
		t.Errorf("assigned = %v, want 2", got["assigned"])
	}

	// 派完后 record 视图应该空
	_, listBody := base.do(t, "GET", "/api/me/pull-records", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	list := decode[struct {
		Total int `json:"total"`
	}](t, listBody)
	if list.Total != 0 {
		t.Errorf("派完后 record 视图应空，剩 %d", list.Total)
	}
}

// assign · push_pool：只标 pushed_at，不动归属
func TestAssign_PushPool(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "pr@e.com", "pruser", "password123")
	pid := passengerIDOf(t, base, "pr@e.com")

	e.insertRound(t, "round-1")
	e.insertRecordCred(t, "c1", pid, "round-1", "alive", 1)

	status, body := base.do(t, "POST", "/api/me/pull-records/assign",
		map[string]any{
			"credential_ids": []string{"c1"},
			"destination":    "push_pool",
		},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) },
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", "00000000000000000000000000000002") })
	if status != http.StatusOK {
		t.Fatalf("assign 失败: status=%d body=%s", status, body)
	}
	// c1 仍在 record 视图（push_pool 不改归属）
	_, listBody := base.do(t, "GET", "/api/me/pull-records", nil,
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	list := decode[struct {
		Total int `json:"total"`
	}](t, listBody)
	if list.Total != 1 {
		t.Errorf("push_pool 后号仍在 record，total 应 1，得到 %d", list.Total)
	}
}

// assign · 事务原子性：成功后 credential_ledger + pending_assignment + idempotency_record.response_body
// 三者必须一致。查库确认三张表都落到了。这是 09-transactions §5 的最小断言。
func TestAssign_TxAtomicity(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "tx@e.com", "txuser", "password123")
	pid := passengerIDOf(t, base, "tx@e.com")

	sc, cb := base.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "tx", "kind": "single"},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	if sc != http.StatusCreated {
		t.Fatalf("建车: %d %s", sc, cb)
	}
	var b struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(cb, &b)
	e.insertRound(t, "round-tx")
	e.insertRecordCred(t, "cx1", pid, "round-tx", "alive", 1)
	e.insertRecordCred(t, "cx2", pid, "round-tx", "alive", 2)

	idem := "0000000000000000000000000000abcd"
	status, _ := base.do(t, "POST", "/api/me/pull-records/assign",
		map[string]any{
			"credential_ids": []string{"cx1", "cx2"},
			"destination":    "into_bus",
			"bus_id":         b.ID,
		},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) },
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", idem) })
	if status != http.StatusOK {
		t.Fatalf("assign: %d", status)
	}

	// 断言 1: credential_ledger 两号都进车了
	var ownerCount int
	if err := e.db.QueryRow(
		`SELECT count(1) FROM credential_ledger WHERE owner_bus_id = ? AND id IN ('cx1','cx2')`, b.ID,
	).Scan(&ownerCount); err != nil {
		t.Fatalf("查 credential_ledger: %v", err)
	}
	if ownerCount != 2 {
		t.Errorf("credential_ledger owner_bus_id 匹配 %d, want 2", ownerCount)
	}

	// 断言 2: pending_assignment 落了 2 行 completed
	var eventCount int
	if err := e.db.QueryRow(
		`SELECT count(1) FROM pending_assignment WHERE passenger_id = ? AND target_bus_id = ? AND status = 'completed'`,
		pid, b.ID,
	).Scan(&eventCount); err != nil {
		t.Fatalf("查 pending_assignment: %v", err)
	}
	if eventCount != 2 {
		t.Errorf("pending_assignment count = %d, want 2", eventCount)
	}

	// 断言 3: idempotency_record 已 finalize（response_body 非空）
	var respBody []byte
	if err := e.db.QueryRow(
		`SELECT response_body FROM idempotency_record WHERE idempotency_key = ?`, idem,
	).Scan(&respBody); err != nil {
		t.Fatalf("查 idempotency_record: %v", err)
	}
	if len(respBody) == 0 {
		t.Errorf("idempotency_record.response_body 空 · 事务未合并保存幂等响应")
	}

	// 断言 4: 幂等重放拿同 body
	_, replay := base.do(t, "POST", "/api/me/pull-records/assign",
		map[string]any{
			"credential_ids": []string{"cx1", "cx2"},
			"destination":    "into_bus",
			"bus_id":         b.ID,
		},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) },
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", idem) })
	if !bytes.Equal(respBody, replay) {
		t.Errorf("重放 body 不一致\nfirst = %s\nreplay = %s", respBody, replay)
	}
}

// assign · into_bus 有 housepool pool 时·真调 UpdateCredential 迁 group
// **09-transactions §5 · 1a 收尾** —— 修 into_bus 不迁 group 的历史缺口
func TestAssign_IntoBus_MigratesHousepoolGroup(t *testing.T) {
	pool := &fullMockPool{}
	e := newPREnvWithPool(t, pool)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "grp@e.com", "grpuser", "password123")
	pid := passengerIDOf(t, base, "grp@e.com")

	sc, cb := base.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "grp", "kind": "single"},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) })
	if sc != http.StatusCreated {
		t.Fatalf("建车: %d %s", sc, cb)
	}
	var b struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(cb, &b)
	e.insertRound(t, "round-grp")
	// kiro_rs_credential_id 用非零值 · 才会被 LookupKiroRSCredentialIDs 命中
	e.insertRecordCred(t, "cgrp1", pid, "round-grp", "alive", 12345)

	idem := "0000000000000000000000000000ff11"
	status, _ := base.do(t, "POST", "/api/me/pull-records/assign",
		map[string]any{
			"credential_ids": []string{"cgrp1"},
			"destination":    "into_bus",
			"bus_id":         b.ID,
		},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) },
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", idem) })
	if status != http.StatusOK {
		t.Fatalf("assign: %d", status)
	}

	// 断言：pool.UpdateCredential 被调了 1 次·target group = bus-{id}
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if len(pool.updateCalls) != 1 {
		t.Fatalf("UpdateCredential 调用次数 = %d, want 1", len(pool.updateCalls))
	}
	call := pool.updateCalls[0]
	if uint64(call.ID) != 12345 {
		t.Errorf("UpdateCredential id = %d, want 12345", call.ID)
	}
	if call.Patch.Groups == nil || len(*call.Patch.Groups) != 1 || (*call.Patch.Groups)[0] != "bus-"+b.ID {
		t.Errorf("Groups = %v, want [bus-%s]", call.Patch.Groups, b.ID)
	}
}

// **P0-1 复现 · assign 并发跨系统分叉**
//
// 场景：两个不同 idempotency key 同时对同一 credential 派往不同 bus。
// 修前：R1 pool = bus-X · R2 pool = bus-Y · 只有一个台账成功 → 台账 / pool 分叉。
// 修后：R2 在 tx1 落 initial 时 UNIQUE(credential_id) WHERE status='initial' 挡住 · 409。
//
//	pool 只被调 1 次 · 台账跟 pool 都指向同一个 bus。
func TestAssign_ConcurrentSameCredentialToDifferentBuses(t *testing.T) {
	pool := &fullMockPool{}
	e := newPREnvWithPool(t, pool)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "conc@e.com", "concurrent", "password123")
	pid := passengerIDOf(t, base, "conc@e.com")

	// 建两辆车 A / B
	newBus := func(name string) string {
		sc, cb := base.do(t, "POST", "/api/me/buses",
			map[string]any{"name": name, "kind": "single"},
			func(r *http.Request) { r.Header.Set("X-API-Key", key) })
		if sc != http.StatusCreated {
			t.Fatalf("建车 %s: %d %s", name, sc, cb)
		}
		var b struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(cb, &b)
		return b.ID
	}
	busA := newBus("A")
	busB := newBus("B")
	e.insertRound(t, "round-conc")
	e.insertRecordCred(t, "cconc", pid, "round-conc", "alive", 55555)

	// 让 pool.UpdateCredential 阻塞·造并发窗口
	release := make(chan struct{})
	pool.BlockUpdates(release)

	// 并发发两个 assign · 不同 idem key · 同 credential · 不同 bus
	type resp struct {
		status int
		body   []byte
	}
	results := make(chan resp, 2)
	send := func(idem, busID string) {
		s, b := base.do(t, "POST", "/api/me/pull-records/assign",
			map[string]any{
				"credential_ids": []string{"cconc"},
				"destination":    "into_bus",
				"bus_id":         busID,
			},
			func(r *http.Request) { r.Header.Set("X-API-Key", key) },
			func(r *http.Request) { r.Header.Set("X-Idempotency-Key", idem) })
		results <- resp{s, b}
	}
	go send("00000000000000000000000000000a1a", busA)
	go send("00000000000000000000000000000a1b", busB)

	// 稍等 · 让两个请求都推到 pool 阶段（或被 UNIQUE 挡住）
	// UNIQUE 挡住的会先返回·再放 pool
	time.Sleep(200 * time.Millisecond)
	close(release)

	got := []resp{<-results, <-results}
	success, conflict := 0, 0
	for _, r := range got {
		switch r.status {
		case http.StatusOK:
			success++
		case http.StatusConflict:
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("并发结果异常 · 成功 %d 冲突 %d · want 1/1 · results=%+v", success, conflict, got)
	}

	// 断言 1：pool.UpdateCredential 只被调 1 次（P0-1 关键：不能两个都调）
	pool.mu.Lock()
	calls := len(pool.updateCalls)
	var poolBus string
	if calls > 0 {
		if g := pool.updateCalls[0].Patch.Groups; g != nil && len(*g) > 0 {
			poolBus = (*g)[0]
		}
	}
	pool.mu.Unlock()
	if calls != 1 {
		t.Fatalf("pool.UpdateCredential 调用 %d 次 · want 1 (P0-1：并发不该都调 pool)", calls)
	}

	// 断言 2：credential_ledger.owner_bus_id 跟 pool 一致（无分叉）
	var ledgerBus string
	_ = e.db.DB.QueryRow(
		`SELECT COALESCE(owner_bus_id, '') FROM credential_ledger WHERE id='cconc'`,
	).Scan(&ledgerBus)
	wantBus := ""
	if poolBus == "bus-"+busA {
		wantBus = busA
	} else if poolBus == "bus-"+busB {
		wantBus = busB
	}
	if wantBus == "" || ledgerBus != wantBus {
		t.Errorf("分叉 · pool=%q · ledger.owner_bus_id=%q · want ledger=%q",
			poolBus, ledgerBus, wantBus)
	}
}

// assign · destination=handoff 应被拒绝（走三段式，不在这个端点）
func TestAssign_RejectsHandoff(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	key := seedWithAPIKey(t, base, "pr@e.com", "pruser", "password123")
	pid := passengerIDOf(t, base, "pr@e.com")

	e.insertRound(t, "round-1")
	e.insertRecordCred(t, "c1", pid, "round-1", "alive", 1)

	status, _ := base.do(t, "POST", "/api/me/pull-records/assign",
		map[string]any{
			"credential_ids": []string{"c1"},
			"destination":    "handoff",
		},
		func(r *http.Request) { r.Header.Set("X-API-Key", key) },
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", "00000000000000000000000000000003") })
	if status != http.StatusBadRequest {
		t.Errorf("destination=handoff 应 400，得到 %d", status)
	}
}

// assign · 校验车归属：派进别人的车 → 404
func TestAssign_IntoBusRejectsOtherBus(t *testing.T) {
	e := newPREnv(t)
	base := e.toTestEnv()
	keyA := seedWithAPIKey(t, base, "a@e.com", "aa", "password123")
	keyB := seedWithAPIKey(t, base, "b@e.com", "bb", "password123")
	pidA := passengerIDOf(t, base, "a@e.com")

	// B 建车
	sc, cb := base.do(t, "POST", "/api/me/buses",
		map[string]any{"name": "B的车", "kind": "single"},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyB) })
	if sc != http.StatusCreated {
		t.Fatalf("B 建车: %s", cb)
	}
	var busResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(cb, &busResp)

	// A 拉了个号
	e.insertRound(t, "round-1")
	e.insertRecordCred(t, "c1", pidA, "round-1", "alive", 1)

	// A 想把号派进 B 的车 → 404
	status, _ := base.do(t, "POST", "/api/me/pull-records/assign",
		map[string]any{
			"credential_ids": []string{"c1"},
			"destination":    "into_bus",
			"bus_id":         busResp.ID,
		},
		func(r *http.Request) { r.Header.Set("X-API-Key", keyA) },
		func(r *http.Request) { r.Header.Set("X-Idempotency-Key", "00000000000000000000000000000004") })
	if status != http.StatusNotFound {
		t.Errorf("派进别人车该 404，得到 %d", status)
	}
}
