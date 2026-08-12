# 6 家 vendor 官方端点全量摸底 · v2（2026-08-12）

**跟 v1 的区别**：v1 只探针式 curl 试了 16 个候选路径。**v2 用账号密码登进每家后台 · Playwright 抓官方文档 + Network 抓页面用的隐藏端点**。原始输出留 `.playwright-mcp/vendor-scrape-2026-08-12/`（含 API key 明文 · 不入库）。

## 每家端点数量总览

| Vendor | 官方文档列端点 | Network 抓隐藏 | 合计 | 我方 adapter 已接 | 覆盖率 |
|---|---|---|---|---|---|
| **91kiro** | ~30 | – | ~30 | 8 | 27% |
| **kiroceo** | 9 | – | 9 | 8 | **89%** |
| **kirooo** | 32 | – | 32 | 9 | 28% |
| **kiroappio** | ~25 | – | ~25 | 8 | 32% |
| **kiroappcc** | 4 (openapi) | 15 (/api/user/*) | 19 | 6 | 32% |
| **kirodrop** | 8 | – | 8 | 6 | 75% |

**总缺口 · 大概 90+ 个端点没接**。

## 共性 · 6 家都有的基础能力（**每家都必须接 · 是"最小闭环"**）

| 能力 | 端点样式 | 6 家都有 |
|---|---|---|
| 账户 profile | `GET .../profile` | ✅ 全 |
| 库存查询 | `GET .../stock` | ✅ 全 |
| 提货 | `POST .../purchase` 或 `/claim` | ✅ 全 |
| 提货幂等 | `client_order_id` 32 hex | ✅ 全（**语义完全一致**） |
| 历史订单 | `GET .../purchase-orders` 或 `/orders` | 5/6（kirodrop 无独立端点） |
| 我的 keys | `GET .../keys` | 5/6（kirodrop 无） |
| Webhook 设置 | `PUT .../webhook` | ✅ 全 |
| Webhook 测试 | `POST .../webhook/test` | ✅ 全 |
| Webhook new_keys_available | 有新号推送 | ✅ 全 |
| Webhook all_keys_dead | 整批死推送 | ✅ 全 |
| 兑换码 | `POST .../redeem` | 5/6（kirodrop 无 · 走直接充值） |
| 双区分离 us/eu | zone/region 参数 | 5/6（kiroappcc 无区） |
| 质保退款 | 时间/用量条件 | ✅ 全（但机制不同） |

**结论**：**我方 adapter 已覆盖 6 家最小闭环** —— 探针 + backfill + 抢号链能跑起来。**没有紧急必接的**。

## 大缺口 · 差异化能力（用户可感 · 但我方**都没接**）

### 1 · 母号供应侧（**只 3 家有 · 我方全没接**）

作为 vendor 客户 · 我方可以**投放自己的 AWS 母号进 vendor 号池**，vendor 帮开号，卖出后**分成给我**。

| 家 | 母号 API | 分成机制 | 我方接了 |
|---|---|---|---|
| **91kiro** | `POST/PUT/DELETE /api/my/mothers/*` (8 个端点) | 按售价 × (100 − service_fee%) 返 · income ledger | ❌ |
| **kiroappio** | `POST/PATCH/DELETE /api/me/accounts/*` (6 个端点) | 按批次分账 | ❌ |
| **kiroappcc** | `GET /api/user/my-mothers` + `earnings/settlements` (5 个端点) | 手动结算收益 · 有"评价"字段 | ❌ |
| kiroceo / kirooo / kirodrop | 无 · 只作为纯客户 | – | – |

**这是**我方可以**反向变现的核心 · 我们同时也可以做 vendor 的供应商**。

### 2 · 抢号 UX 相关（差异化 · 抢到的关键）

| 能力 | 91kiro | kiroceo | kirooo | kiroappio | kiroappcc | kirodrop | 我方 |
|---|---|---|---|---|---|---|---|
| **client_order_id 幂等** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **单价保护 max_total_cny** | – | – | – | – | – | ✅ **独家** | ❌ |
| **持有上限 max_keys_held** | ✅ **独家** | – | – | – | – | – | ❌ |
| **响应带 order_id** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **补拉 GET /orders/{id}/keys** | ✅ | – | ✅ | – | – | – | 部分 |
| **分档定价响应 total_credits** | ✅ | – | – | ✅ | – | – | ❌ |
| **US/EU 双区** | ✅ | ✅ | ✅ | ✅ | – | ✅ | 部分 |
| **US/EU 合并通知** | – | – | – | – | – | ✅ | ❌ |

**要抢到号必须接的**：`max_total_cny` (kirodrop 独家 · 涨价保护) + `max_keys_held` (91kiro 独家 · 持有上限)

### 3 · Webhook 事件差异

| 事件 | 91kiro | kiroceo | kirooo | kiroappio | kiroappcc | kirodrop | 我方接了 |
|---|---|---|---|---|---|---|---|
| new_keys_available | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| all_keys_dead | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| test | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **warranty_refund** | ✅ **独家** | – | – | – | – | – | ✅ |
| **reserved_keys_delivered** | ✅ **独家** | – | – | – | – | – | ❌ |
| **key_revoked_abuse** | – | – | – | ✅ **独家** | – | – | ❌ |
| **visibility public/private** | – | – | – | ✅ **独家** | – | – | ❌ |
| **notification_scope dual (合并 US/EU)** | – | – | – | – | – | ✅ **独家** | ❌ |
| **US/EU 分开 2 条** | – | – | – | ✅ | – | – | – |

### 4 · 其他独家能力（每家一句话）

| 家 | 独家 |
|---|---|
| **91kiro** | Key 剩余额度同步 (usage) · 母号供应 · 双 URL webhook · 包量预留 · 车次 7 态 · 按存活时长降价 |
| **kirooo** | **发车 auto-fleet** (预留 + 自动车配置) · **Key 单价阶梯 bands** · Telegram 通知订阅（4 类）· 充值 USDT 链选择 |
| **kiroappio** | **AWS 母号供应侧全套** · **分档定价按母号累计产量** · 可自签发 API 令牌（`km_...`）· visibility 公私分推 · key_revoked_abuse 独家事件 |
| **kiroappcc** | 每批"评价" (拉完了/NPC/人上人/夯/NPC) · 双条件质保 (2h OR 7000 积分) · 车主分成 (earnings + settlements 端点) |
| **kirodrop** | **US/EU 合并通知一次** · **max_total_cny 涨价保护** · **/api/v1/reservation 完整报价** · status 端点**免鉴权**（探针无成本） |
| **kiroceo** | 最简 · 9 个端点全公开 · 无额外能力 · 但也无坑 |

## 我方 adapter 缺口清单 · 按优先级

### 紧急 · 稳定性护栏（推荐**立刻加**）
- [ ] **kirodrop max_total_cny** —— 涨价保护 · 一行改 adapter · 防"vendor 突然涨价我们照买"
- [ ] **91kiro max_keys_held** —— 持有上限 · 防"号堆着不用还占额度"

### 中等 · 抢号能力（数据观察后决定）
- [ ] kirodrop US/EU 合并通知处理（现在按老逻辑走 · 可能少收 EU 一半通知）
- [ ] kiroappio visibility=private 处理（不然会把自留车当公开车抢）
- [ ] kiroappio key_revoked_abuse 事件（vendor 主动收回我方号 · 我们要更新 credential_ledger）
- [ ] 91kiro reserved_keys_delivered（包量预留自动到货 · **不用 purchase**）

### 未来 · 反向变现（阶段 3+）
- [ ] 母号供应侧：91kiro / kiroappio / kiroappcc 三家母号 CRUD + 分成 ledger
- [ ] Key 额度实时同步（91kiro usage）· 精确判 key 死活
- [ ] Kirooo auto-fleet 用户端配置（我方作为 kirooo 客户可以享受这个能力 · 但用户不直接可见）

## 页面能看到但接口不确定的（**Network 未细扫**）

- kirodrop 首页监控 dashboard · 应该有 `/api/monitor` 之类
- kiroappio dashboard / /buy · 未抓 Network
- 91kiro webhook_deliveries · 已知端点 · 我方未用
- kiro91 mother 排队 · queue_position 未用

## 下一步

- 更新 `docs/17-vendor-work-order.md` A 层各家一行 · 用本表数据
- 关键：说明**"最小闭环已够 · 现在可以稳定运行 · 后续按需补差异化"**
- 剩下端点做进 backlog · 按用户可感优先级排

## 原始素材（本机 · 不入库）

- .playwright-mcp/vendor-scrape-2026-08-12/kiro91-docs.md      · 官方公开文档 29 KB
- .playwright-mcp/vendor-scrape-2026-08-12/kiro91-summary.txt   · 结构化摘要
- .playwright-mcp/vendor-scrape-2026-08-12/kiroceo-docs.txt     · 登录抓
- .playwright-mcp/vendor-scrape-2026-08-12/kirooo-docs.txt      · 登录抓
- .playwright-mcp/vendor-scrape-2026-08-12/kiroappio-docs.txt   · 登录抓
- .playwright-mcp/vendor-scrape-2026-08-12/kiroappcc-docs.txt   · 登录抓 + Network
- .playwright-mcp/vendor-scrape-2026-08-12/kirodrop-docs.txt    · 登录抓
