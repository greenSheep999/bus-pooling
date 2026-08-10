-- +migrate up

-- 023 · vendor 历史订单 + key 生命周期
--
-- 数据来源：vendor 自己的 /api/my/purchase-orders + /api/my/keys 端点。
-- 我方定期 backfill 全量 · 落这两张表。
--
-- 这两张表是 **/api/vendors/status（公开脱敏）+ /api/vendors/prices（登录带价）**
-- 的共同数据源。避免两个页面对同一批 vendor 数据画出不一样的图（错位）。
--
-- 保留策略：永久保留（一年一 vendor 就几百到几千行，无所谓）。
--
-- 字段选择：
--   - order 层：何时开的一批 / 多少个 / 花了多少（价格）
--   - key 层：单把 key 的生命周期（何时发出、何时挂、活多久、用量 / 用量上限）
-- 前端 status 画"新发 keys 数量随时间"、"平均寿命"曲线用这两张表 JOIN 即可。

CREATE TABLE IF NOT EXISTS vendor_order (
    vendor_id           TEXT NOT NULL,       -- kirooo / kiroceo / 91kiro / kiroappio / kiroappcc / kirodrop
    vendor_order_id     TEXT NOT NULL,       -- vendor 侧的 order_id / orderNo / id · 稳定唯一（同 vendor 内）
    created_at          TEXT NOT NULL,       -- 单在 vendor 侧的创建时间 · RFC3339 UTC
    purchased           INTEGER NOT NULL,    -- 这一批实际拿到的 key 数
    requested           INTEGER,             -- 请求的 key 数（部分 vendor 出 · 允许 NULL）
    unit_price_micro    INTEGER,             -- 单价（vendor 侧的 credit / 币种·内部字段 · 只走 /prices）
    total_cost_micro    INTEGER,             -- 总价（purchased * unit_price）· 内部字段
    source              TEXT,                -- api / manual / 其他 · vendor 自报
    fetched_at          TEXT NOT NULL,       -- 我方拉这条记录的时刻（backfill 时间戳）
    raw                 BLOB,                -- 完整 order JSON · 排查用
    PRIMARY KEY (vendor_id, vendor_order_id)
);

CREATE INDEX IF NOT EXISTS idx_vendor_order_time
    ON vendor_order (vendor_id, created_at DESC);

CREATE TABLE IF NOT EXISTS vendor_key (
    vendor_id            TEXT NOT NULL,      -- 同上
    vendor_key_id        TEXT NOT NULL,      -- vendor 侧的 key_id / id · 稳定唯一（同 vendor 内）
    order_id             TEXT,                -- 关联的 vendor_order.vendor_order_id · 允许 NULL（有 vendor 不关联）
    key_masked           TEXT,                -- 明文 key 的脱敏版（前 8 位 + ****）· **不存明文**
    region               TEXT,                -- us-east-1 / eu-central-1 之类
    status               TEXT NOT NULL,       -- active / dead / suspect / handed_off / unknown
    created_at           TEXT NOT NULL,       -- vendor 侧的 key 创建时间 · RFC3339 UTC
    dispatched_at        TEXT,                -- 派给我方的时间（可能 = created_at）
    dead_at              TEXT,                -- 挂掉的时间 · alive 时 NULL
    dead_reason          TEXT,                -- 挂的原因（vendor 自报 · 譬如 "权限不足"）
    last_probe_at        TEXT,                -- vendor 最后一次探测这个 key 的时间
    current_usage        INTEGER,             -- 已用量（credit / points · vendor 单位）
    usage_limit          INTEGER,             -- 用量上限
    warranty_until       TEXT,                -- 质保结束时间 · vendor 侧的（跟我方 refill window 不同）
    unit_price_micro     INTEGER,             -- 这把 key 单独定价（有 vendor 支持）· 内部字段
    fetched_at           TEXT NOT NULL,       -- 我方拉这条记录的时刻
    raw                  BLOB,                -- 完整 key JSON · 排查用
    PRIMARY KEY (vendor_id, vendor_key_id)
);

CREATE INDEX IF NOT EXISTS idx_vendor_key_time
    ON vendor_key (vendor_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_vendor_key_status
    ON vendor_key (vendor_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_vendor_key_order
    ON vendor_key (vendor_id, order_id);

-- +migrate down

DROP INDEX IF EXISTS idx_vendor_key_order;
DROP INDEX IF EXISTS idx_vendor_key_status;
DROP INDEX IF EXISTS idx_vendor_key_time;
DROP TABLE IF EXISTS vendor_key;

DROP INDEX IF EXISTS idx_vendor_order_time;
DROP TABLE IF EXISTS vendor_order;
