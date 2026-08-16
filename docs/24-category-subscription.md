# 24 · Offer 模型（vendor × category × subscription × zone）· 全链路 map

> **读这份的场景**：动 `category`(企业号/个人号) 或 `subscription`(Power/PRO/PRO+) 任何一处代码前。
>
> **为什么单独一份**（`CLAUDE.md §4.3` 要求说明）：这四个维度共同决定一次购买的商品，横跨
> **10 个环节**（§7 的 A-J：库存 / 展示 / 选择 / 估价 / 下单 / 落库 / 自动补车 / 撮合 / 用量 / 派发）。
> 现有文档每份只管一片（`06-db-schema` 管表 · `05-api-contract` 管端点 · `10-pricing` 管算价 ·
> `15-scheduling` 管策略），没有一份能回答"改一处要连带改哪些"。
>
> **状态**：⏸ **冻结中** —— 2026-08-16 审计判定既有实现领域模型错位。
> 作废概念见 §2 · 解冻前置条件见 §9 · 冻结范围见 §10。
>
> **决策背景**看 `decisions.md §8.45`。

---

## 1 · 领域模型 · 四个平级维度

一次购买的商品叫 **Offer**，由四个**平级**维度共同决定：

```
Offer = vendor × category × subscription × zone
```

| 维度 | 取值 | 含义 |
|---|---|---|
| **vendor** | 6 家（可扩）| 从哪家供应商拉 |
| **category** | `enterprise` / `personal` | 这一轮拉企业号还是个人号 |
| **subscription** | `power` / `pro` / `pro_plus` | 拉哪个订阅档位 |
| **zone** | `us` / `eu` / 无区 | 哪个区服 |

**四者平级 · 无包含关系。** 用户可以从任一维度先选，其余维度按可用性联动（§4）。

同一家 vendor 的真实形态：

```
Vendor A
  ├── enterprise · 可能支持 · 可能有货
  ├── personal   · 可能支持 · 可能有货
  └── 两个 category 的库存 / 档位 / 价格各自独立
```

**⚠️ `subscription` 跟 `passenger.tier` 是两回事**：`tier`（`retail`/`community`/`wholesale`）是
**买号的人**的档次（决定折扣 + 能否看 vendor 真名 · `docs/10-pricing §2.1`）；
`subscription` 是**号本身**的档位（决定 quota 上限）。别混。

---

## 2 · 作废概念（**这些说法一律不用**）

| 作废 | 为什么错 | 替代 |
|---|---|---|
| `vendor 属于 enterprise 或 personal` | vendor 与 category 平级 · 一家可同时供两类 | vendor **支持哪些** category |
| `6 家 vendor = enterprise 线` | 账号类型来自 vendor 的 Offer · 不能按"在不在 registry"推导 | 每家的 capability matrix |
| `personal = 运营手工上架` | 未经协议证据的推断（§9）| 待上游契约确认 |
| `category 是号的来源线` | category 是**单个 Offer 的商品类型** | 见 §1 |
| **`企业车` / `个人车`** | 车没有账号类型 · 一辆车可先拉企业号后拉个人号 | 车只有**拉号偏好**（§6）|
| `bus.account_kind` / `bus.category` | 同上 | `bus.pull_category_preference`（§6）|
| `anon 撮合要按 category 分车` | category 只约束后续拉号 · 不定义车辆身份 | 撮合**不看** category |
| `category === "enterprise" ? ["power"] : ["pro","pro_plus"]` | 组合矩阵不能全局硬编码 · 由各 vendor Offer 决定 | 从 Offer matrix 推（§5）|
| `categories: ["enterprise"]`（只表示"出现过"）| 分不清"不支持"与"支持但缺货" | `supported` + `available` 分离（§3）|
| `any` 作为一轮购买的 category | `any` 只属于自动拉号偏好 | 手动拉号必须落到具体 category（§5）|
| `kiro_market` 作为第 7 家 vendor | 不可购买的 pseudo-vendor · 见 §10 | 冻结 |
| `market_inventory.available` 作为可售库存 | 无 item 级 allocation / 预占 / 扣减 | 冻结 |

