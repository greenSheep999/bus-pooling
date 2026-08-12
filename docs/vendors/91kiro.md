# Vendor: 91kiro (`api.91kiro.com`)

## 0 · 档案元信息

| 项 | 值 |
|---|---|
| Base URL | `https://api.91kiro.com` |
| 官方文档 | `GET /api/docs`（Markdown · 公开可读 · 21 KB）· 抓取存档 `.playwright-mcp/vendor-scrape-2026-08-12/kiro91-docs.md`（684 行 · **6 家里最详细**）|
| 抓取日期 | 2026-08-12 |
| 存活探活 | `GET /health` = 200 · `GET /api/docs` = 200 |
| 鉴权 | `X-API-Key: usr-…` **或** `Authorization: Bearer usr-…` · 也支持会话 cookie（写请求需 CSRF 头）|
| 密钥前缀 | `usr-` |
| **官方端点总数** | **31 个** |
| **我方 adapter 接了** | **8 个 · 覆盖率 26%** |
| Provider group | `providers.ProviderKiro` |
| Adapter 目录 | `internal/providers/kiro/vendors/kiro91/` |
| 计价 | **积分**（1:1 充值）· 每区独立单价 · **按存活时长降价** |
| 质保 | 默认 10 分钟 · `warranty_minutes` 可变 · **有 `warranty_refund` webhook** |

文档自述：**"这份文档可以整份粘给 AI，让它替你写客户端。全部字段、错误码、幂等规则与签名算法都在里面。"**

---

## 1 · 端点清单（vendor 官方 31 个）

### 1.1 账号（5 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 1 | GET | `/api/my/profile` | 账号 + 余额 + 持号上限 | `Adapter.Balance()` · `adapter.go:148` | ✅ |
| 2 | PUT | `/api/my/settings` | 改持有上限 `{max_keys_held}` · 0–1000 · 0=不限 | — | ❌ **未接** |
| 3 | POST | `/api/my/password` | 改密码 · 成功后所有设备登出（API 令牌不受影响）| — | ❌ 不需要 |
| 4 | POST | `/api/my/api-key/rotate` | 轮换令牌 · **只接受会话鉴权** · 用令牌调返 `403 session_required` | — | ❌ 不可能（需 web session）|
| 5 | GET | `/api/my/ledger` | 积分流水 · `reason` 7 种 | — | ❌ **未接** |

### 1.2 库存与车次（3 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 6 | GET | `/api/my/stock` | 库存 + `zones[]` 各区单价 + 质保时长 | `Adapter.Stock()` · `adapter.go:70` | ✅ |
| 7 | GET | `/api/my/stock/rounds` | **逐车次**库存 · 带 `current_price` + 降价参数 | — | ❌ **未接 · 高价值** |
| 8 | GET | `/api/my/rounds` | 与我有关的车次（我开的 + 我买过的）| `Adapter.Rounds()` | ✅ |

### 1.3 领取与补拉（4 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 9 | POST | `/api/my/purchase`（别名 `/api/me/purchase`）| 提货 · 幂等 32-hex | `Adapter.Purchase()` · `adapter.go:98` | ✅ |
| 10 | GET | `/api/my/orders/{order_id}/keys` | 按订单补拉 · 原样返当时交付 | `Adapter.OrderKeys()` · `adapter.go:124` | ✅ |
| 11 | GET | `/api/my/orders`（别名 `/api/my/purchase-orders`）| 历史订单 · `?limit=&offset=`（上限 200）| `Adapter.PurchaseHistory()` | ✅ |
| 12 | GET | `/api/my/keys` | 已领 key 列表 · **只给前缀不给正文** | `Adapter.KeyStats()`（`?history=1`）| ✅ |

### 1.4 积分（1 个 + ledger 见 §1.1）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 13 | POST | `/api/my/redeem` | 兑换码换积分 · 返 `{quota, balance}` | `Adapter.Redeem()` | ✅ |

