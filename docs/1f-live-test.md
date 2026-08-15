# 1f live 链路测试报告

## 2026-08-15 10:00 · 阶段 1 验收 smoke

> 本次由 sprint-1f 分支 · smoke-1f.sh 驱动 · 覆盖 5 大链路（注册-充值 / 建车-配 k2a / 拉号 / housepool 落库 / 派去向）· 目标是把「live 链路」跑通并落台账。
> 老报告（本文下半 · 6:22 分位）保留 · 不删。

### TL;DR

| 结论 | 状态 |
|---|---|
| a. 注册 → 登录 → wallet → topup 充值 | ✅ 全通 · 走 dev-topup mock 路径（`BP_ENABLE_DEV_TOPUP=1`）· 105 CNY→100M 积分 + 5M 通道费落 `wallet_ledger` 两条明细 · CLAUDE §1.4 口径对上 |
| b. 建 bus + 配 passengerpool k2a | ✅ 建 `single` bus 成功 · PUT `/api/me/downstream/passengerpool` 落库 · POST `.../test` 打到真 k2a `latency_ms=602` 返 `ok:true` |
| c. 手动拉号（真扣款 · 走 91kiro / kiroceo） | ⚠️ live 模式下 6 家 vendor 全部 `stock_bucket=out` · 返 `409 no_stock` · **符合当前市场真实状态**（vendor 侧无货）· 拉号链路契约 OK · 但没能验到真扣款 |
| d. 号进 housepool `bus-<id>` group（SELECT + curl 双验） | ⚠️ live 无 stock 拉不到 · 走 mock（DryRun）时 credential_ledger 落库正确（含 `owner_bus_id` / `current_group=bus-<id>` / `kiro_rs_credential_id` / `status=alive`）· 但 mock 不推真 housepool · **housepool 侧无法在 live 模式下验证** |
| e. 派去向 · 进车 / 推 k2a 双写 / handoff 明文一次性 | ⚠️ 部分通 · `push_pool` 真调 k2a 且落 `pending_assignment.status=need_manual` + `credential_ledger.push_error_code=bad_request`（k2a 拒 mock 假 refreshToken 长度 65）· `handoff` 三段式：`POST /me/handoff` 返 token+expires ✅ · `GET /me/handoff/<token>` 返 **501 `handoff_not_ready`**（"housepool 明文导出端点未接"）· `confirm` 返 409 · **handoff 明文交付整条链目前顶到 kiro.rs 侧未实现** |

**关键定性**：应用侧代码路径全通（授权 / 定价 / 落台账 / 双写 pusher 装配 / 三段式 handoff 状态机 / 幂等）· 卡点在两处**外部依赖**：
- **vendor 无 stock**：6 家均 `out` · 只能等或换真订单窗口再验真扣款
- **housepool 未提供明文导出端点**：handoff download 阶段返 501（这是本项目一直认得的 gap · 阶段 1 明文一次性通道靠此收尾）

---

### env / 前置

- 后端旧 dev 服务：`http://127.0.0.1:8090` · `DRY_RUN=1` · 保留未动
- 前端 vite：`http://127.0.0.1:3100`
- smoke 用**独立端口 8091 + 独立 DB `data/smoke-1f.db`**（脚本默认 · 不污染 dev DB）
- 系统池 housepool：`https://kiro.aibbq.xyz`（vps22）· `BP_HOUSEPOOL_ADMIN_KEY` 在 `.dev.env`
- 下游 k2a：`https://k2a.muxpay.xyz`（vps196）· `K2A_ADMIN_KEY` 从 `/tmp/k2a.env`（`chmod 600` · 未 commit · 未落 `.dev.env`）
- 6 家 vendor api key：已在 `.dev.env`

**第一次跑发现**：smoke-1f.sh 只 `export DRY_RUN=0` · 但 `cmd/bus-pooling/main.go:354` 需要**双锁**才 live：
```go
live := !cfg.DryRun && os.Getenv("BP_ALLOW_LIVE_PULL") == "1"
```
没设第二把锁 → 走 `DryRunPool + DryRunVendor` · vendor 返 fake `ksk_...` 号 · housepool 是内存 stub。第二次跑加了 `BP_ALLOW_LIVE_PULL=1` 才真走 kirors client → live vendor + live kiro.aibbq.xyz。

---

### 逐步验收（live 模式 · `BP_ALLOW_LIVE_PULL=1` · DRY_RUN=0）

