# 21 · 1d 阶段待办清单（2026-08-14 · 从 1c 收尾捞出来的）

> **写这份的目的**：把 1c 收尾时**没做完的 · 需要 1d 中后期做的**记下来。
> 每条只写"是什么 / 为什么留 / 该怎么做" · 不写代码。
>
> **入口**：从 `docs/00-values-and-phases.md` 的 1d 计划里找位置塞。**别在这里
> 独立规划节奏** —— 主时间线是 §00。

---

## 1 · 真集单（coalescer）· 中期项

**现状**：`internal/coalescer/coalescer.go` 有 skeleton · `Anon()` / `Team()` 直接
`return nil, ErrNotImplemented`。业务层 grep `coalescer\.` 无调用（除测试）· 整包在生产未接线。

**为什么留 1d**（不在 1c 收尾做）：
- 真集单不是简单加个函数 · 涉及：**意图池表**（新 migration）+ **窗口调度**（新 ticker）+
  **decider dep** 改造（原来 pull 路径直接调 Pull · 改成写意图池）+ **测试重写**
  （`coalescer_test.go:33-45` 显式断言 ErrNotImplemented · 实作后必挂 · 需替换）。
- 用户价值：多人 bus 减少 vendor 侧次数 · 但 1c 阶段拉号量还不够引出这个瓶颈。
- 优先级低于"上游余额管理 · 号寿命喂 quality · 比价 fallback" —— 那些是**决策链闭环**·
  真集单是**开销优化**。

**设计要点**（1d 落手前的备忘 · 别重新想）：
- 意图池表：`intent_pool(id, passenger_id, bus_id, count, zone, vendor_id, submitted_at, deadline_at, status)`
- 窗口调度：默认 500ms · 到时间就把同 bus + 同 zone + 同 vendor 的意图合并成一个 count 拉号
- decider 加 `SubmitIntent()` 走异步 · 返 `intent_id` 让上层轮询
- 单人 bus / 立即成交场景（缺货挂单）**跳过集单** —— 那些走原 `Pull()` 直发
- 幂等：意图 dedupe 用 (passenger, bus, client_order_id · 前端生成)
- 计费：合并后按每人各自 count 分摊 —— 复用现有 `participants_split_json`

**改动面**（预估）：
- 新 migration + intent_pool store
- coalescer/coalescer.go 全重写 · anon.go / team.go 拆
- api/pull.go 改为写意图池
- api/bus.go 同
- decider 加 `SubmitIntent`（新入口 · Pull 保留给挂单 fire 路径）
- 前端 · 拉号响应从"立即 result"改成"意图 id · 稍后回调" —— UI 大改
- 测试 · coalescer_test / api pull_test / e2e 全套跟着改

**预计工作量**：**一整个 sprint** · 别在 1c 收尾里做。

---

## 2 · 自动补车 Step 2（真 fire）· 短期项

**现状**：Step 1 已上（migration 037 + `pending_refill` 队列 + skeleton `RefillTick`）·
号死后落待补记录 · worker 只 log 不真拉。

**Step 2 要做**：`internal/deathwatch/refill.go` 的 `RefillTick` 里改成真调 `decider.Pull`。

**顺序**：
1. **观察一周** · 看 pending_refill 落多少条 · 触发场景合不合理（有没有异常量 · 有没有归属反查失败的）
2. 加**策略校验**：从 `bus.Strategy` 读 `AutoRefill == true` 才补 · 用户关了就 skipped
3. 加**并发限流**：一次 tick 最多 fire N 个 · 防死号大爆发瞬间下 100 单
4. 加**幂等**：dead_credential_id UNIQUE 已经防重塞 · fire 时用 (refill_id) 做 decider 幂等键
5. `RefillTick` 挂到 janitor 里 · 1min 一次
6. 失败重试 3 次后进 `expired` 态 · 出监控告警

**改动面**：只在 `refill.go` 里 · 不动 migration · 不动 skeleton 结构。**回滚容易**。

---

## 3 · vendor 余额自动切换 · 短期项

