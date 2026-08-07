# Mockup 审计 · 逐字段/状态/文案对齐权威文档

> 权威文档：`00-values-and-phases.md` · `04-scenarios.md` · `06-db-schema.md` · `05-api-contract.md` · `decisions.md §8`
> 目标：每一处 UI 元素追溯到**schema 字段** + **状态机态** + **对外/内部术语**

## Discrepancy 分级

- 🔴 **P0** = 数据错 / 状态错 / 术语内部对外混（必修）
- 🟡 **P1** = 字段缺失但数据对（补齐）
- 🟢 **P2** = 微调 / 视觉细节（可推迟）

---

## 全站通用问题

### #A1 · 积分单位精度 🔴
- **现状**：UI 上"1,245 积分" / "6,400 / 10k 积分" / "-106 积分" —— 都是整数
- **schema**：`wallet.balance` = INTEGER microunit（1_000_000 = 1 元）
- **decisions §8.7 (v2)**：1 积分 = 1 元
- **修**：
  - 余额显示：`floor(wallet.balance / 1_000_000)` = 整数积分
  - 花费/号价（可能有小数如号价 4.5 元）：保留 2 位（`4.50 积分`）
  - `1,245.00 积分` 还是 `1,245 积分`？—— **整数场合无小数点**（Cash App 风格）；单价才带小数

### #A2 · vendor 内部 id vs 显示名 🔴
- **现状**：UI 里混用 "Kiro Market" / "Kiro CEO" / "Kiro OOO" / "Kiro App IO" / "Kiro App CC" / "Kiro Drop"
- **schema**：`vendor_id ∈ {91kiro, kiroceo, kirooo, kiroappio, kiroappcc, kirodrop}`
- **decisions §12.5**：对外显示名，不暴露内部 id
- **正确映射**（跟 `docs/vendors/*.md` 对齐）：
  | vendor_id | 对外显示名 |
  |---|---|
  | 91kiro | Kiro Market |
  | kiroceo | Kiro CEO |
  | kirooo | Kiro OOO |
  | kiroappio | Kiro App IO |
  | kiroappcc | Kiro App CC |
  | kirodrop | Kiro Drop |
- **修**：mockup 现有 6 vendor chip 名字**是对的**。但需在字段引用时注意后端返回 id、前端做映射
- **落 decisions §8.16**：vendor 显示名映射表

### #A3 · 内部状态枚举渗到 UI 🔴
- **现状**：机器人通知页出现 `round.completed` / `round.failed` / `credential.dead` / `bus.refilled` / `wallet.low` webhook 事件名
- **CLAUDE.md §12.6**：UI 不出现内部术语
- **判定**：**webhook 配置页 · 例外允许**（技术用户看的，跟对接文档同性质）· 但**订阅事件卡片的显示文案要加中文说明**（现在有"拉号轮次完成/拉号失败/号死了"作为副标 —— OK）
- **修**：机器人通知页已经 OK · 不改。**但对接文档页要在页面 title 上加 badge "面向开发者"**（技术页 · 允许内部 id）

### #A4 · 用量 10k 阈值 🟡
- **现状**：`6,400 / 10k 积分`（用量进度条）
- **decisions §8.14**：10k 积分寿命阈值
- **schema**：`credential_usage_snapshot.credits_used` (microunit)
- **修**：
  - 计算：`credits_used / 1_000_000` = 积分
  - 阈值：10_000_000_000 microunit = 10k 积分
  - 显示：`{floor(used_credits/1M)} / 10k 积分` · **已是对的**

---

## 各页 audit

### 05-home · 拼车 tab（Pooling）

