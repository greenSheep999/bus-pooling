# bus-pooling · 分层架构

> 前置阅读：[`00-values-and-phases.md`](./00-values-and-phases.md)
> 本文只画"层"和"每层的边界"。**不写具体的表、函数、包**（那是 `03-modules.md`）；**不写运行时时序**（那是 `02-flows.md`）。

## 1. 分层俯视图

```
┌───────────────────────────────────────────────────────────────┐
│         Layer 5 · 出货（去向 ② / ③ 的实现）                    │
│                                                               │
│   去向 ② 推 passengerpool ── 双写：housepool 副本 + 乘客 kiro.rs│
│   去向 ③ handoff（拿走）── 号数据交出，离开系统                │
│                                                               │
│   （入口是 UI 或 API，非 Layer 5 的事；见 §2）                 │
│   （去向 ① 进车不算"出货"——号仍在 housepool 里）              │
└───────────────────────────────────────────────────────────────┘
                              ▲
┌───────────────────────────────────────────────────────────────┐
│                Layer 4 · housepool（我方号池）                │
│         我方运维的 kiro.rs (kiro.aibbq.xyz)                   │
│         职责：credential 存储 / 分组 / client_key / 存活监控  │
│         不复刻这些能力，全部委托 kiro.rs                      │
└───────────────────────────────────────────────────────────────┘
                              ▲
┌───────────────────────────────────────────────────────────────┐
│                   Layer 3 · bus-pooling 本体                  │
│                                                               │
│   3a · 乘客账号 · 积分钱包 · 兑换码 · 通道费 · 计费流水       │
│   3b · 策略引擎（手动/自动/上限/上车规则/补车规则）           │
│   3c · 集单调度器（bus 维度补车意图合流）                     │
│   3d · 决策模型（跨 vendor 比价 / fallback）                  │
│   3e · 号死监控 + 质保退款处理（跟随上游）                    │
│   3f · 对外 webhook 出向（推乘客）                            │
│   3g · 上车分配器（拉到号后决定去哪个 bus）                   │
│   3h · bus 实体管理（匿名撮合 + 邀请码组队）                  │
└───────────────────────────────────────────────────────────────┘
                              ▲
┌───────────────────────────────────────────────────────────────┐
│         Layer 2 · provider / vendor 抽象                      │
│                                                               │
│   provider = kiro（当前唯一 · 未来可加 cursor 等）            │
│   ├─ 6 家 vendor 客户端（每家一份 adapter）                   │
│   └─ 6 家 vendor webhook 接收器（归一化事件）                 │
└───────────────────────────────────────────────────────────────┘
                              ▲
┌───────────────────────────────────────────────────────────────┐
│              Layer 1 · 上游 vendor（外部）                    │
│    91kiro · kiro.ceo · kiro.ooo · kiroapp.io · .cc · drop     │
└───────────────────────────────────────────────────────────────┘
```

**读法**：数据自下（vendor）而上（乘客交付）流动；调用自上（乘客触发）而下发起。

## 2. 每层职责

### Layer 1 · 上游 vendor（外部，不在我方代码里）

- **是什么**：6 家 kiro vendor，各自域名 / 契约 / 币种 / 幂等 / 质保规则
- **契约来源**：`docs/vendors/*.md` 六份档案
- **对我方的语义**：**进货渠道**，不是"合作方"也不是"竞品"
- **我方的假设**：vendor 契约会**独立演化**（半年内可能改字段、加事件、变签名）—— Layer 2 adapter 是隔离带

### Layer 2 · provider / vendor 抽象

- **两级抽象**：
  - **provider**（协议族） —— 当前唯一 `kiro`；未来可能 `cursor` 等
  - **vendor**（同 provider 下的具体供应源） —— kiro 下 6 家 vendor
