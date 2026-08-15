# 阶段 1 全量验收 · Phase 1 Acceptance Freeze

**验收时间**: 2026-08-15
**当前基线**: sprint-1e 分支 · 工作树 dirty(1e webhookout_bridge.go 未 commit)· sprint-1f HEAD=5a13e29 已完成 1f 落码

## 一句话总结

阶段 1 六大能力(1a-1f)代码路径全部就位 · **但 sprint-1f refactor 撤 nullable 时遗留两份 stale test → go vet/build 直接爆错 · CI 门槛未过** → **暂不允许 merge sprint-1f → main**;修完 test + 幽灵 API + 死 UI 后可进入 Stage 0 归档;live-ready 仍需 kiro.rs 侧先开 reveal endpoint(P0-6/P0-7 同一外部 gate)。

## 结论

- **是否 feature-complete**: ⚠️ candidate — 代码路径齐,CI 未过(P0-1 stale test)
- **是否 live-ready**: ❌ — kiro.rs reveal endpoint 未开,Stage 2/6 卡点(P0-6/P0-7)
- **是否允许 merge sprint-1f → main**: ⏸ 暂缓 — 修 5 条内部 P0(P0-1..P0-5)后可 merge;P0-6/P0-7 是外部 gate,不阻 merge 只阻 live
- **下一步 action**: 修 §7 两条 stale test → 修 §3 幽灵 PUT /members/{pid} → 修 §6 三条 UI 撒谎点 → Stage 0 归档 → 等 kiro.rs reveal 就绪进 Stage 2+

## 8 层验收结果汇总

| 层 | verdict | P0 | P1 | P2 |
|---|---|---|---|---|
| §1 阶段范围 | p1_before_live | 2(外部) | 2 | 3 |
| §2 主文档一致性 | p1_before_live | 0 | 11 | 5 |
| §3 API 契约 | **p0_blocker** | 1 | 3 | 3 |
| §4 DB migration | p2_cleanup | 0 | 0 | 7 |
| §5 策略调度 | p1_before_live | 0 | 2 | 2 |
| §6 前端真实性 | **p0_blocker** | 3 | 2 | 4 |
| §7 测试上线 | **p0_blocker** | 3(等价 1 根因) | 3 | 2 |
| smoke live | partial | - | - | - |
| **合计** | **p0_blocker** | **9(3 外部)** | **23** | **26** |

## P0 Blockers(9 条 · 3 外部 gate + 6 内部)

按严重程度排 · 严格 file:line 定位 · 一眼看懂 · 一句话修法。

### P0-1 · stale test 卡 CI(阻 Stage 0 全线)

- **位置**: `internal/api/strategy_1f_test.go:111` + `internal/bus/strategy_nullable_test.go:30`
- **问题**: commit 6d446e9 refactor(1f-040) 撤 nullable 时,`bus.Strategy.AutoRefillEnabled` 从 `*bool` 撤回成 `bool`(`internal/bus/bus.go:74`)、`RefillWatermark` 从 `*int` 撤回成 `int` · **两份 test 遗留没同步** · go vet 报 `invalid operation: got.AutoRefillEnabled == nil (mismatched types bool and untyped nil)` · internal/api + internal/bus 两个包 build failed
- **影响**: sprint-1-final Stage 0 "全套 CI 通过(go build + vet + test -race + npm build)" 铁门槛未过 → 阻 Stage 1-7 全线
- **修法**: 二选一 · (A) 按最新语义改断言 · 把 `!= nil` 改成 `!b.Strategy.AutoRefillEnabled` · 删 `falseVal := false` 指针化;(B) 若需保持 §4.3.2b 方案 A 语义(跟随全局)· 回滚 040 撤 nullable 部分。**推荐 (A)**,简洁 · 与 migration 040 一致

### P0-2 · 幽灵 PUT /api/me/buses/{bus_id}/members/{pid}(挂起按钮 404)

- **位置**: `web/src/api/hooks.ts:437 useSetMemberSuspended` + `web/src/pages/BusDetail.tsx:368` + `internal/api/server.go:212-232`(只挂了 DELETE)
- **问题**: 前端 hook 用 `put()` 到 `/api/me/buses/{busId}/members/{memberId}` 传 `{ suspended: bool }` · **真后端 server.go 只挂了 DELETE /members/{pid} 没挂 PUT** · MSW mock 有完整实现骗过本地开发 · 生产会返 405/404 · 用户 UI 看 spinner 转完但操作完全失败
- **影响**: CLAUDE §0.1 铁律违反 · Tab D 成员挂起/解挂功能不可用
- **修法**: 二选一 · (A) 后端 `server.go` 加 `mux.Handle("PUT /api/me/buses/{bus_id}/members/{pid}", handler(s.RequireAuth(s.handleSetMemberSuspended)))` + 对应 handler + `bus.Store.SetSuspended` 方法(schema 里 `members.status` 已有 'suspended' 枚举 · decisions §8.26 明标 1c 功能);(B) 前端把挂起按钮标 disabled + tooltip 'v1.0 后开放'。**推荐 (A)**,符合 sprint-1e 已到位的多人拼车语义

