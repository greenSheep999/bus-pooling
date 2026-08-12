# 聚合源: xi8（`xi8.cc`）· 内部数据源 · **绝不下发前端**

> **⚠️ 定位跟 6 家 vendor 完全不同**：xi8 **不是我方拉号的上游** · 是**第三方聚合站** ·
> 我方只用它做**观测补漏 + 交叉核对**（价格 / 上货信号 / 出货历史）。
>
> **对外一律不出现**（`CLAUDE.md §0.1`）：xi8 字段 / 昵称 / vendor_id 映射 / 评级文案
> 都是**内部术语** · 前端 / webhook / 帮助中心一个字都不许漏。

## 0 · 档案元信息

| 项 | 值 |
|---|---|
| Base URL | `https://xi8.cc/api` |
| 官方文档 | `GET /api/docs`（Markdown · 需 key · 454 行）|
| 抓取日期 | 2026-08-13 |
| 鉴权 | `X-API-Key: <key>` **或** `Authorization: Bearer <key>`（两者等价）|
| 我方 key 变量 | `BP_XI8_API_KEY`（`.dev.env` + vps22 `.env`）|
| **官方端点总数** | **25 个** |
| **我方接了** | **3 个 · 覆盖率 12%** |
| 我方实现 | `internal/xi8/`（`client.go` / `backfiller.go` / `types.go`）|
| 计价 | **CNY**（`price_fen` 分为单位 + `price` 字符串）· 6 家 vendor 里没有一家这么给 |
| 对接的 vendor | **5 家**（`kiroappcc` 不在 xi8）|

---

## 1 · 端点清单（官方 25 个）

### 1.1 公开 / 观测（6 个）

| # | 方法 | 路径 | vendor 描述 | 我方接了 | 状态 |
|---|---|---|---|---|---|
| 1 | GET | `/status` | 系统总库存 · 有货商家数 · **不需要 key** | — | ❌ **未接** |
| 2 | GET | `/stock` | **查库存**：一行一个商家 · us/eu 并排 · `?buyable=1` 只看能买的 | — | ❌ **未接 · 最全的库存端点** |
| 3 | GET | `/stock?shape=flat` | 换成按价排序的扁平列表（一行一个「商家 × 区域」）· 挑最便宜取 `rows[0]` | — | ❌ **未接** |
| 4 | GET | `/vendors` | 商家详情：库存 / 价格 / 最近出库 / **质量评级** · `?in_stock=1&region=us` | `Backfiller.pushVendorsToZone()` | ✅ **已接** |
| 5 | GET | `/signals` | 商家上货信号原文（含数量和批次号）· `?vendor_id=&limit=` | `Backfiller.RunOnce()` | ✅ 已接 |
| 6 | GET | `/restock-log` | 出库历史 · 看上货节奏 · `?vendor_id=&limit=` | `Backfiller.RunOnce()` | ✅ 已接 |

### 1.2 账号 / 积分（4 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 7 | GET | `/my/profile` | 账号 · 余额 · **各状态订单数** · 限流额度 | ❌ **未接** |
| 8 | GET | `/my/balance` | 只回余额 · 适合高频心跳 | ❌ 未接 |
| 9 | GET | `/my/credits` | 余额 + **流水** · `?limit=&type=order_pay` | ❌ **未接** |
| 10 | POST | `/my/recharge` | 卡密充值 `{code}` | ❌ 手工充值 |

### 1.3 下单 / 取号（6 个 · **阶段外 · 我方不通过 xi8 买**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 11 | GET | `/my/quote` | **下单前实时报价** · `?vendor_id=&region=` | ❌ 阶段外 |
| 12 | POST | `/my/orders` | **下单** · `{vendor_id, region, quantity, client_order_id, max_price_fen}` | ❌ 阶段外 |
| 13 | GET | `/my/orders` | 我的订单 · `?limit=&status=delivered` | ❌ 阶段外 |
| 14 | GET | `/my/orders/{order_no}` | 单笔详情（key 为掩码）| ❌ 阶段外 |
| 15 | GET | `/my/orders/{order_no}/keys` | 取该单**完整 key** | ❌ 阶段外 |
| 16 | POST | `/my/orders/{order_no}/aftersale` | 提交售后 `{reason}` | ❌ 阶段外 |

