# 17 · Vendor 相关全套工作编排（三层）

**为什么写这份**：我一直把工作揉成两大坨"基础设施 / 用户体验"· 用户点出这不对 ·
真实结构是**三层**：

- **A · 跟上游对接**（拿数据回来 · vendor 那头是黑盒 · 我方只能观测和调）
- **B · 我方内部策略和程序**（数据到手后怎么组织 · 抢号 · 落账 · 状态机）
- **C · 用户端体验**（把 A 采到的数据 + B 定的策略 · 用户能看到能配置）

每层各自盘：**每项 · 现状 · 剩余 · 上线状态**。上线一件 · 回来更新一件。

**盘点日期**：2026-08-12 · 生产 vps22 跑的镜像是 8-12 04:26 版（8-12 早我 push 的 5 个
commit 都没上线）· 本地 HEAD `8875864`（docs 那条）。

---

## A · 跟上游对接（观测 + 调 vendor 的能力）

### A0 · 6 家 vendor 端点全量摸底 · **v2 完成**（2026-08-12）

**用账号密码逐家登进后台 + Playwright 抓文档 + Network 抓隐藏端点**（v1 是探针式 curl · v2 是逆向）。详见 `docs/vendors/_endpoints-audit-2026-08-12-v2.md`。

**总量**：官方端点合计 ~100 个（91kiro 30 / kirooo 32 / kiroappio 25 / kiroappcc 19 / kirodrop 8 / kiroceo 9）· **我方 adapter 已接 45 个（覆盖率 45%）**。

**共性 · 6 家都有的最小闭环**（我方全接了 · 抢号链能跑）：
- profile / stock / purchase(client_order_id 32hex 幂等) / keys / orders
- webhook set + test + new_keys_available + all_keys_dead
- 兑换码 · 双区 us/eu · 质保退款

**紧急缺口 · 稳定性护栏**：
- ❌ **kirodrop `max_total_cny` 涨价保护** · 一行改 adapter · 防"vendor 突然涨价我们照买"
- ❌ **91kiro `max_keys_held` 持有上限** · 防"号堆着不用还占额度"

**中等缺口 · 抢号能力**：
- ❌ **kirodrop US/EU 合并通知** · 现按老逻辑走可能少收 EU 一半
- ❌ **kiroappio visibility=public/private** · 会把自留车当公开车抢
- ❌ **kiroappio key_revoked_abuse** · vendor 主动收回我方号事件
- ❌ **91kiro reserved_keys_delivered** · 包量预留自动到货（不 purchase）

**未来 · 反向变现（阶段 3+）**：
- ❌ **91kiro / kiroappio / kiroappcc 母号供应侧全套** · 我方投放 AWS 母号进 vendor 号池 · vendor 帮开号卖出后分成回来
- ❌ Kirooo auto-fleet 用户端配置（我方作为 kirooo 客户享受）

**状态**：✅ 摸底完 · **各家端点已并进 `docs/vendors/{91kiro,kiro-ceo,kiro-ooo,kiroapp-io,kiroapp-cc,drop-kiro-ss}.md`**（不新开 audit 文档）· raw 抓包留本机 `.playwright-mcp/vendor-scrape-2026-08-12/` 不入库

---

### A1 · 探针（60s stock 采样）

**做什么**：每 60s 打 6 家 `/api/my/stock` · 采到货量 / 价格 / 单区可购 · 落 `vendor_probe` 表。

**现状**：
- ✅ 代码：`internal/vendorview/prober.go`
- ✅ **生产在跑**：过去 1 小时每家 400+ 条样本 · 均匀
- ✅ 自适应频次（cool 60s / hot 10s）代码就绪 —— **未上线** · 生产还是 60s 固定
- ⚠️ kirooo 探针拿单值 stock · 改打 `/api/my/stock/regions` 分区版本 —— **未上线**

**剩余**：跟 B 层一起上线 · 本身没独立工作

---

### A2 · Fleet Backfiller（5min 拉批次历史）

**做什么**：5min 一轮 · 有 fleet 端点的 4 家（kiro91 / kiroceo / kirooo / kiroappcc）拉批次历史 · 落 `vendor_dispatch source=vendor_self`。

**现状**：
- ✅ 代码：`internal/vendorview/backfiller.go`
- ✅ **生产在跑**：kiroceo 100 条 vendor_self 数据（说明它在采）
- ⚠️ kiroappio / kirodrop **无 fleet 端点** —— 靠探针 stock-delta 推算兜底（未上线）
- ⚠️ kiroappcc `/openapi/orders` 老档案说没有 · 实测存在 · adapter 修好了 —— **未上线**

**剩余**：跟 B 层一起上线