### 1.5 母号 / 供应侧（8 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 14 | POST | `/api/my/mothers` | 提交 AWS 母号 · 立刻 STS 验证 · AK/SK 加密落库 | ❌ 阶段外 |
| 15 | GET | `/api/my/mothers` | 母号列表 · 带 `queue_position` / `queue_total` | ❌ 阶段外 |
| 16 | PUT | `/api/my/mothers/{id}` | 改母号 | ❌ 阶段外 |
| 17 | POST | `/api/my/mothers/{id}/pool` | 改发车池 `{pool: public\|private}` · 有未结束车次返 `409 mother_busy` | ❌ 阶段外 |
| 18 | POST | `/api/my/mothers/{id}/status` | 停用 / 启用 | ❌ 阶段外 |
| 19 | POST | `/api/my/mothers/{id}/verify` | 重新 STS 验证 | ❌ 阶段外 |
| 20 | POST | `/api/my/mothers/{id}/quota` | 改额度 | ❌ 阶段外 |
| 21 | DELETE | `/api/my/mothers/{id}` | 删母号（有未结束车次不允许 · 先停用）| ❌ 阶段外 |

**为什么不接**：这是**供应侧**（我方交 AWS 账号给 vendor 开号 · 分成）· `CLAUDE.md §3` 明确不做 AWS 开号（阶段 3b/3c 才转发）。

### 1.6 Webhook 管理（5 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 22 | GET | `/api/my/webhook` | 读双通道配置 `{private_url, public_url}` | ❌ **未接** |
| 23 | PUT | `/api/my/webhook` | 设双通道 | ❌ 未接（手工在后台配）|
| 24 | POST | `/api/my/webhook/test` | 发测试 `{channel: private\|public}` | ❌ 未接 |
| 25 | POST | `/api/my/webhook/rotate` | 轮换 webhook 签名密钥 | ❌ 未接 |
| 26 | GET | `/api/my/webhook/deliveries` | **投递历史**（成功/失败记录）| ❌ **未接 · 排查漏推用** |

### 1.7 Key 剩余额度（3 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 27 | GET | `/api/my/usage` | 汇总名下全部 key 剩余额度 `{remaining, total, synced, keys}` | ❌ **未接** |
| 28 | POST | `/api/my/keys/{id}/usage` | 同步单把 key · 返 `{used, max, remaining, subscription, reset_days, checked_at, error}` | ❌ **未接** |
| 29 | POST | `/api/my/keys/usage/refresh` | 批量同步 · 返 `{usages[], total, failed}` | ❌ **未接** |

**为什么重要**：这是**上游侧的 key 用量**（Kiro 官方额度剩多少）· 我方目前靠 `kiro.rs` 探测 · 但 vendor 有**权威额度数字**（含 `subscription` 类型 + `reset_days` 重置周期）· 比我方探测更准。

---

## 2 · 逐端点字段清单（vendor 原文命名）

### 2.1 `GET /api/my/profile`

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

| 字段 | 类型 | 语义 |
|---|---|---|
| `profile.id` | str | vendor 侧我方账号 id |
| `profile.username` | str | 用户名 |
| `profile.role` | str | `user` / 其他 |
| `profile.balance` | int | **我方在 vendor 侧的积分余额** |
| `profile.spent` | int | 累计已花积分 |
| `profile.earned` | int | 累计赚取积分（供应侧分成）|
| `profile.max_keys_held` | int | **我方自己设的持号上限** · 0–1000 · 0 = 不限 |
| `profile.hold_cap_effective` | int | **叠加 vendor 全局硬顶后真正生效的上限** · min(我设值, 全局硬顶) · 0 = 不限 |
| `profile.keys_held` | int | **名下当前存活 key 数** |
| `profile.api_key_prefix` | str | 我方 key 前缀（脱敏展示）|
| `profile.webhook_private_url` | str | 自己车通道 webhook |
| `profile.webhook_public_url` | str | 公共车通道 webhook |
| `profile.created_at` | str ISO | 账号创建时刻 |
| `profile.last_login_at` | str ISO | 最近登录 |
| `auth_mode` | str | `api_key` / `session` · 本次请求的鉴权方式 |

