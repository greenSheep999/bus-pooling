-- +migrate up

-- 039_strategy_nullable_and_globals.sql
--
-- 1f-B · 策略字段两分离 · 车级 nullable(方案 A) + 全局补 auto/refill 三字段
--
-- **背景**：`docs/15-scheduling.md §4.3.2b` 拍板方案 A(nullable) —— NULL = 跟随全局默认 ·
-- 非 NULL(含 0 / false) = 覆盖本车。当前 `bus.auto_refill_enabled` / `bus.refill_watermark`
-- 是 `NOT NULL DEFAULT 0` · 无法表达"跟随" · 得先改可空。
--
-- 全局默认原本没这三字段 · 也补上：
--   default_auto_refill_enabled · 新车默认关(0) · 车级 NULL 时 fallback
--   default_refill_watermark    · 新车默认 0(即不触发) · 同上
--   default_refill_min_count    · NULL = 按 gap 补齐差额(§4.3.2c 选项 X)
--
-- **迁移保行为铁律**(15-scheduling §4.3.2b)：
--   现有 bus 行的 auto_refill_enabled / refill_watermark 值**一律保留**为"显式覆盖本车" ·
--   不能借 migration 一律转 NULL。老车 Effective() 返值 migration 前后必须不变 ·
--   即使之后全局默认改变了也不影响老车 —— 用户建车时明确关/开过 · 就算作显式意图 ·
--   要"跟随全局"得他自己去 UI 里显式切换。
--
-- SQLite 不支持 ALTER COLUMN DROP NOT NULL · 走"新表 + 复制"(跟 011 一致)。

-- ── 全局补三字段 · 一律走 ALTER ADD COLUMN(默认值填入所有历史行) ──
ALTER TABLE passenger_strategy_default ADD COLUMN default_auto_refill_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE passenger_strategy_default ADD COLUMN default_refill_watermark    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE passenger_strategy_default ADD COLUMN default_refill_min_count    INTEGER;

-- ── bus 表 · auto_refill_enabled / refill_watermark 改可空 ──
-- 保留 refill_min_count(本来就可空 · 不动) · 保留其它所有列 · 保留所有值 ·
-- 保留 CHECK 约束 · 保留 FK · 索引在新表建好后重建。

DROP INDEX IF EXISTS idx_bus_creator;
DROP INDEX IF EXISTS idx_bus_kind_status;
DROP INDEX IF EXISTS idx_bus_anon_match;

CREATE TABLE bus_new (
  id                     TEXT PRIMARY KEY,
  name                   TEXT NOT NULL,
  kind                   TEXT NOT NULL,
  creator_passenger_id   TEXT NOT NULL,
  invite_code            TEXT UNIQUE,
  max_members            INTEGER,
  status                 TEXT NOT NULL DEFAULT 'active',
  created_at             TEXT NOT NULL,
  dissolved_at           TEXT,
  -- 1f-B · 三字段改 nullable · NULL = 跟随全局默认(§4.3.2b 方案 A)
  auto_refill_enabled    INTEGER,
  refill_watermark       INTEGER,
  refill_min_count       INTEGER,
  per_round_count        INTEGER,
  max_unit_price         INTEGER,
  daily_round_limit      INTEGER,
  daily_spend_limit      INTEGER,
  preferred_vendor       TEXT,
  anon_zone              TEXT,
  anon_max_unit_price    INTEGER,
  FOREIGN KEY (creator_passenger_id) REFERENCES passenger(id),
  CHECK (kind IN ('single', 'anon', 'team')),
  CHECK (status IN ('active', 'dissolved'))
);

-- **保行为**：直接复制原值 · auto_refill_enabled / refill_watermark 保留 0 / 1 / N ·
-- 不 UPDATE 成 NULL(那会让老车"跟随全局" · 违反铁律)。
INSERT INTO bus_new
SELECT id, name, kind, creator_passenger_id, invite_code, max_members,
       status, created_at, dissolved_at,
       auto_refill_enabled, refill_watermark, refill_min_count,
       per_round_count, max_unit_price, daily_round_limit, daily_spend_limit,
       preferred_vendor, anon_zone, anon_max_unit_price
FROM bus;

DROP TABLE bus;
ALTER TABLE bus_new RENAME TO bus;

-- 重建索引(跟 001 + 011 一致)
CREATE INDEX idx_bus_creator ON bus(creator_passenger_id, status);
CREATE INDEX idx_bus_kind_status ON bus(kind, status);
CREATE INDEX idx_bus_anon_match ON bus(kind, status, anon_zone);

-- +migrate down

-- 回滚 · bus 表恢复 NOT NULL DEFAULT 0(NULL 用 0 兜) · 删全局三字段

DROP INDEX IF EXISTS idx_bus_creator;
DROP INDEX IF EXISTS idx_bus_kind_status;
DROP INDEX IF EXISTS idx_bus_anon_match;

CREATE TABLE bus_old (
  id                     TEXT PRIMARY KEY,
  name                   TEXT NOT NULL,
  kind                   TEXT NOT NULL,
  creator_passenger_id   TEXT NOT NULL,
  invite_code            TEXT UNIQUE,
  max_members            INTEGER,
  status                 TEXT NOT NULL DEFAULT 'active',
  created_at             TEXT NOT NULL,
  dissolved_at           TEXT,
  auto_refill_enabled    INTEGER NOT NULL DEFAULT 0,
  refill_watermark       INTEGER NOT NULL DEFAULT 0,
  refill_min_count       INTEGER,
  per_round_count        INTEGER,
  max_unit_price         INTEGER,
  daily_round_limit      INTEGER,
  daily_spend_limit      INTEGER,
  preferred_vendor       TEXT,
  anon_zone              TEXT,
  anon_max_unit_price    INTEGER,
  FOREIGN KEY (creator_passenger_id) REFERENCES passenger(id),
  CHECK (kind IN ('single', 'anon', 'team')),
  CHECK (status IN ('active', 'dissolved'))
);

INSERT INTO bus_old
SELECT id, name, kind, creator_passenger_id, invite_code, max_members,
       status, created_at, dissolved_at,
       COALESCE(auto_refill_enabled, 0), COALESCE(refill_watermark, 0),
       refill_min_count, per_round_count, max_unit_price,
       daily_round_limit, daily_spend_limit, preferred_vendor,
       anon_zone, anon_max_unit_price
FROM bus;

DROP TABLE bus;
ALTER TABLE bus_old RENAME TO bus;

CREATE INDEX idx_bus_creator ON bus(creator_passenger_id, status);
CREATE INDEX idx_bus_kind_status ON bus(kind, status);
CREATE INDEX idx_bus_anon_match ON bus(kind, status, anon_zone);

ALTER TABLE passenger_strategy_default DROP COLUMN default_refill_min_count;
ALTER TABLE passenger_strategy_default DROP COLUMN default_refill_watermark;
ALTER TABLE passenger_strategy_default DROP COLUMN default_auto_refill_enabled;
