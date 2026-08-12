# Vendor: 91kiro (`api.91kiro.com`)

## 1. 基础信息

| 项 | 值 |
|---|---|
| Base URL | `https://api.91kiro.com` |
| 官方文档 | `GET /api/docs`（Markdown，公开可读，21 KB） |
| 抓取日期 | 2026-08-07 |
| 站点标题 | Kiro Key 自助平台 |
| 存活探活 | `GET /health` = 200；`GET /api/docs` = 200 |
| 前端 bundle | `/assets/*`（Vite 构建） |

文档自述：**"这份文档可以整份粘给 AI，让它替你写客户端。全部字段、错误码、幂等规则与签名算法都在里面。"** 因此本档案完全按上游文档的节次和用词组织。

## 2. 鉴权

两种方式，任选其一：

| 方式 | 用法 | 适用 |
|---|---|---|
| API 令牌 | `X-API-Key: usr-…` **或** `Authorization: Bearer usr-…` | 脚本、服务端集成 |
| 会话 cookie | 浏览器登录后自动携带 | 网页界面 |

**脚本一律用 API 令牌**。cookie 方式的写请求需要额外的 CSRF 头，脚本不必处理。

- 令牌前缀：`usr-`
- 令牌只在注册时明文出现一次，库里只存哈希
- **换令牌必须在网页上做**（`POST /api/my/api-key/rotate` 只接受会话鉴权，用 API 令牌调回 `403 session_required`）
- 所有 `/api/*` 响应带 `Cache-Control: no-store`

## 3. 概念 / 术语（上游自述）

| 术语 | 含义 |
|---|---|
| 母号 | 一个 AWS 账号的 AK/SK，所有 key 的来源 |
| 车次 | 一个母号一次性产出的一批号及其 key。母号死则整车死 |
| 公共车 | 开成"发库存"的车，产出进公共库存、全站可买。母号可以是平台的，也可以是客户贡献的 |
| 自己车 | 开成"留自用"的车，只有母号归属人能领，且免费。不进公共库存 |
| 积分 | 账户余额。1:1 充值，领 key 时扣 |
| 持有上限 | 名下同时持有的存活 key 上限，达到就买不了。手里的号失效后额度自动腾出 |
| 补拉 | 用历史 `order_id` 重放订单，原样取回当时那批 key。不重复扣费 |

## 4. 计费规则（原文摘录）

- **单价按整车产出数量查阶梯表**，产出越多越便宜。单价在开号完成那一刻冻结，之后调价不影响已有车次。
- **只有你调购买接口领取时才扣积分**。没有任何在零动作情况下扣钱的路径。
- **共享库存，先到先得**。每辆公共车产出后直接进公共库存，不为任何人预留。
- 单价按每把 key 自己的**区域**定（同一辆车的 us 与 eu 可以不同价），响应里逐把给出实付。
- 余额不足直接返回 `insufficient_balance`，**不会部分成交**。
- 超过持有上限返回 `purchase_cap_reached`，也是整单失败。
- **免费只对"留自用"的车成立**：母号 `private` 时自己领这批 key 不扣积分，响应里带 `"free": true` 与免费原因。
- 母号开成"发库存"时**不免费**——那批货进公共库存、全站可买，母号主自己去买它跟别人一样按正常单价扣费。贡献母号的回报走"按产出返积分"（产出那一刻结清），不是"买它免费"。

### 4.1 质保

- 领取后有质保期（默认 10 分钟，运营方配置，`GET /api/my/stock` 的 `warranty_minutes` 是当前值）
- 质保期内车次被判死，为该批 key 付的积分**自动全额退回**，无需申请，并推 `warranty_refund` webhook
- 倒计时结束后失效不再退
- 每把 key 的质保窗口在**交付那一刻**固定下来，事后运营方改配置不影响已交付订单
- 免费领的（留自用车）没有可退积分，不计质保（`warranty_until` 为空）
- 退款流水类型 `warranty`；若该 key 来自别人投放，那笔钱同时从投放者收入冲回（他那边是 `clawback`）

## 5. 账号 / Profile / Settings

### `GET /api/my/profile`

```json
{
  "profile": {
    "id": "…", "username": "alice", "role": "user",
    "balance": 1400, "spent": 600, "earned": 0,
    "max_keys_held": 20, "hold_cap_effective": 10, "keys_held": 7,
    "api_key_prefix": "usr-1a2b3c4d",
    "webhook_private_url": "", "webhook_public_url": "https://…",
    "created_at": "2026-07-30T12:00:00Z", "last_login_at": "…"
  },
  "auth_mode": "api_key"
}
```

