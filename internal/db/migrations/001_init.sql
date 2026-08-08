-- 阶段 1a 首批 schema · 19 张表
-- 权威定义在 docs/06-db-schema.md，这里是它的可执行版本。改表先改那份文档。
--
-- 约定（CLAUDE.md §7.2）：
--   主键 UUID v7 存 TEXT · money 整数 microunit（1 积分 = 1_000_000）
--   时间 ISO-8601 UTC 字符串 · 布尔用 INTEGER 0/1

-- +migrate up

-- ── 账号 ────────────────────────────────────────────────

CREATE TABLE passenger (
  id                     TEXT PRIMARY KEY,
  username               TEXT NOT NULL UNIQUE,
  email                  TEXT NOT NULL UNIQUE,
  email_verified         INTEGER NOT NULL DEFAULT 0,
  password_hash          TEXT NOT NULL,                  -- Argon2id
  role                   TEXT NOT NULL DEFAULT 'user',   -- user | admin
  status                 TEXT NOT NULL DEFAULT 'active', -- active | disabled
  -- 注册时填过**系统**邀请码（decisions §8.20 §8.29）· 个人码不置这个
  invited                INTEGER NOT NULL DEFAULT 0,
  invite_code_used       TEXT,
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  last_login_at          TEXT,
  deleted_at             TEXT,
  CHECK (role IN ('user', 'admin')),
  CHECK (status IN ('active', 'disabled'))
);
CREATE INDEX idx_passenger_username ON passenger(username);
CREATE INDEX idx_passenger_email ON passenger(email);
CREATE INDEX idx_passenger_status ON passenger(status);

