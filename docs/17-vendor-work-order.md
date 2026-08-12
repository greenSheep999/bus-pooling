# 17 · Vendor 相关全套工作编排

**用途**：抢号链跑通那天后（`decisions §11.15`）· 我一直"做完一件停一件问一次" ·
用户明确要的是**基础设施全部准备好 · 再优化前端体验**的完整推进 · 不是让他挑一件。
这份文档把跟 vendor 对接 + 我方业务相关的所有工作列全 · 每项写明现状 / 剩余 / 依赖 /
上线顺序 · 对齐后按顺序推 · 不再挑一件停一件。

**编排两大层**：

- **A 基础设施层**（用户感受不到 · 但是根基 · **必须先做扎实**）
- **B 用户体验层**（前端展示 / 交互优化 · **基础扎实后再做**）

上线一件 · 观察一件 · 再推下一件。

---

## A · 基础设施层（用户感受不到）

### A0 · 生产库脏数据洗掉（阻塞所有 A1-A5 · 最优先）

**现状**（2026-08-12 06:35 UTC 实测 · vps22）：
- `vendor_dispatch` 里 **kiroceo 24 条 `dispatched_at > now+1h`** —— 时区 bug 遗留脏数据
- dev.db 洗过（8-12 早）· **vps22 从没洗过**
- 探针 60s 在采（服务在跑）· 但 8-12 上午我 push 的 5 个 commit（stockwatch / migration 027 / kirooo 端点修 / 时区 fix）**都没上线**

**剩余工作**：
1. 推 vps22（等 GHCR build · 用现有 docker-compose pull）
2. 上线后跑 migration 027（stock_watcher 表建）
3. 一次性 SQL 洗生产库脏数据（同 dev.db 做过的那条 · 只针对 kirooo + kiroceo）
4. 确认服务重启后打 `抢号链已装配` 日志

**依赖**：无 · 直接做

**上线顺序**：**第 1 步** · 不做完这个后面全是错数据上的花活

### A1 · Vendor 监测（探针 + fleet backfill）· 基本完成

**现状**：
- ✅ 6 家 vendor 60s 探针（`vendorview.Prober`）
- ✅ 5min Backfiller 拉 fleet 端点（`vendorview.Backfiller`）
- ✅ 探针自适应频次（cool 60s / hot 10s · `§11.12`）—— 但**线上跑的是旧镜像 · 还没这个逻辑**
- ✅ stock-delta 推算（`§11.9`）· 补 4 家无 fleet 端点的 vendor —— 同上 · 未上线
- ✅ kirooo `/api/my/stock/regions` 分区拉法（`§11.10 kirooo 采集诊断`）· 同上未上线

**剩余工作**：
- 无独立工作 · 跟 A0 一起上线就位

**上线顺序**：**跟 A0 同一次部署**

### A2 · Webhook 对接 · 6 家全通 · 抢号链未接

**现状**：
- ✅ 6 家 vendor adapter 都实现了 `webhookPayload` parser（`new_keys_available` / `all_keys_dead` / `warranty_refund`）
- ✅ 生产 kiroceo 收到 100 条 vendor_self dispatch 事件（webhook 通了）
- ✅ HMAC 验签（`kiro91` / `kirodrop` / `kiroappcc`）
- ✅ 幂等去重（`inbound_webhook_event` 表 · migration 025）
- ✅ 抢号链 `webhookin.onNewKeys → watcher.Notify` **代码写了** · 未上线

**剩余工作**：
- 无独立工作 · 跟 A0 一起上线

**上线顺序**：**跟 A0 同一次部署**

**已知遗留**（推 vps22 后要观察 · 不阻塞上线）：
- Region 命名口径不统一（`docs/16-buy-race.md` 缺口 5）· 上线后**大概率所有挂单都匹配不上 webhook** ·
  修法：`providers.ZoneOf(region)` 归一 · 加到 A5 里做

### A3 · Xi8 对账 + 补历史（内部数据源 · 不出前端）

**现状**：
- ✅ `internal/xi8/` client + backfiller · 30s signals + 5min full
- ✅ 5 家 xi8 vendor 匿名映射实锤（kiroappcc 不在 xi8）
- ✅ `bus-pooling xi8-audit --window=48` CLI · 对账 vendor_self vs xi8
- ✅ vendor_dispatch source 列（migration 026）· xi8 数据落 `source='xi8'` · 前端只查 vendor_self

**剩余工作**：
- 上线后跑一次 `xi8-audit` · 看漏采差异（对齐 vs 修补基准）
- xi8 数据用途边界文档没写死：**是"参考"还是"补历史空窗填前端展示"**（讨论过 · 未定稿）

**上线顺序**：**A0 后立即** · 拿它做基准验 A1/A2 数据质量

### A4 · 抢号策略 · 内部运营（代码就绪 · 未上线未验证）

