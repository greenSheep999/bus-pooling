# 1f 全流程走查 + 策略分层分析(2026-08-15 凌晨)

> **背景**：sprint-1f 分支 · 后端 `127.0.0.1:8090` DRY_RUN=1 · 前端 `localhost:3100` vite dev · 测试账号 `smoke@x.com` · 用户睡前留题:**"全局/车级策略两层字段一样 · auto refill 的 follow-global toggle 是不是设计错了"** · 本文只写结论 · 不改代码。
>
> **执行**：Playwright MCP 全流程走 26+ 页面 · 抽 32 张截图存 `.playwright-mcp/` · 已过 `.gitignore` · 不入库。

---

## 结论先行 · 三句话

1. **走查 26 页 · 0 挂 · 3 处 UX 打磨点 · 无 JS 报错、无 404、无白屏。**
2. **用户对"全局 auto"的直觉是对的** —— `Effective()` 里 `AutoRefillEnabled / RefillWatermark / RefillMinCount` 三字段的 `车级→全局→系统默认` 三层链把两层字段"复制粘贴"了 · **不是分层 · 是并层**。全局的 `default_auto_refill_enabled` 只在"该车字段是 NULL" 时才生效 —— 语义上是 seed + fallback 混用 · **不是 "全局辅助 / 车级精准"** 的关系。
3. **建议撤回 1f-B/C 里 3 处过度设计** · **保留 1f-C `Effective()` 骨架** · **新加 3-5 个真正的"全局调度层"字段**(如钱包预算 · 全局急停 · vendor 白名单) —— 让全局层承载**跨车调度**的东西 · 车级只保留 auto 开关 + 水位 + 每轮 count 三个"当车行为"参数。

---

## Part 1 · 26 页面走查结果

### ✅ 全通(23/26)

| # | 页面 | 备注 |
|---|---|---|
| 1 | `/` Landing | 视觉 + 交互 OK · vendor 卡片轮播 · CTA 到 register / docs · **只有** 1 处 401 err(未登录时 `/api/me` 探测 · 是**预期行为** · 不算挂) |
| 2 | `/register` | 邮箱 · 用户名 · 密码 · 邀请码(可选)· 表单结构对 |
| 3 | `/login` | 邮箱或用户名 · 密码 · Remember 30 天 · 忘密提示"Coming in phase 3+ · contact admin"(阶段 1 无邮件 · 对) |
| 4 | `/overview` | Header stats(0 credits · 0 available)· 三张业务卡(Pooling / Extract / Wallet)· Activity 空态 · Beta 特惠横幅倒计时 |
| 5 | `/buses` 列表 | KPI 行 · 空态 · "Start pooling" 按钮 · 后端建车 200 OK |
| 6 | 建车弹窗(3 种入口) | Start a bus / Hop on / Join by code 三卡分流 |
| 7 | 建车表单(single) | 名字 · count · vendor · **auto refill toggle** · Advanced 折叠区 |
| 8 | 车详情(6 tabs) | Keys / Pull history / Push log / Members / Refill strategy / Stats · 建车即跳详情 |
| 9 | Refill strategy tab | 5 字段 · 每个覆盖字段一个 "Follow global / Override for this bus" 二态 · max_unit_price 无 toggle 直显 "Effective ... min(bus, global)" |
| 10 | Pull now 弹窗 | count + vendor 两字段 · Cancel + Fetch |
| 11 | Bus settings 弹窗 | Pool name 重命名 · Danger zone (Dissolve)· 语义完整 |
| 12 | `/extract` | Pending / Extract history 两 tab · 三个派去向按钮(Add to bus / Push to my pool / Download & take)· "Push to my pool 需先去设置配 URL/token" 提示对 |
| 13 | `/dispatch` | 另一个跟 extract 并存的路由(见 §Part 1.warn.1) |
| 14 | `/status` | 6 家 vendor 卡片 · 匿名 label(`Vendor 01`..`Vendor 06`)· 质量标签(High volume / Stable / Warranty)· 库存全 0 是 DRY_RUN 数据·  正常 |
| 15 | `/status/f274c3` 详情 | 单 vendor · 页面路由通 · 页面 title 变成 "Upstream status · Kiro.bus" (**只有它变了 title**) |
| 16 | `/prices` | Price trends chart · Round history · "Cheapest now: Vendor 01" |
| 17 | `/wallet` | Balance / Ledger 空态 / Top up 卡 (5% channel fee) / Redeem code 卡 |
| 18 | 充值卡 · 无实际付 | UI 就位 · "Create top-up order" 按钮 · Stage 1 上线才切真 gateway(见 `1f-morning-brief` Stage 1) |
| 19 | 兑换码卡 | Input + Redeem 按钮(空态 disabled)· 后端存在 |
| 20 | `/me` | 邮箱 · 改密码 · 登出 |
| 21 | `/settings` | 4 卡索引(Pull preferences / My pool / Bot notifications / API keys)|
| 22 | `/settings/preferences` | **三个板块**：Pull limits(硬上限) + New bus defaults(seed) + Auto refill defaults(auto/watermark/min-count 三字段) |
| 23 | `/settings/downstream` | passengerpool 配置页 |
| 24 | `/settings/webhook` | 机器人通知配置 |
| 25 | `/settings/api-keys` | 已存在 |
| 26 | `/community` · `/invite` · `/docs` (Start/Pull/Assign/Matrix/Fields/Webhook/Errors 7 tabs) | 3 页面都通 · `/docs` 的 Matrix + Fields 是 1f-E 新加 · 结构完整 |