**为什么阶段外**：我方直接对接 6 家 vendor · **不经 xi8 中转采购**（多一层加价 + 多一层故障点）。

### 1.4 我的 key（1 个）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 17 | GET | `/my/keys` | 我买过的所有 key · `?vendor_id=&since=&limit=&format=text` | ❌ 阶段外（我方没在 xi8 买过）|

### 1.5 通知（5 个）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 18 | GET | `/my/notify/prefs` | 订阅与 webhook 地址 · 含 `push_paused` | ❌ **未接** |
| 19 | PUT | `/my/notify/prefs` | 改订阅 · **只改你传的字段** | ❌ 未接（手工配）|
| 20 | POST | `/my/notify/test` | 往配的地址发测试推送 | ❌ 未接 |
| 21 | GET | `/my/notifications` | **站内通知列表** · `?limit=` | ❌ **未接 · 唯一有历史价格的端点** |
| 22 | POST | `/my/notifications/read` | 全部标记已读 | ❌ 不需要 |

### 1.6 API Key 管理（2 个 · **我方完全没接**）

| # | 方法 | 路径 | vendor 描述 | 状态 |
|---|---|---|---|---|
| 23 | GET | `/my/keys/list` | 我有哪些 API Key（只回前缀）| ❌ 不需要 |
| 24 | DELETE | `/my/keys/{id}` | 吊销某把 API Key | ❌ 不需要 |

**签发新 key 只能在网页做** · API 没这个端点（vendor 明说理由：一个凭据能凭自己造下一个 · 吊销就永远不彻底）。

### 1.7 文档（1 个）

| # | 方法 | 路径 | 说明 |
|---|---|---|---|
| 25 | GET | `/docs` | 这份文档本身（Markdown）|

---

## 2 · 全局约定（vendor 原文）

### 2.1 响应格式

成功：`{"ok": true, …}` · 失败：`{"ok": false, "msg": "中文说明", "error": "稳定标识"}`

### 2.2 **金额一律给两个字段**

```json
{ "price_fen": 4545, "price": "45.45" }
```
- `price_fen` · **分**（整数 · 计算用）
- `price` · 元（字符串 · 展示用）

### 2.3 **时间一律给三个字段**

```json
{ "at": "2026-08-09 14:23:01", "iso": "2026-08-09T14:23:01+08:00", "ago_secs": 412 }
```
- `at` · 本地格式（UTC+8）
- `iso` · **带时区 · 我方解析用这个**
- `ago_secs` · 相对现在秒数（对账用不上）

---

## 3 · 逐端点字段清单

### 3.1 `GET /status`（❌ 未接 · 免鉴权）

**实测响应**（2026-08-13）：
```json
{
  "ok": true,
  "vendors_total": 5,
  "vendors_in_stock": 0,
  "stock_total": 0,
  "restocking": false,
  "last_restock": { "at": "…", "iso": "…", "ago_secs": 5924 },
  "server_time": "2026-08-13T00:15:20+08:00"
}
```

| 字段 | 语义 |
|---|---|
| `vendors_total` | xi8 对接的商家总数（**5** · 不含 kiroappcc）|
| `vendors_in_stock` | 当前有货的商家数 |
| `stock_total` | 全站总库存 |
| **`restocking`** | ★ **是否正在上货** |
| `last_restock` | 最近一次出货时刻（三段式）|
| `server_time` | xi8 服务器时刻 |

**为什么该接**：一个请求拿到**全网库存概览** · 免鉴权 · 比逐家探针便宜。可作为"是否值得提频"的粗信号。

---

### 3.2 `GET /stock`（❌ 未接 · **最全的库存端点**）

