# bus-pooling · 模块清单

> 前置阅读：[`01-architecture.md`](./01-architecture.md) · [`02-flows.md`](./02-flows.md)
> 本文只列**代码模块**（包 / 目录）。**不画层次结构**（那是 01）；**不写运行时时序**（那是 02）；**不排阶段计划**（那是 `04-phases.md`）。

## 规则

- **业务包上限 15 个**（顶层 `internal/*` 的**业务包**）；破了要写清理由
- **基础设施包不算业务包**：`config` / `httpx` / `secrets` / `db` / `api` / `web` 独立算
- 每模块行数：`目的 · 输入 · 输出 · 依赖 · 谁调它 · P 标签`（每模块 5-15 行）
- **P 标签**指该模块**首次落地的阶段**；后续阶段可能扩它，不再重复列
- 模块间**只允许下层被上层导入**（Layer N import Layer <N，禁止反向）

## 阶段速查

对应 §00.7 的三大阶段（+ 阶段 1 内部 milestone）：

| 阶段 | 关键交付 |
|---|---|
| **1a** | 单 vendor 手动拉 → 1 人 bus → housepool → 用户 UI 处理 |
| **1b** | 6 家 vendor + 兑换码 + payment-gateway（waffo） |
| **1c** | 匿名撮合多人 bus 拼车 + 号价按 N 摊 |
| **1d** | 自动模式 + 号死自动补 |
| **1e** | 去向 ② 推 passengerpool（双写）+ 去向 ③ handoff + 对外 webhook |
| **2a** | 邀请码组队 bus（认识的人拼车） |
| **2b** | 列队策略（多 bus 抢 vendor 排队） |
| **2c** | 压车治理（bus 内噪邻探测 + 限速） |
| **3a** | 数据图表（管理端 + 乘客端） |
| **3b** | 发车（乘客上传 AWS 凭证） |
| **3c** | 市场（公开池 + 分成） |

---

## Layer 2 · providers · vendor 适配

### `internal/providers/` (业务包 1/15)

- **目的**：provider 顶层契约。跨 provider（未来加 cursor）共享的最小接口
- **输入**：`ProviderID` (string), `VendorID` (string), 拉号/查库存/查价请求
- **输出**：归一化的 `Vendor`, `Snapshot`, `PurchaseResult`, `WebhookEvent`
- **依赖**：无
- **谁调它**：`decider`, `webhookin`, 后续任何需要"提供上游能力"的模块
- **P 标签**：1a（先只有 kiro 一个 provider）
- **契约**：`provider.go` 定义 `Provider` interface：`Vendors() []Vendor` / `EventParser() WebhookParser`；`vendor.go` 定义 `Vendor` interface：
  - `Stock()` / `Purchase()` / `OrderKeys()` / `Balance()` —— 基础
  - `KeyHealth(id)` —— 单号死活探测（91kiro `/api/my/keys/{id}/usage`、drop.kiro.ss `/api/status`、kiroapp.io `/api/me/keys/{id}` 各家不同）
  - `KeyStats()` —— 批量健康统计（91kiro `/api/my/rounds`、kiro.ooo `/my/dispatch-log`）

### `internal/providers/kiro/`

- **目的**：kiro provider 层的公共契约与共享类型（`Zone: us/eu`、`KskCredential`、`ksk_` key 形态）
- **P 标签**：1a
- **备注**：不是独立业务包，属于 `providers/` 家族内部

### `internal/providers/kiro/vendors/kiro91/`

- **目的**：91kiro 官方 API 适配（见 `docs/vendors/91kiro.md`）
- **输入**：`Stock / Purchase / OrderKeys / Balance` 抽象调用
- **输出**：归一化 struct（把 `code / message / error` 三兄弟 → 内部 `APIError`）
- **依赖**：`infra/httpx`, `providers/kiro`
- **谁调它**：`decider`（拉快照 + 拉号）；`webhookin`（解 vendor webhook）
- **P 标签**：**1a**（首家）
- **备注**：不 import `providers/kiro/vendors/*` 其它 adapter

