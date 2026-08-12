# 16 · 抢号链 · 又快又稳又省钱

**触发时机 · 2026-08-12**：市场号短命（几分钟被抢空）· 单纯"用户拉号 → 后端排队 → 打 vendor" 慢一步就没了。要做的是：**多路信号并行触发 → 缺货挂单 → 有货立即抢**。

## 问题

用户诉求："又快又稳又省钱地拉 key"。

现有链路（decider.PullOnce）：

```
用户 POST /me/pull
  → decider.Enqueue
  → pull_intent{status=pending} 落库
  → coalescer 集单
  → decider.Purchase(vendor)
    → 打 vendor.Stock 查库存 · stock=0 直接返 ErrOutOfStock
    → 用户拿到 failed · 退钱
```

**问题清单**：
1. 缺货 → 直接失败 · 用户体验差 · 也没利用后续 restock 事件
2. 库存判断走探针快照 · 60s 前的数 · 决策已经过时
3. 只等被动 webhook 通知 · vendor 侧上号到 webhook 到手上少说 1-3 秒 · 早被别人抢
4. 6 家里只 kirooo/kiroappcc 有 webhook · 其他家全靠拉

## 目标信号金字塔

**又快** · 从最快到最慢的信号源：

| 级 | 信号 | 延迟 | 覆盖 | 说明 |
|---|---|---|---|---|
| 1 | vendor 自家 webhook `new_keys_available` | 200ms-2s | 少数家 | 最快 · 但要注册 URL |
| 2 | xi8 signals 推送订阅 | 3s | 5 家 | xi8 自己也订 vendor · 转推我方 |
| 3 | Prober stock-delta 30s hot 模式 | 5-15s | 6 家 | 已实现（§11.9/§11.12） |
| 4 | Prober baseline 60s | 30-60s | 6 家 | 兜底 |
| 5 | Backfiller 5min | 分钟级 | 有 fleet 端点的家 | 历史补录 |

**又稳** · 每级都幂等 + 有单价上限保护：
- 拉号请求带 `client_order_id` · vendor 侧同一号重复 POST 返同一批（09-transactions §2）
- decider 前置校验单价上限 · 防"vendor 突然涨价 100 倍" 时误买
- 缺货挂单有 TTL · 不会永久等下去

**又省** · Prober 自适应频次已实现 · 加抢号链后 · 6min hot 窗内还额外 fire 抢号请求 · 但**只当有 pending 意向**时才 fire · 无意向就纯观察。

## pull_intent 状态机扩展

现有 6 态：`pending / in_flight / coalesced / fulfilled / failed / cancelled`

**加 1 态**：`pending_on_stock`

```
                       ┌──── 缺货 → pending_on_stock
                       │        ↓
                       │     restock 事件（webhook / xi8 / stock-delta）
                       │        ↓ 唤醒
pending → in_flight ───┤     in_flight → decider.Purchase
                       │        ├──→ fulfilled
                       │        └──→ failed（再次缺货 or vendor 出错 · 退款）
                       └──── 直接成 → fulfilled

超时：pending_on_stock 到 TTL（默认 10min）· 自动 → failed · 退款
```

**语义**：
- `pending_on_stock` = "钱冻结 · 等 restock" · 用户视角"排队中"
- 触发抢号事件到 decider.Purchase · 走同一段代码 · 只是入口不同
- TTL 保护 · 库存长期不来时释放 · 不永久占款

## 新表 · `stock_watcher`

```sql
CREATE TABLE stock_watcher (
    intent_id      TEXT PRIMARY KEY,      -- 引 pull_intent(id)
    vendor_id      TEXT NOT NULL,         -- 等哪家的号
    region         TEXT,                  -- 特定 region · NULL = 任 region
    max_unit_price INTEGER,               -- 单价上限 microunit · 保护涨价
    count          INTEGER NOT NULL,      -- 要几个
    started_at     TEXT NOT NULL,
    expires_at     TEXT NOT NULL,         -- 超时释放
    status         TEXT NOT NULL,         -- watching / fired / expired
    fired_at       TEXT,                  -- 唤醒时刻
    fired_reason   TEXT                   -- webhook / xi8_signal / stock_delta
);
CREATE INDEX idx_stock_watcher_active ON stock_watcher(vendor_id, status);
```

