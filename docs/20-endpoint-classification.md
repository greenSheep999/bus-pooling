# 20 · 端点分类与补接优先级（施工蓝图）

> **这份只干一件事**：把**尚未接入**的上游端点按"为什么要接"分类 + 排补接顺序。
>
> **不重复的边界**（CLAUDE.md §4.3）：
> - 字段级差异对照 → 看 `19-fields.md`
> - 定价换算规则 → 看 `18-pricing-normalization.md`
> - 抢号链设计 → 看 `16-buy-race.md`
> - **端点到底接没接 → 看代码**（`internal/providers/kiro/vendors/*/adapter.go` 的实际
>   HTTP 调用 · 和各家 `*_test.go`）。**本文不设"已接/未接"状态列** —— 那种列每次
>   部署就腐烂（17 号的教训 · 见 §5）。
>
> **判据来源**：2026-08-14 逐家文档端点表 × adapter 实际调用路径 × 生产库落库情况三方核对。

---

## 0 · 分类框架

补接需求分**两个正交维度**：

**维度 A · 为什么要接**（三选一）：

1. **交叉共性** —— 六家都有同类端点 · 缺一家对账链就断（vendor 自己的账本/流水）
2. **vendor 特殊** —— 只此一家给 · 别家无替代（独家数据）
3. **产品补充** —— 我方产品场景倒逼 · 不接对应功能就是残的/假的

**维度 B · 出不出前端**（二选一）：

- **内部辅助** —— 接进来做对账/质量/抢号决策 · **绝不出前端**（CLAUDE.md §0.1）
- **脱敏上前端** —— 经 vendorview 映射后可展示（价格类）

**不在这四类里的 = 明确不接**（阶段外 · 见 §4）· 别再算进"欠账"。

---

## 1 · 维度 A① · 交叉共性 · 对账链（**最高优先 · 全内部**）

**本质**：vendor 侧的积分流水 / 订单账本 · 跟我方 `wallet_ledger` + `pull_round` 双向核对。
六家都有同类端点 · **缺一家 = 那家的扣费无法验证是否多扣/漏退**。生产已开 `dry_run=false`
真实扣费 · 这是当前最实的缺口。

| vendor | 待接端点 | 我方已有 | 缺口 |
|---|---|---|---|
| kiro91 | `GET /api/my/ledger`（7 种 reason）| purchase-orders（订单 · 无逐笔流水）| 逐笔积分流水 |
| kiroappio | `GET /api/me/ledger`（8+ type）| purchase-orders | 逐笔积分流水 |
| kirooo | `GET /my/credits`（余额+流水）| purchase-orders | 逐笔积分流水 + 余额 |
| kiroappcc | `GET /api/user/orders` + `/txns` | `/openapi/orders`（已接）| 交易流水 txns |
| kiroceo | `GET /api/my/purchase-orders`（**已接**）| ✅ | 无独立流水端点 · 现状够 |
| kirodrop | 订单历史列表 | 只接单笔 keys 补拉 | 订单列表 |

**落地进度**（2026-08-14）：
- ✅ 基础设施：`providers.LedgerLister` + `VendorLedgerEntry`（reason 归一 6 类）·
  migration 033 `vendor_ledger` 表 · `vendorview.LedgerStore` · Backfiller 已接（拉 ledger 落库）
- ✅ 对账器 `vendorview.Reconciler` + CLI `bus-pooling reconcile [天数]` —— **三层核对**：
  存在性 / 数量（用 `vendor_order` · 5 家现成）+ 金额 / 漏退（用 `vendor_ledger` · 接了的家）·
  差异分 4 类：`orphan_ours` / `count_mismatch` / `amount_mismatch` / `refund_missing`
- ✅ kiro91 `ListLedger` —— **容错解析**（外层包装名 + 字段名各试多个 · 永远存 raw）
- ⏳ **其余 5 家 ledger adapter 待接**（kiroappio `/api/me/ledger` · kirooo `/my/credits` ·
  kiroappcc `/api/user/txns` · kirodrop 订单列表 · kiroceo 用已接的 purchase-orders）
  —— **必须先拿真实响应核字段再写**（别信文档推断 · kiroappcc webhook 100% 丢的教训）

**⚠️ kiro91 ledger 上线纪律**：响应 schema 是文档推断的（vendor 只给了 reason 列表）·
上线后**第一件事**是 `sqlite3 ... "SELECT raw FROM vendor_ledger WHERE vendor_id='kiro91' LIMIT 3"`
核字段名 · 对不上就按真实形状收紧 `ledger.go` 的 `ledgerRow`。

**对账现状**：`pull_round` 生产/本地都空（还没真实拉号）· 对账逻辑已就位测过（8 个单测覆盖
四类差异）· 等真拉号一发生就能 `reconcile` 出结果。

---

## 2 · 维度 A② · vendor 特殊 · 独家数据

只此一家给 · 接了能力多一块 · 不接主链不受影响 · 但 status/pricing 深度差一档。

| vendor | 端点/字段 | 独家价值 | 维度 B |
|---|---|---|---|
| kirooo | `GET /my/dispatch-log` | **唯一按车次给活死统计** → 真实寿命 | 内部（喂 quality）|
| kirooo | `GET /my/keys/export` | **唯一给 master_id + 死因** | 内部 |
| kirodrop | `GET /api/v1/reservation` | **唯一分档报价 + 权威汇率** → 填 `vendor_price_tier` | 脱敏上前端 |
| kiroappio | webhook `price_us/eu` + `stock_us/eu` | **唯一 webhook 直接带价+库存** | 内部→脱敏 |
| kiroappcc | webhook `price` | 第二个 webhook 带价的家 | 内部 |
| xi8 | quality 五字段 | **六家 vendor 都不给的号质量评级** | 内部（喂 quality 标签）|

