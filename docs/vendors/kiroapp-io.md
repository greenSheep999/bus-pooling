# Vendor: kiroapp.io

## 1. 基础信息

| 项 | 值 |
|---|---|
| Base URL | `http://kiroapp.io`（vendor 文档明写 http，非 https） |
| 官方文档 | `https://kiroapp.io/api-docs`（登录后访问，Next.js SPA 页面） |
| 抓取日期 | 2026-08-07（登录抓取） |
| 站点标题 | Kiro 密钥市场 |
| 我方账号 | `<redacted>` |
| 计费币种 | 积分（无单位标注，纯积分制） |
| 单价档 | 按母号累计产量分档；`price_min / price_max` 双档 |
| 前端观察值 | 当前 US 60 / EU 40 积分/个（我方账号 2026-08-07 视角） |

## 2. 鉴权

**任选其一**（vendor 文档原文强调 "读写一致"）：

| Header | 用法 |
|---|---|
| `Authorization: Bearer km_…` | 推荐 |
| `X-API-Key: km_…` | 等效，适配 ai-relay-go 等客户端 |

- **令牌前缀：`km_`**（与 91kiro / kiro.ceo 的 `usr-` 不同）
- 令牌在 `设置 → API 令牌` 生成（`/settings/tokens`）
- **明文只显示一次**
- **令牌只能调 `/api/me/*` 前台接口，`/api/admin/*` 一律 403**
- 无需 Cookie / CSRF
- 泄露后**在设置页吊销即可**（可自服务 rotate，与 91kiro 差异）

## 3. 概念 / 术语

| 术语 | 含义 |
|---|---|
| 母号 | 一个 AWS 账号 |
| 号池 | 我在 vendor 侧提交的一组母号 |
| 批次 | 一次开号的产出，用 `order_id` 标识 |
| 自动发车 | 号池级别的低水位补号（存活凭证跌破阈值就自动开一批） |
| 供货到公开池 | 批次产出可以留自用（`private`）或上架公开池（`public`） |
| 直接买密钥 | 从公共库存即时提取（60 积分/个起） |
| 买断平台母号 | 500 积分/台，无需自备 AWS，母号过户 |
| 上传自有母号 | 免费提交，仅按开号收少量手续费 |

## 4. 计费规则

- **单价按【产出该 key 的母号累计产量】分档**，同一单里各 key 可能不同价
- **便宜的先出货**
- 阶梯定价下前端**无法精确预估总价**，以 `total_debit` 与每个 key 的 `price` 为准
- **10 分钟失效质保**（首页标语，接口层未详列条款）
- **产出 key > 0 才收开号手续费**，管理员免

## 5. 账户与积分

### `GET /api/me/profile` — 当前用户档案

返回令牌对应用户的完整档案（余额、限购、通知配置等）。

```json
{
  "user": {
    "id": "a8d5e9d5-…",
    "name": "alice",
    "email": "alice@example.com",
    "balance": 2060,
    "min_purchase": 1,
    "max_purchase": 10,
    "notify_new_batch": true,
    "created_at": "2026-07-29T09:51:32+09:00"
  }
}
```

### `GET /api/me/ledger` — 积分流水

带符号的每笔积分变动与运行余额，附累计收支汇总。

**Query 参数**：

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `page` | int | 可选 | 页码，默认 1 |
| `page_size` | int | 可选 | 每页条数，默认 50，上限 500 |
| `type` | string | 可选 | 按类型过滤 |

**`type` 取值**：`stripe_recharge` / `redeem_credit` / `purchase_debit` / `supplier_payout` / `open_fee` / `warranty_refund` / `admin_adjust` …

**响应示例**：

```json
{
  "items": [
    {
      "seq": 2,
      "type": "stripe_recharge",
      "amount": 60,
      "balance_after": 2060,
      "ref_type": "recharge_order",
      "ref_id": "47166cdc-…",
      "memo": "Stripe 充值",
      "created_at": "2026-07-30T10:45:29+09:00"
    }
  ],
  "total": 2,
  "page": 1,
  "page_size": 50,
  "pages": 1,
  "summary": { "total_in": 2060, "total_out": 0 }
}
```

### `POST /api/me/redeem` — 兑换码充值

**Body**：

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `code` | string | 必填 | 兑换码 |

**响应示例**：

```json
{ "quota": 100, "replayed": false }
```

