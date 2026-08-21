# CLAUDE.md · 给未来 AI agent 的行为铁律

> **每次进入本项目动代码前**：先读 `README.md`，再读 `docs/00-values-and-phases.md`，最后读**本文**。
>
> **写前端页面前额外必读**：`docs/13-frontend-design.md`（数据表达 + 组件用法规范，从概览页 v7 沉淀，硬约束）。写新页面时抄 `web/src/pages/Overview.tsx` 的结构、不重造轮子。
> 本文是防止重蹈旧项目 `kiro-auto` 覆辙的**硬约束**。
>
> 旧项目 `kiro-auto`（本机 `~/Repositories/daniel/kiro-auto`）—— 90+ 内部模块、60+ 份 v3 文档、5 tier 文档 governance —— 演化成了它自己解释不清的样子。本项目**不重蹈**。

---

## 0. 第一优先级（覆盖以下所有规则）

### 0.1 前台接口 = 只给结果数据

**给前端 / 对外 webhook / 帮助中心的接口，只返回乘客做决策需要的结果数据。内部字段、内部枚举、内部术语一律不出去。**

**不得出现在响应体 / 错误 message / 前端类型定义里的**（非穷举）：

- 加价链的分层字段：`vendor_fee` / `region_fee` / `single_pull_fee` / `capability_fee` 等 —— **对外只有单价 + 服务费**（`decisions §8.20`）
- 号池实现细节：`housepool` / `record group` / `bus-<id>` / `record-<pid>` / `kiro_rs_id` / `kiro.rs` / `current_group` / `death_source`
- 上游架构名：`provider` / `adapter` / `decider` / `coalescer` / `deathwatch` / `pullrecord`
- 内部状态多态：`preparing` / `standby` / `dying` / `scrapped` / `purchasing` / `reserved` 等（收敛见 §12.5）
- 内部错误码里的实现术语：`housepool_unavailable` 之类

**写法**：每个对外端点必须**独立定义**响应 struct（如 `strategyResponse` / `profileResponse`），**不许**把内部 struct 直接 `writeJSON` 出去；枚举一律**在 api 层做映射**，不许直接 `string(internalEnum)`。

**审计动作**：写 / 改任何对外接口前，先扫 §1（术语铁律）和 §12.6（对外文案）—— 写完再扫一遍响应体和 message，含内部术语则打回。

**冲突时**：`web/src/types/index.ts` 是可执行契约（前端 TS 编译器会检查），跟 `docs/05-api-contract.md` 有出入以它为准（`05-api-contract §625` 已定）。发现 md 里写着内部字段（比如响应体里出现 `death_source`）时，改 md 对齐 TS，不是反过来。

### 0.2 代码注释只写代码解释

**注释里只写**：为什么这样实现、别人容易踩的坑、隐含的契约。

**注释里不写**：

- 我自己的推理过程 / 思考日志 / 「我一开始想…后来发现…」
- 跟车主 / 用户的对话内部沟通记录（"车主原话：…"、"讨论过 A 但被否决"等 —— 这些进 `decisions.md`，不进代码）
- 项目背景 / 阶段规划 / 需求来源 / 谁在什么时候说过什么
- 大段的业务政策复述（价格结构、议价规则、阶段 roadmap 等）

**判据**：注释描述**代码此刻在做什么、为什么这么做**。如果读代码的人不需要知道就能维护它，那条注释就删。

---

## 1. 术语铁律（不许再讨论、不许再改）

### 1.1 分层名词

| 中文 | 英文 | 定义 |
|---|---|---|
| **provider** | `provider` | 上游协议族。当前唯一：`kiro`。未来可能：`cursor` 等 |
| **vendor** | `vendor` | 同 provider 下的具体供应源。当前 6 家：91kiro、kiro.ceo、kiro.ooo、kiroapp.io、kiroapp.cc、drop.kiro.ss |
| **乘客** | `passenger` | bus-pooling 的终端用户 |
| **housepool** | `housepool` | 我方运维的 kiro.rs 实例（当前 `kiro.aibbq.xyz`）。5 项能力：校验 / 探活 / 用量 / 分组 / 并发监控 |
| **passengerpool** | `passengerpool` | 乘客自己运维的号池（外部系统） |
| **credential** | `credential` | housepool 里一份号（不叫 key、不叫号、不叫账号） |
| **bus** | `bus` | 拼车局实体（1 人或多人都是 bus）。housepool 里对应 `bus-<bus_id>` group |
| **拉号记录** | `pull_record` | **housepool 里 `record-<pid>` group + `disabled=true`**（不是数据库表；号已进池已监控） |
| **市场** | `market` | 阶段 3d 的公开 group（阶段 1/2 不实施） |

