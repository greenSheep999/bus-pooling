# 18 · Pricing 一整套 · 观测 → 换算 → 三档定价 → 减免

**2026-08-12 · 用户拍板 · 一份文档说清整个逻辑** · 不再散写。

这文档**覆盖以下老决策**里跟定价 / 分档相关的所有条目 · 未来读**这一份**即可：
`decisions §8.20` / `§8.29` / `§8.32` / `§8.34` / `§8.39` · `06-db-schema §16` · `05-api-contract §5`

老决策**保留原文**不删（历史查证用）· 但**发生冲突时以本文档为准**。

---

## 0 · 一句话结构

```
上游 vendor  →  机制 A · 我方观测层（数据库存原始 + 结果积分）
                    ↓ our_unit_credits（唯一权威积分价）
              机制 B · 按用户 tier 加加价栈  →  组合价（返给前端）
                    ↓ ⊗ 减免栈（临时码 / 邀请奖励 · 有时效）
              最终价（前端显示 · 唯一形态 = 积分）
```

**两条铁律**：
- **前端只看结果 · 单位一律积分（1 积分 = 1 RMB）**
- **换算逻辑跑在入库那一步 · 一次 · 之后所有读方读同一列**

---

## 1 · 机制 A · Vendor 观测层（数据库 · 内部权威）

### 1.1 责任

采齐上游 6 家的 stock / price / currency / exchange_rate / 分档 · 落库**原样存 + 计算标准化积分**。

**上游给的存原样 · 上游没给的我方计算填充** —— 这句话是本层核心。

### 1.2 表设计

**扩表 `vendor_probe`**（现有表加列 · migration 待定）：

```sql
-- 上游原样字段（拿到就存 · 没有则 NULL）
vendor_currency        TEXT       -- credit / CNY / USD
vendor_unit_raw        INTEGER    -- microunit · vendor 报价原值
vendor_exchange_rate   REAL       -- vendor 侧汇率（例 kirodrop 的 6.80）
vendor_price_usd_raw   INTEGER    -- USD 原值（kirodrop 有）
vendor_price_cny_raw   INTEGER    -- CNY 原值（kirodrop 双币展示的 CNY）
vendor_stock           INTEGER    -- 库存

-- 我方计算标准化（唯一权威积分列）
our_unit_credits       INTEGER NOT NULL   -- microunit · 唯一权威积分 · 1_000_000 = 1 积分 = 1 RMB
our_unit_source        TEXT               -- vendor_native / cny_direct / computed_from_usd / fallback_last_rate
our_computed_at        TEXT NOT NULL
```

**新表 `vendor_price_tier`**（分档规则 · 只 kirodrop 有 timed_pricing · 其他 5 家不写）：

```sql
CREATE TABLE vendor_price_tier (
  vendor_id            TEXT NOT NULL,
  region               TEXT,                -- us / eu
  probed_at            TEXT NOT NULL,       -- 探到这轮 schedule 的时刻
  tier_enabled         INTEGER,             -- 0 / 1
  tier_active          INTEGER,             -- 0 / 1（正在降价窗口内）
  tier_interval_min    INTEGER,             -- 每档间隔（分钟）
  tier_max_reductions  INTEGER,             -- 最多降几次
  tier_applied         INTEGER,             -- 已降几次
  tier_start_at        TEXT,                -- 阶梯启动时刻
  -- 下面每档一行（跟主键 tier_index 联合 unique）
  tier_index           INTEGER NOT NULL,    -- 0 = base · 1 = 第一次降 · ...
  effective_at         TEXT NOT NULL,       -- 这档生效时刻
  unit_price_credits   INTEGER NOT NULL,    -- microunit · 这档积分
  unit_price_usd_raw   INTEGER,             -- 这档 USD 原值（有则存）
  PRIMARY KEY (vendor_id, region, probed_at, tier_index)
);
```

### 1.3 换算规则（走 `vendor_pricing` + `exchange_rate` 表 · 系统配置 · 6 家都走）

**vendor_pricing** · 每家一行 · 换算规则表（可后台配 · 应对波动）：

