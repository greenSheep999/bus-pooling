# Sprint 1a · 首个开发周期 backlog

> 前置：`00-values-and-phases.md §7 阶段表`（1a 定义）· `03-modules.md` · `05-api-contract.md` · `06-db-schema.md` · `07-provider-contract.md` · `08-housepool-contract.md`
>
> **Sprint 1a 目标**：**单 vendor（91kiro）+ 主入口拼车（1 人 bus）+ 次入口单独拉号 + 手动派去向（handoff）+ housepool 承载 + 基础监控 + 手动号死处理**，端到端可以跑一次拉号流程。

## 完成标准（Definition of Done）

**能跑通这一整条主链**：

1. 一个新乘客能注册 / 登录 / 生成 API key
2. 用 CLI or Postman 调 `POST /api/me/buses` 建一个 1 人 bus
3. 调 `POST /api/me/buses/{id}/pull` 拉 5 个号 → 号入 housepool 的 `bus-<id>` group
4. 调 `GET /api/me/buses/{id}/credentials` 看到 5 个号（每号显示 pulled_at / 存活状态 / 用量占位）
5. 调 `POST /api/me/pull` 单独拉 3 个号 → 号入 `record-<pid>` group + `disabled=true`
6. 调 `POST /api/me/pull-records/{id}/handoff` 拿走 1 个号 → 收到明文 + housepool 里号被删
7. kiro.rs 探活侦测某号死 → deathwatch 从 group 踢出 → wallet ledger 记录（暂不触发补车，那是 1d）

**不要求**：自动化拉号 / 拼车集单 / 多 vendor / 兑换码 / payment-gateway / 推 passengerpool / webhook out。

## 阶段 1a 的"必须做" 12 项模块

映射到 `03-modules.md` 里的业务包：

| # | 包 | 完成度目标 |
|---|---|---|
| 1 | `infra/db` + `infra/config` + `infra/httpx` + `infra/secrets` | 骨架就绪 |
| 2 | `internal/passenger` + `internal/authpassenger` | 注册 / 登录 / API key 生成 |
| 3 | `internal/wallet` | 余额 + ledger 事务原子性 |
| 4 | `internal/providers` + `internal/providers/kiro/vendors/kiro91` | 单家 vendor adapter；Stock / Purchase / OrderKeys 三个方法 |
| 5 | `internal/housepool` + `internal/housepool/kirors` | HousePool interface + kiro.rs client 关键方法 |
| 6 | `internal/strategy` | 手动拉号 · 判可行（钱 / 每日上限） |
| 7 | `internal/decider` | 单 vendor 直选 · 拉号 → 记账 → 入池 |
| 8 | `internal/bus` | 建 / 查 / 退出 / 解散 · single kind |
| 9 | `internal/pullrecord` | 单独拉号后写 record group + 派去向 |
| 10 | `internal/deathwatch` | 探活 + 踢死号（**不做**自动补车，是 1d） |
| 11 | `internal/delivery/handoff` | 拿走号数据（返回明文 + DELETE） |
| 12 | `internal/api` + 基础 handler | 上述端点的 HTTP 层 + API key 鉴权 |

**共 12 个包骨架 + 1 套 e2e 测试**。

## Issue 拆分（估工时 = 单人纯开发；不含 review / 联调）

### Iss #1 · 项目骨架 · 0.5 天
- `go mod init` / `.gitignore` / `Dockerfile` 占位
- `cmd/bus-pooling/main.go` 启动骨架
- `internal/config/` 读 yaml
- `internal/db/` SQLite 连接 + migration runner
- CI 骨架（`go build ./... && go test ./...`）
- **DoD**：`go build` 通过；`./bus-pooling` 起来能读 config

### Iss #2 · 基础设施 · 1 天
- `internal/httpx/` 出向 client（代理 + 超时 + 重试骨架）
- `internal/secrets/` AES-GCM 主密钥从 env；`Encrypt` / `Decrypt` API
- 单元测试
- **DoD**：httpx 覆盖 200/timeout/503/rate limit 场景；secrets 加密解密可逆

### Iss #3 · 首批 migration · 0.5 天
- `internal/db/migrations/001_init.sql` 覆盖 12 张表（`06-db-schema.md §Migration 顺序 · 1a`）：
  - `passenger`, `passenger_api_key`, `passenger_downstream`
  - `wallet`, `wallet_ledger`
  - `bus`, `bus_member`
  - `pull_intent`, `pull_round`, `credential_ledger`
  - `vendor_account`, `passenger_daily_counter`
- migration runner 支持 up / rollback
- **DoD**：`migrate up` / `migrate down` 干净