### 1.2 动作与去向

| 动作 | 说明 |
|---|---|
| **拉号** | 从 vendor 取号进 housepool。主入口拼车拉的号直接进 `bus-<id>`；次入口单独拉号进 `record-<pid>` |
| **上车（进车）** | 号进 `bus-<id>` group |
| **推乘客号池** | 复制到 passengerpool（**双写**：housepool 保留监控副本） |
| **拿走（handoff）** | 号数据交给乘客 + `DELETE /credentials/{id}`；**唯一"发出去不管"的路径**。注意："不管"指**不再监控存活、不留明文**，**不等于不留记录** —— `credential_ledger` 台账行永不删（含 masked / vendor / 时间 / 已耗额度），供售后追溯，见 `decisions §8.24` |
| **补车** | bus 内号死后自动触发新一轮拉号 |
| **集单** | 同 bus 内多成员补车意图在窗口内合流 |
| **发车** | 阶段 3b/3c 才做：乘客上 AWS → 我方转发 vendor 开号 |

### 1.3 计费术语

| 中文 | 英文 | 归属 |
|---|---|---|
| 号价 | `key_cost` | vendor pass-through |
| 单次议价 | `single_pull_fee` | 我方（`count==1` 时链上加一层 `× (1+率)`，**不是号价 × 率** —— 见 `decisions §8.34` 分项拆法） |
| 附加能力费 | `capability_fee` | 我方（**插槽**，阶段 1 无实例） |
| 服务费 | `service_fee` | 我方（加价链最后一层 `× (1+率)` · 单一费率不分档 · 具体率在后台配置，**不写进代码** · 见 `decisions §8.34`） |
| 通道费 | `channel_fee` | waffo（`pass-through` 5%） |

**用户档次**（`passenger.tier` · **`docs/10-pricing §2.1` 三档定稿** · 覆盖老 `decisions §8.39` 命名）：

| 内部档名 | tier 字段 | 谁 | 减免层 | 看 vendor 真名 | 相对倍率（批量） |
|---|---|---|---|---|---|
| **零售** | `retail` | 散客 · 无码 | 无 · 全套加价 | ❌ 匿名 label | 2.10× |
| **社群** | `community` | TG/Discord · 社群码 | 免区域附加费 | ❌ 匿名 label | 1.75× |
| **批发商** | `wholesale` | B2B 定向 · 批发商码 | 免 vendor + 区域附加费 | ✅ **唯一可见的档** | 1.05× |

> **⚠️ `wholesale` 同名不同义** —— 老 `§8.39` 里 `wholesale` 是**中间档**（社群）· 现在是**最优档**（批发商）·
> 老 `insider` 档**已废**。迁数据 / 改代码时**别做字符串替换**。

**计费模型：逐层乘**（`docs/10-pricing §2.2`）：
```
最终单价 = 号价 × (1+vendor 附加费) × (1+区域附加费) × (1+单次议价) × (1+插槽…) × (1+服务费率)
本次扣除 = 最终单价 × 号数
通道费**不在这条链里** —— 充值时单独收一次（§8.21）
```

**按档跳过的层**：`retail` 全套 · `community` 跳 region_markup · `wholesale` 跳 vendor_markup + region_markup ·
**服务费 + 单次议价（`count==1`）所有档都收**。

**定价只有一个查询入口**：`vendorview.PricedFor`（`docs/10-pricing §4`）· 拉号 / 拼车 / Pricing 页 / Status 页全走它 ·
**不许出现第二处算价**。读定价规则**只读 `docs/10-pricing.md`** —— `§8.20/§8.29/§8.32/§8.34/§8.39` 已被它覆盖。

**各层费率是内部配置** · 只在文档和后台，**不进代码注释**（前端代码用户看得到 · §8.20 不许暴露加价幅度）

### 1.4 充值口径（跟拉号扣除完全独立）

**积分是单位，不是币种**。基准：`1 积分 ≡ 1 CNY`（会计对账口径）。

**通道费加在本金上**（不是含在总额里）：

```
目标积分 = 乘客想充的数字
通道费   = 目标积分 × 5%             （waffo 侧的 pass-through）
本次到账 = 目标积分                   （净增到 wallet.balance 的量）

支付显示 = (目标积分 + 通道费) / 汇率 = 目标积分 × 1.05 / 7  单位 USD
汇率     = 7 CNY / USD               （对乘客展示层用；后端记账单位始终是积分）
```

