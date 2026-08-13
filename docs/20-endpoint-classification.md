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

## 0.5 · API 面 vs SPA 面 · 6 家逐个测通（2026-08-14 · 全实测 · 别再问"接没接"）

**两种 vendor**：
- **API 面 = SPA 面**（4 家）：`kiro91 / kirooo / kiroceo / kiroappio` —— SPA 登录**就是发 api_key**
  （实测 kirooo 登录返 `{api_key,...}`）· 网页和我方用**同一套** `/api/my/*`（或 `/api/me/*`）·
  api_key 直达全部数据 · **无独立 SPA 端点**。已测：stock / ledger(credits) / pricing / dispatch / keys 全 200。
- **API 面 ≠ SPA 面**（2 家）：api_key 面很窄 · 富数据在 SPA session 后：
  - `kiroappcc`：API `/openapi/*`（stock/balance/orders/claim ✅）+ SPA `/api/user/*`（txns/orders/me ✅ ·
    登录**无验证码** · adapter 自动登录）
  - `kirodrop`：API `/api/my/profile`+`/api/me/stock` ✅ + SPA `/api/v1/*`（dashboard/reservation ✅ ·
    登录**带图形验证码** · 要 seed token）

**测通矩阵**（2026-08-14 · 6 家**逐个登录实测** · 含各家登录机制）：

| vendor | 登录机制（实测）| API 面 | SPA 面 | 结论 |
|---|---|---|---|
| kiro91 | 账密 `dalio` → `km_session` cookie（7d·HttpOnly）| `/api/my/*` 全 200 | 同 API（session 只多 rotate）| ✅ 登录+数据通 |
| kirooo | 账密 → 返 **api_key**（SPA 就用它）| `/api/my/*` 全 200 | = API | ✅ 登录+数据通 |
| kiroceo | **单 key**（API=登录同一把 usr-…）| `/api/my/*` + `/api/me/*` 全 200（含 ledger/quotes）| = API | ✅ 登录+数据通 |
| kiroappio | 账密 · 登录页**无验证码** | `/api/me/*` 全 200 | = API | ✅ 登录+数据通 |
| kiroappcc | 账密 → token（**无验证码**·adapter 自动登录）| `/openapi/*` 全 200 | `/api/user/*` 全 200 | ✅ 两面都通 |
| kirodrop | 账密 → token（**带图形验证码**）| profile+stock 200 | `/api/v1/*` 200（dashboard/reservation）| ✅ 两面都通（要 seed token）|

**一句话**：6 家**全部登录成功 + 数据面全 200**。4 家自助平台（kiro91/kirooo/kiroceo/kiroappio）
登录只是拿 api_key/cookie · SPA 和 API 是同一套 `/api/my/*`（数据无遗漏）· 2 家（kiroappcc/kirodrop）
有独立 SPA 数据面也都测通。**唯一要人工续的是 kirodrop**（验证码登录·token 过期）· 其余全自动。

**登录凭证口径**（seed）：kiro91=账密（用户名 `dalio` 非 `danlio`）· kiroceo=单 key（API/登录同一把）·
其余 4 家=账密。别再纠结"少给了谁"——6 家登录机制已全部实测确认。

---

## 0.6 · 6 家 SPA 端点**实测清单**（2026-08-14 · 浏览器逐页登录 · `performance` 抓真实调用）

> 用途：这是每家 SPA **实际调的全部 `/api/` 端点**（浏览器登录后逐页点开抓的 · 不是文档猜）。
> 对照各家 adapter 的实际调用即知"我方接了 SPA 面的哪些"。**API key 可直达的都 200 实测过**。

| vendor | 站点 | SPA 实测调用的端点（全量）|
|---|---|---|
| kiro91 | api.91kiro.com | `/api/login` · `/api/my/{profile,keys,orders,rounds,ledger,mothers,gen-logs,reservation,usage,webhook,webhook/deliveries}` · `/api/my/stock/rounds` · `/api/docs` · `/api/admin/subaccounts/pending` |
| kirooo | kiro.ooo | `/api/user/login` · `/api/my/{profile,stock,stock/personal-pool,stock/regions,auto-fleet,purchase-orders}` · `/api/status` |
| kiroceo | kiro.ceo | `/api/login` · `/api/me/{overview,keys,orders,ledger,quotes}` · `/api/my/gen-logs` · `/api/public/config` |
| kiroappio | kiroapp.io | `/api/status` · `/api/me/{profile,stock,orders,keys,ledger}`（网页表单登录带腾讯 Turing 滑块验证码 · **但 SPA 调的这些 `/api/me/*` 用 api_key 直连全 200** · 验证码只挡人机登录 · 不挡 api_key 面）|
| kiroappcc | kiroapp.cc | API `/openapi/{stock,balance,orders,claim}` + SPA `/api/user/{login,txns,orders,me}` |
| kirodrop | drop.kiro.ss | api_key 面仅 `/api/status` · `/api/me/stock` · `/api/my/profile` · `/api/my/purchase` · `/api/my/redeem` · `/api/v1/reservation`；富数据（orders/ledger/dashboard）全在 SPA `/api/v1/*` session 后（api_key 打 404）|

