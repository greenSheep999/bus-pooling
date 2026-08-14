# 15 · 调度契约（vendor → 系统 → 用户 三层字段清单 + 触发矩阵）

> **本文管什么**：**调度决策依据什么** —— 每个开关 / 阈值 / 字段是谁的、谁读它、什么时候读、读来干什么。是 `01-architecture` / `02-flows` / `03-modules` 的**落地细化**。
>
> **不管**：状态机步骤（去 `09-transactions`）· 加价栈算法（去 `10-pricing`）· 具体 vendor 字段（去 `11-fields`）· 讨论 / 待决策（去 `decisions.md §12`）。
>
> **为什么必须有本文**：08-14 到 08-15 因缺这份契约 · 我把三层字段散写成四份文档（21/22/23/24）+ 一套私造版本号 · 全部作废重整。本文是**统一入口** —— 未来所有关于"这个字段被谁读"的讨论都锁在这一份 · 不再新开文档。
>
> **贯穿模块**：`strategy` / `bus` / `decider` / `stockwatch` / `deathwatch` / `coalescer` / `vendorbalance` / `webhookin`。**不属于单一 sprint** —— 字段本身随阶段增长（1a 建 bus.Strategy · 1b 加 capability · 1c 加 anon 字段 · 1d 加 auto 触发路径）。

---

## 1 · 三层是什么

```
┌───────────────────────────────────────────────────────────────┐
│ ① Vendor 层 · 外部事件源 + 我方在 vendor 侧的钱               │
│   我方观测但不能改 · 我方只能"接住"                            │
└───────────────────────────────────────────────────────────────┘
                          ↓ 事件流入
┌───────────────────────────────────────────────────────────────┐
│ ② 系统运营层 · 我方策略 · admin 或代码写死                     │
│   决定"什么时候用户策略生效 / 系统 mode / 加价栈率"            │
└───────────────────────────────────────────────────────────────┘
                          ↓ 系统按用户配置执行
┌───────────────────────────────────────────────────────────────┐
│ ③ 用户策略层 · passenger 全局默认 + bus 每车覆盖              │
│   决定"我这辆车怎么补 / 上限多少 / 是否愿意付更多抢"           │
└───────────────────────────────────────────────────────────────┘
                          ↓
                 唯一号入库出口：decider.Pull
```

---

## 2 · 层 ① Vendor 层 · 事件源全清单

**推送（webhook）· 200ms-2s 到达**：

| 事件类型 | 落表 | 直接触发的下游 |
|---|---|---|
| `new_keys_available` | `vendor_dispatch` + `inbound_webhook_event` | `stockwatch.Notify` → 唤醒挂单 |
| `all_keys_dead` | `credential_ledger.status='dead'` | `deathwatch.markDead` → `pending_refill` 入队 |
| `warranty_refund` | `pull_round.status='refunded'` + `wallet_ledger` 入账 | 无（终结事件）|
| `key_revoked_abuse` | `credential_ledger` 强制标死 | deathwatch sweep（不退款）|
| `reserved_keys_delivered` | 只 log | 无（该 vendor 独家 · 我方不认领）|

**轮询**：

| 数据源 | 周期 | 落表 | 谁读它 |
|---|---|---|---|
| vendor `GET /stock` 探针 | 60s | `vendor_probe` + `vendor_probe_zone` | AutoPick 定价 · 缺货判定 |
| xi8 signals 增量 | 30s | `vendor_probe_zone` source='xi8' | 补 vendor 单端只给一区的空缺 |
| xi8 全量 restock-log | 5min | `vendor_probe_zone` source='xi8_notif' | 探针空窗历史价补 |
| vendor `Balance()` | 5min | `vendorbalance.Cache`（内存）| decider 拉号前预检 · 不够切下一家 |
| webhook 载荷 price/available | 事件驱动 | `vendor_probe_zone` source='webhook' | 前端 price-trend 图 + 定价 fallback |

**代码位置**：`internal/webhookin/dispatcher.go` · `internal/vendorview/prober.go` · `internal/xi8/backfiller.go` · `internal/vendorbalance/cache.go`

---

