# Mockup Gaps · v1 vs 文档需求缺口清单

> 对齐 `04-scenarios.md` · `06-db-schema.md` · `decisions.md §8` · v1 已画 15 张 mockup
> 目标：**阶段 1a-1e** 内该覆盖的逻辑一张不漏；**阶段 2+** 一律不画（未来事）

## 一、当前 v1 已画（沿用）

| # | Mockup | 场景覆盖 |
|---|---|---|
| 05-home.pen · Pooling | A · 拼车 tab 首页（车列表 + 拉号记录） |
| Detail · 头像 Dropdown | F · 账户菜单 |
| Detail · 立即拼车 | A · 建车入口下拉 |
| Detail · Card Hover | 视觉规范 |
| Overview | 概览页 · 数据摘要 + 混流活动 |
| Extract Key | B1-B6 · 单独拉号主动作 + 记录 |
| Create Bus 模态 | A1 · 建 1 人 bus |
| Assign 派去向弹窗 | B2/B3/B4 · 三去向 handoff 详情 |
| Dispatch 空态 | 阶段 3 占位 |
| API Docs | 静态文档 |
| Webhook · 机器人通知 | F3 · 对外 webhook 配置 |
| Wallet · 钱包 | D1/D2 · 充值 + 兑换 + 流水 |
| Settings · 我的号池 | F2 · passengerpool 配置 |
| API Key | F4 · key 管理 |
| Profile / Login / Register | F · 账户 |

## 二、缺失的核心页 · 必补（阶段 1a-1e）

### G1 · Bus 详情页 · 缺
**场景**：A1/A6/A7/C1/C2 · 用户点某辆车 → 车详情
**必有 tab**：概况 · 号列表 · 拉号历史 · 补车策略 · 成员（1 人 bus 隐藏）· 危险区（解散）
**字段**：
- Hero：车名 / kind chip · 状态 chip · 号活/失效数 · 累计消费 · 平均寿命 · 车龄
- **补车策略 tab**：`auto_enabled` toggle · `per_round_count` · `min_count` · `keep_safety_stock` · `max_unit_price` · `daily_round_limit` · `daily_spend_limit` · `refill_watermark`
- **号列表 tab**：每号一行 · vendor · pulled_at · 状态圆点 · 用量进度条 · 寿命 · 死因 death_source · 是否已推池
- **拉号历史 tab**：同 v1 拼车页底部 4 行结构
- **解散 tab**：警告 + `Dissolve` CTA · 提示"活号挪到你 record group 保留"

### G2 · 号级详情面板 · 缺
**场景**：点号列表任一号 → 抽屉或模态
**字段**：
- cred_id · vendor · pulled_at · 归属 bus / record group
- 状态 · dead_at · death_source（housepool_probe / vendor_webhook / vendor_poll）
- 用量曲线（credential_usage_snapshot 里 24h/7d/30d）
- 是否已推 passengerpool · 推池时间
- 单号操作：**清理**（DELETE 无明文，因为号已无用）· **拿走**（handoff）· **推池**（B3）· **派进 bus**（B2）
- 若已 handoff · 显示"已离开系统 · 无法追踪"

### G3 · 单独拉号 · loading + 结果态 · 缺
**场景**：B1 · 用户点"提取 5 号" → 中间 loading → 成功/部分/失败 结果
**必画**：
- **loading 态**：skeleton 记录行 + 顶部横条 "正在从 Kiro Market 拉号 · 已完成 2/5"
- **成功后**：新 5 号跳出来 highlight 动画 · 顶部横条 toast "5 号已进池待派 → 派去向"（primary CTA）
- **部分成功**：横条 "拉到 3/5 号 · 2 号失败 · 已退 42 积分" · 失败原因（no_stock / rate_limit / vendor_error）
- **失败**：错误页 / 横条红色 · 退款 confirm

### G4 · 兑换码充值 · 缺
**场景**：D1 · 输码 → 到账
**必画**：模态或独立页
- 输入框 · 大 CTA "兑换"
- 成功态：giant "+50 积分" + 到账动画
- 失败态：过期 / 已用 / 不存在（红色 hint）

### G5 · Waffo 充值流程 · 缺
**场景**：D2 · 5% 通道费 pass-through
**必画**：3 步
- Step 1：输金额（100 元）· 显示"到账 95 积分（waffo 扣 5% 通道费）"
- Step 2：显示假二维码 · 等待支付 · loading dots
- Step 3：成功 checkmark · giant "+95 积分" · 跳回钱包

