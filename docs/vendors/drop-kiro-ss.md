# Vendor: Kiro Drop (`drop.kiro.ss`)

## 0 · 档案元信息

| 项 | 值 |
|---|---|
| Base URL | `https://drop.kiro.ss` |
| 官方文档 | `https://drop.kiro.ss/docs`（登录后 · Next.js SPA）· 抓取存档 `.playwright-mcp/vendor-scrape-2026-08-12/kirodrop-docs.txt` |
| 抓取日期 | 2026-08-12 |
| 鉴权 | `X-API-Key: usr-…` |
| 密钥前缀 | `usr-` |
| **官方端点总数** | **8 个（公开文档写的）· 应该还有隐藏（页面用的 `/api/*` 未详摸）** |
| **我方 adapter 接了** | **6 个 · 覆盖率 75%** |
| Provider group | `providers.ProviderKiro` |
| Adapter 目录 | `internal/providers/kiro/vendors/kirodrop/` |
| **计价 · 混币** | **账户余额 = CNY** · **在售单价 = USD**（字符串保留小数）· 6 家里唯一非积分计价的 |
| 单价 | us-east-1 = **7.35 USD**（2026-08-12 实测）→ 我方换算 49.98 积分（× 6.8）|
| 分区 | **实测只 `us-east-1`**（文档说支持 `?region=us\|eu` · 但 stock 端点只返单区）|
| xi8 视角 | xi8 报该家 US 51.97 CNY / **EU 36.34 CNY** —— **EU 定价我方官端拿不到** |
| 质保 | vendor 文档未明说 |

---

## 1 · 端点清单（vendor 官方 8 个）

### 1.1 账户（1 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 1 | GET | `/api/my/profile` | 名称 · quota · remaining · used_quota · webhook_url | `Adapter.Balance()` | ✅ |

### 1.2 系统状态（1 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 2 | GET | `/api/status?region=us\|eu` | `keys_active` / `keys_dead` / `keys_stock` / `generating` · **免鉴权** | `Adapter.PublicStatus()` | ✅ |

### 1.3 库存与报价（2 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 3 | GET | `/api/me/stock?region=us\|eu` | `region` / `stock` / `price`（USD 字符串）/ `balance`（CNY）| `Adapter.Stock()` | ✅ |
| 4 | GET | `/api/v1/reservation?quantity=2&region=eu` | **完整报价**（独家）· 支持多货币 | `Adapter.Reservation()` · **声明了但返"未接入"** | ⚠️ **半接** |

**⚠️ `Reservation` 现状**（`adapter.go:224`）：方法存在但直接返 `Message: "本 vendor reservation 阶段 1a 未接入"` · **实际没打端点**。`docs/18` 里 `TieredPricing`（分档降价）本该从这里来 · 所以 `vendor_price_tier` 表至今是空的。

### 1.4 购买（1 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 5 | POST | `/api/my/purchase` | `{count/quantity, client_order_id, order_id?, region?, max_total_cny?}` | `Adapter.Purchase()` | ✅ |

**⚠️ 独家参数 `max_total_cny`** · **价格保护** —— 涨价直接返 409 不扣款。6 家里唯一原生支持的（我方 `vendorMaxTotal` 就是为它写的）。

### 1.5 Webhook 管理（3 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 6 | GET | `/api/my/webhook` | 读配置 · `webhook_url` + `secret` | ❌ 未接 |
| 7 | PUT | `/api/my/webhook` | 改配置 `{webhook_url}` | ❌ 未接（手工在后台配）|
| 8 | POST | `/api/my/webhook/test` | 发测试 | ❌ 未接 |

**我方还接了**：`GET /api/my/orders/{id}/keys`（补拉 · `adapter.go:128`）· `POST /api/my/redeem` ✅

---

## 2 · 逐端点字段清单（vendor 原文命名）

### 2.1 `GET /api/my/profile`

**实测响应**（我方账号 · 2026-08-12）：
```json
{ "name": "…", "quota": "…", "remaining": "…", "used_quota": "…", "webhook_url": "" }
```

| 字段 | 类型 | 语义 |
|---|---|---|
| `name` | str | 账号显示名 |
| `quota` | **str** | 总额度（CNY · **字符串保留小数**）|
| `used_quota` | **str** | 已用 |
| `remaining` | **str** | 剩余 |
| `webhook_url` | str | 已配 webhook 地址 |

