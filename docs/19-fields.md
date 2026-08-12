# 19 · 跨 vendor 字段对齐表（**权威** · 拉平所有上游差异）

> **读这份的场景**：写 adapter / 加字段 / 排查"为什么这家没数据" / 决定前端展示什么。
>
> **写法**：每组一张表 · 列头是 vendor · **格子里写 vendor 自己的原文字段名**（不是我方名）·
> 最后一列是**我方标准字段**。空格 = 该 vendor 不给这个数据。
>
> **单家详情看** `docs/vendors/<家>.md`（每家 §2 逐端点字段清单 + §6 adapter 缺口）。
>
> **定价规则看** `docs/18-pricing-normalization.md`（换算 / 三档 / 减免栈）。

---

## 0 · 端点覆盖率总览

| vendor | 官方端点数 | 我方接了 | 覆盖率 | 端点风格 |
|---|---:|---:|---:|---|
| **kiro91** | 31 | 8 | 26% | `/api/my/*` · 最详细文档（684 行）|
| **kirooo** | 32 | 7 | 22% | `/api/my/*` · **端点最多** |
| **kiroappio** | 25 | 8 | 32% | `/api/me/*` · 分页信封 |
| **kiroceo** | 8 | 8 | **100%** | `/api/my/*` · 最精简 |
| **kiroappcc** | 5 公开 + 15 隐藏 | 4 | 80%（公开）| `/openapi/*` · camelCase |
| **kirodrop** | 8 | 6 | 75% | `/api/my/*` + `/api/me/*` + `/api/v1/*` 混用 |
| **xi8**（聚合源）| 25 | 3 | 12% | `/api/*` + `/api/my/*` |

---

## 1 · 组 1 · 库存（可购数量）

| vendor | 上游字段（原文）| 结构 | 我方标准字段 |
|---|---|---|---|
| **kiro91** | `stock.public_available`（总）· `zones[].available`（逐区）| 数组 `zones[]` | `StockSnapshot.Available` · `ZoneStock[].Available` |
| **kiroceo** | `max`（总）· `zones[].available` · `zones[].stock` | 数组 `zones[]` | 同上 |
| **kirooo** | `stock`（总 · 单区端点）· `regions[].stock`（逐区）· `regions[].claimable` | 数组 `regions[]` | 同上 |
| **kiroappio** | `stock`（总）· `stock_us` · `stock_eu` | **平铺后缀** | 同上（adapter 拆两条）|
| **kiroappcc** | `availableKeys` | **无区 · 单值** | 同上（Zone=general）|
| **kirodrop** | `stock` | **单区 · 单值** | 同上 |
| **xi8** | `total_stock`（总）· `us.stock` / `eu.stock`（`/stock`）· `regions[].stock`（`/vendors`）| 两种都有 | `vendor_probe_zone.available`（source=xi8）|

**⚠️ 差异点**：
- **kiroceo 同时给 `available` 和 `stock`** —— 语义可能是"可提 vs 库存"（部分被 reserved）· 我方只用 `available` · **差异不落库**
- **kirooo 给 `claimable`** —— 比 `stock` 更准（考虑了我方配额）· 我方**没用**
- **kiroappio 平铺** —— 无数组 · adapter 硬编码拆成 us/eu 两条

---

## 2 · 组 2 · 单价（★ 最重要 · 计费依赖）

| vendor | 上游字段（原文）| 币种 | 类型 | 我方换算 |
|---|---|---|---|---|
| **kiro91** | `zones[].unit_price`（**现价**）· `zones[].base_price`（基准价）| 积分 | int | `× 10^6` → `Money{credit}` |
| **kiroceo** | `zones[].unit_price` | 积分 | int | `× 10^6` → `Money{credit}` |
| **kirooo** | `regions[].unit_price` | 积分（**= 1 CNY**）| int | `× 10^6` → `Money{credit}` |
| **kiroappio** | `price`（默认）· `price_us` · `price_eu` · 文档还提 `price_min` / `price_max` | 积分 | int | `× 10^6` → `Money{credit}` |
| **kiroappcc** | `keyPrice` | 积分 | int | `× 10^6` → `Money{credit}` |
| **kirodrop** | `price` | **USD** | **str**（`"7.35"`）| `parseFloat × 10^6` → `Money{USD}` → `× 6.8` → 积分 |
| **xi8** | `price_fen`（分）· `price`（元字符串）· `old_price_fen`（变化前）| **CNY** | int / str | `price_fen × 10^4` → microunit → 1:1 积分 |

**我方标准字段**：`ZoneStock.UnitPrice`（`providers.Money{Amount, Currency}`）→ 入库时换算成 `vendor_probe_zone.our_unit_credits`（microunit 积分）。

**⚠️ 换算规则**（`docs/18 §1.3`）：
```
our_unit_credits = UnitPrice.Amount × vendor_pricing.credits_per_unit / 1_000_000
```
5 家 `credits_per_unit = 1_000_000`（1:1）· kirodrop `= 6_800_000`（$1 = ¥6.80）。

**⚠️ 数据缺口**：

