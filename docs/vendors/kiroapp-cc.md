# Vendor: kiroapp.cc

## 0 · 档案元信息

| 项 | 值 |
|---|---|
| Base URL | `https://kiroapp.cc` |
| 官方文档 | **无独立 `/docs`** · 对接说明写在登录后 "API Key" tab 页面里 · 6 家里**最简的开放 API 面板**。抓取存档 `.playwright-mcp/vendor-scrape-2026-08-12/kiroappcc-docs.txt` |
| 抓取日期 | 2026-08-12 |
| 鉴权（openapi）| `Authorization: Bearer sk-…` |
| 鉴权（`/api/user/*`）| **浏览器会话 Cookie** · 我方 API key 拿不到 |
| 密钥前缀 | **`sk-`**（6 家里唯一 · ⚠️ 跟 OpenAI 的 `sk-` 混淆风险）|
| **官方公开端点** | **5 个 `/openapi/*`** |
| **隐藏内部端点** | **15 个 `/api/user/*`**（页面用 · 官方文档没写 · 需 cookie）|
| **我方 adapter 接了** | **4 个 · 覆盖公开端点 80%** |
| Provider group | `providers.ProviderKiro` |
| Adapter 目录 | `internal/providers/kiro/vendors/kiroappcc/` |
| 计价 | 积分 · **只有一档 · 无区域拆分** |
| 分区 | **完全无区概念** · 6 家里唯一 |
| 质保 | **双条件**（2 小时 **OR** 累计消耗 7000 积分）· 满一即结束 |
| 商业模式 | **车主分成模式**（提供 AWS 母号 · 卖 key 拿收益）|

---

## 1 · 端点清单

### 1.1 官方公开 openapi（5 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 1 | POST | `/openapi/claim` | 提取 1 个 key · body `{}` → `{key, pointsCost}` | `Adapter.Purchase()` | ✅ |
| 2 | POST | `/openapi/claim` + `{count:N}` | **批量提取** → `{keys:[…], pointsCost}` | 同上 | ✅ |
| 3 | GET | `/openapi/stock` | 库存 → `{availableKeys, keyPrice}` · **camelCase** | `Adapter.Stock()` | ✅ |
| 4 | GET | `/openapi/balance` | 积分 → `{balance}` | `Adapter.Balance()` | ✅ |
| 5 | GET | `/openapi/orders` | 历史订单 · **老档案曾判"不存在" · 实测存在** | `Adapter.OrderKeys()` | ✅ |

**⚠️ 关键修正**（`docs/vendors/_endpoints-audit-2026-08-12.md`）：老档案 §7 写"本 vendor 无 `/openapi/orders` 端点 · 不支持补拉" · **实测存在** · 18 条历史订单 · 每条含 `probeState` / `warrantyStatus` / `refundedAt` / `usageSnapshot`。已修正。

### 1.2 隐藏内部端点（15 个 · 页面用 · **需 cookie · 我方拿不到**）

**用户与站点（3 个）**：

| # | 方法 | 路径 | 用途 | 状态 |
|---|---|---|---|---|
| 6 | GET | `/api/user/site-info` | 站点公告 / 时间 | ❌ 需 cookie |
| 7 | GET | `/api/user/announcements` | 公告列表 | ❌ 需 cookie |
| 8 | GET | `/api/user/me` | 当前用户（含积分）· 页面多次调 | ❌ 需 cookie |

**提取 key（1 个）**：

| # | 方法 | 路径 | 用途 | 状态 |
|---|---|---|---|---|
| 9 | POST | `/api/user/claim-preview` | **提取预估** · body 带 count → 返扣多少积分 | ❌ 需 cookie · **有价值**（下单前预估）|

**API Key + Webhook（2 个）**：

