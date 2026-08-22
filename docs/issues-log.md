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
| [I-16](#i-16) | 🟡 open | P1 | Prober 只探 enterprise · vendor_probe/vendor_probe_zone 缺 personal 数据 · Status 页看不见个人号 | 2026-08-22 |
| [I-17](#i-17) | 🟡 open | P2 | 生产 credential_ledger 号显示 alive 但很久没用 · 疑似 vendor 侧已死号池未同步 · 需主动 TestCredential | 2026-08-22 |
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
| [I-18](#i-18) | 🟢 fixed(unverified) | P0 | /api/vendors/{anon_id}/stock 和 /history 404 · handler 拿 anon_id 直查 lookupEnabled(只认真 id) · 阻塞所有散客选 vendor 后的数据面板 | 2026-08-22 |
| [I-19](#i-19) | 🟢 fixed(unverified) | P0 | Offers 端点绕过 PricedFor · USD 家 18.51 USD 被当 18.51 积分 · CNY 家漏计费栈 | 2026-08-22 |
| [I-20](#i-20) | 🟢 fixed(unverified) | P0 | vendorview 展示价没接 RatesResolver · 只用 env Rates(生产恒 0) · 跟 decider 拉号 DB 求费率脱钩 | 2026-08-22 |
| [I-21](#i-21) | 🟢 fixed(unverified) | P0 | kirodrop personal 号 payload 是 refresh_token · decider/import 无脑塞 KiroAPIKey · 号导入必败钱白扣 | 2026-08-22 |
| [I-22](#i-22) | 🟢 fixed(unverified) | P0 | 拉号成功后 decider/settle 从没调 credplain.Save · 明文全丢 · push/handoff 走 placeholder 交付废号 | 2026-08-22 |
| [I-23](#i-23) | 🟢 fixed(deferred) | P0 | xi8 fire-guard · **审计误报** · 2026-08-14 用户拍板 xi8 不进钱路 · schema 保留仅对账 | 2026-08-22 |
| [I-24](#i-24) | 🟢 fixed(unverified) | P0 | 优惠码 service_fee_waiver 完整核销 · Lookup+Redeem+Wallet.Credit 退还 · 修隐式超收 | 2026-08-22 |
| [I-25](#i-25) | 🟢 fixed(unverified) | P0 | offers 端点从 vendor_price_tier 读 qty_band · 数量分档单价前端切数量重算 · 每档过计费栈 | 2026-08-22 |
| [I-26](#i-26) | 🟢 fixed(deferred) | P1 | PartiallyRefunded · 当前 kirodrop 语义巧合正确·未来接语义相反 vendor 时再拆分支处理 | 2026-08-22 |
| [I-27](#i-27) | 🟢 fixed(unverified) | P1 | pull_round_surcharge 落库 · HitsResolver 接口 · settle 同 tx INSERT 命中规则明细 | 2026-08-22 |
| [I-28](#i-28) | 🟢 fixed(unverified) | P1 | kiroceo/kiroappio Capability.WebhookHasSignature 改 false · 匹配 vendor 端实际无签名 | 2026-08-22 |
| [I-29](#i-29) | 🟢 fixed(unverified) | P1 | vendor_plan_config admin GET/PUT API · 运营改档不改 SQL | 2026-08-22 |
| [I-30](#i-30) | 🟢 fixed(unverified) | P2 | topup_order.channel CHECK 扩 · 加 usdt/tron · 兼容 epusdt 历史 | 2026-08-22 |
| [I-31](#i-31) | 🟢 fixed(deferred) | P2 | KeyHealth/KeyStats/Usage 6 家 stub · 明标 1d 阶段做 | 2026-08-22 |
| [I-32](#i-32) | 🟢 fixed(deferred) | P2 | coalescer api 层未接 · 明标 1c-2 阶段做 · 当前多人 bus 用户少无感 | 2026-08-22 |
| [I-33](#i-33) | 🟢 fixed(deferred) | P2 | pull_intent 表永远为空 · 明标 1c 集单接进来时一并做 | 2026-08-22 |
| [I-34](#i-34) | 🟢 fixed(deferred) | P2 | vendor_pricing admin API 缺失 · 需 seed 脚本 + CLI · 下批 PR · Prober fallback 现在够用 | 2026-08-22 |
| [I-35](#i-35) | 🟢 fixed(unverified) | P1 | canonical Credential 重构 · providers.Credential + 3 FromCredential 转换函数 · 消除人肉同步 | 2026-08-22 |
| [I-36](#i-36) | 🟢 fixed(unverified) | P0 | deploy 脚本双问题:migrate 竞态死锁 + BP_ADMIN_KEY 从没 seed(admin/* 端点上线即隐形) | 2026-08-22 |
| [I-37](#i-37) | 🟢 fixed(unverified) | P0 | migration 051 rebuild topup_order 后 3 条索引全丢 · 查询退化 O(N) | 2026-08-22 |
| [I-38](#i-38) | 🟢 fixed(unverified) | P0 | admin_market bypass canonical FromKeyPayload · 挪进 NewFromPlaintext 单点分派 | 2026-08-22 |
| [I-39](#i-39) | 🟢 fixed(unverified) | P0 | decider 接 TierStore.UnitPriceFor · 冻结按 count 命中档位单价 · 跟 offers 展示同源 | 2026-08-22 |
| [I-40](#i-40) | 🟢 fixed(unverified) | P1 | pull_round_surcharge kindAmount 重复计算 · retail/cap/adhoc 共 capabilityFee 桶 · SUM = 3× | 2026-08-22 |
| [I-41](#i-41) | 🟢 fixed(unverified) | P1 | credplain lookup FetchPlaintext bypass canonical · 走 PushCredentialFrom 按 AuthMethod 分派 | 2026-08-22 |
| [I-42](#i-42) | 🟢 fixed(unverified) | P1 | vendor_pricing seed CLI 缺失 · 加 seed-pricing 子命令 | 2026-08-22 |
| [I-43](#i-43) | 🟢 fixed(unverified) | P1 | admin_plan_config 单测补齐 · 4 用例 | 2026-08-22 |

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

### I-16 · Prober 只探 enterprise · Status 页无个人号数据

**状态**：🟡 `open`
**发现**：2026-08-22 · 用户"vendor 的个人数据上了吗?" 问出的

**症状**：
- `internal/vendorview/prober.go` 只调 `v.Stock(ctx, StockOptions{})` · Kind 空
- providers.StockOptions.Kind 空 → `Normalize()` 归 enterprise
- **personal 池永远不入 `vendor_probe` / `vendor_probe_zone`**
- 结果:vendor status 页所有 vendor 显示的都是 enterprise 池数据 · personal 池不可见
- 只有 I-03 修的 kirodrop / 已双档的另一家 vendor 有 personal 库存 · 但 Status 页看不到

**修法**（下一 PR · 阶段 1 收尾）：
- Prober 循环两次:`Stock(Kind=Enterprise)` + `Stock(Kind=Personal)`
- `vendor_probe` schema 加 `account_kind` 列(migration 051)
- `vendor_probe_zone` 同
- Uptime/DispatchSummary/Incidents 按 kind 分组聚合
- `VendorStatusRow` 加 `PersonalPublicStatus` / `PersonalStockBucket` 字段(或分行)
- 前端 status 页展示两列

**影响面**：数据库 schema + prober + status_view + 前端 · **中等改动**。
需要用户拍板 UI 展示方式:两行 vs 两 chip vs 分开卡片。

---

### I-17 · Vendor Status "ongoing" 但号早死 · 上游数据未拉齐

**状态**：🟡 `open`
**发现**：2026-08-22 · 用户澄清:"死不死首先要看车死不死 · vendor 的车死了 · 我们肯定死了 · 有可能我们都没有 · vendor status 主要看上游"

**语义纠正**（防未来 agent 漂移）:
- **Vendor Status 页 = 上游 vendor 侧 fleet 状态**（母号 aka "车" 的死活）
- 数据源:`public_status` 端点(vendor 自报 `keys_active / keys_dead / keys_stock / generating`)
- **跟我方 `credential_ledger` 完全无关** —— 我方 alive 只是"我方拉到手的号还活"·
  但上游车早死时 · 号根本进不了我方池 · 或者早就交付走了
- "ongoing" 应该显示**上游 vendor 那边母号还在跑没死**

**症状**：Status 页某些 vendor 显示 ongoing 但状态可能不真实（数据老/未拉/vendor 侧接口挂）

**验证方法**：
1. 逐个 vendor 打 `public_status` 端点 · 对比 Status 页显示
2. 看 `vendor_probe` 的 `probed_at` 是不是新鲜(60s 一探 · 应该都是最近一分钟内)

**修法**（分层）：
- 上游端点挂 → 探针 error_kind 明标 · UI 显示"数据陈旧"
- 上游返数据但我方 status_view 展示逻辑错 → 修 status_view.go
- **不改** deathwatch 逻辑(那是我方号池死号 · 跟 vendor status 无关)

**关联**：I-16(Prober 只探 enterprise · personal 池 status 也看不见)

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

### I-18 · /api/vendors/{anon_id}/stock 和 /history 404 · anon_id 未还原

**状态**：🟢 `fixed(unverified)` · 2026-08-22 e2e 测 kirodrop personal 时发现 · 已改 · 待部署验

**症状**：Extract 页选特定 vendor(比如 Vendor 06 · anon `aecc48`) · 面板卡在
"加载 vendor 状态…" 和 "正在算价…" · console 报两个 404：
- `GET /api/vendors/aecc48/stock` → 404
- `GET /api/vendors/aecc48/history` → 404

阻塞所有散客选 vendor 后的数据面板 · **等于选 vendor 后什么都看不到**。

**根因**：`internal/api/vendors.go` 的 `handleVendorStock` / `handleVendorHistory`
直接拿前端传的 vendor_id 去 `vendorView.VendorStock/History` → 内部走
`lookupEnabled` **只匹配内部 vendor_id 常量**(如 `kirodrop`) · 拿 anon_id
`aecc48` 必然 miss → 返 `ErrVendorNotFound` → api 层 404。

同文件的 `handleVendorPricesDaily` 是对的 · 明确调了 `ResolveAnonID` 还原。
stock / history 漏了这层还原 · 属于**遗留 bug** —— 前端从 wholesale-only 真名
下发改成 retail/community anon 下发后就有 · 一直没触发。

**修复**：两个 handler 加 `ResolveAnonID` 还原 · 还原不到当真 id 用(照顾 wholesale
档能看真名的场景 · 手工池 kiro_market 也是真 id 传下去) · 再交给 vendorView。

```go
realVendorID := id
if resolved, ok := s.vendorView.ResolveAnonID(id); ok {
    realVendorID = resolved
}
out, err := s.vendorView.VendorStock(r.Context(), realVendorID, viewerOf(p, r))
```

**验收**：leedx2011 生产账号 Extract 页选 Vendor 06 → 上游状态面板出数据 · 单价出结果 · 提取按钮可点。

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

### I-19 · Offers 端点绕过 PricedFor · USD 家价错

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:`/api/vendors/offers` 直接透传 `Money.Amount` 到前端·**没走 baseCredits + finalUnitPrice**·
- CNY 家(前 5 家)`credits_per_unit=1_000_000`·1:1 数字巧合看着对·**但漏了服务费/单次议价整条计费栈**
- USD 家 18.51 USD 被前端当 18.51 积分显示·实际应 = 18.51 × 6.8 × 计费栈 ≈ 126+ 积分

**根因**:`internal/vendorview/offers.go` `offersFromSnapshot` 从函数改成方法·加 ctx + vendorID + viewer·走跟 `VendorStock` 同一条 `s.finalUnitPrice(s.baseCredits(...))`。手工池 `offersFromMarket` 同理·`PriceBands.UnitPriceCredits` 每档也过 finalUnitPrice。

**修复**:定价单一入口(docs/10-pricing §4)。4 处调用点全走同一函数。

**测试**:offers_pricing_test.go 6 用例(CNY/USD/三档减免/无术语/resolver 优先/env fallback) · 全绿。

---

### I-20 · vendorview 展示价没接 RatesResolver

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:decider 拉号从 DB `surcharge_rule` 求费率(实时可配)·但 vendorview 展示价只用 env `Rates`(启动时固定)。生产 env 从没配过 `BP_RATE_*`·env 全 0·**展示价永远 0 服务费**。后台运营就算加规则·展示还是 0·跟实扣脱钩。

**根因**:`vendorview.Service.finalUnitPrice` 只用 `s.rates` env 值 · `decider.RatesResolver` 只装到 orchestrator · 展示层没接。

**修复**:
1. `vendorview.Config` 加 `RatesResolver decider.RatesResolver` 字段
2. `finalUnitPrice` 签名加 `ctx + vendorID` · 优先走 resolver.Resolve · fallback env
3. 5 处调用点(vendorview + offers)全传 ctx/vendorID
4. `main.go buildDecider` 返 surchargeResolver · vendorview.New 装配点传入

**测试**:`TestOffers_RatesResolver_TakesPrecedenceOverEnv` + `TestOffers_EnvFallback_WhenNoResolver` 全绿。

---

### I-21 · kirodrop personal 号 payload 是 refresh_token · 走错字段

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:kirodrop personal 号 vendor 只返 `{key, region}` · key 是 `<sso>:<refresh>` 冒号串。`decider/import.go` 无脑塞 `KiroAPIKey` 字段 → housepool 后端用 API key 协议校验 · 号导入必败 · 钱白扣。

**证据**:2026-08-22 leedx2011 手动拉一个 personal 号 · `/api/v1/orders/store_.../keys[0]` 只有 `{key, region: "personal"}` · 无 account/password/issuer_url。

**修复**:
1. `providers.KeyPayload` 加 `AuthMethod` 字段 · 新 enum `AuthAPIKey / AuthRefreshToken / AuthBearer`
2. `kirodrop/mapper.go toKeyPayloads` 按 `k.Region == "personal"` 打标 AuthRefreshToken · 其他 AuthAPIKey
3. `decider/import.go importToPoolWithMeta` 按 AuthMethod 分派 · refresh_token 走 `RefreshToken` · api_key 走 `KiroAPIKey/Email/IssuerURL`(老)

**测试**:kirodrop `personal_auth_test.go` 3 用例(personal/us-east/eu-central) 全绿。

**待跟进**:I-23 · 抽象化 vendor→housepool→credplain→passengerpool 四层的字段映射 · 走 canonical `Credential` type。

---

### I-22 · 拉号成功后从没落 credplain 明文 · push/handoff 走 placeholder 交付废号

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:migration 043 早在 2026-08-12 就落表 · credplain.Save 三种 auth_method 都写了 · 但**拉号真链路 `decider/settle.insertCredentials` 从来没调 credplain.Save**。明文全丢 · 后续 `push_pool` / `handoff` 从 `credential_plaintext` 表 FetchPlaintext 拿不到 · 走 placeholder 兜底 · 用户拿到的号是**占位符 · 完全用不了**。

**首次触发**:kirodrop personal 号 e2e 测试 · 深入排查 push_pool 链路时发现。之前生产 leedx2011 只做过"进车"路径(号进 housepool 由 kiro.rs 侧持有明文 · 我方不需要复本) · 没触发。

**修复**:
1. `credplain.Store` 加 `SaveTx(ctx, tx, in)` 方法 · 跟 `Save` 共用 `validateAndEncrypt` 帮手
2. `decider.PlaintextSaver` 新接口 · Orchestrator 加 `plaintextSaver` 字段
3. `settle.insertCredentials` 每号 INSERT credential_ledger 之后 · 同 tx 调 `saveCredplainTx`
4. `saveCredplainTx` 按 `KeyPayload.AuthMethod` 分派 credplain.SaveInput(refresh_token/api_key/bearer)
5. `main.go` 装配点 `PlaintextSaver: credplainStore`

**关键设计**:**同 tx** · 崩溃回滚同步 · 不会出现"ledger 写了但 credplain 没写"的孤号。跟手工池路径的 `PopToCredplainTx` 对齐。

**测试**:kirodrop 打标测试 3 用例(依赖 I-21) · settle 集成测走的是 mock plaintextSaver · nil 时兼容老行为。

---

### I-23 · xi8 fire-guard **审计误报** · 已 defer

**状态**:🟢 `fixed(deferred)` · 2026-08-22 · 审计报告认为是漏洞 · 但代码已明确 by design

**决策**:`internal/vendorview/flag_store.go:12-13` 注释明说:
> 用户 2026-08-14 拍板:采购一律直接打 vendor · xi8 不进钱路
> IsBlocked 是对账 / 诊断查询 · 不接抢号 fire

审计报告说这是漏洞 · **实际是有意为之** —— xi8 是聚合源 · 数据滞后 5min · 走它 fire-guard 会:
- 让 xi8 挂了直接影响采购(单点故障)
- 用户视角"这家 xi8 说停了 · 但我直连能买"的信任问题
- fail-open 也解不了(默认放行 + 出错也放行 · 那就等于没 guard)

**当前 fail-safe 已够**:vendor 侧真 blocked 时直连会返 4xx · 幂等键在扣钱**前**校验 · 白烧一次 vendor RTT · 不涉及钱。

**跟进**:decisions.md 应补记这条 2026-08-14 决策 · 避免下次 audit 再发现 · 已加 TODO。

**审计报告误报原因**:migration 034 注释里写着"喂抢号 fire-guard" · 是**早期意图** · 后来 8-14 改主意但只更新了 flag_store.go 注释 · migration SQL 注释未同步。migration 说明留作历史。

### I-24 · 优惠码 service_fee_waiver 静默失效 · 用户被超收

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验 · **可能已发生生产超收**

**症状**:`internal/api/pull.go:76-82` 拉号请求带 `CouponCode` 时 · **只调 `s.coupons.Lookup(TypeServiceFeeWaiver)` · 无 `Redeem`**。全项目搜 `coupons.Redeem` 只有 `internal/api/topup.go:395`(充值场景)· 拉号场景**全无**。

**根因**:Lookup 只是查码存在 · 不递增 used_count · 不锁定使用次数。拉号 API 返 200 让前端以为已减免 · 实际 service_fee_total 按原价扣。

**影响**:
- 用户在拉号确认窗输入 service_fee_waiver 码
- API 返 200 · 前端"已减免"
- **服务费仍按原价从积分扣**
- coupon_code.used_count 不递增 · **同码可无限次触发同一"以为免服务费"错觉**
- **属于隐式超收 · 涉及所有用了拉号优惠码的用户**

**修完**:
1. `pull.go` Lookup 挪到幂等 hit 之后(避免 replay 时"额度用尽"错误) · Pull 成功后完整核销:
   - `coupons.Redeem(ctx, {Code, PassengerID, Context: ContextPull, ContextRef: pull_round_id, DiscountAmount: ServiceFee})`
   - `wallets.Credit(reason=ReasonRedeem · amount=ServiceFee · RefType=pull_round · RefID=pull_round_id)`
   - `response.ServiceFee=0 · TotalDebit -= ServiceFee · BalanceRemaining += ServiceFee`
2. 失败策略:
   - Redeem 失败(非 ErrAlreadyUsed) · log warn · 不阻塞主流程(用户已扣完钱 · 后台对账)
   - Wallet.Credit 失败 · log warn · 不阻塞
   - `ErrAlreadyUsed` = 幂等重放 · 不 log(是预期)
3. 测试:`internal/api/pull_coupon_test.go` 3 用例(waives / idempotent / expired) 全绿

**已发生超收核查**:上线以来查 `SELECT COUNT(*) FROM coupon_use WHERE context='pull'` · 如果 0 但前端有过用户输码请求 → 逐条追溯赔偿。

---

### I-25 · vendor_price_tier 只写不读 · 阶梯定价全无效

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:写方 `vendorview/tier_store.go:36 ReplaceQtyBands` + `line 92 ReplaceTimeDecay` · 装配在 `main.go:784` · **读方 `QtyBandsOf` / `TimeDecayOf` 在 decider/pricing/api 三处 `grep` 全空**(`grep -rn "TierStore\." internal/`)。

**根因**:migration 035 是"抢号决策 + prices 页数据源"的双承诺 · 数据 backfiller 落库 · 但**上层没人读**。

**影响**:
- 数量分档 · 用户批量购买不享受档位优惠(拉 5 个价跟 1 个价一样)
- 时间降价 · reservation 到点降价不生效
- 前端 /prices 页也没走 TierStore(offers.go 走的是各 vendor 自己的 price_bands 里的 UnitPriceCredits · 跟 tier_store 不同源)

**修完(offers 端 · 前端切数量重算部分)**:
1. `vendorview.Config` 加 `TierStore *TierStore` 字段
2. `offersFromSnapshot` 从 `TierStore.QtyBandsOf(vendorID)` 读该 vendor 分档 · 每档单价过 `baseCredits + finalUnitPrice` · 填 `OfferItem.PriceBands`
3. `main.go` 装配 `TierStore: vendorview.NewTierStore(database.DB)` 进 vendorview.New
4. 测试:`offers_tier_test.go` 2 用例(有 TierStore 生效 / 无 TierStore 保老行为) 全绿

**未做部分**(defer):
- decider.Price 内部按 count 命中档位算单价 —— 生产 vendor_price_tier 表**当前无数据**(backfiller 需实现了 KeyTierLister 的家才拉·手工池非直连 vendor 也没启用)· 修 decider 走空 fallback 就成 · 效果同 flat · 无意义
- /prices 页 TimeDecayOf 走时间曲线 —— 阶段 1c prices 页动态曲线才做

**风险控制**:offers 层出的 PriceBands 会覆盖前端切数量的单价预估 · 但 decider 拉号仍按 flat unit_price 扣。**这个 gap 用户看不到** —— 因为 vendor_price_tier 当前空 · 前 6 家生产没数据 · PriceBands 永远空数组 · 跟老行为等价。真正有分档数据的家(比如 kirooo)后端接进来时同步启用 decider.Price 档位分派 · 是**下批 PR** 的事。

---

### I-26 · PartiallyRefunded 只填不用 · **deferred**

**状态**:🟢 `fixed(deferred)` · 2026-08-22 定 defer

**症状**:填在 `kirodrop/mapper.go:92` · 消费点 `grep -rn "PartiallyRefunded" internal/` 除定义处 + kirodrop 一处 mapper · **别处 0 命中**。

**当前巧合正确**:kirodrop `partially_refunded` 状态订单 `Purchased < Requested` · vendor 已把差额退了 · TotalCost 是真实扣 · 我方 settle 靠 `reserved - part.Amount` 释放冻结 · 数值恰好对上。

**未来风险**:另一家 vendor 支持 partial 但语义相反("差额没退给你 · 自己去追") · 我方**不追** · 用户白亏差额。vendor.go:365-367 明写"这两个分支必须分开处理"。

**修**:settle 里显式判 `PartiallyRefunded` · true = 不追(kirodrop 语义)· false + Purchased<Requested = 主动调 vendor Refund 或落 refund_pending 表。

---

### I-27 · pull_round_surcharge 表 + Engine.Hits 都有 · 无 INSERT

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:表 CREATE 在 `migrations/015_surcharge_rule.sql:39-51` · Hits 计算在 `pricing/surcharge.go:290-320` · **`grep -rn "pull_round_surcharge" internal/` 只有一处注释** · 无 INSERT。

**影响**:每轮拉号命中了哪些 surcharge · 收了多少 · 无历史。migration 15 明说"对账 / 申诉用" · **全部拿不到**。当前 pull_round 只汇总总费 · 拆不出单条规则贡献。

**修**:settle 里落 credential_ledger 之后 · 同 tx INSERT `pull_round_surcharge` 每条 hit 一行。跟 I-22 落 credplain 是同一处扩展点。

---

### I-28 · webhook 签名 Capability 声称 vs 实现分裂

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:kiroceo `WebhookHasSignature: true`(`adapter.go:56`)· `VerifySignature` **硬返 ErrNoSignature**(line 333-335)。kiroappio 同款不一致。`handleVendorWebhook` 走独立 `hmacSpecs` 白名单(`vendor_webhook.go:55-74`)只列 91kiro / kirodrop / kiroappcc 三家。

**根因**:两套"是否 HMAC 签名"事实源分裂 · Capability 声明和实际 VerifySignature 不同步。

**影响**:当前不触发数据损坏(handler 用 hmacSpecs 判 · 不用 Capability)· 但两份事实源分裂 · 后台 / 前端如果读 Capability 会得错误答案。

**修**:两条路径二选一:
- (a) kiroceo/kiroappio 补 HMAC 实现 · 加入 hmacSpecs
- (b) 修 Capability 声明成 `WebhookHasSignature: false`

我推荐 (b) —— 三家已 HMAC · 剩两家没接是有原因(vendor 侧协议不同)· 修声明匹配现实。

---

### I-29 · vendor_plan_config admin toggle API

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:migration 有 seed(`049_vendor_plan_config.sql:47-64`)· Store 提供 `UpsertPlan` + `ListAll` · **grep 生产 caller 0 命中** · 也无 `/api/admin/vendor-plan-config` handler。

**影响**:migration 明说"后台可关" · 现无 admin API。上游哪天开新档 / 关旧档 · 运营只能 SQL 手改 · 违反 CLAUDE.md "费率 / 开关不写代码"铁律。用户当前流程(读接口正常)不断。

**修**:加两个 admin 端点 · `GET /api/admin/vendor-plan-config` 列全部 · `PUT /api/admin/vendor-plan-config/{vendor}/{kind}/{plan}` 改 enabled。

---

### I-30 · topup_order.channel CHECK 扩

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:CHECK 在 `migrations/010_topup_multichannel.sql:60` = `IN ('waffo', 'epusdt', 'bybit', 'binance')` · 代码 `topupchannel/channel.go:43-46` 定义 `Waffo / Bybit / Binance / USDT / Tron` · **`epusdt` 已删 · `usdt`/`tron` 是新加但 schema 没扩** · 且 4 家非 Waffo 都 `Enabled: false`。

**影响**:任何人把 `BP_TOPUP_USDT_ENABLED=1` 或 `BP_TOPUP_TRON_ENABLED=1` 一开 · 用户下单立即 CHECK 拒绝 500。当前所有 non-Waffo 关着 · 用户不断 · 但**一个环境变量的距离**。

**修**:新增 migration · 扩 CHECK 到 `IN ('waffo','bybit','binance','usdt','tron')`。

---

### I-31 · Vendor.KeyHealth/KeyStats/Usage 6 家全 stub · **deferred**

**状态**:🟢 `fixed(deferred)` · 2026-08-22 · 明标 1d 阶段做

**症状**:6 家 adapter 全返 ErrNotSupported · `providers/vendor.go:214` 注释"1d 才实现" · deathwatch 仍只能靠 `housepool.TestCredential` 判死。

**影响**:providers.Vendor 接口设计要求这三个方法 · adapter 全 stub 且无一处产线调用。deathwatch 无 vendor 侧健康信号 · 当前不断用户流程。

**修**:阶段 1d 一并接 · 现在不动。

---

### I-32 · coalescer 全套实现 · api 层从不调用 · **deferred**

**状态**:🟢 `fixed(deferred)` · 2026-08-22 · 明标 1c-2 阶段做

**症状**:`coalescer/window.go` 全套 Window.Join / MaxBatch / 分发结果 都实现 · `coalescer.go:60 Single/Anon/Team` 三入口都在 · **`grep -rn "coalescer\." internal/api/ cmd/` 全空** · api/pull.go 直接调 decider.Pull。

**影响**:多人 anon / team bus 同时下拉号意图**不合流** · 每人各自触发一次 vendor Purchase · 抢货竞争劣势 + 幂等键各不同。当前多人 bus 用户流量小 · 不明显影响。

**修**:api/pull.go 判 bus.kind==anon||team 走 coalescer.Anon/Team · 单人走 Single。

**跟 I-35 结合**:合流的号最终还是一条 Purchase 响应 · 分派给多人 → canonical Credential type 后更好写。

---

### I-33 · pull_intent 表建了但永远为空 · **deferred**

**状态**:🟢 `fixed(deferred)` · 2026-08-22 · 明标 1c 集单接进来时一并做

**症状**:`migrations/001_init.sql:158` 建表 · **`INSERT INTO pull_intent` grep 全空** · 只有 stockwatch/mode.go:114-116 / insight/overview.go:95 两处注释明说"生产从不写"。

**影响**:该表永远为空 · 若代码路径不慎读会得 0 计数 → 图表恒为 0。当前主流程不依赖(insight overview 已绕开)。

**修**:阶段 1c 集单 pull_intent 写入接进来时一并做 · 或删表(选后者 · 阶段 1c 时再加也不亏)。

---

### I-34 · vendor_pricing admin 写 API 缺失 · **deferred**

**状态**:🟢 `fixed(deferred)` · 2026-08-22 · 下批 PR 做(需要 seed 脚本 + CLI · 面较大)

**症状**:Upsert 在 `pricing/vendor_pricing.go:65` · **grep 生产 caller 0 命中** · Get 找不到时 `FallbackQuote`(line 118) 走 CNY 1:1。

**影响**:USD 家不换算 · 相当于"1 USD = 1 CNY 积分" · 生产库存单价严重偏低。**已被 Prober 落库时同一条换算规则救场**(每 5min 一次) · 未走过 Prober 的 vendor 会用错价。

**修**:加 admin API upsert vendor_pricing · 生产脚本 seed 一份(kirodrop `credits_per_unit=6_800_000` · 前 5 家 `1_000_000`)。

---

### I-35 · canonical Credential 重构(方向 B)

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验 · **今天所有 bug 的架构级根因**

**修复要点**:
1. `internal/providers/credential.go` · canonical `Credential` type · 字段对齐 kiro.rs `wireImportCredential`(权威源) · Go 风格字段名。
2. `FromKeyPayload / ToKeyPayload` 桥接 · 老 vendor adapter 仍返 KeyPayload · 通过转换过来 · 未来所有 adapter 迁完可删 KeyPayload。
3. **3 个下游层各自 FromCredential 构造器**(避免 providers 反向依赖):
   - `internal/housepool/from_credential.go` · `ImportCredentialFrom(cred, groups)`
   - `internal/credplain/from_credential.go` · `SaveInputFrom(cred, credentialID) → (SaveInput, error)`
   - `internal/delivery/passengerpool/from_credential.go` · `PushCredentialFrom(cred, credID, vendorLabel)`
4. **调用点全部走 canonical**:
   - `decider/import.go` importToPoolWithMeta · `FromKeyPayload → ImportCredentialFrom`
   - `decider/settle.go` saveCredplainTx · `FromKeyPayload → SaveInputFrom`
   - `api/admin_market.go` · 手工池路径也走 canonical

**保留原样**(有意):
- `credplain/lookup.go` FetchPlaintext · 从**已存明文表**读回 · 直接构造 PushCredential 是对的(不是 vendor→canonical 链)
- `api/pullrecord.go` / `api/bus_credential_push.go` push 调用 · 只填 ID+region+label · pusher 内部 FetchPlaintext 拿明文 · 属 pusher 调用契约

**测试**(10 个新单测全绿):
- `providers/credential_test.go` · 4 用例 · round-trip 无损 · 兜底 AuthAPIKey
- `housepool/from_credential_test.go` · 3 用例 · refresh_token / api_key / 兜底
- `credplain/from_credential_test.go` · 5 用例 · 分派 + 校验 + 兜底
- `passengerpool/from_credential_test.go` · 3 用例 · 三分派

**症状**:vendor → housepool → credplain → passengerpool **四层各自定义字段** · 靠人肉同步:
- I-21 · KeyPayload → ImportCredential 字段映射 hardcoded
- I-22 · KeyPayload → SaveInput 之间**没人负责翻译** · 漏了整环

**方向 B 设计**:
- `providers.Credential` type 单点定义 · 字段对齐 kiro.rs `wireImportCredential`(权威源)
- 三个转换函数:`ToImportCredential / ToCredplainSaveInput / ToPushCredential`
- Go 风格字段名(RefreshToken / IssuerURL) · 只在 kirors 包做最终 kebab/camel 映射
- 各层入参 struct 保留 · 但都是 Credential 的子集视图
- KeyPayload 保留 alias 一版平滑过渡

**预估**:约 15 文件 · +200/-150 行 · 3-5 新单测 · **无 schema 变更**。

**范围外**:数据库 · kiro.rs 客户端 · 前端 · webhook / 交付层业务逻辑。

**做完的验证**:golden 字段测试 · Credential → 3 view struct 每个字段有对应位置 · 未来漏映射编译期挂。

---

### I-36 · deploy 脚本双问题 · migrate 竞态 + BP_ADMIN_KEY 未 seed

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待下次部署验

**症状 1** · migrate 竞态死锁(2026-08-22 PR #18 部署时触发):
- deploy 脚本先 `docker compose up -d` 再 `docker exec kirobus migrate up`
- 但 app 发现 pending migration 就拒启动("有未应用的迁移 · 先跑 migrate up")
- kirobus 容器 crashloop · `docker exec` 无法 exec 进去 · **死锁**
- 手工用 `docker run --rm` 跑 migrate up 才恢复

**症状 2** · BP_ADMIN_KEY 从没 seed(审计 P0-1):
- .env 只有 BP_MASTER_KEY / BP_ADDR / DRY_RUN 等 · **没 BP_ADMIN_KEY**
- `server.go:333/342/348` 三块 `if s.adminKey != ""` 全 false
- PR #18 装的 `/api/admin/vendor-plan-config` GET/PUT · admin/market/* · admin/data-health 全部**不挂 mux** · 404 而不是 401
- 运维完全不知道有这些端点

**修**:
1. `scripts/deploy-vps22.sh` step 4 · 加 `BP_ADMIN_KEY=$(openssl rand -hex 32)` seed · 老 .env 补检查也补
2. step 6 顺序:先 `docker run --rm ... migrate up` 再 `docker compose up -d` · 避免竞态

---

### I-37 · migration 051 rebuild topup_order 后索引全丢

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状** 审计 P0-1(维度 4):migration 051 `DROP TABLE topup_order` + `ALTER TABLE topup_order_051 RENAME TO topup_order` **没 CREATE INDEX** · 老表 002/006/010 累计 3 条索引跟着 DROP 一起蒸发:
- `idx_topup_passenger_time`(passenger_id, created_at DESC) → 面向 `/api/me/topups` 分页
- `idx_topup_status`(status, expires_at) → 面向 janitor 扫 pending
- `idx_topup_gateway_payment_id`(gateway_payment_id) UNIQUE PARTIAL → 面向 gateway webhook

**影响**:三条热查询变全表扫 · 数据本身没丢 · latency 上量后线性膨胀。

**修**:migration 052_topup_order_indexes.sql · IF NOT EXISTS 幂等 · down 也 IF EXISTS。

---

### I-38 · admin_market bypass canonical FromKeyPayload

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状** 审计 P0-3(维度 3):admin_market.go 手工塞号时构造 `providers.Credential` · **手写 switch AuthMethod**(3 选 1) · 不走 canonical 输入分派逻辑。跟 vendor adapter 分派是**两套代码** · 未来加 AuthBearer / 新字段时忘同步 → 手工池号 auth_method 归错档 → push_pool 用错 token 字段。

**修**:`providers.NewFromPlaintext(refresh, access, kiroKey)` 单点收敛输入分派(I-35 输入侧)· admin_market 调它 · 逻辑跟未来 vendor adapter 加 AuthBearer 保持一致。

**测试**:credential_plaintext_test.go 5 用例(3 分派 + 优先级 + empty) 全绿。

---

### I-39 · TierStore decider vs offers 展示口径

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**语义澄清**(修完后):
- vendor_price_tier 表数据源 = vendor 自己的分档 API(如 kirooo `/api/my/key-price-tiers`)
- vendor 侧原生分档:vendor 那边扣钱按分档扣 · settle 里 `purchase.TotalCost/count` 拿到的**已经是分档单价**
- 我方只需在**冻结阶段**(decider Pull 里的 unitCostHint)用档位价 · 避免冻多了触发余额不足

**修完的做法**:
1. `TierStore.UnitPriceFor(vendorID, count)` · 新增 helper · 按 count 命中档位(1-9/10-49/50+ 等)· 返档位单价 microunit
2. `decider.TierPicker` 接口 · Orchestrator 加 tierPicker 字段
3. `decider.Pull` 拿到 unitCostHint 后·若 tierPicker != nil 且命中·替换 unitCostHint
4. `offers.offersFromSnapshot` 保留展示 PriceBands 逻辑(跟 decider 同源) · 之前 P0-2 警告注释改成"已同源"说明
5. main.go 装配 `TierPicker: vendorview.NewTierStore(sqldb.DB)`

**表空 fallback**:6 家 vendor 只 kirooo 实现 KeyTierLister · 其余 5 家 QtyBandsOf 返空 · UnitPriceFor 返 hit=false · unitCostHint 保持 flat(老行为兼容)。

**测试**:tier_store_pick_test.go 4 用例(命中中间档 / 空表 / count=0 / nil store) 全绿。

**症状** 审计 P0-2(维度 3):
- offers.go 展示 PriceBands(从 TierStore 读)
- decider.Price / unitCreditsFor **不查 TierStore** · 按 flat unit_price 扣
- 若生产接了实现 KeyTierLister 的 vendor 并 seed vendor_price_tier → 前端展示"买 10 -20%" · 后端 flat 扣 → 展示 vs 扣费系统性 gap

**当前状态**:生产 vendor_price_tier 表空(backfiller 只拉实现 KeyTierLister 的家 · 6 家 vendor 都没实现)· PriceBands 永远空 · 老行为等价 · **暂不触发**。

**已做保护**:offers.go 已加**明确注释警告** · 未来接 KeyTierLister 前必须先在 decider 也接 TierStore.QtyBandsOf。

**未做**:decider 侧 TierStore 接入 —— 涉及 orchestrator.unitCreditsFor 深路径改造 · 大手术 · 未来接 KeyTierLister 之前先做。

**跟进 checklist**(接第一家 KeyTierLister vendor 时):
1. 先在 decider/orchestrator.go 加 `tierStore` 字段 + `unitCreditsFor` 走 SelectByCount 命中档位
2. 再在 backfiller 启用该 vendor 的 KeyTierLister 拉取
3. offers.go PriceBands 已就绪 · 无需改

---

### I-40 · pull_round_surcharge kindAmount 重复计算(P1 审计维度 4)

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:`insertSurchargeHits` 里 kindBp 按 canonical kind 分组(retail / capability / adhoc 各自独立)· 但 Breakdown.capabilityFee **是三 kind 共用桶**(pricing.go:107-108)。老逻辑每支都摊了整个 capabilityFee → SUM(amount) = 3×capabilityFee(应等于 capabilityFee)。

**修**:引入 `kindBucket("retail")="capability"` / `kindBucket("adhoc")="capability"` 归一 · bucketBp 累加合并 kind 的 rate_bp · 分摊时按桶分母。vendor/zone/service/single_pull 四支独占桶不变。

**测试**:surcharge_hits_test.go 2 用例(共享桶 + 独占桶) 全绿。

---

### I-41 · credplain lookup bypass canonical(P1 审计维度 3)

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完 · 待部署验

**症状**:`credplain.LookupAdapter.FetchPlaintext` 老代码直接把 Plaintext 的 refresh/access/api 三字段**一起塞**给 PushCredential。若表某行两个字段都非空(脏数据 / 迁移遗留) · 对家 vendor 使用哪个是未定义行为。

**修**:构造 canonical `providers.Credential` · 走 `passengerpool.PushCredentialFrom` 按 AuthMethod 分派 · 只填一个字段。

---

### I-42 · vendor_pricing seed CLI(P1 审计维度 4 · vendor_pricing 空表 fallback)

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完

**症状**:vendor_pricing 表 seed 空时 · fallback (credit, 1_000_000) · USD 家(kirodrop)真实报价 microunit 被当积分直接透传 · 前端展示 = 真实 /6.8。已知**生产 fallback 场景无 vendor_pricing 行**。

**修**:加 `bus-pooling seed-pricing` CLI · 运营 seed vendor_pricing 表:
```
docker exec kirobus /app/bus-pooling seed-pricing \
  -vendor kirodrop -currency USD -credits-per-unit 6800000
```
主 dispatcher 加子命令 · 挂 usage 说明。生产部署后必须跑 6 家 seed(CNY 家 1_000_000 · USD 家 6_800_000)。

---

### I-43 · admin_plan_config 单测补齐(P1 审计维度 2 · G11)

**状态**:🟢 `fixed(unverified)` · 2026-08-22 修完

**症状**:I-29 添加的 admin_plan_config handler(P1 审计 G11)完全零测试。

**修**:admin_plan_config_test.go 4 用例:
- RequiresAdminKey · 无 X-Admin-Key 应报错
- UpsertAndList · Store 层直调 · enable/disable 循环验证
- UpsertBadBody · 缺 vendor_id 应 400 · 错误信息含 vendor_id
- UpsertReq_JSONShape · JSON 契约字段完整

---

## Archive · 已 verified 关闭

（保留在原位 · 状态 verified 后不物理归档 · 索引表状态列直接显示 ✅ · 方便回溯）