| 缺什么 | 哪家有 | 影响 |
|---|---|---|
| **基准价**（原价）| 只 **kiro91** 给 `base_price` | 做不了"原价 40 → 现价 25"划线展示 |
| **分档区间** | **kiroappio** `price_min` / `price_max` · **kirooo** `/my/key-price-tiers` | 做不了"80~120 积分"区间 · `vendor_price_tier` 表空着 |
| **逐把实付** | **kiro91** `keys[].paid` · **kiroappio** `keys[].price` | 混价单对账缺权威值 |
| **变化前价** | 只 **xi8** 给 `old_price_fen` | 做不了降价幅度分析 |

---

## 3 · 组 3 · 区域标识（★ 最容易错 · 每家都不一样）

| vendor | zone 字段 | region 字段 | 中文名 | 结构 | 归一后 |
|---|---|---|---|---|---|
| **kiro91** | `zones[].zone`（`us`/`eu`）| `zones[].region`（`us-east-1`/`eu-central-1`）| — | 数组 | ✅ `us` / `eu` |
| **kiroceo** | `zones[].zone`（`us`/`eu`）| ❌ **不给** | `zones[].label`（`美国区`/`欧洲区`）| 数组 | ✅ `us` / `eu` |
| **kirooo** | ❌ **不给** | `regions[].region`（`us-east-1`/`eu-central-1`）| `regions[].label` | 数组 | ⚠️ **落成 `us-east-1`** · **bug** |
| **kiroappio** | ❌ 不给 | ❌ 不给 | — | **平铺 `_us`/`_eu`** | ✅ `us` / `eu`（adapter 硬编码）|
| **kiroappcc** | ❌ 不给 | ❌ 不给 | — | **完全无区** | ✅ `general` |
| **kirodrop** | ❌ 不给 | `region`（`us-east-1`）| — | **单区单值** | ⚠️ **落成空** · **bug** |
| **xi8** | `region`（`us`/`eu`）| — | `region_label`（`美区`/`欧区`）| 数组 / 对象键 | ✅ `us` / `eu` |

**我方标准字段**：**`providers.Zone`** · 值域 **`us` / `eu` / `general`** · 归一函数 `providers.ZoneOf()`。

**⚠️ 这是我方唯一的地区标准** —— 前端 `type Zone = "us" | "eu"` · 后端 `providers.ZoneUS/ZoneEU` · DB `vendor_probe_zone.zone` · API 请求/响应一律 `zone`。

**⚠️ 两个 adapter bug**（`docs/19-fields.md §9` 待修）：
1. **kirooo** · `Zone: providers.Zone(r.Region)` 直接转 → 落 `"us-east-1"` · 应改 `providers.ZoneOf(r.Region)`
2. **kirodrop** · 完全没归一 → 落空 · 应改 `Zone: providers.ZoneOf(sr.Region)`

**⚠️ `ZoneStock.Region` 是死冗余**（`docs/19-fields.md §9` 待清）：
- 只 kiro91 / kirooo / kirodrop 三家给原文 · kiroceo / kiroappio / kiroappcc 三家空
- 原文在 `vendor_probe.raw_snapshot` 里已全存 · 单独抠一列出来让"一半空"看着像数据有洞
- **决定**：删字段 + 侧表列停写

**⚠️ AWS 端点对照**（kiro91 明确警告 · 其他家没说但同理）：

| zone | region | AWS 端点 |
|---|---|---|
| `us` | `us-east-1` | `https://codewhisperer.us-east-1.amazonaws.com` |
| `eu` | `eu-central-1` | `https://q.eu-central-1.amazonaws.com` |

**两个区主机名形态不一样** —— 欧区没有 `codewhisperer.eu-central-1`（根本不解析）· 拿欧区 key 打美区端点得 403 · **看起来像废号实际只是打错地方**。

---

## 4 · 组 4 · 质保

| vendor | 上游字段 | 时长 | 条件 | 退款方式 | 我方标准字段 |
|---|---|---|---|---|---|
| **kiro91** | `warranty_minutes`（stock）· `keys[].warranty_until`（purchase）| 默认 10 分钟 · 可变 | 时间 | **自动退 + `warranty_refund` webhook** | `StockSnapshot.WarrantyMinutes` |
| **kiroceo** | ❌ 不返 | 恒 10 分钟（文档写死）| 时间 | 自动退 · **无 webhook**（静默入账 reason=`refund`）| 我方硬编码 |
| **kirooo** | ❌ 不返 | 未明说 | — | — | 无 |
| **kiroappio** | `warranty_minutes` | 实测 10 分钟 | 时间 | — | ✅ |
| **kiroappcc** | ❌ 不返（在 orders 详情 `warrantyStatus`）| **2 小时** | ★ **双条件**：2h **OR** 累计消耗 7000 积分 · 满一即结束 | — | ⚠️ **我方只判时间** |
| **kirodrop** | ❌ 不返 | 文档未明说 | — | 订单状态含 `partially_refunded` / `refunded` · `refunded_amount_cny` | 无 |
| **xi8** | `warranty_minutes`（可为 null）| 逐家不同 | — | — | 不落库 |

**⚠️ 最大缺口**：**kiroappcc 双条件质保** —— 我方只判 2 小时 · **缺"累计消耗 7000 积分"维度** · 号已出保我方还当在保 → 退款判断错。

