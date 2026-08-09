package topup

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockCompleter · janitor 测试用·可控 MarkPaid 结果
type mockCompleter struct {
	err        error
	markPaidCn int
}

func (m *mockCompleter) MarkPaid(_ context.Context, _ string) (Order, error) {
	m.markPaidCn++
	if m.err != nil {
		return Order{}, m.err
	}
	return Order{}, nil
}

// mockPoller · P0-3 测试用·可控 GetPayment 返回的 state
type mockPoller struct {
	state    string
	err      error
	calls    int
	// findResult · FindByClientOrderID 返回（nil = 走 findErr）
	findResult *GatewayPayment
	findErr    error
	findCalls  int
}

func (m *mockPoller) PollByGatewayPaymentID(_ context.Context, _ string) (string, error) {
	m.calls++
	return m.state, m.err
}

func (m *mockPoller) FindByClientOrderID(_ context.Context, _ string) (*GatewayPayment, error) {
	m.findCalls++
	if m.findResult != nil {
		return m.findResult, nil
	}
	if m.findErr != nil {
		return nil, m.findErr
	}
	return nil, ErrGatewayFindUnavailable
}

// initial 卡超时 → expired
func TestJanitor_InitialExpiredAfterTimeout(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	// 推早 updated_at
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	j := NewJanitor(JanitorConfig{
		Pending:        s,
		Completer:      &mockCompleter{},
		InitialTimeout: 1 * time.Second,
	})
	r := j.SweepOnce(context.Background())
	if r.InitialExpired != 1 {
		t.Errorf("InitialExpired=%d · want 1 · report=%+v", r.InitialExpired, r)
	}
	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingExpired {
		t.Errorf("status=%s · want expired", p.Status)
	}
}

// gateway_paid 卡·janitor 重试 MarkPaid 成功·推到 credited
func TestJanitor_GatewayPaidRetried(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	// 推到 gateway_paid + updated_at 早
	_ = s.Advance(context.Background(), id, PendingInitial, PendingGatewayOrdered)
	_ = s.Advance(context.Background(), id, PendingGatewayOrdered, PendingGatewayPaid)
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	j := NewJanitor(JanitorConfig{
		Pending:            s,
		Completer:          &mockCompleter{}, // MarkPaid 返 nil = 成功
		GatewayPaidTimeout: 1 * time.Second,
	})
	r := j.SweepOnce(context.Background())
	if r.GatewayPaidRetried != 1 {
		t.Errorf("GatewayPaidRetried=%d · want 1 · report=%+v", r.GatewayPaidRetried, r)
	}
	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingCredited {
		t.Errorf("status=%s · want credited", p.Status)
	}
}

// **P0-3 复现**：gateway_ordered 卡 · janitor poll gateway 发现已 settled · 主动入账
func TestJanitor_PollsSettledFromGateway(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	// 推到 gateway_ordered + updated_at 早·且 gateway_payment_id 已回填
	_ = s.Advance(context.Background(), id, PendingInitial, PendingGatewayOrdered)
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE topup_order SET gateway_payment_id='pay_xyz' WHERE id=?`, oid); err != nil {
		t.Fatal(err)
	}

	completer := &mockCompleter{}
	poller := &mockPoller{state: "settled"}
	j := NewJanitor(JanitorConfig{
		Orders:                NewStore(s.db),
		Pending:               s,
		Completer:             completer,
		Poller:                poller,
		PollAfter:             1 * time.Second,
		GatewayOrderedTimeout: 999 * time.Hour, // 只走 poll 分支 · 不走 expire
	})
	r := j.SweepOnce(context.Background())
	if poller.calls != 1 {
		t.Errorf("poller 调用 = %d · want 1", poller.calls)
	}
	if completer.markPaidCn != 1 {
		t.Errorf("MarkPaid 调用 = %d · want 1（poll 后应触发）", completer.markPaidCn)
	}
	if r.GatewayOrderedPolled != 1 {
		t.Errorf("GatewayOrderedPolled = %d · want 1 · report=%+v", r.GatewayOrderedPolled, r)
	}
	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingCompleted {
		t.Errorf("poll 后 status = %s · want completed", p.Status)
	}
}

// P0-3 · gateway 还 pending · TTL 未到 · janitor 不该 expire
func TestJanitor_PollPendingKeepsAlive(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	_ = s.Advance(context.Background(), id, PendingInitial, PendingGatewayOrdered)
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE topup_order SET gateway_payment_id='pay_xyz' WHERE id=?`, oid); err != nil {
		t.Fatal(err)
	}

	poller := &mockPoller{state: "pending"}
	j := NewJanitor(JanitorConfig{
		Orders:                NewStore(s.db),
		Pending:               s,
		Completer:             &mockCompleter{},
		Poller:                poller,
		PollAfter:             1 * time.Second,
		GatewayOrderedTimeout: 999 * time.Hour, // gateway 还 pending 且 TTL 未到 · 保留
	})
	j.SweepOnce(context.Background())
	if poller.calls != 1 {
		t.Errorf("poller 应调 1 次 · got=%d", poller.calls)
	}
	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingGatewayOrdered {
		t.Errorf("gateway 还 pending · pending_topup 应保留 gateway_ordered · got=%s", p.Status)
	}
}

