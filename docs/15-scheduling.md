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
| `webhook` | ✅ | ✅ | ❌ | ✅ |
| `xi8_signal` | ✅ | ✅ | ❌ | ✅ |
| `stock_delta`（探针推算） | ✅ | ❌ | ❌ | ✅ |
| `manual`（CLI） | ✅ | ✅ | ✅ | ✅ |

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
| `bus.Scheduler` | 5min | 扫 auto_refill 车水位 · 触发 refill | ✅ 读 bus.Strategy |

---

## 4 · 层 ③ 用户策略层 · 全字段清单

### 4.1 `bus.Strategy`（每车一份 · `internal/bus/bus.go`）

| 字段 | 类型 | 语义 | 谁读 |
|---|---|---|---|
| `AutoRefillEnabled` | bool | 自动补车总开关 | `bus.Scheduler` · `deathwatch.RefillTick` |
| `RefillWatermark` | int | 水位阈值（**语义待定** · 见 `decisions §12` 位置 6）| `bus.Scheduler.ScanOnce` |
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

---

## 5 · 触发路径矩阵

**六条已存在的触发** —— 全部最终落到 `decider.Pull`（唯一号入库出口）：

| # | 触发源 | 代码位置 | 判据（读什么字段）| 是否已装配 |
|---|---|---|---|---|
| **T1 · 号死立即补** | `deathwatch.markDead` → `pending_refill` 入队 → `RefillTick` 1min 扫 | `internal/deathwatch/refill.go:117` | 读 `bus.Strategy.AutoRefillEnabled`（关了则 skipped）· 反查 owner / bus | ✅ |
| **T2 · vendor webhook 新号** | `webhookin.onNewKeys` → `stockwatch.Notify` → fire 挂单 | `internal/webhookin/dispatcher.go:244` · `internal/stockwatch/store.go:495` | 只 fire `status='watching'` 的挂单 · 挂单里读 `max_unit_price` 涨价保护 | ✅ |
| **T3 · 缺货挂单** | `decider.Pull` 判 `ErrNoStock` → `maybeEnqueueOnNoStock` → `stockwatch.Enqueue` | `internal/stockwatch/store.go:164` | 记录 `client_order_id` / `max_unit_price` / TTL | ✅ |
| **T4 · 水位巡检** | `bus.Scheduler.ScanOnce` 5min 扫 | `internal/bus/autorefill.go:130` | 读 `bus.Strategy.AutoRefillEnabled` + `RefillWatermark` + `RefillMinCount` | ✅ 装配 · 语义待改 |
| **T5 · vendor 余额切换** | `decider.Pull` 打 vendor 前预检 → 不够 → `PickBestVendorExcluding` | `internal/decider/orchestrator.go` | `vendorbalance.Cache.Enough` + `bus.Strategy.PreferredVendor` | ✅ |
| **T6 · 探针推算 restock** | `prober.deriveStockDelta` 60s 采样 → `stockwatch.Notify` source='stock_delta' | `internal/vendorview/prober.go` | ModeMgr `sourceShouldFire('stock_delta')` | ✅ |

**六条已识别但语义待补的位置**（见 `decisions §12` · 拍板后更新本文）：

1. **webhook 唤醒范围** —— T2 现只喂"缺货挂单车" · 开了 auto_refill 但没挂单的车收不到
2. **prebuy-pool 分配** —— stockwatch 抢到无主号 5min TTL 到期只能退回 vendor · 没有分配路径
3. **多 vendor 同车判据** —— T4 只看整车 alive · 不看"vendor01 死了但 vendor02 撑得住"
4. **建拼车后第一次一律手动** —— T4 会给刚建的空 auto 车立即拉一批 · 违反约定
5. **保底触发方式** —— T4 应挂 stockwatch 等 webhook · 不是硬 Pull
6. **用户字段命名** —— `RefillWatermark` 是目标还是红线不清

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
