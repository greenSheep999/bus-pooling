# bus-pooling · 数据库 Schema

> 前置阅读：[`03-modules.md`](./03-modules.md) · [`05-api-contract.md`](./05-api-contract.md)
> 本文钉**表结构 + 约定**。具体 migration 脚本随代码走（`internal/db/migrations/`）。
>
> **技术底座**：SQLite WAL 单节点（`00 §9 未决`可能改，但 schema 层面 SQLite 已够用）。

## 通用约定

| 项 | 值 |
|---|---|
| 主键 | **UUID v7**（时间有序 + 无遍历攻击面）；存 `TEXT`（26 字符 Crockford Base32） |
| 时间 | `TEXT` 存 **ISO-8601 UTC**（`2026-08-07T12:34:56.789Z`）；不用 SQLite `DATETIME` |
| 金额 | `INTEGER` **microunit**（1 元 = 1_000_000）—— 精度稳定、可原子加减 |
| 布尔 | `INTEGER 0/1` |
| 命名 | **snake_case** —— 表名单数（`passenger` 不是 `passengers`）、字段 snake_case |
| 外键 | 用 `FOREIGN KEY` 但**不加 `ON DELETE CASCADE`**（软删更安全，我方 pull_record 表 credential 死了也保留） |
| 索引 | 每张表关键字段建索引，见每张表说明 |
| 软删 | 默认硬删；只有 `passenger` / `bus` / `payment_order` 需要软删（加 `deleted_at`） |

**加密字段** 用 `secret_<name>_encrypted` 命名（AES-GCM，主密钥来自环境变量，见 `internal/secrets`）。

---

## 1. 账号 · `passenger`

```sql
CREATE TABLE passenger (
  id                     TEXT PRIMARY KEY,              -- UUID v7
  username               TEXT NOT NULL UNIQUE,
  email                  TEXT NOT NULL UNIQUE,
  email_verified         INTEGER NOT NULL DEFAULT 0,
  password_hash          TEXT NOT NULL,                  -- Argon2id
  role                   TEXT NOT NULL DEFAULT 'user',   -- user | admin
  status                 TEXT NOT NULL DEFAULT 'active', -- active | disabled
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  last_login_at          TEXT,
  deleted_at             TEXT                             -- 软删
);
```

**索引**：`(username)`, `(email)`, `(status)`。

**依赖**：`internal/passenger`（Layer 3a）。

## 2. API Key · `passenger_api_key`

```sql
CREATE TABLE passenger_api_key (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  key_hash               TEXT NOT NULL UNIQUE,           -- SHA-256(明文)
  prefix                 TEXT NOT NULL,                  -- 前 12 位（含 usr-）用于 UI 展示
  name                   TEXT,                            -- 备注
  created_at             TEXT NOT NULL,
  last_used_at           TEXT,
  revoked_at             TEXT,                            -- 吊销
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
```

**索引**：`(passenger_id)`, `(key_hash)`, `(prefix)`。

## 3. 下游配置 · `passenger_downstream`

```sql
CREATE TABLE passenger_downstream (
  passenger_id                       TEXT PRIMARY KEY,
  passengerpool_url                  TEXT,                 -- 乘客的 kiro.rs 地址
  secret_passengerpool_token_encrypted  BLOB,              -- AES-GCM 加密的 admin key
  webhook_url                        TEXT,                 -- 我方推给他的地址
  secret_webhook_secret_encrypted    BLOB,                 -- webhook 签名密钥
  updated_at                         TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
```

**注意**：**明文永不落库**，只存 encrypted。

## 4. 钱包 · `wallet`

```sql
CREATE TABLE wallet (
  passenger_id           TEXT PRIMARY KEY,
  balance                INTEGER NOT NULL DEFAULT 0,      -- microunit（可用余额）
  reserved               INTEGER NOT NULL DEFAULT 0,      -- microunit（冻结中，未消费）
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (balance >= 0),
  CHECK (reserved >= 0)
);
```

**并发控制**（见 `09-transactions.md § 7`）：

- 用 `BEGIN IMMEDIATE` 拿写锁（SQLite 无行级锁）
- 冻结：条件更新 `WHERE balance >= ?`，`rowsAffected == 0` 视为余额不足或冲突
- **禁止**：先 SELECT 判 balance 再 UPDATE 的"检查-然后-操作"模式

