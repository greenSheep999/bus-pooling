-- +migrate up

-- 010_topup_multichannel.sql
--
-- 1b · 目标：
--   1. topup_order 支持 3 大类 topup_type × 4 具体 channel（waffo 启 · 其他 3 家关但通道预留）·
--      放松 CHECK · 加 provider_kind / payer_reference 列供 gateway 用
--   2. 建 pending_topup 状态机表（09-transactions §6）· webhook 主推进 + janitor 兜
--
-- 三个正交维度（前端可任意维度过滤 · 后端也用这三维决定 gateway 参数）：
--   1. channel   · 具体渠道名（waffo / epusdt / bybit / binance / ...）
--   2. region    · 地区（domestic 国内 · overseas 海外）
--   3. rail      · 到账方式（direct 直连转账·乘客直转我方对账 / hosted 三方托管·跳走 checkout）
--
-- 四家渠道属性（1b 阶段·当前全 overseas）：
--   channel   | region   | rail    | provider_kind        | 状态
--   ----------|----------|---------|----------------------|-----
--   waffo     | overseas | hosted  | waffo_checkout       | 启
--   epusdt    | overseas | direct  | epusdt_onchain       | 关（预留）
--   bybit     | overseas | direct  | bybit_internal       | 关（预留）
--   binance   | overseas | direct  | binance_internal     | 关（预留）
--
-- 未来加 domestic 渠道时（支付宝 / 微信等·可能既是 hosted 也是 direct）·
-- 只加行 · 不改 schema。
--
-- payer_reference（gateway 要求 · 直连转账 rails 强烈建议）：
--   waffo   → 乘客 email（乘客 profile 已知）
--   bybit   → 乘客提供的 Bybit UID
--   binance → 乘客提供的 Binance ID
--   epusdt  → 乘客提供的钱包地址（可选·帮助 disambiguation）

-- ─ 1) topup_order rebuild in-place · 加 region / topup_type / provider_kind / payer_reference ─
CREATE TABLE topup_order_new (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  channel                TEXT NOT NULL,                        -- waffo | epusdt | bybit | binance | ...
  region                 TEXT NOT NULL DEFAULT 'overseas',    -- domestic | overseas
  rail                   TEXT NOT NULL DEFAULT 'hosted',      -- direct（乘客直转对账）| hosted（跳三方 checkout）
  credits                INTEGER NOT NULL,
  channel_fee            INTEGER NOT NULL,
  paid                   INTEGER NOT NULL,
  pay_url                TEXT NOT NULL,
  status                 TEXT NOT NULL,
  expires_at             TEXT NOT NULL,
  paid_at                TEXT,
  wallet_ledger_id       TEXT,
  gateway_payment_id     TEXT,
  checkout_url           TEXT,
  qr_content             TEXT,
  provider_kind          TEXT,                                 -- payment-Gateway rail · null 时按 channel 推
  payer_reference        TEXT,                                 -- 按 channel 变·gateway 去重匹配用
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (credits > 0),
  CHECK (channel_fee >= 0),
  CHECK (paid = credits + channel_fee),
  CHECK (region IN ('domestic', 'overseas')),
  CHECK (rail IN ('direct', 'hosted')),
  CHECK (channel IN ('waffo', 'epusdt', 'bybit', 'binance'))
  -- status CHECK 006 rebuild 时已松 · 这里不再重复
);

INSERT INTO topup_order_new (
  id, passenger_id, channel, region, rail, credits, channel_fee, paid, pay_url,
  status, expires_at, paid_at, wallet_ledger_id, gateway_payment_id,
  checkout_url, qr_content, provider_kind, payer_reference,
  created_at, updated_at
)
SELECT
  id, passenger_id, channel,
  'overseas',                                                  -- 历史 waffo 全是海外
  'hosted',                                                    -- 历史 waffo 都是 hosted checkout
  credits, channel_fee, paid, pay_url,
  status, expires_at, paid_at, wallet_ledger_id, gateway_payment_id,
  checkout_url, qr_content,
  CASE WHEN channel = 'waffo' THEN 'waffo_checkout' ELSE NULL END,
  NULL,
  created_at, updated_at
FROM topup_order;

DROP TABLE topup_order;
ALTER TABLE topup_order_new RENAME TO topup_order;
CREATE INDEX idx_topup_passenger_time ON topup_order(passenger_id, created_at DESC);
CREATE INDEX idx_topup_status ON topup_order(status, expires_at);
CREATE INDEX idx_topup_gateway_pid ON topup_order(gateway_payment_id) WHERE gateway_payment_id IS NOT NULL;

-- ─ 2) pending_topup 状态机（09-transactions §6）─────────────────────
CREATE TABLE pending_topup (
  id                     TEXT PRIMARY KEY,
  idempotency_record_id  TEXT NOT NULL,
  passenger_id           TEXT NOT NULL,
  topup_order_id         TEXT NOT NULL,
  status                 TEXT NOT NULL,
  error                  TEXT,
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (topup_order_id) REFERENCES topup_order(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id),
  CHECK (status IN (
    'initial',            -- CreateOrder 落·gateway 还没建
    'gateway_ordered',    -- gateway CreatePayment 已建 · gateway_payment_id 已回填
    'gateway_paid',       -- 收到 settled webhook · wallet 还没入账
    'credited',           -- wallet_ledger recharge/channel_fee 已落·状态未推 completed
    'completed',          -- 终态
    'expired',            -- gateway_ordered 但超时 · gateway 侧 pending_payment 也过期
    'cancelled',          -- 乘客主动取消（1b 可能不做）
    'refunded',           -- gateway 通知 refunded/reversed
    'pending_manual'      -- 卡多轮·转人工
  ))
);
CREATE INDEX idx_pt_scan ON pending_topup(status, updated_at);
CREATE INDEX idx_pt_order ON pending_topup(topup_order_id);
CREATE INDEX idx_pt_passenger ON pending_topup(passenger_id, created_at DESC);

-- +migrate down

-- 回滚：drop pending_topup · topup_order 收紧 channel CHECK 回 waffo 单渠道
DROP TABLE pending_topup;

CREATE TABLE topup_order_old (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  channel                TEXT NOT NULL,
  credits                INTEGER NOT NULL,
  channel_fee            INTEGER NOT NULL,
  paid                   INTEGER NOT NULL,
  pay_url                TEXT NOT NULL,
  status                 TEXT NOT NULL,
  expires_at             TEXT NOT NULL,
  paid_at                TEXT,
  wallet_ledger_id       TEXT,
  gateway_payment_id     TEXT,
  checkout_url           TEXT,
  qr_content             TEXT,
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (credits > 0),
  CHECK (channel_fee >= 0),
  CHECK (paid = credits + channel_fee),
  CHECK (channel IN ('waffo'))
);

INSERT INTO topup_order_old
SELECT id, passenger_id, channel, credits, channel_fee, paid, pay_url,
       status, expires_at, paid_at, wallet_ledger_id, gateway_payment_id,
       checkout_url, qr_content, created_at, updated_at
FROM topup_order
WHERE channel = 'waffo';

DROP TABLE topup_order;
ALTER TABLE topup_order_old RENAME TO topup_order;
