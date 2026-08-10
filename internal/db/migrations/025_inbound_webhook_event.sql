-- +migrate up

-- inbound_webhook_event · vendor 推给我方的事件日志 + (vendor_id, event_id) 幂等去重。
--
-- 为什么要这张表：
--   vendor 侧网络抖动会重推同 event_id（他们的 retry 策略）· 我方不能对同一次
--   开号 / 号死 / 退款处理两遍（重复退款是重大 bug）。落表 + 主键 UPSERT 保证幂等。
--
-- 存**已解析的归一化事件字段**（EventType 分类后 · 分派器要用的东西） · **不存原始 body**
-- （太大 · 明文里可能有 key payload · 隐私风险）· 需要原始 body 时看 docker log。
--
-- 分派语义（webhookin.Dispatch 用）：
--   - new_keys_available → 落 vendor_dispatch（fleet 视图立即刷新）
--   - all_keys_dead      → 触发 deathwatch.SweepOnce（提前号死处理 · 不等 5min ticker）
--   - warranty_refund    → deathwatch.RefundOnce（走 bus_member share_pct 分摊）
--   - webhook_test       → 只 log · 不动业务
--
-- **永久保留**（decisions §11.x A 方案）· 磁盘廉价 · 售后追溯不能丢。

CREATE TABLE IF NOT EXISTS inbound_webhook_event (
    vendor_id       TEXT    NOT NULL,   -- 6 家 vendor slug
    event_id        TEXT    NOT NULL,   -- vendor 推的唯一 id · 幂等主键
    event_type      TEXT    NOT NULL,   -- new_keys_available | all_keys_dead | warranty_refund | test | other
    order_id        TEXT,               -- vendor 开号批次 id
    purchase_order_id TEXT,             -- 我方 client_order_id（如果有）
    new_keys        INTEGER,            -- new_keys_available 事件的数量
    dead_keys       INTEGER,            -- all_keys_dead 事件的数量
    refund_micro    INTEGER,            -- warranty_refund 的退款 micro 金额
    zone            TEXT,               -- us | eu · 空 = 无区
    received_at     TEXT    NOT NULL,   -- 我方收到时刻 RFC3339 UTC
    dispatched_at   TEXT,               -- 后处理完成时刻 · 空 = 收到但没派单成功
    dispatch_status TEXT,               -- ok | skipped | error · 空 = 未派
    dispatch_error  TEXT,               -- 派单出错时的信息（不敏感 · 前 200 字）
    PRIMARY KEY (vendor_id, event_id)
);

CREATE INDEX IF NOT EXISTS idx_inbound_webhook_recent
    ON inbound_webhook_event (received_at DESC);
CREATE INDEX IF NOT EXISTS idx_inbound_webhook_vendor_type
    ON inbound_webhook_event (vendor_id, event_type, received_at DESC);

-- +migrate down

DROP INDEX IF EXISTS idx_inbound_webhook_vendor_type;
DROP INDEX IF EXISTS idx_inbound_webhook_recent;
DROP TABLE IF EXISTS inbound_webhook_event;