**响应结构**（vendor 文档示例）：
```json
{
  "ok": true, "shape": "vendor", "count": 2,
  "rows": [{
    "vendor_id": 4, "name": "商家A",
    "total_stock": 12, "max_per_order": 5,
    "us": {
      "region": "us", "region_label": "美区", "label": "美区",
      "stock": 10, "price_fen": 4545, "price": "45.45",
      "buyable": true, "floating": true, "blocked": false, "block_reason": "",
      "last_restock": {"at":"…","iso":"…","ago_secs":412},
      "restock_source": "webhook",
      "stock_synced": {"at":"…","iso":"…","ago_secs":3}
    },
    "eu": { "stock": 2, "price_fen": 3030, "price": "30.30", "buyable": true },
    "last_restock": {"…": "各区里最晚的那次"},
    "restock_source": "推算",
    "warranty_minutes": 60,
    "stock_synced": {"…": ""}
  }],
  "stock_ttl_secs": 30,
  "server_time": "2026-08-09T14:29:53+08:00"
}
```

**顶层**：

| 字段 | 语义 |
|---|---|
| `shape` | `"vendor"`（一行一商家）/ `"flat"`（一行一商家×区）|
| `count` | 行数 |
| **`stock_ttl_secs`** | ★ **库存缓存周期**（30s）· 说明数据新鲜度 |
| `server_time` | xi8 时刻 |

**`rows[]` 顶层**：

| 字段 | 语义 |
|---|---|
| `vendor_id` | ★ **下单要用它** · 也是我方映射键 |
| `name` | xi8 给的商家昵称（**内部术语 · 绝不下发**）|
| `total_stock` | 该商家总库存 |
| `max_per_order` | 单次上限 |
| `warranty_minutes` | **质保时长** |
| `last_restock` | 各区里最晚那次出货 |
| `restock_source` | 见下方三态表 |
| `stock_synced` | 这份库存数字什么时候从商家同步的 |

**`rows[].us` / `rows[].eu`**（★ vendor 明说：商家没上架某区时**该键是 `null`** · 不是缺字段 · 可以直接 `if (row.us)`）：

| 字段 | 类型 | 语义 |
|---|---|---|
| `region` | str | `us` / `eu` |
| `region_label` / `label` | str | `美区` / `欧区` |
| `stock` | int | 该区库存 |
| `price_fen` | int | ★ **该区单价（分）** |
| `price` | str | 该区单价（元 · 字符串）|
| **`buyable`** | bool | ★ **一个布尔顶掉三个条件** · vendor 明说 **"不要自己写 `stock > 0 && price_fen > 0`"** —— 那样会漏掉 `blocked` |
| **`blocked`** | bool | ★ **暂时不卖**（xi8 发现成本数据可疑时主动停售）· 为真时 `price_fen` 是 0 · **这不是"没货"** |
| **`block_reason`** | str | ★ 停售原因 |
| **`floating`** | bool | ★ **价格随商家成本浮动** · 缓存的价随时会变 · 下单务必带 `max_price_fen` |
| `last_restock` | obj | 该区最近出货（三段式）|
| **`restock_source`** | str | ★ 见下表 |
| `stock_synced` | obj | 该区库存同步时刻 |

**`restock_source` 三态**（★ 关键 —— 决定时间戳有多准）：

| 值 | 含义 | 准确度 |
|---|---|---|
| `webhook` | 商家推送「有新号」那一刻 | **最准** · 就是真正出货时刻 |
| `商家` | 商家在库存接口里自报的批次时间 | 准 · 但只有部分商家提供 |
| `推算` | xi8 轮询观测到「库存由 0 变正」 | **会偏晚几十秒到几分钟**（vendor 实测最多差 **492 秒**）|

**为什么该接**：
1. **比 `/vendors` 更全** —— 多了 `buyable` / `blocked` / `block_reason` / `floating` / `stock_ttl_secs` / `restock_source` 逐区版
2. **`buyable` 是权威可购判断** —— 我方现在自己算 `stock > 0` · 会漏 `blocked` 态
3. **`floating`** 标记浮动定价商品 —— 我方现在不知道哪家价会随时变