- 同一个码重复兑换返回 `replayed: true`，**不重复到账**
- 码不存在返回 **404**
- 已被他人使用返回 **409**

## 6. 库存与报价

### `GET /api/me/stock`

可提取数量、在售价格区间与我的余额。**下单前先查它**。

**响应示例**：

```json
{
  "stock": 120,
  "price": 30,
  "price_min": 30,
  "price_max": 65,
  "balance": 2060,
  "stock_us": 108,
  "stock_eu": 12
}
```

| 字段 | 说明 |
|---|---|
| `stock` | 公共库存总量 |
| `price` | = `price_min`，向后兼容字段 |
| `price_min` / `price_max` | 单价档区间 |
| `balance` | 我的余额 |
| `stock_us` | 美区可售量 |
| `stock_eu` | 欧区可售量。**为 0 时不要传 `region=eu`** |

## 7. 拉号 / 补拉

### `POST /api/me/purchase` — 下单购买（幂等）

扣积分并返回**密钥明文**。**单价按产出该 key 的母号累计产量分档，同一单里各 key 可能不同价**。幂等靠 `client_order_id`：相同 id 重放返回**字节一致**的原响应，**绝不重复扣款**。

**Body 参数**：

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `count` | int | 必填 | 购买数量 |
| `client_order_id` | string | 必填 | **32 位十六进制**，调用方生成并保存，用于幂等重试 |
| `order_id` | string | 可选 | Webhook 推送里的**开号批次 id**，只拉取该批次产出的 key。传了它就**不再按 region 筛选**——批次本身已经确定了区域 |
| `region` | string | 可选 | `us`（默认）或 `eu`；也接受 `us-east-1` / `eu-central-1`。不传等同 `us`。传认不出的值返回 **400**，**不会静默按 us 发货** |

**请求示例（vendor 原文）**：

```bash
# 不传 region = 美区（老客户端原样可用）
curl -X POST http://kiroapp.io/api/me/purchase \
  -H "Authorization: Bearer km_xxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"count":5,"client_order_id":"a1b2c3d4e5f60718293a4b5c6d7e8f90"}'

# 买欧区：client_order_id 必须与美区那单不同，
# 同一个 id 会命中幂等重放、把上一单的美区 key 原样返回
curl -X POST http://kiroapp.io/api/me/purchase \
  -H "Authorization: Bearer km_xxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"count":5,"region":"eu","client_order_id":"b2c3d4e5f60718293a4b5c6d7e8f901a"}'
```

**响应示例**：

```json
{
  "purchased": 5,
  "requested": 5,
  "remaining": 115,
  "unit_price": 38,
  "total_debit": 190,
  "order_id": "0d9f…",
  "keys": [
    {
      "key": "sk-…",
      "account": "user-…",
      "password": "…",
      "issuer_url": "https://…",
      "price": 30
    }
  ],
  "replayed": false
}
```

**每把 key 的 payload 五件套**：`{ key, account, password, issuer_url, price }` —— **比 91kiro/kiro.ceo 多一个 `price` 字段**，直接给出这把实扣值。

| 字段 | 说明 |
|---|---|
| `purchased` | 实际成交数量 |
| `requested` | 请求数量 |
| `remaining` | 扣后余额 |
| `unit_price` | **本单实际均价 = `total_debit / purchased`** |
| `total_debit` | 实际扣费总额，**权威数字** |
| `keys[].price` | 这一个 key 实际扣了多少 |
| `replayed` | 是否是幂等重放 |

**vendor 原文强调**：

- 同 `client_order_id` 换 `count` 重放返回 **409**
- 余额不足时按买得起的数量成交（`purchased < requested`）；有货但**一个都买不起返回 403**
- 阶梯定价下**前端无法精确预估总价**，以 `total_debit` 与每个 key 的 `price` 为准
- 网络超时后**用同一个 `client_order_id` 重试即可，安全**

### `GET /api/me/orders` — 我的订单

历史提取订单，分页信封。

**Query**：`page`、`page_size`（默认 50，上限 500）

### `GET /api/me/keys` — 我的密钥

分页信封。`history=1` 时含已失效的密钥。

**Query**：

| 参数 | 类型 | 说明 |
|---|---|---|
| `history` | `0 \| 1` | `1` = 含已失效的密钥；默认只返回存活的 |
| `page` / `page_size` | int | 分页 |

**响应示例**：

