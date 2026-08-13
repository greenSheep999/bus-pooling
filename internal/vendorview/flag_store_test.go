package vendorview

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bus-pooling/bus-pooling/internal/db"
)

func flagDB(t *testing.T) *FlagStore {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "flags.db"))
	if err != nil {
		t.Fatalf("开库: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return NewFlagStore(d.DB)
}

// blocked 的 vendor+zone → IsBlocked 返 true
func TestFlagStore_BlockedDetected(t *testing.T) {
	fs := flagDB(t)
	ctx := context.Background()
	_ = fs.UpsertFlags(ctx, []VendorFlag{
		{VendorID: "kiroceo", Zone: "us", Buyable: false, Blocked: true, BlockReason: "成本可疑", Floating: false},
		{VendorID: "kiroceo", Zone: "eu", Buyable: true, Blocked: false},
	})

	blocked, reason := fs.IsBlocked(ctx, "kiroceo", "us")
	if !blocked || reason != "成本可疑" {
		t.Fatalf("us 区应 blocked · 得 blocked=%v reason=%q", blocked, reason)
	}
	// eu 区没 blocked
	if b, _ := fs.IsBlocked(ctx, "kiroceo", "eu"); b {
		t.Error("eu 区不该 blocked")
	}
}

// 查不到该 vendor → fail-open（不拦）
func TestFlagStore_UnknownVendorFailOpen(t *testing.T) {
	fs := flagDB(t)
	if b, _ := fs.IsBlocked(context.Background(), "kiroappcc", "us"); b {
		t.Error("xi8 不覆盖的 vendor 应 fail-open 不拦")
	}
}

// zone 空 → 任一区 blocked 就算（保守）
func TestFlagStore_EmptyZoneAnyBlocked(t *testing.T) {
	fs := flagDB(t)
	ctx := context.Background()
	_ = fs.UpsertFlags(ctx, []VendorFlag{
		{VendorID: "kirooo", Zone: "us", Blocked: false},
		{VendorID: "kirooo", Zone: "eu", Blocked: true, BlockReason: "eu 停售"},
	})
	if b, _ := fs.IsBlocked(ctx, "kirooo", ""); !b {
		t.Error("zone 空 · 任一区 blocked 应算 blocked")
	}
}

// 数据太旧 → fail-open（xi8 可能挂了 · 别误伤）
func TestFlagStore_StaleFailOpen(t *testing.T) {
	fs := flagDB(t)
	fs.staleAfter = 30 * time.Minute
	ctx := context.Background()
	// 手插一条 40 分钟前的 blocked 行
	old := time.Now().UTC().Add(-40 * time.Minute).Format(time.RFC3339)
	_, err := fs.db.Exec(`INSERT INTO xi8_vendor_flags
		(vendor_id, zone, buyable, blocked, floating, updated_at) VALUES ('kiroceo','us',0,1,0,?)`, old)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := fs.IsBlocked(ctx, "kiroceo", "us"); b {
		t.Error("数据过旧应 fail-open 不拦")
	}
}

// upsert 幂等 · 同 vendor+zone 覆盖不新增行
func TestFlagStore_UpsertIdempotent(t *testing.T) {
	fs := flagDB(t)
	ctx := context.Background()
	f := []VendorFlag{{VendorID: "kiro91", Zone: "us", Blocked: true}}
	_ = fs.UpsertFlags(ctx, f)
	// 再写一次 · blocked 改 false
	_ = fs.UpsertFlags(ctx, []VendorFlag{{VendorID: "kiro91", Zone: "us", Blocked: false}})

	var cnt int
	_ = fs.db.QueryRow(`SELECT COUNT(*) FROM xi8_vendor_flags WHERE vendor_id='kiro91'`).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("幂等应 1 行 · 得 %d", cnt)
	}
	// 最新状态是 not blocked
	if b, _ := fs.IsBlocked(ctx, "kiro91", "us"); b {
		t.Error("覆盖后应 not blocked")
	}
}
