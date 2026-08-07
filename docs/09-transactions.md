# bus-pooling · 核心交易状态机 + 补偿规则

> 前置：`03-modules.md` · `05-api-contract.md` · `06-db-schema.md` · `07-provider-contract.md` · `08-housepool-contract.md`
>
> **核心问题**：拉号涉及 **3 个独立系统** —— vendor（外部）· housepool = kiro.rs（外部）· 我方 SQLite。**没有跨系统的两阶段提交**。任何一步崩溃都可能留下不一致状态。
>
> **解决方案**：把关键操作显式建成**持久化状态机**，每步崩溃后有确定的**恢复/补偿规则**。

## 1. 涉及的关键操作

**四类**跨系统操作，都要状态机：

| 操作 | 状态机 |
|---|---|
| **拉号** (`bus.pull` / `me/pull`) | §2 `pending_purchase` |
| **派去向 · 进车 / 推 passengerpool** (`pull-records/assign`) | §3 `pending_assignment` |
| **handoff · 拿走**（两阶段 token） | §4 `pending_handoff` |
| **充值** (`me/topup`) | §5 `pending_topup` |
| **Bus 解散**（批量号迁移） | §5.5 `pending_dissolution` |

## 2. 拉号状态机 · `pending_purchase`

**跨系统**：wallet（SQLite） · vendor.Purchase（外部） · housepool.BatchImport（kiro.rs）。

### 状态（含 `purchasing` 中间态 —— codex P0-1 修补）

```
                    ┌──────────────┐
                    │   initial    │  API 收到请求，写 pending_purchase 行
                    └──────┬───────┘
                           ▼ (预占余额，扣积分冻结)
                    ┌──────────────┐
                    │  reserved    │  wallet 冻结成功；准备调 vendor
                    └──────┬───────┘
                           ▼ (**发请求前落此状态**)
                    ┌──────────────┐
                    │  purchasing  │  ★ 已把请求发给 vendor，未确认响应
                    └──────┬───────┘   （崩溃恢复的黄金窗口 —— 必须查询/重放，不能直接释放）
                           ▼ (vendor.Purchase 成功返回)
                    ┌──────────────┐
                    │  purchased   │  vendor 已扣账已出号，未入 housepool
                    └──────┬───────┘
                           ▼ (BatchImport 到 housepool)
                    ┌──────────────┐
                    │  imported    │  号在 housepool 里，group 正确；准备结账
                    └──────┬───────┘
                           ▼ (wallet 从冻结 → 消费；ledger 落账)
                    ┌──────────────┐
                    │  completed   │  完全成功
                    └──────────────┘

失败分支：
   reserved   ─fail→   cancelled_reserve      （wallet 冻结释放）
   purchasing ─crash→  见 §2.1 恢复策略        （不能直接释放，vendor 可能已扣款）
   purchased  ─fail→   need_recover_vendor    （补拉 OrderKeys）
   imported   ─fail→   need_manual            （极少见，人工介入）
```

### 状态含义与补偿

| 状态 | 含义 | 崩溃后恢复 | 补偿 |
|---|---|---|---|
| `initial` | 已写行，未做任何外部动作 | 重启后 janitor 扫到 → 直接删除 | 无 |
| `reserved` | wallet 已冻结，未调 vendor | janitor 超时（> 60s） → 释放冻结 → `cancelled_reserve` | 冻结回退 |
| **`purchasing`** | **★ 请求已发 vendor，响应未确认** | **见 §2.1 分类处理**（不能直接释放） | 视 vendor 幂等能力而定 |
| `purchased` | vendor 已扣账已出号，未入 housepool | janitor 调 `vendor.OrderKeys(client_order_id)` 补拉 → 继续走 `imported` | 若号已死则退回冻结 |
| `imported` | housepool 已入 group，未落 wallet ledger | 完成 wallet 消费扣款 + ledger | 已入池号可用 |
| `completed` | 全部完成 | — | — |
| `cancelled_reserve` | 冻结已释放 | — | — |
| `need_recover_vendor` | vendor 成功但 OrderKeys 补拉失败 N 次 | 报警 → 人工 | 保留号在 vendor 侧账 + 退乘客积分 |
| `need_manual` | housepool BatchImport 反复失败 | 报警 → 人工 | 号出了但入不了池；退乘客积分或人工重导 |

### 2.1 · `purchasing` 状态的恢复策略（P0-1 修补）

**问题**：在 `reserved → 发 vendor 请求 → 得到响应` 之间，进程可能崩溃。此时 vendor **可能已扣款**，但我方尚未写 `purchased`。若 janitor 简单释放冻结（跟 `reserved` 一样处理），会导致：
- **vendor 已扣款** + **乘客获退款** = 我方吃亏（免费给乘客）

**修补**：**在发请求前一刻先落 `purchasing`**，然后按 vendor 能力分类处理：

