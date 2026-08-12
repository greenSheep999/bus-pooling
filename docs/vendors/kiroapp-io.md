# Vendor: kiroapp.io

## 0 · 档案元信息

| 项 | 值 |
|---|---|
| Base URL | `http://kiroapp.io`（vendor 文档**明写 http · 非 https**）|
| 官方文档 | `https://kiroapp.io/api-docs`（登录后 · Next.js SPA）· 抓取存档 `.playwright-mcp/vendor-scrape-2026-08-12/kiroappio-docs.txt` |
| 抓取日期 | 2026-08-12 |
| 鉴权 | `Authorization: Bearer km_…` **或** `X-API-Key: km_…` |
| 密钥前缀 | **`km_`**（6 家里唯一不用 `usr-` / `sk-` 的）|
| **官方端点总数** | **~25 个 · 分 5 组** |
| **我方 adapter 接了** | **8 个 · 覆盖率 32%** |
| Provider group | `providers.ProviderKiro` |
| Adapter 目录 | `internal/providers/kiro/vendors/kiroappio/` |
| 计价 | 积分 · **按母号累计产量分档** · `price_min` / `price_max` 双档 |
| 单价 | us = 80 · eu = 40（2026-08-12 实测）|
| 分页信封 | `{items, total, page, page_size, pages}` · `?page=1&page_size=50`（上限 500）|
| 错误格式 | `{"error": "描述"}` · **无 code 字段**（跟 kiroceo 一样弱）|
| 分区 | `us` / `eu` · **也接受 `us-east-1` / `eu-central-1`** |
| 质保 | `warranty_minutes`（实测 10 分钟）|

---

## 1 · 端点清单（vendor 官方 ~25 个 · 5 组）

### 1.1 账户与积分（3 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 1 | GET | `/api/me/profile` | 用户档案 · `balance` / `min_purchase` / `max_purchase` / `notify_new_batch` | `Adapter.Balance()` · `adapter.go` | ✅ |
| 2 | GET | `/api/me/ledger` | 积分流水 · `?page&page_size&type` · **8+ 种 type** | — | ❌ **未接** |
| 3 | POST | `/api/me/redeem` | 兑换码充值 `{code}` | `Adapter.Redeem()` | ✅ |

### 1.2 提取密钥 · 消费侧（5 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 4 | GET | `/api/me/stock` | 库存 · `stock` / `price_min` / `price_max` / `balance` / `stock_us` / `stock_eu` | `Adapter.Stock()` | ✅ |
| 5 | POST | `/api/me/purchase` | 下单 · `{count, client_order_id, order_id?, region?}` | `Adapter.Purchase()` | ✅ |
| 6 | GET | `/api/me/orders` | 我的订单（分页）| `Adapter.PurchaseHistory()` · `history.go:83` | ✅ |
| 7 | GET | `/api/me/keys` | 我的密钥 · `?history=1` 含失效 | `Adapter.KeyStats()` · `history.go:135` | ✅ |
| 8 | GET | `/api/me/keys/created-at` | 最早密钥时间 + 总数 | — | ❌ **未接** |

**我方还接了**：`GET /api/me/orders/{id}/keys`（补拉 · `adapter.go:118`）· `GET /api/status`（fleet 状态）✅

### 1.3 供应侧 · 我的号池（AWS 母号 · 7 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 9 | POST | `/api/me/accounts` | 提交母号 `{access_key, secret_key, region?, note?, subscription_tier?, gen_mode?}` | ❌ 阶段外 |
| 10 | GET | `/api/me/accounts` | 号池列表（分页）| ❌ 阶段外 |
| 11 | PATCH | `/api/me/accounts/{id}` | 更新母号 `{note?, auto_open?, target_alive?}` | ❌ 阶段外 |
| 12 | DELETE | `/api/me/accounts/{id}` | 删除母号 | ❌ 阶段外 |
| 13 | POST | `/api/me/accounts/{id}/generate` | **手动开号** `{count}` | ❌ 阶段外 |
| 14 | GET | `/api/me/refill-config` | 读自动发车配置 | ❌ 阶段外 |
| 15 | PUT | `/api/me/refill-config` | 改配置 · `refill_enabled` / `refill_low_watermark` / `refill_batch` / `refill_auto_check` | ❌ 阶段外 |