**同事务原子性**：`wallet.balance` / `wallet.reserved` 变更必须与 `wallet_ledger` 插入**在同一事务**。

## 5. 积分流水 · `wallet_ledger`

```sql
CREATE TABLE wallet_ledger (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  seq                    INTEGER NOT NULL,               -- 该乘客的序号（严格递增）
  reason                 TEXT NOT NULL,                  -- see enum below
  amount                 INTEGER NOT NULL,               -- microunit，带符号
  balance_after          INTEGER NOT NULL,               -- 该笔后的余额
  ref_type               TEXT,                            -- pull_round | topup_order | cdk | ...
  ref_id                 TEXT,
  memo                   TEXT,
  created_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  UNIQUE (passenger_id, seq)
);
```

**`reason` 枚举**（跟 `03-modules.md wallet` 一致）：

- `recharge` · 兑换码 / payment-gateway 到账
- `channel_fee` · 通道费明细（负值，pass-through 记账，不重复扣 balance）
- `redeem` · 兑换码兑换（已并入 recharge？看实现）
- `key_cost` · 号价（负值）
- `single_pull_fee` · 单次议价（负值）
- `capability_fee` · 附加能力费（负值，memo 里写具体附加能力）
- `service_fee` · 服务费（负值）
- `warranty_refund` · 质保退款（正值）
- `admin_adjust` · 手动调整

**索引**：`(passenger_id, seq)`, `(passenger_id, created_at DESC)`, `(reason)`。

## 6. 兑换码 · `cdk` + `cdk_redemption`

```sql
CREATE TABLE cdk (
  code                   TEXT PRIMARY KEY,               -- 大小写、连字符归一化后
  quota                  INTEGER NOT NULL,               -- microunit
  status                 TEXT NOT NULL DEFAULT 'active', -- active | redeemed | expired | revoked
  batch_id               TEXT,                            -- 批次（管理端生成时打）
  expires_at             TEXT,
  redeemed_by            TEXT,                            -- passenger_id
  redeemed_at            TEXT,
  created_at             TEXT NOT NULL,
  created_by             TEXT                             -- admin_id
);

CREATE TABLE cdk_redemption (
  id                     TEXT PRIMARY KEY,
  cdk_code               TEXT NOT NULL,
  passenger_id           TEXT NOT NULL,
  quota                  INTEGER NOT NULL,
  redeemed_at            TEXT NOT NULL,
  ledger_id              TEXT NOT NULL,                  -- 关联的 wallet_ledger 行
  FOREIGN KEY (cdk_code) REFERENCES cdk(code),
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
```

**索引**：`cdk(status, expires_at)`, `cdk_redemption(passenger_id, redeemed_at)`。

## 7. 充值单 · `payment_order`

```sql
CREATE TABLE payment_order (
  id                          TEXT PRIMARY KEY,
  passenger_id                TEXT NOT NULL,
  channel                     TEXT NOT NULL,             -- waffo | ...
  amount_original             INTEGER NOT NULL,          -- 乘客付的（microunit）
  currency                    TEXT NOT NULL DEFAULT 'CNY',
  channel_fee                 INTEGER NOT NULL,          -- 通道费（microunit，正值）
  amount_credited             INTEGER NOT NULL,          -- 到账积分 = original - channel_fee
  status                      TEXT NOT NULL,             -- pending | paid | failed | cancelled | refunded
  gateway_order_id            TEXT,                       -- waffo 侧订单号
  pay_url                     TEXT,
  paid_at                     TEXT,
  created_at                  TEXT NOT NULL,
  updated_at                  TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
```

**索引**：`(passenger_id, created_at DESC)`, `(status)`, `(gateway_order_id)`。

**幂等**：以 `gateway_order_id` 为幂等键（vs waffo webhook 重投）。

## 8. Bus · `bus` + `bus_member`