**现状**：P5 加了 `vendorbalance.Cache` · 拉号前查缓存 · **不够就直接拒**·
不切换到有钱的 vendor。

**Step 2 要做**：预检不够时 · 不直接返 `ErrVendorInsufficient` · 而是：
1. 从 vendor registry 里找**同 zone + 余额够 + 单价接近的备胎**
2. 记 log · 切过去继续拉
3. 用户看到的还是"拉成功"

**难点**：切换会打乱 preferred_vendor / P4 picker 的选择 · 前端如果显示"你选的 X 家"
但实拉走 Y · 又是新的割裂。**要跟前端约定好回填**（result.VendorID 是真拉的那家）。

**先不做的理由**：P5 已经**能让 vendor 没钱时报明确错**（而不是等 vendor 侧返 insufficient_balance
被动失败）· 用户能看到告警去手工充值 · 短期够用。

---

## 4 · webhook 带的 price/stock 字段落库 · 长期项

**现状**：kiroappio / kiroappcc 的 webhook 独家带 price + stock 字段（`docs/19-fields.md` 组 11）·
我方 webhookin 收下后 · 存 `inbound_webhook_event.raw_body` 但**不抽字段落库**。

**价值**：这两家没 fleet 端点 · webhook 是**唯一实时价源** —— 现在只能靠 xi8 补 ·
xi8 断了这两家就断了。webhook 抽字段落库能补上（源类型 = `webhook_native`）。

**顺序**：
1. adapter 里 `WebhookEvent` 加 `Price *Money` / `Stock *int` 可选字段
2. dispatcher 里如果字段非空 · 转成 `vendor_probe_zone` 一行落库（source='webhook_native'）
3. `pricing.ProbeCredits.LatestCredits` 的 fallback 链加一级 · 优先级 `vendor_self > webhook_native > xi8 > xi8_notif`

**改动小 · 但要连着 6 家 vendor 的 webhook 载荷字段核对** —— 留 1d 中期做。

---

## 5 · 号寿命 / 用量明细 · 高价值中期项

**现状**：`AutoPick` 打分里 `AliveRate30d` 恒 0（数据没采集）· 比价 fallback（P4 已接
入 decider）纯靠价格。用户点名说"这块卡着"。

**未接的上游端点**（`docs/20 §2` "号寿命/用量明细"）：
- kirooo `/api/my/dispatch-log`（按车次给活死 → 真实寿命）· 唯一
- kirooo `/api/my/keys/export`（master_id + 死因）
- kiro91 `/api/my/usage` + `/api/my/keys/{id}/usage`（含 subscription + reset_days）
- kiroceo `/api/my/keys/usage`
- 各家 `/api/my/keys/created-at`（最早产出时刻 · 算平均寿命的基线）

**要做的**：
1. adapter 各家加 `ListLifespans()` 接口（返 (key_id, created_at, dead_at, usage_pct)）
2. `vendor_key` 表已经有字段 · 只需填数据
3. `vendorview.Service.qualityFor(vendorID)` 里从 `vendor_key` 聚合平均寿命
4. AutoPick 打分公式用真实寿命率替代 50% 常数

**改动面**：6 家 adapter + vendorview 打分 · 中等工作量。

---

## 6 · 单价上限（P2）跟展示层对齐 · 短期项

**现状**：P2 加了积分口径硬拦 · 但前端 UI 显示"你的上限是 X 积分"· **前后端语义确认过对齐**（都是 microunit）· 无 bug。

**追加**：让**预估阶段就能反馈**（现在只在 decider 里拦 · 用户提交后才知道超限）。
- api/estimate 或 GET /me/stock 返值加 `blocked_by_price_cap` 字段
- 前端拿到这个字段禁用拉号按钮 + 显示"当前单价 100 积分 · 超过你设的 80 积分上限"

**放这里的原因**：不阻塞 P2 硬拦（已经防止真扣错钱）· 只是 UX 优化。

---

## 完整 1d 计划见 `docs/00-values-and-phases.md`

这份是**具体施工项** · 时间安排跟 §00 走。