| 元素 | 现状 | 应该 | 分级 |
|---|---|---|---|
| Hero "3 辆车正在跑" | 硬编码 | `SELECT COUNT(*) FROM bus WHERE creator_passenger_id=? AND status='active'` | 🟢 mock 说明 |
| Hero pill "12 号活" | 硬编码 | `SUM(alive_count) FROM bus_usage_snapshot WHERE bus_id IN 我方 bus` | 🟢 mock 说明 |
| Hero "2 已失效" | 硬编码 | 同上 dead_count · 但**注意 handed_off 不算 dead** | 🟢 |
| Hero "今日消费 45 积分" | 硬编码 | `passenger_daily_counter.spend_total / 1M` (WHERE date=today) | 🟢 |
| Featured Card 名字 "周末拼车局" | 硬编码 | `bus.name` | 🟢 |
| Featured chip "拼车·3车友" | 硬编码 | `bus.kind` 映射 + `COUNT(bus_member)`；**1 人 bus 只显示 kind chip 不显示成员数** | 🟡 需分 kind 显示 |
| Featured stat "12 号活/1 已失效" | 硬编码 | `bus_usage_snapshot.alive_count/dead_count` | 🟢 |
| Featured stat "28 积分今日消费" | 硬编码 | 从 `pull_round.key_cost_total + service_fee_total` WHERE bus_id=X AND date=today | 🟢 |
| Featured stat "42h 平均寿命" | 硬编码 | `bus_usage_snapshot.avg_lifespan_seconds / 3600` | 🟢 |
| Featured sparkline 24 柱 | 假数据 | 从 `credential_usage_snapshot` 或 `pull_round` 聚合 —— **数据源待落码时定**（gaps §8.14a） | 🟡 已 TODO |
| Side card "我的号池 4" | 语义混淆 🔴 | "我的号池" = passengerpool（乘客的下游 kiro.rs）· **不是 bus** · 这里当 bus 显示不对 | 🔴 术语 |
| Side card "Kiro 常驻车 6" | 硬编码 bus 名 | `bus.name` | 🟢 |
| "立即拼车 ▾" CTA | 下拉 3 项 | 阶段 1 只 single 可点 · 后 2 项灰（`decisions §8.11`）· 已对 | 🟢 |
| 拉号记录 tag "成功/部分/失败" | 3 态 | `pull_round.status` 5 态收敛 · **`initiated` 中态哪去了？** | 🔴 状态机 |
| 拉号记录 "推池 ✓ / 未推 / 推池失败" | 3 态 | 是**下游推送状态** · 但 schema 里没有独立字段追踪 · 需要新增 `pending_assignment.push_status` 或走 `outbound_webhook_delivery` 关联 · **待补 schema** | 🔴 schema 缺 |
| 拉号记录 usage progress "6.4k/10k 积分" | 显示 | `credential_usage_snapshot.credits_used` · 但轮次里可能是多号 · **是显示轮次总用量还是某个代表号？** | 🟡 语义待定 |
| 花费列 "-12 积分" | 显示 | `pull_round.key_cost_total + single_pull_fee_total + service_fee_total` / 1M · 负号 UI 层加 | 🟢 |

### Overview · 概览

| 元素 | 现状 | 应该 | 分级 |
|---|---|---|---|
| Hero "今天用得不错" | 文案 | 静态问候语 · OK 但需按时间段变（早/午/晚） | 🟢 |
| Card 总余额 "1,245 积分" | 硬编码 | `wallet.balance / 1M` · 已对 | 🟢 |
| Card 总余额 "本月充值 +2,500" | 硬编码 | `SUM(wallet_ledger.amount) WHERE type='topup' AND month=current` · **schema wallet_ledger 存不存？** | 🔴 需查 schema |
| Card 今日消费 "-45 积分" | 硬编码 | 同 A1.hero.today_spend · **正负号方向**：消费显示 `-45` OK | 🟢 |
| Card 累计拉号 "128 次" | 硬编码 | `SELECT COUNT(*) FROM pull_round WHERE passenger IN participants_split_json AND status='completed'` | 🟢 |
| Card 活跃号 "12" | 硬编码 | `bus_usage_snapshot.alive_count` 汇总 · **注意**：这个跟 hero pill 12 号活重复？—— 概览是**全局**汇总，拼车 hero 是**用户 bus** 汇总 · 应该一致 · **需澄清覆盖范围** | 🟡 语义待定 |
| 30 天趋势 3 维度切换 | UI toggle | 消耗积分/拉号次数/平均寿命 · 数据源分别是 wallet_ledger / pull_round / vendor_lifespan_snapshot · **schema 是否有按日聚合视图？** | 🔴 schema 可能缺 |
| 今日活动 tag "入车/提取" | 2 tag | 事件类型收敛 · OK | 🟢 |
| 今日活动"推我的号池"作为 dest | 显示 | dest 语义：入车 → `bus.name`；提取 → `record group` 待派 / passengerpool / handoff · **OK** | 🟢 |