**为什么不接**：供应侧（提交我方 AWS · 系统开号 · 分账）· `CLAUDE.md §3` 明确不做 AWS 开号。

### 1.4 API 令牌（3 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 16 | GET | `/api/me/tokens` | 令牌列表（不含明文）| ❌ 不需要 |
| 17 | POST | `/api/me/tokens` | **签发令牌** `{name?, expires_in_days?}` · 明文只返一次 | ❌ 不需要 |
| 18 | DELETE | `/api/me/tokens/{id}` | 吊销令牌（立即生效）| ❌ 不需要 |

**⚠️ 独家能力**：**API 令牌可以自己签发**（不用找 admin）· 6 家里唯一。令牌只能调 `/api/me/*` 不能调 `/api/admin/*`。

### 1.5 公开（1 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 19 | GET | `/api/status` | fleet 状态 · 含 `generating` / `uptime_seconds` / `started_at` | `Adapter.PublicStatus()` | ✅ |

---

## 2 · 逐端点字段清单（vendor 原文命名）

### 2.1 `GET /api/me/profile`

| 字段 | 类型 | 语义 |
|---|---|---|
| `balance` | int | 我方积分余额 |
| `min_purchase` | int | 单次下限 |
| `max_purchase` | int | 单次上限 |
| **`notify_new_batch`** | bool | ★ 是否推送新批次通知 |

**我方 adapter 映射**：`Balance` ← `balance × 10^6`。

**⚠️ 数据缺口**：`notify_new_batch` 不落库（低影响）。

---

### 2.2 `GET /api/me/stock`

**实测响应**（我方账号 · 2026-08-12）：
```json
{
  "balance": 0, "max": 10,
  "price": 80, "price_us": 80, "price_eu": 40,
  "stock": 0, "stock_us": 0, "stock_eu": 0,
  "warranty_minutes": 10
}
```

| 字段 | 类型 | 语义 |
|---|---|---|
| `stock` | int | 总库存 |
| **`stock_us`** | int | ★ **美区库存**（平铺 · 不是数组）|
| **`stock_eu`** | int | ★ **欧区库存** |
| `price` | int | 默认单价（= `price_us`）|
| **`price_us`** | int | ★ **美区单价**（平铺）|
| **`price_eu`** | int | ★ **欧区单价** |
| `balance` | int | 我方积分余额 |
| `max` | int | 单次最多提取 |
| `warranty_minutes` | int | 质保时长 |

**⚠️ 结构特点**：**没有 `zones[]` / `regions[]` 数组** —— 用 `_us` / `_eu` 后缀**平铺**。6 家里唯一这么干的（kiroappcc 是完全无区）。

**官方文档还提到** `price_min` / `price_max`（分档区间）· **实测响应里没有** —— 可能只在有货时才返。

**我方 adapter 映射**（`mapper.go:112`）：

| 我方字段 | 来源 |
|---|---|
| `StockSnapshot.Available` | `stock` |
| `StockSnapshot.MaxPerOrder` | `max` |
| `StockSnapshot.WarrantyMinutes` | `warranty_minutes` ✅ |
| `ZoneStock[0].Zone` | **硬编码 `providers.ZoneUS`** |
| `ZoneStock[0].Available` | `stock_us` |
| `ZoneStock[0].UnitPrice` | `Money{price_us × 10^6, "credit"}` |
| `ZoneStock[1].Zone` | **硬编码 `providers.ZoneEU`** |
| `ZoneStock[1].Available` | `stock_eu` |
| `ZoneStock[1].UnitPrice` | `Money{price_eu × 10^6, "credit"}` |
| `ZoneStock[].Region` | **空**（vendor 不返 region 字段）|