**⚠️ vendor 明说**：库存是缓存值（`stock_ttl_secs` 秒刷一次）· 商家那边随时有人在买 · **"查到有货但下单说没货是正常的 · 不是 bug"**。

---

### 3.3 `GET /stock?shape=flat`（❌ 未接）

```json
{ "shape": "flat", "count": 1,
  "rows": [{ "vendor_id": 4, "name": "商家A", "region": "us", "price_fen": 4545, "stock": 1, "buyable": true }] }
```

**按「能买优先 · 价低在前」排好** · 挑最便宜的直接取 `rows[0]`。

**为什么该接**：我方 `AutoPick` 自己做跨 vendor 比价（`vendorview.go:AutoPick`）· xi8 这个端点**已经排好序了** · 可作交叉核对（"xi8 认为最便宜的是不是我方也认为"）。

---

### 3.4 `GET /vendors`（✅ 我方已接）

**实测响应**（2026-08-13 · 5 家）：
```json
{
  "ok": true, "count": 5,
  "vendors": [{
    "vendor_id": 1, "name": "…",
    "total_stock": 0, "max_per_order": 10, "warranty_minutes": null,
    "last_restock": {…}, "restock_source": "webhook", "stock_synced": {…},
    "quality": { "survival": "40 分钟-2 小时", "risk": "低", "grade": "推荐", "verdict": "…", "note": "…" },
    "regions": [
      { "region": "eu", "region_label": "欧区", "stock": 0, "price_fen": 7070, "price": "70.70",
        "buyable": false, "floating": false, "blocked": false, "block_reason": "",
        "last_restock": {…}, "restock_source": "…", "stock_synced": {…} },
      { "region": "us", "…": "同上 · price_fen 10100" }
    ]
  }]
}
```

**`vendors[]` 顶层**：

| 字段 | 语义 |
|---|---|
| `vendor_id` | ★ 映射键 |
| `name` | xi8 昵称（**内部** · 不下发）|
| `total_stock` | 总库存 |
| `max_per_order` | 单次上限 |
| `warranty_minutes` | 质保时长（**可为 null**）|
| `last_restock` / `restock_source` / `stock_synced` | 同 §3.2 |

**`quality`**（★ **xi8 的人工 + 算法评级** · 6 家 vendor 自己都不给这种数据）：

| 字段 | 类型 | 语义 · 示例 |
|---|---|---|
| **`survival`** | str | ★ **号存活时长区间** · `"40 分钟-2 小时"` |
| **`risk`** | str | ★ 风险等级 · `"低"` / `"中"` / `"高"` |
| **`grade`** | str | ★ 评级 · `"推荐"` / `"观察"` / `"警告"` |
| **`verdict`** | str | ★ 一句话总结 |
| **`note`** | str | ★ 补充说明 |

**`regions[]`**：跟 §3.2 的 `us`/`eu` 对象字段完全一致 · 但**是数组**（`/stock` 是 `us`/`eu` 两个键）。

**我方 adapter 用它**（`backfiller.go:pushVendorsToZone`）：

| 我方字段 | 来源 |
|---|---|
| `vendor_probe_zone.vendor_id` | `VendorSlugForXi8ID(vendor_id)` 映射 |
| `vendor_probe_zone.zone` | `providers.ZoneOf(regions[].region)` |
| `vendor_probe_zone.region` | `xi8RegionToOurs(regions[].region)` |
| `vendor_probe_zone.available` | `regions[].stock` |
| `vendor_probe_zone.vendor_currency` | 硬编码 `"CNY"` |
| `vendor_probe_zone.vendor_unit_raw` | `regions[].price_fen × 10_000`（分 → microunit）|
| `vendor_probe_zone.our_unit_credits` | 同上（CNY 1:1 到积分）|
| `vendor_probe_zone.source` | `"xi8"` |

