# bus-pooling · 端到端时序

> 前置阅读：[`00-values-and-phases.md`](./00-values-and-phases.md) · [`01-architecture.md`](./01-architecture.md)
> 本文只画"运行时数据流"。**不写具体表 / 函数 / 包**（那是 `03-modules.md`）；**不画层次结构**（那是 `01-architecture.md`）。
>
> 图例：`─►` 调用；`◄─` 返回；`▼` 内部推进；`⨯` 失败/无效；`【新一轮】` 触发回到某个更早步骤。
> 每条时序里的 `[N]` 标号对应下方文字说明。

## 时序索引

| # | 时序 | 触发 | 备注 |
|---|---|---|---|
| A | 兑换码充值 | 乘客输入兑换码 | |
| B | payment-gateway 充值 | 乘客点"充值" | waffo，5% pass-through |
| C | 拼车触发拉号 | 主入口：乘客建 bus + 触发拉一次 / 自动策略触发 | **拉的号直接进 bus group** |
| D | 拼车补车集单 | bus 内多成员意图合流 | 多人 bus，号价按 N 摊 |
| E | 号死 → bus 补车 | housepool 判死 / vendor webhook | bus group 跌水位触发 |
| F | 质保退款 | vendor webhook `warranty_refund` | |
| G-② | 去向 ② · 推 passengerpool | 用户从 bus 或拉号记录选"推我号池" | 双写 |
| G-③ | 去向 ③ · handoff（拿走号数据） | 用户从拉号记录选"拿走这几个" | 离开系统 |
| H | 次入口：单独拉号 | 乘客手动或 API 拉一批号 | 进 housepool 的 `record-<pid>` group，`disabled=true` |
| I | 派去向（改 group/disabled/删除） | 用户在拉号记录里对每号派去向 | 进车 / 推自己号池 / 拿走 三种任意组合 |

---

## A · 兑换码充值

```
乘客        redeem            wallet          ledger
 │            │                 │                │
 │  提交 code │                 │                │
 ├───────────►│                 │                │
 │            │  查码有效性     │                │
 │            │  ├── 无效 ⨯     │                │
 │            │  └── 有效       │                │
 │            │       ▼         │                │
 │            │  预留原子锁     │                │
 │            │  ────────────►  │                │
 │            │                 │  balance += X  │
 │            │                 │  ─────────────►│  reason=redeem
 │            │                 │                │  amount=+X
 │            │  ◄───── ok ─────┤                │
 │            │  标记码已用     │                │
 │  ok / 余额 │                 │                │
 │◄───────────┤                 │                │
```

**关键点**：

- 码有效性 + 扣码 + 加积分 + 落流水在**一个事务**里
- 兑换码**不走通道费**（本地兑换，不涉及 payment-gateway）
- `wallet_ledger.reason = "redeem"`

---

## B · payment-gateway 充值（waffo）· 通道费加在本金上

**口径**（CLAUDE.md §1.4）：积分是单位不是币种，`1 积分 ≡ 1 CNY` 基准，汇率 `7 CNY / USD`。
通道费 = 目标积分 × 5%，**加在本金上**。乘客想充 N 积分 → 支付 `(N × 1.05) / 7` USD。

**举例 · 乘客想充 100 积分**：

```
乘客       payment       payment-gateway     wallet       ledger
 │            │              (外部)            │            │
 │ 点"充值"   │                                │            │
 │ 目标 100 积分                               │            │
 ├───────────►│                                │            │
 │            │  下单：目标 100，通道费 5      │            │
 │            │  → 应付 (100+5)/7 ≈ 15 USD     │            │
 │            ├──────────────────────────────► │            │
 │            │◄──── 收款链接/二维码 ──────────┤            │
 │◄─ 链接/QR ─┤   （waffo 界面显示 15 USD）    │            │
 │            │                                │            │
 │  (线下扫码支付 15 USD ≈ 105 CNY)            │            │
 │────────────────────────────────────────►    │            │
 │                                             │            │
 │            │◄──── webhook: paid ─────────── ┤            │
 │            │                                │            │
 │            │  内部换算（记账走积分单位）：  │            │
 │            │    毛入账 +105 积分            │            │
 │            │    通道费  −5  (pass-through)  │            │
 │            │    净到手  +100                │            │
 │            │                                │            │
 │            │  balance += 105                │            │
 │            ├──────────────────────────────► │            │
 │            │                                │  +105      │
 │            │                                │ ─────────► │  reason=recharge
 │            │                                │            │
 │            │  balance -= 5  (pass-through)  │            │
 │            ├──────────────────────────────► │            │
 │            │                                │  −5        │
 │            │                                │ ─────────► │  reason=channel_fee
 │            │                                │            │
 │  余额净 +100 积分                           │            │
 │◄───────────┤                                │            │
```

