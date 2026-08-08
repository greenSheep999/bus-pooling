# 提取 key 页方案 · v1

> **前置阅读**：`CLAUDE.md §1.2 §12` · `docs/vendors/*.md` 六家 API 档案 · `12-frontend-pages.md § 6-7`（旧 · 待作废）· `decisions.md §8.9 §8.17-8.19`
>
> **状态**：方案讨论中 · 落码前需车主 review
> **抓取日期**：2026-08-08
> **作废旧文档节次**：`12-frontend-pages.md §6 /pull` + `§7 /pull-records` 合并成本文档描述的 `/extract` 单页

## 1. 现状 · 现在的 Extract 页不对

**只有 165 行** · 结构：
```
Hero: [提取 key] 按钮 + 3 种去向标签说明
└── 待派列表（BareList · 复选 · 底部批量派去向）
```

**问题清单**（车主提出）：

1. **拉号维度太少** · 现在只有 `count + vendor` 两个输入，但**六家 vendor 都强制要求区域**（us/eu）· 单价 · 库存 · 单次上限，全没体现
2. **没有提取历史** · 只有"待派"当前态 · 派完就消失 · 用户看不到"我上周拉过 12 次"
3. **没有派发历史** · 派完不知道去哪了
4. **拿走后号数据消失** · `DELETE /credentials/{id}` 一执行 · 用户查不到"上午下载的号是哪家 vendor 的"

## 2. 六家 Vendor 拉号维度共性（`docs/vendors/*.md` 提炼）

### 2.1 强制维度

| 维度 | 六家表现 | UI 必要 |
|---|---|---|
| **count** 数量 | 1-200（91kiro）/ 1-10（kiro-ceo）/ 1-N（各家不同） | ✅ 必填 |
| **zone/region** | `us` / `eu` 二选一 · **每区独立单价** · 91kiro / kiro-ceo / kiroapp-io / drop-kiro-ss 都有 · kiroapp-cc 无区域 | ✅ 必选 or 自动 |
| **client_order_id** 幂等键 | 32 位 hex · 前端不用管 · 后端自动生成 | ❌ 隐藏 |

### 2.2 展示维度（不填但要看）

| 维度 | 从哪来 | UI 必要 |
|---|---|---|
| **单价** | `stock.zones[].unit_price` 或 `unit_price` | ✅ 选完 vendor+zone 显示 |
| **库存** | `stock.zones[].available` 或 `available` | ✅ 显示 · 缺货禁用 |
| **max_purchase** | 各家不同（91kiro=200, kiro-ceo=10, kiroapp-io=10 …） | ✅ 约束 count 上限 |
| **min_per_order** | 通常 1 | ✅ 约束 count 下限 |
| **warranty_minutes** | 各家 5-30 分钟不等（91kiro=10） | ✅ 显示"这批号 N 分钟内失效可退" |
| **hold_cap_effective** | 91kiro 独有（名下持有上限）· 快满了禁拉 | ⚠️ 选 91kiro 时提示 |
| **currency** | 5 家积分 · drop-kiro-ss 混币（CNY 账户 USD 定价） | ⚠️ 选 drop-kiro-ss 时提示 |

### 2.3 我方增值维度（不属于 vendor）

| 维度 | 含义 | 手动出现？ | 自动出现？ |
|---|---|---|---|
| **auto_vendor** | "让系统按有效成本比价"（不指定 vendor · 后端按当前库存 + 单价 + 单次议价综合选） | ✅ | ✅ |
| **service_fee** | 每次拉号动作固定 1 元服务费（`CLAUDE.md §1.3`） | ✅ 显示 | ✅ 内部扣 |
| **bargain_fee** | count==1 时的单次议价（内部术语 · UI 不出现 · 只体现在"拉 2+ 更划算"提示） | ✅ | ✅ |
| **channel_fee** | 5% 通道费（waffo 充值时收 · 拉号动作不重复收） | ❌ | ❌ |
| **max_unit_price** | 单价上限 · 护栏字段 | ❌ **不出现** | ✅ 策略护栏（超过就不自动拉） |
| **daily_round_limit** | 日轮次上限 | ❌ **不出现** | ✅ 策略护栏 |
| **daily_spend_limit** | 日花费上限 | ❌ **不出现** | ✅ 策略护栏 |

### 2.4 手动 vs 自动 · 字段分工铁律（本次讨论新增）

**手动拉号 = 用户主动决策 · 眼见为实价 · 看到当前单价直接拍板买不买 · 不需要预设阈值**

