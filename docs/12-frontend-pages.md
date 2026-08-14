# bus-pooling · 前端页面清单 + 路由

> 前置：`04-scenarios.md` · `05-api-contract.md` · `sprint-1a-frontend.md`
>
> **14 页面 + 2 layout** 覆盖阶段 1 全部乘客侧需求。管理端阶段 3+ 单独开文档。
>
> **视觉基线**：概览页 v7（commit `15ff2b1`）· 24 张 Pencil mockup 已废弃转真代码
>
> **⚠️ 写新页面前必读**：
> 1. `docs/13-frontend-design.md` —— 数据表达 + 交互 + 组件用法（**从概览页 v7 沉淀，是硬约束**）
> 2. `docs/13-frontend-design.md` —— 品牌色系 / 字号 / 阴影 token（外观规范）
> 3. `CLAUDE.md §12` —— 术语双分离铁律
>
> **核心原则**（`CLAUDE.md §12`）：
> - **不出现内部术语**（`housepool` / `provider` / `record group` / `initiated`）
> - **状态只显示 2-3 态**（"活" / "已失效"，不显示 `preparing/live/dying`）
> - **参考实现**：所有其他页面都以 `Overview.tsx` 为节奏基准，不重造轮子

## 导航形态（`decisions §8.2`）

**顶栏 5 tab · 无侧栏**（侧栏方案已推翻）：

```
[K logo]  概览 · 拼车 · 提取 key · 我的发车 · 对接文档     [上游库存] [积分] [🔔] [头像▾]
```

- **左对齐** tab（logo 紧邻，非居中）
- **右侧 4 元素**：上游库存 badge · 积分 pill（绿色）· 通知铃铛 · 头像 dropdown
- **头像 dropdown**：用户名（→ `/me`）· 设置（→ `/settings`）· 语言 · 主题 · 登出
  - 「设置」是**主入口**（索引页），三类设置是它的**下级**，不在 dropdown 里跟它并列
  - 「我的」在 `/me` —— 账号不是一种设置，且跟 `GET /api/me` 同名
  - 设置页的面包屑「设置」链回 `/settings`

## 路由树

```
/                              → 概览（数据看板，登录后跳）
/login                         → 登录（未登录跳）
/register                      → 注册

/buses                         → 拼车 · 车列表（tab 主页）
/buses/new                     → 建车（模态）
/buses/:id                     → 车详情（tab: 号列表 / 拉号历史 / 补车策略 / 成员 / 危险区）

/extract                       → 提取 key · 主动作 + 提取记录
/extract/assign                → 派去向（模态：进车 / 推我的号池 / 拿走）

/dispatch                      → 我的发车（阶段 3 空态占位）
/docs                          → 对接文档（静态帮助页）

/wallet                        → 钱包 · 余额 + 充值 + 兑换 + 流水（积分 pill 点击进入）
/me                            → 我的 · 邮箱 / 改密码 / 登出（**不是**设置的下级）

/settings                      → 设置索引 · 四张卡带实时状态（主入口）
/settings/preferences          → 设置 › 拉号偏好（**全局每日上限** + 新车默认值 · §8.27）
/settings/downstream           → 设置 › 我的号池（passengerpool 配置）
/settings/webhook              → 设置 › 机器人通知（webhook 配置 + 投递记录）
/settings/api-keys             → 设置 › API key 管理
```

**共 16 条路由**（+ `/settings/profile` → `/me` 兼容跳转）。

**层级铁律**：
- `/me` 是**账号本身**，不挂在 `/settings` 下 —— 账号不是一种设置
- `/settings` 是**真索引页**，不是某个具体设置页的别名。任何「设置」入口都指向它
- **配置归属规则**（`decisions §8.27`）：
  - **跨对象复用的** → 设置（全局每日上限 / 新车默认值 / 号池 / webhook / API key）
  - **跟单个对象绑的** → 那个对象自己的页面（每车补车策略、车成员都在车详情里，`§8.6`）
- 所以**不做** `/settings/strategy`（那是每车的），但**要做** `/settings/preferences`（那是全局的）

## 概览页结构（`/`）

