-- +migrate up

-- 033 · vendor_ledger · vendor 侧积分流水（交叉对账 · docs/20 §1）
--
-- **为什么**：生产已开 dry_run=false 真实扣费 · 但我方只有自己的账本
-- （pull_round + wallet_ledger）· 拿不到 vendor 侧流水做双向核对 —— 被多扣 /
-- 漏退都发现不了。这张表存 vendor 自报的每笔流水 · 对账 job 拿它跟 pull_round 比。
--
-- **纯内部**（CLAUDE.md §0.1）· 绝不出前端 · 只做后台对账。
--
-- 幂等主键 (vendor_id, entry_id)：
--   - entry_id 优先用 vendor 稳定流水 id
--   - vendor 不给 id 的 · adapter 用 (created_at+reason+amount) 合成指纹（同笔重拉不重复）
--
-- amount_micro 带符号：**扣费为负 · 入账为正**（adapter 统一口径 · 各家原文口径不一）。
-- raw 永远存：字段推断不准时（vendor 不公开 schema）· raw 是唯一可信的原文。

CREATE TABLE IF NOT EXISTS vendor_ledger (
    vendor_id        TEXT    NOT NULL,
    entry_id         TEXT    NOT NULL,             -- vendor 流水 id 或合成指纹
    order_id         TEXT,                          -- 关联订单号 · 对账 join 键（purchase/refund 有）
    reason           TEXT    NOT NULL,              -- 归一：purchase/refund/recharge/income/adjust/other
    raw_reason       TEXT,                          -- vendor 原文 reason
    amount_micro     INTEGER NOT NULL,              -- 带符号 · 扣费负 / 入账正
    balance_after    INTEGER,                       -- 该笔后余额（vendor 支持时）
    created_at       TEXT    NOT NULL,              -- vendor 侧时刻 · RFC3339 UTC
    fetched_at       TEXT    NOT NULL,              -- 我方拉取时刻
    raw              BLOB,                          -- 完整流水 JSON · 对账排查
    PRIMARY KEY (vendor_id, entry_id)
);

-- 对账主查询：按 order_id join 我方 pull_round.vendor_order_id
CREATE INDEX IF NOT EXISTS idx_vendor_ledger_order
    ON vendor_ledger (vendor_id, order_id);

-- 按类型+时间扫（对账 job 只关心 purchase/refund）
CREATE INDEX IF NOT EXISTS idx_vendor_ledger_reason_time
    ON vendor_ledger (vendor_id, reason, created_at DESC);

-- +migrate down

DROP INDEX IF EXISTS idx_vendor_ledger_reason_time;
DROP INDEX IF EXISTS idx_vendor_ledger_order;
DROP TABLE IF EXISTS vendor_ledger;