**看到旧对话或旧文档里出现左列说法，立即警觉。**

---

## 3 · 后端库存模型 · Offer matrix

**不能**只返 `categories: ["enterprise", "personal"]` —— 那只说明"出现过这个 category"，
回答不了：是否支持 / 当前是否有货 / 有多少 / 什么档位 / 哪个区 / 什么价。

正确形状（`GET /api/vendors/offers` · 单一数据源）：

```json
{
  "vendors": [{
    "vendor_id": "v01",
    "vendor_label": "AWS-Q Kiro Vendor 01",
    "categories": {
      "enterprise": {
        "supported": true,
        "available": 10,
        "offers": [{
          "offer_id": "v01:enterprise:power:us",
          "subscription": "power", "zone": "us",
          "available": 10, "unit_price": 30000000
        }]
      },
      "personal": {
        "supported": true,
        "available": 5,
        "offers": [
          { "offer_id": "v01:personal:pro:general",
            "subscription": "pro", "zone": null,
            "available": 3, "unit_price": 22000000 },
          { "offer_id": "v01:personal:pro_plus:general",
            "subscription": "pro_plus", "zone": null,
            "available": 2, "unit_price": 28000000 }
        ]
      }
    }
  }]
}
```

**`supported` 与 `available` 必须分离** —— 三种状态要能区分：

| supported | available | UI |
|---|---|---|
| `true` | `> 0` | 可选 · 显示数量 |
| `true` | `0` | **暂时缺货** · disabled |
| `false` | `0` | **该 vendor 不提供** · disabled |

**单一数据源**：前端 vendor / category / subscription 三个选择器都从**同一份** Offer 数据算
enabled/disabled。不能继续 `/vendors/stats` 判 category + `/vendors/{id}/stock` 拿库存 +
`/vendors/auto-pick` 走第三套 —— 三份数据必然漂移。

---

## 4 · UI · 二维可用性矩阵 · 双向联动

用户心里的表是这个（不是层级）：

```
                企业号          个人号
Vendor A        有货 10         有货 5
Vendor B        有货 8          缺货
Vendor C        不支持          有货 12
Vendor D        缺货            不支持
```

**vendor 或 category 任一变化，都从这张矩阵重算 enabled/disabled。**

### 4.1 两个 category 始终显示

缺货**不能隐藏** —— 用户需要知道系统支持个人号，只是当前没货。

```
企业号        可选 · 12 个
个人号        暂时缺货 · disabled
```

### 4.2 先选 vendor → category 联动

选了 Vendor B（支持企业、不支持个人）：

```
企业号        可选 · 8 个
个人号        该 vendor 暂不提供 · disabled
```

### 4.3 先选 category → vendor 联动

选了个人号，vendor 列表**仍全部显示**（用户要知道谁支持、谁只是暂时缺货）：

```
Vendor A     个人号有货 · 可选
Vendor B     个人号缺货 · disabled
Vendor C     不提供个人号 · disabled
Vendor D     个人号有货 · 可选
```

### 4.4 vendor = auto 时的 category 可用性

按**全网**判：

```
enterprise enabled = 至少一家启用 vendor 有 enterprise Offer 库存
personal   enabled = 至少一家启用 vendor 有 personal   Offer 库存
```

全网企业 0 / 个人 20 时：

```
企业号        暂时缺货 · disabled
个人号        可选 · 20 个
```

选个人号后 AutoPick **只在 personal Offer 里挑 vendor**。

### 4.5 组合非法时不静默切换

用户已选 `personal + PRO`，再切到不支持 personal 的 Vendor B：

```
personal 仍显示为当前选择（标红 / disabled）
确认按钮不可用
提示"该 vendor 暂不提供个人号，请选择其他 vendor 或切换企业号"
```

**不要静默改成 enterprise** —— 否则用户可能买到与预期不同的账号。

---

## 5 · 前端选择状态 · 平级

```ts
type PullSelection = {
  vendorId: string | "auto";
  category: "enterprise" | "personal";
  subscription: "power" | "pro" | "pro_plus" | null;
  zone: string | "auto";
};
```

**不是** category 包 vendor，也**不是** vendor 包 category。

