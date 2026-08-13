-- +migrate up

-- 035 · vendor_price_tier 扩展：支持**数量分档**（不止时间降价）
--
-- **背景**：028 建 vendor_price_tier 时只按**时间降价**设计（interval_min / effective_at /
-- tier_index=0/1/…）· 匹配部分 vendor 的时间降价（每 30min 降一档）。但 2026-08-14
-- 实测发现两种"阶梯"模型：
--   - **时间降价**（部分 vendor 的 rounds decay）· 价随时间降
--   - **数量分档**（部分 vendor 的 key-price-tiers 端点 · bands[{lower,upper,price}]）· 价按产量档
-- 数量分档的 lower/upper（数量阈值）是它的核心 · 老表没有列装它。
--
-- 本迁移加：
--   - tier_kind · 'time_decay'（默认 · 老数据）| 'qty_band'
--   - qty_lower / qty_upper · 数量档区间（qty_band 才有 · upper=0 表示"及以上"）
--
-- 时间降价仍走老列（effective_at / tier_interval_min …）· tier_kind='time_decay'。
-- 数量分档走 qty_lower/qty_upper · effective_at 填 probed_at（满足 NOT NULL）·
-- 时间列留 NULL。
--
-- **纯内部 pricing 数据**（不直接出前端 · 经 vendorview 脱敏后才展示）。

ALTER TABLE vendor_price_tier ADD COLUMN tier_kind TEXT NOT NULL DEFAULT 'time_decay';
ALTER TABLE vendor_price_tier ADD COLUMN qty_lower INTEGER;
ALTER TABLE vendor_price_tier ADD COLUMN qty_upper INTEGER;

-- +migrate down

-- SQLite 不支持 DROP COLUMN（旧版）· 用重建表回滚
CREATE TABLE vendor_price_tier_old (
  vendor_id            TEXT NOT NULL,
  region               TEXT,
  probed_at            TEXT NOT NULL,
  tier_enabled         INTEGER,
  tier_active          INTEGER,
  tier_interval_min    INTEGER,
  tier_max_reductions  INTEGER,
  tier_applied         INTEGER,
  tier_start_at        TEXT,
  tier_index           INTEGER NOT NULL,
  effective_at         TEXT NOT NULL,
  unit_price_credits   INTEGER NOT NULL,
  unit_price_usd_raw   INTEGER,
  created_at           TEXT NOT NULL,
  PRIMARY KEY (vendor_id, region, probed_at, tier_index),
  CHECK (unit_price_credits >= 0)
);
INSERT INTO vendor_price_tier_old
  SELECT vendor_id, region, probed_at, tier_enabled, tier_active, tier_interval_min,
         tier_max_reductions, tier_applied, tier_start_at, tier_index, effective_at,
         unit_price_credits, unit_price_usd_raw, created_at
    FROM vendor_price_tier WHERE tier_kind = 'time_decay';
DROP INDEX IF EXISTS idx_vpt_by_vendor;
DROP TABLE vendor_price_tier;
ALTER TABLE vendor_price_tier_old RENAME TO vendor_price_tier;
CREATE INDEX idx_vpt_by_vendor ON vendor_price_tier(vendor_id, effective_at);