// P1-1 · 过期同步双表
func TestJanitor_ExpireSyncsBothTables(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	j := NewJanitor(JanitorConfig{
		Pending:        s,
		Completer:      &mockCompleter{},
		InitialTimeout: 1 * time.Second,
	})
	j.SweepOnce(context.Background())
	var pendingSt, orderSt string
	_ = s.db.QueryRow(`SELECT status FROM pending_topup WHERE id=?`, id).Scan(&pendingSt)
	_ = s.db.QueryRow(`SELECT status FROM topup_order WHERE id=?`, oid).Scan(&orderSt)
	if pendingSt != "expired" || orderSt != "expired" {
		t.Errorf("双表未同步：pending=%s · order=%s · want expired/expired（P1-1 修）", pendingSt, orderSt)
	}
}

// **P0 复现**：gateway_creating 卡后 · gateway 侧已建 · janitor 应回填不 expire
func TestJanitor_GatewayCreating_ReconcilesWhenGatewayHasIt(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	// 推到 gateway_creating + updated_at 早（模拟 CreatePayment 后崩溃）
	_ = s.Advance(context.Background(), id, PendingInitial, PendingGatewayCreating)
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}

	// mock poller · 返 gateway 已有单
	poller := &mockPoller{
		findResult: &GatewayPayment{ID: "pay_recovered", State: "pending"},
	}
	j := NewJanitor(JanitorConfig{
		Orders:                NewStore(s.db),
		Pending:               s,
		Completer:             &mockCompleter{},
		Poller:                poller,
		PollAfter:             1 * time.Second,
		GatewayOrderedTimeout: 999 * time.Hour, // 不 expire
	})
	j.SweepOnce(context.Background())

	// pending 应推到 gateway_ordered · gateway_payment_id 回填
	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingGatewayOrdered {
		t.Errorf("反查成功后 status=%s · want gateway_ordered", p.Status)
	}
	var gwID string
	_ = s.db.QueryRow(`SELECT COALESCE(gateway_payment_id,'') FROM topup_order WHERE id=?`, oid).Scan(&gwID)
	if gwID != "pay_recovered" {
		t.Errorf("gateway_payment_id 未回填 · got=%q want=pay_recovered", gwID)
	}
	// order 应保留 pending（不 expire）
	var orderSt string
	_ = s.db.QueryRow(`SELECT status FROM topup_order WHERE id=?`, oid).Scan(&orderSt)
	if orderSt != "pending" {
		t.Errorf("order.status=%s · want pending（不该被误 expire）", orderSt)
	}
}

// **P0 复现**：gateway_creating 卡后 · 反查能力缺失（gateway 未提供端点）·
// 应转 pending_manual 不 expire · 防丢单
func TestJanitor_GatewayCreating_FindUnavailableGoesManual(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	_ = s.Advance(context.Background(), id, PendingInitial, PendingGatewayCreating)
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}

	// 默认 mockPoller.FindByClientOrderID 返 ErrGatewayFindUnavailable
	poller := &mockPoller{}
	j := NewJanitor(JanitorConfig{
		Orders: NewStore(s.db), Pending: s, Completer: &mockCompleter{},
		Poller: poller, PollAfter: 1 * time.Second,
	})
	j.SweepOnce(context.Background())

	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingManual {
		t.Errorf("反查能力缺失时应转 pending_manual · got=%s", p.Status)
	}
	var orderSt string
	_ = s.db.QueryRow(`SELECT status FROM topup_order WHERE id=?`, oid).Scan(&orderSt)
	if orderSt != "pending" {
		t.Errorf("反查能力缺失时 order 不该 expire · got=%s", orderSt)
	}
}

