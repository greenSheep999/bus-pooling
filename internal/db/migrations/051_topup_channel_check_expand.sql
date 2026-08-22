-- +migrate up

-- 051 · 扩 topup_order.channel CHECK · 允许 usdt/tron(I-30)。
--
-- **问题**:migration 010 落表 CHECK 只列 (waffo, epusdt, bybit, binance)·
-- 但 topupchannel/channel.go:43-46 后来加了 Waffo/Bybit/Binance/USDT/Tron ·
-- BP_TOPUP_USDT_ENABLED=1 一开 · 下单立即 CHECK 500。
--
-- **修法**:SQLite 不支持 ALTER TABLE MODIFY CHECK · 重建表 · 复制数据 · rename。
-- 新 CHECK 保留 epusdt(兼容老历史行) · 加 usdt/tron(代码新加的)。
--
-- **列**:原表 010 之后陆续 ADD COLUMN(005/016/020/041) · 新表要一并列出:
--   gateway_payment_id/checkout_url/qr_content · gateway_request_snapshot ·
--   fee_waiver_applied/fee_subsidy · coupon_code

CREATE TABLE topup_order_051 (
  id                        TEXT PRIMARY KEY,
  passenger_id              TEXT NOT NULL,
  channel                   TEXT NOT NULL,
  region                    TEXT NOT NULL DEFAULT 'overseas',
  rail                      TEXT NOT NULL DEFAULT 'hosted',
  credits                   INTEGER NOT NULL,
  channel_fee               INTEGER NOT NULL,
  paid                      INTEGER NOT NULL,
  pay_url                   TEXT NOT NULL,
  status                    TEXT NOT NULL,
  expires_at                TEXT NOT NULL,
  paid_at                   TEXT,
  wallet_ledger_id          TEXT,
  gateway_payment_id        TEXT,
  checkout_url              TEXT,
  qr_content                TEXT,
  provider_kind             TEXT,
  payer_reference           TEXT,
  gateway_request_snapshot  BLOB,
  fee_waiver_applied        INTEGER NOT NULL DEFAULT 0,
  fee_subsidy               INTEGER NOT NULL DEFAULT 0,
  coupon_code               TEXT,
  created_at                TEXT NOT NULL,
  updated_at                TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (credits > 0),
  CHECK (channel_fee >= 0),
  CHECK (paid = credits + channel_fee),
  CHECK (region IN ('domestic', 'overseas')),
  CHECK (rail IN ('direct', 'hosted')),
  -- I-30 · 加两个新 channel 值 · 老 channel 保留历史兼容
  CHECK (channel IN ('waffo', 'epusdt', 'bybit', 'binance', 'usdt', 'tron'))
);

-- 显式列名 INSERT · 避免列数变化时脆弱
INSERT INTO topup_order_051 (
  id, passenger_id, channel, region, rail, credits, channel_fee, paid, pay_url,
  status, expires_at, paid_at, wallet_ledger_id, gateway_payment_id,
  checkout_url, qr_content, provider_kind, payer_reference,
  gateway_request_snapshot, fee_waiver_applied, fee_subsidy, coupon_code,
  created_at, updated_at
)
SELECT
  id, passenger_id, channel, region, rail, credits, channel_fee, paid, pay_url,
  status, expires_at, paid_at, wallet_ledger_id, gateway_payment_id,
  checkout_url, qr_content, provider_kind, payer_reference,
  gateway_request_snapshot, fee_waiver_applied, fee_subsidy, coupon_code,
  created_at, updated_at
FROM topup_order;

DROP TABLE topup_order;
ALTER TABLE topup_order_051 RENAME TO topup_order;

-- +migrate down

-- 回退老 CHECK
CREATE TABLE topup_order_051_down (
  id                        TEXT PRIMARY KEY,
  passenger_id              TEXT NOT NULL,
  channel                   TEXT NOT NULL,
  region                    TEXT NOT NULL DEFAULT 'overseas',
  rail                      TEXT NOT NULL DEFAULT 'hosted',
  credits                   INTEGER NOT NULL,
  channel_fee               INTEGER NOT NULL,
  paid                      INTEGER NOT NULL,
  pay_url                   TEXT NOT NULL,
  status                    TEXT NOT NULL,
  expires_at                TEXT NOT NULL,
  paid_at                   TEXT,
  wallet_ledger_id          TEXT,
  gateway_payment_id        TEXT,
  checkout_url              TEXT,
  qr_content                TEXT,
  provider_kind             TEXT,
  payer_reference           TEXT,
  gateway_request_snapshot  BLOB,
  fee_waiver_applied        INTEGER NOT NULL DEFAULT 0,
  fee_subsidy               INTEGER NOT NULL DEFAULT 0,
  coupon_code               TEXT,
  created_at                TEXT NOT NULL,
  updated_at                TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (credits > 0),
  CHECK (channel_fee >= 0),
  CHECK (paid = credits + channel_fee),
  CHECK (region IN ('domestic', 'overseas')),
  CHECK (rail IN ('direct', 'hosted')),
  CHECK (channel IN ('waffo', 'epusdt', 'bybit', 'binance'))
);

INSERT INTO topup_order_051_down (
  id, passenger_id, channel, region, rail, credits, channel_fee, paid, pay_url,
  status, expires_at, paid_at, wallet_ledger_id, gateway_payment_id,
  checkout_url, qr_content, provider_kind, payer_reference,
  gateway_request_snapshot, fee_waiver_applied, fee_subsidy, coupon_code,
  created_at, updated_at
)
SELECT
  id, passenger_id, channel, region, rail, credits, channel_fee, paid, pay_url,
  status, expires_at, paid_at, wallet_ledger_id, gateway_payment_id,
  checkout_url, qr_content, provider_kind, payer_reference,
  gateway_request_snapshot, fee_waiver_applied, fee_subsidy, coupon_code,
  created_at, updated_at
FROM topup_order
WHERE channel IN ('waffo', 'epusdt', 'bybit', 'binance');

DROP TABLE topup_order;
ALTER TABLE topup_order_051_down RENAME TO topup_order;
