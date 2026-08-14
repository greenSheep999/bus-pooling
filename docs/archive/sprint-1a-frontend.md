# Sprint 1a · 前端骨架 + 定跳转（前端优先）

> 前置：`04-scenarios.md` · `05-api-contract.md` · `12-frontend-pages.md`
>
> **Sprint 1a 目标**：**用户能点着玩的前端 demo**。12 个页面、路由跳转通、mock 数据、无需 Go 后端。定型后再开 Sprint 1b 后端。
>
> **理由**：旧项目 `kiro-auto` 的教训 —— 后端 90+ 模块造完，前端跟着做一堆没人用的页 → 复杂度失控。**前端优先 = UI 决定必要功能，用户看不到的就不做**。

## 完成标准（Definition of Done）

**能点着走完 4 条主流程**：

1. **注册 → 登录 → 首页** —— 看到余额（假数据）+ 车列表（假的 3 辆）
2. **建 1 人 bus → 拉 5 号 → 车详情看到 5 个号**（都是 mock）
3. **单独拉 3 号 → 拉号记录页看到 3 行 → 派 1 号进车 · 1 号推自己号池 · 1 号拿走**（拿走时前端提示"这是唯一一次展示明文，请复制"）
4. **策略页 / 下游配置页 / API key 页** 都能进，表单能填，保存 mock 返回成功

**不要求**：真实 API 调用（用 MSW mock）· Go 后端 · 生产打包 · 单测覆盖率。

**用户 review UI 后**：把用户点着觉得"多余 / 缺" 的**页面 / 端点 / 字段**记录到 `decisions.md` 或 `05-api-contract.md`，**Sprint 1b 后端就砍 / 补对应的**。

## Dev Loop（前端热更新，秒级刷新）

```
终端：bun dev
  ↓
localhost:3000（Vite HMR + MSW 拦截 fetch）
  ↓
浏览器打开 :3000
  ─ 改 .tsx / .css → HMR 秒刷
  ─ MSW 返回假数据（响应形态照 05-api-contract.md）
  ─ Go 后端根本没启动
```

**改前端一个文件 → 秒级刷新**。**Go 完全不参与**。

## 技术栈

| 用途 | 选型 | 版本约束 |
|---|---|---|
| 运行时 | **Bun** | ≥ 1.1 |
| 框架 | **React** + **TypeScript** | React 18+, TS 5+ |
| 打包 dev server | **Vite** | ≥ 5 |
| 样式 | **Tailwind CSS** | ≥ 3.4 |
| 组件库 | **shadcn/ui**（复制粘贴式，非依赖） | latest |
| 路由 | **React Router** | v6 |
| 状态 | React Query（server state）+ Zustand（少量 client state） | latest |
| Mock | **MSW (Mock Service Worker)** | ≥ 2 |
| Form | **react-hook-form** + **zod** | latest |
| Table | **@tanstack/react-table** | latest |
| 图标 | **Lucide** | latest |
| i18n | **i18next** + **react-i18next** | latest（阶段 1a 只做中文） |

**选型理由**（跟 `decisions.md §1.9` 一致 · Go 自建登录 + 前端最简技术栈）：
- 熟悉度：跟旧项目 kiro-auto 前端一致，无学习成本
- 无外部依赖：shadcn/ui 不是 npm 包，代码进项目自己维护
- 状态收敛：React Query 管服务端，Zustand 管少量客户端（避免 Redux 复杂度）
- Mock：MSW 拦截 fetch，dev 环境完全独立于 Go

**不用**：
- Next.js / Remix（SSR 不需要，纯 SPA 更简单）
- Redux（React Query 够了）
- Vue / Svelte / SolidJS（团队熟悉度）

## 目录布局

```
web/
├── package.json
├── vite.config.ts            proxy /api → localhost:8080（后端来了才用）
├── tsconfig.json
├── tailwind.config.ts
├── postcss.config.js
├── index.html
├── src/
│   ├── main.tsx              React 入口 + Router + QueryClient + MSW
│   ├── App.tsx               根组件（Layout 挂路由）
│   ├── layouts/
│   │   ├── AppLayout.tsx     侧栏 + 顶栏 + Outlet
│   │   └── AuthLayout.tsx    登录/注册页外壳
│   ├── pages/                见 §12-frontend-pages.md · 12 页
│   ├── components/
│   │   ├── ui/               shadcn/ui 复制来的
│   │   ├── BusCard.tsx
│   │   ├── CredentialRow.tsx
│   │   └── ...
│   ├── api/                  API client（fetch wrapper + hooks）
│   │   ├── client.ts
│   │   ├── passenger.ts
│   │   ├── wallet.ts
│   │   ├── bus.ts
│   │   ├── pull.ts
│   │   └── ...
│   ├── mocks/                MSW handlers
│   │   ├── handlers.ts
│   │   ├── fixtures/
│   │   │   ├── passenger.ts
│   │   │   ├── wallet.ts
│   │   │   ├── bus.ts
│   │   │   ├── credentials.ts
│   │   │   └── ...
│   │   └── browser.ts
│   ├── i18n/
│   │   └── zh-CN.json
│   ├── router.tsx            路由树
│   └── types/                TypeScript types from 05-api-contract.md
└── public/
    └── mockServiceWorker.js  MSW init
```

