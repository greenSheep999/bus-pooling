# Vendor: kiro.ooo

## 1. 基础信息

| 项 | 值 |
|---|---|
| Base URL | `https://kiro.ooo/api` |
| 官方文档 | `https://kiro.ooo/index.html#/api`（登录后可见，是 Vue SPA 里的一个页面） |
| 抓取日期 | 2026-08-07（登录抓取） |
| 站点标题 | Kiro Key 自助平台 |
| 我方账号 | `<redacted>` |
| 计费币种 | **积分（1 积分 = 1 元人民币）** —— 是唯一显式把积分挂钩 CNY 的家 |
| 系统库存 | 页面显示"系统库存 0 / 存活 1,026"（快照瞬时值） |
| 我方 API Key | `<redacted>`（页面持续明文显示） |

## 2. 鉴权

**两种任选其一**（vendor 原文强调）：

| Header | 用法 |
|---|---|
| `X-API-Key: usr-...` | 主推 |
| `Authorization: Bearer usr-...` | 等效 |

- 令牌前缀：**`usr-`**（与 91kiro / kiro.ceo 同前缀，容易混淆）
- **API Key 长期在 API 文档页明文回显**（"下面所有示例里的 KEY= 已经替换成你自己的 Key，复制出来可以直接跑"）
- **有"一键复制整篇（含 API Key）" / "复制整篇（Key 用占位符）" 两个按钮**：文档强度足够，直接给开发者两种粘贴姿势

## 3. 概念 / 术语

kiro.ooo 是**功能面板最大**的一家。有独立的**发车方 / 自动车 / 自留 / 名单 / 阶梯定价 / Telegram 通知**等概念。

| 术语 | 含义 |
|---|---|
| Key / 子号 | 平台产出的 kiro 密钥 |
| 母号 (`master_id`) | 一个 AWS 母号，产出一批 Key |
| 车次 (`order_id`) | 一次开号产出的一批 Key（"某趟车"） |
| 发车 | 用户提交 AWS 凭证参与产能 |
| 发车预留 (`reserve_count`) | 发车主为自己保留几个 Key（最少 1；填 0 = 本期不预留不上车） |
| 自动车 (`auto-fleet`) | 自动挂市场；开启前 `reserve_count` 必须 ≥ 1 |
| 自留配置 (`dispatch-config`) | `keep_keys` / `auto_sell`；剩余的自动挂市场 |
| 单价阶梯 (`key-price-tiers`) | `base` 基准价 + `bands` 分档 `{lower, upper, price}` |
| 积分 | 账户余额，**1 积分 = 1 元** |
| 充值链 | 走 USDT 上链（多链可选） |
| 发车名单 (`fleet-roster`) | 仅发车主可见的其他车主行 |
| Telegram 通知 | 独立于 webhook 的推送通道 |

## 4. 计费规则

- **1 积分 = 1 元人民币**（币种明确挂钩 CNY）
- **单价按阶梯**（`GET /my/key-price-tiers` 返回 `base + bands`）
- **下单前算钱用 `/my/key-price-tiers`**（vendor 原文）
- 充值走 **USDT 上链**（`POST /my/recharge/order` 返回收款地址 + 应付 USDT）

## 5. 账号 / Profile

### `GET /my/profile`

账号与额度、速率、可领数量。字段清单未在文档页展示（TODO：直接调用一次验证形状）。

### `POST /my/password`

改自助台登录密码。Body：`{old_password, new_password}`。

### `POST /my/bind-account`

**只有 Key、还没有登录账号的老客户补一套用户名密码**。Body：`{username, password}`。
（**独家**：兼容老用户"只有 key 无账号"迁移到"账号 + key"）

### `POST /my/2fa-verify`

敏感操作前的二次验证。Body：`{code}`。

## 6. 库存

### `GET /my/stock`（**授权**）

可领上限 / 我可取库存 / 剩余配额。示例脚本里读的是 `.claimable` 字段。

### `GET /status`（**免鉴权**）

系统库存 / 存活 / 是否正在开号。是**唯一免鉴权的公开状态端点**。

## 7. 拉号 / 补拉

### `POST /my/keys/claim`

**Body**：`{count, client_order_id}`

**幂等要点（vendor 原文）**：
- `client_order_id` 做幂等：**同一单号重复提交返回上次那批 Key，不重复扣配额**
- 实际取 `min(count, 剩余配额, 我方可取库存)`，**单次上限 500**
- 取不到货返回 **4xx 且不扣配额**，可安全重试
- 被限流返回 **429**，建议指数退避

