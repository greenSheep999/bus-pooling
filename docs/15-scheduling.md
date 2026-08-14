# 15 · 调度系统设计（完整版）

> **本文管什么**:拼车产品的调度系统怎么运作 —— 用户能配什么、系统何时替用户拉号、上游有货没货怎么应对、多个 vendor 之间怎么切换。**这份是完整系统设计**·不是历史修补记录。
>
> **不管**:状态机步骤(去 `09-transactions`)· 加价栈算法(去 `10-pricing`)· 具体 vendor API 字段(去 `11-fields`)。
>
> **贯穿模块**:`strategy` / `bus` / `coalescer` / `decider` / `stockwatch` / `deathwatch` / `vendorbalance` / `webhookin` / `pricing`。
>
> **字段口径**:本文正式字段必须跟当前 API / schema / Go struct 对齐：`AutoRefillEnabled` / `RefillWatermark` / `RefillMinCount` / `PerRoundCount` / `MaxUnitPrice` / `PreferredVendor`。未落库的产品设想（例如 `PrebuyEnabled` / 付费抢号优先级）只能写进 `decisions.md`，不能写成本设计的当前字段。

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
- **车级策略**:`bus` 表 · 每车一份 · **两种状态**:
  - **跟随全局**:字段 NULL(方案 A) 或 inherit=true(方案 B) · 运行时读全局当前值 · 全局变化会影响该车
  - **覆盖本车**:字段有值(方案 A) 或 inherit=false(方案 B) · 运行时读本车值 · 全局变化不影响该字段
  - 建车时若走"覆盖本车"路径 · 抄一份全局值作为 seed 后独立演化;走"跟随全局"路径 · 不 seed · 运行时始终读全局
- **全局默认**:`passenger_strategy_default` 表 · 每乘客一份 · **三种用途**:
  1. **硬上限**(`MaxUnitPrice / DailyRoundLimit / DailySpendLimit`):运行时**始终参与** · 跨所有车生效
  2. **新车默认 seed**:建车向导预填 UI · 用户显式选"覆盖本车"时把这份值落到车表
  3. **运行时 inherit fallback**(1f-B 引入后):字段处于"跟随全局"态时 · 运行时读全局当前值
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

**覆盖的"是否有值"由字段继承语义决定** · **不是**用"非空"泛化所有字段:

| 继承方案 | "跟随上层" | "覆盖本车" |
|---|---|---|
| **方案 A · nullable** | 字段 = NULL | 字段非 NULL(**包括 0 / false**) |
| **方案 B · inherit flag** | `xxx_inherit = true` | `xxx_inherit = false`(值字段允许 0 / false) |
| **request override** | 请求 payload 里字段**未出现** | 请求 payload 里字段**出现且合法**(**包括 0 / false**) |

**关键**:对 `bool` / `int` 字段 · `0` 和 `false` 是**合法覆盖值** · 不是"空"。**别用"非空"判"是否覆盖"** —— 判断依据是"是否存在 / 是否显式声明" · 见上表。

**⚠️ `AutoRefillEnabled` / `RefillWatermark` / `RefillMinCount` · 现状 vs 目标口径**:

**当前现状**(001_init.sql / 011_bus_anon_match.sql · sprint-1e 落地):
- `passenger_strategy_default` **没有** `default_auto_refill_enabled` / `default_refill_watermark` / `default_refill_min_count` 三个字段
- `bus.auto_refill_enabled` / `bus.refill_watermark` **NOT NULL DEFAULT 0** · 无法表达"NULL = 跟随全局"
- 因此**当前 auto/refill 三字段仅存在于车级** · 无全局 fallback · 无"跟随全局"语义

**1f-B 目标口径**(等 sprint-1f-B 落 DB migration 后生效):
- 新增全局字段 `default_auto_refill_enabled bool NOT NULL DEFAULT 0` / `default_refill_watermark int NOT NULL DEFAULT 0` / `default_refill_min_count int NULL`
- 只作为**新车默认值 seed** + **车级"跟随全局"时的 fallback**
- 继承语义(NULL = 跟随 vs 显式覆盖为 0)见 §4.3.2b