### ⚠️ 警告(3 项 · UX 打磨 · 不影响功能)

| 位置 | 现象 | 影响 | 修法 |
|---|---|---|---|
| 侧栏导航 | 路由 `/extract` 和 `/dispatch` **并存** · Header 主导航链的是 "My Dispatch" 指向 `/dispatch` · footer 里同时列 "Extract key" 指向 `/extract` | 用户可能怀疑两个是不是不同页面 | 收敛到 `/dispatch` 一条路由 · 保留 `/extract` 做 301 redirect · 或改导航文案统一 |
| 车详情 tabs 数量 | 用户睡前描述"5 tabs" · 实际 **6 tabs**(多了 "Push log" 跟 "Stats") | 无实际问题 · 只是描述过时 | 走查者已修正 · 无需改代码 |
| Bus settings 弹窗 | 标题叫 "Pool settings"(Pool name / Dissolve pool)· 主 UI 用词是 "Bus" · Bus 和 Pool 混用 | 术语双分离要求(`CLAUDE.md §12.6`)禁的是内部术语 · Bus/Pool 都是对外术语 · 但同一 UI 里两个称呼扰乱 | 全 UI 统一 "Bus"(主用词)· "Pool" 是 vendor-facing 术语 · 混用容易让新用户误以为它俩不同 |

### ❌ 挂的(0 项)

**JS console 0 error**(除首页 `/api/me` 401 · 预期行为)· 无 404 · 无白屏。

---

## Part 2 · 全局/车级策略分层分析

### 现状回顾 · 一图对齐(证据在代码里)

**证据文件**：
- `internal/db/migrations/039_strategy_nullable_and_globals.sql:28-30` · psd 表新增 `default_auto_refill_enabled` / `default_refill_watermark` / `default_refill_min_count`
- `internal/strategy/effective.go:227-247` · `AutoRefillEnabled` / `RefillWatermark` / `RefillMinCount` 三字段的 `车级→全局` fallback 链
- `web/src/components/EditStrategyPanel.tsx:41-70` · 5 个 "Follow global / Override for this bus" 二态 toggle
- `web/src/pages/Preferences.tsx` · Preferences 页有 "Auto refill defaults" 板块(见 `.playwright-mcp/11-preferences.md` 第 f5e237-e258 行)

**两层现有字段并列表**(1f-B/C 落码后 · 从 `docs/15-scheduling.md §4.1 + §4.2` 抠出来):