```json
{
  "items": [
    {
      "id": "…",
      "key_value": "sk-…",
      "account": "user-…",
      "password": "…",
      "issuer_url": "https://…",
      "status": "sold",
      "purchased_at": "2026-07-30T09:00:00+09:00",
      "created_at": "2026-07-30T08:12:00+09:00"
    }
  ],
  "total": 42,
  "page": 1,
  "page_size": 50,
  "pages": 1
}
```

**关键**：本 vendor 的 `/api/me/keys` **返回完整明文**（`key_value` / `account` / `password` / `issuer_url` 全给），**不像 91kiro 只给前缀**。字段名是 `key_value` 而不是 `key`。

### `GET /api/me/keys/created-at` — 最早密钥时间

最早一条密钥的创建时间与总数，估算账龄用。

```json
{ "created_at": "2026-07-29T10:00:00+09:00", "count": 42 }
```

## 8. 积分 / 兑换 / 流水

见 §5（本 vendor 把账户与积分合并在一节）。

## 9. 母号 / 开号 / 供应侧（"我的号池"）

### `POST /api/me/accounts` — 提交母号

提交自己的 AWS 母号，**提交即可用（无审核流程）**。凭证只进不出，任何读接口都不会返回。

**Body 参数**：

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `access_key` | string | 必填 | AWS Access Key |
| `secret_key` | string | 必填 | AWS Secret Key |
| `region` | string | 可选 | 默认 `us-east-1` |
| `note` | string | 可选 | 备注 |
| `subscription_tier` | string | 可选 | 开号订阅档位：`20 / 40 / 100 / 200`，留空跟随平台默认 |
| `gen_mode` | string | 可选 | 开号模式：`group | user` |

**响应示例**：

```json
{ "id": "…", "generating": 0 }
```

### `GET /api/me/accounts` — 号池列表

我的全部母号（分页，含存活凭证数）。

### `PATCH /api/me/accounts/{id}` — 更新母号

| 参数 | 类型 | 说明 |
|---|---|---|
| `note` | string | 备注 |
| `auto_open` | bool | 自动发车开关 |
| `target_alive` | int | 常驻凭证数 0..50；与 `auto_open` 同时生效 |

### `DELETE /api/me/accounts/{id}` — 删除母号

- 名下**仍有未失效密钥时删除会被拒绝**

### `POST /api/me/accounts/{id}/generate` — 手动开号

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `count` | int | 必填 | 开号数量，上限见平台设置（**默认 100**） |

- **产出 key > 0 才收开号手续费**，管理员免

### `PUT /api/me/refill-config` — 自动发车配置

号池级低水位补号：私有池存活凭证跌破低水位就自动开一批。**`GET` 同路径读取当前配置**。

| 参数 | 类型 | 说明 |
|---|---|---|
| `refill_enabled` | bool | 总开关 |
| `refill_low_watermark` | int | 低水位阈值 |
| `refill_batch` | int | 每批开号数 |
| `refill_auto_check` | bool | 自动检测开关 |

## 10. API 令牌

### `GET /api/me/tokens` — 令牌列表

不含明文，只有展示用前缀。

### `POST /api/me/tokens` — 签发令牌

**Body**：

| 参数 | 类型 | 说明 |
|---|---|---|
| `name` | string | 名称，便于区分用途 |
| `expires_in_days` | int | 有效期天数，**0 = 永不过期，最长 365** |

**响应示例**：

```json
{
  "token": "km_xxxxxxxx…",
  "item": {
    "id": "…",
    "name": "生产脚本",
    "prefix": "km_xxxxxxxx",
    "expires_at": "2026-10-28T…"
  }
}
```

**明文只在这里返回一次**。令牌只能调 `/api/me/*` 前台接口，`/api/admin/*` 一律 **403**。

### `DELETE /api/me/tokens/{id}` — 吊销令牌

**立即生效**，使用中的调用开始返回 **401**。

## 11. Webhook（事件推送）

批次开号完成后向配置地址推送。

### 事件类型

- `new_keys_available` — 有新 key 产出
- `all_keys_dead` — 本轮 key 全灭，带 `dead=<数量>`，系统正在自动补号
- `key_revoked_abuse` — **某个已售 key 因用量异常被收回**（本 vendor 独有事件），带 `key_prefix / avg_per_min / threshold`
- `test` — 设置页「测试」按钮直发

### `new_keys_available` 字段（vendor 原文表格）