```sql
CREATE TABLE bus (
  id                     TEXT PRIMARY KEY,
  name                   TEXT NOT NULL,
  kind                   TEXT NOT NULL,                   -- single | anon | team
  creator_passenger_id   TEXT NOT NULL,
  invite_code            TEXT UNIQUE,                     -- team 才有
  refill_watermark       INTEGER NOT NULL DEFAULT 0,      -- 号数低于水位触发补车
  max_members            INTEGER,                          -- anon / team 才有
  status                 TEXT NOT NULL DEFAULT 'active',  -- active | dissolved
  created_at             TEXT NOT NULL,
  dissolved_at           TEXT,
  FOREIGN KEY (creator_passenger_id) REFERENCES passenger(id)
);

CREATE TABLE bus_member (
  bus_id                 TEXT NOT NULL,
  passenger_id           TEXT NOT NULL,
  role                   TEXT NOT NULL DEFAULT 'member',  -- member | owner
  joined_at              TEXT NOT NULL,
  left_at                TEXT,                             -- 退出后不删行，留历史
  PRIMARY KEY (bus_id, passenger_id, joined_at),
  FOREIGN KEY (bus_id) REFERENCES bus(id),
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
```

**索引**：`bus(creator_passenger_id, status)`, `bus(invite_code)`, `bus(kind, status)`, `bus_member(passenger_id, left_at)`, `bus_member(bus_id, left_at)`。

**注意**：`bus.kind = single`（1 人 bus）也进 bus 表；没有单独的"solo bus"表（跟 `CLAUDE.md §1 术语铁律`一致）。

## 9. 拉号意图 · `pull_intent`

```sql
CREATE TABLE pull_intent (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  bus_id                 TEXT,                             -- null = 单独拉号（次入口）
  target                 TEXT NOT NULL,                    -- to-bus | to-record
  count_requested        INTEGER NOT NULL,
  constraints_json       TEXT,                             -- JSON: {max_unit_price, ...}
  status                 TEXT NOT NULL DEFAULT 'pending',  -- pending | in_flight | coalesced | fulfilled | failed | cancelled
  batch_id               TEXT,                             -- coalesce 合流后同一 batch
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (bus_id) REFERENCES bus(id)
);
```

**索引**：`(passenger_id, status, created_at)`, `(bus_id, status)`, `(status, created_at)` (集单扫描)。

## 10. 拉号轮次 · `pull_round`

```sql
CREATE TABLE pull_round (
  id                        TEXT PRIMARY KEY,
  vendor_id                 TEXT NOT NULL,
  client_order_id           TEXT NOT NULL,                  -- 32 hex；给 vendor 的幂等键
  bus_id                    TEXT,                            -- 拼车拉号 or NULL（单独拉号）
  count_requested           INTEGER NOT NULL,
  count_purchased           INTEGER NOT NULL,
  key_cost_total            INTEGER NOT NULL,               -- microunit
  single_pull_fee_total     INTEGER NOT NULL DEFAULT 0,
  capability_fee_total      INTEGER NOT NULL DEFAULT 0,
  service_fee_total         INTEGER NOT NULL,
  participants_split_json   TEXT NOT NULL,                  -- JSON: {passenger_id: count} 用于分摊
  status                    TEXT NOT NULL,                  -- initiated | completed | failed | partial | refunded
  vendor_response_json      TEXT,                            -- 原始响应存档
  vendor_order_id           TEXT,                            -- vendor 侧订单 id
  created_at                TEXT NOT NULL,
  completed_at              TEXT,
  UNIQUE (vendor_id, client_order_id)
);
```

**索引**：`(bus_id, created_at DESC)`, `(vendor_id, created_at DESC)`, `(status)`。

**幂等**：`UNIQUE (vendor_id, client_order_id)` 保证 vendor 侧一次动作只落一行。

## 11. 号台账 · `credential_ledger`

**号归属 = Bus**（见 `decisions.md §3.7`）。号属于 bus_id；成员通过 bus_member 权限访问。个人单独拉号进的 `record-<pid>` group 视为"pid 名下私有 bus"（简化模型）。