**全页时间维度控制器**在 Hero 右侧：`今日 / 7 天 / 30 天 / 90 天 / 全部` —— 切换后下方所有数据（KPI / 业务卡 / 趋势 / Vendor / 活动）跟着变。

1. **Hero** — 「概览」+ 副标 + 时间切换
2. **4 KPI 卡** — 总余额（focal 光晕）· 今日消费 · 累计拉号 · 活跃号；每卡右上角 icon
3. **3 业务卡**（等高）— 拼车 / 提取 key / 我的发车
   - 各含：主数字 · 分布堆叠条 · 明细列表 · 底部汇总行
   - 我的发车为阶段 3 灰卡占位
4. **使用趋势** — 全宽 · 曲线图 + 渐变面积 · 维度切换（消耗 / 拉号 / 寿命）
5. **Vendor 行**（两卡等高）— 左：监测表（单价/寿命/有效成本/存活率/今日拉/fallback）· 右：占比环形图
6. **活动记录** — 全宽裸列表（无卡片外壳，hairline 分隔）

## 通用组件（`src/components/`）

**布局**
- `<AppLayout>` · 顶栏（5 tab + 右侧 4 元素）+ 内容区
- `<AuthLayout>` · 登录/注册居中卡片

**数据展示**
- `<KpiCard>` · 数据卡（label + 右上 icon + 大数字 + 单位基线对齐 + 副标）
- `<TrendChart>` · 曲线图（贝塞尔平滑 + 渐变面积）+ 维度切换
- `<DonutChart>` · 环形占比图（中心数字 + 图例带百分比）
- `<DistributionBar>` · 堆叠横条（业务卡内的号池/去向分布）
- `<DataTable>` · 表格（表头/数据居中对齐，末列居右）
- `<TimeRangePicker>` · 全页时间维度切换

**业务**
- `<BusCard>` · 车卡片（车名 / kind / 号数 / 状态 / 寿命 / 今日消费）
- `<CredentialRow>` · 号行（vendor / pulled_at / 状态 / 用量进度条 / 寿命）
- `<ActivityRow>` · 活动行（时间 / tag / 描述 / 金额）
- `<VendorTag>` · vendor 展示名（`91kiro` → "Kiro Market"，映射见下）
- `<StockBadge>` · 上游库存（header · 绿点 + 可拉号数）
- `<CreditPill>` · 积分余额（header · **绿色系** `#E8F7EF` / `#1F7A47`）

**通用**
- `<StatusBadge>` · 状态徽章（**只支持 2-3 态**）
- `<PriceBreakdown>` · 消费明细（号价 / 单次议价 / 服务费；**通道费只在充值页出现**）
- `<HandoffModal>` · handoff 两步（警告确认 → 明文一次性展示）
- `<CopyButton>` · 明文复制（API key / handoff / webhook secret）
- `<Pager>` · 分页
- `<Toaster>` · 全局 toast（success / info / warning / error）

## 页面详解

### 1. `/login` · 登录

**布局**：`<AuthLayout>` 居中卡片。

**字段**：
- Email / 用户名
- 密码
- （可选）"记住我" 30 天

**动作**：
- 登录 → POST `/api/login` → session cookie → 跳 `/`
- "忘记密码"链接 → 显示"阶段 3+ 支持" tooltip（暂无端点）
- "去注册"链接 → `/register`

**mock 行为**：任何邮箱 + 密码"1234" 都通过。

---

### 2. `/register` · 注册

**布局**：`<AuthLayout>`。

**字段**：
- 邮箱
- 用户名
- 密码 + 确认密码（前端 zod 校验强度：≥8 位 + 含字母和数字）
- **专属邀请码 / 好友邀请码**（选填 · 一次填一个）:
  - **专属邀请码**(内部 `system_invite_code`) → 永久绑账号 · 按 tier 影响价格/可见性:
    · `community` 档 → 加价栈跳 zone 减免;不看 vendor 真名
    · `wholesale` 档 → 加价栈跳 vendor+zone 减免;**只有它能看 vendor 真名**
    · UI 只出 chip"社群成员" · 不暴露 community/wholesale
  - **好友邀请码**(内部 `personal_invite_code`) → 建 referral · 收益给发码人手续费/通道费减免额度 · **不改被邀请人 tier · 不看 vendor 真名**
