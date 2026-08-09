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

### handoff · 三段式 token 交付（P0 修补 · 见 `09-transactions.md § 4`）

**问题**：单次 HTTP handoff 无法保证明文可靠交付（响应中途断线，我方已 DELETE 但客户端没收到明文）。

**解决**：**三段式 token**——先发 token，客户端 fulfill 拿明文（TTL 内可重试），最后 confirm 才 DELETE：

```
1. POST /api/me/handoff
   请求：{ credential_ids: [...] }
   返回：{ download_token: "32hex", expires_at: "5min later" }
   （不返回明文；credential 保留 disabled=true）

2. GET /api/me/handoff/{token}
   TTL 内可多次调用（网络断了重试用），每次返回 credential 明文四件套
   （credential 仍在 housepool）

3. POST /api/me/handoff/{token}/confirm
   客户端**显式确认收到** → 触发 housepool DELETE + credential_ledger 标 handed_off
   （幂等：多次 confirm 返回同状态）
```

**TTL 过期未 confirm** → janitor 恢复 credential（保留 disabled=true 状态），用户可重新 `POST /me/handoff`。

**客户端集成**：
```javascript
const { download_token } = await POST('/me/handoff', { credential_ids })
const { keys } = await GET(`/handoff/${download_token}`)  // 断线可重试
displayToUser(keys)
await POST(`/handoff/${download_token}/confirm`)
```

**其它端点**（购买 / 派进车 / 派 passengerpool）**没有明文问题**，字节一致重放规则依然适用。

---

## 1. 账号

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| POST | `/api/register` | 注册（邮箱 + 密码 · 密码 Argon2id 哈希 · Go 自建） | 1a |
| POST | `/api/login` | 登录（返 session cookie） | 1a |
| POST | `/api/logout` | 登出（清 session） | 1a |
| GET | `/api/me` | 当前账号信息（原 `/api/me/profile` · 改名对齐前端 `/me` 页面） | 1a |
| POST | `/api/me/password` | 改密码（会话鉴权强制） | 1a |

### `GET /api/me` 响应示例

```json
{
  "id": "01H8...",
  "username": "alice",
  "email": "alice@example.com",
  "email_verified": false,
  "created_at": "2026-08-07T12:00:00Z",
  "tier": "insider",
  "invited": true
}
```

**`tier`**（`decisions §8.39`）· 三档定价：

| 值 | 含义 | vendor 显示 | 加价链 |
|---|---|---|---|
| `retail` | 零售 · 散客 · 无系统邀请码 | Vendor 0N 匿名编号 | 全套加价 |
| `wholesale` | 批发 · TG/Discord 社群 · 社群码注册 | vendor 真名 | 免区域附加费 |
| `insider` | 同行 · 同行群邀请制 · 同行码注册 | vendor 真名 | 免 vendor + 区域附加费 |

**`invited`**（**兼容字段** · 下版删）：等同 `tier != 'retail'` · 保留供旧前端兜底 · 新前端一律读 `tier`。

**余额不在这里** —— 走 `GET /api/me/wallet`（前端顶栏积分 pill 和钱包页都用那个，避免两处返回同一个数字导致不一致）。

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
| GET | `/api/promos` | **公开** · 顶部跑马灯活动位（config.promo.items · 过期条目服务端不下发） | 1c |
| POST | `/api/me/redeem` | 兑换码 | 1b |
| GET | `/api/me/invite` | 我的个人邀请码 + 邀请数 + 剩余手续费减免次数 | 1c |
| POST | `/api/me/community-code` | 补绑社群码（已注册用户拿社群身份）· 404 码无效 · 409 已绑过 | 1c |
| POST | `/api/me/topup` | 起充值单（走 waffo） | 1b |
| GET | `/api/me/topup/{order_id}` | 查充值单 | 1b |
| GET | `/api/me/topup-orders` | 我的充值单历史（分页） | 1b |

### `GET /api/me/ledger` `?type=` 枚举

**对外只有 6 个类型**（跟 `web/src/types/index.ts` 的 `LedgerType` 一致）：