**⚠️ 关键差异**：**quota/remaining/used_quota 是字符串**（其他 5 家都是 int）—— 因为 CNY 有小数。我方解析要 `strconv.ParseFloat` 不能直接当 int。

**我方 adapter 映射**：`Balance` ← `parseFloat(remaining) × 10^6`。

---

### 2.2 `GET /api/me/stock`

**实测响应**（我方账号 · 2026-08-12）：
```json
{ "balance": "0.000000", "price": "7.35", "region": "us-east-1", "stock": 0 }
```

| 字段 | 类型 | 语义 |
|---|---|---|
| **`region`** | str | **`us-east-1`** · ⚠️ **只返单区** · 文档说支持 `?region=` 但实测只有 us |
| `stock` | int | 该区库存 |
| **`price`** | **str** | ★ **USD 单价**（`"7.35"`）· 6 家里唯一 USD 计价 |
| **`balance`** | **str** | 我方余额（**CNY** · `"0.000000"`）|

**⚠️ 混币**：`price` 是 **USD** · `balance` 是 **CNY** · **同一个响应里两种币种** · 6 家里唯一。

**⚠️ 无 `zones[]` / `regions[]` 数组** · 只有单个 `region` 字段（跟 kiroappio 平铺、kiroappcc 无区都不同 —— 这家是"单区 + region 字段"）。

**我方 adapter 映射**（`mapper.go`）：

| 我方字段 | 来源 |
|---|---|
| `StockSnapshot.Available` | `stock` |
| `ZoneStock[0].Zone` | ⚠️ **落空** —— vendor 不返 `zone` 字段 · adapter 没从 `region` 归一 · **这是 bug**（见 §7 缺口 1）|
| `ZoneStock[0].Region` | `region`（`"us-east-1"`）|
| `ZoneStock[0].Available` | `stock` |
| `ZoneStock[0].UnitPrice` | `parseUSDStringToMoney(price)` → `Money{7_350_000, "USD"}` ✅ |
| `StockSnapshot.Balance` | `parseFloat(balance) × 10^6`（CNY）|

**⚠️ 换算链**：`price="7.35"` (USD) → `Money{7_350_000, USD}` → `vendor_pricing.credits_per_unit=6_800_000` → **49.98 积分**。

**⚠️ 数据缺口**：`balance`（CNY 余额）解析出来但**不落 vendor_probe** —— 其他家的 balance 也一样不落。

**⚠️ EU 定价完全拿不到**：xi8 报该家 EU = 36.34 CNY · 我方 stock 端点只返 us-east-1 · **EU 那一半定价我方从来不知道**（`docs/19-fields.md` 里 xi8 补漏就是为这个）。

---

### 2.3 `GET /api/v1/reservation`（⚠️ 半接 · 高价值）

**vendor 文档**：`?quantity=2&region=eu` · **完整报价**（独家 · 支持多货币）。

**我方现状**：`Adapter.Reservation()` 存在但返 `"阶段 1a 未接入"` · **实际没打**。

**为什么高价值**：
1. **完整报价** —— `quantity` 参数说明支持"买 N 个多少钱"的批量报价（可能有分档）
2. **支持多货币** —— 可能能直接拿到 CNY 报价 · 不用我方按 6.8 汇率换（xi8 用 7.07 · 我方用 6.8 · **差 4%** · 这个端点可能是权威口径）
3. **`region=eu` 参数** —— **可能这才是拿 EU 定价的正确路径**（`/api/me/stock` 只返 us）
4. `docs/18` 的 `TieredPricing` / `vendor_price_tier` 表数据源就是它 —— 现在表空着

**⚠️ 必须实测这个端点** · 是当前最大的信息缺口。

---

### 2.4 `POST /api/my/purchase`