## 3 · 层 ② 系统运营层 · 全策略清单

### 3.1 抢号链 mode（`internal/stockwatch/mode.go`）

**每 30s 自采样**：

```
demand = pending + in_flight 的 pull_intent 总数
supply = 6 家 vendor 过去 5min stock 均值累加
ratio  = demand / max(1, supply)

ratio > 2       → ModeTight    紧张 · Prober + webhook 都 fire
0.3 < ratio ≤ 2 → ModeBalance  均衡 · 只 webhook fire · Prober 只观测
ratio ≤ 0.3     → ModeCool     冷 · 都不 fire · 用户来了现打 vendor
```

**source × mode fire 决策表**（`Watcher.sourceShouldFire`）：

| source | tight | balance | cool | TURBO_ON |
|---|---|---|---|---|
| `webhook`（vendor 主动 push 200ms-2s） | ✅ | ✅ | ❌ | ✅ |
| `stock_delta`（我方探针 60s 采样对比推算） | ✅ | ❌ | ❌ | ✅ |
| `manual`（CLI 手工调试） | ✅ | ✅ | ✅ | ✅ |

**抢号信号源日常只有两个：`webhook` + `stock_delta`**。`manual` 是运营调试用 · 不算日常路径。

**为什么探针比 webhook 更关键**（别只等 push）：号少时 · vendor 上新库存 → webhook 需要 200ms-2s 才到我方 · 而**其他家平台 / 手速快用户也在盯着** · 单靠 webhook = 跟他们赛跑；且部分 vendor webhook 会漏/延（生产实测过某家一天丢 21 条）。我方 60s 探针**主动去问 vendor** —— stock 比上一轮多了立即推 `stock_delta`·**不依赖 vendor 主动 push**·常常比 webhook 到得早。所以 ModeTight 时 `stock_delta` 必须 fire · 这是 webhook 慢/漏时的**关键补位**。

**xi8 不是抢号信号源** —— xi8 backfiller 只写 `vendor_probe_zone` source='xi8' 和 `vendor_dispatch` source='xi8'（数据补齐 · 对账用）· **从不调 `stockwatch.Notify`**。`stockwatch/store.go` 代码里保留 `xi8_signal` 字符串常量是历史设计残留 · 从未使用。

### 3.2 文件哨兵（`internal/stockwatch/killswitch.go` · 5s poll）

| 文件 | 存在 = | 释放 |
|---|---|---|
| `TURBO_ON` | 强制抢 · 无视 mode | `rm` |
| `KILL_PULLS` | 一切 Purchase 停 | `rm` |

### 3.3 加价栈率（`surcharge_rule` 表 · admin 后台配 · 不写死）

| kind | 语义 | 生效条件 |
|---|---|---|
| `vendor` | vendor 层附加率 | 所有拉号 |
| `zone` | 区域分项 | 所有拉号 |
| `retail` | 零售分项 | 未 invited 用户 |
| `capability` | 附加能力槽 | 用户开对应能力（如 prebuy）|
| `adhoc` | 临时分项 | 运营手工加 |
| `service` | 服务费 | 所有拉号 · 最外层 |
| `single_pull` | 单次分项 | `count==1` 时 |

**减免逻辑**（详见 `10-pricing §2.1`）：
- retail 档全套 · community 档跳 zone · wholesale 档跳 vendor + zone

### 3.4 vendor 管控

- **启停开关** · `Registry.SetEnabled(id, bool)` · admin API · 生效即时
- **vendor 内建限**（每家静态配 · `providers.Vendor.WarrantyMinutes` / `MinPerOrder` / `MaxPerOrder`）
- **vendorbalance.Cache** · 5min poll · 拉号前预检 · 不够 → `PickBestVendorExcluding` 切下一家

### 3.5 拉号数量硬约束（`config.pull` · 全局）

| 字段 | 语义 |
|---|---|
| `MinCount / MaxCount` | 单次拉号数量区间 · 超区间直接拒 |
| `DefaultCount` | 客户端没指定时用几 |
| `MaxConcurrentPerVendor` | 同 vendor 在飞上限（0=不限）|
| `MaxConcurrentPerPassenger` | 同乘客在飞上限（防刷）|