- **顶层契约在 `providers/provider.go`**：跨 provider 共享的最小接口
- **provider 层契约在 `providers/kiro/kiro.go`**：kiro 家族特有的公共约定（如 `zone: us/eu`、`ksk_` key 形态等）
- **每家 vendor 一个 adapter**，实现 provider 契约（详见 `03-modules.md`）
- **输入**：Layer 3 的"拉号 / 查库存 / 查价 / 补拉 / webhook 事件"抽象调用
- **输出**：内部归一化 struct（跨 vendor 一致的字段）
- **不做**：不做决策、不做策略、不做集单、不做计费、不持任何状态。**纯翻译**
- **边界**：**adapter 不 import Layer 3 的任何包**（可以被替换 / mock）；**加 Cursor 只加 `providers/cursor/`，不动 `providers/kiro/`**

### Layer 3 · bus-pooling 本体（核心）

按内部功能再分 6 个子层（3a-3f）。每个子层**是一个独立包**，包间调用走接口，不循环依赖。

#### 3a · 乘客账号 · 积分钱包 · 兑换码 · 充值渠道 · 计费流水
- 乘客身份与登录（含 SuperTokens 或类似）
- 积分账户（存余额、扣费、退款）
- 兑换码兑换（阶段 1b）
- **payment-gateway 客户端**（外部服务，waffo 通道，5% 通道费 pass-through 给乘客）
- 计费流水（含 §00.3 里的号价 pass-through 记账、服务费固定记账、通道费记账、质保退款）
- **是所有其它子层的"账房"**：任何拉号动作最终都在这里落一笔账

#### 3b · 策略引擎
- 存乘客的策略参数：`{ auto_enabled, per_round_count, min_count, keep_safety_stock, max_unit_price, daily_round_limit, daily_spend_limit, default_boarding_bus, allocation_rule }`（无 carpool|solo 二选一——上车目标由 `default_boarding_bus` + `allocation_rule` 决定，可混合）
- 判断"当下能不能拉"：查策略上限 → 查钱包 → 返回可 / 不可
- **只判断，不动手**：动手是 3c/3d 的事

#### 3c · 集单调度器（coalescer）
- **集单发生在 bus 维度**：同一 bus 内多成员的补车意图，在窗口内合流成一次拉号意图
- **不跨 bus 合并**：不同 bus 各自集单，避免抢资源 / 分账混乱
- **1 人 bus 不集单**：一乘客一意图直发 decider（无合流对象）
- 两种子模式：
  - `coalescer/anon.go` —— 匿名撮合的 bus 用（阶段 1）
  - `coalescer/team.go` —— 邀请码组队的 bus 用（阶段 2）
- 窗口触发：**先到者胜** —— N 秒到期 或 积攒到 M 需求
- 输出**一个批量拉号意图**：`{bus_id, participants[], count_total}`
- **不选 vendor**（那是 3d） · **不实际调 vendor**（那是走 Layer 2）

#### 3d · 决策模型
- 输入：一个拉号意图 + 6 家 vendor 实时快照 + **6 家平均寿命统计**
- 决策维度：
  - **单价**：跨 vendor 归一算价（CNY/USD/积分/阶梯/手续费 → "每 key 积分成本"）
  - **平均寿命**：从历史号 `dead_at - created_at` 采样算得（近 N 天），得"每 vendor 号平均活多久"
  - **有效成本** = 单价 / 平均寿命 = **每积分能活的时长**（真正的比价维度）
  - **筛选**：按策略上限（`max_unit_price`）过滤
  - **健康**：按存活 / 缺货信号过滤
  - **Fallback**：主选挂了走次选
- 输出：一个具体的 vendor 选择 + 幂等键 + 调用参数
- **不发请求**（发请求是 Layer 2 的事）
- **平均寿命数据来源**：`deathwatch` 记录的号死时间 - `providers` 记录的拉号时间；聚合成"vendor × 最近 N 天 → 平均寿命"表

