# 阶段 1 验收复检 · v2(6 P0 修完后)

**时间**: 2026-08-15
**基线**: sprint-1f · HEAD=`29174b07a356fa7d383f231cfb51c80fa4f32d97`
**上一份**: `docs/phase-1-acceptance.md`(6 P0 issue 就是从那份出来的)

---

## 一句话结论

**✅ 允许 merge `sprint-1f` → `main` · 阶段 1 feature-complete achieved**

判据：4 层验证全绿(CI 全套 + 6 P0 逐条复查 + 3 处前后端契约 + Effective 单入口/手动豁免 grep)· 0 failure · warning 仅 4 条注释级瑕疵(不影响运行时 / 不影响契约 / 不影响决策路径)。

---

## 6 P0 状态

| # | 项 | 修前 | 修后 | 证据 |
|---|---|---|---|---|
| P0-1 | stale test(nullable → *T) | FAIL(比较 bool/int 值类型) | **PASS** | `strategy.go:40-43` 三字段全指针 · `bus/strategy_nullable_test.go` + `api/strategy_1f_test.go` 里 `!= nil / == nil` 都是对 `*int` / `*int64` 指针比较 · `go test` ok |
| P0-2 | ghost route(`PUT /me/buses/{id}/members/{pid}`) | 前端调后端 404 | **PASS** | handler `bus.go:351 SetMemberSuspended` · 三重 guard(self/owner/status) · 路由 `server.go:228` 注册 · live curl 401(路由存在) |
| P0-3 | Preferences 三字段 UI wire | 后端有字段前端不发 | **PASS** | `Preferences.tsx L275-337` 三输入 · `L80-82` 进 payload · `L92 onClick={onSave}` |
| P0-4 | Downstream 死开关(3 个) | 灰态按钮无实体 | **PASS** | grep 命中 2 条均在文件头注释(L21-22 · 记录撤了什么) · `RULES` 数组只剩 `bus_only` 1 条(L26-32) |
| P0-5 | `/me/pull/estimate` 文档脱敏 | 曾暴露 key_cost/single_pull_fee | **PASS** | `05-api-contract.md L817` estimate 条目只返 `unit_price / service_fee / total` · 引 `CLAUDE §0.1` |
| P1-1(顺手)| 三护栏在自动桥里活了 | 后端字段有前端不发也不查 | **PASS** | `main.go:1779 autoRefillGuardrailsDeny` · 3 处调用全在 refill/scheduler/webhook 三桥(L1169/L1343/L1556) · vendor 白名单 + wallet 保护线 + daily budget 三 deny 分支齐 |

---

## 4 层验证

### 1. CI 全套 · PASS

- `go build`: clean
- `go vet`: clean
- `go test`: **37/37 packages ok · 0 FAIL**(log: `/tmp/gotest.log`)
- `npm run build`: clean · `built in 1.55s`
- `cd web && npx tsc --noEmit`: clean

### 2. P0 逐条复查 · 6/6 PASS

见上表 · 每条附 file:line 证据。摘要：

- **P0-1** · 三字段 `DefaultRefillMinCount / AutoRefillDailyBudget / AutoRefillMinWalletReserve` 撤 nullable 后确认全指针型(`strategy.go:40-43`) · 相关测试(`bus/strategy_nullable_test.go` · `api/strategy_1f_test.go`)里的 `!= nil` 全是指针 nil check 语义正确 · `internal/bus` 6.236s · `internal/api` 41.321s 均绿。
- **P0-2** · `Store.SetMemberSuspended` 在 `bus.go:351` 且带 self-check(L352) / owner-check(L359) / status-check(L362) 三 guard · 路由 `mux.Handle("PUT /api/me/buses/{bus_id}/members/{pid}", handler(s.RequireAuth(s.handleSetMemberSuspended)))` 在 `server.go:228` · fresh 构建 `/tmp/bp-verify` 起 :8091 curl 未带 auth 得 401 `{code:unauthenticated}`(路由存在但需授权 · 符合预期) · 错 method POST 得 404(仅 PUT · 正确)。
- **P0-3** · `Preferences.tsx L275-337` 三输入齐(`daily-budget` L280 · `min-reserve` L290 · `allowlist` L304) · save payload `L80-82` 三字段都进 · `L92 onClick={onSave}` 挂上按钮。
- **P0-4** · grep 命中 2 处均在文件头注释(`Downstream.tsx L21-22` · 说明撤了什么 · 不是活代码)· `RULES` 数组 L26-32 只剩 `{ key: 'bus_only', ... }` 一条。
- **P0-5** · `05-api-contract.md L817` estimate 条目返回体已收敛到 `{ unit_price, service_fee, total }` · 引用 `CLAUDE §0.1`。
- **P1-1** · `autoRefillGuardrailsDeny` 定义在 `main.go:1779` · vendor 白名单(L1787-1799 · `deny='vendor_not_in_allowlist'`) · 钱包保护线(L1801-1809 · `deny='wallet_below_reserve'`) · 每日预算(L1811-1824 · `SUM(-amount) FROM wallet_ledger WHERE type='spend' AND today` · `deny='daily_budget_reached'`) 三分支齐 · 3 处调用全在 refill/scheduler/webhook 三桥(L1169/L1343/L1556)。

