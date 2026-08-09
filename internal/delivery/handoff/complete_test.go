package handoff

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/housepool"
)

// P1-A 锁：credential_ledger 里的 kiro_rs_credential_id 是 NOT NULL·所以
// "号没 krID"在 credential_ledger 里不可能发生。但如果有个未知 credential id
// 传进来·Complete 会通过"metas 长度 != credIDs 长度"检查捕获·返 error·
// 不会走到标 handed_off。**这是审计说的静默跳过 bug 的锁**。
func TestComplete_RejectsUnknownCredential(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	err := Complete(ctx, CompleteDeps{DB: d.DB, Pool: &noopPool{}}, []string{"nope"})
	if err == nil {
		t.Fatalf("Complete 应返 error（未知 credential）·实际 nil")
	}
	if !strings.Contains(err.Error(), "只有") {
		t.Errorf("error 信息应说明差多少号·实际: %v", err)
	}
}

// 正常 path：号有 krID · pool.Delete 成功 · 台账标 handed_off
func TestComplete_HappyPath(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)

	seedCredWithKrID(t, d, "chappy", 12345)

	pool := &noopPool{}
	if err := Complete(ctx, CompleteDeps{DB: d.DB, Pool: pool}, []string{"chappy"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(pool.deleted) != 1 || pool.deleted[0] != 12345 {
		t.Errorf("pool.Delete 应被调 1 次·krID=12345·实际 %v", pool.deleted)
	}
	var status string
	_ = d.DB.QueryRow(`SELECT status FROM credential_ledger WHERE id='chappy'`).Scan(&status)
	if status != "handed_off" {
		t.Errorf("status = %s, want handed_off", status)
	}
}

// pool 侧 404 视为幂等成功
func TestComplete_PoolNotFoundIdempotent(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	seedCredWithKrID(t, d, "c404", 99)

	pool := &noopPool{deleteErr: &housepool.Error{Op: "DeleteCredential", Err: housepool.ErrNotFound}}
	if err := Complete(ctx, CompleteDeps{DB: d.DB, Pool: pool}, []string{"c404"}); err != nil {
		t.Fatalf("pool 404 应幂等成功·实际 error=%v", err)
	}
	var status string
	_ = d.DB.QueryRow(`SELECT status FROM credential_ledger WHERE id='c404'`).Scan(&status)
	if status != "handed_off" {
		t.Errorf("404 后仍应标 handed_off·实际 %s", status)
	}
}

// 没在台账里的 credential id 返 error
func TestComplete_UnknownCredential(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	err := Complete(ctx, CompleteDeps{DB: d.DB, Pool: &noopPool{}}, []string{"never-exist"})
	if err == nil || !strings.Contains(err.Error(), "只有") {
		t.Errorf("未知 credential 应返 error·实际 %v", err)
	}
}

// pool == nil (DRY_RUN) · 只更新台账
func TestComplete_NilPoolOnlyLedger(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	seedCredWithKrID(t, d, "cdryrun", 55)
	if err := Complete(ctx, CompleteDeps{DB: d.DB, Pool: nil}, []string{"cdryrun"}); err != nil {
		t.Fatalf("nil pool 应只更台账·实际 %v", err)
	}
	var status string
	_ = d.DB.QueryRow(`SELECT status FROM credential_ledger WHERE id='cdryrun'`).Scan(&status)
	if status != "handed_off" {
		t.Errorf("status = %s, want handed_off", status)
	}
}

// ── test helpers ──

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "complete.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MigrateUp(context.Background(), "../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func seedCredNoKrID(t *testing.T, d *db.DB, cid string) {
	t.Helper()
	seedCred(t, d, cid, 0)
}

func seedCredWithKrID(t *testing.T, d *db.DB, cid string, krID uint64) {
	t.Helper()
	seedCred(t, d, cid, krID)
}

func seedCred(t *testing.T, d *db.DB, cid string, krID uint64) {
	t.Helper()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if _, err := d.DB.Exec(`
		INSERT INTO passenger (id, email, username, password_hash, created_at, updated_at)
		VALUES ('p1', 'p1@e.com', 'p1', 'x', ?, ?)
		ON CONFLICT (id) DO NOTHING`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.Exec(`
		INSERT INTO pull_round (id, vendor_id, client_order_id, count_requested, count_purchased,
		                       key_cost_total, service_fee_total, participants_split_json, status, created_at)
		VALUES ('r1', '91kiro', 'co1', 1, 1, 100, 10, '{}', 'completed', ?)
		ON CONFLICT (id) DO NOTHING`, now); err != nil {
		t.Fatal(err)
	}
	var krArg any
	if krID > 0 {
		krArg = krID
	}
	if _, err := d.DB.Exec(`
		INSERT INTO credential_ledger (id, kiro_rs_credential_id, owner_bus_id, owner_record_passenger_id,
		                              current_group, vendor_id, source_pull_round_id, status, disabled, pulled_at, credits_used)
		VALUES (?, ?, NULL, 'p1', 'record-p1', '91kiro', 'r1', 'alive', 0, ?, 0)`,
		cid, krArg, now); err != nil {
		t.Fatal(err)
	}
}

// noopPool · Complete 测试用
type noopPool struct {
	deleted   []uint64
	deleteErr error
}

func (p *noopPool) DeleteCredential(_ context.Context, id housepool.CredentialID) error {
	if p.deleteErr != nil {
		return p.deleteErr
	}
	p.deleted = append(p.deleted, uint64(id))
	return nil
}

// 其余方法·complete_test 不用·stub
func (p *noopPool) BatchImport(_ context.Context, _ housepool.BatchImportRequest) (*housepool.BatchImportResult, error) {
	return nil, nil
}
func (p *noopPool) UpdateCredential(_ context.Context, _ housepool.CredentialID, _ housepool.CredentialPatch) error {
	return nil
}
func (p *noopPool) SetDisabled(_ context.Context, _ housepool.CredentialID, _ bool) error {
	return nil
}
func (p *noopPool) SetDisabledBatch(_ context.Context, _ []housepool.CredentialID, _ bool) error {
	return nil
}
func (p *noopPool) DeleteCredentialBatch(_ context.Context, _ []housepool.CredentialID) error {
	return nil
}
func (p *noopPool) ListCredentials(_ context.Context, _ housepool.CredentialFilter) ([]housepool.Credential, *housepool.PoolSnapshot, error) {
	return nil, nil, nil
}
func (p *noopPool) GetCredential(_ context.Context, _ housepool.CredentialID) (*housepool.Credential, error) {
	return nil, nil
}
func (p *noopPool) GetBalance(_ context.Context, _ housepool.CredentialID) (*housepool.Balance, error) {
	return nil, nil
}
func (p *noopPool) TestCredential(_ context.Context, _ housepool.CredentialID) error { return nil }
func (p *noopPool) RefreshToken(_ context.Context, _ housepool.CredentialID) error   { return nil }
func (p *noopPool) ListGroups(_ context.Context) ([]housepool.Group, error)          { return nil, nil }
func (p *noopPool) CreateGroup(_ context.Context, _ housepool.GroupRequest) (*housepool.Group, error) {
	return nil, nil
}
func (p *noopPool) UpdateGroup(_ context.Context, _ string, _ housepool.GroupRequest) error {
	return nil
}
func (p *noopPool) DeleteGroup(_ context.Context, _ string) error { return nil }
func (p *noopPool) ListClientKeys(_ context.Context, _ housepool.ClientKeyFilter) ([]housepool.ClientKey, error) {
	return nil, nil
}
func (p *noopPool) CreateClientKey(_ context.Context, _ housepool.ClientKeyRequest) (*housepool.ClientKey, error) {
	return nil, nil
}
func (p *noopPool) RotateClientKey(_ context.Context, _ housepool.ClientKeyID) (*housepool.ClientKey, error) {
	return nil, nil
}
func (p *noopPool) UpdateClientKey(_ context.Context, _ housepool.ClientKeyID, _ housepool.ClientKeyRequest) error {
	return nil
}
func (p *noopPool) DeleteClientKey(_ context.Context, _ housepool.ClientKeyID) error {
	return nil
}
func (p *noopPool) SetClientKeyDisabled(_ context.Context, _ housepool.ClientKeyID, _ bool) error {
	return nil
}
func (p *noopPool) StatsOverview(_ context.Context) (*housepool.StatsOverview, error) {
	return nil, nil
}
func (p *noopPool) StatsByCredential(_ context.Context, _ housepool.StatsOptions) ([]housepool.CredentialStats, error) {
	return nil, nil
}
func (p *noopPool) StatsByModel(_ context.Context, _ housepool.StatsOptions) ([]housepool.ModelStats, error) {
	return nil, nil
}
func (p *noopPool) StatsTimeSeries(_ context.Context, _ housepool.StatsOptions) ([]housepool.TimeSeriesPoint, error) {
	return nil, nil
}
func (p *noopPool) GetConcurrency(_ context.Context, _ housepool.CredentialID) (*housepool.Concurrency, error) {
	return nil, nil
}
func (p *noopPool) Ping(_ context.Context) error { return nil }
func (p *noopPool) Close() error                 { return nil }

var _ housepool.HousePool = (*noopPool)(nil)
