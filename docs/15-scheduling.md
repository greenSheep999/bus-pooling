# 15 · 调度系统设计（完整版）

> **本文管什么**:拼车产品的调度系统怎么运作 —— 用户能配什么、系统何时替用户拉号、上游有货没货怎么应对、多个 vendor 之间怎么切换。**这份是完整系统设计**·不是历史修补记录。
>
> **不管**:状态机步骤(去 `09-transactions`)· 加价栈算法(去 `10-pricing`)· 具体 vendor API 字段(去 `11-fields`)。
>
> **贯穿模块**:`strategy` / `bus` / `coalescer` / `decider` / `stockwatch` / `deathwatch` / `vendorbalance` / `webhookin` / `pricing`。
>
> **字段口径**:本文正式字段必须跟当前 API / schema / Go struct 对齐：`AutoRefillEnabled` / `RefillWatermark` / `RefillMinCount` / `PerRoundCount` / `MaxUnitPrice` / `PreferredVendor`。未落库的产品设想（例如 `PrebuyEnabled` / 付费抢号优先级）只能写进 `decisions.md`,不能写成本设计的当前字段。

---

## 0 · 整体导览 · 本文是"调度权威入口"

**一句话定位**:三类车(single/anon/team) · 六触发源(manual/webhook/probe/deathwatch/scheduler/coalescer) · 一个决策器(`decider.Decide`) · 一个拉号出口(`decider.Pull`) · 一个策略读取入口(`strategy.Effective`) · **本文是这五件事的唯一权威定义**。

**读本文的顺序**:

| 想了解 | 看这里 |
|---|---|
| 一句话看清全貌 | §1 三层视图 |
| vendor 层什么事件推给我方 | §2 事件源清单 |
| 系统层有哪些策略 / 阈值 / 后台任务 | §3 全策略清单 |
| 用户能配哪些字段 · 优先级怎么算 | §4(**§4.3 是策略优先级铁律 · sprint-1f-A 已落权威 · 别改**) |
| 一次拉号从触发到入池的完整流程 | §5 完整决策流 |
| 缺货挂单 / 四层兜底 / 建车路径 | §6-§8 |
| 场景验证 / 反查表 / 变更协议 | §9-§11 |
| **三类车怎么统一到同一模型** | **§12 三条车路径统一调度模型**(1f-D 新增) |
| **六触发源边界 · 输入 / busID 来源 / 输出 / 钱** | **§13 六触发源边界表**(1f-D 新增) |
| **stockwatch → decider → refund 完整状态时序** | **§14 状态机 + 时序**(1f-D 新增) |

**跟其他文档的分工**:

- **`docs/09-transactions.md`**:`pending_purchase` / `pending_assignment` / `pending_handoff` 状态机的**逐字段定义 + 崩溃恢复**。本文 §14 引它 · **不重复表结构**。
- **`docs/10-pricing.md`**:加价栈 / 三档减免 / `vendorview.PricedFor`。本文 §3.2 引它 · **不重复算价规则**。
- **`docs/06-db-schema.md`**:表结构 / 字段类型 / 索引。本文字段名跟它对齐 · **不重复 SQL**。
- **`docs/03-modules.md`**:15 业务包依赖关系。本文只写"调度决策"跨的那几个包(`strategy` / `decider` / `bus` / `stockwatch` / `deathwatch` / `coalescer`)。

**能力覆盖标注约定**(下文"当前 vs 目标"处一律按四项标实 · 不用"已接通"等模糊词):

- **API**:是否有 HTTP 接口(handler 路径 · 已落 = ✅ / 未落 = ⏸)
- **DB**:是否有 schema 支撑(migration + 表/字段 · ✅ / ⏸)
- **state**:是否有运行时状态字段(status 枚举 / 落表 · ✅ / ⏸)
- **test**:是否有测试守住行为(`_test.go` · ✅ / ⏸)

四项全 ✅ 才叫"当前实现" · 任一 ⏸ 就要在段内标"目标口径"。

---

## 1 · 三层视图 · 一图看清

```
┌──────────────────────────────────────────────────────────────┐
│ Vendor 层(外部 · 上游 6 家 + xi8 聚合)                        │
│   我方观测但不能改 · 只能"接住"事件和"主动去问"              │
│   · webhook 推:新号 / 号死 / 质保退款 / 号被撤                │
│   · 我方探针 60s:GET /stock                                   │
│   · xi8 聚合 30s+5min(数据补齐 · 不参与抢号)                  │
│   · 我方在 vendor 侧的钱(vendorbalance 5min)                  │
└──────────────────────────────────────────────────────────────┘
                          ↓ 事件流入
┌──────────────────────────────────────────────────────────────┐
│ 系统层(我方 · 决定何时替用户动作)                              │
│   · 上游状态判断:Cool(号多) / Balance / Tight(号少)           │
│   · 加价栈:号价 + 服务费 + surcharge_rule                    │
│   · 拉号出口:decider.Pull(所有触发最终都调它)                │
│   · vendor 切换:某家没钱切下一家                              │
│   · 急停开关:TURBO_ON / KILL_PULLS 文件哨兵                   │
└──────────────────────────────────────────────────────────────┘
                          ↓ 系统按用户配置动作
┌──────────────────────────────────────────────────────────────┐
│ 用户层(乘客配置 · 每车一套 + 全局默认)                        │
│   · 自动补:开关 + 水位线 + 每轮最少拉几个                    │
│   · 缺货挂单:系统能力 · 付费优先级尚未落库                   │
│   · 上限:每号最贵多少 / 每天最多几轮 / 每天最多花多少        │
│   · 偏好:首选哪家 vendor / 默认区域                          │
└──────────────────────────────────────────────────────────────┘
                          ↓
                   唯一号入库出口:decider.Pull
                          ↓
                     号进 bus group
```

---

## 2 · Vendor 层 · 事件源清单

**推送**(webhook · 200ms-2s 到达):

| 事件 | 落表 | 直接触发 |
|---|---|---|
| 新号可拉 | `vendor_dispatch` + `inbound_webhook_event` | 唤醒挂单车 |
| 号死了 | `credential_ledger.status='dead'` | 立刻标死 + 入待补队列 |
| 质保退款 | `pull_round.status='refunded'` + `wallet_ledger` | 无(终结事件) |
| 号被撤 | `credential_ledger` 强制标死 | 立刻标死(不退款) |

**轮询**:

| 数据源 | 周期 | 作用 |
|---|---|---|
| vendor `GET /stock` 探针 | 60s | 定价 + 缺货判定 + 号少时抢号关键补位 |
| xi8 聚合 signals | 30s | 补 vendor 单端只给一区的空缺 · 不参与抢号 |
| xi8 全量 restock-log | 5min | 探针空窗历史价补 |
| vendor `Balance()` | 5min | 我方在 vendor 侧的钱 · 不够切下一家 |
| webhook 载荷 price/available | 事件驱动 | 前端 price-trend 图 + 定价 fallback |

---

## 3 · 系统层 · 全策略清单

### 3.1 上游状态判断(每 30s 自采样)

```
demand = 挂着等抢号的意图数
supply = 6 家 vendor 过去 5min stock 均值累加
ratio  = demand / max(1, supply)

ratio > 2       → Tight(号少)     · 探针 + webhook 都主动抢
0.3 < ratio ≤ 2 → Balance(一般)   · 只 webhook 主动抢 · 探针只观测
ratio ≤ 0.3     → Cool(号多)      · 都不主动抢 · 用户来了才打
```

**抢号信号源只有两个**:

| 信号 | 说明 |
|---|---|
| webhook | vendor 主动 push · 200ms-2s · 最快 |
| 我方探针 | 60s 采样对比推算 · 号少时的**关键补位** —— webhook 会漏 / 慢 / 或其他家抢先 · 靠我方主动去问兜底 |

xi8 是数据补齐 · **不参与抢号**。

### 3.2 加价栈(每次拉号最终价 = 号价 × 一层层叠加)

| 层 | 什么时候叠 | 谁受益 |
|---|---|---|
| `vendor` | 所有拉号 | 我方 |
| `zone` | 所有拉号(有区域差价时) | 我方 |
| `retail` | 未 invited 用户 | 我方 |
| `capability` | 用户开了对应能力(如抢号)时叠 | 我方 |
| `service` | 所有拉号(最外层) | 我方 |
| `single_pull` | count==1 时叠 | 我方 |

**减免逻辑**(详见 `10-pricing §2.1`):零售全套 · 社群跳 zone · 批发跳 vendor+zone。

### 3.3 系统级别硬约束

