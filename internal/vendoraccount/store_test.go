package vendoraccount

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/secrets"

	_ "modernc.org/sqlite"
)

// I-09 · vendoraccount 无单元测试 · 补 encrypt/decrypt round-trip 关键路径。
// 这个包管 vendor api_key / webhook_secret 的密态存储 · 是生产安全边界:
// 明文永不落库 · AES-GCM cipher 装错就全家 broken。

// setupTest · 建 dev sqlite + 应用完整 migrations + 装 cipher
func setupTest(t *testing.T) (*Store, context.Context) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(context.Background(), ""); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// 32 字节随机主密钥 → hex 编码 · 跟生产 BP_MASTER_KEY 同格式
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("gen key: %v", err)
	}
	cipher, err := secrets.New(hex.EncodeToString(key))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	return NewStore(d.DB, cipher), context.Background()
}

// TestUpsertAndLoad_RoundTrip · 加密写 → 解密读 → 明文完全一致
func TestUpsertAndLoad_RoundTrip(t *testing.T) {
	store, ctx := setupTest(t)

	cred := Credential{
		APIKey:        "ksk_test_1234567890",
		WebhookSecret: "whsec_abc_xyz",
	}
	if err := store.Upsert(ctx, "kiro91", "default", "api_key", cred); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.LoadActive(ctx, "kiro91")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil {
		t.Fatal("load 返 nil · 应有活跃凭证")
	}
	if got.APIKey != cred.APIKey || got.WebhookSecret != cred.WebhookSecret {
		t.Errorf("round-trip 值不一致 · want %+v got %+v", cred, got)
	}
}

// TestUpsert_NoPlaintextInDB · 落库的 blob **不能**包含明文 API key
func TestUpsert_NoPlaintextInDB(t *testing.T) {
	store, ctx := setupTest(t)

	plainKey := "ksk_shouldnotappear_1234"
	cred := Credential{APIKey: plainKey}
	if err := store.Upsert(ctx, "kiro91", "default", "api_key", cred); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// 直接查 blob · 断言明文串不出现
	var blob []byte
	if err := store.db.QueryRowContext(ctx,
		`SELECT secret_credentials_encrypted FROM vendor_account WHERE vendor_id='kiro91'`,
	).Scan(&blob); err != nil {
		t.Fatalf("查 blob: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("blob 空 · 加密写没落库")
	}
	// AES-GCM 密文里不含明文子串 · 就算 32 字节明文遇到 nonce 也是随机的
	if containsAsBytes(blob, plainKey) {
		t.Errorf("blob 里出现明文 %q · 加密没生效 · 数据泄漏风险 · blob=%x",
			plainKey, blob)
	}
}

// TestLoad_MissingReturnsNilNoError · 表空 or 没匹配 vendor · 返 (nil, nil)
// 让上层能干净判断"该 fallback env 了"
func TestLoad_MissingReturnsNilNoError(t *testing.T) {
	store, ctx := setupTest(t)

	got, err := store.LoadActive(ctx, "kiro91")
	if err != nil {
		t.Errorf("表空时应无错 · 得 %v", err)
	}
	if got != nil {
		t.Errorf("表空时应返 nil · 得 %+v", got)
	}
}

// TestDisable_LoadReturnsNil · 软删后 LoadActive 拿不到(status != active)
func TestDisable_LoadReturnsNil(t *testing.T) {
	store, ctx := setupTest(t)

	cred := Credential{APIKey: "ksk_test"}
	if err := store.Upsert(ctx, "kiro91", "default", "api_key", cred); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.Disable(ctx, "kiro91"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, err := store.LoadActive(ctx, "kiro91")
	if err != nil {
		t.Errorf("软删后 load 应无错 · 得 %v", err)
	}
	if got != nil {
		t.Errorf("软删后应拿不到 · 得 %+v", got)
	}
}

// TestUpsert_UpdateExistingRow · 同 vendor_id + label 再 Upsert · 覆盖 · 不新增行
func TestUpsert_UpdateExistingRow(t *testing.T) {
	store, ctx := setupTest(t)

	if err := store.Upsert(ctx, "kiro91", "default", "api_key",
		Credential{APIKey: "ksk_v1"}); err != nil {
		t.Fatalf("upsert v1: %v", err)
	}
	if err := store.Upsert(ctx, "kiro91", "default", "api_key",
		Credential{APIKey: "ksk_v2"}); err != nil {
		t.Fatalf("upsert v2: %v", err)
	}

	// 只应有一行
	var count int
	if err := store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM vendor_account WHERE vendor_id='kiro91'`,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("Upsert 应覆盖 · 得 %d 行", count)
	}

	// 读回的是新值
	got, _ := store.LoadActive(ctx, "kiro91")
	if got.APIKey != "ksk_v2" {
		t.Errorf("读回应是 ksk_v2 · 得 %q", got.APIKey)
	}
}

// containsAsBytes · []byte 里搜字符串子串
func containsAsBytes(haystack []byte, needle string) bool {
	nb := []byte(needle)
	if len(nb) > len(haystack) {
		return false
	}
	for i := 0; i <= len(haystack)-len(nb); i++ {
		match := true
		for j := range nb {
			if haystack[i+j] != nb[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// 未用 · 保 sql 导入
var _ = sql.ErrNoRows