**⚠️ 只 kiro91 有 `warranty_refund` webhook** —— 其他家质保退款都是静默的 · 我方只能靠对账发现。

---

## 5 · 组 5 · 我方在 vendor 侧的额度 / 余额

| vendor | 余额字段 | 额度字段 | 持号上限 | 类型 | 我方标准字段 |
|---|---|---|---|---|---|
| **kiro91** | `profile.balance` | — | `profile.max_keys_held`（我设）· `hold_cap_effective`（真生效）· `keys_held`（当前持有）| int | `Balance`（microunit）|
| **kiroceo** | `remaining`（= `quota` − `used_quota`）| `quota` / `used_quota` | `min_purchase` / `max_purchase`（单次）| int | 同上 |
| **kirooo** | `remaining` | `quota` / `used_quota` | `claimable`（可领）· `reserve_count` / `min_reserve` | int | 同上 |
| **kiroappio** | `balance` | — | `min_purchase` / `max_purchase` | int | 同上 |
| **kiroappcc** | `balance`（`/openapi/balance`）| — | ❌ 不给 | int | 同上 |
| **kirodrop** | `remaining` · `balance`（stock 里 · **CNY**）| `quota` / `used_quota` | ❌ 不给 | **str** | 同上（`parseFloat`）|
| **xi8** | `/my/balance` · `/my/profile` | 限流额度 | `max_per_order` | int | 未接 |

**⚠️ 字段名同名不同义**：
- **kiroceo / kirooo / kirodrop** 的 `quota` **语义是积分**（不是"还能提几个号"）· vendor 明说"字段名一个都没改 · 只是数字现在表示积分"
- **kirodrop** 的 `quota` / `remaining` / `used_quota` 是 **字符串**（CNY 有小数）· 其他家是 int

**⚠️ 数据缺口**：
- **kiro91 `hold_cap_effective` / `keys_held` 不落库** —— vendor 明说"买之前用这两个数判一下能省掉一次注定失败的下单"
- **kirooo `claimable` 不落库** —— 比 `stock` 更准（已考虑我方配额）

---

## 6 · 组 6 · 拉号请求（purchase / claim）

| vendor | 端点 | 数量字段 | 幂等键 | 区域字段 | 价格保护 | 批次指定 |
|---|---|---|---|---|---|---|
| **kiro91** | `POST /api/my/purchase` | `count`（1–200）| `client_order_id`（**32-hex**）或 `Idempotency-Key` 头 | `zone`（不传=只美区 · 其他值 400）| ❌ | ❌ |
| **kiroceo** | `POST /api/my/purchase` | `count` | `client_order_id`（**32-hex**）或头 | `zone`（同上）| ❌ | ❌ |
| **kirooo** | `POST /api/my/keys/claim` | `count`（**上限 500**）| `client_order_id` | `region`（**必须显式 `eu-central-1`**）| ❌ | ❌ |
| **kiroappio** | `POST /api/me/purchase` | `count` | `client_order_id` | `region`（`us`/`eu` · 也接受长名）| ❌ | ★ `order_id` |
| **kiroappcc** | `POST /openapi/claim` | `count`（可选 · 默认 1）| ❌ **无幂等键** | ❌ 无区 | ❌ | ❌ |
| **kirodrop** | `POST /api/my/purchase` | `count` **或** `quantity` | `client_order_id` | `region` | ★ **`max_total_cny`** | ★ `order_id` |
| **xi8** | `POST /my/orders` | `quantity` | `client_order_id`（≤64 字符）| `region`（必填）| ★ `max_price_fen` | ❌ |

**我方标准字段**：`providers.PurchaseRequest{Count, ClientOrderID, Zone, MaxTotal}`。

**⚠️ 最大风险**：**kiroappcc 无幂等键** —— 6 家里唯一 · **网络超时无法安全重试** · 我方只能靠 `pending_purchase` 状态机兜（`docs/09-transactions.md`）。

**⚠️ 只 kirodrop 原生支持价格保护**（`max_total_cny`）—— 我方 `vendorMaxTotal()` 就是为它写的 · 其他 5 家靠我方 `strategy.decide` 那层护栏。

**⚠️ 幂等键格式不统一**：kiro91 / kiroceo 要**严格 32-hex**（正则约束）· 其他家宽松。

---

## 7 · 组 7 · 拉号响应（key 交付）

| vendor | key 结构 | 逐把价 | 权威总额 | 实际成交 | 区域 | 质保 |
|---|---|---|---|---|---|---|
| **kiro91** | `keys[]` **对象**：`{id, round_id, key, region, zone, free, paid, warranty_until}` | ★ `keys[].paid` | ★ `total_credits` | `purchased` | `keys[].region` + `zone` | `keys[].warranty_until` |
| **kiroceo** | `keys[]` **对象**：`{key, account, password, issuer_url}` | ❌ | ★ `total_credits` | `purchased` | 顶级 `zone` | ❌ |
| **kirooo** | （需再抓确认）| ❌ | ❌ **不返金额** | `purchased` | — | ❌ |
| **kiroappio** | `keys[]` 对象 · 含 `price` | ★ `keys[].price` | ★ `total_debit` | `purchased` | — | ❌ |
| **kiroappcc** | `keys[]` **字符串数组** | ❌ | ★ `pointsCost` | 隐含（数组长度）| ❌ | ❌ |
| **kirodrop** | （需再抓确认）| ❌ | `total_credits`（⚠️ 字段名 credits 但 CNY 计价）| — | 顶级 `region` | ❌ |
| **xi8** | `keys[]` 对象：`{key, account, password, region}` | ❌ | `balance.fen`（扣后余额）| — | `keys[].region` | ❌ |