### 3.6 后台 goroutine 全清单

| 名字 | 周期 | 干啥 | 读用户策略？ |
|---|---|---|---|
| `prober` | 60s | vendor 探 stock · 落 `vendor_probe_zone` | ❌ 纯观测 |
| `stockwatch.Sweep` | 30s | 扫过期挂单 · 释放冻结 | ❌ |
| `ModeMgr.sample` | 30s | 采样 ratio · 自动切 mode | ❌ |
| `FileFlag.refresh` | 5s | 读 TURBO_ON / KILL_PULLS | ❌ |
| `deathwatch.Sweep` | 5min | 号池探活 · 标死 | ❌ |
| `deathwatch.RefundTick` | 1min | 扫 refund 队列 · 走 vendor 退款 | ❌ |
| `deathwatch.RefillTick` | 1min | 扫 `pending_refill` · 触发 `decider.Pull` | ✅ 读 bus.Strategy |
| `xi8.Backfiller` | 30s+5min | 拉聚合 signals + 全量 | ❌ |
| `vendorbalance.Cache.poll` | 5min | 拉各家 vendor Balance() | ❌ |
| `janitor.Tick` | 1min | 扫 `pending_purchase` 卡态 · 恢复 | ❌ |
| `webhookHealth` | 1min | 长期无 webhook 报警 | ❌ |
| `StalenessChecker` | 5min | 扫 `pipeline_health` · 陈旧管线 ERROR | ❌ |
| `bus.Scheduler` | 5min | 扫 auto_refill 车里还剩几个号 · 触发 refill | ✅ 读 bus.Strategy |

---

## 4 · 层 ③ 用户策略层 · 全字段清单

### 4.1 `bus.Strategy`（每车一份 · `internal/bus/bus.go`）

| 字段 | 类型 | 语义 | 谁读 |
|---|---|---|---|
| `AutoRefillEnabled` | bool | 自动补车总开关 | `bus.Scheduler` · `deathwatch.RefillTick` |
| `RefillWatermark` | int | 补到几个（**语义待定** · 见 `decisions §12` 位置 6）| `bus.Scheduler.ScanOnce` |
| `RefillMinCount` | \*int | 每次补几个 · nil 用差额 | `bus.Scheduler` · `RefillTick` |
| `PerRoundCount` | \*int | 手动拉号默认数 | `handleBusPull` |
| `MaxUnitPrice` | \*int64 | 单价上限 microunit · 车级 ∧ 全局取严 | `strategy.CanPull` |
| `PreferredVendor` | \*string | 首选 vendor · AutoPick 兜底 | `decider.Pull` |
| `DailyRoundLimit` | \*int | **车级不生效 · deprecated** | 无 |
| `DailySpendLimit` | \*int64 | **车级不生效 · deprecated** | 无 |

**anon 车专属**（`bus` 表）：`anon_zone` / `anon_max_unit_price`（撮合上限 · 不参与拉号定价）

### 4.2 `passenger_strategy_default`（全局默认 · 每乘客一份 · `internal/strategy/strategy.go`）

| 字段 | 类型 | 语义 | 谁读 |
|---|---|---|---|
| `MaxUnitPrice` | \*int64 | 硬上限（跟车级 AND 取严）| `strategy.CanPull` |
| `DailyRoundLimit` | \*int | 硬上限（跨所有车累加）| `strategy.CanPull` |
| `DailySpendLimit` | \*int64 | 硬上限（跨所有车累加 · microunit）| `strategy.CanPull` |
| `PerRoundCount` | int | 新车默认值 · 改它不动已有车 | 建车时读 |
| `PreferredVendor` | \*string | 新车默认值 | 建车时读 |
| `DefaultZone` | string | 新车默认值 · `auto/us/eu` | 建车时读 |

### 4.3 参数解析优先级 · 三类字段不同规则

**用户拍板 2026-08-15**：读同一个字段值时按类别走不同解析规则 —— 不是全部"车级降级到全局到系统"。

**类① · 硬上限 · 取最严**（不是降级 · 是 `min()`）：

