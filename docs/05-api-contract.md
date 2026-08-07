# bus-pooling · 乘客侧 API 契约

> 前置阅读：[`00-values-and-phases.md`](./00-values-and-phases.md) · [`04-scenarios.md`](./04-scenarios.md)
> 本文只写**乘客侧对外 HTTP API 契约**。**不写**具体 DB schema（那是 `06-db-schema.md`）、内部包接口（那是 `03-modules.md`）。
>
> 契约稳定后，端点字段的完整细节可在代码里用 Swagger / OpenAPI 自动生成。本文只钉**契约面貌 + 幂等语义 + 错误码**。

## 通用约定

### 基础

| 项 | 值 |
|---|---|
| Base URL | 待定（阶段 1a 部署时确定） |
| Content-Type | `application/json`（除 webhook 外） |
| 编码 | UTF-8 |
| 时间格式 | ISO-8601 UTC 字符串，如 `2026-08-07T12:34:56.789Z` |
| 金额单位 | **整数 microunit**（1 元 = 1_000_000）—— 前端展示时除 1M |
| 币种 | 阶段 1 只有 `CNY`；阶段 2+ 若加海外 payment 通道再补 `USD` |
| 分页信封 | `{ "items": [...], "total": N, "page": P, "page_size": PS, "pages": T }`，`?page=1&page_size=50`，上限 500 |

### 鉴权（3 种入口）

| 入口 | Header | 用途 |
|---|---|---|
| 会话 cookie | 浏览器登录后自动 | UI 操作 |
| API key | `X-API-Key: usr-<hex>` 或 `Authorization: Bearer usr-<hex>` | 脚本 / 服务端 |
| 内部（vendor webhook 入向） | 各 vendor 各自签名（见 `docs/vendors/*.md`） | vendor → 我方 |

**API key 权限**：只能调 `/api/me/*`；**不能调** `/api/admin/*`（管理端），也**不能改自己的登录密码 / rotate API key**（避免"泄露的 key 换成新 key 把主人锁在门外"，跟 91kiro 一致）。

### 错误响应

统一形状：

```json
{
  "code": "no_stock",
  "message": "暂无可交付库存，请稍后重试",
  "retry_after": 30
}
```

- `code` 是**稳定标识**（客户端按 code 分派，**不按 message**）
- `message` 是中文人话
- `retry_after` 仅限流 / 上游临时不可用时给（秒）
- 全部错误码见文末 §错误码

### 幂等键（`X-Idempotency-Key`）

- 写请求**建议**带 `X-Idempotency-Key: <32 hex>` header
- **拉号 / 派去向 / 充值起单**这三类**必须**带，否则响应体的 `client_order_id` 会由服务端生成，客户端无法重放
- 同 key 重复 → 返回**字节一致**的原响应，不重复副作用
- 幂等窗口：**30 天**（服务端 `idempotency_record` 表 30 天后清理，之后重放视为新请求）

### handoff 幂等特例（重要）

`POST /api/me/pull-records/{id}/handoff` 和 `POST /api/me/pull-records/assign`（含 handoff 分支）**是幂等规则的例外**：

- **首次调用**：返回 credential 明文（`keys[]` 含完整字段）
- **重放（同 X-Idempotency-Key）**：返回**`already_delivered: true` + `credential_ids: [...]` + `delivered_at: "..."` + `keys: []`**（**明文只给一次**）

理由：handoff 语义是"号数据交出去 + 我方 DELETE 号"，明文我方**不留**（安全），故不可能"字节一致重放"。客户端需要缓存首次响应里的明文，重放时不再期望拿到明文。

**其它端点**（购买 / 派进车 / 派 passengerpool）**没有明文问题**，字节一致重放规则依然适用。

---

## 1. 账号

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| POST | `/api/register` | 注册（邮箱 + 密码 或 SuperTokens 引导） | 1a |
| POST | `/api/login` | 登录 | 1a |
| POST | `/api/logout` | 登出 | 1a |
| GET | `/api/me/profile` | 当前账号信息 | 1a |
| POST | `/api/me/password` | 改密码（会话鉴权强制） | 1a |

### `GET /api/me/profile` 响应示例