### subscription 是第三个平级约束

**不能**按 category 写死（当前 `PullExtractForm.tsx:48` 就是这个错）。
合法档位来自**当前 vendor + category 对应的 Offers**：

```
Vendor A + personal → 该 vendor 提供 PRO / PRO+  → 下拉两项
Vendor B + personal → 该 vendor 只提供 PRO       → PRO+ disabled
vendor=auto         → 全网是否存在满足 category+subscription 的 Offer
```

### 手动拉号必须落到具体 Offer

```json
{ "count": 3, "vendor_id": "auto",
  "category": "personal", "subscription": "pro", "zone": "auto" }
```

**不能提交 `"category": "any"`** —— `any` 只属于自动拉号偏好，不是一轮具体购买的商品类型。

即使车级偏好是 `any`，首次/手动拉号**仍必须明确选**这一轮拉企业号还是个人号，
最终解析成一个确定 Offer：`vendor=v02 · category=enterprise · subscription=power · zone=us`。

---

## 6 · 车级 = 拉号偏好 · 不是车类型

**车没有 enterprise/personal 身份。** 一辆车可以先拉企业号、后拉个人号 —— 只要偏好允许。
车内已有企业号**不代表**这是"企业车"。

字段名：`bus.pull_category_preference`（**不叫** `bus_category` / `account_kind`）

| 存储 enum | 运行时集合 | 自动补车行为 |
|---|---|---|
| `enterprise_only` | `["enterprise"]` | 只在 enterprise Offer 里选 · **企业缺货就挂等 · 不降级买个人号** |
| `personal_only` | `["personal"]` | 只在 personal Offer 里选 · **个人缺货不买企业号** |
| `any` | `["enterprise","personal"]` | 两类一起比价（库存 / 价格 / vendor 偏好 / 价格上限）· 车内可混合持有 |

运行时最好转成集合处理（`AllowedCategories []Category`）。

### 首次拉号 vs 车级偏好 · 两个不同概念

| | 作用 | 取值 |
|---|---|---|
| **首次/手动拉号** | 本轮买什么 | 必须是具体 category |
| **车级拉号偏好** | 自动补车允许买什么 | `enterprise_only` / `personal_only` / `any` |

合法组合示例：

```
首次拉号 personal + PRO · 后续偏好 any       ✅
首次拉号 enterprise      · 后续偏好 personal_only ✅
```

车内已有号**不决定**后续只能拉同类型。

---

## 7 · 策略优先级（对齐 `CLAUDE.md §1.5` / `15-scheduling §4.3`）

category 的优先级**不是**「request > bus」这种简单覆盖，要分两条路径：

### 手动 / 首次请求

```
request.category = 本轮必须购买的具体 category（硬约束）
```

用户本次点"拉个人号"，**不能**因为个人号缺货就自动买企业号。

### 自动补车（无 request）

```
bus.allowed_categories > passenger 全局 allowed_categories > 系统默认
```

AutoPick 在允许集合内选。车级 `any` → 集合 `{enterprise, personal}` → 两类比价。

**归类**：`allowed_categories` 属 `§4.3.2` 的**类②覆盖字段**（后者盖前者），
但手动 request 的具体 category 是**本轮硬约束** —— 不受 `any` 放宽。

---

## 8 · 全链路状态表

> ✅ 通了 · ❌ 断了 · ⚠️ 部分 / 概念错 · 🧊 冻结（实现存在但方向错 · 见 §10）