- `topup` · 充值到账
- `spend` · 拉号扣款（内部的 key_cost / service_fee 等分层**合并展示**，对外不出计费链结构 —— CLAUDE.md §0.1）
- `redeem` · 兑换码
- `refund` · 一般退款
- `warranty_refund` · 质保退款
- `share` · **车友份额清算**（1c 加 · `decisions §8.23`）· 金额正负表示方向：正 = 车友分摊回款给我（我派号进车）· 负 = 我买入别人派进来的号份额
  - 为什么单开一类：这是**乘客之间**的内部转移·既不是充值（没花真钱）也不是消费（钱没出系统）· 并进 topup/spend 会让对账算错
  - 内部对应 `wallet.ReasonShareIncome` / `ReasonShareExpense`

**内部记账**仍按 `wallet.Reason` 的多种分类落库（对账 / 分项统计用），api 层做映射收敛。

### `POST /api/me/redeem`

```json
// req
{ "code": "KRC-XXXX" }
// resp（对齐 web/src/types RedeemResult · CLAUDE.md §0.1 TS = 权威）
{ "credits": 100000000, "memo": "兑换码 KRC-XXXX", "balance_after": 195000000 }
```

**幂等**：`X-Idempotency-Key` 可选。带 key 时重放返回同一 body；不带 key 走 Store 的条件 UPDATE 单一乘客 replay 兜底。

### `POST /api/me/topup`

```json
// req · credits = 想充值的目标积分（净到账）· 通道费 5% 加在本金上（CLAUDE.md §1.4）
{ "credits": 100000000, "channel": "waffo" }
// resp（201 Created · 对齐 web/src/types TopupOrder）
{
  "order_id": "01H8...",
  "qr_payload": "https://waffo.example/order/01H8...",  // 前端渲染 QR
  "paid": 105000000,                                     // credits + channel_fee
  "credits": 100000000,                                  // 净到账
  "expires_at": "2026-08-07T12:15:00Z",
  "status": "pending",                                   // pending | paid | failed
  "created_at": "2026-08-07T12:00:00Z"
}
```

**通道费 pass-through**（详见 CLAUDE.md §1.4）：乘客想充 100 积分 → 通道费 = 100 × 5% = 5 积分 → 折 105 CNY → 按 7 CNY/USD 汇率显示 **15 USD** 走 waffo。内部账本记两笔：`recharge +105` + `channel_fee −5`（净 +100 积分到 balance）。

## 4. Bus（拼车主入口）

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| GET | `/api/me/buses` | 我参与的所有 bus | 1a |
| POST | `/api/me/buses` | 建 bus | 1a（`kind: single`）→ 1c（`anon`）→ 2a（`team`） |
| GET | `/api/me/buses/{bus_id}` | bus 详情 + 号池状态 | 1a |
| POST | `/api/me/buses/{bus_id}/join` | 加入 anon bus（匿名撮合） | 1c |
| POST | `/api/me/buses/join-by-invite` | **拼车码**加入一辆车（对外叫「拼车码」· 跟个人邀请码区分 · §8.38） | 1c |
| POST | `/api/me/buses/{bus_id}/leave` | 退出 bus | 1a |
| DELETE | `/api/me/buses/{bus_id}` | 解散 bus（创建人 or 最后一位成员） | 1a |
| POST | `/api/me/buses/{bus_id}/pull` | 给这个 bus 拉一次号 | 1a |
| PUT | `/api/me/buses/{bus_id}/strategy` | 该车的补车策略（`decisions §8.6` 跟车绑） | 1a |
| GET | `/api/me/buses/{bus_id}/credentials` | 该 bus 的号列表（含存活状态 + 用量） | 1a |
| GET | `/api/me/buses/{bus_id}/pulls` | 该 bus 的拉号历史（分页） | 1a |
| GET | `/api/me/buses/{bus_id}/stats` | 该 bus 号池的聚合统计（跨窗口） | 1d |
| PUT | `/api/me/buses/{bus_id}/members/{pid}` | 挂起 / 解挂成员（`§8.26`） | 2a |
| DELETE | `/api/me/buses/{bus_id}/members/{pid}` | 移除成员（车主有权 · `§8.36`）· 剩余成员 share_pct 均分重算 | 1c |
| POST | `/api/me/buses/{bus_id}/invite-code` | 重新生成**拼车码**（旧码和旧链接立即失效） | 1c |