## 唤醒路径

**入口 · 三个信号源都走同一函数**（复用逻辑）：

```go
// stockwatch.Notify · 有 restock 事件时打这个 · 唤醒等的 pending 意向
func (w *Watcher) Notify(ctx context.Context, vendorID, region string, count int, source string) error {
    // 找 status='watching' 且 (region NULL 或 region 匹配) 的 intent
    // 按 started_at 顺序 · 一次最多唤醒 count 条
    // 每唤醒一条 · fire decider.Purchase · 幂等 by client_order_id
}
```

**信号源接入点**：

| 源 | 位置 | 代码路径 |
|---|---|---|
| vendor webhook new_keys | `webhookin.dispatcher.onNewKeys` | 现有 · 加一句 `w.Notify(...)` |
| xi8 signals（未来） | 新 `internal/xi8/subscribe.go` · webhook 或 SSE | 未做 |
| Prober stock-delta | `vendorview.prober.deriveStockDelta` | 现有 · 加一句 `w.Notify(...)` |

## 幂等保护

一个 vendor 上号后 · 可能三个信号源同时 fire：
1. Prober delta 15s 后发现
2. xi8 signals 8min 后转推（xi8 也有延迟）
3. Vendor webhook 2s 后到

**幂等 by client_order_id**：
- 每个 pull_intent 生成一次性 `client_order_id`（uuid v7）· 存 pull_intent 表
- 唤醒时用这个 client_order_id 调 decider.Purchase
- vendor 侧同 client_order_id 只成一单（09-transactions §2）
- 第一个 fire 的抢到号 · 后续 fire 打 vendor 返"已成单" · decider 归一为 replayed=true · 直接落库不重复扣

## 单价上限保护

pull_intent 落库时记 `max_unit_price` · Watcher fire 前先校验：

```go
if snap.UnitPrice > watcher.MaxUnitPrice {
    // 涨价 · 不 fire · 继续等 · 或超时退款
    logger.Info("stock_watcher: 涨价超上限 · 跳过唤醒", ...)
    return nil
}
```

## 用户 UX（**先不做 · 内部用**）

阶段 1a **只给补车链用**（deathwatch 检测到号死 · 自动开一个 pending_on_stock）·
乘客端不暴露"等货挂单"选项。理由：
- 内部先跑通机制 · 观察实际唤醒延迟 / 唤醒率
- 乘客端"挂单等货" UI + i18n + 取消流 + 超时策略不小 · 阶段 2 再做

## 落地步骤

1. migration 027 · `stock_watcher` 表 + `pull_intent.status` CHECK 加 `pending_on_stock`
2. `internal/stockwatch/watcher.go` · Notify / Enqueue / Sweep（TTL 扫）
3. `webhookin.dispatcher.onNewKeys` · 加 `w.Notify` 一句
4. `vendorview.prober.deriveStockDelta` · 加 `w.Notify` 一句
5. `deathwatch` 补车触发 · 拿 stockwatch.Enqueue 排队 · 不直接调 decider
6. 单测：三源同时 fire · 只成一单 · 幂等验证
7. **不动前端** · 阶段 1a 内部机制

## 明确不做（阶段 1a）

- ❌ 用户端"挂单等货"UI · 阶段 2
- ❌ xi8 signals SSE 订阅 · 先看 xi8 是否推给我方 · 现在只做 REST poll
- ❌ 全局限流器 · 单 vendor 请求 QPS 阈值 · 阶段 2c

---

# 待讨论 · 四个缺口（2026-08-12 · 未定稿 · 不要照着写码）

抢号链设计完之后浮出来的三个问题。**都还没定** · 记这里防丢 · 定了再挪进 `decisions.md`。

## 缺口 1 · 单价上限只有一个值 · 不分时段

**现状**（核对过代码 · `internal/strategy/canpull.go:136`）：

两层上限**已经有了** · 而且是 AND 取更严（`stricter()`）：

