# bus-pooling · 用户场景路径

> 前置阅读：[`00-values-and-phases.md`](./00-values-and-phases.md) · [`02-flows.md`](./02-flows.md)
> 本文是**产品视角**的用户场景清单。每场景写"乘客点了什么 / 发生了什么 / 钱怎么算 / 号最终在哪"。
> 技术时序（服务间调用图）在 [`02-flows.md`](./02-flows.md)；本文每场景**指向**它的技术时序编号。

## 场景索引（29 条）

按**用户行为**分组，不按技术模块。

### A · 拼车（主入口）
- A1 建 1 人 bus + 手动拉一次
- A2 建多人 bus（匿名撮合）+ 拉一次
- A3 建多人 bus（邀请码）+ 拉一次
- A4 加入他人的多人 bus（匿名撮合）
- A5 加入他人的多人 bus（邀请码）
- A6 退出 bus
- A7 解散 bus
- A8 手动拼车 vs 自动拼车（策略触发）

### B · 单独拉号（次入口）
- B1 只拉号，不派去向（自留在 record group）
- B2 拉号 → 派进 bus
- B3 拉号 → 推自己号池（双写）
- B4 拉号 → 拿走号数据（handoff）
- B5 拉号 → 一次派多个去向（混合派）
- B6 record group 里号死 → 用户看到"已失效"

### C · 号池状态维护
- C1 bus 里号死 → 自动补车
- C2 bus 所有号死光 → 补车链条一整轮
- C3 双写副本号死 → webhook 通知乘客
- C4 质保退款分摊（vendor 退 → 我方按比例退乘客积分）

### D · 钱
- D1 兑换码充值
- D2 payment-gateway 充值（waffo，5% pass-through）
- D3 拉号时余额不足 → 拒绝
- D4 单价超策略上限 → 跳过 vendor / fallback
- D5 日轮次 / 日花费触顶 → 停止自动
- D6 单次拉号触发单次议价 +20%
- D7 批量拉号无议价

### E · 决策 / vendor
- E1 比价选 vendor（有效成本 = 单价 / 平均寿命）
- E2 首选 vendor 缺货 → fallback 到次选
- E3 所有 vendor 都缺货 → 排队等 webhook

### F · 配置 / 用户操作
- F1 乘客配置策略参数
- F2 乘客配置 passengerpool url + token
- F3 乘客配置对外 webhook（我方推给他的地址）
- F4 乘客生成 / 吊销 API key

---

## A · 拼车（主入口）

### A1 · 建 1 人 bus + 手动拉一次

**乘客视角**：「我要拼车」→ 选"独自一人"→ 选 vendor 或让系统选 → 输入拉几个号 → 点"发车"

**发生了什么**：系统建 `bus-<bus_id>`（`kind: single`，成员只有他），策略引擎判钱够 → decider 选 vendor → adapter 调 vendor purchase → 号入 housepool `bus-<bus_id>` group（`disabled=false`）。

**钱**：号价 pass-through（10 号 × P = 10P 全归他）+ 服务费 1（他一人一轮，1）。

**号在**：housepool 的 `bus-<bus_id>`，只有他有 client_key。

**技术时序**：`02-flows.md` C。**阶段**：1a。

### A2 · 建多人 bus（匿名撮合）+ 拉一次

**乘客视角**：「我要拼车」→ 选"匿名拼车"→ 填拼车偏好（区域、单价上限、最多几人）→ 点"发车"

**发生了什么**：系统建或复用一个 `kind: anon` bus；乘客进意图池；集单窗口触发（N 秒 或凑够 M 需求）→ 同 bus 内多成员合流成一次拉号 → 号入 `bus-<bus_id>` group。

**钱**：号价按参与人 count 比例分摊；每人服务费固定 1。

**号在**：`bus-<bus_id>` group，全体成员共享 client_key。

**技术时序**：C（首次）+ D（后续合流补车）。**阶段**：1c。

### A3 · 建多人 bus（邀请码）+ 拉一次

**乘客视角**：「我要拼车」→ 选"邀请码组队"→ 建 bus，系统给邀请码 → 分享给朋友 → 朋友输码加入 → 拉号

**发生了什么**：同 A2，只是 bus `kind: team`；成员通过邀请码显式加入而非匿名撮合。集单 + 拉号 + 记账 + 分摊逻辑与 A2 一致。

**技术时序**：C + D。**阶段**：2a。

### A4 · 加入他人的多人 bus（匿名撮合）

**乘客视角**：「我要拼车」→ 选"匿名"→ 填偏好 → 系统匹配到已存在的 bus X → 我加入 → 等 bus X 下一轮补车

**发生了什么**：`bus.Join(bus_id, passenger_id)` → housepool 侧 client_key 授权给我 → 我进意图池等下一次集单窗口。**加入不触发拉号**，只是把我算进后续补车的分摊单里。

