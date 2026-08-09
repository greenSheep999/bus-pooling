-- +migrate up

-- 018_warranty_refund.sql
--
-- 1c · 质保退款落地（00 §7.5 规则 B）。
--
-- 之前 ReasonWarrantyRefund 只在 ledger 展示映射里出现·deathwatch 只标死号不退钱 ——
-- 等于上游 vendor 在质保窗口内退了我方，我方吞了没退乘客。
--
-- warranty_refunded_at 是**幂等锚点**：
--   条件 UPDATE（... WHERE warranty_refunded_at IS NULL）抢锁·抢到才入账·
--   保证同一个号的质保退款**永不重复给钱**（并发 / janitor 重跑都安全）。
--
-- 为什么不用单独的 refund 表：一个号最多退一次·一列足够。多退一次就是资金事故·
-- 用列 + 条件 UPDATE 比用表 + 唯一索引更难写错。

ALTER TABLE credential_ledger ADD COLUMN warranty_refunded_at TEXT;

-- 扫待退款号用（status='dead' + 未退 + 上游已退）
CREATE INDEX IF NOT EXISTS idx_credential_warranty_refund
  ON credential_ledger (status, warranty_refunded_at, dead_at);

-- +migrate down

DROP INDEX IF EXISTS idx_credential_warranty_refund;
ALTER TABLE credential_ledger DROP COLUMN warranty_refunded_at;
