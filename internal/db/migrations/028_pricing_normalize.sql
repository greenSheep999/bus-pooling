-- +migrate up

-- 028 · Pricing 一整套 · 数据入库标准化（docs/18-pricing-normalization.md 落码 Step 2）
--
-- 5 张事：
--   1. exchange_rate 表 · 系统配置汇率 · 有历史（应对波动）
--   2. vendor_probe 加 8 列 · 存上游原样 + 我方标准化积分
--   3. vendor_price_tier 表 · kirodrop 分档 schedule
--   4. user_subsidy 表 · 减免栈（个人邀请码 / 邀请奖励 / 优惠码 · 有时效额度）
--   5. passenger.tier 加列 · retail / community / wholesale（docs/18 §2.1）
--
-- 落码后 vendor_pricing 表（013 建 · 空）配合本 migration 一起 seed 6 家。

-- ─── 1. exchange_rate · 系统配置汇率 · 应对波动 ────────────────

CREATE TABLE exchange_rate (
  currency        TEXT NOT NULL,       -- USD / (未来可加 EUR 等)
  rate_to_credits INTEGER NOT NULL,    -- microunit · 1 单位货币 = N microunit 积分
  effective_from  TEXT NOT NULL,       -- 生效时刻 · RFC3339
  effective_to    TEXT,                -- 失效时刻（当前汇率 NULL）
  source          TEXT NOT NULL,       -- system_config | vendor_ref | external_api
  note            TEXT,                -- 运营备注
  created_at      TEXT NOT NULL,
  PRIMARY KEY (currency, effective_from),
  CHECK (rate_to_credits > 0),
  CHECK (source IN ('system_config','vendor_ref','external_api'))
);

CREATE INDEX idx_exchange_rate_current ON exchange_rate(currency, effective_to);

-- Seed 当前汇率（vendor_ref 对齐上游 kirodrop UI 2026-08-12 展示的 6.8）
INSERT INTO exchange_rate (currency, rate_to_credits, effective_from, source, note, created_at)
VALUES ('USD', 6800000, '2026-08-12T00:00:00Z', 'vendor_ref',
        '对齐 kirodrop UI 2026-08-12 展示 · $1 = ¥6.80', '2026-08-12T09:00:00Z');


-- ─── 2. vendor_pricing seed · 6 家 · 013 建的空表现在填 ──────

INSERT INTO vendor_pricing (vendor_id, quote_currency, credits_per_unit,
                            rate_source, rate_updated_at,
                            vendor_surcharge_bp, active,
                            created_at, updated_at)
VALUES
  ('kiro91',    'credit', 1000000, 'manual', '2026-08-12T09:00:00Z', 0, 1, '2026-08-12T09:00:00Z', '2026-08-12T09:00:00Z'),
  ('kiroceo',   'credit', 1000000, 'manual', '2026-08-12T09:00:00Z', 0, 1, '2026-08-12T09:00:00Z', '2026-08-12T09:00:00Z'),
  ('kirooo',    'credit', 1000000, 'manual', '2026-08-12T09:00:00Z', 0, 1, '2026-08-12T09:00:00Z', '2026-08-12T09:00:00Z'),
  ('kiroappio', 'credit', 1000000, 'manual', '2026-08-12T09:00:00Z', 0, 1, '2026-08-12T09:00:00Z', '2026-08-12T09:00:00Z'),
  ('kiroappcc', 'credit', 1000000, 'manual', '2026-08-12T09:00:00Z', 0, 1, '2026-08-12T09:00:00Z', '2026-08-12T09:00:00Z'),
  ('kirodrop',  'USD',    6800000, 'manual', '2026-08-12T09:00:00Z', 0, 1, '2026-08-12T09:00:00Z', '2026-08-12T09:00:00Z');


-- ─── 3. vendor_probe 加 8 列 · 上游原样 + 我方标准化 ─────────

ALTER TABLE vendor_probe ADD COLUMN vendor_currency        TEXT;
ALTER TABLE vendor_probe ADD COLUMN vendor_unit_raw        INTEGER;    -- microunit · vendor 报价原值
ALTER TABLE vendor_probe ADD COLUMN vendor_exchange_rate   REAL;       -- vendor 侧汇率（kirodrop UI 有 · api 无 · 保留字段）
ALTER TABLE vendor_probe ADD COLUMN vendor_price_usd_raw   INTEGER;    -- USD 原值 microunit
ALTER TABLE vendor_probe ADD COLUMN vendor_price_cny_raw   INTEGER;    -- CNY 原值 microunit（kirodrop UI 有 · api 无 · 保留字段）
ALTER TABLE vendor_probe ADD COLUMN our_unit_credits       INTEGER;    -- ★ 唯一权威积分 microunit · 1_000_000 = 1 积分 = 1 RMB
ALTER TABLE vendor_probe ADD COLUMN our_unit_source        TEXT;       -- vendor_native / computed_from_usd / fallback_last_rate
ALTER TABLE vendor_probe ADD COLUMN our_computed_at        TEXT;

