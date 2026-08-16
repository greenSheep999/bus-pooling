-- +migrate up

-- 041_topup_coupon_code.sql · topup_order 加 coupon_code 列 · decisions §8.43
--
-- 社群发放的一次性充值优惠码 · 减实付 USD · 不加积分。
-- 阶段 1(sprint-1e)先落库不算减免 · 减免规则在 sprint-1f 起手接。
-- coupon_code 表本身在 §8.32/§8.42 已规划 · 本迁移只在 topup_order 上关联 · 校验 / 减免逻辑独立迭代。

ALTER TABLE topup_order ADD COLUMN coupon_code TEXT;

-- 无索引:阶段 1 只落库 · 无查询驱动 · 后续减免核销时按需加
--          按 coupon 追溯用量走 wallet_ledger.reason=coupon_discount(未来加)

-- +migrate down

-- SQLite 3.35+ 支持 DROP COLUMN · 单纯空列保留不影响读写
-- 但 TestMigrateDownDropsEverything 要求 down 清干净 · 显式 DROP 保测试契约
ALTER TABLE topup_order DROP COLUMN coupon_code;