**我方 adapter 映射**：`Balance` ← `profile.balance × 10^6`。

**⚠️ 数据缺口**：`hold_cap_effective` / `keys_held` **不落库** —— vendor 明说"买之前用这两个数判一下能省掉一次注定失败的下单"· 我方没用。

---

### 2.2 `GET /api/my/stock`

```json
{
  "stock": { "public_available": 12, "my_private": 0, "my_keys": 27 },
  "zones": [
    { "zone": "us", "region": "us-east-1",    "available": 8, "unit_price": 25, "base_price": 40 },
    { "zone": "eu", "region": "eu-central-1", "available": 4, "unit_price": 10, "base_price": 10 }
  ],
  "max": 12,
  "min_per_order": 1,
  "max_per_order": 200,
  "warranty_minutes": 10
}
```

| 字段 | 类型 | 语义 |
|---|---|---|
| `stock.public_available` | int | **公共车当前可买余量合计** · 先到先得 |
| `stock.my_private` | int | 我自己车里可领数量（**免费**）|
| `stock.my_keys` | int | 我已领走的 key 总数 |
| `zones[].zone` | str | `us` / `eu` · **权威地区标识** |
| `zones[].region` | str | `us-east-1` / `eu-central-1` · **完整 AWS region**（决定打哪个端点）|
| `zones[].available` | int | 该区可购量 · 按 `us`/`eu` **固定顺序给全** · 没货的区为 0 |
| `zones[].unit_price` | int | 该区**现价**（已按存活时长降过）· 可直接展示 |
| `zones[].base_price` | int | 该区**基准价**（未降价时 = unit_price）· 想显示"原价 40 → 现价 25"用它 |
| `max` | int | 当前一次性最多能提数量（= 公共余量 · 封顶 200）· **轮询兜底先看它 > 0** |
| `min_per_order` | int | 单次下限 · 1 |
| `max_per_order` | int | 单次上限 · 200 |
| `warranty_minutes` | int | 当前质保时长（分钟）· 0 = 未开启 |

**vendor 明说**：`unit_price` / `base_price` 是**取数那一刻的快照** · 价格随车次存活时长每隔几分钟降一档 · 展示价要跟着轮询刷新别缓存太久。

**我方 adapter 映射**：

| 我方字段 | 来源 |
|---|---|
| `StockSnapshot.Available` | `stock.public_available` |
| `StockSnapshot.MinPerOrder` | `min_per_order` |
| `StockSnapshot.MaxPerOrder` | `max_per_order` |
| `StockSnapshot.WarrantyMinutes` | `warranty_minutes` ✅ |
| `ZoneStock[].Zone` | `zones[].zone` |
| `ZoneStock[].Region` | `zones[].region` ✅ **6 家里唯一两个都给的** |
| `ZoneStock[].Available` | `zones[].available` |
| `ZoneStock[].UnitPrice` | `Money{unit_price × 10^6, "credit"}` |

**⚠️ 数据缺口**：`base_price`（原价）**不落库** —— 想做"原价划掉 → 现价"展示就缺这个数。`stock.my_private` / `my_keys` 也不落库。

---

### 2.3 `GET /api/my/stock/rounds`（❌ 未接 · 高价值）

vendor 明说这个端点每行多给 `current_price` + 降价参数：

| 字段 | 语义 |
|---|---|
| `current_price` | 该车次**现价**（服务端用与计费同一公式算）|
| `unit_price` | 该车次**基准价** · ⚠️ 开了降价后 ≠ 实扣 |
| `decay_minutes` | 每 N 分钟降一档 |
| `decay_amount` | 每档降 M 积分 |
| `price_floor` | 降价封底 F |
| `launched_at` | 发车时刻（降价起算点）|
| `remaining` | 该车次剩余可买 |

**降价公式**（vendor 原文）：
```
现价 = max(基准价 − ⌊已存活分钟 / decay_minutes⌋ × decay_amount, price_floor)
```
三个参数都是 0 表示这一组没开降价。