**收到 webhook 后拉号（vendor 原文脚本）**：

```bash
#!/usr/bin/env bash
KEY="usr-<你的 key>"
BASE="https://kiro.ooo/api"

# 1) 看现在能领几个
n=$(curl -s -H "X-API-Key: $KEY" "$BASE/my/stock" | jq -r .claimable)
[ "$n" -gt 0 ] || { echo "暂无可领"; exit 0; }

# 2) 领取。收到 webhook 的话, OID 直接用推送里的 client_order_id,
# 不要像下面一行那样自己 date 造 —— 同一轮货被重投时,
# 只有原样回传才不会当成新一轮再领一次。轮询场景才用下面这个。
OID="auto-$(date +%Y%m%d%H%M)"
curl -s -X POST "$BASE/my/keys/claim" \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d "{\"count\":$n,\"client_order_id\":\"$OID\"}" \
  | jq -r ".keys[]" >> keys.txt
```

**每把 key 的 payload**：`keys[]` 字段（具体每项字段未在 API 文档页详列；从示例 `.keys[]` 追加到文件推断是字符串数组或有 `key` 字段的对象数组）。TODO：调一次真实 claim 验证。

### `GET /my/keys`

我的 Key 列表。`?history=1` 含已失效。

### `GET /my/keys/export`

**按母号下载 Key**。Query：`?master_id=&history=1&format=json`。
（**独家**：能按母号维度导出，其他家没有）

### `GET /my/keys/created-at`

最早产出时间 + 累计个数（同 kiroapp.io 的 `keys/created-at`）。

### `GET /my/purchase-orders`

最近 **50 笔**订单。

### `GET /my/orders/{id}`

**某趟车的全部子号**。是**独家的"按车次维度查全部子号"** —— 与"按订单查交付"不同。

### `GET /my/dispatch-log`

按车次聚合的活死统计。

## 8. 积分 / 兑换 / 流水

### `GET /my/credits`

积分余额 + 母号单价 + **积分流水**。Query：`?limit=50`。

### `GET /my/recharge/options`

充值档位、最低/最高额、**可选链**（USDT 多链）。

### `POST /my/recharge/order`

下充值单。Body：`{credits, network}`，返回**收款地址和应付 USDT**。

### `GET /my/recharge/orders`

我的充值单。Query：`?limit=50`。

### `GET /my/recharge/order/{order_id}`

单笔充值单状态。

**特点**：完全走 **USDT 链上收款**，不做兑换码、不做支付宝。**这是 6 家里唯一原生用加密货币的**。

## 9. 母号 / 开号 / 供应侧（发车方 API 化）

kiro.ooo 是**唯一把发车方能力完整 API 化**的家。

### 发车预留

- **`PUT /my/reserve`** — Body：`{reserve_count}`；**最少 1，填 0 = 本期不预留不上车**

### 自留配置

- **`GET /my/dispatch-config`** — 返回 `{keep_keys, auto_sell}`
- **`PUT /my/dispatch-config`** — 改自留配置；余下的自动挂市场

### 自动车

- **`GET /my/auto-fleet`** — 返回 `{enabled, unit_price, credits, next_count, afford_count, est_cost}`
- **`PUT /my/auto-fleet`** — 开/关自动车；Body：`{enabled}`；**开之前 `reserve_count` 必须 ≥1**

### 单价阶梯

- **`GET /my/key-price-tiers`** — 返回 `{base, bands[{lower, upper, price}]}`；**下单前算钱用**

### 发车名单

- **`GET /my/fleet-roster`** — **仅发车主可见**；每行 `{name, user_no, credits, reserve_count, auto_fleet, eligible}`
（独家：车主能看到同行的其他车主状态）

**注意**：`kiro.ooo` 的对外文档里**没有**直接展示如何"上传 AWS AK/SK"—— 这套 API 面板假定车主已通过网页 / 后台完成母号绑定，然后用 API 做**运营配置**（预留 / 自留 / 自动车 / 单价阶梯 / 查看名单）。

## 10. API Key 管理

**API 文档页只展示"当前 key"**，没有列出创建/吊销端点。API Key 由 `usr-` 前缀识别；页面上直接明文回显。

## 11. Webhook

### 配置