```sql
CREATE TABLE credential_ledger (
  id                          TEXT PRIMARY KEY,           -- 我方内部 id
  kiro_rs_credential_id       INTEGER NOT NULL UNIQUE,    -- housepool (kiro.rs) 里的 credential id (u64)
  owner_bus_id                TEXT,                        -- 号所归属的 bus（若在 bus group 里）
  owner_record_passenger_id   TEXT,                        -- 或号在 record-<pid> group 里，属于该乘客的拉号记录
  current_group               TEXT NOT NULL,               -- bus-<id> | record-<pid> | market
  vendor_id                   TEXT NOT NULL,
  vendor_order_id             TEXT,                        -- vendor 侧的批次订单 id
  source_pull_round_id        TEXT NOT NULL,               -- 从哪一次拉号来
  status                      TEXT NOT NULL,               -- alive | dead | handed_off
  disabled                    INTEGER NOT NULL DEFAULT 0,  -- 是否在 housepool 侧 disabled
  pulled_at                   TEXT NOT NULL,
  dead_at                     TEXT,
  death_source                TEXT,                        -- housepool_probe | vendor_webhook | vendor_poll
  handed_off_at               TEXT,                        -- handoff 时间
  FOREIGN KEY (owner_bus_id) REFERENCES bus(id),
  FOREIGN KEY (owner_record_passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (source_pull_round_id) REFERENCES pull_round(id),
  CHECK (
    (owner_bus_id IS NOT NULL AND owner_record_passenger_id IS NULL) OR
    (owner_bus_id IS NULL AND owner_record_passenger_id IS NOT NULL)
  )
);
```

**索引**：`(owner_bus_id, status)`, `(owner_record_passenger_id, status)`, `(current_group)`, `(vendor_id, status)`, `(status, dead_at)`, `(pulled_at)`。

**注意**：
- **credential 明文永不落库**（在 housepool = kiro.rs 里）。本表只是我方的**台账**（who / when / where / dead？）
- **handoff 后 `status = handed_off`**，明文早已交给乘客、housepool 里已 DELETE
- **权限查询**：拼车 bus 里的号，成员通过 `bus_member` 表能拿访问权；1 人 bus 也走同一 join
- **号在 record group 时** `owner_bus_id IS NULL`，走 `owner_record_passenger_id`

## 12. 平均寿命聚合视图 · `vendor_lifespan_snapshot`

```sql
CREATE TABLE vendor_lifespan_snapshot (
  id                     TEXT PRIMARY KEY,
  vendor_id              TEXT NOT NULL,
  window                 TEXT NOT NULL,                  -- '24h' | '7d' | '30d'
  sample_count           INTEGER NOT NULL,
  avg_lifespan_seconds   INTEGER NOT NULL,
  median_lifespan_seconds INTEGER NOT NULL,
  observed_at            TEXT NOT NULL,
  UNIQUE (vendor_id, window, observed_at)
);
```

**用途**：`decider` 决策比价读这张表（"每积分能活多久" = 号价 / 平均寿命）。

**索引**：`(vendor_id, window, observed_at DESC)`。

## 12.5 号用量快照 · `credential_usage_snapshot`

从 kiro.rs `GET /stats/by-credential` 定期拉快照落进本表，用于 UI 展示"每号用了多少 / 平均积分消耗 / 平均并发"。

```sql
CREATE TABLE credential_usage_snapshot (
  id                        TEXT PRIMARY KEY,
  kiro_rs_credential_id     INTEGER NOT NULL,           -- housepool 里的 credential id
  window                    TEXT NOT NULL,              -- '24h' | '7d' | '30d'
  calls                     INTEGER NOT NULL DEFAULT 0,
  input_tokens              INTEGER NOT NULL DEFAULT 0,
  output_tokens             INTEGER NOT NULL DEFAULT 0,
  errors                    INTEGER NOT NULL DEFAULT 0,
  credits_used              INTEGER NOT NULL DEFAULT 0, -- microunit，从 vendor 或 kiro.rs 拿
  avg_credits_per_day       INTEGER NOT NULL DEFAULT 0, -- microunit
  concurrency_avg           INTEGER,                     -- **kiro.rs 未直接给，可能是 null**
  observed_at               TEXT NOT NULL,
  UNIQUE (kiro_rs_credential_id, window, observed_at)
);
```

**索引**：`(kiro_rs_credential_id, window, observed_at DESC)`。

**采集节奏**：每 5-15 分钟一次（阶段 1d 可配置）。