**为什么该接**：
1. **精确到车次的定价** —— 现在我方只看 `zones[].unit_price`（该区最便宜一档）· 同区多辆车混价时看不到分布
2. **本地重算现价** —— 有 `decay_*` + `launched_at` 就能每分钟本地算 · 不用轮询刷新
3. vendor 推荐的兜底流程就是**每 60s 查这个端点**看 `remaining > 0` 再买（我方现在查 `/api/my/stock`）

---

### 2.4 `GET /api/my/rounds`

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

| 字段 | 语义 |
|---|---|
| `id` | 车次 id |
| `mother_id` | 母号 id |
| `owner_id` | 归属人 |
| `visibility` | `public`（发库存 · 全站可买）/ `private`（留自用 · 只归属人免费领）|
| `scope` | `platform`（平台自有母号）/ 其他 |
| `state` | **7 态**：`preparing` → `standby` → `live` → `dying` → `dead` · 另 `failed`（开号失败）/ `scrapped`（发车前母号已死）|
| `keys_total` | 该车产出总数 |
| `unit_price` | ⚠️ **基准价** · 不是实扣 |
| `launched_at` / `died_at` / `death_reason` | 生命周期 |
| `is_mine` | 是否我的母号开的 |

**⚠️ 这就是 `CLAUDE.md §12.5` 提的"旧项目 7 态全暴露"那个反例的源头** —— vendor 侧确实是 7 态 · 我方对乘客只收敛成"活/死"二态。

---

### 2.5 `POST /api/my/purchase`