**没有 `GET /buses/{id}/members`** —— 成员数组**内嵌在 `GET /api/me/buses/{bus_id}` 的 `members[]`** 里。理由：车详情的成员 tab 切过去就该有数据，不值得为它多一次请求 + loading 态；成员数量天然很小（1 人车 1 条、多人车个位数）。1 人车 `members` 只有 owner 一条，`share_pct=100`。

### `POST /api/me/buses`

```json
// req
{ "name": "我的号池", "kind": "single" }  // kind: single | anon | team
{ "name": "程序员拼车", "kind": "team", "invite_code_hint": "friends" }  // 阶段 2a
// resp
{ "bus": { "id": "01H8...", "kind": "single", "invite_code": null, "members": [...] } }
```

### `POST /api/me/buses/{bus_id}/pull`

**响应形状跟 `POST /api/me/pull` 一致**（CLAUDE.md §0.1 · 对外只暴露单价 / 总额 / 服务费；内部加价链分层不出）。

```json
// req
{
  "count": 5,
  "vendor_id": "kiro91",        // optional · 乘客偏好，服务端可否决
  "constraints": {                // optional
    "max_unit_price": 30000000
  }
}
// resp
{
  "pull_round_id": "01H8...",
  "vendor_id": "kiro91",
  "purchased": 5,
  "credential_ids": ["01H8a", "..."],   // 我方 UUID
  "unit_price": 21000000,       // microunit · 单价（含所有内部加价，一口价）
  "service_fee": 5000000,       // microunit · 服务费一项显式列出
  "total_debit": 105000000,     // = unit_price × purchased
  "balance_remaining": 895000000
}
// resp (错误)
{ "code": "insufficient_balance", "message": "余额不足 X" }
```

**服务端裁定**，客户端 `constraints.max_unit_price` 只是软偏好。响应体不下发内部加价明细。

**幂等**：`X-Idempotency-Key` 必填（32 hex）。

## 5. 单独拉号（次入口 + 拉号记录）

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| POST | `/api/me/pull` | 单独拉一批号（不指定 bus） | 1a |
| GET | `/api/me/pull-records` | 我的拉号记录（`Paged<Credential>` · 每号一条 · `?history=1` 含已死号） | 1a |
| GET | `/api/me/pull-records/{record_id}` | 单条详情（返回 `Credential` 单体） | 1a |
| POST | `/api/me/pull/estimate` | 提取确认窗的费用预估（**不下单**）· 含优惠码折后价 | 1a |
| GET | `/api/me/pull/events` | 提取历史（每次拉号一条） | 1a |
| GET | `/api/me/assign/events` | 派发历史（每次派动作一条 · 可展开看每个号） | 1a |
| POST | `/api/me/pull-records/assign` | 派去向 · **只管进车 / 推自己号池** | 1a |
| POST | `/api/me/handoff` | 拿走 ① 发 token（见 §5b） | 1a |
| GET | `/api/me/handoff/{token}` | 拿走 ② fulfill 取明文 | 1a |
| POST | `/api/me/handoff/{token}/confirm` | 拿走 ③ 确认 → 触发 DELETE | 1a |

### `POST /api/me/pull`

```json
// req
{ "count": 10, "vendor_id": null }   // vendor_id null = 系统选
// resp
{
  "pull_round_id": "01H8...",
  "vendor_id": "kiro91",
  "purchased": 10,
  "credential_ids": ["01H8a", "01H8b", "..."],   // 我方 UUID · 派发 / handoff 都用这个
  "unit_price": 21000000,           // microunit · 单价（含所有内部加价，一口价）
  "service_fee": 10000000,          // microunit · 服务费一项显式列出
  "total_debit": 210000000,         // = unit_price × purchased
  "balance_remaining": 790000000
}
```

**对外只暴露三项金额**（CLAUDE.md §0.1）：`unit_price` / `service_fee` / `total_debit`。号价、vendor 附加费、区域附加费、单次议价、附加能力等内部分层**不出响应体** —— 想调结构不需要改契约。

**ID 口径**：`credential_id` = 我方 `credential_ledger.id`（UUID v7 · TEXT）· 所有对外 API 都用这个。上游 id（如号池 u64）**不进响应体**（CLAUDE.md §0.1）。

**服务端裁定**，客户端传什么都不信。请求里可以传 `vendor_id`（乘客偏好），但服务端可以否决。

### `GET /api/me/pull-records` 返回

