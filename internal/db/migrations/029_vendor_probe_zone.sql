-- migration 029 · vendor_probe_zone · 每次探针每 zone 一行
--
-- **为什么单开侧表**（docs/18 §5 未收口那条 · 2026-08-12 补齐）：
-- vendor_probe 主键 (vendor_id, probed_at) 每探针一行 · 8 新列采样首个 zone ·
-- US / EU 差价被压平（部分 vendor US 比 EU 贵 30~50% · 计费不该按"大概"）。
--
-- 侧表逐 zone 落 · PricedFor(vendor, region) 的 region 参数真的生效。
-- 主表 8 列保留（首 zone 采样 + 兼容老读取 + 便于跨 vendor 概览）·
-- **权威值在侧表** —— 精确定价一律读这里。

-- +migrate up

CREATE TABLE vendor_probe_zone (
  vendor_id            TEXT NOT NULL,
  probed_at            TEXT NOT NULL,   -- 跟 vendor_probe.probed_at 严格一致
  zone                 TEXT NOT NULL,   -- 归一后的 · us / eu / '' · providers.ZoneOf 出口
  region               TEXT,            -- vendor 原文 · us-east-1 / 美国区 / '' · 便于对账
  available            INTEGER,         -- 该 zone 库存
  vendor_currency      TEXT,            -- vendor 报价币种 · USD / credit / CNY
  vendor_unit_raw      INTEGER,         -- vendor 原始单价 microunit（币种由 vendor_currency 定）
  our_unit_credits     INTEGER,         -- 我方积分 microunit · 已按 vendor_pricing 换算好 · 权威值
  our_unit_source      TEXT,            -- vendor_native / computed_from_usd · 排查用
  PRIMARY KEY (vendor_id, probed_at, zone)
);

-- 查最近某 zone 的价 · PricedFor 的主查询
CREATE INDEX idx_vpz_vendor_zone_recent
  ON vendor_probe_zone (vendor_id, zone, probed_at DESC);

-- 有价才查（PricedFor 只关心 our_unit_credits > 0 的行）
CREATE INDEX idx_vpz_priced
  ON vendor_probe_zone (vendor_id, zone, probed_at DESC)
  WHERE our_unit_credits IS NOT NULL AND our_unit_credits > 0;

-- +migrate down

DROP INDEX IF EXISTS idx_vpz_priced;
DROP INDEX IF EXISTS idx_vpz_vendor_zone_recent;
DROP TABLE IF EXISTS vendor_probe_zone;