### `internal/providers/kiro/vendors/kiroceo/`
- **P 标签**：1b；契约见 `docs/vendors/kiro-ceo.md`

### `internal/providers/kiro/vendors/kirooo/`
- **P 标签**：1b；契约见 `docs/vendors/kiro-ooo.md`

### `internal/providers/kiro/vendors/kiroappio/`
- **P 标签**：1b；契约见 `docs/vendors/kiroapp-io.md`

### `internal/providers/kiro/vendors/kiroappcc/`
- **P 标签**：1b；契约见 `docs/vendors/kiroapp-cc.md`
- **注意**：无幂等键 / camelCase 字段 / 简号形态；接入风险高

### `internal/providers/kiro/vendors/kirodrop/`
- **P 标签**：1b；契约见 `docs/vendors/drop-kiro-ss.md`
- **注意**：混币（CNY 余额 / USD 单价）；比价归一在 `decider` 里处理

### `internal/webhookin/` (业务包 2/15)

- **定位**：**事件适配层** —— vendor webhook 入口 · 验签 / 解析 / 归一化 / 落 `inbound_webhook_event` / 分派事件到各下游 · **不做业务决策**
- **输入**：HTTP POST（来自各家 vendor）
- **输出**：归一化的 `providers.WebhookEvent` + 调用下游接口（`SweepTrigger` / `RestockNotifier` / `DispatchStore` / `ProbeZoneSink`）
- **依赖**：`providers/*`（各家 adapter 的 `Parse` / `VerifySignature`）· 下游接口由装配层注入（**不硬 import deathwatch/stockwatch**）
- **谁调它**：`internal/api` 路由 `POST /webhook/vendor/<vendor_id>`
- **P 标签**：1d
- **幂等**：以 `(vendor_id, event_id)` 去重
- **不做**：
  - **不做决策**（是否补车 / 是否 fire 挂单 → 交给 `decider.Decide`）
  - **不自己扣钱 / 不改钱包 / 不落 credential 状态**（交给 deathwatch / decider）
  - **不 import `strategy`**（webhookin → strategy 是老思路 · 已废）

---

## Layer 3 · bus-pooling 本体

### `internal/passenger/` (业务包 3/15)

- **目的**：乘客身份、登录、profile、下游配置（passengerpool url + token）
- **输入**：注册 / 登录请求；profile 更新
- **输出**：`Passenger { id, name, email, downstream_config, created_at, ... }`
- **依赖**：`infra/db`, `infra/secrets`（passengerpool token 加密存）
- **谁调它**：`api`（HTTP 层），几乎所有业务包（凭 `passenger_id` 索引）
- **P 标签**：1a（Go 自建 Argon2id + session cookie，先做最小可用）

### `internal/wallet/` (业务包 4/15)

- **目的**：积分余额 + ledger 流水；所有扣款 / 退款 / 通道费 / 服务费全走这里
- **输入**：`{passenger_id, amount, reason, ref_type, ref_id, memo}`
- **输出**：`Balance`, `LedgerEntry[]`
- **依赖**：`infra/db`, `passenger`
- **谁调它**：`redeem`, `payment`, `decider`（记账）, `deathwatch`（退款）
- **P 标签**：1a
- **ledger reason 枚举**：`recharge` / `channel_fee` / `redeem` / `key_cost` / `single_pull_fee` / `capability_fee` / `service_fee` / `warranty_refund` / `admin_adjust`
  - `single_pull_fee` 和 `capability_fee` 都是**我方收入**，分开记账便于以后拆报表看利润构成
  - `capability_fee` 的 memo 里写具体是哪个附加能力（`stable_priority` / 未来其它）
- **原子性**：扣款 + 落流水在**一个事务**；跨包调用用返回值确认

### `internal/redeem/` (业务包 5/15)

- **目的**：兑换码兑换积分
- **输入**：`{passenger_id, code}`
- **输出**：`{quota, replayed, balance}`
- **依赖**：`wallet`, `infra/db`
- **谁调它**：`api`
- **P 标签**：1b