**请求**：
```json
{ "count": 5, "zone": "us", "client_order_id": "0a1b2c3d4e5f60718293a4b5c6d7e8f9" }
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `count` | ✓ | 1–200 |
| `client_order_id` | ★ | **32 位十六进制** · 或 `Idempotency-Key` header（两者都给且不一致 → 400）· 不传服务端生成但无法重放 |
| `zone` | ✗ | 不传 = **只从美区取** · 缺货就缺货不会用欧区顶 · `"eu"` 显式 · **其他值 400 `bad_zone`** |

**响应**：
```json
{
  "client_order_id": "0a1b…",
  "order_id": "…",
  "zone": "us",
  "purchased": 5,
  "unit_price": 30,
  "total_credits": 150,
  "remaining": 4500,
  "keys": [{
    "id": "…", "round_id": "…",
    "key": "ksk_…",
    "region": "us-east-1", "zone": "us",
    "free": false, "paid": 30,
    "warranty_until": "2026-08-01T12:34:56Z"
  }],
  "free_count": 0,
  "warranty_until": "2026-08-01T12:34:56Z",
  "warranty_minutes": 10
}
```

| 字段 | 类型 | 语义 |
|---|---|---|
| `purchased` | int | **实际成交** · vendor 明说"务必按它而非 count 处理" |
| `unit_price` | int | ⚠️ **混价单里只是其中一把的价** · 乘出来会跟实扣不一致 |
| `total_credits` | int | ★ **权威扣款额** · 恒等于 Σ`keys[].paid` |
| `remaining` | int | 扣款后余额 |
| `keys[].id` | str | vendor 侧 key id · 补拉 / 用量同步用 |
| `keys[].round_id` | str | 来自哪辆车 |
| `keys[].key` | str | ★ **就是商品**（`ksk_` 前缀）|
| `keys[].region` | str | `us-east-1` / `eu-central-1` · **决定打哪个 AWS 端点** |
| `keys[].zone` | str | `us` / `eu` |
| `keys[].free` | bool | 是否免费（留自用车）|
| `keys[].paid` | int | ★ **每把实际扣的积分**（质保能退的金额）|
| `keys[].warranty_until` | str ISO | 该把质保截止 · 空 = 无质保（免费交付）|
| `free_count` | int | 本单免费数量 |
| `warranty_minutes` | int | 质保时长 |

**⚠️ 2026-08-07 vendor 变更**：**不再下发 `account` / `password` / `issuer_url`**（子号网页登录凭据）· 也不再下发 `endpoint`（由 region 唯一决定）。**只给 `key`**。理由是安全（同一子号从多 IP 使用 = 凭证泄露特征 → 整号被封）。

**我方 adapter 映射**：
- `PurchaseResult.TotalCost` ← `total_credits × 10^6`
- `PurchaseResult.Keys[].Key` ← `keys[].key`

**⚠️ 数据缺口**：`keys[].paid`（逐把实付）/ `keys[].round_id` / `keys[].warranty_until` / `keys[].free` **都不落库** —— 混价单对账 + 质保追踪都缺。

---

### 2.6 `GET /api/my/keys`

已领 key 列表 · **只给前缀不给正文**（要正文走 `/api/my/orders/{id}/keys` 补拉）。

`status` 三态：

| 值 | 语义 |
|---|---|
| `sold` | 正常 · 已交付 |
| `dead` | 探测确认失效（整车判死也变这个）· **质保期内自动退款** |
| `revoked` | **被吊销** · 作废且**不退积分** · 原因：公开分发导致一把 key 挂几十人调用量 → 母号被上游判异常 → 整车提前死 |

**vendor 明说**：判断 key 还能不能用**看 `status`** · 不要看 `remaining`。

---

### 2.7 `GET /api/my/orders`（别名 `/api/my/purchase-orders`）

`?limit=&offset=` · limit 上限 200。每条含：

| 字段 | 语义 |
|---|---|
| `id` | 订单号 |
| `client_order_id` | 幂等键 |
| `count` | 数量 |
| `unit_price` | ⚠️ 基准价 · 混价单不能乘 |
| `charged` | ★ **实扣值** · 对账用这个 |
| `free_count` | 免费数量 |
| `created_at` | 下单时刻 |

---

### 2.8 `GET /api/my/ledger`（❌ 未接）

积分流水 · `reason` 7 种：

| 值 | 语义 |
|---|---|
| `recharge` | 兑换码充值 |
| `purchase` | 领 key 扣费（**唯一扣费时机**）|
| `income` | 别人买走我交给自动车的 key · 按 `实付 × (100 − 平台服务费%)` 返 |
| `warranty` | 质保退款 |
| `clawback` | 质保退款冲回（我供的 key 被退款 · 当初分我那笔冲回）|
| `adjust` | 运营手工调整 |
| `commit` | 历史遗留（早期发车时自动扣费 · 已不产生新的）|

**为什么该接**：我方对账现在只有自己账本 · 拿不到 vendor 侧流水做双向核对。

---

### 2.9 Key 剩余额度三端点（❌ 全未接）

**vendor 明确区分两个"积分"**：

| 概念 | 语义 | 端点 |
|---|---|---|
| **平台积分** | 我方在 vendor 的余额 · 领 key 时扣 | `/api/my/profile` 的 `balance` |
| **Key 剩余额度** | 这把 `ksk_` 在 Kiro 侧还能调多少次 | 本节三个端点 |

**`GET /api/my/usage`**（汇总）：
```json
{ "usage": { "remaining": 4820, "total": 6000, "synced": 12, "keys": 15 } }
```
`synced < keys` 表示还有 key 从未同步成功 · 那部分**没计入 remaining** · 不是"没额度"。

**`POST /api/my/keys/{id}/usage`**（单把同步 · 响应恒 200）：
```json
{ "usage": {
  "key_id": "…", "used": 180, "max": 500, "remaining": 320,
  "subscription": "Kiro Pro", "reset_days": 12,
  "checked_at": "2026-07-31T01:20:00Z", "error": ""
}}
```
`error` 非空 = 这次同步失败（通常 Kiro 限流）· **上次成功的数字保留不清空**。

**`POST /api/my/keys/usage/refresh`**（批量）：
```json
{ "usages": [ /* 同上 */ ], "total": 15, "failed": 2 }
```
部分失败是常态 · 服务端有并发上限不会把 Kiro 打成 429。

**vendor 建议用法**：交付后同步一次 · 之后按小时同步 · 不要每次请求前都同步。`remaining` 掉到 `max` 10% 以下就该换 key。

**为什么该接**：
- `subscription`（Kiro Pro 等）+ `reset_days`（额度重置周期）**是我方 kiro.rs 探测拿不到的**
- 我方现在判 key 存活靠 kiro.rs 探活 · 但"额度快用完"这个维度完全没有
- 关系到"号还能用多久"的用户体验（`credential.quota` 展示）

---

## 3 · Webhook

### 3.1 双通道配置

| 通道 | 何时触发 | 字段 |
|---|---|---|
| 自己车 | 我方母号开号完成 · 只通知我 | `private_url` · 留空回落公共 |
| 公共车 | 平台公共池补货 | `public_url` |

地址必须 http/https · **不能指向内网 / 回环 / 云元数据地址** · 不能在 URL 里带账号口令。

管理端点：`GET/PUT /api/my/webhook` · `POST /api/my/webhook/test`（`{channel: private|public}`）· `POST /api/my/webhook/rotate` · `GET /api/my/webhook/deliveries`（**投递历史**）。

### 3.2 事件类型（5 种）

| `event` | 语义 | 关键字段 |
|---|---|---|
| `new_keys_available` | 有新库存可买 · **我方主要靠这条** | `zone` · `purchase_order_id`（**当幂等键不是订单号**）· `new_keys` · `pool_id` · `visibility` · `timestamp` |
| `reserved_keys_delivered` | **包量预留已按协议价交付** · 钱已扣号已是我的 | `order_id`（★ 拿它补拉取正文）· `zone` · `region` · `new_keys` · `unit_price` · `round_id` · `mother_id` |
| `all_keys_dead` | 本车全部失效 | `round_id` · `dead` · （自己车还带 `mother_id`）|
| `warranty_refund` | 质保期内车次失效 · 积分已退 | `round_id` · `refunded_quota` · `refunded_keys` · `reason` |
| `webhook_test` | 点测试时的探测 | — |

### 3.3 ⚠️ 两条事件处理方式完全相反（vendor 明确警告）

| | `new_keys_available` | `reserved_keys_delivered` |
|---|---|---|
| 含义 | 有货了 · **去买** | 已经买好了 · **是你的了** |
| 钱 | 还没扣 | 已按协议价扣完 |
| 该做什么 | 拿 `purchase_order_id` 调 purchase | 拿 `order_id` 调补拉接口取正文 |
| 再调 purchase | 正常成交 | **会按公共价再买一批** · 不要这么做 |

**vendor 明说**：包量预留是服务端代下单的 · 我方那边没有任何请求也就没有响应能带出 key 正文 · 而 `GET /api/my/keys` 只给前缀 —— **这条通知里的 `order_id` 是取到正文的唯一入口**。漏处理的后果：钱扣了 · 号记在我名下 · 但程序永远拿不到。

**⚠️ 待查**：我方 `internal/webhookin/` 是否处理了 `reserved_keys_delivered`？若签了包量协议且没处理 · 会丢号。

### 3.4 载荷示例

`new_keys_available`：
```json
{
  "event": "new_keys_available",
  "event_id": "去重用的唯一 id",
  "visibility": "public",
  "message": "美国区新增 20 个 Key 已就绪，可提货",
  "new_keys": 20,
  "zone": "us",
  "purchase_order_id": "32 位十六进制",
  "pool_id": "产出该批货的母号 id",
  "timestamp": 1785000000
}
```

**这条不带** `order_id` / `round_id` / `unit_price` —— 要看价先查 `/api/my/stock`。

⚠️ **`purchase_order_id` 不是订单号 · 别拿它调补拉接口**（会 404 —— 此刻还没有订单）。它是 vendor 为这批货预生成的**幂等键**。

### 3.5 请求头 + 签名

| 头 | 说明 |
|---|---|
| `X-KM-Event` | 事件名 |
| `X-KM-Event-Id` | 去重 id |
| `X-KM-Timestamp` | Unix 秒 |
| `X-KM-Delivery-Attempt` | 第几次（最多 3）|
| `X-KM-Signature` | `sha256=<hex>` |

**签名原文**：`timestamp + "." + 原始请求体` · HMAC-SHA256 · **用原始字节校验**不要先解析再重新序列化。建议拒绝 timestamp 偏差 > 5 分钟。

### 3.6 重试

非 2xx 重试 3 次 · 间隔递增带抖动 · 同事件多次尝试携带相同 `event_id`。

---

## 4 · 错误码

**统一形状**（`error` 是 `message` 的别名）：
```json
{ "code": "no_stock", "message": "暂无可交付库存，请稍后重试", "error": "…" }
```
**优先判 `code`**（稳定标识）· 不要判文案。

| HTTP | `code` | 处理 |
|---|---|---|
| 400 | `bad_json` | 请求体非法 JSON / 含未知字段 |
| 400 | `bad_order_id` | 幂等键不是 32-hex |
| 400 | `bad_count` | 数量超 1–200 |
| 400 | `bad_zone` | zone 不是 us/eu |
| 400 | `idempotency_conflict` | body 与 header 幂等键不一致 |
| 401 | `unauthenticated` / `invalid_api_key` | 检查令牌 |
| 402 | `insufficient_balance` | 余额不足 |
| 403 | `disabled` | 账号停用 |
| 403 | `csrf_failed` | cookie 调写接口缺 CSRF 头 |
| 403 | `session_required` | 只能网页登录做（当前只有 rotate）|
| 404 | `not_found` | 资源不存在 / 不属于你 |
| 404 | `redeem_invalid` | 兑换码无效（不区分原因 · 防枚举）|
| 413 | `body_too_large` | 超 1 MiB |
| 409 | `no_stock` | 暂无库存 |
| 409 | `purchase_cap_reached` | 达持有上限 · **重试无用** |
| 409 | `retry_same_order` | 库存被并发领走 · **用同一幂等键重试** |
| 429 | `rate_limited` | 看 `Retry-After` |
| 502 | `verify_failed` / `quota_failed` | 打 AWS 那跳失败 · 可重试 |
| 500 | `internal` | 服务端问题 |

---

## 5 · 计费 / 定价规则（vendor 原文）

- **单价按整车产出数量查阶梯表** · 产出越多越便宜 · 单价在开号完成那刻冻结
- **只有调购买接口领取才扣积分** · 无零动作扣钱路径
- **共享库存 · 先到先得** · 公共车产出直接进公共库存不为任何人预留
- **单价按每把 key 自己的区域定**（同车美/欧可不同价）· 响应逐把给实付
- 余额不足 → `insufficient_balance` · **不会部分成交**
- 超持有上限 → `purchase_cap_reached` · 整单失败不给一半
- **免费只对「留自用」车成立** · 响应带 `"free": true`
- **母号开成「发库存」时不免费** · 那批进公共库存全站可买 · 我方买它和别人一样扣费

### 5.1 按存活时长降价

```
现价 = max(基准价 − ⌊已存活分钟 / N⌋ × M, F)
```
- 从**发车时刻**起算 · 越老越便宜
- 展示价读现价字段：`/api/my/stock` 的 `zones[].unit_price` 已是现价 · `base_price` 是基准价
- `/api/my/stock/rounds` 每行多给 `current_price`
- **降价只作用于还没卖出去的货** · 已交付的 `paid` 一个字不改

### 5.2 质保

- 默认 10 分钟（`warranty_minutes` 是当前值）
- 质保期内车被判死 → 付的积分**自动全额退** · 不需申请 · 推 `warranty_refund` webhook
- 每把 key 的质保窗口在交付那刻固定 · 事后改配置不影响已交付
- 免费领的（留自用车）没有可退积分 · 不计质保（`warranty_until` 为空）
- **被吊销的 key 不在质保范围内**（`status=revoked`）· 不退积分

### 5.3 售出分成（供应侧 · 我方阶段外）

```
返给你 = 这一把的实付 × (100 − contribute_service_fee_pct) / 100    （向下取整）
```
- **定价不分归属**：平台自有母号和客户交上来的卖同一个价 · 归属只决定分成给谁
- **按每把 key 的实付算** · 不是均价
- 费率改动不追溯 · 每把返多少在交付那刻固定
- 自己买自己交的号**不分成** · 但**包量预留照常分成**（例外）
- 流水类型：入账 `income` · 质保冲回 `clawback`

---

## 6 · 区域与端点（vendor 明确警告）

| zone | region | AWS 端点 |
|---|---|---|
| `us` | `us-east-1` | `https://codewhisperer.us-east-1.amazonaws.com` |
| `eu` | `eu-central-1` | `https://q.eu-central-1.amazonaws.com` |