**钱**：加入本身**不扣积分**；等下一轮补车时才按 N+1 摊。

**技术时序**：先走 bus 加入（无独立时序，`03 modules/bus`），后续拉号走 D。**阶段**：1c。

### A5 · 加入他人的多人 bus（邀请码）

**乘客视角**：朋友给我邀请码 → 我在 UI 输码 → 加入 bus

**发生了什么**：`bus.JoinByInvite(code, passenger_id)` → 校验邀请码 → 同 A4 后续。**阶段**：2a。

### A6 · 退出 bus

**乘客视角**：在 bus 详情页点"退出"

**发生了什么**：`bus.Leave(bus_id, passenger_id)` → housepool 侧从该乘客的 group 权限剥离 → 下一轮补车不再摊他 → **已扣的号价不退**（服务已交付；退款政策见 §00.7.5）。

**钱**：不退。

**阶段**：1a。

### A7 · 解散 bus

**乘客视角**：bus 创建人 或 最后一位成员点"解散"，或系统自动解散（长期无成员）

**发生了什么**：`bus.Dissolve(bus_id)` → housepool 侧 `bus-<bus_id>` group 里的所有号 → **按解散策略处理**：
- 有活号 → 挪到创建人的 `record-<pid>` group（保留给他处理）
- 死号 → 保留历史后 `DELETE`

**钱**：不退。

**阶段**：1a。

### A8 · 手动拼车 vs 自动拼车

**手动拼车**：乘客每次自己点"拉一次"，走 C。
**自动拼车**：乘客配好策略参数（水位、上限），系统在时钟 / 水位跌破 / vendor webhook 三种触发下自动拉，走 D 集单。**阶段**：1a（手动）→ 1d（自动）。

---

## B · 单独拉号（次入口）

### B1 · 只拉号，不派去向（自留 record group）

**乘客视角**：「我要单独拉几个号」→ 选 vendor + 数量 → 拿到号进"拉号记录"→ 不管了 → 号一直躺在 record

**发生了什么**：走时序 H → 号入 housepool `record-<pid>` group + `disabled=true`。**号已进池已监控**（Layer 4 5 项能力覆盖），只是不发下游、不参与拼车。

**钱**：号价（× count）+ 服务费（× count，每号一轮）在拉号那一刻就扣了；后续不再扣。

**号死**：靠 housepool 探活 / vendor webhook 判死 → housepool 标记 `dead`；UI 显示"已失效"，用户可手动清理（走 handoff DELETE 但不给明文，因为已经无用）。

**阶段**：1a。

### B2 · 拉号 → 派进 bus

**乘客视角**：拉号记录里选几个号 → 点"进 bus X"

**发生了什么**：走时序 I 的"进车"分支 → `PUT /credentials/{id} groups=[bus-X], disabled=false` → 号被 bus X 全体成员共享。

**钱**：**派动作不再扣钱**（钱在拉号那一刻已扣）。**注意**：这与"拼车触发拉号"的分摊逻辑不同——B2 是"某乘客独家出资拉的号，事后送进 bus 白给车友用"，号价不重新分摊。

**阶段**：1a。

### B3 · 拉号 → 推自己号池（双写）

**乘客视角**：拉号记录里选几个号 → 点"推我的号池"

**发生了什么**：走时序 I 的"推 passengerpool"分支：
1. 取 credential 明文 → `delivery/passengerpool` BatchImport 到乘客 kiro.rs
2. `PUT /credentials/{id} disabled=false`（housepool 副本保留 record group）

**钱**：不再扣。

**号在**：housepool 副本（我方监控）+ 乘客 passengerpool（乘客用）。

**阶段**：1e。

### B4 · 拉号 → 拿走号数据（handoff）

**乘客视角**：拉号记录里选几个号 → 点"下载号数据"（或 API 请求）

**发生了什么**：走时序 I 的"handoff"分支：
1. 取 credential 明文 → `delivery/handoff` 返回给用户（UI 下载 or API 返回）
2. `DELETE /credentials/{id}` → 号离开 housepool
3. 数据库落"已 handoff"历史（不留明文）

**号在**：用户自己那里，**我方不再监控**（唯一 fire-and-forget 路径）。

**钱**：不再扣。**阶段**：1e。

### B5 · 拉号 → 一次派多个去向（混合派）

**乘客视角**：拉号记录里勾选 10 个 → 派 5 个进 bus X + 2 个推我的号池 + 3 个拿走

**发生了什么**：走时序 I 全流程，三种去向并发（或串行）处理。每号独立决定，一次动作里同时处理。

**技术时序**：I。**阶段**：1e。

### B6 · record group 里号死 → 用户看到"已失效"

**乘客视角**：拉号记录里某几个号显示"已失效"，可以选择"清理"

