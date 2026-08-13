-- +migrate up

-- 036 · pipeline_health · 各数据管线的"最后成功/最后失败"心跳（可观测性）
--
-- **为什么**：/healthz 只查 DB ping（HTTP 活着）· 不查数据在不在更新。某条管线
-- 静默停更（vendor 改响应形状 / token 过期 / 端点下线）时容器照样"健康"· 数据变旧
-- 没人知道。这张表让每条管线每轮跑完盖个戳 · StalenessChecker + admin 端点据此发现
-- "谁多久没成功了"。
--
-- **纯内部/运维**（CLAUDE.md §0.1）· 不出乘客前端 · 只喂运维视角。
--
-- 主键 (vendor_id, pipeline)：
--   - vendor_id='' 表示全局管线（xi8 / daily_rollup 等不分 vendor 的）
--   - pipeline：probe / orders / keys / dispatch / ledger / qty_tiers / time_decay ...
--
-- last_ok_at 是新鲜度判据（对比"现在 - last_ok_at"和该管线预期间隔）· last_err 留最近一次
-- 失败摘要（成功不清 · 便于看"上次为啥挂过"）。

CREATE TABLE IF NOT EXISTS pipeline_health (
    vendor_id   TEXT NOT NULL DEFAULT '',   -- '' = 全局管线
    pipeline    TEXT NOT NULL,              -- probe / orders / keys / dispatch / ledger / qty_tiers / time_decay
    last_ok_at  TEXT,                        -- 最后成功 · RFC3339 UTC
    last_err    TEXT,                        -- 最后错误摘要（截断）· 成功不清
    last_err_at TEXT,                        -- 最后错误时刻 · RFC3339 UTC
    updated_at  TEXT NOT NULL,               -- 本行最后一次写入时刻
    PRIMARY KEY (vendor_id, pipeline)
);

-- +migrate down

DROP TABLE IF EXISTS pipeline_health;