| 字段 | 规则 | 为什么 |
|---|---|---|
| `MaxUnitPrice` | `min(bus.MaxUnitPrice, passenger.MaxUnitPrice, +∞)` | 全局是"这个人不想超" · 车级是"这辆车不想超" · 任一层触发都要拦。降级会让用户在某辆车设 max=1000 就绕开全局 max=100 · 全局就没用了 |
| `DailyRoundLimit` | 只读全局 · 车级 deprecated | 跨所有车累加 · 车级没意义 |
| `DailySpendLimit` | 只读全局 · 车级 deprecated | 同上 |

**类② · 偏好 · 车级 > 全局 > 系统内建**（真正的降级链）：

| 字段 | 规则 |
|---|---|
| `PerRoundCount` | `bus.PerRoundCount ?? passenger.PerRoundCount ?? config.pull.DefaultCount` |
| `PreferredVendor` | `bus.PreferredVendor ?? passenger.PreferredVendor ?? AutoPick(比价选)` |
| `DefaultZone` | 只在全局层 · 否则代码默认 `auto` |

**类③ · 每车专属 · 只在车级** · 无全局对应：

| 字段 | 说明 |
|---|---|
| `AutoRefillEnabled` | 每辆车自己决定要不要自动补 · 全局默认无意义 |
| `RefillWatermark`（补到几个）| 每辆车目标数量不同 |
| `RefillMinCount`（每次补几个）| 同上 |

**类④ · 系统内建 · 用户碰不着**：

`config.pull.{MinCount, MaxCount, MaxConcurrent*}` · `vendor.{WarrantyMinutes, MaxPerOrder, MinPerOrder}` · `surcharge_rule` 加价率 · `stockwatch.ModeMgr` 阈值 · `TURBO_ON` / `KILL_PULLS` 文件哨兵。

**UI 展示规范**（决策 2026-08-15）：给用户看"**实际生效值**" 而不是"你设的值" —— 让用户一眼看到全局有没有把他压低。例："你设的 max=1000·全局 max=100·**实际生效 100**"。

---

## 5 · 统一决策器（六步串行判据）

**核心洞察**（2026-08-15 用户拍板）：**六种触发源共享同一套判据** —— 写一次·用六处。不是"六个平行场景选一个"·是"决策器有六个维度"。

代码上对应**一个** `func Decide(input) DecideResult` · 六个触发源都调它·不再一个后台 goroutine 各写各的判据。

### 5.1 决策器输入 / 输出

**输入**（一次触发的四个变量）：

```
① 触发源类型   webhook / probe / death / usage / manual / scheduler
② 目标 bus_id  哪辆车
③ 当刻 mode    Cool / Balance / Tight（stockwatch.ModeMgr 30s 自采样）
④ 车里活号快照 按 vendor 分组的 alive 数（credential_ledger 查）
```

**输出**（三种）：

```
【拒·<原因>】    不动 · 附拒因
【下单】         立刻 decider.Pull(count, vendor, maxPrice)
【挂单】         立刻 stockwatch.Enqueue(vendor, count, maxPrice, TTL)
```

### 5.2 六步串行判据（一步失败直接返"拒"）

**Step 1 · 系统闸门**（用户绕不开·第一件事）
- `KILL_PULLS` 文件存在 → 【拒·全停】
- 否则继续

**Step 2 · 用户 auto 开关**（用户没授权就不主动）
- 触发源 == `manual`（用户点手动拉号）→ 跳过 · 直进 Step 5
- 触发源 == `death`（号死质保退款）→ 跳过 · 直进 Step 5（退款是天赋权利·不受 auto 影响）
- 其他触发源：读 `bus.Strategy.AutoRefillEnabled`
  - false → 【拒·auto off】
  - true → 继续

**注**：抢号是自动 / 手动拉号**之外的附加能力** · 不是独立触发源。用户只有两种主动动作 —— 手动拉号（走 `manual` 分支）· 或开 `prebuy_enabled`（订阅式能力 · 之后系统在合适时机替他抢）。**没有"用户主动 POST prebuy"这条 API**。

**Step 3 · 车里活号快照 + 多 vendor 备胎判据**

按 vendor 分组数 `alive`：