### `internal/topup/` + `internal/topupchannel/` + `internal/paymentgw/` (业务包 6/15 · 充值家族)

- **目的**：充值订单状态机 + 充值通道配置 + payment-gateway 客户端；下单 / 收 webhook / settlement / refund / reversed / 手续费 pass-through 记账
- **输入**：`{passenger_id, amount, channel}`
- **输出**：`{order_id, pay_url, qr, status}`
- **依赖**：`wallet`, `infra/httpx`, `infra/secrets`（payment-gateway 密钥）
- **谁调它**：`api`
- **P 标签**：1b
- **通道费规则**：见 CLAUDE.md §1.4；乘客想充 100 积分 → 通道费 5 积分 → 支付 15 USD → 内部 `recharge +105` + `channel_fee −5` → 净 +100

### `internal/strategy/` (业务包 7/15)

- **目的**：存 / 校验乘客的策略参数；判断"当下能否拉号"；生成拉号意图
- **参数**：乘客全局 `{max_unit_price, daily_round_limit, daily_spend_limit, per_round_count, preferred_vendor, default_zone}` + 车级 `{auto_refill_enabled, refill_watermark, refill_min_count, per_round_count, max_unit_price, preferred_vendor}`
- **输入**：`passenger_id`（或系统触发源）
- **输出**：**意图** `Intent { passenger_id, bus_id, want_count, constraints }`
- **依赖**：`passenger`, `wallet`（查余额）, `bus`（查 bus 归属）, `infra/db`
- **谁调它**：手动拉号从 `api` 触发；自动拉号由 `deathwatch`（补车）/ 时钟 / vendor webhook 触发
- **P 标签**：1a（手动）→ 1d（自动）
- **谁读它的字段**：
  - 手动拉号：`api/bus.go:handleBusPull` → `strategy.CanPull` → `decider.Pull`
  - 自动拉号：`decider.Decide` 读 `AutoRefillEnabled` / `RefillWatermark` / `RefillMinCount` / `MaxUnitPrice` / `PreferredVendor` 等（见 `docs/15-scheduling.md §5`）
- **不做**：
  - **不选 vendor / 不动 housepool / 不调 Pull**（那是 decider）
  - **不做系统调度决策**（是否补车 / mode 判断 / vendor mode 都是 `decider.Decide`）

### `internal/coalescer/` (业务包 8/15)

- **状态**：**骨架 · 未接生产**(2026-08-15 codex 审计确认)
  · `coalescer.go` / `window.go` 有实现 + 单测 · 但 API 层未装配 `SetDefaultWindow`
  · `internal/api/bus.go` 拉号入口仍直发 `decider.Pull` · 未走窗口合流
  · 不能宣称'生产集单已完成' · 只是库层能力就绪等接线
- **目的**(接线后)：**bus 维度**集单调度；同 bus 内多成员意图在窗口内合流成一次拉号意图
- **子文件**：
  - `coalescer.go` · 定义 Intent / BatchIntent · Single 直发 pass-through
  - `window.go` · 单进程同步窗口，按 `(bus_id, zone, vendor_id)` 合流，默认 200ms / MaxBatch 8
- **输入**：意图流（`Intent`）
- **输出**：合流后的**批量意图** `BatchIntent { bus_id, participants[], count_total }`
- **依赖**：`strategy`, `bus`
- **谁调它**(接线后)：API / 调度层把多人 bus 意图送进窗口；窗口关闭后由 executor 调 `decider`
- **P 标签**：1c 骨架已建 · **真接线 = 2a/2b**
- **不做**：不选 vendor / 不算价；1 人 bus 意图**绕过**（直发 decider）；跨进程意图池 / 后台异步 fire 留后续

### `internal/decider/` (业务包 9/15)

