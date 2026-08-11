-- +migrate up

-- 026 · vendor_dispatch 加 `source` 列 + 主键含 source
--
-- 背景：
--   - 5 家 kiro 系 vendor 的 dispatch 由**它们自己**的 fleet 端点拉（source='vendor_self'）
--   - xi8.cc 是聚合平台 · 拿它当**内部数据源**校对/backfill vendor 数据 · 打 source='xi8'
--   - **不注册为 vendor** · 不出前端（`CLAUDE.md §0.1`）· 只在后端跟 vendor_self 并存
--
-- 为什么主键要含 source：
--   - 两个源可能都记同一批 · dispatch_key 定义不同 · 独立行不冲突
--   - vendor_self 修好时区 bug 后是权威 · xi8 只留作对账/gap 兜底
--   - 上层读时 · 前端端点**只查 source='vendor_self'** · xi8 数据留后端调试
--
-- 迁移：SQLite 不支持 ALTER PRIMARY KEY · 建新表 → 拷数据 → 换名
--   老数据全部打 source='vendor_self'（旧写入方全来自 vendor 自己）

CREATE TABLE vendor_dispatch_new (
    vendor_id       TEXT NOT NULL,
    dispatch_key    TEXT NOT NULL,
    source          TEXT NOT NULL DEFAULT 'vendor_self', -- vendor_self | xi8
    region          TEXT,
    dispatched_at   TEXT NOT NULL,
    count           INTEGER NOT NULL,
    alive           INTEGER,
    dead            INTEGER,
    dead_at         TEXT,
    status          TEXT,
    fetched_at      TEXT NOT NULL,
    raw             BLOB,
    PRIMARY KEY (vendor_id, dispatch_key, source)
);

INSERT INTO vendor_dispatch_new
    (vendor_id, dispatch_key, source, region, dispatched_at,
     count, alive, dead, dead_at, status, fetched_at, raw)
SELECT vendor_id, dispatch_key, 'vendor_self', region, dispatched_at,
       count, alive, dead, dead_at, status, fetched_at, raw
  FROM vendor_dispatch;

DROP INDEX IF EXISTS idx_vendor_dispatch_time;
DROP TABLE vendor_dispatch;
ALTER TABLE vendor_dispatch_new RENAME TO vendor_dispatch;

CREATE INDEX idx_vendor_dispatch_time
    ON vendor_dispatch (vendor_id, source, dispatched_at DESC);

-- +migrate down

-- 回滚：反向做一次表重建 · 丢 xi8 那部分行
CREATE TABLE vendor_dispatch_old (
    vendor_id       TEXT NOT NULL,
    dispatch_key    TEXT NOT NULL,
    region          TEXT,
    dispatched_at   TEXT NOT NULL,
    count           INTEGER NOT NULL,
    alive           INTEGER,
    dead            INTEGER,
    dead_at         TEXT,
    status          TEXT,
    fetched_at      TEXT NOT NULL,
    raw             BLOB,
    PRIMARY KEY (vendor_id, dispatch_key)
);

INSERT INTO vendor_dispatch_old
    (vendor_id, dispatch_key, region, dispatched_at,
     count, alive, dead, dead_at, status, fetched_at, raw)
SELECT vendor_id, dispatch_key, region, dispatched_at,
       count, alive, dead, dead_at, status, fetched_at, raw
  FROM vendor_dispatch
 WHERE source = 'vendor_self';

DROP INDEX IF EXISTS idx_vendor_dispatch_time;
DROP TABLE vendor_dispatch;
ALTER TABLE vendor_dispatch_old RENAME TO vendor_dispatch;

CREATE INDEX idx_vendor_dispatch_time
    ON vendor_dispatch (vendor_id, dispatched_at DESC);
