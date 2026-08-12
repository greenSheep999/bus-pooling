> ⚠️ 敏感数据豁免：本档案的实测响应示例里 `webhook_url`/`api_key_prefix` 字段
> 值均为演示占位；真实 vendor 凭据/账号明文一律不入档 · `docs/vendors/_sources/`
> 已在 .gitignore 屏蔽 · docs/vendors/*.md 里出现的都是脱敏值。

# Vendor: kiro.ceo

## 0 · 档案元信息

| 项 | 值 |
|---|---|
| Base URL | `https://kiro.ceo` |
| 官方文档 | `https://kiro.ceo/#/docs`（preact SPA · 源码 `/js/pages/docs.js` · 13.7 KB） |
| 抓取日期 | 2026-08-12（`.playwright-mcp/vendor-scrape-2026-08-12/kiroceo-docs.txt`） |
| 存活探活 | `GET /api/my/profile` 带 API key → 200 · 无 key → 401 |
| 鉴权 | 只支持 `X-API-Key` header · **不支持 `Authorization: Bearer`**（与 kiro91 差异） |
| 密钥前缀 | `usr-` |
| **官方端点总数** | **8 个 `/api/my/*` + 别名 `/api/me/*`** |
| **我方 adapter 接了** | 8 个 · 覆盖率 100% · 见 §1 表 |
| Provider group | `providers.ProviderKiro` |
| Adapter 目录 | `internal/providers/kiro/vendors/kiroceo/` |

---

## 1 · 端点清单（vendor 官方 8 个）

按 vendor 官方 `docs.txt` 原文顺序列 · **端点名 + 参数 + 响应结构 + 我方 adapter**：

| # | 方法 | vendor 官方路径 | vendor 官方描述 | 我方 adapter 方法 · 位置 | 状态 |
|---|---|---|---|---|---|
| 1 | GET  | `/api/my/profile` | 账号信息 + 积分余额 | `Adapter.Balance()` · `adapter.go:145` | ✅ 接了 |
| 2 | GET  | `/api/my/stock` | 可提取数量 + 单次上下限 + `zones[]` 各区单价与可购量 | `Adapter.Stock()` · `adapter.go:67` | ✅ 接了 |
| 3 | POST | `/api/my/purchase`（别名 `/api/me/purchase`）| 提货 · 幂等 `client_order_id` | `Adapter.Purchase()` · `adapter.go:95` | ✅ 接了 |
| 4 | GET  | `/api/my/keys` | 我的凭据（`?history=1` 含已失效）| `Adapter.KeyStats()` · `history.go:104` | ✅ 接了（用 `?history=1`）|
| 5 | GET  | `/api/my/purchase-orders` | 历史订单 | `Adapter.PurchaseHistory()` · `history.go:73` | ✅ 接了 |
| 6 | GET  | `/api/my/keys/usage` | 用量采样（每分钟积分消耗）| — | ❌ **未接** |
| 7 | POST | `/api/my/redeem` | 兑换码换积分 | `Adapter.Redeem()` · `adapter.go:188` | ✅ 接了 |
| 8 | PUT  | `/api/my/webhook` | 保存到货通知地址 | — | ❌ **未接**（webhook URL 在 vendor 后台手工配 · 不通过 API 设）|

**我方还接了 1 个隐藏端点**（vendor 官方 8 个之外）：

| # | 方法 | 路径 | 用途 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 9 | GET | `/api/my/gen-logs` | fleet 出货批次历史（全平台可见 · `avg_interval_min`+`items[]`）| `fleet.go:35` | ✅ 接了 · 探针提频用 |
| 10 | GET | `/api/my/orders/{order_id}/keys` | 按 order_id 补拉 key | `Adapter.OrderKeys()` · `adapter.go:121` | ✅ 接了 |

**未在 vendor 文档但探测存在的路径**（返 200 但是 SPA HTML · 非真 API · 不接）：
- `/api/status` · `/api/public/status` · `/api/vendors` · `/api/restock-log` · `/api/dispatches`
- `/api/my/dispatch-history` · `/api/my/fleet-history` · `/api/my/history` · `/api/my/stock/regions`
- 探测记录见 `docs/vendors/_endpoints-audit-2026-08-12.md`

**未在 vendor 文档且探测 404**：kirodrop / kiroappio / kiroappcc 独有的路径全部 404 · 无 cross-vendor 隐藏端点。

---

## 2 · 逐端点字段清单（vendor 原文命名）

### 2.1 `GET /api/my/profile`

**实测响应**（我方账号 · 2026-08-12）：

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

| 字段 | 类型 | 语义 | vendor 原文说明 |
|---|---|---|---|
| `name` | str | 账号显示名 | — |
| `quota` | int | 总积分 | **注意：字段名叫 quota 但语义是积分**（vendor 兼容旧接口 · 数值意义已改）|
| `used_quota` | int | 已用积分 | 同上 |
| `remaining` | int | 剩余积分 | = `quota - used_quota` |
| `min_purchase` | int | 单次提货数量下限 | 账号维度 |
| `max_purchase` | int | 单次提货数量上限 | 账号维度 |
| `webhook_url` | str | 已保存的 webhook 地址 | 空字符串表示未配 |

**我方 adapter 映射**：
- `Balance` (int64 microunit) ← `remaining × 1_000_000`
- 其他字段目前**不落库** · 只在 adapter 层用作 sanity check

---

### 2.2 `GET /api/my/stock`

**实测响应**（我方账号 · 2026-08-12）：

```json
{
  "max": 0,
  "max_purchase": 10,
  "min": 1,
  "quota": 0,
  "reserved": 0,
  "zones": [
    { "zone": "us", "label": "美国区", "unit_price": 100, "enabled": true, "available": 0, "max": 0, "stock": 0 },
    { "zone": "eu", "label": "欧洲区", "unit_price": 70,  "enabled": true, "available": 0, "max": 0, "stock": 0 }
  ]
}
```

**顶层字段**：

| 字段 | 类型 | 语义 |
|---|---|---|
| `max` | int | 当前可一次性提取的最大数量（= 各 zone 汇总 · 受账户上限约束）|
| `min` | int | 单次最小提货量 · 恒 1 |
| `max_purchase` | int | 账户单次最大提货量 |
| `quota` | int | 账户维度的积分（跟 profile 同）|
| `reserved` | int | 账户维度的保留（未充值账号恒 0 · 用途未在 docs 说明）|
| `zones` | array | 每区一条 |

**`zones[]` 元素字段**：

| 字段 | 类型 | 语义 |
|---|---|---|
| `zone` | str | `"us"` / `"eu"` · **权威地区标识** |
| `label` | str | 中文显示名 · `"美国区"` / `"欧洲区"` |
| `unit_price` | int | 该区单价（积分/个）· 已按母号存活时长降到现价 |
| `enabled` | bool | 该区是否启用 |
| `available` | int | 该区当前可提数量 |
| `max` | int | 该区一次性最多能提数量 |
| `stock` | int | 该区库存原始数（可能 ≥ `available` · 部分被 reserved）|

**我方 adapter 映射**（`mapper.go:89`）：

| 我方字段 | 来源 |
|---|---|
| `StockSnapshot.Available` | `stock.public_available`（**旧字段** · 实际用 `max`）|
| `StockSnapshot.MinPerOrder` | `min` |
| `StockSnapshot.MaxPerOrder` | `max_purchase` |
| `StockSnapshot.WarrantyMinutes` | ❌ **不返** · vendor 恒 10 分钟质保 · 我方硬编码 |
| `ZoneStock[].Zone` | `zones[].zone` 直接（`"us"` / `"eu"`）|
| `ZoneStock[].Region` | `zones[].region` · **vendor 不返** · adapter 落空字符串 |
| `ZoneStock[].Available` | `zones[].available` |
| `ZoneStock[].UnitPrice` | `Money{Amount: unit_price × 10^6, Currency: "credit"}` |

**⚠️ 数据缺口**：`label`（中文名）· `enabled` · `stock`（vs available 差异）· `reserved` · `max_purchase` 都**不落库**。

---

### 2.3 `POST /api/my/purchase`

**请求**：
```json
{ "count": 5, "zone": "us", "client_order_id": "0123456789abcdef0123456789abcdef" }
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `count` | ✓ | 范围 `min_purchase` ~ `max_purchase` |
| `client_order_id` | ★ 强推 | **32 位十六进制**（正则约束）· 或用 `Idempotency-Key` header |
| `zone` | ✗ | 不传默认 `"us"` · `"eu"` 显式指定 · **其他值直接 400**（不静默按 us）|

**响应**：
```json
{
  "client_order_id": "0123456789abcdef0123456789abcdef",
  "purchased": 5,
  "remaining": 4500,
  "keys": [
    { "key": "kiro-xxx", "account": "user@example.com", "password": "...", "issuer_url": "https://..." }
  ],
  "zone": "us",
  "unit_price": 100,
  "total_credits": 500,
  "order_id": "a1b2c3..."
}
```

| 字段 | 类型 | 语义 |
|---|---|---|
| `client_order_id` | str | 回传的幂等键 |
| `purchased` | int | **实际成交数量** · 可能 < count |
| `remaining` | int | 扣款后剩余积分 |
| `keys` | array of obj | 每把 key **四件套** `{ key, account, password, issuer_url }` |
| `zone` | str | 交付区（就是请求那个 · 严格隔离）|
| `unit_price` | int | 该单单价（积分/个）|
| `total_credits` | int | **权威扣款额**（= unit_price × purchased · 但混价单可能 ≠）|
| `order_id` | str | vendor 侧订单号 · 补拉用 |

**vendor 明说**：
- "务必按 `purchased` 而不是 `count` 处理结果"
- "网络超时后请用同一个 `client_order_id` 重试 —— 服务端会识别成同一笔订单原样返回 · 不会重复扣费"

**我方 adapter 映射**（`mapper.go:65`）：

| 我方字段 | 来源 |
|---|---|
| `PurchaseResult.OrderID` | `order_id` |
| `PurchaseResult.ClientOrderID` | `client_order_id` |
| `PurchaseResult.Zone` | `providers.Zone(zone)` |
| `PurchaseResult.Purchased` | `purchased` |
| `PurchaseResult.Requested` | 请求侧 `count`（回填）|
| `PurchaseResult.TotalCost` | `Money{Amount: total_credits × 10^6, Currency: "credit"}` |
| `PurchaseResult.Keys[]` | 每把 `{Key, Account, Password, IssuerURL}` |

---

### 2.4 `GET /api/my/keys` · `GET /api/my/keys?history=1`

**实测响应**（我方账号 · 2026-08-12）：
```json
{ "active": 0, "count": 0, "keys": [] }
```

**vendor 未在 docs 明说完整字段** · 只知：
- `?history=1` 时含已失效 key · 否则只 active
- 顶层 `active` / `count` 是聚合数
- `keys[]` 每条含 key 数据（**实测账号为空 · 无法列字段**）

**我方 adapter 用它**（`history.go:104`）：
- 走 `?history=1` · 拿聚合数（active/count）·填 `KeyStatsBatch`
- **不解析 `keys[]`**（vendor 侧字段结构未确认 · 我方靠 credential_ledger 存明细）

---

### 2.5 `GET /api/my/purchase-orders`

**实测响应**（我方账号 · 2026-08-12）：
```json
[]
```
（我方账号无订单 · 无法列元素字段）

**我方 adapter 用它**（`history.go:73`）· 遍历历史订单 · fill `PurchaseHistory`。字段推测（从 kiro91 同类端点对齐 · 未在 kiroceo docs 明写）：
- `id` / `client_order_id` / `count` / `unit_price` / `charged` / `free_count` / `created_at`

---

### 2.6 `GET /api/my/keys/usage`

**vendor 文档只写了一行**："用量采样（每分钟积分消耗）"

**未接** · 字段结构未知。**用途候选**：分析我方购入 key 的实时消耗速率 · 用来判定 key 快"跑完"了。

---

### 2.7 `POST /api/my/redeem`

**请求**：`{ "code": "XXXXXXXX" }`

**响应**：vendor docs 未明说 · 从错误码 `410 兑换码过期` 推测走类似 kiro91 的 `{ quota, balance }` 结构。

**我方 adapter 接了**（`adapter.go:188`）· `Adapter.Redeem()` · 是运维手动兑换用 · 用户不触发。

---

### 2.8 `PUT /api/my/webhook`

**未接** · vendor docs 只说"保存到货通知地址" · 请求体格式未在 docs 明写。

**为什么不接**：我方 webhook URL 是**部署前手工在 vendor 后台配好的** · 一次性 · 无需 API 改。要改就手动登录 vendor 后台。

---

### 2.9 `GET /api/my/gen-logs`（我方接 · 官方 8 端点之外）

**实测响应**：
```json
{
  "avg_interval_min": 24.028070175438597,
  "items": [
    { "created_at": "2026-08-12T11:12:29Z", "count": 10, "status": "done" }
  ]
}
```

| 字段 | 类型 | 语义 |
|---|---|---|
| `avg_interval_min` | float | vendor 自算的**平均开号间隔（分钟）** |
| `items[]` | array | 批次列表 · **全平台可见**（不只我方账号）|
| `items[].created_at` | str (ISO8601) | 批次开号时刻 |
| `items[].count` | int | 该批次开号数量 |
| `items[].status` | str | `running` / `error` / `done` |

**跟 kiro91 差异**：kiro91 同名端点只返自己车 · kiroceo 返**全平台**（fleet 视角）· 是全 6 家里 fleet 数据最新鲜的。

**我方 adapter 用它**：
- `FleetLister.RecentDispatches()` · 落 `vendor_dispatch` 表
- 探针提频决策：`items[]` 有 `running` 状态时进 hot 模式

---

### 2.10 `GET /api/my/orders/{order_id}/keys`（我方接 · 官方文档提到但未列端点）

**用途**：按 vendor 侧 `order_id` 补拉这批 key · 是 webhook `purchase_order_id` 的配套。

**响应**：跟 `POST /api/my/purchase` 的响应字段完全一致 · **同一个订单的 key 明文重发**。

**我方 adapter 用它**（`adapter.go:121`）· `Adapter.OrderKeys()` · webhook 通知后补拉时用。

---

## 3 · Webhook

### 3.1 配置

- 端点：`PUT /api/my/webhook`（我方**未接** · 手工在 vendor 后台配）
- 前端有"模拟推送"（新号 + 全死两种事件 · 载荷跟真事件一致 · 只 message 标 `[模拟]`）

### 3.2 事件类型（3 种）

| `event` 值 | 触发时机 | 载荷独家字段 |
|---|---|---|
| `new_keys_available` | 有新号入库 | `new_keys`, `purchase_order_id`, `zone` |
| `all_keys_dead` | 本轮号全失效 · 系统自动补 | `dead` |
| `test` | 手工发测试 | 无 |

### 3.3 载荷字段

```json
{
  "event": "new_keys_available",
  "event_id": "7f3a9c2e1b4d5a6f8e9c0b1a2d3e4f5a",
  "purchase_order_id": "7f3a9c2e1b4d5a6f8e9c0b1a2d3e4f5a",
  "pool_id": "a1b2c3d4e5f6",
  "message": "美国区新增 20 个 Key 已就绪 · ...",
  "new_keys": 20,
  "zone": "us"
}
```

| 字段 | 类型 | 语义 | 出现在哪个事件 |
|---|---|---|---|
| `event` | str | 事件类型 | 全部 |
| `event_id` | str (32-hex) | 唯一 id · 去重键 | 全部 |
| `purchase_order_id` | str (32-hex) | **直接当 `client_order_id` 提货** · vendor 值 = `event_id` 双用 | new_keys / test |
| `pool_id` | str | 触发的母号 id · **all_keys_dead 事件可能是逗号连接的多母号** | new_keys / all_keys_dead |
| `message` | str | 中文摘要 · 给人看 | 全部 |
| `new_keys` | int | 新增数量 | new_keys |
| `dead` | int | 失效数量 | all_keys_dead |
| `zone` | str | 区域 | new_keys |

**关键差异**：**无 HMAC 签名** · 靠 URL 里的私密路径/token 自保护（与 kiro91 / kirodrop 差异）。

**重试**：10s 超时 · 失败重试 3 次 · 间隔 3s / 8s。

### 3.4 无 `warranty_refund` 独立事件

kiroceo 的 10 分钟质保退款**只静默入积分明细**（reason=`refund`）· 不推 webhook（与 kiro91 差异）。

---

## 4 · 错误码

**错误响应体**：`{"error": "中文说明"}` · **没有 code 字段** · **只有中文文案**（与 kiro91 的 `{code, message, error}` 结构差异）。

| HTTP | 类别 |
|---|---|
| 400 | 参数错误（幂等键格式 / zone 非法 / 数量越界）|
| 401 | 密钥无效 |
| 402 | 积分不足 |
| 403 | 账号停用 |
| 404 | 不存在 |
| 409 | 状态冲突（库存不足 / 持有上限 / 幂等键撞另一单）|
| 410 | 兑换码过期 |
| 429 | 过于频繁 |
| 5xx | 服务端错误 · **同 `client_order_id` 重试安全** |

---

## 5 · 特有事实（跟其他 vendor 的差异）

- **只 `X-API-Key`** · 不支持 `Authorization: Bearer`
- **文档写成 preact SPA 页** · 不是静态 md
- **错误响应无 `code` 字段** · 靠 HTTP status 分派
- **webhook 无 HMAC 签名** · 靠 URL secret
- **无 `warranty_refund` webhook** · 质保退款静默入账
- **无自服务 rotate 端点** · 泄露只能"联系管理员"
- **无母号/供应侧 API** · 纯代购
- **`purchase_order_id` = `event_id`** · 一值双用
- **`pool_id` 在 all_keys_dead 里可能是逗号连接的多 id** · 解析别当单一 id
- **`quota` / `remaining` / `used_quota` / `purchased` 字段名保留旧兼容** · 但语义已改成积分 · 不是数量
- **`/api/my/gen-logs` 返全平台数据**（不只我方账号）· 是 6 家里 fleet 数据最新鲜的

---

## 6 · 已知问题 / 未接的能力

| # | 问题 | 优先级 |
|---|---|---|
| 1 | `/api/my/keys/usage` 未接 · 拿不到我方购入 key 的实时用量曲线 | 低 · 目前靠 kiro.rs 探测 |
| 2 | `/api/my/webhook` 未接 · webhook URL 只能手工在 vendor 后台配 | 低 · 一次性配置 |
| 3 | `zones[].stock` 跟 `available` 差异不落库 | 中 · 想区分"库存 vs 可提"要接 |
| 4 | `zones[].label`（中文名）不落库 | 低 · 前端用 zone 归一 label 就行 |
| 5 | `POST /api/my/purchase` 混价单里 `keys[].paid` 不落库（只落 `total_credits`）| 中 · 对账时逐把 key 的实付缺 |