```json
{
  "profile": {
    "id": "01H8...",
    "username": "alice",
    "email": "alice@example.com",
    "balance": 1400000000,
    "created_at": "2026-08-07T12:00:00Z"
  },
  "auth_mode": "session"
}
```

## 2. API Key

| Method | Path | 说明 | 备注 |
|---|---|---|---|
| GET | `/api/me/api-keys` | 我的 key 列表（不含明文） | 只显示 prefix |
| POST | `/api/me/api-keys` | 创建 key | **明文只返回一次**；会话鉴权强制 |
| DELETE | `/api/me/api-keys/{id}` | 吊销 key | 立即生效 |

**创建响应**：

```json
{
  "key": "usr-1a2b3c4d5e6f...",
  "item": { "id": "01H8...", "prefix": "usr-1a2b3c4d", "created_at": "..." }
}
```

## 3. 钱包 / 充值

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| GET | `/api/me/wallet` | 余额 + 概要 | 1a |
| GET | `/api/me/ledger` | 积分流水（分页） | 1a |
| POST | `/api/me/redeem` | 兑换码 | 1b |
| POST | `/api/me/topup` | 起充值单（走 waffo） | 1b |
| GET | `/api/me/topup/{order_id}` | 查充值单 | 1b |
| GET | `/api/me/topup-orders` | 我的充值单历史（分页） | 1b |

### `GET /api/me/ledger` `?type=` 枚举

- `recharge` / `channel_fee` / `redeem` / `key_cost` / `single_pull_fee` / `capability_fee` / `service_fee` / `warranty_refund` / `admin_adjust`

### `POST /api/me/redeem`

```json
// req
{ "code": "KRC-XXXX" }
// resp
{ "quota": 100000000, "replayed": false, "balance": 195000000 }
```

### `POST /api/me/topup`

```json
// req
{ "amount": 100000000, "channel": "waffo" }
// resp
{ "order_id": "01H8...", "pay_url": "https://waffo/...", "expires_at": "..." }
```

**通道费 pass-through**：乘客付 100 CNY → 到账 95 CNY 积分（`recharge +95` + `channel_fee -5` 明细，见 `00 §3`）。

## 4. Bus（拼车主入口）

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| GET | `/api/me/buses` | 我参与的所有 bus | 1a |
| POST | `/api/me/buses` | 建 bus | 1a（`kind: single`）→ 1c（`anon`）→ 2a（`team`） |
| GET | `/api/me/buses/{bus_id}` | bus 详情 + 号池状态 | 1a |
| POST | `/api/me/buses/{bus_id}/join` | 加入 anon bus（匿名撮合） | 1c |
| POST | `/api/me/buses/join-by-invite` | 邀请码加入 team bus | 2a |
| POST | `/api/me/buses/{bus_id}/leave` | 退出 bus | 1a |
| DELETE | `/api/me/buses/{bus_id}` | 解散 bus（创建人 or 最后一位成员） | 1a |
| POST | `/api/me/buses/{bus_id}/pull` | 给这个 bus 拉一次号 | 1a |
| GET | `/api/me/buses/{bus_id}/members` | bus 成员列表 | 1a |
| GET | `/api/me/buses/{bus_id}/credentials` | 该 bus 的号列表（含存活状态 + 用量） | 1a |
| GET | `/api/me/buses/{bus_id}/pulls` | 该 bus 的拉号历史（分页） | 1a |
| GET | `/api/me/buses/{bus_id}/stats` | 该 bus 号池的聚合统计（跨窗口） | 1d |

### `POST /api/me/buses`

```json
// req
{ "name": "我的号池", "kind": "single" }  // kind: single | anon | team
{ "name": "程序员拼车", "kind": "team", "invite_code_hint": "friends" }  // 阶段 2a
// resp
{ "bus": { "id": "01H8...", "kind": "single", "invite_code": null, "members": [...] } }
```

### `POST /api/me/buses/{bus_id}/pull`

