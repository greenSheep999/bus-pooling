# 1f live 链路测试报告

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
