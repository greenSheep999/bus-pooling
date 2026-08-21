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
| [I-03](#i-03) | 🟡 open | P1 | kirodrop 新增 personal 号未接入（vendor.AccountKinds 未声明） | 2026-08-22 |
| [I-04](#i-04) | 🟡 open | P1 | 5 家 vendor 都只声明 enterprise（只 kirooo 双档接完） | 2026-08-22 |
| [I-05](#i-05) | 🟢 fixed(unverified) | P1 | 主文档 3 份滞后 migration 040（15-scheduling / 06-db / 05-api / 03-modules） | 2026-08-15 |
| [I-06](#i-06) | 🟢 fixed(unverified) | P2 | 建车 Advanced 仍含已废弃的 daily_round_limit / daily_spend_limit | 2026-08-15 |
| [I-07](#i-07) | 🟢 fixed(unverified) | P2 | handoff init 幂等契约不一致（前端送 idempotency key · 后端忽略） | 2026-08-15 |
| [I-08](#i-08) | ✅ verified | P2 | 05-api-contract 列了未实现端点（/me/buses/{id}/stats 等 3 个） | 2026-08-15 |
| [I-09](#i-09) | 🟡 open | P2 | housepool / vendoraccount / kiroappio / kiroceo 无单元测试 | 2026-08-15 |
| [I-10](#i-10) | 🟡 open | P2 | migration 040 缺集成测试 | 2026-08-15 |
| [I-11](#i-11) | 🟡 open | P2 | 缺 stage-1..6 分级 smoke 脚本 | 2026-08-15 |
| [I-12](#i-12) | 🟡 open | P3 | 主文档 P2 drift（26 条 · 06-db 漏收 9 张新表 / 依赖图漏连线 etc） | 2026-08-15 |

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

**状态**：🟡 `open`
**发现**：2026-08-22（用户提及 · pro_max 5000 单位 134.98 CNY）
**症状**：kirodrop vendor 上游新增了 personal 号池 · `internal/providers/kiro/vendors/kirodrop/adapter.go` 的 `Capability()` 未声明 `AccountKinds` → 默认只认 enterprise → **系统看不到 personal 号**。

**修法**（照 kirooo 那套抄）：
1. `adapter.go · Capability()` 加 `AccountKinds: [enterprise, personal]`
2. 建 `personal.go` · 存 personal 池的 stock / purchase 端点
3. `Stock() / Purchase()` 判 `opts.Kind == Personal` → 转发 personal.go
4. `docs/vendors/drop-kiro-ss.md` §2.3b 补 personal 池文档
5. `vendor_pricing` 表看是否要新 row（personal 单价独立）
6. `personal_test.go`

**前置**：需要 kirodrop personal 池的 API 端点契约（Playwright 探或从 vendor 拿文档）· 现有 vendor 档案里 0 处提 personal。

---

### I-04 · 5 家 vendor 只声明 enterprise · 只 kirooo 双档接完

**状态**：🟡 `open`
**发现**：2026-08-22
**症状**：现在 6 家 vendor 里 **只有 kirooo** 走完双档接入。**91kiro / kiroceo / kiroappio / kiroappcc / kirodrop** 都还只有 enterprise。

**影响**：如果上游那 5 家其中任何一家开了 personal 号池 · 我方系统都看不到 · 需要按 I-03 每家单独接。

**修法**：每家 vendor 单独接（不是共通改造 · vendor API 各家不同）· 见 I-03 步骤。

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

### I-06 · 建车 Advanced 仍含已废弃的 daily_round_limit / daily_spend_limit

**状态**：🟡 `open`
**发现**：2026-08-15（phase-1-acceptance §P1）
**症状**：`docs/15-scheduling §4.1` 明标车级已废弃 · 但 DB / API / TS / 前端建车表单 Advanced 段都还保留。

**修法**：前端建车表单拿掉两字段（`StartCarpoolModal.tsx L84-85`）· 后端 DTO 加 deprecated 注释。

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

**状态**：🟡 `open`
**发现**：2026-08-15
**症状**：
- `internal/housepool` 只有 kirors 子包有测试 · 主包无
- `internal/vendoraccount` 无测试（vendor api_key/webhook_secret 明文密态存储关键路径）
- `internal/providers/kiro/vendors/kiroappio` 无测试
- `internal/providers/kiro/vendors/kiroceo` 无测试

**修法**：`vendoraccount` 补 encrypt/decrypt round-trip · `kiroappio/kiroceo` 补 stock/purchase/webhook 归一化三条测试 · `housepool` 补包级 unit test。

---

### I-10 · migration 040 缺集成测试

**状态**：🟡 `open`
**发现**：2026-08-15
**症状**：migration 039 有 `TestMigration039_PreservesBusAutoRefillValues` · migration 040 无对应测试。

**修法**：新建 `internal/db/migrations_040_test.go` · 复用 039 骨架 · 验"从 nullable 撤回 NOT NULL 时车级值不丢"。

---

### I-11 · 缺 stage-1..6 分级 smoke 脚本

**状态**：🟡 `open`
**发现**：2026-08-15
**症状**：只有 `smoke-1f.sh` 一份 · 每档切换后靠人肉验。

**修法**：派生 `scripts/smoke-stage1-payment.sh` / `smoke-stage2-housepool.sh` / `smoke-stage3-vendor.sh` 三份。

---

### I-12 · 主文档 P2 drift（26 条）

**状态**：🟡 `open`
**发现**：2026-08-15
**摘要**：
- `06-db-schema` 漏收 9 张 1b~1e 新表（stock_watcher / vendor_ledger 等）+ 若干字段
- `03-modules` 依赖图漏 stockwatch / vendorbalance / pricing / xi8 / vendorview 连线
- `01-architecture §5` 目录树缺 5 个已存在的业务包
- 详见 `docs/phase-1-acceptance.md §P2 Cleanup` 全表

**影响**：不阻运行时 · 只误导下一个 agent 建代码时按老 schema 造字段。

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
