# sprint-1e · 后端主线交付清单

> **本文只写"1e 要做什么 + 上线前还剩什么"**。技术细节 / 决策讨论 / 长期契约 →
> 分别去 A 契约（00-14）/ D 决策（decisions.md）· 别塞进这里。
>
> 阶段 1e 目标（`00-values-and-phases §7`）：**去向 ② 推 passengerpool（双写）+
> 对外 webhook**。**无发车**（发车留 3b/3c）。
>
> **1e = 阶段 1 的最后一片拼图** · 完成后 = code-complete → 进入 live-ready 序列。

## 交付清单

### 1e-1 · 推 passengerpool 双写（去向 ②）

- [ ] `internal/delivery/passengerpool/kirors/` · 复用 `housepool/kirors` 客户端能力 · 拿乘客加密的 admin_token 打他自建的 kiro.rs
- [ ] 号从 `record-<pid>` group 派到 passengerpool 时:
  - housepool 本地保留副本（`disabled=true` · 我方继续监控存活）
  - 复制到 passengerpool（乘客自己用）
  - **双写**·不是 fire-and-forget · 见 `docs/01-architecture §Layer 5`
- [ ] `credential_ledger.pushed_to_passengerpool_at` 时间戳标记
- [ ] 失败结构化落 `push_error_code / push_error_status / push_error_message / push_error_retriable / push_attempts`（表已定义 · migration 001）
- [ ] `api/pullrecord.go` handoff 派去向增加 "推自己号池" 分支
- [ ] 生产 dry-run 兜底 · 未装 passengerpool 时保 record group

### 1e-2 · 对外 webhook（webhookout）

- [ ] `internal/webhookout/` · 我方推给乘客的事件出向
- [ ] 事件类型:`new_keys_available` / `all_keys_dead` / `warranty_refund` / `boarded`
- [ ] HMAC-SHA256 签名 · 用乘客自己配的 secret
- [ ] 重试 3 次 · 指数退避 · 8s 超时
- [ ] `outbound_webhook_delivery` 表(migration 003 · 已存在)记 attempt / response_status
- [ ] 触发源:decider.Pull 成功 / deathwatch 标死 / warranty_refund settle
- [ ] webhook 静默失败不影响主链路

### 1e-3 · 装配 · CI · 文档

- [ ] `main.go` 装配 passengerpool client + webhookout sender
- [ ] 单测 + 集成测试(passengerpool mock server · webhookout mock receiver)
- [ ] `05-api-contract §8` 已定的下游端点全接
- [ ] `13-frontend-design`/`12-frontend-pages` 检查:下游配置页跟 1e 语义一致

## 依赖 / 阻塞

- 1d 完成(已完成 · 30 commit 上线)
- passengerpool client · 复用 `housepool/kirors` 能力(共用 kiro.rs 协议)

## 1e 上线判据

- [ ] passengerpool 双写走通端到端(number 进 passengerpool · housepool 保留副本 · 时间戳打上)
- [ ] outbound webhook 至少推一次成功事件到 mock receiver · HMAC 签名验通
- [ ] deathwatch 触发号死时 · outbound webhook 也推
- [ ] 双写失败结构化 · push_error_* 五字段有值

---

## 1e 完成 = 阶段 1 code-complete

1e 是阶段 1 最后一片拼图。做完之后进入**阶段 1 收官上线路线**(mock/live 分阶段切真链路)· 详见 `docs/sprint-1-final.md`。

## 归档时机

跟 sprint-1a/1b/1c/1d 一起 · 阶段 1 收官(见 `sprint-1-final.md` Stage 7)后归档到 `docs/archive/`。