- **目的**：跨 vendor 决策（比价 / 健康 / fallback） + 实际发起 vendor purchase + 记账 + 存进 housepool + 触发上车
- **输入**：`BatchIntent` 或 `Intent`
- **输出**：`PurchaseResult { credentials[], vendor_id, cost, participants_split }`
- **依赖**：`providers/*`, `wallet`（记账）, `housepool`（进 bus group）, `pullrecord`（单独拉号写记录）, `deathwatch`（读平均寿命统计）, `pricing`（`SurchargeResolver` + `VendorPricing` 换算·可选）
- **谁调它**：`strategy` / `api`（手动和单人直发）, `coalescer`（多人 bus 合流后发）, `stockwatch`（缺货挂单 fire）, `deathwatch` / `bus.Scheduler`（自动补）
- **P 标签**：1a（单 vendor 直选）→ 1c-2（`surcharge_rule` 引擎动态求费率 + `vendor_pricing` 多币种）→ 1d（比价 + fallback + **平均寿命**，与自动一起上）
- **比价维度**：**单价 / 平均寿命 = 每积分能活的时长**（不是只看单价）
- **归一算价**：跨 vendor 单价 → 一个"每 key 有效积分成本"，含通道费/手续费在内
- **附加费引擎**：可选注入 `RatesResolver`（由 `internal/pricing.SurchargeResolver` 实现）· 按本次拉号 `EvalContext`（vendor_id / zone / count / passenger.invited / bus.avg_lifespan_h）从 `surcharge_rule` 表命中规则求 `Rates` · 每轮命中细节留痕到 `pull_round_surcharge` · nil resolver 走 env 兜底

#### `decider/decide.go` · 统一系统调度决策入口

- **目的**：实现 `docs/15-scheduling.md §5` 的 `Decide(input) → output`
- **谁调它**：`bus.Scheduler`（水位巡检）· `deathwatch.RefillTick`（号死后是否补）· `stockwatch.Notify`（webhook/探针唤醒时判要不要 fire · 待第五刀）
- **输入**：触发源 / bus_id / 当刻 mode / 车里活号快照(按 vendor 分组)
- **输出**：`{拒·原因}` / `{下单 → 交给 decider.Pull}` / `{挂单 → 交给 stockwatch.Enqueue}`
- **不做**：不直接调 Pull / Enqueue —— 调用方按输出触发
- **落点理由**：调度输出只有两类（Pull / Enqueue），Pull 本来就在 `decider` · 落这里最内聚 · 不新开顶层业务包

#### 调度支撑包（服务 `decider`，不单独占业务包编号）

- `internal/stockwatch/`：缺货挂单队列；`Enqueue` / `Notify` / `Sweep` / `ModeMgr` / `FileFlag`；webhook / 探针唤醒后回到 `decider.Pull`
- `internal/vendorbalance/`：vendor 侧余额 5min 缓存；供 `decider` 做乐观预检和 fallback
- `internal/pricing/`：`vendor_pricing` / `surcharge_rule` 解析；供估价 API 和 `decider` 统一算价
- `internal/insight/`：看板聚合读模型；不参与拉号决策

### `internal/deathwatch/` (业务包 10/15)

- **定位**：**号死监控 + 质保退款** —— 探活 housepool + 收号死 webhook → 标 `credential_ledger.status='dead'` · 走 vendor 政策退款 · 号死后是否补车**不由自己决定**（交给 `decider.Decide(death_refill)`）
- **输入**：housepool 探活轮询 / 归一化的 webhook 事件（`webhookin` 通过 `SweepTrigger` 接口调）
- **输出**：
  - `credential_ledger` 标死 + `pending_refill` 入队（号死路径）
  - 退款事务（走 `wallet.warranty_refund`）
  - `RefillTick` 扫 pending_refill → 调 `decider.Decide(death_refill)` → 输出为下单则调 `decider.Pull`