| Vendor 幂等能力 | 崩溃恢复策略 |
|---|---|
| **有幂等键（91kiro/kiroceo/kirooo/kiroappio/kirodrop）** | janitor 用同 `client_order_id` 重放调 vendor.Purchase → 得到原批（vendor 幂等保证） → 走 `purchased` |
| **无幂等键（kiroappcc）** | **进入 `need_manual`** → 人工对账 vendor 侧订单是否真实存在 → 决定退冻结 or 走 `imported` |

**具体决策规则**（janitor 扫到 `purchasing` 超时 > 30s 时）：

```
if vendor.Capability().SupportsIdempotency:
    result = vendor.Purchase(same client_order_id)  # 幂等重放
    if result.success:
        推进到 purchased
    elif result 是 "no such order" 一类:
        安全释放冻结 → cancelled_reserve
    else:
        need_recover_vendor 报警
else:  # 无幂等键（kiroappcc）
    进入 need_manual → 报警
```

**代码层面**：`decider` 中调 `vendor.Purchase` **前一行**必须写 `pending_purchase.status = 'purchasing'`（同事务）；否则事务外的 vendor 调用会留下没有 `purchasing` 记录的黄金窗口，恢复不了。

**幂等键强制要求**：**接入无幂等键的 vendor 必须开启 `MANUAL_RECONCILE_ONLY` 模式**（`config.yaml` 里显式配），否则拒启动。这防止无幂等 vendor 造成不可恢复的钱损失。

### 关键约定

1. **wallet 冻结 vs 扣款分离** —— 用两个字段：`balance`（可用）+ `reserved`（冻结）。`Debit` 时先冻结，成功后再从冻结转移到消费。
2. **vendor 调用带 client_order_id 幂等** —— 崩溃后 `vendor.Purchase` 用同 id 重放，vendor 会返回原批（不多扣）。
3. **housepool BatchImport 幂等** —— kiro.rs 对同 refresh_token 已入库的返回 `duplicate` 事件（不重复入）。
4. **每步只推进一个状态字段** —— 单表原子 UPDATE，SQLite `BEGIN IMMEDIATE` 保证串行。
5. **超时阈值**（重启后 janitor 用）：
   - `reserved` 超时 60s（vendor 未响应）
   - `purchased` 超时 5 min（housepool 未响应）
   - `imported` 超时 30s（结账未完成）

## 3. 派去向状态机 · `pending_assignment`

**跨系统**：housepool（可能 PUT / BatchImport 到 passengerpool）· SQLite（credential_ledger 状态）。

### 状态

```
initial → external_done → status_updated → completed
                  │
                  └─fail→ need_manual
```

| 状态 | 说明 |
|---|---|
| `initial` | 收到 assign 请求，写 pending_assignment 行 |
| `external_done` | 外部动作完成（进车：groups 已改；推 passengerpool：passengerpool 已收到复制；handoff：明文已给用户） |
| `status_updated` | 我方 credential_ledger 状态更新完 |
| `completed` | 全部完成 |
| `need_manual` | 外部动作失败 3 次 → 报警 |

**顺序铁律**（`CLAUDE.md § 12` 已写）：**先外部动作，再改 housepool 状态**。

**幂等**：客户端带 `X-Idempotency-Key`；服务端查 `idempotency_record`（§6）判是否重放。

## 4. handoff 状态机 · `pending_handoff` · 两阶段 Token 交付（P0-3 修补）

**问题背景**（codex P0-3）：单次调用 handoff 有可靠交付漏洞——HTTP 响应可能在网络中断线，服务端已 DELETE credential + 返回明文但客户端没收到 → **明文永久丢失**。

**解决方案**：**两阶段 token 交付**——先发放 download_token；用户显式 fulfill；客户端显式 confirm 才真 DELETE。

### 状态

```
                    ┌──────────────┐
                    │   initial    │  API 收到 handoff-init 请求
                    └──────┬───────┘
                           ▼
                    ┌──────────────┐
                    │  token_issued│  已生成 download_token（32 hex）+ TTL 5 分钟
                    └──────┬───────┘   housepool 保留 credential，disabled=true
                           ▼ (用户 GET /handoff-fulfill/{token})
                    ┌──────────────┐
                    │  fulfilled   │  明文已返回（幂等：token TTL 内可多次 GET）
                    └──────┬───────┘   credential 仍在 housepool
                           ▼ (客户端 POST /handoff-confirm/{token})
                    ┌──────────────┐
                    │  confirmed   │  客户端确认已收到 → 触发 DELETE
                    └──────┬───────┘
                           ▼ (housepool DELETE)
                    ┌──────────────┐
                    │  completed   │  号已离开系统，credential_ledger 标 handed_off
                    └──────────────┘

失败分支：
   token_issued  ─5min TTL 到期→  expired          （credential 复原 disabled，用户可重发 handoff-init）
   fulfilled     ─5min 未 confirm→ expired_after_fulfill  （同上，credential 仍在，可重来）
   confirmed     ─DELETE fail 3次→ need_manual     （报警）
```

