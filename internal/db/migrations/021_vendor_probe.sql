-- +migrate up

-- 021 · vendor 状态探针 + 日聚合
--
-- 为 /status 公开页 + /overview 内 vendor monitor + /prices 内单价矩阵提供数据源。
-- 我方后端跑 poller · 用运维 credential（BP_VENDOR_PROBE_KEY_* env）打上游 Stock ·
-- 结果落 vendor_probe（分钟级明细）· 每天 UTC 00:00 聚合成 vendor_daily。
--
-- 前端 /api/vendors/status 公开端点从这两张表读，不实时打上游：
--   - vendor 频繁 rate-limit，未登录流量不可控不能透传
--   - 24h uptime% / stockout minutes / lifespan 等指标需要历史样本才能算
--
-- 字段设计对齐 internal/providers/vendor.go 的 StockSnapshot + Capability，
-- 加两个内部字段（sample_price / sampled_lifespan）不出对外响应，只走 /prices。
--
-- 保留策略：
--   - vendor_probe：30 天滚动删（janitor 顺带清 · 分钟级样本量大）
--   - vendor_daily：永久保留（每天一行 · 每 vendor 一年 365 行，无所谓）

CREATE TABLE IF NOT EXISTS vendor_probe (
    vendor_id            TEXT    NOT NULL,   -- vendor slug（见 术语铁律 §1.1）
    probed_at            TEXT    NOT NULL,   -- RFC3339 UTC · 前端不看它，内部 range 查询用
    alive                INTEGER NOT NULL,   -- 0/1 · vendor.Stock 成功返回即 1
    latency_ms           INTEGER,            -- 探测响应时延（成功时才有意义）
    stock_total          INTEGER,            -- StockSnapshot.Available · 总可购数
    stock_by_region      TEXT,               -- JSON [{region, available, unit_price_micro}] · 用于分区档位
    warranty_minutes     INTEGER,            -- Capability.WarrantyMinutes · 质保时长
    max_per_order        INTEGER,            -- Capability.MaxPerOrder · 单次上限
    sample_price_micro   INTEGER,            -- 首个 zone 的单价 · **内部字段** · 不出 /status
    sample_price_region  TEXT,               -- 采样 zone id · 内部字段
    error_kind           TEXT,               -- 空=成功；否则 timeout / auth / http_5xx / http_4xx / other
    raw_snapshot         BLOB,               -- 完整 StockSnapshot JSON · 排查用
    PRIMARY KEY (vendor_id, probed_at)
);

CREATE INDEX IF NOT EXISTS idx_vendor_probe_recent
    ON vendor_probe (probed_at DESC);

CREATE INDEX IF NOT EXISTS idx_vendor_probe_alive
    ON vendor_probe (vendor_id, alive, probed_at DESC);

CREATE TABLE IF NOT EXISTS vendor_daily (
    vendor_id                 TEXT NOT NULL,
    date                      TEXT NOT NULL,          -- YYYY-MM-DD · UTC 边界
    uptime_pct                REAL NOT NULL,          -- 0.0-1.0 · 当天 alive / total 探测占比
    stock_avg                 REAL,                    -- 当天均值 · 库存量趋势
    stock_min                 INTEGER,                 -- 当天最低 · 触发缺货警报的依据
    stockout_minutes          INTEGER,                 -- 库存为 0 的累计分钟数
    median_price_micro        INTEGER,                 -- 中位数单价 · **内部字段**
    median_price_by_region    TEXT,                    -- JSON {region: median_micro}
    sampled_lifespan_avg_sec  INTEGER,                 -- KeyHealth 平均寿命秒数 · **内部字段**
    warranty_minutes          INTEGER,                 -- 当天 Capability.WarrantyMinutes（罕见变动）
    incident_flag             INTEGER NOT NULL DEFAULT 0,  -- 0/1 · uptime<95% 或 stockout>60min
    PRIMARY KEY (vendor_id, date)
);

CREATE INDEX IF NOT EXISTS idx_vendor_daily_recent
    ON vendor_daily (date DESC);

-- +migrate down

DROP INDEX IF EXISTS idx_vendor_daily_recent;
DROP TABLE IF EXISTS vendor_daily;

DROP INDEX IF EXISTS idx_vendor_probe_alive;
DROP INDEX IF EXISTS idx_vendor_probe_recent;
DROP TABLE IF EXISTS vendor_probe;