### `PUT /api/my/settings`

```json
{ "max_keys_held": 20 }
```

`max_keys_held` 范围 0–1000，是**持有上限**：名下存活 key 达到它就买不了，`0 = 不限`。手里的号被判死或吊销后额度自动腾出，不需要任何动作。

运营方还有全局硬顶，你设的值与它取更严的那个：

| 字段 | 含义 |
|---|---|
| `max_keys_held` | 你自己设的值 |
| `hold_cap_effective` | 叠加全局硬顶后**真正生效**的上限（0 = 不限） |
| `keys_held` | 名下当前还活着的 key 数 |

买之前用 `keys_held` 和 `hold_cap_effective` 判一下，能省一次注定失败的下单。

### `POST /api/my/password`

```json
{ "old_password": "…", "new_password": "…" }
```

成功后**所有设备的登录状态失效**（API 令牌不受影响）。

### `POST /api/my/api-key/rotate`

无请求体。返回新令牌，旧令牌立即失效。

**只接受会话鉴权（网页登录后调用）。** 用 API 令牌调它回 `403 session_required`。理由：否则拿到你泄露令牌的人可以用它换一把新的、把你自己锁在门外。**脚本里不要写这个接口。**

## 6. 库存与车次

### `GET /api/my/stock`

```json
{
  "stock": {
    "public_available": 12,
    "my_private": 0,
    "my_keys": 27
  },
  "zones": [
    { "zone": "us", "region": "us-east-1",    "available": 8, "unit_price": 30 },
    { "zone": "eu", "region": "eu-central-1", "available": 4, "unit_price": 10 }
  ],
  "max": 12,
  "min_per_order": 1,
  "max_per_order": 200,
  "warranty_minutes": 10
}
```

| 字段 | 含义 |
|---|---|
| `public_available` | 公共车当前可买余量合计，先到先得 |
| `my_private` | 你自己车里可领的数量（免费） |
| `my_keys` | 你已经领走的 key 总数 |
| `zones` | 各区可购量与单价。按 `us`、`eu` 固定顺序给全，没货的区 `available` 为 0 |
| `zones[].unit_price` | 该区当前**最便宜的一档**（估价用）；实扣以 purchase 的 `total_credits` 为准（同区多车可能混价） |
| `max` | 当前一次性最多能提的数量（= 公共余量，封顶 200）。轮询兜底时先看它 > 0 再提货 |
| `min_per_order` / `max_per_order` | 单次提货的数量上下限（1 / 200） |
| `warranty_minutes` | 当前质保时长（分钟），0 表示未开启 |

### `GET /api/my/rounds`

只返回与你有关的车次（你的母号开的，以及你从中买过 key 的）。

```json
{
  "rounds": [{
    "id": "…", "mother_id": "…", "owner_id": "…",
    "visibility": "public", "scope": "platform", "state": "live",
    "keys_total": 20, "unit_price": 30,
    "launched_at": "…", "died_at": "", "death_reason": "",
    "is_mine": false
  }],
  "total": 1
}
```

车次状态：`preparing` 准备中 → `standby` 备车 → `live` 运行中 → `dying` 复核中 → `dead` 已死。另有 `failed`（开号失败）、`scrapped`（发车前母号已死）。

### `GET /api/my/stock/rounds`

比 `/api/my/rounds` 多给一列 `current_price`（按存活时长降价的**现价**）· 用于价格趋势展示。
建议客户端**每 60 秒轮询一次** · `remaining > 0` 就去 purchase。

vendor 原文：
> `GET /api/my/stock/rounds` 每行则多给一个 `current_price`。两者都由服务端用与计费
> 同一个公式算出，取数那一刻有效。

跟 `/api/my/rounds` 的区别：`rounds` 里的 `unit_price` 是**基准价**（`base_price`），
`stock/rounds` 里 `current_price` 是**已按存活时长现算过的价** —— 展示实际扣费应该用后者。

### `GET /api/my/keys`

已领走的 key 列表。**只给前缀，不给正文** —— 要完整正文走 §7 的补拉。

## 7. 拉号 / 补拉

### `POST /api/my/purchase`（等价别名 `POST /api/me/purchase`）

