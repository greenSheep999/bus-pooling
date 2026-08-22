# 25 · 数据粒度与断层修复实施方案

> 六组审计（g1–g6）结论收敛。**一句话定性**：这轮"缺数据"几乎全是**读取层没接**（查询查错表 / 查错字段 / 拿到了不渲染），**不是采集断、也不是存储缺**。真正缺存储的只有 1 处（补车事件无关联行），且**无需新表、无需新业务包**。
>
> 判据沿用 `CLAUDE.md §13`：**有页面要显示、或要算趋势 才存**。不要"未来可能有用"就存。
>
> 先读 `docs/06-db-schema.md`（表在哪）· `docs/13-frontend-design.md §10`（图表规范）· `docs/15-scheduling.md`（策略口径）。

---

## §1 症状 → 断层 对照表

断层四态定义：
- **not_collected** — housepool / 探针根本没给这个数
- **not_stored** — 拿到了但没落库（要加列或加行）
- **not_queried** — 库里有数，读取层的 SQL 没去查（查错表 / 查错字段）
- **not_rendered** — 后端已返到前端，前端拿到了不画

| # | 页面 · 位置 | 症状 | 断层 | 根因（一句话） | 数据在哪 |
|---|---|---|---|---|---|
| 1 | Overview「用量趋势」图 | 看着缺数据 | **not_queried** | trend.go 只查 wallet_ledger/pull_round/credential_ledger，从不查 `credential_usage_snapshot`（真用量在那·1224 行）· 且标题「用量趋势」配的是花费/拉号/寿命，**命名与内容错配** | `credential_usage_snapshot` 已存 |
| 2 | Overview「Vendor 监测」表 | 全 "-" / 全「缺货」 | **not_queried** | `vendorview.Stats()` 是 1a 占位桩，逐家 append 空行，从不查 `vendor_probe`(5.6万行)/`vendor_dispatch`(534) | `vendor_probe`/`vendor_dispatch`/`vendor_daily` 已存 |
| 3 | Overview「Vendor 占比」环形图 | 空环 · 中心计数 0 | **not_queried** | 同 #2，`Stats()` 返 `Pulls=0 Ratio=0`，前端 `filter(pulls>0)` 全空 | 同 #2 |
| 4 | 活动流 | 没有「推拼车」记录 | **not_queried** | `activities.go` 只读 4 源，从不读 `pending_assignment`（派车真实落库表·生产派车全走 assign） | `pending_assignment` 已存(completed 5+) |
| 5 | 活动流 | 已有记录缺 kind/字段 | **not_queried** | dead 事件只填 Kind/Summary/时间，Source/Target/Count/Amount 全空；into_bus 的 Target 是裸 UUID 不是车名 | `bus`/`credential_ledger` 已存 |
| 6 | 拼车纵览卡片 | 「消费」显 0 | **wrong_field** | `spend_today` 只算今天（生产轮次在 08-15/16，今天→0）· 且卡片**根本没有累计消费字段** | `pull_round`/`credential_ledger` 已存 |
| 7 | 拼车纵览卡片 + BusStats | 「平均寿命」显 0 | **wrong_field** | `avg_lifespan` 只 SUM 死号（全是活号→0）· 且 header 口径（死号）与 BusStats(活号算到 now) **自相矛盾** | `credential_ledger.pulled_at/dead_at` 已存 |
| 8 | BusStats 多人车摊分 | member_stats 全 0（真 bug） | **wrong_field** | `bus_member_stats.go` 用 `WHERE pr.bus_id=?` 过滤，本车两轮 bus_id 为空 → 查 0 轮；应走 `owner_bus_id → source_pull_round_id` 回溯 | `credential_ledger`/`pull_round` 已存 |
| 9 | 车内号「重推」按钮 | 除「推送中…」外无任何反馈 | **not_rendered** + **not_stored** | 前端拿到了 `{state,message}` 只塞进 tooltip、丢弃 message（渲染层）· 后端 failed/dead 分支漏 UPDATE 六字段（一小块存储） | 返回体已到前端；`credential_ledger` 六字段已建 |