- **单次拉号数量**:`config.pull.MinCount ≤ count ≤ config.pull.MaxCount` · 超区间直接拒
- **同时在飞数**:每 vendor 上限 + 每乘客上限 · 满了直接拒
- **vendor 内建**:每家静态配 `WarrantyMinutes / MaxPerOrder / MinPerOrder`
- **vendor 启停**:`Registry.SetEnabled(id, bool)` · admin 生效即时
- **vendor 侧余额**:5min poll · 拉号前预检 · 不够切下一家

### 3.4 急停 / 强抢 · 文件哨兵(5s poll)

| 文件 | 存在 = |
|---|---|
| `KILL_PULLS` | 一切 Purchase 停(所有拉号立刻拒) |
| `TURBO_ON` | 强抢(无视 mode · 一律 fire) |

### 3.5 后台 goroutine 清单

| 名字 | 周期 | 干啥 |
|---|---|---|
| vendor 探针 | 60s | 探 stock · 落 `vendor_probe_zone` |
| stockwatch.Sweep | 30s | 扫过期挂单 · 标 expired |
| ModeMgr.sample | 30s | 采样 ratio · 自动切 mode |
| FileFlag.refresh | 5s | 读急停/强抢哨兵 |
| deathwatch.Sweep | 5min | 号池探活 · 标死 |
| deathwatch.RefundTick | 1min | 走 vendor 退款 |
| deathwatch.RefillTick | 1min | 号死后补车 |
| xi8.Backfiller | 30s+5min | 数据补齐 |
| vendorbalance.Cache.poll | 5min | 拉 vendor 余额 |
| janitor.Tick | 1min | 扫卡态 pending · 恢复 |
| webhookHealth | 1min | 长期无 webhook 报警 |
| StalenessChecker | 5min | 陈旧管线 ERROR |
| bus.Scheduler | 5min | 自动车看还剩几个号 · 该补就调 Decide |

---

## 4 · 用户层 · 全字段清单

### 4.1 每车配置(`bus.Strategy` · 每辆车一份)

| 字段 | 类型 | 什么意思 | 默认 |
|---|---|---|---|
| `AutoRefillEnabled` | bool | **自动补车总开关** · 关了系统不主动拉 | false(建车时默认关) |
| `RefillWatermark` | int | **水位线** · 活号数低于它才考虑自动补 | 0(不触发) |
| `RefillMinCount` | `*int` | **本轮最少拉几个** · nil 时按 `RefillWatermark - alive_total` 补齐差额 | nil |
| `PerRoundCount` | `*int` | 手动拉号默认几个 | 建车时抄全局 |
| `MaxUnitPrice` | `*int64` | **每号最贵多少**(microunit) | nil = 不限 |
| `PreferredVendor` | `*string` | **首选 vendor** · 空 = 系统比价选 | nil |
| `DailyRoundLimit` | `*int` | **废弃车级字段** · 当前只读乘客全局 daily limit | nil |
| `DailySpendLimit` | `*int64` | **废弃车级字段** · 当前只读乘客全局 daily limit | nil |

**anon 车专属**(撮合用 · 不参与拉号定价):`AnonZone` / `AnonMaxUnitPrice`

**明确不在当前 `bus.Strategy` 里的字段**:没有 `PrebuyEnabled` / `prebuy_enabled`。缺货挂单由 `stockwatch` 承担；付费优先排队如果要做，必须另开 migration + API + UI，不能只在本文造字段。

### 4.2 全局默认(`passenger_strategy_default` · 每乘客一份)

| 字段 | 什么意思 |
|---|---|
| `MaxUnitPrice` | 每号最贵多少的**硬上限**(跟车级取最严) |
| `DailyRoundLimit` | 每天最多几轮拉号(**跨所有车累加**) |
| `DailySpendLimit` | 每天最多花多少(**跨所有车累加**) |
| `PerRoundCount` | 建新车时预填的默认数 |
| `PreferredVendor` | 建新车时预填的首选 vendor |
| `DefaultZone` | 建新车时预填的默认区域 |

### 4.3 策略优先级铁律(权威)

**本节是策略层的唯一权威口径** —— API / decider / scheduler / 前端 / 测试**必须**引这一节 · 别自己再造规则(sprint-1f-A · `decisions.md §13.5`)。

#### 4.3.1 四层优先级 · 顺序固定

```
本次请求约束(request override)  >  车级策略(bus.strategy)  >  全局默认(passenger_strategy_default)  >  系统默认值(config.pull.*)
```

- **本次请求约束**:用户手动动作携带的一次性参数(手动拉号带 `count`/`vendor`/`zone` · 建车向导若携带首次拉号参数)· **不落库** · 只影响这一次
- **车级策略**:`bus` 表 · 每车一份 · **字段按类分两种状态语义**(migration 040 落码后):
  - **auto_refill_* 三字段**:**纯车级** · `NOT NULL DEFAULT 0` · **无"跟随全局"语义** · 建车时抄全局 default_* seed 一次 · 之后独立演化
  - **其他覆盖字段**(`per_round_count / preferred_vendor / zone`):`nullable` · NULL = 跟随全局默认 · 非 NULL(含 0/false) = 覆盖本车
- **全局默认**:`passenger_strategy_default` 表 · 每乘客一份 · **四种用途**:
  1. **硬上限**(`MaxUnitPrice / DailyRoundLimit / DailySpendLimit`):运行时**始终参与** · 跨所有车生效
  2. **新车默认 seed**(`default_auto_refill_enabled / default_refill_watermark / default_refill_min_count`):建车向导预填 UI · 落到车表后独立 · **不做运行时 fallback**
  3. **运行时 inherit fallback**(**仅对其他覆盖字段** · 不含 auto_refill_*):字段 NULL 时读全局当前值
  4. **跨车调度护栏**(migration 040 新加 · 只对自动补车生效):
     - `auto_refill_daily_budget` 所有 auto 车合计每日预算(microunit)
     - `auto_refill_min_wallet_reserve` 钱包低于此值所有 auto 车暂停(microunit)
     - `auto_refill_vendor_allowlist` 自动补车允许的 vendor JSON 数组
- **系统默认值**:`config.pull.MinCount / MaxCount / DefaultCount` 等 · vendor 静态限 · 无用户入口

#### 4.3.2 字段两类 · 规则不同

**类① 硬上限字段** —— 取更严(`min` · 不是覆盖):

| 字段 | 规则 |
|---|---|
| `MaxUnitPrice` | `min(request, 车级, 全局, +∞)` · 任一层拦住都拦 |
| `DailyRoundLimit` | 只在全局(车级 deprecated · 见 §4.1) |
| `DailySpendLimit` | 只在全局(车级 deprecated · 见 §4.1) |

**为什么取更严不覆盖**:硬上限是护栏 · 车级 30 全局 20 时若"车级覆盖" → 用户在某车设 30 就绕过全局 20 的保护;车级 15 全局 20 时取 min 15 · 车级更严也听。**任一层触发都拦**。

**请求约束更严**:手动拉号带 `max_unit_price=10` · 全局 20 车级 15 · 最终按 10 拉;若手动带 20 · 全局 15 · 仍按 15 拉(请求**不能放宽**硬上限)。

**类② 覆盖字段** —— 后者盖前者:

| 字段 | 规则 |
|---|---|
| `PerRoundCount` | 车级 → 全局 → `config.pull.DefaultCount`(**注**:是"默认每轮拉几个"偏好·非 request 字段·见 §4.3.2d) |
| `PreferredVendor` | request → 车级 → 全局 → 系统比价选(AutoPick) |
| `Zone` / `DefaultZone` | request → 车级 → 全局 → 代码默认 `auto` |
| `AutoRefillEnabled` | 车级(**当前唯一来源** · 无 request 语义 · 无全局 fallback) |
| `RefillWatermark` | 车级(**当前唯一来源** · 同上) |
| `RefillMinCount` | 车级(**当前唯一来源** · 同上) |

**覆盖的"是否有值"由字段类决定**(migration 040 后):

| 字段类 | "跟随上层" | "覆盖本车" |
|---|---|---|
| **auto_refill_* 三字段**(纯车级) | ❌ **无此语义** —— 字段 `NOT NULL DEFAULT 0` · false/0 就是"关自动补" · 不是"跟随全局" | 字段任意合法值 |
| **其他覆盖字段**(per_round_count / preferred_vendor / zone) · nullable | 字段 = NULL | 字段非 NULL(**含 0 / false**) |
| **request override**(手动拉号一次性参数) | 请求 payload 字段**未出现** | 字段**出现且合法**(**含 0 / false**) |

**关键**:对 `bool` / `int` 字段 · `0` 和 `false` 是**合法覆盖值** · 不是"空"。别用"非空"判"是否覆盖" —— 判据见上表。