**自动补车 = 系统按策略自动触发 · 用户不在场 · 需要护栏防止乱扣钱**

**结论**：
- Extract 页拉号弹窗 / Bus 详情"立即拉号"弹窗 = **无 `max_unit_price` / `daily_*_limit` 字段**
- Bus 补车策略 tab = **保留所有护栏字段**（现状 EditStrategyPanel 不变）
- 手动拉号请求提交时 · 后端**不检查**这些护栏（护栏只对自动补车生效）

## 3. 页面结构（新版 · 3 tab）

### 3.1 整体

```
提取 key
├── Hero
│   ├── H1 · 「提取 key」
│   ├── 副行 · 「拉号进"待派" · 之后你派 3 种去向：进车 / 推我的号池 / 下载 txt 拿走」
│   ├── 副行数字 · 「待派 <N> 个 · 本月已提取 <M> 个 · 花 <K> 积分」
│   └── 右上 [提取 key] 按钮（brand 紫 · rounded-xl · hero CTA）
│
└── Tabs · 3 段（跟 BusDetail Tabs 组件复用）
    ├── 待派 · 号还在池，选去向
    ├── 提取历史 · 每次拉号操作时间序列
    └── 派发历史 · 每次派动作时间序列（三种去向 badge 区分）
```

**不做数据 tab** · 车主判定：Extract 语义简单 · 号进出没有复杂存续状态 · 数据纵览走概览页 · 不重复。

### 3.2 Tab · 待派

**当前有的**：号列表 + 复选 + 底部批量派去向。

**新增**：
- **顶部聚合**：`共 <N> 个 · 从 <vendors count> 家 vendor · 累计冻结 <credits> 积分`
- **表头**：复选 · key masked · vendor · **区域**（us/eu）· 单价 · 寿命 · 质保剩余 · 拉入时间
- **区域 badge**：`us` 蓝底 · `eu` 橙底（视觉一眼区分）
- **质保剩余倒计时**：如果 `warranty_until > now` · 显示 `质保 4 分 32 秒`（红色警示 · 快到期变闪烁）
- **底部批量派**：保留现有 AssignModal · label "拿走" → "**下载 txt · 拿走**"

### 3.3 Tab · 提取历史（新）

**每条 = 一次拉号操作**：

```
表头：时间 · 结果 · vendor · 区域 · 数量 · 花费 · 状态

行示例：
08/07 15:23  ✓ 成功    Kiro Drop   us   5 个  -12 积分   全部待派
08/07 12:15  ✓ 成功    Kiro Market eu   3 个  -8  积分   已派 2/3
08/06 22:30  ✗ 失败    Kiro CEO    us   0/3        缺货
08/05 09:00  ⚠ 部分    Kiro Drop   us   2/5  -5  积分   部分成交
```

**说明**：
- 结果 Chip 用 dot（跟 BusDetail 拉号历史规则统一 · `decisions §8.19 Chip 语义`）
- 状态列显示"这批号现在怎么样了"：全部待派 / 已派 N/M / 已进车 / 已推池 / 已拿走 / 部分失效
- 点行 → 展开显示这批号明细

### 3.4 Tab · 派发历史（新）

**每条 = 一次派去向动作**（一次派可以是多个号一起派同一去向）：

```
表头：时间 · 去向 · 数量 · 明细

行示例：
08/07 16:00  → 进车「周末拼车局」        3 个   [3 个号 masked]
08/07 15:45  → 推我的号池 pool.foo.com   2 个   [2 个号 masked]
08/06 10:30  → 下载 txt 拿走             1 个   [1 个号 masked · 已删明文]
```

**说明**：
- 去向 badge 视觉区分：进车（紫 brand）· 推池（Send icon）· 拿走（红 danger · Download icon）
- 点行 → 展开号明细
- **拿走的行**：只显示 masked · 明文已删（`CLAUDE.md §1.2` 唯一发出去不管 · 但元数据保留见 §5）

## 4. 拉号弹窗（新版 · 严肃对齐 vendor 能力）

**替换现有的 `<PullExtractModal>`** · 内容：

### 4.1 布局