| 字段 | 类型 | 说明 |
|---|---|---|
| `event` **恒存在** | string | `new_keys_available` \| `all_keys_dead` \| `key_revoked_abuse` \| `test` |
| `event_id` **恒存在** | string | **去重键**。推送失败会重试，同一事件可能送达多次，按它幂等 |
| `visibility` | string | `private` = 本批留给你自用（免费自提）；`public` = 上架公开池（花积分买） |
| `new_keys` | number | 这一条通知你能拿到的数量——**不是批次总产出**。`private` 条是自用留存数，`public` 条是上架数 |
| `supplied_count` | number | 仅 `private` 条且本批有上架时出现：其中多少个已转入公开市场。知情字段，这些自提拉不到 |
| `order_id` | string | 开号批次 id。下单时带上它就只拉本批次的 key |
| `purchase_order_id` | string | 替你备好的幂等键，**原样作为 `client_order_id` 传给购买接口**即可 |
| `mother_id` | string | 产出这批 key 的母号 id |
| `message` **恒存在** | string | 一句人话描述 |
| `finished_at` | string | 开号完成时间，**东八区**，格式 `2026-01-02 15:04:05 CST` |
| `stock_us` **恒存在** | number | 推送这一刻公开池的**美区可售量** |
| `stock_eu` **恒存在** | number | 同上，欧区 |
| `price_us` **恒存在** | number | 美区当前单价（积分 / key）。母号累计产量分档时这是**最低档价** |
| `price_eu` **恒存在** | number | 同上，欧区当前单价 |

### 载荷（vendor 原文摘录）

```
POST <your_webhook_url>
Content-Type: application/json

// 同一批开号最多发两条通知，载荷结构【完全一致】，只有 visibility 与数量不同：
// visibility=private → 只推池主，new_keys 是留给你自用的数量（免费自提）
// visibility=public  → 推全体订阅者，new_keys 是上架公开池的数量（花积分买）
// 开了「供货到公开池」的池主，一批产出会同时收到这两条（前提是有自用留存）。

{
  "event": "new_keys_available",
  "event_id": "<去重用的唯一 id>",
  "visibility": "private",
  "new_keys": 10,
  "supplied_count": 12,
  "order_id": "<开号批次 id>",
  "purchase_order_id": "d5c4fd9460b70fb8e944bd7faa519896",
  "mother_id": "<母号 id>",
  "message": "母号新开号完成，10 个 Key 归你自用...",
  "finished_at": "2026-07-31 14:30:05 CST",
  "stock_us": 108,
  "stock_eu": 12,
  "price_us": 20,
  "price_eu": 15
}

// 收到后直接下单：两个 id 原样带上，不必自己生成幂等键
POST /api/me/purchase
{
  "count": 10,
  "region": "us",
  "order_id": "<order_id>",
  "client_order_id": "<purchase_order_id>"
}
```

### 其它事件

- `all_keys_dead`：本轮 key 全灭，带 `dead=<数量>`，系统正在自动补号
- `key_revoked_abuse`：某个已售 key 因用量异常被收回，带 `key_prefix / avg_per_min / threshold`
- `test`：设置页「测试」按钮直发。**公共车的测试与真实补货广播同构**（`event=new_keys_available`、`order_id` 为随机假批次），照标准流程去 purchase 会得到 **404**，不会扣费

**这两个事件不带 `order_id` / `new_keys`；`stock_*` 与 `price_*` 字段仍在但恒为 0，请勿据此判断行情。**

### 关键行为（vendor 原文摘录）

- 载荷**不含密钥明文**。收到后把 `order_id` 传给 `POST /api/me/purchase` 即可只拉取该批次的 key
- 标了 **恒存在** 的字段每条推送都会带上，**值为 0 是有意义的答案**（该区域没货 / 该事件不适用），不会因为是 0 就消失——直接读数值即可，不必先判断字段存不存在
- **`purchase_order_id` 由（批次 + 收件人）确定性派生**，推送重试、重复推送、服务重启后都是同一个值——拉取超时后原样重发即命中幂等重放，不会二次扣费
- 回调地址在 `设置 → Webhook` 配置，可先发一条 `test` 事件验证连通

### 签名

**vendor 文档在 api-docs 页面未列出 webhook 签名 header 和算法**。（TODO：登录 `/settings/webhook` 页面查看签名密钥展示 —— 但从 profile 的 `notify_new_batch` 布尔字段判断，本 vendor 的 webhook 契约相对简单）