```json
{
  "items": [
    // 对齐 web/src/types Credential · UI status 二态（alive | dead）
    {
      "id": "01H8a...",
      "vendor_id": "kiro91",
      "status": "alive",
      "key_masked": "ksk_live_xxxx…vn6",
      "region": "us-east-1",
      "credits_used": 3200000,
      "pulled_at": "2026-08-07T12:00:00Z",
      "warranty_until": "2026-08-07T12:15:00Z",
      "dead_at": null,
      "pushed_at": null,
      "push_failed": false,
      "push_error": null,
      "source_pull_round_id": "01H8..."
    }
  ],
  "total": 42, "page": 1, "page_size": 50, "pages": 1
}
```

**默认只返存活号**（`status=alive`）· 带 `?history=1` 时把已死 / 已交出的号也带上（售后追溯）。

### `POST /api/me/pull-records/assign` （派去向 · 只管进车 / 推号池）

**`destination` 取值**：线上契约用 `into_bus` / `push_pool`；`pending_assignment.target` 存 `to-bus` / `to-passengerpool`（历史命名）。**映射在 handler 里做一次**，别让两套值互相渗透。

```json
// req · 一次一个去向（界面上就是"勾一批 → 点一个动作"）
{
  "credential_ids": ["01H8a", "01H8b"],
  "destination": "into_bus",       // into_bus | push_pool
  "bus_id": "01H8..."              // destination=into_bus 时必填
}
// resp
{ "assigned": 2, "errors": [] }    // 部分失败时 errors 列出哪几个
```

**修订说明**（2026-08-08）：本节原来的形状是 `assignments[]` 混合三种去向、并在响应里直接返 handoff 明文。两处都改了：

1. **不做混合去向** —— 一次请求一个 `destination`。`CLAUDE.md §2` 已废「混合上车 / allocation 组件」，原形状跟它冲突；界面上也是"勾一批 → 点一个动作"，没有混合入口
2. **handoff 不走这个端点** —— 明文不能在普通响应里返回（响应断线 = 号已删但明文丢失）。走 §5b 三段式

**顺序**：先做外部动作（推 passengerpool 完成），再改 housepool 状态 —— 防止"号推出去了但状态没改"（`CLAUDE.md §7.1`）。

### 5b. 拿走 handoff · 三段式（`09-transactions §4` P0-3）

| Method | Path | 阶段 | 说明 |
|---|---|---|---|
| POST | `/api/me/handoff` | ① | 发 `download_token`（TTL 5 min）· **不返明文** · 号仍在池里 |
| GET | `/api/me/handoff/{token}` | ② | fulfill 取明文 · **TTL 内可反复取**（断线重试） |
| POST | `/api/me/handoff/{token}/confirm` | ③ | 确认收到 → 这时才 DELETE + 台账标 `handed_off` |

```json
// ① POST /api/me/handoff
// req
{ "credential_ids": ["01H8h", "01H8i"] }
// resp
{ "download_token": "a3f2…（32 hex）", "expires_at": "2026-08-08T12:05:00Z" }

// ② GET /api/me/handoff/{token}
{
  "keys": [
    { "credential_id": "01H8h", "key": "ksk_live_…", "vendor_id": "kiro91", "account": "aws-…@kiro.tmp" }
  ]
}

// ③ POST /api/me/handoff/{token}/confirm
{ "ok": true }
```

**为什么非要三段**：号交出去不可逆。一次性做法（删号 + 同响应返明文）在响应断线时 → 号已删、明文没收到、**钱白花**。三段式把"取明文"和"删号"分开，②可以重试，③之前号一直在。

**明文永不落我方库** —— 每次 fulfill 从 housepool 实时读（`pending_handoff` 表没有明文字段）。

**前端映射**（`AssignModal`）：点「下载拿走」→ ①+② 一起做完并展示明文 → 用户点「我已保存 · 确认拿走」→ ③。点「返回」= 不 confirm，号留在池里可以重来。**用户感知只有两步**。

**原 `POST /pull-records/{id}/handoff-init` 已废** —— 改成 `POST /me/handoff` 不绑单条 record（界面上是批量勾选，token 天然对应一批号）。