### Iss #4 · passenger 包 · 1 天
- 表：`passenger` / `passenger_api_key`
- 端点：`POST /api/register` / `POST /api/login` / `POST /api/logout` / `GET /api/me/profile`
- 密码：Argon2id
- API key 生成 + 存 hash + 列表 / 吊销端点
- 中间件：会话鉴权 + API key 鉴权（`X-API-Key` / `Authorization: Bearer`）
- **DoD**：注册 → 登录 → 生成 API key → 用 API key 调 profile 全通

### Iss #5 · wallet 包 · 1 天
- 表：`wallet` / `wallet_ledger` / `passenger_daily_counter`
- 内部 API：`Debit(passenger_id, amount, reason, ref, memo)` / `Credit(...)` / `GetBalance`
- **同事务**：balance 变更 + ledger 插入
- 端点：`GET /api/me/wallet` / `GET /api/me/ledger`（分页）
- **DoD**：并发 Debit 保证不超扣（用 `SELECT ... FOR UPDATE` 或 SQLite 事务隔离）；ledger `seq` 严格递增

### Iss #6 · providers 骨架 + 91kiro adapter · 1.5 天
- `internal/providers/provider.go` / `vendor.go` / `errors.go` / `webhook.go` interface + struct
- `internal/providers/kiro/vendors/kiro91/`：
  - `adapter.go` 实现 `Vendor` interface
  - `Stock` / `Purchase` / `OrderKeys` / `Balance` 四个方法
  - `KeyHealth` / `KeyStats` 骨架（返回 `ErrNotSupported` 也行，1d 再补）
  - `types.go` wire type（91kiro 的原字段）
  - `mapper.go` wire → 归一化
- adapter 使用 `internal/httpx` + `internal/secrets` 拿 API key
- **DoD**：httptest mock 91kiro 端点，Stock / Purchase 两个方法覆盖成功 / 401 / 429 / no_stock / retry_same_order 各一个用例

### Iss #7 · housepool/kirors client · 1.5 天
- `internal/housepool/housepool.go` interface + struct
- `internal/housepool/kirors/client.go`：
  - `BatchImport`（SSE 流处理）
  - `UpdateCredential` (`PUT /credentials/{id}`)
  - `SetDisabled` / `DeleteCredential`
  - `ListCredentials` / `GetCredential` / `GetBalance`
  - `ListGroups` / `CreateGroup`
  - `Ping`
- **DoD**：httptest mock kiro.rs 端点，覆盖上述方法各成功 + 一个错误用例

### Iss #8 · strategy 骨架 · 0.5 天
- 表：`passenger_strategy`
- 内部方法：`CanPull(passenger_id, count, unit_price_hint) (Intent, error)`
- 检查项：余额 / 每日上限（阶段 1a 用简单硬编码，不做 UI 配置）
- 端点：`GET/PUT /api/me/strategy`（可存空对象，暂不用）
- **DoD**：`CanPull` 拒绝余额不足 / 触发上限；返回 `Intent` struct

### Iss #9 · decider v0（单 vendor 直选）· 1 天
- 拿意图 → 强制走 91kiro → 调 `providers/kiro91.Purchase`
- 计算：`key_cost + single_pull_fee(count==1 时 20%) + service_fee(1 元)`
- 事务：wallet Debit + kiro.rs BatchImport 到目标 group（`bus-<id>` 或 `record-<pid>`）
- 生成幂等键 `client_order_id`（32 hex uuid）
- 记录：`pull_round` + `credential_ledger` 各号一行
- **DoD**：一次拉号 → 号入 kiro.rs 正确 group + wallet 扣款正确 + ledger 3 条（key_cost / service_fee / 可能 single_pull_fee）

### Iss #10 · bus 包（single kind） · 1 天
- 表：`bus` / `bus_member`
- 端点：`POST /api/me/buses`（`kind: single`）/ `GET /api/me/buses` / `GET /api/me/buses/{id}` / `POST /api/me/buses/{id}/leave` / `DELETE /api/me/buses/{id}`（解散）
- **不做**：anon / team kind（那是 1c / 2a）
- **拉号入口**：`POST /api/me/buses/{id}/pull` → 调 strategy + decider
- **查看号**：`GET /api/me/buses/{id}/credentials` 从 `credential_ledger` join `housepool.ListCredentials(bus-<id>)`
- **DoD**：建 1 人 bus → 拉 5 个号 → 看到 5 个号在 bus 里

