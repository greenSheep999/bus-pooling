-- +migrate up
--
-- 042_coupon_code.sql · coupon_code 主表 · decisions §8.43 v2
--
-- 优惠码有两种 type:
--   topup_discount     · 充值弹窗输 · 减 USD 实付 · discount_bp 存百分点(500 = 5%)
--   service_fee_waiver · 拉号确认窗输 · 免 N 轮服务费 · waive_rounds 存轮数
--
-- 一次生效 · remaining_uses / expires_at 硬上限。**跟 tier / referral / bus 邀请无关**
-- (四码分离铁律 · §8.42)。
--
-- 命名冲突: 041 已在 topup_order 加 coupon_code TEXT 列存"用户输的码字符串"·
-- 这里再建**表** · 表主键 id · 通过 `code` 字段跟 topup_order.coupon_code 关联。
--
-- 阶段 1(sprint-1e)只落库不算减免 · 减免规则在 sprint-1f 后接。
-- 落库先做是为了避免上线后加字段还要迁移。

CREATE TABLE coupon_code (
  id               TEXT PRIMARY KEY,
  code             TEXT UNIQUE NOT NULL,           -- 用户输的码(大写字母数字混合)
  type             TEXT NOT NULL,                  -- topup_discount | service_fee_waiver
  discount_bp      INTEGER,                        -- topup_discount 用 · 折扣 basis point(500=5%, 2000=20%)
  waive_rounds     INTEGER,                        -- service_fee_waiver 用 · 免几轮(NULL = 单次)
  remaining_uses   INTEGER,                        -- NULL = 不限次
  used_count       INTEGER NOT NULL DEFAULT 0,
  expires_at       TEXT,                           -- NULL = 不限时
  status           TEXT NOT NULL DEFAULT 'active', -- active | disabled
  memo             TEXT,                           -- 批次说明(运营用)
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL,
  CHECK (type IN ('topup_discount','service_fee_waiver')),
  CHECK (status IN ('active','disabled')),
  -- 类型互斥:topup 用 discount_bp · pull 用 waive_rounds · 各只填自己那份
  CHECK (
    (type = 'topup_discount' AND discount_bp IS NOT NULL AND waive_rounds IS NULL) OR
    (type = 'service_fee_waiver' AND waive_rounds IS NOT NULL AND discount_bp IS NULL)
  )
);

CREATE INDEX idx_coupon_code_code ON coupon_code(code);
CREATE INDEX idx_coupon_code_active ON coupon_code(status, expires_at);

-- coupon_use · 核销记录(哪笔单用了 · 减了多少 · 幂等)
CREATE TABLE coupon_use (
  id              TEXT PRIMARY KEY,
  coupon_code_id  TEXT NOT NULL,
  passenger_id    TEXT NOT NULL,
  context         TEXT NOT NULL,                    -- topup | pull
  context_ref     TEXT NOT NULL,                    -- topup_order.id / pull_round.id
  discount_amount INTEGER NOT NULL DEFAULT 0,       -- 减了多少 microunit(topup 场景)or 免了几轮(pull)
  created_at      TEXT NOT NULL,
  FOREIGN KEY (coupon_code_id) REFERENCES coupon_code(id),
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  UNIQUE (context, context_ref)                     -- 一单只核销一次(幂等)
);

CREATE INDEX idx_coupon_use_coupon ON coupon_use(coupon_code_id);
CREATE INDEX idx_coupon_use_passenger ON coupon_use(passenger_id, created_at);

-- +migrate down

-- 保测试契约（TestMigrateDownDropsEverything）· 显式 DROP
DROP INDEX IF EXISTS idx_coupon_use_passenger;
DROP INDEX IF EXISTS idx_coupon_use_coupon;
DROP TABLE IF EXISTS coupon_use;
DROP INDEX IF EXISTS idx_coupon_code_active;
DROP INDEX IF EXISTS idx_coupon_code_code;
DROP TABLE IF EXISTS coupon_code;