### Extract Key · 提取 key

| 元素 | 现状 | 应该 | 分级 |
|---|---|---|---|
| 主动作 vendor 6 选 | 6 chip | `providers/kiro/vendors/*` 6 家 · 名字对齐 A2 · 已对 | 🟢 |
| 数量 stepper "5 号" | 硬编码 | 1-20 号（`05-api-contract §extract` 限制） · **UI 应显示上限 hint** | 🟡 补 hint |
| 预估 breakdown "号价 · 5 号 100" | 硬编码 | 号价 = vendor 实时快照 × count · **schema 里 vendor 报价存哪？** —— `providers` 层实时抓 · UI 请求预估接口 | 🟢 逻辑对 |
| 预估 "单次议价 20%" 缺失 🔴 | count=5 时无议价 · OK；count=1 时应有 | `decisions §8.9`（错，是 00§3）：count=1 加 20% · **当前 mockup count=5 · breakdown 正确无议价**；但需画 **count=1 状态**看效果 | 🟡 补 count=1 态 |
| 预估 "服务费 5" | 硬编码 | 服务费 = 1 元 × 1（每人每次动作固定 1，无论几号）· **我写 5 错了 · 应该是 1** | 🔴 计费错 |
| 预估 "通道费 5% · 1" | 硬编码 | **等等**：通道费只在**充值时**扣（`00 §3` waffo pass-through），不在拉号时扣！**mockup 把通道费加进拉号成本是错的** | 🔴 计费错 |
| 提取记录 tag "推池/待派/进车/拿走" | 4 tag | 是 `pending_assignment.destination` · **schema 有这字段吗？** | 🔴 schema 缺 |
| 提取记录已 handoff 行 "已离开系统 · 无追踪" | 显示 | `credential.status='handed_off'` · **按 §12.5 应该从列表消失** · **矛盾** | 🔴 状态机 |
| 花费列 "-6 积分" | 4 号价+服务费+议价 | 见上计费错误 · 需按正确公式 | 🔴 计费 |

### Bus Detail · 车详情

| 元素 | 现状 | 应该 | 分级 |
|---|---|---|---|
| Hero chip "拼车·3 车友" | 假 | `bus.kind=anon` 且成员数 = COUNT(bus_member WHERE left_at IS NULL) · **但 anon 阶段 2b · 阶段 1 没有多人 bus** | 🔴 阶段不对 |
| Hero "邀请码 KIRO-A8Q2" | 硬编码 | `bus.invite_code` · **只 kind=team 才有** · 阶段 1 全 single 无 invite_code · **不应显示** | 🔴 |
| Hero 3 头像堆叠 | 硬编码 | bus_member 头像 · **1 人 bus 应该只显示 1 头像或不显示** | 🔴 |
| Hero "创建于 8 天前" | 硬编码 | `now - bus.created_at` · 逻辑对 | 🟢 |
| 5 tab (号列表/拉号历史/补车策略/车友/危险区) | 5 tab | `kind=single` 时**车友 tab 应隐藏**（自己一个人）· 或改成"我" | 🔴 |
| 补车策略 · 水位线 "3 号" | 硬编码 | `bus.refill_watermark` = INTEGER · **单位是"号数"还是"积分"？** schema 注释"号数低于水位触发补车" · **确认是号数** | 🟢 |
| 补车策略 · 最少补到 "5 号" | 硬编码 | `passenger_strategy.min_count` · **注意**：schema 里在 passenger_strategy 表 · 而**decisions §8.6 定策略跟 bus 绑** · **schema 不匹配 · 需修 schema** | 🔴 schema 冲突 |
| 补车策略 · 单号最高价 "25 积分" | 硬编码 | `passenger_strategy.max_unit_price` (microunit) / 1M · 单位对 | 🟢 |
| 补车策略 · 每日花费 "200 积分" | 硬编码 | `passenger_strategy.daily_spend_limit` (microunit) / 1M · 单位对 | 🟢 |
| "拉号中" 应该显示 initiated 状态 | 未画 | pull_round.status='initiated' 时车里怎么显示？—— **待补 pending 拉号横条** | 🟡 缺 |