## Issue 拆分（估工时 = 单人纯开发；不含 review / 联调）

### Iss #F1 · 前端脚手架 · 0.5 天
- `bun create vite web -- --template react-ts`
- 装 Tailwind + shadcn/ui 初始化
- Router + QueryClient + MSW 初始化
- 空 App.tsx 能启动看到 "hello"
- **DoD**：`bun dev` 起 localhost:3000，改 App.tsx 秒刷

### Iss #F2 · Layout + Router · 0.5 天
- `AppLayout` 侧栏（12 页导航）+ 顶栏（余额 + 头像）
- `AuthLayout`（登录 / 注册居中卡片）
- 路由树（`router.tsx`）
- 所有 12 页占位组件（`<div>PageName</div>`）
- **DoD**：点侧栏能跳到每一页

### Iss #F3 · MSW handlers + fixtures · 1 天
- 按 `05-api-contract.md` 每个端点写一个 handler
- fixtures：假 passenger / wallet / 3 辆 bus / 20 个 credential / 5 条 pull_record / 3 条 topup / 10 条 ledger
- **DoD**：F4 之后每页 fetch 都能拿到假数据

### Iss #F4 · 登录 / 注册页 · 0.5 天
- 表单：邮箱 + 密码（`react-hook-form` + `zod`）
- MSW 假登录（存 mock session）
- 跳转到首页
- **DoD**：注册 → 登录 → 跳首页流程通

### Iss #F5 · 首页 / 仪表盘 · 1 天
- 顶部：余额 + 快捷按钮（建车 / 单独拉号 / 充值）
- 中部：我的车列表（3 辆，卡片形式）
- 底部：最近拉号历史 5 条
- **DoD**：点每张车卡片跳车详情页

### Iss #F6 · 建车页 + 车详情页 · 1 天
- 建车表单：名字 + kind（阶段 1a 只有 single）
- 车详情：顶部车信息 + Tab（成员 / 号列表 / 拉号历史 / 统计）
- 号列表：每行显示 credential_id / vendor / pulled_at / 状态（"活" / "已失效"）/ 用量占位
- "拉号"按钮 → 弹窗 → 输 count → 触发 mock purchase
- **DoD**：建 1 人 bus → 拉 5 号 → 车里看到 5 号

### Iss #F7 · 单独拉号页 + 拉号记录页 · 1 天
- 单独拉号表单：vendor 选择 + count
- 拉号记录列表：每行 credential + 状态 + 拉号时间
- 选中多行 → 派去向弹窗
- **DoD**：单独拉 3 号 → 记录页 3 行

### Iss #F8 · 派去向弹窗 + handoff · 1 天
- 弹窗内三个选项卡：进车 / 推自己号池 / 拿走
- 进车：选择 bus
- 推：直接触发（前提是配了 passengerpool url）
- 拿走：确认对话框 → 显示明文 4 件套（**强调"仅这一次可见"**）+ 复制按钮
- **DoD**：3 号分别派 3 个去向都能操作

### Iss #F9 · 钱包页（充值 + 兑换 + 流水） · 0.5 天
- 充值按钮：输金额 → 显示假 QR + waffo 提示（**通道费 5% pass-through** 说明）
- 兑换码：输入 → 提交 → 显示到账积分
- 流水表：分页 + 按 type 筛选
- **DoD**：3 种操作都能跑通

### Iss #F10 · 策略页 · 0.5 天
- 表单：`auto_enabled` toggle + `per_round_count` + `max_unit_price` + `daily_round_limit` + `daily_spend_limit` + `target_bus_id`
- 保存按钮 → mock PUT
- **DoD**：填表保存看到 toast

### Iss #F11 · 下游配置页（passengerpool + webhook） · 0.5 天
- passengerpool url + token 表单（token 输入型改成"保存后打码显示"）
- webhook url + secret（保存后显示 secret 供复制）
- 测试 webhook 按钮
- **DoD**：三个表单都能保存 + toast