// **P0 复现**：gateway 明确 404（真没有单）· 才可以 expire
func TestJanitor_GatewayCreating_NotFoundExpires(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	_ = s.Advance(context.Background(), id, PendingInitial, PendingGatewayCreating)
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}

	poller := &mockPoller{findErr: ErrGatewayNotFound}
	j := NewJanitor(JanitorConfig{
		Orders: NewStore(s.db), Pending: s, Completer: &mockCompleter{},
		Poller: poller, PollAfter: 1 * time.Second,
	})
	j.SweepOnce(context.Background())

	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingExpired {
		t.Errorf("gateway 明确 404 应 expire · got=%s", p.Status)
	}
	var orderSt string
	_ = s.db.QueryRow(`SELECT status FROM topup_order WHERE id=?`, oid).Scan(&orderSt)
	if orderSt != "expired" {
		t.Errorf("双表未同步 · order.status=%s", orderSt)
	}
}

// **P1 复现**：gateway_ordered · poll 失败（网络错）· 不 expire · 累计次数
func TestJanitor_GatewayOrdered_PollFailDoesNotExpire(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	_ = s.Advance(context.Background(), id, PendingInitial, PendingGatewayCreating)
	_ = s.Advance(context.Background(), id, PendingGatewayCreating, PendingGatewayOrdered)
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE topup_order SET gateway_payment_id='pay_x' WHERE id=?`, oid); err != nil {
		t.Fatal(err)
	}

	// poll 报网络错·polled=false
	poller := &mockPoller{err: errors.New("network unreachable")}
	j := NewJanitor(JanitorConfig{
		Orders: NewStore(s.db), Pending: s, Completer: &mockCompleter{},
		Poller: poller, PollAfter: 1 * time.Second,
		GatewayOrderedTimeout: 1 * time.Second, // 就算 TTL 到 · 也不该 expire（因为 poll 失败）
	})
	j.SweepOnce(context.Background())

	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingGatewayOrdered {
		t.Errorf("poll 失败不该 expire · got=%s（P1 修）", p.Status)
	}
	if p.PollFailCount != 1 {
		t.Errorf("poll_fail_count = %d · want 1", p.PollFailCount)
	}
}

// **P1 复现**：poll 反复失败到上限 · 转 pending_manual（不 expire）
func TestJanitor_GatewayOrdered_PollFailReachesManualAfterN(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	_ = s.Advance(context.Background(), id, PendingInitial, PendingGatewayCreating)
	_ = s.Advance(context.Background(), id, PendingGatewayCreating, PendingGatewayOrdered)
	// 手工把 poll_fail_count 推到上限-1
	if _, err := s.db.Exec(
		`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z', poll_fail_count=? WHERE id=?`,
		maxCreatingPollFails-1, id,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE topup_order SET gateway_payment_id='pay_x' WHERE id=?`, oid); err != nil {
		t.Fatal(err)
	}

	poller := &mockPoller{err: errors.New("network err")}
	j := NewJanitor(JanitorConfig{
		Orders: NewStore(s.db), Pending: s, Completer: &mockCompleter{},
		Poller: poller, PollAfter: 1 * time.Second,
	})
	j.SweepOnce(context.Background())

	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingManual {
		t.Errorf("反复 poll 失败应转 pending_manual · got=%s", p.Status)
	}
	var orderSt string
	_ = s.db.QueryRow(`SELECT status FROM topup_order WHERE id=?`, oid).Scan(&orderSt)
	if orderSt != "pending" {
		t.Errorf("order 不该 expire · got=%s（P1 修：未知不等于失败）", orderSt)
	}
}

// credited 卡·janitor 直推 completed
func TestJanitor_CreditedToCompleted(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	_ = s.Advance(context.Background(), id, PendingInitial, PendingGatewayOrdered)
	_ = s.Advance(context.Background(), id, PendingGatewayOrdered, PendingGatewayPaid)
	_ = s.Advance(context.Background(), id, PendingGatewayPaid, PendingCredited)
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	j := NewJanitor(JanitorConfig{
		Pending:         s,
		Completer:       &mockCompleter{},
		CreditedTimeout: 1 * time.Second,
	})
	r := j.SweepOnce(context.Background())
	if r.CreditedCompleted != 1 {
		t.Errorf("CreditedCompleted=%d · want 1 · report=%+v", r.CreditedCompleted, r)
	}
	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingCompleted {
		t.Errorf("status=%s · want completed", p.Status)
	}
}
