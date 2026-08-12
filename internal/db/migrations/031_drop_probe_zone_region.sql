-- migration 031 · 停用 vendor_probe_zone.region 列
--
-- **为什么**：这列原意是"存 vendor 官方 region 原文用作对账" · 但：
--   - 一半 vendor 官方 API 就不返这个字段（列常空）· 让人以为"数据有洞"
--   - 原文 vendor_probe.raw_snapshot 里已经全存了 · 对账去那捞就行
--   - 归一后的标准地区字段是 `zone`（'us' / 'eu' · providers.ZoneOf 出口）· 一个就够
--
-- **改动**：
--   - 存量：UPDATE region = NULL · 清掉噪音
--   - SQLite 不支持 DROP COLUMN · 列保留（下次 rebuild table 时删）· 应用层停写 + 停读

-- +migrate up

UPDATE vendor_probe_zone SET region = NULL WHERE region IS NOT NULL;

-- +migrate down

-- 无从恢复（原文丢了）· down 是 no-op · region 列本来就允许 NULL
SELECT 1;