#### 3e · 号死监控 + 质保退款 + 寿命统计
- 三个信号源，任一触发号"判死"：
  - **kiro.rs 探活**（housepool 5 项能力之一，Layer 4 提供）
  - **vendor 主动 webhook**（`all_keys_dead` / `warranty_refund` / `key_revoked_abuse` / `on_key_suspect` —— 各家事件名不同，见档案 §10-11）
  - **定时轮询 vendor 死活端点**（drop.kiro.ss `/api/status`、91kiro `/api/my/rounds`、kiro.ooo `/my/dispatch-log`）—— 兜底
- 号死落库：记 `credential.dead_at`（+ `death_source`：kiro.rs 探活 / vendor webhook / vendor 轮询）
- 规则 A：号死 → 从分组踢出 → 分组全死 → 触发新一轮（回到 3b/3c/3d）
- 规则 B：上游退我方 → 我方按乘客比例退积分（走 3a 记账）
- **平均寿命统计**：从 `credential.created_at → dead_at` 聚合，按 (vendor, 时间窗口) → 平均寿命，供 3d 决策
- **不担保号本身质量**（§00.1、§00.7.5）

#### 3f · 对外 webhook 出向
- 我方给乘客推的 webhook：`new_keys_available` / `all_keys_dead` / `refund` / `boarded` / …
- 载荷形态是"我们的形态"，不是任何单一 vendor 的形态

#### 3g · 拉号记录（pullrecord）
- **单独拉号入口的暂存表**：单独拉号后号进这里，`status: unassigned`
- 用户后续对每号选去向：
  - 进 bus → 走 3d/3h 移到 `bus-<bus_id>` group
  - 推 passengerpool → 走 Layer 5 双写
  - 拿走 → 走 Layer 5 handoff（离开系统）
- **注意**：拉号记录**不在 housepool**，不监控；号一旦有去向，才进相应 group 或离开
- **拼车触发的号不经过这里**（直接进 bus group）

#### 3h · bus 实体管理
- bus 是**长期存在的实体**（成员加入/退出 · 补车规则 · 目标水位 · 分账规则 · 邀请码）
- **1 人 bus 也是 bus**（一乘客自建，自动或手动创建）
- **多人 bus** 两种加入方式：
  - 匿名撮合：乘客声明"要拼"，系统撮合到已存在的 bus 或新建（阶段 1）
  - 邀请码组队：乘客建 bus 拿邀请码，其他乘客输码加入（阶段 2）
- housepool 里每个 bus 对应一个 `group = bus-<bus_id>`
- **不管 credential 具体在不在**（那是 housepool 的事）；**不管服务费**（那是 3a）

### Layer 4 · housepool（我方号池）

- **是什么**：**我方**运维的号池，当前部署 = `kiro.aibbq.xyz`（一个 kiro.rs 实例）
- **不复刻**。housepool 承担的 5 项能力**全部由 kiro.rs 提供，我方不做**：
  1. **校验凭证** —— 号存进去时验证有效性
  2. **存活探测** —— 号什么时候死了
  3. **成本 / 用量追踪** —— 每 group（每辆 bus / 个人 / 市场）用了多少额度；号死时耗了多少
  4. **分组** —— 按 group 组织号（`personal-<pid>` / `bus-<bus_id>` / market group）+ `CreateClientKey` / `RotateClientKey`
  5. **并发监控** —— 每 group 的调用频率（是阶段 2c 压车治理的探测基础）
- **我方是 kiro.rs 的客户端**（Layer 3 通过 `housepool/kirors` 包调它）
- **命名理由**：抽象叫 `housepool`（我方自家的号池），具体实现叫 `kirors`。将来若换其它号池实现，只加一个 `housepool/otherimpl/`，不改上层
- **不能省**：曾讨论过"用数据库替代 housepool"—— **不可行**。数据库只能做记账（"这号归谁"），但 §1-5 的**运行时**能力（校验 / 探活 / 用量 / 并发监控）必须号池实现才有

### Layer 5 · 出货（去向 ② / ③ 的实现）

