-- +migrate up

-- 040_strategy_split_layers.sql · 策略分层重构收口
--
-- **背景**：039 把 bus.auto_refill_enabled / refill_watermark / refill_min_count 改
-- nullable(NULL = 跟随全局默认) —— 但用户走查(docs/1f-review.md)确认这是**镜像噪音·
-- 不是有效抽象**。全局层三个 default_* 字段变成"车级字段的复制品" · 用户容易误触。
--
-- **本次分层重构**(用户拍板 · 见 sprint-1f 讨论):
--
-- 1. **车级 3 字段回退 NOT NULL DEFAULT 0** —— 车级就是车级 · 不再"跟随全局"
--    · auto_refill_enabled INTEGER NOT NULL DEFAULT 0
--    · refill_watermark    INTEGER NOT NULL DEFAULT 0
--    · refill_min_count    INTEGER (保持可空 · 语义 = 按 gap 补齐差额)
--
-- 2. **全局 3 个 default_* 字段保留 · 但语义收窄为"新车 seed"** —— 不再是运行时 fallback ·
--    只在建车向导预填 · 之后车级独立演化(用户建的车用户改 · 不受全局影响)
--
-- 3. **全局新加 3 个跨车调度护栏** —— 真正需要全局才能表达的:
--    · auto_refill_daily_budget         · 所有 auto 车加起来一天最多花 N 积分(microunit)
--    · auto_refill_min_wallet_reserve   · 钱包低于 N 积分时所有 auto 车暂停(microunit)
--    · auto_refill_vendor_allowlist     · 自动补车只允许从这几家 vendor 拉(TEXT JSON 数组)
--
-- **迁移保行为铁律**：老车 auto/refill 值 migration 前后**完全不变** ·
-- NULL 用 0 兜底(NULL 语义原本 = 跟随全局 · 全局 default 也是 0 · 结果一致 · 不改变行为)。
--
-- FK 由 runner 自动关+校验(见 internal/db/migrate.go)。

-- ── 全局补 3 个护栏字段 ──

ALTER TABLE passenger_strategy_default ADD COLUMN auto_refill_daily_budget       INTEGER;
ALTER TABLE passenger_strategy_default ADD COLUMN auto_refill_min_wallet_reserve INTEGER;
ALTER TABLE passenger_strategy_default ADD COLUMN auto_refill_vendor_allowlist   TEXT;

-- ── bus 表回退 · nullable → NOT NULL DEFAULT 0 ──

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
  -- 040 · 回退 NOT NULL · 老车 NULL 用 0 兜(语义等价 · 无行为变化)
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

-- 保行为:NULL 用 0 兜(COALESCE) · 老车行为不变
INSERT INTO bus_new
SELECT id, name, kind, creator_passenger_id, invite_code, max_members,
       status, created_at, dissolved_at,
       COALESCE(auto_refill_enabled, 0),
       COALESCE(refill_watermark, 0),
       refill_min_count,
       per_round_count, max_unit_price, daily_round_limit, daily_spend_limit,
       preferred_vendor, anon_zone, anon_max_unit_price
FROM bus;

DROP TABLE bus;
ALTER TABLE bus_new RENAME TO bus;

CREATE INDEX idx_bus_creator ON bus(creator_passenger_id, status);
CREATE INDEX idx_bus_kind_status ON bus(kind, status);
CREATE INDEX idx_bus_anon_match ON bus(kind, status, anon_zone);

-- +migrate down

-- 回滚 · 全局删 3 护栏字段 + bus 表回到 039 的 nullable 状态

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
  -- 回到 039 nullable
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

INSERT INTO bus_new
SELECT id, name, kind, creator_passenger_id, invite_code, max_members,
       status, created_at, dissolved_at,
       auto_refill_enabled, refill_watermark, refill_min_count,
       per_round_count, max_unit_price, daily_round_limit, daily_spend_limit,
       preferred_vendor, anon_zone, anon_max_unit_price
FROM bus;

DROP TABLE bus;
ALTER TABLE bus_new RENAME TO bus;

CREATE INDEX idx_bus_creator ON bus(creator_passenger_id, status);
CREATE INDEX idx_bus_kind_status ON bus(kind, status);
CREATE INDEX idx_bus_anon_match ON bus(kind, status, anon_zone);

ALTER TABLE passenger_strategy_default DROP COLUMN auto_refill_vendor_allowlist;
ALTER TABLE passenger_strategy_default DROP COLUMN auto_refill_min_wallet_reserve;
ALTER TABLE passenger_strategy_default DROP COLUMN auto_refill_daily_budget;