**过渡期约束**:1f-B DB migration 未落之前 · `Effective()` 对 auto/refill 三字段**只读车级**·`grep` 验收(§4.3.4)不检查这三字段的 fallback 链。

#### 4.3.2b 继承语义 · "跟随全局 vs 覆盖本车" 如何表达(sprint-1f-B 决策)

**问题**:`bus.auto_refill_enabled` / `bus.refill_watermark` 现在是 `NOT NULL DEFAULT 0` · 用户改成 0 时**无法区分**"跟随全局(全局若为 0 就 0 · 若为 1 就 1)" vs "显式覆盖为 0(不管全局多少都是 0)"。

**两种落库方案 · 1f-B 二选一**:

**方案 A · nullable 字段表达继承**(推荐 · SQL 简单 · 无冗余):
```sql
-- migration: 改 bus 表 auto/refill 三字段为可空
ALTER TABLE bus …  auto_refill_enabled INTEGER NULL
                   refill_watermark    INTEGER NULL
                   refill_min_count    INTEGER NULL  -- 本来就可空 · 保留
```
- `NULL` = 跟随全局默认
- 非 `NULL` = 覆盖本车(包括显式 0 / false)
- `RefillMinCount` 本来就 NULL · 但要**跟"按 gap 补齐"的 nil 语义区分** —— 见 §4.3.2c

**方案 B · 显式 inherit flag**(冗余但语义显式):
```sql
-- 保留现有 NOT NULL 值字段 · 新增 inherit 标记
ALTER TABLE bus ADD COLUMN auto_refill_inherit INTEGER NOT NULL DEFAULT 1
ALTER TABLE bus ADD COLUMN refill_watermark_inherit INTEGER NOT NULL DEFAULT 1
ALTER TABLE bus ADD COLUMN refill_min_count_inherit INTEGER NOT NULL DEFAULT 1
```
- `inherit=1` → 读全局
- `inherit=0` → 读本车值(值字段仍 NOT NULL DEFAULT 0)
- 场景:如果"覆盖为 0"是有意义的动作(比如车级明确关闭自动补)·inherit flag 更清晰

**1f-B 决策要求**:落 migration 前先在 `docs/decisions.md §13.4` 补一条方案选择 · 拍板后再动 DB。

**⚠️ 迁移保行为铁律**(无论选方案 A 还是 B · 硬约束):

**核心**:1f-B migration **不允许**让历史车因为全局默认变化而突然开始补车。

**方案 A(nullable)** 落库规则:
- 现有 `bus` 行的 `auto_refill_enabled / refill_watermark / refill_min_count` 值 **一律保留为"显式覆盖本车"** · 不能一律转 NULL
  - 用户建车时明确关了 auto(值为 0)· migration 后仍是 0(覆盖) · 全局 auto=1 也不影响
  - 用户建车时明确开了 auto(值为 1)· migration 后仍是 1(覆盖) · 全局 auto=0 也不影响
- 只有**新建车走"跟随全局"路径** · 或**存量车用户主动在 UI 选"跟随全局"** · 字段才置 NULL
- SQL 层面 · `ALTER TABLE bus ALTER COLUMN auto_refill_enabled DROP NOT NULL` · 但**不 UPDATE 现有值**

**方案 B(inherit flag)** 落库规则:
- 现有 `bus` 行 `auto_refill_inherit / refill_watermark_inherit / refill_min_count_inherit` 一律 **DEFAULT 0(覆盖本车)** · 保留当前值字段的行为
- 新建车是否默认 `inherit=1` · 由 1f-B 产品决策单独写入 `decisions.md §13.4`(默认跟随 vs 默认覆盖需拍板)