- **`PUT /my/webhook`** — Body：`{webhook_url}`
- **`POST /my/webhook/test`** — 发一条测试到你配的地址

### 请求头

**vendor 原文**：

- `Content-Type: application/json`
- `User-Agent: kiro-reseller-webhook/1.0`
- **不带签名，请自己用不可猜的 URL 路径当口令**

**这是 6 家里明确说"不签名"的一家**（91kiro/kirodrop 有 HMAC，kiroapp.io 未列签名，kiroapp.cc/kiroceo 也无签名）。

### 重试

- 回 2xx 即算收到
- 超时或 5xx/429 我方**退避重试 3 次**（**0s、2s、6s**）
- **4xx 视为你明确拒绝，不再重试**

### 事件字段（vendor 原文表）

| 字段 | 类型 | 出现在 | 说明 |
|---|---|---|---|
| `event` | string | 全部 | `new_keys_available` \| `all_keys_dead` \| `test` |
| `event_id` | string | 全部 | 这一条推送自己的编号，**32 位小写 hex**，每次投递都不同。**去重键** —— 注意它和 `client_order_id` **形态不同**，不要拿它当 claim 单号 |
| `client_order_id` | string | `new_keys_available` | ★ 这一轮货的单号。**原样回传给 claim 即可，不要自己造** |
| `purchase_order_id` | string | `new_keys_available` | `client_order_id` 的**旧名**，值完全一样。老脚本继续读它，新接入用 `client_order_id` |
| `new_keys` | number | `new_keys_available` | 这一轮就绪的 Key 数。实际能领仍以 `min(count, 剩余配额, 可取库存)` 为准 |
| `claim_hint` | string | `new_keys_available` | 给人看的取货提示，已经把该传的 `client_order_id` 填好了。程序不用解析它 |
| `dead` | number | `all_keys_dead` | 本轮判失效的 Key 数 |
| `message` | string | 全部 | 中文摘要，可直接转发到群里 |
| `time` | string | 全部 | 我方发出时间，**UTC+8，格式 `YYYY-MM-DD HH:MM:SS`** |

### 事件载荷（vendor 原文）

```json
// event = new_keys_available —— 有新货, 回头 claim 取
{
  "event": "new_keys_available",
  "event_id": "356f1c34bc5eb11f0aba4d5fcbb2247e",
  "client_order_id": "ORD3F8N1TW6C",
  "purchase_order_id": "ORD3F8N1TW6C",
  "message": "新一轮 12 个 Key 已就绪",
  "new_keys": 12,
  "claim_hint": "POST /api/my/keys/claim {\"count\":N,\"client_order_id\":\"ORD3F8N1TW6C\"} —— 带上 client_order_id,重复调用返回同一批 key,不会多扣配额",
  "time": "2026-08-02 14:30:00"
}

// event = all_keys_dead —— 本轮 key 全挂了, 等下一轮
{
  "event": "all_keys_dead",
  "event_id": "5168b3156afa6c00be79ca406632dfba",
  "message": "本轮全部 12 个 Key 已失效",
  "dead": 12,
  "time": "2026-08-02 14:30:00"
}

// event = test —— 点「发送测试」按钮时
{
  "event": "test",
  "event_id": "81d4e47a2fce52d07df00b7340472242",
  "message": "这是一条来自 Kiro Key 系统的测试消息",
  "time": "2026-08-02 14:30:00"
}
```

### 幂等要点（vendor 原文）

> 认 `client_order_id`，别用 `event_id`，也别自己 date 造单号 —— 同一轮货被重投时，只有原样回传才不会重复扣配额。

**特别注意**：`client_order_id` 与 `event_id` **形态不同**（前者示例是 `ORD3F8N1TW6C` 短标识、后者是 32-hex），vendor 明确警告不要混用。

## 12. Telegram 通知（独立于 webhook）

这是 kiro.ooo 独家的第二通道推送。

| 端点 | 说明 |
|---|---|
| `GET /my/notify/prefs` | Telegram 推送订阅开关 `{on_key_new, on_key_dead, on_key_suspect, on_dispatch}` |
| `PUT /my/notify/prefs` | 改订阅开关，缺省字段视为开 |
| `POST /my/notify/test` | 给已对接的 Telegram 发一条测试消息 |
| `POST /my/notify/unbind` | 解绑 Telegram 推送，body 空；**解绑后对接码作废，要重新在页面上对接** |

**独家**：**`on_key_suspect` 事件类型** —— 除 `new / dead` 外，还有"疑似失效"这一中间态。

