# Sprint 1a · 后端（跟前端一起交付阶段 1a）

> 前置：Sprint 1a 前端骨架（`sprint-1a-frontend.md`）已完成 + `05-api-contract.md` 已冻结；`09-transactions.md`（状态机）；`06-db-schema.md`（含 sessions + idempotency_record + pending_handoff）；`07-provider-contract.md` · `08-housepool-contract.md`
>
> **Sprint 1a 后端目标**：**单 vendor（91kiro）+ 主入口拼车（1 人 bus）+ 次入口单独拉号 + 手动派去向（含两阶段 handoff）+ housepool 承载 + 4 类事务状态机 + 幂等 + 基础监控 + 手动号死处理**，端到端跟前端联调通。
>
> **命名说明**：**阶段 1a 由前后端两个 sprint 联合交付**：
> - `sprint-1a-frontend.md` · 前端骨架 + MSW + 12 页
> - `sprint-1a-backend.md` · 本文件 · 后端真接口
>
> 阶段 1b（未来）是"5 家 vendor + CDK + payment"，跟本文件无关。

## 完成标准（Definition of Done）

**跟前端联调通 4 条主流程**（Sprint 1a 前端已跑 mock 版）：

1. **注册 / 登录 / API key** —— 前端表单 → 真后端 → session cookie / api key hash 都对
2. **建 1 人 bus + 拉 5 号** —— 前端点击 → decider 走 `pending_purchase` 状态机 → 号入 housepool `bus-<id>` group
3. **单独拉 3 号 + 派 3 种去向** —— 拉号进 `record-<pid>`；派进车 + 推 passengerpool（后端此步 mock，1c 才实现）+ handoff（DELETE + 返回明文）
4. **手动置死一个号** → deathwatch 5 分钟内标记 dead_at（**不做**自动补车，是 1d）

**不要求**：自动化拉号 / 拼车集单 / 多 vendor / 兑换码 / payment-gateway 真实链路 / webhook out / 真实 passengerpool 双写。

## Sprint 1a Endpoint Matrix（冻结）

**只有下面明列的端点在 Sprint 1b 实现**；其余延后。**前端 mock 已覆盖但未列在这里的端点** = Sprint 1b **保留骨架 + 返回 501**，等 1c/1d 再补真实实现。

| 端点 | Sprint 1b | 备注 |
|---|---|---|
| `POST /api/register` | ✅ 实现 | Argon2id 密码 hash |
| `POST /api/login` | ✅ 实现 | session cookie |
| `POST /api/logout` | ✅ 实现 | 清 session |
| `GET /api/me/profile` | ✅ 实现 | |
| `POST /api/me/password` | ⚙️ 骨架 501 | 邮箱验证阶段 3+ |
| `GET /api/me/api-keys` | ✅ 实现 | |
| `POST /api/me/api-keys` | ✅ 实现 | 明文返回一次 |
| `DELETE /api/me/api-keys/{id}` | ✅ 实现 | |
| `GET /api/me/wallet` | ✅ 实现 | 含 reserved |
| `GET /api/me/ledger` | ✅ 实现 | 分页 |
| `POST /api/me/redeem` | ⚙️ 骨架 501 | 1b 后端 sprint 无 CDK 表 |
| `POST /api/me/topup` | ⚙️ 骨架 501 | payment 是 1c |
| `GET /api/me/topup/{id}` | ⚙️ 骨架 501 | 同上 |
| `GET /api/me/buses` | ✅ 实现 | |
| `POST /api/me/buses` | ✅ 实现 | 只支持 `kind: single` |
| `GET /api/me/buses/{id}` | ✅ 实现 | |
| `POST /api/me/buses/{id}/join` | ⚙️ 骨架 501 | anon bus 是 1c |
| `POST /api/me/buses/join-by-invite` | ⚙️ 骨架 501 | team bus 是 2a |
| `POST /api/me/buses/{id}/leave` | ✅ 实现 | 1 人 bus 不允许 leave（要 delete 解散） |
| `DELETE /api/me/buses/{id}` | ✅ 实现 | 解散 |
| `POST /api/me/buses/{id}/pull` | ✅ 实现 | **走 `pending_purchase` 状态机** |
| `GET /api/me/buses/{id}/members` | ✅ 实现 | 1 人 bus 只有 owner |
| `GET /api/me/buses/{id}/credentials` | ✅ 实现 | 显示号列表 + 用量占位 |
| `GET /api/me/buses/{id}/pulls` | ✅ 实现 | 拉号历史 |
| `GET /api/me/buses/{id}/stats` | ⚙️ 骨架 501 | 平均寿命是 1d |
| `POST /api/me/pull` | ✅ 实现 | 单独拉号 → record group |
| `GET /api/me/pull-records` | ✅ 实现 | |
| `GET /api/me/pull-records/{id}` | ✅ 实现 | |
| `POST /api/me/pull-records/assign` | ✅ 实现 | 进车 + 推 passengerpool 分支实现（passengerpool 走 mock 通道）；handoff 分支返回 download_token |
| `POST /api/me/pull-records/{id}/handoff-init` | ✅ 实现 | **两阶段 token 阶段 1** |
| `GET /api/me/handoff/{token}` | ✅ 实现 | **两阶段 token 阶段 2 · fulfill** |
| `POST /api/me/handoff/{token}/confirm` | ✅ 实现 | **两阶段 token 阶段 3 · 触发 DELETE** |
| `GET /api/me/credentials` | ✅ 实现 | |
| `GET /api/me/credentials/{id}` | ✅ 实现 | 含 usage 字段（concurrency_avg 常态 null） |
| `GET /api/me/strategy` | ✅ 实现 | 存空对象即可 |
| `PUT /api/me/strategy` | ✅ 实现 | 存表，1d 才用 |
| `GET /api/me/downstream` | ✅ 实现 | |
| `PUT /api/me/downstream/passengerpool` | ✅ 实现 | 加密存 token |
| `PUT /api/me/downstream/webhook` | ⚙️ 骨架 501 | webhook out 是 1e |
| `POST /api/me/downstream/webhook/test` | ⚙️ 骨架 501 | 同上 |
| `GET /api/me/downstream/webhook/deliveries` | ⚙️ 骨架 501 | 同上 |
| `GET /api/vendors` | ✅ 实现 | 单家 91kiro + 5 家标 disabled |
| `GET /api/vendors/{id}/stock` | ✅ 实现 | 只 91kiro 通 |
| `GET /api/vendors/{id}/health` | ⚙️ 骨架 501 | 1d 有平均寿命才做 |
| `POST /webhook/vendor/91kiro` | ⚙️ 骨架 501 | 1d 才做 |

