# 1f 明晨简报 · 睡前批处理落码收官

**收工时间**：2026-08-15 04:30 前 · sprint-1f 分支 · 7 commit 分层落好 · push 就绪

> 阶段 1 从 **code-complete** 走到 **feature-complete** —— 代码不再需要新增。剩下的 Stage 1-6 只需**填 env → 灰度切真链路 → 观察 24h**。上线路线看 [`docs/archive/sprint-1-final.md`](./archive/sprint-1-final.md)。

---

## 完成清单

### 1f-B · 车级策略 nullable + 全局默认三字段

后端(commit `6927f00` + `ac87de1`)
- migration `039_strategy_nullable_and_globals.sql` · bus 三字段改可空 · psd 加 `default_*` 三字段 · 保行为铁律(§4.3.2b)
- `internal/strategy/strategy.go` · Strategy struct 加 `Default*` · Get/Put SQL 拼装带上
- `internal/bus/bus.go` · `Strategy.AutoRefillEnabled` / `RefillWatermark` → `*bool` / `*int` · Create/Get/UpdateStrategy 走 nullable
- `internal/api/strategy.go` · strategyResponse 补三字段 · `buildStrategyResponse` 收敛装填
- `internal/api/bus.go` · busStrategyDT 三字段 → `*bool` / `*int`
- 单测 · migration 保行为 + 全局默认往返 + bus.Strategy nullable 三对入口

前端(commit `e8a0b0a` + `38fc7d2`)
- `web/src/types/index.ts` · BusStrategy 5 字段加 `| null` · GlobalStrategy 补 default_* 三字段 · JSDoc 说明两类字段语义
- `EditStrategyPanel.tsx` · 加 "跟随全局" toggle · 三态切换(off / on / 显式值)
- `Preferences.tsx` · 补全局默认三字段编辑区(defaults-auto section)
- `BusCard` / `BusFocalCard` / `BusMiniCard` / `BusDetail` · null 时显示"跟随全局(值=X)"
- i18n `settings.json` / `buses.json` · 中英对照落地
- MSW `fixtures.ts` · 三辆车覆盖三态(全覆盖 / 混合 / 全跟随)
- MSW `handlers.ts` · PUT strategy 真落 fx.buses · 接受 null 值

### 1f-C · `strategy.Effective()` 单入口 + 9 处接入

后端(commit `ac87de1`)
- `internal/strategy/effective.go` · `Effective()` + `BusStrategy` + `EffectiveDeps` + `SystemDefaults` + `RequestOverride` · 硬上限取 min · 覆盖字段后者盖前者
- `internal/strategy/effective_test.go` · 4 层 × 3 类字段回归 · 579 行
- `internal/bus/effective_adapter.go` · `ToStrategyBus` + `EffectiveBusGet` 搬运层
- `internal/bus/autorefill.go` · Scheduler.decideAndAct 只走 Decider · 移除"未装配 decider 走老路径"
- `internal/api/strategy_effective.go` · effectiveDeps 组合器 · `s.effective()` 单入口
- `internal/api/bus.go` handleBusPull + `internal/api/pull.go` handlePull · 全部走 `s.effective`
- `cmd/bus-pooling/effective_bridge.go` · autorefill Scheduler / deathwatch / stockwatch 三桥统一过 Effective
- `cmd/bus-pooling/main.go` · 装配 effective_bridge + SysDefaults 注入
- 删 `cmd/bus-pooling/pick_vendor_test.go` · 逻辑并入 Effective · 测试用 strategy_1f_test / effective_test 覆盖

### 1f-D · `docs/15-scheduling.md` 调度权威升级

文档(commit `eaf3c6a` · 单文件 +326 行)
- §0 整体导览 · 一句话定位 + 阅读顺序表
- §12 三条车路径统一调度模型 · 三类车 × 5 段 + 违反检查 + 当前 vs 目标口径
- §13 六触发源边界表 · manual/webhook/probe/deathwatch/scheduler/coalescer × 4 列 + 输入→Effective 解析路径
- §14 状态机 + 时序 · 完整拉号事务 + no_stock 恢复路径 + 三去向钱与号归属 + refund 反向路径

### 1f-E · `/docs` 页扩容

文档(commit `e547f42` · 3 文件 +678 行)
- `web/src/pages/Docs.tsx` · Section 加 "matrix" / "fields" 两 tab · MatrixSection endpoint 全表 · FieldsSection 全字段表
- API key 生命周期段(create / rotate / revoke / list)收在 matrix 顶部
- i18n `docs.json` · 术语对齐(handoff 不写"take out" · push-pool 说"dual-write")

### 主文档对齐 + sprint 归档(commit `90e9ad4`)

