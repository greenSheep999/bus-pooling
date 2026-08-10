-- +migrate up

-- 024 · vendor_dispatch = vendor 侧的"每批开号"记录（fleet-wide 时序）
--
-- 跟 vendor_order 的区别：
--   - vendor_order 是"**我方账户**下的订单"（我们买过什么）· 大多是空的
--   - vendor_dispatch 是"**vendor 平台整体**发过的每批 key"· 全网可见 · 每家都有量
--
-- 从各家 vendor 拉：
--   - kirooo: /api/my/stock/regions → regions[].dispatches[]（含 alive/dead/dead_at）
--   - kiroceo: /api/my/gen-logs → items[] （time/count/status · 简约版）
--   - 其他 vendor: 各自的端点
--
-- 用于 /api/vendors/status 每张 vendor 卡的"最近开号"曲线 · 6 家都有数据。

CREATE TABLE IF NOT EXISTS vendor_dispatch (
    vendor_id       TEXT NOT NULL,      -- 91kiro / kiroceo / …
    dispatch_key    TEXT NOT NULL,      -- vendor_id 内稳定的批次标识（time 字符串就够）
    region          TEXT,               -- us-east-1 / eu-central-1 · 单区 vendor 为空
    dispatched_at   TEXT NOT NULL,      -- 这批发出时刻 · RFC3339 UTC
    count           INTEGER NOT NULL,   -- 这批发了多少个 key（"delivered" / "count"）
    alive           INTEGER,            -- 现在还活着几个（vendor 支持时填）
    dead            INTEGER,            -- 挂了几个
    dead_at         TEXT,               -- 全批挂完的时刻（有则填）
    status          TEXT,               -- running / done / dead 之类 · vendor 自报
    fetched_at      TEXT NOT NULL,      -- 我方拉这条记录的时刻
    raw             BLOB,               -- 完整 dispatch JSON · 排查用
    PRIMARY KEY (vendor_id, dispatch_key)
);

CREATE INDEX IF NOT EXISTS idx_vendor_dispatch_time
    ON vendor_dispatch (vendor_id, dispatched_at DESC);

-- +migrate down

DROP INDEX IF EXISTS idx_vendor_dispatch_time;
DROP TABLE IF EXISTS vendor_dispatch;