| 层 | 字段 | 作用域 |
|---|---|---|
| 全局 | `passenger_strategy_default.max_unit_price` | 跨所有车 + 提取 key |
| 车级 | `bus.max_unit_price` | 那辆车 |

`decisions §8.27` 已定这个分工 · **不是缺口**。

**真缺口 · 上限是单值 · 不随时段变**：

> "白天价格高应该也要上 · 晚上贵了就不要了没有那么大"

实测（8-12 · `vendor_probe.sample_price_micro`）：

```
vendor  日期        最低    最高    样本
kiro91  08-11    60.00   80.00     29
本 vendor 群 · us 区已经吵到 100 · eu 区 70
```

价格**日内波动 33%**（60 → 80）· 单值上限只能取一头：
- 取高（80）· 晚上 80 也买 · 但那时你不想买
- 取低（60）· 白天 80 一次都拉不到 · 车停了

**候选方案 · 三个 · 都没定**：

**A · 时段上限表**（表达力最强 · 最复杂）
```sql
CREATE TABLE price_cap_schedule (
  scope_kind   TEXT,     -- global | bus
  scope_id     TEXT,     -- passenger_id | bus_id
  weekday_mask INTEGER,  -- bit 0-6 · 哪几天生效
  hour_start   INTEGER,  -- 0-23 本地时
  hour_end     INTEGER,
  max_unit_price INTEGER
);
```
- ✅ 想怎么排都行
- ❌ 新表 + 新 UI + 时区处理（用户在哪个时区？）· 阶段 1a 太重
- ❌ 用户要理解"时段"这个概念 · 认知负担

**B · 两档 · 白天价 / 夜间价**（够用 · 最省）
```sql
ALTER TABLE bus ADD COLUMN max_unit_price_night INTEGER;  -- NULL = 跟白天一样
ALTER TABLE passenger_strategy_default ADD COLUMN max_unit_price_night INTEGER;
-- 夜间时段写死 · 比如 23:00-07:00 本地时（config 可调 · 不进代码）
```
- ✅ 一列 + 一个 UI 输入框 · 阶段 1a 能做
- ✅ 覆盖你说的场景（白天松 · 晚上紧）
- ❌ 只有两档 · 不能表达"周末不一样"

**C · 不做上限分档 · 改成"相对上限"**（换个角度）
```
max_unit_price_pct = 130   -- 不超过"过去 7 天同 vendor 均价"的 130%
```
- ✅ 自动跟着市场走 · 不用用户改
- ✅ 数据已经有（`/api/vendors/{anon_id}/prices/daily` 端点已通）
- ❌ 用户看不懂"130%"是多少钱 · 得同时显示折算值
- ❌ 市场整体涨价时它跟着涨 · 挡不住"整个市场都贵"

**我倾向 B** · 理由：阶段 1a 的复杂度预算不够 A · C 的心智模型对公益工具的用户太绕。B 一列一框 · 覆盖你说的实际痛点。

**但要你拍**：
- 夜间时段边界写死几点？（23:00-07:00？还是 00:00-08:00？）
- 按谁的时区？（用户注册地拿不到 · 只能用固定 UTC+8 或让用户选）
- 要不要给车级也加夜间价？（还是只全局一个夜间价 · 车级不分档）

## 缺口 2 · 预留（推 passengerpool）开了 · 抢到的号要不要强给

**你的原话**：

> "预留开关是不是应该给用 · 如果用户开了预留 · 我们抢到了也要给他发（分摊我们抢到了没人要的风险）"

**核对现状**：`credential_ledger` 有 `pushed_to_passengerpool_at` / `passengerpool_push_error` 字段 · `passenger_downstream` 表存乘客的号池配置 · **推池能力已有**（`decisions §8.x` 推池语义已定）。

**真问题 · 加速预存池的库存风险谁承担**：

抢号链的加速预存池（`prebuy-pool` · 5min TTL）有个硬风险：

```
探针见 stock>0 → 抢 3 个进 prebuy-pool
  ↓ 5 分钟内
没人来拉 → TTL 到 → 走 warranty 退 vendor
  ↓ 但
warranty 只有 10 分钟 · 而且不是所有 vendor 都给退
  → **我方吃掉这 3 个号的成本**
```

