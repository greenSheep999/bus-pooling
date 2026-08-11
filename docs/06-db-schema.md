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
| 软删 | 默认硬删；只有 `passenger` / `bus` / `topup_order` 需要软删（加 `deleted_at`） |

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
  -- 用户档次（decisions §8.39 · 三档 · 一档多减一层）
  --   retail    = 零售 · 无系统邀请码 · 全套加价
  --   wholesale = 批发 · 社群码注册（TG/Discord）· 免区域附加费
  --   insider   = 同行 · 同行码注册（同行群邀请制）· 免 vendor + 区域附加费
  tier                   TEXT NOT NULL DEFAULT 'retail'
                         CHECK(tier IN ('retail','wholesale','insider')),
  -- 兜底字段 · 下次 schema 变更删（decisions §8.39）· 迁移：invited=true → tier=insider
  invited                INTEGER NOT NULL DEFAULT 0,
  invite_code_used       TEXT,                            -- 注册时用的码（留痕 · 便于追来源）
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  last_login_at          TEXT,
  deleted_at             TEXT                             -- 软删
);
```

**索引**：`(username)`, `(email)`, `(status)`, `(tier)`（按档次统计 / 加价链读取）。

**依赖**：`internal/passenger`（Layer 3a）。

**tier 迁移**（`decisions §8.39`）：
- 现有 `invited = 1` 全部迁到 `tier = 'insider'`（保守 · 老用户不掉档）
- 现有 `invited = 0` 迁到 `tier = 'retail'`
- 加价链一律读 `tier`，不再读 `invited`
- `invited` 字段作为兜底保留 · 下次 schema 变更时移除

## 1.5 会话 · `session`

自建登录用：cookie 存随机 token，映射到本表；服务端每次请求校验。

```sql
CREATE TABLE session (
  id                     TEXT PRIMARY KEY,              -- session token（32 hex）hash 后的 SHA-256
  passenger_id           TEXT NOT NULL,
  ip_created             TEXT,                           -- 登录时的 IP（可选，安全审计用）
  user_agent             TEXT,                           -- 登录时的 UA（可选）
  created_at             TEXT NOT NULL,
  last_used_at           TEXT NOT NULL,
  expires_at             TEXT NOT NULL,                  -- 默认 30 天；"记住我" 可延长
  revoked_at             TEXT,                           -- 主动登出 or 密码变更
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
```

**索引**：`(passenger_id, revoked_at)`, `(expires_at)`（janitor 清过期）。

**cookie**：**httpOnly + Secure + SameSite=Lax**；名字 `bp_session`；值是 token 明文（服务端存 hash）。

**清理**：janitor 定时删 `expires_at < now` 或 `revoked_at IS NOT NULL AND ...` 的行。

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

**并发控制**（见 `09-transactions.md § 8`）：

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

## 6. 兑换码 · `redeem_code`（单表 · 迁移 002）

早前设计文档写 `cdk` + `cdk_redemption` 两表·实际 1a 落地时合并成一张 `redeem_code`
（成人 code 直接携带 used_by / used_at · 不需要独立 redemption 事件表 · redemption
事件通过 `wallet_ledger` 的 `redeem` reason + `ref_id = code` 已能追溯）。

```sql
CREATE TABLE redeem_code (
  code                   TEXT PRIMARY KEY,               -- 大小写敏感·生成时统一大写
  credits                INTEGER NOT NULL,               -- microunit · 兑换到手积分
  status                 TEXT NOT NULL,                  -- unused | used | expired
  used_by                TEXT,                            -- passenger.id
  used_at                TEXT,
  expires_at             TEXT,                            -- NULL = 不过期
  memo                   TEXT,                            -- 批次 / 活动等
  created_at             TEXT NOT NULL,
  FOREIGN KEY (used_by) REFERENCES passenger(id),
  CHECK (credits > 0),
  CHECK (status IN ('unused', 'used', 'expired')),
  CHECK (
    (status = 'unused' AND used_by IS NULL AND used_at IS NULL) OR
    (status = 'used'   AND used_by IS NOT NULL AND used_at IS NOT NULL) OR
    (status = 'expired')
  )
);
```

**索引**：`idx_redeem_used_by(used_by)`。

**兑换事件溯源**：`wallet_ledger WHERE reason='redeem' AND ref_id=<code>` 拿到"哪个乘客
什么时候用哪码·换了多少积分"·不需要独立表。

## 7. 充值单 · `topup_order`（迁移 002/005/006）

早前设计文档写 `payment_order`·实际 1a 落地时叫 `topup_order`（跟 API 端点
`/api/me/topup` 保持一致）。1b 之后又加了几列（`gateway_payment_id` / `checkout_url`
/ `qr_content` · 迁移 005）·refund 状态支持（迁移 006）。**表名不改** —— 迁移已上线。

```sql
CREATE TABLE topup_order (
  id                        TEXT PRIMARY KEY,                -- UUID v7 · 对外叫 order_id
  passenger_id              TEXT NOT NULL,
  channel                   TEXT NOT NULL,                    -- 具体渠道 id · 由 topupchannel registry 定义
  credits                   INTEGER NOT NULL,                 -- microunit · 净到账
  channel_fee               INTEGER NOT NULL,                 -- microunit · 手续费
  paid                      INTEGER NOT NULL,                 -- microunit · credits + channel_fee
  pay_url                   TEXT NOT NULL,                    -- checkout URL 占位·gateway 建后 AttachGateway 回填
  status                    TEXT NOT NULL,                    -- pending | paid | expired | cancelled | refunded | reversed
  expires_at                TEXT NOT NULL,
  paid_at                   TEXT,
  wallet_ledger_id          TEXT,                             -- MarkPaid 后指向 recharge 那条流水
  gateway_payment_id        TEXT,                             -- 005 加·gateway 分配的支付单 id
  checkout_url              TEXT,                             -- 005 加·真 checkout URL
  qr_content                TEXT,                             -- 005 加·扫码支付内容（USDT 等）
  region                    TEXT,                             -- 010 加·地区分类（cn / overseas）
  rail                      TEXT,                             -- 010 加·到账方式（direct / hosted）
  provider_kind             TEXT,                             -- 010 加·gateway 侧 rail 名
  payer_reference           TEXT,                             -- 010 加·direct rail 需要（UID / ID）
  gateway_request_snapshot  BLOB,                             -- 016 加·起单时冷冻的 CreatePaymentRequest JSON · janitor 反查用它幂等重发
  created_at                TEXT NOT NULL,
  updated_at                TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (credits > 0),
  CHECK (channel_fee >= 0),
  CHECK (paid = credits + channel_fee)
  -- channel CHECK 在 010 rebuild 时放宽·允许多渠道（waffo / bybit_internal / binance_internal / epusdt）
  -- 状态 CHECK 在 006 rebuild 时放宽·允许 refunded / reversed
);
```

**索引**：`idx_topup_passenger_time(passenger_id, created_at DESC)` · `idx_topup_status(status, expires_at)`。

**幂等**：webhook 侧以 `X-404bus-Event-Id` 为幂等键·记录在 `settlement_event` 表（迁移 005）。
反查我方 order 优先用 `gateway_payment_id` · fallback `client_order_id = topup_order.id`
（P0-A 修·见 `09-transactions §6`）。

## 8. Bus · `bus` + `bus_member`

```sql
CREATE TABLE bus (
  id                     TEXT PRIMARY KEY,
  name                   TEXT NOT NULL,
  -- kind 只区分"谁建的"·**不区分能不能加人**（见下面 §车 kind 语义）
  kind                   TEXT NOT NULL,                   -- single / team = 用户建 · anon = 系统撮合池
  creator_passenger_id   TEXT NOT NULL,
  invite_code            TEXT UNIQUE,                     -- 用户建的车一律有（anon 池没有）
  max_members            INTEGER,                          -- 系统统一 · 走 config.bus.max_members
  status                 TEXT NOT NULL DEFAULT 'active',  -- active | dissolved
  created_at             TEXT NOT NULL,
  dissolved_at           TEXT,

  -- 补车策略（每车一策略 · decisions §8.6）
  auto_refill_enabled    INTEGER NOT NULL DEFAULT 0,      -- 0/1 · 号死自动补
  refill_watermark       INTEGER NOT NULL DEFAULT 0,      -- 活号低于水位（号数）触发补车
  refill_min_count       INTEGER,                          -- 每轮至少补到 N 号
  per_round_count        INTEGER,                          -- 每轮拉几号
  max_unit_price         INTEGER,                          -- microunit · 单号最高价
  daily_round_limit      INTEGER,                          -- 每日最多拉号次数
  daily_spend_limit      INTEGER,                          -- microunit · 每日花费上限
  preferred_vendor       TEXT,                              -- NULL = 有效成本比价自动选

  -- 1c-1 · 匿名撮合（migration 011）
  anon_zone              TEXT,                              -- anon 撮合的 zone 过滤（相同 zone 才匹配）· NULL = 不限
  anon_max_unit_price    INTEGER,                            -- microunit · anon 撮合价格上限 · NULL = 不限

  FOREIGN KEY (creator_passenger_id) REFERENCES passenger(id)
);

CREATE TABLE bus_member (
  bus_id                 TEXT NOT NULL,
  passenger_id           TEXT NOT NULL,
  role                   TEXT NOT NULL DEFAULT 'member',  -- member | owner
  joined_at              TEXT NOT NULL,
  left_at                TEXT,                             -- 退出后不删行，留历史
  -- ↓ 阶段 2a 才用（多人车）· 1a 全是 1 人车，owner 恒 share_pct=100 / status=active
  -- 列在 1a 就建好，免得 2a 改表（SQLite ALTER 受限）
  share_pct              INTEGER NOT NULL DEFAULT 100,     -- 分摊比例 · 全车加起来 = 100（§8.18 §8.23）
  status                 TEXT NOT NULL DEFAULT 'active',   -- active | suspended（§8.26）
  skipped_count          INTEGER NOT NULL DEFAULT 0,       -- 连续因余额不足被跳过几次 · 到 3 自动挂起 · 充值归零
  last_skipped_at        TEXT,
  PRIMARY KEY (bus_id, passenger_id, joined_at),
  FOREIGN KEY (bus_id) REFERENCES bus(id),
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  CHECK (share_pct BETWEEN 0 AND 100),
  CHECK (status IN ('active', 'suspended'))
);
```

**为什么 2a 的列现在就建**：SQLite 的 `ALTER TABLE` 只能加列不能加 CHECK / 改约束，而且这几列有默认值、1 人车语义天然成立（100% / active / 0 次）。1a 建好 = 2a 只写逻辑不动表。

**挂起语义**（§8.26）：`status='suspended'` 的成员**不参与分摊**、也**没有 client_key**（取不到号），但 `share_pct` **保留** —— 这正是"挂起"跟"移除"的区别。移除要改其他人的 share_pct —— **车主有权直接做**（§8.36 覆盖了原先的全员确认要求）。

**索引**：`bus(creator_passenger_id, status)`, `bus(invite_code)`, `bus(kind, status)`, `bus_member(passenger_id, left_at)`, `bus_member(bus_id, left_at)`。

### 车 kind 语义（1c 定稿 · 别再当三种类型看）

**一辆车就是一辆车**（`CLAUDE.md §2`）。`kind` **只记录"谁建的"**，不决定行为：

| kind | 谁建的 | 有邀请码 | 能加人 | 说明 |
|---|---|---|---|---|
| `single` | 用户 | ✅ | ✅ | 历史值 · 行为跟 `team` **完全一致** |
| `team` | 用户 | ✅ | ✅ | 跟 `single` 无差别 · 新建车用哪个都行 |
| `anon` | 系统 | ❌ | ✅ | 系统撮合池 · 谁进由撮合决定·不靠码 |

**人数是状态不是类型**：车里 1 个人 = 独享，把邀请码给朋友、朋友进来 = 多人拼车。**同一辆车**，不需要"换类型"，`kind` 一个字都不用改。

**因此代码里不允许出现**：
- ❌ `if kind == team` 才给邀请码 —— 用户建的车一律有码
- ❌ `if kind != single` 才允许 Join —— 任何车都能加人
- ❌ UI 上出现"1 人车 / 邀请码车"这类**类型**标签 —— 只按 `member_count` 显示"独享 / N 人拼车"

**老数据自愈**：1c 之前建的 `single` 车没生成过邀请码。不做 migration，`bus.Store.EnsureInviteCode` 在读车时补一个（幂等 · 条件 UPDATE · 并发安全）。

**没有单独的 "solo bus" 表**（跟 `CLAUDE.md §1 术语铁律`一致）。

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
  pushed_to_passengerpool_at  TEXT,                        -- 号是否推给了乘客 passengerpool（双写状态）· NULL = 未推 · 有值 = 已推
  -- 推送失败 · **结构化**（decisions §8.24 §8.25）· 原来只有一个 TEXT 存不下，
  -- 但售后要靠这几个字段判断"是用户配错还是我方问题"、以及能不能重试
  push_error_code             TEXT,                        -- unauthorized | timeout | conflict | server_error | …
  push_error_status           INTEGER,                     -- HTTP 状态码 · 超时类为 NULL
  push_error_message          TEXT,                        -- 人话原因（对外展示）
  push_error_retriable        INTEGER,                     -- 0/1 · 决定 UI 给「重试」还是「去检查配置」
  push_attempts               INTEGER NOT NULL DEFAULT 0,  -- 已试次数（退避 5s/30s/5min 共 3 次）
  push_last_attempt_at        TEXT,
  -- 质保窗口截止 · 各 vendor 10-30 分钟不等（docs/vendors/*.md §13）
  -- 号在这之前死 → 跟随上游退款（00 §7.5 规则 B）· UI 上「质保内失效·可退」就靠它判
  warranty_until              TEXT,
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
- **台账行永不删** —— housepool 里的 credential 删了，本表的行留着（售后追溯的唯一依据，见 `decisions §8.24`）

### 11.1 售后追溯字段（`decisions §8.24`）

handoff / 推送失败后用户找客服，客服必须能定位到具体是哪个号。原表只有内部 id，对不上用户说的「我那个 xxx 结尾的号」，所以补：

```sql
-- 首批 001 迁移里漏了这三列 · 由 004_credential_ledger_traceability.sql 补上
ALTER TABLE credential_ledger ADD COLUMN key_masked   TEXT;    -- ksk_live_xxxx…xxx · 客服/用户对号的唯一凭据
ALTER TABLE credential_ledger ADD COLUMN region       TEXT;    -- us-east-1 | eu-central-1
ALTER TABLE credential_ledger ADD COLUMN credits_used INTEGER; -- 交付/失效那一刻已耗额度（microunit）
```

- `key_masked` **不是明文**（只有前缀 + 后 3 位），可以安全长期保留
- 三个字段在**号入池时写入**，handoff / dead 后**不再变更**（快照语义）
- 客服工作流：用户报「ksk_live_9f4a…vn6 有问题」→ 按 `key_masked` 查台账 → 看到 vendor / 拉号时间 / 交付时间 / 已耗额度 / 死亡来源 → 判断是否质保内、要不要退款
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
  window                    TEXT NOT NULL,              -- '1h' | '24h' | '7d' | '30d'   （1h 用于号详情抽屉 24 柱图）
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
  latency_ms             INTEGER,                        -- 单次投递耗时（ms）· 前端 WebhookDelivery 展示用
  next_retry_at          TEXT,
  delivered_at           TEXT,
  created_at             TEXT NOT NULL
);
```

**索引**：`(passenger_id, created_at DESC)`, `(status, next_retry_at)`。

**对外收敛**（CLAUDE.md §12.5）：内部 4 态 `pending / delivered / failed / dropped` → 对外 `ok: boolean`（`delivered → true`，其余 → `false`）。API 响应体字段名对齐前端 `WebhookDelivery`：`event / ok / status_code / attempt / latency_ms / created_at`（不出 `status_text`）。

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

## 16. 全局默认策略 · `passenger_strategy_default`

**注意**：`decisions §8.6` 定"补车策略跟 bus 绑"—— 每车一策略字段在 `bus` 表 §8。此表只作为**乘客建新车时的默认值** + **单独拉号（record group）时的策略参数**。

```sql
CREATE TABLE passenger_strategy_default (
  passenger_id             TEXT PRIMARY KEY,
  -- ↓ 硬上限（decisions §8.27）· 超了**拒绝拉号**，不是"建车时的默认值"
  max_unit_price           INTEGER,                      -- microunit · 单价超这个数就不拉 · NULL = 不限
  daily_round_limit        INTEGER,                      -- 全局 · 跨所有 bus 累加 · NULL = 不限
  daily_spend_limit        INTEGER,                      -- microunit · 全局 · NULL = 不限
  -- ↓ 建新车时的默认值（改它不影响已有的车）
  per_round_count          INTEGER,
  preferred_vendor         TEXT,                         -- NULL = 让系统比价
  default_zone             TEXT NOT NULL DEFAULT 'auto', -- us | eu | auto
  updated_at               TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
```

**两类字段语义不同，别混**（§8.27）：

| 字段 | 性质 | 行为 |
|---|---|---|
| `max_unit_price` / `daily_round_limit` / `daily_spend_limit` | **硬上限** | 每次拉号 / 提取前校验，超了拒绝。跟车级同名字段取**更严**的（AND） |
| `per_round_count` / `preferred_vendor` / `default_zone` | **新车默认值** | 只在建车时填初值，改它不动已有的车 |

- **提取 key（record group）只受全局限额管** —— 车级限额管不到 record group
- `max_unit_price` **手动拉号也拦**：提取确认窗超限时禁用确认按钮（判优惠码折后价）· 不给"就这次放行"的口子
- 1b 就要**真的生效**（拉号 / 提取前校验），不是存着等 1d

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
            ├─ passenger_strategy_default
            ├─ passenger_daily_counter
            ├─ wallet ─── wallet_ledger
            ├─ topup_order ─── settlement_event
            ├─ redeem_code
            ├─ bus (creator) ── bus_member ─── passenger
            ├─ pull_intent ── pull_round ── credential_ledger
            └─ outbound_webhook_delivery

vendor_account (我方 vendor 账号) --独立--
vendor_webhook_delivery         --独立--
vendor_lifespan_snapshot        --聚合视图--
capability_slot / pull_round_capability （阶段 2c 起写）
```

## Migration 顺序（1a）

**001（首批）覆盖阶段 1a 主链所需 19 张表**：

1. `passenger`（**含 invited**）, `passenger_api_key`, `passenger_downstream`, **`session`**
2. `wallet`（**含 reserved 字段**）, `wallet_ledger`
3. `bus`, `bus_member`（**含 share_pct / status / skipped_count** —— 2a 才用但 1a 建列，见 §8）
4. `pull_intent`, `pull_round`, `credential_ledger`（**含 owner_bus_id / owner_record_passenger_id / push_error_* / warranty_until**）
5. `vendor_account`, `passenger_daily_counter`, **`passenger_strategy_default`**（§16 · `GET/PUT /me/strategy` 是 1a 必做）
6. `idempotency_record`（HTTP 幂等）
7. `pending_purchase`, `pending_assignment`, **`pending_handoff`**, **`pending_dissolution`**（都是 1a；`pending_topup` 是 1b）

**数 19 的口径**：1(4) + 2(2) + 3(2) + 4(3) + 5(3) + 6(1) + 7(4) = **19**。

**1a 追加的 migration**：
- 002_redeem_topup：`redeem_code`, `topup_order`（1a CDK + 通用充值单）
- 003_outbound_webhook：`outbound_webhook_delivery`（1a 出向 webhook 兜底）
- 004_credential_ledger_traceability：加列，无新表
- 005_topup_gateway：`settlement_event`（1a 支付网关结算兜账）
- 006_topup_refund：加列，无新表
- 007_handoff_placeholder：加列，无新表
- 008_handoff_retry_count：加列，无新表

**累计到 008 · 1a 收工时共 23 张业务表**（19 首批 + 4 追加 · 009 加 wallet 允许负余额但**没加新表**）。

**009**（1b P0-B 修）：`wallet` rebuild in-place drop `CHECK (balance >= 0)` · 允许 refund 时负余额记账·**无新表**。

**1b 落地**：`pending_topup`（充值状态机 · 见 09-transactions.md）· migration 010 加 · 加 topup_order 三维属性列（region/rail/provider_kind/payer_reference）·**24 张业务表**。
"cdk / cdk_redemption / payment_order" 是**过期设计口径** —— 实际用 `redeem_code` / `topup_order`
一张表已够（§6 / §7 已修 · redemption 事件通过 wallet_ledger 溯源）。

**1c-1**：migration 011 · `bus` 表加 `anon_zone` / `anon_max_unit_price` 列 · **无新表** · 累计 24 张业务表。

**1c-2（当前）**：
- migration 012 · `pending_assignment` 加 UNIQUE partial index（防 assign 并发分叉）· **无新表** · 累计仍 24 张
- migration 013 · `vendor_pricing`（vendor 多币种定价 · USD 号价换算积分）· **+1 · 累计 25**
- migration 014 · `pending_topup` rebuild in-place · 加 `gateway_creating` 状态 + `poll_fail_count` 列（P0 修 · CreatePayment 崩溃窗口反查恢复）· **无新表** · 累计 25
- migration 015 · `surcharge_rule` + `pull_round_surcharge`（附加费引擎 · JSON 谓词命中 + 每轮附加费明细留痕）· **+2 · 累计 27**
- migration 016 · `topup_order` 加 `gateway_request_snapshot BLOB`（P0 修 · janitor 反查用起单时冻结的 CreatePaymentRequest 重 POST · 保证幂等指纹一致）· **无新表** · 累计仍 27

- migration 019 · `system_invite_code` + `personal_invite_code` + `invite_referral`（邀请码体系 · decisions §8.29/§8.32）· **+3 · 累计 30**
- migration 020 · `topup_order` 加 `fee_waiver_applied` + `fee_subsidy`（手续费减免实际生效）· **无新表** · 累计 30

**1c 收工时共 30 张业务表**。

### 邀请码体系（migration 019 · decisions §8.29 / §8.32）

**修的老漏洞**：`register` 里原来是 `invited := in.InviteCode != ""` —— **任何**非空码都置
`invited=1`。等于随便编个码就能拿社群身份（解锁 vendor 真名 + 免区域分项）· §8.20 的
定价分层形同虚设。现在查 `system_invite_code` 白名单。

| 表 | 作用 | 关键约束 |
|---|---|---|
| `system_invite_code` | 我方发给社群 / 同行的码 · **只有它能置 tier ∈ {wholesale, insider}** | `max_uses` / `expires_at` / `disabled` 三重限制 · `grants_tier` 定授予档 |
| `personal_invite_code` | 每人一个 · 只给**手续费减免额度**·**不改 tier** | `passenger_id` UNIQUE（一人一码） |
| `invite_referral` | 谁邀请了谁 · 防刷 + 溯源 | 主键 = 被邀请人（**一人只能被邀一次**）· CHECK 挡自己邀自己 |

**`system_invite_code` 加 `grants_tier` 列**（`decisions §8.39`）：

```sql
grants_tier TEXT NOT NULL CHECK(grants_tier IN ('wholesale','insider'))
-- 'insider'   = 同行码 · 授 tier=insider · 同行群邀请制 · 车主手发白名单
-- 'wholesale' = 社群码 · 授 tier=wholesale · TG/Discord · 车主批量生成投放
```

对外 UI 上两种码都叫「专属邀请码」（`CLAUDE.md §2`）· 用户不感知 · 后端按 `grants_tier` 决定授予哪档。

**为什么个人码绝不能改 tier**（§8.29 明文）：如果个人码也解锁 wholesale/insider 身份，任何人都能
生成码让别人免区域分项 → 定价分层崩掉。所以个人码**只给减免额度**。

**补绑**：`POST /api/me/community-code` 让已注册用户补绑（原来只能注册时填 ——
用户往往先注册后进社群，没补绑入口只能注销重注册）。条件 UPDATE（`WHERE tier = 'retail'`）
保证一个账号只能绑一次·并发安全 · 补绑后 `tier` 定死不能重复升降。

### 手续费减免怎么生效（migration 020）

**减免时机必须在起单时**，不能在 MarkPaid：手续费决定乘客实际要付给支付通道多少钱，
订单建出来那一刻金额就定了、二维码/跳转链接也带着这个金额。等他付完再说"其实可以少付"
已经来不及。

| 字段 | 含义 |
|---|---|
| `fee_waiver_applied` | 这单用掉了一次减免额度（0/1）· 退额度时清零防重复退 |
| `fee_subsidy` | **我方垫付**给支付通道的手续费（microunit）· 单独记 |

**为什么 fee_subsidy 要单独记**（§8.32 明文）：手续费是支付通道实收的 pass-through，
我方减免 = 自掏腰包垫付。这是有意的营销支出（类似充值返现），但必须能算出补贴了多少 ——
混进 `channel_fee` 财务上看不出来。**对外不下发**（我方成本结构 · CLAUDE.md §0.1）。

**乘客账本不落 channel_fee 那笔** —— 他没花这钱。补贴是我方成本，不进乘客 ledger。

**额度归还**：订单 expire / cancel / refund 时退回（`returnFeeWaiverForOrderTx`）——
额度是起单时扣的，单子没付成那次减免实际没发生，不退等于用户白掉一次。
条件 UPDATE 保证只退一次（janitor 重跑 / 并发都安全）。

**1d 加**：`vendor_lifespan_snapshot`, `vendor_webhook_delivery`, `credential_usage_snapshot`, `bus_usage_snapshot`

**2c 加**：`capability_slot`, `pull_round_capability`

**3+**：管理端 / 市场相关表另写

### vendor 观测系列（migration 021-026）· `/status` 页数据源

**021 · `vendor_probe` + `vendor_daily`**：Prober 每 60s 拨号 `vendor.Stock` 落样本 · `vendor_daily` 24h 聚合 uptime/incident_flag。**stock-delta 推算 restock** 也走这层（`decisions §11.9`）· probe 落库前跟上一轮同 region stock 对比 · 正 delta 落 `vendor_dispatch`。

**022 · `vendor_public_status`**：vendor 自报的 fleet 累计（keys_active / keys_total / ps_uptime）· 从 `vendor.PublicStatus` 端点拉 · 落 `vendor_probe.ps_*` 列。

**023 · `vendor_order` + `vendor_key`**：Backfiller 每 5min 全量拉 vendor 侧历史订单 + key 生命周期 · 用于 `/prices` 页价格分析 + `/status` 页寿命数据。

**024 · `vendor_dispatch`**：vendor 平台**全网**开号批次（fleet-wide 时序）· 是 `/status` 页"过去 X 时间发几批"曲线的硬数据源。

**026 · `vendor_dispatch` 加 `source` 列**（`decisions §11.11`）：主键改成 `(vendor_id, dispatch_key, source)` · `source ∈ {vendor_self, xi8}` · xi8 是内部聚合数据源（**非 vendor** · 不出前端 · `CLAUDE.md §0.1`）· 前端读路径全部只查 `source='vendor_self'` · xi8 行只做后端对账 + 历史空窗填。

**025 · `inbound_webhook_event`**：vendor 推给我方的事件日志 + 幂等去重（`new_keys_available` / `all_keys_dead` / `warranty_refund`）。

## 19. HTTP 幂等 · `idempotency_record`

见 `09-transactions.md § 7` 完整说明。

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

见 `09-transactions.md § 2/3/4/5/6` 完整状态定义。

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
  status                   TEXT NOT NULL,                 -- initial | reserved | purchasing | purchased | imported | completed | cancelled_reserve | need_recover_vendor | need_manual
                                                            -- ★ purchasing = 请求已发 vendor、响应未确认（09-transactions §2.1 · P0-1）
                                                            --   崩在这个状态**不能直接释放冻结** —— vendor 可能已扣款
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
  status                TEXT NOT NULL,                    -- initial | completed | need_manual （旧版四态 external_done / status_updated 已废 · 见 09-transactions §3）
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

### `pending_handoff` （P0 修补 · 三段式 token 交付）

见 `09-transactions.md § 4` 完整状态定义。

```sql
CREATE TABLE pending_handoff (
  id                    TEXT PRIMARY KEY,
  idempotency_record_id TEXT,                           -- init 阶段 X-Idempotency-Key（可选）
  passenger_id          TEXT NOT NULL,
  download_token        TEXT NOT NULL UNIQUE,           -- 32 hex，客户端拿它来 fulfill/confirm
  credential_ids_json   TEXT NOT NULL,                  -- ["cred_id_1", "cred_id_2", ...]
  status                TEXT NOT NULL,                  -- token_issued | fulfilled | confirmed | completed | expired | expired_after_fulfill | need_manual
  fulfill_count         INTEGER NOT NULL DEFAULT 0,     -- fulfill 被调多少次（断线重试用）
  fulfilled_at          TEXT,
  confirmed_at          TEXT,
  completed_at          TEXT,
  expires_at            TEXT NOT NULL,                  -- token TTL，默认 now + 5min
  error                 TEXT,
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id)
);
```

**索引**：`(download_token)`, `(status, expires_at)` （janitor 扫过期用），`(passenger_id, created_at DESC)`。

**特殊**：
- **明文永不存本表**（每次 fulfill 从 housepool 实时读）
- `download_token` 是幂等键（不用 X-Idempotency-Key）
- `expires_at` 到期后 janitor 复原 credential（disabled=true 保持）+ status=`expired`

### `pending_dissolution`（Bus 解散批量号迁移）

见 `09-transactions.md § 5`。

```sql
CREATE TABLE pending_dissolution (
  id                     TEXT PRIMARY KEY,
  idempotency_record_id  TEXT NOT NULL,
  bus_id                 TEXT NOT NULL,
  initiator_passenger_id TEXT NOT NULL,
  credential_ids_json    TEXT NOT NULL,        -- 快照，解散触发时锁定
  processed_count        INTEGER NOT NULL DEFAULT 0,
  failed_count           INTEGER NOT NULL DEFAULT 0,
  status                 TEXT NOT NULL,        -- initial | snapshot_taken | moving | completed | need_manual
  error                  TEXT,
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (bus_id) REFERENCES bus(id),
  FOREIGN KEY (initiator_passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id)
);
```

**索引**：`(status, updated_at)`, `(bus_id)`。

### `pending_topup`

```sql
CREATE TABLE pending_topup (
  id                     TEXT PRIMARY KEY,
  idempotency_record_id  TEXT NOT NULL,
  passenger_id           TEXT NOT NULL,
  topup_order_id         TEXT NOT NULL,                   -- 关联 topup_order.id（旧文档写 payment_order_id · 表名对齐后统一）
  -- migration 010（1b 落地）建表·migration 014（1c-2 P0 修）rebuild in-place
  -- 加 gateway_creating + poll_fail_count
  status                 TEXT NOT NULL,                   -- initial | gateway_creating | gateway_ordered | gateway_paid | credited | completed | expired | cancelled | refunded | pending_manual
  error                  TEXT,
  poll_fail_count        INTEGER NOT NULL DEFAULT 0,      -- migration 014 · janitor 反查 gateway 失败累计 · 到 maxCreatingPollFails=5 转 pending_manual · **未知不等于失败**
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (topup_order_id) REFERENCES topup_order(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id)
);
```

**索引**：`(status, updated_at)`。

**`gateway_creating` 中间态**（migration 014 引入）：
- API handler 起单时·**先** `EnsureAtLeast(gateway_creating)` 落库·**再**调 `paymentgw.CreatePayment`
- 失败 hard fail 500·**绝不**发外部请求（不然 gateway 端已建单本地无痕 → 走 initial → expire 丢单）
- 崩溃在 `CreatePayment ↔ AttachGateway` 之间时·janitor 用 `client_order_id` 反查 gateway 侧幂等表恢复
- 详细状态机行为见 `09-transactions.md §6`

**`gateway_request_snapshot`**（migration 016 · P0 修）：`topup_order` 加 BLOB 列存 CreatePaymentRequest JSON 快照·janitor 反查时读快照重新 POST（保证跟起单时的幂等指纹一致 · 不用当前 config 重建）。

## 备份 / 迁移

- SQLite `.backup` 每小时 dump 一次
- 加密字段主密钥**分离备份**（不跟 DB 一起）
- 迁移到 PostgreSQL 的门槛：单机 QPS 撑不住 时（预计阶段 2 之后）

## 已冻结的约定（跟 `CLAUDE.md § 7.2` 一致）

- **时间**：UTC 存储（`TEXT` ISO-8601）· UI 层转 CST
- **金额**：`INTEGER` microunit（1 元 = 1_000_000）
- **主键**：UUID v7（26 字符 Crockford Base32，`TEXT` 存）
- **并发**：SQLite `BEGIN IMMEDIATE` + 条件 UPDATE
- **加密字段**：`secret_<name>_encrypted BLOB`（AES-GCM，见 `10-secrets.md` 阶段 1a 起手时补）

## 待定（阶段 1a 落码时确定）

- UUID v7 生成库（Go 有多个实现）—— 落码时选具体的
- 索引具体的 SQL —— 每张表首次 migration 时定
- SQLite 版本 · WAL 参数（`journal_mode=WAL` / `synchronous=NORMAL` 是默认，特殊调整跟着实测走）
