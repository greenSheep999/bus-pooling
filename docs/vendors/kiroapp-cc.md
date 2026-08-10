# Vendor: kiroapp.cc

## 1. 基础信息

| 项 | 值 |
|---|---|
| Base URL | `https://kiroapp.cc` |
| 官方文档 | 无独立 `/docs`；对接说明写在登录后 "API Key" tab 的页面里，是 6 家里**最简的开放 API 面板** |
| 抓取日期 | 2026-08-07（登录抓取） |
| 站点标题 | Kiro Admin · Kiro Key 分发平台 |
| 我方账号 | `<redacted>` |
| 计费币种 | 积分 |
| 当前单价 | 50 积分 / 个（我方 2026-08-07 视角，只有一档，无区域拆分） |

## 2. 鉴权

| 项 | 值 |
|---|---|
| Header | `Authorization: Bearer <API Key>` |
| API Key 前缀 | `sk-`（**唯一一家用 sk- 前缀的**，注意与 OpenAI 的 `sk-` 混淆风险） |
| 创建入口 | 登录后 "API Key" tab → 备注名称 → 创建 |
| 页面明文回显 | **API Key 会在列表里持续明文显示**（不像其他家只显示一次） |
| 支持轮换 | 页面有"删除"按钮，创建-删除即可换 key |
| 限制 | **仅程序化调用 `/openapi/*` 生效**；网页端提取、兑换不受影响 |

## 3. 概念 / 术语（观察到的 UI 用词）

| 术语 | 含义 |
|---|---|
| 车 / 批次 | 一次上架的一批 Key（如"第 70 批"，或"手动添加"管理员上架） |
| 车主 / 发车人 | 提交 AWS 凭证发车的用户；有 `@shuaigege` 这类用户名标记 |
| 发车评价 | 每车下架后有社区评价：**拉完了 / 夯 / NPC** 等（是 6 家里唯一有社区评价机制的） |
| 提取消耗 | 积分扣费类型 |
| 质保退款 | 积分退还类型 |
| 待管理员打款 | 收益结算的中间态 |
| 现金结算 | 收益选择走现金而非积分 |
| 收款码 | 现金结算时上传的支付宝/微信收款码（PNG/JPEG/WebP, ≤ 512KB） |

## 4. 计费规则（原文摘录）

- **单价当前 50 积分 / 个**（无区域拆分，一档到底）
- **一车产出多少 Key 只发一条 Webhook**
- **车主提示**：如果投放了 AWS 凭证发车，`/openapi/claim` 会**优先返回自己产出的 Key 并且不扣积分**（返回值里 `pointsCost` 为 0）。不需要单独的自取接口
- **质保退款有独立 ledger 类型**：从我方积分流水观察到 7 笔"质保退款 +50" 集中在 `2026-07-30 15:50:11`（一次批量退款 350 积分）
- 收益结算：一车 Key 全部售罄或失效、**且质保期结束后**才能申请结算

## 5. 账户 / 积分

**没有独立 `GET /openapi/profile` 端点**（是 6 家里唯一没有 profile 端点的）。开放 API 层只暴露：

### `GET /openapi/balance`

```bash
curl "https://kiroapp.cc/openapi/balance" -H "Authorization: Bearer <API Key>"
```

响应：`{ balance }`（**极简，只有余额一个字段**）

### 积分流水（`/api/user/*` 前台会话接口，非开放 API）

页面能看到但没暴露给 `/openapi/*`。从流水 UI 观察到的 ledger 类型：

| 观察到的类型 | 含义 |
|---|---|
| `提取消耗` | 扣积分（负值） |
| `质保退款` | 退积分（正值） |

### 兑换码充值

**只能在网页兑换**（`/openapi/*` 无兑换端点）：

- 入口：账户与积分 tab
- 格式 `KRC-XXXX...`
- **支持批量**：一行一个
- 购买兑换码走外部：`https://pay.ldxp.cn/shop/DWUSVPTJ`

## 6. 库存

### `GET /openapi/stock`

```bash
curl "https://kiroapp.cc/openapi/stock" -H "Authorization: Bearer <API Key>"
```

**响应字段**：

```json
{ "availableKeys": <int>, "keyPrice": <int> }
```

| 字段 | 说明 |
|---|---|
| `availableKeys` | 当前存活库存量 |
| `keyPrice` | 单价（积分/个） |

**注意字段名是 camelCase `availableKeys` / `keyPrice`**，与其他家 snake_case 完全不同——**独树一帜**。

**无区域字段**（不分 us/eu）。