**共 43 端点，Sprint 1a 后端实现 31 + 骨架 12。**

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

## 关键新增（相比原 sprint-1a）

基于 codex review + 用户拍板：

1. **`pending_purchase` / `pending_handoff` / `pending_assignment` 状态机** —— 见 `09-transactions.md`，janitor goroutine 每 30s 扫超时行做补偿
2. **`idempotency_record` 表 + 中间件** —— HTTP 幂等，30 天窗口
3. **wallet 冻结机制** —— `wallet.reserved` 字段 + `BEGIN IMMEDIATE` + 条件更新
4. **credential_ledger 号归属 = Bus**（`owner_bus_id` XOR `owner_record_passenger_id`）
5. **DRY_RUN 开关** —— 环境变量 `DRY_RUN=1` 时 vendor.Purchase 走 mock（避免误扣真钱）；生产 `DRY_RUN=0`
6. **kiro.rs commit sha 绑定** —— `config.yaml` 加 `housepool.kirors.expected_sha`，启动时 ping kiro.rs `/version` 校验，防止契约漂移
7. **登录方案冻结** —— Argon2id + session cookie + API key hash，**不接** SuperTokens（`decisions.md §1.9`）

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

### Iss #9 · decider + pending_purchase 状态机 · **1.5 天**
- **完整走 `pending_purchase` 状态机**（`09-transactions.md § 2`）:
  - `initial` → `reserved`（wallet 冻结 · `BEGIN IMMEDIATE` + 条件 UPDATE）
  - `reserved` → `purchased`（调 `providers/kiro91.Purchase` · 带 `client_order_id` 32 hex 幂等）
  - `purchased` → `imported`（`housepool.BatchImport` 到 `bus-<id>` 或 `record-<pid>`）
  - `imported` → `completed`（wallet 从 reserved 转消费 + ledger 落三笔）
- 计算：`key_cost + single_pull_fee(count==1 时 20% × key_cost) + service_fee(1 元)`
- 记录：`pull_round` + `credential_ledger` 各号一行 + `pending_purchase` 全程可查
- **DRY_RUN 模式**：`DRY_RUN=1` 时 vendor 调用走 mock（返 fake credential）
- **DoD**：
  - 一次拉号 → 4 个状态推进都对
  - 中间任一步 kill 进程 → 重启 janitor 30s 内推进到下一状态 or 补偿
  - 幂等重放同 `X-Idempotency-Key` → 返回 first response 字节一致
  - 并发 5 个 goroutine 同一乘客 → wallet 不超扣（reserved 冻结正确）

### Iss #10 · bus 包（single kind） · 1 天
- 表：`bus` / `bus_member`
- 端点：`POST /api/me/buses`（`kind: single`）/ `GET /api/me/buses` / `GET /api/me/buses/{id}` / `POST /api/me/buses/{id}/leave` / `DELETE /api/me/buses/{id}`（解散）
- **不做**：anon / team kind（那是 1c / 2a）
- **拉号入口**：`POST /api/me/buses/{id}/pull` → 调 strategy + decider
- **查看号**：`GET /api/me/buses/{id}/credentials` 从 `credential_ledger` join `housepool.ListCredentials(bus-<id>)`
- **DoD**：建 1 人 bus → 拉 5 个号 → 看到 5 个号在 bus 里