### P0-3 · Preferences 页缺全局护栏 3 字段 UI(跨车调度护栏无控件)

- **位置**: `web/src/pages/Preferences.tsx`(缺渲染)
- **问题**: `auto_refill_daily_budget` / `auto_refill_min_wallet_reserve` / `auto_refill_vendor_allowlist` 三字段 · 后端 `internal/api/strategy.go` strategyResponse + buildPatch 已完整实现 · migration 040 已落库 · TS 契约 `web/src/types/index.ts:614/616/619` 已定义 · MSW fixture `web/src/mocks/fixtures.ts:834-836` 有 seed 值 · **唯独 Preferences.tsx 从未渲染这 3 个字段** · 乘客看不到、也调不动
- **影响**: 阶段 1 明标要落的"跨车调度护栏"实际上不存在于用户视野 · migration 040 撤镜像的核心新价值悬空
- **修法**: 在 Preferences.tsx 加第 3-4 张卡 · 字段:daily_budget (Input 积分/'留空 = 不限') · min_wallet_reserve (Input 积分/'留空 = 不限') · vendor_allowlist (多选 chip 或 select · 值域 VENDOR_NAME keys) · onSave 补 3 个参数进 useSaveGlobalStrategy mutate 载荷

### P0-4 · Downstream 4 toggle 里 3 个是死控件

- **位置**: `web/src/pages/Downstream.tsx`(RULES 数组 3 条)
- **问题**: `PUT /api/me/downstream/passengerpool` 真会把 push_on_pull / resync_on_dead / retry_on_failure / bus_only 4 个布尔值写进 passenger_downstream 表(`internal/api/downstream.go:216-234`) · **除 bus_only 被 `internal/webhookout/events.go:95` 消费外 · 另外 3 个字段在 internal/delivery/、internal/pullrecord/、internal/decider/、internal/deathwatch/ 里都没有任何读点** · `webhookout/events.go:85-87` 明确注释 "push_on_pull / resync_on_dead / retry_on_failure 跟 webhookout 无关"
- **影响**: 用户勾选/取消这 3 个开关 · 系统行为 0 变化 · CLAUDE §0.1 铁律违反
- **修法**: 二选一 · (A) 在 delivery/passengerpool 里真读并按语义生效(push_on_pull 控 assign into_bus 触发双写 · resync_on_dead 控 deathwatch 号死同步删推 · retry_on_failure 控 pusher 失败退避);(B) 从 UI 撤 3 条 rules 只留 bus_only。**推荐 (B)**,阶段 1 收官优先精简

### P0-5 · docs/05-api-contract.md estimate 响应形状写错 + 暴露 §0.1 禁字段

- **位置**: `docs/05-api-contract.md:817`
- **问题**: doc 表写 `POST /me/pull/estimate` 返 `{ key_cost, single_pull_fee, service_fee, total }` · **后端 `internal/api/estimate.go:23-27` estimateResp 实际是 `{ unit_price, service_fee, total }`** · 前端 `web/src/api/hooks.ts:497-500` useEstimate 也期望 `{ unit_price, service_fee, total }` · **且 key_cost / single_pull_fee 是 CLAUDE §0.1 明令禁的内部加价链字段**
- **影响**: 契约文档过度承诺 · 三方按 doc 抄会拿到错字段 · 且泄露内部加价链
- **修法**: 改 `docs/05-api-contract.md:817` 字段列表为 `{ unit_price, service_fee, total }` · 跟后端 struct 和前端 TS 对齐 · 删加价链分层

### P0-6 · passengerpool 双写走 placeholder(外部 gate · 阻 Stage 6)

- **位置**: `internal/delivery/passengerpool/pusher.go:288-310` (fetchPlaintext + placeholderPlaintext)
- **问题**: 1e 交付承诺"去向 ② 推 passengerpool(双写)" · 但 `housepool.HousePool.GetCredentialPlaintext` **整仓未实现** · Plaintext=nil 或 `BP_ALLOW_PASSENGERPOOL_PLACEHOLDER=1` 时每号发 `PLACEHOLDER:not-a-real-token:<id>` · 上游 kiro.rs 未开放 reveal endpoint · 1f-audit.json fixable_tonight=false
- **影响**: **外部依赖 gate** · 阻 Stage 6 灰度 · 不阻 sprint-1f merge(1f 收敛的是策略层不是双写)
- **修法**: kiro.rs 后端先加 `GET /credentials/{id}/plaintext`(或等价) · bus-pooling 侧装配层传真 PlaintextLookup · 属外部依赖 gate · 需协调 kiro.rs 侧优先落 reveal API

### P0-7 · handoff 真明文 DELETE 未落地(外部 gate · 同 P0-6)