### Cred Drawer · 号级抽屉

| 元素 | 现状 | 应该 | 分级 |
|---|---|---|---|
| Hero id "cred_01H8Z3M...N4F2" | 显示内部 id | `credential_ledger.id` UUID · **对乘客展示是否合适？** —— 已 decisions§12.5 收敛"号"对外不叫 credential · **但抽屉里可作为 debug 显示** · OK | 🟢 |
| Hero "6,400 / 10k 积分" | usage 进度 | `credential_usage_snapshot.credits_used / 1M` · 阈值 10k · OK | 🟢 |
| 归属 · 车 "周末拼车局" | 硬编码 | `bus.name` WHERE id=credential.owner_bus_id · OK | 🟢 |
| 归属 · vendor "Kiro Market" + "账号 · account-3" | 硬编码 | `vendor_account.label` · **对乘客展示"account-3"不合适**（内部账号编号） · **只显示 vendor 名，账号 debug 用** | 🔴 术语 |
| 归属 · 推池 "已推 · 2 分钟前" | 硬编码 | 需追踪 pending_assignment.push_status + timestamp · **schema 需补** | 🔴 schema 缺 |
| 24h 用量 24 柱 | 假 | `credential_usage_snapshot` 需按小时聚合 · 但表只按 window (24h/7d/30d) 存 · **schema 无小时粒度** · **需补或改按天** | 🔴 schema 或改 UI |
| 底部 3 CTA (派进其他车 / 推池 / 拿走) | 3 按钮 | 需检查号所在状态：若在 bus group → "派进其他车"应该是"移到其他车"；若已推池 → "推池" disabled；已 handed_off → 抽屉不应打开 | 🔴 状态相关 |

### Wallet · 钱包

| 元素 | 现状 | 应该 | 分级 |
|---|---|---|---|
| Giant "1,245 积分" | 硬编码 | `wallet.balance / 1M` · OK | 🟢 |
| delta "本月充值 +2,500" | 硬编码 | `SUM(wallet_ledger WHERE type='topup' AND month=current) / 1M` · schema 里 wallet_ledger 表存在（06 §5） · OK | 🟢 |
| 流水 tag "消费/充值/兑换/退款" | 4 tag | `wallet_ledger.type ∈ ?` · **schema 里 type 枚举是什么？** 需查 06 §5 | 🟡 查 schema |
| 流水行 "拿走·3 号·Kiro CEO" | 硬编码 | 拿走应当 wallet_ledger 里没有独立行（钱不重扣）· 应从 credential_ledger 关联展示 · **可能不该出现在钱包流水** | 🔴 |
| 流水行 "waffo·支付宝·200 元 +200" | 硬编码 | 充 200 元 → 5% 通道费 → 到账 190 积分 · **我写 +200 错了** · 应该 +190 | 🔴 计费 |
| 流水"质保退款 +22" | 硬编码 | `wallet_ledger.type='warranty_refund'` · 参照 §00.7.5 "只退积分不退款" | 🟢 |

### 其他页快速列 P0 问题

- **Settings · 我的号池**：kiro.rs URL + Admin API Key —— 但 §12.6 UI 不出现 "kiro.rs" 内部术语 —— 但**这个页对乘客本身就是技术页**（自己配 passengerpool）· 类比对接文档 · 应允许 —— 🟡 加"面向技术乘客"标注
- **Webhook · 机器人通知**：同上 · 技术页允许内部术语 · OK
- **API key · 生产 · N8N 机器人**：mock 名字 · 用户自填 · OK
- **Profile 危险区 · 注销账户 · 30 天内可恢复**：schema 里 passenger 表 `deleted_at`（软删）· 30 天恢复期没在 schema 定义 · **需 decisions 明确**

