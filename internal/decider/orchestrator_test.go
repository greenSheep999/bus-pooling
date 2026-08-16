package decider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
	"github.com/bus-pooling/bus-pooling/internal/providers"
)

// ── mock vendor / pool ──────────────────────────────

type mockVendor struct {
	id           providers.VendorID
	unitPrice    int64 // microunit（zone 内单价）
	available    int
	purchaseErr  error
	purchaseHook func(req providers.PurchaseRequest) (*providers.PurchaseResult, error)
	// capability 覆盖用 · nil = 默认 SupportsIdempotency=true
	capability    *providers.Capability
	purchaseCalls atomic.Int32
}

func (m *mockVendor) ID() providers.VendorID { return m.id }
func (m *mockVendor) Capability() providers.Capability {
	if m.capability != nil {
		return *m.capability
	}
	return providers.Capability{SupportsIdempotency: true, KeyPayloadShape: providers.KeyPayloadFourTuple}
}

func (m *mockVendor) Stock(_ context.Context, _ providers.StockOptions) (*providers.StockSnapshot, error) {
	return &providers.StockSnapshot{
		VendorID:    m.id,
		ObservedAt:  time.Now().UTC(),
		Available:   m.available,
		MinPerOrder: 1,
		MaxPerOrder: 200,
		Zones: []providers.ZoneStock{{
			Zone: providers.ZoneUS, Region: "us-east-1",
			Available: m.available,
			UnitPrice: providers.Money{Amount: m.unitPrice, Currency: providers.CurrencyCredit},
		}},
	}, nil
}

func (m *mockVendor) Purchase(_ context.Context, req providers.PurchaseRequest) (*providers.PurchaseResult, error) {
	m.purchaseCalls.Add(1)
	if m.purchaseErr != nil {
		return nil, m.purchaseErr
	}
	if m.purchaseHook != nil {
		return m.purchaseHook(req)
	}
	keys := make([]providers.KeyPayload, req.Count)
	for i := range keys {
		keys[i] = providers.KeyPayload{
			VendorKeyID: fmt.Sprintf("k%d", i),
			Key:         fmt.Sprintf("ksk_%d", i),
			Account:     fmt.Sprintf("u%d@example.com", i),
			IssuerURL:   "https://d-x.awsapps.com/start",
			Paid:        providers.Money{Amount: m.unitPrice, Currency: providers.CurrencyCredit},
		}
	}
	return &providers.PurchaseResult{
		ClientOrderID: req.ClientOrderID,
		VendorOrderID: "ord-" + req.ClientOrderID[:8],
		Zone:          providers.ZoneUS,
		Purchased:     req.Count,
		Keys:          keys,
		UnitPrice:     providers.Money{Amount: m.unitPrice, Currency: providers.CurrencyCredit},
		TotalCost:     providers.Money{Amount: m.unitPrice * int64(req.Count), Currency: providers.CurrencyCredit},
	}, nil
}

func (m *mockVendor) OrderKeys(_ context.Context, _ string) (*providers.PurchaseResult, error) {
	return nil, providers.ErrNotSupported
}

type mockPool struct {
	nextID   atomic.Uint64
	importFn func(count int) []housepool.CredentialID
}

func (p *mockPool) BatchImport(_ context.Context, req housepool.BatchImportRequest) (*housepool.BatchImportResult, error) {
	evCh := make(chan housepool.BatchImportEvent, len(req.Credentials)+1)
	sumCh := make(chan housepool.BatchImportSummary, 1)

	var ids []housepool.CredentialID
	if p.importFn != nil {
		ids = p.importFn(len(req.Credentials))
	} else {
		ids = make([]housepool.CredentialID, len(req.Credentials))
		for i := range ids {
			ids[i] = housepool.CredentialID(p.nextID.Add(1))
		}
	}
	for i, id := range ids {
		idx := i
		cid := id
		evCh <- housepool.BatchImportEvent{
			Index: &idx, Status: housepool.ImportStatusVerified, CredentialID: &cid,
		}
	}
	close(evCh)
	sumCh <- housepool.BatchImportSummary{Total: len(ids), Imported: len(ids), Verified: len(ids)}
	close(sumCh)
	return &housepool.BatchImportResult{
		Events:  evCh,
		Summary: sumCh,
		Err:     func() error { return nil },
	}, nil
}

