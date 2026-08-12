# Vendor: kiro.ooo

## 0 · 档案元信息

| 项 | 值 |
|---|---|
| Base URL | `https://kiro.ooo/api` |
| 官方文档 | `https://kiro.ooo/index.html#/api`（登录后可见 · Vue SPA 页）· 抓取存档 `.playwright-mcp/vendor-scrape-2026-08-12/kirooo-docs.txt` |
| 抓取日期 | 2026-08-12 |
| 鉴权 | `X-API-Key: usr-…` **或** `Authorization: Bearer usr-…` |
| 密钥前缀 | `usr-`（跟 kiro91 / kiroceo 同前缀 · 容易混）|
| **官方端点总数** | **32 个 · 分 7 组 · 6 家里最多** |
| **我方 adapter 接了** | **7 个 · 覆盖率 22%** |
| Provider group | `providers.ProviderKiro` |
| Adapter 目录 | `internal/providers/kiro/vendors/kirooo/` |
| 计价 | **积分（1 积分 = 1 元人民币）** · 6 家里唯一显式挂钩 CNY 的 |
| 分区 | `us-east-1` / `eu-central-1`（**用完整 AWS region 名当 zone** · 不是 us/eu 短名）|
| 单价 | us-east-1 = 100 · eu-central-1 = 70（2026-08-12 实测）|
| 充值 | **走 USDT**（链上）· 我方不用 |

---

## 1 · 端点清单（vendor 官方 32 个 · 7 组）

### 1.1 账号 + 库存 + 领取（10 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 1 | GET | `/my/profile` | 账号 · 额度 · 速率 · 可领数量 | `Adapter.Balance()` | ✅ |
| 2 | GET | `/my/stock` | 可领上限 / 可取库存 / 剩余配额（**单区聚合**）| — | ❌ 未接（用 #3 代替）|
| 3 | GET | `/my/stock/regions` | **双区货架** `[{region,label,open,unit_price,claimable,can_buy}]` | `Adapter.Stock()` | ✅ |
| 4 | POST | `/my/keys/claim` | 自助领取 `{count, client_order_id, region}` | `Adapter.Purchase()` | ✅ |
| 5 | GET | `/my/keys` | 我的 key 列表 · `?history=1` 含已失效 | `Adapter.KeyStats()` | ✅ |
| 6 | GET | `/my/keys/created-at` | 最早产出时间 + 累计个数 | — | ❌ **未接** |
| 7 | GET | `/my/keys/export` | 按母号下载 key · `?master_id&history=1&format=json` | — | ❌ **未接** |
| 8 | GET | `/my/dispatch-log` | **按车次聚合的活死统计** | — | ❌ **未接 · 高价值** |
| 9 | GET | `/my/purchase-orders` | 最近 50 笔订单 | `Adapter.PurchaseHistory()` | ✅ |
| 10 | GET | `/my/orders/{id}` | 某趟车的全部子号 | `Adapter.OrderKeys()`（走 `/my/purchase-orders/{id}/keys`）| ✅ |

### 1.2 发车相关（6 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 11 | GET | `/my/dispatch-config` | 发车自留配置 `{keep_keys, auto_sell}` | ❌ 阶段外 |
| 12 | PUT | `/my/dispatch-config` | 改自留配置 | ❌ 阶段外 |
| 13 | PUT | `/my/reserve` | 设发车预留 `{reserve_count}` · 0=本期不预留 | ❌ 阶段外 |
| 14 | GET | `/my/auto-fleet` | 自动车状态 `{enabled, unit_price, credits, next_count, afford_count, est_cost}` | ❌ 阶段外 |
| 15 | PUT | `/my/auto-fleet` | 开/关自动车 `{enabled}` | ❌ 阶段外 |
| 16 | GET | `/my/fleet-roster` | 发车名单（仅发车主可见）· `[{name,user_no,credits,reserve_count,auto_fleet,eligible}]` | ❌ 阶段外 |

**为什么不接**：供应侧 / 发车（我方交号给 vendor 开号）· `CLAUDE.md §3` 明确不做。

