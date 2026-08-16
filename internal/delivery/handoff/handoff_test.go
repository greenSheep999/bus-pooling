package handoff

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

// setup 建库 + 塞乘客 + 塞一辆车 + 塞一次拉号轮次 + 塞两个 record group 号
func setup(t *testing.T) (*Store, *sql.DB, string) {
	t.Helper()
	ctx := context.Background()

	d := db.NewTestDB(t)

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
	// DB 里 download_token 存的是 hash（review 修补：token 明文永不落库）
	var st, storedToken string
	if err := dbConn.QueryRow(
		`SELECT status, download_token FROM pending_handoff WHERE id = ?`, p.ID).Scan(&st, &storedToken); err != nil {
		t.Fatal(err)
	}
	if st != "token_issued" {
		t.Errorf("DB 里 status = %s", st)
	}
	if storedToken == p.DownloadToken {
		t.Errorf("download_token 落库应是 hash 不是明文")
	}
	if storedToken != hashToken(p.DownloadToken) {
		t.Errorf("download_token 应等于 sha256(明文)")
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

// janitor 扫卡在 confirmed 的行 · completeFn 重试成功 → completed
func TestJanitor_ConfirmedRetrySuccess(t *testing.T) {
	s, _, pid := setup(t)
	p, _ := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1"},
	})
	if err := s.MarkFulfilled(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkConfirmed(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	// 等 stuckAfter 过
	time.Sleep(60 * time.Millisecond)

	completeCalled := 0
	j := NewJanitor(JanitorConfig{
		Store:      s,
		StuckAfter: 50 * time.Millisecond,
		CompleteFn: func(_ context.Context, _ Pending) error {
			completeCalled++
			return nil
		},
	})
	rep := j.SweepOnce(context.Background())
	if rep.StuckConfirmedRetried != 1 {
		t.Errorf("StuckConfirmedRetried = %d, want 1", rep.StuckConfirmedRetried)
	}
	if completeCalled != 1 {
		t.Errorf("completeFn 调用次数 = %d, want 1", completeCalled)
	}
	got, _ := s.Get(context.Background(), p.ID)
	if got.Status != StatusCompleted {
		t.Errorf("status = %s, want completed", got.Status)
	}
}

// janitor 扫卡在 confirmed 的行 · completeFn 一直失败 → 超 maxRetries 转 need_manual
func TestJanitor_ConfirmedMaxRetriesToManual(t *testing.T) {
	s, _, pid := setup(t)
	p, _ := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1"},
	})
	_ = s.MarkFulfilled(context.Background(), p.ID)
	_ = s.MarkConfirmed(context.Background(), p.ID)
	time.Sleep(60 * time.Millisecond)

	j := NewJanitor(JanitorConfig{
		Store:      s,
		StuckAfter: 50 * time.Millisecond,
		MaxRetries: 2,
		CompleteFn: func(_ context.Context, _ Pending) error {
			return errFake
		},
	})
	// 前 2 次失败 · 不到上限
	j.SweepOnce(context.Background())
	j.SweepOnce(context.Background())
	got, _ := s.Get(context.Background(), p.ID)
	if got.Status != StatusConfirmed {
		t.Errorf("2 次失败后 status = %s, want 仍 confirmed", got.Status)
	}
	// 第 3 次 · attempts=3 > maxRetries=2 · 转 need_manual
	rep := j.SweepOnce(context.Background())
	if rep.StuckConfirmedManual != 1 {
		t.Errorf("StuckConfirmedManual = %d, want 1", rep.StuckConfirmedManual)
	}
	got, _ = s.Get(context.Background(), p.ID)
	if got.Status != StatusNeedManual {
		t.Errorf("status = %s, want need_manual", got.Status)
	}
}

var errFake = &fakeErr{}

type fakeErr struct{}

func (e *fakeErr) Error() string { return "fake" }

// P1-B 锁：retry_count 落库·跨 Janitor 实例（模拟服务重启）计数保持
func TestJanitor_ConfirmedRetryPersistsAcrossRestart(t *testing.T) {
	s, _, pid := setup(t)
	p, _ := s.IssueToken(context.Background(), IssueTokenInput{
		PassengerID: pid, CredentialIDs: []string{"c1"},
	})
	_ = s.MarkFulfilled(context.Background(), p.ID)
	_ = s.MarkConfirmed(context.Background(), p.ID)
	time.Sleep(30 * time.Millisecond)

	// janitor 实例 1：跑 2 次失败·attempts=1、2
	j1 := NewJanitor(JanitorConfig{
		Store:      s,
		StuckAfter: 20 * time.Millisecond,
		MaxRetries: 3,
		CompleteFn: func(_ context.Context, _ Pending) error { return errFake },
	})
	j1.SweepOnce(context.Background())
	j1.SweepOnce(context.Background())

	// 模拟服务重启：新造一个 janitor（新内存·如果计数存内存这里就会归零）
	j2 := NewJanitor(JanitorConfig{
		Store:      s,
		StuckAfter: 20 * time.Millisecond,
		MaxRetries: 3,
		CompleteFn: func(_ context.Context, _ Pending) error { return errFake },
	})
	// 再跑 1 次·attempts 应该是 3（不是 1）
	rep := j2.SweepOnce(context.Background())
	// attempts=3 · 不超 MaxRetries=3 · 不转 need_manual
	if rep.StuckConfirmedManual != 0 {
		t.Errorf("attempts=3 <= MaxRetries=3 不该转 need_manual · 实际 %d", rep.StuckConfirmedManual)
	}
	// 再跑 1 次 · attempts=4 > 3 · 转 need_manual
	rep = j2.SweepOnce(context.Background())
	if rep.StuckConfirmedManual != 1 {
		t.Errorf("attempts=4 > 3 应转 need_manual · 实际 %d", rep.StuckConfirmedManual)
	}
	got, _ := s.Get(context.Background(), p.ID)
	if got.Status != StatusNeedManual {
		t.Errorf("status = %s, want need_manual", got.Status)
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