---

### A3 · Vendor Webhook（vendor 主动 push · 最快信号）

**做什么**：6 家 vendor 上货时 push `new_keys_available` 给我方 · 落 vendor_dispatch + 触发抢号链。

**现状**：
- ✅ 代码：`internal/webhookin/dispatcher.go` + 6 家 adapter 各自 `ParseWebhook`
- ✅ HMAC 验签（kiro91 / kirodrop / kiroappcc）
- ✅ 幂等去重（`inbound_webhook_event` 表 · migration 025）
- ✅ **生产真的在收**：kiroceo 100 条 vendor_self dispatch · 但那是**8-11 22:09 之前**的
  · 8-11 22:09 之后没数据 —— 需要观察是 vendor 没上货还是 webhook 断了
- ✅ 抢号链接入代码就绪（`onNewKeys → watcher.Notify`）· **未上线**

**剩余**：**上线后关键观察点** —— vendor push 到我方是否 200ms-2s（预期 · 抢号链靠它）

---

### A4 · Xi8 对账数据源（内部数据 · 不出前端）

**做什么**：拉 xi8.cc 的 restock-log + signals · 作为**校对基准**（不是主数据源）。

**现状**：
- ✅ 代码：`internal/xi8/` · CLI `xi8-backfill` · `xi8-audit`
- ✅ **生产库有 202 条 xi8 数据**（说明有跑过 backfill）· 最新 8-12 06:04 —— **是服务重启前手动跑过 CLI**
- ✅ vendor_dispatch.source 列分开 vendor_self / xi8 · 前端只查 vendor_self
- ⚠️ xi8 后台 backfiller（30s signals + 5min full 自动跑）代码就绪 —— **未上线**
- ⚠️ 5 家匿名映射实锤（脆脆=kiroceo / 羽毛=kiroappio / 南南=kirodrop / 小鸡=kirooo / 饭饭=kiro91）· kiroappcc 不在 xi8

**剩余**：上线自动 backfiller · 之后不再手工跑 CLI

---

### A5 · 生产库脏数据（**必须先清 · 阻塞 A/B/C 所有观察**）

**做什么**：kirooo + kiroceo 早期时区 bug 遗留数据（存进去的时间早了 8 小时）· dev 洗了 · **生产没洗**。

**现状**（实测 2026-08-12 06:35 UTC）：
- ❌ `vendor_dispatch` 里 kiroceo 至少 **24 条 `dispatched_at > now+1h`**（时区污染）
- 更全的：还要查 `>now` 的（不止 24 条）+ `vendor_probe.ps_started_at`（kirooo）+ vendor_key
- 影响面：`window` 过滤全歪 · Status 页曲线错位 · Prices 均值污染 · Freshness 打偏

**剩余**：一次性 SQL · 只针对 kirooo + kiroceo · 减 8 小时
```sql
UPDATE vendor_dispatch SET dispatched_at = datetime(dispatched_at,'-8 hours')
 WHERE vendor_id IN ('kirooo','kiroceo') AND dispatched_at > datetime('now');
-- 同样处理 dead_at · vendor_probe.ps_started_at · vendor_key 各列
```

---

## B · 我方内部策略和程序（数据到手后怎么组织）

### B1 · Housepool 号池（kiro.rs · 我方运维）

**做什么**：号进池 · 分组 · 探活 · 交付。

**现状**：
- ✅ 代码：`internal/housepool/kirors/`
- ✅ **生产在跑**：kiro.aibbq.xyz · Up 22 小时
- ✅ Registry / BatchImport / Groups / KeyHealth / Concurrency 5 项能力全接
- 无剩余工作

---

### B2 · 状态机 · pending_purchase（拉号 5 状态）

**做什么**：`initial → reserved → purchasing → purchased → imported → completed` 崩溃恢复。

**现状**：
- ✅ 代码：`internal/decider/state.go` · `janitor.go` · `recovery.go`
- ✅ 单测齐 · 生产在跑
- 无剩余

---

### B3 · 拉号策略 · CanPull 判护栏（全局 + 车级）

**做什么**：判 daily 限额 / 单价上限 / 余额。

**现状**：
- ✅ 代码：`internal/strategy/canpull.go`
- ✅ 生产在跑
- ⚠️ **发现车级 daily_round/spend 是死字段**（`decisions §11.16`）· 挂在讨论清单

**剩余**：讨论清单里排 · 现在不做

---

### B4 · 抢号链（缺货挂单 → webhook 唤醒 → fire）

**做什么**：vendor 缺货时挂 `stock_watcher` · restock 事件唤醒 · fire 走完整 Pull。