### 1.3 Webhook（2 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 17 | PUT | `/my/webhook` | 设通知地址 `{webhook_url}` | ❌ 未接（手工在后台配）|
| 18 | POST | `/my/webhook/test` | 发一条测试 | ❌ 未接 |

### 1.4 Telegram 通知（4 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 19 | GET | `/my/notify/prefs` | 推送订阅开关 `{on_key_new, on_key_dead, on_key_suspect, on_dispatch}` | ❌ 不需要 |
| 20 | PUT | `/my/notify/prefs` | 改订阅开关 | ❌ 不需要 |
| 21 | POST | `/my/notify/test` | 给 TG 发测试消息 | ❌ 不需要 |
| 22 | POST | `/my/notify/unbind` | 解绑 TG | ❌ 不需要 |

**为什么不接**：TG 是 vendor 给**人**看的通知渠道 · 我方走 webhook 程序化处理。

### 1.5 充值（4 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 23 | GET | `/my/recharge/options` | 充值档位 / 最低最高 / 可选链 | ❌ 手工充值 |
| 24 | POST | `/my/recharge/order` | 下充值单 `{credits, network}` | ❌ 手工充值 |
| 25 | GET | `/my/recharge/orders` | 我的充值单 `?limit=50` | ❌ 手工充值 |
| 26 | GET | `/my/recharge/order/{id}` | 单笔状态 | ❌ 手工充值 |

**为什么不接**：**走 USDT 链上充值** · 我方运维手工充 · 不做程序化。

### 1.6 积分 + 定价（2 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 27 | GET | `/my/credits` | 积分余额 + 母号单价 + **积分流水** `?limit=50` | — | ❌ **未接** |
| 28 | GET | `/my/key-price-tiers` | **key 单价阶梯** · `base` 基准价 + `bands` 分档 | — | ❌ **未接 · 高价值** |

### 1.7 账号安全（3 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 29 | POST | `/my/password` | 改自助台登录密码 `{old_password, new_password}` | ❌ 不需要 |
| 30 | POST | `/my/bind-account` | 补一套用户名密码 `{username, password}` | ❌ 不需要 |
| 31 | POST | `/my/2fa-verify` | 敏感操作前二次验证 `{code}` | ❌ 不需要 |

### 1.8 公开（1 个）

| # | 方法 | 路径 | vendor 描述 | 我方 adapter | 状态 |
|---|---|---|---|---|---|
| 32 | GET | `/status` | 系统库存 / 存活 / 是否正在开号（**免鉴权**）| `Adapter.PublicStatus()` | ✅ |

---

## 2 · 逐端点字段清单（vendor 原文命名）

### 2.1 `GET /my/profile`

**实测响应**（我方账号 · 2026-08-12）：
```json
{
  "auto_fleet": false, "claimable": 0, "is_fleet_owner": false, "is_super": false,
  "min_reserve": 0, "name": "…", "needs_2fa": false,
  "quota": 0, "remaining": 0, "used_quota": 0,
  "reserve_count": 0,
  "risk_at": "…", "risk_flag": 0, "risk_rate": 0, "risk_threshold": 0,
  "role": "user", "twofa_ok": true,
  "user_no": "…", "username": "…", "webhook_url": ""
}
```

| 字段 | 类型 | 语义 |
|---|---|---|
| `name` / `username` | str | 显示名 / 登录名 |
| `user_no` | str | vendor 侧账号编号 |
| `role` | str | `user` / 其他 |
| `quota` | int | 总积分 |
| `used_quota` | int | 已用积分 |
| `remaining` | int | 剩余积分 |
| `claimable` | int | **当前可领数量** |
| `webhook_url` | str | 已配 webhook 地址 |
| **`risk_flag`** | int | ★ **风控标记** · 独家 |
| **`risk_rate`** | int | ★ 风险率 |
| **`risk_threshold`** | int | ★ 风险阈值 |
| **`risk_at`** | str | ★ 风控判定时刻 |
| **`is_fleet_owner`** | bool | ★ 是否发车主 |
| **`auto_fleet`** | bool | ★ 自动车是否开 |
| **`reserve_count`** | int | ★ 发车预留数 |
| **`min_reserve`** | int | ★ 最低预留 |
| **`is_super`** | bool | ★ 超级用户 |
| **`needs_2fa`** / **`twofa_ok`** | bool | ★ 2FA 状态 |