- **依赖**：`housepool`, `wallet`, `decider`（RefillTick 通过 puller 接口调用 · 装配层桥接）
- **谁调它**：定时器（`Sweep` / `RefundTick` / `RefillTick`）+ `webhookin`（SweepTrigger）
- **平均寿命统计**：`credential.dead_at - created_at` 聚合成 `(vendor, 窗口)→ 平均寿命`，供 `decider` 使用
- **死活信号 3 路**：housepool 探活 / vendor webhook / vendor 死活端点轮询（兜底）
- **P 标签**：1a 基础（探活 + 踢死号）→ 1d（webhook 归一化 + 自动补车 + 寿命统计）
- **不做**：
  - **不 import `strategy`**（deathwatch → strategy 意图池是老思路 · 已废）
  - **不自己决定要不要补车**（现代码 `RefillTick` 已接 `decider.Decide` · 不再自己直拉）
  - **不选 vendor / 不动加价栈**（那是 decider 的活）
- **规则 A**：见 §00.7.5

### `internal/webhookout/` (业务包 11/15)

- **目的**：我方对乘客推的 webhook（`new_keys_available` / `all_keys_dead` / `refund` / `boarded`）
- **输入**：内部事件（来自 `decider` / `deathwatch` / `bus`）
- **输出**：HTTP POST 到乘客配置的 webhook URL；重试 3 次（超时 8 秒，指数退避）
- **依赖**：`passenger`（拿 webhook URL 与签名密钥）, `infra/httpx`, `infra/secrets`
- **谁调它**：`decider`, `deathwatch`, `bus`
- **P 标签**：1e

### `internal/pullrecord/` (业务包 12/15)

- **目的**：单独拉号后号在 housepool `record-<pid>` group + `disabled=true` 的**编排器**（不是数据库表管理器）
- **输入**：`decider` 拉到号 & 意图 `target: record-<pid>` → 调 `housepool.BatchImport(groups=[record-<pid>], disabled=true)`
- **输出**：用户后续调 `Assign(passenger_id, plan)` 派去向：
  - 进车 X → `housepool.UpdateCredential(id, {groups: [bus-X], disabled: false})`
  - 推自己号池 → 取明文 → `delivery/passengerpool` 双写 → `housepool.UpdateCredential(id, {disabled: false})`
  - 拿走 → 取明文 → `delivery/handoff` 交给用户 → `housepool.DeleteCredential(id)`
- **依赖**：`housepool`, `delivery/*`, `infra/db`（存 handoff 历史）
- **谁调它**：`decider`（单独拉号后创建）、`api` UI/API（用户派去向时）
- **P 标签**：1a（次入口一开始就要）
- **原子性要求**：**先做外部 delivery，再改 housepool 状态**（顺序反了 → 用户没拿到号但号已经变态）

### `internal/bus/` (业务包 13/15)

- **定位**：**车实体管理** —— 车的 CRUD / 成员 / 邀请码 / 车级策略字段存储 / anon 撮合 / 5min 兜底 `Scheduler`（水位巡检 · 但**不自己判**是否补 · 调 `decider.Decide(scheduler)`）
- **目的**：bus 实体管理（成员 / 邀请码 / 补车规则 / 目标水位）
- **子概念**（1c 定稿 · `kind` 只分"谁建的"·不分行为·见 `06-db-schema.md §车 kind 语义`）：
  - **用户建的车**（`kind: single` / `team` · 两者行为完全一致）—— 一律带邀请码 · 1 人时独享 · 邀朋友进来就是多人拼车
  - **系统撮合池**（`kind: anon`）—— 系统建 · 没邀请码 · 谁进由撮合决定
  - **人数是状态不是类型** —— 独享 / 拼车只看 `member_count`·不看 `kind`
- **输入**：`CreateBus` / `Join` / `Leave` / `SetInviteCode`
- **输出**：`Bus { id, name, kind, invite_code, members[], refill_watermark, ... }`（`kind` 只标"谁建的"）
- **依赖**：`passenger`, `infra/db`
- **谁调它**：`strategy`, `coalescer`, `deathwatch`, `pullrecord`（用户从拉号记录派号进车）
- **P 标签**：1a（single）→ 1c（anon）→ 2a（team）
- **housepool 映射**：每个 bus 对应一个 group `bus-<bus_id>`
- **不做**：
  - **不选 vendor / 不算价 / 不做加价栈**（那是 decider）
  - **不感知 vendor mode / stockwatch / vendorbalance**（那是 decider 的调度支撑包）
  - **不自己判要不要补车**（Scheduler 只负责巡检 + 调 `decider.Decide` · 输出决定动作）

