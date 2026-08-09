package topup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

// pendingTestDB · 用 in-file sqlite 跑迁移·返回 PendingStore + orderID（预置 idempotency + order）
func pendingTestDB(t *testing.T) (*PendingStore, string, string) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, t.TempDir()+"/p.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	// seed passenger + idempotency_record + topup_order（FK 要求）
	pid := "p-test"
	iid := "i-test"
	oid := "o-test"
	if _, err := d.Exec(`INSERT INTO passenger (id,email,username,password_hash,role,status,created_at,updated_at)
		VALUES(?, 'p@e.local','p','x','user','active','2026-01-01','2026-01-01')`, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO idempotency_record (id,passenger_id,method,path,idempotency_key,request_fingerprint,created_at)
		VALUES(?,?,'POST','/api/me/topup','k',' fp','2026-01-01')`, iid, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO topup_order (id,passenger_id,channel,credits,channel_fee,paid,pay_url,status,expires_at,created_at,updated_at)
		VALUES(?,?,'waffo',100,5,105,'https://x','pending','2027-01-01','2026-01-01','2026-01-01')`, oid, pid); err != nil {
		t.Fatal(err)
	}
	return NewPendingStore(d.DB), pid, oid
}

func TestPending_LifecycleHappyPath(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, err := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// initial → gateway_ordered → gateway_paid → credited → completed
	for _, step := range []struct{ from, to PendingStatus }{
		{PendingInitial, PendingGatewayOrdered},
		{PendingGatewayOrdered, PendingGatewayPaid},
		{PendingGatewayPaid, PendingCredited},
		{PendingCredited, PendingCompleted},
	} {
		if err := s.Advance(context.Background(), id, step.from, step.to); err != nil {
			t.Fatalf("%s → %s: %v", step.from, step.to, err)
		}
	}
	p, err := s.GetByOrderID(context.Background(), oid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != PendingCompleted {
		t.Errorf("status = %s · want completed", p.Status)
	}
}

func TestPending_StaleTransition(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	// 先推到 gateway_ordered
	if err := s.Advance(context.Background(), id, PendingInitial, PendingGatewayOrdered); err != nil {
		t.Fatal(err)
	}
	// 再从**错误的 from**推 · from 不匹配·应 ErrStaleTransition
	err := s.Advance(context.Background(), id, PendingInitial, PendingGatewayPaid)
	if !errors.Is(err, ErrStaleTransition) {
		t.Errorf("from 不匹配应 ErrStaleTransition · got=%v", err)
	}
	// 状态保留 gateway_ordered
	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingGatewayOrdered {
		t.Errorf("状态应保留 gateway_ordered · got=%s", p.Status)
	}
}

func TestPending_FindStuck(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	// 手工把 updated_at 推早
	if _, err := s.db.Exec(`UPDATE pending_topup SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	rows, err := s.FindStuck(context.Background(), time.Now().UTC(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != id {
		t.Errorf("FindStuck 应扫到 1 行·got=%v", rows)
	}
}

func TestPending_MarkManual(t *testing.T) {
	s, pid, oid := pendingTestDB(t)
	id, _ := s.Create(context.Background(), Pending{
		IdempotencyRecordID: "i-test", PassengerID: pid, TopupOrderID: oid,
	})
	if err := s.MarkManual(context.Background(), id, "test reason"); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetByOrderID(context.Background(), oid)
	if p.Status != PendingManual {
		t.Errorf("MarkManual 后 status=%s · want pending_manual", p.Status)
	}
	if p.Error != "test reason" {
		t.Errorf("Error=%q · want test reason", p.Error)
	}
}
