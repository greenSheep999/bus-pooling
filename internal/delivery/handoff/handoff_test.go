package handoff

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

// setup 建库 + 塞乘客 + 塞一辆车 + 塞一次拉号轮次 + 塞两个 record group 号
func setup(t *testing.T) (*Store, *sql.DB, string) {
	t.Helper()
	ctx := context.Background()

	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}

	pid := "pass-a"
	if _, err := d.DB.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES (?, ?, ?, 'h', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z')`,
		pid, pid, pid+"@e.com"); err != nil {
		t.Fatalf("塞 passenger: %v", err)
	}

	// 拉号轮次
	if _, err := d.DB.ExecContext(ctx, `
		INSERT INTO pull_round
		  (id, vendor_id, client_order_id, bus_id, count_requested, count_purchased,
		   key_cost_total, service_fee_total, participants_split_json, status, created_at)
		VALUES ('round1', '91kiro', 'coid-1', NULL, 2, 2, 40000000, 2000000, '{}', 'completed',
		        '2026-01-01T00:00:00.000Z')`); err != nil {
		t.Fatalf("塞 pull_round: %v", err)
	}

	// 塞两个属乘客的 record 号（key_masked 通过 004 迁移）
	for i, id := range []string{"c1", "c2"} {
		if _, err := d.DB.ExecContext(ctx, `
			INSERT INTO credential_ledger
			  (id, kiro_rs_credential_id, owner_bus_id, owner_record_passenger_id,
			   current_group, vendor_id, source_pull_round_id, status, disabled, pulled_at,
			   key_masked, region, credits_used)
			VALUES (?, ?, NULL, ?, ?, '91kiro', 'round1', 'alive', 0,
			        '2026-01-01T00:00:00.000Z', ?, 'us-east-1', 100000)`,
			id, i+1, pid, "record-"+pid, "ksk_live_..."+id); err != nil {
			t.Fatalf("塞 credential %s: %v", id, err)
		}
	}

	return NewStore(d.DB, 100*time.Millisecond), d.DB, pid
}

// IssueToken 成功 · pending_handoff 落 token_issued
func TestIssueToken_Ok(t *testing.T) {
	s, dbConn, pid := setup(t)
	p, err := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1", "c2"},
	})
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if p.Status != StatusTokenIssued {
		t.Errorf("status = %s, want token_issued", p.Status)
	}
	if len(p.DownloadToken) != 32 {
		t.Errorf("token 长度 %d，应 32 hex", len(p.DownloadToken))
	}
	// DB 里能找到这行
	var st string
	if err := dbConn.QueryRow(
		`SELECT status FROM pending_handoff WHERE download_token = ?`, p.DownloadToken).Scan(&st); err != nil {
		t.Fatal(err)
	}
	if st != "token_issued" {
		t.Errorf("DB 里 status = %s", st)
	}
}

// IssueToken 拒绝非本人的号（整批）
func TestIssueToken_RejectsNonOwned(t *testing.T) {
	s, _, pid := setup(t)
	// 传一个不存在的 id
	if _, err := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1", "not-exists"},
	}); err != ErrCredentialNotOwned {
		t.Errorf("非本人号该 ErrCredentialNotOwned，得到 %v", err)
	}
}

// IssueToken 拒绝空 credential_ids
func TestIssueToken_EmptyRejected(t *testing.T) {
	s, _, pid := setup(t)
	if _, err := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: nil,
	}); err != ErrEmptyCredentials {
		t.Errorf("空 credentials 该 ErrEmptyCredentials，得到 %v", err)
	}
}

// GetByToken：过期 → ErrTokenExpired
func TestGetByToken_Expired(t *testing.T) {
	s, _, pid := setup(t)
	p, err := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 等 TTL 过（setup 里配的是 100 ms）
	time.Sleep(150 * time.Millisecond)
	if _, err := s.GetByToken(context.Background(), p.DownloadToken); err != ErrTokenExpired {
		t.Errorf("过期后应 ErrTokenExpired，得到 %v", err)
	}
}

// MarkFulfilled：首次推进 token_issued → fulfilled；重复调只累加 fulfill_count
func TestMarkFulfilled_Idempotent(t *testing.T) {
	s, dbConn, pid := setup(t)
	p, _ := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1"},
	})
	if err := s.MarkFulfilled(context.Background(), p.ID); err != nil {
		t.Fatalf("首次 fulfill: %v", err)
	}
	if err := s.MarkFulfilled(context.Background(), p.ID); err != nil {
		t.Fatalf("二次 fulfill 该幂等，得到 %v", err)
	}
	var st string
	var cnt int
	if err := dbConn.QueryRow(
		`SELECT status, fulfill_count FROM pending_handoff WHERE id = ?`, p.ID).
		Scan(&st, &cnt); err != nil {
		t.Fatal(err)
	}
	if st != "fulfilled" {
		t.Errorf("status = %s，want fulfilled", st)
	}
	if cnt != 2 {
		t.Errorf("fulfill_count = %d，want 2（首次 + 重放）", cnt)
	}
}

// MarkConfirmed：fulfilled → confirmed；重复调对 confirmed / completed 幂等
func TestMarkConfirmed_Idempotent(t *testing.T) {
	s, _, pid := setup(t)
	p, _ := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1"},
	})
	if err := s.MarkFulfilled(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkConfirmed(context.Background(), p.ID); err != nil {
		t.Fatalf("首次 confirm: %v", err)
	}
	if err := s.MarkConfirmed(context.Background(), p.ID); err != nil {
		t.Errorf("二次 confirm 该幂等静默 ok，得到 %v", err)
	}
}

// 完整三段：issue → fulfill → confirm → complete
func TestFullFlow(t *testing.T) {
	s, dbConn, pid := setup(t)
	p, err := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFulfilled(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkConfirmed(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCompleted(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	var st string
	if err := dbConn.QueryRow(
		`SELECT status FROM pending_handoff WHERE id = ?`, p.ID).Scan(&st); err != nil {
		t.Fatal(err)
	}
	if st != "completed" {
		t.Errorf("最终 status = %s，want completed", st)
	}
}

// janitor 扫过期 token_issued
func TestJanitor_ExpiresTokenIssued(t *testing.T) {
	s, _, pid := setup(t)
	p, _ := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1"},
	})
	// TTL 过
	time.Sleep(150 * time.Millisecond)

	j := NewJanitor(JanitorConfig{Store: s})
	rep := j.SweepOnce(context.Background())
	if rep.ExpiredTokenIssued != 1 {
		t.Errorf("ExpiredTokenIssued = %d，want 1", rep.ExpiredTokenIssued)
	}
	// 状态推到 expired
	got, err := s.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusExpired {
		t.Errorf("status = %s，want expired", got.Status)
	}
}

// janitor 扫过期 fulfilled
func TestJanitor_ExpiresFulfilled(t *testing.T) {
	s, _, pid := setup(t)
	p, _ := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1"},
	})
	if err := s.MarkFulfilled(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	// TTL 过
	time.Sleep(150 * time.Millisecond)

	j := NewJanitor(JanitorConfig{Store: s})
	rep := j.SweepOnce(context.Background())
	if rep.ExpiredAfterFulfil != 1 {
		t.Errorf("ExpiredAfterFulfil = %d，want 1", rep.ExpiredAfterFulfil)
	}
	got, _ := s.Get(context.Background(), p.ID)
	if got.Status != StatusExpiredAfterFulfill {
		t.Errorf("status = %s，want expired_after_fulfill", got.Status)
	}
}