**现状**（`decisions §11.15`）：
- ✅ 三层优先级：急停 > turbo 强制 > ModeMgr 自动
- ✅ 两开关文件哨兵：`TURBO_ON` / `KILL_PULLS` · 运维 SSH touch 即时生效
- ✅ ModeMgr 自动切 tight/balance/cool · 30s 一采
- ✅ 缺货挂单 + Notify 唤醒 · 3 层幂等（conditional UPDATE / client_order_id / DB UNIQUE）
- ✅ TTL sweeper 60s · 防过期挂单钉死 mode
- ✅ 死循环哨兵 + 三源竞争 + wiring 契约测试
- ✅ **端到端模拟测试通过**（DryRunVendor · 不花钱验通 · `buyrace_e2e_test.go`）
- ❌ **生产真流量一次 fire 都还没观察过**（vps22 还是旧镜像）
- ⚠️ Region 命名口径不统一（A2 遗留）· 上线后要盯 fire 率

**剩余工作**：
- 上线后**首要观察指标**：webhook 收到 → Notify 触发 → fire 成功率
- 加速预存池（`prebuy-pool` group · 5min TTL · **暂缓**）—— 是"没人排队时也预抢"能力 ·
  但当前场景（一人自用 · 少量测试）用不上 · 有真流量数据后再评估
- 前端展示挂单状态 / mode / 开关 —— 归 B4

**上线顺序**：**A0 部署后自动就位** · 但**上线后 24h 密切观察** · fire 率不对再改

### A5 · Region 命名归一 · 修 A2 后遗症

**现状**：
- ❌ 三处对 region 用三套字面量（zone / vendor region / dryrun region）
- ❌ `stock_watcher.region` 列语义没定死 · SQL 严格匹配就查不到
- 详见 `docs/16-buy-race.md` 缺口 5

**剩余工作**：
1. `providers.ZoneOf(region string) Zone` · 6 家 adapter 各实现
2. `webhookin.onNewKeys` + `prober.deriveStockDelta` 用它归一
3. `stock_watcher.region` 列语义定死为 zone 名 · 加注释

**依赖**：A0-A2 上线后观察 fire 率 · 确认这条真影响再修（否则可能"探针 delta 传 zone 名了 · 一切正常"）

**上线顺序**：**A0-A4 上线后一周内**

### A6 · 抢号服务费控制（用户能感受到）· `docs/16-buy-race.md` 缺口 4

**现状**：用户想加倍率抢货 · 优先派发 · **决策：现在不做 · 等真实队列数据**

**剩余工作**：
- 归入 `decisions §2.6&2.7` 插槽候选（capability_fee 层）· 阶段 2c
- 现在不动

**上线顺序**：**暂缓** · 明确不做

---

## B · 用户体验层（基础扎实后再做）

### B1 · Status 页样式优化 · Region 拆分展示

**现状**（`web/src/pages/Status.tsx`）：
- ✅ 6 家共享 x 轴 heatmap（`§11.14`）
- ✅ vendor 卡 · 24h/7d/30d tab · Quality 标签
- ❌ 没暴露 mode 当前是什么（cool/balance/tight）· 只我方后端看
- ❌ 没暴露"当前有几条挂单在等" —— 数据在 stock_watcher 表

**剩余工作**：
- 加一个总览条：当前 mode / 挂单队列长度 / 6 家 fire 率
- vendor 卡加 fire 成功率标签
- 前端读 mode 需要新 endpoint（现在 mode 只在服务内存）

**依赖**：A0-A4 上线跑一周攒真数据 · 才知道要展示什么

**上线顺序**：**A 层全部稳定后**

### B2 · Prices 页数据源统一 · daily 端点接上

**现状**：
- ✅ 后端 `/api/vendors/{anon_id}/prices/daily?days=30` 端点写了（`§C`）· 返 min/max/avg/samples
- ✅ 数据源：`vendor_probe.sample_price_micro` · 60s 一条 · 每家每天 1440 条
- ❌ **前端 Prices 页零个引用 daily 端点** —— 后端写了没人用
- 前端还在用老的 `useVendorPrices`（vendors/prices 端点 · 返 30 天 rounds stub）

**剩余工作**：
1. 前端加 `useVendorPricesDaily` hook
2. Prices 页加"每日高低点蜡烛图"或"波动区间"（design 讨论 · `docs/15-prices-page-design.md` 已有草稿）
3. 老的 `useVendorPrices` 保留还是废弃：需要定
4. **样本数少的日子要不要显示**：<10 条时是画还是不画

**依赖**：
- A0 上线后 · 让探针跑几天攒真数据
- 时区 bug 修完后 sample_price_micro 才准

**上线顺序**：**A 层稳定后 1-2 天**（数据先攒）

### B3 · 拼车前后端接口对接优化