CREATE INDEX idx_vp_credits ON vendor_probe(vendor_id, probed_at, our_unit_credits);


-- ─── 4. vendor_price_tier · kirodrop 分档 schedule ──────────

CREATE TABLE vendor_price_tier (
  vendor_id            TEXT NOT NULL,
  region               TEXT,                -- us / eu · NULL = 全区
  probed_at            TEXT NOT NULL,       -- 探到这轮 schedule 的时刻
  tier_enabled         INTEGER,             -- 0/1
  tier_active          INTEGER,             -- 0/1（正在降价窗口内）
  tier_interval_min    INTEGER,
  tier_max_reductions  INTEGER,
  tier_applied         INTEGER,
  tier_start_at        TEXT,
  tier_index           INTEGER NOT NULL,    -- 0=base · 1=第一次降 · ...
  effective_at         TEXT NOT NULL,       -- 这档生效时刻
  unit_price_credits   INTEGER NOT NULL,    -- microunit · 这档积分
  unit_price_usd_raw   INTEGER,             -- 这档 USD 原值（有则存）
  created_at           TEXT NOT NULL,
  PRIMARY KEY (vendor_id, region, probed_at, tier_index),
  CHECK (unit_price_credits >= 0)
);

CREATE INDEX idx_vpt_by_vendor ON vendor_price_tier(vendor_id, effective_at);


-- ─── 5. user_subsidy · 减免栈（跟 tier 正交 · 有时效额度）─────

CREATE TABLE user_subsidy (
  id              TEXT PRIMARY KEY,
  passenger_id    TEXT NOT NULL,
  kind            TEXT NOT NULL,           -- channel_fee / service_fee / single_pull / total_discount
  source          TEXT NOT NULL,           -- personal_invite / promo / invite_reward / coupon
  source_ref      TEXT,                    -- 码 id / 奖励 id
  amount_rule     TEXT NOT NULL,           -- JSON · {"kind":"waive"} / {"kind":"pct","pct":10}
  remaining_uses  INTEGER,                 -- NULL = 不限次
  used_count      INTEGER NOT NULL DEFAULT 0,
  expires_at      TEXT,                    -- NULL = 不限时
  created_at      TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id) ON DELETE CASCADE,
  CHECK (kind IN ('channel_fee','service_fee','single_pull','total_discount')),
  CHECK (source IN ('personal_invite','promo','invite_reward','coupon'))
);

CREATE INDEX idx_subsidy_active ON user_subsidy(passenger_id, kind, expires_at);


-- ─── 6. passenger.tier · 三档定价（docs/18 §2.1） ────────────
--
-- SQLite ALTER TABLE 不支持带 CHECK 的加列 · 分两步：
--   加列（默认 retail）· 加索引 · 应用层约束（Go const + 校验）
-- 未来若真需 DB 层 CHECK · 走 rebuild table 那套（当前不值当）

ALTER TABLE passenger ADD COLUMN tier TEXT NOT NULL DEFAULT 'retail';

CREATE INDEX idx_passenger_tier ON passenger(tier);


-- +migrate down

DROP INDEX IF EXISTS idx_passenger_tier;
-- SQLite 不支持 DROP COLUMN · passenger.tier 保留（migration down 只删本轮新表）

DROP INDEX IF EXISTS idx_subsidy_active;
DROP TABLE IF EXISTS user_subsidy;

DROP INDEX IF EXISTS idx_vpt_by_vendor;
DROP TABLE IF EXISTS vendor_price_tier;

DROP INDEX IF EXISTS idx_vp_credits;
-- vendor_probe 加的列同上 · SQLite 不支持删列 · 保留

DELETE FROM vendor_pricing WHERE vendor_id IN
  ('kiro91','kiroceo','kirooo','kiroappio','kiroappcc','kirodrop');

DROP INDEX IF EXISTS idx_exchange_rate_current;
DROP TABLE IF EXISTS exchange_rate;