| 字段 | 车级(`bus.Strategy`) | 全局(`passenger_strategy_default`) |
|---|---|---|
| MaxUnitPrice | `*int64` · 车级更严即取 | `*int64` · 硬上限 |
| DailyRoundLimit | ⚠️ deprecated(§4.1)· 车级不参与 | `*int` · 硬上限跨车累加 |
| DailySpendLimit | ⚠️ deprecated · 车级不参与 | `*int64` · 硬上限跨车累加 |
| PerRoundCount | `*int` · nullable 覆盖 | `int` · seed + fallback |
| PreferredVendor | `*string` · nullable 覆盖 | `*string` · seed + fallback |
| Zone | `*string` · anon 专属(anon_zone) | `DefaultZone` · seed |
| **AutoRefillEnabled** | `*bool` · **nullable 覆盖(1f-B 新加)** | **`DefaultAutoRefillEnabled bool` · seed + fallback(1f-B 新加)** |
| **RefillWatermark** | `*int` · **nullable 覆盖(1f-B 新加)** | **`DefaultRefillWatermark int` · seed + fallback(1f-B 新加)** |
| **RefillMinCount** | `*int` · nullable 覆盖 | **`DefaultRefillMinCount *int` · seed + fallback(1f-B 新加)** |

**关键观察**：最后 3 行(auto/watermark/min-count)**两层字段名互为镜像** —— 全局叫 `default_*` · 车级叫本名 · 语义完全一样 · 只是层的位置不同。**这就是用户直觉说的"一样"**。

### Q1 · 两层分别管什么(重新对表 · 拍板建议)

**用户直觉**：全局层应该做"调度总开关 / 辅助 / 系统级预算" · 而不是"每车拉号行为的镜像 seed"。

**按语义重排** · 每个字段应该在哪一层:

| 字段 | 应该只在**全局** | 应该只在**车级** | 两层都要 · 但语义不同 |
|---|---|---|---|
| 硬上限 · MaxUnitPrice | ✅ 全局硬上限 | ✅ 车级更严护栏 | ✅ **min(车级, 全局)** —— 已对 |
| 硬上限 · DailyRoundLimit | ✅ 只全局 · 跨车累加 | ❌ | 已对 |
| 硬上限 · DailySpendLimit | ✅ 只全局 · 跨车累加 | ❌ | 已对 |
| PerRoundCount | seed(新车预填) | 车行为 · 每车不同 | **建车抄一份 · 独立演化** —— 不需要"跟随全局" toggle |
| PreferredVendor | seed | ✅ 每车固定 vendor 是核心诉求(单区独享 · 多车抢不同家等) | seed + 车级独立(不 fallback) |
| Zone | seed(新车预填) | ✅ 每车可锁区 | seed + 车级独立(anon 除外) |
| **AutoRefillEnabled** | ❌ **不该有** —— 全局层跟自动补车是**两件事** | ✅ **只在车级** —— 每车独立开关 | ❌ **别镜像** |
| **RefillWatermark** | seed(新车默认值) · 建议保留 | ✅ 车级 · 每车水位不同 | seed 独立(不 fallback) |
| **RefillMinCount** | seed | ✅ 车级 | seed 独立(不 fallback) |

**核心变化**：`AutoRefillEnabled` **从"两层镜像"改成"只在车级"** · 全局层不出现这个字段。原因见 Q2/Q3。

### Q2 · "全局自动"到底该是什么

**用户原话**："全局里应该会多一些调度开关 · 是否启用或者去辅助车的一些选项"。

**回答**：**全局层不做 "auto refill 的镜像开关"** · 应该承载**跨车调度的系统级配置**。以下是 5-8 个候选"全局层"字段 · 每个说明干啥 + 为什么车级放不下:

**候选 1 · `global_auto_pause` (bool · 全局自动补车总开关)**
- **干啥**：一键关闭乘客名下**所有**车的自动补车行为 · 类似"我账户下所有自动化暂停"
- **为什么必须全局**：跨车级的"总闸门" —— 车级只管 "这辆车有没有开 auto" · 全局管 "全部车的 auto 会不会被跳过"。生产场景:凌晨钱包快空了但不想手动关十几辆车 · 一个开关全停
- **跟车级 `auto_refill_enabled` 的关系**:AND 关系(两者都 true 才补) · 全局 pause=true 时 · 车级 auto=true 也不补(等类似 `KILL_PULLS` 文件哨兵的"用户版")

