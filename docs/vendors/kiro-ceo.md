# Vendor: kiro.ceo

## 1. 基础信息

| 项 | 值 |
|---|---|
| Base URL | `https://kiro.ceo` |
| 官方文档 | `https://kiro.ceo/#/docs`（前端页面，源码 `https://kiro.ceo/js/pages/docs.js`，13.7 KB） |
| 抓取日期 | 2026-08-07 |
| 站点标题 | Kiro 控制台（前端 preact SPA，dark 主题） |
| 存活探活 | `GET /api/my/profile` 带 API key = 200；无 key = 401 |
| 文档语气自述 | "写成页面而不是给一份外部文档，是因为里面的 base URL、密钥占位、区域单价都跟当前这套部署有关—— 抄一份静态文档出去，很快就会和实际接口对不上。" |
| 我方实测账号 | `野生达利奥 Danlio`，quota=0 / used=0 / remaining=0（未充值） |

## 2. 鉴权

- Header：`X-API-Key: <YOUR_API_KEY>`
- 密钥前缀：`usr-`（与 91kiro 相同前缀）
- **没有额外的登录步骤、也不需要换 token**（原文摘录）
- 密钥可以在「账户」页查看
- 密钥泄露"请联系管理员轮换"（**没有自服务 rotate 端点**，与 91kiro 差异）
- 无 CSRF / cookie 二次通道要求

## 3. 概念 / 术语

**vendor 自述用词非常精简**，只有以下几个：

| 术语 | 含义 |
|---|---|
| 积分 | 计费单位。按积分计费，不按"还能提几个号" |
| zone | 区域，当前实测有 `us` / `eu` 两个（美国区 / 欧洲区），每区独立单价 |
| 母号 (`pool_id`) | 产出 key 的上游账号，webhook 用它做去重键 |
| 单价 | 每区独立设置，实测 us=50 积分/个、eu=35 积分/个（2026-08-07 我方账号视角） |
| 质保 | 提货后 10 分钟内号被判死自动退积分（详见 §13） |

**没有"车次"、"公共车/自己车"、"母号供应"这些概念**（与 91kiro 差异）。上游是纯代购视角，不暴露母号管理面。

## 4. 计费规则（原文摘录）

- "本站按**积分**计费，不再按「还能提几个号」。"
- "每个区域单价独立设置。"
- "提货时扣的积分 = 该区单价 × 实际成交数量。"
- "**如果你之前对接的是旧版接口：字段名一个都没改**（`quota`、`remaining`、`used_quota`、`purchased`），只是这些数字现在表示积分而不是个数。原来的代码不用改就能继续跑。"

## 5. 账号 / Profile

### `GET /api/my/profile`

**实测响应**（我方账号，2026-08-07）：

```json
{
  "max_purchase": 10,
  "min_purchase": 1,
  "name": "野生达利奥 Danlio",
  "quota": 0,
  "remaining": 0,
  "used_quota": 0,
  "webhook_url": ""
}
```

| 字段 | 类型 | 含义 |
|---|---|---|
| `name` | string | 账号显示名 |
| `quota` | int | 总积分（**注意：字段名叫 quota 但语义是积分**，见 §4） |
| `used_quota` | int | 已用积分 |
| `remaining` | int | 剩余积分 = `quota - used_quota` |
| `min_purchase` / `max_purchase` | int | 单次提货数量下限/上限 |
| `webhook_url` | string | 已保存的 webhook 地址 |

## 6. 库存

### `GET /api/my/stock`

**实测响应**：

```json
{
  "max": 0,
  "max_purchase": 10,
  "min": 1,
  "quota": 0,
  "reserved": 0,
  "zones": [
    { "zone": "us", "label": "美国区", "unit_price": 50, "enabled": true, "available": 0, "max": 0, "stock": 0 },
    { "zone": "eu", "label": "欧洲区", "unit_price": 35, "enabled": true, "available": 0, "max": 0, "stock": 0 }
  ]
}
```

| 字段 | 含义 |
|---|---|
| `max` | 当前可一次性提取的最大数量（= 各 zone 汇总，受账户上限约束） |
| `min` | 单次最小提货量（= 1） |
| `max_purchase` | 账户单次最大提货量（此账号 = 10） |
| `quota` / `reserved` | 账户维度的积分与保留（未充值账号都是 0） |
| `zones[]` | 每区独立字段 |
| `zones[].zone` | `us` / `eu` |
| `zones[].label` | 中文显示名 |
| `zones[].unit_price` | 每个号的积分单价（此账号视角：us=50, eu=35） |
| `zones[].enabled` | 该区是否启用 |
| `zones[].available` | 该区当前可提数量 |
| `zones[].max` | 该区最大可一次性提取 |
| `zones[].stock` | 该区库存 |

**"死"vs"缺货"判据**：`max == 0` 或 `zones[i].available == 0` 只代表缺货；号死走 webhook `all_keys_dead` 事件。