---

## Layer 4 · 我方号池

### `internal/housepool/` (业务包 14/15)

- **目的**：我方号池的**抽象**（可插不同实现）
- **输入**：`BatchImport / MoveToGroup / IssueClientKey / RevokeClientKey / Alive`
- **输出**：`Credential[]`, `ClientKey`, `HealthReport`
- **依赖**：具体实现子包（`housepool/kirors`）
- **P 标签**：1a

### `internal/housepool/kirors/` (业务包 14/15 · 同家族)

- **目的**：**具体实现** = kiro.rs 客户端（对接 `kiro.aibbq.xyz`）
- **kiro.rs 端点清单**（已在 kiro.rs 源码 `src/admin/router.rs` 确认）：

| 我方法 | kiro.rs 端点 |
|---|---|
| `ListCredentials` | `GET /credentials` |
| `GetCredentialBalance(id)` | `GET /credentials/{id}/balance` |
| `TestCredential(id)` | `POST /credentials/{id}/test` |
| `BatchImport(reqs)` | `POST /credentials/batch-import` |
| `UpdateCredential(id, {groups?, disabled?, priority?, ...})` | `PUT /credentials/{id}` |
| `SetDisabled(id, bool)` | `POST /credentials/{id}/disabled` |
| `SetDisabledBatch(ids, bool)` | `POST /credentials/batch/disabled` |
| `DeleteCredential(id)` | `DELETE /credentials/{id}` |
| `DeleteCredentialBatch(ids)` | `POST /credentials/batch/delete` |
| `ListGroups` / `CreateGroup` / `UpdateGroup` / `DeleteGroup` | `GET/POST/DELETE/PATCH /groups[/{name}]` |
| `ListClientKeys` / `CreateClientKey` / `UpdateClientKey` / `DeleteClientKey` | `/client-keys/*` |
| **统计端点**（bus 视角号数据） | |
| `StatsOverview` | `GET /stats/overview`（today/week calls, tokens, errors, credits, activeCredentials, activeClientKeys） |
| `StatsByCredential(window, ?group)` | `GET /stats/by-credential?range=24h\|7d\|30d[&group=bus-<id>]`（每号 calls / inputTokens / outputTokens / errors） |
| `StatsTimeSeries(window, ?group)` | `GET /stats/timeseries?range=24h\|7d\|30d&granularity=hour\|day` |
| `StatsByModel(window)` | `GET /stats/by-model` |

**平均并发**：kiro.rs **未提供直接读端点**（虽然内部有并发计数——`POST /credentials/{id}/clear-concurrency` 存在为证）。当前 3 条出路：
- (a) 给 kiro.rs 加 `GET /credentials/{id}/concurrency` 端点 —— **推荐**
- (b) 我方定期采样聚合分钟级峰值 / 均值
- (c) 从 `stats_timeseries` 反推（需响应时间，未提供）

**决策**：**(a) 待跟 kiro.rs 运维方（用户本人）拍板**；未拍板前平均并发字段留空展示"—"。

- **依赖**：`infra/httpx`, `infra/secrets`（kiro.rs admin key）
- **P 标签**：1a

---

## Layer 5 · 下游交付

### `internal/delivery/` (业务包 15/15)

- **目的**：号"出去向"的实现父目录（去向 ② 双写 + 去向 ③ handoff）
- **子包**：
  - `passengerpool/kirors/` —— 去向 ②：推乘客的 kiro.rs（双写）
  - `handoff/` —— 去向 ③：把号数据交给用户，离开系统
- **注意**：**API 不是 delivery 子包**（API 只是入口通道，能触发 3 种去向的任一种）；API 层在 `internal/api/` 基础设施包

### `internal/delivery/passengerpool/kirors/`