**候选 2 · `auto_refill_daily_budget` (int64 · 自动补车专用日预算 · microunit)**
- **干啥**:自动触发的补车(webhook/probe/deathwatch/scheduler)每天最多花多少 · 手动拉号不算
- **为什么必须全局**:预算是"跨车" · 车级设 "我车花 X" 已经有 max_unit_price · 但 "自动化今天总共花多少" 是**账户级风控**
- **跟车级的关系**:decider Step 6 累加所有车的**自动触发**支出 · 到顶后所有车的 auto 全部 reject · 手动仍可拉

**候选 3 · `auto_refill_vendor_allowlist` ([]string · 自动补车允许的 vendor)**
- **干啥**:自动触发只从这些 vendor 里选 · 手动拉号无视此表
- **为什么必须全局**:vendor 白名单是"账户级偏好" · 比如乘客只信任 kiro91 + kiroceo 两家 · 不希望自动化跑去 vendor 04
- **跟车级的关系**:车级 `PreferredVendor` 只是"首选" · 白名单是"允许" · 有交集才 fire · 车级设的 vendor 若不在全局白名单 · 自动触发跳过该车

**候选 4 · `auto_refill_time_window` (string · 自动补车允许的时间窗 · e.g. "00:00-06:00")**
- **干啥**:只在深夜自动补(号老退款率高 / 便宜时段)· 白天只手动
- **为什么必须全局**:时区是账户级 · 车级设这个太累 · 全局一次
- **跟车级的关系**:窗口外全部 auto 拒 · 手动无视

**候选 5 · `auto_refill_min_wallet_reserve` (int64 · 钱包最低保留积分)**
- **干啥**:钱包低于此值 · 停一切 auto · 只手动 · 防"自动化把钱包烧空"
- **为什么必须全局**:钱包是账户级
- **跟车级的关系**:决策器 Step 6 判 `wallet.balance < min_reserve → 拒 auto`

**候选 6 · `stock_watch_max_concurrent` (int · 挂单并发数)**
- **干啥**:同时挂 stockwatch 的最多几个(避免一辆车挂 5 单 · 另一辆车挂号时被排后)
- **为什么必须全局**:公平性 · 全局资源
- **跟车级的关系**:全局硬上限 · 到顶后新的 fire 走 `stockwatch_full` 拒

**候选 7 · `webhook_notify_channel` (string · alert / info / off · 报警级别 seed)**
- **干啥**:自动补车"成功/失败/水位低"的通知级别默认
- **为什么必须全局**:通知是账户级
- **跟车级的关系**:seed 到 `settings/webhook` 事件订阅

**候选 8 · `refund_auto_replay_hours` (int · 死号退款后多久内自动补 · seed)**
- **干啥**:号在 X 小时前拉的 · 死了自动补 · 更老的只退款不补(避免自动化对老号做无意义补车)
- **为什么必须全局**:是策略偏好 · 车级不细管
- **跟车级的关系**:seed 到车级 · 车级可以覆盖(这是**seed 独立演化**的字段 · 不 fallback)

**这 8 个的共同特征**:都是**"辅助 / 护栏 / 调度总开关"** · 车级放不下 · 或放了也是重复劳动。**没有一个是"车级行为的镜像"**。

### Q3 · 车级 `auto/refill` 三字段的语义

**当前实现**(sprint-1f-B/C 落码后):
- `auto_refill_enabled` · **是否自动补车** · 每车独立 · 一辆车开另一辆车关很正常
- `refill_watermark` · **水位线** · 每车不同 · 车用途不同(独享 vs 拼车)水位大不同
- `refill_min_count` · **每轮最少拉几个** · 每车不同 · 想稳可以设 5 · 想省钱设 1

**这三个都是纯每车行为参数** · 不需要全局 fallback · 不需要 nullable · 用户直觉完全正确。

**当前 1f-B 落 nullable 的两个问题**:

1. **过度设计** —— 让用户在 UI 上多点一次 "Override for this bus" · 才能改车级的 auto refill 开关 · 完全没必要。这个开关就是"这辆车要不要自动补" · 应该跟建车向导里的那个 toggle 一样直接改 · 不是 "跟随 seed" 的东西
2. **误引导** —— UI 上有 "Follow global" 按钮 · 用户以为全局层有个"权威" 值 · 实际上全局的 `default_auto_refill_enabled` 就是个建车 seed · 不是活的调度指令 · 名字叫 "default_" 是 seed 语义 · 不是 "current" —— 但前端 toggle 说 "Follow global (value=on)" · 暗示全局是"当前生效值" · 语义错位