**现状**：
- ✅ 代码全套：`internal/stockwatch/` · migration 027 · `decider.FireWatcher` · main.go 装配
- ✅ 三层开关：急停 > turbo 强制 > ModeMgr 自动
- ✅ 两文件哨兵：TURBO_ON / KILL_PULLS · 运维 SSH touch 即时生效
- ✅ **端到端模拟测试 3/3 全绿**（`buyrace_e2e_test.go` · DryRun 不花钱）· 但只是本地
- ❌ **生产未上线** · migration 027 未应用 · `stock_watcher` 表不存在
- ❌ **生产真流量一次 fire 都没跑过**
- ⚠️ Region 命名 3 套不统一（zone / vendor region / dryrun region）· 上线后大概率所有挂单匹配不上 webhook · 需修

**剩余**：上线（跟 A 层同一次部署） + 观察真实 fire 率 + 修 region 命名

---

### B5 · Deathwatch（号死后触发退款 + 补车）

**做什么**：vendor `all_keys_dead` 或探活推 dead → 扫号池 · 退款 · 触发补车。

**现状**：
- ✅ 代码：`internal/deathwatch/`
- ✅ 生产在跑
- ⚠️ 补车只标死 · **不主动触发新一轮拉号** —— 是 1d 才做的事
- ⚠️ 抢号链上线后 · 补车可以走 `stock_watcher.Enqueue`（挂单等 restock） —— **未接**

**剩余**：抢号链验证稳定后 · 让 deathwatch 触发挂单（而不是失败退款）

---

### B6 · 加速预存池（无排队时预抢 · 5min TTL）· 未做

**做什么**：探针见 vendor 出货 + 我方没排队 · 主动抢 3 个进 `prebuy-pool` group · 5min 内无人认领退 vendor。

**现状**：
- ❌ 全没做
- 明确"能抢到"的最后一块 · 但**当前场景（1 人自用）用不上**
- 归 `decisions §11.15 加速预存池` 部分（未落码）

**剩余**：等抢号链在生产真跑一波 · 观察是否需要（有真数据再评估）

---

### B7 · 抢号服务费（用户加倍率优先派发）· 明确暂缓

**做什么**：加钱插队 · 队列排序。

**现状**：讨论清单 · 明确"现在不做 · 归 `decisions §2.6&2.7` 阶段 2c"

---

## C · 用户端体验（前端 + 用户可配置）

### C1 · Status 页（vendor 状态展示）

**做什么**：6 家 vendor 卡 · uptime / stock / dispatch 曲线 / quality 标签。

**现状**：
- ✅ 页面在（`web/src/pages/Status.tsx`）
- ✅ 6 家共享 x 轴 heatmap · 24h/7d/30d tab · Quality 标签
- ✅ 数据源：`useVendorStatus` / `useVendorDispatchEvents` / `useVendorStatusTrend`
- ❌ 没暴露 mode（cool/balance/tight）· 没暴露"当前有几个挂单在等"
- ❌ 前端 hook 用的是**旧的老 hook**（部分数据源 A5 脏数据修完前都是歪的）

**剩余**：A 层上线 + A5 洗数据后 · 加两个展示（mode + 队列长度）

---

### C2 · Prices 页（价格趋势）

**做什么**：6 家单价历史 · 每日高低点 · 供用户选便宜的时候拉。

**现状**：
- ✅ 页面在（`web/src/pages/Prices.tsx`）· 用 `useVendorPrices` 拿数据
- ✅ 后端 `/api/vendors/{anon_id}/prices/daily` 端点写了（返 min/max/avg/samples · 数据源 `vendor_probe.sample_price_micro`）
- ❌ **前端零个 hook 引用 daily 端点** —— 后端写了没人用
- ❌ 现在 Prices 页拿的还是老 `useVendorPrices` · 那接口返 30 天 rounds stub · 数据不真

**剩余**：
1. 前端加 `useVendorPricesDaily` hook
2. Prices 页接上 daily 端点
3. 老 `useVendorPrices` 决定保留还是废弃

---

### C3 · 拼车前后端接口对接

**做什么**：建车 / 加成员 / 邀请码 / 拉号 / 策略 / 匿名撮合。

**现状**：
- ✅ 后端 15 个 handler 齐（Create/List/Get/Update/Members/Match/Join/RegenInvite 等）
- ✅ 前端 12+ hook 在用（`useBuses` / `useBus` / `useBusCredentials` / `useCreateBus` / ...）
- ⚠️ 有没有断层 · 匿名撮合流程能不能跑通 · 邀请码流程 —— **未审计**

**剩余**：**审计** · 后端 handler 逐个对前端 hook · 找缺口 · 修

---