**关键点**：

- 支付展示单位是 **USD**（走 waffo），内部记账单位始终是**积分**
- `wallet_ledger` 记**两笔真的动 balance**：`recharge +105` 和 `channel_fee −5`（净 +100）
- 幂等：以 `payment-gateway.order_id` 为幂等键，webhook 重投不重复入账
- 失败 / 超时 / 取消：payment-gateway 状态告知，我方**不加积分不落流水**
- 对外前端只看到 `type: topup`（两笔明细都是 topup 类型 · `05-api-contract §3`）

---

## C · 拼车触发拉号（主入口）

**场景**：乘客建了 bus X（1 人或多人），点"给这个 bus 拉一次 5 个号"。号**直接进 bus group**。

```
乘客     bus          strategy       decider     provider-adapter   vendor    housepool
 │        │              │              │              │              │           │
 │ 主入口拼车                                                                       │
 │ bus=X, count=5, vendor=?                                                         │
 ├──────►│               │              │              │              │           │
 │        │  验证 bus 存在                                                          │
 │        │  查成员 & 目标水位                                                      │
 │        ├─────────────►│                                                          │
 │        │              │  判可行：钱够 / 未触上限                                 │
 │        │              │  意图 { bus_id: X, count:5 }                             │
 │        │              ├─────────────►│                                          │
 │        │              │              │  拉 6 家快照 [1]                          │
 │        │              │              │  归一算价 [2]                             │
 │        │              │              │  健康过滤 [3]                             │
 │        │              │              │  选中 vendor=Y, price=P                   │
 │        │              │              │                                           │
 │        │              │              │  purchase(count=5, client_order_id=...)   │
 │        │              │              ├──────────────►│                          │
 │        │              │              │               ├─────────────►│           │
 │        │              │              │               │◄──── keys ───┤           │
 │        │              │              │  归一化响应 [4]                           │
 │        │              │              │◄──────────────┤                          │
 │        │              │              │                                           │
 │        │              │              │  号价 P × 5 pass-through                  │
 │        │              │              │  按 bus 成员比例分摊                       │
 │        │              │              │  服务费按 §3 那条链算（各成员按 share_pct 分摊）                       │
 │        │              │              │  → wallet 扣 + ledger 落                   │
 │        │              │              │                                           │
 │        │              │              │  BatchImport → housepool                  │
 │        │              │              │  **直接进 group: bus-<X>**                │
 │        │              │              ├──────────────────────────────►│           │
 │        │              │              │                                           │
 │        │              │              │  bus 成员配了"推自己号池"的 → 见 G-②       │
 │        │              │              │                                           │
 │◄── 通知：bus X 补了 5 个号 ──                                                    │
```

**关键点**：

- **拼车触发的号直接进 bus group** —— 不经过"拉号记录"，一步到位入池监控
- **[1]** 快照来源：vendor 的 `GET /stock`（各家档案 §6）实时或缓存
- **[2]** 归一算价：跨币种（CNY/USD/积分）+ 跨定价（区间/阶梯/固定）→ 一个"每 key 有效积分成本"
- **[3]** 健康过滤：`stock == 0` / 最近 `all_keys_dead` 频繁 / 429 / 5xx 近期比例超阈值
- **[4]** 归一化：Layer 2 adapter 把 vendor 特有字段变成内部 struct
- **1 人 bus 也走这条**（1 人建 bus，触发拉号，号进 `bus-<X>`）；跟 N 人 bus 无本质差异
- 乘客可**指定 vendor**（跳过 3d 决策）；也可让我们选（走 3d）
- 全过程扣积分在 **BatchImport 成功之后**（防止号价扣了但号进不了池）
- 号进 bus 后，成员各自的"推自己号池"配置决定是否触发 G-② 双写