**结论**：9 条里 **6 条 not_queried · 2 条 wrong_field · 1 条 not_rendered(+一小块 not_stored)**。**0 条 not_collected**，**0 条要新表/新包**。
---

## §2 修复清单（按"改哪层"分组）

工作量：小 <30min · 中 30min–2h · 大 >2h。

### A. 后端查询层（Go · 把 SQL 接到已有数据）

| # | 文件 · 位置 | 改什么 | 量 |
|---|---|---|---|
| 2·3 | `internal/vendorview/vendorview.go:662 Stats()` | 从占位桩改真查：复用 `StatusOverview` 已经能查出数据的那批 store（`probeStore` 聚合 + `orderKeyStore.DispatchSummary`），别各写一套。`share.Pulls` 从 `vendor_dispatch` 按 vendor_id 聚合；`out_of_stock` 从最近 `vendor_probe.alive`+stock 判定；unit_price/alive_rate/avg_lifespan 从 probe+quality_store。**对外脱敏**：VendorLabel/AnonID 走 `labelAndAnon`，不透 sample_price/median_price（§0.1） | 大 |
| 4·5 | `internal/insight/activities.go:26` | 加第 5 段 SQL 读 `pending_assignment(status='completed')`：`to-bus`→Kind/target_kind=into_bus·`JOIN bus b ON b.id=pa.target_bus_id` 取车名填 Target(不是裸 UUID)；`to-passengerpool`→push_pool。与现有 push 段按 credential_id 去重(pending_assignment 优先)。补字段：dead 填 Source=vendor/TargetKind=cred_dead/Count=1；topup/redeem 填 TargetKind=topup_source。into_bus 从 pull_round 直拼路径来的也 JOIN 车名替裸 UUID。参照 `events.go:179-198` JOIN 写法 | 中 |
| 4 | `internal/insight/activities.go` | 加第 6 段读 `pending_handoff(status='completed')` + 新 ActivityKind='handoff' | 小 |
| 6 | `internal/api/bus.go:586 buildBusResponse` / `:618 busCredStats` | busResponse 加 `spend_total`（累计·走 `credential_ledger.owner_bus_id → source_pull_round_id → pull_round` 回溯·DISTINCT round 防一轮 N 号乘 N 倍·**别用 pr.bus_id 直接过滤**）。`spend_today` 保留（今日无消费本就是 0，正确） | 中 |
| 7 | `internal/api/bus.go:625 busCredStats` | `avg_lifespan_seconds` 改 `*int64`：0 死号返 **null**（前端显"暂无·号都还活着"），别返 0 误导。死号口径**对**，保留 | 小 |
| 8 | `internal/api/bus_member_stats.go:96` | 真 bug：`WHERE pr.bus_id=?` 改回溯 `pr.bus_id=? OR pr.id IN (SELECT DISTINCT source_pull_round_id FROM credential_ledger WHERE owner_bus_id=? AND source_pull_round_id IS NOT NULL)`（抄 busCredStats 写法） | 小 |
| 1 | `internal/insight/trend.go` | （见 §4 决策）如确认要真"用量趋势"：加第四 metric `TrendUsage`，按天聚合 `credential_usage_snapshot`（按乘客名下 credential 过滤·取每日 current_usage_micro 末值/增量之和·micro→积分同 L103） | 中 |

### B. 后端存储层（Go · 补一次 UPDATE，表已存在）

| # | 文件 · 位置 | 改什么 | 量 |
|---|---|---|---|
| 9 | `internal/api/bus_credential_push.go:177-189` | failed 分支补 UPDATE `credential_ledger`：落 `push_error_code/status/message/retriable` + `push_attempts+1` + `push_last_attempt_at`（跟成功分支 158-169 对称）→ refetch 后行 Chip 变"推失败"、刷新仍可见。dead 分支同理落 push_error | 小 |

### C. 前端渲染层（TS · 数据已在手）