- **命名铁律**(`CLAUDE §1.2` / `decisions §8.42` 四码分离):
  · 注册时 = **邀请码**(专属 or 好友 · 单一入口·填哪个系统自己识别码类型)
  · 支付 / 提号窗里那个叫**优惠码**(内部 `coupon_code`)
  · 加入某辆车叫**拼车码**(内部 `bus.invite_code`) · **绝不叫邀请码**
  · UI 上不许混用

**动作**：
- 注册 → POST `/api/register` → 自动登录 → 跳 `/`
- 校验只在**失焦或提交后**报错，不边打字边红

---

### 3. `/` · 概览

**布局**：`<AppLayout>`。**定位：数据看板，不做操作**（车列表在 `/buses`）。

结构见上文「概览页结构」。要点：

- **时间维度全页控制** · Hero 右侧 `<TimeRangePicker>`
- **4 KPI**：总余额（光晕 focal）/ 今日消费 / 累计拉号 / 活跃号
- **3 业务卡等高**：拼车（号池分布 3 车）/ 提取 key（去向分布 4 类）/ 我的发车（阶段 3 灰卡）
- **趋势图**全宽曲线 · **Vendor 监测表 + 占比环形图**两卡等高 · **活动记录**裸列表

**API**：
- `GET /api/me/wallet` · 余额
- `GET /api/me/overview?range=30d` · KPI + 业务汇总（**待补端点**）
- `GET /api/me/trend?range=30d&metric=credits` · 趋势序列（**待补端点**）
- `GET /api/vendors/stats` · Vendor 监测 + 占比（**待补端点**）
- `GET /api/me/activities?range=30d` · 活动记录（**待补端点**）
- `GET /api/vendors/stock` · 上游库存 badge（**待补端点**）

**颜色约定**：
- 积分/余额 → **绿色系**（`$credit-bg` / `$credit-fg`）
- 品牌紫留给导航高亮 + focal 卡 + 主 CTA
- 分布图配色用**同色系深浅**（紫 `#9147FF` → `#A574FF` → `#C9A9FF` → `#E3D5FF`），不用蓝/黄/橙杂色

---

### 4. `/buses/new` · 建车

**字段**：
- 车名（必填）
- kind（阶段 1a 只有 `single`；1c 加 `anon` 撮合 · 用户建的车一律带**拼车码**·不区分 single/team UI）
- （anon）单价上限 / 最大成员数（1c 才启用）
- **kind 不对外**：UI 只让用户"建一辆车"·**是否多人是状态·不是类型**（`CLAUDE §1.2`）

**动作**：
- 建车 → POST `/api/me/buses` → 跳车详情

---

### 5. `/buses/:id` · 车详情

**布局**：`<AppLayout>`。

**顶部**：
- 车名 · kind · 创建时间 · 成员数 · 号数（活 / 失效） · **"拉号"主按钮**

**Tab 切换**：

**Tab A · 号列表**（默认）：
- `<CredentialRow>` 表：credential prefix（前 8 字符）· vendor · pulled_at · 状态（"活" / "已失效"） · 24h 调用 · 总消耗积分 · "详情"按钮
- 点"详情"展开：完整用量 + `concurrency_avg`（可能显示 `—`）

**Tab B · 拉号历史**：
- 表格：时间 · vendor · count · 参与人（谁分几个）· 号价 · 服务费 · 议价 · 通道费 · **总消费** · 状态
- 点行展开号明细

**Tab C · 补车策略**（`decisions §8.6` · 跟车绑，非全局设置）：
- `auto_refill_enabled` toggle（号死自动补）
- `refill_watermark` 水位线（活号低于 N 触发）
- `refill_min_count` 本轮最少拉 N 号；未设时按水位差额补齐
- `per_round_count` 每轮拉几号
- `max_unit_price` 单号最高价（积分）
- `daily_round_limit` / `daily_spend_limit` 只展示乘客全局每日限额；车级字段 deprecated，不做车详情入口
- `preferred_vendor` 指定 vendor（默认空 = 有效成本比价自动选）