```json
{ "count": 5, "zone": "us", "client_order_id": "0a1b2c3d4e5f60718293a4b5c6d7e8f9" }
```

`client_order_id` 是 **32 位十六进制**幂等键，也可用 `Idempotency-Key` 请求头传（两者都给且不一致会 400）。不传则由服务端生成，但那样无法重放。

`count` 范围 1–200。

`zone` **按区严格隔离，不跨区补货**：

| 传值 | 行为 |
|---|---|
| 不传 | **默认只从美国区取号**；美国区缺货就返回缺货，不会用欧洲区顶上 |
| `"us"` | 只拿美国区（us-east-1） |
| `"eu"` | 只拿欧洲区（eu-central-1） |
| 其它值 | 直接 **400 `bad_zone`**，不会静默按美国区处理 |

想要欧洲区的号，**必须显式传 `zone: "eu"`**；只有这一种方式能拉到欧洲区。

响应：

```json
{
  "client_order_id": "0a1b2c3d4e5f60718293a4b5c6d7e8f9",
  "order_id": "…",
  "zone": "us",
  "purchased": 5,
  "unit_price": 30,
  "total_credits": 150,
  "remaining": 4500,
  "keys": [{
    "id": "…", "round_id": "…",
    "key": "ksk_…",
    "account": "user@example.com",
    "password": "…",
    "issuer_url": "https://d-xxxx.awsapps.com/start",
    "free": false, "paid": 30,
    "warranty_until": "2026-08-01T12:34:56Z"
  }],
  "free_count": 0,
  "warranty_until": "2026-08-01T12:34:56Z",
  "warranty_minutes": 10
}
```

**每把 key 的 payload 四件套**：`{ key, account, password, issuer_url }`。

关键语义：

- **务必按 `purchased` 而不是 `count` 处理结果**。库存是并发争抢的，申请 5 个拿到 3 个是正常结果，只按实际成交数量扣费。
- **对账一律以 `total_credits` 为准，不要用 `unit_price × 数量` 去算**。同一个区可以同时有多辆 live 车，单价按每辆车各自的产出量查阶梯，所以一单里可能混着不同价的 key（提货按最早入库的先给，会跨车次）。混价单里 `unit_price` 只是其中一把的价，乘出来会和实扣不一致。`total_credits` 与 `keys[].paid` 才是权威值，逐把 `paid` 之和恒等于 `total_credits`。
- `keys` 是对象数组，每个元素含 `key` / `account` / `password` / `issuer_url` 四件套，直接遍历取用。
- `paid` 是每把实际扣掉的积分（质保能退回的金额）；`warranty_until` 为空表示这把没有质保（免费交付即如此）。
- 同一个 `client_order_id` 重复调用返回**字节完全一致**的结果，不会二次扣费。

### `GET /api/my/orders/{order_id}/keys`

按订单号补拉，原样返回当时的交付结果。这是 webhook 通知的配套接口 —— 通知里不放密钥，你拿 `order_id` 来换。

### `GET /api/my/orders`（等价别名 `GET /api/my/purchase-orders`）

历史订单列表，支持 `?limit=&offset=`（limit 上限 200）。每条含：

`id` / `client_order_id` / `count` / `unit_price` / `charged` / `free_count` / `created_at`

对账用 `charged`（实扣值，混价单里 `unit_price` 不能乘数量，见上）。

## 8. 积分 / 兑换 / 流水

### `POST /api/my/redeem`

```json
{ "code": "XXXXXXXXXX" }
```

返回 `{"quota": 500, "balance": 1900}`。**兑换码大小写与连字符都会被忽略。**

### `GET /api/my/ledger`

积分流水。`reason` 取值：

| 值 | 含义 |
|---|---|
| `recharge` | 兑换码充值 |
| `purchase` | 领取 key 时扣费（唯一的扣费时机） |
| `income` | 别人买走了你投放到公共池的 key（全额归你） |
| `warranty` | 质保退款：质保期内车次判死，已扣的积分退回 |
| `clawback` | 质保退款冲回：你投放的 key 被退款，那笔收入从你这里冲回 |
| `adjust` | 运营方手工调整 |
| `commit` | 历史遗留：早先版本在发车时自动扣费，现在不再产生新的这类流水 |

## 9. 母号 / 开号 / 供应侧（"开自己车"）

> 这些端点与"从 vendor 拉号"无关，但属于上游 API 全集，如实记录。