**⚠️ 这家的 `Region` 空是对的** —— vendor 平铺结构里根本没有 region 概念。

**⚠️ 数据缺口**：`price_min` / `price_max`（分档区间）不落库 —— 想显示"80~120 积分"区间就没数据。

---

### 2.3 `POST /api/me/purchase`

**请求**：`{ "count": 5, "client_order_id": "…", "order_id": "…", "region": "us" }`

| 字段 | 必填 | 说明 |
|---|---|---|
| `count` | ✓ | 范围 `min_purchase` ~ `max_purchase` |
| `client_order_id` | ★ | 幂等键 |
| **`order_id`** | ✗ | ★ **webhook 里的 order_id** · 精确指定批次 |
| `region` | ✗ | `us` / `eu` · 也接受 `us-east-1` / `eu-central-1` |

**vendor 明说的定价特点**：
- **单价按【母号累计产量】分档** · 同一单各 key 可能不同价
- 响应 `keys[].price` **每个 key 独立价**
- **`total_debit` 是权威扣费**

**响应关键字段**：

| 字段 | 语义 |
|---|---|
| `purchased` | 实际成交 |
| **`keys[].price`** | ★ **每把 key 独立价**（混价单）|
| **`total_debit`** | ★ **权威扣费额** |

**我方 adapter 映射**：
- `PurchaseResult.TotalCost` ← `total_debit × 10^6`

**⚠️ 数据缺口**：`keys[].price` 逐把价**不落库**（跟 kiro91 的 `keys[].paid` 同一个问题）。

---

### 2.4 `GET /api/me/keys` · `?history=1`

分页 · `{items[], total, page, page_size, pages}`。

**我方 adapter**（`history.go:135`）· 走 `?page=N&page_size=100` 遍历。

---

### 2.5 `GET /api/me/orders`

分页 · 同上信封。**我方 adapter**（`history.go:83`）。

---

### 2.6 `GET /api/me/ledger`（❌ 未接）

积分流水 · `?page&page_size&type` · **8+ 种 type**（vendor 文档没列全）。

**为什么该接**：跟 kiro91 / kirooo 一样 —— 对账需要 vendor 侧流水。

---

### 2.7 `GET /api/status`

**实测响应**：
```json
{
  "auto_check": true, "auto_generate": true,
  "captcha_app_id": "…", "captcha_enabled": false,
  "generating": false,
  "price": 80, "price_us": 80, "price_eu": 40,
  "started_at": "…", "stock": 0, "stock_us": 0, "stock_eu": 0,
  "uptime_seconds": 12345
}
```

| 字段 | 语义 |
|---|---|
| `generating` | **是否正在开号** |
| `started_at` / `uptime_seconds` | vendor 服务运行时长 |
| `stock` / `stock_us` / `stock_eu` | 库存（跟 `/api/me/stock` 重复）|
| `price` / `price_us` / `price_eu` | 单价（重复）|
| **`auto_check`** | ★ 自动探活是否开 |
| **`auto_generate`** | ★ 自动开号是否开 |
| **`captcha_enabled`** / **`captcha_app_id`** | ★ 验证码配置 |

**⚠️ 特点**：这个端点**同时给库存和价格**（跟 `/api/me/stock` 重复）· 而且**免鉴权**（探针能不带 key 打）。

**⚠️ 数据缺口**：`auto_check` / `auto_generate` **不落库** —— 这两个是"vendor 是否在自动补货"的信号 · 判断 vendor 健康度有用。

**⚠️ vendor 特点**：实测 `generating=true` 但 `stock=0` 是常态 —— **一直在发号但库存被抢空**。

---

## 3 · Webhook

### 3.1 事件类型（**4 种** · 比 kiroceo 多一个）

