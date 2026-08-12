-- +migrate up

-- 027 · stock_watcher · 抢号链核心表（docs/16-buy-race.md · decisions §11.7）
--
-- 场景：拉号时 vendor 缺货 · 不直接失败退款 · auto 模式下挂 watcher · 等 restock 事件
-- （vendor webhook new_keys / xi8 signals / 探针 stock-delta 推算）唤醒 → 立即 fire
-- decider.Purchase · 抢到就 fulfilled · 抢不到（涨价 / 又抢空）继续等或 TTL 到期退款。
--
-- 幂等：一个 pull_intent 只挂一条 stock_watcher · 多个信号源同时 fire 只成一单
-- （靠 pull_intent.client_order_id · vendor 侧同 order_id 重放不重复扣）。
--
-- **只 auto 模式挂** —— 用户明确指定 vendor 时（preferred_vendor）· 缺货直接失败
-- （他要等的是那家 · 不是别家）· 不代抢。

CREATE TABLE stock_watcher (
    intent_id       TEXT PRIMARY KEY,               -- pull_intent(id) · 一对一 · CASCADE 删
    vendor_id       TEXT NOT NULL,                  -- 等哪家 vendor · auto 时初始按 auto-pick 结果
    region          TEXT,                           -- 特定 region · NULL = 任意 region 都行
    max_unit_price  INTEGER,                        -- microunit · 涨价保护 · NULL = 不限
    count           INTEGER NOT NULL,               -- 要几个 key
    started_at      TEXT NOT NULL,                  -- RFC3339 UTC · 挂单时刻
    expires_at      TEXT NOT NULL,                  -- TTL 到期自动 → expired 退款
    status          TEXT NOT NULL DEFAULT 'watching', -- watching / fired / fulfilled / expired / cancelled
    fired_at        TEXT,                           -- fire 触发时刻
    fired_reason    TEXT,                           -- webhook / xi8_signal / stock_delta / retry
    fire_count      INTEGER NOT NULL DEFAULT 0,     -- fire 次数 · 一批失败可再试 · 防 spam
    FOREIGN KEY (intent_id) REFERENCES pull_intent(id) ON DELETE CASCADE,
    CHECK (status IN ('watching','fired','fulfilled','expired','cancelled'))
);

-- 唤醒扫热路径：某 vendor 有 restock 事件 → 找 (vendor_id, status='watching') 按 started_at 队列
CREATE INDEX idx_stock_watcher_active
    ON stock_watcher (vendor_id, status, started_at);

-- TTL sweep：找过期未 fire 的行 · janitor 定时扫
CREATE INDEX idx_stock_watcher_ttl
    ON stock_watcher (status, expires_at);

-- +migrate down

DROP INDEX IF EXISTS idx_stock_watcher_ttl;
DROP INDEX IF EXISTS idx_stock_watcher_active;
DROP TABLE IF EXISTS stock_watcher;