**⚠️ `AutoRefillEnabled` / `RefillWatermark` / `RefillMinCount` · 最终口径**(migration 040 · CLAUDE §1.5):

- **车级**:**纯车级值** · `NOT NULL DEFAULT 0`(RefillMinCount 保留 nullable · nil = 按 gap 补) · **无"跟随全局"语义**
- **全局 `passenger_strategy_default.default_*` 三字段**:**只做建车向导 seed** · 建车预填一次 · **不做运行时 fallback** · 改这里不影响老车
- **全局 3 个跨车调度护栏**(migration 040 新加 · 只对自动补车生效):`auto_refill_daily_budget` / `auto_refill_min_wallet_reserve` / `auto_refill_vendor_allowlist` —— 手动拉号不受此约束(见 §4.3.4)

**已作废方案**(sprint-1f-A/B 中间态 · migration 040 撤回):
- ❌ 车级 `auto_refill_enabled` / `refill_watermark` 改 nullable · NULL = 跟随全局
- ❌ 车级 3 个 Segmented toggle "跟随全局 / 覆盖本车"
- ❌ 全局 `default_*` 作为运行时 fallback

**决策记录**:`docs/decisions.md §13.5`(migration 040 撤镜像的完整语义讨论)。

#### 4.3.2d `request.count` vs `PerRoundCount` · 别混

两个东西**语义不同 · 优先级链也不同**:

- **`request.count`**:一次性动作参数 · 用户手动拉号 / 建车向导首次拉号时**显式**带的"这一次拉几个"。**最高优先级** · **不是**持久策略字段 · 不落库。
- **`PerRoundCount`**:**默认批量偏好** · 落库(车级 / 全局)· 用于:
  - 手动拉号但 request **没显式给 count** 时的 fallback
  - 建车向导 seed(建车时抄全局值到车级)
  - **不用于自动补车 count** —— 自动补车的 count 由 `RefillMinCount` / `RefillWatermark - alive_total` 的 gap 决定(Step 5 · §5.2)

**优先级链**:
- `request.count`(手动动作)· 优先级最高 · 有值就用 · 不用降级
- 无 `request.count` 时才走 `PerRoundCount` 三层链(车级 → 全局 → `config.pull.DefaultCount`)
- 自动补车根本不查 `PerRoundCount`(gap-based 见 Step 5)

#### 4.3.3 自动触发无 request · 走车级 → 全局 → 系统

`webhook` / `deathwatch` / `scheduler` / `probe` / `coalescer` 五个触发源**没有**"本次请求" · 直接从车级开始走:

```
车级(有 busID)  →  全局默认  →  系统默认值
```

**关键 · 调用 `Effective()` 前必须解析成"逐车决策"**:

原始 webhook / probe 事件本身**通常只有 `vendor_id` / `zone` / `stock` 信息** · 不天然带 `busID`。**这不代表**自动触发链路可以绕过 busID —— **正确口径是**:

| 触发源 | 原始 payload | 如何解析出 busID |
|---|---|---|
| `webhook`(vendor 新号) | vendor / zone / stock | webhook 桥先按 vendor/zone/低水位扫**候选 bus 集合** · 逐辆调 `Effective(pid, busID, nil)` · 各自决策 |
| `probe`(我方探针) | vendor / zone / new_stock | 同 webhook · probe 桥扫候选 · 逐辆决策 |
| `deathwatch.RefillTick` | `pending_refill.dead_credential_id` | `pending_refill.bus_id` 字段已带(NULL = 单独提取 · 不走车级) |
| `bus.Scheduler`(5min 兜底) | 遍历自动车 | 每辆车自带 `busID` |
| `stockwatch.Notify`(fire 挂单) | `stock_watcher.id` | `stock_watcher.bus_id` 字段已带 |
| `coalescer`(集单) | 意图窗口 | 意图窗口必须归属某个 `busID`(coalescer 不跨车合并) |

**禁止**:没有 `busID` 就直接用全局策略对**所有车**做自动拉号(会造成"改一次全局默认 · 所有 auto 车瞬间齐补" · 违反用户预期)。

**允许**:`record` 模式(用户单独拉号进 `record-<pid>` group)· `busID` 为空 · 走 request override → 全局默认 · **不涉及车级**。

#### 4.3.4 唯一入口 · `internal/strategy.Effective()`

**别再手工拼字段** —— 所有策略读取路径必须调:

```go
strategy.Effective(ctx, passengerID, busID, requestOverride) → EffectiveStrategy
```

- `passengerID`:必填
- `busID`:自动触发时必填(不填退化成全局-only) · 手动无 bus 场景(record group)可为空
- `requestOverride`:手动动作时填 · 自动触发传 nil
- 返值:**已经算好优先级的最终值** · 调用方不能再二次拼

**接入清单**(sprint-1f-C 落码 · 路径均为**当前仓库真实位置**):

| 调用点 | 位置 | busID | requestOverride |
|---|---|---|---|
| 手动拉号 | `internal/api/bus.go handleBusPull` | 有 | 有(count/vendor/zone) |
| 建车向导首次拉号 | **目标链路**(当前 `handleCreateBus` 不做内部拉号 · 用户建完车再手动点拉号走 `handleBusPull`)· 若 1f-B/C 加 `first_pull` / `initial_pull` 参数 · 落在 `handleCreateBus` 后半段 · 现暂无此实现 | 有(新建 bus 的 id) | 有 |
| bus.Scheduler(5min 兜底) | `internal/bus/autorefill.go`(**当前文件名 · 非 scheduler.go**) | 有 | 无 |
| deathwatch.RefillTick | `internal/deathwatch/refill.go` | 有(`pending_refill.bus_id` · NULL=单独提取) | 无 |
| webhook auto scan | `cmd/bus-pooling/main.go` 里的 `webhookAutoScanBridge`(**当前位置 · 非独立 bridge 文件**) | 无 → 由 bridge 扫候选 bus 后逐辆调 | 无 |
| stockwatch.Notify(fire 挂单) | `internal/stockwatch/`(fire 路径) | 有(`stock_watcher.bus_id`) | 无 |
| decider.Pull 内部兜底 | `internal/decider/orchestrator.go` / `fire.go`(当前无独立 `pull.go`) | 上游传入 | 上游传入 |
| record 拉号(单独) | `internal/api/pullrecord.go` | 无(空串) | 有 |

**不存在的路径**(gpt 审阅时误列 · 记录避免造):
- ❌ `internal/api/bus.go handleBusRefillNow` —— 当前**没有**这个 handler(立即补车走 `handleBusPull` 主路径 · 未来若单开可补)
- ❌ `internal/bus/scheduler.go` —— 当前实际是 `autorefill.go`(1f-C 若拆文件可命名 scheduler · 现在保留原名)
- ❌ `cmd/bus-pooling/webhookout_bridge.go` —— 事件通知出向的 bridge 在 `webhookout_bridge.go` · **webhook auto scan(vendor 入向) bridge 在 `main.go`** · 别混

**验收**(sprint-1f-C 完成后 · 精确 grep · 不误伤 DTO/schema/tests):

**禁止**:
- `decider` / `bus.Scheduler` / `deathwatch` / `stockwatch` / `webhookAutoScanBridge` **运行时决策路径**直接 SELECT 或手工合并策略字段
- 在 `Effective()` 外计算 `max_unit_price` min 链
- 在 `Effective()` 外做 `preferred_vendor` 三级 fallback
- 在 `Effective()` 外解释 `refill_watermark` / `refill_min_count` 语义

**允许**:
- DB schema / migration(SQL 文件)
- API request/response DTO(struct 字段定义 + json tag)
- store 基础读写(`SELECT ... FROM passenger_strategy_default` / `bus` 的 CRUD)
- `internal/strategy/` 包内部(`Effective()` 自身实现 + 单测)
- `_test.go` 测试用例
- `docs/` markdown
- handler 把 request override 参数**传给** `Effective()` 的合法转发代码

#### 4.3.5 UI 展示规范 · 前端契约(先立 · 后端 1f-B 按此对齐)

**目的**:前端 UI 契约先落定 · 避免 1f-B 后端落 DB / API 时前后端交互层再偏移(用户 1f-A 补丁指令)。

##### 4.3.5.1 展示规则:实际生效值

**给用户看"实际生效值"**·不是"你设的值":

- **车级设 `max=1000` · 全局设 `max=100`** → 展示 "实际生效 100(受全局上限约束)" · 不是"当前:1000"
- **车级留空 `preferred_vendor`(NULL)** → 展示"跟随全局默认(**读全局配的那家 · 按用户 tier 显示真名或匿名 label**)" · 不是空
- **全局也留空** → 展示"系统自动选(AutoPick)"

