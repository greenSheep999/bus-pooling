-- +migrate up

-- 8. pending_handoff 加 retry_count · janitor 重试计数落库
--
-- 之前 janitor 的 confirmed 重试计数放在内存 map[string]int·
-- 服务重启就清零 · 一个 confirmed 卡单会被无限重试。文档要求 3 次转 need_manual
-- （docs/09-transactions.md:195）· 实现要能跨重启保持计数。
--
-- 加列而不重建表·列有默认值 0 兼容旧行。

ALTER TABLE pending_handoff ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0;

-- +migrate down

-- SQLite 3.35+ 才支持 DROP COLUMN·线上环境版本够·直接 drop
ALTER TABLE pending_handoff DROP COLUMN retry_count;