## 7. 拉号 / 补拉

### `POST /api/my/purchase`（等价别名 `POST /api/me/purchase`）

**请求**：

```bash
curl -X POST https://kiro.ceo/api/my/purchase \
  -H "X-API-Key: <YOUR_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"count": 5, "zone": "us", "client_order_id": "0123456789abcdef0123456789abcdef"}'
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `count` | 是 | 范围看 profile 的 `min_purchase` ~ `max_purchase` |
| `client_order_id` | 强推 | **32 位十六进制**（正则约束）；可用 `Idempotency-Key` header 替代 |
| `zone` | 否 | 不传默认 `us`；`eu` 显式指定；**其他值直接 400，不静默按 us**（与 91kiro 语义一致） |

**响应**（vendor 原文示例）：

```json
{
  "client_order_id": "0123456789abcdef0123456789abcdef",
  "purchased": 5,
  "remaining": 4500,
  "keys": [
    {
      "key": "kiro-xxx",
      "account": "user@example.com",
      "password": "...",
      "issuer_url": "https://..."
    }
  ],
  "zone": "us",
  "unit_price": 100,
  "total_credits": 500,
  "order_id": "a1b2c3..."
}
```

**每把 key 的 payload 四件套**：`{ key, account, password, issuer_url }`（与 91kiro 完全相同）。

**关键语义（vendor 原文摘录）**：

- "务必按 `purchased` 而不是 `count` 处理结果。库存是并发争抢的，申请 5 个拿到 3 个是正常结果，只按实际成交数量扣费。"
- "`keys` 是对象数组，每个元素含 `key / account / password / issuer_url` 四个字段，直接遍历取用即可。"
- "`client_order_id` 必须是 32 位十六进制（也可以用 `Idempotency-Key` 请求头传）。**网络超时后请用同一个 id 重试**—— 服务端会识别成同一笔订单原样返回，不会重复扣费、重复发货。换一个新 id 重试则会变成第二笔订单。"

### `GET /api/my/purchase-orders`

历史订单列表。**实测响应**：`[]`（我方账号无订单）。

### `GET /api/my/keys`

我的凭据列表。带 `?history=1` 时含已失效的。

**实测响应**：

```json
{ "active": 0, "count": 0, "keys": [] }
```

## 8. 积分 / 兑换 / 流水

### `POST /api/my/redeem`

兑换码换积分。**vendor 文档没给字段细节**，只在错误码里列了 `410 兑换码过期`。

### 积分明细里的 refund

vendor 文档说：质保退款"在积分明细里记为 `refund`"—— 暗示存在一个积分明细读端点，但 docs.js 未列出。

## 9. 母号 / 开号 / 供应侧

**不存在**。kiro.ceo 是纯代购视角，没有暴露母号管理、供应侧、发车等能力。（与 91kiro / kiroapp.io 等有 supply-side API 的 vendor 差异）

## 10. Webhook

### 配置

- **`PUT /api/my/webhook`** — 保存到货通知地址（vendor 文档列出，但未给请求体样例）
- **前端有"模拟推送"能力** —— 在"账户"页保存 webhook 地址时可直接模拟推送 `new_keys_available` 和 `all_keys_dead` 两种事件，载荷与真实事件完全一致（只在 `message` 里标了「[模拟]」）

### 事件类型（vendor 原文表格）

| `event` | 类型 | 含义 |
|---|---|---|
| `new_keys_available` | string | 有新号入库。带 `purchase_order_id`，直接拿去当提货的幂等键。 |
| `all_keys_dead` | string | 你名下的号本轮全部失效，系统正在自动补货。 |
| `test` | string | 你点「发送测试」时推的，仅用于验证地址可达。 |

### 载荷字段（vendor 原文表格）

| 字段 | 类型 | 说明 |
|---|---|---|
| `event` | string | 事件类型 |
| `event_id` | string | 事件唯一 id，**可用于去重** |
| `purchase_order_id` | string | 仅 `new_keys_available` 有。**作为 `client_order_id` 提货** |
| `pool_id` | string | 触发本次通知的母号 id。同一母号的重复通知按它去重，避免重复拉取；**全死事件涉及多个母号时用逗号连接** |
| `message` | string | 给人看的中文描述 |
| `new_keys` | int | 仅 `new_keys_available` 有。新增数量 |
| `dead` | int | 仅 `all_keys_dead` 有。失效数量 |
| `zone` | string | 区域（`us` / `eu`），仅补货事件有 |

### 载荷示例（vendor 原文）

`new_keys_available`：

```json
{
  "event": "new_keys_available",
  "event_id": "7f3a9c2e1b4d5a6f8e9c0b1a2d3e4f5a",
  "purchase_order_id": "7f3a9c2e1b4d5a6f8e9c0b1a2d3e4f5a",
  "pool_id": "a1b2c3d4e5f6",
  "message": "美国区新增 20 个 Key 已就绪；...",
  "new_keys": 20,
  "zone": "us"
}
```

`all_keys_dead`：

```json
{
  "event": "all_keys_dead",
  "event_id": "3c8d1f0a5b7e2694c1d8a0f3b5e7c9d2",
  "message": "本轮全部 12 个 Key 已失效，系统正在自动补充新账号",
  "dead": 12
}
```

### 签名

**docs.js 没有描述 webhook 签名 header 或算法**。这是与 91kiro 的关键差异 —— **kiro.ceo webhook 没有 HMAC 签名**，接收端需要靠 URL 里的私密路径/query token 自行保护。

### 重试

- 10 秒超时
- 失败自动重试 **3 次**，间隔 **3s / 8s**（vendor 原文）

## 11. Key 剩余额度 / 使用同步

### `GET /api/my/keys/usage`

用量采样（每分钟积分消耗）。**vendor 文档没给字段细节**（说明是"用量采样"）。

实测：我方账号无数据。

## 12. 错误码与限流（vendor 原文表格）

**错误响应体**：`{"error": "中文说明"}` —— 与 91kiro 的 `{code, message, error}` 结构**不同**，**只有中文文案，没有稳定的 code 字段**。

| HTTP | 类别 | 处理建议（原文） |
|---|---|---|
| 400 | 参数错误 | 幂等键格式不对、区域非法、数量越界 |
| 401 | 密钥无效 | 检查 `X-API-Key`，或密钥已被轮换 |
| 402 | 积分不足 | 先兑换积分再提货 |
| 403 | 账号被停用 | 联系管理员 |
| 404 | 不存在 | 查询的资源不属于你，或已被删除 |
| 409 | 状态冲突 | 库存不足、已达最大持有库存上限、幂等键撞了别的订单；用同一个 id 重试 |
| 410 | 兑换码过期 | 换一张 |
| 429 | 过于频繁 | 降低频率后重试 |
| 5xx | 服务端错误 | **用同一个 `client_order_id` 重试是安全的** |

**vendor 强调**：遇到 5xx 或网络超时时，请用同一个 `client_order_id` 重试而不是换新的——订单可能已经成交，换 id 会变成第二笔。

## 13. 质保 / 退款

**10 分钟质保**（vendor 原文摘录）：

- 提货后 10 分钟内，如果号被系统检测到失效（封号），会**自动**把这单实际扣的积分退还到余额，无需申请
- 退款按下单时的单价计算
- 在积分明细里记为 `refund`
- **超过 10 分钟才失效的属于正常损耗，不退**

**没有 `warranty_refund` 独立 webhook 事件**（与 91kiro 差异）—— 质保退款只体现在积分明细里，不会主动推送。

## 14. 本 vendor 特有的事实（可验证的差异）

- **只用 `X-API-Key`，不支持 `Authorization: Bearer`**（与 91kiro 差异）
- **文档写成 preact 页面而非静态 md**，官方自述"抄一份静态文档出去，很快就会和实际接口对不上"
- **错误响应体没有 `code` 字段**，只有 `{error: "中文文案"}`；解析要按 HTTP status 分派，不能按稳定 code 分派（**这是本档案里语义最弱的错误契约**）
- **webhook 无 HMAC 签名**（与 91kiro、kirodrop 差异），只能靠 URL secret 自保护
- **无 `warranty_refund` webhook**，质保退款静默入账，只能通过积分明细回查
- **无自服务 rotate 端点**，泄露只能"联系管理员"
- **无母号/供应侧 API**，纯代购
- **`purchase` 响应字段名保留旧兼容**：`quota`/`remaining`/`used_quota`/`purchased` 名字未改，但数字语义已改为积分（不是数量），这一点写代码时要非常小心
- **`purchase_order_id` = `event_id`**（示例里两个值一模一样）—— vendor 用同一个 32-hex 值双用，天然幂等去重
- **`pool_id` 在 `all_keys_dead` 事件里可能是"逗号连接的多母号"**，解析时不能当单一 id
- **前端能"模拟推送"**：调 webhook 联调不用等真实号入库

## 15. Fleet 观测端点（2026-08-10 探测）

| 端点 | 结果 | 备注 |
|------|------|------|
| `GET /api/my/gen-logs` | ✅ `{avg_interval_min, items:[{created_at,count,status}]}` | **全平台可见**——即使账户空也返所有账户的开号批 · 是 kiro91 端点的"全网视图"版本 |
| `GET /api/me/orders` | 200 `{items:[],total:0}` | 账户视角空 |
| `GET /api/status` | 200 但返 SPA HTML | 不是 API 端点 |
| `GET /openapi/orders` | 200 但返 SPA HTML | 不可用 |

**结论**：kiro91 和 kiroceo 用同一个 `/api/my/gen-logs` 契约 · **但 kiroceo 返全平台数据**（区别点）· 我方 adapter `internal/providers/kiro/vendors/kiroceo/fleet.go` 直接用这个当 FleetLister · 是 6 家里数据最新鲜的。
