-- migration 037 · pending_refill · 号死后待自动补车队列（P6）
--
-- **为什么**：deathwatch 老代码只标死不补 —— 号死了乘客要手动去点"拉号"·
-- 半自动化不好用。加一张待补队列：deathwatch 标死后往这里塞一条 · worker 消费。
--
-- **拆两步的原因**（阶段性回滚友好）：
--   本迁移 + skeleton worker（log only）先落 · 观察落多少 · 什么场景触发；
--   1d 再改成真触发 decider.Pull（要保证策略允许 + 幂等 + 并发限流）。
--
-- **字段说明**：
--   · dead_credential_id · 触发本次补车的死号 · 幂等主键的一半
--   · bus_id             · 补进哪辆车 · 空 = 单独提取（乘客视角）
--   · passenger_id       · 谁的车 / 谁在等
--   · count              · 补几个（当前从死号推算 · 1d 可能改成"按车级策略"）
--   · vendor_id          · 优先用哪家（可空 · 让 decider auto-pick）
--   · status             · pending / processing / fulfilled / expired / skipped
--   · attempts / last_attempt_at / last_error · worker 重试用
--   · created_at         · 号死时刻（跟 credential.dead_at 对得上）
--   · resolved_at        · 最终态时刻（fulfilled/expired/skipped）
--
-- **幂等**：(dead_credential_id) 唯一 · 重复触发同一死号不重塞。

-- +migrate up

CREATE TABLE pending_refill (
  id                    TEXT PRIMARY KEY,
  dead_credential_id    TEXT NOT NULL UNIQUE,     -- credential_ledger.id
  bus_id                TEXT,                     -- 空 = 单独提取
  passenger_id          TEXT NOT NULL,
  count                 INTEGER NOT NULL,         -- 补几个 · 从死号语义推算
  vendor_id             TEXT,                     -- 可空 · decider auto-pick
  status                TEXT NOT NULL,            -- pending/processing/fulfilled/expired/skipped
  attempts              INTEGER NOT NULL DEFAULT 0,
  last_attempt_at       TEXT,
  last_error            TEXT,
  reason                TEXT,                     -- 触发原因 · 'dead' / 'revoked' 等
  created_at            TEXT NOT NULL,
  resolved_at           TEXT,
  CHECK (status IN ('pending', 'processing', 'fulfilled', 'expired', 'skipped')),
  FOREIGN KEY (dead_credential_id) REFERENCES credential_ledger(id),
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);

CREATE INDEX idx_pending_refill_status ON pending_refill(status, created_at);
CREATE INDEX idx_pending_refill_passenger ON pending_refill(passenger_id, created_at DESC);

-- +migrate down
DROP INDEX IF EXISTS idx_pending_refill_passenger;
DROP INDEX IF EXISTS idx_pending_refill_status;
DROP TABLE IF EXISTS pending_refill;
