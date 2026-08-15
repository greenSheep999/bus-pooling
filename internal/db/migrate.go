package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration 一个版本 = 一个 .sql 文件，里面用标记分成 up / down 两段：
//
//	-- +migrate up
//	CREATE TABLE ...;
//	-- +migrate down
//	DROP TABLE ...;
type Migration struct {
	Version int
	Name    string
	Up      string
	Down    string
}

var migrationFileRe = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.sql$`)

// LoadMigrations 从 embed FS 读迁移。dir 非空时从磁盘读（开发时改 sql 不用重编译）。
func LoadMigrations(dir string) ([]Migration, error) {
	var fsys fs.FS
	var base string
	if dir == "" {
		fsys, base = migrationsFS, "migrations"
	} else {
		fsys, base = osDirFS(dir), "."
	}

	entries, err := fs.ReadDir(fsys, base)
	if err != nil {
		return nil, fmt.Errorf("读迁移目录: %w", err)
	}

	var out []Migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			// 命名不合规范的文件直接报错，别静默跳过 —— 那会让人以为迁移生效了
			if strings.HasSuffix(e.Name(), ".sql") {
				return nil, fmt.Errorf("迁移文件名不合规范 %q（应为 001_snake_name.sql）", e.Name())
			}
			continue
		}
		version, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("迁移文件 %q 版本号非法: %w", e.Name(), err)
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("迁移版本 %d 重复: %s 和 %s", version, prev, e.Name())
		}
		seen[version] = e.Name()

		raw, err := fs.ReadFile(fsys, path.Join(base, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("读迁移 %s: %w", e.Name(), err)
		}
		up, down, err := splitUpDown(string(raw))
		if err != nil {
			return nil, fmt.Errorf("迁移 %s: %w", e.Name(), err)
		}
		out = append(out, Migration{Version: version, Name: m[2], Up: up, Down: down})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

const (
	markerUp   = "-- +migrate up"
	markerDown = "-- +migrate down"
)

func splitUpDown(raw string) (up, down string, err error) {
	iUp := strings.Index(raw, markerUp)
	if iUp < 0 {
		return "", "", fmt.Errorf("缺少 %q 标记", markerUp)
	}
	iDown := strings.Index(raw, markerDown)
	if iDown < 0 {
		return "", "", fmt.Errorf("缺少 %q 标记（rollback 是 DoD 要求）", markerDown)
	}
	if iDown < iUp {
		return "", "", fmt.Errorf("%q 必须在 %q 之后", markerDown, markerUp)
	}
	up = strings.TrimSpace(raw[iUp+len(markerUp) : iDown])
	down = strings.TrimSpace(raw[iDown+len(markerDown):])
	if up == "" {
		return "", "", fmt.Errorf("up 段为空")
	}
	if down == "" {
		return "", "", fmt.Errorf("down 段为空")
	}
	return up, down, nil
}

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migration (
  version    INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  applied_at TEXT NOT NULL
);`

func (d *DB) ensureMigrationTable(ctx context.Context) error {
	if _, err := d.ExecContext(ctx, schemaMigrationsDDL); err != nil {
		return fmt.Errorf("建 schema_migration 表: %w", err)
	}
	return nil
}