**结论**：4 家自助平台（kiro91/kirooo/kiroceo/kiroappio）SPA = api_key 面同一套 · **无隐藏数据端点**；
kiroappcc SPA 面（`/api/user/*`）已接（自动登录）；kirodrop 富数据 session-gated（验证码 · 见 §1）。

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

**对账源全摸清（2026-08-14 · vendor-probe 逐家实测 · 不是文档猜）**：

| vendor | 对账源（实测）| 状态 |
|---|---|---|
| kiro91 | `GET /api/my/ledger` → `{entries,total}` | ✅ 已接（外层实测 · items 空 · 内层推断+存 raw）|
| kirooo | `GET /api/my/credits` → `{credits,ledger:[{id,kind,amount,ref_id,created_at}]}` | ✅ 已接 · **真实数据实测**（claim_key/recharge · 北京墙钟）|
| kiroappio | `GET /api/me/ledger` → `{items,page,summary,total}` 分页 | ✅ 已接（外层实测 · items 空 · 内层推断+存 raw）|
| kiroceo | `GET /api/me/ledger` → `{items,page,pages,total}` 分页（同 kiroappio 壳）| ✅ 已接（外层实测 · items 空 · 内层推断+存 raw）· 另有 purchase-orders 做 count-recon |
| kirodrop | `GET /api/v1/dashboard` → `{orders:[], wallet:{total_spent,total_recharged,...}}`（实测真端点 · SPA 用的就是它）| ⏳ **源存在但 session-gated**（要 `kiro_session_token` · 登录带图形验证码 · 不能自动重登）· 当前 orders 空（0 购买）· 要接需 seed 会过期的 token（同 reservation）|
| kiroappcc | `GET /api/user/txns`（login-session · 无验证码）| ✅ 已接 · **真实数据实测**（41 笔 · claim=purchase · delta 带符号）· adapter 自动登录换 token（api key 只管 /openapi/* · 两套独立鉴权）|

**四家四个外层形状**（kiro91 `{entries,total}` · kirooo `{credits,ledger}` · kiroappio
`{items,summary,total}` · kiroappcc bare array `[{id,delta,reason,refId,balanceAfter}]`）
—— 照一家套另一家必错 · 这就是"逐家实测别猜"的硬证据。

**kiroappcc 特殊**（2026-08-14 纠正）：`/api/user/*` 不认 API key（API key 只管
`/openapi/*`）· 要网页 session token · 但**登录无验证码**（`POST /api/user/login`
账密直接换 token）· 所以 adapter 内置自动登录+token 缓存+401 重登 · **可自动化**。

**6 家都有对账源**（2026-08-14 逐家实测确认 · 别再说 5 家）：

| vendor | 源 | 自动化 |
|---|---|---|
| kiro91 | /api/my/ledger | ✅ API key |
| kirooo | /api/my/credits | ✅ API key |
| kiroappio | /api/me/ledger | ✅ API key |
| kiroceo | /api/me/ledger（+ purchase-orders 做 count-recon）| ✅ API key |
| kiroappcc | /api/user/txns | ✅ 自动登录（无验证码）|
| kirodrop | /api/v1/dashboard（orders+wallet）| ⚠️ session-gated（登录带验证码 · 要 seed token · 会过期）|

**分档同理**：kirodrop 的 reservation 与 dashboard 都在 `/api/v1/*` · 同一个 `kiro_session_token` ·
要么都通（seed 一个 token · 定期人工续）· 要么都靠 seed。其余 5 家 API key / 无码登录直达。

**落地进度**（2026-08-14）：
- ✅ 基础设施：`providers.LedgerLister` + `VendorLedgerEntry`（reason 归一 6 类）·
  migration 033 `vendor_ledger` 表 · `vendorview.LedgerStore` · Backfiller 已接（拉 ledger 落库）
- ✅ 对账器 `vendorview.Reconciler` + CLI `bus-pooling reconcile [天数]` —— **三层核对**：
  存在性 / 数量（用 `vendor_order` · 5 家现成）+ 金额 / 漏退（用 `vendor_ledger` · 接了的家）·
  差异分 4 类：`orphan_ours` / `count_mismatch` / `amount_mismatch` / `refund_missing`
- ✅ **5/6 家 ledger adapter 已接**：kiro91 `/api/my/ledger` · kirooo `/api/my/credits`（真实数据）·
  kiroappio `/api/me/ledger` · kiroappcc `/api/user/txns`（自动登录 · 真实 41 笔）· kiroceo `/api/me/ledger`
  —— 全**容错解析**（外层实测 + 内层字段各试多个 · 永远存 raw · 有真数据再收紧）
- ⏳ kirodrop ledger **拿不到**：orders/ledger/dashboard 全在 SPA session 后（API key 打全 404 · 2026-08-14 实测）·
  登录带图形验证码 · 不能自动重登 —— **vendor 限制 · 非我方缺口**（要接需人工 seed 会过期的 token）

**✅ 抓真实形状工具化 `bus-pooling vendor-probe <slug> <path>`**（2026-08-14）：
只读探测 · 抓 vendor 端点真实响应（脱敏）· **institutionalize "写 adapter 前先抓真形状"**
—— 这是本项目反复弄错形状的根治（文档只有字段猜测 · §19.3 那批"高价值未接"都没实测 JSON）。
**只读铁律**：只发 GET · 危险词（purchase/reservation/…）拒发。

**本轮抓到的真实形状**（本地 · keys 在 .dev.env）：
- 某家 `/api/my/ledger` → `{entries:[...],total}`（验证了已提交的 ledger adapter 外层对）
- 某家 `/api/my/stock/rounds` → `{rounds:[],incoming:[],total,warranty_minutes}`（当前空 · 内层待有数据再核）
- kirooo `/api/my/credits` → **真实 ledger 形状**：`{credits,ledger:[{id,kind,amount(带符号),balance_after,ref_id,note,created_at}]}` ·
  kind 实测 `claim_key`/`recharge` · created_at 北京墙钟（时区坑）· **已据此写 kirooo ledger adapter**
- kirooo `key-price-tiers` → **文档路径 `/my/` 错 · 真实是 `/api/my/key-price-tiers`** · 返 `{bands:[{lower,upper,price}],base,tiers}` ·
  文档路径会返 HTML 登录页（盲写必炸）· 真形状已备 · 待做 vendor_price_tier

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

**注**：kirodrop `reservation` **已实测抓到真形状**（2026-08-14 · 浏览器 session）：只认
`Authorization: Bearer <kiro_session_token>`（网页登录+验证码才有 · 会过期 · 不适合 backfiller）。
**关键收获**：`exchange_rate:"6.8"` 权威定案 —— **我方 6.8 正确 · xi8 7.07 错** · kirodrop 定价不用改。
`timed_pricing.schedule` 是 `vendor_price_tier` 数据源（时间降价）· 但要 session token · 当前不接
（当前价我方已用 stock×6.8 拿到且验证正确 · 时间降价是展示锦上添花）。详见 `docs/vendors/drop-kiro-ss.md §2.3`。

---

## 3 · 维度 A③ · 产品补充 · 场景倒逼

由我方功能驱动 · 不接功能就残：

| 端点 | 服务的产品场景 | 维度 B |
|---|---|---|
| kiro91 `GET /api/my/stock/rounds` | Prices 页逐车次现价（否则均值失真）| 脱敏上前端 |
| kirooo `GET /my/key-price-tiers` | 同上 · kirooo 定价阶梯 | 脱敏上前端 |

**⚠️ xi8 的角色（2026-08-14 用户拍板 · 纠正前一版）**：xi8 **只做内部对账 / 参考**
（看它怎么对齐上游数据）· **绝不介入采购**。采购一律**直接打 vendor** · 能不能买以 vendor
自己的响应为准。所以 xi8 的 buyable/blocked/floating **不接进抢号 fire 决策**（前一版
`stockwatch.BlockGuard` 已撤 —— 让内部参考源 veto 真实购买 = 把 xi8 塞进钱路 · misalign
时会拦本可成交的单）。

**✅ 已落地为对账数据**：migration 034 `xi8_vendor_flags`（最新快照 · 每 vendor+zone 一行）·
xi8 backfiller 5min 落 buyable/blocked/floating。用途是**对账 / 看 xi8 对齐上游的准确度**
（`FlagStore.IsBlocked` 是诊断查询 · 不接 fire）。⏳ xi8 webhook 不接（抢号靠直连 vendor）。
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
2. ✅ **xi8 buyable/blocked/floating 落对账表**（维度 A③ · `xi8_vendor_flags`）—— 已落地 ·
   **纯对账参考 · 不接采购**（用户拍板 · 前一版误接 fire 已撤）· xi8 webhook 不接
3. ~~**kirodrop `reservation`**~~ —— **实测拿不到**（401 · 网页 session + 验证码 · 见 §2 注）·
   转走 **kirooo `key-price-tiers`**（真形状已抓 · API key 可达）填 `vendor_price_tier`
4. ✅ **kirooo `key-price-tiers` → 数量分档已落地**（阶梯价格）· migration 035 扩
   `vendor_price_tier`（tier_kind='qty_band' + qty_lower/upper）· backfiller 拉 · 真形状
   `{bands:[{lower,upper,price}]}`（实测 · 当前 flat 全 100 · 结构真实一开分档就反映）·
   kirodrop 时间降价形状已抓（token-gated · 见 §2 注）· kiro91 `stock/rounds`（当前空）待有数据接
5. **号明细/寿命一批**（维度 A②③ · 喂 quality）—— dispatch-log / keys/export / usage / created-at
6. ✅ **ledger adapter 5/6 已接**（维度 A①）· kiro91/kirooo/kiroappio/kiroappcc/kiroceo 全接 ·
   仅 kirodrop 拿不到（SPA session + 图形验证码 · API key 打 404 · vendor 限制 · 非欠账）

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