Layer 5 只处理**离开 housepool 或双写副本**的动作。**去向 ① 进车不在这里**（号仍在 housepool bus group）。

- **去向 ② · 推 passengerpool（双写）**
  - 乘客配 url + token（当前只支持乘客用 kiro.rs → `delivery/passengerpool/kirors`）
  - 做 kiro.rs → kiro.rs 的 BatchImport 复制推送
  - **housepool 里保留监控副本**（Layer 4 5 项能力继续覆盖）
  - 乘客那侧是**一份 credential 的复制**
- **去向 ③ · handoff（拿走号数据）**
  - 用户主动"我要这几个号的原始数据"
  - 号数据（credential 明文）交给用户 —— UI 下载 或 API 返回
  - **housepool 副本删除**（用户拿走后号离开系统）
  - **我方不再监控这批号**

**入口 vs 去向**：Layer 5 只关心**去向**。**入口**（UI 或 API 触发）在 Layer 3 里由 `internal/api`（HTTP 层）承担。API 不是"独立的去向" —— 它是**入口通道**，能触发上面 2 种去向 + 去向 ①（进车）任一种。

## 3. 数据流向（一次典型拉号 + 上车 + 交付）

```
[乘客触发或策略自动]                                 (Layer 3b 判可行)
        │
        ▼
[分支：主入口拼车 or 次入口单独拉号]
        │
   [主入口拼车路径]
        ▼
[集单调度合流 - bus 维度]                            (Layer 3c)
   意图: {bus_id, participants[], count}
        │
        ▼
[决策模型选 vendor]  ◄──── vendor 实时快照 (Layer 3d ← Layer 2)
        │
        ▼
[Layer 2 adapter 调 vendor purchase]                 (Layer 2 → Layer 1)
        │
        ▼
[响应归一化，得到成品 credential(s)]                 (Layer 2)
        │
        ▼
[Layer 3a 记账：号价 pass-through 分摊 + 服务费]     (Layer 3a)
        │
        ▼
[BatchImport 进 housepool，group = bus-<bus_id>]     (Layer 3 → Layer 4)
   拼车触发的号直接进 bus group
        │
        └── 成员配了"推自己号池"的 → 去向 ② 双写      (Layer 5)


   [次入口单独拉号路径]
        ▼
[decider → adapter → vendor purchase → 记账]
        │
        ▼
[写"拉号记录"数据库表]                               (Layer 3g)
   status: unassigned；不进 housepool，不监控
        │
        ▼
[用户后续对每号选去向]
        ├── 进车 → 移到 housepool bus-<bus_id>       (Layer 4)
        ├── 推自己号池 → 去向 ② 双写                  (Layer 5)
        └── 拿走 → 去向 ③ handoff                    (Layer 5)
```

## 4. 边界约束（每层不做什么）

- **Layer 2** 不做决策 / 不做记账 / 不 import Layer 3；加 provider 只加 `providers/<name>/`，不动其它 provider 一行
- **Layer 3a**（钱包）不知道 vendor 是谁；只处理积分与流水
- **Layer 3b**（策略）不发请求 / 不动 housepool
- **Layer 3c**（集单）不选 vendor / 不算价 / 不分配号（分配是 3g）
- **Layer 3d**（决策）不发请求 / 不记账
- **Layer 3e**（号死）不发拉号请求（触发的是"新一轮意图"，交给 3b/3c/3d 走完整链）
- **Layer 3g**（分配）不选 vendor / 不发请求 / 不管 bus 成员关系（那是 3h）
- **Layer 3h**（bus）不管号具体在不在（那是 housepool）/ 不管扣费（那是 3a）
- **Layer 4 housepool** 不做业务规则（拼车、集单、比价、策略都不在 kiro.rs 里）
- **Layer 5 delivery** 不做扣费（扣费在 3a）；`passengerpool` 对我方是外部系统，性质同 vendor

