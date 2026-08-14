# sprint-1f · 策略收口 · 范围（暂定）

> **本文只写"1f 要做什么"** · 详细决策讨论见 `docs/decisions.md §13`。
>
> **1f 位置**：1e 收官后发现"策略模型没有收口" · 不能直接开 2a(阶段号严格接 `CLAUDE §7`) ·
> 补上这批漏才能开新范围。**1e code-complete 但不算 feature-complete** · 1f 后才是。

## 1f 目标（1 个 sprint）

**收口整个"策略层"** —— 让全局默认 / 车级覆盖 / 本次请求约束三层用一份权威优先级 · 一个函数吐最终值 · 全代码路径都走同一入口。

## 交付清单

按优先级(高 → 低)：

### 1f-A · 策略优先级铁律（最高优 · 别的都依赖）

- [ ] `docs/15-scheduling.md` 新增 §6 · 策略优先级铁律
  - 固定顺序:`本次请求约束 > 车级策略 > 全局默认策略 > 系统默认值`
  - **硬上限字段**(max_unit_price / daily_spend_limit / daily_round_limit) · 取 min 不是取覆盖
  - **覆盖字段**(preferred_vendor / zone / per_round_count / auto_refill_* / refill_*) · 后者直接盖
- [ ] `CLAUDE.md §1` 铁律段引用 · 未来 AI 别再重讨论

### 1f-B · 全局策略字段对齐 + 车级覆盖层

- [ ] 逐字段对表 `docs/06-db-schema.md` / `docs/05-api-contract.md` / `docs/03-modules.md` / `web/src/types/index.ts`
- [ ] 每字段标清 6 项:来源 / 默认值 / 是否可空 / 是否硬上限 / 是否参与自动调度 / 是否展示给用户
- [ ] 车级 `bus.max_unit_price / preferred_vendor / auto_refill_* / refill_*` 明确"覆盖 or 硬上限"归类
- [ ] 前端 `EditStrategyPanel` UI 语义:"跟随全局默认 / 覆盖" 二态显式化

### 1f-C · Effective() 函数收口

- [ ] `internal/strategy/effective.go` 新增 `Effective(ctx, passengerID, busID, override) → EffectiveStrategy`
- [ ] decider / autoscheduler / deathwatch / stockwatch / bus.Scheduler 全走这一个函数
- [ ] 返值是**已经算好优先级的最终值** · 调用方不再拼字段

### 1f-D · 015 调度文档升级为权威入口

- [ ] 从"系统缺货挂单文档"扩为**权威调度入口**
- [ ] 三部分:
  1. 三条车路径(single / anon / team)如何进入统一调度模型 · 图 + 一段话
  2. 6 触发源(webhook / deathwatch / scheduler / probe / coalescer / manual)输入 / 调用 / 输出边界表
  3. `stockwatch.Enqueue / decider.Pull / assign / refund` 状态机 + 时序
- [ ] 别用"已接通"这种模糊词 · 按 API/DB/state/test 逐项对齐

### 1f-E · /docs 对接文档扩

- [ ] `docs/05-api-contract §2` API key 段扩:一次性明文 · 列表只返 prefix · 吊销语义 · 权限收窄
- [ ] `web/src/pages/Docs.tsx` 加两卡:
  - endpoint matrix(哪些接口支持 API key / 哪些必须 session / 哪些必须 idempotency key)
  - request/response 全字段(用户对接方能直接抄)

## 依赖 / 阻塞

- 1e code-complete(已完成 · 8 commit 上线)
- 无外部阻塞

## 1f 上线判据

- [ ] `internal/strategy.Effective()` 有单测覆盖 6 字段 × 3 层(全局/车级/请求)
- [ ] decider / bus.Scheduler / deathwatch 全部走 Effective() · grep 应扫不到旧的"手工拼字段"
- [ ] `docs/15-scheduling.md` 一份文档能让新人理解整个调度链路
- [ ] `/docs` 用户能只看文档就完成对接(不需要问支持)

## 归档时机

跟 sprint-1a/1b/1c/1d/1e 一起 · 阶段 1 收官(见 `sprint-1-final.md` Stage 7)后归档到 `docs/archive/`。

---

## 完成 = 阶段 1 feature-complete

1e code-complete + 1f 策略收口 = **阶段 1 feature-complete** · 之后进入**live-ready 序列**(mock/live 分阶段切真链路)。