**举例**：想充 100 积分 → 通道费 5 积分 → 折 105 CNY → 除 7 汇率 → **waffo 界面显示 15 USD**

**内部账本记积分**（`wallet_ledger`）：一次充值两条明细
- `recharge +105`（乘客真金白银换到的总积分，含要给 waffo 的那部分）
- `channel_fee -5`（pass-through 立刻扣回给 waffo）
- **净变化 = +100 积分**（乘客账户实际到手）

**跟拉号扣除的分工**（别混）：
- **拉号消费**：走 `decisions §8.34` 的加价链 · reason 是 `key_cost` / `vendor_fee` / `region_fee` / `single_pull_fee` / `capability_fee` / `service_fee` · 对外 `LedgerType = spend`
- **充值**：走本节口径 · reason 是 `recharge` / `channel_fee` · 对外 `LedgerType = topup`
- **运营调整**：reason 是 `admin_adjust` · 对外 `LedgerType = refund`

**旧口径作废**（散在多份 md 里，遇到就改）：
- ❌「乘客付 100 CNY，到账 95」/「recharge +95」/「channel_fee -5」的旧版
- ❌ 计价用 CNY 描述（`docs/02-flows.md` / `docs/04-scenarios.md` 里的时序图）
- ✅ 统一按「乘客想充 N 积分，通道费 = N × 5%，支付 (N × 1.05) / 7 USD」

### 1.5 策略优先级铁律（`docs/15-scheduling.md §4.3` 是唯一权威）

**读取任何拉号策略字段前 · 必须先看 `docs/15-scheduling.md §4.3`**。别在代码 / 文档 / 讨论里重新发明规则。

**四层优先级 · 顺序固定**：

```
本次请求约束 > 车级策略(bus.strategy) > 全局默认(passenger_strategy_default) > 系统默认值(config.pull.*)
```

**字段两类 · 规则不同**（见 §4.3.2）：

- **类① 硬上限**（`MaxUnitPrice` / `DailyRoundLimit` / `DailySpendLimit`）—— 取 `min` · **请求不能放宽**
- **类② 覆盖**（后者盖前者 · 但要区分**当前 vs 1f-B 目标**）：
  - **当前已成立**：`PerRoundCount` / `PreferredVendor` / `Zone` —— 全局作为运行时 fallback 已可用
  - **1f-B 目标**：`AutoRefillEnabled` / `RefillWatermark` / `RefillMinCount` —— **当前仅在车级**·`passenger_strategy_default` 表**尚无**这三字段·1f-B 才新增全局字段 + 继承语义(§4.3.2b)。**当前 `Effective()` 对这三字段只读车级**。

**自动触发无 request**（webhook / deathwatch / scheduler / probe / coalescer）：
- **当前**：调 `Effective()` 前必须解析到具体 `busID`(见 §4.3.3 解析路径表)·`auto/refill` 只读车级·**无全局 fallback**
- **1f-B 后**：车级选"跟随全局"时可 fallback 到 `passenger_strategy_default`(需先落全局字段 + 继承语义)

**唯一入口**：`internal/strategy.Effective(ctx, passengerID, busID, requestOverride) → EffectiveStrategy`（sprint-1f-C 收口）。**别再手工拼字段** —— code review 验收规则见 `docs/15-scheduling.md §4.3.4`（精确到"运行时决策路径不得手工合并策略字段" · **不误伤** DTO / schema / store 基础读写 / 测试 / 文档）。

**UI 展示规范**：给用户看"**实际生效值**"·不是"你设的值"。EditStrategyPanel 必须清晰二态（跟随全局 / 覆盖本车）· 硬上限字段旁标"仍受全局 X 约束"。

---

## 2. 术语作废清单（**这些词一律不用**）