### G6 · 余额不足拒绝态 · 缺
**场景**：D3 · 提取 key CTA 点击时余额 < 号价+服务费
**必画**：横条红 "余额不足 · 需要 106 积分 · 当前 45" + "去充值" CTA · 提取按钮 disabled

### G7 · 日花费/日轮次触顶 · 缺
**场景**：D5 · 自动补车触顶
**必画**：车详情页 hero 下方红色横条 "今日已达上限 · 明日 00:00 UTC 重置" · 手动拉号按钮 disabled hint

### G8 · 拉号轮次单价超上限 · 缺
**场景**：D4 · 用户配了 max_unit_price 50 · 当前 vendor 60
**必画**：提取 key 页顶部 warning 横条黄色 "已选 vendor 单价超策略上限 · 跳过 or 放宽" 二选一按钮

### G9 · 单次议价预估 · 补
**场景**：D6 · count==1 触发 +20%
**必改**：提取 key 页 count=1 时右侧预估要显示 breakdown 里多一行 "单次议价 · +4 积分"（v1 已有但没体现 count==1 vs count>=2 的差异）

### G10 · Handoff 二次确认（安全）· 缺
**场景**：B4 · 拿走号明文（决定性 · 不可回滚）
**当前 v1**：Assign 弹窗直接明文
**改造**：分两步
- Step 1：警告页 · "确认拿走 3 号 · 无法撤销"（红色 · 大 CTA "确认拿走" + 取消）
- Step 2：明文 4 件套显示（v1 现有内容）+ 复制 + "关闭"倒计时 5 min

### G11 · 空态（每页都要）· 缺
**新号页面必备空态**：
- **拼车页 · 没车**：hero 光晕 + "还没有车" + "建你的第一辆车" primary CTA
- **提取 key 页 · 没记录**：hero 光晕 + "还没有提取过号" + "试试提取 5 号"
- **概览 · 首次登录**：所有数字都是 0 · "从建车或提取 key 开始"
- **API key · 没 key**：hero + "还没有 key" + "新建"
- **对接文档 · 无需空态**

### G12 · Toast 系统 · 缺
**场景**：每次动作后需要短暂反馈
**规范**：
- 位置：右下 corner · 或顶部横条（跨页面通知）
- 4 类型：success（绿）· info（灰）· warning（黄）· error（红）
- 内容例：
  - "5 号已进池待派" · action "派去向 →"
  - "拿走 3 号 · 明文已复制"
  - "余额不足 · 去充值 →"
- **画一张 detail frame · 4 种 toast 陈列**

### G13 · 号死通知 · 缺
**场景**：C1 / C3 · webhook / UI 里号死
**必画**：
- 通知铃铛点开 popover · 4-5 条通知列表（号死 · 补车成功 · 号池副本失效 · 余额低）
- 顶部横条 · "3 号刚失效 · 已触发自动补车 →" · 可关闭

### G14 · 加入他人 bus · 邀请码 · 缺
**场景**：A5 · 阶段 2a
**画不画**：**画占位模态**（灰 CTA "阶段 2 开放"），跟"立即拼车"下拉里第 3 项对齐

### G15 · 匿名搭车配置 · 缺
**场景**：A2/A4 · 阶段 1c/2b
**画不画**：**画占位模态**（灰 CTA "阶段 2 开放"）

## 三、需要重构的现有 mockup

### R1 · 概览页
- 4 stat 卡：**只有余额是 focal**（光晕 + brand-hairline），其他 3 张**白底 + hairline 灰边**
- 30 天趋势卡：白底 + hairline
- 今日活动：**去掉 card 包裹** · 行 + 分隔线（Cash App 风格）

### R2 · 拼车页
- Featured car 保持光晕 · 是 focal
- 双 side card 改白底 + hairline
- 拉号记录去 card 包裹 · 直接行 + 分隔线

### R3 · 提取 key 页
- 主动作 card 保留光晕 · 是 focal
- 提取记录去 card 包裹 · 行 + 分隔线

### R4 · 建车模态
- 补车策略字段补齐（v1 缺 `min_count` / `keep_safety_stock` / `refill_watermark`）
- 分组：**基础**（车名）· **补车触发**（watermark / min_count）· **每轮拉几个**（per_round_count）· **成本上限**（max_unit_price）· **限额**（daily_round / daily_spend）

