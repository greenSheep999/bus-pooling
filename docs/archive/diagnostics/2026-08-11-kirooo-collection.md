# kirooo 采集中断 · 诊断报告（2026-08-12）

## 症状

- 探针 24h alive 率 **75%**（其他家 98%+）
- 用户报 kirooo 8-11 23:32 上新 +19/+14 批次 · 我方 `vendor_dispatch` 缺失

## 结论

**不是采集中断 · 是幽灵事件 + 端点选错**（两个独立问题）。

## 证据链

### 事件 1 · 8 小时窗口的 `error_kind='other'`（已自愈）

```
2026-08-10 15:00 ～ 8-10 23:00  →  287 次 error_kind='other' · alive=0
2026-08-10 23:00 之后            →  0 次 error · alive=100%
```

- 全部 287 条 error 探针 `latency_ms=NULL` 且 `raw_snapshot=NULL` · 说明请求**未建成或未收到响应**
- 时间集中在 8 小时窗内 · 之后再没复现 · **是 vendor 侧短期故障** · 已自然恢复
- 探针无对应错误码入库（只有 error_kind 分类没有原始消息）· 只能推断是 network / 5xx 之类

**改进项**（可选）：探针 error 时也落 `raw_snapshot` 存原始响应 · 未来同类事件能定根因。

### 事件 2 · 用户报 23:32 上新漏采 · 根因是 dispatch_key 端点错配

**探针端点** `/api/my/stock`（我方账户视角）· 只返：
```json
{"credits":70,"stock":0,"claimable":0,"max":0,"unit_price":100,"remaining":70}
```
**没有 region 拆分** · 也**没有 dispatches 数组**。

**fleet 端点** `/api/my/stock/regions`（全网视角）· 结构：
```json
{
  "fleet_active": true,
  "regions": [
    {"region":"us-east-1", "dispatches":[{"time":"2026-08-11 20:50:00","delivered":19,...}]},
    {"region":"eu-central-1", "dispatches":[...]}
  ]
}
```
**有 region + dispatches 数组** · 但当下 stock=0 时 dispatches 也是空。

**Backfiller 每 5min 拉 fleet 端点** · 落 `vendor_dispatch` source=vendor_self。
23:32 那批用户看到的 · 我方 22:55 fleet_started_at 之后才启动记录 · 中间时段的历史 batches vendor 侧不保留（`fleet_active` 只保留当前 session）· **backfill 拉不到过去的**。

## 根因分析

1. **explicit 采集断层**：vendor 侧 fleet 端点**只保留当前 fleet session 的 batches** · 我方 5min 拉一次 · 两次之间 vendor fleet 挂了又起 · 历史批次会丢
2. **探针 stock 端点没 region**：stock-delta 推算路径下 · region 信息全缺 · 落 `vendor_dispatch.region = ''` · 无法按区拆
3. **探针 error 无溯源**：287 次 error 探针没存 raw · 事后无法定根因

## 修法

### 短期修（现在做）

- **探针改打 fleet 端点** `/api/my/stock/regions` · 拿到 regions[].stock 拆分 + fleet_active/started_at 信号
- 相同响应体 · 直接从 `regions[].dispatches[]` 抽当前 session 的批次 · 落 `vendor_dispatch` 附带 region · 频次上 60s 比 Backfiller 5min 密 12 倍
- **error 时落 raw_snapshot** · 未来同类事件能定根因

### 中期修（Step 5 xi8 backfill 覆盖）

- xi8 restock-log 记的是"通过定时轮询 + prev_stock delta 推算"的历史 · 保留时间比 vendor 自家 fleet_session 长
- 一次性 xi8 backfill 能补齐 vendor fleet 断层期的批次（source='xi8'）

### 长期修（Step §11.7 · 待定）

- 让 vendor 加 webhook `fleet.dispatched` 事件 · push 拿数据不再靠 poll · 阶段 3+

## 数据校对（xi8 vs vendor_self）

xi8 记 kirooo 8-11 23:32 上新 +19/+14 · 我方 db 里没有（fleet 端点在那时可能 fleet_active=false，或 backfill 恰好在两轮之间）· 修完时区 + 探针改端点 + Step 5 xi8 backfill 之后 · 预期能对上。