**⚠️ 两个区主机名形态不一样 · 不能靠拼字符串推**：只有美区有 `codewhisperer.<region>` 这个主机 · 欧区的 `codewhisperer.eu-central-1` **根本不解析** · 同一套 REST API 在 `q.<region>.amazonaws.com` 上。拿欧区 key 打美区端点得 403 · **看起来像"买到废号"实际只是打错地方**。

Key 与区域绑定不能跨区用。一单只来自一个区（下单时 `zone` 决定）。

---

## 7 · 特有事实（跟其他 vendor 的差异）

- **端点最多**（31 个）· 文档最详细（684 行）
- **鉴权最灵活**：`X-API-Key` **或** `Authorization: Bearer` **或** cookie（写请求需 CSRF）
- **`zones[]` 同时给 `zone` + `region`** · 6 家里唯一（其他家要么只给一个要么都不给）
- **`base_price` vs `unit_price`** · 唯一支持"原价 → 现价"展示的
- **`keys[].paid` 逐把实付** · 混价单对账唯一可靠字段
- **7 态车次状态机**（preparing/standby/live/dying/dead/failed/scrapped）
- **有 `warranty_refund` webhook**（kiroceo 没有 · 静默入账）
- **有 `reserved_keys_delivered` webhook** · 包量预留场景 · **处理方式跟补货完全相反**
- **双通道 webhook**（private/public 分开）· 6 家里唯一
- **有 `webhook/deliveries` 投递历史端点** · 排查漏推
- **key 用量三端点** · 含 `subscription` + `reset_days`
- **`status=revoked` 态** · 公开分发 key 会被吊销且不退款
- **2026-08-07 停发 account/password/issuer_url** · 只给 `key`（安全考虑）
- **母号供应侧 8 个端点** · 含 `queue_position` 排队进度
- **错误码有稳定 `code` 字段**（kiroceo 只有中文文案）