| 作废 | 替代 |
|---|---|
| `solo bus` | 直接说 "1 人 bus" 或就叫 `bus` |
| `pooled bus` | 直接说 "多人 bus" 或就叫 `bus` |
| `mode: carpool / solo` | 号的去向决定，不用 mode 二选一 |
| `混合上车` | 号的分配是自然结果，不叫"混合"什么 |
| `方式 A / 方式 B` | 用**3 种去向**：进车 / 推 passengerpool / handoff |
| `交付方式 B` = fire-and-forget | 只有**去向 ③ handoff** 才是 fire-and-forget |
| `allocation_plan / allocation 组件` | 不做混合上车，也没这组件 |
| `personal-<pid> group` | 用 `record-<pid>` |
| `credential 记账在数据库` | **拉号记录在 housepool 里**，是 group + disabled 状态 |
| `拉号记录不进池` | ❌ **进池，是 housepool 里的 group** |
| `拉号推给 passengerpool = 号离开系统` | ❌ **双写**，housepool 副本保留监控 |
| `发出去就不管` | 只有去向 ③ handoff 才不管 |
| `P0 / P1 / P4a / P∞` 等 P 标签 | 用**阶段 1a / 1b / ... / 2a / 3a** 命名 |
| `轮 = 一个号` | ❌ **1 轮 = 1 次拉号动作**，不管几个号 |
| `single 车不能加人` | ❌ **任何车都能加人** · 1 人是状态不是类型 · **拼车码**给出去就变多人拼车（1c 定稿） |
| `只有 team 车有邀请码` | ❌ **用户建的车一律有拼车码**（`single` / `team` 行为完全一致）· 只有系统撮合池 `anon` 没码 |
| `1 人车 / 邀请码车`（UI 类型标签） | ❌ 按 `member_count` 说：**独享** / **N 人拼车** · `kind` 不对外 |
| `拼车邀请码` / bus 侧的"邀请码" | ❌ **只叫拼车码**（`bus.invite_code`）· 好友邀请是另一个东西(`personal_invite_code`)· 别混 |
| `建车时选车类型` | ❌ 建车没有类型可选 · 建出来就是一辆能加人的车 |
| **`社群价 / 社群成员`**（作为**内部档名**）| ⚠️ 内部档名是 **`community`（社群）/ `wholesale`（批发商）**（`docs/10-pricing §2.1`）· **对外 UI 保留"社群成员"** Chip（用户视角只区分"绑了专属邀请码"vs"没绑" · 具体档次不对外） |
| **`散客 / 标准价 / 优惠价 / 零售价 / 批发价 / 同行价`** | ❌ **UI 里都不出现** —— 用户端不暴露档次差别 · 加价链算完的最终单价就是他能看到的（`CLAUDE.md §0.1`）|
| `invited: bool`（作为档次判断） | ❌ 用 `passenger.tier ∈ {retail, community, wholesale}`（三档 · `docs/10-pricing §2.1`）· 字段 `invited` 保留作兜底，下次 schema 变更删 |
| **`insider` / `同行` / `同行价` / `同行码`**（tier 命名）| ❌ **档已废** —— 三档现为 `retail / community / wholesale`（`docs/10-pricing §2.1`）· 老 `insider` 的位置由 **`wholesale`（批发商）** 接替 |
| `系统邀请码`（单一）| ❌ 内部分**批发商码 / 社群码** · **对外 UI 都叫「专属邀请码」**（用户不感知级别 · `docs/10-pricing §2.1`）|
| `账号安全`（作为设置页名）| ❌ 用 **账号设置**（还含邮箱/用户名/第三方登录 · `decisions §8.40`）|

**如果看到旧对话或旧文档里出现上面左列的词，立即警觉，去 `docs/decisions.md` 查为什么废**。

---

## 3. 不做清单（阶段 1）

**阶段 1 明确不做**（跟 `docs/00-values-and-phases.md §6/7` 一致）：

- ❌ AWS 开号（是 vendor 内部的事，阶段 3b/3c 才转发）
- ❌ vendor 内部号池调度
- ❌ vendor 内部拼车池
- ❌ 号池的存储/分组/客户端 key 下发（用 kiro.rs）
- ❌ 号池存活探测底层（用 kiro.rs）
- ❌ 列队策略（阶段 2a · 多 bus 抢同 vendor 时的上层调度）
- ❌ 压车治理（阶段 2b · bus 内噪邻探测 + 限速降级）
- ❌ 数据图表（阶段 3a）
- ❌ 市场（阶段 3d）
- ❌ 发车（阶段 3b/3c）
- ❌ 会员制、订阅制、长效号池、数据看板、白标 API、优先拍单议价、指定 vendor 议价、寿命 SLA 议价、地区议价 —— **全部已被否决**（见 `decisions.md`）
- ❌ 附加能力具体实例（插槽预留，阶段 1 无实例）

**如果对一件事该不该做不确定，先查 `decisions.md`，再问用户。不要自作主张实施。**

---

## 4. 目录规范

### 4.1 业务包上限：15 个核心业务包

**旧项目痛点**：`internal/` 长到 90+ 个包，任何业务概念都开新包。

**本项目硬约束**：