### Q4 · 新车 seed 从哪来

**用户建车向导预填哪些字段?**(见 `.playwright-mcp/07-start-bus-form.md` + `08-create-bus-advanced.md`):

- **Bus name** —— hardcoded "My bus" · 无 seed 概念
- **How many keys (count)** —— seed 到全局 `per_round_count`
- **Vendor** —— seed 到全局 `preferred_vendor`
- **Auto refill toggle** —— 建议 seed 到**新独立字段** `settings.preferences.new_bus_auto_refill_default: bool`(**不叫 `default_auto_refill_enabled`** —— 强调 seed 语义 · 不是 fallback)
- **Advanced · Max unit price** —— seed 到全局 `max_unit_price`
- **Advanced · Daily round limit** ⚠️ — **建车向导不该出现这个** —— `bus.daily_round_limit` 是 deprecated 字段(`15 §4.1`)· UI 应该拿掉此输入 · 或明确标"全局字段 · 建车时不能改"

**seed 命名规范建议**:
- 全局 seed 字段一律 `default_*` 前缀
- **明确不 fallback** —— 建车时 copy 一份到 `bus.Strategy` · 之后**独立演化** · 全局改了不影响老车
- UI 上标 "Prefill for new buses only · existing buses keep their own values"(现有 Preferences 页第 7 行已经这么写 · 保留)

### Q5 · 1f-A/B/C 该撤回哪些

**保留**(方向对):
- ✅ `docs/15-scheduling.md §4.3` 策略优先级铁律 · 硬上限 min 链 · 覆盖字段后者盖前者 —— 这是对的 · 别撤
- ✅ `internal/strategy/effective.go` `Effective()` 单入口函数骨架 —— 收口是必要的 · 就是内部规则要改
- ✅ `MaxUnitPrice` `stricter3(全局, 车级, request)` 硬上限 min 链
- ✅ `PreferredVendor` / `Zone` / `PerRoundCount` 三字段的 request→车级→全局→系统默认覆盖链
- ✅ 类①硬上限 vs 类②覆盖 两分类
- ✅ 1f-E `/docs` 页 Matrix + Fields tab · 跟策略层无关 · 已经落好

**撤回**(过度设计 · 且跟用户直觉冲突):
1. ❌ `bus.auto_refill_enabled` / `bus.refill_watermark` 三字段的 **nullable** 语义 —— 直接 `NOT NULL DEFAULT 0` · 用户建车即定 · 想改直接改 · 没有"跟随全局"这一态
2. ❌ `passenger_strategy_default.default_auto_refill_enabled` / `default_refill_watermark` **fallback 语义** —— 这仨字段只作为**建车向导 seed** · 运行时 `Effective()` **不读全局** · 不做 fallback
3. ❌ `EditStrategyPanel.tsx` 车级 auto/watermark/min-count 三字段的 "Follow global / Override for this bus" toggle · 直接输入值即可
4. ❌ `docs/15-scheduling.md §4.3.2b + §4.3.2c` 关于 auto/refill 的 "方案 A nullable 继承" 讨论 —— 明确写"这三字段**只在车级** · 全局的 `default_*` **只是建车 seed 不是运行时 fallback**"
5. ❌ `docs/decisions.md §13.5` 说 auto/refill 是 "1f-B 目标"的段落 —— 改成"决议不做 · 保持每车独立"
6. ❌ `internal/strategy/effective.go:227-247` 三行 fallback 链(`global.DefaultAutoRefillEnabled` / `global.DefaultRefillWatermark` / `global.DefaultRefillMinCount` 的三行)—— 改成"只读车级 · 全局 default_* 字段供建车 API 抄一份 seed"

**新加**(填补真"全局层"的空白):
- ✅ 从 Q2 的 8 个候选里挑 3-5 个真"全局调度层"字段 · 落 migration 041(041 是下一号):
  - 强推荐 · 简单落:`auto_refill_daily_budget`(候选 2)· `auto_refill_min_wallet_reserve`(候选 5)· `auto_refill_vendor_allowlist`(候选 3)
  - 可选:`global_auto_pause`(候选 1 · 是 UX 增强)· `refund_auto_replay_hours`(候选 8 · 是 seed)