### R5 · 派去向弹窗
- Handoff tab 分两步（G10）
- "进车" tab 补 bus 下拉选择 UI（v1 未画）
- "推我的号池" tab 补状态说明（配置正常/未配置提示去 F2）

### R6 · 我的发车空态
- 已 OK · 视觉不变

### R7 · 对接文档
- 已 OK · 视觉不变

### R8 · 机器人通知
- 3 卡都是灰底 · 改：Webhook 端点保留（focal 灰底）· 订阅事件白底 · 投递记录白底

### R9 · 钱包
- Hero 卡光晕保留 · 是 focal
- 流水去 card 包裹 · 直接行 + 分隔线

### R10 · 设置 · 我的号池
- 3 状态卡改白底 · 只有连通那张浅绿 tint
- kiro.rs 端点 card：focal 光晕
- 推送策略 card：白底 + hairline

### R11 · API key
- 表白底 · 每行 hairline 分隔

### R12 · Profile
- Hero 光晕 focal 保留
- 账户安全 section 白底 + hairline
- 危险区保持红 tint

## 四、视觉三层强弱规范（最终定稿）

| 层 | 用法 | fill | stroke |
|---|---|---|---|
| **Focal**（一屏一个） | Hero 卡 · 主动作 card | 光晕 gradient + `$bg-elevated` | `$brand-hairline` |
| **Default** | 内容 card · 表格 · 表单区 | `$bg`（白/黑） | `$hairline` |
| **Subtle** | 分组容器 · 装饰性 hint · 二级模态背景 | `$bg-elevated` | 无 or `$hairline` |
| **Ghost**（去 card） | 列表流 · Cash App 交易记录 | 无 | 只用行间 hairline |
| **Status tint** | 状态说明卡 · 警告条 | `$tag-*-bg` | `$tag-*-fg` |

## 五、字号系统

| 用途 | size | weight | letterSpacing |
|---|---|---|---|
| Giant（余额） | 96 | 600 | -2 |
| Hero title | 48 | 600 | -1 |
| Focal 数字 | 40 | 600 | -0.5 |
| Sub-hero | 32 | 600 | -0.5 |
| Stat 数字（次要） | 32 | 600 | -0.5 |
| Section title | 20 | 600 | 0 |
| Card title | 18 | 600 | 0 |
| Modal title | 22 | 600 | 0 |
| Body large | 15 | 500 | 0 |
| Body | 14 | 500 | 0 |
| Body small | 13 | 500 | 0 |
| Label / caption | 12 | 500-600 | 0 |
| Micro / hint | 11 | 500 | 0 |
| Mono code | 12 | 500 | 0 |

**只用 500 / 600 两档字重**（decisions.md §8.15 加个补充）。

## 六、间距系统

| 元素 | padding |
|---|---|
| Focal 大 card | 32 / 32 |
| Default card | 24 / 24 |
| Modal | 32 / 32（body）· 28 / 32（header） |
| Pill | 6 / 14 |
| Chip | 3 / 10 |
| Button primary | 12-14 / 20 |
| Button ghost | 8 / 14 |
| Input | 12-14 / 16 |

| 元素 | gap |
|---|---|
| Section 间 | 48 |
| Section 内 group 间 | 24 |
| Group 内 field 间 | 12-16 |
| Inline group（chip + text） | 6-8 |

## 七、执行顺序 · 二轮改造分批

**批 1 · 视觉重构**（现有 15 张全过一遍）
- R1-R12 按上面规范改
- 一张一张 · 每张改完截图确认

**批 2 · 补关键缺页**（阶段 1a-1e 必备）
- G1 · Bus 详情页（最重要 · 阶段 1a）
- G2 · 号级详情抽屉
- G3 · 提取 key loading / 结果态
- G4 · 兑换码流程
- G5 · Waffo 充值 3 步
- G10 · Handoff 二次确认改造
- G11 · 4 个空态
- G12 · Toast 系统 4 类型 detail
- G13 · 通知铃铛 popover

**批 3 · 阶段 2/3 占位**（G14/G15）
- 灰化占位模态 · 2 张

**总计 · 二轮完成后**：15 张改造 + 9-12 张新增 = **24-27 张 mockup**