**`concurrency_avg` 字段现状**：
- kiro.rs 未提供直接读端点（`POST /credentials/{id}/clear-concurrency` 存在为证——内部有并发计数）
- 三条出路（详见 `03-modules.md · housepool/kirors` 备注）：
  - (a) 给 kiro.rs 加 `GET /credentials/{id}/concurrency`
  - (b) 我方采样聚合
  - (c) 反推（不可用）
- 未拍板前该字段常态 `NULL`，UI 显示 `—`

## 12.6 Bus 用量聚合视图 · `bus_usage_snapshot`

```sql
CREATE TABLE bus_usage_snapshot (
  id                              TEXT PRIMARY KEY,
  bus_id                          TEXT NOT NULL,
  window                          TEXT NOT NULL,          -- '24h' | '7d' | '30d'
  alive_count                     INTEGER NOT NULL,
  dead_count                      INTEGER NOT NULL,
  avg_lifespan_seconds            INTEGER NOT NULL,
  total_calls                     INTEGER NOT NULL,
  total_credits_used              INTEGER NOT NULL,       -- microunit
  avg_credits_per_cred_per_day    INTEGER NOT NULL,       -- microunit
  errors_rate                     REAL NOT NULL,          -- 0.0..1.0
  concurrency_avg                 INTEGER,                 -- 同上，可能 null
  observed_at                     TEXT NOT NULL,
  UNIQUE (bus_id, window, observed_at)
);
```

**用途**：Bus 详情页 UI 读这张表；跨窗口对比时用。

**索引**：`(bus_id, window, observed_at DESC)`。

## 13. Webhook 入向去重 · `vendor_webhook_delivery`

```sql
CREATE TABLE vendor_webhook_delivery (
  id                     TEXT PRIMARY KEY,
  vendor_id              TEXT NOT NULL,
  event_id               TEXT NOT NULL,
  event_type             TEXT NOT NULL,
  payload                TEXT NOT NULL,                  -- 原始 body
  signature_valid        INTEGER NOT NULL,
  received_at            TEXT NOT NULL,
  processed_at           TEXT,
  UNIQUE (vendor_id, event_id)
);
```

**索引**：`(vendor_id, received_at DESC)`, `(event_type, received_at DESC)`。

## 14. Webhook 出向投递 · `outbound_webhook_delivery`

```sql
CREATE TABLE outbound_webhook_delivery (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  event_id               TEXT NOT NULL,                  -- 我方生成
  event_type             TEXT NOT NULL,
  target_url             TEXT NOT NULL,
  payload                TEXT NOT NULL,
  attempt                INTEGER NOT NULL DEFAULT 1,
  status                 TEXT NOT NULL,                  -- pending | delivered | failed | dropped
  response_status        INTEGER,
  response_body_snippet  TEXT,
  next_retry_at          TEXT,
  delivered_at           TEXT,
  created_at             TEXT NOT NULL
);
```

**索引**：`(passenger_id, created_at DESC)`, `(status, next_retry_at)`。

## 15. 附加能力插槽 · `capability_slot`

```sql
CREATE TABLE capability_slot (
  id                     TEXT PRIMARY KEY,
  name                   TEXT NOT NULL UNIQUE,           -- e.g. 'stable_priority'（阶段 1 无实例）
  rate_bp                INTEGER NOT NULL,               -- 议价率，basis point（2000 = 20%）
  active                 INTEGER NOT NULL DEFAULT 0,     -- 是否启用
  applies_when_json      TEXT,                            -- 何时自动勾选（例：策略参数满足条件）
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL
);

CREATE TABLE pull_round_capability (
  pull_round_id          TEXT NOT NULL,
  capability_id          TEXT NOT NULL,
  fee_amount             INTEGER NOT NULL,               -- 该项目对这轮的收费（microunit）
  PRIMARY KEY (pull_round_id, capability_id),
  FOREIGN KEY (pull_round_id) REFERENCES pull_round(id),
  FOREIGN KEY (capability_id) REFERENCES capability_slot(id)
);
```

**阶段 1 空表**（无实例）；阶段 2c 加"稳定优先"时才写第一行。

## 16. 策略 · `passenger_strategy`

