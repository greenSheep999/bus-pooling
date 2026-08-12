# Vendor: Kiro Drop (`drop.kiro.ss`)

## 1. 基础信息

| 项 | 值 |
|---|---|
| Base URL | `https://drop.kiro.ss` |
| 官方文档 | `https://drop.kiro.ss/docs`（登录后可访问，Next.js SPA 页面渲染） |
| 抓取日期 | 2026-08-07（登录抓取） |
| 站点标题 | Kiro Drop · Key Store |
| 我方账号 | `<redacted>` |
| 文档语气自述 | "使用个人 API Key 接入购买接口和 Webhook 通知" |
| 计费币种 | **账户余额 = CNY**，**在售单价 = USD**（混币，见 §4） |

## 2. 鉴权

| 项 | 值 |
|---|---|
| Header | `X-API-Key: usr-xxx` |
| 密钥前缀 | `usr-` |
| Content-Type | `application/json` |
| API 基础路径 | `/api` |

登录到 Web 后台后可获取 API Key。文档未描述 Bearer 备用形式，也未描述 rotate 端点。

## 3. 概念 / 术语

| 术语 | 含义 |
|---|---|
| Key | 提供售卖的凭据。用于 Kiro 上游 API |
| 批次（batch） | 一次开号产出的一组 Key，用 `order_id` = `batch_xxx` 标识 |
| 开号批次 ID | webhook 事件里的 `order_id`（**注意与购买订单 `order_id` 同名不同物**，见 §7） |
| dispatch（自动发车） | 多个 AK/SK 同次自动发车共享一个 `dispatch_id`；合并为一条 webhook |
| 区域（region） | `us` / `eu`，或完整值 `us-east-1` / `eu-central-1` |
| 额外发车 | 系统检测到库存耗尽时自动触发的补货，可在"个人设置"里开关订阅 |

## 4. 计费规则

- **余额币种 = CNY**（`quota` / `remaining` / `used_quota` 字段单位是人民币）
- **在售单价 = USD**（`/api/me/stock` 的 `price` 字段单位是美元）
- 混币场景购买时可用 `max_total_cny` 做**人民币总价保护**（价格上涨时返回 409 且不扣款）
- 无明示单价冻结时机、无免费条件

## 5. 账户信息

### `GET /api/my/profile`

**响应示例（vendor 原文）**：

```json
{
  "name": "user@example.com",
  "quota": "2000.000000",
  "remaining": "884.400000",
  "used_quota": "1115.600000",
  "webhook_url": "https://your-server.example/hook"
}
```

| 字段 | 说明 |
|---|---|
| `name` | 账户名称或邮箱 |
| `quota` | 累计充值总额 (CNY) |
| `remaining` | 当前可用余额 (CNY) |
| `used_quota` | 累计消费总额 (CNY) |
| `webhook_url` | 已配置的 Webhook 地址，未配置时为空字符串 |

**注意所有金额字段都是字符串**（保留六位小数）。

## 6. 系统状态

### `GET /api/status?region=eu`

**响应示例**：

```json
{
  "region": "eu-central-1",
  "keys_active": 5,
  "keys_dead": 0,
  "keys_stock": 25,
  "generating": false
}
```

| 字段 | 说明 |
|---|---|
| `region` | 本次查询的标准区域；**不传 region 时默认 US** |
| `keys_active` | 当前活跃的 Key 数量 |
| `keys_dead` | 已失效的 Key 数量 |
| `keys_stock` | 可购买的库存数量 |
| `generating` | 系统是否正在生成新 Key |

**"死"vs"缺货"判据独立字段**：`keys_dead > 0` 明确标示失效数量；`keys_stock == 0` 表示缺货；`generating == true` 表示正在补货。这是 6 家里语义最清晰的一家。

## 7. 库存与报价

### `GET /api/me/stock?region=eu`

`region` 可选，接受 `us` / `eu` / `us-east-1` / `eu-central-1`；**不传默认 US**。

**响应示例**：

```json
{
  "region": "eu-central-1",
  "stock": 120,
  "price": "30.00",
  "balance": "2060.00"
}
```

| 字段 | 说明 |
|---|---|
| `region` | 标准化后的完整区域 |
| `stock` | 当前可提取的库存数量 |
| `price` | 单价 (USD)，字符串格式保留两位小数 |
| `balance` | 我的可用余额 (CNY)，字符串格式保留小数 |

### `GET /api/v1/reservation`

完整报价接口 · Query `quantity=2&region=eu`。**独家**（对齐 6 家 · 只有 kirodrop 有）。
vendor 文档只列了路径 · 未详列返回字段（2026-08-12 抓文档时已确认）。