**我方 adapter 映射**：`Balance` ← `remaining × 10^6`。

**⚠️ 数据缺口**：**风控四字段全部不落库**（`risk_flag` / `risk_rate` / `risk_threshold` / `risk_at`）· `claimable` 也不落库。风控字段是**独家信号** —— vendor 觉得我方账号有风险时这里会变 · 我方现在完全不知情。

---

### 2.2 `GET /my/stock`（❌ 未接 · 单区聚合）

**实测响应**：
```json
{ "afford": 0, "can_buy": false, "claimable": 0, "credits": 70,
  "max": 0, "remaining": 0, "short_credits": 0, "stock": 0, "unit_price": 100 }
```

| 字段 | 语义 |
|---|---|
| `stock` | 可取库存 |
| `unit_price` | 单价（聚合 · 某一区）|
| `credits` | 我方积分余额 |
| `claimable` | 可领数量 |
| `can_buy` | 能否买（一个布尔顶掉多个条件）|
| `afford` | 按余额能买几个 |
| `short_credits` | 差多少积分 |
| `max` / `remaining` | 上限 / 剩余配额 |

**我方不接**（用 `/my/stock/regions` 代替 · 那个逐区更精确）。

---

### 2.3 `GET /my/stock/regions`（✅ 我方主用）

**实测响应**：
```json
{
  "credits": 70, "fleet_active": true,
  "fleet_now": "2026-08-12 22:17:47", "fleet_started_at": "2026-08-12 22:15:05",
  "ok": true, "remaining": 0,
  "regions": [
    { "afford": 0, "can_buy": false, "claimable": 0, "label": "美国区",
      "open": false, "region": "us-east-1", "short_credits": 0, "stock": 0, "unit_price": 100 },
    { "afford": 1, "can_buy": false, "claimable": 0, "label": "欧洲区",
      "open": false, "region": "eu-central-1", "short_credits": 0, "stock": 0, "unit_price": 70 }
  ]
}
```

**顶层字段**：

| 字段 | 类型 | 语义 |
|---|---|---|
| `ok` | bool | 请求成功标记 |
| `credits` | int | 我方积分余额 |
| `remaining` | int | 剩余配额 |
| **`fleet_active`** | bool | ★ **vendor 平台此刻是否正在开号** · 我方探针提频靠它 |
| **`fleet_started_at`** | str | ★ 本轮开号开始时刻（UTC+8）|
| **`fleet_now`** | str | ★ vendor 服务器当前时刻（UTC+8）· 算已开多久 |

**`regions[]` 元素**：

| 字段 | 类型 | 语义 |
|---|---|---|
| `region` | str | **`us-east-1` / `eu-central-1`** · ⚠️ 用完整 AWS region 当 zone 标识（跟 kiro91/kiroceo 的 us/eu 短名不同）|
| `label` | str | 中文名 `"美国区"` / `"欧洲区"` |
| `unit_price` | int | 该区单价（积分/个）|
| `stock` | int | 该区库存 |
| `claimable` | int | 该区可领数量 |
| `open` | bool | 该区是否开放 |
| `can_buy` | bool | 能否买（综合判断）|
| `afford` | int | 按余额该区能买几个 |
| `short_credits` | int | 差多少积分 |

**我方 adapter 映射**（`mapper.go`）：

| 我方字段 | 来源 |
|---|---|
| `StockSnapshot.Available` | Σ`regions[].stock` |
| `ZoneStock[].Zone` | ⚠️ **`providers.Zone(r.Region)` 直接转** → 落成 `"us-east-1"` **不是 `"us"`** · **这是 bug**（见 §8 缺口 1）|
| `ZoneStock[].Region` | `regions[].region` |
| `ZoneStock[].Available` | `regions[].stock` |
| `ZoneStock[].UnitPrice` | `Money{unit_price × 10^6, "credit"}` |