---

## 8 · 我方 adapter 缺口（按优先级）

| # | 缺什么 | 影响 | 优先级 |
|---|---|---|---|
| 1 | `GET /api/my/stock/rounds` | 拿不到逐车次现价 + 降价参数 · 只能看该区最便宜一档 | **高** |
| 2 | `keys[].paid` 逐把实付不落库 | 混价单对账缺权威值 | **高** |
| 3 | key 用量三端点 | 拿不到 `subscription` / `reset_days` / 权威 remaining | **中** |
| 4 | `GET /api/my/ledger` | 无 vendor 侧流水做双向对账 | **中** |
| 5 | `reserved_keys_delivered` webhook 是否处理？ | 若签了包量协议 · 漏处理会丢号 | **中**（先查 webhookin）|
| 6 | `hold_cap_effective` / `keys_held` 不落库 | 少一次"能否下单"预判 | 低 |
| 7 | `base_price` 不落库 | 做不了"原价划掉"展示 | 低 |
| 8 | `GET /api/my/webhook/deliveries` | 排查漏推靠猜 | 低 |
| 9 | `PUT /api/my/settings`（持号上限）| 只能手工在后台设 | 低 |
| 10 | 母号供应侧 8 端点 | `CLAUDE.md §3` 明确不做 | ❌ 阶段外 |