**我方标准字段**：`providers.PurchaseResult{OrderID, ClientOrderID, Zone, Purchased, Requested, TotalCost, Keys[]}`。

**⚠️ 关键差异**：
1. **kiroappcc 的 `keys` 是字符串数组** —— 其他家是对象数组
2. **kiro91 2026-08-07 停发 `account` / `password` / `issuer_url`** —— 只给 `key`（安全考虑：同一子号从多 IP 使用 = 凭证泄露特征 → 整号被封）。**kiroceo 还在给四件套**
3. **kirooo 的 `/my/purchase-orders` 不返金额** —— 对账必须配 `/my/credits`
4. **kirodrop 的 `total_credits` 字段名骗人** —— 这家 CNY 计价 · 需核对 adapter 是否正确处理

**⚠️ 数据缺口**：**逐把实付全丢** —— kiro91 `keys[].paid` / kiroappio `keys[].price` 我方都不落库 · 混价单对账缺权威值。

---

## 8 · 组 8 · key 生命周期 / 明细

| 字段 | kiro91 | kiroceo | kirooo | kiroappio | kiroappcc | kirodrop |
|---|---|---|---|---|---|---|
| **端点** | `/api/my/keys`（只前缀）| `/api/my/keys`（只 count）| `/api/my/keys?all=1` ★ **最全** | `/api/me/keys`（分页）| `/openapi/orders` 内嵌 | ❌ |
| id | — | — | `keys[].id` | — | orders 内 | — |
| 明文 | ❌ 只前缀 | — | ★ `keys[].key` | — | orders 内 | — |
| 状态 | `status`（`sold`/`dead`/**`revoked`**）| 只 aggregate `active` | `keys[].status` | — | `probeState` | — |
| **可疑态** | ❌ | ❌ | ★ 顶层 `suspect` | ❌ | ❌ | ❌ |
| 死因 | — | — | ★ `keys[].dead_reason` | — | — | — |
| 产出时刻 | — | — | `keys[].created_at` | — | — | — |
| **派发时刻** | — | — | ★ `keys[].dispatched_at` | — | — | — |
| **最近探活** | — | — | ★ `keys[].last_probe` | — | `probeState` | — |
| **当前用量** | `/api/my/usage` 三端点 ★ | — | ★ `keys[].current_usage` | — | ★ `usageSnapshot` | — |
| **用量上限** | ★ `max`（usage 端点）| — | ★ `keys[].usage_limit` | — | — | — |
| **用量速率** | — | — | ★ `keys[].usage_rate` | — | — | — |
| **订阅类型** | ★ `subscription`（usage 端点 · 如 "Kiro Pro"）| — | — | — | — | — |
| **额度重置** | ★ `reset_days` | — | — | — | — | — |
| 母号 | `keys[].round_id` | `pool_id`（webhook）| ★ `keys[].master_id` | `mother_id`（webhook）| — | — |
| 区域 | `keys[].region` | — | `keys[].region` | — | — | — |
| **转售** | — | — | ★ `keys[].on_sale` / `listing_price` | — | — | — |
| **质保状态** | `keys[].warranty_until` | — | — | — | ★ `warrantyStatus` | — |
| **退款时刻** | — | — | — | — | ★ `refundedAt` | — |

**我方标准字段**：`credential` 表 + `credential_ledger`（我方自己的台账）· vendor 侧 key 明细**基本不落库**。

**⚠️ 最大浪费**：
- **kirooo 是唯一给逐把 key 完整明细的**（用量 / 探活 / 死因 / 转售 / 母号）· 我方 adapter **只拿聚合数 · `keys[]` 完全不解析**
- **kiro91 有 key 用量三端点**（含 `subscription` + `reset_days`）· 我方**完全没接** · 却在用 kiro.rs 自己探
- **kiroappcc `/openapi/orders` 有 `probeState` / `warrantyStatus` / `usageSnapshot`** · 我方**不落库**

**⚠️ 状态枚举不统一**：

| vendor | 状态值 |
|---|---|
| kiro91 | `sold` / `dead` / `revoked` |
| kirooo | （自定义 · 另有顶层 `suspect` 计数）|
| kiroappcc | `probeState`（值域待确认）|

**我方收敛**（`CLAUDE.md §12.5`）：对乘客只 **"活" / "已失效"** 二态。

---

## 9 · 组 9 · fleet 观测（vendor 平台大盘）

| 字段 | kiro91 | kiroceo | kirooo | kiroappio | kiroappcc | kirodrop |
|---|---|---|---|---|---|---|
| **端点** | ❌ **无**（只能 stock-delta 推算）| `/api/my/gen-logs` ★ **全平台可见** | `/status` + `/my/stock/regions` | `/api/status` | ❌ | `/api/status` |
| 存活数 | — | — | `keys_active` / `keys_alive` | — | — | `keys_active` |
| 已死数 | — | — | `keys_dead` | — | — | `keys_dead` |
| 库存数 | — | — | `keys_stock` | `stock` | — | `keys_stock` |
| **可疑数** | — | — | ★ `keys_suspect` | — | — | — |
| 总数 | — | — | `keys_total` | — | — | — |
| **正在开号** | — | `items[].status=running` | ★ `generating` + `fleet_active` | ★ `generating` | — | ★ `generating` |
| 运行时长 | — | — | `started_at` / `uptime_secs` | `started_at` / `uptime_seconds` | — | — |
| **批次列表** | — | ★ `items[]`（`created_at`/`count`/`status`）| ★ `regions[].dispatches[]` | — | — | — |
| **平均间隔** | — | ★ `avg_interval_min` | — | — | — | — |
| **开号起点** | — | — | ★ `fleet_started_at` + `fleet_now` | — | — | — |
| **自动模式** | — | — | ★ `auto_mode` | ★ `auto_generate` / `auto_check` | — | — |
| **站点公告** | — | — | ★ `announce.*`（enabled/level/text/updated_at/updated_by）| — | — | — |
| **验证码配置** | — | — | — | ★ `captcha_enabled` / `captcha_app_id` | — | — |

**我方标准字段**：`vendor_probe.ps_*` 系列（`ps_keys_active` / `ps_keys_dead` / … / `ps_generating` / `ps_started_at` / `ps_uptime_seconds`）+ `vendor_dispatch` 表（批次）。

**⚠️ kiro91 完全没有 fleet 端点** —— 我方只能靠**探针 stock-delta 推算**（`vendorview/dispatch_deriver.go`）+ xi8 补齐。

**⚠️ 数据缺口**：
- **kirooo `announce.*`** 不落库 —— vendor 挂"今晚维护"公告我方不知道
- **kirooo `fleet_active`** 只用于探针提频 · **不落库** —— 事后分析"开号节奏 vs 抢号成功率"没数据
- **kiroappio `auto_check` / `auto_generate`** 不落库 —— "vendor 是否在自动补货"的健康信号丢了
- **kiroceo `avg_interval_min`** 只用于提频 · 不落库

---

## 10 · 组 10 · Webhook 事件类型

| 事件 | kiro91 | kiroceo | kirooo | kiroappio | kiroappcc | kirodrop |
|---|---|---|---|---|---|---|
| **有新号** | `new_keys_available` | `new_keys_available` | `new_keys_available` | `new_keys_available` | （已接 · 事件名待确认）| `new_keys_available` |
| **全部失效** | `all_keys_dead` | `all_keys_dead` | `all_keys_dead` | `all_keys_dead` | — | `all_keys_dead` |
| **质保退款** | ★ `warranty_refund` | ❌ 静默入账 | ❌ | ❌ | ❌ | ❌ |
| **包量交付** | ★ `reserved_keys_delivered` | ❌ | ❌ | ❌ | ❌ | ❌ |
| **滥用收回** | ❌ | ❌ | ❌ | ★ `key_revoked_abuse` | ❌ | ❌ |
| **测试** | `webhook_test` | `test` | `test` | `test` | — | `test` |
| **事件数** | **5** | 3 | 3 | **4** | ? | 3 |

**⚠️ 两个"处理不对就出事"的独家事件**：

| 事件 | vendor | 不处理的后果 |
|---|---|---|
| **`reserved_keys_delivered`** | kiro91 | **钱扣了 · 号记在我方名下 · 但程序永远拿不到 key**（vendor 明说这条通知里的 `order_id` 是取正文的唯一入口）|
| **`key_revoked_abuse`** | kiroappio | vendor 主动收回已售 key · 我方还当它活着 → **用户拿到废号** |

**⚠️ 我方 `internal/webhookin/` 是否处理了这两个？** —— `docs/19-fields.md §12` 待查。

---

## 11 · 组 11 · Webhook 载荷字段

| 字段语义 | kiro91 | kiroceo | kirooo | kiroappio | kirodrop | xi8 |
|---|---|---|---|---|---|---|
| **事件名** | `event` | `event` | `event` | `event` | `event` | `event` |
| **去重 id** | `event_id` + 头 `X-KM-Event-Id` | `event_id` | `event_id`（32 小写 hex）| `event_id` | `event_id` + 头 `X-Kiro-Event-Id` | `notif_id` |
| **幂等键** | `purchase_order_id`（⚠️ **不是订单号**）| `purchase_order_id`（= `event_id`）| ★ **`client_order_id`**（vendor 明说别用 event_id）+ 旧名 `purchase_order_id` | `purchase_order_id` | `purchase_order_id` + ★ **`purchase_order_ids_by_region`** | `vendor_order_id` |
| **订单号** | ★ 只 `reserved_keys_delivered` 有 `order_id` | ❌ | ❌ | ★ `order_id` | ★ `order_id` | — |
| **新增数** | `new_keys` | `new_keys` | `new_keys` | `new_keys` | `new_keys` + ★ `new_keys_by_region` | `count` |
| **失效数** | `dead` | `dead` | `dead` | `dead` | `dead` | — |
| **区域** | `zone` + `region` | `zone` | ❌ | — | `region` + ★ `regions[]` | `region`（**可能空串**）|
| **母号** | `pool_id` · `mother_id` | `pool_id`（⚠️ 全死事件可能**逗号连接多个**）| — | `mother_id` | — | — |
| **车次** | `round_id` | — | — | — | ★ `dispatch_id` | — |
| **价格** | ❌（要查 stock）| ❌ | ❌ | ★ `price_us` / `price_eu`（**恒存在**）| ❌ | ★ `price_fen` + `price_stale` |
| **库存** | ❌ | ❌ | ❌ | ★ `stock_us` / `stock_eu`（**恒存在**）| ❌ | — |
| **可见性** | `visibility` | ❌ | ❌ | ★ `visibility`（private/public）| ❌ | — |
| **时刻** | `timestamp`（Unix 秒）| ❌ | `time`（**UTC+8** 字符串）| `finished_at` | `created_at`（UTC）| `time`（ISO）|
| **摘要** | `message` | `message` | `message` + `claim_hint` | `message` | `message` | `title` + `hint` |
| **批次 id** | — | — | — | — | ★ `batch_ids_by_region` | — |
| **通知范围** | — | — | — | — | ★ `notification_scope="dual"` | — |
| **重试标记** | 头 `X-KM-Delivery-Attempt` | ❌ | ❌ | ❌ | ❌ | ★ `retry` |
| **测试标记** | `event=webhook_test` | `event=test` | `event=test` | `event=test` | `event=test` | ★ `test` 字段（`vendor_id`=0）|

**⚠️ 我方生产实测踩过的坑**（`decisions §11.x`）：
- **kirooo / kiroceo 只给 `client_order_id` / `purchase_order_id` · 没有独立 `order_id`** —— 我方代码原来只读 `OrderID` → 空 → dispatch 落库失败。**已修**（fallback 到 `PurchaseOrderID`）
- **kirodrop 双区合并通知** —— 幂等键要从 `purchase_order_ids_by_region` 按区取 · **不是顶级那个**。**待查我方是否正确处理**

---

## 12 · 组 12 · Webhook 签名

| vendor | 签名头 | 算法 | 签名原文 | 我方已接 |
|---|---|---|---|---|
| **kiro91** | `X-KM-Signature`（`sha256=<hex>`）+ `X-KM-Timestamp` | HMAC-SHA256 | `timestamp + "." + rawBody` | ✅ |
| **kiroceo** | ❌ **无签名** | — | 靠 URL secret | ✅（无签名可验）|
| **kirooo** | ❌ **无签名** | — | 靠"不可猜 URL 路径当口令" | ✅ |
| **kiroappio** | （待确认）| — | — | ✅ |
| **kiroappcc** | `X-Kiro-Signature` | HMAC-SHA256 | `body` | ✅ |
| **kirodrop** | `X-Kiro-Signature`（**`v1=<hex>`**）+ `X-Kiro-Event-Id` + `X-Kiro-Timestamp` | HMAC-SHA256 | `timestamp + "." + rawBody` | ✅ |
| **xi8** | （未配 webhook）| — | — | ❌ |

**⚠️ 3 家有签名 · 3 家没有** —— kiroceo / kirooo 靠 URL 保密（我方 webhook 路径含随机 token）。

**⚠️ kirodrop 的 `v1=` 前缀** · kiro91 的 `sha256=` 前缀 —— 格式不统一 · 我方 verify 逻辑要分家处理。

---

## 13 · 组 13 · 重试策略

| vendor | 超时 | 重试次数 | 间隔 | 4xx 行为 | 停推 |
|---|---|---|---|---|---|
| **kiro91** | — | 3 | 递增 + 抖动 | — | — |
| **kiroceo** | 10s | 3 | 3s / 8s | — | — |
| **kirooo** | — | 3 | **0s / 2s / 6s** | ★ **4xx 视为拒绝不再重试** | — |
| **kiroappio** | — | — | — | — | — |
| **kiroappcc** | — | — | — | — | — |
| **kirodrop** | **8s** | 3 | 1s | — | — |
| **xi8** | ★ **3s**（最严）| 3 | — | 非 2xx 算失败 | ★ **连续失败 5 次自动停推** · `push_paused=true` |

**⚠️ xi8 超时最严（3s）** · vendor 明说"抢货是拼秒的 · 一个慢地址不该拖住其他用户" —— **必须先回 200 再异步下单**。

---

## 14 · 组 14 · 错误契约

| vendor | 响应体 | 有稳定 code? | 我方分派依据 |
|---|---|---|---|
| **kiro91** | `{code, message, error}` | ✅ **20+ 个 code** | 判 `code`（vendor 明说别判文案）|
| **kiroceo** | `{error: "中文文案"}` | ❌ **只有中文** | 只能按 HTTP status |
| **kirooo** | （待确认）| — | — |
| **kiroappio** | `{error: "描述"}` | ❌ | HTTP status |
| **kiroappcc** | `{error: {message, type}}` | ⚠️ 有 `type` | — |
| **kirodrop** | `{error: {code, details, message, request_id}}` | ✅ + ★ **`request_id`** | 判 `code` |
| **xi8** | `{ok, msg, error}` | ✅ **12 个 error** | 判 `error` |

**⚠️ kiroceo 错误契约最弱** —— 只有中文文案 · 文案改了我方就挂。我方只能按 HTTP status 分派。

**⚠️ 只 kirodrop 给 `request_id`** —— 找 vendor 排查时能直接引用。

---

## 15 · 组 15 · 限流

| vendor | 规则 | 作用域 |
|---|---|---|
| **kiro91** | 按账号 · `Retry-After` 头 · 登录/注册另有按 IP 限速 | 账号 |
| **kiroceo** | 429 · 无具体数字 | — |
| **kirooo** | 429 · 指数退避 | — |
| **kiroappio** | — | — |
| **kiroappcc** | ★ **60 次 / 60s** · 超出 **180s 冷却** · 429 + `Retry-After` | ★ **按账号**（多 key 共享）|
| **kirodrop** | 文档未写 | — |
| **xi8** | ★ **三桶互不占用**：read 120/min · write 30/min · **order 10/min** | 账号 |

**⚠️ kiroappcc 限流最严**（60/min + 180s 冷却）· 且**多 API key 共享额度** —— 我方探针 + 拉号共用同一个 key 时要注意。

---

## 16 · 组 16 · 供应侧（母号 / 发车 · **我方全部阶段外**）

| vendor | 端点数 | 独家能力 |
|---|---|---|
| **kiro91** | 8 | `queue_position` / `queue_total` 排队进度 · `pool` public/private · 售出分成 |
| **kirooo** | 6 | `auto-fleet`（自动车）· `fleet-roster`（发车名单）· `reserve`（预留）|
| **kiroappio** | 7 | `target_alive`（目标存活数）· `refill-config` 4 参数 · 手动 `generate` |
| **kiroappcc** | 3（需 cookie）| **收益分成模式** · "评价"字段（拉完了/NPC/人上人/夯）· 半人工审核 |
| **kiroceo** | 0 | **纯代购 · 无供应侧** |
| **kirodrop** | 0 | 无 |

**为什么全不接**：`CLAUDE.md §3` 明确不做 AWS 开号（阶段 3b/3c 才转发）。

---

## 17 · 组 17 · xi8 独家数据（6 家 vendor 都不给）

| 字段 | 语义 | 我方是否落库 |
|---|---|---|
| **`quality.survival`** | 号存活时长区间（`"40 分钟-2 小时"`）| ❌ |
| **`quality.risk`** | 风险等级（低/中/高）| ❌ |
| **`quality.grade`** | 评级（推荐/观察/警告）| ❌ |
| **`quality.verdict`** | 一句话总结 | ❌ |
| **`quality.note`** | 补充说明 | ❌ |
| **`buyable`** | 一个布尔顶掉 stock>0 + price>0 + !blocked | ❌ |
| **`blocked`** / **`block_reason`** | xi8 主动停售（成本数据可疑）| ❌ |
| **`floating`** | 价格随商家成本浮动 | ❌ |
| **`restock_source`** | `webhook`（最准）/ `商家` / `推算`（偏晚最多 492s）| ❌ |
| **`stock_ttl_secs`** | 库存缓存周期（30s）| ❌ |
| **`old_price_fen`** | 变化前价格（降价事件）| ❌ |
| **`price_stale`** | 这个价是刷新前的缓存 | ❌ |

**⚠️ `quality` 五字段是最可惜的** —— 6 家 vendor 自己都不给号质量评级 · xi8 有 · 我方 `/api/vendors/status` 页面的"质量"维度本来可以直接用。

---

## 18 · 我方标准字段总表（前后端一路统一）

| 概念 | Go（`internal/providers`）| DB | 前端 TS | API 契约 |
|---|---|---|---|---|
| **地区** | `providers.Zone`（`us`/`eu`/`general`）| `vendor_probe_zone.zone` | `type Zone = "us" \| "eu"` | 请求/响应 `zone` |
| **单价（原始）** | `ZoneStock.UnitPrice`（`Money{Amount, Currency}`）| `vendor_probe_zone.vendor_unit_raw` + `vendor_currency` | ❌ 不下发 | ❌ |
| **单价（我方积分）** | — | `vendor_probe_zone.our_unit_credits` | `unit_price`（microunit）| `unit_price` |
| **库存** | `ZoneStock.Available` | `vendor_probe_zone.available` | `available` | `available` |
| **vendor 身份** | `providers.VendorID` | `vendor_probe.vendor_id` | `vendor_id`（**匿名档返 anon_id**）| `vendor_id` + `anon_id` + `vendor_label` |
| **质保** | `StockSnapshot.WarrantyMinutes` | `vendor_probe.warranty_minutes` | `warranty_minutes` | `warranty_minutes` |
| **余额** | `StockSnapshot.Balance` | ❌ 不落库 | ❌ | ❌ |
| **实扣** | `PurchaseResult.TotalCost` | `pull_round` + `wallet_ledger` | — | — |
| **fleet 状态** | `PublicStatusSnapshot` | `vendor_probe.ps_*` | `status` 页字段 | `/api/vendors/status` |
| **出货批次** | `providers.VendorDispatch` | `vendor_dispatch` | `dispatch.*` | `/api/vendors/{id}/events` |

**⚠️ 前端不给的**（`CLAUDE.md §0.1`）：原始 vendor 数字（未换算）· `region` 原文 · `raw_snapshot` · 各层分项 · 内部枚举 · xi8 任何字段 · vendor 真名（除 wholesale 档）。

---

## 19 · 待修 bug + 待接端点（**第 5 步的工作单**）

### 19.1 是 bug · 必修

| # | 问题 | 位置 | 修法 |
|---|---|---|---|
| 1 | **kirooo `Zone` 落 `us-east-1`** 不是 `us` | `kirooo/mapper.go` | `Zone: providers.ZoneOf(r.Region)` |
| 2 | **kirodrop `Zone` 落空** | `kirodrop/mapper.go` | `Zone: providers.ZoneOf(sr.Region)` |
| 3 | **`ZoneStock.Region` 死冗余** | `providers/vendor.go` + 侧表 | 删字段 + migration 031 停写 |
| 4 | **kiroappcc 质保只判时间** · 缺 7000 积分维度 | 我方质保逻辑 | 加用量维度 |

### 19.2 待查（可能已经是 bug）

| # | 要查什么 | 为什么 |
|---|---|---|
| 5 | `internal/webhookin/` 是否处理 **kiro91 `reserved_keys_delivered`** | 漏了会丢号（钱扣了拿不到 key）|
| 6 | `internal/webhookin/` 是否处理 **kiroappio `key_revoked_abuse`** | 漏了用户拿废号 |
| 7 | **kirodrop 双区通知**是否按区取 `purchase_order_ids_by_region` | 用错幂等键 → 拉错区 / 重复扣费 |
| 8 | **kirodrop `TotalCost`** 字段名 credits 但 CNY 计价 · adapter 处理对吗 | 可能币种搞错 |
| 9 | **kirodrop `partially_refunded`** 状态是否处理 | 部分退款订单可能状态卡住 |

### 19.3 待接端点（按价值排序）

| # | 端点 | vendor | 价值 |
|---|---|---|---|
| 1 | **`GET /my/notifications`** | xi8 | ★ **唯一有历史价格的端点** · 补探针上线前数据 |
| 2 | **`GET /api/v1/reservation`** | kirodrop | ★ EU 定价 + 分档报价 + 权威汇率口径（`vendor_price_tier` 表因此空着）|
| 3 | **`GET /my/key-price-tiers`** | kirooo | ★ 完整定价阶梯 |
| 4 | **`GET /api/my/stock/rounds`** | kiro91 | ★ 逐车次现价 + 降价参数 |
| 5 | **`GET /stock`** | xi8 | `buyable` / `blocked` / `floating` / `restock_source` |
| 6 | **`keys[]` 明细解析** | kirooo | 用量 / 探活 / 死因 / master_id（唯一给这些的）|
| 7 | **key 用量三端点** | kiro91 | `subscription` / `reset_days` / 权威 remaining |
| 8 | **`GET /my/credits`** | kirooo | kirooo 对账唯一金额来源 |
| 9 | **`quality` 五字段落库** | xi8 | 号质量评级（6 家 vendor 都不给）|
| 10 | **`GET /my/dispatch-log`** | kirooo | vendor 侧车次质量数据 |
| 11 | **`GET /api/my/ledger`** | kiro91 / kiroappio | vendor 侧流水双向对账 |
| 12 | **xi8 webhook** | xi8 | 省 30s 轮询延迟（抢货关键）|

### 19.4 数据缺口汇总（vendor 给了但我方不落库）

| 类别 | 缺什么 | 哪家 |
|---|---|---|
| **定价** | `base_price`（原价）| kiro91 |
| | `price_min` / `price_max`（区间）| kiroappio |
| | `keys[].paid` / `keys[].price`（逐把实付）| kiro91 / kiroappio |
| | `old_price_fen`（变化前价）| xi8 |
| **库存** | `claimable`（考虑配额后的可领）| kirooo |
| | `available` vs `stock` 差异 | kiroceo |
| | `buyable` / `blocked` / `floating` | xi8 |
| **额度** | `hold_cap_effective` / `keys_held` | kiro91 |
| **key 明细** | 用量 / 探活 / 死因 / 转售 / master_id | kirooo |
| | `probeState` / `warrantyStatus` / `usageSnapshot` / `refundedAt` | kiroappcc |
| **fleet** | `announce.*`（站点公告）| kirooo |
| | `fleet_active` / `fleet_started_at` | kirooo |
| | `auto_check` / `auto_generate` | kiroappio |
| | `avg_interval_min` | kiroceo |
| **质量** | `quality.*` 五字段 | xi8 |
| **其他** | `风控四字段`（risk_flag/rate/threshold/at）| kirooo |
| | `refunded_amount_cny` | kirodrop |
| | `restock_source`（时间戳准确度）| xi8 |