### C4 · 拼车全局配置 · 4 个讨论缺口

**做什么**：设置页拉号偏好 · 车级策略 tab。

**现状**：
- ✅ 页面在（`Preferences.tsx` / `BusDetail.tsx` 策略 tab）
- ⚠️ 4 个讨论缺口（`docs/16-buy-race.md`）· 全是配置类：
  - 缺口 1 · 单价上限白天/夜间两档 · **未做**
  - 缺口 2 · 预留开关分摊风险 · **未做**
  - 缺口 3 · 车级 daily 死字段清 · **未做**
  - 缺口 4 · 加倍率优先派发 · **明确不做**（归 `§2.6&2.7`）

**剩余**：A/B 层稳定 + 队列有真数据后 · 拍板 1/2/3 顺序

---

### C5 · Extract / 提取（handoff）· 已完成

**做什么**：号交付给乘客 · fire-and-forget 拿走。

**现状**：
- ✅ 代码 + 页面齐
- 无剩余

---

## 整体状态一览

| 层 | 项 | 代码 | 单测 | 生产上线 | 生产在跑 | 剩余 |
|---|---|---|---|---|---|---|
| A | A0 端点能力矩阵 | ✅ | ✅ | ✅ | ✅ | 无 |
| A | A1 探针 | ✅ | ✅ | 部分 | ✅ | 自适应频次未上线 |
| A | A2 Backfiller | ✅ | ✅ | 部分 | ✅ | kiroappcc /orders 修未上线 |
| A | A3 Vendor Webhook | ✅ | 部分 | ✅ | ✅ | 抢号链接入未上线 |
| A | A4 Xi8 | ✅ | 无 | 手工跑过 | ❌ 后台没跑 | 后台 backfiller 未上线 |
| A | **A5 洗生产脏数据** | N/A | N/A | **❌** | N/A | **一条 SQL** |
| B | B1 Housepool | ✅ | ✅ | ✅ | ✅ | 无 |
| B | B2 状态机 | ✅ | ✅ | ✅ | ✅ | 无 |
| B | B3 CanPull | ✅ | ✅ | ✅ | ✅ | 死字段挂讨论清单 |
| B | **B4 抢号链** | ✅ | ✅ | **❌** | **❌** | **上线 + 观察 + 修 region 命名** |
| B | B5 Deathwatch | ✅ | ✅ | ✅ | ✅ | 补车触发挂单未接 |
| B | B6 加速预存池 | ❌ | ❌ | ❌ | ❌ | 等真数据决定 |
| B | B7 抢号服务费 | ❌ | ❌ | ❌ | ❌ | 明确暂缓 |
| C | C1 Status 页 | ✅ | ✅ | ✅ | ✅ | 加 mode / 队列展示 |
| C | **C2 Prices 页** | ⚠️后端有前端未接 | ✅后端 | 部分 | ✅ | **前端接 daily** |
| C | C3 拼车对接 | ✅ | ✅ | ✅ | ✅ | 审计 |
| C | C4 拼车配置 | ✅ | ✅ | ✅ | ✅ | 4 缺口等拍板 |
| C | C5 Extract | ✅ | ✅ | ✅ | ✅ | 无 |

---

## 现在的推进顺序

**第 1 步 · 现在就做 · 一次做完不问**：
1. **A5** 洗生产脏数据 · 1 条 SQL（先备份 · 只针对 kirooo + kiroceo · 只减未来时间的）
2. **B4 部署** · 推 vps22 拉新镜像（等 GHCR build）· 跑 migration up 027
3. **A1/A2/A3/A4 一起就位**（同一次部署 · 都是本地已 push 的 commit）
4. 观察 15 分钟：`docker logs kirobus | grep "抢号链已装配\|xi8 backfiller\|Notify"`

**第 2 步 · 24-48 小时观察窗**：
- B4 生产真流量 fire 率
- 如果 fire 率 = 0（region 命名 bug）· 修 A5-B4 联动
- Xi8 后台 backfiller 稳吗

**第 3 步 · 数据攒 3-7 天后 · C 层前端**：
- C2 · Prices 页接 daily 端点
- C1 · Status 页加 mode / 队列展示
- C3 · 拼车接口审计

**第 4 步 · A/B/C 都稳后**：
- C4 · 4 讨论缺口拍板
- B5 · Deathwatch 触发挂单
- B6 · 加速预存池（若真需要）

---

## 记录约定（防再跑偏）

- 上线一件 · **立刻回来更新对应节** `生产上线` 列（❌→✅）
- 观察发现新问题 · 追加到对应节 · 不新造独立编排线
- 只有本文档标为**生产上线 ✅**才算完 · 本地代码不算完