- `internal/` 顶层**核心业务包**不超过 15 个（下面 §4.2 列出）
- **加新核心业务包必须写清"为什么不能放进已有包"**（在 `03-modules.md §5 目录规划` 里说明）
- **基础设施包**（`api / config / db / httpx / secrets / authpassenger / authadmin / web`）不算业务包
- **decider 支撑包**（`pricing / stockwatch / vendorbalance / insight / vendorview / xi8 / marketstock / topupchannel / vendoraccount / paymentgw / credplain / downstream`）**跟核心业务包分开算** —— 它们是 decider / payment / 数据视图的支撑层
- **调度收口不得新开顶层业务包**·系统主动拉号的决策入口(见 `docs/15-scheduling.md §5`)统一归入 `internal/decider/` —— 不要造 `internal/scheduling/` 之类新包

### 4.2 当前 15 核心业务包（见 `03-modules.md`）

```
providers · webhookin · passenger · wallet · redeem · topup ·
strategy · coalescer · decider · deathwatch · webhookout ·
pullrecord · bus · housepool · delivery
```

**当前 internal/ 目录 34 个包**（2026-08 实际）：
- 15 核心业务（上面列表）
- 8 基础设施：`api / config / db / httpx / secrets / xi8 / web`（加 `authpassenger/authadmin` 若未来拆）
- 12 支撑层：`pricing / stockwatch / vendorbalance / insight / vendorview / xi8 / marketstock / topupchannel / vendoraccount / paymentgw / credplain / downstream`

**破 15 上限的诱惑**（旧项目栽在这里 · 核心业务不许扩）：
- ❌ 新加 `carpool_room`（用 `bus`）
- ❌ 新加 `allocation`（不做混合上车，`pullrecord` 一并处理去向派发）
- ❌ 新加 `matching`（撮合逻辑归 `coalescer/anon.go`）
- ❌ 新加 `refund`（走 `wallet` + `deathwatch`）
- ~~❌ 新加 `pricing`~~ · **已存在** · 归 decider 支撑包（`vendor_pricing` 表读写 · 换算规则）
- ❌ 新加 `newapi` / `tokensheep` / `apilane` / `meter`（都不做，不是拼车产品线）

### 4.3 一份文档一件事

**旧项目痛点**：文档 60+ 份，一份文档里啥都塞。

**本项目分工**：

| 文档 | 只写 | 不写 |
|---|---|---|
| `00-values-and-phases.md` | 定位 / 价值 / 计费 / 阶段 / 术语 | 具体表 / 时序 / 函数 |
| `01-architecture.md` | 层 / 职责 / 顶层目录 | 具体时序 / 表 |
| `02-flows.md` | 端到端运行时时序 | 层次 / 具体 SQL |
| `03-modules.md` | 模块清单 / 依赖图 | 表 / 端点 |
| `04-scenarios.md` | 用户视角场景 | 技术实现 |
| `decisions.md` | 讨论过并否决的方案 | 待做 TODO |

**加新文档**（超过 5 份主文档 + `decisions.md`）**要写清为什么**。

---

## 5. 讨论过并否决的方案（速查，防重复讨论）

**完整列表在 `docs/decisions.md`**。**动手前先扫一眼**：

- ❌ 卖号 / 加价号价 / 差价模型
- ❌ 会员订阅制
- ❌ 长效号预留池
- ❌ 数据看板订阅
- ❌ 白标 API B2B
- ❌ 优先拍单 / 急单议价（跟单次议价重复）
- ❌ 指定 vendor 议价（没实质价值）
- ❌ 寿命 SLA 议价（没数据）
- ❌ 地区议价（无法识别，等多通道再说）
- ❌ **长效号预留池**（预付 + 议价商业模型 · 号老）· ✅ **加速预存池**（抢号缓冲 · 5min TTL · 号新 · 落 housepool `prebuy-pool` group · 见 `decisions §11.15`）—— 两回事别混
- ❌ Solo bus / pooled bus 概念（简化成 1 人 bus / 多人 bus）
- ❌ 混合上车 / allocation 组件
- ❌ 拉号记录 = 数据库表（应是 housepool group）
- ❌ 发出去就不管（除 handoff 外全监控）
- ❌ 用数据库替代 housepool（5 项能力必须号池才有）
- ❌ 内嵌 kiro.rs（外部服务，我方做 client）
- ❌ 按 CNY/USD 双币种区分海外国内（waffo 单币种不支持）
- ❌ 按用户注册地区判定（无法校验）
- ❌ 散户切片（一把 key 切多份）—— 未来产品线，不并入拼车
- ⏸ SuperTokens / Casdoor / Ory 等外接登录方案 —— 阶段 1 自建（Go + Argon2id + session cookie），未来可评估