⚠️ **文档里不写死 vendor 名** —— 全局配的哪家是**用户配置项** · 不是文档常量。展示时按 `docs/10-pricing §2.1` 的 tier 规则决定显真名(wholesale)还是匿名 label(retail/community · 例:`Vendor 03`)。

##### 4.3.5.2 EditStrategyPanel 二态切换 UI(依赖 §4.3.2b 继承落地)

**每个"覆盖字段"旁边有一个 toggle · 二态**:

```
○ 跟随全局默认                 ● 覆盖本车
  值灰显 · 只读                   值可编辑
  显示当前全局值 · 加"跟随"标签    可自由输入
  全局改了这里也改                 全局改不影响这里
```

**硬上限字段无 toggle** —— 硬上限总是取 min · 车级设的值只作为**更严的补充** · UI 直接输入 · 但旁边**必须**标"仍受全局 X 约束(实际生效 min(车级, 全局))"。

##### 4.3.5.3 前端字段契约(TS 类型 · migration 040 落码后)

**分两组** —— nullable(覆盖字段) vs 非 null(auto_refill_* 纯车级):

```typescript
interface BusStrategy {
  // 硬上限 · 车级值 · 无二态 · null = 车级不加严 · min(车级, 全局) 由后端算
  max_unit_price: Money | null;

  // 覆盖字段 · null = 跟随全局 · 非 null(含 0/false) = 覆盖
  per_round_count: number | null;
  preferred_vendor: string | null;
  zone: "us" | "eu" | "auto" | null;

  // auto_refill_* 三字段 · **纯车级 · 非 null**(migration 040 后)
  // - 无"跟随全局"语义 · 建车时抄一次 default_* seed · 之后独立
  // - false / 0 就是"这辆车关自动补" · 不是"跟随全局"
  auto_refill_enabled: boolean;
  refill_watermark: number;
  refill_min_count: number | null; // 单独保留 nullable · nil = 按 gap 补
}

interface GlobalStrategy {
  // 硬上限 · 跨车累加
  max_unit_price: Money | null;
  daily_round_limit: number | null;
  daily_spend_limit: number | null;

  // 覆盖字段 · 车级 NULL 时的运行时 fallback
  per_round_count: number | null;
  preferred_vendor: string | null;
  zone: "us" | "eu" | "auto" | null;

  // 新车 seed · 建车向导预填 · **不做运行时 fallback**
  default_auto_refill_enabled: boolean;
  default_refill_watermark: number;
  default_refill_min_count: number | null;

  // 跨车调度护栏(migration 040) · 只对自动补车生效
  auto_refill_daily_budget: Money | null;         // 所有 auto 车每日合计预算
  auto_refill_min_wallet_reserve: Money | null;   // 钱包低于此值 auto 车暂停
  auto_refill_vendor_allowlist: string[] | null;  // 允许的 vendor id
}
```

**关键**:
1. **auto_refill_* 非 nullable** —— 用户界面**没有** toggle "跟随全局 / 覆盖本车" · 直接编辑
2. **改全局 default_* 不影响老车** —— 只影响新建车的 seed
3. **保存时** · 覆盖字段 toggle "跟随全局" 就发 `null` · toggle "覆盖本车" 就发用户填的值(允许 `0` / `false`)

##### 4.3.5.4 全局页(Preferences.tsx)契约

**Preferences 展示所有全局默认字段** · 无 toggle(因为它是"全局默认" · 本身就是最上层的可覆盖源) · 但要:

1. 硬上限字段旁标"**跨所有车累加 · 每车都受此约束**"
2. 覆盖字段旁标"**新车默认值** + 车级选'跟随全局'时的运行时值"(1f-B 后)
3. **今日已用/上限** 进度条(`used_today.rounds / rounds_limit` · 已存在)

##### 4.3.5.5 落地状态(migration 040 后 · 阶段 1 收官)

**已完成**:
- ✅ EditStrategyPanel 现有字段的 UI
- ✅ auto_refill_* 三字段前端**非 null**契约(TS 已对齐 · commit 3a0eca3 撤 useGlobalStrategy fallback)
- ✅ 全局 Preferences 三跨车护栏输入(daily_budget / min_wallet_reserve / vendor_allowlist)
- ✅ 三桥(refill/scheduler/webhook auto scan)decider 层 enforce 跨车护栏(commit 29174b0)

**待做**(阶段 2 · P2 收尾):
- ⏸ 覆盖字段(`per_round_count / preferred_vendor / zone`) toggle UI(现有 UI 让用户填 · null 语义未暴露)
- ⏸ Preferences 页"新车默认值"标注(现有 UI 未明说这是 seed · 不做运行时 fallback)

##### 4.3.5.6 交互失败态

用户操作过程中的**边界态**:

- **用户点"跟随全局" · 但全局也没配** → toggle 变为跟随 · 值区显示 "系统默认(N)" · N 是 `config.pull.DefaultCount` 或类型默认值
- **用户填了车级值 · 但硬上限被全局卡住** → 保存成功 · 但保存后 UI 展示"你设的 1000 · 实际生效 100(受全局上限约束)"
- **1f-B migration 未完成时用户改 auto/refill** → 前端不显示 toggle · 直接编辑车级值(跟当前一致)

---

#### 4.3.6 字段两类 · 一览

**类① 硬上限**(取 min · request 不能放宽):`MaxUnitPrice` / `DailyRoundLimit` / `DailySpendLimit`

**类② 覆盖**(后者盖前者):`PerRoundCount` / `PreferredVendor` / `Zone` / `AutoRefillEnabled` / `RefillWatermark` / `RefillMinCount`

**类③ 系统内建**(用户碰不着):`config.pull.*` / vendor 内建 / `surcharge_rule` / `ModeMgr` 阈值 / 文件哨兵

**anon 车专属**(撮合用 · 不参与拉号定价):`AnonZone` / `AnonMaxUnitPrice`(见 §4.1)

---

## 5 · 完整决策流

**核心**:六种触发源共享同一套判据 —— 一个函数 `Decide(input)` · 六处都调它。

**代码落点**:`internal/decider/decide.go`(不新开业务包 · 见 `CLAUDE.md §4.1` + `03-modules.md decider/decide.go`)。`decider` 本来就是跨 vendor 决策 + Pull 出口·Decide 只是把 bus.Scheduler / deathwatch.RefillTick / stockwatch 触发路径的判据收口到一处。

### 5.1 触发源(什么时候调 Decide)

| 触发源 | 谁触发 | 说明 |
|---|---|---|
| `death_refill` | 号死了 · 判是否补 | deathwatch 标死后立刻调 · **走完整决策器 · 受 auto 开关约束** |
| `webhook` | vendor 有新号了 | 收到 webhook 立刻调 |
| `probe` | 我方探针发现 vendor 库存涨了 | 60s 采样对比后调 |
| `scheduler` | 5min 兜底扫 | 遍历所有开了自动补的车 |
| `manual` | 用户手动点拉号 | HTTP API 调 · **绕过 auto 检查** |
| `usage` | 号快用光了 | 用量数据采集后启用(阶段 1d 后期) |

**不进决策器的路径**(独立走·不算触发源):
- **死号退款** · deathwatch 走 vendor 政策退款 · 是天赋权利 · 跟决策器无关
- **建车向导首次拉号** · 用户在 UI 明说要 · 直接调 decider.Pull · 不进决策器

### 5.2 Decide 六步串行(一步拒直接返)

**输入**:触发源 + bus_id + 当刻 mode + 车里活号快照(按 vendor 分组)

**输出**:【拒·原因】 / 【下单】(立刻 decider.Pull) / 【挂单】(记入 stockwatch 等抢)

---

**Step 1 · 系统闸门**

`KILL_PULLS` 存在 → 【拒·全停】

---

**Step 2 · 用户 auto 开关**

- `manual` → 跳过 · 直进 Step 5(手动是用户明说 · 系统不该拦)
- 其他触发源(含 `death_refill`) → 读 `AutoRefillEnabled`:
  - false → 【拒·auto off】
  - true → 继续

**关键**:`death_refill` **不再跳过** auto 检查 —— 号死退款是天赋(deathwatch 独立走)·**号死后是否补车**是自动行为·必须受 auto 约束。用户关了 auto 就是说"死了就死了·别自动补"·系统就该听。

**注**:缺货挂单不是独立触发源 —— 用户主动动作只有手动拉号 / 修改车策略。没有"POST 临时抢一次"这种 API；付费优先抢号仍是待决策能力，不是当前字段。

---

**Step 3 · 水位 + 多 vendor 备胎判据**