```sql
CREATE TABLE vendor_pricing (
  vendor_id           TEXT PRIMARY KEY,
  quote_currency      TEXT NOT NULL,     -- credit / CNY / USD
  credits_per_unit    INTEGER NOT NULL,  -- microunit · 1 单位 vendor 报价 = N microunit 积分
  vendor_surcharge_bp INTEGER DEFAULT 0, -- 该家 vendor 附加费的率（bp · 100 = 1%）· 覆盖全局
  updated_at          TEXT NOT NULL
);
-- 5 家（credit / CNY）：credits_per_unit = 1_000_000（1 vendor 积分 = 1 我方积分 = 1 RMB）
-- kirodrop（USD）：credits_per_unit = exchange_rate × 1_000_000（当前 6_800_000）
```

**exchange_rate** · 汇率是系统配置字段 · 有历史（应对波动）：

```sql
CREATE TABLE exchange_rate (
  currency        TEXT NOT NULL,       -- USD 等
  rate_to_credits INTEGER NOT NULL,    -- microunit · 1 USD = N microunit 积分
  effective_from  TEXT NOT NULL,       -- 生效时刻
  effective_to    TEXT,                -- 失效时刻（当前汇率 NULL）
  source          TEXT NOT NULL,       -- system_config / vendor_ref（对齐上游）/ external_api
  PRIMARY KEY (currency, effective_from)
);
```

**换算路径**（6 家统一 · 入库时一次 · 之后不再算）：

```
Prober 拿到 vendor stock 响应
  ↓
读 vendor_pricing[vendor_id] · 拿 quote_currency + credits_per_unit
  ↓
按 quote_currency 分派：
  credit / CNY · our_unit_credits = vendor_unit_raw × credits_per_unit / 1_000_000
  USD          · our_unit_credits = vendor_unit_raw × credits_per_unit / 1_000_000（credits_per_unit 已含汇率）
  ↓
落 vendor_probe · 打时间戳
```

**汇率变了怎么办** · 后台改 `exchange_rate` 加一行新 effective_from · 定时任务 / 手动触发**重算 vendor_pricing.credits_per_unit** · 后续 Prober 落库自动用新值。历史 vendor_probe 行**不回填**（那是快照 · 保留当时汇率下的值）。

**对齐上游** · Prober 探到 vendor 返的 `exchange_rate` 值 · **跟我方系统配置比对** · 差 > 5% 打 WARN 到日志 · 提醒运维评估调整。

### 1.4 出口

- **Pricing 页** · 读 `vendor_probe.our_unit_credits` 时间序列 + `vendor_price_tier`
- **Status 页** · 读 `vendor_stock` + probe 元数据
- **拉号定价** · 见 §2 机制 B · 走同一列 `our_unit_credits`

### 1.5 Q2 · 数据缺失时的 fallback（用户拍 ABC 都要 · 我拍优先级）

1. **首选** · 用上一条已知的 `our_unit_credits` + 打时间戳（前端"更新于 N 分钟前"）
2. **B 没有再走** · "价格暂缺"（vendor 从没探到过 / 长期断线）
3. **C 不做** · 复杂度不值当

### 1.6 Q3 · kirodrop 分档到点更新（用户拍 ABC 都要 · 我拍优先级）

1. **首选 A** · Prober 60s tick 自动碰上（延迟最多 60s · 现成不加代码）
2. **观察后升级 B** · 若 60s 延迟真让我方错过抢号 · janitor 按 `schedule[].effective_at` 定时任务提前触发（准 · 复杂）
3. **C 未来** · 若 vendor 开 webhook 通知降价（现在没确认支持）· 加进 Notify 源

---

## 2 · 机制 B · 三档用户 · 加价栈

### 2.1 三档定义（用户 2026-08-12 拍板 · UI 一律不出现档次名）