```json
// req
{
  "count": 5,
  "vendor_id": "kiro91",      // optional，让系统选就不传
  "constraints": {              // optional
    "max_unit_price": 30000000
  }
}
// resp (成功)
{
  "pull_round_id": "01H8...",
  "vendor_id": "kiro91",
  "purchased": 5,
  "key_cost": 100000000,        // 号价（pass-through）
  "single_pull_fee": 0,          // count==5 → 0
  "service_fee_total": 5000000,  // 参与人 × 1
  "channel_fee": 0,              // 拉号动作不涉及通道费
  "total_debit": 105000000,      // wallet 扣的
  "balance_remaining": 895000000
}
// resp (错误)
{ "code": "insufficient_balance", "message": "余额不足 X" }
```

**关键**：`count == 1` 触发单次议价，响应里 `single_pull_fee` 是号价 × 20%。

**幂等**：`X-Idempotency-Key` 必填。

## 5. 单独拉号（次入口 + 拉号记录）

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| POST | `/api/me/pull` | 单独拉一批号（不指定 bus） | 1a |
| GET | `/api/me/pull-records` | 我的拉号记录（分页；含状态、去向历史） | 1a |
| GET | `/api/me/pull-records/{record_id}` | 单条详情 | 1a |
| POST | `/api/me/pull-records/assign` | 派去向（一次可混合三种） | 1a |
| POST | `/api/me/pull-records/{record_id}/handoff` | 单条拿走（快捷） | 1e |

### `POST /api/me/pull`

```json
// req
{ "count": 10, "vendor_id": null }   // vendor_id null = 系统选
// resp
{
  "pull_round_id": "01H8...",
  "vendor_id": "kiro91",
  "purchased": 10,
  "records": [
    { "record_id": "01H8...", "credential_id": 12345 },   // 12345 是 kiro.rs credential id (u64)
    ...
  ],
  "key_cost": 200000000,
  "single_pull_fee": 0,             // count>=2 → 0
  "service_fee": 1000000,           // 一次动作 1 元
  "total_debit": 201000000,
  "balance_remaining": 799000000
}
```

**注意**：`service_fee = 1 元`（一次动作一轮，跟 count 无关）；`single_pull_fee` 只在 `count==1` 才有。

### `POST /api/me/pull-records/assign` （核心派去向）

```json
// req
{
  "assignments": [
    { "record_ids": ["01H8a", "01H8b", ...5 个], "target": "to-bus", "bus_id": "01H8..." },
    { "record_ids": ["01H8f", "01H8g"], "target": "to-passengerpool" },
    { "record_ids": ["01H8h", "01H8i", "01H8j"], "target": "handoff" }
  ]
}
// resp
{
  "assigned": 10,
  "handoff_credentials": [   // 拿走的 credential 明文，仅这次返回
    { "record_id": "01H8h", "key": "ksk_...", "account": "...", "password": "...", "issuer_url": "..." },
    ...
  ],
  "errors": []   // 部分失败时列出
}
```

**关键**：
- 一次调用可**混合三种去向**（进车 / 推 passengerpool / handoff）
- 顺序：**先做外部动作（handoff 明文返回、推 passengerpool 完成），再改 housepool 状态**
- handoff 的 credential 明文**只在这个响应里返回一次**，之后 API 不再回

