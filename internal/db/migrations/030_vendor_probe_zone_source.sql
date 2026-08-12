-- migration 030 · vendor_probe_zone 加 source 列
--
-- **背景**：xi8 `/api/vendors` 也是逐 zone 单价源（可交叉核对 · 补 vendor 官端只给单 zone
-- 的部分 vendor 的 EU 定价 · docs/decisions §11.11）。两个价格源都写侧表 · 得能区分。
--
-- **约定** · 跟 vendor_dispatch.source 一样的口径：
--   - `vendor_self` · Prober 从 vendor 官 API 拉的（权威）
--   - `xi8` · xi8 backfiller 拉的（第二源 · 交叉核对 + 补空）
--
-- **主键改动**：加入 source 列（vendor_id, probed_at, zone, source）· 允许同一时刻
-- 两个源都落一行 · PricedFor 查最近一条时可以按 source 过滤。

-- +migrate up

ALTER TABLE vendor_probe_zone ADD COLUMN source TEXT NOT NULL DEFAULT 'vendor_self';

-- 老数据全部标 vendor_self（本来就是 Prober 落的）
-- SQLite ALTER TABLE ADD COLUMN 加 DEFAULT 时已经填了 · 无需 UPDATE

-- 按 source 过滤的查询 · PricedFor 权威路径要 source='vendor_self'
CREATE INDEX idx_vpz_source_recent
  ON vendor_probe_zone (vendor_id, zone, source, probed_at DESC)
  WHERE our_unit_credits IS NOT NULL AND our_unit_credits > 0;

-- +migrate down

DROP INDEX IF EXISTS idx_vpz_source_recent;
-- SQLite 不支持 DROP COLUMN · source 列保留（老 code 会写 NULL · 无害）