**前置**:`RefillWatermark ≤ 0` → 【拒·未设水位】(用户没配水位线·视同不启用自动补 · 手动 pull 例外) · 直接返

按 vendor 分组数活号 · 判"有没有 vendor 撑得住":

先算本轮需求:

```
gap  = max(0, RefillWatermark - alive_total)
want = RefillMinCount ?? gap
want = max(1, want)
```

**"撑得住"定义**:
```
撑得住 = (该 vendor 活号数 ≥ want)
       AND (该 vendor 当前单价 ≤ min(车级 MaxUnitPrice, 全局 MaxUnitPrice))
       AND (该价格数据新鲜度在允许窗口内)
```

价格过滤为什么加:就算某家 vendor 数字够·但**用户超价拉不动它**·用它做备胎没意义。

**四种情况**:

- **整车 alive == 0** → 档 = 急 · 直跳 Step 4 · **强 output**(按 Case A 特殊规则·见 Step 4 底)
- **有任一 vendor 撑得住** → 【拒·有备胎】(那家撑着·等它也见底再动)
- **所有 vendor 都撑不住·但 alive_total < RefillWatermark** → 档 = 常规 · 继续 Step 4
- **alive_total ≥ RefillWatermark** → 【拒·已达水位】

---

**Step 4 · 上游 mode × 触发源 → 决定 output**

**Case A(整车挂 alive=0)强制规则**·**优先于下表**·忽略触发源:
- Cool → 【下单】(vendor 有货直拉)
- Balance/Tight → 【挂单】(紧俏时下单必 ErrNoStock)

**Case C(常规 · alive > 0 但撑不住)** · 按下表:

| 触发源 \ mode | Cool(号多) | Balance(一般) | Tight(号少) |
|---|---|---|---|
| `webhook` | 【拒·cool 不响应】 | 【下单】vendor 说有货 | 【下单】 |
| `probe` | 【拒·cool 不响应】 | 【拒·balance 只 webhook fire】 | 【下单】关键补位 |
| `death_refill` | 【下单】vendor 有货直拉 | 【下单】 | 【挂单】紧俏时下单会 ErrNoStock |
| `scheduler` | 【下单】号多下单不亏 | 【下单】 | 【挂单】兜底扫改挂不 Pull |
| `usage` | TBD · 阶段 1d 后期数据采集完再定 | TBD | TBD |

`manual` **不进这层** —— Step 2 已跳到 Step 5 · 用户明说要 · 有货就下单 · 没货 decider.Pull 内部返 ErrNoStock(挂单是 decider.Pull 内部的 `maybeEnqueueOnNoStock` 行为·不由决策器决定)。

---

**Step 5 · 参数解析** —— **必须**走 `strategy.Effective()`(§4.3.4 · sprint-1f-C 收口)

```
eff := strategy.Effective(ctx, passengerID, busID, requestOverride)
  // 返 EffectiveStrategy · 已经算好优先级 · 直接取字段

【count · 这次拉几个】
gap    = max(0, eff.RefillWatermark - alive_total)      · 差多少号
raw    = eff.RefillMinCount ?? gap                      · 设了 min_count 就按它拉;没设就补齐差额
raw    = max(1, raw)
count  = clamp(raw, config.pull.MinCount, min(config.pull.MaxCount, vendor.MaxPerOrder))
  · 若 raw > 上限 → 截断到上限 · 差额下轮触发再补(不重触发·等 death/scheduler 自然触发)
  · 若 raw < config.pull.MinCount → 提升到 MinCount(避免 vendor 拒小单)

【maxPrice · 单价上限 · 类① 硬上限】
maxPrice = eff.MaxUnitPrice
  // Effective() 内部已算 min(request, 车级, 全局, +∞) · 见 §4.3.2

【preferredVendor · 选哪家 · 类② 覆盖】
preferredVendor = eff.PreferredVendor
  // Effective() 内部已算 request → 车级 → 全局 → nil · nil 时走 AutoPick

【加价栈】
加价栈 = 号价 × vendor × zone × retail × service × [single_pull if count==1]
```

**别再在 Step 5 里手工拼字段** —— sprint-1f-C 之后 · `decider.Pull` / `bus.Scheduler` / `deathwatch.RefillTick` 全部通过 `Effective()` 拿最终值 · `grep` 验收见 §4.3.4。

---

**Step 6 · 每日限额 + vendor 可行性**(最后一关)

- `passenger.DailyRoundLimit` 累计已用 + 1 超 → 【拒·当日轮次到顶】
- `passenger.DailySpendLimit` 累计已用 + est 超 → 【拒·当日花费到顶】
- `vendorbalance.Enough(preferredVendor, est)` 不够 → 排除该 vendor · 走 `PickBestVendorExcluding` 找下一家 · 都不够 → 【拒·vendor 侧全没钱】
- 并发满(每 vendor / 每乘客在飞上限) → 【拒·并发到顶】

**都过 → 按 Step 4 的 output 执行**:
- 【下单】→ `decider.Pull(count, vendor, maxPrice)`
- 【挂单】→ 记入 `stock_watcher` · 状态 watching · TTL 10min

**两处判据都是"乐观 + 兜底"设计**:
- **每日限额**:决策器 Step 6 提前判(省 decider.Pull 开销) · 但两辆车并发都过关时靠 `wallet.Reserve` 事务原子兜底防超限
- **vendor 侧余额**:决策器 Step 6 判 `vendorbalance.Cache`(5min 陈旧数据·乐观) · decider.Pull 内部真调 vendor 时若返 `ErrVendorInsufficient` 会再走 `PickBestVendorExcluding` 兜底切下一家

---

## 6 · 缺货挂单 · 当前实现与待决策

**当前实现**:

- 缺货挂单由 `stockwatch` 负责，当前排序是先挂先抢。
- `reserved_amount` 当前恒为 0；挂单时不预冻结。
- `stockwatch.Notify` 只在 `watching` 行上触发，fire 失败会回 `watching`，硬错才 `expired`。

**待决策能力**:

- 付费优先排队（此前叫 `PrebuyEnabled`）尚未落库，不能作为本文正式行为。
- 如果后续要做，必须同时补字段、冻结/扣费/退款、排序 SQL、前端解释、审计测试。

---

## 7 · 四层兜底 · 时间粒度递进

**"vendor 有新号"信号的兜底**(号少时 · 抢货):

```
① webhook   200ms-2s     vendor 主动 push
     ↓ 漏了(vendor 挂 / 网络抖 / 我方接收挂)
② 探针      60s          我方主动 GET /stock 采样对比 · 关键补位
     ↓ 漏了(服务重启窗口)
③ scheduler 5min         bus.Scheduler 兜底扫车里还剩几个号
     ↓ 触发了但被 Step 3 拒(有备胎撑着)
④ 下一轮    等 vendor02 也见底再判
```

**"号死了要补"信号的兜底**(独立链路):

```
① deathwatch webhook  vendor 主动 push 号死    (立即入 pending_refill 队列)
② deathwatch 探活     5min 扫号池主动确认      (webhook 漏了兜底)
③ RefillTick          1min 扫 pending_refill · 触发 death_refill 走决策器
```

**号少时等 webhook 就晚了** —— 因此 Tight 时探针必须 fire · 抢在其他平台 / 手速快用户之前。

---

## 8 · 建拼车 · "第一次一律手动"

**产品约定**:

- 建车向导 UI 里让用户选"首次如何拉第一批号"(count / vendor / zone)
- 建车 API 完成时·如果用户填了·跑一次 `decider.Pull` · 号进车
- 建车后 `AutoRefillEnabled` **默认 false** —— 用户想自动补·到车详情自己开开关
- 用户开 AutoRefillEnabled=true 后·决策器完全按 §5 六步跑

**代码不需要"从没拉过号的车跳过"特判** —— 靠 auto 默认 false 就够。

---

## 9 · 决策器 × 场景验证表