---

## D · 拼车补车集单（多成员合流）

**前提**：bus X 有 3 位成员 A/B/C，都开了自动策略。同一 bus 内多成员的补车意图在窗口内合流成一次拉号。

```
[触发源 · 三种任一]                                策略引擎
 ├── 时钟：定时轮询                                    │
 ├── 水位：housepool 该 bus 分组存活数跌破 threshold   │
 └── 事件：vendor webhook new_keys_available           │
                                                       │
                                                       ▼
       strategy   coalescer   decider   provider-adapter  vendor   housepool
          │          │           │            │              │         │
          │  意图池收 [1]                                              │
          │  A: bus=X, want 10                                          │
          │  B: bus=X, want 8                                           │
          │  C: bus=X, want 12                                          │
          ├─────────►│                                                  │
          │          │  bus 维度合流 [2]                                │
          │          │  同 bus_id 才合                                  │
          │          │  窗口 T 秒 / M 需求                              │
          │          │       ▼                                          │
          │          │  合流成一笔                                      │
          │          │  { bus_id: X, count_total: 30,                   │
          │          │    participants: [A:10, B:8, C:12] }             │
          │          │                                                  │
          │          ├──────────►│  同 C.[1..4]                         │
          │          │           │  purchase(30, client_order_id=...)   │
          │          │           ├──────────►│                          │
          │          │           │           ├───────►│                 │
          │          │           │           │◄── keys┤                 │
          │          │           │◄──────────┤                          │
          │          │           │                                      │
          │          │           │  号价 P × 30 pass-through            │
          │          │           │  按 participants 分摊 [3]            │
          │          │           │  A: P × 10/30                        │
          │          │           │  B: P × 8/30                         │
          │          │           │  C: P × 12/30                        │
          │          │           │  服务费 A/B/C 各 1（不分摊）         │
          │          │           │                                      │
          │          │           │  BatchImport → housepool             │
          │          │           │  **直接进 bus-X group**              │
          │          │           ├─────────────────────►│               │
          │          │           │                                      │
          │          │           │  成员各自配了"推自己号池"的 → G-②     │
```

**关键点**：

- **拼车触发的号直接进 bus group** —— 没有"暂存"、"未分配区"、"分配步骤"这些中间态
- **[1]** 意图池：每个乘客的补车意图带 `bus_id`（属于哪辆 bus）；1 人 bus 也在此
- **[2]** **合流仅在同 bus 内**：A/B/C 都在 bus X → 合并；乘客 D 在 bus Y → 独立合流。窗口大小是全局配置或按 vendor 阶梯档动态算
- **[3]** 号价分摊：一批 30 把号价 = P，按 A/B/C 的 `count` 比例记账；每笔独立走 ledger
- **服务费不分摊**：A/B/C 各付 1（例）
- **1 人 bus 的补车**：意图直发 decider，绕开 coalescer（一人独家，无合流对象）

---

## E · 号死 → bus 分组重整 → 新一轮补车

```
housepool  deathwatch   bus   strategy    coalescer    decider
   │           │          │       │           │            │
   │  号 K1 判死                                            │
   │  (探活 or vendor                                       │
   │   webhook)                                             │
   ├──────────►│                                            │
   │           │  从 bus 分组踢出 K1                        │
   │           │  ─────────────►                            │
   │           │      (kiro.rs disable client_key /         │
   │           │       修改 group)                          │
   │           │                                            │
   │           │  查该 bus 剩余存活                         │
   │           ├─────────►│                                 │
   │           │◄─────────┤ 剩余 count                      │
   │           │                                            │
   │           │  ├── 还多于水位 → 停 ✓                     │
   │           │  └── 跌破水位（补车触发） → 下一步         │
   │           │                                            │
   │           │  发补车意图 [1]                            │
   │           │  {bus_id, want: target-current}            │
   │           ├────────────────►│                          │
   │           │                 │  意图池收                │
   │           │                 ├───────────►│             │
   │           │                 │            │  bus 维度   │
   │           │                 │            │  合流 → D   │
```

