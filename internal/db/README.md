# internal/db

SQLite 连接 + schema 迁移。

**主要类型**：`DB`（包 `*sql.DB`）· `Migration`。

**关键约束**：
- WAL 单节点 · **写连接只留 1 个**（SQLite 同时只允许一个写者，放开只是把冲突挪到驱动层变 SQLITE_BUSY）
- 事务默认 `BEGIN IMMEDIATE`（DSN 里 `_txlock=immediate`）—— 写冲突在开始时暴露而不是 COMMIT 时
- `InTx` 里 `recover` 后必须回滚：`MaxOpenConns=1`，连接被占死就是全站挂

**迁移格式**：`NNN_snake_name.sql`，用 `-- +migrate up` / `-- +migrate down` 分段，两段都不能空（rollback 是 DoD 要求）。