**⚠️ 数据缺口**：**`quality` 五字段全部不落库** —— 这是 xi8 独家的号质量评级（`survival` / `risk` / `grade` / `verdict` / `note`）· 6 家 vendor 自己都不给。我方 `/api/vendors/status` 页面的"质量"维度本来可以直接用它。

**⚠️ 也不落库**：`buyable` / `blocked` / `block_reason` / `floating` / `warranty_minutes` / `restock_source` / `stock_synced`。

---

### 3.5 `GET /signals`（✅ 我方已接）

**实测响应**（2026-08-13）：
```json
{
  "ok": true, "count": 1,
  "signals": [{
    "vendor_id": 1, "name": "…", "event": "restock",
    "vendor_order_id": "e2e6546a0af47444da0c5363160",
    "regions": ["us"], "region_labels": ["美区"],
    "count": 7,
    "at": { "at": "2026-08-09 21:34:29", "iso": "2026-08-09T21:34:29+08:00", "ago_secs": 268798 }
  }]
}
```

| 字段 | 语义 |
|---|---|
| `vendor_id` | 映射键 |
| `event` | `restock` 等 |
| **`vendor_order_id`** | ★ **商家侧批次号** · 我方 `dispatch_key` 用它派生（`"xi8-sig-" + vendor_order_id`）|
| `regions` | **数组**（一批可能跨多区）|
| `region_labels` | 中文名数组 |
| `count` | 该批数量 |
| `at` | 出货时刻（三段式）|

**⚠️ 无价格字段** —— signals 只有数量和批次号。

**vendor 明说**：`/signals` **只回验签通过的** —— 没接 webhook 的商家不会出现在这里 · 他们的上货时间只能靠 `/restock-log`（轮询推算 · 会偏晚）。

**我方 adapter 映射**（`backfiller.go:RunOnce`）：
- `vendor_dispatch.dispatch_key` ← `"xi8-sig-" + vendor_order_id`
- `vendor_dispatch.region` ← `xi8RegionToOurs(regions[0])` · **⚠️ 只取第一个** · 跨区批次会丢
- `vendor_dispatch.count` / `alive` ← `count`
- `vendor_dispatch.source` ← `"xi8"`

---

### 3.6 `GET /restock-log`（✅ 我方已接）

**实测响应**（2026-08-13）：
```json
{
  "ok": true, "count": 200,
  "rows": [{
    "vendor_id": 1, "name": "…",
    "region": "eu", "region_label": "欧区",
    "stock": 10, "prev_stock": 0, "added": 10,
    "at": { "at": "2026-08-12 22:36:36", "iso": "…", "ago_secs": 5835 }
  }]
}
```

| 字段 | 语义 |
|---|---|
| `vendor_id` | 映射键 |
| `region` | **单个**（不是数组 · 跟 signals 不同）|
| **`stock`** | ★ 变化后库存 |
| **`prev_stock`** | ★ **变化前库存** |
| **`added`** | ★ **增量**（= stock − prev_stock）|
| `at` | 时刻（三段式）|

**⚠️ 无价格字段**。

**我方 adapter 映射**：
- `vendor_dispatch.dispatch_key` ← `"xi8-log-{region}-{timestamp}"`
- `vendor_dispatch.count` / `alive` ← `added`

**⚠️ 数据缺口**：`stock` / `prev_stock` **不落库** —— 只落了 `added`。想分析"上货后多快被抢空"就缺 `prev_stock` 序列。

---

### 3.7 `GET /my/notifications`（❌ 未接 · ★ **唯一有历史价格的端点**）

**实测响应**（2026-08-13）：
```json
{
  "ok": true, "unread": 0, "count": 100,
  "items": [{
    "id": 914, "kind": "restock",
    "title": "… 欧区 上新 10 个，¥70.70",
    "vendor": "…", "region": "eu", "region_label": "欧区",
    "stock": 10,
    "price_fen": 7070, "price": "70.70",
    "old_price_fen": null,
    "at": { "at": "2026-08-12 22:36:36", "iso": "…", "ago_secs": 5940 }
  }]
}
```