**Tab D · 成员**：
- 头像 · 用户名 · 加入时间 · "退出/移除"按钮
- **`member_count=1` 时显示「成员 1」= 自己**·**拼车码**永远显示(用户建的车一律有码)·可分享出去邀朋友

**Tab E · 危险区**：
- 解散车 → 二次确认 → 活号挪到你的提取记录，死号归档

**进行中状态**：`pull_round.status=initiated` 时 hero 下方显示「拉号中 · +N 号 · 已完成 x/y」banner

**动作**：
- "拉号"按钮 → 弹窗输 count + vendor 可选 → POST `/api/me/buses/{id}/pull`
- 解散 → 二次确认 → DELETE `/api/me/buses/{id}`

**API**：
- `GET /api/me/buses/{id}` · 基础信息
- `GET /api/me/buses/{id}/credentials`
- `GET /api/me/buses/{id}/pulls`
- `GET /api/me/buses/{id}/members`
- `GET /api/me/buses/{id}/stats` （1d）
- `PUT /api/me/buses/{id}/strategy` · 补车策略（**待补端点**）
- `POST /api/me/buses/{id}/pull`

---

### 6. `/pull` · 单独拉号

**布局**：`<AppLayout>`。

**字段**：
- vendor 选择（"让系统选便宜的" / 具体 6 家；1a 只启用 91kiro）
- count（1-200）

**"预估"块**：填 count 后实时算：
- 号价（vendor 侧单价 × count · pass-through）
- 单次议价（`count==1` 时 20% × 号价）
- 服务费（1 元）
- 小计
- 通道费 5% pass-through（充值时才实际收）

**动作**：
- 拉号 → POST `/api/me/pull` → toast + 跳 `/pull-records`

---

### 7. `/pull-records` · 拉号记录 + 派去向

**布局**：`<AppLayout>`。

**表格**：
- 选择框 · credential prefix · vendor · 拉号时间 · 状态（"待派" / "已进车" / "已推号池" / "已拿走" / "已失效"） · 操作

**批量操作**：
- 选中多行 → "派去向"按钮 → 弹窗

**派去向弹窗**：三个 Tab
- **进车**：选择哪辆 bus（下拉）→ 确认
- **推自己号池**：需要先在设置里配 passengerpool url + token；未配则灰化 + 引导链接
- **拿走 handoff**：确认 → 弹 `<HandoffModal>` 展示明文（**"这是唯一一次可见，请复制"**）

**动作**：
- POST `/api/me/pull-records/assign` `{ assignments: [...] }`

**API**：
- `GET /api/me/pull-records`
- `POST /api/me/pull-records/assign`

---

### 8. `/wallet` · 钱包

**布局**：`<AppLayout>`。

**上半**：
- 余额大数字 · reserved 冻结 · 累计充值 · 累计消费

**充值卡片**：
- 输金额 → 显示到账积分（预扣 5% 通道费 pass-through 说明）
- "生成充值单"按钮 → POST `/api/me/topup` → 显示 QR + 提示"扫码支付到 waffo"
- 充值历史小表

**兑换码卡片**：
- 输入框（可批量粘贴，一行一个 · 1b 才做批量）
- "兑换" → POST `/api/me/redeem` → toast

**流水表**：
- 分页 + type 筛选
- 展开每条：reason · amount · balance_after · ref · memo · 时间

**API**：
- `GET /api/me/wallet`
- `GET /api/me/ledger`
- `POST /api/me/topup`
- `POST /api/me/redeem`

---

### 9. `/dispatch` · 我的发车（阶段 3 空态）

**布局**：`<AppLayout>` · 顶栏「我的发车」高亮。

**内容**：hero 光晕 + 「阶段 3 开放」badge + 大标题 + 3 张 feature 卡（你的 AWS · 合规透明 · 寿命最长）+ 底部 hint。

**说明**：`decisions §8.3` — 顶栏保留占位 tab，结构定型不推翻，阶段 3b 直接填内容。

---

### 10. `/settings/downstream` · 设置 · 我的号池

**布局**：`<AppLayout>` · 面包屑「设置 › 我的号池」。**技术页**（允许出现 kiro.rs 等术语，类比对接文档）。