---

## 6. 决策流程

### 6.1 遇到不确定的事，按顺序

1. 查 `decisions.md` —— 有可能已经讨论过并否决
2. 查术语铁律（§1）和作废清单（§2）—— 有可能是命名冲突
3. 查阶段（`00 §7`）—— 有可能不属于当前阶段
4. **问用户**。不要自作主张造抽象、造模块、造术语

### 6.2 添加新概念前的 self-check

- [ ] 是不是已废名词的换皮？
- [ ] 能不能放进已有 15 个业务包？
- [ ] 是不是"未来才做"的（`decisions.md` 里未来增值方向 / 阶段 3+）？
- [ ] 讨论过被否决没？

任何一项 ✓ 就停下来问用户。

### 6.3 议价 / 收费点新增

**这条特别提醒**：定价讨论极容易反复。当用户想加新收费点时：

1. 先查 `decisions.md §5.x 议价点历史`
2. 若讨论过并否决 → 直接回复"这条讨论过并否决，理由：..."
3. 若是新方向 → 用户拍板后**同步落 `00 §3` + `00 §8.4` + `04 D 场景` + `decisions.md`**

---

## 7. 代码规范（P1a 编码时详化，先立骨）

### 7.1 Go 侧

- 包命名：全小写，无下划线（`pullrecord` 不是 `pull_record`）
- 依赖只能"下层被上层导入"，见 `03-modules.md §依赖关系`
- 每业务包**自己写 SQL**，不建 ORM
- 出向 HTTP 一律走 `internal/httpx`（proxy / timeout / no_proxy 统一）
- 敏感字段一律走 `internal/secrets`（AES-GCM，主密钥来自环境变量）
- **每次 credential 状态转换**（BatchImport / PUT / DELETE）**先做外部动作，再改 housepool 状态**（防止"号交出去了但状态没改"）

### 7.2 数据库

- SQLite WAL 单节点
- 主键：**UUID v7**（时间有序 + 无遍历攻击面），存 `TEXT`
- money 字段：**整数 microunit**（1 元 = 1_000_000）
- 时间：ISO-8601 UTC 存储；UI 层做时区转换
- **并发控制**：SQLite 用 `BEGIN IMMEDIATE`（不用 `SELECT ... FOR UPDATE` —— SQLite 不支持行级锁）
- **事务边界**：跨系统写（vendor + kiro.rs + SQLite）**不做隐式两阶段提交** —— 用**持久化状态机**（见 `docs/09-transactions.md`）
- 幂等：写请求以 `X-Idempotency-Key` 做请求指纹落 `idempotency_record` 表（见 `06-db-schema.md`）

### 7.3 命名冲突哨兵

**如果你在写代码时想造这些名字，立即停下来查作废清单**：
- `carpool` / `car_id` / `carpool_room` / `carpool_id` → 用 `bus`
- `allocation_plan` / `allocation_rule` → 无此概念
- `personal_group` → 用 `record-<pid>`
- `delivery_a` / `delivery_b` → 用 `delivery/passengerpool/*` / `delivery/handoff/*`
- `mode_solo` / `mode_carpool` → 无 mode 概念
- `refund` 类 API → 走 `deathwatch` + `wallet`

---

## 8. Git 规范

### 8.1 敏感字过滤

- **绝不提交**账号 / 密码 / API key 明文
- 提交前扫一遍：`grep -rEn 'sk-|usr-|1qazxsw|passwd|token=' <changed files>`
- 已经在 `.gitignore` 屏蔽的 Playwright / _sources 中间产物 —— **不要手动加回来**

### 8.2 commit message

- 格式：`docs: <what changed> - <why>`
- 中文英文都可以，一致即可
- **绝不写**：`git config user.name` 用真人名字（用 `bus-pooling` 或 `assistant`）

### 8.3 amend / rebase

- 敏感字漏进去了：**立即 amend**（工作树还干净时）
- 已经提交过：`filter-repo` 或干脆重建历史（旧项目历史里有账号密码，别在本项目重演）

### 8.4 目录 .gitignore（已定，别改）

```
.playwright-mcp/
/kiro-*.md（Playwright snapshot 落到根的）
docs/vendors/_sources/*.js (除 kiroceo-docs.js)
docs/vendors/_sources/root-*.html
.DS_Store · *.swp
```

