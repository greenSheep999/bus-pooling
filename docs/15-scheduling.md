# 15 · 调度系统设计（完整版）

> **本文管什么**:拼车产品的调度系统怎么运作 —— 用户能配什么、系统何时替用户拉号、上游有货没货怎么应对、多个 vendor 之间怎么切换。**这份是完整系统设计**·不是历史修补记录。
>
> **不管**:状态机步骤(去 `09-transactions`)· 加价栈算法(去 `10-pricing`)· 具体 vendor API 字段(去 `11-fields`)。
>
> **贯穿模块**:`strategy` / `bus` / `decider` / `stockwatch` / `deathwatch` / `vendorbalance` / `webhookin`。

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
│   · 加价栈:号价 + 服务费 + 占坑费 + ...(surcharge_rule)       │
│   · 拉号出口:decider.Pull(所有触发最终都调它)                │
│   · vendor 切换:某家没钱切下一家                              │
│   · 急停开关:TURBO_ON / KILL_PULLS 文件哨兵                   │
└──────────────────────────────────────────────────────────────┘
                          ↓ 系统按用户配置动作
┌──────────────────────────────────────────────────────────────┐
│ 用户层(乘客配置 · 每车一套 + 全局默认)                        │
│   · 自动补:开关 + 补到几个 + 每次补几个                      │
│   · 抢号:开关(付占坑费享优先排单权)                          │
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
| `RefillTarget` | int | **补到几个**(活号数达到这个就停) | 0(不设) |
| `RefillBatchMin` | int | **每次至少补几个** | 1 |
| `PrebuyEnabled` | bool | **抢号开关** · 开了付占坑费享优先排单 | false |
| `MaxUnitPrice` | int64 | **每号最贵多少**(microunit) | nil = 不限 |
| `PreferredVendor` | string | **首选 vendor** · 空 = 系统比价选 | nil |
| `PerRoundCount` | int | 手动拉号默认几个 | 建车时抄全局 |

**anon 车专属**(撮合用 · 不参与拉号定价):`AnonZone` / `AnonMaxUnitPrice`

### 4.2 全局默认(`passenger_strategy_default` · 每乘客一份)

| 字段 | 什么意思 |
|---|---|
| `MaxUnitPrice` | 每号最贵多少的**硬上限**(跟车级取最严) |
| `DailyRoundLimit` | 每天最多几轮拉号(**跨所有车累加**) |
| `DailySpendLimit` | 每天最多花多少(**跨所有车累加**) |
| `PerRoundCount` | 建新车时预填的默认数 |
| `PreferredVendor` | 建新车时预填的首选 vendor |
| `DefaultZone` | 建新车时预填的默认区域 |

### 4.3 参数解析优先级 · 三类字段不同规则

**类① · 硬上限 · 取最严**(不是降级 · 是 `min()`):

| 字段 | 规则 |
|---|---|
| `MaxUnitPrice` | `min(车级, 全局)` · 任一层拦住都拦 |
| `DailyRoundLimit` | 只在全局(车级 deprecated) |
| `DailySpendLimit` | 只在全局(车级 deprecated) |

**为什么取最严不降级**:全局是"这个人不想超" · 车级是"这辆车不想超" · 任一层触发都要拦。降级会让用户在某辆车设 max=1000 就绕开全局 max=100 的护栏。

**类② · 偏好 · 车级 > 全局 > 系统内建**(真正的降级链):

| 字段 | 规则 |
|---|---|
| `PerRoundCount` | 车级 → 全局 → `config.pull.DefaultCount` |
| `PreferredVendor` | 车级 → 全局 → 系统比价选(AutoPick) |
| `DefaultZone` | 只在全局 → 代码默认 `auto` |

**类③ · 每车专属 · 只在车级**:

`AutoRefillEnabled` / `RefillTarget` / `RefillBatchMin` / `PrebuyEnabled` —— 每辆车独立配 · 无全局默认。

**类④ · 系统内建 · 用户碰不着**:

`config.pull.*` · vendor 内建限 · `surcharge_rule` 加价率 · `ModeMgr` 阈值 · 文件哨兵。

**UI 展示规范**:给用户看"**实际生效值**"·不是"你设的值"。例:"你设的 max=1000·全局 max=100·**实际生效 100**"。

---

## 5 · 完整决策流

**核心**:六种触发源共享同一套判据 —— 一个函数 `Decide(input)` · 六处都调它。