**关键点**：

- **[1]** deathwatch 只发**补车意图**（带 bus_id），不直接调 vendor。**补车走完整的策略→集单→决策→拉号→上车链条**（保证策略上限、集单机会都不错过）
- 号在分组里的 client_key **立即禁用**，避免乘客拿到死号发 API
- 多人 bus 的补车意图落进意图池后，**该 bus 全体成员的意图会一起集单**（成员之间同 bus，天然合流）
- 1 人 bus 的补车意图**只属于该乘客**，绕开 coalescer 直发 decider
- 触达"每日轮次上限 / 每日花费上限" → **不再自动补**，等次日或人工干预
- **拿走的号（去向 ③）**：号已经离开系统，housepool 无副本，号死我方无从知晓——用户自己发现
- **推 passengerpool 双写的号（去向 ②）**：housepool 副本仍在，走本时序踢死号 + 触发补车

---

## F · 质保退款

```
vendor       webhookin      deathwatch     wallet       ledger
 │              │              │              │            │
 │ POST /hook   │              │              │            │
 │ event=       │              │              │            │
 │ warranty_    │              │              │            │
 │ refund       │              │              │            │
 │ round_id=…   │              │              │            │
 │ refund=Q     │              │              │            │
 ├─────────────►│              │              │            │
 │              │  验签 [1]    │              │            │
 │              │  归一化事件  │              │            │
 │              │  {vendor,    │              │            │
 │              │   order_id,  │              │            │
 │              │   refund_    │              │            │
 │              │   amount}    │              │            │
 │              │              │              │            │
 │              │  转发 [2]    │              │            │
 │              ├─────────────►│              │            │
 │              │              │  查 order_id 对应的       │
 │              │              │  参与人 & 各自的比例      │
 │              │              │              │            │
 │              │              │  按比例退                 │
 │              │              │  A: +Q × 10/30            │
 │              │              │  B: +Q × 8/30             │
 │              │              │  C: +Q × 12/30            │
 │              │              ├─────────────►│            │
 │              │              │              │  balance   │
 │              │              │              │  +...      │
 │              │              │              │ ─────────► │  reason=
 │              │              │              │            │  warranty_refund
 │              │              │              │            │  ref=order_id
 │              │              │                           │
 │              │              │  服务费**不退** [3]       │
 │              │              │                           │
 │◄─ 200 ok ────┤              │                           │
```

**关键点**：

- **[1]** 验签：91kiro 用 `sha256=` HMAC；drop 用 `v1=` HMAC；kiro.ooo 不签名靠 URL secret；kiro.ceo / kiroapp.cc / kiroapp.io 各家规则（见 vendor 档案 §10-11）
- **[2]** 通用事件形态 `{vendor, order_id, refund_amount}`，各家字段名归一（`purchase_order_id` / `order_id` / `refunded_quota` / ... 都映射到 `refund_amount`）
- **[3]** 服务费不退（§00.7.5 规则 B）：因为服务本身已交付（拉号动作发生了）
- **通道费不退**：payment-gateway 层面的费已经交 waffo 了，与本次质保无关
- 幂等：以 `(vendor, event_id)` 为去重键
- 上游过窗口**不推 warranty_refund** → 我方无输入 → **不退**（跟随规则 B）

---

## G-② · 去向 ② · 推 passengerpool（双写）

**场景**：号在 housepool 的某个 group（bus 或拉号记录）里，乘客配了"推自己号池"，触发把这号**复制**到他的 passengerpool。

```
触发源（bus 补了号 / 用户从拉号记录选"推我号池"）
 │
 ▼
delivery       passengerpool.kirors      乘客的号池（外部）
   │              (客户端)                      │
   │  查该乘客的下游配置                        │
   │  {url, token, group}                       │
   │              │                             │
   │  BatchImport 归一化的 credential           │
   ├─────────────►│                             │
   │              ├────────────────────────────►│
   │              │◄──────── 200 ok ────────────┤
   │◄────── ok ───┤                             │
   │                                            │
   │  **housepool 副本保留** —— 监控继续       │
```