## 8. 购买

### `POST /api/my/purchase`

**请求体字段**：

| 字段 | 说明 |
|---|---|
| `count` | 购买数量（**也接受 `quantity`**） |
| `client_order_id` | **必填**，**32 位十六进制**幂等号。同一 `client_order_id + count` 会原样重放；**更改 count 返回 409** |
| `order_id` | 可选，Webhook 中的**开号批次 ID**。传入后只购买该批次，并以批次区域为准 |
| `region` | 可选，接受 `us` / `eu` / `us-east-1` / `eu-central-1`；不传默认 US，**未知值返回 400** |
| `max_total_cny` | 可选，**最高人民币总价保护**。价格上涨时返回 409 且不扣款 |

**请求示例（vendor 原文）**：

```bash
POST /api/my/purchase
X-API-Key: usr-xxx
Content-Type: application/json

{
  "count": 2,
  "region": "eu",
  "client_order_id": "0123456789abcdef0123456789abcdef"
}
```

**成功响应（vendor 原文）**：

```json
{
  "client_order_id": "0123456789abcdef0123456789abcdef",
  "order_id": "store_xxx",
  "region": "eu-central-1",
  "purchased": 2,
  "remaining": "884.400000",
  "status": "completed",
  "refunded_amount_cny": "0.000000",
  "keys": [
    {"key": "ksk_...", "region": "eu-central-1"},
    {"key": "ksk_...", "region": "eu-central-1"}
  ]
}
```

| 字段 | 说明 |
|---|---|
| `client_order_id` | 本次请求的订单号 |
| `order_id` | Drop 购买订单 ID；请求中的批次 ID 在订单查询响应里显示为 `source_batch_id` |
| `region` | 本单实际出货区域，始终为完整区域值 |
| `purchased` | 实际购买成功的数量 |
| `remaining` | 购买后的剩余余额 (CNY) |
| `status` | 订单状态；退款后重放同一幂等键会返回 `partially_refunded` 或 `refunded`，**不会重复购买** |
| `refunded_amount_cny` | 该订单累计已退款金额 (CNY) |
| `keys` | 购买到的 Key 列表，**每项都带完整区域** |

**每把 key 的 payload 极简**：`{ key, region }` —— **只有一个 `key` 字符串 + 区域**，没有 `account / password / issuer_url`（与 91kiro / kiro.ceo 差异，本档案最简 payload）。

### 状态码

| HTTP | 说明 |
|---|---|
| `200` | 购买成功，返回 Key 列表 |
| `400` | 参数错误：数量、订单号或区域格式不正确 |
| `401` | API Key 缺失或无效 |
| `409` | 余额不足、库存不足、订单号冲突或价格超过 `max_total_cny` |
| `503` | 上游 Store 暂时不可用；**已冻结金额会自动释放** |

**订单状态取值（`status` 字段）**：`completed` / `partially_refunded` / `refunded`。

## 9. 积分 / 兑换 / 流水

vendor 文档未展示 redeem/ledger 端点。**这一节在 Drop 是空缺**（可能只支持后台充值，无兑换码；账户是余额型账户不是积分型）。

## 10. 母号 / 开号 / 供应侧

**不存在于对接文档**。Kiro Drop 是纯买方视角。

## 11. Webhook

### 概述

- 每个用户**只保留一个 Webhook 地址**
- 保存后系统会 POST JSON
- 地址必须是**公网 HTTP/HTTPS**
- 最多跟随 **3 次重定向**
- **请求超时 8 秒**，返回任意 `2xx` 即成功
- 失败后**间隔 1 秒重试，最多 3 次**（vs 91kiro 递增+抖动、kiro.ceo 3s/8s）

### 配置端点

- `GET /api/my/webhook` — 读当前配置
- `PUT /api/my/webhook` — 保存

**保存请求（vendor 原文）**：

```bash
PUT /api/my/webhook
X-API-Key: usr-xxx
Content-Type: application/json

{"webhook_url":"https://your-server.example/hook"}
```

**保存响应**：

```json
{
  "ok": "true",
  "webhook_url": "https://your-server.example/hook",
  "webhook_secret": "64位十六进制签名密钥"
}
```

**注意 `webhook_secret` 长度为 64 位十六进制** —— 保存时**立即返回**（无 rotate 端点在文档里展示）。

### 测试端点

- `POST /api/my/webhook/test`

**载荷**：

```json
{
  "event": "test",
  "event_id": "32位ID",
  "message": "这是一条 Webhook 测试消息"
}
```

### 事件类型

vendor 文档明确列出的事件：