#### step 1 · 起服务

- 端口：`:8091` · DB：`data/smoke-1f.db` · **迁移到 v40**（`strategy_split_layers`）· 迁移耗时 <1s
- 装配日志确认：`"拉号走 LIVE 链路 · 会产生真扣款" enabled_vendors="[kiroappcc kirodrop kiro91 kiroceo kirooo kiroappio]" default=kiro91`
- healthz 2s 内通
- **`file:line` 主线索**：`cmd/bus-pooling/main.go:354`（live 判定）· `internal/api/server.go:82`（pusher 装配位）

#### step 2-4 · 注册 + session + 空 wallet · ✅

```
POST /api/register → 201 · {"id":"267395b0-...", "tier":"retail", "invited":false, ...}
GET  /api/me       → 200 · session cookie 好使
GET  /api/me/wallet → 200 · {"balance":0, ...}
```

#### step 5-7 · topup + mock paid · ✅

- `GET /api/topup/channels` → 200 · 返 `waffo` + `bank01`（2 家 channel）
- `POST /api/me/topup` 100000000 积分 · 返 `order_id` + `paid:105000000`（含 5% 通道费）· `credits:100000000`
- `POST /api/internal/topup/<order>/paid`（`BP_ENABLE_DEV_TOPUP=1` mock）→ 200 · `status:paid`
- `GET /api/me/wallet` → `balance:100000000`
- **`wallet_ledger` 双条**（CLAUDE §1.4 口径）：
  - seq 1 `recharge +105000000` balance_after=105M
  - seq 2 `channel_fee -5000000` balance_after=100M（净 +100M）

#### step 8 · 建 bus · ✅

```
POST /api/me/buses → 201
{"id":"8d7539b5-...", "name":"smoke-1f-bus", "kind":"single", "status":"active",
 "member_count":1, "invite_code":"QY6UT4ZT",
 "members":[{"role":"owner","share_pct":100,"balance":100000000, ...}],
 "strategy":{"auto_refill_enabled":false, ...}, "alive_count":0, "dead_count":0}
```

#### step 9-10 · 配 downstream passengerpool → k2a · ✅

```
PUT /api/me/downstream/passengerpool → 200 · {"ok":true}
POST .../test                        → 200 · {"ok":true, "latency_ms":611}    ← 真打到 k2a · 网络通
```

DB 落 `passenger_downstream` 一行 · `push_on_pull=1 retry_on_failure=1 bus_only=0`。

#### step 11 · 触发拉号 · ❌（预期外 · vendor 无货）

对 6 家 vendor 各起一次拉号：
```
POST /api/me/buses/<id>/pull vendor=kiro91    → 409 {"code":"no_stock", ...}
                              kiroceo    → 409 no_stock
                              kirooo     → 409 no_stock
                              kiroappio  → 409 no_stock
                              kiroappcc  → 409 no_stock
                              kirodrop   → 409 no_stock
```

侧证：
- `GET /api/vendors/stock` → 6 家 `available=0` · `total_available=0`
- `GET /api/vendors/status` → 每家 `stock_bucket:"out"` · `dispatch.last_dispatch_at` 都在几天前

**结论**：拉号契约 OK（幂等 header 认 · 参数校验通过 · 到 decider）· vendor 侧真的没货。**这是本次 smoke 的最大遗憾——真扣款一步验不到**。
钱包不动（`{"balance":100000000, ...}`）· 幂等表 7 条（覆盖 register / topup / 6 次拉号失败）· `pull_round` 表 0 行 · `credential_ledger` 0 行 · 无侧作用。

**`file:line`**：`internal/decider/orchestrator.go`（no_stock 判定）· `internal/api/pullrecord.go:437`（pusher 挂钩位 · 本次未进入）

---

### 补充跑法（mock 模式 · 验代码路径完整）

因为 live 拉不到号 · 又用 `DRY_RUN=1`（DryRun stub）重跑一次拉号 + 派去向 · 目的**只验代码路径**（不代表真链路能扣款）：

