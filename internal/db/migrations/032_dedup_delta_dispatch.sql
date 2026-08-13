-- migration 032 · 清掉 stock-delta 的**重复批次行**（老键格式的孤儿）
--
-- **怎么来的**（2026-08-13 生产实测）：
--   1. 老 deriveStockDelta 拿 `region` 当对比键 · 不返 region 的 vendor 键里那段是空 ·
--      落成 `delta--<ts>`（两个横杠）
--   2. 键改成 `zone` 后（应用层已修）· 新行是 `delta-eu-<ts>` / `delta-general-<ts>`
--   3. `backfill-stock-delta` 从 raw_snapshot 重放历史时用的是**新键规则** ——
--      于是同一个探测点又落了一条新键行 · 老行还在
--
-- 结果：同 vendor + 同 dispatched_at + 同 count 的两条 delta 行并存 ·
-- /status 的"批次数 / 累计 key 数"**双倍虚增**（生产实测一家虚增 4 批 25 个 key）。
--
-- **只删能证明是重复的那些**：同 vendor、同时刻、同数量、且存在对应的新键行。
-- 没有配对新键行的老键行**保留** —— 那是真实观测 · 只是键不精确 · 删了就丢数据。
--
-- 不可逆：删的是重复行 · down 不做恢复（配对行仍在 · 数据没丢）。

-- +migrate up

DELETE FROM vendor_dispatch
 WHERE dispatch_key LIKE 'delta--%'
   AND EXISTS (
       SELECT 1 FROM vendor_dispatch b
        WHERE b.vendor_id     = vendor_dispatch.vendor_id
          AND b.dispatched_at = vendor_dispatch.dispatched_at
          AND b.count         = vendor_dispatch.count
          AND b.source        = vendor_dispatch.source
          AND b.dispatch_key LIKE 'delta-%'
          AND b.dispatch_key NOT LIKE 'delta--%'
   );

-- +migrate down

-- 无操作 · 删掉的是重复行（每条都有等价的新键行留着）· 无从也无需恢复
SELECT 1;
