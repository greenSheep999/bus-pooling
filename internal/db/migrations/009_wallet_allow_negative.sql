-- +migrate up

-- 009_wallet_allow_negative.sql
--
-- P0-B 修：refund 允许 wallet.balance 走到负 —— 用户已花光时·refund 是"退给 gateway
-- 不是换用户余额"·系统必须记这笔"负债"（wallet.balance 变负）让运营看到并追讨。
--
-- SQLite CHECK 约束不能 ALTER · 只能 rebuild 表。同 006/007 的 rebuild in-place 手法。
--
-- 变更：drop `CHECK (balance >= 0)`·**保留** `CHECK (reserved >= 0)`（冻结额不能负）。
-- 应用层防超扣：Debit / Reserve 里的条件 UPDATE `WHERE balance >= amount` 挡在扣款前·
-- ForceApplyTx 才走无条件 UPDATE（只有 topup_refund / admin_adjust 允许走）。

CREATE TABLE wallet_new (
  passenger_id           TEXT PRIMARY KEY,
  balance                INTEGER NOT NULL DEFAULT 0,
  reserved               INTEGER NOT NULL DEFAULT 0,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (reserved >= 0)
);

INSERT INTO wallet_new (passenger_id, balance, reserved, updated_at)
SELECT passenger_id, balance, reserved, updated_at FROM wallet;

DROP TABLE wallet;
ALTER TABLE wallet_new RENAME TO wallet;

-- +migrate down
-- 回滚：加回 balance >= 0 约束（历史 balance 若已负则 rollback 报错·符合"回滚是最后手段"契约）

CREATE TABLE wallet_old (
  passenger_id           TEXT PRIMARY KEY,
  balance                INTEGER NOT NULL DEFAULT 0,
  reserved               INTEGER NOT NULL DEFAULT 0,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (balance >= 0),
  CHECK (reserved >= 0)
);

INSERT INTO wallet_old (passenger_id, balance, reserved, updated_at)
SELECT passenger_id, balance, reserved, updated_at FROM wallet;

DROP TABLE wallet;
ALTER TABLE wallet_old RENAME TO wallet;