**⚠️ 数据缺口**：`label` / `open` / `can_buy` / `afford` / `short_credits` **都不落库**。`fleet_active` 只用于探针提频 · **不落库**（想事后分析"开号节奏 vs 我方抢号成功率"就没数据）。

---

### 2.4 `POST /my/keys/claim`

**请求**：`{ "count": 5, "client_order_id": "…", "region": "us-east-1" }`

| 字段 | 必填 | 说明 |
|---|---|---|
| `count` | ✓ | **单次上限 500**（6 家里最高）|
| `client_order_id` | ★ | 幂等键 · 同单号重复提交返上次那批 · 不重复扣配额 |
| `region` | ✗ | **必须显式传 `eu-central-1` 才拉 EU** |

**vendor 明说**：实际取 `min(count, 剩余配额, 可取库存)` · 取不到返 4xx 且**不扣配额** · 可安全重试。

**我方 adapter**：`Adapter.Purchase()`。

---

### 2.5 `GET /my/keys` · `?history=1`

**实测响应**（我方账号有数据 · 是 6 家里唯一给完整 key 明细的）：
```json
{
  "active": 0, "count": 0, "suspect": 0,
  "keys": [{
    "id": 123, "key": "…", "master_id": "…", "order_id": "…",
    "region": "us-east-1", "status": "…",
    "created_at": "…", "dispatched_at": "…", "last_probe": "…",
    "dead_reason": "…",
    "current_usage": 0, "usage_limit": 0, "usage_rate": 0,
    "on_sale": false, "listing_price": 0
  }]
}
```

**顶层聚合**：

| 字段 | 语义 |
|---|---|
| `active` | 存活数 |
| `count` | 总数 |
| **`suspect`** | ★ **可疑数**（vendor 判定异常但未确认死）· 独家 |

**`keys[]` 元素**（★ = 6 家里独有）：

| 字段 | 类型 | 语义 |
|---|---|---|
| `id` | int | vendor 侧 key id |
| `key` | str | key 正文 |
| **`master_id`** | str | ★ **母号 id**（拼车池 root）|
| `order_id` | str | 归属订单 |
| `region` | str | `us-east-1` / `eu-central-1` |
| `status` | str | 状态 |
| `created_at` | str | 产出时刻 |
| **`dispatched_at`** | str | ★ 派发时刻（跟 created_at 分开）|
| **`last_probe`** | str | ★ **vendor 最近一次探活时刻** |
| **`dead_reason`** | str | ★ 死因 |
| **`current_usage`** | int | ★ **当前用量** |
| **`usage_limit`** | int | ★ 用量上限 |
| **`usage_rate`** | int | ★ **用量速率** |
| **`on_sale`** | bool | ★ 是否挂在转售市场 |
| **`listing_price`** | int | ★ 挂牌价 |

**我方 adapter 用它**：只拿聚合数（`active` / `count` / `suspect`）· **`keys[]` 明细完全不解析**。

**⚠️ 最大数据缺口**：kirooo 是**唯一给逐把 key 用量 + 探活 + 死因的 vendor** · 我方全丢了。`current_usage` / `usage_limit` / `usage_rate` / `last_probe` / `dead_reason` 都是我方 kiro.rs 探测想要的数据 · vendor 直接给了却没接。

---

### 2.6 `GET /my/keys/created-at`（❌ 未接）

最早产出时间 + 累计个数。轻量端点 · 用途：判断"我方在这家 vendor 用了多久"。

---

### 2.7 `GET /my/keys/export`（❌ 未接）

`?master_id=&history=1&format=json` · **按母号下载 key**。

**为什么有价值**：按 `master_id` 分组能看出"哪个母号出的号活得久" —— 是选 vendor 的质量信号。

---

### 2.8 `GET /my/dispatch-log`（❌ 未接 · 高价值）