| # | 方法 | 路径 | 用途 | 状态 |
|---|---|---|---|---|
| 10 | GET | `/api/user/api-keys` | 自己 API key 列表（不含明文）| ❌ 需 cookie |
| 11 | GET | `/api/user/webhook` | 读 webhook 配置（url + 签名密钥 + 启用状态）| ❌ 需 cookie |

（对应保存未抓到 PUT/POST · 推测同路径 PUT）

**订单 + key（2 个）**：

| # | 方法 | 路径 | 用途 | 状态 |
|---|---|---|---|---|
| 12 | GET | `/api/user/orders?limit=200` | 我的订单 | ❌ 需 cookie（有 `/openapi/orders` 替代）|
| 13 | GET | `/api/user/my-keys` | 我的密钥（历史）| ❌ 需 cookie |

**发车 · 母号供应侧（3 个）**：

| # | 方法 | 路径 | 用途 | 状态 |
|---|---|---|---|---|
| 14 | GET | `/api/user/my-mothers` | 我的母号列表 | ❌ 阶段外 |
| 15 | GET | `/api/user/my-mothers/usage` | 母号用量统计 | ❌ 阶段外 |
| 16 | GET | `/api/user/dispatch?limit=50` | **发车历史** · 含"评价"字段（拉完了 / NPC / 人上人 / 夯）| ❌ 阶段外 |

**收益 · 车主分成（4 个）**：

| # | 方法 | 路径 | 用途 | 状态 |
|---|---|---|---|---|
| 17 | GET | `/api/user/payout-qr` | 收款二维码（我方账号 404 · 说明还没设）| ❌ 阶段外 |
| 18 | GET | `/api/user/earnings` | 收益汇总 | ❌ 阶段外 |
| 19 | GET | `/api/user/settlements` | 结算历史 | ❌ 阶段外 |
| 20 | GET | `/api/user/txns?limit=200` | **积分流水** | ❌ 需 cookie · **有价值**（对账）|

---

## 2 · 逐端点字段清单（vendor 原文命名）

### 2.1 `GET /openapi/stock`

**实测响应**（我方账号 · 2026-08-12）：
```json
{ "availableKeys": 0, "keyPrice": 50 }
```

| 字段 | 类型 | 语义 |
|---|---|---|
| **`availableKeys`** | int | 可购库存 · **camelCase**（6 家里唯一用驼峰的）|
| **`keyPrice`** | int | 单价（积分/个）· **只有一档** |

**⚠️ 这是 6 家里最简的库存端点** —— 2 个字段 · **无区域拆分** · 无质保时长 · 无单次上下限。

**我方 adapter 映射**（`mapper.go`）：

| 我方字段 | 来源 |
|---|---|
| `StockSnapshot.Available` | `availableKeys` |
| `ZoneStock[0].Zone` | **硬编码 `providers.ZoneGeneral`**（无区）|
| `ZoneStock[0].Available` | `availableKeys` |
| `ZoneStock[0].UnitPrice` | `Money{keyPrice × 10^6, "credit"}` |
| `ZoneStock[0].Region` | **空**（vendor 无区概念）|
| `StockSnapshot.MinPerOrder` / `MaxPerOrder` | ❌ **vendor 不返** · 我方留 0 |
| `StockSnapshot.WarrantyMinutes` | ❌ **vendor 不返**（质保信息在 orders 详情里）|

**⚠️ `Region` 空 + `Zone=general` 都是对的** —— vendor 根本没有区域概念。

---

### 2.2 `GET /openapi/balance`

```json
{ "balance": 785 }
```

| 字段 | 语义 |
|---|---|
| `balance` | 我方积分余额 |

**我方 adapter**：`Balance` ← `balance × 10^6`。

---

### 2.3 `POST /openapi/claim`

**请求**：
- 单个：`{}`（空 body）
- 批量：`{"count": N}`

**响应**：
- 单个：`{ "key": "…", "pointsCost": 50 }`
- 批量：`{ "keys": ["…", "…"], "pointsCost": 100 }`