- **Case A · `alive_total == 0`**（整车挂）→ 档 = 急 · 直跳 Step 4 · Step 4 会强制 output = 挂 stockwatch（Tight 时）or 下单（Cool/Balance 时）
- **Case B · 任一 vendor 单独 `alive >= RefillMinCount`** → 【拒·有备胎】· 那家撑得住·等它也见底再动（**你的 S6 原话**：vendor01 死 5 · vendor02 活 6 · min=3 → vendor02 撑得住·不拉）
- **Case C · 所有 vendor `alive < RefillMinCount` 且 `alive_total < RefillWatermark`** → 档 = 常规·继续 Step 4
- **Case D · `alive_total >= RefillWatermark`** → 【拒·已达目标】

**Step 4 · 上游 mode × 触发源 → 决定 output 类型**

| 触发源 \ mode | Cool（号多）| Balance（一般）| Tight（号紧俏）|
|---|---|---|---|
| `webhook` | 【拒·cool 不响应】| 【下单】vendor push 说有货·冲上去 | 【下单】除非车里也慢 · 此时改挂 stockwatch |
| `probe`（stock_delta）| 【拒·cool 不响应】| 【拒·balance 只 webhook fire · 省 API】| 【下单】关键补位·webhook 慢/漏时靠它 |
| `death`（号死立补）| 【下单】vendor 有货 · 大概率成 | 【下单】| 【挂单】紧俏时下单大概率 ErrNoStock |
| `usage`（用量见底 · 数据未采集）| 【下单】| 【下单】| 【挂单】|
| `scheduler`（5min 兜底扫）| 【下单】货多下单不亏 | 【下单】| 【挂单】兜底扫在紧俏时改挂不 Pull |
| `manual`（用户手动 / prebuy API）| 【下单】任何 mode 都 fire | 【下单】| 【下单】用户明说要·系统硬上 |

**Case A（整车挂）强制路径**：
- Cool → 【下单】
- Balance / Tight → 【挂单】

**Step 5 · 参数解析**（读用户各字段·按 §4.3 三类规则）

```
count = bus.RefillMinCount ?? (RefillWatermark - alive_total)
  · 必须 config.pull.MinCount ≤ count ≤ config.pull.MaxCount
  · 且 count ≤ vendor.MaxPerOrder(vendor 内建)

maxPrice = min(bus.MaxUnitPrice ?? +∞, passenger.MaxUnitPrice ?? +∞)   · 类①

preferredVendor = bus.PreferredVendor
               ?? passenger.PreferredVendor
               ?? AutoPick(比价选)                                      · 类②

rates = surcharge_rule 查表 · 叠加价栈
  · 用户开 prebuy_enabled → capability 层加率
  · 用户是 wholesale 档 → 跳 vendor + zone 层
```

**Step 6 · 每日限额 + vendor 可行性**（最后一关）

- `passenger.DailyRoundLimit` 累计已用 rounds + 1 > 上限 → 【拒·当日轮次到顶】
- `passenger.DailySpendLimit` 累计已用 + est > 上限 → 【拒·当日花费到顶】
- `vendorbalance.Enough(preferredVendor, est)` 不够 → 排除该 vendor · 走 `PickBestVendorExcluding` 找下一家；都不够 → 【拒·vendor 侧全没钱】
- `MaxConcurrentPerVendor` / `MaxConcurrentPerPassenger` 满 → 【拒·并发到顶】

以上都过 → 按 Step 4 的 output 类型执行：
- 【下单】→ `decider.Pull(count, vendor, maxPrice)`
- 【挂单】→ `stockwatch.Enqueue(vendor, count, maxPrice, TTL)`

**注**：Step 6 的每日限额判定是**乐观的** —— 两辆车同时触发 · 都读到"还有额度" · 都过决策器 · 靠 `wallet.Reserve` 事务 + `MaxConcurrentPerPassenger` 兜底防超限。真正的原子扣款在 `decider.Pull` 内的事务里。

### 5.3 决策器 × 你的场景 · 验证表