- **位置**: `internal/api/handoff.go:138-150,166-182` · `readHandoffPlaintext:282` hardcode 返 `errHandoffPlaintextUnavailable`
- **问题**: 1a 交付承诺"handoff 三段式" · 真 DELETE 明文路径未接 · 三态兜底(默认 501 / PLACEHOLDER / BP_HANDOFF_TRUE_PLAINTEXT 才走真 DELETE) · 上游未提供 reveal endpoint · smoke 已实测 GET /api/me/handoff/{token} 返 `501 handoff_not_ready`
- **影响**: **外部依赖 gate** · 跟 P0-6 共用 kiro.rs reveal endpoint · 阻生产切 live
- **修法**: 跟 P0-6 一起处理 · 需协调 kiro.rs 侧优先落 reveal API

### P0-8/P0-9 · smoke live 卡点(vendor 无货 + kiro.rs reveal 未开)

- **位置**: smoke 报告见 `docs/1f-live-test.md`(2026-08-15 10:00 段)
- **问题**: 6 家 vendor 全 409 no_stock(市场真实无货 · 非代码问题) · handoff GET 返 501 handoff_not_ready(同 P0-7)
- **影响**: Stage 3-4 单家 vendor 灰度需等 vendor 补货窗口 · Stage 6 handoff 等 P0-7 gate
- **修法**: 等外部依赖 · 不需代码改动

## P1 Before Live(23 条 · 上线前必修)

### 阶段范围(2 条)

- **1f-B 三字段 fallback 待用户拍板**:`internal/strategy/effective.go:227-247` · migration 039 三行 fallback 已被自我审计标撤回 · 但 UI 三个 Follow global toggle 仍在 · 需拍板 (A) 撤三行 fallback + 撤 UI toggle · 或 (B) 拍板保留 + 补文档定义 seed+fallback 双语义
- **建车向导 Advanced 里仍出现 daily_round_limit / daily_spend_limit 输入**:docs/15-scheduling.md §4.1 明标车级已废弃 · 但 DB/API/TS 三处都还保留 · 前端建车表单 Advanced 段仍显示两字段。修法:前端建车表单拿掉两字段输入位或改成只读 · 后端 DTO 加 deprecated 注释

### 主文档一致性(11 条 · 大面积滞后)

主文档大面积落后于 migration 040 / sprint-1e 结论:

- **docs/15-scheduling.md §4.3.2/§4.3.2b/§4.3.2c 整节仍描述已撤的 nullable 继承(方案 A)**:整节需重写为"车级 NOT NULL · 全局层 default_* 只做建车 seed · 无运行时 fallback" + 补 3 个新护栏字段引入原因
- **docs/06-db-schema.md §8 bus 表 + §16 passenger_strategy_default 描述与实际 schema 相反**:bus 三字段改成 NOT NULL DEFAULT 0 描述 · §16 建表 SQL 补 3 个新护栏字段 · Migration 索引补 040 条目
- **docs/05-api-contract.md §7 auto_refill 三字段 null 语义已作废**:GET /me/strategy 响应示例增列 3 个护栏字段 · PUT 三字段 null 语义整段删 · 收敛到"auto_refill_enabled bool / refill_watermark int / refill_min_count 保留 nullable"
- **docs/03-modules.md §7 strategy 依赖描述里仍写 fallback + 缺 3 护栏字段**:L144 补 3 字段 · L145 车级说明改为 NOT NULL(migration 040)
- **docs/06-db-schema.md §5 wallet_ledger reason 枚举漏 vendor_fee / region_fee 两条**:补两条 reason(拉号分层记账 · 三档模型 retail 才落)
- **docs/06-db-schema.md §20 状态机表节标题 + Migration 索引没收 pending_refill / stock_watcher / vendor_probe 系列**:§20 补 pending_dissolution + pending_refill 状态机表 · Migration 索引补 021-041 每条摘要 · §18 补 stock_watcher / vendor_ledger 等表定义
- **docs/01-architecture.md §5 目录树缺 5 个已存在的业务包 + 上限断言与现实脱节**:目录树补 downstream / insight / vendoraccount / vendorview / xi8 · "不超过 15 个"附注补一句"基础设施 + 支撑层不计入"
- **pending_purchase 表 · reserve_split_json 列 06/09 两处都没收**:06-db §20.pending_purchase 建表补 reserve_split_json TEXT 列 · 09-transactions §2 补按人拆冻结/释放说明
- **docs/09-transactions.md §7 janitor 扫描范围漏 pending_refill**:§7 扫描范围补 pending_refill + pending_dissolution
- **docs/15-scheduling.md §4.3.4「不存在的路径」把已存在的 webhookout_bridge.go 列进去 · 表述互相矛盾**:L412 挪出去 · 换措辞明确 bridge 存在(sprint-1e passengerpool 双写落地时新建)
- **CLAUDE §4.2 15 业务包清单跟 03-modules §业务包盘点对不齐 + payment vs topup/topupchannel/paymentgw**:CLAUDE §4.2 第 6 项改成 topup 家族三包 · 底部备注 pricing / stockwatch / vendorbalance / insight 是 decider 支撑包 · 删/改 L251「❌ 新加 pricing」条目(pricing 已建)

### API 契约(3 条)