- **目的**：把 credential **复制**到乘客运维的 kiro.rs；**housepool 副本保留用于监控**
- **输入**：`{passenger_id, credentials[]}`
- **输出**：成功 / 失败（失败重试 3 次）
- **依赖**：`passenger`（拿 url + token）, `housepool`, `infra/httpx`, `infra/secrets`
- **谁调它**：`bus`（成员配了自动双写时）、`pullrecord`（用户选"推我号池"）
- **P 标签**：1e

### `internal/delivery/handoff/`

- **目的**：把号原始数据（credential 明文）交给用户，然后从系统删除
- **输入**：`{passenger_id, credentials[]}`
- **输出**：credential 明文（UI 下载 / API 返回）
- **副作用**：housepool 副本删除、数据库拉号记录标"已 handoff"（不留明文）
- **依赖**：`housepool`, `pullrecord`
- **谁调它**：`pullrecord`（用户选"拿走"）、`internal/api`（API 触发）
- **P 标签**：1e
- **注意**：**这是唯一"发了不管"的路径**，其他去向我方都监控

---

## 基础设施包（不计业务包上限）

### `internal/api/`

- **目的**：HTTP server 主入口 + 路由 + 认证中间件 + 请求日志
- **依赖**：几乎所有业务包
- **P 标签**：1a

### `internal/config/`

- **目的**：yaml / env 配置读取；单例
- **P 标签**：1a

### `internal/db/`

- **目的**：SQLite 连接 + migration；每业务包**自己**写 sql（不建 ORM）
- **P 标签**：1a

### `internal/httpx/`

- **目的**：出向 HTTP 客户端（代理 / 超时 / 重试骨架 / no_proxy 一致处理）
- **P 标签**：1a
- **旧项目复用**：`kiro-auto/internal/httpx/*` 可参考

### `internal/secrets/`

- **目的**：加密存储敏感字段（vendor 账户凭证、payment-gateway 密钥、乘客 passengerpool token、乘客 webhook 密钥）
- **P 标签**：1a
- **实现**：AES-GCM，主密钥来自环境变量

### `internal/authpassenger/`

- **目的**：乘客登录 · **Go 自建**（无外部依赖）
- **实现**：
  - 密码哈希：Argon2id（`golang.org/x/crypto/argon2`，`memory=64MB, iterations=3, parallelism=2`）
  - Session：`sessions` 表 + cookie 存随机 token（32 hex）；也接受 `X-API-Key` header 登录
  - API key：`passenger_api_key.key_hash` 存 SHA-256(明文)；明文只 create/rotate 时返回一次
  - 邮箱验证 / 忘记密码 / 社交登录：**阶段 1 不做**（放 §00.9 未决 · 阶段 3+ 补）
- **P 标签**：1a
- **备注**：**不用 SuperTokens**（`decisions.md §1.9` 记录）—— 项目是公益工具，自建 200 行 Go 够用

### `internal/authadmin/`

- **目的**：管理端登录（简单 email/password + 白名单）
- **P 标签**：2c
- **备注**：管理端 UI 靠后，登录能力也靠后

### `internal/vendorview/`（策略支撑层 · 不占业务包编号）

- **定位**：**服务 `decider` 的策略支撑层** —— 不是纯 read-model。既是`providers.Registry` 的对外视图（脱敏 + 已计价数据给 API 用），又是**采集层**（Prober / Backfiller 主动打 vendor）和**决策数据源**（`PickBestVendor` / `PickBestVendorExcluding` 供 `decider.Decide` 用）。
- **为什么不算业务包**：本文档 CLAUDE.md §4 的 15 包上限是给**业务域**留的。vendorview 是给 decider 提供 vendor 侧原料的支撑层（跟 stockwatch / vendorbalance / pricing 一样），不代表一个独立业务域。
- **组件**：
  - `Service` · aggregate stock / prices / auto-pick / stats（api 层直接调这里）
  - `Prober` · 每 60s 打每家 vendor 的 `Stock()` + `PublicStatus()` · 结果落 `vendor_probe`
  - `Backfiller` · 每 5min 拉 vendor 侧真历史（`FleetLister` / `OrderHistoryLister` / `KeyHistoryLister`）· 落 `vendor_order` / `vendor_key` / `vendor_dispatch`
  - `ProbeStore` · `vendor_probe` 读写 + `DeriveDispatchSummary()`（3 家无 fleet 端点的兜底：从探针增量推 dispatch）
  - `OrderKeyStore` · `vendor_order` / `vendor_key` / `vendor_dispatch` 读写 + `DispatchSummary` / `HistorySummary` / `KeyLifecycleBuckets`