**你的提议 = 让"开了预留的用户"分摊这个风险**：
- 用户开预留开关 = 声明"我要号 · 有就给我"
- 我方抢到号 · 没人主动拉 → **推给开了预留的用户** · 扣他钱
- 我方不吃库存风险 · 用户拿到号（他本来就想要）

**这个逻辑成立 · 但有三个必须先答的问题**：

**① 扣钱要不要用户当场同意？**
- 预留开关打开时就是"预授权"？还是每次推之前问一次？
- 不问 → 用户可能醒来发现被扣了 5 次钱（一晚上 vendor 上了 5 次货）
- 问 → 就不是"预留"了 · 退化成普通拉号
- **中间方案**：预留开关 + **每日预留上限**（最多帮我抢 N 个 / 每天最多花 M）· 用现有的 `daily_round_limit` / `daily_spend_limit` 兜

**② 号推过去了 · 用户说"我不要" 怎么办？**
- 号已经在他 passengerpool 里 · 已经能用了 · 退不回来
- 只能靠**预留上限**事前限制 · 不能事后退

**③ 多个用户都开了预留 · 抢到 1 个号给谁？**
- 先开预留的先得？（`started_at` 排序）
- 还是按 tier（insider > wholesale > retail）？—— 这会变成"付费优先" · 跟公益定位冲突
- 还是轮询公平（上次给了 A · 这次给 B）？
- **我倾向轮询公平** · 但要你拍

**我的倾向**：
- 预留开关**要做** · 你说的风险分摊逻辑对
- 但必须配**每日预留上限**（复用现有 daily limit 字段 · 不新增）
- 分配用**轮询公平** · 不按 tier
- 阶段 1a 先**只给自己（运营账号）开** · 观察实际抢到率和吃单率 · 再放给用户

## 缺口 3 · 车配置 vs 全局配置的优先级

**现状**（`internal/strategy/canpull.go:127-143` · 已实现）：

| 字段 | 全局 vs 车级 | 规则 |
|---|---|---|
| `max_unit_price` | AND · 取更严 | `stricter(全局, 车级)` |
| `daily_round_limit` | 只有全局 | 车级字段存在但 `decide()` 没读 |
| `daily_spend_limit` | 只有全局 | 同上 |
| `per_round_count` | 车级优先 | 全局只当新车默认值 |
| `preferred_vendor` | 车级优先 | 全局只当新车默认值 |
| `default_zone` | 全局 | 车级无此字段 |

**发现两个不一致**（读代码时发现 · `decisions §8.27` 没覆盖）：

**① `bus.daily_round_limit` / `bus.daily_spend_limit` 是死字段**

schema 里有（`001_init.sql` bus 表）· `bus.Strategy` struct 也读了 · 但 `strategy.decide()` **只判全局的** · 车级那两个从来没生效过。

要么：
- **A** · 实现车级每日限额 · 跟全局 AND（跟 max_unit_price 一致）
- **B** · 删掉这两列 · 明确"每日限额只有全局"（`§8.27` 的原意像是这个）
- **C** · 保留字段但文档写明"预留 · 阶段 2 用"

**我倾向 B 或 C** · 理由：每日限额的意义是"防止我一天花太多" · 这是**人的预算**不是车的预算。车级再加一层 · 用户要维护 N 个车 × 2 个限额 · 认知爆炸。

**② 提取（BusID 空）绕过车级上限 · 这个是对的但没写进 §8.27**

`canpull.go:132-135` 主动把 `busCap` 置 nil · 注释说得很清楚（提取走 record group · 没有车）。**这个行为对** · 但 `§8.27` 的表格只说"每车策略管那辆车" · 没说"提取完全不受车级管"。建议补一行。

**优先级总原则 · 请你拍板一个说法**（现在是隐含的 · 想写死进文档）：

- **护栏类**（会拦下操作的：max_unit_price / daily_*）→ **AND · 取更严** · 任一层拦住就拦
- **偏好类**（只影响默认选择的：per_round_count / preferred_vendor / zone）→ **就近优先** · 车级 > 全局 > 系统默认