```
┌ 提取 key ─────────────────────────────────────────┐
│                                                    │
│ [ 数量 ]      [ 区域 · us / eu / 自动 ]            │
│                                                    │
│ [ vendor · 让系统选 / 具体 6 家 ]                  │
│                                                    │
│ ┌ 上游即时状态（选完 vendor+zone 出现） ─────┐    │
│ │ 库存         42 个 · 当前区可提                 │ │
│ │ 单价         30 积分 / 个                       │ │
│ │ 质保         10 分钟内失效可退                  │ │
│ │ 单次上限     200 个                             │ │
│ │ 历史存活     平均 12h · 30 天成活率 87%         │ │
│ │ [仅 91kiro] 持有上限 剩余 5（名下已 5/10）      │ │
│ │ [仅 drop-kiro-ss] 美元定价 · 汇率约 X USD → Y 积分│ │
│ └───────────────────────────────────────────────┘   │
│                                                    │
│ ┌ 预估费用 ─────────────┐                          │
│ │ 号价      30 × 5 = 150 │                          │
│ │ 服务费    1 元         │                          │
│ │ ────────────────────── │                          │
│ │ 小计      151 积分     │                          │
│ └────────────────────────┘                          │
│                                                    │
│ ⓘ 拉 2 个及以上单价更低（已省）                    │
│                                                    │
│ [取消]                        [拉 N 个 key ▸]     │
└────────────────────────────────────────────────────┘
```

### 4.2 字段规则

| 字段 | 规则 |
|---|---|
| **数量** | number · min 1 · max = 所选 vendor 的 `max_purchase`（默认 200 · 选具体 vendor 后动态收紧） |
| **区域** | Select: `auto` / `us` / `eu` · 默认 `auto` · 选具体 vendor 后如果只有一区可用就自动切 |
| **vendor** | Select: `auto` / 6 家 · 缺货 vendor 灰化 · 悬停显示"缺货" |
| **单价上限** | ❌ **不出现**（手动模式眼见为实价 · 见 §2.4 铁律） |
| **预估费用** | 实时算 · vendor+zone 都选定后精确 · `auto` 时显示"约 X-Y 积分（选定 vendor 后精确）" |

### 4.3 上游即时状态面板（新增 · 核心 UI）

**选完 vendor + zone 后弹出** · 默认 `auto` 时显示合并信息或引导"选具体 vendor 看详情"。

**字段来源**：

| 字段 | 从哪来 | 显示规则 |
|---|---|---|
| **库存** | `stock.zones[selected_zone].available` | 数字 + "当前区可提" · 缺货显示红字 "0 · 缺货" |
| **单价** | `stock.zones[selected_zone].unit_price` | 数字 + "积分 / 个" · 是"最便宜一档"（91kiro） · 或固定单价（kiroapp-cc） |
| **质保** | `stock.warranty_minutes` | 数字 + "分钟内失效可退" · `0` 时显示 "无质保" · red 警示 |
| **单次上限** | `stock.max_per_order` 或 vendor `max_purchase` | 数字 + "个" |
| **历史存活** | 我方数据（近 30 天该 vendor 所有号）· 用 `credentials` 表 aggr | "平均 Xh · 30 天成活率 Y%" · 无数据时"暂无数据" |
| **持有上限**（91kiro） | `profile.hold_cap_effective - profile.keys_held` | 只在 vendor=91kiro 时出现 · 快满时 red 警示 |
| **币种警示**（drop-kiro-ss） | 静态提示 | 只在 vendor=drop-kiro-ss 时出现 · 显示"美元定价 · 实扣按汇率" |

**视觉**：紧凑的 `<Card>` 内嵌 `<dl>` · 每行左侧灰 label · 右侧 tnum 数字 · 关键值加粗。

### 4.4 vendor 特殊提示（不新增 · 已归入 §4.3 上游状态面板）

- 91kiro hold_cap · drop-kiro-ss 混币 · kiroapp-cc 无区域 —— 都归到面板的 conditional 字段
- 手动拉号后端**不检查**任何护栏 · 用户看到就要能拉（除非余额不足 / 库存不足 / vendor 侧限流）

### 4.5 数据依赖新 API

**当前 `useVendorStats` 不够** · 只返回聚合的 `VendorStat[]`。新加：

```ts
GET /api/me/vendors/:vendor_id/stock?zone=us|eu
  → { available, unit_price, warranty_minutes, max_per_order, min_per_order, hold_cap_remaining? }

GET /api/me/vendors/:vendor_id/history
  → { avg_lifespan_seconds, alive_rate_30d, total_pulled_30d }
```

阶段 1a mock 返回 · 阶段 1b 后端聚合真数据。

### 4.6 拉完后的动作