| `event` | 语义 | 独家字段 |
|---|---|---|
| `new_keys_available` | 批次开号完成 | `order_id` / `mother_id` / `stock_us` / `stock_eu` / `price_us` / `price_eu` / **`visibility`** |
| `all_keys_dead` | 本轮全灭 · 系统自动补 | `dead` |
| **`key_revoked_abuse`** | ★ **已售 key 用量异常被收回** | `key_prefix` / `avg_per_min` / `threshold` |
| `test` | 手工测试 | — |

**⚠️ `key_revoked_abuse` 是 6 家里独有的事件** —— vendor 主动收回已卖出的 key（因为用量异常）。**我方是否处理了？** 如果没处理 · 号被收回我方还当它活着 → 用户拿到废号。

### 3.2 `visibility` 字段

| 值 | 语义 |
|---|---|
| `private` | 池主自用（**免费自提**）|
| `public` | 上架公开池 |

### 3.3 载荷关键字段（vendor 标注"恒存在"的每次必有）

**恒存在**：`event` / `event_id` / `message` / `stock_us` / `stock_eu` / `price_us` / `price_eu`

**⚠️ vendor 明说**：`stock_*` / `price_*` **永远有 · 值 0 有意义**（不是"缺字段"）。

**仅特定事件**：`new_keys` / `order_id` / `purchase_order_id` / `mother_id` / `supplied_count` / `finished_at` / `visibility`

### 3.4 幂等

- `purchase_order_id` 由 **(批次 + 收件人) 确定性派生** · 重试重复推送同一值
- 收到后直接 `POST /api/me/purchase` · `client_order_id` 用 `purchase_order_id` · `order_id` 用 `order_id`
- 同 `client_order_id` 换 `count` 重放返 **409**
- **`test` 事件按标准流程 purchase 会得到 404** · 不扣费

---

## 4 · 特有事实（跟其他 vendor 的差异）

- **Base URL 明写 http**（非 https）· 6 家里唯一
- **密钥前缀 `km_`** · 6 家里唯一（其他 `usr-` / `sk-`）
- **库存 / 价格用 `_us` / `_eu` 平铺** · 没有 zones[] 数组 · 6 家里唯一
- **`/api/status` 同时给库存 + 价格 + 免鉴权** · 跟 `/api/me/stock` 重复
- **API 令牌可自己签发**（3 个端点）· 6 家里唯一
- **`key_revoked_abuse` webhook 事件** · vendor 主动收回已售 key · 6 家里唯一
- **`visibility` 字段区分 private / public** · 免费自提 vs 公开池
- **webhook 恒存在字段里含 `stock_*` + `price_*`** · 不用再查库存就知道现价
- **分档定价按母号累计产量** · `price_min` / `price_max` 区间
- **AWS 母号供应侧 7 个端点** · 含 `target_alive`（目标存活数）
- **`refill_config` 自动发车 4 参数**（enabled / low_watermark / batch / auto_check）
- **错误格式 `{"error": "…"}`** · 无 code 字段（跟 kiroceo 一样弱）
- **分页信封 `{items, total, page, page_size, pages}`** · 上限 500

---

## 5 · 我方 adapter 缺口（按优先级）

| # | 缺什么 | 影响 | 优先级 |
|---|---|---|---|
| 1 | **`key_revoked_abuse` webhook 是否处理？** | 号被 vendor 收回我方还当它活 → 用户拿废号 | **最高 · 先查 webhookin** |
| 2 | `keys[].price` 逐把价不落库 | 混价单对账缺权威值 | **高** |
| 3 | `GET /api/me/ledger` | 无 vendor 侧流水做对账 | **中** |
| 4 | `price_min` / `price_max` 不落库 | 做不了"80~120 积分"区间展示 | 中 |
| 5 | `/api/status` 的 `auto_check` / `auto_generate` 不落库 | 判断 vendor 是否在自动补货的信号丢了 | 中 |
| 6 | `notify_new_batch` 不落库 | 低影响 | 低 |
| 7 | `GET /api/me/keys/created-at` | 用途有限 | 低 |
| 8 | 供应侧 7 端点 · 令牌 3 端点 | 阶段外 / 不需要 | ❌ |