| # | 环节 | 代码位置 | 状态 | 缺口 |
|---|---|---|---|---|
| **A · 上游契约** | | | | |
| A1 | provider `Stock` 接口 | `providers/vendor.go:121` | ❌ | 只有 zone · **无 category / subscription / offer** |
| A2 | provider `Purchase` 接口 | `providers/vendor.go:219` | ❌ | 只有 count/zone/幂等 · **无 offer_id** |
| A3 | 6 家 adapter 采购请求 | `providers/kiro/vendors/*/` | ❌ | 同上 |
| A4 | vendor capability matrix | — | ❌ | **不存在** · 无从判断谁支持哪个 category |
| A5 | personal stock/purchase 协议证据 | — | ❌ | **仓库内无任何抓包** · §9 阻塞点 |
| **B · 库存（读）** | | | | |
| B1 | Offer matrix 端点 | — | ❌ | **不存在**（§3 是目标形状）|
| B2 | `/vendors/stock` 聚合 | `vendorview.go:AggregateStock` | ⚠️ | 有 `by_category` · 但 registry vendor 全被归 enterprise |
| B3 | `/vendors/stats` → `categories[]` | `vendorview.go:Stats` | ⚠️ | 只表示"出现过" · **无 supported/available 分离** |
| B4 | `/vendors/{id}/stock` | `vendorview.go:VendorStock` | 🧊 | market-only 合成路径 · **可展示但不可购买** |
| B5 | `/vendors/auto-pick` | `vendorview.go:AutoPick` | ❌ | 只遍历 registry · **不认 category** |
| B6 | `market_inventory` | `migrations/045` | 🧊 | 无 item 级 allocation / 预占 / 扣减 |
| **C · 展示 + 选择（前端）** | | | | |
| C1 | 提取页 category tab | `pages/Extract.tsx` | ⚠️ | tab 在 · 但**缺货即隐藏 vendor** · 无 supported/缺货区分 · 无双向联动 |
| C2 | subscription 下拉 | `PullExtractForm.tsx:48,54` | ❌ | 按 category **硬编码组合** · 且 state **从不进请求** |
| C3 | vendor 下拉联动 | `PullExtractForm.tsx:43` | ❌ | 按 `categories[]` 过滤 → 不支持的 vendor 直接消失（应 disabled）|
| C4 | 拼车拉号弹窗 | `PullNowModal.tsx` | ❌ | 整个维度不存在 |
| C5 | 建车向导 | `StartCarpoolModal.tsx` | ❌ | 同上 |
| **D · 估价** | | | | |
| D1 | `/me/pull/estimate` | `api/estimate.go:16` | ❌ | 不接 category/subscription → PRO 与 PRO+ **同价** |
| D2 | `PricedFor`（唯一算价入口）| `vendorview/priced_for.go` | ❌ | 不认 Offer 维度 |
| D3 | market-only 价格 | `vendorview.go:505` | 🧊 | **写死 30 积分**当成本基础 · 违反 `CLAUDE.md §1.3` |
| **E · 下单** | | | | |
| E1 | `pullRequest`（单独提取）| `api/pull.go:22` | ❌ | 4 字段 · 无 Offer 维度 |
| E2 | `pullRequest`（拼车拉号）| `api/bus.go:291` | ❌ | **复用同一 struct** |
| E3 | `decider.PullInput` | `decider/orchestrator.go:291` | ❌ | 收了也传不下去 |
| **F · 落库** | | | | |
| F1 | housepool import 元数据 | `decider/import.go:45` | ❌ | 只收 credential ID · **丢掉 event 里的 Subscription** |
| F2 | 写 `credential_ledger` | `decider/settle.go:348` | ❌ | INSERT 不含新列 → migration 044 的列**0 行有值** |
| F3 | 扣库存 / 预占 | `internal/marketinv/` | ❌ | 只有聚合读 + 后台 Upsert · **无 decrement/reserve/release** → 必然超卖 |
| **G · 自动补车（4 条路径）** | | | | |
| G1 | 自动补车 | `decider/refill_puller.go:45` | ❌ | 无 Offer 维度 |
| G2 | 抢号 fire | `decider/fire.go:51` | ❌ | 同上 |
| G3 | 车级偏好 | `bus` 表 | ❌ | **无 `pull_category_preference`**（§6）|
| G4 | 全局偏好 | `passenger_strategy_default` 表 | ❌ | 同上 |
| G5 | `EffectiveStrategy` | `strategy/effective.go:30` | ❌ | 无 `AllowedCategories` → §7 优先级不生效 |
| G6 | stockwatch 挂单上下文 | `stockwatch/store.go:76` | ❌ | 只存 vendor/region/count → **恢复后可能买错商品** |
| **H · 撮合** | | | | |
| H1 | anon 车撮合 | `bus.anon_zone` + `max_unit_price` | ✅ | **不该按 category 分车**（§2）· 现状正确 |
| **I · 用量** | | | | |
| I1 | 前端用量条 | `lib/utils.ts:106` | ❌ | `QUOTA_MAX = 10_000` 写死 → PRO 错 10 倍 |
| I2 | 死号 95% 阈值 | `api/pullrecord.go:428` | ❌ | 按 10000 算 → 个人号**永不判 quota** |
| I3 | housepool 真 quota | `housepool/types.go:84` | ✅ | `UsageLimit` / `CurrentUsage` / `UsagePercentage` 已可读（§11）|
| **J · 派发** | | | | |
| J1 | assign（进车/推池/拿走）| `api/pullrecord.go` | ⚠️ | 本身不关心 Offer · 受 F2 拖累 |
| J2 | 推 passengerpool | `kirors/types.go:39` | ⚠️ | `Subscription` 字段在 · 源头 NULL → 推过去永远空 |