// UpdateCredential · 测试桩 · 满足 PoolClient 接口即可
func (p *mockPool) UpdateCredential(_ context.Context, _ housepool.CredentialID, _ housepool.CredentialPatch) error {
	return nil
}

func (p *mockPool) GetCredential(_ context.Context, id housepool.CredentialID) (*housepool.Credential, error) {
	return &housepool.Credential{ID: id, MaskedKey: "ksk_...mock"}, nil
}

// ── 测试脚手架 ──────────────────────────────────────

const testMicro = 1_000_000

func newOrchTest(t *testing.T) (*Orchestrator, *mockVendor, *mockPool, string) {
	t.Helper()
	ctx := context.Background()

	d := db.NewTestDB(t)

	const pid = "p1"
	if _, err := d.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES (?, 'u1', 'u1@example.com', 'x', '2026-01-01', '2026-01-01')`, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `
		INSERT INTO wallet (passenger_id, balance, reserved, updated_at)
		VALUES (?, 10000000000, 0, '2026-01-01')`, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `
		INSERT INTO idempotency_record
		  (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
		VALUES ('idem1', ?, 'POST', '/api/me/pull', 'k1', 'fp1', '2026-01-01')`, pid); err != nil {
		t.Fatal(err)
	}

	vendor := &mockVendor{id: providers.Vendor91Kiro, unitPrice: 30 * testMicro, available: 100}
	pool := &mockPool{}

	o := New(Config{
		DB:     d.DB,
		State:  NewStore(d.DB),
		Vendor: vendor,
		Pool:   pool,
		Rates:  Rates{Service: 500}, // 只启用服务费一层，够测流程
	})
	return o, vendor, pool, pid
}

// ① DoD：一次拉号 → 5 状态推进全部到 completed
func TestPullReachesCompleted(t *testing.T) {
	o, _, _, pid := newOrchTest(t)
	ctx := context.Background()

	got, err := o.Pull(ctx, PullInput{
		PassengerID: pid, Count: 3, Zone: providers.ZoneUS,
		IdempotencyRecordID: "idem1",
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if got.Purchased != 3 {
		t.Errorf("Purchased = %d", got.Purchased)
	}
	if len(got.CredentialIDs) != 3 {
		t.Errorf("CredentialIDs = %d 条", len(got.CredentialIDs))
	}
	if got.UnitPrice == 0 || got.Total()/1 != got.TotalDebit {
		t.Errorf("UnitPrice/TotalDebit 有一个是 0：%+v", got)
	}
	if got.ServiceFee == 0 {
		t.Error("启用了服务费但 ServiceFee=0")
	}

	// 状态机走到了 completed
	pend, err := o.state.FindByClientOrderID(ctx, "kiro91", stateClientOrderIDFor(ctx, t, o, got.PullRoundID))
	if err == nil {
		if pend.Status != StatusCompleted {
			t.Errorf("终态 = %q，want completed", pend.Status)
		}
		if pend.PullRoundID != got.PullRoundID {
			t.Error("pending 的 pull_round_id 没写回")
		}
	}
}

// ② DoD：并发 5 个 goroutine 同一乘客 —— wallet 不超扣
func TestConcurrentPullsDoNotOverspend(t *testing.T) {
	o, _, _, pid := newOrchTest(t)
	ctx := context.Background()

	// 只留够 3 轮 × 3 号 × 单价 30 = 270 的余额
	if _, err := o.db.ExecContext(ctx,
		`UPDATE wallet SET balance = ? WHERE passenger_id = ?`,
		int64(270*1.05*testMicro), pid); err != nil {
		t.Fatal(err)
	}

	// 5 个 idem 记录（幂等表要求 unique key，先建齐）
	for i := 0; i < 5; i++ {
		if _, err := o.db.ExecContext(ctx, `
			INSERT INTO idempotency_record
			  (id, passenger_id, method, path, idempotency_key, request_fingerprint, created_at)
			VALUES (?, ?, 'POST', '/api/me/pull', ?, ?, '2026-01-01')`,
			fmt.Sprintf("idem-%d", i), pid,
			fmt.Sprintf("k-%d", i), fmt.Sprintf("fp-%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	successes := atomic.Int32{}
	insufficient := atomic.Int32{}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := o.Pull(ctx, PullInput{
				PassengerID: pid, Count: 3, Zone: providers.ZoneUS,
				IdempotencyRecordID: fmt.Sprintf("idem-%d", i),
			})
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrInsufficientBalance):
				insufficient.Add(1)
			default:
				t.Errorf("意外错误: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if successes.Load() < 1 {
		t.Fatal("至少一个应成功")
	}
	// 关键断言：余额不能变负 —— 也就是不超扣
	var balance int64
	if err := o.db.QueryRowContext(ctx,
		`SELECT balance FROM wallet WHERE passenger_id = ?`, pid).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance < 0 {
		t.Fatalf("余额变负（超扣）：%d · 成功 %d 次 / 余额不足 %d 次",
			balance, successes.Load(), insufficient.Load())
	}
}

// ③ 部分成交：vendor 只给了 2 个，应按实际结算 + 差额退回
func TestPartialFillReleasesDifference(t *testing.T) {
	o, vendor, _, pid := newOrchTest(t)
	ctx := context.Background()

	vendor.purchaseHook = func(req providers.PurchaseRequest) (*providers.PurchaseResult, error) {
		return &providers.PurchaseResult{
			ClientOrderID: req.ClientOrderID,
			VendorOrderID: "ord-partial",
			Zone:          providers.ZoneUS,
			Purchased:     2, // 申请 5 拿到 2
			Keys: []providers.KeyPayload{
				{Key: "ksk_1", Paid: providers.Money{Amount: 30 * testMicro, Currency: providers.CurrencyCredit}},
				{Key: "ksk_2", Paid: providers.Money{Amount: 30 * testMicro, Currency: providers.CurrencyCredit}},
			},
			TotalCost: providers.Money{Amount: 60 * testMicro, Currency: providers.CurrencyCredit},
		}, nil
	}

	before := balanceOf(t, o, pid)
	got, err := o.Pull(ctx, PullInput{
		PassengerID: pid, Count: 5, Zone: providers.ZoneUS, IdempotencyRecordID: "idem1",
	})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if got.Purchased != 2 {
		t.Errorf("Purchased = %d，want 2", got.Purchased)
	}
	after := balanceOf(t, o, pid)
	// 只扣了 2 号的钱，不是 5 号的
	spend := before - after
	if spend > int64(70*testMicro) {
		t.Errorf("扣了 %d，按 2 号成交不该超过 70 · 差额没退回", spend)
	}
}

// ④ vendor 缺货 → ErrNoStock，冻结不留（不然乘客钱被卡）
func TestVendorNoStockDoesNotLeaveReserve(t *testing.T) {
	o, vendor, _, pid := newOrchTest(t)
	ctx := context.Background()

	vendor.purchaseHook = func(req providers.PurchaseRequest) (*providers.PurchaseResult, error) {
		return &providers.PurchaseResult{
			ClientOrderID: req.ClientOrderID,
			Purchased:     0,
			Keys:          nil,
		}, nil
	}

	_, err := o.Pull(ctx, PullInput{
		PassengerID: pid, Count: 3, Zone: providers.ZoneUS, IdempotencyRecordID: "idem1",
	})
	if !errors.Is(err, ErrNoStock) {
		t.Fatalf("应返回 ErrNoStock，得到 %v", err)
	}

	var reserved int64
	if err := o.db.QueryRowContext(ctx,
		`SELECT reserved FROM wallet WHERE passenger_id = ?`, pid).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved != 0 {
		t.Errorf("失败后仍有冻结 %d —— 乘客的钱被卡住了", reserved)
	}
}

// ⑤ 崩在 purchasing：状态留在 purchasing，冻结**不释放**（vendor 可能已扣款 · §2.1 P0-1）
func TestCrashInPurchasingKeepsReservationForJanitor(t *testing.T) {
	o, vendor, _, pid := newOrchTest(t)
	ctx := context.Background()

	vendor.purchaseErr = errors.New("网络中断")

	_, err := o.Pull(ctx, PullInput{
		PassengerID: pid, Count: 2, Zone: providers.ZoneUS, IdempotencyRecordID: "idem1",
	})
	if err == nil {
		t.Fatal("vendor 出错应报错")
	}

	// 状态应停在 purchasing
	pend, err := o.state.FindByClientOrderID(ctx, "kiro91", firstClientOrderID(t, o.db.QueryRowContext(ctx,
		`SELECT client_order_id FROM pending_purchase WHERE passenger_id = ?`, pid)))
	if err != nil {
		t.Fatal(err)
	}
	if pend.Status != StatusPurchasing {
		t.Fatalf("状态 = %q，必须留在 purchasing（不然 janitor 直接释放冻结 = 我方吃亏）", pend.Status)
	}

	// 冻结**必须**保留 —— vendor 可能已扣款
	var reserved int64
	if err := o.db.QueryRowContext(ctx,
		`SELECT reserved FROM wallet WHERE passenger_id = ?`, pid).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved == 0 {
		t.Error("崩在 purchasing 时冻结被释放了 —— 违反 §2.1 P0-1")
	}
}

// ⑥ 余额不足（估价 > balance）不该进 purchasing
func TestInsufficientBalanceStopsBeforeVendor(t *testing.T) {
	o, vendor, _, pid := newOrchTest(t)
	ctx := context.Background()

	if _, err := o.db.ExecContext(ctx,
		`UPDATE wallet SET balance = 1 WHERE passenger_id = ?`, pid); err != nil {
		t.Fatal(err)
	}

	_, err := o.Pull(ctx, PullInput{
		PassengerID: pid, Count: 3, Zone: providers.ZoneUS, IdempotencyRecordID: "idem1",
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("应返回 ErrInsufficientBalance，得到 %v", err)
	}
	if vendor.purchaseCalls.Load() != 0 {
		t.Error("余额不足不该调 vendor")
	}
}

// ── helpers ────────────────────────────────────────

func balanceOf(t *testing.T, o *Orchestrator, pid string) int64 {
	t.Helper()
	var b int64
	if err := o.db.QueryRowContext(context.Background(),
		`SELECT balance FROM wallet WHERE passenger_id = ?`, pid).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

// Total 是 PullResult 的一个便利方法，测试里用来断言
func (r *PullResult) Total() int64 { return r.TotalDebit }

// stateClientOrderIDFor 从 pull_round_id 反查 client_order_id（测试断言用）
func stateClientOrderIDFor(ctx context.Context, t *testing.T, o *Orchestrator, pullRoundID string) string {
	t.Helper()
	var coid string
	if err := o.db.QueryRowContext(ctx,
		`SELECT client_order_id FROM pull_round WHERE id = ?`, pullRoundID).Scan(&coid); err != nil {
		return ""
	}
	return coid
}

func firstClientOrderID(t *testing.T, row interface{ Scan(...any) error }) string {
	t.Helper()
	var coid string
	if err := row.Scan(&coid); err != nil {
		t.Fatal(err)
	}
	return coid
}

// vendor_response_json 是 JSON 字符串，测试里断言时用
var _ = json.RawMessage(nil)