**发生了什么**：housepool 探活或 vendor webhook 判定该号死 → housepool `credential.dead_at` 设置 → UI 显示状态。用户点"清理"→ `DELETE /credentials/{id}`（无明文交付，因为号已经无用）。

**钱**：**不退**（号在拉号后过了上游质保窗口才死；跟随上游规则）。若在质保窗口内死，走 C4（vendor 主动退我方 → 我方按比例退乘客积分）。

**阶段**：1a（基础） → 1d（vendor webhook）。

---

## C · 号池状态维护

### C1 · bus 里号死 → 自动补车

**乘客视角**：无感知（自动运维）；或收到 webhook 通知"补了 X 个"

**发生了什么**：deathwatch 收到死号信号 → 从 `bus-<X>` group 踢出 → 检查剩余水位 → 跌破触发补车意图 → coalescer 合流 → decider 走完一整轮拉号 → 号回 bus。

**钱**：新拉的号按 bus 当前成员分摊；服务费按新一轮计（每人+1）。

**技术时序**：E → D。**阶段**：1d。

### C2 · bus 所有号死光

**乘客视角**：收到 webhook `all_keys_dead`；下一轮补车拉齐

**发生了什么**：同 C1，只是本轮意图从 bus 全体成员当前需求生成。**阶段**：1d。

### C3 · 双写副本号死 → 通知乘客

**乘客视角**：收到 webhook（我方推给他的地址），载荷含"你 kiro.rs 里的号 X 失效了"

**发生了什么**：housepool 副本判死 → deathwatch → webhookout POST 到乘客配置的地址 → 乘客那侧的 kiro.rs 自己会独立判死（我方无控制权）。**若号还在拉号记录（未派 bus）**，触发新一轮拉号意图 = 用户手动决定要不要补（我方不自动补）。

**阶段**：1e。

### C4 · 质保退款分摊

**乘客视角**：账户流水多一笔 `warranty_refund +X`

**发生了什么**：vendor `warranty_refund` webhook 到我方 → deathwatch 查订单参与人 → 按比例（拼车按 N 摊）加回乘客积分 → ledger 落 `warranty_refund`。**只退积分，不退款**（§00.7.5 规则 B）。**服务费 / 通道费不退**。

**技术时序**：F。**阶段**：1d。

---

## D · 钱

### D1 · 兑换码充值

**乘客视角**：账户页输入兑换码 → 到账 X 积分

**发生了什么**：走时序 A。**阶段**：1b。

### D2 · payment-gateway 充值（waffo 5% pass-through）

**乘客视角**：账户页点"充值 100 积分"→ 页面展示"应付 ~15 USD（含 5 积分通道费）"→ 扫码支付 → 到账 **100 积分**（乘客要多少充多少）。

**发生了什么**：走时序 B（详见 CLAUDE.md §1.4）。乘客想充 100 积分 → 通道费 5 积分 → 折 105 CNY → 按 7 CNY/USD 显示 15 USD 走 waffo → 内部 `recharge +105` + `channel_fee −5` → 乘客净 +100 积分。**通道费 pass-through**，我方不代垫。**阶段**：1b。

### D3 · 拉号时余额不足 → 拒绝

**乘客视角**：点"拉一次"→ 弹提示"余额不足 X 积分，请充值"

**发生了什么**：strategy 判可行时查 wallet 余额 < 号价 + 服务费 → 拒绝，不调 vendor。**阶段**：1a。

### D4 · 单价超策略上限 → 跳过 vendor / fallback

**乘客视角**：策略配了"单价上限 50"→ 系统看当前最便宜 vendor 也 60 → 跳过本次拉号，或选下一个 fallback vendor

**发生了什么**：decider 里按 `max_unit_price` 过滤 vendor；全部 vendor 超限 → 意图挂起，等价格降或用户手动放宽。**阶段**：1d。

### D5 · 日轮次 / 日花费触顶 → 停止自动

**乘客视角**：自动拉号停了，UI 显示"今日已达上限"

**发生了什么**：strategy 判可行时查每日累计（`daily_round_limit` / `daily_spend_limit`）→ 达上限拒绝，直到次日 UTC/CST 零点重置。**阶段**：1d。

### D6 · 单次拉号 → 触发单次议价 +20%

**乘客视角**：拉一次拉一个号（`count == 1`）→ 结账时账单里多一笔 `single_pull_fee`

**发生了什么**：decider 拉号动作里判 `count == 1` → 加价链上多乘一层 → ledger 落 `single_pull_fee`（我方收入）。

**账单示例**（vendor 号价 20 积分 · 阶段 1a 的链只有议价 + 服务费）：
- `20 × 1.20 × 1.05 = 25.2 积分`
- 分项（每层增量 · `decisions §8.34`）：号价 **20** · 单次议价 **4** · 服务费 **1.2** → 合计 **25.2**
- **通道费不在这里** —— 充值时已收过（`decisions §8.21`），拉号是积分抵扣