**3 状态卡**：连通状态（绿点） · 推送成功率 · 累计推送数

**kiro.rs 端点卡**（focal 光晕）：
- URL 输入 + "测试连接"
- Admin API Key（保存后打码 · 查看按钮）

**推送策略卡**：4 条规则 toggle
- 号进 bus 立即推（双写）
- 号死重推（同步删除）
- 失败自动重试（5s / 30s / 5min 退避）
- 仅拼车号推（关 = 拼车+提取都推）

**API**：
- `GET /api/me/downstream`
- `PUT /api/me/downstream/passengerpool`
- `POST /api/me/downstream/passengerpool/test`

---

### 10b. `/settings/webhook` · 机器人通知

**布局**：`<AppLayout>` · 面包屑「设置 › 机器人通知」。**技术页**。

**Webhook 端点卡**（focal 光晕）：
- URL 输入 + "发测试事件"
- Secret（HMAC 签名密钥 · 打码 + 复制 + 重新生成）
- 右上「启用中」绿 chip

**订阅事件卡**：5 个 event 卡带 toggle
- `round.completed` 拉号轮次完成
- `round.failed` 拉号失败
- `credential.dead` 号死了
- `bus.refilled` 补车触发
- `wallet.low` 余额低

**投递记录卡**：时间 · 成功/失败 chip · event · HTTP code · retry 次数 · 延迟 + 筛选

**API**：
- `PUT /api/me/downstream/webhook`
- `POST /api/me/downstream/webhook/test`
- `GET /api/me/downstream/webhook/deliveries`

---

### 11. `/settings/api-keys` · API key 管理

**表格**：
- prefix · 备注名 · 创建时间 · 最近使用 · 操作（"复制 prefix" · "吊销"）

**"新建"按钮** → 弹窗输名字 → 显示明文一次（用 `<CopyButton>`）+ 关闭后再也拿不到明文

**API**：
- `GET /api/me/api-keys`
- `POST /api/me/api-keys`
- `DELETE /api/me/api-keys/{id}`

---

### 12. `/me` · 我的

**不用 `SettingsHead`**（它不在设置下，没有「设置 ›」面包屑）· hero 直接叫「我的」。

**只读字段**：邮箱（带已验证 chip）· 用户名 · 注册时间 · 会员状态(绑了专属邀请码 = 显示"社群成员" chip · 不暴露 community/wholesale 内档)

右上角「退出登录」。

---

### 12b. `/settings` · 设置索引

**四张卡**，每张带**实时状态**（不是干巴巴的链接列表）：

| 卡 | 状态 chip | 副行 |
|---|---|---|
| **拉号偏好**（第一张） | 已设上限 / 未设上限 | 今日 N/上限 轮 · 花 N/上限 |
| 我的号池 | 已连通 / 未连通 | 推送成功率 + 累计次数 |
| 机器人通知 | 启用中 / 已停用 | 已订阅几个事件 |
| API key | N 个可用 / 还没建 | 共几个 + 已吊销几个 |

拉号偏好排第一：四个里只有它会**拦下操作**，其余是"连了/没连"性质。

整卡可点（`<Card to=...>` 自带 hover 悬浮）· 底部一行说明每车策略跟车绑、账号在 `/me`。

---

### 12c. `/settings/preferences` · 拉号偏好

**全局策略**（`decisions §8.27` · `06-db-schema §16` · API `GET/PUT /api/me/strategy`）。

**两块分开摆**，因为语义不同（混一起会让人以为改默认值就改了上限）：

**① 拉号上限** —— 会真的拦下操作
- **单价上限**「单价超过 N 就不拉」· **每次都判** · 单独一行，跟下面两个分隔线隔开
  - **手动拉号也拦**：提取确认窗超了 → 禁用确认按钮 + 「现在 26 / 上限 5 / 超了 21」+ 「去拉号偏好调高上限」链接
  - 判**优惠码折后价**（用码压到线内就放行）
  - 不给「就这次放行」的口子 —— 要放行就去改上限