| 字段 | 类型 | 语义 |
|---|---|---|
| `key` | str | 单个提取时的 key |
| `keys` | array of str | 批量提取时 · **字符串数组**（不是对象数组 · 跟 kiro91/kiroceo 差异）|
| **`pointsCost`** | int | ★ **本次总扣积分** · 车主自取时为 **0**（特权）|

**⚠️ 关键差异**：
1. **`keys` 是字符串数组** —— 其他家是对象数组（含 account/password/issuer_url 或 region/paid 等）· 这家只给 key 正文
2. **无 `client_order_id` 幂等键** —— 6 家里唯一不支持幂等的 · 网络超时后**无法安全重试**
3. **车主自取 `pointsCost=0`** —— vendor 档案 §7 明确的特权

**我方 adapter 映射**（`mapper.go:59`）：
- `PurchaseResult.TotalCost` ← `Money{pointsCost × 10^6, "credit"}`
- `PurchaseResult.Keys[].Key` ← `keys[i]`
- `PurchaseResult.Keys[].Paid` ← **留零值**（vendor 只给总额不给逐把）

---

### 2.4 `GET /openapi/orders`（老档案曾判错）

**实测存在** · 18 条历史订单。每条含（★ = 6 家里独有）：

| 字段 | 类型 | 语义 |
|---|---|---|
| `orderNo` | str | 订单号 |
| **`probeState`** | str | ★ **vendor 侧探活状态** |
| **`warrantyStatus`** | str | ★ **质保状态** |
| **`refundedAt`** | str | ★ 退款时刻 |
| **`usageSnapshot`** | obj | ★ **用量快照** |
| （其他字段需再抓一次确认全量）| | |

**我方 adapter 用它**（`Adapter.OrderKeys()`）· 拿全量后从里面挑 `orderNo == 参数 order_id` 那条。

**⚠️ 数据缺口**：`probeState` / `warrantyStatus` / `refundedAt` / `usageSnapshot` **都不落库** —— 这几个是 vendor 侧的**号质量 + 质保 + 用量**信号 · 全丢了。

---

### 2.5 `POST /api/user/claim-preview`（❌ 需 cookie）

**提取预估** · body 带 count → 返扣多少积分。

**为什么有价值**：**下单前预估扣费** —— 我方现在只能凭 `keyPrice × count` 估 · 但混价 / 车主特权 / 分档时会算错。这个端点是权威预估。

**⚠️ 但需 cookie · 我方 API key 打不通**。

---

### 2.6 `GET /api/user/txns?limit=200`（❌ 需 cookie）

**积分流水**。跟其他家的 `/ledger` 对应。

**⚠️ 需 cookie** —— kiroappcc 是 6 家里**唯一没有 openapi 流水端点**的 · 对账数据拿不到。

---

## 3 · Webhook

### 3.1 签名（HMAC-SHA256 · **我方已接**）

| 项 | 值 |
|---|---|
| 签名头 | `X-Kiro-Signature` |
| 算法 | `HMAC-SHA256(secret, body)` |
| 我方已注册 | ✅ |

### 3.2 事件类型

（vendor 页面没给完整事件表 · 需再抓 · 我方 webhookin 已处理注册的那些）

---

## 4 · 质保（**双条件** · 6 家里唯一）

| 条件 | 值 |
|---|---|
| 时间 | **2 小时** |
| 用量 | **累计消耗 7000 积分** |
| 规则 | **满一即结束**（OR 关系）|

**⚠️ 我方缺口**：我方只判时间维度 · **缺积分消耗维度** —— 号在 2 小时内消耗超 7000 积分后质保已结束 · 我方还当它在保。

---

## 5 · 频率限制（openapi）

| 项 | 值 |
|---|---|
| 窗口 | 每 60s 最多 **60 次** |
| 超出 | 进 **180s 冷却** · 返 429 + `Retry-After` |
| 作用域 | **按账号限流** · 多个 API key 共享 |

---