**"死"vs"缺货"判据**：`availableKeys == 0` 只表示当前无货。真正的失效走 UI 上"下架"信号，开放 API 未直接暴露。

### 上架历史（页面观察，无 API 化）

页面显示"最近 10 批上架记录"，每条含：

- 批次号（如"第 70 批" / "手动添加"）
- 车主（用户名或"管理员"）
- 上架时间、下架时间
- 持续时长（12 分钟 ~ 14 小时 47 分钟不等）
- 发车评价（拉完了 / 夯 / NPC / 历史未知）

**这些历史信息只在 UI，没有对应的 openapi 端点**。

## 7. 拉号（核心）

### `POST /openapi/claim` — 提取 1 个 Key

```bash
curl -X POST "https://kiroapp.cc/openapi/claim" -H "Authorization: Bearer <API Key>"
```

**响应形态**：`{ key }` —— **只有一个字符串 `key`**，无其他字段。**是 6 家里返回 payload 最简的**（比 Kiro Drop 的 `{key, region}` 还要少一个 region）。

### `POST /openapi/claim`（批量）

```bash
curl -X POST "https://kiroapp.cc/openapi/claim" \
  -H "Authorization: Bearer <API Key>" \
  -H "Content-Type: application/json" \
  -d "{\"count\":2}"
```

**响应**：`{ keys: [...] }` —— 字符串数组，无对象包装。

### 车主自取特权

- **`/openapi/claim` 会优先返回你自己产出的 Key 并且不扣积分**
- 返回值里 `pointsCost` 为 `0` —— 说明**响应里其实还有 `pointsCost` 字段**（虽然主文档没列在返回形态里，是车主特权时才特别标示的）

### **本 vendor 最大坑：无幂等键**

- **没有 `client_order_id` / `Idempotency-Key`** 概念
- **官方页面明确警告轮询也受频率限制**，但对"网络超时后重试可能双扣"**没有任何机制保护**
- 与其他 5 家形成鲜明对比（91kiro/kiroceo/kirodrop/kiroapp.io 都强制 32-hex 幂等键）

### 补拉 / 订单查询

- **无 `/openapi/orders` 端点**
- 有"我的订单" tab（前台会话）但未 API 化
- **一旦网络超时，无法确认是否扣款成功；重试会重复扣款**

## 8. 积分 / 兑换 / 流水

见 §5（本 vendor 把这些混在账户 tab 里，且 `/openapi/*` 只有 balance）。

## 9. 母号 / 开号 / 供应侧（"我要发车"）

**页面能力**（`/api/user/*` 前台会话接口，非开放 API）：

### 加入号池

- 入口："我要发车" tab
- 支持批量：`一行一个，备注 AK SK` 或 `AK SK`
- **每行独立检测，失败不影响其他行**
- **发车参数由平台统一管理**（不像 kiroapp.io 用户可以自选 tier）
- **凭证加密存储**
- **提交时先检测配额**，额度不足无法加入

### 发车规则（原文摘录）

- 把 AWS 凭证加入号池，系统自动开子账户并提取 Kiro Key
- **号池由平台统一调度**，无需手动操作 —— 轮到你的号时会自动发车，收益照常计
- 产出的 Key 进入**公共库存**
- 其他用户购买后你获得**分成收益**
- 提取 Key 时优先发你自己产出的，且不扣积分

### 收益结算（"我的收益" tab）

| 状态 | 含义 |
|---|---|
| 待管理员打款 | 收益已计算，等待管理员批准打款（显示积分数 + 人民币金额） |
| 已付现金结算 | 走现金支付通道 |
| 已完成转入余额 | 走积分抵账 |
| 尚未形成待付单 | 车还未售罄或质保期未结束 |

收益规则原文摘录：

- 你发车产出的 Key 被**其他用户**购买后产生收益
- 你自己提取的 Key 不扣积分，也不计入收益
- **质保期内退款的订单不计收益**
- 一车的 Key 全部售罄或失效、**且质保期结束后**，即可申请结算

**关键点**：**开放 API 完全不暴露发车能力**。要发车必须走网页 UI。

## 10. Webhook

### 配置

- 入口：API Key tab 底部"到货通知（Webhook）"
- 只有**一个 URL 字段** + **一个"启用推送"开关**
- 无独立配置 API（只有 UI）
- **一车产出多少 Key 只发一条**（原文强调）

### 事件（vendor 原文只有一句话）

**"有新库存时主动推一条 JSON 到你的地址，不用一直轮询"**