## 6. 号池 / credential 查询（跨 bus + 拉号记录）

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/me/credentials` | 我名下所有活的号（跨 bus / 拉号记录 / 已推 passengerpool 的） |
| GET | `/api/me/credentials?history=1` | 含死号 |
| GET | `/api/me/credentials/{id}` | 单号详情：进池时间 / 死亡时间 / 用了多少 / 平均积分消耗 / 并发（若有） |

**这是"我名下号"的入口**。`handoff` 出去的号不在这里（已离开 housepool）。

### 单号详情返回字段（`GET /api/me/credentials/{id}`）

```json
{
  "credential": {
    "id": "01H8...",
    "vendor_id": "kiro91",
    "current_group": "bus-01H...",  // 或 record-<pid>
    "pulled_at": "2026-08-01T10:00:00Z",
    "dead_at": null,                 // null = 存活；有值 = 已死
    "death_source": null,             // housepool_probe | vendor_webhook | vendor_poll
    "usage": {
      "calls_24h": 320,
      "calls_7d": 1850,
      "input_tokens_24h": 82000,
      "output_tokens_24h": 15000,
      "errors_24h": 4,
      "credits_used_total": 4700000,  // microunit，累计消耗额度
      "avg_credits_per_day": 1560000,  // 累计消耗 / 存活天数（microunit）
      "concurrency_avg": null           // 平均并发；kiro.rs 未直接给，暂空
    }
  }
}
```

**`concurrency_avg` 字段现状**：kiro.rs 未提供并发读端点。**待方案**：
- (a) 给 kiro.rs 加 `GET /credentials/{id}/concurrency`（**推荐**，运维方拍板）
- (b) 我方定期采样聚合
- (c) 反推（需响应时间数据）—— 不可用

暂未拍板 → 该字段返回 `null`，UI 显示 `—`。

### `GET /api/me/buses/{bus_id}/credentials` 返回

```json
{
  "credentials": [
    { "id": "...", "pulled_at": "...", "dead_at": null, "usage": { "calls_24h": 320, ... } },
    ...
  ],
  "aggregate": {
    "alive_count": 8,
    "dead_count": 2,
    "total_calls_24h": 2500,
    "total_credits_used": 40000000
  }
}
```

### `GET /api/me/buses/{bus_id}/pulls` 返回（拉号历史）

```json
{
  "items": [
    {
      "pull_round_id": "01H...",
      "vendor_id": "kiro91",
      "count_purchased": 5,
      "participants_split": { "01H_alice": 2, "01H_bob": 3 },  // 谁分几个
      "key_cost_total": 100000000,
      "service_fee_total": 2000000,
      "single_pull_fee_total": 0,
      "created_at": "..."
    },
    ...
  ],
  "total": 42, "page": 1, "page_size": 50
}
```

### `GET /api/me/buses/{bus_id}/stats` 返回（阶段 1d）

跨 24h/7d/30d 窗口聚合：

```json
{
  "window": "7d",
  "alive_count_now": 8,
  "dead_count": 3,
  "avg_lifespan_hours": 42.5,
  "total_calls": 18000,
  "total_credits_used": 350000000,
  "avg_credits_per_credential_per_day": 500000,
  "errors_rate": 0.012,
  "concurrency_avg": null   // 同单号 concurrency，暂空
}
```

## 7. 策略参数

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| GET | `/api/me/strategy` | 我的策略 | 1a |
| PUT | `/api/me/strategy` | 更新策略 | 1a → 1d |

### `PUT /api/me/strategy`

```json
{
  "auto_enabled": false,
  "per_round_count": 5,
  "min_count": 1,
  "keep_safety_stock": 3,
  "max_unit_price": 30000000,      // 号价上限（microunit）
  "daily_round_limit": 20,
  "daily_spend_limit": 500000000,
  "target_bus_id": "01H8..."       // 自动拉号进哪辆 bus
}
```

## 8. 下游配置

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| GET | `/api/me/downstream` | 当前配置 | 1e |
| PUT | `/api/me/downstream/passengerpool` | 配 passengerpool url + token | 1e |
| PUT | `/api/me/downstream/webhook` | 配我方推给他的 webhook 地址 | 1e |
| POST | `/api/me/downstream/webhook/test` | 发一条测试 webhook | 1e |
| GET | `/api/me/downstream/webhook/deliveries` | 投递日志 | 1e |

### `PUT /api/me/downstream/passengerpool`

```json
{
  "url": "https://my-kiro-rs.example.com",
  "token": "..."          // 保存后不再回显
}
```

### `PUT /api/me/downstream/webhook`

```json
{
  "url": "https://your-server.com/hook"
}
// resp
{ "url": "...", "secret": "64-hex-signing-secret" }
// secret 用来给我方推的 webhook 签名，pass 给你验签
```

## 9. Vendor 状态（公开只读）

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| GET | `/api/vendors` | 6 家 vendor 列表 + 当前 stock / price 快照 | 1a |
| GET | `/api/vendors/{vendor_id}/stock` | 单家实时快照 | 1a |
| GET | `/api/vendors/{vendor_id}/health` | 单家健康（平均寿命等） | 1d |

**匿名可访问**（不含敏感）。

## 10. Webhook 入向（vendor 推我方）

| Method | Path | 说明 |
|---|---|---|
| POST | `/webhook/vendor/{vendor_id}` | vendor 事件入口 |

各家 vendor 各自的签名验证：
- 91kiro：HMAC-SHA256，`X-KM-Signature: sha256=<hex>`
- kiro drop：HMAC-SHA256，`X-Kiro-Signature: v1=<hex>`
- 详见 `docs/vendors/*.md §10`

内部落 `vendor_webhook_delivery` 表去重后转发给 `deathwatch` / `strategy`。

## 11. Webhook 出向（我方推乘客）

**由乘客配置**（`PUT /api/me/downstream/webhook`）。

**事件类型**：

| `event` | 触发 |
|---|---|
| `new_keys_available` | 拉号成功、bus 补车完成 |
| `all_keys_dead` | 某 bus 全体号死 |
| `warranty_refund` | 质保退款到乘客积分 |
| `test` | `POST /api/me/downstream/webhook/test` 触发 |

### 载荷示例（`new_keys_available`）

```json
{
  "event": "new_keys_available",
  "event_id": "01H8...",
  "bus_id": "01H8...",
  "new_keys": 5,
  "vendor_id": "kiro91",
  "timestamp": "2026-08-07T..."
}
```

### 签名 header

```
X-Bus-Event: new_keys_available
X-Bus-Event-Id: 01H8...
X-Bus-Timestamp: 1785000000
X-Bus-Signature: sha256=<hex HMAC-SHA256(secret, timestamp + "." + body)>
```

### 重试

- 超时 8 秒 → 视为失败
- 重试 3 次，间隔 3s / 8s / 20s（指数退避 + 抖动）
- 4xx 视为明确拒绝，不重试
- 5xx / timeout 重试直到耗尽

## 12. 管理端（占位，阶段 3 才做）

- `/api/admin/*` 路由，会话鉴权（不接受 API key）
- 端点在 `12-admin-api.md`（未来）落地

---

## 错误码全表

| HTTP | `code` | 含义 |
|---|---|---|
| 400 | `bad_json` | 请求体非法 JSON / 含未知字段 |
| 400 | `bad_idempotency_key` | 幂等键格式错（非 32 hex） |
| 400 | `bad_count` | count 超范围 |
| 400 | `bad_vendor` | 未知 vendor_id |
| 400 | `bad_bus_id` | bus 不存在或不属于该乘客 |
| 400 | `bad_assignment_plan` | 派去向 plan 校验失败（record_id 不属于该乘客 / target 未知等） |
| 401 | `unauthenticated` / `invalid_api_key` | 未登录 / key 无效 |
| 402 | `insufficient_balance` | 余额不足 |
| 403 | `disabled` | 账号停用 |
| 403 | `session_required` | 该操作只能会话鉴权（改密码 / API key 创建等） |
| 403 | `csrf_failed` | cookie 写操作缺 CSRF |
| 404 | `not_found` | 资源不存在 |
| 409 | `no_stock` | vendor 缺货 |
| 409 | `purchase_cap_reached` | 达持有上限（vendor 侧） |
| 409 | `idempotency_conflict` | 同 key 但 body 不一致 |
| 409 | `bus_full` | anon bus 已达容量 |
| 409 | `already_member` | 已在该 bus |
| 413 | `body_too_large` | 请求体 > 1 MiB |
| 429 | `rate_limited` | 限流；看 `retry_after` |
| 502 | `vendor_error` | 上游 vendor 5xx / 网络 |
| 503 | `housepool_unavailable` | kiro.rs 暂时不可用 |
| 500 | `internal` | 服务端问题 |

## 未定 / 阶段后期补的

- API 版本策略（前缀 `/api/v1/*` vs 头 `X-API-Version`）—— **阶段 1 落码时确定**
- 限流细粒度（按 IP / 按乘客 / 按 API key）—— **阶段 1 落码时确定**
- CORS 策略 —— **阶段 1a 前端联调时确定**
- OpenAPI schema 自动生成 —— **阶段 1a 完成后生成**
- 管理端 `/api/admin/*` —— **阶段 3 才写**