## 6. 号池 / credential 查询（跨 bus + 拉号记录）

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/me/credentials` | 我名下所有活的号（跨 bus / 拉号记录 / 已推 passengerpool 的） |
| GET | `/api/me/credentials?history=1` | 含死号 |
| GET | `/api/me/credentials/{id}` | 单号详情：进池时间 / 死亡时间 / 用了多少 / 平均积分消耗 / 并发（若有） |

**这是"我名下号"的入口**。`handoff` 出去的号不在这里（已离开 housepool）。

### 单号详情返回字段（`GET /api/me/credentials/{id}`）

**对外只暴露乘客做决策需要的字段**（CLAUDE.md §0.1）。内部的 group 命名、死亡来源、号池 id 等一律不出。乘客视角只有：号在哪辆车 / 是否存活 / 用了多少。

```json
{
  "credential": {
    "id": "01H8...",
    "vendor_id": "kiro91",
    "bus_id": "01H...",              // 属于哪辆车；单独拉号（提取入口）为 null
    "pulled_at": "2026-08-01T10:00:00Z",
    "alive": true,                    // false = 已死（不告诉乘客是谁探到的死）
    "dead_at": null,                  // null = 存活；有值 = 已死
    "usage": {
      "calls_24h": 320,
      "calls_7d": 1850,
      "input_tokens_24h": 82000,
      "output_tokens_24h": 15000,
      "errors_24h": 4,
      "credits_used_total": 4700000,  // microunit
      "avg_credits_per_day": 1560000,
      "concurrency_avg": null
    }
  }
}
```

**`concurrency_avg`** 上游未提供并发读端点前返回 `null`，UI 显示 `—`。

**故意不返回的字段**（CLAUDE.md §0.1 · §12.5-12.6）：
- `current_group` / `record-<pid>` / `bus-<id>` group 名 —— 号池实现细节，乘客不需要
- `death_source` —— 谁探到的死用户不关心；只关心死了
- `kiro_rs_id` —— 上游 id，只在跟 kiro.rs 对账时内部用

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
      "service_fee_total": 5000000,   // 按 share_pct 在 alice/bob 之间分摊
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

**两层，别混**（`decisions §8.27`）：

| 层 | 端点 | 管什么 |
|---|---|---|
| **全局** | `GET/PUT /api/me/strategy` | ① 硬上限（每日轮数 / 每日消费 / **单价**）② 建新车的默认值 |
| **每车** | `PUT /api/me/buses/{id}/strategy` | 那辆车的自动补车（watermark / 车级限额）· `decisions §8.6` |

前端入口：全局在「设置 › 拉号偏好」· 每车在「车详情 › 补车策略」tab。

### `GET /api/me/strategy`

```json
{
  "max_unit_price": 30000000,       // 硬上限 · 超了拒绝拉号 · null = 不限
  "daily_round_limit": 20,          // 硬上限 · 跨所有车累加 · null = 不限
  "daily_spend_limit": 500000000,   // 硬上限 · null = 不限
  "per_round_count": 3,             // 新车默认值
  "preferred_vendor": null,         // 新车默认值 · null = 让系统比价
  "default_zone": "auto",           // 新车默认值
  "used_today": { "rounds": 6, "spend": 45000000 }   // 只读 · UI 画用量进度条
}
```

`PUT` 收上面除 `used_today` 外的字段（部分更新）。

**上限的执行点**（1b 就要真生效，不是存着等 1d）：
- `daily_*` 在 `POST /me/pull` 和 `POST /me/buses/{id}/pull` 入口校验 · 超了返 `daily_limit_reached`
- `max_unit_price` 在**下单前**比价阶段校验 · 超了返 `price_over_cap`（带 `cap` 和 `current` 便于前端提示"超了多少"）
- 跟车级同名字段取**更严**的（AND）
- **提取 key 只受全局管** —— record group 没有车级限额

## 8. 下游配置

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| GET | `/api/me/downstream` | 当前配置（含 `rules` + 推送统计 + `passengerpool_token_masked`） | 1a |
| PUT | `/api/me/downstream/passengerpool` | 配 passengerpool url + token + 4 条 rules（partial update） | 1a |
| POST | `/api/me/downstream/passengerpool/test` | 探活目标号池 URL | 1a |
| GET | `/api/me/downstream/webhook` | webhook 配置（含 `secret_masked` · **明文不返回**） | 1a |
| PUT | `/api/me/downstream/webhook` | 配 URL / events · **不接收 secret** | 1a |
| POST | `/api/me/downstream/webhook/secret` | 轮换 secret · **明文只在这一次返回** | 1a |
| POST | `/api/me/downstream/webhook/test` | 发一条测试 webhook · 落 delivery 台账 | 1a |
| GET | `/api/me/downstream/webhook/deliveries` | 投递日志（数组 · 不是 Paged 信封） | 1a |

### `PUT /api/me/downstream/passengerpool`

```json
// req · 三项都可选，只带哪个改哪个
{
  "passengerpool_url": "https://my-kiro-rs.example.com",
  "token": "sk-...",              // 空字符串 = 不改现有 token · 明文加密后落库
  "rules": {
    "push_on_pull": true,
    "resync_on_dead": true,
    "retry_on_failure": true,
    "bus_only": false
  }
}
// resp
{ "ok": true }
```

### `POST /api/me/downstream/passengerpool/test`

发一次 3s timeout 的 GET 探活。**不发敏感字**（只关心 TCP + TLS + HTTP 层能不能通）。

```json
// resp
{ "ok": true, "latency_ms": 187 }
// or
{ "ok": false, "latency_ms": 3005, "error": "连不上目标地址" }
```

### `PUT /api/me/downstream/webhook`

```json
// req
{ "url": "https://your-server.com/hook", "events": ["round.completed", "credential.dead"] }
// resp（不返 secret · secret 只在 POST /secret 轮换那一刻返回）
{ "ok": true }
```

### `POST /api/me/downstream/webhook/secret`

```json
// resp · 前端拿到后弹一次性对话框让用户手抄，关闭后就再也拿不到（GET 只有 mask）
{ "secret": "64-hex-signing-secret" }
```

**为什么 secret 独立端点**（CLAUDE.md §0.1 + §11）：明文只在轮换那一刻返回一次。PUT webhook 用来改 URL / events，不接受 secret 明文进来（防用户把明文塞进请求体 → 日志泄漏）。

## 9. Vendor 状态

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| GET | `/api/vendors` | 6 家 vendor 列表 + 当前 stock / price 快照 | 1a |
| GET | `/api/vendors/stock` | **聚合**总可拉数 + 按 vendor 明细（顶栏库存徽标） | 1a |
| GET | `/api/vendors/{vendor_id}/stock` | 单家实时快照 · 支持 `?coupon_code=` | 1a |
| GET | `/api/vendors/stats` | 概览「Vendor 监测」表 + 占比 | 1a |
| GET | `/api/vendors/auto-pick` | auto 档的推荐结果（推哪家 + 价 + 库存） | 1a |
| GET | `/api/vendors/prices?days=&zone=` | 价格走势（**轮次级**历史 · `decisions §8.22`）· 1a stub 返 `{trends: []}` | 1a stub → 1d 补数据源 |
| GET | `/api/vendors/{vendor_id}/history` | 单家历史 | 1d |
| GET | `/api/vendors/{vendor_id}/health` | 单家健康（平均寿命等） | 1d |

**不带 `/me` 前缀** —— vendor 是公共数据。但**返回的价格是按调用者个性化的**：

| 调用者 | 看到的 |
|---|---|
| 有注册邀请码 | vendor **真名** + 不加价 |
| 无邀请码 | `AWS-Q Kiro Vendor 0N` **编号** + 默认加价 |
| 无邀请码但带 `?coupon_code=` | 编号（**码不解锁真名**）+ 本次免加价 |

**只下发最终价**，不下发原价和加价明细（`decisions §8.20`）。所以这几个端点**要鉴权**（拿不到身份就没法定价）—— 全部走 `RequireAuth`，别用 vendors 作为"未登录可看的公共接口"。

**`auto-pick` 为什么必须 1a 有**：散客默认就走 auto 档，界面上 auto 项要显示"推荐到哪家 + 单价 + 库存 + 预估费用"才能下单（`decisions §8.20`）。没有它，无邀请码用户根本下不了单。

**`/api/vendors/prices` 1a stub · 1d 补数据源**：轮次级历史要先采集（1d 起走 `vendor_round` + `vendor_lifespan_snapshot`）。1a 返 `200 OK` + `{trends: []}` 让价格走势页渲染空态，别返 501（前端会白屏）。

## 9b. 概览页数据

原契约整段漏了这三个，但概览是登录后的首页 —— 它们返 501 就是白屏。

| Method | Path | 说明 | 阶段 |
|---|---|---|---|
| GET | `/api/me/overview?range=` | KPI 4 项 + 3 业务线汇总 | 1a |
| GET | `/api/me/trend?range=&metric=` | 趋势序列 · `metric ∈ {credits, pulls, lifespan}` | 1a |
| GET | `/api/me/activities?range=` | 活动记录**混流**（`decisions §8.4`） | 1a |

`range ∈ {today, 7d, 30d, 90d}`。

**`/me/trend` 的 scope**：可选 `?bus_id=` 或 `?vendor=`（二选一，不同时传）· 车详情「数据」tab 和 vendor 下钻都复用这个端点，不另开。

**`/me/activities` 混流**：入车 / 提取 打平按时序排，每条带 `kind` 让前端上 tag chip。不分组（`§8.4` 明确否决过"最近拼车 / 最近提取"两小节）。

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

## 响应形状 · 以前端类型为准的那几个

下面这些端点是 **1a 必做**，但本文档没写完整响应形状。**权威定义在 `web/src/types/index.ts`** —— 前端已经按那个形状跑通并验证过整套 UI，后端照它实现即可。

| 端点 | 形状 | 参考实现 |
|---|---|---|
| `POST /me/pull/estimate` | `{ key_cost, single_pull_fee, service_fee, total }` | `hooks.ts useEstimate` |
| `GET /me/pull/events` | `Paged<ExtractEvent>` | `types` `ExtractEvent` |
| `GET /me/assign/events` | `Paged<AssignEvent>` | `types` `AssignEvent` |
| `GET /vendors/stock` | `StockSummary` | `types` `StockSummary` |
| `GET /vendors/{id}/stock` | `VendorStock` | `types` `VendorStock` |
| `GET /vendors/stats` | `{ stats: VendorStat[], share: VendorShare[] }` | `types` |
| `GET /vendors/auto-pick` | `AutoPickResult` | `types` `AutoPickResult` |
| `GET /me/overview` | `Overview` | `types` `Overview` |
| `GET /me/trend` | `TrendPoint[]` | `types` `TrendPoint` |
| `GET /me/activities` | `Paged<Activity>` | `types` `Activity` |
| `GET /me/strategy` | `GlobalStrategy` | `types` `GlobalStrategy` |

**mock 实现可以直接参考** `web/src/mocks/handlers.ts` —— 那里每个端点都有能跑的返回值，字段齐全。

**为什么不在这里重抄一遍**：抄一份就多一处会漂移的地方。类型定义是**可执行的契约**（TS 编译器会检查前端有没有用错），Markdown 表格不是。有出入时**以 `types/index.ts` 为准**。

**唯一例外**：金额字段一律 **整数 microunit**（`types` 里 `Money = number` 看不出这个），命名一律 snake_case（前端 `types` 已经是 snake_case，直接对应）。

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
| 409 | `price_over_cap` | 单价超过全局上限（`§7`）· 带 `cap` / `current` 便于提示"超了多少" |
| 409 | `daily_limit_reached` | 今日轮数或消费达上限 · 带 `limit` / `used` |
| 404 | `token_expired` | handoff 的 `download_token` 过期（TTL 5 min）· 重新发起即可 |
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
| 502 | `upstream_error` | 上游临时不可用（vendor 或号池；对外不区分具体来源 · CLAUDE.md §0.1） |
| 503 | `service_unavailable` | 我方服务暂时不可用 |
| 500 | `internal` | 服务端问题 |

**内部术语禁止上错误码**（`housepool_unavailable` 之类原本在这，已改）：错误 code 是对外契约的一部分，暴露 `housepool` / `kiro.rs` 等于告诉用户我方依赖哪家上游 —— 换供应商就变成一次破坏性变更。日志和监控里保留内部原因即可。

## 未定 / 阶段后期补的

- API 版本策略（前缀 `/api/v1/*` vs 头 `X-API-Version`）—— **阶段 1 落码时确定**
- 限流细粒度（按 IP / 按乘客 / 按 API key）—— **阶段 1 落码时确定**
- CORS 策略 —— **阶段 1a 前端联调时确定**
- OpenAPI schema 自动生成 —— **阶段 1a 完成后生成**
- 管理端 `/api/admin/*` —— **阶段 3 才写**