**按车次聚合的活死统计**。

**为什么高价值**：这是 vendor 侧的**车次质量数据**（每批开了多少 / 死了多少 / 存活多久）· 我方现在靠自己探针推算 · vendor 直接给了。

---

### 2.9 `GET /my/purchase-orders`

最近 50 笔订单。**实测响应**：
```json
[{ "client_order_id": "…", "created_at": "…", "purchased": 3, "requested": 5, "source": "…" }]
```

| 字段 | 语义 |
|---|---|
| `client_order_id` | 幂等键 |
| `requested` | 申请数量 |
| `purchased` | **实际成交** |
| `created_at` | 下单时刻 |
| **`source`** | ★ 订单来源（api / web / auto 等）|

**⚠️ 数据缺口**：**这个端点不返单价 / 扣款额** —— 对账拿不到金额（要靠 `/my/credits` 流水补）。

---

### 2.10 `GET /my/credits`（❌ 未接）

积分余额 + 母号单价 + **积分流水** `?limit=50`。

**为什么该接**：`/my/purchase-orders` 不给金额 · 只有这里能拿到**实扣流水** · 是 kirooo 对账的唯一路径。

---

### 2.11 `GET /my/key-price-tiers`（❌ 未接 · 高价值）

**key 单价阶梯** · `base` 基准价 + `bands` 分档。

**为什么高价值**：这是 vendor 的**完整定价规则表**（不只当前价 · 是整张阶梯）。有了它我方能：
1. 预测"再买 N 个会掉到哪一档"
2. 算出"什么数量最划算"
3. 做 `docs/18` 里 `vendor_price_tier` 表真正的数据源（现在那表是空的）

---

### 2.12 `GET /status`（免鉴权）

**实测响应**：
```json
{
  "announce": { "enabled": false, "level": "…", "text": "…", "updated_at": "…", "updated_by": "…" },
  "auto_mode": true, "generating": false,
  "keys_active": 1026, "keys_alive": 1026, "keys_dead": 0,
  "keys_stock": 0, "keys_suspect": 0, "keys_total": 1026,
  "started_at": "…", "uptime_secs": 12345
}
```

| 字段 | 语义 |
|---|---|
| `keys_active` / `keys_alive` | fleet 存活 key 数 |
| `keys_dead` | 已死 |
| `keys_stock` | 可买库存 |
| **`keys_suspect`** | ★ 可疑数 |
| `keys_total` | 总数 |
| `generating` | 是否正在开号 |
| **`auto_mode`** | ★ 自动模式是否开 |
| `started_at` / `uptime_secs` | vendor 服务运行时长 |
| **`announce.*`** | ★ **站点公告**（enabled / level / text / updated_at / updated_by）|

**我方 adapter 映射**：`PublicStatus` ← `keys_*` 系列 + `generating` + `started_at` + `uptime_secs`。

**⚠️ 数据缺口**：`announce.*` **不落库** —— vendor 挂公告（例："今晚维护"）我方不知道。`auto_mode` 也不落库。

---

## 3 · Webhook

### 3.1 事件类型（3 种）

| `event` | 语义 |
|---|---|
| `new_keys_available` | 有新 key 就绪 |
| `all_keys_dead` | 本轮全灭 |
| `test` | 手工测试 |

### 3.2 载荷字段（vendor 原文完整表）

| 字段 | 类型 | 语义 |
|---|---|---|
| `event` | str | 事件类型 |
| `event_id` | str | **32 位小写 hex** · 每次投递不同 · 去重用 |
| **`client_order_id`** | str | ★ **原样回传给 claim**（vendor 明说：**不要用 event_id**）|
| `purchase_order_id` | str | `client_order_id` 的**旧名** · 值完全一样 |
| `new_keys` | int | 就绪数 |
| `claim_hint` | str | 给人看的取货提示 |
| `dead` | int | 失效数 |
| `message` | str | 中文摘要 |
| `time` | str | **UTC+8** `YYYY-MM-DD HH:MM:SS` |