- 成功 → toast "拉到 N 个号 · 已进待派" · 弹窗关闭 · Extract 页 tab 自动跳"待派"
- 缺货 → toast danger "缺货 · 换个 vendor 或区域试试"
- 余额不足 → toast danger "余额不足 · 差 K 积分" + 引导链接 `/wallet` 充值
- vendor 限流 / 侧异常 → toast warn "vendor 暂不可用 · 稍后重试 或 换个 vendor"

（手动模式不做"单价超上限拒绝" · 因为没有 max_unit_price 字段）

## 5. 全局默认策略 · 车 vs 全局 vs 手动（本次讨论新增）

车主提问："**单价上限是相对自动车的 · 全局设置 · 这个应该在车和全局配置设置吧**"

### 5.1 三层配置的分工

| 层 | 内容 | 用于 |
|---|---|---|
| **手动拉号** | 当次填 count / zone / vendor | Extract 弹窗 · Bus 详情"立即拉号"弹窗 · **无护栏字段** |
| **每车策略**（现有） | auto_refill_enabled / refill_watermark / per_round_count / max_unit_price / daily_round_limit / daily_spend_limit / preferred_vendor | Bus 详情"补车策略"tab · **只影响自动补车** |
| **全局默认策略**（新增 · 阶段 2 或不做） | 默认 zone / 默认 vendor / 默认单价上限（给自动补车用）/ 默认每次数量 | 建新车时的初值 · Extract 弹窗的初值 |

### 5.2 全局默认放哪里

**推荐位置**：头像菜单 → **设置 → 拉号偏好**（跟"我的号池"/"webhook 通知"平级 · `docs/12-frontend-pages.md §11-14`）

**内容**：
- 默认首选 vendor（Select · 6 家或"让系统比价"）
- 默认区域（us / eu / 自动）
- 默认每次数量（number）
- 默认单价上限（number · 空 = 不限 · **仅给自动补车用**）

### 5.3 阶段 1a 是否做

**✅ 已推翻并落地**（见 `decisions §8.27`）：本节当时判断"1a 不做"，理由是"全局默认值价值低"。但**漏了一半** —— `passenger_strategy_default` 里的 `daily_round_limit` / `daily_spend_limit` 不是"默认值"，是**硬上限**，而且**提取 key 只受它管**。等于用户被一个看不见改不了的限额管着。所以阶段 1a **做了** `设置 › 拉号偏好`：上限那半必须有入口，默认值那半顺手一起做（同一个表、同一个端点）。

下面是当时"不做"的原始论证，保留备查：

**原推荐 · 阶段 1a 不做**（`decisions.md §8.21` 归档 —— 注意 §8.21 后来被通道费那条占用，本节记录实际落在 `§8.27`）：

- 阶段 1a 只 3 辆 mock 车 · 每辆车都能填自己的策略 · 全局默认价值极低
- 用户建车弹窗的 default 值 hardcode 就够（count=3 · vendor=auto · zone=auto）
- 等阶段 2 有多人车 / 用户量起来后 · "改一次全局默认 = 应用到所有新车"这个价值才成立
- **阶段 1a 手动拉号 / 补车策略 已经覆盖 100% 场景**

### 5.4 决策记录（`decisions §8.21`）

新加一条：

> **§8.21 · 三层策略分工**（阶段 1a 定型）
> - 手动拉号：无护栏 · 眼见为实价
> - 每车策略：护栏字段（single_unit_price / daily_*_limit）+ 触发条件（watermark）· 只影响自动补车
> - 全局默认：阶段 2 才做 · 路径 `设置 → 拉号偏好`

## 6. 拿走后的元数据保留 · 更新 `CLAUDE.md §1.2`

### 6.1 现状（§1.2 铁律）

> 拿走（handoff）· 号数据交给乘客 + `DELETE /credentials/{id}` · **唯一"发出去不管"的路径**

### 6.2 车主提出的问题

拿走后**号数据没了 · 提取记录里查不到**。用户视角非常差（"我昨天下载的号是哪家 vendor 的都查不到"）。

### 6.3 拟更新为

> **拿走（handoff）**
> - **key 明文**：`DELETE /credentials/{id}` 一秒不留 · 我方无备份
> - **元数据**：**数据库 `credentials` 表保留**（`handoff_at = now` flag · 明文清空 · masked / vendor / 花费 / 寿命 / 拉入时间 保留）
> - **housepool 副本**：删（对应 `record-<pid>` group 里的 credential 记录移除）
> - **UI 可见**：只有 masked · 用户能查"我拿走过什么号 / 花了多少 / 从哪家 vendor"
> - **合规**：明文一秒不留 · 元数据不涉及密钥内容

