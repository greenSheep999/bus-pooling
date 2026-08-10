-- +migrate up

-- 022 · vendor_probe 加 PublicStatus 扩展字段（vendor 自报的 fleet 累计数据）
--
-- 之前的 vendor_probe 只记我方账户的库存视图（Stock 端点）。有些 vendor（本 vendor /
-- 多家 vendor）还有 /api/status 端点，返 fleet-wide 累计状态：
-- keys_active / keys_dead / keys_stock / generating / uptime_seconds / started_at
--
-- 落库策略：跟 Stock 探测同一行 · 都是 poller 一次探测的产物。
-- 没有 /api/status 端点的 vendor（多家 vendor）这些字段为 NULL。
--
-- 前端 /api/vendors/status 响应体从这些字段拼"过去 24h 稳定度 + vendor 自报 keys_active"。

ALTER TABLE vendor_probe ADD COLUMN ps_keys_active     INTEGER;    -- vendor 侧当前活跃 key
ALTER TABLE vendor_probe ADD COLUMN ps_keys_alive      INTEGER;    -- 部分 vendor 才有 · active + suspect
ALTER TABLE vendor_probe ADD COLUMN ps_keys_dead       INTEGER;    -- vendor 侧当前失效 key
ALTER TABLE vendor_probe ADD COLUMN ps_keys_stock      INTEGER;    -- vendor 侧当前可购买库存
ALTER TABLE vendor_probe ADD COLUMN ps_keys_suspect    INTEGER;    -- 部分 vendor 才有 · 探测异常但没判死
ALTER TABLE vendor_probe ADD COLUMN ps_keys_total      INTEGER;    -- 历史累计发过的 key 总数
ALTER TABLE vendor_probe ADD COLUMN ps_generating      INTEGER;    -- 0/1 · vendor 是否正在生成新 key
ALTER TABLE vendor_probe ADD COLUMN ps_started_at      TEXT;       -- vendor 平台启动时间 · RFC3339 UTC
ALTER TABLE vendor_probe ADD COLUMN ps_uptime_seconds  INTEGER;    -- vendor 自报运行时长秒数
ALTER TABLE vendor_probe ADD COLUMN ps_raw             BLOB;       -- 原始 /api/status 响应 · 排查用
ALTER TABLE vendor_probe ADD COLUMN ps_error_kind      TEXT;       -- 空=成功；PublicStatus 端点独立于 Stock 的错误

-- +migrate down

-- SQLite 不支持 DROP COLUMN · 但 021 的 down 会 DROP TABLE，rollback 021 时这几列跟着走
-- 单独 rollback 022 = no-op（不影响 · 字段允许 NULL）
SELECT 1;