| # | 文件 · 位置 | 改什么 | 量 |
|---|---|---|---|
| 9 | `web/src/pages/BusDetail.tsx:595 PushRetryButton` | `lastResult` 从存 `r.state` 改存整个 `{state,message}`，按钮旁**内联**渲染一行（沿用 EditStrategyPanel 的 inline 2s 反馈约定·项目无 toast 库）。因 failed/dead 返 200 走 onSuccess，**必须在 onClick 里 branch r.state**：pushed/already_pushed→绿字显 message；failed/dead→红字显 message 全文；catch(真网络错)→红字通用文案。i18n `buses.json credentials.push` 加 `neterr`(zh-CN+en) | 中 |
| 7 | `web/src/components/BusStats.tsx:229-234` | LifespanHistogram 的 avgLife 与 header 口径对齐：**只算死号**，或明确改标"当前平均已存活"而非"平均寿命"——两处口径必须统一 | 小 |
| 6·7 | `web/src/components/BusCard.tsx:104/187` | 渲染 `spend_total`（累计）+ `avg_lifespan_seconds` 的 null 态（"暂无") | 小 |
| 5 | `web/src/types/index.ts:392-399` | ActivityKind 枚举补 `handoff`（TS 已有 target_kind='handoff'） | 小 |

### D. 命名/文案层（对齐，不改功能）

| # | 位置 | 改什么 | 量 |
|---|---|---|---|
| 1 | i18n `overview.json:102` | 若不做 usage metric：把「用量趋势」改成与内容相符的名（花费/拉号/寿命趋势），消除命名错配。若做 metric：METRIC_KEYS(Overview.tsx:85) 加一档 + label | 小 |

---

## §3 入库方案

### Q1 · housepool 那 10 个字段，哪些要存？

判据：**有页面显示或要算趋势 才存**。逐个过：

| housepool 字段 | 已映射到 Go? | 有页面? | 决定 | 理由 |
|---|---|---|---|---|
| `currentUsage` / `usageLimit` | ✅ 已存 snapshot | ✅ 号详情进度条 | **已在存** | 现状正确 |
| `subscriptionTitle` / `nextResetAt` | ✅ 已存 snapshot | ✅ | **已在存** | 现状正确 |
| `accruedCost` | ✅ 映射了(types.go:39)但没落 snapshot | ❌ 无页面 | **不存** | 无页面·无趋势需求。真要做"号成本趋势"再加列 |
| `billedRequests` | ✅ 映射了但没落 | ❌ 无页面 | **不存** | 同上 |
| `successCount` / `failureCount` | ✅ 映射了 | ❌ 无乘客页面（探针 alive 率走 vendor_probe） | **不存** | vendor 健康度已由 vendor_probe 覆盖·重复 |
| `lastUsedAt` | ✅ 映射了 | ❌ | **不存** | 便宜但无页面·按判据跳过 |
| `totalDispatch` | ❌ **未映射**（wire 里没有） | ❌ | **不接不存** | housepool 有·但 Go 侧没映射·无页面 |
| `recentDispatch10s/60s/5m` | ❌ 未映射 | ❌ | **不接不存** | 5min 快照采不到 10s 窗口·语义对不上（要接得走 §12.5b passenger_usage_log·⏸ 未来） |
| `recentTokens60s` | ❌ 未映射 | ❌ | **不接不存** | 同上·实时速率非快照粒度能表达 |

**Q1 答案：一个都不新增存。** 现有 snapshot 的 4 个字段已够号详情进度条 + markDead 拷贝 credits_used。`accruedCost/billedRequests` 已映射到 Go 结构体但**故意不落库**——等有明确页面再说。`recentDispatch*/recentTokens*` 是实时速率，5min 快照粒度根本表达不了，属于下游 pool 请求日志（`§12.5b passenger_usage_log`）的活，阶段 1 不做。

### Q2 · 怎么存？加列还是新表？

**都不用。** 唯一要动存储的是 #9（push 反馈），而 `credential_ledger` 六字段（`push_error_code/status/message/retriable/push_attempts/push_last_attempt_at`）**已经建好**（成功分支在写），只是 failed/dead 分支漏了 UPDATE。补 SQL 即可，**不加列、不加表、不加包**（`CLAUDE.md §4.1` 15 包已满，本方案不破）。