- **docs/05-api-contract.md 里 estimate 响应形状写错**:见 P0-5(已升级 P0)
- **handleHandoffInit 未验 X-Idempotency-Key · 前端却送了**:`internal/api/handoff.go:34 handleHandoffInit` · `web/src/api/hooks.ts:655 useHandoffInit(postIdempotent)` · 双击/网络重发时后端会为同一批 credential 起两个不同 download_token · 各自 TTL 5min 内都能取明文。修法:后端补 ensureIdempotencyRecord 或前端 useHandoffInit 改回普通 post
- **docs §4/§6 列了未实现的 GET /me/buses/{id}/stats · GET /me/credentials · GET /me/credentials/{id}**:doc `05-api-contract.md:298 · :475-477` 列了但 server.go 未注册 · 修法:doc 里标 `阶段 1d/未实现` + strikethrough,或删

### 策略调度(2 条)

- **全局 3 大 guardrail 决策路径未 enforce**:migration 040 加 auto_refill_daily_budget / auto_refill_min_wallet_reserve / auto_refill_vendor_allowlist · API + Store 已开出 · **决策路径(decider/scheduler/deathwatch/webhookAutoScanBridge)一处未 enforce** · 上线前必接。修法:在 schedulerDecideBridge / refillDecideBridge / webhookAutoScanBridge 三桥调 Effective 后加读三字段 + 判 · 手动拉号不受此约束
- **缺 migration 040 保行为集成测试**:039 有 TestMigration039_PreservesBusAutoRefillValues · 040 无对应测试 · 若未来误改 migration 040 SQL 单测跑不到。修法:新建 `internal/db/migrations_040_test.go` 复用 039 骨架

### 前端真实性(2 条)

- **顶栏通知铃铛 popover 占位**:`web/src/layouts/AppLayout.tsx:70-118 NotificationsBell` · 阶段 1 明标 · 未来做 /api/me/notifications 时替换 popover 内容
- **GET /api/me/trend Overview 端点前端未测过滤参数 (bus_id / vendor)**:mock 接受 4 个 query params 按参数生成不同曲线 · 但 Overview.tsx 是否传 bus_id/vendor 未定位 · 需验证真后端 handleTrendWith 是否解析

### 测试上线(3 条)

- **internal/housepool 无 test 文件**:只有 kirors 子包有 · Stage 2 切 housepool 真链路的语义只靠 kirors 客户端测试兜底 · 修法:阶段 1 收官前建议至少补一份 internal/housepool 包级 unit test
- **web / vendoraccount / kiroappio / kiroceo 也无 test**:vendoraccount 是 vendor api_key/webhook_secret 明文密态存储的关键路径 · kiroappio / kiroceo 是 6 家 vendor 中的两家 · 修法:vendoraccount 补 encrypt/decrypt round-trip · kiroappio/kiroceo 补 stock/purchase/webhook 归一化三条测试
- **只有 smoke-1f.sh 一份 smoke 脚本 · 没有 stage-1..6 分级 smoke**:每档切换后靠人肉验 · 修法:派生 scripts/smoke-stage1-payment.sh · smoke-stage2-housepool.sh · smoke-stage3-vendor.sh 三份

## P2 Cleanup(26 条 · 阶段 1 收尾清单)

### 阶段范围(3 条)

- **陈旧管线告警未真外发生产验证**:`cmd/bus-pooling/main.go:696-700 BP_ALERT_WEBHOOK` · Stage 6 前给 env 一个 slack/discord webhook URL 即可
- **死号退款按分摊比例生产验证未完成**:`sprint-1c-backend.md` 上线判据倒数第 1 项 · Stage 3-4 单家 vendor 灰度时人为触发一次号死观察 wallet_ledger + warranty_refund 台账
- **/extract 与 /dispatch 路由并存 · 术语混用**:1f-review.md Part 1 warn.1 · 定一个主路由 · 另一个 301 redirect

### 主文档一致性(5 条)

- **pull_intent 表 001 建了从未写过 · 主文档仍照旧列它**:migration 027 stock_watcher 头注释明文说明实际拉号走 pending_purchase · 修法:附注"pull_intent 表为 1a 规划表 · 生产未使用"
- **10-pricing §2.5 rates 表格与 CLAUDE §1.3 的"不写进代码"措辞可再对齐**:CLAUDE §1.3 加一句"初始默认率写在 docs/10-pricing.md §2.5 · 代码只读 surcharge_rule 表 · 前端永不见"
- **05-api-contract §POST /me/buses 例子的 kind 说明与 CLAUDE §2 术语对齐但缺 anon 说明**:附一句"kind=anon 不通过此端点创建 · 由 coalescer 内部撮合创建 · 用户不感知"
- **06-db-schema §表数累计计数最后停在 30(1c 收工) · 之后从没更新**:L731 之后加了 vendor_probe 系列 · stock_watcher · pending_refill 等一批表 · 修法:全 migration 大盘点或改成"每次 migration 只写变更 · 不再维护总数"
- **03-modules §依赖图缺 stockwatch / vendorbalance / pricing / xi8 / vendorview 的连线**:§依赖图补 decider 支撑包群子图