| 场景 | Step 1 | Step 2 | Step 3 | Step 4 | 结果 |
|---|---|---|---|---|---|
| 号死·一家死一家活 6·min=3·auto on | 过 | death_refill · auto on | Case B 备胎撑着 | - | **拒·有备胎** |
| 号死·两家都见底·auto on | 过 | 同上 | 常规 | 视 mode | **执行** |
| 号死·整车挂·auto on·Cool | 过 | 同上 | Case A 急 | 强制 Cool → 下单 | **执行 · 下单** |
| 号死·整车挂·auto on·Tight | 过 | 同上 | Case A 急 | 强制 Tight → 挂单 | **执行 · 挂单** |
| **号死·auto off** | 过 | **拒·auto off** | - | - | **拒·用户关了自动补·死了就死了**(退款照走) |
| vendor 新号·挂单命中 | 过 | webhook · auto on | 常规 | 按先挂先抢 | **执行** |
| 保底扫·剩号少于水位线·Tight | 过 | scheduler · auto on | 常规 | Tight → 挂单 | **挂单** |
| 用户没开 auto·上游有货 | 过 | 【拒·auto off】 | - | - | **拒·手动才动** |
| 新建拼车·空车·水位线未设 | 过 | auto off(默认) | - | - | **不动·第一批用户手动拉** |
| 用户开 auto·空车·水位线=5 | 过 | scheduler · auto on | Case A 急 · 视 mode | Cool 下单 / 其他挂单 | **执行** |
| 用户手动点拉号·有货 | 过 | manual 跳 | 不判 | 不进 | **调 decider.Pull 直下单** |
| 用户手动点拉号·没货 | 过 | manual 跳 | 不判 | 不进 | **decider.Pull 返 ErrNoStock · 内部挂 stockwatch** |

---

## 10 · 反查表(改一个字段 · 影响哪里)

**用户改一个字段**:

| 改这个 | 立即感知 |
|---|---|
| `AutoRefillEnabled` | 决策器 Step 2 · 号死立即补 · 5min 兜底扫 |
| `RefillWatermark` | 决策器 Step 3 前置 · 0 = 未启用自动补 · > 0 才判达标 |
| `RefillMinCount` | 决策器 Step 3 备胎判据 + Step 5 拉几个 |
| `MaxUnitPrice`(车级) | Step 5 · 与全局 AND 取严 |
| `PreferredVendor` | Step 5 · 车级 → 全局 → 比价 |
| `passenger.MaxUnitPrice` | 与车级 AND 取严 |
| `passenger.DailyRoundLimit` | Step 6 · 跨车累加 |
| `passenger.DailySpendLimit` | Step 6 · 跨车累加 |

**运营改一个开关**:

| 改这个 | 立即感知 |
|---|---|
| `TURBO_ON` 文件 | webhook/probe 无视 mode 一律 fire |
| `KILL_PULLS` 文件 | 所有拉号立刻拒 |
| `surcharge_rule` 改率 | 下次拉号新率生效 |
| `Registry.SetEnabled(vid, false)` | 决策器跳该 vendor |
| ModeMgr 自动切档 | webhook/probe fire 决策变 |

---

## 11 · 变更协议

- **加新触发源** → 加一行到 §5.1 + Step 4 表加一行
- **加新用户字段** → 加一行到 §4 + §4.3 分类 + §10 反查表
- **加系统开关** → 加到 §3 对应小节 + §10 反查表底行
- **改字段语义** → 更新 §4 + §10 + 落 `decisions.md §12`
- **边界漏洞 / 讨论 / 待决策 / 未拍板** → 落 `decisions.md §12` · **不进本文**
- **产品决策** → 落 `decisions.md §12` · 拍完把最终态回落本文相应位置

**本文永远是"完整设计"** —— 只写"最终该怎么运作" · 不含讨论 / 待决策 / 边界漏洞清单。

---

## 12 · 三条车路径统一调度模型(1f-D)

**核心命题**:三类车(`kind ∈ {single, anon, team}`)**建车流程不同 · 但拉号调度走同一套模型**。差别只在"车怎么诞生"和"谁能加人",诞生后的**策略读取 → 决策 → 拉号 → 派去向**四步完全一致。

### 12.1 三类车的建车流程差异

| 维度 | `single`(独享) | `anon`(系统撮合搭车) | `team`(邀请码拼车) |
|---|---|---|---|
| **谁建的** | 用户显式建(带向导) | 系统在**撮合**时按需建(用户匿名搭车) | 用户显式建(带向导 + 邀请码) |
| **建车 API** | `POST /api/me/buses` · body `kind=single` | `POST /api/me/buses/anon/match`(若没有活跃 anon 就顺便建) | `POST /api/me/buses` · body `kind=team` |
| **`bus.invite_code`** | 有(1c 之后 · 用户建的都有拼车码) | **无**(系统建的匿名池 · 不接受邀请码加人) | 有 |
| **初始 `member_count`** | 1(建者本人) | 1 或 N(取决于并发撮合) | 1 |
| **能否加人** | ✅ 能(拼车码给出去 → 变多人拼车) | ✅ 能(通过 `/anon/match` 撮合加入 · 不通过邀请码) | ✅ 能(邀请码加入) |
| **建车时首次拉号** | 向导可选(用户填 count/vendor/zone) | 撮合时**不**自动拉号 · 撮合成功后走乘客手动拉号 or 车级 auto | 向导可选 |
| **`bus.Strategy` 初值** | 抄乘客全局默认(seed) · `AutoRefillEnabled` 默认 false | 抄乘客全局默认(seed) · `AnonZone` / `AnonMaxUnitPrice` 从 `/anon/match` 请求填入 | 抄乘客全局默认(seed) |

**关键**:`kind` 只记"谁建的 / 撮合规则" · **不决定能不能加人 · 不决定调度模型**。对乘客 UI 只展示人数(独享 / N 人拼车 / 搭车) · **不暴露 kind**(见 `CLAUDE.md §12.5 / §2 术语作废清单`)。

### 12.2 三类车统一走同一调度模型

**建完车后 · 拉号调度四步一致**:

```
[任一 kind 的 bus]
    ↓
Step A · strategy.Effective(passengerID, busID, requestOverride)
    → 读 bus.Strategy(车级) · 融合 passenger_strategy_default(全局) · 融合 requestOverride
    → 返 EffectiveStrategy(已算好优先级 · §4.3 铁律)
    ↓
Step B · decider.Decide(六步决策 · §5.2)
    → 判 KILL_PULLS / auto 开关 / 水位 / mode × 触发源 / 参数 / 每日限额
    → 返 【拒·原因】 / 【下单】 / 【挂单】
    ↓
Step C · 若【下单】→ decider.Pull(count, vendor, maxPrice)
         若【挂单】→ stockwatch.Enqueue(vendor, count, ttl)
    ↓
Step D · 号入 housepool `bus-<bus_id>` group(或视 assign 参数进 `record-<pid>` group / handoff DELETE)
    → 落 credential_ledger + pull_round + wallet_ledger
```

**三类车走 A-D 完全同流** —— **没有** "anon 车走 anon 决策器"这种分叉。**唯一 anon 专属**:

- 撮合入口 `POST /api/me/buses/anon/match`(bus 建车侧 · 不进决策器)
- `bus.Strategy` 里的 `AnonZone` / `AnonMaxUnitPrice` 字段(只影响撮合 · 不进定价栈 · 见 §4.1)

### 12.3 三类车调度差异 · 反查表

| 差异点 | `single` | `anon` | `team` |
|---|---|---|---|
| **coalescer 集单** | 同 bus 内多成员补车意图合流(1c-2 落地) | 同上 | 同上 |
| **auto 补车判据** | `bus.AutoRefillEnabled` 车级值 | 同上 | 同上 |
| **拉号价格** | `vendorview.PricedFor(passenger.tier)` | 同上 | 同上 |
| **`bus.Scheduler` 5min 兜底** | 遍历所有 `AutoRefillEnabled=true` 的车 · 不区分 kind | 同上 | 同上 |
| **deathwatch 补车** | `pending_refill.bus_id` 触发 death_refill · 走 §5.2 | 同上 | 同上 |
| **加人方式** | 拼车码(`bus.invite_code`) → `/join-by-invite` | 撮合(`/anon/match`) | 邀请码(`bus.invite_code`) → `/join-by-invite` |

**这张表看下来 · 除了"加人方式"其他全一样** —— 这就是"统一调度模型"的含义:kind 只在建车 / 加人时区分 · 建完之后拉号调度不分家。

### 12.4 违反检查(改代码时必看)

- [ ] `decider` / `stockwatch` / `deathwatch` / `bus.Scheduler` 里出现 `if kind == "anon"` / `if kind == "team"` 分支?**违反** —— 调度不该按 kind 分叉 · 有差异应该走**策略字段**表达(如 `AnonZone` 单开字段就是这个思路)
- [ ] `strategy.Effective()` 依赖 `bus.kind`?**违反** —— Effective 只读策略字段 · 不看 kind
- [ ] `bus.Scheduler` 5min 兜底扫车时 `WHERE kind = 'single'` 或类似?**违反** —— 遍历判据是 `AutoRefillEnabled = true` · 跨 kind

