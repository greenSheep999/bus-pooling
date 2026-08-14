-- migration 031 · vendor_probe_zone.zone 列语义收口（**不再清 region**）
--
-- **本迁移的历史**：初稿想 `UPDATE region = NULL` 清掉这列 · 理由是"一半 vendor 不返 ·
-- 列常空 · 看着像数据有洞"。**这个判断是错的** —— 查完发现：
--
--   1. `region` 不是死冗余 · 它是**vendor 原文留痕**（3 家给 · 3 家不给 · 空是上游没给的事实）
--   2. 真正的 bug 在别处：stock-delta 对比**拿 region 当键** · 不返 region 的 vendor
--      两个 zone 会塌成一条 · 整区 restock 被漏。已在应用层修（键改用 zone）
--   3. 清了 region 就丢了那 3 家的原文 · 对账时想核"vendor 当时管这区叫什么"就没了
--
-- **所以本迁移只做一件事**：给 zone 列补索引 · 让"按 zone 查最近价"这条主查询走索引。
-- region 列**保留原样** · 语义写进 docs/11-fields.md §3。
--
-- 地区字段的唯一标准是 `zone`（'us' / 'eu' / 'general' · providers.ZoneOf 出口）·
-- `region` 只是 vendor 原文快照 · 允许空 · **不参与任何匹配逻辑**。

-- +migrate up

-- zone 单列索引 · 支撑"某 vendor 某 zone 最近一条有效价"（PricedFor 主查询）
-- 已有的 idx_vpz_vendor_zone_recent 是 (vendor_id, zone, probed_at DESC) ·
-- 这个补的是跨 vendor 按 zone 聚合（对账 / 报表用）
CREATE INDEX IF NOT EXISTS idx_vpz_zone_probed
  ON vendor_probe_zone (zone, probed_at DESC);

-- +migrate down

DROP INDEX IF EXISTS idx_vpz_zone_probed;