**注**：kirodrop `reservation` 现在是 501 桩（`adapter.go:231`）· `vendor_price_tier` 表因此
恒空（有表无写入方）· 是这组里唯一卡住**前端定价真实性**的。

---

## 3 · 维度 A③ · 产品补充 · 场景倒逼

由我方功能驱动 · 不接功能就残：

| 端点 | 服务的产品场景 | 维度 B |
|---|---|---|
| kiro91 `GET /api/my/stock/rounds` | Prices 页逐车次现价（否则均值失真）| 脱敏上前端 |
| kirooo `GET /my/key-price-tiers` | 同上 · kirooo 定价阶梯 | 脱敏上前端 |
| xi8 buyable/blocked/floating | **抢号决策**：blocked 别 fire · floating 必带价保 | 内部 |
| xi8 webhook | **抢号速度**：省 30s 轮询延迟 | 内部 |

**✅ blocked/floating 已落地**（2026-08-14）：发现这几个字段**其实 `/api/vendors` 已在拉**
（`VendorRegion.Buyable/Blocked/Floating`）· 老代码 `pushVendorsToZone` 只落价格把它们丢了 ·
不用新端点。migration 034 `xi8_vendor_flags`（最新快照 · 每 vendor+zone 一行）· xi8
backfiller 5min 落 · `stockwatch.BlockGuard` fire 前查 blocked 就跳过（turbo 绕过 · 急停优先 ·
**fail-open**：查不到/数据旧不拦）。⏳ xi8 webhook（抢号提速那路）仍未接。
| kiro91 usage×3 / kiroceo `keys/usage` / kirooo `keys/created-at` / kiroappio `keys/created-at` | 号寿命/用量 → status quality 更准 | 内部 |

**注**：xi8 `/stock` + webhook 是**抢号链最后两块拼图** —— 现在抢号靠 60s 探针 + vendor
webhook · xi8 这两个补掉盲区（`16-buy-race.md` 的多路信号里 xi8 那两路还没接）。

---

## 4 · 明确不接（阶段外 · 不算欠账）

别再在盘点里把这些算成"没接完"：

| 类别 | 端点 | 为什么不接 |
|---|---|---|
| **母号供应侧** | kiro91 `mothers×8` · kiroappio `accounts×5` · kiroappcc `my-mothers/earnings/settlements/payout-qr/txns(收益向)` | 阶段 3+ 反向变现（`decisions` 未来方向）|
| **webhook 地址配置** | 六家 `PUT /webhook` · `/webhook/test` · `/rotate` · `/deliveries` | 部署前**手工在 vendor 后台配** · 不走 API（各家档 §已注明）|
| **账号设置** | `password` · `settings` · `api-key/rotate` · `2fa` · `bind-account` | 跟数据无关 · 一次性运维 |
| **充值** | kirooo `recharge×4` | 我方手工充 · 不走 API |
| **通知偏好** | kirooo `notify/prefs×4` | 我方 webhook 程序化处理 · 不用 vendor 的 TG 通知 |

---

## 5 · 补接优先级（施工顺序）

按"防钱错 → 抢号打满 → 前端转真 → 增强"排：

1. ✅ **维度 A① 对账链**（5 家断链 · 全内部）—— 已落地（Reconciler + CLI + kiro91 ledger）
2. ✅ **xi8 blocked/floating guard**（维度 A③ · 抢号"blocked 别 fire"）—— 已落地 ·
   ⏳ xi8 webhook（抢号提速）仍未接
3. **kirodrop `reservation`**（维度 A② · 前端定价从"估"转"真" · 填 `vendor_price_tier`）← 下一个
4. **kiro91 `stock/rounds` + kirooo `key-price-tiers`**（维度 A③ · Prices 页逐车次真价）
5. **号明细/寿命一批**（维度 A②③ · 喂 quality）—— dispatch-log / keys/export / usage / created-at
6. **其余 5 家 ledger adapter**（维度 A① · 拿真实响应核字段后接）

---

## 6 · 之前几份 vendor 文档的问题（本次核出 · 需修）

盘点时发现 16/17/18/19 四份都在讲 vendor · 有实打实的硬伤：

| 文档 | 问题 | 处理 |
|---|---|---|
| **17-vendor-work-order** | ① 悬空引用 `docs/vendors/_endpoints-audit-2026-08-12-v2.md`（文件不存在）② 整张"状态一览"钉在 2026-08-12 · 把抢号链/xi8 后台/洗数据/自适应频次全标 ❌未上线 · 但**这些已全部部署**（状态列腐烂 · CLAUDE.md §10.4）③ §B4 整节跟 16 号重复 · §A 跟 19 号重复（§4.3 一份文档一件事）| **建议废弃**（内容拆给 16/19/本文）· 待用户拍板 |
| **19-fields §0** | 覆盖率表数字过时：说 kiroceo `8/8 100%` · 但 `keys/usage`+`webhook` 两个官方端点没接 · 实为 6/8 | ✅ 本次已修（改为实测数 + 指向本文）|
| **16 / 18** | 内容本身准 · 但都带"待讨论/未定稿"残留 · 与已定稿部分混在一起 | 低优先 · 后续清 |

**防再犯**：端点连接状态**只以代码为准** · 任何文档都不设"已接/未接"状态列。
本文只分类 + 排序 · 不追踪状态。