### 5.1 触发源(什么时候调 Decide)

| 触发源 | 谁触发 | 说明 |
|---|---|---|
| `death` | 号死了 | deathwatch 标死后立刻调 |
| `webhook` | vendor 有新号了 | 收到 webhook 立刻调 |
| `probe` | 我方探针发现 vendor 库存涨了 | 60s 采样对比后调 |
| `scheduler` | 5min 兜底扫 | 遍历所有开了自动补的车 |
| `manual` | 用户手动点拉号 | HTTP API 调 |
| `usage` | 号快用光了(未来) | 用量数据采集后启用 |

### 5.2 Decide 六步串行(一步拒直接返)

**输入**:触发源 + bus_id + 当刻 mode + 车里活号快照(按 vendor 分组)

**输出**:【拒·原因】 / 【下单】(立刻 decider.Pull) / 【挂单】(记入 stockwatch 等抢)

---

**Step 1 · 系统闸门**

`KILL_PULLS` 存在 → 【拒·全停】

---

**Step 2 · 用户 auto 开关**

- `manual` / `death` → 跳过 · 直进 Step 5(手动是用户明说·死号退款是天赋)
- 其他触发源:读 `AutoRefillEnabled`
  - false → 【拒·auto off】
  - true → 继续

**注**:抢号不是独立触发源 —— 用户主动动作只有两个:手动拉号 or 开 `PrebuyEnabled`。没有"POST 临时抢一次"这种 API。

---

**Step 3 · 多 vendor 备胎判据**

按 vendor 分组数活号 · 判"有没有 vendor 撑得住":

**"撑得住"定义**:
```
撑得住 = (该 vendor 活号数 ≥ RefillBatchMin)
       AND (该 vendor 当前单价 ≤ min(车级 MaxUnitPrice, 全局 MaxUnitPrice))
```

价格过滤为什么加:就算某家 vendor 数字够·但**用户超价拉不动它**·用它做备胎没意义。

**四种情况**:

- **整车 alive == 0** → 档 = 急 · 直跳 Step 4 · 强 output
- **有任一 vendor 撑得住** → 【拒·有备胎】(那家撑着·等它也见底再动)
- **所有 vendor 都撑不住·但 alive_total < RefillTarget** → 档 = 常规 · 继续 Step 4
- **alive_total ≥ RefillTarget** → 【拒·已达目标】

---

**Step 4 · 上游 mode × 触发源 → 决定 output**

| 触发源 \ mode | Cool(号多) | Balance(一般) | Tight(号少) |
|---|---|---|---|
| `webhook` | 【拒·cool 不响应】 | 【下单】vendor 说有货 | 【下单】(除非车里也慢 · 改挂) |
| `probe` | 【拒·cool 不响应】 | 【拒·balance 只 webhook fire】 | 【下单】关键补位 |
| `death` | 【下单】vendor 有货直拉 | 【下单】 | 【挂单】紧俏时下单会 ErrNoStock |
| `scheduler` | 【下单】号多下单不亏 | 【下单】 | 【挂单】兜底扫改挂不 Pull |
| `manual` | 【下单】用户明说要 | 【下单】 | 【下单】 |
| `usage`(未来) | 【下单】 | 【下单】 | 【挂单】 |

**Case A(整车挂)强制**:
- Cool → 【下单】(有货直拉)
- Balance/Tight → 【挂单】

---

**Step 5 · 参数解析**(按 §4.3 三类规则)

```
count = min(RefillBatchMin, RefillTarget - alive_total)
  · 且 config.pull.MinCount ≤ count ≤ config.pull.MaxCount
  · 且 count ≤ vendor.MaxPerOrder

maxPrice = min(bus.MaxUnitPrice ?? +∞, passenger.MaxUnitPrice ?? +∞)   · 类①

preferredVendor = bus.PreferredVendor
               ?? passenger.PreferredVendor
               ?? AutoPick(比价选)                                     · 类②

加价栈 = 号价 × vendor × zone × retail × [capability if PrebuyEnabled]
       × service × [single_pull if count==1]

如果 PrebuyEnabled=true → capability 层叠一个"占坑费"(固定小额)
```

---

**Step 6 · 每日限额 + vendor 可行性**(最后一关)

- `passenger.DailyRoundLimit` 累计已用 + 1 超 → 【拒·当日轮次到顶】
- `passenger.DailySpendLimit` 累计已用 + est 超 → 【拒·当日花费到顶】
- `vendorbalance.Enough(vendor, est)` 不够 → 切下一家(PickBestVendorExcluding) · 都不够 → 【拒·vendor 侧全没钱】
- 并发满 → 【拒·并发到顶】