| 字段 | 类型 | 语义 |
|---|---|---|
| `id` | int | 通知 id · **递增** · `since_id` 用它 |
| **`kind`** | str | ★ `restock` / `price_drop` / `sold_out` |
| `title` | str | 中文标题（含价格）|
| `vendor` | str | xi8 昵称（**内部**）|
| `region` | str | `us` / `eu` |
| `region_label` | str | 中文名 |
| `stock` | int | 该批数量 |
| **`price_fen`** | int | ★★ **该批价格（分）** —— **这就是历史价格** |
| `price` | str | 元（字符串）|
| **`old_price_fen`** | int/null | ★★ **变化前价格** · `price_drop` 事件才有值 |
| `at` | obj | 时刻（三段式）|

**⚠️ 顶层还有 `unread`**（未读数）· 我方不需要。

**为什么这是关键端点**：**6 家 vendor 都不给历史价格** · xi8 的 `/signals` / `/restock-log` 也没有 · **只有这里有** —— 每条上货通知都带那一刻的 `price_fen`。这是**唯一能补我方探针上线前（2026-08-10 15:49）历史价格的数据源**。

**⚠️ 数据深度限制**（实测 2026-08-13）：

| 参数 | 结果 |
|---|---|
| `?limit=100` | 100 条 · 最早 `2026-08-10T21:54:59+08:00` |
| `?limit=500` / `1000` / `5000` | **仍返 100**（硬顶）|
| `?page=N&per_page=30` | ⚠️ **返 30 条但永远是首页**（`page` 不生效）|
| `?since_id=1&limit=1000` | **100 条 · id 718..914** —— `since_id` 生效但 limit 仍顶 100 |
| `?before_id=` / `?offset=` / `?cursor=` / `?until_id=` / `?max_id=` | **全不生效** · 返首页 |

**结论**：API 层**最多拿 100 条**（约 2 天）· `id < 718` 拿不到。

**⚠️ xi8 网页版** `xi8.cc/notify?page=33` **能翻到 8/9** · 走的是**带 cookie 的内部端点** · API Key 拉不到。要更早的历史需浏览器抓包。

---

### 3.8 `GET /my/profile`（❌ 未接）

vendor 描述：账号 · 余额 · **各状态订单数** · 限流额度。

**为什么可能有用**：`各状态订单数` + `限流额度` 能看我方在 xi8 的限流余量（三桶 · 见 §5）。但我方不在 xi8 下单 · 用途有限。

---

### 3.9 `GET /my/credits`（❌ 未接）

余额 + 流水 · `?limit=&type=order_pay`。**我方不在 xi8 消费 · 用途低**。

---

### 3.10 `GET /my/quote`（❌ 阶段外）

**下单前实时报价** · `?vendor_id=&region=`。

**⚠️ 潜在价值**：这是 xi8 侧的**权威实时价**（不是缓存）· 可用来交叉核对我方探针价。但我方不通过 xi8 买 · 优先级低。

---

## 4 · Webhook（xi8 推给我方 · **我方未配**）

### 4.1 配置

```
PUT https://xi8.cc/api/my/notify/prefs
{"webhook_url": "https://你的服务/hook", "on_restock": true}
```
**PUT 只改你传的字段** · 没传的保持原值。

验证：`POST /api/my/notify/test`

### 4.2 载荷（vendor 原文示例）

```json
{
  "event": "restock",
  "vendor_id": 4, "vendor": "商家A",
  "region": "us", "region_label": "美区",
  "count": 10,
  "title": "商家A 美区 有新号可买（10 个）",
  "vendor_order_id": "849ea953c9ada16a216df95563db72f3",
  "price_fen": 4545, "price": "45.45",
  "price_stale": true,
  "stock_synced_at": "2026-08-09T14:23:40+08:00",
  "notif_id": 812,
  "time": "2026-08-09T14:23:01+08:00",
  "hint": "立刻下单: POST /api/my/orders {…}"
}
```

