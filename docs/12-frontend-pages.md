# bus-pooling · 前端页面清单 + 路由

> 前置：`04-scenarios.md` · `05-api-contract.md` · `sprint-1a-frontend.md`
>
> **12 页面 + 2 layout** 覆盖阶段 1 全部乘客侧需求。管理端阶段 3+ 单独开文档。
>
> **原则**（`CLAUDE.md §12`）：
> - **不出现内部术语**（`housepool` / `provider` / `record group` / `initiated`）
> - **状态只显示 2-3 态**（"活" / "已失效"，不显示 `preparing/live/dying`）

## 路由树

```
/                              → 首页 / 仪表盘（登录后跳）
/login                         → 登录（未登录跳）
/register                      → 注册
/buses                         → 我的车列表（首页一部分，独立页可选）
/buses/new                     → 建车
/buses/:id                     → 车详情（tab: 号列表 / 拉号历史 / 成员 / 统计）
/pull                          → 单独拉号
/pull-records                  → 拉号记录列表 + 派去向
/wallet                        → 钱包 · 余额 + 充值 + 兑换 + 流水（可拆两页，1a 合并）
/settings/strategy             → 策略参数配置
/settings/downstream           → 下游配置（passengerpool + 我方 webhook）
/settings/api-keys             → API key 管理
/settings/profile              → 账号 · 邮箱 / 改密码（简化版）
```

**共 13 条路由 · 12 独立页面**（`/` 是仪表盘）。

## 通用组件（`src/components/`）

- `<AppLayout>` · 侧栏 + 顶栏（余额 + 头像）
- `<AuthLayout>` · 登录/注册居中卡片
- `<BusCard>` · 车卡片（车名 / kind / 成员数 / 号数 / 状态）
- `<CredentialRow>` · 号行（credential 简版 / vendor icon / pulled_at / 状态 / 用量）
- `<StatusBadge>` · 通用状态徽章（**只支持 2-3 态**）
- `<PriceBreakdown>` · 展开一次消费的 4 项组成（号价 / 单次议价 / 服务费 / 通道费）
- `<HandoffModal>` · handoff 明文展示模态（一次性、复制按钮）
- `<VendorTag>` · vendor 展示名（`91kiro` → "Kiro Market"，映射见 `CLAUDE.md §12.5`）
- `<CopyButton>` · 明文复制（用于 API key / handoff / webhook secret）
- `<Pager>` · 分页
- `<Toaster>` · 全局 toast

## 12 页详解

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
- 密码 + 确认密码（前端 zod 校验强度）

**动作**：
- 注册 → POST `/api/register` → 自动登录 → 跳 `/`

---

### 3. `/` · 首页 / 仪表盘

**布局**：`<AppLayout>`。

**上半部**：
- 余额卡片（`<PriceBreakdown>` 概览：可用 / 冻结 / 总）
- 快捷按钮：建车 · 单独拉号 · 充值

**中部**：
- **我的车列表**（`<BusCard>` × N）
  - 每卡片：车名 · single/anon/team 徽章 · 成员 3/5 · 号 8 活 2 失效 · "进入"按钮
- 空态：引导"建你的第一辆车"

**下部**：
- **最近拉号 5 条**（跨 bus）—— 每条：时间 · vendor · count · 花费

**API**：
- `GET /api/me/wallet` · 余额
- `GET /api/me/buses` · 车列表
- （近期拉号列表 API 待补 · MSW mock）

---

### 4. `/buses/new` · 建车

**字段**：
- 车名（必填）
- kind（阶段 1a 只有 `single`；1c 加 `anon`；2a 加 `team` · 灰化未启用的）
- （anon）单价上限 / 最大成员数（1c 才启用）
- （team）邀请码提示（2a 才启用）

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

**Tab C · 成员**：
- 头像 · 用户名 · 加入时间 · "退出/移除"按钮（1 人 bus 不显示；多人 bus 是 1c）

**Tab D · 统计**（1d 数据成熟才显示）：
- 24h / 7d / 30d 窗口切换
- 图表：调用趋势 · 号存活分布 · 平均寿命

**动作**：
- "拉号"按钮 → 弹窗输 count + vendor 可选 → POST `/api/me/buses/{id}/pull`
- 解散 → 二次确认 → DELETE `/api/me/buses/{id}`

**API**：
- `GET /api/me/buses/{id}` · 基础信息
- `GET /api/me/buses/{id}/credentials`
- `GET /api/me/buses/{id}/pulls`
- `GET /api/me/buses/{id}/members`
- `GET /api/me/buses/{id}/stats` （1d）
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

### 9. `/settings/strategy` · 策略参数

**字段**：
- Toggle：auto_enabled
- per_round_count（数字）
- min_count（数字）
- keep_safety_stock（数字）
- max_unit_price（数字 · microunit UI 转元）
- daily_round_limit（数字）
- daily_spend_limit（数字 · microunit UI 转元）
- target_bus_id（下拉 · 从我的车里选）

**说明区**：
- "auto_enabled 开启后系统按参数自动拉号补车"
- **阶段 1a 后端不用这些参数**，只存表；1d 才真的按参数触发

**API**：
- `GET /api/me/strategy`
- `PUT /api/me/strategy`

---

### 10. `/settings/downstream` · 下游配置

**passengerpool 卡片**：
- URL 输入
- Token 输入（保存后打码 · 每次编辑要重新输）
- 保存 → PUT `/api/me/downstream/passengerpool`

**我方 webhook 卡片**：
- URL 输入
- "保存并生成 secret" → PUT `/api/me/downstream/webhook` → 显示 secret 一次
- "发测试" 按钮 → POST `/api/me/downstream/webhook/test`
- 投递日志表（`GET /api/me/downstream/webhook/deliveries`）

**API**：
- `GET /api/me/downstream`
- `PUT /api/me/downstream/passengerpool`
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

### 12. `/settings/profile` · 账号资料

**只读字段**：邮箱 · 用户名 · 注册时间

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
| `kiroappio` | Kiroapp.io |
| `kiroappcc` | Kiroapp.cc |
| `kirodrop` | Kiro Drop |

前端 `<VendorTag>` 组件封装此映射，UI 只显示 display name。

## i18n 词条（1a 只做中文）

- `web/src/i18n/zh-CN.json` 存全部文案
- 按页面分 namespace：`login.title` / `bus.detail.tab.credentials` / ...
- **阶段 1 只中文**；阶段 3+ 加英文时不改代码

## 术语审查（`sprint-1a-frontend.md Iss #F13`）

**每次 PR 都要**：`grep -rE 'housepool|provider|adapter|record group|initiated|imported|handed_off' web/src/` **必须 0 命中**。

有命中 = 违反 `CLAUDE.md §12` = review 打回。

## 未来页面（不在 Sprint 1a）

- `/market` · 市场（3d）
- `/dispatch` · 发车 · 上传 AWS（3b/3c）
- `/stats` · 数据看板（3a）
- `/admin/*` · 管理端（3+）