| `event` | 触发条件 |
|---|---|
| `new_keys_available` | 新批次上架或仍有库存可购买 |
| `all_keys_dead` | 你持有的 Key 全部失效 |
| `test` | 手工测试 |

### `new_keys_available`（单区域普通）

```json
{
  "event": "new_keys_available",
  "event_id": "32位ID",
  "purchase_order_id": "32位ID",
  "order_id": "batch_xxx",
  "region": "eu-central-1",
  "message": "新一批 Key 已上架",
  "new_keys": 40,
  "created_at": "2026-08-04T12:34:56.000000Z"
}
```

| 字段 | 说明 |
|---|---|
| `purchase_order_id` | **推荐直接作为购买请求的 `client_order_id`** |
| `order_id` | 开号批次 ID；购买时传入可只取该批次 |
| `region` | 本次到货的标准区域 |
| `new_keys` | 该区域当前可售库存数量 |
| `created_at` | 本批 Key 写入库存的 UTC 时间 |

### `new_keys_available`（多 AK/SK 同次自动发车合并）

**Drop 独有形态**。当同次自动发车使用多个 AK/SK 时，多账号完成后**合并为一条 webhook**：

```json
{
  "event": "new_keys_available",
  "event_id": "32位ID",
  "dispatch_id": "monitor_auto_xxx",
  "region": "dual",
  "regions": ["us-east-1", "eu-central-1"],
  "message": "新一批 Key 已上架：US 10 个，EU 12 个",
  "new_keys": 22,
  "new_keys_by_region": {
    "us-east-1": 10,
    "eu-central-1": 12
  },
  "batch_ids_by_region": {
    "us-east-1": ["batch_us_1", "batch_us_2"],
    "eu-central-1": ["batch_eu_1", "batch_eu_2"]
  },
  "purchase_order_ids_by_region": {
    "us-east-1": "32位美区购买幂等号",
    "eu-central-1": "32位欧区购买幂等号"
  },
  "created_at": "2026-08-06T12:34:56.000000Z"
}
```

| 字段 | 说明 |
|---|---|
| `dispatch_id` | 同次自动发车共享 ID；多个 AK/SK 只产生一条通知 |
| `new_keys_by_region` | 本次实际写入 Store 的 US/EU 库存数量 |
| `batch_ids_by_region` | 各区域包含的底层开号批次，仅用于来源追踪 |
| `purchase_order_ids_by_region` | 购买对应区域时使用的 `client_order_id`；**同一区域重试保持不变** |

**关键**：`region == "dual"` 是**合并事件的标志值**，此时解析要走 `_by_region` 字段而不是 `region`。

### `all_keys_dead`

```json
{
  "event": "all_keys_dead",
  "event_id": "32位ID",
  "order_id": "batch_xxx",
  "region": "eu-central-1",
  "message": "全部 Key 已失效，系统正在自动补充",
  "dead": 5
}
```

| 字段 | 说明 |
|---|---|
| `dead` | 本批失效的 Key 数量 |

### 通知触发规则（vendor 原文摘录）

1. 普通批次上架时，所有订阅了 `new_keys_available` 的用户均会收到通知。
2. 同次自动发车使用多个 AK/SK 时，**所有账号完成后合并为一条 `new_keys_available`**，不会按账号或区域重复通知。
3. 某批次 Key 全部失效时，**仅持有该批 Key 的用户**收到 `all_keys_dead`。
4. 额外批次上架时，开启额外发车通知的用户始终收到通知；未开启的用户**在名下没有有效 Key 时也会收到通知**。
5. 第一批失效后如果第二批仍有库存，系统会向尚未持有第二批有效 Key 的 Webhook 用户发送 `new_keys_available`；**该通知不受额外发车开关影响**。

### 额外发车（vendor 原文摘录）

在「个人设置」中开启「接收额外发车通知」后，当系统检测到所有库存耗尽，会自动触发一次额外补货：

- 开启额外发车：额外批次上架时始终收到 `new_keys_available`
- 未开启且持有有效 Key：不接收额外批次通知
- 未开启且没有有效 Key：额外批次上架时仍会收到 `new_keys_available`
- 第一批失效且第二批仍有库存：尚未持有第二批有效 Key 的用户都会再次收到 `new_keys_available`

### 签名验证

**请求头**：

```
X-Kiro-Event-Id: <event_id>
X-Kiro-Timestamp: <Unix 秒>
X-Kiro-Signature: v1=<hex HMAC-SHA256>
```

- 签名原文：`timestamp.rawBody`
- 密钥：配置接口返回的 `webhook_secret`
- **建议拒绝时间戳偏差超过 5 分钟的请求**
- 签名值有 **`v1=` 前缀**（与 91kiro `sha256=` 前缀差异）