**阶段**：1a。

### D7 · 批量拉号 → 无议价

**乘客视角**：拉一次拉多个号（`count >= 2`）→ 结账里没有 `single_pull_fee`

**账单示例**（vendor 号价 20 × 3 个 = 60 积分）：
- 单价 `20 × 1.05 = 21`（无议价层）→ `21 × 3 = 63 积分`
- 分项：号价 **60** · 单次议价 **0** · 服务费 **3**（每号 1，共 3 个）→ 合计 **63**
- **通道费不在这里**（同 D6）

**关键**：拼车拉一次即使参与人只想拿 1 个，若合流后 `count_total >= 2`，仍走批量口径（不加议价那一层）。**阶段**：1a。

---

## E · 决策 / vendor

### E1 · 比价选 vendor（有效成本）

**发生**：decider 拿 6 家实时快照 + deathwatch 存的 `(vendor, 窗口) → 平均寿命`，算**每 vendor 有效成本 = 单价 / 平均寿命**（每积分能活的时长），选最优。**阶段**：1d。

### E2 · 首选 vendor 缺货 → fallback

**发生**：decider 首选 vendor purchase 返回 `no_stock` / `insufficient_stock` → 按有效成本排名往下走次选 → 直到找到有货的。**阶段**：1d。

### E3 · 所有 vendor 都缺货 → 排队

**发生**：所有 vendor 都返回缺货 → 意图挂进"待补货队列" → 收到任一 vendor `new_keys_available` webhook 触发重试。**阶段**：1d。

---

## F · 配置 / 用户操作

### F1 · 乘客配置策略参数

**发生**：账户页表单：`auto_enabled` / `per_round_count` / `min_count` / `keep_safety_stock` / `max_unit_price` / `daily_round_limit` / `daily_spend_limit` / `target_bus_id`（哪辆 bus）。存入 strategy 表。**阶段**：1a → 1d。

### F2 · 乘客配置 passengerpool url + token

**发生**：账户页表单：`passengerpool_url` + `passengerpool_token`。加密存 `passenger.downstream_config`。配了才有 B3 / A/B 里的"推自己号池"选项。**阶段**：1e。

### F3 · 乘客配置对外 webhook

**发生**：账户页表单：`webhook_url`。存入 passenger 表。webhookout 用来推补车/号死通知。**阶段**：1e。

### F4 · 乘客生成 / 吊销 API key

**发生**：账户页点"生成 API key"→ 返回明文（一次显示）+ 落 hash。所有次入口拉号 / 派去向 API 用它鉴权。**阶段**：1a。

---

## 场景 → 时序 → 阶段 · 全表

| 场景 | 时序 | 阶段 |
|---|---|---|
| A1 · 1 人 bus 手动拉 | C | 1a |
| A2 · 匿名拼车 | C + D | 1c |
| A3 · 邀请码拼车 | C + D | 2a |
| A4 · 加入匿名 bus | (bus.Join) + D | 1c |
| A5 · 加入邀请码 bus | (bus.JoinByInvite) + D | 2a |
| A6 · 退出 bus | (bus.Leave) | 1a |
| A7 · 解散 bus | (bus.Dissolve) | 1a |
| A8 · 手动 vs 自动 | C / D | 1a → 1d |
| B1 · 只拉号自留 | H | 1a |
| B2 · 派进 bus | H + I | 1a |
| B3 · 推自己号池 | H + I + G-② | 1e |
| B4 · 拿走 | H + I + G-③ | 1e |
| B5 · 混合派 | I | 1e |
| B6 · record 号死 | (E 子情况) | 1a → 1d |
| C1 · bus 号死自动补 | E → D | 1d |
| C2 · bus 全死 | E → D | 1d |
| C3 · 双写副本死 | (E) + webhookout | 1e |
| C4 · 质保退款 | F | 1d |
| D1 · 兑换码 | A | 1b |
| D2 · payment-gateway | B | 1b |
| D3 · 余额不足 | (strategy) | 1a |
| D4 · 超单价上限 | (decider) | 1d |
| D5 · 日上限 | (strategy) | 1d |
| D6 · 单次议价 +20% | (decider) | 1a |
| D7 · 批量无议价 | (decider) | 1a |
| E1 · 比价 | (decider) | 1d |
| E2 · fallback | (decider) | 1d |
| E3 · 全缺货排队 | (decider + webhookin) | 1d |
| F1 · 策略 | (config) | 1a → 1d |
| F2 · passengerpool 配置 | (config) | 1e |
| F3 · webhook 配置 | (config) | 1e |
| F4 · API key | (config) | 1a |
