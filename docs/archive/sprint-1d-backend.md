# sprint-1d · 后端主线交付清单

> **本文只写"1d 要做什么 + 上线前还剩什么"**。技术细节 / 决策讨论 / 长期契约 →
> 分别去 A 契约（00-19）/ D 决策（decisions.md）· 别塞进这里。
>
> 阶段 1d 目标（`00-values-and-phases §7`）：**自动模式（时钟 / 剩号少 / vendor
> webhook 触发）+ 号死自动补车 + 决策模型（比价 + fallback）**。无压车治理。

## 交付清单

- [x] 号死自动补车 · `deathwatch.RefillTick` 1min 扫队列 · 真调 `decider.Pull`
- [x] vendor 余额自动切换 · `BalanceChecker` + `PickBestVendorExcluding` · 没钱切下一家
- [x] 号寿命 + 存活率喂 AutoPick · `QualityStore` 真数据（不再是 50% 常数）
- [x] webhook 带的 price/available 落 vendor_probe_zone · source='webhook'
- [x] 陈旧管线告警外发 · `StalenessChecker` + `AlertNotifier`
- [x] 自动模式 scheduler · **codex 六步收敛已完成 2026-08-15**：
    - [x] 位置 0 · `death_refill` 受 `AutoRefillEnabled` 约束(第三刀 · `deathwatch.SetRefillDecider` + `refillDecideBridge`)
    - [x] 位置 1 · vendor 新号 webhook 唤醒范围(第五刀 · `webhookin.AutoScanNotifier` + `webhookAutoScanBridge`)
    - [ ] 位置 2 · prebuy-pool 抢到无主号的分配路径 · **待决策(付费优先能力·见 `decisions.md §12`)**
    - [x] 位置 3 · 多 vendor 同车判据(第一刀 Decide Step 3 · 备胎判据 = 数量 AND 价格 AND 新鲜度)
    - [x] 位置 4 · 建拼车后第一次一律手动 · **由 UI 保证**(建车向导 + AutoRefillEnabled 默认 false · 见 decisions §12.已定 2026-08-15)
    - [x] 位置 5 · 保底触发挂 stockwatch 不是硬下单(第一刀 Decide Step 4 · mode×source 表 · Tight/整车挂时输出 Enqueue)
    - [x] 位置 6 · 用户字段命名对齐(`RefillWatermark` = 水位线；`RefillMinCount` = 本轮最少拉几个；见 `docs/15-scheduling.md §4/§5`)
- [x] auto-pick 缺货挂单 bug(第四刀 · `maybeEnqueueOnNoStock` 用 `requestedVendorID` 判 auto·而非 `in.VendorID`)
- [ ] coalescer 生产接线 · **不做**(第六刀 · 骨架标记 · 见 `docs/03-modules.md § coalescer`)

## codex 六步收敛记录(2026-08-15)

按 codex 审计建议·把系统主动拉号收口到统一决策入口:
- 第一刀 · 建 `internal/decider/decide.go` 纯决策骨架
- 第二刀 · `bus.Scheduler` 接 `Decide(source=scheduler)`
- 第三刀 · `deathwatch.RefillTick` 接 `Decide(source=death_refill)`
- 第四刀 · 修 `maybeEnqueueOnNoStock` auto-pick bug
- 第五刀 · vendor 新号 webhook 触发 `Decide(source=webhook)` · 扫低水位 auto 车
- 第六刀 · coalescer 明确骨架 · 未接生产

## Enqueue 执行链闭合(2026-08-15 · fix: complete VerdictEnqueue execution path)

按 codex 建议 · 补齐三处 Enqueue 分支的真实执行 · 避免"文档说完成·代码只 log":
- `bus.Scheduler.doEnqueue` · 装配 `AutoEnqueuer` · 真调 stockwatch.Enqueue
- `deathwatch.RefillTick` Enqueue 分支 · 装配 `RefillEnqueuer` · 挂完标 pending_refill=fulfilled(note=enqueued_to_stockwatch) · **不无限 pending 空转**
- `webhookAutoScanBridge` Enqueue 分支 · 直用 stockWatcher.Enqueue

**资金语义**:挂意图不预冻结(ReservedAmount=0)· fire 时走 decider.Pull 完整钱包事务。跟 `maybeEnqueueOnNoStock` 一致 · Tight 期间不冻死大量用户资金。

**规格测试**:
- `TestSchedulerDecide_Enqueue_CallsEnqueuer` · 闭环
- `TestSchedulerDecide_Enqueue_NoEnqueuer_LogsOnly` · nil-safe
- `TestRefillTick_DecideEnqueue_CallsEnqueuer_MarksFulfilled` · 挂完标 fulfilled
- `TestRefillTick_DecideEnqueue_NoEnqueuer_Reschedules` · nil-safe

## 依赖 / 阻塞

- 1c 完成（拼车分摊已通）
- 位置 2 待用户决策付费优先能力方向
- **1d 调度闭环完成**:自动补车链路(1a-1d)已全部收口到 `decider.Decide` · Enqueue 分支已真调 stockwatch

## 上线判据

- [x] 号死后 1min 内自动补车 · 生产验证过
- [x] vendor 余额不够自动切下一家 · 单测覆盖
- [ ] 自动模式 scheduler 六条位置全部拍板 + 落码 + 验证
- [ ] 陈旧管线告警真外发到 BP_ALERT_WEBHOOK（生产验证）

**归档时机**：1d 六条位置全通生产 · 挪到 `docs/archive/`。
