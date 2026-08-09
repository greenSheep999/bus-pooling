-- +migrate up

-- 6. topup_order 加 refunded / reversed 状态
--
-- gateway settlement 事件 kind=refunded / reversed 时·我方要能标状态并落反向流水。
-- 之前 CHECK 约束不允许这两个值·直接拒 INSERT/UPDATE。
--
-- SQLite 不支持 ALTER TABLE 加/改 CHECK · 只能重建。步骤：
--   1) 建新表 topup_order_new · 一样的列 · 扩了 CHECK
--   2) 数据迁过去
--   3) 老表删 · 新表 rename
--   4) 索引重建
--
-- 阶段 1a 数据量小·全表重建可接受（生产上量前会切到 PG）。

CREATE TABLE topup_order_new (
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
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  gateway_payment_id     TEXT,
  checkout_url           TEXT,
  qr_content             TEXT,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (credits > 0),
  CHECK (channel_fee >= 0),
  CHECK (paid = credits + channel_fee),
  CHECK (channel IN ('waffo')),
  CHECK (status IN ('pending', 'paid', 'expired', 'cancelled', 'refunded', 'reversed'))
);

INSERT INTO topup_order_new
  SELECT id, passenger_id, channel, credits, channel_fee, paid, pay_url,
         status, expires_at, paid_at, wallet_ledger_id, created_at, updated_at,
         gateway_payment_id, checkout_url, qr_content
    FROM topup_order;

DROP INDEX IF EXISTS idx_topup_gateway_payment_id;
DROP INDEX IF EXISTS idx_topup_status;
DROP INDEX IF EXISTS idx_topup_passenger_time;
DROP TABLE topup_order;
ALTER TABLE topup_order_new RENAME TO topup_order;

CREATE INDEX idx_topup_passenger_time ON topup_order(passenger_id, created_at DESC);
CREATE INDEX idx_topup_status ON topup_order(status, expires_at);
CREATE UNIQUE INDEX idx_topup_gateway_payment_id
  ON topup_order(gateway_payment_id)
  WHERE gateway_payment_id IS NOT NULL;

-- +migrate down
-- 回滚：把 refunded / reversed 状态的行降级到 cancelled（避免 CHECK 冲突）
-- 生产回滚极少走·允许简单实现。

UPDATE topup_order SET status = 'cancelled' WHERE status IN ('refunded', 'reversed');

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
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  gateway_payment_id     TEXT,
  checkout_url           TEXT,
  qr_content             TEXT,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (credits > 0),
  CHECK (channel_fee >= 0),
  CHECK (paid = credits + channel_fee),
  CHECK (channel IN ('waffo')),
  CHECK (status IN ('pending', 'paid', 'expired', 'cancelled'))
);

INSERT INTO topup_order_old SELECT * FROM topup_order;

DROP INDEX IF EXISTS idx_topup_gateway_payment_id;
DROP INDEX IF EXISTS idx_topup_status;
DROP INDEX IF EXISTS idx_topup_passenger_time;
DROP TABLE topup_order;
ALTER TABLE topup_order_old RENAME TO topup_order;

CREATE INDEX idx_topup_passenger_time ON topup_order(passenger_id, created_at DESC);
CREATE INDEX idx_topup_status ON topup_order(status, expires_at);
CREATE UNIQUE INDEX idx_topup_gateway_payment_id
  ON topup_order(gateway_payment_id)
  WHERE gateway_payment_id IS NOT NULL;
