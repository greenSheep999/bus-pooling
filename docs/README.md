# docs/ · 文档架构公约

> **动手写 / 加任何文档前 · 先读本文**。
>
> 08-14 到 08-15 因为文档失控（21/22/23/24 四份文档 + v1/v2/v3/v4 私造版本号跟阶段号
> 1a-1e 双轨 · 契约 / 主线 / 讨论 / 任务 / 修补全糊在同一目录）· 用户拍桌重整。本文
> 是**唯一**的文档架构规则。**违反的文档一律 archive 或删**。

---

## 五类文档 · 每份归一类

| 类 | 用途 | 位置 | 命名 | 写完的动作 |
|---|---|---|---|---|
| **A · 契约** | "是什么 · 长期稳定的架构 / 数据 / 接口" | `docs/` 顶层 | 两位数字前缀（`00-` `01-` ...）| 只加 · 不删 · 慎改 |
| **B · 主线** | "当前阶段的产品需求 → 上线交付清单" | `docs/` 顶层 | `sprint-<阶段号>-<端>.md` | 阶段上线后归档到 `docs/archive/` |
| **C · 衍生** | "独立子系统 / 后期扩展的详细方案" | `docs/` 顶层 | 两位数字前缀（`14-` `15-` `16-` ...）| 落码后**合并回 A 类相关契约** · 不留分文档 |
| **D · 决策日志** | "为什么这样定 · 讨论 / 否决 / 拍板" | `docs/decisions.md` | 只有这一份 · 追加型 | 永不删 |
| **E · 一次性记录** | "诊断 / 手验 / 端点审计 / 老旧待废" | `docs/archive/` | 带日期前缀（`YYYY-MM-DD-*`）| 过 30 天没引用就删 |

### 五类的分界判据

**A 契约**：读的人问"这个系统是怎么架的？谁调谁？数据长什么样？API 什么形状？"
· 答案在 A · 改动应罕见 · 一年一次级别 · 是别人（新人 / AI / 前端）的锚点。

**B 主线**：读的人问"当前这个阶段要交付什么？上线前还剩什么？"
· 答案在 B · 每阶段一份 · 上线后归档 · **主线只谈这个阶段该做的事** · 不谈以后。

**C 衍生**：一个足够大的独立子系统（比如抢号链 · pricing 归一 · 号池承载）· 讨论量
超过 A/D 能容纳的时候 · 允许**临时**开一份 · **落码后必须合并回相关的 A 契约** ·
不能永久留一份分文档。

**D 决策**：`decisions.md` 是**唯一的**决策日志。所有讨论 / 否决 / 拍板都追加到这里 ·
按 `§N.M` 编号。不允许开第二份决策文档。

**E 一次性**：诊断 / 手验 / 端点审计 · 用完就该走 —— 挪到 `docs/archive/` ·
30 天没引用就删。

---

## 当前分类清单（08-15 重编号后 · 编号连续 · 主题分组清晰）

### A 契约层 · 16 份 · 长期锚点

**00-09 · 后端核心契约**：

```
00-values-and-phases.md       项目定位 + 阶段表（1a/1b/1c/1d/1e/2a/2b/3a/3b/3c/3d）
01-architecture.md            分层俯视图
02-flows.md                   端到端时序
03-modules.md                 15 业务包清单
04-scenarios.md               29 条产品场景
05-api-contract.md            乘客侧 HTTP API 契约
06-db-schema.md               数据库表
07-provider-contract.md       Provider/Vendor Go interface
08-housepool-contract.md      Housepool 客户端
09-transactions.md            状态机 + 补偿规则
```

**10-14 · pricing + 前端 + 部署契约**：

```
10-pricing.md                 pricing 一整套(观测 → 换算 → 三档 → 减免)· 权威
11-fields.md                  跨 vendor 字段对齐表 · 权威
12-frontend-pages.md          前端页面清单 + 路由
13-frontend-design.md         前端设计规范(调研 + 主题 + token + 用法)· 新页面必读
14-deployment.md              部署 runbook
15-scheduling.md              调度契约(三层字段 + 触发矩阵 + 反查表)· 是 01/02/03 的落地细化
```

**vendors/ · 上游 API 档案**：

```
vendors/README.md             vendor 档案骨架规范
vendors/91kiro.md · kiro-*.md 各 vendor 官方 API 档案
vendors/xi8.md                聚合站(非 vendor · 内部数据源)
```

### B 主线 · sprint · 每阶段一份 · 上线后归档

```
sprint-1-final.md             阶段 1 收官上线路线(mock/live 分阶段切换)· 1a-1f 全绿 → Stage 1-6 只需 env / 灰度
```

**已归档**（1a-1f 全部 · 2026-08-15 · 阶段 1 feature-complete 后归档）：

```
archive/sprint-1a-backend.md    阶段 1a 后端(生产在跑 · 已归档)
archive/sprint-1a-frontend.md   阶段 1a 前端(生产在跑 · 已归档)
archive/sprint-1b-backend.md    阶段 1b 后端(6 vendor / 兑换码 / payment-gateway · 已归档)
archive/sprint-1c-backend.md    阶段 1c 后端(anon 撮合 / team 邀请码 / 分摊 · 已归档)
archive/sprint-1d-backend.md    阶段 1d 后端(自动补车 / webhook 唤醒 / 比价 fallback · 已归档)
archive/sprint-1e-backend.md    阶段 1e 后端(推 passengerpool 双写 + 对外 webhook · 已归档)
archive/sprint-1f-scope.md      阶段 1f(策略收口 + Effective() 唯一入口 + /docs 扩 · 已归档)
```

### C 衍生 · 5 份 · 独立子系统方案 · 落码后合回 A

```
20-page-extract.md            提取页方案 → 落码后合回 12-frontend-pages
21-page-prices.md             价格页设计 → 落码后合回 12-frontend-pages
22-buy-race.md                抢号链子系统 → 落码后合回 09-transactions + decisions §11.15
23-endpoints-todo.md          端点补接施工蓝图 → 消化完删
24-category-subscription.md   Offer 模型(vendor×category×subscription×zone) → 接完后拆回各主文档
```

**C 类文件顶端**应标"归回目标 + 到期日"· 到期未归档 = 触发重整。

### D 决策 · 唯一

`docs/decisions.md`。

### E 归档

`docs/archive/`（老旧 vendor-work-order · diagnostics · e2e · endpoints-audit 等）

---

## 加新文档的自检

**加前先问三个问题**：

1. 这条内容属于**哪一类**？（A/B/C/D/E）
2. **有没有已有文档**能装下？（每份 A 契约都写了自己的边界 · 先看能不能塞进去）
3. 如果开新文档 · 它**多久后要归回契约或归档**？

**不通过就不许开新文档**。

---

## 铁律

- ❌ **不许**造第二套编号（v1/v2/v3/v4 / P0/P1/P2 / MVP-A/B/C）· 阶段号只用
  CLAUDE §7 + 00-values-and-phases.md §7 的 `1a-1e/2a-2b/3a-3d`
- ❌ **不许**新开 `docs/2N-xxx.md` 讨论已有 A 契约覆盖的话题 · 归到 D 决策日志
- ❌ **不许**在代码注释 / commit / 讨论中出现私造版本号（v3.2 / v4.4 之类）
- ❌ **不许**把 C 衍生长期留在顶层 —— 落码后必须回契约
- ✅ **每份 A 契约文档顶端**应有一行"本文管什么 · 不管什么" · 让读的人第一秒知道能塞什么
- ✅ 加决策 · 只加 `decisions.md` · 不开第二份