### Iss #11 · pullrecord + delivery/handoff + pending_handoff 状态机 · 1 天
- 端点：`POST /api/me/pull`（单独拉，走 decider，目标 group = `record-<pid>`）
- 端点：`GET /api/me/pull-records`（分页）
- 端点：`POST /api/me/pull-records/assign`（支持 handoff / to-bus 分支；to-passengerpool 走 mock 通道 1c 实现）
- 端点：`POST /api/me/pull-records/{id}/handoff`
- `internal/delivery/handoff/`：**走 `pending_handoff` 状态机**
  - `initial` → `plaintext_captured`（读明文）
  - `plaintext_captured` → `returned_to_user`（响应体填明文）
  - `returned_to_user` → `housepool_deleted`（`housepool.DeleteCredential`）
  - `housepool_deleted` → `completed`（`credential_ledger.status='handed_off'` + `idempotency_record` 落状态版响应）
- **幂等特例**：`idempotency_record.response_body` 存"状态版"（不含明文 keys）；重放响应 `already_delivered: true` + credential_ids + 空 keys
- **DoD**：
  - 拉 3 个 → handoff 1 个 → 拿到 4 件套明文 + housepool 里号被删 + `credential_ledger.status='handed_off'`
  - 用同 `X-Idempotency-Key` 重放 → 响应 `already_delivered: true` + 无明文

### Iss #12 · deathwatch v0（探活 + 踢出） · 0.5 天
- 定时任务：每 5 分钟调 `housepool.ListCredentials()` 取死号
- 找出 `credential_ledger` 里 `dead_at IS NULL` 但 kiro.rs 报 `disabled = true + disabled_reason` 表明失效的号
- 更新 `credential_ledger.dead_at = now, death_source = 'housepool_probe'`
- **不做**：补车链条（那是 1d）
- **DoD**：手动在 kiro.rs 侧禁用一个号 → 5 分钟后 credential_ledger 该行更新

### Iss #13 · e2e 测试 + docker-compose + DRY_RUN 环境 · 1.5 天
- `docker-compose.yml` 起：bus-pooling + **kiro.rs 真身 or mock**（两套 profile）+ sqlite volume
- **两套环境**：
  - `DRY_RUN=1` · vendor 调用走 mock，不扣真钱；用于 CI / 开发
  - `DRY_RUN=0` · 真实调 vendor；上线前手工验证一次
- **kiro.rs commit sha 绑定**：`config.yaml` 里 `housepool.kirors.expected_sha`；启动时 `GET /` 校验版本；不匹配拒启（防契约漂移）
- e2e 测试脚本：
  - 注册 → 登录 → API key
  - 建 bus → 拉 5 号 → 看 credentials（DRY_RUN=1）
  - 单独拉 3 号 → handoff 1 号 → 明文正确
  - **中途 kill decider 进程 → 重启 → janitor 恢复 → 状态最终 `completed`**（新增）
  - **同 idempotency key 重放 → 响应字节一致**（新增）
  - **并发 5 个拉号请求 → wallet reserved 正确不超扣**（新增）
  - 手动置死一个号 → deathwatch 5 分钟后正确标记
- **DoD**：`./run-e2e.sh` 全绿；用 `DRY_RUN=1` 跑 CI 无真实扣钱

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
- [ ] 16 张核心表 migration 通过（含 idempotency_record + pending_purchase + pending_assignment）
- [ ] 91kiro adapter + kiro.rs client 都真实调通（DRY_RUN=0 手工验证过 1 次）
- [ ] **状态机 e2e** 全绿：进程 kill 后 janitor 能恢复
- [ ] **幂等 e2e** 全绿：重放同 key 字节一致（handoff 除外）
- [ ] **并发 e2e** 全绿：5 并发拉号 wallet 不超扣
- [ ] e2e 脚本一键跑绿
- [ ] `go vet` / `golangci-lint` 无严重告警
- [ ] 敏感字段 0 命中（`grep -rE 'sk-|usr-[a-f0-9]{40}|password.*=' .`）
- [ ] 每包有 README（一段话说明目的 + 主要类型）
- [ ] git commit 干净（每个 issue 一个 commit or 一个 PR）
- [ ] **kiro.rs commit sha 已绑**并 CI 校验通过
- [ ] **API 响应 grep 无内部术语**（`housepool` / `initiated` / `handed_off` 等）

## Sprint 1a（前后端）结束后 · 下一个 Sprint

**Sprint 1b**（预计 2 周）：
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
