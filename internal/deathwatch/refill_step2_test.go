package deathwatch

// **v3.2 · 2026-08-15** · Step 2 真调 decider.Pull
//
// 覆盖：puller 装配后走真拉 · fulfilled=true → fulfilled 态 · fulfilled=false → 回 pending ·
// err → attempts++ · 3 次后 expired · 并发抢单幂等。

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// mockPuller · 记调用 + 可控返值
type mockPuller struct {
	calls     atomic.Int32
	fulfilled bool
	err       error
}

func (m *mockPuller) Refill(_ context.Context, _ RefillRequest) (bool, error) {
	m.calls.Add(1)
	return m.fulfilled, m.err
}

func setupRefillCase(t *testing.T, puller RefillPuller) (*Watcher, *sql.DB, string) {
	t.Helper()
	sqldb, _, busID, roundID := setupDB(t)
	insertCred(t, sqldb, "c1", 1001, busID, roundID, "alive", true)
	pool := newMockPool()
	pool.list = []housepool.Credential{
		{ID: 1001, Disabled: true, DisabledReason: housepool.ReasonSuspended},
	}
	w := New(Config{DB: sqldb, Pool: pool, RefillPuller: puller,
		Interval: 5 * 60_000_000_000, Now: func() time.Time { return time.Now() }})
	w.SweepOnce(context.Background())
	return w, sqldb, "c1"
}

// puller 返 fulfilled=true · 标 fulfilled
func TestRefillTick_Step2_Fulfilled(t *testing.T) {
	puller := &mockPuller{fulfilled: true}
	w, sqldb, _ := setupRefillCase(t, puller)

	n, err := w.RefillTick(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("应处理 1 · 得 %d", n)
	}
	if puller.calls.Load() != 1 {
		t.Errorf("puller 应被调 1 次 · 得 %d", puller.calls.Load())
	}
	var status string
	sqldb.QueryRow(`SELECT status FROM pending_refill WHERE dead_credential_id='c1'`).Scan(&status)
	if status != "fulfilled" {
		t.Errorf("status = %q · want fulfilled", status)
	}
}

// puller 返 fulfilled=false err=nil（缺货）· 保 pending 等下轮
func TestRefillTick_Step2_NoStockReschedules(t *testing.T) {
	puller := &mockPuller{fulfilled: false}
	w, sqldb, _ := setupRefillCase(t, puller)

	w.RefillTick(context.Background(), 10)

	var status, lastErr string
	sqldb.QueryRow(`SELECT status, COALESCE(last_error,'') FROM pending_refill WHERE dead_credential_id='c1'`).Scan(&status, &lastErr)
	if status != "pending" {
		t.Errorf("缺货应保 pending · 得 %q", status)
	}
	if lastErr != "no_stock" {
		t.Errorf("last_error 应 no_stock · 得 %q", lastErr)
	}
}

// puller 报错 · attempts++ · 未达 3 次保 pending
func TestRefillTick_Step2_ErrorBelowThreshold(t *testing.T) {
	puller := &mockPuller{err: errors.New("上游炸")}
	w, sqldb, _ := setupRefillCase(t, puller)

	// 第一次 attempt · attempts 变 1 · 保 pending
	w.RefillTick(context.Background(), 10)

	var status string
	var attempts int
	sqldb.QueryRow(`SELECT status, attempts FROM pending_refill WHERE dead_credential_id='c1'`).Scan(&status, &attempts)
	if status != "pending" {
		t.Errorf("1 次失败应保 pending · 得 %q", status)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d · want 1", attempts)
	}
}

// 3 次失败标 expired
func TestRefillTick_Step2_ExpiredAfter3(t *testing.T) {
	puller := &mockPuller{err: errors.New("上游炸")}
	w, sqldb, _ := setupRefillCase(t, puller)

	// 3 次 tick
	for i := 0; i < 3; i++ {
		w.RefillTick(context.Background(), 10)
	}

	var status string
	var attempts int
	sqldb.QueryRow(`SELECT status, attempts FROM pending_refill WHERE dead_credential_id='c1'`).Scan(&status, &attempts)
	if status != "expired" {
		t.Errorf("3 次失败应 expired · 得 %q", status)
	}
	if attempts < 3 {
		t.Errorf("attempts = %d · 应至少 3", attempts)
	}
	if puller.calls.Load() != 3 {
		t.Errorf("puller 应被调 3 次 · 得 %d", puller.calls.Load())
	}
}