---

## 9. Playwright / 探测 vendor 时的规矩

- 抓 vendor 官方文档：账号密码用 env 变量，**绝不落文件**
- 抓下来的 snapshot 里含账号 / API key 明文 → **不要**入库；提炼到 `docs/vendors/*.md` 时脱敏成 `<redacted>`
- vendor 档案里保留**可验证的事实**，不做主观筛选（14 节全量骨架，见 `docs/vendors/README.md`）

---

## 10. 交流规范（我方 AI agent 跟用户对话）

（这条是给 AI agent 自己看的）

**旧项目 AI 反复犯的错**（本项目不重蹈）：

1. **不该问的疯狂问 confirm** —— 已经用户明确指示"改"了，还回头问"我这样改对吗"。**别问，直接改，改完汇报**
2. **过度延伸命名** —— 用户说"车"，AI 造出 solo bus / pooled bus / carpool room 三层名字。**用用户的原话**
3. **一件事分 5 轮才达成共识** —— 用户已经解释清楚，AI 又回头造术语。**用户否决过的方向立即扔掉**
4. **文档只加不减** —— 每次对齐都新加一节，从不删旧的。**旧概念作废时立即从文档里删掉**（`decisions.md` 里保留记录）
5. **反问回避决策** —— 用户"这方案 OK 吗" AI 反问"你想 A 还是 B"。**给推荐 + 理由，让用户否决而非选择**

**沟通姿态**：
- 极简：一条建议、一个问题、不列 20 个选项
- 用用户的语言：他叫"发车"就叫"发车"，别造 "dispatch" 之类
- **改完再汇报，不要"改前 confirm"**（用户已经说"改"就动手）
- **失误立刻承认**："是我错了，X 应该是 Y" —— 不辩解、不解释来龙去脉

---

## 11. 危险动作列表（碰之前必须问用户）

- 删除 `docs/vendors/*.md` 任何一份
- 修改 `00-values-and-phases.md §7 阶段表` 里已定的阶段划分
- 修改**术语铁律**（§1）—— 除非用户明确要求
- 修改**议价规则**（`00 §3`）
- 修改**旧项目 kiro-auto** 里的任何文件（本项目**只读参考**，不改）
- 修改 kiro.rs 源码（本机 `~/Repositories/kiro.rs`，我方运维的 kiro.rs 实例；本项目**只读参考**，不改）
- 修改 `.gitignore` —— 尤其别把 `.playwright-mcp/` 从屏蔽名单里拿出
- 修改 payment-gateway 相关的 5% 通道费率
- 修改 kiro.aibbq.xyz 的 admin API key
- git force push / git reset --hard / git filter-repo（历史改写）

---

## 12. 状态与术语 · 严格双分离

**核心**：**内部** vs **对外** 严格分离两件事——
- **状态**（如 `credential.status` / `pull_round.status`）：内部多态，对外收敛（**见 §12.5**）
- **术语**（如 `housepool` / `provider`）：内部随便叫，对外只用人话（**见 §12.6**）

**旧项目失败的根因**：这两个分离都没做 → 内部实现细节全渗透到用户端 → 用户端跟着内部演化跳来跳去 → 复杂度失控 → 衍生一堆"补丁"功能。

**本项目铁律**：**API 返回体 / UI / webhook / 帮助中心里，绝不出现内部状态枚举或内部术语**。有则 code review 直接打回。

### 12.5 状态收敛原则（内部多态 → 用户少态）

**旧项目栽过大跤**：把内部 6 个状态全暴露给用户 → 用户困惑 → 加"解释性"功能（tooltip / 详情页 / FAQ）→ 越加越复杂。

**本项目铁律**：**用户端只显示 2-3 个决策性状态**；内部多态在 API 层做映射收敛。

### 状态映射表（关键实体）