### `POST /api/my/mothers`

```json
{
  "label": "主号",
  "access_key": "AKIA…",
  "secret_key": "…",
  "gen_mode": "group",
  "tier": 20,
  "overage_pref": false,
  "region": "us-east-1",
  "note": ""
}
```

- 录入时会立刻用 STS 验一次凭据，验不过直接拒收
- **AK/SK 加密落库，之后任何接口都只回显 AK 后四位，SK 永不回显**
- `tier` 只能是 `20 / 40 / 100 / 200`（美元档位）

### 其他母号端点

- `GET /api/my/mothers`
- `PUT /api/my/mothers/{id}`
- `POST /api/my/mothers/{id}/pool` · 切换池归属 body `{"pool": "public" | "private"}` · 母号有未结束车次时返 `409 mother_busy`
- `POST /api/my/mothers/{id}/status`
- `POST /api/my/mothers/{id}/verify`
- `POST /api/my/mothers/{id}/quota`
- `DELETE /api/my/mothers/{id}`

母号还有未结束的车次时不允许删除，请先停用。

**池切换语义**（`pool` 端点）：`public` = 进公开车队列（产出全站可买 · 卖出分成回来）·
`private` = 只等运营手动开号（不排队自动车）。跑到一半换池会让"这批产出算哪个池的"没有
答案 · 所以有未结束车次时会拒。

## 10. Webhook

### 配置端点

- `GET /api/my/webhook` / `PUT /api/my/webhook`

  ```json
  {
    "private_url": "https://example.com/kiro/my-webhook",
    "public_url":  "https://example.com/kiro/webhook"
  }
  ```

  | 通道 | 何时触发 | 说明 |
  |---|---|---|
  | 自己车 `private_url` | 你自己的母号开号完成 | 只通知你本人。留空则回落到公共车地址 |
  | 公共车 `public_url` | 平台公共池补货 | 需公网可访问，建议校验签名 |

  地址必须是 http/https，**不能指向内网、回环、云元数据地址**，也不能在 URL 里带账号口令。

- `POST /api/my/webhook/test`（`{"channel":"private"}` 或 `"public"`）
- `POST /api/my/webhook/rotate`
- `GET /api/my/webhook/deliveries`

### 事件类型

| `event` | 含义 |
|---|---|
| `new_keys_available` | 有新库存可买。带 `zone`（补货区域）与 `purchase_order_id`（**当作提货幂等键**，不是订单号） |
| `all_keys_dead` | 本车全部 key 失效，该重新开车了。带 `dead`（失效数量） |
| `warranty_refund` | 质保期内车次失效，积分已退回。带 `refunded_quota` 与 `refunded_keys` |
| `webhook_test` | 点"测试"时的探测事件 |

### 载荷（原文摘录）

**只带元信息，不带密钥。** 补货事件里没有密钥也没有订单号 —— 要做的是拿它的 `zone` 与 `purchase_order_id` 去调 purchase 提货。已经提过、只是想重新取键，才用**那次 purchase 返回的** `order_id` 调补拉接口。

`new_keys_available`（补货，主要靠这条）：

```json
{
  "event": "new_keys_available",
  "event_id": "去重用的唯一 id",
  "visibility": "public",
  "message": "美国区新增 20 个 Key 已就绪，可提货",
  "new_keys": 20,
  "zone": "us",
  "purchase_order_id": "32 位十六进制，建议直接当 client_order_id 用",
  "pool_id": "产出该批货的母号 id，用于按母号去重",
  "timestamp": 1785000000
}
```

⚠️ **`purchase_order_id` 不是订单号，别拿它调补拉接口**（那会 404 —— 此刻还没有订单）。它是**为这批货预生成的幂等键**：把它当 `client_order_id` 传给 purchase，webhook 重投时用同一个值提货只会成交一次，天然幂等。

这条事件**不带** `order_id` / `round_id` / `unit_price` —— 要看价先查 `GET /api/my/stock`。

`all_keys_dead`：带 `round_id`、`dead`（失效数量）；自己车的那条还带 `mother_id`。

`warranty_refund`：带 `round_id`、`refunded_quota`、`refunded_keys`、`reason`。

`zone` 只在补货类事件（`new_keys_available`）出现 —— 可以直接把它当作提货时的 `zone` 参数。

### 请求头

