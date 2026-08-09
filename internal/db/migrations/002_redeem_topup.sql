-- +migrate up

-- 兑换码。管理员批量生成，乘客用 POST /api/me/redeem 消费。
-- 一码一行；used_by / used_at 落定后就再也不能被其他乘客用（used 是终态）。
CREATE TABLE redeem_code (
  code                   TEXT PRIMARY KEY,               -- 大小写敏感；生成时统一大写
  credits                INTEGER NOT NULL,                -- microunit · 兑换到手积分
  status                 TEXT NOT NULL,                   -- unused | used | expired
  used_by                TEXT,                            -- passenger.id
  used_at                TEXT,
  expires_at             TEXT,                            -- NULL = 不过期
  memo                   TEXT,                            -- 管理员备注：批次 / 活动等
  created_at             TEXT NOT NULL,
  FOREIGN KEY (used_by) REFERENCES passenger(id),
  CHECK (credits > 0),
  CHECK (status IN ('unused', 'used', 'expired')),
  -- unused 时 used_by/used_at 必须为空；used 时都必须有值。
  CHECK (
    (status = 'unused' AND used_by IS NULL AND used_at IS NULL) OR
    (status = 'used'   AND used_by IS NOT NULL AND used_at IS NOT NULL) OR
    (status = 'expired')
  )
);
CREATE INDEX idx_redeem_used_by ON redeem_code(used_by);

-- 充值单。乘客发起 → 落 pending 行 + 生成 pay_url；等 支付网关 webhook 到才 MarkPaid，
-- 那时才落 wallet_ledger 两条：recharge + channel_fee（CLAUDE.md §1.4）。
--
-- 字段口径：
--   credits       乘客目标积分（净到账）
--   channel_fee   通道费（credits × 5%，落定时算好）
--   paid          乘客真金白银付出去的总积分（= credits + channel_fee）
--   pay_url       起单时给的支付跳转 URL（mock 阶段是假 URL）
CREATE TABLE topup_order (
  id                     TEXT PRIMARY KEY,               -- UUID v7 · 对外叫 order_id
  passenger_id           TEXT NOT NULL,
  channel                TEXT NOT NULL,                   -- 目前只启用一家 hosted
  credits                INTEGER NOT NULL,                -- microunit · 净到账
  channel_fee            INTEGER NOT NULL,                -- microunit · 通道费
  paid                   INTEGER NOT NULL,                -- microunit · credits + channel_fee
  pay_url                TEXT NOT NULL,
  status                 TEXT NOT NULL,                   -- pending | paid | expired | cancelled
  expires_at             TEXT NOT NULL,
  paid_at                TEXT,
  wallet_ledger_id       TEXT,                            -- MarkPaid 后指向 recharge 那条
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (credits > 0),
  CHECK (channel_fee >= 0),
  CHECK (paid = credits + channel_fee),
  CHECK (channel IN ('waffo')),
  CHECK (status IN ('pending', 'paid', 'expired', 'cancelled'))
);
CREATE INDEX idx_topup_passenger_time ON topup_order(passenger_id, created_at DESC);
CREATE INDEX idx_topup_status ON topup_order(status, expires_at);

-- +migrate down

DROP INDEX IF EXISTS idx_topup_status;
DROP INDEX IF EXISTS idx_topup_passenger_time;
DROP TABLE IF EXISTS topup_order;
DROP INDEX IF EXISTS idx_redeem_used_by;
DROP TABLE IF EXISTS redeem_code;