**一句话**：只有"展示"这一半勉强能跑（且概念错），**估价 / 下单 / 落库 / 自动补车 / 用量全断**。

---

## 9 · 解冻前置：上游协议证据

**当前最大阻塞**：仓库内**没有任何** personal stock / purchase 的真实请求响应。
`docs/23-endpoints-todo.md:60` 提到 `/stock/personal-pool`，但 6 家 vendor 档案和 adapter 里
都没有该端点证据。

`decisions.md §8.45` 里"上游只能走个人号线(用户手动上传)"是**未经协议证据的推断**，已作废（§2）。

### 必须先确认是哪一种

| | 含义 | 能解锁什么 |
|---|---|---|
| **A** | vendor API 仍可采购 · 只是商品类型变 personal | Stage 3 真拉号 → Stage 4/5/6 |
| **B** | 上游不开采购 API · 只能运营/用户手工导入 | **只能**验证"导入 → housepool → 派发"· Stage 3 **不能宣称解锁** |

### 情况 A 需要存档的证据

```
personal stock endpoint     · request / response
personal purchase endpoint  · request / response
是否需要 offer_id / pool_id / plan_id
PRO / PRO+ 是否购买前可选（还是购买后才观察到）
幂等字段 · 订单查询 · 返回 credential 结构
退款 / 失败状态 · 是否支持区服
单价来源 · quota 来源
```

**拿到证据前不新增 DB / UI。**

### 情况 A 的最小实施路线

```
① 单家 vendor 的 capability matrix
     supports_enterprise / supports_personal / offer_ids
     selectable_subscription / supports_zone / supports_idempotency
     returns_subscription_on_purchase
        ↓
② 最小 Offer 契约（购买传 offer_id · 不传易歧义的 category+subscription 字符串）
     type Offer struct {
       ID, AccountKind, Subscription, Zone, Available, UnitPrice
     }
        ↓
③ 只接一条 adapter：ListOffers/Stock → Purchase(offer_id) → 查单 → 错误翻译 → 幂等
        ↓
④ 贯通交易链：estimate → pull → decider pending → purchase
              → BatchImport（**带回 subscription/quota 元数据**）→ settle
              → credential_ledger 元数据 → 钱包结算
        ↓
⑤ 跑 1 个真号 smoke（验收见 §12）
        ↓
⑥ 再进 Stage 4（逐家按 capability matrix）/ 5（Offer 进 strategy+stockwatch+recovery）/ 6
```

**如果该 vendor 只有一个 personal Offer**，Stage 3 初版甚至不需要前端下拉 —— adapter 自动选唯一 Offer。

### 阶段归属修正

`decisions.md §8.45` 把整件事定为"阶段 2 主线"。实际拆分应是：

| 范围 | 阶段 |
|---|---|
| 支持**一家** vendor 的 personal Offer · 完成 Stage 3 smoke | **阶段 1 的 live-readiness gate** |
| 用户上传 / 运营预库存 / 公开市场 / 多来源售卖 | 阶段 2/3 **独立产品能力** |

绑在一起会让一个能小范围解锁 Stage 3 的任务膨胀成完整 marketplace 重构。