| 头 | 说明 |
|---|---|
| `X-KM-Event` | 事件名 |
| `X-KM-Event-Id` | 事件 id，用于去重 |
| `X-KM-Timestamp` | Unix 秒 |
| `X-KM-Delivery-Attempt` | 第几次尝试（最多 3 次） |
| `X-KM-Signature` | `sha256=<hex>` |

### 签名校验

签名内容是 `timestamp + "." + 原始请求体`：

```python
import hmac, hashlib

def verify(secret: str, timestamp: str, signature: str, body: bytes) -> bool:
    mac = hmac.new(secret.encode(), f"{timestamp}.".encode() + body, hashlib.sha256)
    return hmac.compare_digest("sha256=" + mac.hexdigest(), signature)
```

请用**原始字节**校验，不要先解析再重新序列化。建议同时拒绝 `timestamp` 偏离超过 5 分钟的请求。

### 重试

非 2xx 会重试，共 3 次，间隔递增并带抖动。同一事件的多次尝试携带相同 `event_id`，请用它去重。

## 11. Key 剩余额度 / 使用同步

**注意区分两个"积分"：**

| 概念 | 含义 | 看哪里 |
|---|---|---|
| **平台积分** | 你在本平台的余额，领 key 时扣 | `GET /api/my/profile` 的 `balance` |
| **Key 剩余额度** | 这把 `ksk_` 在 Kiro 侧还能调用多少次 | 本节的接口 |

额度数字是**快照**，不是实时值。每次展示都去查 Kiro 会立刻撞上限流，所以要主动同步。

### `GET /api/my/usage`

汇总你名下全部可用 key 的剩余额度。

```json
{
  "usage": {
    "remaining": 4820,
    "total": 6000,
    "synced": 12,
    "keys": 15
  }
}
```

`synced` 小于 `keys` 表示还有 key 从未同步成功 —— 那部分没有计入 `remaining`，不是"没额度"。

### `POST /api/my/keys/{id}/usage`

同步单把 key。响应始终是 200：

```json
{
  "usage": {
    "key_id": "…",
    "used": 180,
    "max": 500,
    "remaining": 320,
    "subscription": "Kiro Pro",
    "reset_days": 12,
    "checked_at": "2026-07-31T01:20:00Z",
    "error": ""
  }
}
```

`error` 非空表示这次同步失败（通常是 Kiro 限流）。**上一次同步成功的数字会被保留**，不会被清空 —— 所以失败时 `used` / `max` 仍是旧的有效值。

### `POST /api/my/keys/usage/refresh`

批量同步全部可用 key（已失效的会跳过）。服务端有并发上限，不会把 Kiro 打成 429。

```json
{ "usages": [ /* 同上，每把一条 */ ], "total": 15, "failed": 2 }
```

部分失败是常态。`failed > 0` 时按条看 `error`，不要整体当成接口错误。

### 建议的用法（原文摘录）

- 交付之后同步一次，之后按小时同步，不要每次请求前都同步。
- `remaining` 掉到 `max` 的 10% 以下就该准备换 key 了。
- 同步失败不影响 key 本身可用性；反过来，`remaining` 还有很多也不代表 key 没被封 —— 存活要看 `GET /api/my/keys` 的 `status`。

## 12. 错误码与限流

统一形状（`error` 是 `message` 的别名，按哪个字段读都行）：

```json
{ "code": "no_stock", "message": "暂无可交付库存，请稍后重试", "error": "暂无可交付库存，请稍后重试" }
```

**优先判 `code`（稳定标识），不要判文案** —— `message`/`error` 会改。