**请求**：
```json
{ "count": 5, "client_order_id": "…", "order_id": "…", "region": "us", "max_total_cny": "50.00" }
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `count` / `quantity` | ✓ | **两个名字都接受** |
| `client_order_id` | ★ | 幂等键 · 同 id 换 count 返 **409** |
| `order_id` | ✗ | webhook 里的 order_id · 精确指定批次 |
| `region` | ✗ | `us` / `eu` |
| **`max_total_cny`** | ✗ | ★ **价格保护** · 涨价返 409 **不扣款** · 6 家里唯一原生支持 |

**状态可能值**：`completed` / **`partially_refunded`** / **`refunded`**

**⚠️ 双区订单响应含 `refunded_amount_cny`** · 记录退款金额。

**我方 adapter 映射**：
- `MaxTotal` → `max_total_cny`（我方 `vendorMaxTotal()` 算出来的）✅
- `PurchaseResult.TotalCost` ← `total_credits × 10^6`（⚠️ 字段名 credits 但这家是 CNY 计价 —— **需核对 adapter 是否正确处理**）

---

### 2.5 `GET /api/status`

**实测响应**（我方账号 · 2026-08-12）：
```json
{ "generating": false, "keys_active": 0, "keys_dead": 0, "keys_stock": 0, "region": "us-east-1" }
```

| 字段 | 语义 |
|---|---|
| `keys_active` | fleet 存活 key 数 |
| `keys_dead` | 已死 |
| `keys_stock` | 可买库存 |
| `generating` | 是否正在开号 |
| **`region`** | ★ **单 region**（`us-east-1` default）· 文档说支持 `?region=` 参数 |

**⚠️ 免鉴权** · 探针能不带 key 打。

**我方 adapter 映射**：`PublicStatus` ← `keys_*` + `generating`。

**⚠️ 数据缺口**：这家 status **没有** `keys_alive` / `keys_suspect` / `keys_total` / `started_at` / `uptime_seconds`（kirooo / kiroappio 都有）· fleet 观测维度最少。

---

## 3 · Webhook

### 3.1 事件类型（2 类 + test）

#### `new_keys_available`（**US/EU 合并通知** · 只推一次 · 6 家里唯一）

**顶级字段**：

| 字段 | 语义 |
|---|---|
| `event` | 事件类型 |
| `event_id` | 去重 id |
| `purchase_order_id` | 幂等键 |
| `order_id` | 订单号 |
| **`dispatch_id`** | ★ 批次 id |
| `region` | 区域 |
| **`notification_scope`** | ★ **`"dual"`** —— 标记这是双区合并通知 |
| `message` | 中文摘要 |
| `new_keys` | **合计**新增数 |
| `created_at` | UTC 时刻 |

**双区拆分字段**（★ 全是这家独有的结构）：

| 字段 | 类型 | 语义 |
|---|---|---|
| **`regions`** | array | `["us-east-1", "eu-central-1"]` |
| **`new_keys_by_region`** | obj | `{"us-east-1": N, "eu-central-1": M}` |
| **`batch_ids_by_region`** | obj | `{"us-east-1": ["batch_us_1", …], "eu-central-1": […]}` |
| **`purchase_order_ids_by_region`** | obj | `{"us-east-1": "<hex32>", "eu-central-1": "<hex32>"}` |

**⚠️ 关键差异**：一次到货**只推 1 条** webhook · 但 body 里带**两区完整信息**（不同于 kiroappio 分两条推）。我方要从 `purchase_order_ids_by_region` 里按区取幂等键 —— **不是用顶级那个**。

#### `all_keys_dead`

| 字段 | 语义 |
|---|---|
| `event` / `event_id` / `order_id` / `region` / `message` / `dead` | 常规 |

### 3.2 签名（**HMAC 三头** · 我方已接）

| 头 | 值 |
|---|---|
| `X-Kiro-Event-Id` | 事件 id |
| `X-Kiro-Timestamp` | 时间戳 |
| `X-Kiro-Signature` | **`v1=<hex HMAC-SHA256>`** |

**签名原文**：`timestamp.rawBody` · 密钥 `webhook_secret`。
**建议**：拒绝时间戳偏差 > 5 分钟。

### 3.3 重试

Webhook 超时 **8s** · 重试 **3 次**（1s 间隔）。

---

## 4 · 混币计价（6 家里唯一 · 最容易算错）

| 项 | 币种 | 字段示例 |
|---|---|---|
| **在售单价** | **USD** | `price: "7.35"` |
| **账户余额** | **CNY** | `balance: "0.000000"` · `remaining: "…"` |
| **价格保护参数** | **CNY** | `max_total_cny` |
| **退款金额** | **CNY** | `refunded_amount_cny` |

**我方换算链**（`docs/18 §1.3`）：
```
price "7.35" (USD)
  → Money{Amount: 7_350_000, Currency: "USD"}
  → vendor_pricing.credits_per_unit = 6_800_000（我方系统汇率 $1 = ¥6.80）
  → our_unit_credits = 7_350_000 × 6_800_000 / 1_000_000 = 49_980_000（49.98 积分）