| 步骤 | 结果 |
|---|---|
| `POST /api/me/buses/<id>/pull kiro91 count=1` | ✅ 200 · `purchased:1` · `unit_price:31500000` · `service_fee:1500000` · `balance_remaining:68500000` |
| wallet 扣款 | ✅ 100M → 68.5M（30M key_cost + 1.5M service_fee 两条 `wallet_ledger` · ref_type=`pull_round`）|
| `credential_ledger` 落 | ✅ `current_group=bus-<busid>` · `kiro_rs_credential_id=1786759682765351001`（UnixNano · DryRunPool 标记）· `status=alive` · `disabled=0` |
| `pull_round` 落 | ✅ `count_purchased:1` · `key_cost_total:30000000` · `service_fee_total:1500000` · `status:completed` |
| 再拉一次（record 组 · `POST /api/me/pull`） | ✅ 200 · `credential_id=a45c0459-...` · `current_group=record-<pid>` |
| **`POST /api/me/pull-records/assign destination:push_pool`** | ✅ 200 · **真调 k2a**（不是 DryRun）· k2a 拒 `bad_request: refreshToken 已被截断（长度: 65 字符）` · 因为 DryRun 假 token 结构不合法 · **证明 pusher 装配 + 网络路径正确 · 校验层严格** |
| `credential_ledger.push_error_code` 落 | ✅ `bad_request` · `push_attempts=1` |
| `pending_assignment` 落 | ✅ `target=to-passengerpool` · `status=need_manual` · `error=passengerpool_push_failed` |
| `POST /api/me/handoff` | ✅ 200 · 返 `download_token` + `expires_at`（5min TTL） · `pending_handoff.status=token_issued` |
| `GET /api/me/handoff/<token>` | ❌ **501 `handoff_not_ready`** · message: "取号功能未开放（housepool 明文导出端点未接）· 号仍在你的池里，可以派进车或推自己号池" |
| `POST /api/me/handoff/<token>/confirm` | 409 `conflict` · "还没取过明文，不能确认"（正确的状态机拒接） |

**关键 file:line**：
- `internal/api/pullrecord.go:422-470` · assign push_pool 分支 · 调 `s.pusher.Push` · 拆 pushResult 落 push_error
- `internal/api/handoff.go` · 三段式 · **下载阶段依赖 housepool `Reveal()` 未实现**
- `internal/delivery/passengerpool/pusher.go:75` · realPusher.Push 主流程

---

### 关键数字（mock 补充跑）

```
passenger_id = 86bb6285-c92f-403d-ba59-c039dddcd43c
bus_id       = e35b0003-84de-48e3-80bf-2a8bbeaa461b
credential(bus)    = 7023e3f8-...cca3800   group=bus-<id>       kiro_rs_id=1786759682765351001
credential(record) = a45c0459-...4a0e4135   group=record-<pid>   kiro_rs_id=1786759682765351002
housepool 侧（kiro.aibbq.xyz）真实凭据数 = 0   ← DryRun 只在 BP 内存生成 · 不推真号池
k2a 侧凭据 total（baseline=8 · smoke 后仍=8）  ← push_pool 被拒 · 未落 k2a
wallet 净流：+100M(recharge) -5M(channel) -30M(key) -1.5M(svc) -30M(key) -1.5M(svc) = 32M（顺序对 · 组合 balance_after 有轻微异常见下）
```

**发现的可疑但非阻塞**：`wallet_ledger` 里 `key_cost` 和 `service_fee` 两条 `balance_after` 相同（68500000 / 37000000）· 应该是 key_cost 先扣 balance_after=70000000 · 再 service_fee 扣到 68500000。似乎两条 delta 一起写但 balance_after 用了最终值。**不影响最终余额** · 但账本查询按 `balance_after` 时序会打脸。建议对齐后再排查（不在本次 smoke 范围内，但记一笔）。

---

### smoke-1f.sh 自身的两个小 bug（不影响验收）

1. **`credential_ledger` 查询列名错**（line 315）：脚本 `WHERE passenger_id='$PID'` · 实际字段是 `owner_bus_id` / `owner_record_passenger_id`。所以 `LEDGER_COUNT=0` 是误报 · 号其实进了。
2. **shell 变量名带特殊字符**（line 337 附近）：`K2A_TOTAL_BEFORE_UNKNOWN` 后面有个 `?` 让 `set -u` 崩 · `CURL_STATUS�: unbound variable` 也是同类症状。跑到步骤 13 之后就 exit 1。

铁律「别改代码 · 只测」· 本次不改 · 记一笔就好。

---

### 结论

阶段 1 应用侧代码路径已经跑通 · smoke 覆盖：
- 会话 · 幂等 · 定价链 · 双写 pusher 网络 · 三段式 handoff 状态机 · vendor 探活 · 全部有效

