# Vendor 上游 API 能力档案

**原则：全量记录上游 API 的所有能力，不做筛选、不做主观解读。**

哪怕某个 endpoint 与"拉号"无关（例如 vendor 侧的 AWS 母号、结算、payout、积分兑换、使用统计、账号资料等），也**必须记录**。中间平台后续要基于这些档案做能力对比、做 Provider 抽象、做多 vendor 聚合分发/比价/fallback 决策——**先把上游是什么如实摊开来，才能谈我们要做什么。**

## 每份 vendor 文档的固定骨架

顺序 = 上游文档自身的顺序（不是"中间平台的价值主张视角"）。

1. **基础信息** — Base URL、状态、官方文档来源、抓取日期
2. **鉴权** — 所有支持的鉴权方式（API key / cookie / OAuth …）、token 前缀、rotate 规则
3. **概念/术语** — 上游自己的名词（车次、母号、公共车、留自用、质保等）
4. **计费/单价** — 币种、阶梯、单价冻结时机、免费条件
5. **账号 / Profile / Settings** — 所有 `/profile`、`/settings`、`/password`、`/api-key` 类端点
6. **库存 / 车次** — 所有库存查询、车次查询、跨区/单区端点
7. **拉号 / 补拉** — purchase / claim / order 详情、幂等语义、部分成交、返回形态**逐字段**
8. **积分 / 兑换 / 流水** — redeem、ledger、余额相关全部端点
9. **母号 / 开号 / 供应侧** — vendor 侧 AWS 母号管理、发车、supply、payout（**即使我们不做也要记录**）
10. **Webhook** — 所有事件类型、载荷、header、签名算法、重试策略、配置端点
11. **Key 剩余额度 / 使用同步** — usage 相关端点
12. **错误码与限流** — 全表：HTTP + code + 上游文案 + 处理建议（照抄，不精简）
13. **质保 / 退款** — 窗口、条件、退款流水类型
14. **本 vendor 特有的坑 / 与其他家不同处** — 只放可验证的事实，不做主张

## 汇总层（六份写完后再动）

六份都到位之后，另开 `docs/00-values-and-phases.md`：抽取每份文档的第 6/7/10 节做横向比较，映射到 Provider 抽象层字段，落 P0 只做哪家、P1 引入第二家时接口如何收敛。

**在六份档案就位之前，不动抽象层、不动阶段规划。**

## 材料来源

- 官方公开文档（`/api/docs`、`/docs`、`/api-docs`、`/openapi/*`）
- 旧项目 `kiro-auto/docs/vendors/vendor-api-inventory-2026-08-05.md`（954 行的六家契约对照）
- 旧项目 `kiro-auto/internal/vendors/{kiroappio,kiromanager,kiroceo,kirodrop,kiroapp,...}/` 里的 adapter 代码
- 官方前端 bundle（页面/资源 URL 在每份文档的第 1 节列出）
