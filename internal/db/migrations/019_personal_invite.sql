-- +migrate up

-- 019_personal_invite.sql
--
-- 1c · 个人邀请码（decisions §8.29 / §8.32）。
--
-- **两种码职责完全不同**（这是必须分开的原因）：
--   - 系统邀请码：我方发给社群 · 划**社群身份**（解锁 vendor 真名 + 免区域分项）
--     → 置 passenger.invited = 1
--   - 个人邀请码：每个乘客自动有一个 · 只给**手续费减免额度**（限次数）
--     → **不改 invited** · 被邀请人仍是零售身份
--
-- **为什么个人码绝不能改 invited**（§8.29 明文）：如果个人码也解锁社群身份，
-- 那任何人都能生成码让别人免区域分项 → 整个定价分层崩掉。
--
-- 现在的漏洞：register 里 `invited := in.InviteCode != ""` —— **任何**非空码
-- 都置 invited=1。本 migration 加系统码白名单表，register 改成查表。

-- 系统邀请码白名单（我方发给社群的码 · 只有这里面的码才置 invited=1）
CREATE TABLE system_invite_code (
  code                 TEXT PRIMARY KEY,                -- 大写 · 我方线下发
  memo                 TEXT,                            -- 发给谁 / 哪个社群（运营备注）
  max_uses             INTEGER,                         -- NULL = 不限次
  used_count           INTEGER NOT NULL DEFAULT 0,
  expires_at           TEXT,                            -- NULL = 不过期
  disabled             INTEGER NOT NULL DEFAULT 0,      -- 1 = 立即失效
  created_at           TEXT NOT NULL
);

-- 个人邀请码 · 每个乘客一个（注册时生成 · 懒补见 passenger.EnsurePersonalCode）
CREATE TABLE personal_invite_code (
  code                 TEXT PRIMARY KEY,                -- 大写 8 位 · 跟 bus.invite_code 同字符集
  passenger_id         TEXT NOT NULL UNIQUE,            -- 一人一码
  -- 邀请成绩
  invited_count        INTEGER NOT NULL DEFAULT 0,      -- 成功拉来几个注册用户
  -- 手续费减免额度（规则见 decisions §8.32）
  fee_waiver_total     INTEGER NOT NULL DEFAULT 0,      -- 累计获得几次减免（上限规则见 decisions §8.32）
  fee_waiver_used      INTEGER NOT NULL DEFAULT 0,      -- 已用掉几次
  created_at           TEXT NOT NULL,
  updated_at           TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);

-- 谁邀请了谁（防刷 + 溯源 · 一个被邀请人只能被算一次）
CREATE TABLE invite_referral (
  invitee_passenger_id  TEXT PRIMARY KEY,               -- 被邀请人（一人只能被邀一次）
  inviter_passenger_id  TEXT NOT NULL,                  -- 邀请人
  code                  TEXT NOT NULL,                  -- 用的哪个个人码
  created_at            TEXT NOT NULL,
  FOREIGN KEY (invitee_passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (inviter_passenger_id) REFERENCES passenger(id),
  CHECK (invitee_passenger_id != inviter_passenger_id)  -- 不能自己邀自己
);

CREATE INDEX IF NOT EXISTS idx_referral_inviter ON invite_referral (inviter_passenger_id);

-- +migrate down

DROP INDEX IF EXISTS idx_referral_inviter;
DROP TABLE IF EXISTS invite_referral;
DROP TABLE IF EXISTS personal_invite_code;
DROP TABLE IF EXISTS system_invite_code;