### API 契约(3 条)

- **Docs.tsx MATRIX 覆盖只是抽样 · 标题'端点矩阵'过度承诺**:`web/src/pages/Docs.tsx:280-359` · 76 路由只覆盖了 30 左右 · 修法:改标题为"关键端点抽样"或补齐
- **前端未消费的后端端点(unused)**:8 个后端已实现但前端 hooks.ts 无 hook 调用 · history-summary / pull-records/{record_id} / topup/{order_id} / topup-orders / topup/channels / vendors/status/{anon_id}/price-trend / vendors/{vendor_id}/prices/daily / buses/{bus_id}/leave · 修法:逐个决策 · 特别是 /api/topup/channels 前端还在 hardcode waffo · 该接就接
- **useHandoffConfirm 送 X-Idempotency-Key · 后端只靠状态机幂等**:`web/src/api/hooks.ts:668` · `internal/api/handoff.go:188` · 行为没错但契约不一致 · 修法:前端改回 post 或 doc §5b 明确说明 confirm 是状态机幂等

### DB migration(7 条 · 文档漂移不影响运行时)

- **docs/06-db-schema §13 引用不存在的 vendor_webhook_delivery 表**:实际入向去重表是 migration 025 建的 inbound_webhook_event · 修法:删 §13 vendor_webhook_delivery 定义块 · 换成 inbound_webhook_event 完整字段定义
- **docs/06-db-schema §10 pull_round 遗漏 vendor_fee_total / region_fee_total 两列**:SQL 001 建 pull_round 时就有 · 修法:补两行
- **docs/06-db-schema §20 pending_handoff 遗漏 retry_count 列 + placeholder 两态**:migration 008 加 retry_count · migration 007 加 placeholder_delivered / confirmed_placeholder · 修法:补 retry_count 列 + 状态 CHECK 补两态
- **docs/06-db-schema 未收录 9 张 1b~1e 新表**:exchange_rate / stock_watcher / vendor_price_tier / user_subsidy / vendor_probe_zone / vendor_ledger / xi8_vendor_flags / pipeline_health / pending_refill · 修法:补入这 9 张表的 CREATE TABLE 定义
- **docs/06-db-schema §11 credential_ledger 遗漏 warranty_refunded_at 字段**:migration 018 已 ALTER TABLE · 修法:§11.1 ALTER 追加块补一行
- **docs/06-db-schema §7 topup_order 遗漏 fee_waiver_applied / fee_subsidy 列**:migration 020 加了 · 修法:CREATE TABLE 块补两行
- **docs/06-db-schema §3 passenger_downstream 遗漏 4 条推送策略 + 2 条 webhook 开关**:001_init 建表时就有 push_on_pull / resync_on_dead / retry_on_failure / bus_only · migration 038 加 webhook_enabled + webhook_events_json · 修法:补齐 6 列

### 策略调度(2 条)

- **handleBusPull 里 Effective + strategy.CanPull 二次计算 MaxUnitPrice**:`internal/api/bus.go:354-374` · 语义等价但冗余 · 修法:CanPull 保留 BusMaxUnitPrice 参数用于 record 场景 · 手动路径 eff.MaxUnitPrice 直接传 CanPull
- **scheduler / webhookAutoScanBridge SQL 扫全表(不预筛 auto=1)**:`internal/bus/autorefill.go:358-371` · `cmd/bus-pooling/main.go:1481-1491` · migration 040 撤镜像后可安全加过滤 · 修法:loadCandidates SQL 加 `AND b.auto_refill_enabled=1 AND b.refill_watermark > 0`

### 前端真实性(4 条 · 无需修 · 记录以免误解)

- **Handoff mock 明文格式和真后端不同(不构成 UI 问题)**:`web/src/mocks/handlers.ts:131-145` · mock 本分应该给假明文
- **注册 tier 判定 mock 简化(不构成 UI 问题)**:mock:填了 invite_code 就 tier='wholesale' · 真后端按 grants_tier 分 · UI 视角不区分 · CLAUDE §1.3 明确档次不对外暴露
- **AccountSettings 社交登录 disabled 明标处理规范**:`web/src/pages/AccountSettings.tsx:175-208` · 规范做法
- **/login 忘记密码为静态 span + title tooltip 而非链接**:`web/src/pages/Login.tsx:78-84` · 规范做法

### 测试上线(2 条)

- **MEMORY.md "睡前批处理"惯例 vs 当前 stale test**:commit 6d446e9 refactor 撤 nullable 漏尾清理 · 建议加一条 pre-commit 或 MEMORY 一句 "refactor struct 字段类型时必须 grep _test.go"
- **sprint-1-final Stage 7 里 sprint-1f 归档条目未完成**:1a-1e 已归档 · 修法:Stage 7 手动 checklist 明写或补 scripts/archive-sprint.sh

## §1 阶段范围

**verdict**: `p1_before_live`

### 实现完成