- **每天最多拉几轮 / 每天最多花多少** · 跨所有车累加 · **单独提取 key 只受这个管**
  - 每项下面一条今日用量进度条：<80% 绿 · ≥80% 黄 · 满 红 +「已拉满」
  - **不限时不画进度条** —— 没有分母，画了是假的
- 三项都跟车级同名字段是 **AND**（取更严的）

**② 新车默认值** —— 只预填新车，改它不动已有的车
- 每次拉几个 / 默认来源（含"让系统比价"）/ 默认区域

**API**：`GET /api/me/strategy` · `PUT /api/me/strategy`

**enforcement 落点**（前端）：`ExtractConfirmModal`（禁用确认）· `PullNowModal`（hint 显示生效上限 + 来源标注）。真正的拦在后端 —— 前端只是别让用户白点。

**改密码块**（**阶段 1b 才后端支持** · 前端表单先做 mock）：
- 旧密码 + 新密码 + 确认

**"忘记密码"**：显示"阶段 3+ 支持"

**API**：
- `GET /api/me/profile`
- `POST /api/me/password` （1b 后端骨架 501，1a 前端 mock 通）

---

## 状态展示规则（复述）

跟 `CLAUDE.md §12.5` 一致。前端**只显示这些状态**：

| 实体 | 显示 |
|---|---|
| credential | "活" / "已失效" |
| bus | "活跃" / "已解散" |
| pull_intent | "拉号中" / "完成" / "失败" |
| pull_round | "成功" / "部分成功" / "失败" / "已退款" |
| payment_order | "待付款" / "已到账" / "失败" |
| webhook 投递 | "成功" / "失败"（+ 重试次数） |

**内部枚举**（`initiated` / `reserved` / `purchased` / `imported` / `completed` / `preparing` / `standby` / `dying` 等）**不直接展示** —— 后端 API 返回时映射成用户可见态。

## Vendor 展示名映射

| 内部 vendor_id | 显示名 |
|---|---|
| `91kiro` | Kiro Market |
| `kiroceo` | Kiro CEO |
| `kirooo` | Kiro OOO |
| `kiroappio` | Kiro App IO |
| `kiroappcc` | Kiro App CC |
| `kirodrop` | Kiro Drop |

前端 `<VendorTag>` 组件封装此映射，UI 只显示 display name。**「Vendor」在标题里首字母大写**（如「Vendor 监测」「Vendor 占比」），不叫「比价」。

## 视觉规范（`decisions §8.15 / §8.16` + mockup 定型）

**字号 5 档**：
| 档 | size | 用法 |
|---|---|---|
| 主 | 36 | 每页 hero title 唯一一档 |
| 数字 | 32 | KPI / focal 数字 |
| 副 | 20 | section 标题 / 卡片标题 |
| Body | 13-14 | 表格 / 列表 / label |
| Micro | 11-12 | hint / 时间 / tag |

**例外**：钱包 hero 余额可用 48（唯一 giant）。

**卡片**：
- 圆角 20（普通）/ 24（focal 大卡）
- padding 24-28
- 默认：`$bg` + `$hairline` 边 + 阴影 `0/2/8 #0A0A0A08`
- focal：加径向紫光晕（右上角）+ `$brand-hairline` 边
- hover：上浮 4px + 紫 tinted 阴影 `0/12/32 #9147FF33`

**间距**：section 之间 56 · 卡片之间 24 · 页边距 96

**列表**（活动记录 / 拉号记录）：**不用卡片外壳**，裸列表 + hairline 分隔行

**配色**：
- 积分 / 余额 → 绿色系 `$credit-bg` `$credit-fg`
- 品牌紫 → 导航高亮 / focal 卡 / 主 CTA
- 分布图 → 同色系深浅（`#9147FF` → `#A574FF` → `#C9A9FF` → `#E3D5FF`），**不用蓝/黄/橙杂色**
- 状态色：活 `$success` · 死 `$danger` · 部分 `$tag-partial-*`

**表格对齐**：首列左对齐 · 中间数据列**居中** · 末列**居右**

## i18n 词条（1a 只做中文）

- `web/src/i18n/zh-CN.json` 存全部文案
- 按页面分 namespace：`login.title` / `bus.detail.tab.credentials` / ...
- **阶段 1 只中文**；阶段 3+ 加英文时不改代码

