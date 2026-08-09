-- +migrate up

-- 020_topup_fee_waiver.sql
--
-- 1c · 个人邀请码的手续费减免**实际生效**（decisions §8.29 / §8.32）。
--
-- migration 019 只建了额度字段·本迁移补上"充值时真的抵扣"所需的两列。
--
-- 减免在**起单时**消耗 · 不在 MarkPaid —— 订单金额（含二维码/跳转链接携带的）
-- 在建单时就固定，付款后不可变。
--
-- fee_subsidy 单独一列（不并进 channel_fee）· 口径见 decisions §8.32。
--
-- 额度归还：订单 expired / cancelled / refunded 时退回（topup.returnFeeWaiverForOrderTx）。

ALTER TABLE topup_order ADD COLUMN fee_waiver_applied INTEGER NOT NULL DEFAULT 0;
ALTER TABLE topup_order ADD COLUMN fee_subsidy INTEGER NOT NULL DEFAULT 0;

-- +migrate down

ALTER TABLE topup_order DROP COLUMN fee_subsidy;
ALTER TABLE topup_order DROP COLUMN fee_waiver_applied;