func (d *DB) appliedVersions(ctx context.Context) (map[int]bool, error) {
	rows, err := d.QueryContext(ctx, `SELECT version FROM schema_migration`)
	if err != nil {
		return nil, fmt.Errorf("查已应用迁移: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// MigrateUp 按版本号顺序应用未应用的迁移。每个迁移单独一个事务。
func (d *DB) MigrateUp(ctx context.Context, dir string) ([]Migration, error) {
	if err := d.ensureMigrationTable(ctx); err != nil {
		return nil, err
	}
	all, err := LoadMigrations(dir)
	if err != nil {
		return nil, err
	}
	applied, err := d.appliedVersions(ctx)
	if err != nil {
		return nil, err
	}

	var ran []Migration
	for _, m := range all {
		if applied[m.Version] {
			continue
		}
		// FK 关闭 · 让"新表+复制+DROP+RENAME"模式可行(SQLite 里 DROP TABLE 会立刻扫依赖它的
		// 外表 · defer_foreign_keys 只 defer INSERT/UPDATE 的 FK 校验 · 不 defer DROP)。
		// 迁移完 tx commit 前 · 手动 PRAGMA foreign_key_check 校验一遍;失败就 rollback。
		if _, err := d.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return ran, fmt.Errorf("迁移 %03d_%s · 关 FK: %w", m.Version, m.Name, err)
		}
		err := d.InTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.Up); err != nil {
				return fmt.Errorf("应用迁移 %03d_%s: %w", m.Version, m.Name, err)
			}
			// 手工 FK 校验 · 有违反就报错 · InTx 自动 rollback
			rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
			if err != nil {
				return fmt.Errorf("迁移 %03d_%s · FK 校验: %w", m.Version, m.Name, err)
			}
			defer rows.Close()
			var violations []string
			for rows.Next() {
				var table, rowid, parent, fkid sql.NullString
				_ = rows.Scan(&table, &rowid, &parent, &fkid)
				violations = append(violations, fmt.Sprintf("%s → %s", table.String, parent.String))
			}
			if len(violations) > 0 {
				return fmt.Errorf("迁移 %03d_%s · FK 违反: %v", m.Version, m.Name, violations)
			}
			_, err = tx.ExecContext(ctx,
				`INSERT INTO schema_migration (version, name, applied_at) VALUES (?, ?, datetime('now'))`,
				m.Version, m.Name)
			return err
		})
		// 无论成功失败 · 恢复 FK
		if _, e2 := d.ExecContext(ctx, "PRAGMA foreign_keys = ON"); e2 != nil && err == nil {
			err = fmt.Errorf("迁移 %03d_%s · 恢复 FK: %w", m.Version, m.Name, e2)
		}
		if err != nil {
			return ran, err
		}
		ran = append(ran, m)
	}
	return ran, nil
}

// MigrateDown 回滚最近 n 个已应用的迁移（n <= 0 时回滚 1 个）。
func (d *DB) MigrateDown(ctx context.Context, dir string, n int) ([]Migration, error) {
	if n <= 0 {
		n = 1
	}
	if err := d.ensureMigrationTable(ctx); err != nil {
		return nil, err
	}
	all, err := LoadMigrations(dir)
	if err != nil {
		return nil, err
	}
	applied, err := d.appliedVersions(ctx)
	if err != nil {
		return nil, err
	}

	byVersion := map[int]Migration{}
	for _, m := range all {
		byVersion[m.Version] = m
	}

	// 从高版本往低版本回滚
	var versions []int
	for v := range applied {
		versions = append(versions, v)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(versions)))

	var ran []Migration
	for i, v := range versions {
		if i >= n {
			break
		}
		m, ok := byVersion[v]
		if !ok {
			// 库里记了这个版本但文件没了 —— 不能瞎猜怎么回滚
			return ran, fmt.Errorf("已应用的迁移版本 %d 找不到对应文件，无法回滚", v)
		}
		// 同 up · FK 关闭 + 手工校验(见 MigrateUp)
		if _, err := d.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			return ran, fmt.Errorf("回滚 %03d_%s · 关 FK: %w", m.Version, m.Name, err)
		}
		err := d.InTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.Down); err != nil {
				return fmt.Errorf("回滚迁移 %03d_%s: %w", m.Version, m.Name, err)
			}
			rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
			if err != nil {
				return fmt.Errorf("回滚 %03d_%s · FK 校验: %w", m.Version, m.Name, err)
			}
			defer rows.Close()
			var violations []string
			for rows.Next() {
				var table, rowid, parent, fkid sql.NullString
				_ = rows.Scan(&table, &rowid, &parent, &fkid)
				violations = append(violations, fmt.Sprintf("%s → %s", table.String, parent.String))
			}
			if len(violations) > 0 {
				return fmt.Errorf("回滚 %03d_%s · FK 违反: %v", m.Version, m.Name, violations)
			}
			_, err = tx.ExecContext(ctx, `DELETE FROM schema_migration WHERE version = ?`, m.Version)
			return err
		})
		if _, e2 := d.ExecContext(ctx, "PRAGMA foreign_keys = ON"); e2 != nil && err == nil {
			err = fmt.Errorf("回滚 %03d_%s · 恢复 FK: %w", m.Version, m.Name, e2)
		}
		if err != nil {
			return ran, err
		}
		ran = append(ran, m)
	}
	return ran, nil
}

// MigrateStatus 返回所有迁移及是否已应用。
func (d *DB) MigrateStatus(ctx context.Context, dir string) ([]Migration, map[int]bool, error) {
	if err := d.ensureMigrationTable(ctx); err != nil {
		return nil, nil, err
	}
	all, err := LoadMigrations(dir)
	if err != nil {
		return nil, nil, err
	}
	applied, err := d.appliedVersions(ctx)
	if err != nil {
		return nil, nil, err
	}
	return all, applied, nil
}
