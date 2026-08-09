-- +migrate up

-- 016_topup_gateway_snapshot.sql
--
-- P0 修（codex 三轮）：janitor 反查 gateway 时·必须用起单时的原始 request 快照
-- 重新 POST（同 client_order_id · 命中 gateway 幂等表 · 200 replay）。
--
-- 若从当前 config 重建 request（旧实现）·汇率 / channel 配置 / payer_email 都
-- 可能已经变了 · gateway 幂等指纹不同 → 命中新单而不是 replay · 语义错。
--
-- BLOB 存 JSON（paymentgw.CreatePaymentRequest 序列化）· NULL 兼容旧行（旧行
-- 由 janitor 走 pending_manual 分支 · 不 replay）。

ALTER TABLE topup_order ADD COLUMN gateway_request_snapshot BLOB;

-- +migrate down

-- SQLite < 3.35 不支持 DROP COLUMN · 但 3.35+（2021）支持 · 项目最低 SQLite
-- 版本已经在 3.35 之后（WAL / partial index 都在用）。若回滚遇到老库直接
-- rebuild table 也行 · 阶段 1a 单机 · 手工也能扛。

ALTER TABLE topup_order DROP COLUMN gateway_request_snapshot;
