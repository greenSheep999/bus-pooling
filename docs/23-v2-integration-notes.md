# 23 · v2 集单 · API 层接入的架构障碍（先记录 · 早上审）

## 我今晚做了什么

**v2.1 · coalescer/window.go 集单窗口库层**（`d2be162`）：
- 同步阻塞 window · 200ms Duration / MaxBatch=8
- windowKey = (bus_id, zone, vendor_id) · 只合并这三者相同的意图
- Executor 接口 + AnonV2 入口
- 12 条测试全绿

## 为什么没直接接到 api/bus.go

**关键发现**（写代码时才看清）：现有 `handleBusPull` 已经在做**"分摊层面"的集单**：
- 每次一个人点拉号 · decider 内部 `planSplit` 把整个 bus 的钱按 count/占比分给全员
- `pull_round.participants_split_json` 已经记 {passenger_id: 号数}
- 所以**我方账本层面**已经是"拼车分摊"了

**真正的 vendor 侧集单**要做的是：**多人同时点拉号 → vendor.Purchase 只调一次**。
但 `decider.Pull` 是一整套原子性状态机（Reserve → Pending → Purchase → Import → Commit）·
window 无法只把中间的 `vendor.Purchase` 单独提出来共享 —— 那要拆 decider 主流程。

## 架构障碍 · 清晰描述

**假设**要真集单（vendor 侧一次 API 调用满足多人）：

1. **谁触发 vendor.Purchase**：Alice 点拉号 · 200ms 内 Bob 也点 · 到时谁的 goroutine
   去打 vendor？—— window 已经解决这个（首个加入者的线程 · 或 executor 装配层）
2. **Reserve 冻结在谁账上**：现在 decider 直接 `wallet.Reserve(Alice.total_after_split)`·
   合流后 · Alice 要 reserve 她那份 · Bob 要 reserve 他那份 · **两笔独立 reserve**
   （不能一个人替所有人 reserve · 违反 §8.18）
3. **pending_purchase / pull_round 是共享还是各建一条**：
   - 方案 A · 一条 pull_round · N 个 passenger 分摊（现在的模式）
   - 方案 B · N 条 pull_round · 关联同一个 vendor_order（新模式）
4. **失败回滚**：vendor.Purchase 挂了 · 谁的 reserve 要释放？—— 现在 decider janitor
   有超时释放 · 但需要按 batch 语义改
5. **响应结构** · pullResponse 里 CredentialIDs 只属于本人 · 需要按 count 分账（现在
   splitPlan 分金额 · 但号本身是共享池 · 后续 handoff 才按人挑）

## 短期方案（早上你审）

**选项 1 · 用现在的 v2.1 库层 + 不动 API**（保守）
- v2.1 window 库测过 · 但不接入
- 老 handleBusPull 保持"分摊层面集单"（我方账本已合流）
- **vendor 侧仍 N 次调用**（还没省）
- 好处：0 风险 · 老行为不变 · 库层就绪等 API 侧重构
- 坏处：用户"太慢了"的问题没解 · 高并发时 vendor 侧还是压力大

**选项 2 · handleBusPull 前置一个 batch collector**（激进 · 一整天工作量）
- API 层收到请求 · 不直接 decider.Pull · 而是塞 window
- window 关时 · executor 用 batch 里的**代表参与者**触发一次 decider.Pull
- decider 内部 splitPlan **改成用 batch.Participants 而不是从 bus_member 拿全员**
- 每个 API 请求 goroutine 从 BatchResult 里挑自己那份返
- 需要改：decider.PullInput 加 `SplitOverride []splitMember` · orchestrator 判非空用它
- 需要改：pullResponse 加 `BatchID` / `Participants` 字段（选做）

**选项 3 · 承认 v2 不做集单 · 只优化 vendor.Purchase 层**（中间路线）
- 保留 handleBusPull 每人各自跑 decider.Pull
- 但在 vendor adapter 层加"purchase 请求聚合" —— 200ms 内同 vendor 同 zone 的
  Purchase 合并成一次 HTTP · 各自等结果
- 好处：改动局限在 vendor 层 · 不动 decider / API 契约
- 坏处：**vendor 侧幂等键怎么办**？每人 clientOrderID 不同 · 合并成一次调用需要
  用一个共享 clientOrderID · 但那样每人的 pull_round 又对不上 vendor_order · 状态机断链

## 我的建议

**选项 1 保守派** —— 库层做完（v2.1 · 已完成）· API 接入等你早上决定架构方向。

**理由**：v2.2 三选一都是架构级改动 · 半夜做完早上你未必认这个方向 · 到时回滚成本大。
选项 2 是"用户想要的样子"但需要跟前端沟通响应结构；选项 3 是"隐藏地做"但幂等对不上。

**明天你审这份文档后拍板选哪个 · 我按你选的一次做完不再兜圈**。

## v2 已经收到的价值（不接入 API 层也已经能用）

- window 库单独有用：**未来任何"多人合并调用"场景**（不只 bus pull · 比如批量对账）
- Executor 接口独立：可以用来做**vendor.Balance() 合并 polling**（多个模块要查同一 vendor 余额 · 合并成一次调用）
- BatchResult 广播模型：可复用做**其他需要"多人等一个共享结果"**的场景

## 早上 checklist

- [ ] 你审这份文档 · 拍板选项 1 / 2 / 3
- [ ] 我按你选的做完 v2.2
- [ ] 部署 · 生产验证（v1 已 push · CI 已过 · 等你说部署我立刻做）