| 场景 | Step 1 | Step 2 | Step 3 | Step 4 | 结果 |
|---|---|---|---|---|---|
| S1 死号 · 一家死另一家活 6 · min=3 | 过 | death 跳过 | Case B 备胎撑着 | - | **拒·有备胎** ✓ |
| S1 死号 · 两家都见底 | 过 | death 跳过 | Case C 常规 | 视 mode 决定 | **执行** ✓ |
| S1 死号 · 整车挂 | 过 | death 跳过 | Case A 急 | Tight 挂单 / Cool 下单 | **执行** ✓ |
| S3 vendor 新号 · 用户开 prebuy | 过 | webhook · auto on | Case C | Balance/Tight 下单 · 叠 capability_fee | **执行 + 收能力费** ✓ |
| S3 vendor 新号 · 用户没开 prebuy | 过 | 同上 | Case C | 同上 · 不叠 capability_fee | **执行** ✓ |
| S4 保底 · 剩号少于紧急线 | 过 | probe/scheduler | Case C | Tight 挂 stockwatch | **挂单** ✓ |
| S5 用户没开 auto · 上游有货 | 过 | auto off | - | - | **拒·auto off** · 想拉手动点 ✓ |
| S8 新建拼车 · 空车 · auto 默认关 | 过 | auto off | - | - | **不动** · 第一批手动 ✓ |
| S8 建车后用户开 auto · 空车 | 过 | auto on | Case A 急 | 挂 stockwatch | **挂单等抢** ✓ |

**S8 最后一条重要**：**代码不需要"从没拉过号的车跳过"这种特判** —— 靠 `AutoRefillEnabled` 默认 false 就够（见 `decisions §12.已定 2026-08-15`）。

### 5.4 四象限投影（上游 × 用户设置）

```
                    Cool（号多）        Balance（一般）      Tight（号紧俏）
                    ─────────────       ─────────────       ─────────────
auto=off        →  拒·auto off         拒·auto off         拒·auto off
                    (死号照常退款·手动 pull 照常)
                    ─────────────────────────────────────────────────────
auto=on         →  只 death / sched    webhook + sched      全触发·探针关键
不开 prebuy         触发·下单           触发·下单            webhook/scheduler
                                                             改挂 stockwatch
                    ─────────────────────────────────────────────────────
auto=on         →  同 auto on          webhook 时优先接      同 auto on(tight)
开 prebuy           (cool 时 prebuy     · 叠 capability_fee   + capability_fee 全叠
                    无用武之地)                               · 用户为紧俏付费
```

### 5.5 三层兜底 · 时间粒度递进

**主链路失败或漏时的兜底顺序**：

```
① webhook  200ms-2s   vendor 主动 push
   ↓ 漏了（vendor 挂 / 网络抖 / 我方接收挂）
② probe    60s        我方主动 GET /stock 采样对比·关键补位
   ↓ 漏了（服务重启窗口）
③ scheduler 5min      bus.Scheduler 兜底扫
   ↓ 触发了但被 Step 3 Case B 拒（备胎撑着）
④ 下一轮   等 vendor02 也见底 · 决策器下轮再判
```

**"号少的时候等 webhook 就晚了"**（用户 S7 原话）—— 因此 Tight 时探针必须 fire（`stock_delta`）· 抢在其他家平台 / 手速快用户之前。

### 5.6 边角定案（2026-08-15 用户拍板）

**① 备胎判据 = 数量 AND 价格 双条件**（拍板：复用 `RefillMinCount` + 加价格上限过滤）

"某 vendor 撑得住"的定义：
```
撑得住 = (该 vendor 活号数 ≥ RefillMinCount)
       AND (该 vendor 当前单价 ≤ min(bus.MaxUnitPrice, passenger.MaxUnitPrice))
```

**为什么加价格过滤**（用户原话）："这个还要受价格上限影响·先要把价格上线的过滤掉吧"。

意思是：判"备胎能不能撑"时·先按价格上限**排除**掉超价的 vendor —— 就算它数字够·**用户超价拉不动它** · 用它做备胎没意义。

Step 3 Case B 判据修正为：`存在 vendor v · v.alive ≥ RefillMinCount AND v.currentPrice ≤ userMaxPrice` → 【拒·有备胎】。