## 6 · 商业模式（车主分成 · 6 家里最特殊）

vendor 页面 6 个 tab 之一是 **"我的收益"** —— **车主是分成模式**：
- 提供 AWS 母号 → vendor 开号 → 卖 key → 车主拿收益
- 首页显示"最近 10 批上架记录"含 **"评价"字段**（`拉完了` / `NPC` / `人上人` / `夯`）· **半人工审核**
- 车主投放 AWS 凭证时 · `/openapi/claim` **优先返自己的号且 `pointsCost=0`**（自留）

**收益端点 4 个**（`earnings` / `settlements` / `payout-qr` / `txns`）· 全需 cookie · 阶段外。

---

## 7 · 特有事实（跟其他 vendor 的差异）

- **API 面板最简**（5 个 openapi 端点）· 无独立 docs 页
- **密钥前缀 `sk-`** · 6 家里唯一（⚠️ OpenAI 混淆风险）
- **响应用 camelCase**（`availableKeys` / `keyPrice` / `pointsCost` / `orderNo` / `probeState`）· 6 家里唯一（其他全 snake_case）
- **完全无区域概念** · 6 家里唯一（其他都至少 us/eu 两区）
- **`keys` 是字符串数组** · 不是对象数组
- **无 `client_order_id` 幂等键** · 6 家里唯一 —— **网络超时无法安全重试**
- **质保双条件**（2h OR 7000 积分）· 6 家里唯一有用量维度的
- **车主分成模式** + **"评价"字段**（拉完了/NPC/人上人/夯）· 半人工审核
- **车主自取 `pointsCost=0`** 特权
- **限流按账号**（多 key 共享）· 60 次/60s · 超出 180s 冷却
- **`/openapi/orders` 有 `probeState` / `warrantyStatus` / `usageSnapshot`** · vendor 侧号质量数据
- **15 个隐藏 `/api/user/*` 端点需 cookie** · 我方拿不到（其他家 API key 能打全部）
- **不在 xi8**（xi8 只对接 5 家 · 这家独立）

---

## 8 · 我方 adapter 缺口（按优先级）

| # | 缺什么 | 影响 | 优先级 |
|---|---|---|---|
| 0 | ~~Zones 留 nil · 这家在定价链上"不存在"~~ | 4209 条探针 · 侧表 0 行 · 无价 · stock-delta 推不出 restock | ✅ **已修**（补 ZoneGeneral · 2026-08-13 生产实测发现）|
| 1 | ~~质保只判时间 · 缺 7000 积分维度 → 退款判断错~~ | ⚠️ **判过头了 · 撤回**：我方退款**完全跟随上游**（`deathwatch.FindRefundable` 要求 `pull_round.status='refunded'` 才退）· 从不独立判质保窗口 · 所以"只判时间"**不会导致错误退款**。<br>**真实缺口小得多**：这家 stock / claim 都不返质保字段（只在 orders 详情有 `warrantyStatus`）· 所以 `credential.warranty_until` 恒空 · **用户端看不到质保信息** · 也展示不出"2h OR 7000 积分"这个条款 | 中 · 展示层 |
| 2 | `/openapi/orders` 的 `probeState` / `warrantyStatus` / `usageSnapshot` 不落库 | vendor 侧号质量数据全丢 | **高** |
| 3 | **无幂等键** · 网络超时无法安全重试 | vendor 侧限制 · 我方只能靠 `pending_purchase` 状态机兜 | **高**（已知限制 · 无解）|
| 4 | `POST /api/user/claim-preview` 需 cookie | 下单前无权威预估 | 中（拿不到）|
| 5 | `GET /api/user/txns` 需 cookie | 对账数据拿不到 | 中（拿不到）|
| 6 | `MinPerOrder` / `MaxPerOrder` vendor 不返 | 我方留 0 · 数量校验只能靠自己配 | 低 |
| 7 | 母号 / 收益 7 端点 | 阶段外 | ❌ |
