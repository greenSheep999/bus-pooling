package db

// testdb.go · **测试专用** · template DB cache
//
// 老问题：44 处 tests 各自 `d.MigrateUp(ctx, "../db/migrations")` · 48 个
// migration 逐个跑一遍 · 单次 3-5 秒 · × 300 test = 15+ 分钟。CI `-race`
// 更慢 · 单包超时 10 分钟不够。
//
// 新方案：进程内 sync.Once 建一个 **template DB**（跑完所有 migration 后关闭 ·
// 保留 .db 文件在 os.TempDir）· 之后每个 test 只 copy 那个文件（100ms 级）·
// 用 t.TempDir 隔离 · t.Cleanup 关连接。测试之间数据完全独立（copy 出的新文件）·
// 但 schema 阶段免费。
//
// **不给业务代码用** · 只在 `_test.go` 里通过 NewTestDB(t) 调。生产路径还是
// Open + MigrateUp（每次真跑 · 保证 up 顺序正确）。

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

var (
	testTemplateOnce sync.Once
	testTemplatePath string
	testTemplateErr  error
)

// NewTestDB · 测试用 DB · 内部 template cache · 首个 test 建 template（跑完
// 所有 migration）· 后续 test 只 copy 那个文件。跟老 helper 语义完全一致：
// 每个 test 拿到独立的 DB · schema 齐全 · 数据表空。
//
// 用法（替换老代码）:
//
//	// 老:
//	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
//	if err != nil { t.Fatal(err) }
//	if _, err := d.MigrateUp(ctx, "../db/migrations"); err != nil {
//	    t.Fatal(err)
//	}
//	t.Cleanup(func() { _ = d.Close() })
//
//	// 新:
//	d := db.NewTestDB(t)
func NewTestDB(t *testing.T) *DB {
	t.Helper()
	tplPath := ensureTemplate(t)

	// copy template → test 自己的临时文件
	dst := filepath.Join(t.TempDir(), "test.db")
	if err := copyFile(tplPath, dst); err != nil {
		t.Fatalf("NewTestDB: copy template: %v", err)
	}

	d, err := Open(context.Background(), dst)
	if err != nil {
		t.Fatalf("NewTestDB: open %s: %v", dst, err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// ensureTemplate · 全进程唯一 · 首次调用建 template DB（跑完所有 migration）·
// 后续复用同一 .db 文件。测试并发下 sync.Once 保证只建一次。
//
// 建库位置在 os.TempDir（不是 t.TempDir · 那个 test 结束就删）· 进程结束时
// 由 OS 清理 tmp。
func ensureTemplate(t *testing.T) string {
	t.Helper()
	testTemplateOnce.Do(func() {
		f, err := os.CreateTemp("", "bp-test-template-*.db")
		if err != nil {
			testTemplateErr = err
			return
		}
		_ = f.Close()
		testTemplatePath = f.Name()

		d, err := Open(context.Background(), testTemplatePath)
		if err != nil {
			testTemplateErr = err
			return
		}
		defer d.Close()

		// 找 migrations 目录（从 caller 相对路径推 · testTemplateOnce 只跑一次 ·
		// 从哪个测试触发都行 · 只要能找到 migrations 目录）
		dir := findMigrationsDir()
		if _, err := d.MigrateUp(context.Background(), dir); err != nil {
			testTemplateErr = err
			return
		}
	})
	if testTemplateErr != nil {
		t.Fatalf("NewTestDB: template init: %v", testTemplateErr)
	}
	return testTemplatePath
}

// findMigrationsDir · 从当前工作目录（各包的 test 都在自己包目录）往上找
// internal/db/migrations · 找到就返回绝对路径 · 找不到返回空（让 MigrateUp
// 走 embed FS 兜底）。
func findMigrationsDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	// 从 cwd 往上找 internal/db/migrations
	for dir := cwd; dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		p := filepath.Join(dir, "internal", "db", "migrations")
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
		// 也可能已经在 internal/xxx 下 · 试兄弟 db/migrations
		p2 := filepath.Join(filepath.Dir(dir), "db", "migrations")
		if fi, err := os.Stat(p2); err == nil && fi.IsDir() {
			return p2
		}
	}
	return "" // MigrateUp 会走 embed FS
}

// copyFile · 简单文件拷贝 · SQLite .db 文件是纯二进制 · 直接 copy 即可。
// 注意：不要在源文件被 open 时 copy（会读到 half-written 状态）· template 建完
// 已经 Close · copyFile 时源是安全的。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