```sql
CREATE TABLE passenger_strategy (
  passenger_id             TEXT PRIMARY KEY,
  auto_enabled             INTEGER NOT NULL DEFAULT 0,
  per_round_count          INTEGER,
  min_count                INTEGER,
  keep_safety_stock        INTEGER,
  max_unit_price           INTEGER,                      -- microunit
  daily_round_limit        INTEGER,
  daily_spend_limit        INTEGER,                      -- microunit
  target_bus_id            TEXT,                          -- 自动拉进哪辆 bus
  updated_at               TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (target_bus_id) REFERENCES bus(id)
);
```

## 17. Vendor 账户凭证（我方在 vendor 那的账号）· `vendor_account`

```sql
CREATE TABLE vendor_account (
  id                              TEXT PRIMARY KEY,
  vendor_id                       TEXT NOT NULL,          -- 91kiro | kiroceo | ...
  label                           TEXT,                    -- 备注
  auth_scheme                     TEXT NOT NULL,           -- api_key | bearer | cookie
  secret_credentials_encrypted    BLOB NOT NULL,           -- AES-GCM
  status                          TEXT NOT NULL DEFAULT 'active',
  created_at                      TEXT NOT NULL,
  updated_at                      TEXT NOT NULL
);
```

**索引**：`(vendor_id, status)`。

**明文永不落库**。

## 18. 每日聚合（限制检查用）· `passenger_daily_counter`

```sql
CREATE TABLE passenger_daily_counter (
  passenger_id           TEXT NOT NULL,
  date                   TEXT NOT NULL,                  -- 'YYYY-MM-DD' (UTC or CST，配置决定)
  round_count            INTEGER NOT NULL DEFAULT 0,
  spend_total            INTEGER NOT NULL DEFAULT 0,     -- microunit
  PRIMARY KEY (passenger_id, date)
);
```

用途：`strategy` 判 `daily_round_limit / daily_spend_limit` 时读；每次拉号成功后原子 +1 / += amount。

---

## 关系图（简略）

```
passenger ──┬─ passenger_api_key
            ├─ passenger_downstream
            ├─ passenger_strategy
            ├─ passenger_daily_counter
            ├─ wallet ─── wallet_ledger
            ├─ payment_order
            ├─ cdk_redemption ─── cdk
            ├─ bus (creator) ── bus_member ─── passenger
            ├─ pull_intent ── pull_round ── credential_ledger
            └─ outbound_webhook_delivery

vendor_account (我方 vendor 账号) --独立--
vendor_webhook_delivery         --独立--
vendor_lifespan_snapshot        --聚合视图--
capability_slot / pull_round_capability （阶段 2c 起写）
```

## Migration 顺序（1a 首批）

**首批 migration 覆盖阶段 1a 所需 16 张表**（其余延后）：

1. `passenger`, `passenger_api_key`, `passenger_downstream`
2. `wallet`（**含 reserved 字段**）, `wallet_ledger`
3. `bus`, `bus_member`
4. `pull_intent`, `pull_round`, `credential_ledger`（**含 owner_bus_id / owner_record_passenger_id**）
5. `vendor_account`, `passenger_daily_counter`
6. `idempotency_record`（HTTP 幂等）
7. `pending_purchase`, `pending_assignment`（1a 只上这两个；`pending_topup` 是 1b）

**1b 加**：`cdk`, `cdk_redemption`, `payment_order`

**1d 加**：`vendor_lifespan_snapshot`, `vendor_webhook_delivery`, `credential_usage_snapshot`, `bus_usage_snapshot`

**1e 加**：`outbound_webhook_delivery`

**2c 加**：`capability_slot`, `pull_round_capability`

**3+**：管理端 / 市场相关表另写

## 19. HTTP 幂等 · `idempotency_record`

见 `09-transactions.md § 6` 完整说明。

```sql
CREATE TABLE idempotency_record (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  method                 TEXT NOT NULL,
  path                   TEXT NOT NULL,
  idempotency_key        TEXT NOT NULL,           -- 客户端 X-Idempotency-Key (32 hex)
  request_fingerprint    TEXT NOT NULL,           -- sha256(canonical body) - 防冲突
  response_status        INTEGER,
  response_headers       TEXT,                     -- JSON
  response_body          BLOB,                     -- 首次响应体（handoff 场景不含明文 keys）
  created_at             TEXT NOT NULL,
  first_completed_at     TEXT,
  UNIQUE (passenger_id, path, idempotency_key)
);
```