- ✅ Preferences 页把"Auto refill defaults"板块**换掉** —— 从当前的 3 字段镜像 · 改成 3-5 个真调度字段 · 板块名建议改成 **"Auto refill guardrails"** 或 **"Scheduling controls"**
- ✅ Preferences 页保留一个板块 **"New bus defaults · auto refill"** —— 只包含 seed 字段(建车向导的默认值)· 明确标 "Prefill for new buses only"

---

## Part 3 · 综合建议

### 优先修 3-5 项

| 挂点 | 优先级 | 修法 |
|---|---|---|
| 全局 `default_auto_refill_enabled` 语义混用(seed + fallback 两用) | **P0** | 拍板"只做 seed · 不做 fallback"· 撤回 `Effective()` 里三行 fallback · 撤回 UI toggle · 见 Q5 撤回清单 |
| `EditStrategyPanel.tsx` 三个"Follow global"按钮误导 | **P0** | 三个 toggle 拿掉 · 直接输入车级值 |
| 建车向导 Advanced 里有 `daily_round_limit` / `daily_spend_limit` 输入 | **P1** | 两字段是全局 deprecated 车级 · 从建车表单拿掉 · 或改成 "只读 · 指向 `/settings/preferences`" |
| `/extract` vs `/dispatch` 路由并存 | **P2** | 定一个主路由 · 另一个 301 redirect |
| Bus settings 弹窗术语"Pool" vs "Bus"混用 | **P2** | 全 UI 统一 "Bus" · 或按 CLAUDE §12.6 说清楚 |

### 策略分层重构(不落码 · 建议)

**migration 041(设想 · 不动手)**:

```sql
-- 041_scheduling_globals_and_bus_denullable.sql
--
-- 撤回 1f-B nullable 三字段 · 保留 seed · 加真调度字段
--
-- **保行为**:老车 auto/watermark/min-count 的 NULL 值转 0(即"不自动补")· 
-- 因为 seed 不是运行时 · 老车用户没显式设时应默认为"不自动" · 而不是"跟随全局的 1" ·
-- 这符合 1f-B migration 铁律的精神(全局变化不影响老车)

-- Step 1 · bus 表三字段回 NOT NULL DEFAULT 0
CREATE TABLE bus_new (
  ...
  auto_refill_enabled INTEGER NOT NULL DEFAULT 0,
  refill_watermark    INTEGER NOT NULL DEFAULT 0,
  refill_min_count    INTEGER,  -- 保留 nullable · nil = gap 语义
  ...
);
INSERT INTO bus_new SELECT ..., COALESCE(auto_refill_enabled, 0), COALESCE(refill_watermark, 0), refill_min_count, ...;
DROP TABLE bus;
ALTER TABLE bus_new RENAME TO bus;

-- Step 2 · 全局 default_* 三字段保留(只作 seed · 不做 fallback)· 不动 SQL

-- Step 3 · 加新全局调度字段(从 Q2 候选里挑 3 个强推荐)
ALTER TABLE passenger_strategy_default ADD COLUMN auto_refill_daily_budget INTEGER;         -- microunit
ALTER TABLE passenger_strategy_default ADD COLUMN auto_refill_min_wallet_reserve INTEGER;   -- microunit
ALTER TABLE passenger_strategy_default ADD COLUMN auto_refill_vendor_allowlist TEXT;        -- CSV · 空 = 全允许
```

**`Effective()` 改法**:
- 三行 `AutoRefillEnabled / RefillWatermark / RefillMinCount` 的 fallback 改成**只读车级**(拿掉 `global.Default*` 那三行)
- 加三个新字段 `AutoRefillDailyBudget` / `AutoRefillMinWalletReserve` / `AutoRefillVendorAllowlist` · 这些进 `EffectiveStrategy` · decider Step 6 判它们

**decider Step 6 加判据**(伪代码 · 15-scheduling §5.2 Step 6 里补):