## 术语审查（`sprint-1a-frontend.md Iss #F13`）

**每次 PR 都要**：`grep -rE 'housepool|provider|adapter|record group|initiated|imported|handed_off' web/src/` **必须 0 命中**。

有命中 = 违反 `CLAUDE.md §12` = review 打回。

## API 端点 · 已全部进契约 ✅

原来这节叫「待补 API 端点」，列了 6 个 mockup 画了但契约没定义的。**2026-08-08 已全部补进 `05-api-contract.md` 并同步到 `sprint-1a-backend.md` 的端点矩阵**（`decisions §8.28`），不再是"待补"。

前端实际调用的完整清单以 `web/src/api/hooks.ts` 为准。几条**命名铁律**（改过，别用旧的）：

| 用 | 不用 |
|---|---|
| `GET /api/me` | ~~`/api/me/profile`~~ |
| `/api/vendors/*`（公共数据 · 定价由服务端按调用者个性化） | ~~`/api/me/vendors/*`~~ |
| `/api/me/pull` `/api/me/pull/estimate` `/api/me/pull/events` | ~~`/api/me/extract*`~~ |
| `POST /api/me/handoff` + `GET /handoff/{token}` + `POST /handoff/{token}/confirm` | ~~`assign` 里带 `destination:"handoff"`~~ |
| `GET /api/me/buses/{id}` 返回内嵌 `members[]` | ~~`GET /api/me/buses/{id}/members`~~ |

**拿走是三段式**（`09-transactions §4`）：界面上只有两步（点「下载拿走」→ 看明文 → 点「我已保存」），但底层是 ①发token ②取明文 ③confirm 才删号。**点「返回」不 confirm，号留在池里可以重来** —— 这正是三段式存在的意义，改这块前先读 `decisions §8.28`。

---

## 落地状态（阶段 1a 前端）

**14 个路由全部实现，无 stub**。共用组件（别各页重写）：

| 组件 | 用途 |
|---|---|
| `components/SettingsHead.tsx` | 4 个设置页的头（面包屑 + hero + 右侧动作）· 间距统一在这里 |
| `components/ui/secret-field.tsx` | 打码密钥展示 + 复制 · 号池 admin key / webhook secret / 新建 api key 共用 |
| `components/ui/code-block.tsx` | 对接文档的代码块 + 复制（不上语法高亮，KISS） |
| `primitives.tsx` 的 `<Em>` | **描述里的重点数字/名字唯一写法** · 见 `13-design-principles §5.2` |
| `primitives.tsx` 的 `<SectionHead>` | **卡片标题 + 副标题唯一写法** · 见 `§5.2b` |

**通道费只在 `/wallet` 充值卡出现**（`decisions §8.21`）· 费率是 `lib/utils.ts` 的 `CHANNEL_FEE_RATE` 单一来源，`topupBreakdown()` 算拆分。

**技术页例外**（允许内部术语 kiro.rs / credential / event id）：`/docs` · `/settings/downstream` · `/settings/webhook`。其余页面走 `CLAUDE.md §12.6` 人话规则。

**mock 已知局限**：`fixtures.ts` 的 `vl()` 在模块加载时求值，所以注册填邀请码后，**已烘进 memo 字符串**的 vendor 名仍是 `Vendor 0N`（真实后端在响应时打标签，不会有这问题）。新渲染的 vendor 名会正确切成真名。

**登录 / 注册 / 退出后必须清 query 缓存**，否则拿着上一个身份的 `["me"]` 渲染（症状：带邀请码注册完 vendor 还显示编号）。用 `qc.removeQueries()` 而**不是** `qc.clear()` —— 后者连 mutation 缓存一起清，`await mutateAsync()` 永不 resolve，跳转不执行。

---

## 未来页面（不在 Sprint 1a）

- `/market` · 市场（3d）
- `/stats` · 数据看板（3a · 概览页的深化版）
- `/admin/*` · 管理端（3+）

`/dispatch` 我的发车已在 1a 画空态占位（`decisions §8.3`），阶段 3b/3c 填内容。
