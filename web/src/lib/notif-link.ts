import type { Activity, ActivityKind } from "@/types";

/** 通知条目的跳转目标 · 前后端约定（后端优先 · 前端兜底）
 *
 *  规则：后端 `activity.link` 填了就直接用（**权威**）· 没填时按 kind 推一个默认页 ——
 *  后端权威因为 bus_id / owner_bus_id / cred_id 只有它能干净拿到；前端兜底防止后端
 *  某种 kind 忘了填导致点了没反应。
 *
 *  8 类通知的默认目标（跟后端 activities.go 保持一致）：
 *   - into_bus / refill        → 车详情（有 busID）· 找不到就 /buses
 *   - extract / handoff        → /extract（提取页 · 号在待派池 / 拿走历史）
 *   - dead                     → 号在车里 → 车详情 · 在待派池 → /extract
 *   - push                     → /settings/downstream（下游 pool 配置）
 *   - topup / redeem           → /wallet（钱包流水）
 */
const DEFAULT_BY_KIND: Record<ActivityKind, string> = {
  into_bus: "/buses",
  extract: "/extract",
  refill: "/buses",
  dead: "/extract",
  topup: "/wallet",
  redeem: "/wallet",
  push: "/settings/downstream",
  handoff: "/extract",
};

/** 返回该条通知的可点目标 · 后端有值优先 · 否则按 kind 兜底
 *  返回 null 意味着"故意不可点"（未来若有此类场景）· 目前所有 kind 都有兜底 */
export function notifLink(a: Activity): string | null {
  if (a.link) return a.link;
  return DEFAULT_BY_KIND[a.kind] ?? null;
}