---

## 10 · 冻结范围（2026-08-16）

以下实现**方向错**，不要继续往下补，也不要带进 main：

- `kiro_market` 作为 `VendorID`（`providers/vendor.go:35`）—— 不可购买的 pseudo-vendor：
  没实现 `providers.Vendor` · 不在 registry · `api/pull.go:85` 会直接 400。
  **前端能选 → 后端必拒 = 假功能。**
- `market_inventory` 作为可售库存 —— 无 credential 关联 / reservation / allocation /
  sold 状态 / 乐观锁 / 失败释放 / 并发扣减。回答不了"用户买到哪一个号""并发怎么防重"
  "付款成功派号失败怎么归还"。
- `stock` / `on_demand` 两套履约 —— `stock` 无 item 级 allocation · `on_demand` **无执行器**。
  且聚合读会把 `on_demand.available` 当现货一起展示。
- market-only stock 合成（`vendorview.go:423`）+ **写死 30 积分**（`vendorview.go:505`）
- 手工 seed 工具（`cmd/bus-pooling/seed_credledger.go`）作为产品履约路径 ——
  无 BatchImport+ledger 原子事务 / group 归属校验 / 值域校验 / 审计边界。**只能是一次性运维工具。**
- "registry vendor 自动库存全部是 enterprise"（`vendorview.go:358,854`）—— 跟"上游目前只有
  personal"正好相反；且 `out_of_stock = len(cats)==0`（`vendorview.go:817`）让真缺货 vendor 显示成可选。
- category 与 subscription 的全局硬绑定

### 保留的设计意图

- credential 最终要记录规范化的 account kind / subscription / origin
- quota 读 housepool 真值（§11）
- 需要一份覆盖全链路的 map（本文 §8）
- Offer 维度最终必须进入估价 / 购买 / 落库 / 调度 / 恢复
- **但这些应在上游协议确认后重新实现**

---

## 11 · quota 上限：读 housepool 真值 · 别硬编码

`housepool.Balance`（`types.go:84`）已返 `SubscriptionTitle` / `CurrentUsage` /
`UsageLimit` / `UsagePercentage`，6 家 adapter 也都解了 `usage_limit`
（`providers/kiro/vendors/*/history.go`）。

所以 I1/I2 **不该按 subscription 硬编码 10000/1000/2000** —— 直接读真值。
硬编码会在 Kiro 改档位配额时静默错。

`lib/utils.ts:106` 的 `QUOTA_MAX = 10_000` 是阶段 1 只有企业号时的简化
（`decisions §8.14` 定的 10k 阈值只对 Power 档成立），现在是 bug 源。

### raw subscription 需要归一层

housepool 可能返 `Pro` / `KIRO PRO+` 等原始字符串，DB 用 `pro` / `pro_plus`。
**当前没有统一转换层** —— 必须在 import 边界做归一，并对无法识别的值留 NULL + 告警。

---

## 12 · 验收矩阵

### UI 联动

| 场景 | 正确行为 |
|---|---|
| Vendor A 企业有货 · 个人有货 | 两个 category 都可选 |
| Vendor A 企业有货 · 个人缺货 | 企业可选 · 个人**显示但 disabled**（"暂时缺货"）|
| Vendor A 不支持个人 | 个人显示"该 vendor 不提供" + disabled |
| Auto 模式 · 企业全网缺货 | 企业**显示但 disabled** · 不隐藏 |
| Auto 模式 · 个人有货 | 个人可选 · AutoPick **只在 personal Offer 中选** |
| 先选 personal · 再切到不支持 personal 的 vendor | **不静默切 enterprise** · 禁止确认 + 提示 |
| 先选 Vendor A · 再看 category | 按 Vendor A 的 Offer matrix 判 enabled |
| 首次拉号 | **必须**选明确 enterprise 或 personal（不接受 `any`）|

### 车级行为