### 6.4 数据结构变更

**`credential` 表加字段**：
```sql
ALTER TABLE credential ADD COLUMN handoff_at TIMESTAMPTZ NULL;
```

- `handoff_at IS NULL` = 未拿走
- `handoff_at IS NOT NULL` = 已拿走 · UI 归到"派发历史" · 号列表不再出现

**列表过滤**：
- 待派 tab：`handoff_at IS NULL AND owner_bus_id IS NULL AND pushed_at IS NULL AND push_failed = false`
- 派发历史 tab：任何 handoff / into_bus / push_pool 事件

### 6.5 新增两张事件表

**`extract_event`**（每次拉号）：
```
id · created_at · vendor_id · zone · count_requested · count_purchased
total_cost · result (success/partial/failed) · fail_reason
```

**`assign_event`**（每次派动作）：
```
id · created_at · destination (into_bus/push_pool/handoff)
bus_id (into_bus 时) · credential_ids (json array)
```

**两张表都不含 key 明文** · 只有 masked 和元数据。

## 7. 概览页衔接（`Overview.tsx`）

**车主判定**：Extract 页不做数据 tab · 纵览数据走概览。

**概览需要展示的**：

1. **提取业务卡**（现有 · 阶段 1a mock）：
   - 本月提取过 N 个号
   - 3 种去向占比（水平堆叠条 · 进车 XX% / 推池 YY% / 拿走 ZZ%）
2. **活动流混流**（`ActivityKind = extract`）：
   - "08/07 15:23 · 从 Kiro Drop us 拉 5 个号 · 花 12 积分"
   - "08/07 16:00 · 把 3 个号进「周末拼车局」"
   - "08/07 15:45 · 推 2 个号进我的号池"
   - "08/07 16:10 · 下载 1 个号（拿走）"

**数据来源**：`extract_event` + `assign_event` 两张表按时间序合并。

## 8. 分阶段实现

### 8.1 阶段 1a 现在做（MVP）

- [x] 新 Extract 页布局：Hero + 3 tab
- [x] 拉号弹窗按 §4 重做：数量 · 区域 · vendor · 单价上限 · 预估费用 · vendor 特殊提示
- [x] 待派 tab：加区域列 · 单价列 · 质保倒计时
- [x] 提取历史 tab：mock 数据落地 + UI
- [x] 派发历史 tab：mock 数据落地 + UI
- [x] `credential.handoff_at` 字段加入类型 + mock fixture
- [x] AssignModal 里"拿走" → "下载 txt · 拿走" · handoff 走"删明文留元数据"逻辑

### 8.2 阶段 1b 补

- [ ] 后端 `extract_event` / `assign_event` 表落地
- [ ] `credential.handoff_at` 数据库迁移
- [ ] `POST /me/extract` 后端返回真实响应（当前 mock）
- [ ] handoff 走真 vendor delete + 数据库 UPDATE 双写事务

### 8.3 阶段 2+ 未来（不做）

- 拿走后再想推池 / 进车（物理不成立 · 号已删）
- 进车后从车里"抽走号 → 下载 txt"（在 Bus 详情号列表里做 · 不在 Extract）
- 推池后追加下载 txt（在推送记录 tab 加"下载副本"按钮）

## 9. 待车主决策

1. **Extract 页 3 tab 结构 OK 吗**？（待派 · 提取历史 · 派发历史 · 不做数据 tab · 纵览走概览）
2. **§6.3 handoff 元数据保留**：**同意更新 `CLAUDE.md §1.2`**？
   - 我推荐 ✅ · 车主已说"用户不派了记录也没了" 不合理
3. **§4 拉号弹窗**：
   - 已删单价上限（§2.4 铁律 · 手动无护栏）
   - **上游即时状态面板**（§4.3 · 库存/单价/质保/历史存活）· 常驻显示还是折叠？
   - 我倾向**常驻**（选完 vendor+zone 立即出 · 用户挑 vendor 时就是要看这些）
4. **§5 全局默认策略**：**阶段 1a 不做** · 记档到 `decisions §8.21` · 阶段 2 再做 · 同意？
5. **§4.5 新 API `GET /me/vendors/:id/stock` + `/history`**：**阶段 1a 先 mock 返回** · 后端阶段 1b 接线 · 同意？

答完这 5 个 · 我就落码。