**⚠️ 关键**：`client_order_id` 和 `purchase_order_id` 是**同一个值的新旧名** · 我方代码要兼容两个字段名（生产实测 bug 就是这个 —— 见 `decisions §11.x` webhook OrderID 空）。

### 3.3 签名

**无签名** · vendor 明说"靠不可猜 URL 路径当口令"。

### 3.4 重试

3 次 · 间隔 **0s / 2s / 6s** · **4xx 视为拒绝不再重试**。

---

## 4 · 幂等 + 限流

- `claim` 传 `client_order_id` · 同单号重复提交返上次那批 · 不重复扣配额
- 实际取 `min(count, 剩余配额, 可取库存)`
- **单次上限 500**（6 家里最高）
- 取不到 4xx 且**不扣配额** · 可安全重试
- 限流 429 · 指数退避

---

## 5 · 特有事实（跟其他 vendor 的差异）

- **端点最多**（32 个 · 分 7 组）
- **用完整 AWS region 当 zone 标识**（`us-east-1` 不是 `us`）· 6 家里唯一 —— **我方 adapter 归一 bug 的来源**
- **积分显式 = 1 元人民币** · 6 家里唯一挂钩 CNY 的
- **单次领取上限 500** · 6 家里最高
- **`/my/keys` 给完整 key 明细**（用量 / 探活 / 死因 / 转售）· 6 家里唯一
- **有 `suspect` 态**（可疑但未确认死）· kiro91 / kiroceo 都只有 active/dead
- **profile 有风控四字段**（`risk_flag` / `risk_rate` / `risk_threshold` / `risk_at`）· 独家
- **`fleet_active` + `fleet_started_at` + `fleet_now`** · vendor 主动告知"我正在开号" · 我方探针提频靠它
- **有 `/status` 公告字段**（`announce.*`）· 独家
- **充值走 USDT 链上** · 6 家里唯一
- **有 TG 通知渠道**（4 个端点）· 独家
- **有 2FA**（`/my/2fa-verify`）· 独家
- **有转售市场**（`on_sale` / `listing_price`）· 独家
- **webhook 无签名** · 跟 kiroceo 一样靠 URL secret
- **`client_order_id` / `purchase_order_id` 双名同值** · 兼容旧客户端
- **`/my/purchase-orders` 不返金额** · 对账必须配 `/my/credits`

---

## 6 · 我方 adapter 缺口（按优先级）

| # | 缺什么 | 影响 | 优先级 |
|---|---|---|---|
| 1 | **`ZoneStock.Zone` 归一 bug** · 落 `us-east-1` 不是 `us` | 侧表 zone 列跟其他 vendor 对不上 · PricedFor 按 zone 查匹配不到 | **最高 · 是 bug** |
| 2 | `GET /my/key-price-tiers` | 拿不到完整定价阶梯 · `vendor_price_tier` 表因此是空的 | **高** |
| 3 | `keys[]` 明细全丢（用量 / 探活 / 死因 / master_id）| 唯一给这些数据的 vendor · 全没接 · kiro.rs 探测在重复劳动 | **高** |
| 4 | `GET /my/credits` | kirooo 对账唯一金额来源 · 现在没有 | **高** |
| 5 | `GET /my/dispatch-log` | vendor 侧车次质量数据 · 我方在自己推算 | **中** |
| 6 | profile 风控四字段不落库 | vendor 觉得我方有风险时完全不知情 | **中** |
| 7 | `fleet_active` 不落库 | 只用于提频 · 事后分析"开号节奏 vs 抢号成功率"没数据 | **中** |
| 8 | `/status` 的 `announce.*` 不落库 | vendor 挂维护公告我方不知道 | 低 |
| 9 | `GET /my/keys/export` | 按母号看号质量 | 低 |
| 10 | `GET /my/keys/created-at` | 轻量 · 用途有限 | 低 |
| 11 | 发车 6 端点 · TG 4 端点 · 充值 4 端点 · 账号安全 3 端点 | 阶段外 / 不需要 | ❌ |
