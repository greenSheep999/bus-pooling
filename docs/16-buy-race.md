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

## 见

- `decisions.md §11.7` · 抢号链改造的长期方向
- `09-transactions.md §2` · client_order_id 幂等约定