### Iss #F12 · API key 页 · 0.5 天
- 表格：现有 keys（prefix + 备注 + created_at + last_used）
- 新建按钮 → 弹窗输名字 → 显示明文一次 + 复制
- 吊销按钮 → 确认 → 灰化
- **DoD**：create + revoke 都能跑

### Iss #F13 · 状态收敛 + 术语审查 · 0.5 天
- 通读所有页面文案 + API mock 响应
- 检查 `CLAUDE.md §12` 违反项：
  - 内部术语（`housepool` / `provider` / `adapter` / `record group`）0 命中
  - 内部状态枚举（`initiated` / `imported` / `handed_off`）0 命中；替换成"拉号中"/"成功"/"活"/"已失效"等
- **DoD**：grep 全 codebase 无内部术语

### Iss #F14 · 用户 Review 会话 · 0.5 天（**你亲自参与**）
- 部署到本地或临时公网（`bun run build` → nginx / caddy 挂）
- **你点着走 4 条主流程**
- 记录：**多余的页 / 缺的字段 / 反直觉的跳转 / 命名不顺**
- 输出到 `decisions.md`（如果需要否决某个"未来功能"）或 `05-api-contract.md`（如果 API 契约要改）
- **DoD**：Review 清单归档

### 总工时估算

约 **9.5 天纯前端开发**（不含 review 之后的调整）。单人节奏约 **2 周**。

**关键路径**：`#F1 → #F2 → #F3 → #F4 → #F5 → #F6 → #F14`（约 5 天）。**#F7-F12 可并行**（多个页面独立开发）。

## 依赖图

```
#F1 骨架  →  #F2 Layout+Router  →  #F3 MSW handlers
                                        ↓
                             #F4 登录  →  #F5 首页
                                             ↓
                             ┌──────────┬────┴────┬────────────┐
                             ↓          ↓         ↓            ↓
                         #F6 建车     #F7 单独   #F9 钱包    #F10 策略
                                       拉号
                             │          │
                             └──────┬───┘
                                    ↓
                                #F8 派去向
                                    ↓
                            (#F11 下游 + #F12 API key 都是并行)
                                    ↓
                             #F13 状态收敛审查
                                    ↓
                             #F14 用户 review
```

## 交付验收

> **状态图例**：✅ done · ⚙️ code-complete 但缺依赖 · ⏸ 待
>
> 最后同步：commit `2eff1ff`

- [x] ✅ 16 页都能进（12 页最小 + 4 页 · 覆盖概览/提取/车/钱包/设置/prices/register/login/账户）
- [x] ⚙️ 4 条主流程能点通
  - 登录 · ✅ 前端 Login 页 + register 后自动登录
  - 建车拉号 · ✅ 建车 + `/me/buses/{id}/pull`
  - 单独拉号派去向 · ✅ Extract 页 + assign 弹窗（into_bus / push_pool / handoff 三段）
  - 钱包 · ✅ topup 走 gateway checkout_url 或 dev endpoint mark paid
- [x] ✅ MSW 假数据全覆盖（`VITE_USE_MOCK=1` 时用 · 默认 =0 走真后端）
- [x] ✅ 术语审查 grep 无内部词（`vendor_id` 是决策标识·不算内部术语·被 `vendorLabel` 匿名化）
- [x] ✅ 状态展示收敛（`Bus / PullRound / TopupOrder / Credential` 都按 §12.5 收敛过）
- [x] ✅ `npm run build` 通过（有 1.19 MB 主 chunk 提示·非阻塞）
- [ ] ⏸ 用户 Review 清单归档（本轮审计已经算一次·后续继续跟）

**未打勾但真跑过的**：
- [x] ✅ 前后端联调（浏览器手验 · 12 页 0 console error 见 `docs/e2e/log-2026-08-09.md`）
- [x] ✅ 真 payment gateway（浏览器测过 waffo test checkout 页跳转）

**Sprint 1a 前端判定**：**基本 done**·差 vendor 有余额跑一次全流程存档。

## Sprint 1a 前端 · 完成后开 · Sprint 1a 后端

**看 `sprint-1a-backend.md`**。

**关键**：Sprint 1a 前端完成、用户 Review 后，`05-api-contract.md` 和 `06-db-schema.md` 可能会有小改；Sprint 1a 后端按**最终版**契约实现，不用推翻重来。

## 提醒

- **本 sprint 完全不写 Go**。改 `internal/` 任何东西都是错的
- **本 sprint 不写生产打包**（阶段 1a 不上线）
- **术语审查在 #F13 完成之前不要跳过** —— 术语渗透到 UI 后再改成本高
- **每 issue 完成 commit**，PR 走 `web/` 子目录
- **有产品分歧** → 先查 `decisions.md`，再问用户；**不要在 UI 里造新功能**