```go
// 只在自动触发时判 · manual 跳过
if trigger != "manual" {
  if eff.AutoRefillDailyBudget > 0 && usage.AutoSpendToday + est > eff.AutoRefillDailyBudget {
    return Reject("auto_daily_budget_exceeded")
  }
  if eff.AutoRefillMinWalletReserve > 0 && wallet.Balance - est < eff.AutoRefillMinWalletReserve {
    return Reject("auto_wallet_reserve_hit")
  }
  if len(eff.AutoRefillVendorAllowlist) > 0 && !slices.Contains(allowlist, chosenVendor) {
    // 车级 preferred_vendor 不在白名单 · 跳该车
    return Reject("auto_vendor_not_allowed")
  }
}
```

### 文档要改的地方

**`docs/15-scheduling.md`**:
- **§4.1 车级配置表**:
  - `AutoRefillEnabled` 恢复 `bool` (非指针 · NOT NULL DEFAULT false)
  - `RefillWatermark` 恢复 `int` (非指针)
  - `RefillMinCount` 保留 `*int`(nil = gap)
- **§4.2 全局默认表**:
  - 保留 `default_auto_refill_enabled` / `default_refill_watermark` / `default_refill_min_count` 三字段 · 但**在 seed 段** · 标注"seed only · not runtime fallback"
  - 新增小节 **§4.2b 调度护栏字段** · 列 Q2 挑中的 3-5 个字段
- **§4.3.2 类②覆盖字段**:
  - `AutoRefillEnabled / RefillWatermark / RefillMinCount` 三行改成 **"车级 · 无全局 fallback · 建车时抄一份 default_* 独立演化"**
- **§4.3.2b nullable 继承讨论**:
  - 整节拿掉 · 或改成 **"1f-C 已撤回 · 保持每车独立"**
- **§4.3.2c RefillMinCount 三态语义**:
  - 简化成两态 · nil = gap · 非 nil = 显式 · 无全局 fallback
- **§4.3.5.2 EditStrategyPanel 二态切换 UI**:
  - 只对 `PerRoundCount / PreferredVendor / Zone` 三字段保留 toggle
  - auto/refill 三字段直接输入 · 无 toggle
- **§4.3.5.3 前端字段契约**:
  - `auto_refill_enabled` / `refill_watermark` 改回 `boolean` / `number`(非 null)
  - `refill_min_count` 保留 `| null`
- **新增 §4.4 调度护栏字段**(可选 · 若加候选 1-8):
  - 每个字段一段 · 干啥 · 决策器怎么用 · UI 位置

**`docs/decisions.md`**:
- **§13.5** 把 "auto/refill 三字段 1f-B 目标" 改成 "决议不做 fallback · 保持每车独立 · 1f-B nullable 已撤回"
- **加 §14** 新条 "全局调度层字段 · 从'车级镜像'改成'跨车护栏'· 决议见 15 §4.2b" · 引本文 Q2

**`web/src/types/index.ts`**:
- `BusStrategy.auto_refill_enabled` / `.refill_watermark` 改回 `boolean` / `number`(非 null)
- `GlobalStrategy` 保留 `default_*` 三字段 · 加新字段 `auto_refill_daily_budget?` / `auto_refill_min_wallet_reserve?` / `auto_refill_vendor_allowlist?`

**`web/src/components/EditStrategyPanel.tsx`**:
- 拿掉 `autoMode` / `watermarkMode` / `minCountMode` 三个 mode state
- 三字段直接编辑
- 保留 `perRoundMode` / `prefMode` 两个 toggle(这俩用户有实际的"跟随全局"诉求)

**`web/src/pages/Preferences.tsx`**:
- 拿掉 "Auto refill defaults" 板块(3 字段镜像)
- 保留 "New bus defaults" 板块 · 加 auto/watermark/min-count 三字段作 seed
- 新增 "Scheduling guardrails" 板块 · Q2 挑中的 3-5 字段

### `Effective()` 要不要重写

**答**:不重写 · **改 3 处**:

1. `AutoRefillEnabled` (effective.go:227-231) —— 拿掉 `global.DefaultAutoRefillEnabled` fallback · 只读车级(车级值改成非指针后直接读)
2. `RefillWatermark` (effective.go:233-237) —— 同上
3. `RefillMinCount` (effective.go:239-247) —— 全局 default 那行拿掉 · 只读车级

**新加 3 处**(若采纳 Q2 候选):
1. `eff.AutoRefillDailyBudget = global.AutoRefillDailyBudget` (只全局 · 无车级)
2. `eff.AutoRefillMinWalletReserve = global.AutoRefillMinWalletReserve`
3. `eff.AutoRefillVendorAllowlist = global.AutoRefillVendorAllowlist`