**测试要求**(1f-B migration 落码时必带):
- fixture:建一辆老 auto=1 车 · 一辆老 auto=0 车 · 各一辆 · 分别验 migration 后 `Effective()` 返值等于 migration 前
- fixture:migration 后 global default_auto_refill_enabled 从 0 改成 1 · 老车 `Effective()` 结果**不变**
- 只有 UI 显式改为"跟随全局"后 · 老车行为才随全局变化

#### 4.3.2c RefillMinCount 三态语义

**当前 `bus.refill_min_count` `*int`** · 但有**三种语义**混在同一个字段:
1. **NULL** = 跟随全局默认(§4.3.2b 方案 A 语义)
2. **NULL** = 按 `RefillWatermark - alive_total` gap 补差额(§4.1 原语义 · Step 3 已在用)
3. **非 NULL** = 覆盖本车(每轮至少拉 N 个)

**1 和 2 冲突** —— 同一个 NULL 值前一个说"跟随全局" · 后一个说"按 gap 补"。

**1f-B 拍板要求**:方案 A 下必须选一:
- **选项 X**:全局 fallback 优先 · NULL 走全局 · 若全局也 NULL 才走 gap
- **选项 Y**:去掉"跟随全局"语义 · `refill_min_count` 只有"NULL = gap · 非 NULL = 显式"两态(auto_refill_enabled / refill_watermark 走继承 · min_count 不走)

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

##### 4.3.5.3 前端字段契约(TS 类型 · 1f-B 后端按此对齐)

**推荐**:方案 A(nullable)· TS 侧 `null` = 跟随全局 · 非 `null` = 覆盖:

```typescript
interface BusStrategy {
  // 硬上限 · 车级值 · 无二态 · null = 车级不加严 · min(车级, 全局) 由后端算
  max_unit_price: Money | null;

  // 覆盖字段 · 当前已成立 · null = 跟随全局
  per_round_count: number | null;
  preferred_vendor: string | null;
  zone: "us" | "eu" | "auto" | null;

  // 覆盖字段 · 1f-B 目标 · null = 跟随全局(1f-B DB migration 落后生效)
  auto_refill_enabled: boolean | null;
  refill_watermark: number | null;
  refill_min_count: number | null;
}
```

**关键**:前端 TS 类型全部改成 `| null` · 未来 1f-B DB migration 落 nullable 时前端不用改类型 · 只需要:
1. 建车 / 存量车读取时 · null 走"跟随全局"分支
2. 保存时 · toggle "跟随全局" 就发 `null` · toggle "覆盖本车" 就发用户填的值(允许 `0` / `false`)

##### 4.3.5.4 全局页(Preferences.tsx)契约

**Preferences 展示所有全局默认字段** · 无 toggle(因为它是"全局默认" · 本身就是最上层的可覆盖源) · 但要:

1. 硬上限字段旁标"**跨所有车累加 · 每车都受此约束**"
2. 覆盖字段旁标"**新车默认值** + 车级选'跟随全局'时的运行时值"(1f-B 后)
3. **今日已用/上限** 进度条(`used_today.rounds / rounds_limit` · 已存在)

##### 4.3.5.5 当前 vs 1f-B 目标 · 前端落地节奏

**当前可以先做的**(不依赖后端 migration):
- ✅ EditStrategyPanel 现有字段的 UI(已完成 · sprint-1e 之前)
- ⏸ **给现有可覆盖字段加 toggle UI**(`per_round_count / preferred_vendor / zone`) · 后端已支持 nullable · 前端 toggle 立刻能落
- ⏸ Preferences 页加"新车默认值"标注

**依赖 1f-B DB migration 才能做的**:
- ⏸ `auto_refill_enabled / refill_watermark / refill_min_count` 的 toggle UI(等 migration 把这三字段改 nullable 后)
- ⏸ 全局 Preferences 加这三字段的输入(依赖 1f-B 新加全局默认字段)

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
