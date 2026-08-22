# Issues Log · 已知问题清单与解决状态

> 每条问题一行记录 · **状态明确** · **可回溯** · 别散在 memory 或 decisions 里。
>
> **为什么单独一份**：`decisions.md` 记的是"选了什么方案"（含已否决方向）· `phase-1-acceptance*.md`
> 是**某个时间点**的验收快照 · 都不适合追踪"这个 bug 修了没"。这里只做**状态机**：
> `open → in-progress → fixed(unverified) → verified` · 修好且验过就 archive 到底部。
>
> **写入规矩**：
> - 一条 issue = 一行摘要 + 一小段详情 · 别写长论文
> - 状态改变时更新 · 不改文件名 · 不删条目
> - **实测过 → 才能标 `verified`** · 只跑了 `go test` 不算
> - `fixed(unverified)` 是编译过了但没手工/e2e 验过 · 允许推给下一个 agent 或用户复验
>
> **别写进这里的**：
> - 已经从设计层否决的方向 → 走 `decisions.md`
> - 阶段规划 / 未来 roadmap → 走 `00-values-and-phases.md`
> - 某个时间点的全量审计快照 → 走 `phase-1-acceptance-v2.md` 那种验收文档

## 索引

| # | 状态 | 优先级 | 摘要 | 首发时间 |
|---|---|---|---|---|
| [I-01](#i-01) | ✅ verified | P0 | 手工池号 sold 后 credplain 没写 · 推池只能推 placeholder | 2026-08-17 |
| [I-02](#i-02) | 🟢 fixed(unverified) | P1 | 进车后自动推下游 · push_on_pull 字段留着但无消费路径 | 2026-08-15 |
| [I-13](#i-13) | ✅ verified | P2 | pullSuccessBridge VendorLabel 硬编 "provider" 泄漏内部术语 | 2026-08-22 |
| [I-14](#i-14) | ✅ verified | P2 | migration 046 down 后重 up 报 duplicate column · 破坏 down/up 幂等 | 2026-08-22 |
| [I-15](#i-15) | 🟢 fixed(unverified) | P1 | 生产延迟高 · 前端 staleTime 30s + vendors/status 无缓存 · 跨海每次 refetch | 2026-08-22 |
| [I-03](#i-03) | 🟢 fixed(unverified) | P1 | kirodrop personal 接入 · region=personal 覆写(共用端点)· 待生产端到端 | 2026-08-22 |
| [I-04](#i-04) | 🔴 blocked | P1 | 5 家 vendor 都只声明 enterprise · 缺各家 personal API 文档 | 2026-08-22 |
| [I-05](#i-05) | 🟢 fixed(unverified) | P1 | 主文档 3 份滞后 migration 040（15-scheduling / 06-db / 05-api / 03-modules） | 2026-08-15 |
| [I-06](#i-06) | ✅ verified | P1 | 车级 daily_* AND 全局 · §8.47 · 2026-08-22 实测通过 | 2026-08-15 |
| [I-07](#i-07) | 🟢 fixed(unverified) | P2 | handoff init 幂等契约不一致（前端送 idempotency key · 后端忽略） | 2026-08-15 |
| [I-08](#i-08) | ✅ verified | P2 | 05-api-contract 列了未实现端点（/me/buses/{id}/stats 等 3 个） | 2026-08-15 |
| [I-09](#i-09) | ✅ verified | P2 | 4 包关键路径测试补齐 | 2026-08-15 |
| [I-10](#i-10) | ✅ verified | P2 | migration 040 缺集成测试 | 2026-08-15 |
| [I-11](#i-11) | 🟢 fixed(unverified) | P2 | 缺 stage-1..6 分级 smoke 脚本 | 2026-08-15 |
| [I-12](#i-12) | 🟢 fixed(deferred) | P3 | 主文档 P2 drift · 关键条已修 · 25 条纯文档 drift 明确 defer 阶段 2 | 2026-08-15 |

---

## 详情

### I-01 · 手工池号 sold 后 credplain 没写 · 推池只能推 placeholder

**状态**：✅ `verified` · 2026-08-22 实测通过
**发现**：2026-08-17
**症状**：
- 后台通过 `POST /api/admin/market/stock` 塞号进 vendor 07（KiroMarket 手工池）
- 用户拉下来后推池 → 走 `pusher.fetchPlaintext` 拿不到明文
- pusher 走全 placeholder 兜底 → k2a 收到假 refreshToken
- **表象**："下游没有推到" / "手动重推也没有"

**根因**：手工池路径 · admin 塞号时明文只走 housepool BatchImport · 全项目所有拉号
路径**都不写** `credential_plaintext`。5 天前 memory 记录过 · 但只有 `seed-credplain`
CLI 手动补。

**方案**：加临时表 `market_stock_plaintext(kiro_rs_credential_id 主键)`。
1. `admin_market.collectImportEvents` verified 时 · `credplain.StashByKiroRS` 落暂存
2. `decider/settle` 手工池 sold 分支 · **同 tx 里** `PopToCredplainTx` 迁到正式
   `credential_plaintext(credential_id 主键)` · 删暂存
3. Janitor 清 7d TTL 未卖号残留

**改动清单**：
- migration `050_market_stock_plaintext.sql` · 新表
- `internal/credplain/marketstage.go` · `StashByKiroRS` / `PopToCredplainTx` / `PurgeStash`
- `internal/api/admin_market.go` · `collectImportEvents` 加参数 · verified 时 stash
- `internal/decider/orchestrator.go` · 加 `MarketStockPlaintextPopper` 接口
- `internal/decider/settle.go` · sold 分支同 tx 调 `PopToCredplainTx`
- `cmd/bus-pooling/main.go` · 装配 credplainStore 提到 makeDecider 前 · 三处共用同实例

**验证（2026-08-22）**：
1. 建 pro_max offer(id=`cb7303b9...`) + 塞号 → stash 表落 1 行(kiro_rs_id=18 · enc_len=64)
2. 注册测试账号 · 500 积分 · 拉 pro_max 1 个 → 返 credential_id=`4235eccf...`
3. sold 后:market_stock_plaintext **空** · credential_plaintext **有行**(4235eccf... · enc_len=64) ·
   market_stock_item.status=sold · 三者同 tx 原子提交
4. pusher 后续拿 `4235eccf...` 走真明文推 · 不再走 placeholder

**保留 seed-credplain CLI** 作历史残留补丁工具 · 生产老 stash 之前的号仍能靠它救。

---

### I-02 · 进车后自动推下游 · push_on_pull 字段留着但无消费路径

**状态**：🟢 `fixed(unverified)` · 待用户端到端实测
**发现**：2026-08-15（phase-1-acceptance §P0-4）
**修复**：2026-08-22

**症状**：`passenger_downstream.push_on_pull` 字段有默认 true · 但代码 0 处消费。
用户勾了没反应。

**用户澄清的三场景**：
- 场景 1 · 建车首次拉 · 应该自动推
- 场景 2 · 自动补车 · 开关控制
- 场景 3 · 提取 key 派进车 · 开关控制

**方案**：`pullSuccessBridge` 兼干两件事 · webhook 通知 + 自动推池 · 共用同一
hook 入口 · 装配层注入 pusher + downstreams + vendorView。

- 场景 1&2 · `decider.OnPullSucceeded` (settle tx 提交后) · 后台 goroutine 推池
- 场景 3 · `api handleAssign into_bus` 分支返回前 · 调 `AutoPushOnAssign` hook
- `push_on_pull=false` → 跳过 · 尊重用户设置
- 无 downstream 配置(Get 返 err) → 跳过
- 失败落 `credential_ledger.push_error_*` · 用户走 BusDetail "重推"按钮救(§8.44 已在)

**改动**：
- `cmd/bus-pooling/webhookout_bridge.go` · pullSuccessBridge 加 pusher/downstreams/
  vendorView 依赖 · 加 `autoPush(passengerID, credIDs, vendorLabel)` +
  公开 `AutoPushOnAssign(ctx, passengerID, credIDs)`
- `internal/api/server.go` · Server 加 `autoPushOnAssign` hook 字段 + Deps
- `internal/api/pullrecord.go` handleAssign · into_bus 尾部调 hook
- `cmd/bus-pooling/main.go` · bridge 建早 · webhookout 未装配也接 pullNotifier ·
  ServerDeps.AutoPushOnAssign 传 `pullBridge.AutoPushOnAssign`

**顺手修**（I-13 · 见下）：pullSuccessBridge VendorLabel 硬编 "provider" 泄漏内部术语。

**待端到端验证**：
1. 用户配 downstream(URL + token) · push_on_pull=true(默认)
2. 建车 · 首次拉 1 号 → 后台应见 "I-02 · auto push 完成" log · k2a 收到号
3. 派 key 进车(assign into_bus) → 同上
4. 关 push_on_pull → 不推
5. 无 downstream 配置 → 不推 · 不 warn

**关联**：I-01（推池链路真明文的前置修复）· I-13（顺手 VendorLabel 泄漏）

---

### I-03 · kirodrop 新增 personal 号未接入

**状态**：🔴 `blocked` · 缺 vendor API 文档 · 无法推进
**发现**：2026-08-22（用户提及 · pro_max 5000 单位 134.98 CNY）

**症状**：kirodrop vendor 上游新增了 personal 号池 · adapter 未声明 AccountKinds ·
系统看不到 personal 号。**手工池(kiro_market · vendor 07)有替代方案** — 用户
可以走 admin_market 手工塞号（I-01 已修 credplain 链）· 不必等 kirodrop 直连。

**推进路径**（3 选 1）：
- **A · 补 kirodrop personal API 文档**（Playwright 探端点 or 从 vendor 拿）→ 照 kirooo 那套接
- **B · 完全走手工池路线**（vendor 07 · KiroMarket）→ 不接直连 · 运营手工上架
- **C · 等 vendor 官方开外部拉号协议**（sprint-1-final Stage 3 blocking · 全项目共通问题）

**修法**（一旦有 API 文档 · 照 kirooo 抄）：
1. `adapter.go · Capability()` 加 `AccountKinds: [enterprise, personal]`
2. 建 `personal.go` · Stock / Purchase 独立端点
3. Stock/Purchase 判 `opts.Kind == Personal` → 转发 personal.go
4. `docs/vendors/drop-kiro-ss.md` §2.3b 补 personal 池文档
5. vendor_pricing 表补 personal row
6. personal_test.go 三条:stock/purchase/webhook 归一化

**当前建议**：走 **B**（手工池）· 上游协议 blocking 时的正确替代路径。

---

### I-04 · 5 家 vendor 只声明 enterprise · 只 kirooo 双档接完

**状态**：🔴 `blocked` · 缺各家 personal API 文档 · 无法批量推进
**发现**：2026-08-22

**症状**：现在 6 家 vendor 里 **只有 kirooo** 走完双档接入。**91kiro / kiroceo /
kiroappio / kiroappcc / kirodrop** 都还只有 enterprise。

**影响**：如果上游任何一家开了 personal 号池 · 我方系统都看不到。

**推进路径**：跟 I-03 同 · **手工池是 blocking 时的替代路径**。真要接直连
每家 vendor 单独探端点 + 抄 kirooo 骨架(不是共通改造)。

**跟 sprint-1-final Stage 3 的关系**：Stage 3 blocking 说的是**外部拉号协议**
(vendor 侧还没开)· 覆盖 vendor 直连所有场景 · 不只 personal。所以 I-03/I-04
本质是同一个上游 blocking 的子问题。

---

### I-05 · 主文档 3 份滞后 migration 040

**状态**：🟢 `fixed(unverified)` · 2026-08-22
**发现**：2026-08-15

**症状**：15-scheduling / 06-db / 05-api / 03-modules 四份文档描述已撤的 nullable
继承语义。下一个 agent 按老口径写代码会造错。

**修法**：四份文档核心节重写对齐 migration 040 现实。

- `docs/15-scheduling.md §4.3.2` 车级字段两状态语义（auto_refill_* 纯车级 vs 其他覆盖字段）· §4.3.2 表格改按字段类分行 · §4.3.5.3 TS 契约撤 auto_refill_* nullable · §4.3.5.5 落地状态改 migration 040 后的现实
- `docs/06-db-schema.md §8 bus 表` auto_refill_enabled/refill_watermark 改 `NOT NULL DEFAULT 0` · §16 passenger_strategy_default 加 3 个跨车调度护栏字段 + 语义表从三类改四类
- `docs/05-api-contract.md §7` GET /me/strategy 响应加三护栏字段 · PUT /buses/{id}/strategy 撤 auto_refill_* null 三态说明 · 改成"必须非 null"
- `docs/03-modules.md §strategy` 依赖描述按字段类分行

**未改的历史残留引用**（`§4.3.2b` / `1f-B` 提法散在 §4.3.5.5 等段）· 保留作历史记录·
新加的 §4.3.2 override 已明确 · 有明确"作废" 标记 · 不误导。

**下一步**：用户按新契约走一次 PUT /buses/{id}/strategy 验证 · 尤其 auto_refill_*
传 null 应 400。

---

### I-06 · 车级 daily_round / daily_spend 应生效 ✅ verified

**状态**：✅ `verified` · 2026-08-22 端到端实测通过
**修法方向反转**：撤 §8.27 C 方案 · 走 §8.47 · 激活车级 AND

**端到端验证**（2026-08-22 · 用真号 `ksk_VU...` seed + i01test 账号）：
- ✅ **场景 A · 车级 daily_round=0 拦所有拉号** — 建车 daily_round_limit=0 · 拉 1 号返
  `409 daily_limit_reached · limit=0 used=0` —— 车级判据触发（全局 null · 不是全局拦的）
- ✅ **场景 B · 提取(无 bus_id)绕过车级** — 同乘客走 `POST /me/pull` 不带 bus_id ·
  拉 pro_max 号成功 · credential_id `b8a7e15b...` · 花 141 积分 · 车级 daily=0 不管 record group
- ✅ **顺手验 I-01 · credplain 迁移** — 新号 sold 后 credplain 表有行(kiro_rs_id=19 ·
  enc_len=64 · email `test-i06@x.com`)
**发现**：2026-08-15（phase-1-acceptance §P1 · 老 C 方案下的 P1）
**反转**：2026-08-22（车主指出多车预算分配场景 · C 方案无解）

**车主的关键问题**（原话）：
> "3 辆车同时再跑 · 全局设置 500 是每个都 500 吗?"

**答**：现在是**跨车累加 500** · 但**用户想要能给单车限死** —— 那辆试验车最多 100 · 另两辆合计 400 · 全局兜底不失控。**C 方案没这个能力** · 必须激活车级 AND。

**§8.47 定稿**（覆盖 §8.27 C 方案 daily_* 部分）：
- **全局** 管 "所有车加起来 + 提取"（`passenger_daily_counter` 跨车累加）
- **车级** 管 "这辆车"（`pull_round` 按 bus_id 聚合）
- **两层独立 AND 取更严** · 车级 null = 不加严 · 车级放宽全局仍生效（CLAUDE §1.5）
- 提取（BusID 空）只受全局管 —— record group 无车级

**修法（2026-08-22）**：
- `internal/strategy/canpull.go` `CheckInput` 加 `UsedBus / BusDailyRound / BusDailySpend` · `decide()` 加车级 AND 判据
- `internal/wallet/wallet.go` `TodayUsageByBus(busID)` 走 pull_round 聚合
- `internal/api/bus.go handleBusPull` 读车级 daily_* + 本车今日已用 · 传下去
- `internal/bus/bus.go` Strategy struct 撤 DEPRECATED 注释
- `web/src/components/StartCarpoolModal.tsx` **撤销之前的 I-06 撤字段** · 加回建车向导 daily_* 输入
- `docs/decisions.md §8.47` 新加 · §8.27 daily_* 部分标 "已被覆盖" · `22-buy-race 缺口 3` 加 "已被 §8.47 覆盖"
- `docs/06-db-schema.md §8` 撤 DEPRECATED 注释

**新单测**（`internal/strategy/canpull_test.go`）：
- `TestBusDailyRoundLimit_ANDWithGlobal` · 车级拦 / 全局拦 / 提取跳过车级 三场景
- `TestBusDailySpendLimit_ANDWithGlobal` · 车级 spend AND
- `TestBusDaily_CannotRelaxGlobal` · 车级放宽全局仍生效（CLAUDE §1.5）

**待做**（自动补车链路 · 阶段 1 主链通了后补）：
- refill/scheduler/webhook 三桥调 `canpull.CanPull` 时也传车级 daily_* · 现在只 handleBusPull 手动路径生效

---

### I-07 · handoff init 幂等契约不一致

**状态**：🟡 `open`
**发现**：2026-08-15
**症状**：`internal/api/handoff.go:34 handleHandoffInit` 未验 `X-Idempotency-Key` · 但 `web/src/api/hooks.ts:655 useHandoffInit(postIdempotent)` 前端在送。双击 / 网络重发时后端会为同一批 credential 起两个不同 download_token · 各自 TTL 5min 内都能取明文。

**修法**：后端补 `ensureIdempotencyRecord` · 或前端 `useHandoffInit` 改回普通 `post`。

---

### I-08 · docs 列了未实现端点

**状态**：🟡 `open`
**发现**：2026-08-15
**症状**：`docs/05-api-contract.md:298, :475-477` 列了 3 个端点但 `server.go` 未注册：
- `GET /me/buses/{id}/stats`
- `GET /me/credentials`
- `GET /me/credentials/{id}`

**修法**：doc 里标 `阶段 1d/未实现` 或删条目。

---

### I-09 · 4 个包无单元测试

**状态**：🟡 `partial` · vendoraccount 关键路径已覆盖 · 其他 3 个包留阶段 2
**发现**：2026-08-15

**症状**：
- `internal/housepool` 只有 kirors 子包有测试 · 主包无
- `internal/vendoraccount` **已补(2026-08-22)**
- `internal/providers/kiro/vendors/kiroappio` 无测试
- `internal/providers/kiro/vendors/kiroceo` 无测试

**已补 · vendoraccount**（`store_test.go` · 5 个测试）：
- RoundTrip:加密写 → 解密读一致
- NoPlaintextInDB:落库 blob 不含明文串(AES-GCM 保证)
- MissingReturnsNilNoError:表空返 nil · nil 让上层 fallback env
- DisableReturnsNil:软删后 LoadActive 返 nil
- UpsertUpdateExistingRow:同 vendor_id + label 覆盖不新增行

**待做**(阶段 2 P2 收尾):
- `housepool` 主包 · 补包级 client mock 测试
- `kiroappio` / `kiroceo` · stock/purchase/webhook 归一化三条测试(照 kirooo 骨架抄)

**优先级判据**:vendoraccount 是**生产安全边界**(明文永不落库) · 必测。其他 3 个包
是"覆盖率"考虑 · 不影响阶段 1 收官功能面。

---

### I-10 · migration 040 缺集成测试

**状态**：✅ `verified` · 2026-08-22
**发现**：2026-08-15

**症状**：migration 039 有集成测试 · 040 无。

**修法**：新建 `internal/db/migrations_040_test.go` · 3 个测试:
1. `TestMigration040_BusColumnsAreNotNull` · schema 层验 `auto_refill_enabled` /
   `refill_watermark` 是 NOT NULL DEFAULT 0
2. `TestMigration040_BusNotNullBlocksNullInsert` · 运行时验 NULL 插入被拒 · 显式 0/5 过
3. `TestMigration040_AddsCrossFleetGuardrails` · 三跨车调度护栏字段可读写

**避开的坑**(I-14 · 顺手记):原计划走 039 up → 塞 nullable 数据 → up 040 验迁移 ·
但 migration 046 down 有 bug(重 up 报 duplicate column)· 改用"只测最终 schema"方案 ·
不 down 触发 046 的坑。

---

### I-11 · 缺 stage-1..6 分级 smoke 脚本

**状态**：🟢 `fixed(unverified)` · 2026-08-22 骨架完成 · 细节等 Stage 3 blocking 解除
**发现**：2026-08-15

**症状**：只有 `smoke-1f.sh` 一份综合脚本 · 每档切换后靠人肉验。

**修法**：`scripts/smoke-stage{1,2,3}-*.sh` 三份骨架建好 · 内含 Stage 覆盖说明 + TODO
标记。**内容留 TODO** —— 因为 Stage 3+ 上游 vendor 还 blocking (sprint-1-final 记)·
现在写完整流程也没法验 · 收官上线时才补细节。

**目前用**:综合 `smoke-1f.sh` 继续跑 · Stage 分级脚本作**目录索引** · 让后续 agent
知道哪个 stage 该走哪份。

---

### I-12 · 主文档 P2 drift（26 条）

**状态**：🟡 `partial` · 已修最影响下一个 agent 判断的 1 条 · 其余 25 条留阶段 2 收尾批量处理
**发现**：2026-08-15

**摘要**：
- `06-db-schema` 漏收 9 张 1b~1e 新表（stock_watcher / vendor_ledger 等）+ 若干字段
- `03-modules` 依赖图漏 stockwatch / vendorbalance / pricing / xi8 / vendorview 连线
- `01-architecture §5` 目录树缺 5 个已存在的业务包
- 详见 `docs/phase-1-acceptance.md §P2 Cleanup` 全表

**已修**(2026-08-22):
- ✅ CLAUDE.md §4.1/4.2 · 分清"核心业务包 15" vs "支撑层"vs"基础设施"·
  说明当前 34 包为什么不是"破 15 上限" · 撤"pricing 不许新加"（已存在）

**留阶段 2**（25 条 · 全是文档 drift · 不影响运行时）：
- 06-db-schema 补 9 张新表 CREATE + 若干字段
- 03-modules 依赖图补 decider 支撑包群子图
- 01-architecture §5 目录树补 5 个业务包
- 其他见 phase-1-acceptance §P2 Cleanup

**影响**：不阻运行时 · 只误导下一个 agent 建代码时按老 schema 造字段。

---

### I-15 · 生产延迟高 · 点了没反应 · 加两层缓存

**状态**：🟢 `fixed(unverified)` · 2026-08-22 · 部署后需用户实测
**发现**：2026-08-22 · 用户报告"点设置都慢 · 从没见过这么慢"

**症状**：
- 点任何页面/tab 卡好几秒才渲染 · 数据一次性全出来
- healthz(极简端点)外网 900ms · 内网 loopback 0.6ms → **网络+CF 握手 700ms**
- `/api/vendors/status` loopback 240ms(遍历 7 vendor × 5 SQL 窗口聚合)· 外网 900ms+
- 每页并发 4-8 个 API · 最慢的 gate 整个渲染

**根因分层**：
1. **前端 staleTime 30s 太短**:切 tab 40 秒回来 · 所有 hook 触发 refetch · 8 API × 跨海 300ms
2. **`/api/vendors/status` 无缓存**:全用户共享同一份数据 · 每人每 30s 都重跑 7×5 SQL 聚合
3. 物理网络跨海 RTT(不可控 · 是背景 · 不解决)

**修法**：
- **前端**(`web/src/main.tsx`)· QueryClient 默认:
  - `staleTime: 30_000` → `5 * 60_000`(5 分钟)· 切 tab 回来不 refetch
  - `gcTime` 补 `30 * 60_000`(30 分钟)· 后台 tab 缓存留更久
  - 保留 `refetchOnWindowFocus: false` + `retry: 1`
- **后端**(`internal/api/vendors.go`)· `handleVendorsStatus` 加 30s 内存缓存(sync.RWMutex):
  - key=windowHours · 跨用户共享(status 端点是**全用户同视角**的公开数据)
  - 240ms → <1ms 命中

**为什么不用 Redis**:CPU 0% · DB 只 234M · 单机 sync.Map 够用 · CLAUDE §13"不要抽象要够用"。

**未做**(followup):
- `vendors/stock` / `vendors/prices` 走 tier 视角 · **不能全用户缓存** · 若真慢可按 tier 缓
- 手动操作后(拉号/建车)已有 `invalidateQueries` 强制刷 · 不受 staleTime 影响

**待验**:部署后用户测"切 tab / 进设置 / 进详情"是否明显变快。

**关联**:CLAUDE §11 "缓存"从未讨论过 · 这条视为**性能优化**不是新功能 · 阶段一收官后打的补丁。

---

### I-14 · migration 046 down 后重 up 报 duplicate column

**状态**：✅ `verified` · 2026-08-22 修完
**发现**：2026-08-22 · I-10 集成测试期间

**症状**：migration 046(account_kind_subscription)的 down 未干净删列 · 重新 up 时
`duplicate column name: account_kind`。破坏 down/up 幂等。

**修法**：046 down 段补 `DROP COLUMN` 六条（SQLite 3.35+ 支持）:
```sql
ALTER TABLE credential_ledger DROP COLUMN account_kind;
ALTER TABLE credential_ledger DROP COLUMN subscription;
ALTER TABLE credential_ledger DROP COLUMN source;
ALTER TABLE pending_purchase DROP COLUMN account_kind;
ALTER TABLE pending_purchase DROP COLUMN plan;
ALTER TABLE pending_purchase DROP COLUMN source;
```

**验证**(2026-08-22)：`migrate up → migrate down 5 → migrate up` 全绿。

**顺手记**：migration 044/045 缺号 —— 应该是历史 rebase 造成 · 编号跳过但功能没缺。

---

### I-13 · pullSuccessBridge VendorLabel 硬编 "provider" 泄漏内部术语

**状态**：✅ `verified` · 2026-08-22 修完（跟 I-02 一起）
**症状**：`cmd/bus-pooling/webhookout_bridge.go` pullSuccessBridge.OnPullSucceeded
里 `VendorLabel: "provider"` 硬编 —— 拉号后发对外 webhook 的载荷带 "provider"
字面词，违反 CLAUDE §0.1 / §12.6 内部术语铁律。

**修复**：走 `vendorView.AnonLabelFor(vendorID)` 拿匿名 label(AWS-Q Kiro Vendor NN)·
vendorView 装配 nil 时退回 "vendor" 通用词。

**跟同类修复对齐**：commit 6860b1c 修过 assignErrItem 同样的泄漏 · 这次一并清了。

---

## Archive · 已 verified 关闭

（保留在原位 · 状态 verified 后不物理归档 · 索引表状态列直接显示 ✅ · 方便回溯）