**测试**:
- `internal/strategy/effective_test.go` · 撤 auto/refill 三字段的 fallback 测试用例 · 加 "全局改 default_auto_refill_enabled · 老车不受影响" 保行为测试
- 加 3 个新调度字段的 decider Step 6 集成测试(1f-C `bus.Scheduler` 测试补 "全局 daily_budget 到顶 · 所有 auto 车 reject" 用例)

---

## 附录 · Playwright 截图索引(存 `.playwright-mcp/`)

| # | 文件 | 页面 |
|---|---|---|
| 1 | `1f-review-01-landing.png` | 首页 / Landing |
| 2 | `1f-review-02-register.png` | 注册页 |
| 3 | `1f-review-03-login.png` | 登录页 |
| 4 | `1f-review-04-overview.png` | 概览(未加载) |
| 5 | `1f-review-05-buses.png` | 拼车列表(空态) |
| 6 | `1f-review-06-create-bus.png` | 建车弹窗 · 3 卡分流 |
| 7 | `1f-review-07-start-bus-form.png` | 建车表单 · single |
| 8 | `1f-review-08-create-bus-advanced.png` | 建车 · Advanced 展开(有 daily_* 字段) |
| 9 | `1f-review-09-bus-detail.png` | 车详情 · 6 tabs |
| 10 | `1f-review-10-bus-refill-strategy.png` | Refill strategy tab(5 字段 · 三个 toggle 是本文靶心) |
| 11 | `1f-review-11-preferences.png` | 拉号偏好(三板块 · 含 Auto refill defaults) |
| 12 | `1f-review-12-overview-loaded.png` | 概览 · 加载后 |
| 13 | `1f-review-13-extract.png` | Extract page |
| 14 | `1f-review-14-status.png` | 上游状态 · 6 vendor |
| 15 | `1f-review-15-prices.png` | 价格走势 |
| 16 | `1f-review-16-wallet.png` | 钱包 |
| 17 | `1f-review-17-settings.png` | 设置索引 |
| 18 | `1f-review-18-downstream.png` | 我的号池 |
| 19 | `1f-review-19-webhook.png` | 机器人通知 |
| 20 | `1f-review-20-api-keys.png` | API keys |
| 21 | `1f-review-21-me.png` | 我的 |
| 22 | `1f-review-22-community.png` | 社群 |
| 23 | `1f-review-23-invite.png` | 邀请 |
| 24 | `1f-review-24-docs.png` | 对接文档 |
| 25 | `1f-review-25-docs-matrix.png` | 文档 · Matrix tab |
| 26 | `1f-review-26-docs-fields.png` | 文档 · Fields tab |
| 27 | `1f-review-27-pull-modal.png` | Pull now 弹窗 |
| 28 | `1f-review-28-dispatch.png` | Dispatch(重定向) |
| 29 | `1f-review-29-dispatch-loaded.png` | Dispatch 加载后 |
| 30 | `1f-review-30-status-detail.png` | 上游详情 |
| 31 | `1f-review-31-bus-settings.png` | 车设置弹窗(Danger zone) |
| 32 | `1f-review-32-extract.png` | Extract 加载后 |

**证据文件**(代码引用):
- `internal/strategy/effective.go:227-247` — 三行 auto/refill fallback(要撤)
- `internal/db/migrations/039_strategy_nullable_and_globals.sql:28-30` — 全局补三字段(seed 定位 · fallback 撤)
- `web/src/components/EditStrategyPanel.tsx:41-70` — 5 个 toggle · 三个要撤
- `web/src/pages/Preferences.tsx` "Auto refill defaults" 板块 — 要改成 scheduling guardrails
- `docs/15-scheduling.md §4.3.2b + §4.3.2c` — 继承语义讨论 · 要收敛
- `docs/decisions.md §13.5` — auto/refill "1f-B 目标" 描述 · 要改

---

**执笔者**:sprint-1f 走查 · 走完 26 页 · 花约 20 分钟(playwright 加载 + 截图) · 分析 25 分钟。**不改代码 · 不 commit** · 用户明早看。
