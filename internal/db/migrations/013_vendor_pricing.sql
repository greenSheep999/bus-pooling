-- +migrate up

-- 013_vendor_pricing.sql
--
-- 1b P1-2A · vendor 报价换算表（decisions §8.30 A）
--
-- 目的：不同 vendor 报价币种不同（CNY / USD / 内部积分）·各家换算成"我方积分"
-- 的汇率不一样·**不能硬编到代码**。这张表让运营在后台调汇率·配 vendor 分项。
--
-- 单位统一：1 积分 ≡ 1 CNY（decisions §8.7 · CLAUDE.md §1.4）。
--   quote_currency='CNY' → credits_per_unit = 1_000_000（1:1 · microunit）
--   quote_currency='USD' → credits_per_unit = 汇率 × 1_000_000（例：7 CNY/USD → 7_000_000）
--   quote_currency='credit' → credits_per_unit = 1_000_000（vendor 内部积分·跟我方 1:1）

CREATE TABLE vendor_pricing (
  vendor_id            TEXT PRIMARY KEY,
  quote_currency       TEXT NOT NULL,
  credits_per_unit     INTEGER NOT NULL,           -- microunit · 1 unit(vendor) = X microunit(我方)
  rate_source          TEXT NOT NULL DEFAULT 'manual',
  rate_updated_at      TEXT NOT NULL,
  vendor_surcharge_bp  INTEGER NOT NULL DEFAULT 0, -- vendor 层分项（basis point · 500 = 5%）
  active               INTEGER NOT NULL DEFAULT 1,
  created_at           TEXT NOT NULL,
  updated_at           TEXT NOT NULL,
  CHECK (quote_currency IN ('CNY', 'USD', 'credit')),
  CHECK (credits_per_unit > 0),
  CHECK (vendor_surcharge_bp >= 0),
  CHECK (rate_source IN ('manual', 'api'))
);

CREATE INDEX idx_vp_active ON vendor_pricing(active);

-- 不 seed 具体 vendor 行 —— 运营在后台 / migration hook 里添。默认表空 · decider
-- 找不到行时回落 env（cfg.Vendors * 的 fallback · 保持 1a 行为兼容）。

-- +migrate down
DROP INDEX IF EXISTS idx_vp_active;
DROP TABLE IF EXISTS vendor_pricing;