### 三个 API 端点

- `POST /api/me/pull-records/{id}/handoff-init` · 发放 token
  - 请求：`{ credential_ids: [...] }`
  - 返回：`{ download_token: "01H...", expires_at: "..." }`
  - 生成 `pending_handoff` 行，`status: token_issued`
  - **不返回明文**
- `GET /api/me/handoff/{token}` · fulfill · 拿明文
  - **无 X-Idempotency-Key 要求**（token 本身就是幂等键）
  - 首次调用：读明文 → 返回 → 状态 `fulfilled`
  - TTL 内重复调用：**再次返回明文**（用户网络断了重试用）
  - TTL 过期：404 `token_expired`
- `POST /api/me/handoff/{token}/confirm` · 客户端确认收到
  - 状态 `fulfilled → confirmed`
  - 触发 housepool DELETE + credential_ledger 标 `handed_off`
  - 幂等：多次 confirm 返回同状态

### janitor 恢复规则

- `token_issued` 超时 5 分钟未 fulfill → 状态 `expired`；credential 保留，用户可重来
- `fulfilled` 超时 5 分钟未 confirm → 状态 `expired_after_fulfill`；credential 保留（**假设用户已看到明文但没确认，视同"收到"** —— 保守做法：credential 仍在池里，用户可再发 handoff 重来）
- `confirmed` 但 DELETE 失败 3 次 → `need_manual` 报警

### 明文存储

- **明文仅在 fulfilled 状态时从 housepool 实时读**，**不在数据库存储**
- `pending_handoff.plaintext_encrypted` 字段**不存在**（明文永不落我方 DB）
- token 是幂等键，绑定 credential_id；每次 fulfill 都从 housepool 实时读

### 客户端集成建议

**推荐流**：
```javascript
const { download_token } = await POST('/pull-records/X/handoff-init', { credential_ids })
const { keys } = await GET(`/handoff/${download_token}`)  // 明文（可断线重试）
displayToUser(keys)
await POST(`/handoff/${download_token}/confirm`)  // 确认，触发 DELETE
```

**断线重试**：GET 阶段网络断了 → 客户端重新 GET 同 token 拿明文（TTL 内 OK）。

## 5. Bus 解散状态机 · `pending_dissolution`（新增 · P0 修补）

**问题**：`04-scenarios.md § A7` 描述 bus 解散时"活号移到创建人 `record-<pid>` group"。这是**批量号 group 迁移**，可能有部分失败。

### 状态

```
initial → snapshot_taken → moving → completed
                              │
                              └─partial→ need_manual
```

| 状态 | 说明 |
|---|---|
| `initial` | 收到解散请求，写 pending_dissolution 行 |
| `snapshot_taken` | 从 credential_ledger 拿到该 bus 所有活号 id 列表 |
| `moving` | 循环调 `housepool.UpdateCredential(id, groups=[record-<creator>])`，进度记录 |
| `completed` | 全部号迁移完成；bus.status = dissolved |
| `need_manual` | 有号迁移失败 3 次 → 报警 |

**表结构**（`06-db-schema.md § 20` 补）：
```sql
CREATE TABLE pending_dissolution (
  id                    TEXT PRIMARY KEY,
  idempotency_record_id TEXT NOT NULL,
  bus_id                TEXT NOT NULL,
  initiator_passenger_id TEXT NOT NULL,
  credential_ids_json   TEXT NOT NULL,        -- 快照
  processed_count       INTEGER NOT NULL DEFAULT 0,
  failed_count          INTEGER NOT NULL DEFAULT 0,
  status                TEXT NOT NULL,
  error                 TEXT,
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL,
  FOREIGN KEY (bus_id) REFERENCES bus(id),
  FOREIGN KEY (initiator_passenger_id) REFERENCES passenger(id),
  FOREIGN KEY (idempotency_record_id) REFERENCES idempotency_record(id)
);
```

**恢复**：janitor 扫 `moving` 超时 → 继续未完成的号迁移；`failed_count > 3` → `need_manual`。

## 5. 充值状态机 · `pending_topup`

**跨系统**：payment-gateway（外部）· wallet（SQLite）。

```
initial → gateway_ordered → gateway_paid → credited → completed
                    │            │
                    │            └─fail→ pending_manual （gateway 说已付但 credit 失败）
                    └─timeout→ expired  （用户没扫码支付）
                    └─cancel→ cancelled
                    └─refund→ refunded
```

payment-gateway webhook 是主推进器；我方 janitor 兜底轮询状态。

## 6. `idempotency_record` 表设计

