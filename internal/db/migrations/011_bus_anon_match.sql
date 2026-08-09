-- +migrate up

-- 011_bus_anon_match.sql
--
-- 1c-1 · 匿名撮合多人 bus 骨架
--
-- bus 表加两列：
--   anon_zone            · anon 撮合的 zone 过滤（相同 zone 才互相匹配）
--   anon_max_unit_price  · anon 撮合的价格上限（microunit）
--
-- 也放开 CreateInput 里的 kind 限制（原来只允许 single · 现在 kind IN (single, anon)）·
-- team 仍然属于 2a 不放开。

ALTER TABLE bus ADD COLUMN anon_zone TEXT;
ALTER TABLE bus ADD COLUMN anon_max_unit_price INTEGER;

-- 撮合查询要索引 · (kind, status, anon_zone) 组合快速找 active anon bus
CREATE INDEX idx_bus_anon_match ON bus(kind, status, anon_zone);

-- +migrate down

DROP INDEX IF EXISTS idx_bus_anon_match;

-- SQLite DROP COLUMN 3.35+ 支持 · 我方最低目标 3.35 · 但为了兼容更早的 sqlite build ·
-- 用 rebuild in-place（跟 006/007 一致）
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
  FOREIGN KEY (creator_passenger_id) REFERENCES passenger(id),
  CHECK (kind IN ('single', 'anon', 'team')),
  CHECK (status IN ('active', 'dissolved'))
);

INSERT INTO bus_old
SELECT id, name, kind, creator_passenger_id, invite_code, max_members,
       status, created_at, dissolved_at, auto_refill_enabled, refill_watermark,
       refill_min_count, per_round_count, max_unit_price,
       daily_round_limit, daily_spend_limit, preferred_vendor
FROM bus;

DROP TABLE bus;
ALTER TABLE bus_old RENAME TO bus;