**关键点**：

- **双写**：号复制到 passengerpool；**housepool 副本保留** 用于监控（Layer 4 5 项能力覆盖）
- 号在乘客那侧被消费；housepool 副本**只读监控**，我方不能拿来消费（否则上游双扣）
- 推送失败：重试 3 次；仍失败通知乘客
- housepool 副本判死 → 触发 E（bus 补车 或 用户拉号记录里标失效）→ 新一轮 → 再触发 G-②

---

## G-③ · 去向 ③ · handoff（拿走号数据）

**场景**：乘客从拉号记录里选几个号"拿走"，或调 API 直接下载号数据。

```
乘客/API           delivery/handoff        housepool         数据库
   │                    │                     │                │
   │ handoff 请求        │                     │                │
   │ credentials: [...] │                     │                │
   ├───────────────────►│                     │                │
   │                    │  校验：号归该乘客 & 在拉号记录里      │
   │                    ├────────────────────────────────────►│
   │                    │◄────────────────────────────────────┤
   │                    │                                      │
   │                    │  返回 credential 明文               │
   │◄───── 明文 ────────┤                                      │
   │                    │                                      │
   │                    │  从 housepool 删除                   │
   │                    ├────────────────────►│                │
   │                    │                                      │
   │                    │  数据库：拉号记录标记 "已 handoff"   │
   │                    ├────────────────────────────────────►│
```

**关键点**：

- **号离开系统**：数据交给乘客后 housepool 删除，数据库仅留一条"已 handoff"历史（不留 credential 明文）
- **我方不再监控**：号死无从知晓
- 乘客拿走后自己处理（转发他人 / 存自己服务 / 导入别的地方）—— **我方不关心**
- 服务费按拉号时收，handoff 不再另收
- **这是唯一"发了不管"的路径**

---

## H · 次入口：单独拉号（号进 housepool `record-<pid>` group）

**场景**：乘客点"单独拉几个号"（不指定 bus），或调 API `POST /pull` 拉一批。号**进 housepool**，进 `record-<pid>` group，`disabled=true`（不发下游），等用户派去向。

```
乘客/API      strategy       decider     provider-adapter   vendor    housepool
 │              │              │              │                │         │
 │ 单独拉号                                                              │
 │ count=10, vendor=?                                                    │
 ├─────────────►│              │              │                │         │
 │              │  判可行（钱够 / 未触上限）                              │
 │              │  意图 { count:10, target: record-<pid> }                │
 │              ├─────────────►│                                         │
 │              │              │  同 C.[1..4]                            │
 │              │              │  purchase(10)                           │
 │              │              ├───────────►│                            │
 │              │              │            ├───────►│                   │
 │              │              │            │◄─keys ─┤                   │
 │              │              │◄───────────┤                            │
 │              │              │                                         │
 │              │              │  号价 × 10 pass-through                 │
 │              │              │  服务费 = 链上最后一层的增量（`decisions §8.34`）              │
 │              │              │  → wallet 扣 + ledger 落                │
 │              │              │                                         │
 │              │              │  BatchImport → housepool                │
 │              │              │  { groups: [record-<pid>],              │
 │              │              │    disabled: true }                     │
 │              │              ├─────────────────────────────►│           │
 │              │              │                                         │
 │◄── 10 个号已入 record-<pid> group，等派去向 ──                            │
```

**关键点**：

- **号已进 housepool**（唯一物理位置）；靠 `record-<pid>` group + `disabled=true` 标"未派"状态
- **已监控**：Layer 4 5 项能力全覆盖（校验、探活、用量、并发、组统计）—— 平均寿命也能采到这些号，数据价值更高
- **不发下游**：`disabled=true`，即使有 client_key 也发不到这些 credential
- **过期定义 = 号本身死了**（不是 N 天）：靠 housepool 探活 / vendor webhook / vendor 端点轮询（见 §00.8 术语"平均寿命"）
  - 死号处理：housepool 保留（`dead` 状态由 kiro.rs 标记），UI 里显示"已失效"，不删除（留历史）
  - 用户可手动清理死号（走 handoff 的 DELETE 路径，但不给明文，因为号已经无用）