**允许**:
- 建车 API(`handleCreateBus` / `handleMatchAnonBus`)按 kind 分建车路径(必然)
- 加人 API(`handleJoinBus` / `handleJoinByInvite`)按 kind 校验(anon 车拒绝邀请码 join)
- UI 按 `member_count` 展示"独享 / N 人拼车 / 搭车"(不暴露 kind 名字)

### 12.5 当前 vs 目标口径

| 能力 | API | DB | state | test | 备注 |
|---|---|---|---|---|---|
| `single` 建车 + 拉号 + 调度 | ✅ | ✅ | ✅ | ✅ | `handleCreateBus` / `handleBusPull` / `bus.Scheduler` |
| `team` 邀请码建车 + join | ✅ | ✅ | ✅ | ✅ | `handleJoinByInvite` |
| `anon` 撮合(单车+多车) | ✅ | ✅ | ✅ | ✅ | `handleMatchAnonBus` · 1c-1 落地 |
| `coalescer` 同 bus 集单窗口 | ⏸ | ⏸ | ⏸ | ⏸ | 目标口径:1c-2 才做 · 当前 `ErrNotImplemented` |
| 统一调度模型(§12.2 A-D 四步) | ✅ | ✅ | ✅ | ✅ | 三 kind 走同一 `Effective + Decide + Pull` 路径 |

---

## 13 · 六触发源边界表(1f-D)

**目的**:一次拉号可能被六种事件触发 · 每种事件的**输入上下文 / busID 来源 / requestOverride / 可能输出 / 钱何时扣 / 失败态 / janitor 兜底**必须清晰。别在 §5.2 决策流之外造第七种触发路径。

### 13.1 六触发源边界总表

| 触发源 | 输入上下文 | busID 来源 | requestOverride | 可能输出 | 钱何时扣 | 失败后 pending 状态 | janitor 兜底 |
|---|---|---|---|---|---|---|---|
| **manual** | HTTP `POST /api/me/buses/{id}/pull` payload | 路径参数 `{id}`(必有 · 无则 record 路径) | 有(`count` / `vendor` / `zone` / `max_unit_price`) | Pull(直下单 · 可能内部 ErrNoStock → 内部挂 stockwatch) / Reject(auto 检查跳过 · 但 KILL_PULLS / 日限额 / 并发满仍能拒) | `wallet.Reserve` 冻结 → `Purchase` 成功后转消费 | `pending_purchase` → `reserved` / `purchasing` / `purchased` / `imported`(见 `09-transactions §2`) | janitor.Tick 1min 扫 · 分状态处理(§2.1) |
| **webhook** | vendor push · `vendor_id` / `zone` / `stock` / `available` | 无 · 由 `webhookAutoScanBridge` 按 vendor/zone/低水位扫候选 bus 集合 · **逐车**调 Effective + Decide | 无 | Pull(Balance/Tight 下单) / Enqueue(Tight 下单必 ErrNoStock 时挂) / Reject(auto off / Cool 不响应) / Noop(候选集为空) | 同 manual · 决策器出 Pull 才走 Reserve | 同 manual | 同 manual |
| **probe** | 我方 60s `GET /stock` 采样对比 | 无 · 由 probe 桥扫候选 bus 集合 · 逐车决策 | 无 | Pull(Tight 时关键补位) / Reject(Cool/Balance 不响应) / Noop | 同 manual | 同 manual | 同 manual |
| **deathwatch(RefillTick)** | `pending_refill` 队列一行 | `pending_refill.bus_id`(NULL = record 单独拉 · 走无 busID 分支) | 无 | Pull(mode 允许) / Enqueue(Tight) / Reject(auto off) | 同 manual · 但**退款独立走**(见 §14.3) | 同 manual | 同 manual |
| **scheduler(bus.Scheduler 5min)** | 遍历所有 `AutoRefillEnabled = true` 的 bus | 遍历时每辆车自带 | 无 | Pull(Cool/Balance) / Enqueue(Tight) / Reject(水位未到 · 备胎撑着 · auto off) | 同 manual | 同 manual | 同 manual |
| **coalescer(集单窗口 · 1c-2 目标)** | 同 bus 内多个 pull_intent 在时间窗内合流 | 意图窗口必属某 busID(coalescer 不跨车) | 有(合流后的最大公约 count / 一致 vendor) | Pull(合流后一次下单) / Reject(合流失败落回单独执行) | 同 manual · 合流后一次 Reserve | 同 manual | 同 manual |

### 13.2 六触发源共同不变量

1. **输出 Pull 时 · 一律调 `decider.Pull(ctx, count, vendor, maxPrice)`** —— 没有第二个拉号出口
2. **输出 Enqueue 时 · 一律调 `stockwatch.Enqueue(EnqueueParams)`** —— `reserved_amount=0`(当前不预冻结 · §6)
3. **调 `Effective()` 前 · 必须解析出 busID(或明确无 busID · 走 record 分支)** —— 见 §4.3.3
4. **钱只在 `decider.Pull` 内部走 wallet.Reserve** —— 决策器只判"能不能拉" · 不动钱
5. **Reject 不写 pending_purchase** · Enqueue 只写 `stock_watcher`(不占钱) · Pull 才落 `pending_purchase`

### 13.3 六触发源 · 输入 payload 到 Effective 的解析路径

| 触发源 | 原始 payload 字段 | 解析到 Effective 的输入 |
|---|---|---|
| manual | `{bus_id, count, vendor, zone, max_unit_price}` | `passengerID` 从 session · `busID` 从路径 · `requestOverride = {count, vendor, zone, max_unit_price}` |
| webhook | `{vendor_id, zone, stock, available_at}` | 逐候选 bus 循环:`passengerID = bus.passenger_id` · `busID = bus.id` · `requestOverride = nil` |
| probe | `{vendor_id, zone, new_stock, prev_stock}` | 同 webhook |
| deathwatch | `{dead_credential_id, bus_id}` | `passengerID = bus.passenger_id` · `busID = pending_refill.bus_id` · `requestOverride = nil` |
| scheduler | `bus 行` | `passengerID = bus.passenger_id` · `busID = bus.id` · `requestOverride = nil` |
| coalescer | `{bus_id, intent_ids[], merged_count, vendor}` | `passengerID = bus.passenger_id` · `busID = bus.id` · `requestOverride = {count: merged_count, vendor}` |

### 13.4 当前 vs 目标口径

| 触发源 | API | DB | state | test | 备注 |
|---|---|---|---|---|---|
| manual | ✅ | ✅ | ✅ | ✅ | `handleBusPull` / `handlePullRecord` |
| webhook | ✅ | ✅ | ✅ | ✅ | `webhookAutoScanBridge`(在 `cmd/bus-pooling/main.go` · 非独立文件) |
| probe | ✅ | ✅ | ✅ | ✅ | probe 60s poll · 触发链跟 webhook 共用 bridge |
| deathwatch(RefillTick) | ✅ | ✅ | ✅ | ✅ | `internal/deathwatch/refill.go` |
| scheduler(bus.Scheduler) | ✅ | ✅ | ✅ | ✅ | `internal/bus/autorefill.go`(**非** `scheduler.go`) |
| coalescer(集单窗口) | ⏸ | ⏸ | ⏸ | ⏸ | 目标口径:1c-2 · 当前 `ErrNotImplemented` |

---

## 14 · 状态机 + 时序(1f-D)

**核心命题**:一次拉号请求穿过 stockwatch(可能) → decider → wallet → vendor → housepool → credential_ledger 六个系统 · 每步都有可能失败 · **状态机必须能崩溃恢复**。本节只做**跨模块串联**;每个模块内部的详细状态字段 / 崩溃恢复策略在 `docs/09-transactions.md` 里逐节写清。

### 14.1 完整时序 · Enqueue(不预冻结) → Fire → Pull(完整事务)

```
[触发源(manual/webhook/probe/deathwatch/scheduler/coalescer)]
    │
    ▼
[decider.Decide] ← 六步串行(§5.2)
    │
    ├─── 拒 ────────────────────────────────────────────► [结束 · 无副作用]
    │
    ├─── 挂单 ─► [stockwatch.Enqueue] ── status=watching · reserved_amount=0(不冻钱)
    │              │
    │              ▼
    │           [等 vendor 上货 / 或 stockwatch.Sweep 30s 扫过期]
    │              │
    │              ├─── 到货 · webhook/probe 触发 fire ─► [stockwatch.Notify(watching→fulfilled)]
    │              │                                             │
    │              │                                             ▼
    │              │                                        [走同一 decider.Pull 分支] (下面 ▼)
    │              │
    │              └─── TTL 到 · Sweep 标 expired ─► [结束 · 未拉号 · 无副作用]
    │
    ▼
[下单 · decider.Pull]  ← 完整事务(下面详展)
    │
    ├─ tx1: wallet.Reserve + pending_purchase(initial) 一次原子 commit ── 崩溃留痕给 janitor
    │
    ├─ pending_purchase → reserved(冻结成功 · 未调 vendor)
    │
    ├─ pending_purchase → purchasing(**发 vendor 请求前一刻落此** · P0-1 修补 · `09-transactions §2.1`)
    │
    ├─ vendor.Purchase(client_order_id) ────► [vendor 侧扣款出号]
    │
    ├─ pending_purchase → purchased(vendor 返成功 · 未入 housepool)
    │
    ├─ housepool.BatchImport(refresh_tokens, group="bus-<bus_id>") ─► [号进车 group]
    │
    ├─ pending_purchase → imported(号入池 · 未结账)
    │
    ├─ tx2: wallet 冻结→消费 + wallet_ledger 落账 + pull_round + credential_ledger 一次原子 commit
    │
    ▼
[pending_purchase → completed] · 号可用
```

