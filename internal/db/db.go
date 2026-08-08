// Package db 管 SQLite 连接和 schema 迁移。
//
// SQLite 单节点 + WAL（CLAUDE.md §7.2）。几个硬约束写在 Open 里：
//   - 并发控制用 BEGIN IMMEDIATE，不用 SELECT ... FOR UPDATE（SQLite 没有行锁）
//   - 写连接**只留 1 个** —— SQLite 同时只允许一个写者，放开只会把冲突从
//     应用层挪到驱动层变成 SQLITE_BUSY
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // 纯 Go 驱动，不需要 CGO
)

type DB struct {
	*sql.DB
	path string
}

func Open(ctx context.Context, path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("建数据库目录 %s: %w", dir, err)
		}
	}

	// _txlock=immediate：所有事务默认 BEGIN IMMEDIATE，写冲突在开始时就暴露，
	// 而不是等到 COMMIT 才失败（那时候业务逻辑已经跑完了）
	dsn := path + "?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(ON)" +
		"&_txlock=immediate"

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库 %s: %w", path, err)
	}

	// 见包注释：写者只能有一个
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("连接数据库 %s: %w", path, err)
	}

	return &DB{DB: sqlDB, path: path}, nil
}

func (d *DB) Path() string { return d.path }

// InTx 在一个事务里跑 fn。
//
// 事务是 BEGIN IMMEDIATE（见 Open 的 _txlock）—— 拉号扣款这类跨系统写要靠它
// 保证「同一时刻只有一个人在改这个钱包」（09-transactions §8）。
func (d *DB) InTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开事务: %w", err)
	}
	defer func() {
		// fn panic 时也要回滚，否则连接会被占死（MaxOpenConns=1，占死就是全站挂）
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w（回滚也失败: %v）", err, rbErr)
		}
		return err
	}
	return tx.Commit()
}