| 场景 | 正确行为 |
|---|---|
| 车级偏好 `any` | 后续自动拉号可选 enterprise 或 personal |
| 车级偏好 `enterprise_only` | 自动拉号**绝不**买 personal · 企业缺货就挂等 |
| 车级偏好 `personal_only` | 自动拉号**绝不**买 enterprise |
| 车内已有企业号 | **不代表**该车是"企业车" |
| 车内已有个人号 | **不代表**该车是"个人车" |
| 用户本次明确选 personal | 这一轮**不能**被 `any` 放宽成 enterprise |

### Stage 3 smoke（情况 A · §9）

```
解析出明确 personal Offer
→ 真实 vendor 扣款
→ 返回 1 个 credential
→ BatchImport 成功
→ credential_ledger 有 account_kind + subscription
→ wallet / pull_round 金额一致
→ 崩溃恢复可重放
```

---

## 13 · 冻结执行记录（2026-08-16）

### 已删除

| 文件 | 为什么 |
|---|---|
| `internal/marketinv/`（整包）| §10 冻结 · 无 allocation/预占/扣减 |
| `internal/api/admin_market.go` | 手工上架 API · 依赖已删的包 |
| `cmd/bus-pooling/seed_credledger.go` | 手工 seed 不能当产品履约链 |
| `internal/db/migrations/044` | 列名未对齐 Offer 契约 · 无 CHECK · down 不回滚 · 无人写值 |
| `internal/db/migrations/045` | `market_inventory` 表本身已冻结 |
| `web/src/i18n/locales/*/vendor.json` | 只为 `kiro_market` 建的 · 一并撤 |

### 已回退到 HEAD

`vendorview.go`（除下方保留项）· `api/server.go` · `cmd/main.go` · `providers/vendor.go`
（`VendorKiroMarket` 常量）· `pages/Extract.tsx` · `PullExtractForm.tsx` · `i18n/index.ts`
· `types/index.ts` · `locales/*/extract.json`

**Extract 页 category tab + 档位下拉的形状车主已确认可用（§4/§5 记录了设计）**，
但当前无真实 Offer 数据可驱动 —— 等 Offer matrix 端点落地后按 §3-§5 重接。

### 保留的独立修复（与 Offer 模型无关）

| 修复 | 位置 | 为什么独立成立 |
|---|---|---|
| activities 的 vendor 真名匿名化 | `api/insight.go` | `insight/activities.go:123,126` 把 `vendor_id` 直接放进 `Source` 且拼进 `Summary` → **retail/community 档能看到真名**（§0.1 违规）|
| `Service.LabelFor(vendorID, Viewer)` | `vendorview.go` | 上一条需要"按档次出显示名"的入口 · HEAD 只有恒匿名的 `AnonLabelFor` · 未注册 id 返 `""` 让 caller 区分 bus_id/credential_id |
| `"91kiro"` → `kiro91` | `lib/utils.ts` + `mocks/` | **key 拼错**（后端是 `kiro91`）→ `vendorName()` 落 `?? id` 分支把原始 id 漏给 wholesale 档 · 且匿名编号查不到（显示成无编号兜底）|
| 匿名文案对齐后端 | `lib/utils.ts` | 前端算 `Vendor 01` · 后端返 `AWS-Q Kiro Vendor 01` → 同一家在不同页面两个名字 |
| lint 允许列表补 `kiro91` | `tools/lint/` | 允许列表原先只列错拼的 `91kiro` · 所以真 id 一直没被 lint 覆盖到 |

### 清理后实测（vs 基线）

| 检查 | 基线 | 现在 |
|---|---|---|
| `go build` | ✅ | ✅ |
| `go vet` | ✅ | ✅ |
| `go test` 失败包 | `internal/db`(3) + `tools/lint`(1) | **完全一致** |
| migration down 残留 | 3 张（041-043）| **3 张**（`market_inventory` 已消失）|
| 内部术语违规 | 22 行 | **19 行** |
| `gofmt -l` | 78 | 78 |
| `web build` | ✅ | ✅ |

`internal/db` 三条 migration 测试 + `tools/lint` 剩余 19 行是**基线既有问题**，
不在本次范围（`credential_ledger` 的 `category`/`subscription`/`source` 三列在
dev.db 里保留 —— SQLite `DROP COLUMN` 风险高 · 全 NULL 不影响读写 · 接 Offer 契约时复用）。