```

**⚠️ 汇率分歧**：
- **我方**：6.8（对齐 vendor UI 2026-08-12 展示）
- **xi8**：7.07（= 51.97 CNY / 7.35 USD）
- **差 4%** —— 谁对？`/api/v1/reservation`（支持多货币）可能是权威口径 · **待实测**

---

## 5 · 频率限制

| 项 | 值 |
|---|---|
| Webhook 超时 | 8s |
| Webhook 重试 | 3 次（1s 间隔）|
| API 限流 | **vendor 文档未写** · 页面用的其他 `/api` 未详摸 |

---

## 6 · 特有事实（跟其他 vendor 的差异）

- **混币计价** —— 单价 USD · 余额 CNY · 6 家里唯一非纯积分的
- **数字用字符串**（`price: "7.35"` / `balance: "0.000000"`）· 保留小数 · 6 家里唯一
- **`max_total_cny` 价格保护** · 涨价 409 不扣款 · 6 家里唯一原生支持
- **US/EU 合并 webhook 通知**（`notification_scope: "dual"`）· 一次到货只推 1 条但带两区完整信息 · 6 家里唯一
- **`purchase_order_ids_by_region`** —— 幂等键**按区分开** · 我方要按区取不能用顶级那个
- **`/api/v1/reservation` 完整报价端点** · 支持多货币 · 6 家里唯一
- **`/api/status` 免鉴权** · 探针能不带 key 打
- **`count` / `quantity` 两个参数名都接受**
- **订单状态含 `partially_refunded`** · 6 家里唯一有部分退款态的
- **`refunded_amount_cny`** 记录退款金额
- **HMAC 三头签名**（Event-Id / Timestamp / Signature · `v1=` 前缀）
- **实测只返 us-east-1** —— 文档说支持 `?region=` 但 stock 只给单区 · **EU 定价拿不到**（xi8 有）
- **fleet 观测维度最少** · status 只 4 个字段（其他家 8-12 个）
- **错误格式** `{"error":{"code":"AUTH_REQUIRED","details":{},"message":"…","request_id":"req_…"}}` · **有 request_id**（6 家里唯一 · 便于找 vendor 排查）

---

## 7 · 我方 adapter 缺口（按优先级）

| # | 缺什么 | 影响 | 优先级 |
|---|---|---|---|
| 1 | **`ZoneStock.Zone` 落空** · 没从 `region` 归一 | 侧表 zone 列空 · PricedFor 按 zone 查匹配不到 | **最高 · 是 bug** |
| 2 | **`/api/v1/reservation` 声明了但没实现** | ① 拿不到 EU 定价 ② 拿不到分档报价（`vendor_price_tier` 表因此空着）③ 拿不到权威汇率口径 | **最高** |
| 3 | 汇率分歧未解（我方 6.8 vs xi8 7.07 · 差 4%）| 定价可能系统性偏低 4% | **高** |
| 4 | `purchase_order_ids_by_region` 是否按区取？ | 双区通知用错幂等键 → 拉错区 / 重复扣费 | **高 · 待查 webhookin** |
| 5 | `TotalCost` 字段名是 credits 但这家 CNY 计价 | 需核对 adapter 是否正确处理币种 | **高 · 待查** |
| 6 | `refunded_amount_cny` 不落库 | 部分退款金额丢 | 中 |
| 7 | `partially_refunded` 状态是否处理？ | 部分退款订单可能状态卡住 | 中 · 待查 |
| 8 | webhook 管理 3 端点未接 | 手工配 · 可接受 | 低 |
| 9 | `balance`（CNY 余额）不落 vendor_probe | 跟其他家一致 · 低影响 | 低 |
| 10 | 页面用的其他 `/api` 未详摸 | 可能还有隐藏端点（需 Network 抓）| 中 · **待摸** |