### Iss #11 · pullrecord + delivery/handoff · 1 天
- 端点：`POST /api/me/pull`（单独拉，走 decider，目标 group = `record-<pid>`）
- 端点：`GET /api/me/pull-records`（分页）
- 端点：`POST /api/me/pull-records/assign`（阶段 1a 只支持 handoff 分支）
- 端点：`POST /api/me/pull-records/{id}/handoff`（快捷路径）
- `internal/delivery/handoff/`：读明文 → 返回给用户 → `housepool.DeleteCredential`
- **DoD**：拉 3 个 → handoff 1 个 → 拿到 4 件套明文 + housepool 里号被删 + `credential_ledger.status='handed_off'`

### Iss #12 · deathwatch v0（探活 + 踢出） · 0.5 天
- 定时任务：每 5 分钟调 `housepool.ListCredentials()` 取死号
- 找出 `credential_ledger` 里 `dead_at IS NULL` 但 kiro.rs 报 `disabled = true + disabled_reason` 表明失效的号
- 更新 `credential_ledger.dead_at = now, death_source = 'housepool_probe'`
- **不做**：补车链条（那是 1d）
- **DoD**：手动在 kiro.rs 侧禁用一个号 → 5 分钟后 credential_ledger 该行更新

### Iss #13 · e2e 测试 + docker-compose · 1 天
- `docker-compose.yml` 起：bus-pooling + kiro.rs mock + sqlite volume
- e2e 测试脚本：
  - 注册 → 登录 → API key
  - 建 bus → 拉 5 号 → 看 credentials
  - 单独拉 3 号 → handoff 1 号 → 明文正确
  - 手动置死一个号 → deathwatch 5 分钟后正确标记
- **DoD**：`./run-e2e.sh` 全绿

### 总工时估算

约 **12 天纯开发**（不含 review + 联调 + fix bug）；单人节奏约 **3 周**；两人并行约 **1.5-2 周**。

## 依赖关系（哪个 issue 先做）

```
#1 骨架  →  #2 基础设施  →  #3 migration  →  #4 passenger
                                              ↓
              #6 providers/kiro91 ──┐        #5 wallet
              #7 housepool/kirors ──┼──►    #9 decider  →  #10 bus (single) → #11 pullrecord + handoff
                                    │        ↑
                                    │        #8 strategy
                                    │
                                    #12 deathwatch v0（依赖 #7）
                                                                                         ↓
                                                                                        #13 e2e
```

**关键路径**：`#1 → #3 → #5 → #9 → #10 → #13`（约 6.5 天）。**#6 + #7 可并行**（#6 是 vendor 侧，#7 是 housepool 侧，两个不同人可以同时做）。

## 交付验收（sprint 结束时）

- [ ] 12 个业务/基础设施包骨架都在
- [ ] 5 张核心表 migration 通过
- [ ] 91kiro + kiro.rs 都能真实调通（不是全 mock）
- [ ] e2e 脚本一键跑绿
- [ ] `go vet` / `golangci-lint` 无严重告警
- [ ] 敏感字段 0 命中（`grep -rE 'sk-|usr-[a-f0-9]{40}|password.*=' .`）
- [ ] 每包有 README（一段话说明目的 + 主要类型）
- [ ] git commit 干净（每个 issue 一个 commit or 一个 PR）

## Sprint 1a 结束后 · 下一个 Sprint 是什么

**Sprint 1b**（预计另 2 周）：
- 加 5 家 vendor adapter（kiroceo / kirooo / kiroappio / kiroappcc / kirodrop）
- 兑换码 `redeem` 包
- payment-gateway (waffo) `payment` 包

**Sprint 1c**（预计 1-2 周）：
- `bus.kind = anon` 匿名撮合
- `coalescer/anon.go` 集单调度
- 号价按 N 分摊

**Sprint 1d**（预计 1-2 周）：
- 自动策略引擎（时钟 / 水位 / vendor webhook 触发）
- 决策模型比价 + fallback + 平均寿命统计
- 号死自动补车链条
- `webhookin` vendor webhook 归一化
- `credential_usage_snapshot` + `bus_usage_snapshot` 采集

**Sprint 1e**（预计 1 周）：
- `delivery/passengerpool/kirors` 双写
- `webhookout` 我方推乘客

## 提醒

- **每个 issue 完成时 commit 一次**（不要等一堆 issue 一起提）
- **每个 commit 必须 `go build` 通过 + 相关测试通过**
- **不写代码前先看 `CLAUDE.md`**（术语铁律 / 作废清单 / 不做清单）
- **有分歧先看 `decisions.md`** —— 讨论过并否决 / 待定 全在里面
- **API 返回体 / 错误 message 别出现内部术语**（见 `CLAUDE.md §12`）