| 中文档 | `passenger.tier` | 用什么码注册 | 看 vendor 真名 | 看到的价 |
|---|---|---|---|---|
| **零售** | `retail`（默认）| 无码 / 个人邀请码 | ❌ 匿名 label（Vendor 01 / 02 …） | 组合价 · 全套加 |
| **社群** | `community` | **社群码**（TG/Discord 投放） | ❌ 匿名 label | 组合价 · 免区域附加费 |
| **批发商** | `wholesale` | **批发商码**（B2B 定向发） | ✅ **真名** | 组合价 · 免 vendor + 区域附加费（几乎 pass-through） |

**只有 wholesale 看真名 + 上游积分原值** —— 别搞错。

**schema 改动**（跟老 §8.39 撞车 · 老那份改成 `retail / wholesale / insider` · 本条覆盖）：
```
passenger.tier CHECK(tier IN ('retail','community','wholesale'))
system_invite_code.grants_tier CHECK(grants_tier IN ('community','wholesale'))
```

### 2.2 加价栈（**静态 · tier 决定 · 永久**）

**逐层乘**（`§8.34` 铁律 · 号价 base 起 · 每层乘上一层结果）：

```
retail:     base × (1 + vendor_markup 67%) × (1 + region_markup 20%) × (1 + single_pull 20% 若 count=1) × (1 + service_fee 5%)
community:  base × (1 + vendor_markup 67%)                            × (1 + single_pull 20% 若 count=1) × (1 + service_fee 5%)
wholesale:  base                                                       × (1 + single_pull 20% 若 count=1) × (1 + service_fee 5%)
```

**关键铁律**：
- **`single_pull` 三档都加**（count=1 时 · 面向所有用户）
- **`service_fee` 三档都加**（5% · 固定收入层 · 恒在链末尾）
- **`vendor_markup + region_markup` 是 tier 的分界** —— retail 都加 · community 免区域 · wholesale 都免

### 2.3 实测数值（号价 20 积分 · 7 积分 ≈ 1 USD）

| 档 | 批量（count > 1） | 单拉（count = 1） |
|---|---|---|
| **retail** | 20 × 1.67 × 1.20 × 1.05 = **42.08 积分** ≈ 6.01 USD | 20 × 1.67 × 1.20 × 1.20 × 1.05 = **50.50 积分** ≈ 7.21 USD |
| **community** | 20 × 1.67 × 1.05 = **35.07 积分** ≈ 5.01 USD | 20 × 1.67 × 1.20 × 1.05 = **42.08 积分** ≈ 6.01 USD |
| **wholesale** | 20 × 1.05 = **21.00 积分** = 3.00 USD | 20 × 1.20 × 1.05 = **25.20 积分** ≈ 3.60 USD |

**倍率**（相对号价）：

| 档 | 批量 | 单拉 |
|---|---|---|
| retail | 2.10× | 2.52× |
| community | 1.75× | 2.10× |
| wholesale | 1.05× | 1.26× |

**说明**：
- community 批量 = retail 单拉的一半（不是错 · 数学上刚好）
- wholesale 只加 single_pull + service_fee · 是最接近 pass-through 的档
- retail 单拉是最贵的档

### 2.4 分项拆法约束（`§8.34` 保留）

1. **顺序影响分项 · 不影响总额** —— 乘法可交换 · 但"谁乘在谁后面"决定各层分到多少 · **链的顺序必须固定**
2. **舍入在最后一层做** —— 全程 microunit 整数算 · 每层取整后 · **最后一层用「总额 − 已分配」兜底** · 保证恒等式
3. `total_debit = 最终单价 × 号数` · 分项也各自 × 号数

### 2.5 率的存储

**在 `surcharge_rule` 表 · 不进代码**（初始配置写死这个值 · 后台可调）：

| 规则 | `kind` | 初始率 | 生效条件 |
|---|---|---|---|
| `vendor_markup` | `vendor` | 67% | 每家可覆盖（vendor 侧配置） |
| `region_markup` | `region` | 20% | tier ∈ {retail} · community / wholesale 都跳过 |
| `single_pull` | `adhoc` | 20% | `count == 1` |
| `service_fee` | `service` | 5% | 恒定 · 链末尾 |