**索引**：`(passenger_id, path, idempotency_key)`, `(created_at)`。

**过期**：`created_at` 早于 30 天前的记录 janitor 清理（幂等窗口）。

**handoff 特例**：response_body 存"状态版"（不含 credential 明文）；重放响应 `already_delivered: true` + credential_ids 数组 + 空 keys 数组。

## 20. 状态机持久化表 · `pending_purchase` / `pending_assignment` / `pending_topup`

见 `09-transactions.md § 2/3/4/5` 完整状态定义。

### `pending_purchase`

```sql
CREATE TABLE pending_purchase (
  id                       TEXT PRIMARY KEY,
  idempotency_record_id    TEXT NOT NULL,
  passenger_id             TEXT NOT NULL,
  bus_id                   TEXT,                          -- 拼车拉号时；单独拉号 null
  target_group             TEXT NOT NULL,                 -- bus-<id> | record-<pid>
  vendor_id                TEXT NOT NULL,
  client_order_id          TEXT NOT NULL,                 -- vendor 幂等键 (32 hex)
  count_requested          INTEGER NOT NULL,
  reserved_amount          INTEGER NOT NULL,              -- 冻结的积分
  status                   TEXT NOT NULL,                 -- initial | reserved | purchased | imported | completed | cancelled_reserve | need_recover_vendor | need_manual
  vendor_order_id          TEXT,                          -- purchased 后填
  pull_round_id            TEXT,                          -- completed 时关联到 pull_round
  error                    TEXT,
  created_at               TEXT NOT NULL,
  updated_at               TEXT NOT NULL,
  UNIQUE (vendor_id, client_order_id),
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (bus_id) REFERENCES bus(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id),
  FOREIGN KEY (pull_round_id) REFERENCES pull_round(id)
);
```

**索引**：`(status, updated_at)` （janitor 扫超时用），`(passenger_id, created_at)`。

### `pending_assignment`

```sql
CREATE TABLE pending_assignment (
  id                    TEXT PRIMARY KEY,
  idempotency_record_id TEXT NOT NULL,
  passenger_id          TEXT NOT NULL,
  credential_id         TEXT NOT NULL,                    -- 我方 credential_ledger.id
  target                TEXT NOT NULL,                    -- to-bus | to-passengerpool | handoff
  target_bus_id         TEXT,                              -- target='to-bus' 时
  status                TEXT NOT NULL,                    -- initial | external_done | status_updated | completed | need_manual
  error                 TEXT,
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (credential_id) REFERENCES credential_ledger(id),
  FOREIGN KEY (target_bus_id) REFERENCES bus(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id)
);
```

**索引**：`(status, updated_at)`, `(passenger_id, created_at)`。

### `pending_topup`

```sql
CREATE TABLE pending_topup (
  id                     TEXT PRIMARY KEY,
  idempotency_record_id  TEXT NOT NULL,
  passenger_id           TEXT NOT NULL,
  payment_order_id       TEXT NOT NULL,
  status                 TEXT NOT NULL,                   -- initial | gateway_ordered | gateway_paid | credited | completed | expired | cancelled | refunded | pending_manual
  error                  TEXT,
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (payment_order_id) REFERENCES payment_order(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id)
);
```

**索引**：`(status, updated_at)`。

## 备份 / 迁移

- SQLite `.backup` 每小时 dump 一次
- 加密字段主密钥**分离备份**（不跟 DB 一起）
- 迁移到 PostgreSQL 的门槛：单机 QPS 撑不住 时（预计阶段 2 之后）

## 待定（阶段 1a 落码时确定）

- 时间是否用 CST 还是 UTC —— **建议 UTC 存 + UI 层转 CST**
- money 是否真用 microunit —— **建议是**，跟 91kiro/kiroappio 精度对齐
- UUID v7 生成库（Go 有多个实现）—— **落码时选**
- 索引具体的 SQL —— **每张表首次 migration 时定**
