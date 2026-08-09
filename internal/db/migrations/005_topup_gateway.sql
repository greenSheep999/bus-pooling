-- +migrate up

-- 5. 充值单接 404bus-payment-gateway
--
-- 之前 topup_order 里 pay_url 是 mock URL。接了 gateway 之后要多留：
--   gateway_payment_id      gateway 返回的 pay_xxx（内部字段·不出对外接口）
--   gateway_client_order_id 我方生成的 client_order_id（= topup_order.id · 保留列方便日后重命名）
--   checkout_url            gateway.instructions.checkout_url（前端跳转用）
--   qr_content              gateway.instructions.qr_content（有 QR 的 rail 才给）
--
-- pay_url 保留兼容·新流程写 checkout_url。旧行迁上来 pay_url 值抄一份到 checkout_url。

ALTER TABLE topup_order ADD COLUMN gateway_payment_id TEXT;
ALTER TABLE topup_order ADD COLUMN checkout_url TEXT;
ALTER TABLE topup_order ADD COLUMN qr_content TEXT;

UPDATE topup_order SET checkout_url = pay_url WHERE checkout_url IS NULL;

CREATE UNIQUE INDEX idx_topup_gateway_payment_id
  ON topup_order(gateway_payment_id)
  WHERE gateway_payment_id IS NOT NULL;

-- 5.2 settlement_event · gateway 回调幂等表
--
-- gateway 回调 at-least-once·同一个 event_id 可能来多次。我方按 event_id 去重·
-- 已处理过的直接返 200·不再动 wallet。event_id 是 UUID·gateway 生成。
CREATE TABLE settlement_event (
  event_id             TEXT PRIMARY KEY,        -- gateway 生成的 UUID
  gateway_payment_id   TEXT NOT NULL,
  kind                 TEXT NOT NULL,           -- settled | refunded | reversed
  received_at          TEXT NOT NULL,
  outcome              TEXT NOT NULL,           -- accepted | duplicate | unmatched | ignored
  detail               TEXT                     -- 出错时的原因文本
);
CREATE INDEX idx_settlement_event_payment
  ON settlement_event(gateway_payment_id, kind);

-- +migrate down

DROP INDEX IF EXISTS idx_settlement_event_payment;
DROP TABLE IF EXISTS settlement_event;
DROP INDEX IF EXISTS idx_topup_gateway_payment_id;
-- 注意：SQLite 不支持 DROP COLUMN 直到 3.35+·但线上是 3.4x·直接删列。
-- 生产回滚极少走·允许简单实现。
ALTER TABLE topup_order DROP COLUMN qr_content;
ALTER TABLE topup_order DROP COLUMN checkout_url;
ALTER TABLE topup_order DROP COLUMN gateway_payment_id;