| 字段 | 语义 |
|---|---|
| `event` | ★ `restock`（上新）/ `price_drop`（降价）/ `sold_out`（售完）· **只有 restock 值得抢** |
| `vendor_id` | ★ vendor 明说"这份包里最要紧的字段" |
| `count` | ★ 商家声称的新增数 · **不等于一定能买到这么多**（vendor 实测有商家报 14 而 xi8 只能取 5）|
| `region` | ★ **可能是空串** —— 有的商家推送不说区域 |
| **`price_fen`** | ★ **该批价格** —— webhook 也带价 |
| **`price_stale`** | ★ `true` = 这个价来自刷新前的缓存 · 可能不是这批的价。xi8 为抢时间在**刷库存之前**就推 |
| `stock_synced_at` | 库存同步时刻 |
| `notif_id` | 对应 `/my/notifications` 的 `id` |
| `time` | 出货时刻 |
| `hint` | 给人看的下单提示 |
| **`test`** | ★ **只出现在测试推送里** · 真实事件没这个字段 · 测试包的 `vendor_id` 是 **0** |
| **`retry`** | ★ 重试来的包带 `retry: true` 且 `price_stale: false` |

### 4.3 重复推送

**vendor 明说**：实测有商家一次上货推两条 · 间隔 9~10 秒（最长见过 37 秒）· **接收端会被调两次**。

推荐幂等键：`client_order_id = "hook-" + vendor_order_id + "-" + region`

### 4.4 投递要求

- **单地址超时 3 秒** —— vendor 明说"抢货是拼秒的 · 一个慢地址不该拖住其他用户"
- **先回 200 再异步下单** —— 同步下单会超时 · xi8 当投递失败
- 投递失败重试最多 **3 次**
- **连续失败 5 次自动停推** · `GET /my/notify/prefs` 的 `push_paused` 变 `true` · 重新 PUT 恢复

**⚠️ 我方未配 xi8 webhook** —— 现在靠 30s 轮询 signals。配了能省 30s 延迟（抢货场景关键）。

---

## 5 · 限流（三桶 · 互不占用）

| 桶 | 覆盖 | 上限（次/分钟）|
|---|---|---|
| **read** | 所有 GET | **120** |
| **write** | 充值 / 改通知设置 / 标记已读 / 吊销 key | **30** |
| **order** | **下单 · 取号** | **10** |

**我方现状**：只用 read 桶（30s 拉 signals + 5min 拉 vendors）· 远低于 120/min。

---

## 6 · 错误码

| HTTP | `error` | 含义 | 能重试 |
|---|---|---|---|
| 401 | `missing_key` / `invalid_key` | key 没带或无效 | ❌ |
| 403 | `readonly_key` | 只读 key · 不能下单或取号 | ❌ 换一把 |
| 429 | `rate_limited` | 超限流 · 带 `Retry-After`（秒）| ✅ 退避后 |
| 400 | `missing_param` / `bad_region` / `bad_param` | 参数问题 | ❌ |
| 409 | `price_exceeded` | 超 `max_price_fen` · **未下单** | ✅ 重新报价 |
| 409 | `not_delivered` | 订单还没发货 · 暂无 key | ✅ 稍后 |
| 400 | `order_failed` | 下单失败 · 钱没扣或已退 | ✅ |
| **202** | `pending` | **采购中或待人工核对** | ❌ **不要重试** · 改用 GET 查 |
| 400 | `duplicate_in_flight` | 同幂等键请求正在处理 | ❌ 不要重试 POST · 用 GET 查 |
| 404 | `not_found` | 路径或资源不存在 | ❌ |
| 503 | `api_disabled` | 管理员临时关了 API | ✅ 稍后 |
| 500 | `internal_error` | xi8 出错 | ✅ 退避后 |