**都过 → 按 Step 4 的 output 执行**:
- 【下单】→ `decider.Pull(count, vendor, maxPrice)`
- 【挂单】→ 记入 `stock_watcher` · 状态 watching · TTL 10min

**每日限额是乐观判定** —— 两辆车同时触发都过关时·靠 `wallet.Reserve` 事务兜底防超限。

---

## 6 · 抢号能力(PrebuyEnabled) · 用户场景故事

**Alice 开了抢号开关**:

> Alice 车里号少了(所有 vendor 都撑不住) · 系统按 §5 决策器决定:tight 时挂单 / balance 时下单。
>
> **不管下单还是挂单**·因为 Alice 开了 PrebuyEnabled·加价栈里叠一层**占坑费**(固定小额·跟服务费一个量级)。
>
> **优先级** —— 挂单场景下·vendor 有新号 fire 时·**开了 PrebuyEnabled 的车排在没开的车前面**·先拿到号。
>
> Alice 视角:每次拉号多花一小笔占坑费 · 换来的是"号少时优先分到"。

**Bob 没开抢号开关**:

> Bob 号少了 · 系统同样按决策器工作 · **只是**挂单 fire 时排在 Alice 后面。加价栈不叠占坑费。
>
> Bob 视角:少付一层 · 但号少时可能抢不到(Alice 先拿)。

**关键**:
- 抢号 = **付费享优先级** · 不是"额外触发一种拉号"
- **每次拉号都收占坑费**(开了就收 · 不管抢到没抢到) · 属于"你付的是能力资格·不是能力次数"
- **抢不到没有退款一说** —— 挂单本来就不冻钱 · 只是意向记录 · TTL 到 expired 就算了(号价压根没花)

---

## 7 · 三层兜底 · 时间粒度递进

主链路失败或漏时:

```
① webhook   200ms-2s     vendor 主动 push
     ↓ 漏了(vendor 挂 / 网络抖 / 我方接收挂)
② 探针      60s          我方主动 GET /stock 采样对比 · 关键补位
     ↓ 漏了(服务重启窗口)
③ scheduler 5min         bus.Scheduler 兜底扫水位
     ↓ 触发了但被 Step 3 拒(有备胎撑着)
④ 下一轮    等 vendor02 也见底再判
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
| 号死·一家死一家活 6·min=3 | 过 | death 跳 | Case B 备胎撑着 | - | **拒·有备胎** |
| 号死·两家都见底 | 过 | death 跳 | 常规 | 视 mode | **执行** |
| 号死·整车挂 | 过 | death 跳 | Case A 急 | Cool 下单 / 其他挂单 | **执行** |
| vendor 新号·Alice 开抢号 | 过 | webhook · auto on | 常规 | Balance/Tight 下单 · 叠占坑费 | **执行 + 收占坑费** |
| vendor 新号·Bob 没开 | 过 | 同上 | 常规 | 同上 · 不叠 | **执行·Alice 排前面** |
| 保底扫·剩号少于紧急线 | 过 | scheduler | 常规 | Tight 挂单 | **挂单** |
| 用户没开 auto·上游有货 | 过 | auto off | - | - | **拒·手动才动** |
| 新建拼车·空车·auto 默认关 | 过 | auto off | - | - | **不动 · 第一批手动** |
| 用户手动点拉号·有货 | 过 | manual 跳 | - | Cool/Balance/Tight 都下单 | **执行** |
| 用户手动点拉号·没货 | 过 | manual 跳 | - | 直接返 ErrNoStock · 挂 stockwatch 等 | **挂单** |

---

## 10 · 反查表(改一个字段 · 影响哪里)

**用户改一个字段**:

| 改这个 | 立即感知 |
|---|---|
| `AutoRefillEnabled` | 决策器 Step 2 · 号死立即补 · 5min 兜底扫 |
| `RefillTarget` | 决策器 Step 3 判"达标了吗" |
| `RefillBatchMin` | 决策器 Step 3 备胎判据 + Step 5 拉几个 |
| `PrebuyEnabled` | 决策器 Step 5 加价栈叠占坑费 + 挂单 fire 排优先 |
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

**本文永远是"完整设计" · 不含待决策**(那些落 `decisions.md §12`)。