未验证到的两件事，卡点在**外部**：
- 真扣款 · 需要至少一家 vendor 有货
- handoff 明文交付 · 需要 kiro.rs housepool 加 reveal 端点

前后端两侧的运行时状态未变（8090 / 3100 未受打扰）· smoke 端口 8091 已释放 · smoke DB `data/smoke-1f.db` 保留（供事后 sqlite3 追查）· K2A_ADMIN_KEY 仅存于 `/tmp/k2a.env`（chmod 600 · 未泄漏进 log / commit / md）。

---


**时间**：2026-08-15 · sprint-1f 分支 · 主 workflow phase 2-8 正在跑（bus/strategy 重构中）

## SSH 状态

| VPS | 用途 | 状态 |
|---|---|---|
| vps22 (`v2202607366793478602`) | 我方 housepool · `kiro.aibbq.xyz` | ✅ 通 |
| vps196 (`v2202606366793468914`) | 测试用 passengerpool · `k2a.muxpay.xyz` | ✅ 通 |

## env 补齐状态

| 变量 | 状态 | 位置 |
|---|---|---|
| `BP_HOUSEPOOL_URL` | ✅ 已加 | `.dev.env`（`https://kiro.aibbq.xyz`） |
| `BP_HOUSEPOOL_ADMIN_KEY` | ✅ 已加 | `.dev.env`（vps22 `/opt/kiro-aibbq/data/config.json` 里 `adminApiKey`） |
| `BP_DECIDER_DEFAULT_VENDOR` | ✅ 已加 | `.dev.env`（`kiro91` · config.yaml 里为空） |
| `K2A_ADMIN_KEY` | ⚠️ 不进 .dev.env | 环境变量传给 smoke 脚本 · 来自 vps196 `docker exec kiro-rs cat /app/config/config.json` |

**已直接验证明文 admin key 可用**：
```
curl -H "x-api-key: <housepool_admin_key>" https://kiro.aibbq.xyz/api/admin/credentials → HTTP 200
curl -H "x-api-key: <k2a_admin_key>" https://k2a.muxpay.xyz/api/admin/credentials → HTTP 200 · total=7
```

`.dev.env` 已 `chmod 600`。

## smoke test 结果

**中间态**：**部分链路通** · 走完到 pull 前失败·主 workflow 破坏了 build 未跑到底。

### 已验证通的项（在 build 还好的窗口内跑过一遍）

| 步骤 | 结果 | 备注 |
|---|---|---|
| 迁移新 DB（`migrate up`） | ✅ | 001_init 到最新一路应用 |
| `go run ./cmd/bus-pooling serve` 起服务 | ✅ | 4s 内 `/healthz` 通 · live 模式（`DRY_RUN=0`） |
| 6 家 vendor 注册 | ✅ | kiro91 / kiroceo / kirooo / kiroappio / kiroappcc / kirodrop 全 `enabled=true` |
| `POST /api/register` | ✅ | HTTP 201 · session cookie 落 |
| `GET /api/me` | ✅ | HTTP 200 · session 好使 |
| `GET /api/me/wallet` | ✅ | HTTP 200 · balance=0 |
| `GET /api/topup/channels` | ✅ | 返回 `waffo` hosted 通道 |
| `POST /api/me/topup`（credits=100_000_000） | ✅ | HTTP 201 · 生成 mock checkout url |
| `POST /api/internal/topup/{id}/paid`（dev mock） | ✅ | HTTP 200 · 走完 completed 闭环 |
| `POST /api/me/buses`（kind=single） | ✅ | HTTP 201 · 分配 invite_code |
| `PUT /api/me/downstream/passengerpool`（配 k2a URL + admin key） | ✅ | HTTP 200 `{ok:true}` |
| `POST /api/me/downstream/passengerpool/test`（探活） | ⚠️ | HTTP 200 但返 `{ok:false, latency:3000, error:"连不上目标地址"}` |

### 挂/漏的项