**⚠️ 202 特别说明**（vendor 原文）：`ok` 是 `false` 但**不代表失败** —— 表示"钱已扣 · 结果还不知道"。通常是上游超时 · xi8 无法确认扣没扣费 · 所以既不自动退款也绝不重下。这种单挂起等人工核对 · 通常几分钟到几十分钟。**收到 202 不要重发 POST** · 改用 `GET /my/orders/{order_no}` 轮询。

---

## 7 · vendor_id → 我方 slug 映射

`internal/xi8/types.go` 已定（实证 2026-08-11 · 见 `decisions §11.11`）：

| xi8 `vendor_id` | 我方 slug | 判据 |
|---|---|---|
| 1 | `kiroceo` | 价 101/70.70 · 无质保 |
| 2 | `kiroappio` | 价 80.80/40.40 · warranty 10min |
| 3 | `kirodrop` | USD 计价 · 无质保 |
| 4 | `kirooo` | 价 101/70.70 · 时间戳吻合 |
| 5 | `kiro91` | warranty 10min · 时间戳吻合 |

**`kiroappcc` 不在 xi8**（xi8 只对接 5 家）。

**⚠️ 未来 xi8 加新 vendor** · 需在 `vendorSlugByXi8ID` 补映射并说明证据。

---

## 8 · xi8 价格 vs 我方探针价（实测差异 · 2026-08-13）

| vendor | 我方探针 | xi8 | 差 |
|---|---|---|---|
| kiroappio | US 80 / EU 40 | US 80.80 / EU 40.40 | **+1.0%** |
| kiroceo | US 100 / EU 70 | US 101 / EU 70.70 | **+1.0%** |
| kirooo | US 100 / EU 70 | US 101 / EU 70.70 | **+1.0%** |
| kirodrop | US 49.98（USD 7.35 × 6.8）| US 51.97 / **EU 36.34** | **+4.0%** · **EU 我方拿不到** |
| kiro91 | 稀疏（缺货多）| — | — |

**两个发现**：
1. **1% 系统性偏差** —— xi8 CNY 价比我方**一律高 1%** · 疑似 xi8 加了自己 1% 议价（不是 bug）
2. **kirodrop 4% 差** —— 我方汇率 6.8 · xi8 隐含 7.07 · **汇率口径分歧未解**（见 `docs/vendors/drop-kiro-ss.md §7` 缺口 3）

---

## 9 · 我方缺口（按优先级）

| # | 缺什么 | 影响 | 优先级 |
|---|---|---|---|
| 1 | **`GET /my/notifications` 未接** | **唯一有历史价格的端点** · 补不了探针上线前的价格 | **最高** |
| 2 | `GET /vendors` 的 `quality` 五字段不落库 | xi8 独家号质量评级（survival / risk / grade / verdict / note）全丢 · 6 家 vendor 自己都不给这数据 | **高** |
| 3 | `GET /stock` 未接（比 `/vendors` 更全）| 缺 `buyable` / `blocked` / `block_reason` / `floating` / `stock_ttl_secs` | **高** |
| 4 | `buyable` 未用 · 我方自己算 `stock > 0` | 会漏 `blocked` 态（xi8 主动停售）→ 我方以为有货 | **高** |
| 5 | xi8 webhook 未配 | 靠 30s 轮询 · 抢货场景差 30s | **中** |
| 6 | signals 的 `regions[]` 只取第一个 | 跨区批次丢一半 | **中** |
| 7 | restock-log 的 `stock` / `prev_stock` 不落库 | 分析"多快被抢空"缺序列 | 中 |
| 8 | `floating` 标记不落库 | 不知道哪家价会随时变 | 中 |
| 9 | `GET /status` 未接 | 一个免鉴权请求拿全网概览 | 低 |
| 10 | `GET /stock?shape=flat` 未接 | AutoPick 交叉核对 | 低 |
| 11 | 下单 / 取号 6 端点 | 我方不经 xi8 采购 | ❌ 阶段外 |