> 若将来真要做"号成本/请求量趋势"：**复用 `credential_usage_snapshot` 加列**（`accrued_cost_micro`/`billed_requests`），不新表。加表只在做 `§12.5b passenger_usage_log`（下游请求日志·并发/分摊用）时才发生，那是阶段 2b 的事。

### Q3 · 采样频率 + 保留多久？

**体积估算**（生产 480 行/天量级·单行 ~200B 含索引）：

| 窗口 | 行数 | 体积 |
|---|---|---|
| 30 天 | ~14,400 | ~2.7 MB |
| 1 年 | ~175,200 | ~33 MB |

**结论：量极小，1 年 33MB，暂不分级。** 5min 采样保留不动。

**但要加一个清理任务**（当前无·会无限增长）：
- 号详情进度条只看最近 24h·markDead 只读最近一条→**明细保 30 天足够**
- 加轻量清理：`DELETE FROM credential_usage_snapshot WHERE observed_at < now-30d`（deathwatch 或 scheduler 每日跑一次·`credits_used` 死时已拷进 `credential_ledger`·删旧快照不丢账）
- **不做日聚合表**：33MB/年不值得为聚合建第二张表（`CLAUDE.md §13`：够用不过度）。只有 #1 usage-trend 上线且 30 天窗口不够、或体积破百 MB 时再评估日聚合

---

## §4 图表与 UI 建议（Q4）

### 4.1 BusStats 两个空图（PullVolume / DailySpend）为什么空 · 怎么修

**不是没数据，是查错列。** `useBusPulls`(bus_credentials.go:167-190) 已用 IN 子查询兜空 bus_id，能拿到两轮 → 拉号量应显 6、每日消费 08-16 应显 500。**修 §2·A #6/#8 的回溯口径后这两图自动有数**，不需要新图表。

### 4.2 「用量趋势」命名错配 · 二选一

Overview 那张图标题「用量趋势」，画的却是 spend/pulls/lifespan。两条路，**需车主拍板**：

- **路 A（省事·推荐先做）**：改标题为「趋势」并让 metric 自解释（当前 credits/pulls/lifespan 三档本就正确·只是生产真没活动=数据稀疏·非 bug）
- **路 B（真做用量）**：加第四 metric `TrendUsage` 查 snapshot（§2·A #1）·METRIC_KEYS 加档

> ⚠️ 需向车主确认：他说的"缺数据"到底指 **snapshot 那份没画**（=路 B 新 metric）还是**现有三档稀疏**（=works_fine·靠时间积累）。别猜。

### 4.3 拼车还该显示什么（结合 §13 §10 规范）

沿用 `TrendChart.tsx` 视觉基调（品牌紫 Area·平均虚线·峰值点·图例必解释语义·label 带单位）。**本阶段只补"数据已在库、查得出"的**：

| 建议 | 数据源 | 做不做 | 理由 |
|---|---|---|---|
| 卡片加**累计消费**（今日旁） | pull_round 回溯 | ✅ 本阶段 | 健康车今日常为 0·累计才有意义（#6） |
| 平均寿命 null 态"暂无·号都还活着" | credential_ledger | ✅ 本阶段 | 消除 0 误导（#7） |
| 车级**号存活时长直方图** | 现有 LifespanHistogram·口径对齐即可 | ✅ 本阶段 | 组件已在·只修口径 |
| PushSuccessRate 图 | 需 downstream.connected | ⏸ 有条件 | 未接下游则 warn 空·非 bug |
| 车级"号用量趋势"（进度条时序） | credential_usage_snapshot | ❌ 延后 | 依赖 #1 路 B·先在 Overview 验证再下沉到车级 |
| 成员摊分饼图 | member_stats（修 #8 后） | ✅ 顺带 | bug 修完多人车自然显示·单人车 return null 不变 |

**UI 优化**：图表按 §10.6 scope 下拉切维度·空态用 `EmptyChart` 给 hint 不要留白·label 带单位跟 metric 联动（积分/次/h）。**不新增图表组件**——PullVolume/DailySpend 修数据即活，够用。

---

## §5 不做清单（这阶段明确不做 + 理由）

