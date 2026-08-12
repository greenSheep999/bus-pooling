# 6 家 vendor 端点审计 · 2026-08-12

**目的**：文档可能错漏 · 有些端点换名字没更新 · 直接 curl 6 家真实响应 · 找隐藏端点 / 修正老档案。

**方法**：`/tmp/probe-vendors.sh` 用 6 家 API key 打 16 个候选路径 · 200 且 body >20 存到 `docs/vendors/_endpoints-2026-08-12/`。

## 各家真实端点清单

### 本 vendor 群路径规律

| 家 | 路径前缀 | 鉴权 |
|---|---|---|
| kiro91 · kiroceo · kirooo | `/api/my/*` · `/api/status` | `X-API-Key` |
| kiroappio | `/api/my/*` · `/api/status` | `Authorization: Bearer` |
| kiroappcc | `/openapi/*` | `Authorization: Bearer` |
| kirodrop | `/api/my/*` · `/api/status` | `X-API-Key` |

### kiroceo（发现最多）

- `/api/my/stock` · 单值 stock · zones 数组 · 200
- `/api/my/gen-logs` · **✨ 完整批次历史 · `avg_interval_min` + `items:[{created_at,count,status}]`** · running/error/done 三态
- `/api/my/purchase-orders` · 我方历史订单
- `/api/my/keys` · **status=dead 也返** · 完整 key 生命周期
- `/api/my/profile` · 含 `webhook_url` 我方注册值（校对用）
- 其他 404 但 body 都是 576 字节 SPA 首页（不是真 API）

### kirooo

- `/api/my/stock` · 单值 · 新格式（数字）
- `/api/my/stock/regions` · **`fleet_active`/`fleet_started_at`/`regions[].dispatches[]`** · fleet 视角（Step §11.9 探针改端点靠这个）
- `/api/my/keys` · 只返 active · dead 不返（跟 kiroceo 不同）
- `/api/status` · 完整 keys_active/alive/dead/suspect/total + uptime + started_at
- `/api/my/purchase-orders` · 简约版（无 keys 详情）

### kiroappio

- `/api/my/stock` · 单值 stock + price + stock_us/eu
- `/api/status` · **含 `generating`/`uptime_seconds`/`started_at`**（一直在发号但库存被抢空）
- `/api/my/profile` · 404 · **档案错了** · 不存在
- 其他 keys/orders 类端点全 404 · 无补拉

### kiroappcc（**最大惊喜 · 老档案漏了**）

- `/openapi/balance` · balance=785 · 200
- `/openapi/stock` · **camelCase** `{availableKeys,keyPrice}` · 无 zone 拆分
- `/openapi/claim` · POST 拉号
- **`/openapi/orders` · 200 · 18 条历史订单 · 每条含 `probeState`/`warrantyStatus`/`refundedAt`/`usageSnapshot`** ⚠️
  - **档案 §7 错**："本 vendor 无 /openapi/orders 端点 · 不支持补拉" · adapter.OrderKeys 直接拒绝
  - 实际存在 · 应该开通补拉能力 · 更新 adapter

### kirodrop

- `/api/my/profile` · quota/remaining（USD 计价）
- `/api/status` · `keys_active`/`keys_dead`/`keys_stock` · 单 region（us-east-1 default）
- 其他 404（用了自定义 error 格式 `{"error":{"code":"NOT_FOUND",...}}`）

### kiro91

- `/api/my/stock` · **旧格式** `stock:{public_available,my_private}` · 我们代码已兼容
- `/api/my/profile` · 完整 profile
- `/api/my/keys` · **空** · 全挂了
- `/api/my/purchase-orders` · **空**
- 无 gen-logs · 无 stock/regions · **fleet 历史无直接端点**（探针改推 stock-delta 兜底）

## 关键发现

### ⚠️ 1. kiroappcc `/openapi/orders` 之前判错

档案 §7 说"无 /openapi/orders 端点" · **实测存在** · adapter.OrderKeys 逻辑要改：

```go
// 现在（错的）：
Message: "本 vendor 无 /openapi/orders 端点，不支持补拉",

// 应该：
GET /openapi/orders 拿全量 · 从里面挑 orderNo == 参数 order_id 那条
```

**影响面**：
- 拉号中间 kiroappcc vendor 侧 200 但我方超时 → 我们把订单标 `need_manual` · 现在**可以自动补拉**了
- 历史 backfill 拉过一遍 · 后续增量 · 每单 `probeState`/`warrantyStatus`/`refundedAt` 都有

### ⚠️ 2. kirooo `/api/my/keys` 只返 active · 之前当 history 用是错的

- kirooo `/api/my/keys?history=1` 参数**没生效** · 返值跟 `/api/my/keys` 完全一样
- 之前 History 层假设能拉到 dead key · 实际拉不到
- 需要靠 vendor webhook 落 `credential_ledger` · 或 xi8 backfill 补齐

### ⚠️ 3. kiro91 无 fleet 历史端点 · 强依赖 stock-delta

- kiro91 也没有类似 `/api/my/stock/regions` 里 dispatches 数组的端点
- 探针拉的 `/api/my/stock` 有 `incoming` 空数组（可能未来给 pre-launch 用）
- backfill 时代加 xi8 补齐 · 长期靠 stock-delta 推算

### ✨ 4. kiroceo `/api/my/gen-logs` 是隐藏金矿

- 有 `avg_interval_min: 24.01` · vendor 自算的平均开号间隔
- `items[]` 完整批次历史 · 三态（running/error/done）
- **fleet.go 已经在用** · 但**没记进档案** · 补上

## 修法优先级

1. **kiroappcc adapter · 开通 /openapi/orders 补拉 + 历史 backfill**（Step B）
2. **kirooo history 依赖 xi8 + webhook** · 别指望 `?history=1` 参数
3. **kiro91 无 fleet 数据** · 全靠 stock-delta（已加 · Step §11.9）+ xi8 补
4. **kiroceo gen-logs** · fleet.go 已用 · 档案补一行说明

## 6 家 vendor 端点能力矩阵

| 家 | 现库存 | 分区 | 历史订单 | 历史 keys | Fleet 批次 | Fleet PS | Webhook |
|---|---|---|---|---|---|---|---|
| kiro91 | stock | zones ✅ | orders 空 | keys 空 | ❌ | ❌ | ✅ |
| kiroceo | stock | zones ✅ | orders ✅ | keys ✅（含 dead） | ✨ gen-logs ✅ | ❌ | ✅ |
| kirooo | stock/regions | ✅ | orders ✅（简约） | keys（仅 active） | ✅ regions.dispatches | ✅ status | ✅ |
| kiroappio | stock | ✅（us/eu） | ❌ | ❌ | ❌ | ✅ status | ✅ |
| kiroappcc | stock | ❌ | **orders ✅ ⚠️** | orders 里带 | orders 每条=一批 | ❌ | ✅ |
| kirodrop | stock 单 region | ❌ | ❌ | ❌ | ❌ | ✅ status（薄） | ✅ |

**结论**：
- **kiroceo + kirooo**：数据最全 · 能画出完整 fleet 曲线
- **kiroappcc**：orders 全 · 但字段跟其他家差别大（需 mapper 单独处理）
- **kiro91 + kiroappio + kirodrop**：贫瘠 · 强依赖 stock-delta + xi8 + webhook