| 步骤 | 现象 | 分析 |
|---|---|---|
| `POST /api/me/buses/{id}/pull` | HTTP 402 `insufficient_balance` | **第一轮跑**是因为把 `credits=100` 传成了 microunit（1 积分 = 1_000_000 microunit）· 已在脚本里改成 `100_000_000` |
| passengerpool `test` 探活 | 3s 超时 | 本地 Go `http.Client{Timeout:3s}` 探 `https://k2a.muxpay.xyz/` 超时 · 但 `curl` 同 URL 0.65s 通 · 疑似 macOS local DNS / IPv6 首连开销 · **非阻塞**（真正的双写走 `internal/delivery/passengerpool/kirors` 不走这条 probe · probe 只是 UI 上的连通性指示） |
| build 挂 | `internal/bus/bus.go:224 undefined: nullableBool` | 主 workflow 正在 phase 2-8 改 `internal/bus/bus.go` + `internal/strategy/strategy.go` · 1f-B 引入 `*bool` 三态 · helper 函数 `nullableBool` 还没落 · 等主 workflow phase 3 完成会修 |

### 未跑到的项（因 build 挂）

- 实际 live 拉号（`POST /api/me/buses/{id}/pull` count=1 vendor=kiro91）
- 查 `credential_ledger` 看号是否落库
- 查 `outbound_webhook_delivery` 看 `boarded` 事件
- k2a 侧 `/api/admin/credentials?total` 增量对比双写命中

## 挂点修复建议

1. **`internal/bus/bus.go:224, 779, 885`** · `nullableBool` 未定义 —— 主 workflow phase 2-8 会在 `internal/bus/util.go` 或类似位置补 `func nullableBool(*bool) any`（跟 `nullableInt` / `nullableInt64` 同款）· **不属于我这个 agent 的活**。
2. **`internal/api/downstream.go:269 probeReachability`** · 3s timeout 对 macOS 本地首连 https 太紧 —— 建议放到 6s，或对 IPv6 disabled。**非阻塞**·真拉号路径不走这个函数。
3. **`.dev.env` 无 `default_vendor`** · 已补 `BP_DECIDER_DEFAULT_VENDOR=kiro91`。config.yaml `decider.default_vendor` 空 · env 覆盖会生效（`internal/config/config.go:325`）。

## 用户明早步骤

1. 确认主 workflow phase 2-8 build 修好：
   ```
   cd /Users/danlio/Repositories/daniel/bus-pooling
   go build ./cmd/bus-pooling
   ```
   若 `undefined: nullableBool` 仍在 → 等主 workflow 收尾。
2. `.dev.env` 已包含 `BP_HOUSEPOOL_ADMIN_KEY` + `BP_HOUSEPOOL_URL` + `BP_DECIDER_DEFAULT_VENDOR`。
3. 拿 k2a admin key 作为环境变量：
   ```
   ssh vps196 'docker exec kiro-rs cat /app/config/config.json' | grep adminApiKey
   export K2A_ADMIN_KEY="sk-admin-<那串>"
   ```
   （**别写进 .dev.env** · 那是我方系统池的 · k2a 是"乘客的" passengerpool。）
4. 一键跑：
   ```
   bash scripts/smoke-1f.sh
   ```
   默认端口 8091（避 8080 冲突）· 用独立 DB `data/smoke-1f.db`（不污染主 dev DB）。
   要换端口：`SMOKE_PORT=8092 bash scripts/smoke-1f.sh`。
5. **看结果**：
   - 全绿 ✅ → 1f 链路 live · 可以进 2a 阶段
   - 挂 pull（402 / 502 / 503）→ 修 `挂点修复建议` 里对应条 · 再跑
6. **清理**：脚本会自动 kill serve · 保留 `data/smoke-1f.db` 供事后 sqlite 查台账；serve log 跑通后删 · 跑挂时保留 `/var/folders/.../tmp.*` 路径。

## 明早 QA 账户

脚本里**每次生成**唯一测试账号（`smoke-1f-<epoch>@example.com`）· **不撞历史 DB**。

密码：`smoke-1f-password-please-change`（脚本顶部变量 `SMOKE_PASSWORD` · 可改）。

## 附：本地 verify 手动 curl

```bash
# 直接验证 housepool admin key
curl -sS -H "x-api-key: $(grep BP_HOUSEPOOL_ADMIN_KEY .dev.env | cut -d= -f2)" \
  https://kiro.aibbq.xyz/api/admin/credentials | python3 -m json.tool | head

# 直接验证 k2a admin key
curl -sS -H "x-api-key: $K2A_ADMIN_KEY" \
  https://k2a.muxpay.xyz/api/admin/credentials | python3 -m json.tool | head
```

两个端点都在 vps22/vps196 上确认存活 · Caddy 反代 → 容器 `127.0.0.1:8990` · 无 rate limit / IP allowlist。