- **对外接口**（optional interfaces · `internal/providers/`）：
  - `PublicStatuser` · fleet 累计（kirooo / kirodrop / kiroappio）
  - `FleetLister` · 历史开号批（kirooo / kiroceo / kiroappcc）
  - `OrderHistoryLister` · 个人订单
  - `KeyHistoryLister` · 个人 key 生命周期
- **P 标签**：1a
- **6 家端点覆盖矩阵**：见 `docs/vendors/README.md` 底部 · 决策见 `docs/decisions.md §11`

### `web/`

- **目的**：前端 SPA（乘客侧 + 管理侧）
- **P 标签**：1a 起走
- **技术栈**：待 §00.9 未决拍板（沿用旧项目 Bun + React 或换栈）

---

## 依赖关系（不完全，只列关键）

```
    api ──► (所有业务包)

    passenger ── wallet ── redeem
                 │  └── payment
                 ▼
             strategy ──► coalescer ──► decider ──► housepool（进 bus group）
                              │             │            │
                              │             │            └──► pullrecord（次入口单独拉号）
                              ▼             ▼                       │
                             bus         providers                   ▼
                                                                delivery/{passengerpool,handoff}

    deathwatch ──► strategy / wallet / housepool / bus
    webhookin  ──► providers（解析）→ deathwatch / strategy
    webhookout ──► passenger / httpx
```

**无循环**：`api > 业务包 > 基础设施`；同层禁止互相 import 除下面这些明确条：
- `decider` 可以 import `pullrecord`（单独拉号后写记录）
- `deathwatch` 可以 import `strategy`（补车意图回投）
- `coalescer` 可以 import `bus`（查成员）
- `pullrecord` 可以 import `bus` / `delivery/*` / `housepool`（用户指派去向时调）

**如果发现同层还需要互 import，先来一次架构 review 再决定**。

---

## 业务包盘点

| # | 业务包 | Layer | P 标签 |
|---|---|---|---|
| 1 | `providers` | 2 | 1a → 1b |
| 2 | `webhookin` | 2 | 1d |
| 3 | `passenger` | 3a | 1a |
| 4 | `wallet` | 3a | 1a |
| 5 | `redeem` | 3a | 1b |
| 6 | `topup` / `topupchannel` / `paymentgw` | 3a | 1b |
| 7 | `strategy` | 3b | 1a（手动）→ 1d（自动） |
| 8 | `coalescer` | 3c | 1c-1（骨架）→ 1c-2（真集单）→ 2a（team） |
| 9 | `decider` | 3d | 1a → 1d（比价 + fallback） |
| 10 | `deathwatch` | 3e | 1a（基础）→ 1d（webhook + 自动补车） |
| 11 | `webhookout` | 3f | 1e |
| 12 | `pullrecord` | 3g | 1a（次入口一开始就要） |
| 13 | `bus` | 3h | 1a（single）→ 1c（anon）→ 2a（team） |
| 14 | `housepool` | 4 | 1a |
| 15 | `delivery` | 5 | 1e |

**基础设施包**（不计 15 上限）：`api` / `config` / `db` / `httpx` / `secrets` / `authpassenger` / `authadmin` / `web`

---

## `03-modules.md` 之外

- **每个业务包的内部 struct / sql / 具体函数**：**不在本文档写**。落到该包 `README.md` 或直接看代码
- **具体的 API endpoint 列表**：见 `api/` 的 README（1a 时才写）
- **数据库 schema**：待 1a 落码时随第一批 migration 定形
- **前端页面清单**：待前端启动时另开文档