| HTTP | `code` | 处理建议（上游原文） |
|---|---|---|
| 400 | `bad_json` | 请求体不是合法 JSON，或含未知字段 |
| 400 | `bad_order_id` | 幂等键不是 32 位十六进制 |
| 400 | `bad_count` | 数量超出 1–200 |
| 400 | `bad_zone` | `zone` 不是 `us` / `eu`。想要欧区必须显式传 `zone:"eu"` |
| 400 | `idempotency_conflict` | 请求体与请求头的幂等键不一致 |
| 401 | `unauthenticated` / `invalid_api_key` | 检查令牌 |
| 402 | `insufficient_balance` | 余额不足，先充值 |
| 403 | `disabled` | 账号已停用，联系运营方 |
| 403 | `csrf_failed` | 用 cookie 调写接口时缺 CSRF 头；脚本请改用 `X-API-Key` |
| 403 | `session_required` | 该操作只能在网页登录后做（目前只有换 API 令牌），**用令牌重试无用** |
| 404 | `not_found` | 订单/资源不存在，或不属于你 |
| 404 | `redeem_invalid` | 兑换码不存在 / 已用 / 已过期 / 已停用（不区分，防枚举） |
| 413 | `body_too_large` | 请求体超过 1 MiB |
| 409 | `no_stock` | 暂无库存，稍后重试 |
| 409 | `purchase_cap_reached` | 已达持有上限，**重试无用**：调高 `max_keys_held` 或等手里的号失效 |
| 409 | `retry_same_order` | 库存被并发领走，**用同一个 `client_order_id` 重试** |
| 429 | `rate_limited` | 看 `Retry-After` 头。登录/注册在入口另有按 IP 的限速（正常调用不会碰到，并发洪水会被挡）；`POST /api/my/webhook/test` 按账号限次 |
| 502 | `verify_failed` / `quota_failed` | 打 AWS 那一跳失败，可重试 |
| 500 | `internal` | 服务端问题，重试或联系运营方 |

## 13. 上游推荐的客户端流程（原文摘录）

```
启动
 └─ GET /api/my/profile        确认令牌可用、看余额
 └─ PUT /api/my/settings       设置持有上限（可选，0 = 不限）
 └─ PUT /api/my/webhook        配好回调，POST …/test 验一次

常态
 └─ 收到 new_keys_available
      └─ POST /api/my/purchase {count, client_order_id, zone}
         zone 与 client_order_id 都用事件里的（后者取 purchase_order_id）
      └─ 已经领过、只是要重新取键 → GET /api/my/orders/{order_id}/keys
 └─ 收到 all_keys_dead
      └─ 把手里这批标记为失效，等下一条 new_keys_available
 └─ 收到 warranty_refund
      └─ 质保期内死的那批钱已退回，按 refunded_quota 对账即可，不用申请

兜底（webhook 送不到时）
 └─ 每 60 秒 GET /api/my/stock/rounds，看到 remaining > 0 就去买
```

请求失败按 §12 的表处理；`retry_same_order` 必须复用同一个幂等键。
**不要每秒轮询** —— 有按账号的限速。

## 14. 本 vendor 特有的事实（可验证的差异）

- **`purchase_order_id` 是幂等键，不是订单号**。它出现在 `new_keys_available` webhook 载荷里，语义是"下次 purchase 时当 `client_order_id` 用"；直接拿它去调 `/orders/{id}/keys` 会 404。
- **`client_order_id` 强制 32 位十六进制**（正则约束），不允许自定义 UUID 或短字符串。
- **区域强隔离**：`zone` 缺省 = us，`zone` 非法 = 400，明确拒绝"用 eu 顶 us"这种跨区补货。
- **混价单**：同区多辆 live 车时，一单里 `keys[]` 可能来自不同车、单价不同，`unit_price` 字段只反映一辆车。对账权威值是 `total_credits` 与 `keys[].paid`。
- **持有上限双重**：用户设 `max_keys_held` + 运营方全局硬顶，取更严 = `hold_cap_effective`。
- **webhook 载荷不含 key** —— 与"直接把 key 推过来"的 vendor 不同，必须再回调 purchase 兑现。
- **HMAC 签名内容包含 timestamp 前缀**：`sha256(secret, timestamp + "." + body)`，且必须用**原始字节**校验。
- **换 API 令牌走网页**（`session_required` 403）—— 与允许 API 自服务 rotate 的 vendor 不同。

## 15. Fleet 观测端点（2026-08-10 探测）

| 端点 | 结果 | 备注 |
|------|------|------|
| `GET /api/my/gen-logs` | ⚠️ `{logs:null}` | 端点存在 · 但账户视角**只返自己账户下的批** · 我方账户无购买 → null |
| `GET /api/my/orders` | 200 `{orders:null,total:0}` | 账户空 |
| `GET /api/status` | 404 | **无 fleet 自报端点** |
| `GET /openapi/orders` | 200 但返 HTML（SPA 拦截） | 不可用 |
| `GET /api/public/*` | 全 404 | 不暴露公开数据 |

**结论**：kiro91 无 fleet-wide 数据端点 · 我方 `/status` 页从 `vendor_probe.stock_total` 增量推 dispatch（`internal/vendorview/dispatch_deriver.go`）· 精度较低但真实。
