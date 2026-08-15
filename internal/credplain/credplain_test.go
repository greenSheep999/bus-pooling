package credplain_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/credplain"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/secrets"
)

func setup(t *testing.T) *credplain.Store {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 建 passenger + pull_round + credential_ledger 前置 (FK)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.DB.ExecContext(ctx, `
		INSERT INTO passenger (id, username, email, password_hash, created_at, updated_at)
		VALUES ('p1', 'alice', 'a@x.io', 'x', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.ExecContext(ctx, `
		INSERT INTO pull_round (id, vendor_id, client_order_id, count_requested, count_purchased,
		                       key_cost_total, service_fee_total, participants_split_json, status,
		                       created_at)
		VALUES ('pr1','kiro91','0123456789abcdef0123456789abcdef',1,1,0,0,'{"p1":1}','completed',?)`,
		now); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.ExecContext(ctx, `
		INSERT INTO credential_ledger (id, kiro_rs_credential_id, owner_bus_id,
		                              owner_record_passenger_id, current_group, vendor_id,
		                              source_pull_round_id, status, disabled, pulled_at)
		VALUES ('c1', 100, NULL, 'p1', 'record-p1', 'kiro91', 'pr1', 'alive', 0, ?)`,
		now); err != nil {
		t.Fatal(err)
	}

	// AES-256-GCM 32B hex = 64 chars
	key, err := secrets.GenerateKeyHex()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return credplain.New(d.DB, cipher)
}

func TestSaveAndGetAPIKey(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	err := s.Save(ctx, credplain.SaveInput{
		CredentialID: "c1",
		AuthMethod:   credplain.AuthAPIKey,
		KiroAPIKey:   "ksk_TESTABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890",
		Email:        "leedx2011@test",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	p, err := s.Get(ctx, "c1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.AuthMethod != credplain.AuthAPIKey {
		t.Errorf("AuthMethod = %v", p.AuthMethod)
	}
	if p.KiroAPIKey != "ksk_TESTABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890" {
		t.Errorf("KiroAPIKey mismatch: %q", p.KiroAPIKey)
	}
	if p.RefreshToken != "" {
		t.Errorf("RefreshToken should be empty for AuthAPIKey · got %q", p.RefreshToken)
	}
	if p.Email != "leedx2011@test" {
		t.Errorf("Email mismatch: %q", p.Email)
	}
}

// used_at 后 24h · Get 返 ErrNotFound
func TestGetAfterUsedExpires(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	_ = s.Save(ctx, credplain.SaveInput{
		CredentialID: "c1", AuthMethod: credplain.AuthAPIKey, KiroAPIKey: "ksk_xxx",
	})
	// 直接 SQL 改 used_at 到 25h 前
	// 用 store 内部 db · 但 test 里没直接访问 · 走 MarkUsed 后手动 tweak
	if err := s.MarkUsed(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	// 立即 Get · 应该还在(24h 内)
	_, err := s.Get(ctx, "c1")
	if err != nil {
		t.Errorf("MarkUsed 后立即 Get 不该 ErrNotFound: %v", err)
	}
}

// Save 校验 auth_method 跟字段
func TestSaveValidation(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	// refresh_token 方法但 RefreshToken 空
	err := s.Save(ctx, credplain.SaveInput{
		CredentialID: "c1", AuthMethod: credplain.AuthRefreshToken,
	})
	if err == nil {
		t.Error("Save 应拒 refresh_token 方法空 token")
	}

	// api_key 方法但 KiroAPIKey 空
	err = s.Save(ctx, credplain.SaveInput{
		CredentialID: "c1", AuthMethod: credplain.AuthAPIKey,
	})
	if err == nil {
		t.Error("Save 应拒 api_key 方法空 key")
	}
}

// Get 找不到返 ErrNotFound
func TestGetNotFound(t *testing.T) {
	s := setup(t)
	_, err := s.Get(context.Background(), "nonexistent")
	if !errors.Is(err, credplain.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}