---

## 3 · 减免栈（**动态 · 跟码 / 邀请奖励挂 · 有时效 / 有额度**）

**减免栈跟 tier 无关 · 跟码有关 · 拿到就减 · 用完 / 到期恢复到静态加价栈** —— 这是**跟 §2 完全正交**的一层。

### 3.1 四种减免

| 减免项 | 来源 | 减在哪层 | 时效 |
|---|---|---|---|
| **通道费减免 5%** | 个人邀请码额度（`personal_invite_code` · `§8.29`）| 充值时的 waffo 通道费 | 限次数 or 限时 |
| **服务费减免** | 邀请奖励 / 推广码 / 促销码 | 服务费层（免掉 N 轮 5%） | 限次数（N 轮）or 限时（N 天） |
| **单次议价减免** | 优惠码（`coupon_code`）· 提号确认窗填 | `single_pull` 那层 | 单次生效 |
| **组合价整体减 5-20%** | 优惠码 · 更激进 | 组合价最终额 | 单次生效 |

### 3.2 数据落地

**减免记录**（每份减免有 `expires_at` · 有 `remaining_uses`）：

```sql
CREATE TABLE user_subsidy (
  id                 TEXT PRIMARY KEY,
  passenger_id       TEXT NOT NULL,
  kind               TEXT NOT NULL,   -- channel_fee / service_fee / single_pull / total_discount
  source             TEXT NOT NULL,   -- personal_invite / promo / invite_reward / coupon
  source_ref         TEXT,            -- 码 id / 奖励 id
  amount_rule        TEXT NOT NULL,   -- JSON · 如 {"kind":"waive"} / {"kind":"pct","pct":10}
  remaining_uses     INTEGER,         -- NULL = 不限次
  expires_at         TEXT,            -- NULL = 不限时
  used_count         INTEGER DEFAULT 0,
  created_at         TEXT NOT NULL,
  FOREIGN KEY (passenger_id) REFERENCES passenger(id)
);
```

**会计科目**：
- 通道费减免 · 记 `channel_fee_subsidy` **独立科目**（我方垫付 · 财务可算清补贴总额 · 违反 `00 §3` pass-through 原则但**有意的营销支出**）
- 服务费减免 · 记 `service_fee_waived`
- 单次议价 / 组合价减 · 记 `coupon_discount`

### 3.3 计算顺序

```
静态加价栈（§2.2 tier 决定）
   ↓
应用减免栈（§3.1 · 按 kind 减对应层）
   ↓
最终价（前端显示）
```

**减免不改 tier · 不解锁真名** —— 拿到码只是省钱 · 该看不到的还是看不到。

---

## 4 · 唯一查询入口（前后端都走这里）

```go
// vendorview.PricedFor · Server 层拿到用户 tier + vendor + 数量 → 最终价 + 展示 label
func (s *Service) PricedFor(ctx, vendorID, region, count, viewer) (*PricedView, error) {
    // 1. 读机制 A · 结果积分
    credits := probeStore.LatestCredits(vendorID, region)
    staleAge := 0
    if credits == 0 {
        credits, staleAge = probeStore.LastKnownCredits(vendorID, region)  // Q2 fallback
        if credits == 0 { return nil, ErrPriceMissing }
    }

    // 2. 按 tier 走静态加价栈
    combined := applySurchargeStack(credits, viewer.Tier, count, rates)

    // 3. 应用减免栈（拉出用户可用 subsidies · 按 kind 减对应层）
    final := applySubsidies(combined, viewer.PassengerID)

    // 4. 按 tier 决定 label
    label := anonLabelOf(vendorID)
    if viewer.Tier == "wholesale" {
        label = realVendorName(vendorID)
    }

    return &PricedView{Label: label, PriceCredits: final, StaleAge: staleAge}, nil
}
```

**拉号 · 拼车 · Pricing 页 · Status 页** —— 全部走这个门面。不再有第二处算价。

**未登录**（没 session）—— api 层 401 · 定价接口不返 · UI 用公开脱敏视图（`CLAUDE.md §0.1`）。

