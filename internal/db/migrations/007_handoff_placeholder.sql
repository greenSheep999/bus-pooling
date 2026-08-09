-- +migrate up
--
-- pending_handoff 加两个占位路径专用状态（P0 · handoff 数据丢失回归修复）
--
-- 背景：上一轮为了让联调三段跑通·把 fulfill 默认放行占位字符串
-- （PLACEHOLDER:not-a-real-key:*）·但 confirm 分支仍走真 DELETE 到 housepool·
-- 用户拿到假号后 confirm 会真删号池里的号·明文永久丢失。
--
-- 修：区分"真明文 fulfilled" vs "占位 placeholder_delivered"·后者的 confirm
-- **不允许**走 DELETE 分支·只推到 confirmed_placeholder 终态（号仍在 pool 里）。

-- SQLite 不支持 ALTER CHECK · 只能重建。列顺序 / 类型 / FK 严格对齐 001_init。
CREATE TABLE pending_handoff_new (
  id                    TEXT PRIMARY KEY,
  idempotency_record_id TEXT,
  passenger_id          TEXT NOT NULL,
  download_token        TEXT NOT NULL UNIQUE,
  credential_ids_json   TEXT NOT NULL,
  status                TEXT NOT NULL,
  fulfill_count         INTEGER NOT NULL DEFAULT 0,
  fulfilled_at          TEXT,
  confirmed_at          TEXT,
  completed_at          TEXT,
  expires_at            TEXT NOT NULL,
  error                 TEXT,
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id),
  CHECK (status IN ('token_issued','fulfilled','confirmed','completed',
                    'expired','expired_after_fulfill','need_manual',
                    'placeholder_delivered','confirmed_placeholder'))
);

INSERT INTO pending_handoff_new SELECT * FROM pending_handoff;

DROP INDEX IF EXISTS idx_ph_scan;
DROP INDEX IF EXISTS idx_ph_passenger;
DROP TABLE pending_handoff;
ALTER TABLE pending_handoff_new RENAME TO pending_handoff;

CREATE INDEX idx_ph_scan ON pending_handoff(status, expires_at);
CREATE INDEX idx_ph_passenger ON pending_handoff(passenger_id, created_at DESC);

-- +migrate down

-- 回滚：先把占位状态降到 need_manual（避免 CHECK 冲突）
UPDATE pending_handoff
   SET status = 'need_manual'
 WHERE status IN ('placeholder_delivered', 'confirmed_placeholder');

CREATE TABLE pending_handoff_old (
  id                    TEXT PRIMARY KEY,
  idempotency_record_id TEXT,
  passenger_id          TEXT NOT NULL,
  download_token        TEXT NOT NULL UNIQUE,
  credential_ids_json   TEXT NOT NULL,
  status                TEXT NOT NULL,
  fulfill_count         INTEGER NOT NULL DEFAULT 0,
  fulfilled_at          TEXT,
  confirmed_at          TEXT,
  completed_at          TEXT,
  expires_at            TEXT NOT NULL,
  error                 TEXT,
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id),
  CHECK (status IN ('token_issued','fulfilled','confirmed','completed',
                    'expired','expired_after_fulfill','need_manual'))
);

INSERT INTO pending_handoff_old SELECT * FROM pending_handoff;

DROP INDEX IF EXISTS idx_ph_scan;
DROP INDEX IF EXISTS idx_ph_passenger;
DROP TABLE pending_handoff;
ALTER TABLE pending_handoff_old RENAME TO pending_handoff;

CREATE INDEX idx_ph_scan ON pending_handoff(status, expires_at);
CREATE INDEX idx_ph_passenger ON pending_handoff(passenger_id, created_at DESC);
