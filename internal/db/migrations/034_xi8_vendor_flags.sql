-- +migrate up

-- 034 · xi8_vendor_flags · 聚合源的 buyable/blocked/floating 快照（抢号决策 · docs/20 §3）
--
-- **为什么**：xi8 `/api/vendors` 每 5min 拉的 VendorRegion 里带 buyable/blocked/
-- block_reason/floating —— 这几个探针给不了（探针只看得到"我方账户能买几个"·
-- 看不到 vendor 是否**主动停售**）。老代码 pushVendorsToZone 只落价格 · 把这几个 flag
-- 全丢了。这张表捞起来 · 喂抢号 fire-guard：
--   - blocked=1 → **别 fire**（vendor 成本可疑主动停售 · fire 必被拒 · 白烧一次尝试）
--   - floating=1 → fire 必带单价上限保护（价随成本浮动 · 不带保护 = 涨到多少都买）
--
-- **纯内部**（CLAUDE.md §0.1）· 不出前端。
--
-- **最新快照**（不是历史）：每 vendor+zone 一行 · upsert 覆盖 · 全表最多 6×2=12 行。
-- xi8 只覆盖 5 家（kiroappcc 不在 xi8）· 查不到 flag 的 vendor **默认不拦**（fail-open ·
-- 宁可多试一次 · 不要因为聚合源没数据把能抢的也拦了）。

CREATE TABLE IF NOT EXISTS xi8_vendor_flags (
    vendor_id     TEXT    NOT NULL,          -- 我方 slug（xi8 vendor_id 已映射）
    zone          TEXT    NOT NULL,          -- us / eu / general
    buyable       INTEGER NOT NULL DEFAULT 0,
    blocked       INTEGER NOT NULL DEFAULT 0,
    block_reason  TEXT,
    floating      INTEGER NOT NULL DEFAULT 0,
    price_fen     INTEGER,                    -- xi8 报价（分 CNY）· 留证
    updated_at    TEXT    NOT NULL,           -- 我方拉取时刻 · 判新鲜度（太旧的 flag 不该信）
    PRIMARY KEY (vendor_id, zone)
);

-- +migrate down

DROP TABLE IF EXISTS xi8_vendor_flags;