**② 抢号能力(prebuy_enabled) = 占坑排队**

用户视角完整故事：

> Alice 开了她拼车的"抢号"开关(bus.Strategy.prebuy_enabled = true)。
>
> **平时**车里号够用 · 系统跟没开一样 · 什么都不发生。
>
> **号少了**(决策器 Step 3 Case C · 所有 vendor 撑不住) · 系统替 Alice 挂 stockwatch 去 vendor 排队等新货。挂上排队的这一刻 · 系统**冻结一小笔占坑费**(固定金额·跟服务费一个量级·就一点点)。
>
> **10 分钟内 vendor 上新号了 → 抢到** · 号进 Alice 车 · Alice 付:
>   · 号价(vendor pass-through)
>   · 服务费(每次拉号都收)
>   · 占坑费(那笔冻结的·此刻转正扣走)
>
> **10 分钟到期没抢到** · Alice 没号进车 · 但**占坑费不退** · 归我方。相当于"你占了这个排队位置·占了就消费掉了"。

**Bob 没开抢号开关**:号少了系统**不替他排队** · 只能等他自己手动拉号时·如果 vendor 缺货·走老"缺货挂单"路径(号价+服务费全冻·到期全退·**不涉及占坑费**)。

**两条路径对照**:

| 谁 | 号少了系统主动？ | 挂单时冻什么 | 抢不到 |
|---|---|---|---|
| 开了 prebuy(Alice) | ✅ 替他排队 | 只冻占坑费(小固定金额) | 占坑费不退·归我方 |
| 没开 prebuy(Bob) | ❌ 不管 | (只有 Bob 手动拉遇缺货才挂 · 冻号价+服务费全部) | 全退·不收占坑费 |

**占坑费金额**(用户原话):"这个也不贵·跟服务费一样啊·我们就收一点点·可能是个固定金额"。**固定金额 · 不按号价比例算**·跟服务费一个量级·加价栈里作为 `capability` 层的固定值·具体金额在 `surcharge_rule` 表配。

**代码改造点**:现有 `stock_watcher.reserved_amount` 冻结的是号价+服务费全部·**开 prebuy 挂单时改成只冻占坑费**·非 prebuy 老路径不变。

**③ 排队等 10 分钟没抢到就自动放弃**（保留现默认值）

系统替 Alice 排队等 vendor 上新号 · 一直等下去不合理(她钱被占着)。**10 分钟没抢到就自动结束这次排队** · 占坑费按 ② 规则处理(不退)。10 分钟这个值在 `internal/stockwatch/store.go:117` 已定 · 不改。

**④ 用户开抢号开关是唯一入口 · 没有"临时抢一次"的 API**

用户原话:"抢号是手动 / 自动拉号之外的附加能力"·不是独立触发。用户主动动作只有两种:
- **手动拉号**(点"拉号"按钮 · 有货直发 · 没货直失败)
- **开抢号开关**(bus.Strategy.prebuy_enabled = true · 系统之后在合适时机替他排队)

不再有"用户 POST /api/me/buses/{id}/prebuy count=N 主动喊一次抢"这种 API。§5.2 Step 2 中那条错误路径已删。

**⑤ 号快用完了就提前补 · 阈值待定**(数据没采集 · 不阻塞)

未来功能:某个号累计用量到达一定百分比 · 系统提前拉一批备着(不等它死)。现在 `credential_usage_snapshot` 表已定义但数据没采集 · 阈值(80%？90%？)等数据上线后按实测定。决策器 §5.2 Step 4 里保留 `usage` 触发源位。

---


**六条已存在的触发** —— 全部最终应该调 §5.2 决策器 · 由决策器决定 output（下单 / 挂单 / 拒）：