## 5. 顶层模块清单（预告，详见 `03-modules.md`）

```
internal/
├── providers/                  Layer 2 · provider/vendor 抽象
│   ├── provider.go             provider 顶层契约（跨 provider 共享）
│   └── kiro/                   provider = kiro（当前唯一）
│       ├── kiro.go             kiro provider 层公共契约
│       └── vendors/            kiro 下 6 家具体 vendor
│           ├── kiro91/
│           ├── kiroceo/
│           ├── kirooo/
│           ├── kiroappio/
│           ├── kiroappcc/
│           └── kirodrop/
├── webhookin/                  Layer 2 · vendor webhook 归一化
├── passenger/                  Layer 3a · 账号
├── wallet/                     Layer 3a · 积分钱包 + 流水
├── redeem/                     Layer 3a · 兑换码
├── payment/                    Layer 3a · payment-gateway 客户端（waffo）
├── strategy/                   Layer 3b · 策略引擎
├── coalescer/                  Layer 3c · 集单调度器（bus 维度）
│   ├── anon.go                 匿名撮合（阶段 1）
│   └── team.go                 邀请码组队（阶段 2）
├── decider/                    Layer 3d · 决策模型
├── deathwatch/                 Layer 3e · 号死监控 + 质保退款
├── webhookout/                 Layer 3f · 对外 webhook
├── pullrecord/                 Layer 3g · 拉号记录（次入口暂存表）
├── bus/                        Layer 3h · bus 实体管理（含匿名撮合 + 邀请码组队）
├── housepool/                  Layer 4 · 我方号池（存/管/监控）
│   └── kirors/                 具体实现 = 我方运维的 kiro.rs 客户端
└── delivery/                   Layer 5 · 出货（去向 ②/③）
    ├── passengerpool/          去向 ② · 推乘客号池（双写）
    │   └── kirors/             当前只支持乘客用 kiro.rs
    └── handoff/                去向 ③ · 拿走号数据（离开系统）
```

**上限约束**：`internal/` 顶层目录**不超过 15 个**。旧项目 90+ 的痛，是每加一个业务概念就开一个包。本项目**新加包必须能解释为什么不能放进已有包**。

## 6. 与旧项目的对照

旧项目 `kiro-auto` 的 `internal/` 有 90+ 目录（`rideorder / ridegroup / trip / tripgroup / passenger / passengersupply / passengerauth / matching / matchingaccept / ...`），本项目**明确不做**：

| 旧项目模块 | 本项目对应 |
|---|---|
| `internal/vendors/*` | **保留但重构**：加 provider 抽象层 → `internal/providers/kiro/vendors/*` |
| `internal/kiroclient/*` | **保留但重命名**：号池语义分层 → `internal/housepool/kirors/*`（我方号池的 kiro.rs 客户端） |
| `internal/{rideorder,ridegroup,matching,ridegroupjoin,ridegroupmanage,...}` | **不做**（旧项目 15+ 个包）：bus 相关全在 `internal/bus/` + `coalescer/` + `allocation/` 三个包完成 |
| `internal/{trip,tripgroup,tripcredential,ridehandoff,...}` | **不做**：交付一层（Layer 5）解决 |
| `internal/{exclusivemother,supplybatch,supplyfulfillment,supplycatalog,...}` | **不做**：发车（乘客上传 AWS）延后到阶段 3b/3c，届时只加一个薄薄的转发层，不复刻母号管理 |
| `internal/{passenger,wallet,points,...}` | **保留**：合并为 3a 一个子层 |
| `internal/{tokensheep,newapi,apilane,...}` | **不做**：不是拼车产品线的一部分 |
| `internal/{payments,404bus-payment-gateway,...}` | **不做**：复用外部 payment-gateway 服务（waffo 通道已打通），本项目只做 client → `internal/payment/` |
| `internal/{connectors,contract,forecast,replenisher,...}` | **不做**：过度抽象 |