---

## 5 · 落码顺序（每步一 commit · 可回退）

1. **migration** · vendor_probe 加 8 列 + 新表 `vendor_price_tier` + 新表 `user_subsidy` + 改 `passenger.tier` CHECK
2. **老 tier 数据迁移** · 生产库若有 `insider` 值（老 §8.39 命名）· 迁到 `wholesale`
3. **providers.StockSnapshot** 加字段 · `TieredPricing` 结构
4. **kirodrop adapter · Stock 打两个端点** · `/api/me/stock` + `/api/v1/reservation` 合成 snapshot
5. **其他 5 家 adapter** · 填 `our_unit_credits = vendor_unit_raw`（pass-through）
6. **Prober 落库** · 补 A 层字段 + 分档 tier 每档一行
7. **`vendorview.PricedFor`** · 唯一门面 · 服务费/tier 判断都在这
8. **decider `convertToMicroCredits` / `vendorMaxTotal`** · **删** · 走 PricedFor 拿的积分
9. **拉号 / 拼车 / Pricing / Status 全部改走 PricedFor**
10. **前端** · vendor 真名可见性由 API 决定 · retail / community 拿到的就是匿名 label · 不再前端藏

---

## 6 · 明确不做 · 明确要做（用户 2026-08-12 纠正）

### 明确不做
- ❌ **hot path 换算 / 反推 / 等比映射** —— `convertToMicroCredits` / `vendorMaxTotal` 全删 · 一律读入库后的结果积分列 `our_unit_credits`

### 明确要做（我之前误判 · 用户纠正）
- ✅ **`vendor_pricing` 表保留** —— 根据业务需要评估 · 存换算规则和历史（未来波动 / 下游议价基准都要用）· 每家一行 · 不是只 kirodrop
- ✅ **6 家都走换算** —— 不是"只 kirodrop 特殊 switch case" · 5 家哪怕 1:1 也走 `vendor_pricing.credits_per_unit=1_000_000` · **入库时我方程序自动补齐 our_unit_credits** · 统一路径
- ✅ **汇率是系统配置字段** —— 不是"用 vendor 给的" · 我方独立存 · vendor 给的值作参考对齐 · 我方下游议价 / 换算也用这份配置 · 字段设计要考虑**未来波动**（历史汇率快照 · 生效时段）

---

## 7 · 老决策覆盖表

| 老条目 | 冲突点 | 本文覆盖 |
|---|---|---|
| `§8.20` 邀请码双层机制 | 三档命名 `retail/wholesale/insider` | 改成 `retail/community/wholesale` |
| `§8.29` 邀请奖励减免服务费 | 具体减多少 | 归入 §3 减免栈 · 具体率 `user_subsidy.amount_rule` |
| `§8.32` 三种码定稿 | 减免规则散在这条 | 归入 §3 减免栈 · 一处说完 |
| `§8.34` 加价栈 | 分档命名 · 加价栈参考的老命名 | 加价栈保留 · tier 名对齐本文 §2.1 |
| `§8.39` 三档定价 · `insider=同行` | 三档命名 `retail/wholesale/insider` | 改成 `retail/community/wholesale` · 无"同行"档 |
| `06-db-schema §16` passenger.tier | CHECK 列表 | 改成 `('retail','community','wholesale')` |
| `05-api-contract §5` vendor 真名 | tier=wholesale 才可见 | 对齐 |

**未来读定价请只读本文** · 老条目查历史用。

---

## 8 · 参考

- `docs/vendors/*.md` · 6 家 API 详情
- `docs/17-vendor-work-order.md` · 全套编排 · A 层
- `docs/16-buy-race.md` · 抢号链
- `internal/pricing/*.go` · 换算实现（本文档 §1.3 落码后重写）
- `internal/decider/orchestrator.go` · 加价栈实现（本文档 §2.2 落码后重写）
- `CLAUDE.md §0.1` · 内部术语不出前端
- `CLAUDE.md §1.3` · 加法顺序（**改乘法**见 `§8.34`）