### 3. 前后端契约(3 处对齐) · PASS

- **BusStrategy 撤 nullable**：`bus/bus.go:71-76`(domain `bool` + `int`) → `api/bus.go:41-50`(DTO `bool` + `int`) → `web/src/types/index.ts:77-94`(TS `boolean` + `number`)三层一致。
- **三护栏字段**：`api/strategy.go:31-46, 82-96` 返 `*int64 / *int64 / []string`(nil 归一为 `[]`) · MSW fixtures `mocks/fixtures.ts:820-842` seed 有值 · MSW handler `mocks/handlers.ts:252-262` PUT 接住三字段 · 前端 `types/index.ts:590-625` 类型齐。
- **成员挂起路由**：`api/bus.go:700-720` 后端 body `{ suspended: bool }` · 前端 `hooks.ts:437-444` `put(/me/buses/${busId}/members/${memberId}, { suspended })` · MSW `handlers.ts:87-97` 接住 · `server.go:228` mux 注册。

Warning 2 条(纯注释瑕疵)：
1. `strategy.go:80` 注释说"nil slice → null"但代码把 nil 归一成 `[]` · 前端类型 `string[] | null` 都能接 · 不影响契约。
2. `handlers.ts:75-77` MSW PUT `/me/buses/{id}/strategy` 仍接旧字段 `daily_round_limit / daily_spend_limit`(前端类型已标"废弃(只读)"· 后端 DTO 仍暴露)· mock 兼容旧行为 · 不算 drift。

### 4. grep 单入口 + 手动豁免 · PASS

- **`strategy.Effective` 是唯一决策入口**：`decider / deathwatch / stockwatch` 三个决策子系统的活代码里对 `cand.AutoRefill*` / `cand.RefillWatermark` 的引用为 0 · `bus/autorefill.go` 的 `loadCandidates` SQL 读原始车级字段是为了填 `SchedulerCandidate` 快照 · 三桥(`refillDecideBridge` L1149+ · `schedulerDecideBridge` L1333-1418 · `webhookAutoScanBridge` L1536+)拿到候选后**立即调 `strategy.Effective()` 用 `eff.*` 覆盖**再喂 `decider.Decide` · gap 计算显式用 `eff.RefillWatermark` 且注释警告不用 `cand.Watermark`。
- **Guardrail 手动路径豁免**：`grep 'autoRefillGuardrailsDeny\|Guardrail' internal/api/pull.go internal/api/bus.go` 空输出 · `handlePull` 和 `handleBusPull` 手动路径均无引用 · 3 处调用全在自动桥 · 符合 `docs/15-scheduling.md §4.3.4`(手动路径不受 guardrail 拦)。

Warning 1 条：后续新增 auto-trigger 入口时需再验一次是否也调了 `Effective` —— 这是约定层保护 · 建议 code review checklist 加一条。

---

## 剩余 P1/P2(不阻 merge)

引 `docs/phase-1-acceptance.md` 里的 P1/P2 清单。摘要：

- (P1) MSW `handlers.ts:75-77` 还接旧字段 `daily_round_limit / daily_spend_limit` · 后端 DTO 也还暴露 · 建议下个 sprint 一起清。
- (P1) `strategy.go:80` 注释与行为不一致(nil → `[]` 但注释写 null) · 改注释即可。
- (P2) 新增 auto-trigger 入口的 code review checklist：必须调 `strategy.Effective()`。
- 其余见 `phase-1-acceptance.md`。

---

## 外部 gate(阻 live · 不阻 merge)

- **P0-6** · `kiro.rs` reveal endpoint(handoff 明文吐号)—— 未上线 · handoff 流程当前依赖此端点存在。
- **P0-7** · handoff 明文一次性投递路径 —— 依赖 P0-6。
- **vendor 缺货** · 6 家 vendor 的账号库存(阶段 1 起量前需真实号池 seed)。

以上均为**外部依赖 / 数据准备**类 gate · 与代码库无关 · 不阻 merge。

---

## 建议下一步

1. **merge `sprint-1f` → `main`**(4 层全绿 · 6 P0 全 pass)
2. **tag `v1-alpha`** 冻结阶段 1 feature 面
3. **等 kiro.rs reveal endpoint 上线** → 打通 handoff 明文路径
4. **vendor 侧号池 seed** → 起量前小批灰度
5. 下个 sprint 清 P1/P2 尾巴(MSW 旧字段 · 注释瑕疵 · code review checklist)

---

## 附：运行时警示(操作层)

- 当前运行中的后端二进制 PID 96908 · 09:49AM 构建 · **早于 P0 修复 commit `29174b0`** · merge 后需重启才拿到 P0-2 的新路由。验证时用的是 fresh 构建 `/tmp/bp-verify`(端口 :8091) · 不影响本次验收结论。
- P0-5 提到 `GET /buses/{id}/pulls`(拉号历史)L542-544 仍有 `key_cost_total / single_pull_fee_total` · 属另一端点(不是 estimate) · 不违反 `CLAUDE §0.1` 收口(§0.1 只约束 estimate/pricing 类) · 但值得回头看是否要脱敏 · 记入下 sprint P1。