**崩溃恢复**:
- `initial` / `reserved` / `purchasing` / `purchased` / `imported` 每一态都有 janitor 兜底(见 `09-transactions §2` 状态表)
- **`purchasing` 是黄金窗口** —— vendor 可能已扣款 · 必须靠幂等键重放而非直接释放冻结(见 `09-transactions §2.1`)

### 14.2 no_stock → pending → fulfilled / need_manual

**stockwatch 状态机**(`stock_watcher` 表 · 见 `docs/06-db-schema.md`):

```
[decider.Pull 内部 vendor.Purchase 返 ErrNoStock]
    ↓
    (decider.Pull 走 maybeEnqueueOnNoStock 分支)
    ↓
[stockwatch.Enqueue] · INSERT stock_watcher 行 · status=watching
    ↓
    ├─── vendor 新号事件(webhook/probe)触发 fire ────►[stockwatch.Notify]
    │        ↓
    │        UPDATE stock_watcher SET status='fulfilled' WHERE id=? AND status='watching'
    │        ↓ (条件 UPDATE · 保证只一次触发)
    │        [调 Firer.Fire → decider.Pull 走完整事务]
    │        ↓
    │        ├─ Pull 成功 → 号入车 · 结束
    │        └─ Pull 失败(仍 ErrNoStock / 网络抖) → conditional UPDATE 回 watching(允许下次 fire)
    │
    ├─── TTL 10min 到期 · Sweep 30s 扫 ────►[status=expired]
    │        (**当前未做退款**:reserved_amount=0 本来就没冻钱 · 无需退)
    │
    └─── 硬错(vendor 明确拒绝 / 参数非法) ────►[status=expired] + 日志报警
```

**当前 vs 目标口径**:

| 能力 | API | DB | state | test | 备注 |
|---|---|---|---|---|---|
| Enqueue(不预冻结) | ✅ | ✅ | ✅ | ✅ | `stockwatch.Enqueue` · `reserved_amount=0` |
| watching → fulfilled(fire 成功) | ✅ | ✅ | ✅ | ✅ | `stockwatch.Notify` · 条件 UPDATE |
| fulfilled → watching(fire 失败回滚) | ✅ | ✅ | ✅ | ✅ | `wiring_test.go` 守 |
| watching → expired(TTL) | ✅ | ✅ | ✅ | ✅ | `stockwatch.Sweep` 30s |
| 付费优先排队(前称 `PrebuyEnabled`) | ⏸ | ⏸ | ⏸ | ⏸ | 目标口径:未落 · 若要做必须补字段 + 冻结/扣费/退款 + 排序 SQL + 前端解释 + 测试 · 见 `decisions §11.15 / §12` |
| `need_manual`(硬错兜底) | ✅ | ✅ | ✅ | ⏸ | 当前直接标 expired + 日志 · 无专门的 `need_manual` 状态(未来若做付费优先才需要) |

### 14.3 三去向 · 钱与号归属

拉号成功后 · 号的去向决定"钱归属谁 / 号在哪 / 后续监控归属":

| 去向 | 号落位 | 钱归属 | credential_ledger 落 | 后续监控 |
|---|---|---|---|---|
| **into_bus**(进车) | housepool `bus-<bus_id>` group | 乘客钱包被扣消费 · 我方计入营收 | `status=alive` · `current_group=bus-<bus_id>` | housepool 探活 · 死了走 §14.4 refund |
| **push_pool**(推 passengerpool · 双写) | housepool `bus-<bus_id>` **保留** + passengerpool 也有一份 | 同 into_bus(乘客付了) | 同 into_bus | housepool 副本继续监控 · 死了走 §14.4 refund(passengerpool 侧我方不管) |
| **handoff**(拿走) | 从 housepool DELETE · 明文一次性给乘客 | 同 into_bus | `status=handed_off` · 台账行保留(**永不删** · `decisions §8.24` 售后追溯) | **不监控**(唯一 fire-and-forget 路径) |

**pending_assignment 状态机**(`docs/09-transactions §3`)负责 into_bus / push_pool 的原子性 · `pending_handoff` 状态机(`§4` 三段式 Token 交付)负责 handoff。

**关键**:钱一律在 `decider.Pull` 里就扣完了(tx2 结账) · 三种去向**不影响钱的归属** · 只影响号的物理存放和后续监控范围。

### 14.4 refund / warranty_refund 反向路径

号死了或质保退款时 · 反向走一遍:

```
[号死信号] (vendor webhook: credential_dead / 我方 deathwatch 探活标死)
    ↓
[credential_ledger.status = 'dead'] · 落 `death_source ∈ {housepool_probe, vendor_webhook, vendor_poll}`(内部记录 · 不对外)
    ↓
[deathwatch.RefundTick 1min 扫]
    ↓
    (查 vendor 质保政策 · 是否在保内)
    ↓
    ├─── 在保 ─► vendor.RefundOrder(order_id)
    │       ↓
    │       ├─ vendor 返成功 → [wallet_ledger.reason='warranty_refund' · 退乘客积分]
    │       │                    ↓
    │       │                    [pull_round.status='refunded'](见 `09-transactions §2`)
    │       │                    ↓
    │       │                    [触发 death_refill 进决策器 §5.2] · 若 auto on 就自动补
    │       │
    │       └─ vendor 拒绝退款 → 保留死状态 · 不退钱 · 记 vendor 拒绝原因
    │
    └─── 超保 ─► [不退款] · 只标死 · 等 death_refill 决策器判是否补(受用户 auto 开关约束)
```

**关键**:
- **退款是天赋权利** —— vendor 政策允许就退 · 跟用户 auto 开关**无关**(不能因为用户关了 auto 就不退)
- **是否补车** —— 由 death_refill 走 §5.2 六步决策器 · **受 auto 开关约束**(用户关了 auto · 死了就死了 · 不补)
- **两条链解耦** —— refund 走 wallet + deathwatch · refill 走 decider · 一辆车可能 refund 但不 refill

**当前 vs 目标口径**:

| 能力 | API | DB | state | test | 备注 |
|---|---|---|---|---|---|
| 号死标记(webhook + probe) | ✅ | ✅ | ✅ | ✅ | `credential_ledger.status='dead'` · `death_source` |
| 质保退款(RefundTick) | ✅ | ✅ | ✅ | ✅ | `internal/deathwatch/refund.go` |
| refund → refill 触发(不跳 auto 检查) | ✅ | ✅ | ✅ | ✅ | `pending_refill` 表 + `RefillTick` 走 §5.2 |
| pull_round.refunded 状态 | ✅ | ✅ | ✅ | ✅ | `09-transactions §2` |

### 14.5 交叉引用 · 不冲突

本节写"跨模块串联的时序" · 详细字段 / 崩溃恢复分类 / 幂等键要求在 `docs/09-transactions.md`:

- **`§2 拉号状态机 · pending_purchase`** ← §14.1 引它(六态 + P0-1 purchasing 修补)
- **`§3 派去向状态机 · pending_assignment`** ← §14.3 引它(into_bus / push_pool 原子性)
- **`§4 handoff 状态机 · pending_handoff`** ← §14.3 引它(三段式 Token 交付)
- **`§6 充值状态机 · pending_topup`** ← 本文不涉及(充值不是调度)
- **`§8 wallet 并发控制`** ← §14.1 tx1/tx2 引它(`BEGIN IMMEDIATE`)
- **`§9 janitor 恢复任务`** ← §14.1 janitor 兜底引它

**双向引用**:`09-transactions.md` 顶部导航若要指向"这些状态机怎么被触发" · 引本文 §13 六触发源边界表。**互相引用不冲突** —— 状态机本身归 09 · 触发时序 + 决策入口归本文。