## 13. 错误码与限流

**文档页在幂等区块提到**：

- 取不到货返回 **4xx** 且不扣配额，可安全重试
- 被限流返回 **429**，建议指数退避

**没有全表 code 枚举**（不像 91kiro 的一栏表格）。

页面顶部 tooltip 观察：`积分余额 100（1 积分 = 1 元）`；顶部 badge 显示 `系统库存 0` / tooltip `存活 1,026`。

## 14. 本 vendor 特有的事实（可验证的差异）

- **端点数量最多**（30+ 个），是 6 家里 API 面板最大的
- **1 积分 = 1 元 CNY 显式挂钩**（其他家仅积分不带汇率）
- **充值走 USDT 上链**（多链可选，返回收款地址 + 应付 USDT）—— 独家
- **Webhook 明确"不签名"** —— 靠不可猜 URL 路径当口令
- **Webhook 退避固定 `0s / 2s / 6s`**（vs drop 固定 1s / 91kiro 递增+抖动）
- **4xx 视为"明确拒绝"不再重试**（其他家 4xx 语义未这么明确）
- **`client_order_id` 形态是 `ORD` 前缀短串**（vs 91kiro/kiroceo/kiroapp.io 强制 32-hex）—— 幂等键的形态不同，接入时不能假设"都是 32-hex"
- **`purchase_order_id` 是 `client_order_id` 的老名字**（其他家里两个字段有独立语义！kiroappio 里 `purchase_order_id` 是幂等键、`order_id` 是批次；drop 里也各有其意；kiro.ooo **两者字面同值**）
- **`claim_hint` 独家字段**：完整的 curl 建议直接嵌在 webhook 载荷里
- **Telegram 独立推送通道**（`on_key_new / on_key_dead / on_key_suspect / on_dispatch`），是 webhook 之外的第二通道 —— 独家
- **`on_key_suspect` 事件**："疑似失效"中间态 —— 独家
- **`/status` 免鉴权公开状态**（其他家至少要一次鉴权）
- **`GET /my/keys/export` 按母号导出**（其他家没有）
- **`GET /my/orders/{id}` 是"某趟车的全部子号"**（不是"按订单查交付"）
- **`GET /my/fleet-roster` 车主视角看其他车主**（独家社交/协同信号）
- **`GET /my/key-price-tiers` 显式阶梯定价查询**（kiroappio 只在 stock 里返 `price_min/price_max`，本家给完整阶梯）
- **单次 claim 上限 500**（与 91kiro/kiroappio 一致）
- **`POST /my/bind-account` 老客户补账号**（迁移场景独家 API）
- **`POST /my/2fa-verify` 二次验证**（其他家未展示 2FA API）
- **发车方能力完整 API 化**：`reserve` / `dispatch-config` / `auto-fleet` / `fleet-roster` / `key-price-tiers`（**是发车运营 API 化最完整的家**；kiroapp.io 只做了 refill-config，kiroapp.cc 完全 UI 化）
- **UA 头 `kiro-reseller-webhook/1.0`** 可用于服务端识别
- **时区 UTC+8 明写**（`2026-08-02 14:30:00` 无时区后缀）
- **文档页有"一键复制整篇"按钮** —— 是 6 家里对开发者最友好的对接文档

## 15. Fleet 观测端点（2026-08-10 探测）

| 端点 | 结果 | 备注 |
|------|------|------|
| `GET /api/status` | ✅ **免 auth** `{keys_active, keys_alive, keys_dead, keys_suspect, keys_stock, keys_total, generating, started_at, uptime_secs}` | 最丰富的 PublicStatus 端点 · 有 5 种 key 状态计数 |
| `GET /api/my/stock/regions` | ✅ `regions[].dispatches[]` `{alive, dead, delivered, dead_at, running, time}` | **含 dead 明细** · 是 6 家里最完整的 dispatch 视图 |
| `GET /api/my/stock` | ⚠️ 偶尔网络超时（不是接口问题）| 有 zones · 有 unit_price |
| `GET /api/my/gen-logs` | 404 明确 `{"message":"接口不存在"}` | 无此端点 |

**结论**：kirooo 是**观测能力最强**的 vendor · 同时给出"当前累计"（`/api/status`）+"最近开号明细"（`/api/my/stock/regions`）· 我方 adapter 里 `fleet.go` 和 `public_status.go` 分别接这两个。