- 没有事件类型枚举
- 没有 payload schema 详情（**是 6 家里 webhook 文档最简的**）
- 没有签名 header / 算法说明

**推荐降级方案**（vendor 原文）：轮询 `/openapi/stock`，30 秒一次，有货 (`availableKeys > 0`) 就调 `claim`。

## 11. Key 剩余额度

**无端点**。`/openapi/*` 不暴露 key 用量同步。

## 12. 错误码与限流

### 错误响应形状（原文）

```json
{ "error": { "type": "<code>", "message": "<msg>" } }
```

- **`error` 是嵌套对象**，包含 `type` 和 `message`
- 触发限流时 `type` = `rate_limit_exceeded`，并附 `retryAfter`（秒）

**与其他家对比**：这个嵌套结构最规整；比 kiro.ceo/kiroapp.io 的裸 `{error: "文案"}` 好，但没有 91kiro 那种全表 code 枚举。

### 限流规则（原文摘录）

- **每 60 秒最多调用 60 次开放 API**
- 超出后**进入冷却，180 秒内所有请求都会返回 `429 Too Many Requests`**
- 限制**按账号统计**，创建多个 API Key 也共用同一份额度
- 被限流时响应头会带 `Retry-After`（剩余秒数），建议程序据此自动退避重试
- **网页端不受此限制**，仅程序化 `/openapi/*` 生效

## 13. 质保 / 退款

- 从积分流水观察：`质保退款` 是一种 ledger 类型
- **无独立退款端点**
- **无独立 `warranty_refund` webhook 事件**
- "我的收益"页面明确说明：**质保期内退款的订单不计收益**（暗示存在质保时间窗，但页面没写具体时长）
- 我方账号 2026-07-30 15:50:11 观察到批量 7 笔 +50 质保退款（同一秒），说明一车失效时是**批量结算**

## 14. 本 vendor 特有的事实（可验证的差异）

- **`/openapi/*` 命名**（其他家都是 `/api/my/*` 或 `/api/me/*`）—— 命名空间独立
- **`sk-` API Key 前缀** —— 与 OpenAI 撞名，用错客户端可能踩坑
- **API Key 明文在 UI 里持续显示**（其他家一次性明文）
- **key payload 极简**：单个 `{key}`，批量 `{keys: [string...]}`；**没有 account/password/issuer_url**，甚至没有 region —— 是 6 家里最简
- **无幂等键机制** —— 网络超时重试会双扣，**这是本 vendor 最大接入风险**
- **无 `client_order_id` / `order_id` / 补拉端点**
- **单价无区域拆分**（其他家几乎都有 us/eu 区分）
- **`camelCase` 响应字段**（`availableKeys / keyPrice`），与其他家 snake_case 全部不一致
- **限流最严**：60 QPM，超限后 180 秒冷却（其他家一般看 Retry-After）
- **限流按账号，不按 API Key**：多创建 key 不能绕过
- **发车 / 收益 / 结算完全走网页**，没有 API 化
- **发车带社区评价**（拉完了 / 夯 / NPC 等）—— 独家
- **收益支持现金结算**（要上传收款码 PNG/JPEG/WebP ≤ 512KB） —— 独家
- **Webhook 无签名说明**（其他家至少有 HMAC 或 URL secret）
- **无独立 `/openapi/profile`** —— 只能拿 balance
- **兑换码走外部支付**（`pay.ldxp.cn`），非站内自营
- **错误 envelope 是嵌套的 `error: { type, message }`**，独家结构
- **发车参数由平台统一管理**（vs kiroapp.io 用户可自选 subscription_tier）—— 车主"控制权"最弱

## 15. Fleet 观测端点（2026-08-10 探测）

| 端点 | 结果 | 备注 |
|------|------|------|
| `GET /openapi/orders` | ✅ `[{id, orderNo, kiroApiKey, pointsCost, claimedAt, warranty, probeState, probeTerminalAt, ...}]` | **6 家里唯一"一单一 key"的 vendor** · order 里直接有 key 明文 |
| `GET /api/my/gen-logs` | 404 | 无此端点 |
| `GET /api/me/orders` | 404 | 无此端点 |
| `GET /api/status` | 404 | 无 fleet 自报端点 |

**结论**：kiroappcc 没有独立的 fleet-wide gen-logs 端点 · 但 `/openapi/orders` 直接给到每单每 key 的完整生命周期（probeState / probeTerminalAt）· 我方 adapter `fleet.go` 从 order 数组**推 dispatch**（一单 = 一批 · count=purchased · alive/dead 从 probeState 判定）。