| 不做 | 理由 |
|---|---|
| `passenger_usage_log`（下游 pool 请求日志·RPM/TPM/并发） | `docs/06 §12.5b` 标 ⏸ 未来·要接 passengerpool·并发/分摊用·**阶段 2b 压车治理**才需要（`CLAUDE.md §3`） |
| 存 `recentDispatch10s/60s/5m` / `recentTokens60s` | 实时速率·5min 快照粒度表达不了·且属下游日志范畴（同上） |
| 存 `accruedCost` / `billedRequests` | 已映射 Go 但无页面·`§13` 判据不过 |
| `credential_usage_snapshot` 日聚合表 | 33MB/年·不值得建第二表·`CLAUDE.md §13` 够用原则 |
| refill 补车事件行 | 当前 deathwatch 触发补车只落新 pull_round·无"因谁死而补"关联行。**要做需在补车处落轻量事件行**·但本阶段无明确需求→延后（审计 g2 建议：先接 pending_assignment+handoff 零新增存储·refill 留到有需求） |
| bus 成员 join/leave 活动事件 | `bus_member.joined_at/left_at` 时间戳已在·可直接查·但当前活动流无此需求→延后（不需新存储） |
| 新表 / 新业务包 | `CLAUDE.md §4.1` 15 包已满·本方案全部复用现有表与包（vendorview/insight/api） |
| 列队策略 / 压车治理相关采集 | 阶段 2a/2b（`CLAUDE.md §3`）·别混进来 |
| 数据图表大盘 / 市场 | 阶段 3a/3d·别混进来 |

---

## §6 实施顺序

**分 3 批·可分别独立部署。** 依赖关系已标。

### 批 1 · 纯后端查询修复（无前端依赖·一次部署）
> 全是"把 SQL 接到已有数据"·互不依赖·可并行改、一起测、一次上。

1. **#8** `bus_member_stats.go:96` 回溯口径（真 bug·最小·先修）
2. **#6** `bus.go` 加 `spend_total` 回溯 + 保留 spend_today
3. **#7** `bus.go` avg_lifespan 改 `*int64` 返 null
4. **#2·3** `vendorview.go:662 Stats()` 接真数据（复用 StatusOverview 的 store·工作量最大·独立）
5. **#4·5** `activities.go` 加 pending_assignment + handoff 段 + 补字段

→ 部署后：Overview 的 Vendor 监测/占比、活动流「推拼车」立即有数。

### 批 2 · push 反馈全链（后端存储 + 前端渲染·有依赖·一起上）
> #9 后端 UPDATE 与前端 branch 必须配套·分开上会出现"前端等字段但后端不写"。

6. **#9-后端** `bus_credential_push.go` failed/dead 分支补 UPDATE 六字段
7. **#9-前端** `BusDetail.tsx` PushRetryButton inline 反馈 + branch r.state
8. **#9-i18n** `buses.json` 加 neterr（zh-CN+en）
9. **#5-类型** `types.go` ActivityKind 补 handoff（配合批 1 的 #5）

→ 部署后：重推按钮有绿/红 inline 反馈·刷新仍可见。

### 批 3 · 前端口径对齐 + 命名（依赖批 1 的后端字段·最后上）
> 依赖批 1 已返 spend_total / avg_lifespan(null)。

10. **#6·7-前端** `BusCard.tsx` 渲染 spend_total + null 态·`BusStats.tsx:229` LifespanHistogram 口径对齐
11. **#1-命名** `overview.json` 「用量趋势」→ 消除错配（**先走路 A**）
12. **清理任务**（§3 Q3）：加 30 天快照清理·可并入批 1 或批 3·无依赖

### 待车主拍板（不阻塞上面 12 步）
- **#1 路 A vs 路 B**：「用量趋势」是改名(A)还是真做 snapshot metric(B)。先确认他说的"缺数据"指哪份→再决定要不要做 §2·A #1（trend.go 第四 metric）。

**关键依赖**：批 1 独立可先上 → 批 3 依赖批 1 的后端字段 → 批 2 内部前后端必须配套。批 1、批 2 之间无依赖，可并行。