| # | 触发源 | 代码位置 | 目前判据 | 是否已装配 | 决策器接入 |
|---|---|---|---|---|---|
| **T1 · 号死立即补** | `deathwatch.markDead` → `pending_refill` 入队 → `RefillTick` 1min 扫 | `internal/deathwatch/refill.go:117` | 读 `bus.Strategy.AutoRefillEnabled` · 反查 owner / bus | ✅ | ⏳ 待改成调 Decide(source=death) |
| **T2 · vendor webhook 新号** | `webhookin.onNewKeys` → `stockwatch.Notify` → fire 挂单 | `internal/webhookin/dispatcher.go:244` · `internal/stockwatch/store.go:495` | 只 fire `status='watching'` 挂单 · 读挂单里 `max_unit_price` | ✅ | ⏳ 待改成调 Decide(source=webhook) · 决定唤醒范围 |
| **T3 · 缺货挂单** | `decider.Pull` 判 `ErrNoStock` → `maybeEnqueueOnNoStock` → `stockwatch.Enqueue` | `internal/stockwatch/store.go:164` | 记录 `client_order_id` / `max_unit_price` / TTL | ✅ | 已符合 · 挂单是决策器 output 的一种 |
| **T4 · 巡检车里还剩几个号** | `bus.Scheduler.ScanOnce` 5min 扫 | `internal/bus/autorefill.go:130` | 读 `bus.Strategy.AutoRefillEnabled` + `RefillWatermark` + `RefillMinCount` | ✅ 装配 · 逻辑粗版 | ⏳ 待改成调 Decide(source=scheduler) |
| **T5 · vendor 余额切换** | `decider.Pull` 打 vendor 前预检 → 不够 → `PickBestVendorExcluding` | `internal/decider/orchestrator.go` | `vendorbalance.Cache.Enough` + `bus.Strategy.PreferredVendor` | ✅ | 已符合 · 内嵌在决策器 Step 6 |
| **T6 · 探针推算 restock** | `prober.deriveStockDelta` 60s 采样 → `stockwatch.Notify` source='stock_delta' | `internal/vendorview/prober.go` | ModeMgr `sourceShouldFire('stock_delta')` | ✅ | ⏳ 待改成调 Decide(source=probe) |

**语义待补位置** → 已收敛到 `decisions §12` 六条·和上面 §5.6 五处 · 拍板后更新本节和 §5。

---

## 6 · 决策依据反查表（字段 → 谁读）

**用户改一个字段 · 系统哪里会感知**：

| 用户改这个 | 系统这些位置立即感知 |
|---|---|
| `bus.Strategy.AutoRefillEnabled` | T1 RefillTick · T4 Scheduler |
| `bus.Strategy.RefillWatermark` | T4 Scheduler.ScanOnce（比较 alive）|
| `bus.Strategy.RefillMinCount` | T1 RefillTick · T4 Scheduler（Pull 的 count 参数）|
| `bus.Strategy.MaxUnitPrice` | 所有 T·`strategy.CanPull` 拦下 · stockwatch 挂单守 |
| `bus.Strategy.PreferredVendor` | T5 · `decider.Pull` AutoPick 兜底 |
| `passenger.MaxUnitPrice` | 所有 T · 与车级 AND 取严 |
| `passenger.DailyRoundLimit` | 所有 T · 跨车累加 |
| `passenger.DailySpendLimit` | 所有 T · 跨车累加 |

**运营改一个开关 · 影响哪些触发**：

| 运营改这个 | 立即生效范围 |
|---|---|
| `TURBO_ON` 文件 | T2/T6 无视 mode 一律 fire |
| `KILL_PULLS` 文件 | 所有 T · Purchase 全停 |
| `surcharge_rule` 改率 | 所有 T · 下次 Pull 生效 |
| `Registry.SetEnabled(vid, false)` | 所有 T · AutoPick 跳该 vendor |
| ModeMgr 自动切档 | T2/T6 · source × mode 表决定 fire |

---

## 7 · 变更协议

- **加新触发源** → 加一行到 §5 触发矩阵 + 写清读什么字段
- **加新字段** → 加一行到 §4 用户策略表 · 顶头标"谁读它" · 未接入不允许提交
- **改字段语义** → 更新 §4 + §6 反查表 · 落 `decisions.md §12`
- **加系统开关** → 加到 §3 对应小节 + §6 反查表底行
- **停用某触发源** → 划掉 §5 那行 + 落 `decisions.md`

**本文永远是"事实" · 不含待决策**（那些留 `decisions.md §12`）。