| 能力 | 证据 |
|---|---|
| 1a 全部 | internal/api · internal/bus · internal/housepool · internal/delivery/handoff · sprint-1a-backend.md 全部 [x] · 生产已跑 |
| 1b 5 家 vendor + 兑换码 + payment-gateway | internal/providers/kiro/vendors/{kiro91,kiroceo,kirooo,kiroappio,kiroappcc,kirodrop}/ 六家目录齐 · internal/redeem/redeem.go · internal/paymentgw/client.go · internal/topup/*.go · sprint-1b 上线判据全绿 |
| 1c anon 撮合 + 拼车码 + 号价 N 分摊 + 集单窗口骨架 | sprint-1c-backend.md 交付清单 [x] × 6 · internal/coalescer/window.go 骨架 · internal/decider/split.go · pull_round.participants_split_json · 生产验证 |
| 1d 号死自动补车 + vendor 余额切换 + AutoPick + webhook probe + 陈旧告警 | sprint-1d-backend.md 交付 [x] × 8 · codex 六刀收敛完 · Enqueue 执行链闭合 · internal/decider/decide.go(纯决策) + fire.go + refill_puller.go · internal/deathwatch/*.go · internal/webhookin/AutoScanNotifier |
| 1e 推 passengerpool 双写 + 对外 webhook | internal/delivery/passengerpool/{pusher.go,kirors/} · internal/webhookout/{dispatcher,events,retrier,sender,signer}.go · cmd/bus-pooling/webhookout_bridge.go 已装配(git status 未提交但骨架齐) · 1e-1..3 sprint 文档 code-complete |
| 1f 策略优先级铁律 + Effective() 单入口 + 全局默认三字段 + /docs Matrix/Fields tab | internal/strategy/{effective.go,effective_test.go} · migration 039_strategy_nullable_and_globals.sql · web/src/components/EditStrategyPanel.tsx · docs/15-scheduling.md §12-14 · web/src/pages/Docs.tsx · 1f-morning-brief.md 完成清单 全绿 · HEAD 5a13e29 |

### 阶段错分点(3 条 · 已划到未来阶段)

- **自动模式 scheduler 位置 2 · prebuy-pool 抢到无主号的分配路径** → 阶段 3d 市场 或 未来付费优先产品线(00 §6.5 🟩 加分项)
- **coalescer vendor 侧真集单接入 API** → 阶段 2a 列队策略(sprint-1c 明标延后)
- **1f 全局 default_auto_refill_enabled 三字段镜像作 fallback** → 阶段 1 不该做(见 1f-review.md P0 · 待用户拍板撤或留)

### 阶段缺口(6 条 · 均已标 gate)

- 1e passengerpool 双写 · 真明文推送 · gate=kiro.rs reveal endpoint(P0-6)
- 1a handoff 真明文 DELETE 落地 · gate=kiro.rs reveal endpoint(P0-7)
- 1d 位置 2 · prebuy-pool 无主号分配路径 · gate=决策未定 · 属于付费优先能力扩展
- 1d 位置 4 · coalescer 生产接线 · gate=已决议 2a 再评估
- 1d 陈旧管线告警真外发生产验证 · gate=填 env(BP_ALERT_WEBHOOK)即可
- 1c 死号退款按分摊比例生产验证 · gate=Stage 3+ 真链路验证

## §2 主文档一致性

**verdict**: `p1_before_live` · 主文档大面积落后于 migration 040 / sprint-1e 结论。

主要漂移集中在 1f-refactor(migration 040)撤 nullable 后 · 主文档三份(15-scheduling / 06-db-schema / 05-api-contract)全停在方案 A "nullable · 跟随全局 · inherit" 旧口径:

- **docs/15-scheduling.md §4.3.2/§4.3.2b/§4.3.2c**: 整节仍描述已撤的 nullable 继承(方案 A) · 严重度 high
- **docs/06-db-schema.md §8 bus 表 + §16 passenger_strategy_default**: 描述与实际 schema 相反 · 严重度 high
- **docs/05-api-contract.md §7 auto_refill 三字段 null 语义已作废**: 严重度 high
- **docs/03-modules.md §7 strategy 依赖描述里仍写 fallback + 缺 3 护栏字段**: 严重度 high

其他漂移见 P1/P2 清单。总体不阻塞代码运行 · 但会误导下一个 agent 按撤掉的镜像语义写代码 · P1 上线前必修。

## §3 API 契约

**verdict**: `p0_blocker`

- **路由总数**: 76 个真实 route(server.go mux.Handle)
- **三方一致**: 71 个后端-前端-docs 形状对得上
- **真幽灵**: 1 个(PUT /api/me/buses/{bus_id}/members/{pid} · 从 BusDetail 挂起按钮被点会 404 · 见 P0-2)
- **docs 响应形状错**: 1 个(estimate:{key_cost,single_pull_fee,service_fee,total} 实际 {unit_price,service_fee,total} · 且违 §0.1 · 见 P0-5)
- **docs 列了未实现端点**: 3 个(buses/{id}/stats · credentials · credentials/{id})
- **后端有前端不用**: 8 个(见 P2 清单)
- **handoff init/confirm 前端多送 X-Idempotency-Key**: 后端忽略(P1 · 无害但不一致)
- **Docs.tsx MATRIX 只是抽样不是全量**: 标题需要收敛

## §4 DB migration

**verdict**: `p2_cleanup` · 40 migrations 建 45 张表 · 517 字段 · 结构完整

- **migration 040(strategy_split_layers)已落**: bus.auto_refill_enabled / refill_watermark 从 nullable 回退 NOT NULL DEFAULT 0(refill_min_count 保持可空 · 语义=按 gap 补齐差额)
- **passenger_strategy_default 已加**: auto_refill_daily_budget / auto_refill_min_wallet_reserve / auto_refill_vendor_allowlist 三个跨车调度护栏字段
- **6 个 pending 状态机表齐**: pending_purchase / pending_assignment / pending_handoff / pending_dissolution / pending_topup / pending_refill
- **credential_ledger 关键 8 字段齐**: push_error_code / push_error_status / push_error_message / push_error_retriable / push_attempts / push_last_attempt_at / owner_bus_id / kiro_rs_credential_id
- **wallet 允许负余额记账**: migration 009 dropped CHECK (balance >= 0)

**无 P0/P1 阻断问题**;剩下的都是文档漂移(doc says X but SQL has Y)不影响运行时行为 · 落 P2 收尾清单(7 条)。

## §5 策略调度

**verdict**: `p1_before_live` · Effective() 单入口铁律遵守 · migration 040 语义正确

### 9 调用点验收(全通过)

| 调用点 | 路径 | 走 Effective |
|---|---|---|
| handleBusPull(manual) | internal/api/bus.go:354 | ✅ |
| handlePull(record) | internal/api/pull.go:111 | ✅ |
| refillDecideBridge.Decide | cmd/bus-pooling/main.go:1161 | ✅ |
| schedulerDecideBridge.Decide | cmd/bus-pooling/main.go:1329 | ✅ |
| webhookAutoScanBridge.decideAndAct | cmd/bus-pooling/main.go:1530 | ✅ |
| decider.Pull | internal/decider/orchestrator.go | ✅(上游已过) |
| stockwatch Firer.Fire | internal/stockwatch/store.go | ✅(Enqueue 已存) |
| handleCreateBus | cmd/bus-pooling/main.go | N/A(建车不是决策) |
| CanPull(fallback) | internal/strategy/canpull.go | ✅(canpull.go 无 BusDaily* 参数) |

### 铁律检查

| 铁律 | 结果 |
|---|---|
| 1. Effective 单入口 | ✅ pass |
| 2. auto_refill 无全局 fallback | ✅ pass(effective.go:241-248 直接读 busSt) |
| 3. 全局 guardrail 治跨车 | ❌ **FAIL**(见 P1) |
| 4. 手动拉号不受 auto guardrail 拦 | ✅ pass |
| 5. migration 040 保老车行为 | ⚠️ partial(SQL 用 COALESCE 正确 · 但缺集成测试 · 见 P1) |

### grep 违反

**无**。fallback_leaks: [] · grep_violations: []

### 残留

- scheduler loadCandidates 保留 `COALESCE(auto_refill_enabled,0)` 冗余(migration 039 期历史) · 不影响正确性
- bus.Strategy.DailyRoundLimit / DailySpendLimit 保留车级字段但 deprecated · CanPull 只读全局(canpull.go 无 BusDaily* 参数) · Effective 也只读全局

## §6 前端真实性

**verdict**: `p0_blocker` · 3 处 CLAUDE §0.1 铁律违反

### 26 页扫描

大部分页面 backend 支持到位 · **3 处触发 CLAUDE §0.1 铁律违反(全 P0)**:

1. Preferences 页缺全局护栏 3 字段 UI(P0-3)
2. BusDetail 成员挂起/解挂调 PUT 端点真后端不存在(P0-2)
3. Downstream 4 条推送策略里 3 条 toggle 是死控件(P0-4)

### 处理规范的 disabled 页(不构成 UI lie)

- `/dispatch` 阶段 3+ 空态占位 · 顶栏 badge '阶段 3 开放' · 明标不假装可用
- `AccountSettings` 社交登录 disabled + '即将上线'
- `Login` 忘记密码 static text + title tooltip
- `NotificationsBell` popover 内容明说"通知中心"占位 · 列 3 个入口链接不假装有通知

## §7 测试上线

**verdict**: `p0_blocker` · 根因是 refactor 遗留

### go test / vet

- **通过**: 37 个包
- **构建失败**: 2 个包(internal/api / internal/bus · 见 P0-1)
- **无测试**: 6 个包(cmd/bus-pooling · internal/housepool · providers/kiro/vendors/kiroappio · providers/kiro/vendors/kiroceo · internal/vendoraccount · internal/web · 见 P1)

### npm build

- **通过**: web 侧 vite build 全绿 · 主要 chunk: PublicControls 208KB / Legal 182KB / charts 411KB · 全部 gzip 后可控 · 无 error / warn

### 上线路线(sprint-1-final Stage 表)

| Stage | 状态 |
|---|---|
| Stage 0 code-complete CI | ⏸ pending · CI 门槛未过(P0-1) · 修 stale test 即可 |
| Stage 1 payment live | needs_creds · 代码就绪 · 缺 BP_GW_* 真值 |
| Stage 2 housepool live | needs_creds · 代码就绪(housepool/kirors 客户端有测试) · 缺 housepool.base_url + admin_key + expected_version |
| Stage 3 single vendor live | needs_creds · 6 家 vendor 全 code-complete · 缺 DRY_RUN=0 + BP_ALLOW_LIVE_PULL=1 + 单家 vendor api_key/webhook_secret · seed-vendor 命令待跑 |
| Stage 4 all vendors gradual | pending · 依赖 Stage 3 · 逐家灰度 · 手验 24h |
| Stage 5 auto-refill on | pending · 1d 代码就绪 · 单车灰度手验 |
| Stage 6 outbound webhook + k2a | pending · 1e code-complete · 依赖真号池 · P0-6 gate |
| Stage 7 tag + archive | wip · sprint-1a/1b/1c/1d/1e/1f-scope 已归档 · 剩 sprint-1-final.md 本身归档 + git tag |

## live 链路 smoke 附录

**位置**: `docs/1f-live-test.md`(2026-08-15 10:00 段)

### 5 大链路验证结果

| 步骤 | 状态 | 细节 |
|---|---|---|
| a. 注册 → 登录 → wallet → topup | ✅ pass | topup 105M CNY(含 5M 通道费)/100M 积分到账 · wallet_ledger 双条明细正确(recharge +105M / channel_fee -5M · 净 +100M · 符合 CLAUDE §1.4) |
| b. 建 bus + 配 passengerpool k2a | ✅ pass | passenger_downstream 落库 push_on_pull=1 · POST /test 打到真 k2a latency_ms=602 |
| c. 手动拉号(真扣款 · live) | ❌ fail | 6 家 vendor 全 409 no_stock(市场真实无货 · 非代码问题) · 契约层 OK |
| c'. 拉号 mock 补充(DryRunPool) | ✅ pass | bus pull 200 purchased=1 · balance 100M→68.5M(key 30M + svc 1.5M · 单价 31.5M) · pull_round 落 completed |
| d. 号进 housepool bus-<id> group | ❌ fail | live 拉不到 · mock 模式 DryRunPool 只在 BP 内存生成 · 不推真号池 |
| e-1. 派去向 · push_pool(k2a 双写) | ✅ pass | 真调 k2a(不是 DryRun) · k2a 拒 bad_request:refreshToken 已被截断 · 证明 pusher 网络路径 + k2a 校验层严格 + 归错分类正确 |
| e-2. 派去向 · handoff 明文一次性 | ❌ fail | GET /api/me/handoff/<token> 返 501 handoff_not_ready(housepool 明文导出端点未接) · 卡在 kiro.rs 侧 reveal 未实现 |

### 环境

smoke 用独立 :8091 + data/smoke-1f.db · 不动 dev 环境(8090+3100 保持运行) · 双锁 live 判定(`DRY_RUN=0 && BP_ALLOW_LIVE_PULL=1`)

### 附加发现

- **wallet_ledger balance_after 疑点(记一笔)**: mock 补跑第 3+4 条(key_cost / service_fee)balance_after 相同 · 应该分别为 70M / 68.5M · 似两条 delta 同事务用了最终值 · 不影响最终余额但按时序会打脸 · 不在本次 smoke 修范围

## 上线剩余清单

参见 `docs/archive/sprint-1-final.md` Stage 0-7 checklist。

**阶段 1 收官前必做**:

1. **修 P0-1**(stale test) → Stage 0 CI 全绿 → 允许 merge sprint-1f → main
2. **修 P0-2**(幽灵 PUT /members/{pid}) → BusDetail 挂起按钮生效
3. **修 P0-3**(Preferences 缺 3 字段 UI) → 全局护栏真暴露给用户
4. **修 P0-4**(Downstream 3 死 toggle) → UI 不再撒谎
5. **修 P0-5**(estimate 响应形状 doc) → 契约文档对齐

**live-ready 依赖(外部 gate)**:

6. **P0-6/P0-7 kiro.rs reveal endpoint** → Stage 2/6 灰度切 live
7. **Stage 3-4 vendor 补货窗口** → 单家 vendor 真扣款回归

**P1 上线前建议**:

8. **P1 全局 guardrail 决策路径 enforce**(3 桥补 daily_budget / min_wallet_reserve / vendor_allowlist)
9. **P1 主文档三份对齐 migration 040**(15-scheduling / 06-db-schema / 05-api-contract)
10. **P1 补 housepool / vendoraccount 单元测试**
11. **P1 补 migration 040 集成测试**

**P2 收尾清单**: 26 条 · sprint-1-final.md Stage 7 归档时可一次性做完。

---

## 附录 · 原始 JSON

原始 8 份结构化结果保留在会话上下文中,未落文件以免污染 git。追溯时参见任务对话历史。

