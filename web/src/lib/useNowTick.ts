import { useEffect, useState } from "react";

/** 让"随时间变化的显示值"自己走 —— 不用手动刷新页面
 *
 *  用在哪:寿命（存活时长）和它派生的评价档。这两个是**纯算术**（now - pulled_at）·
 *  不需要问后端 · 本地 tick 就能准。
 *
 *  为什么不只靠 refetchInterval:那是 60s 一次 · 中间这一分钟数字是冻住的 ·
 *  用户盯着看会觉得"卡住了"。用量真值必须等后端（号池 5min 采样）· 但寿命不必。
 *
 *  periodMs 默认 30s —— 寿命显示到分钟级（fmtLifespan）· 30s 足够跟上 ·
 *  比每秒 setState 省得多（列表几十行 · 每秒重渲染没必要）。
 */
export function useNowTick(periodMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), periodMs);
    return () => clearInterval(id);
  }, [periodMs]);
  return now;
}

/** 号活了多久（秒）· 活号算到 now（跟着 tick 走）· 死号算到 dead_at（定值）
 *
 *  **优先用后端给的 lifespan_seconds** —— 它是后端算的权威值（api/lifespan.go）·
 *  本地只在"活号 + 有 pulled_at"时按 now 重算 · 让数字在两次 refetch 之间也走。
 */
export function liveLifespanSeconds(
  c: { pulled_at?: string; dead_at?: string | null; lifespan_seconds?: number },
  now: number,
): number {
  // 死号:后端值就是终值 · 不重算
  if (c.dead_at) return c.lifespan_seconds ?? 0;
  if (!c.pulled_at) return c.lifespan_seconds ?? 0;
  const t0 = new Date(c.pulled_at).getTime();
  if (!Number.isFinite(t0)) return c.lifespan_seconds ?? 0;
  const secs = Math.floor((now - t0) / 1000);
  return secs > 0 ? secs : (c.lifespan_seconds ?? 0);
}