## 12. 错误格式与限流

**统一形状**：

```json
{ "error": "人类可读的原因" }
```

**语义（vendor 原文）**：

- **401** = 令牌无效 / 过期
- **403** = 无权限（如令牌调 admin 接口）
- **429** = 触发限速
- 与 kiro.ceo 一样，**没有稳定的 code 字段**

**分页信封**：所有列表接口统一 `{items, total, page, page_size, pages}`，参数 `?page=1&page_size=50`（上限 500）。

**安全边界**：令牌泄露到设置页吊销即可；管理端接口**不接受令牌**，必须浏览器会话登录。

## 13. 质保 / 退款

- **首页标语**：10 分钟失效质保
- **`type=warranty_refund`** 在 ledger 类型枚举里
- api-docs 未列出独立退款端点或 webhook 事件（**质保退款只走 ledger 记录**）

## 14. 本 vendor 特有的事实（可验证的差异）

- **Base URL 明写 `http://`** —— vendor 文档原文，非 https（这是唯一一家这么标的）
- **令牌前缀 `km_`**（其他家 `usr-`）
- **令牌明文只显示一次**、**支持自服务吊销**、**只能调 `/api/me/*`**（安全边界清晰）
- **key payload 5 字段**：`{key, account, password, issuer_url, price}`，多一个逐把 `price`
- **`/api/me/keys` 直接返回明文**（91kiro 只给前缀）
- **`price_min` / `price_max` 阶梯定价**，前端无法预估总价，权威值是 `total_debit`
- **`unit_price = total_debit / purchased`**（是本单实际均价，不是当前挂牌价）
- **`visibility=private/public` 双载荷**：同一批开号可能推 2 条 webhook，一条自用一条上架
- **`key_revoked_abuse` 独立事件**：某 key 因用量异常被回收，带 `key_prefix / avg_per_min / threshold` —— **6 家里唯一有此语义的**
- **`test` 事件的载荷与真实事件同构**，`order_id` 是假批次，走标准流程 purchase 会 404 不扣费（用于连通测试）
- **恒存在字段策略**：`stock_us` / `stock_eu` / `price_us` / `price_eu` 值为 0 也保留字段，不能靠"字段消失"判事件类型
- **`purchase_order_id` 是确定性派生**（批次 + 收件人 hash），非随机——重投一定命中幂等
- **母号 `subscription_tier` 有 4 档**：20 / 40 / 100 / 200
- **自动发车（refill）字段**：低水位 + 每批数量 + 自动检测三档配置
- **ledger 类型枚举明确**：`stripe_recharge / redeem_credit / purchase_debit / supplier_payout / open_fee / warranty_refund / admin_adjust`（**是 6 家里 ledger 语义最全的**，且提示接受 **Stripe 支付**）
- **分页上限 500** 而不是 200
- **时区东八区 CST**（`2026-01-02 15:04:05 CST`），与其他家 ISO-8601 UTC 差异

## 15. Fleet 观测端点（2026-08-10 探测）

| 端点 | 结果 | 备注 |
|------|------|------|
| `GET /api/status` | ✅ 免 auth `{generating, price, price_us, price_eu, stock, stock_us, stock_eu, started_at, uptime_seconds}` | **无 keys_ * 字段** · 只有当下库存 + 生成状态 |
| `GET /api/public/stats` | 200 `{mother_price, price, price_eu, price_us, stock, stock_eu, stock_us, warranty_minutes}` | 与 `/api/status` 高度重叠 · 前者多了 uptime |
| `GET /api/me/fleet-summary` | 200 `{mine:[], public:[]}` | **返空 · 未来可能承载 fleet 数据** · 目前不可用 |
| `GET /api/me/orders` | 200 `{items:[],pages:0,total:0}` | 账户空 |
| `GET /api/me/stock` | ✅ `{stock, price, price_us, price_eu, balance, max, stock_us, stock_eu, warranty_minutes}` | **注意契约变更**：曾是 `{stock:{public_available:N}}` 嵌套对象 · 2026-08 改成 `stock: N` 数字 · 我方 adapter 已做双形状兼容（`mapper.go`） |

**结论**：kiroappio **无 dispatch 历史端点** · PublicStatus 只有 `generating/uptime`（无 keys_*）· `/status` 页从 `vendor_probe.stock_total` 增量推 dispatch · 精度较低。
