-- 补 credential_ledger 售后追溯三字段（06-db-schema §11.1）
-- 原 001_init.sql 建表时漏了这三列 —— handoff 之后号已从 housepool 删，客服要凭
-- masked key / region / 死时已耗额度对号，只能从台账里读。
--
-- 三个字段在**号入池时**写入（decider settle），handoff / dead 后不再变更（快照语义）。
-- 单独一个迁移 · 只加列，不改语义。

-- +migrate up

ALTER TABLE credential_ledger ADD COLUMN key_masked   TEXT;
ALTER TABLE credential_ledger ADD COLUMN region       TEXT;
ALTER TABLE credential_ledger ADD COLUMN credits_used INTEGER;

-- +migrate down

-- SQLite 3.35+ 支持 DROP COLUMN，但为保守起见（3.35 是 2021 版本）·
-- 无索引 · 单纯的空列，DOWN 阶段不做，回滚不影响读写