这个二分法能覆盖所有字段 · 而且好解释（"限制取最严 · 偏好取最近"）。

## 缺口 4 · 加倍率抢货 · 优先派发 ⚠️ 跟已否决项撞车

**原话**（2026-08-12）："可以让用户加倍率抢货 · 优先派发"。

### ⚠️ 先说清楚：这条**否决过**

| 位置 | 记的什么 |
|---|---|
| `CLAUDE.md §3` | ❌ 优先拍单议价 —— 全部已被否决 |
| `CLAUDE.md §5` | ❌ 优先拍单 / 急单议价（**跟单次议价重复**）|
| `decisions §2.6&2.7` | ⏸ 稳定优先（排噪邻 + 优先撮合）打包 +20% · 阶段 2c 后 |
| `00 §6.5` | 优先拍单议价 → **阶段 2b 后**（依赖列队策略先做） |

**当年否决的理由**：跟「单次议价」重复 —— `count==1` 已经在收 `single_pull_fee`，
再来一层"加钱插队"是同一件事收两次。

### 但你这次提的有个**新前提**

否决那会儿**没有队列这个东西**。现在有了：`stock_watcher` 是真队列，`Notify` 按
`started_at ASC` 先挂先抢（`store.go` 的 ORDER BY）。所以"插队"从抽象概念变成了
**一行 ORDER BY 的改动** —— 技术上几乎零成本，这是新情况。

### 三个必须先答的问题

**① 跟公益定位冲突怎么办**

`CLAUDE.md §13`：「这个项目是给一个人做的公益工具」。而 tier 体系是**反向**的 ——
`insider` 交的钱**更少**（免 vendor + 区域附加费），不是更多。加钱插队会让
「谁出价高谁先拿」，跟这个方向相反。

我上一轮在缺口 2 里推荐过「轮询公平，不按 tier」，理由就是这个。加倍率插队跟那条
是**直接矛盾**的 —— 两个不能同时要，得你选一个。

**② 队列里通常有几个人**

现在实际情况：`demand` 大部分时候是 0-3（我方自用 + 少量测试）。**队列里只有 1 个人
时，插队没有意义**。这个功能的价值完全取决于「同时有多人等同一家 vendor」的频率，
而那个数据现在还没有 —— 抢号链才刚跑通，一次真实 fire 都还没观察过。

**③ 钱怎么算 · 加的倍率加在哪一层**

`CLAUDE.md §1.3` 的计费模型是逐层乘：
```
号价 × (1+vendor) × (1+区域) × (1+单次议价) × (1+插槽…) × (1+服务费)
```
「加倍率」如果做，只能进**插槽层**（`capability_fee`）—— 那正是 `decisions §2.6&2.7`
给「稳定优先」预留的位置，阶段 2c。放别的层都会破坏 §8.34 定的加法顺序。

### 候选方案

**A · 不做**（跟当年否决保持一致）
- ✅ 定位干净 · 不用解释「为什么有钱能插队」
- ✅ 队列现在就 0-3 人 · 做了也没人感知
- ❌ 放弃一个技术上几乎免费的收入点

**B · 记进插槽候选 · 等阶段 2c**（跟 §2.6&2.7 合并）
- ✅ 跟已有决策一致（那条本来就是「优先撮合」）
- ✅ 等有真实队列数据再决定 · 不拍脑袋
- ✅ 现在零改动
- ❌ 要等

**C · 现在就做 ORDER BY 加权**
- ✅ 一行改动（`ORDER BY priority DESC, started_at ASC`）
- ❌ 撞公益定位 + 撞缺口 2 的轮询公平
- ❌ 队列 0-3 人时纯属自娱自乐
- ❌ 违反 §6.3（定价类提议要先查历史 · 查了就是否决过的）

**我推 B** —— 不是因为想法不好，是因为：
1. **数据不支持**：队列现在 0-3 人 · 插队价值无从验证
2. **已有归属**：`decisions §2.6&2.7`「优先撮合」就是这条 · 阶段 2c 有位置
3. **一次只做一件**：抢号链本身还有加速预存池没做 · 先把「能抢到」做完 ·
   再谈「谁先拿」