CREATE TABLE session (
  id                     TEXT PRIMARY KEY,               -- SHA-256(session token)
  passenger_id           TEXT NOT NULL,
  ip_created             TEXT,
  user_agent             TEXT,
  created_at             TEXT NOT NULL,
  last_used_at           TEXT NOT NULL,
  expires_at             TEXT NOT NULL,
  revoked_at             TEXT,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
CREATE INDEX idx_session_passenger ON session(passenger_id, revoked_at);
CREATE INDEX idx_session_expires ON session(expires_at);

CREATE TABLE passenger_api_key (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  key_hash               TEXT NOT NULL UNIQUE,           -- SHA-256(明文)
  prefix                 TEXT NOT NULL,                  -- 前 12 位，UI 展示用
  name                   TEXT,
  created_at             TEXT NOT NULL,
  last_used_at           TEXT,
  revoked_at             TEXT,                            -- 吊销不删行（台账留痕）
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
CREATE INDEX idx_api_key_passenger ON passenger_api_key(passenger_id);
CREATE INDEX idx_api_key_prefix ON passenger_api_key(prefix);

CREATE TABLE passenger_downstream (
  passenger_id                          TEXT PRIMARY KEY,
  passengerpool_url                     TEXT,
  secret_passengerpool_token_encrypted  BLOB,             -- AES-GCM · internal/secrets
  webhook_url                           TEXT,
  secret_webhook_secret_encrypted       BLOB,
  -- 推送策略 4 条（decisions §8.25 · 前端「我的号池」页）
  push_on_pull                          INTEGER NOT NULL DEFAULT 1,
  resync_on_dead                        INTEGER NOT NULL DEFAULT 1,
  retry_on_failure                      INTEGER NOT NULL DEFAULT 1,
  bus_only                              INTEGER NOT NULL DEFAULT 0,
  updated_at                            TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);

-- ── 钱包 ────────────────────────────────────────────────

CREATE TABLE wallet (
  passenger_id           TEXT PRIMARY KEY,
  balance                INTEGER NOT NULL DEFAULT 0,     -- microunit · 可用余额
  reserved               INTEGER NOT NULL DEFAULT 0,     -- microunit · 冻结中（拉号进行中）
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (balance >= 0),
  CHECK (reserved >= 0)
);

CREATE TABLE wallet_ledger (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  seq                    INTEGER NOT NULL,               -- 该乘客的序号（严格递增）
  reason                 TEXT NOT NULL,
  amount                 INTEGER NOT NULL,               -- microunit 带符号
  balance_after          INTEGER NOT NULL,
  ref_type               TEXT,
  ref_id                 TEXT,
  memo                   TEXT,
  created_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  UNIQUE (passenger_id, seq)
);
CREATE INDEX idx_ledger_passenger_time ON wallet_ledger(passenger_id, created_at DESC);
CREATE INDEX idx_ledger_reason ON wallet_ledger(reason);

-- ── 车 ──────────────────────────────────────────────────

CREATE TABLE bus (
  id                     TEXT PRIMARY KEY,
  name                   TEXT NOT NULL,
  kind                   TEXT NOT NULL,                   -- single | anon | team
  creator_passenger_id   TEXT NOT NULL,
  invite_code            TEXT UNIQUE,
  max_members            INTEGER,
  status                 TEXT NOT NULL DEFAULT 'active',  -- active | dissolved
  created_at             TEXT NOT NULL,
  dissolved_at           TEXT,
  -- 补车策略（每车一策略 · decisions §8.6）
  auto_refill_enabled    INTEGER NOT NULL DEFAULT 0,
  refill_watermark       INTEGER NOT NULL DEFAULT 0,
  refill_min_count       INTEGER,
  per_round_count        INTEGER,
  max_unit_price         INTEGER,                          -- microunit
  daily_round_limit      INTEGER,
  daily_spend_limit      INTEGER,                          -- microunit
  preferred_vendor       TEXT,                             -- NULL = 比价自动选
  FOREIGN KEY (creator_passenger_id) REFERENCES passenger(id),
  CHECK (kind IN ('single', 'anon', 'team')),
  CHECK (status IN ('active', 'dissolved'))
);
CREATE INDEX idx_bus_creator ON bus(creator_passenger_id, status);
CREATE INDEX idx_bus_kind_status ON bus(kind, status);

CREATE TABLE bus_member (
  bus_id                 TEXT NOT NULL,
  passenger_id           TEXT NOT NULL,
  role                   TEXT NOT NULL DEFAULT 'member',  -- member | owner
  joined_at              TEXT NOT NULL,
  left_at                TEXT,                             -- 退出不删行，留历史
  -- ↓ 阶段 2a 才用（多人车）· 1a 全 1 人车，owner 恒 100 / active / 0
  --   列在 1a 就建好，免得 2a 改表（SQLite ALTER 加不了 CHECK）
  share_pct              INTEGER NOT NULL DEFAULT 100,     -- 分摊比例 · 全车合计 100
  status                 TEXT NOT NULL DEFAULT 'active',   -- active | suspended
  skipped_count          INTEGER NOT NULL DEFAULT 0,       -- 连续因余额不足被跳过
  last_skipped_at        TEXT,
  PRIMARY KEY (bus_id, passenger_id, joined_at),
  FOREIGN KEY (bus_id) REFERENCES bus(id),
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (role IN ('member', 'owner')),
  CHECK (share_pct BETWEEN 0 AND 100),
  CHECK (status IN ('active', 'suspended'))
);
CREATE INDEX idx_bus_member_passenger ON bus_member(passenger_id, left_at);
CREATE INDEX idx_bus_member_bus ON bus_member(bus_id, left_at);

-- ── 拉号 ────────────────────────────────────────────────

CREATE TABLE pull_intent (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  bus_id                 TEXT,                             -- NULL = 单独拉号（次入口）
  target                 TEXT NOT NULL,                    -- to-bus | to-record
  count_requested        INTEGER NOT NULL,
  constraints_json       TEXT,
  status                 TEXT NOT NULL DEFAULT 'pending',
  batch_id               TEXT,                             -- coalesce 合流后同一 batch
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (bus_id) REFERENCES bus(id),
  CHECK (target IN ('to-bus', 'to-record')),
  CHECK (status IN ('pending','in_flight','coalesced','fulfilled','failed','cancelled'))
);
CREATE INDEX idx_intent_passenger ON pull_intent(passenger_id, status, created_at);
CREATE INDEX idx_intent_bus ON pull_intent(bus_id, status);
CREATE INDEX idx_intent_scan ON pull_intent(status, created_at);

CREATE TABLE pull_round (
  id                        TEXT PRIMARY KEY,
  vendor_id                 TEXT NOT NULL,
  client_order_id           TEXT NOT NULL,                  -- 32 hex · 给 vendor 的幂等键
  bus_id                    TEXT,
  count_requested           INTEGER NOT NULL,
  count_purchased           INTEGER NOT NULL,
  -- 计费分项 · 逐层乘的每层增量（decisions §8.34）· 分项之和 = total_debit
  key_cost_total            INTEGER NOT NULL,               -- microunit
  vendor_fee_total          INTEGER NOT NULL DEFAULT 0,     -- vendor 附加费（1b 才非 0）
  region_fee_total          INTEGER NOT NULL DEFAULT 0,     -- 区域附加费（1b 才非 0）
  single_pull_fee_total     INTEGER NOT NULL DEFAULT 0,
  capability_fee_total      INTEGER NOT NULL DEFAULT 0,
  service_fee_total         INTEGER NOT NULL,
  participants_split_json   TEXT NOT NULL,                  -- {passenger_id: count} 分摊用
  status                    TEXT NOT NULL,
  vendor_response_json      TEXT,
  vendor_order_id           TEXT,
  created_at                TEXT NOT NULL,
  completed_at              TEXT,
  UNIQUE (vendor_id, client_order_id),
  CHECK (status IN ('initiated','completed','failed','partial','refunded'))
);
CREATE INDEX idx_round_bus ON pull_round(bus_id, created_at DESC);
CREATE INDEX idx_round_vendor ON pull_round(vendor_id, created_at DESC);
CREATE INDEX idx_round_status ON pull_round(status);

CREATE TABLE credential_ledger (
  id                          TEXT PRIMARY KEY,
  kiro_rs_credential_id       INTEGER NOT NULL UNIQUE,     -- housepool 侧 id (u64)
  owner_bus_id                TEXT,
  owner_record_passenger_id   TEXT,
  current_group               TEXT NOT NULL,               -- bus-<id> | record-<pid> | market
  vendor_id                   TEXT NOT NULL,
  vendor_order_id             TEXT,
  source_pull_round_id        TEXT NOT NULL,
  status                      TEXT NOT NULL,               -- alive | dead | handed_off
  disabled                    INTEGER NOT NULL DEFAULT 0,
  pulled_at                   TEXT NOT NULL,
  dead_at                     TEXT,
  death_source                TEXT,                        -- housepool_probe | vendor_webhook | vendor_poll
  handed_off_at               TEXT,
  pushed_to_passengerpool_at  TEXT,
  -- 推送失败结构化（decisions §8.24 §8.25）· 售后靠这几个字段判是谁的问题
  push_error_code             TEXT,
  push_error_status           INTEGER,
  push_error_message          TEXT,
  push_error_retriable        INTEGER,
  push_attempts               INTEGER NOT NULL DEFAULT 0,
  push_last_attempt_at        TEXT,
  -- 质保窗口截止（各 vendor 10-30 分钟）· UI「质保内失效·可退」判据
  warranty_until              TEXT,
  FOREIGN KEY (owner_bus_id) REFERENCES bus(id),
  FOREIGN KEY (owner_record_passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (source_pull_round_id) REFERENCES pull_round(id),
  CHECK (status IN ('alive', 'dead', 'handed_off')),
  -- 号要么属于一辆车，要么属于某乘客的拉号记录，不能同时也不能都不属于
  CHECK (
    (owner_bus_id IS NOT NULL AND owner_record_passenger_id IS NULL) OR
    (owner_bus_id IS NULL AND owner_record_passenger_id IS NOT NULL)
  )
);
CREATE INDEX idx_cred_bus ON credential_ledger(owner_bus_id, status);
CREATE INDEX idx_cred_record ON credential_ledger(owner_record_passenger_id, status);
CREATE INDEX idx_cred_group ON credential_ledger(current_group);
CREATE INDEX idx_cred_vendor ON credential_ledger(vendor_id, status);
CREATE INDEX idx_cred_dead ON credential_ledger(status, dead_at);
CREATE INDEX idx_cred_pulled ON credential_ledger(pulled_at);

-- ── 配置 ────────────────────────────────────────────────

CREATE TABLE vendor_account (
  id                              TEXT PRIMARY KEY,
  vendor_id                       TEXT NOT NULL,
  label                           TEXT,
  auth_scheme                     TEXT NOT NULL,           -- api_key | bearer | cookie
  secret_credentials_encrypted    BLOB NOT NULL,           -- AES-GCM
  status                          TEXT NOT NULL DEFAULT 'active',
  created_at                      TEXT NOT NULL,
  updated_at                      TEXT NOT NULL
);
CREATE INDEX idx_vendor_account ON vendor_account(vendor_id, status);

CREATE TABLE passenger_daily_counter (
  passenger_id           TEXT NOT NULL,
  date                   TEXT NOT NULL,                   -- 'YYYY-MM-DD'
  round_count            INTEGER NOT NULL DEFAULT 0,
  spend_total            INTEGER NOT NULL DEFAULT 0,      -- microunit
  PRIMARY KEY (passenger_id, date),
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);

-- 全局策略（decisions §8.27）· 硬上限 + 新车默认值，两类语义不同见文档
CREATE TABLE passenger_strategy_default (
  passenger_id             TEXT PRIMARY KEY,
  -- 硬上限 · 超了拒绝拉号 · NULL = 不限
  max_unit_price           INTEGER,                       -- microunit
  daily_round_limit        INTEGER,                       -- 跨所有车累加
  daily_spend_limit        INTEGER,                       -- microunit
  -- 建新车时的默认值（改它不影响已有的车）
  per_round_count          INTEGER,
  preferred_vendor         TEXT,
  default_zone             TEXT NOT NULL DEFAULT 'auto',  -- us | eu | auto
  updated_at               TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);

-- ── 幂等 ────────────────────────────────────────────────

CREATE TABLE idempotency_record (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  method                 TEXT NOT NULL,
  path                   TEXT NOT NULL,
  idempotency_key        TEXT NOT NULL,                   -- 客户端 X-Idempotency-Key (32 hex)
  request_fingerprint    TEXT NOT NULL,                   -- sha256(canonical body) 防冲突
  response_status        INTEGER,
  response_headers       TEXT,
  response_body          BLOB,                            -- handoff 场景不含明文
  created_at             TEXT NOT NULL,
  first_completed_at     TEXT,
  UNIQUE (passenger_id, path, idempotency_key),
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
CREATE INDEX idx_idem_created ON idempotency_record(created_at);

-- ── 状态机（09-transactions） ────────────────────────────

CREATE TABLE pending_purchase (
  id                       TEXT PRIMARY KEY,
  idempotency_record_id    TEXT NOT NULL,
  passenger_id             TEXT NOT NULL,
  bus_id                   TEXT,
  target_group             TEXT NOT NULL,                 -- bus-<id> | record-<pid>
  vendor_id                TEXT NOT NULL,
  client_order_id          TEXT NOT NULL,                 -- vendor 幂等键 (32 hex)
  count_requested          INTEGER NOT NULL,
  reserved_amount          INTEGER NOT NULL,
  -- ★ purchasing = 请求已发 vendor、响应未确认（09-transactions §2.1 · P0-1）
  --   崩在这个状态**不能直接释放冻结** —— vendor 可能已扣款
  status                   TEXT NOT NULL,
  vendor_order_id          TEXT,
  pull_round_id            TEXT,
  error                    TEXT,
  created_at               TEXT NOT NULL,
  updated_at               TEXT NOT NULL,
  UNIQUE (vendor_id, client_order_id),
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (bus_id) REFERENCES bus(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id),
  FOREIGN KEY (pull_round_id) REFERENCES pull_round(id),
  CHECK (status IN ('initial','reserved','purchasing','purchased','imported',
                    'completed','cancelled_reserve','need_recover_vendor','need_manual'))
);
CREATE INDEX idx_pp_scan ON pending_purchase(status, updated_at);
CREATE INDEX idx_pp_passenger ON pending_purchase(passenger_id, created_at);

CREATE TABLE pending_assignment (
  id                    TEXT PRIMARY KEY,
  idempotency_record_id TEXT NOT NULL,
  passenger_id          TEXT NOT NULL,
  credential_id         TEXT NOT NULL,                    -- credential_ledger.id
  -- DB 值用 to-bus / to-passengerpool（历史命名）· 线上契约是 into_bus / push_pool
  -- 映射在 handler 里做一次，别让两套值互相渗透（05-api-contract §5）
  target                TEXT NOT NULL,
  target_bus_id         TEXT,
  status                TEXT NOT NULL,
  error                 TEXT,
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (credential_id) REFERENCES credential_ledger(id),
  FOREIGN KEY (target_bus_id) REFERENCES bus(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id),
  CHECK (target IN ('to-bus', 'to-passengerpool')),
  CHECK (status IN ('initial','external_done','status_updated','completed','need_manual'))
);
CREATE INDEX idx_pa_scan ON pending_assignment(status, updated_at);
CREATE INDEX idx_pa_passenger ON pending_assignment(passenger_id, created_at);

-- handoff 三段式（09-transactions §4 · P0-3）
-- 明文**永不存本表** —— 每次 fulfill 从 housepool 实时读
CREATE TABLE pending_handoff (
  id                    TEXT PRIMARY KEY,
  idempotency_record_id TEXT,                             -- init 阶段可选
  passenger_id          TEXT NOT NULL,
  download_token        TEXT NOT NULL UNIQUE,             -- 32 hex · 本身就是幂等键
  credential_ids_json   TEXT NOT NULL,
  status                TEXT NOT NULL,
  fulfill_count         INTEGER NOT NULL DEFAULT 0,       -- 被取了几次（断线重试用）
  fulfilled_at          TEXT,
  confirmed_at          TEXT,
  completed_at          TEXT,
  expires_at            TEXT NOT NULL,                    -- token TTL · 默认 now + 5min
  error                 TEXT,
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id),
  CHECK (status IN ('token_issued','fulfilled','confirmed','completed',
                    'expired','expired_after_fulfill','need_manual'))
);
CREATE INDEX idx_ph_scan ON pending_handoff(status, expires_at);
CREATE INDEX idx_ph_passenger ON pending_handoff(passenger_id, created_at DESC);

CREATE TABLE pending_dissolution (
  id                     TEXT PRIMARY KEY,
  idempotency_record_id  TEXT NOT NULL,
  bus_id                 TEXT NOT NULL,
  initiator_passenger_id TEXT NOT NULL,
  credential_ids_json    TEXT NOT NULL,                   -- 解散触发时的快照
  processed_count        INTEGER NOT NULL DEFAULT 0,
  failed_count           INTEGER NOT NULL DEFAULT 0,
  status                 TEXT NOT NULL,
  error                  TEXT,
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (bus_id) REFERENCES bus(id),
  FOREIGN KEY (initiator_passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id),
  CHECK (status IN ('initial','snapshot_taken','moving','completed','need_manual'))
);
CREATE INDEX idx_pd_scan ON pending_dissolution(status, updated_at);

-- +migrate down

DROP TABLE IF EXISTS pending_dissolution;
DROP TABLE IF EXISTS pending_handoff;
DROP TABLE IF EXISTS pending_assignment;
DROP TABLE IF EXISTS pending_purchase;
DROP TABLE IF EXISTS idempotency_record;
DROP TABLE IF EXISTS passenger_strategy_default;
DROP TABLE IF EXISTS passenger_daily_counter;
DROP TABLE IF EXISTS vendor_account;
DROP TABLE IF EXISTS credential_ledger;
DROP TABLE IF EXISTS pull_round;
DROP TABLE IF EXISTS pull_intent;
DROP TABLE IF EXISTS bus_member;
DROP TABLE IF EXISTS bus;
DROP TABLE IF EXISTS wallet_ledger;
DROP TABLE IF EXISTS wallet;
DROP TABLE IF EXISTS passenger_downstream;
DROP TABLE IF EXISTS passenger_api_key;
DROP TABLE IF EXISTS session;
DROP TABLE IF EXISTS passenger;