---

## Schema 缺口需要补

- 🔴 `pending_assignment` 表需含 `destination ∈ {into_bus, push_passengerpool, handoff}` + `push_status` + `pushed_at`
- 🔴 `passenger_strategy` 表跟 bus 关系：应该改成 `bus_strategy` 表（每 bus 一策略）
- 🔴 `credential_usage_snapshot` 需加小时窗 或 UI 改按天
- 🟡 vendor 显示名映射表落 decisions §8.16
- 🟡 30 天恢复期落 decisions §8.17

---

## 状态机 UI 收敛表

| 表 | schema 状态 | UI 状态 |
|---|---|---|
| pull_round | initiated / completed / failed / partial / refunded | 拉号中 / 成功 / 部分 / 失败 / 已退款 |
| credential | alive / dead / handed_off | 活 / 已失效（handed_off 不在列表出现，若列表出现说明查 handed_off 明细页） |
| bus | active / dissolved | 活跃 / 已解散 |
| pull_intent | pending / in_flight / coalesced / fulfilled / failed / cancelled | 排队中 / 拉号中 / 完成 / 失败 · **cancelled 合入 failed** |
| payment_order | pending / paid / failed / cancelled / refunded | 待付款 / 已到账 / 失败 / 已退款 |

**当前 UI 缺**：`initiated`（拉号中态），已在 G3 detail 里画 loading 覆盖，但**首页/车详情/提取 key 页应该也有"进行中"横条**

---

## 修复计划分批

**批 A · 计费错误**（🔴 5 处）
- Extract Key 预估 breakdown：服务费固定 1 · 通道费从拉号剥离 · 议价按 count 分支
- Wallet 流水：waffo 充 200 → 到账 190（不是 +200）
- Bus 详情：抹掉 "邀请码"（single 无）· 车友 tab 隐藏（single 无成员）· hero 头像只 1 个

**批 B · 状态机对齐**（🔴 4 处）
- Extract 记录：handed_off 号从列表移除（或另开 "已 handoff 历史" tab）
- Bus 详情：加"拉号中"横条对应 `initiated`
- Pull 记录：区分 `refunded`（不是"失败"）
- Cred Drawer：按号当前状态显示不同底部 CTA

**批 C · 语义/术语**（🔴 3 处）
- 拼车页 side card "我的号池 4" 改成 kind chip + 真实语义（不是 bus 也不是 passengerpool 混显）
- Cred Drawer 归属：抹掉 "account-3"（内部账号编号）· 只显示 vendor 显示名
- Setting/Webhook/Docs 页顶加"面向技术乘客"标注

**批 D · schema 补充**（🔴 3 处）
- 补 `pending_assignment.destination + push_status`
- 补 `bus_strategy`（替换 passenger_strategy 或独立表）
- 补 `credential_usage_snapshot` 小时窗（或 UI 改按天）

**批 E · 数字对齐 microunit**（🟢 全站）
- 全 mockup 数据说明：`X 积分 = X * 1_000_000 microunit`
- 有小数场合（如号价 4.5 元）：保留 2 位

---

## 三轮修复完成状态（2026-08-07）

- ✅ 批 A（3/3 计费错）：Extract 预估通道费剥离、服务费固定 1 · Waffo 200→190 积分 · Bus 详情 single 收敛
- ✅ 批 B（4/4 状态机）：Extract handed_off 归档说法 · Bus 详情 initiated 拉号中 banner · Cred Drawer 状态 hint
- ✅ 批 C（3/3 术语）：拼车页 side card 语义修 · Cred Drawer 抹 account
- ✅ 批 D（3/3 schema）：credential_ledger 加推池字段 · bus 加 8 策略字段 · usage_snapshot 加 1h window
- ✅ 批 E（全站精度）：decisions §8.7 v2 定义 1 积分 = 1 CNY = 1_000_000 microunit

**下一步**：由代码实现层保证 API 层 microunit ↔ 积分 UI 一致换算。落码前无需 mockup 数字级别修改。