**现状**（需要审计 · 我还没盘完）：
- ✅ 后端 bus API 完整（15 个 handler · Create/List/Get/Update/Members/Match/Join/RegenInvite 等）
- ⚠️ 前端 Buses/BusDetail 页在用哪些 · 有没有断层：**没盘**
- ⚠️ anon 撮合前端能不能调 · 用户能不能建 anon 车：**没盘**
- ⚠️ 邀请码前端流：**没盘**

**剩余工作**：
1. **审计** · 后端 handler 逐个对前端 hook · 找出没接的 / 前端假造的
2. 修接口断层
3. 前端 UX 优化（这轮不细化 · 审计后再列）

**依赖**：无 · 但要人力

**上线顺序**：**A 层稳定后**

### B4 · 拼车全局配置优化 · 三层策略前端落地

**现状**：
- ✅ 后端 `passenger_strategy_default`（全局）+ `bus`（车级）两层字段（`§8.27`）
- ✅ decide() 合并逻辑（`stricter()` 取严）
- ✅ 前端 `Preferences.tsx` 页 · 车级 `BusDetail` 里策略 tab
- ⚠️ **待讨论清单 4 个缺口**（`docs/16-buy-race.md`）· 全是全局配置类：
  - 缺口 1 · 单价上限白天/夜间两档
  - 缺口 2 · 预留开关 + 用户抢到号强给
  - 缺口 3 · 车级 daily_round/spend_limit 死字段
  - 缺口 4 · 加倍率抢货优先派发（明确不做）

**剩余工作**：
- 4 个缺口按顺序做（3 → 1 → 2 · 4 不做）
- **注意**：这 4 条我之前作为独立线在做 · 用户澄清"这是**记录**未定稿·现在不做"·
  归到 B4 · 等 A 层扎实 + 有真数据后再回头拍板

**依赖**：A0-A6 稳定 + 有真流量数据

**上线顺序**：**A 层全绿至少一周后**

---

## 整体上线顺序（对齐用 · 一件一件推）

| 顺 | 工作 | 依赖 | 用户能感受到 | 状态 |
|---|---|---|---|---|
| 1 | **A0 · 部署新镜像 + 洗生产库脏数据** | 无 | 后端稳定 · 前端无变化 | ⏳ 立刻做 |
| 2 | **A1-A4 · 探针/webhook/抢号链上线** | A0 完成 | 后端在抢号 · 前端无感 | ⏳ 跟 A0 同次部署自动就位 |
| 3 | **A3 · 跑 xi8-audit 对账** | A0-A2 上线 | 后端 log · 前端无感 | ⏳ A0 后 |
| 4 | **观察 A4 抢号链真流量 fire 率** | A0-A2 上线 · 至少 24h | 后端 log · 前端无感 | ⏳ 上线后 24h |
| 5 | **A5 · Region 命名归一**（若观察发现漏 fire 严重） | 观察结果 | 后端 · 前端无感 | ⏳ 视观察结果 |
| 6 | **B2 · Prices 页接 daily 端点** | 数据攒 3-7 天 | 用户看到价格波动区间 | ⏳ A 层稳后 |
| 7 | **B1 · Status 页加 mode / 队列展示** | B2 后同期 | 用户看到"我方在抢" | ⏳ B2 平行 |
| 8 | **B3 · 拼车接口对接审计 + 修** | 无硬依赖 · 但要专注 | 用户拼车流畅 | ⏳ B1/B2 后 |
| 9 | **B4 · 全局配置 4 缺口拍板落地** | A 稳 + 队列真实数据 | 用户能配置更细策略 | ⏳ 最后 |

---

## 现在最要紧的一件事：A0

**卡在哪**：本地 push 的 5 个 commit 需要 GHCR build 完镜像 · vps22 才能拉。

**动作清单**（我按顺序自己做完 · 中间不问）：
1. 检查 GHCR build workflow 状态
2. 若 build 完 · SSH vps22 pull + up -d
3. 跑 migration up（027）
4. **洗生产库脏数据**：`UPDATE vendor_dispatch SET dispatched_at = datetime(dispatched_at,'-8 hours') WHERE vendor_id IN ('kirooo','kiroceo') AND dispatched_at > datetime('now')`
5. 验：`docker logs kirobus | grep "抢号链已装配"` · 有则装配成功
6. 观察 15 分钟：看有没有 Notify / fire 日志（有 kiroceo 上货最可能触发）
7. 回报你实测结果

**你回一个字 · 我立刻开工**：
- `A0` · 按上面 6 步做 · 中间不问
- 别的顺序 · 你说

---

## 记录约定（防我再跑偏）

- 每次做完一件 · **回到本文档更新对应节的"现状"**
- 若发现新缺口 · 写进对应节的"剩余" · 不新造独立线
- 只有本文档标为 ✅ 的才能算完 · 未上线的代码不算完