**Python 验签示例（vendor 原文）**：

```python
timestamp = request.headers["X-Kiro-Timestamp"]
signature = request.headers["X-Kiro-Signature"]
raw_body = request.get_data()

if abs(time.time() - int(timestamp)) > 300:
    abort(401)

expected = "v1=" + hmac.new(
    WEBHOOK_SECRET.encode("ascii"),
    timestamp.encode("ascii") + b"." + raw_body,
    hashlib.sha256,
).hexdigest()

if not hmac.compare_digest(signature, expected):
    abort(401)
```

### 注意事项（vendor 原文）

- 收到 `new_keys_available` 后**仍需调用 `/api/my/purchase` 购买**，以实时库存和余额为准
- `new_keys` 是**事件对应区域的库存数量，不是分配给当前用户的**
- **US 有效 Key 不影响 EU 通知判断**，EU 有效 Key 也不影响 US
- 建议接收端收到通知后立刻调用购买接口，**库存先到先得**

## 12. 错误码与限流

vendor 文档在购买端点列了状态码表（见 §8），没有给出统一的 `code` 字段错误契约（**类似 kiro.ceo，只有 HTTP status**）。限流细节未在文档中给出。

## 13. 质保 / 退款

vendor 文档**未展示独立的质保时长条款**，但购买响应里包含：

- `refunded_amount_cny` — 累计已退款金额
- `status` 取值 `partially_refunded` / `refunded`

**这说明 Drop 支持部分退款/全额退款语义**，但触发条件（是否有时间窗、是否检测封号）文档没写。**无独立 `warranty_refund` webhook 事件**。

## 14. 本 vendor 特有的事实（可验证的差异）

- **key payload 只有 `{key, region}`** —— 是 6 家里最简，没有 `account/password/issuer_url` 四件套
- **混币计价**：账户余额 CNY，单价 USD；有 `max_total_cny` 参数保护上限
- **有 `/api/status` 独立健康端点**，`keys_active/keys_dead/generating` 三字段直接给出"死"vs"缺货"vs"补货中"（**其他 5 家都没这么明确**）
- **多 AK/SK 合并 webhook 事件**：`region == "dual"`、`_by_region` 后缀字段族 —— 解析器需要单独一路 branch
- **`purchase_order_ids_by_region`** 让**同一批 dispatch 里每个区域有独立幂等键**
- **签名前缀 `v1=`**，与 91kiro `sha256=` 差异
- **webhook 重试 3 次、间隔 1s**（相对固定），与 91kiro 递增抖动、kiro.ceo 3s/8s 都不同
- **超时 8 秒**（91kiro / kiro.ceo 都是 10 秒）
- **文档级别的 vendor 里表达最结构化的**：section 化、字段表、明确"通知触发规则 5 条"、"额外发车 4 条"
- **`webhook_secret` 是 64 位十六进制**（256 bit）
- **购买请求可传批次 `order_id` 定向拉某一批**（其他家一般只允许区域粒度）
- **`count` 也接受 `quantity`**（vendor 兼容处理），但契约里两个字段名都收
- **购买响应的 `order_id` 是 `store_xxx`（购买订单）**，与 webhook 里的 `order_id`（`batch_xxx` 开号批次）**同名不同物**，写代码时要严格分开

## 15. Fleet 观测端点（2026-08-10 探测）

| 端点 | 结果 | 备注 |
|------|------|------|
| `GET /api/status` | ✅ **需 X-API-Key**（不是 Bearer）· `{keys_active, keys_dead, keys_stock, generating, region, community_qr_urls}` | 6 家里唯一要 API key 的 PublicStatus |
| `GET /api/me/stock` | ✅ `{balance, price, region, stock}` | **注意契约变更**：曾是 `{stock:{public_available:N}}` 嵌套对象 · 2026-08 改成 `stock: N` 数字 · 我方 adapter 已做双形状兼容（`mapper.go`） |
| `GET /api/my/*` 全系列 | 404 明确 `{"error":{"code":"NOT_FOUND"}}` | vendor 用 `/api/me/*` 不用 `/api/my/*` |
| `GET /api/me/orders` `GET /api/me/gen-logs` `GET /openapi/orders` | 全 404 | **完全没有 fleet-wide 历史端点** |

**结论**：kirodrop 是 6 家里**最封闭**的 · 只暴露"当下状态"（PublicStatus）· 无任何历史端点 · 我方 `/status` 页从 `vendor_probe.ps_keys_active + ps_keys_dead` 增量推 dispatch。