- `docs/03-modules.md` · strategy 段口径升级 + webhook 路径修正
- `docs/05-api-contract.md` · API Key 段加鉴权列 + §2.1/§2.2/§2.3 生命周期语义 · strategyResponse 补 default_* · busStrategyDT nullable
- `docs/06-db-schema.md` · bus 三字段改可空 + psd 补 default_* · 指向 §4.3.2b
- `docs/09-transactions.md` / `docs/12-frontend-pages.md` / `docs/14-deployment.md` / `docs/README.md` / `README.md` · 小对齐 + 阶段 1 状态改 feature-complete + archive 导航
- `docs/sprint-1{a,b,c,d,e,f-scope}-*.md` · `docs/sprint-1-final.md` → `docs/archive/`

---

## 数据

| 维度 | 数字 |
|---|---|
| commit 数(1f-A 之后) | **7 个**(feat × 4 + docs × 3) |
| 文件改动 | **53 个** |
| 新增文件 | **16 个** |
| 单测 | migration 039 保行为 + strategy.Effective 4 层 × 3 类 + strategy_1f 优先级 + bus.Strategy nullable 三对入口 · **共 7 份新单测** |
| 行数 | **+4119 / -667** |
| 敏感字扫 | 无残留(过 §8.1 检查) |
| 编译 | `go build ./...` 无 error(sprint-1f 分支 HEAD) |

---

## 明早你需要做的 · env 补齐清单

**上线路线看 [`docs/archive/sprint-1-final.md`](./archive/sprint-1-final.md)**。当前处于 **feature-complete**，Stage 1-6 只需填 env 变量 + 灰度观察。

env 全清单来源：[`docs/1f-audit.json`](./1f-audit.json) 里 `mocksAudit.env_vars_needed` 字段（40+ 项）。下面按 Stage 顺序拆：

### Stage 0 · code-complete 验收(不需要 env · 只跑 CI)

- [ ] 本地跑 `go build ./... && go vet ./... && go test -race ./...`
- [ ] 本地跑 `cd web && npm ci && npm run build && npm run lint`
- [ ] 敏感字扫:`grep -rEn 'sk-|usr-|1qazxsw|passwd|token=' <改动文件>`(§8.1)
- [ ] 内部术语 lint:webhook payload / API 错误 message 里不含 housepool / provider / decider 等(CLAUDE.md §0.1)

### Stage 1 · payment gateway 真链路(填 4 个)

| 变量 | 用途 | 从哪拿 |
|---|---|---|
| `BP_GW_BASE` | payment-gateway 基础 URL | waffo 后台配置 · Stage 1 先切一家 |
| `BP_GW_TOKEN` | waffo API token | waffo 后台 → 我方账号 → API tokens |
| `BP_GW_SETTLEMENT_SECRET` | 验签密钥 | waffo 后台 → webhook settlement 段 |
| `BP_GW_SUCCESS_URL` | 支付成功回跳 URL | 指向线上前端 `https://<domain>/wallet` |

### Stage 2 · housepool 真链路(填 3 个 · **已在 `.dev.env` 有值 · 复制到 prod env**)

| 变量 | 用途 | 从哪拿 |
|---|---|---|
| `BP_HOUSEPOOL_URL` | 我方号池 URL | `https://kiro.aibbq.xyz`(vps22 已部署) |
| `BP_HOUSEPOOL_ADMIN_KEY` | 号池管理员 key | vps22 `/opt/kiro-aibbq/data/config.json` → `adminApiKey` 字段 |
| `BP_HOUSEPOOL_EXPECTED_VERSION` | 校验号池版本一致 | 跟 vps22 kiro.rs 版本对齐(当前 `v0.5.x`) |

### Stage 3 · 单家 vendor 真链路(从 kiro91 起 · 填 4 个)

| 变量 | 用途 | 从哪拿 |
|---|---|---|
| `BP_ALLOW_LIVE_PULL=1` | 打开 live 拉号总开关 | 我方运维决定 · **默认关** |
| `DRY_RUN=false` | 关 dry-run 模式 | 我方运维决定 |
| `BP_DECIDER_DEFAULT_VENDOR=kiro91` | 默认 vendor 兜底 | 我方运维决定 · Stage 3 从一家起 |
| `BP_VENDOR_KIRO91_URL` | kiro91 API 基础 URL | vendor 官网 → 我方账号 → API 文档 |
| `BP_VENDOR_KIRO91_ENABLED=true` | 启用该 vendor | 我方运维决定 |
| `BP_VENDOR_KIRO91_API_KEY` | kiro91 API key | vendor 侧账号后台 · 一次性显示 |
| `BP_VENDOR_KIRO91_WEBHOOK_SECRET` | kiro91 webhook 验签密钥 | vendor 侧 webhook 配置 · 一次性显示 |

### Stage 4 · 其余 5 家 vendor 打开(每家 3-5 个 env · 逐家来)

**别六家一起开**。每家单独开 → 观察 stock / purchase / no_stock / webhook 24h → 再开下一家。

