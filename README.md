# bus-pooling

**多 provider 的号聚合与拼车中间层**。

一个乘客账号 → 接多家上游 provider 的 vendor（当前只做 kiro provider 下 6 家）→ 提供**聚合 / 比价 / Fallback / 拼车集单** 四大价值 → 号存我方 kiro.rs 号池 → 推乘客号池 / 拼车共享 / 乘客拿走。

**主入口是拼车**（最大价值），**次入口是单独拉号**（用户拉了自己派去向）。

**我们不是号商**：
- 不加价号成本（`pass-through`）
- 不做上游能做的（AWS 开号、vendor 内部号池、发车调度、vendor 拼车池）
- 不复刻号池能力（用 kiro.rs）
- **只收固定服务费**（每人每次拉号动作 1 元 / 1 USDT）
- 加**单次议价 20%**（`count == 1`；批量无议价）
- 加**通道费 5%**（waffo 收，`pass-through` 给乘客）

## 全景（一屏）

```
上游 6 家 vendor（外部）
  91kiro · kiro.ceo · kiro.ooo · kiroapp.io · kiroapp.cc · drop.kiro.ss
                       ↑ 我方 vendor adapter 拉号
                       │
     ┌─────────────────┴──────────────────┐
     │   bus-pooling · 中间层（本项目）    │
     │                                    │
     │   乘客账号 / 钱包 / 兑换码 / 支付   │
     │   策略引擎 / 集单调度 / 决策模型    │
     │   bus 实体 / 拉号记录 / webhook out │
     │                                    │
     └────────┬───────────────────────────┘
              ↓ BatchImport / PUT groups+disabled / DELETE
     ┌─────────────────────────────────────┐
     │   housepool（我方 kiro.rs）         │
     │   ─ credential 校验/探活/用量/并发   │
     │   ─ group: bus-<id> / record-<pid>  │
     └─────────────────────────────────────┘
                        ↓ 号出去（可选）
     ┌─────────────────────────────────────┐
     │   下游 · 3 种去向                    │
     │   ① 进车 (仍在 housepool 监控)       │
     │   ② 推乘客 kiro.rs (双写监控)        │
     │   ③ 拿走 handoff (离开系统)          │
     └─────────────────────────────────────┘
```

## 文档索引

**读文档顺序**（新人 / 未来 AI agent 都按这个顺序）：

| 序 | 文档 | 说明 |
|---|---|---|
| 0 | [`CLAUDE.md`](./CLAUDE.md) | 给未来 AI agent 的行为铁律（**动代码前必读**） |
| 1 | [`docs/00-values-and-phases.md`](./docs/00-values-and-phases.md) | 定位 · 三大价值 · 计费 · 3 阶段 · 业务规则 · 术语 |
| 2 | [`docs/01-architecture.md`](./docs/01-architecture.md) | 5 层分层 · 每层职责 · 目录清单 |
| 3 | [`docs/02-flows.md`](./docs/02-flows.md) | A-I 九条端到端技术时序 |
| 4 | [`docs/03-modules.md`](./docs/03-modules.md) | 业务包 15 上限 · 依赖图 · 模块清单 |
| 5 | [`docs/04-scenarios.md`](./docs/04-scenarios.md) | 31 用户场景路径（产品视角） |
| 6 | [`docs/decisions.md`](./docs/decisions.md) | 讨论过并否决的方案（**避免重复讨论**） |
| 7 | [`docs/vendors/*.md`](./docs/vendors/) | 6 家 vendor 官方 API 档案 |

**编码相关文档**（已就位）：`05-api-contract.md` · `06-db-schema.md` · `07-provider-contract.md` · `08-housepool-contract.md` · `09-transactions.md`（核心交易状态机）· `12-frontend-pages.md`（页面清单）· `13-frontend-research.md`（前端调研 + 主题）· `sprint-1a-frontend.md` + `sprint-1a-backend.md`（阶段 1a 前后端）。

**编码起手前尚待写**：`10-secrets.md`（阶段 1 落码时）· `11-testing-strategy.md`（首 e2e 前）· `13-deployment.md`（首上线前）。

## 快速定位

**"我想找 X"**：

- 号最终去哪 → `04-scenarios.md`
- 具体一次拉号里都发生了什么 → `02-flows.md` C/D
- 单次议价怎么算 → `00-values-and-phases.md §3` + `04-scenarios.md D6/D7`
- housepool 是什么 · 为什么不能省 → `01-architecture.md §2 Layer 4`
- 某家 vendor 的 API 长啥样 → `docs/vendors/<vendor>.md`
- 我该写代码在哪个包 → `03-modules.md`
- 有个想法要不要做 → `decisions.md`（很可能讨论过并已否决）

## 三阶段（长期路线，见 `00-values-and-phases.md §7`）

| 阶段 | 内容 |
|---|---|
| **阶段 1** | MVP 拉存推：6 家 vendor + 主入口拼车 + 次入口单独拉号 + 自动补车 + 双写 + handoff |
| **阶段 2** | 邀请码组队 + 列队策略 + 压车治理 |
| **阶段 3** | 数据图表 + 发车（乘客 AWS 转发 vendor）+ 市场分成 |

## 开发（占位）

```
Go + SQLite + kiro.rs client + SPA 前端（栈待定，见 §00.9 未决）
```

具体命令待 P1a 编码时补。

## 项目原则（防止重蹈旧项目 kiro-auto 覆辙）

1. **业务包上限 15 个** —— 破了要写清理由（旧项目 90+ 模块崩了）
2. **一份文档一件事** —— 定位在 `00`、层次在 `01`、时序在 `02`、模块在 `03`、场景在 `04`；不叠加
3. **不做上游能做的** —— vendor 内部的号池、拼车、发车调度全不复刻
4. **每阶段有"不做清单"** —— 防蔓延
5. **术语铁律** —— 见 `CLAUDE.md`；作废术语（solo bus / 混合上车 / 方式 A/B / 交付方式 B 是 fire-and-forget）永远不用
6. **decisions.md 记讨论过并否决的方案** —— 未来 AI agent 不再回滚讨论