- **用户派去向**见时序 **I** ▼

---

## I · 派去向（改 group + disabled，或删除）

**场景**：乘客 A 的 `record-<pid>` group 有 10 个号；派 5 个进 bus X，2 个推自己号池，3 个拿走。

```
乘客/API        pullrecord       housepool             delivery
   │              │              (kiro.rs)                │
   │  Assign(passenger, plan)                             │
   │  plan: { bus-X: 5, passengerpool: 2, handoff: 3 }    │
   ├─────────────►│                                       │
   │              │                                       │
   │  === 5 个进 bus X ===                                 │
   │              │  PUT /credentials/{id} × 5             │
   │              │  { groups: [bus-X], disabled: false }  │
   │              ├──────────────►│                       │
   │              │◄──── ok ──────┤                       │
   │              │                                       │
   │  === 2 个推 passengerpool（双写）===                  │
   │              │  取 credential 明文                    │
   │              ├──────────────►│                       │
   │              │◄─── creds ────┤                       │
   │              │  触发去向 ② 复制到 passengerpool        │
   │              ├─────────────────────────────────────►│
   │              │◄──── ok ──────                          │
   │              │  PUT /credentials/{id} × 2             │
   │              │  { disabled: false }（保留 record group）│
   │              ├──────────────►│                       │
   │              │◄──── ok ──────┤                       │
   │              │                                       │
   │  === 3 个 handoff（拿走）===                          │
   │              │  取 credential 明文                    │
   │              ├──────────────►│                       │
   │              │◄─── creds ────┤                       │
   │              │  触发去向 ③ 返回给用户                  │
   │              ├─────────────────────────────────────►│
   │              │                                       │
   │              │  DELETE /credentials/{id} × 3         │
   │              ├──────────────►│                       │
   │              │◄──── ok ──────┤                       │
   │              │                                       │
   │              │  数据库：数据库落"已 handoff"历史      │
   │              │  （不留明文）                          │
   │              │                                       │
   │◄── 分派完成 ─┤                                       │
```

**关键点**：

- **一次派可以混合三种去向**（进车 + 推自己号池 + 拿走），每号独立决定
- **进车 / 推自己号池**：号仍在 housepool（改 group / 改 disabled，不删）→ 我方持续监控
- **handoff（拿走）**：`DELETE /credentials/{id}` → 号离开 housepool + 明文交给用户
- **原子性**：状态转换的顺序 **先外部交付、后 housepool 状态修改**：
  - handoff：先返回明文给用户 → 再 `DELETE`
  - 推自己号池：先 `BatchImport` 到 passengerpool → 再 `PUT disabled=false`
  - 进车：改 group + disabled 是 kiro.rs 内一步事务
- **credential 其它字段保留**（`priority` / `concurrency_limit` / `endpoint_policy` 等）：`PUT` 只改指定字段
- 派完后：
  - `bus-X` group 里的号 → 参与 bus X 补车集单（时序 D）；号死走时序 E
  - `record-<pid>` group 里 `disabled=false` 的号 → 已复制到 passengerpool；号死时 housepool 副本判死 + webhook 通知乘客

---

## 跨时序共享的不变量

- **积分账房**：所有扣款 / 退款 / 通道费 / 服务费**全部**走 wallet + ledger，无其它路径
- **计费时点**：号价扣款在 kiro.rs BatchImport 成功之后；服务费扣款在同事务；通道费在 payment-gateway webhook `paid` 时确认
- **幂等键**：
  - 充值：`payment-gateway.order_id`
  - 拉号：`client_order_id` (32 hex, 见各 vendor 档案 §7)
  - 质保 webhook：`(vendor, event_id)`
- **失败即回滚**：任何一环失败，先前的扣款事务回滚，号不入池、积分不减
- **去向 ① 进车**：号在 housepool bus group，跟踪
- **去向 ② 推 passengerpool**：**双写**——housepool 副本跟踪监控
- **去向 ③ handoff**：号离开系统，我方不再监控（**唯一"发了不管"的路径**）
