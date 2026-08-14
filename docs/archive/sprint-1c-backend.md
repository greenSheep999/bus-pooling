# sprint-1c · 后端主线交付清单

> **本文只写"1c 要做什么 + 上线前还剩什么"**。技术细节 / 决策讨论 / 长期契约 →
> 分别去 A 契约（00-19）/ D 决策（decisions.md）· 别塞进这里。
>
> 阶段 1c 目标（`00-values-and-phases §7`）：**搭车（anon）系统撮合 + 邀请码（team）
> 两条多人拼车路径同时上 + 号价按 N 分摊 + 集单调度骨架**。无自动、无压车治理。

## 交付清单

- [x] 匿名撮合 · `POST /api/me/buses/anon/match`
- [x] 显式加入 anon bus · `POST /api/me/buses/{bus_id}/join`
- [x] 邀请码加入 team bus · `POST /api/me/buses/join-by-invite`
- [x] 号价 N 分摊 · `decider.planSplit` + `pull_round.participants_split_json`
- [x] 挂起 + 付不起踢人 · `decider.split.go`
- [x] 集单调度骨架 · `coalescer/window.go` 库层就绪（同步窗口 200ms · MaxBatch 8）
- [ ] vendor 侧真集单接入 API（Anon/Team 接进 handleBusPull）· **延后到量上来再评估**

## 依赖 / 阻塞

- 1b 完成（5 vendor + payment 全通）

## 上线判据

- [x] 匿名撮合走通端到端
- [x] 邀请码加入走通端到端
- [x] 多人 bus 拉一次 · 号进 bus group · 钱按 planSplit 分摊冻结 · 落
      `reserve_split_json` · 各人 wallet_ledger 分别扣
- [ ] 死号退款按分摊比例退回每个成员（deathwatch.RefundOnce 已实现 · 待生产验证）

**当前状态**（08-15）：**1c 主体已在生产**。剩 vendor 侧真集单是优化 · 不阻塞上线。

**归档时机**：1c 主体验证稳定 + vendor 侧集单决策落定 → 归档。