### 如果你要做 C · 需要同时定的事

- 倍率进插槽层（`capability_fee`）· 不能进别的层（破坏 §8.34）
- 跟缺口 2 的「轮询公平」二选一 · 明确放弃哪个
- `stock_watcher` 加 `priority` 列 + `ORDER BY priority DESC, started_at ASC`
- **对外文案**：不能叫「加钱插队」（§12.6 对外术语）· 得想个说法
- CLAUDE.md §3 / §5 的否决记录要改 —— 那两条现在明文写着 ❌

## 缺口 5 · Region 命名口径不统一（模拟测试踩出来的真 bug · 2026-08-12）

**症状**：`buyrace_e2e_test.go` 里 Notify 带 `region="us-east-1-dryrun"` 匹配不到
挂单（挂单存的是 `providers.ZoneUS = "us"`）· SQL 严格相等就查不到。

**根因**：三处对 region 用了三套字面量：
- **`enqueue` 时**：decider 传 `in.Zone`（`providers.Zone` 枚举 · 值是 `"us"` / `"eu"`）
- **webhook 来时**：`webhookin.onNewKeys` 传 `e.Zone`（vendor 自己的 zone 名 · 通常也是 `"us"`）
- **探针 delta 时**：`prober.deriveStockDelta` 传 `d.Region`（vendor 的 region 名 ·
  各家不同 · 如 `us-east-1` / `us-east-1-dryrun`）

`stock_watcher.region` 列的语义没定死是哪套。SQL 查询是严格相等：
```sql
WHERE (region IS NULL OR ? = '' OR region = ?)
```
`"us"` 匹配不上 `"us-east-1"` · 挂单被跳过。

**待定的三个方案**：

**A · 挂单和 Notify 都改成 zone 名**（推 · 简单）
- 挂单存 `"us"` / `"eu"` · Notify 也传 zone 名
- 探针那头把 region 归一到 zone：`us-east-1 → us`（`providers.RegionToZone` 加一个）
- 命名统一 · SQL 逻辑不动

**B · 都改成 region 名**
- 分区更细 · 但 vendor 之间 region 命名不一致（`us-east-1` vs `us-east-1-dryrun`）· 硬对齐要每家 vendor 加映射表
- 复杂

**C · 完全放弃分区匹配 · region 忽略**
- Notify 时 region 空 · fire 所有 watching · 由 fire 时的 Purchase 再选 zone
- 简单 · 但失去"只等特定 region 补货"的能力

**当前**：测试临时传空 region 绕过 · 生产代码里 `webhookin` 和 `prober` 都会传非空
region · **上线后大概率所有挂单都匹配不上** —— 需要在推 vps22 前修。

修法建议 · 走 **A**：
1. `providers` 加个 `ZoneOf(region string) Zone` · 6 家 vendor adapter 各自实现
2. `webhookin.onNewKeys` 和 `prober.deriveStockDelta` 用它把 region 归一到 zone
3. `stock_watcher.region` 列**语义定死为 zone 名**（`"us"`/`"eu"`/空）· 加注释

## 落地顺序（讨论定稿后）

1. 缺口 3 先做 —— 纯文档 + 删死字段 · 零风险 · 清掉认知债
2. 缺口 1 做 B 方案 —— 两列 + 两个输入框 + config 时段边界
3. 缺口 2 最后 —— 依赖加速预存池先跑通 · 有真数据才知道吃单率
4. 缺口 4 **不进本轮** —— 归入 `decisions §2.6&2.7` 插槽候选 · 阶段 2c 再评估

## 见

- `decisions.md §11.7` · 抢号链改造的长期方向
- `decisions.md §11.15` · 抢号链三层开关（已落码）
- `decisions.md §8.27` · 全局 vs 车级策略分工（本文缺口 3 要补它）
- `09-transactions.md §2` · client_order_id 幂等约定
- `internal/strategy/canpull.go` · 两层上限合并的**实际实现**