| 实体 | 内部状态（DB / 代码） | 用户可见状态（UI / API 返回 / webhook 载荷） |
|---|---|---|
| **credential** | `status ∈ {alive, dead, handed_off}` × `disabled ∈ {0,1}` × `current_group` × `death_source ∈ {housepool_probe, vendor_webhook, vendor_poll}` | **"活的"** / **"已失效"** 二态。`handed_off` 直接不出现在用户号列表里 |
| **bus** | `kind ∈ {single, anon, team}` × `status ∈ {active, dissolved}` × `member_count` | **按人数说，不按 kind 说**：1 人 → **"独享"** · 多人 → **"N 人拼车"** · 系统撮合池 → **"搭车"**。底部标 **"活跃"** / **"已解散"** 二态。<br>**`kind` 不对外暴露** —— 它只记"谁建的"（`single`/`team` = 用户建 · `anon` = 系统建），不决定能不能加人。详见 `06-db-schema.md §车 kind 语义` |
| **pull_intent** | `pending / in_flight / coalesced / fulfilled / failed / cancelled` | **"拉号中"** / **"完成"** / **"失败"** 三态（合流细节隐去） |
| **pull_round** | `initiated / completed / failed / partial / refunded` | **"成功"** / **"部分成功"** / **"失败"** / **"已退款"** 四态 |
| **payment_order** | `pending / paid / failed / cancelled / refunded` | **"待付款"** / **"已到账"** / **"失败"** 三态（cancelled 合入 failed；refunded 单独） |
| **outbound_webhook_delivery** | `pending / delivered / failed / dropped` × `attempt` × `response_status` | **"成功"** / **"失败"** 二态 + 一个"重试次数" |
| **vendor** | `vendor_id ∈ {91kiro, kiroceo, kirooo, kiroappio, kiroappcc, kirodrop}` | 展示名（例：`91kiro` → **"Kiro Market"**、`kirodrop` → **"Kiro Drop"**）；不暴露内部 id |
| **credential 死亡来源** | `death_source ∈ {housepool_probe, vendor_webhook, vendor_poll}` | **不显示**（用户不关心谁探到的死，只关心死了） |

### 收敛的三条规则

1. **决策性状态**才展示 —— 用户看了会做不同事的才展示（如"待付款"vs"已到账"），只是内部区分用的不展示（如 `death_source`）
2. **合并近义状态** —— `cancelled` / `failed` 用户视角都是"失败"，合并
3. **专业术语翻译成人话** —— `pool` → "号池"（乘客的），`bus-<id> group` 内部叫但对外叫"车"，`housepool` 不出现

### 违反检查（code review 时必看）

- [ ] API 返回体 / webhook 载荷里出现内部 status 枚举值？**违反**，改成映射后的
- [ ] UI 页面 / 帮助文档里出现 `housepool` / `record group` / `provider_id`？**违反**
- [ ] 状态字段超过 4 个可能值？**审查是否可以合并**（除非确实每态都要用户做不同决策）
- [ ] 用户投诉"这个状态是什么意思"？**说明状态没收敛好**

**旧项目最典型的失败**：`credential.state ∈ {preparing, standby, live, dying, dead, failed, scrapped}` 七态全暴露 → 前端加 tooltip 解释 → 又衍生"车次状态解释"帮助页 → 复杂度呈指数级。**本项目对乘客只有"活"/"死"二态**。

### 12.6 术语双分离 · 内部 vs 对外

**内部术语**（架构讨论 / 代码 / 内部文档用）：

- `housepool` / 我方号池 / kiro.rs 承载层 / `record-<pid>` group / `bus-<id>` group / provider / vendor adapter / decider / coalescer / deathwatch / pullrecord / …

**对外文案**（乘客 UI / 对外 webhook 载荷 / API 错误 message / 帮助中心）：

- **绝不出现** `housepool` / `record group` / `provider` / `adapter` / `decider` 等词
- **允许出现**：拼车 / 车 / 号 / 拉号 / 我的号池（指乘客自己的 passengerpool）/ 车友 / 车队 / 补车 / 拿走 / ……
- 乘客只知道：他有账号、钱包、能建车、能拉号、号可以进车 / 推自己号池 / 拿走 —— 中间层的技术细节他不需要知道

**如果 UI 或 API 错误 message 里出现内部术语** → **立即整改**。

**参考**：
- 对外 message / webhook / 页面文案在 `web/i18n/` (阶段 1a 起) 或代码里 hardcode 时**必须**过内部代码 review 时的"术语作废"检查
- 内部文档（`docs/*.md` / `CLAUDE.md` / `README.md`）可以用内部术语；但 `README.md` 里**面向新人开发者**的部分要平衡（术语必要时保留，但加解释）

## 13. 最后一句话

**这个项目是给一个人做的公益工具，不是软件工程师简历**。

- 不要"完整"，要"能跑"
- 不要"抽象"，要"够用"
- 不要"未来考虑"，要"当前阶段做完"
- 不要"新造名词"，要"用现有的"
- 不要"讨论"，要"决定 + 落地"

每次动代码前问自己：**"我这么做是让项目更简单，还是更复杂？"**

复杂 → 停 → 用户拍板。