```sql
CREATE TABLE idempotency_record (
  id                     TEXT PRIMARY KEY,
  passenger_id           TEXT NOT NULL,
  method                 TEXT NOT NULL,           -- 'POST'
  path                   TEXT NOT NULL,           -- '/api/me/buses/{id}/pull'
  idempotency_key        TEXT NOT NULL,           -- 客户端传的 X-Idempotency-Key (32 hex)
  request_fingerprint    TEXT NOT NULL,           -- sha256(canonical body)
  response_status        INTEGER,                  -- 首次响应的 HTTP status
  response_headers       TEXT,                     -- JSON encoded headers
  response_body          BLOB,                     -- 首次响应体（handoff 不含明文 keys）
  created_at             TEXT NOT NULL,
  first_completed_at     TEXT,                     -- 首次动作完成时间
  UNIQUE (passenger_id, path, idempotency_key)
);
```

**索引**：`(passenger_id, path, idempotency_key)`, `(created_at)` （用于过期清理）。

**过期策略**：records **保留 30 天**，之后清理（幂等窗口）。过期后重放视为新请求。

**handoff 特殊**：`response_body` 里**永远不含 credential 明文**；重放时只回状态和 `credential_ids`。

## 7. wallet 并发控制

**旧设计（错）**：`SELECT balance FROM wallet ... → 判够 → UPDATE`（并发多请求可能都通过检查）。

**新设计**：SQLite `BEGIN IMMEDIATE` + 条件更新：

```sql
BEGIN IMMEDIATE;
-- 冻结：把 balance 转 reserved
UPDATE wallet
   SET balance = balance - ?,
       reserved = reserved + ?
 WHERE passenger_id = ?
   AND balance >= ?;
-- 检查 rowsAffected；若 0 则冻结失败（余额不足或并发冲突）
COMMIT;
```

**SQLite 特点**：`BEGIN IMMEDIATE` 拿到 write lock，其它写事务阻塞（不是"行级锁"，是"库级 write lock"）—— 短事务下够用。

**wallet 表加字段** `reserved`（`06-db-schema.md § 4` 补）：

```sql
ALTER TABLE wallet ADD COLUMN reserved INTEGER NOT NULL DEFAULT 0;
```

**冻结释放**：`reserved -= X; balance += X`（原子）。
**冻结转消费**：`reserved -= X; ledger 落 -X`（原子）。

## 8. janitor / 恢复任务

**目的**：应对进程崩溃、网络中断、外部系统超时。

**扫描频率**：每 30 秒。

**扫描范围**：`pending_purchase` / `pending_assignment` / `pending_handoff` / `pending_topup` 表中 `status ∉ {completed, cancelled_*, need_manual}` 且 `updated_at + timeout_for_status < now` 的行。

**动作**（按状态类型）：

- `reserved` 超时 → 释放冻结
- `purchased` 未 imported → 调 `vendor.OrderKeys(client_order_id)` 尝试 imported
- `imported` 未 completed → 补落 ledger + 更新 completed
- `pending_assignment` `external_done` 未 `status_updated` → 补落 status_updated
- `pending_handoff` 未 `housepool_deleted` → 补 DELETE
- `pending_topup` `gateway_paid` 未 `credited` → 补 credit

**报警**：任何状态 → `need_manual` 时立即报警（阶段 1 简单：写 log + 邮件）。

## 9. `06-db-schema.md` 要新增的表

（`06-db-schema.md` 会同步补上，本文只列必需）

- `pending_purchase`（拉号状态机）
- `pending_assignment`（派去向状态机；含 handoff）
- `pending_topup`（充值状态机）
- `idempotency_record`（HTTP 幂等）
- `wallet.reserved` 字段（并发控制）

## 10. 不做的事（阶段 1）

- **不实现两阶段提交** / **不引入分布式事务库** —— SQLite 单机，状态机 + janitor + 幂等足够
- **不做 Saga 框架** —— 状态机是"轻量 Saga"，直接写不用框架
- **不做多进程消费** —— 阶段 1 单进程；janitor 是单个 goroutine

## 11. 阶段推进

- **1a**（sprint-1a-frontend 后的 sprint-1b-backend）：`pending_purchase` + `wallet.reserved` + `idempotency_record` + janitor
- **1a**（同）：`pending_handoff`（阶段 1a 唯一的 assign 场景）
- **1b**：`pending_topup`（充值渠道）
- **1c**：`pending_assignment` 全形态（进车 / 推 passengerpool）
- **1d 之后**：状态机应对补车链条（deathwatch → new pending_purchase）

## 12. 参考

- codex review 的 5 个真问题里的 #1 · #2 · #3 都在本文档解决
- 补偿设计参考：Sagas Pattern · Outbox Pattern（但本项目不引入框架，手写状态机）
- SQLite 事务隔离：https://www.sqlite.org/lockingv3.html