- `kiroceo` · 3 项(URL / ENABLED / API_KEY)
- `kirooo` · 3 项
- `kiroappio` · 3 项
- `kiroappcc` · 6 项(含 LOGIN_USER / LOGIN_PASS / WEBHOOK_SECRET)
- `kirodrop` · 5 项(含 SESSION_TOKEN / WEBHOOK_SECRET)

完整字段列表在 `docs/1f-audit.json` → `mocksAudit.env_vars_needed`。

### Stage 5 · 打开自动模式(不需要新 env · 已就位)

- 用户策略默认 `auto_refill_enabled=false`(§4.1 定 · UI 出厂关)
- 单车灰度打开 · 观察 bus.Scheduler / deathwatch.RefillTick / stockwatch / janitor 30 分钟
- 无异常再放开更多车

### Stage 6 · 计费费率 + 通用配置(填 5-6 个)

| 变量 | 用途 | 从哪拿 |
|---|---|---|
| `BP_RATE_SERVICE_BP` | 服务费率(basis point · 1/10000) | 后台配置 · **不写进代码**(CLAUDE.md §1.3) |
| `BP_RATE_VENDOR_BP` | vendor 附加费率 | 后台配置 |
| `BP_RATE_REGION_BP` | 区域附加费率 | 后台配置 |
| `BP_RATE_SINGLE_PULL_BP` | 单次议价费率 | 后台配置 |
| `BP_RATE_CAPABILITY_BP` | 附加能力费率(阶段 1 无实例 · 填 0) | 后台配置 |
| `BP_MASTER_KEY` | AES-GCM 主密钥 · handoff 明文加密 | 生成 32 字节随机值 · 保管好 · 丢了老号解不出 |
| `BP_ADMIN_KEY` | admin API 鉴权 | 生成随机 32+ 位 · 只给运维用 |
| `BP_ALERT_WEBHOOK` | 告警推送 URL | 我方 IM 群 webhook |
| `BP_XI8_API_KEY` | xi8 汇率服务 key(可选) | xi8 官网 |

---

## 已知局限 · 上线前需要外部依赖

### 1. passengerpool 双写走 placeholder

- 位置：`internal/delivery/passengerpool/pusher.go:288-310`(`fetchPlaintext` + `placeholderPlaintext`)
- 触发：`BP_ALLOW_PASSENGERPOOL_PLACEHOLDER=1` 或 `deps.Plaintext==nil` 时
- 现状：每号发 `PLACEHOLDER:not-a-real-token:<id>` 三字段 · 不真推真明文
- **切 live 前提**：housepool 后端开放 reveal endpoint(`housepool.HousePool.GetCredentialPlaintext` 现在整个仓库没实现)· 装配层传真 `PlaintextLookup`
- **今晚补不了**：上游未提供接口 · 需 kiro.rs 侧先加

### 2. handoff 明文暴露

- 位置：`internal/api/handoff.go:138-150 + 166-182`(`readHandoffPlaceholder`)
- 三态：
  - 默认 501(未开)
  - `BP_ALLOW_HANDOFF_PLACEHOLDER=1` 返 `PLACEHOLDER:not-a-real-key:<id>` 走 `placeholder_delivered` → `confirmed_placeholder`(号不删)
  - `BP_HANDOFF_TRUE_PLAINTEXT=1` 才走真 `DELETE`
- 现状：`readHandoffPlaintext(handoff.go:282)` 目前 hardcode 返 `errHandoffPlaintextUnavailable`
- **切 live 前提**：housepool 后端 reveal endpoint 定义 · 上游未开放前无法切

### 3. vendor webhook secrets 未配

- 每家 vendor 的 `BP_VENDOR_*_WEBHOOK_SECRET` 未配 · vendor 侧先配好后填
- 未配时:webhook 收到会走"验签失败" · 落 `vendor_webhook_delivery_failed` 台账 · 不影响其他路径

### 4. 前端 mock 依赖 MSW

- `web/src/mocks/*` 是 development-only · prod build 不带
- `import.meta.env.VITE_USE_MSW=true` 才开 MSW · prod 走真后端

---

## 下一步

1. **明早**：读本文 → 决定灰度哪家 vendor / 哪几辆车 → 填 Stage 1-3 env → 跑 [`scripts/smoke-1f.sh`](../scripts/smoke-1f.sh) 冒烟
2. **feature-complete → Stage 1-6 逐阶段切**：见 [`docs/archive/sprint-1-final.md`](./archive/sprint-1-final.md)
3. **卡壳**：读 [`docs/1f-audit.json`](./1f-audit.json) 找具体 gap · 配套的 [`docs/1f-live-test.md`](./1f-live-test.md) 有 SSH / env 状态清单

---

**Push 命令**：`git push origin sprint-1f`（在最后一个 commit 落好之后运行；本文即是最后一个 commit 的内容）。
