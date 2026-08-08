import { MICRO } from "@/lib/utils";
import type {
  Activity, ApiKey, Bus, Credential, DownstreamConfig, ExtractRecord,
  LedgerEntry, Overview, Passenger, PullRound, StockSummary, TrendPoint,
  VendorShare, VendorStat, Wallet, WebhookConfig, WebhookDelivery,
} from "@/types";

const C = (n: number) => n * MICRO;
const ago = (h: number) => new Date(Date.now() - h * 3600_000).toISOString();

export const passenger: Passenger = {
  id: "psg_01H8Z3M",
  username: "danlio",
  email: "danlio@example.com",
  email_verified: true,
  created_at: ago(24 * 54),
};

export const wallet: Wallet = {
  balance: C(1245),
  reserved: C(0),
  updated_at: ago(0.2),
};

export const stock: StockSummary = {
  total_available: 128,
  by_vendor: [
    { vendor_id: "91kiro", available: 42 },
    { vendor_id: "kiroceo", available: 31 },
    { vendor_id: "kirooo", available: 24 },
    { vendor_id: "kirodrop", available: 18 },
    { vendor_id: "kiroappio", available: 13 },
    { vendor_id: "kiroappcc", available: 0 },
  ],
};

/* ── Bus ── */

export const buses: Bus[] = [
  {
    id: "bus_weekend", name: "周末拼车局", kind: "single", status: "active",
    member_count: 1, invite_code: null, created_at: ago(24 * 8),
    alive_count: 12, dead_count: 1, spend_today: C(28),
    avg_lifespan_seconds: 42 * 3600,
    strategy: {
      auto_refill_enabled: true, refill_watermark: 3, refill_min_count: 5,
      per_round_count: 3, max_unit_price: C(25), daily_round_limit: 20,
      daily_spend_limit: C(200), preferred_vendor: null,
    },
  },
  {
    id: "bus_daily", name: "日常一号", kind: "single", status: "active",
    member_count: 1, invite_code: null, created_at: ago(24 * 15),
    alive_count: 4, dead_count: 0, spend_today: C(12),
    avg_lifespan_seconds: 36 * 3600,
    strategy: {
      auto_refill_enabled: true, refill_watermark: 2, refill_min_count: 3,
      per_round_count: 2, max_unit_price: C(22), daily_round_limit: 10,
      daily_spend_limit: C(100), preferred_vendor: null,
    },
  },
  {
    id: "bus_kiro", name: "Kiro 常驻车", kind: "single", status: "active",
    member_count: 1, invite_code: null, created_at: ago(24 * 30),
    alive_count: 6, dead_count: 1, spend_today: C(5),
    avg_lifespan_seconds: 28 * 3600,
    strategy: {
      auto_refill_enabled: false, refill_watermark: 2, refill_min_count: null,
      per_round_count: 2, max_unit_price: null, daily_round_limit: null,
      daily_spend_limit: null, preferred_vendor: "91kiro",
    },
  },
];

/* ── 号 ── */

const mkCred = (
  i: number, vendor: string, used: number, lifeH: number,
  alive: boolean, pushed: boolean, busId: string | null,
): Credential => ({
  id: `cred_01H8Z3M${String(i).padStart(3, "0")}`,
  vendor_id: vendor,
  status: alive ? "alive" : "dead",
  key_masked: `ksk_live_${Math.random().toString(36).slice(2, 6)}…${Math.random().toString(36).slice(2, 5)}`,
  account: `aws-${Math.random().toString(36).slice(2, 6)}@kiro.tmp`,
  region: vendor === "kirodrop" ? "eu-west-1" : vendor === "91kiro" ? "us-east-1" : "",
  issuer_url: vendor === "91kiro" ? "auth.91kiro.com" : vendor === "kiroceo" ? "api.kiro.ceo" : "",
  credits_used: C(used),
  pulled_at: ago(lifeH),
  warranty_until: ago(lifeH - 48),
  dead_at: alive ? null : ago(2),
  lifespan_seconds: lifeH * 3600,
  paid: C(vendor === "kirodrop" ? 15 : vendor === "kiroceo" ? 18.5 : 20),
  owner_bus_id: busId,
  pushed_at: pushed ? ago(lifeH - 0.05) : null,
  push_failed: false,
});

export const credentials: Credential[] = [
  mkCred(1, "91kiro", 6400, 42, true, true, "bus_weekend"),
  mkCred(2, "91kiro", 5800, 42, true, true, "bus_weekend"),
  mkCred(3, "kiroceo", 4100, 38, true, true, "bus_weekend"),
  mkCred(4, "kiroceo", 3700, 38, true, false, "bus_weekend"),
  mkCred(5, "kirodrop", 8200, 31, true, true, "bus_weekend"),
  mkCred(6, "kirodrop", 10000, 22, false, false, "bus_weekend"),
  mkCred(7, "91kiro", 2900, 28, true, true, "bus_weekend"),
  mkCred(8, "kiroceo", 5200, 24, true, true, "bus_weekend"),
  mkCred(9, "kiroceo", 4700, 24, true, true, "bus_weekend"),
  mkCred(10, "91kiro", 7100, 19, true, false, "bus_weekend"),
  mkCred(11, "kirodrop", 3300, 16, true, true, "bus_weekend"),
  mkCred(12, "kirodrop", 2600, 16, true, true, "bus_weekend"),
  mkCred(13, "kirooo", 1800, 12, true, false, "bus_weekend"),
  mkCred(14, "91kiro", 3100, 36, true, true, "bus_daily"),
  mkCred(15, "91kiro", 2200, 36, true, true, "bus_daily"),
  mkCred(16, "kiroceo", 1500, 20, true, false, "bus_daily"),
  mkCred(17, "kiroceo", 900, 20, true, true, "bus_daily"),
  mkCred(18, "kirodrop", 4400, 28, true, true, "bus_kiro"),
  mkCred(19, "kirodrop", 3900, 28, true, true, "bus_kiro"),
  mkCred(20, "91kiro", 5100, 26, true, true, "bus_kiro"),
];

/* ── 拉号历史 ── */

export const pullRounds: PullRound[] = [
  { id: "rd_01", vendor_id: "91kiro", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "success", count_requested: 5, count_purchased: 5, alive_count: 5, dead_count: 0, push_state: "pushed", push_ratio: null, total_cost: -C(12), fail_reason: null, created_at: ago(0.5) },
  { id: "rd_02", vendor_id: "kiroceo", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "partial", count_requested: 3, count_purchased: 3, alive_count: 2, dead_count: 1, push_state: "partial", push_ratio: "2/3", total_cost: -C(8), fail_reason: null, created_at: ago(2.7) },
  { id: "rd_03", vendor_id: "kirodrop", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "success", count_requested: 2, count_purchased: 2, alive_count: 2, dead_count: 0, push_state: "pushed", push_ratio: null, total_cost: -C(6), fail_reason: null, created_at: ago(7.8) },
  { id: "rd_04", vendor_id: "kirooo", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "failed", count_requested: 3, count_purchased: 0, alive_count: 0, dead_count: 0, push_state: "none", push_ratio: null, total_cost: 0, fail_reason: "缺货", created_at: ago(22) },
  { id: "rd_05", vendor_id: "91kiro", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "success", count_requested: 2, count_purchased: 2, alive_count: 2, dead_count: 0, push_state: "pushed", push_ratio: null, total_cost: -C(6), fail_reason: null, created_at: ago(27) },
  { id: "rd_06", vendor_id: "91kiro", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "success", count_requested: 1, count_purchased: 1, alive_count: 1, dead_count: 0, push_state: "none", push_ratio: null, total_cost: -C(4), fail_reason: null, created_at: ago(33) },
  { id: "rd_07", vendor_id: "kiroceo", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "success", count_requested: 2, count_purchased: 2, alive_count: 2, dead_count: 0, push_state: "pushed", push_ratio: null, total_cost: -C(7), fail_reason: null, created_at: ago(50) },
  { id: "rd_08", vendor_id: "91kiro", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "partial", count_requested: 2, count_purchased: 1, alive_count: 1, dead_count: 1, push_state: "none", push_ratio: null, total_cost: -C(4), fail_reason: null, created_at: ago(58) },
  { id: "rd_09", vendor_id: "kirodrop", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "success", count_requested: 2, count_purchased: 2, alive_count: 2, dead_count: 0, push_state: "pushed", push_ratio: null, total_cost: -C(5), fail_reason: null, created_at: ago(79) },
  { id: "rd_10", vendor_id: "kirooo", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "success", count_requested: 1, count_purchased: 1, alive_count: 1, dead_count: 0, push_state: "failed", push_ratio: null, total_cost: -C(8), fail_reason: null, created_at: ago(102) },
  { id: "rd_11", vendor_id: "kiroappio", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "refunded", count_requested: 1, count_purchased: 1, alive_count: 0, dead_count: 1, push_state: "none", push_ratio: null, total_cost: C(9), fail_reason: "30 分钟内失效 · 质保退款", created_at: ago(126) },
  { id: "rd_12", vendor_id: "kiroceo", bus_id: "bus_weekend", bus_name: "周末拼车局", result: "success", count_requested: 3, count_purchased: 3, alive_count: 3, dead_count: 0, push_state: "pushed", push_ratio: null, total_cost: -C(10), fail_reason: null, created_at: ago(150) },
];

/* ── 提取记录 ── */

export const extractRecords: ExtractRecord[] = [
  { id: "ex_01", vendor_id: "kirodrop", count: 2, destination: "push_pool", destination_label: "我的号池", alive_count: 2, dead_count: 0, credits_used: C(6400), lifespan_seconds: 6 * 3600, total_cost: -C(6), created_at: ago(0.5) },
  { id: "ex_02", vendor_id: "kirooo", count: 10, destination: "pending", destination_label: "待派去向", alive_count: 10, dead_count: 0, credits_used: C(1600), lifespan_seconds: 4 * 3600, total_cost: -C(25), created_at: ago(4.3) },
  { id: "ex_03", vendor_id: "91kiro", count: 1, destination: "into_bus", destination_label: "周末拼车局", alive_count: 1, dead_count: 0, credits_used: C(8200), lifespan_seconds: 31 * 3600, total_cost: -C(22), created_at: ago(7.9) },
  { id: "ex_04", vendor_id: "kiroceo", count: 3, destination: "handoff", destination_label: "已 handoff", alive_count: 0, dead_count: 0, credits_used: 0, lifespan_seconds: 0, total_cost: -C(18), created_at: ago(26) },
  { id: "ex_05", vendor_id: "kiroappio", count: 5, destination: "push_pool", destination_label: "我的号池", alive_count: 5, dead_count: 0, credits_used: C(4500), lifespan_seconds: 27 * 3600, total_cost: -C(15), created_at: ago(31) },
];

/* ── 活动流 ── */

export const activities: Activity[] = [
  // 号流转（走 vendor → 车/号池 双 badge）· target 简洁一点，箭头承担"去向"语义
  { id: "a1", kind: "extract",  source: "Kiro Drop",   target: "我的号池",     target_kind: "push_pool",  count: 2,  count_unit: "个 key", summary: "Kiro Drop → 我的号池",             amount: -C(6),   created_at: ago(0.5), link: "/extract" },
  { id: "a2", kind: "push",     source: "号池",         target: "我的号池",     target_kind: "push_pool",  count: 2,  count_unit: "个号",   summary: "号池 → 我的号池",                   amount: null,    created_at: ago(0.6), link: "/settings/downstream" },
  { id: "a3", kind: "into_bus", source: "Kiro Market", target: "Kiro 常驻车",   target_kind: "into_bus",   count: 3,  count_unit: "个号",   summary: "Kiro Market → Kiro 常驻车",         amount: -C(8),   created_at: ago(1),   link: "/buses/bus_kiro" },
  { id: "a5", kind: "into_bus", source: "Kiro CEO",    target: "周末拼车局",    target_kind: "into_bus",   count: 5,  count_unit: "个号",   summary: "Kiro CEO → 周末拼车局",             amount: -C(12),  created_at: ago(2.7), link: "/buses/bus_weekend" },
  { id: "a7", kind: "extract",  source: "Kiro OOO",    target: "待派",          target_kind: "pending",    count: 10, count_unit: "个 key", summary: "Kiro OOO → 待派",                   amount: -C(25),  created_at: ago(4.3), link: "/extract" },

  // 非流转（走完整叙述文字，不套 badge）
  { id: "a4", kind: "refill",                                                                              summary: "周末拼车局 · 号少于 3 个 · 自动补车触发",                                                            amount: null,    created_at: ago(1.4), link: "/buses/bus_weekend" },
  { id: "a6", kind: "dead",                                                                                summary: "cred_…4F2 · Kiro Drop · 存活 42h 后失效",                                                          amount: null,    created_at: ago(3.5), link: "/buses/bus_weekend" },
  { id: "a8", kind: "topup",                                                                               summary: "waffo · 支付宝支付 200 元 · 通道费 10 元 · 到账 190 积分",                                            amount: C(190),  created_at: ago(9.8), link: "/wallet" },
  { id: "a9", kind: "redeem",                                                                              summary: "兑换码 KIRO-8Q2P · 邀请奖励 · 到账 50 积分",                                                        amount: C(50),   created_at: ago(12),  link: "/wallet" },
];

/* ── Vendor ── */

/* avg_credits_per_cred = 每号平均能用多少积分才挂 · warranty_count = 被 30 分钟内挂退款的次数 */
export const vendorStats: VendorStat[] = [
  { vendor_id: "91kiro",    rank: 1, unit_price: C(20),   avg_lifespan_seconds: 42 * 3600, effective_cost: 0.48, avg_credits_per_cred: C(8200), warranty_count: 0, alive_rate: 98, pulls_today: 12, fallback_count: 0, out_of_stock: false },
  { vendor_id: "kiroceo",   rank: 2, unit_price: C(18.5), avg_lifespan_seconds: 36 * 3600, effective_cost: 0.51, avg_credits_per_cred: C(6800), warranty_count: 0, alive_rate: 95, pulls_today: 8,  fallback_count: 0, out_of_stock: false },
  { vendor_id: "kirooo",    rank: 3, unit_price: C(22),   avg_lifespan_seconds: 38 * 3600, effective_cost: 0.58, avg_credits_per_cred: C(5900), warranty_count: 1, alive_rate: 92, pulls_today: 3,  fallback_count: 1, out_of_stock: false },
  { vendor_id: "kirodrop",  rank: 4, unit_price: C(15),   avg_lifespan_seconds: 22 * 3600, effective_cost: 0.68, avg_credits_per_cred: C(3800), warranty_count: 2, alive_rate: 88, pulls_today: 5,  fallback_count: 2, out_of_stock: false },
  { vendor_id: "kiroappio", rank: 5, unit_price: C(25),   avg_lifespan_seconds: 30 * 3600, effective_cost: 0.83, avg_credits_per_cred: C(3500), warranty_count: 0, alive_rate: 85, pulls_today: 0,  fallback_count: 0, out_of_stock: false },
  { vendor_id: "kiroappcc", rank: 6, unit_price: 0,       avg_lifespan_seconds: 0,          effective_cost: 0,    avg_credits_per_cred: 0,       warranty_count: 0, alive_rate: 0,  pulls_today: 0,  fallback_count: 0, out_of_stock: true  },
];

/* 6 家 vendor 全列，跟 vendorStats 一一对应 · pulls=0 前端渲染成 "-" 而不是 "0 次" */
export const vendorShare: VendorShare[] = [
  { vendor_id: "91kiro",    pulls: 12, ratio: 0.43 },
  { vendor_id: "kiroceo",   pulls: 8,  ratio: 0.28 },
  { vendor_id: "kirodrop",  pulls: 5,  ratio: 0.18 },
  { vendor_id: "kirooo",    pulls: 3,  ratio: 0.11 },
  { vendor_id: "kiroappio", pulls: 0,  ratio: 0    },
  { vendor_id: "kiroappcc", pulls: 0,  ratio: 0    },
];

/* ── 概览 ── */

export const overview: Overview = {
  kpi: {
    balance: C(1245), balance_delta_topup: C(2500), balance_delta_spend: C(1255),
    spend_today: C(45), spend_yesterday: C(32),
    pull_total: 128, pull_this_month: 42,
    alive_count: 12, dead_count: 2, pending_refill: 1,
    avg_lifespan_seconds: 42 * 3600,
  },
  buses: {
    bus_count: 3, total_credentials: 22, refill_count: 2, coalesce_rate: 0.87,
    /* 阶段 1a single bus 全是自己发起 · 阶段 2 team/anon 起有 member 车才有意义
       此处第 3 辆设为 member 供 UI 视觉演示（后端接通后按真实 role 填） */
    items: buses.map((b, i) => ({
      id: b.id, name: b.name,
      role: i === 2 ? "member" as const : "owner" as const,
      alive: b.alive_count, dead: b.dead_count, spend: b.spend_today,
    })),
  },
  extract: {
    /* handoff 是 fire-and-forget（DELETE /credentials/{id}，号离开系统我方不监控），
       不应在"当前号池去向分布"里 —— 那是号池快照，handoff 走的号早已不在池子里。
       想知道"拿走了几个"看活动记录 · CLAUDE.md:33 + 00 §358 */
    count_today: 4, total_credentials: 17, pending: 10, spend: C(49),
    by_destination: [
      { destination: "pending", count: 10 },
      { destination: "into_bus", count: 5 },
      { destination: "push_pool", count: 2 },
    ],
  },
};

/* 基线 ~50 · 两段平缓起伏 · 相邻日差 ≤4 保证曲线顺滑 */
const SEED = [
  38, 40, 43, 46, 50, 54, 57, 60, 62, 61,
  58, 55, 51, 48, 45, 44, 45, 47, 50, 53,
  56, 58, 59, 58, 56, 54, 52, 51, 50, 50,
];

/* scope 用 hash 出一个稳定的 (offset, scale) 让每辆车 / 每 vendor 曲线区分开
   —— mock 演示用，落码时改成从事件流按 bus_id / vendor 聚合 */
const SCOPE_SHIFT: Record<string, { off: number; k: number }> = {
  bus_weekend: { off: 0, k: 0.55 },
  bus_daily: { off: 7, k: 0.20 },
  bus_kiro: { off: 14, k: 0.25 },
  "91kiro": { off: 3, k: 0.42 },
  kiroceo: { off: 10, k: 0.28 },
  kirooo: { off: 18, k: 0.18 },
  kirodrop: { off: 21, k: 0.22 },
  kiroappio: { off: 5, k: 0.12 },
  kiroappcc: { off: 0, k: 0 }, // 缺货 vendor 恒为 0
};

export function trend(
  metric: string,
  days = 30,
  scope?: { busId?: string; vendor?: string },
): TrendPoint[] {
  const scale = metric === "credits" ? 1 : metric === "pulls" ? 0.06 : 0.55;
  const key = scope?.busId ?? scope?.vendor;
  const shift = key ? SCOPE_SHIFT[key] ?? { off: 0, k: 0.3 } : { off: 0, k: 1 };

  return Array.from({ length: days }, (_, i) => {
    const d = new Date();
    d.setDate(d.getDate() - (days - 1 - i));
    const v = SEED[(i + shift.off) % SEED.length] * scale * shift.k;
    return {
      date: d.toISOString().slice(0, 10),
      value: metric === "pulls" ? Math.max(0, Math.round(v)) : Math.round(v * 10) / 10,
    };
  });
}

/* ── 钱包流水 ── */

export const ledger: LedgerEntry[] = [
  { id: "l1", type: "spend", amount: -C(6), balance_after: C(1245), memo: "提取 key · 2 个 · Kiro Drop", created_at: ago(0.5) },
  { id: "l2", type: "spend", amount: -C(25), balance_after: C(1251), memo: "提取 key · 10 个 · Kiro OOO", created_at: ago(4.3) },
  { id: "l3", type: "spend", amount: -C(18), balance_after: C(1276), memo: "拿走 · 3 个号 · Kiro CEO", created_at: ago(26) },
  { id: "l4", type: "topup", amount: C(190), balance_after: C(1294), memo: "waffo · 支付宝 · 200 元（通道费 10）", created_at: ago(9.8) },
  { id: "l5", type: "redeem", amount: C(50), balance_after: C(1104), memo: "兑换码 KIRO-8Q2P · 邀请奖励", created_at: ago(12) },
  { id: "l6", type: "warranty_refund", amount: C(9), balance_after: C(1054), memo: "号 30 分钟内失效 · 质保退款", created_at: ago(126) },
];

/* ── 配置 ── */

export const downstream: DownstreamConfig = {
  passengerpool_url: "https://kiro-my.example.com",
  passengerpool_token_masked: "kiro_admin_••••••••••••••••a3f2",
  connected: true,
  last_heartbeat_at: ago(0.03),
  push_success_rate: 0.982,
  push_total: 156,
  push_failed: 3,
  rules: { push_on_pull: true, resync_on_dead: true, retry_on_failure: true, bus_only: false },
};

export const webhook: WebhookConfig = {
  url: "https://bot.example.com/kiro-events",
  secret_masked: "whsec_•••••••••••••••••••••••3f2a",
  enabled: true,
  events: ["round.completed", "round.failed", "credential.dead", "wallet.low"],
};

export const webhookDeliveries: WebhookDelivery[] = [
  { id: "w1", event: "round.completed", ok: true, status_code: 200, attempt: 1, latency_ms: 124, created_at: ago(0.5) },
  { id: "w2", event: "credential.dead", ok: true, status_code: 200, attempt: 1, latency_ms: 98, created_at: ago(1) },
  { id: "w3", event: "round.completed", ok: true, status_code: 200, attempt: 1, latency_ms: 115, created_at: ago(2.7) },
  { id: "w4", event: "round.failed", ok: false, status_code: 502, attempt: 3, latency_ms: 5200, created_at: ago(4.3) },
  { id: "w5", event: "wallet.low", ok: true, status_code: 200, attempt: 2, latency_ms: 820, created_at: ago(9.8) },
];

export const apiKeys: ApiKey[] = [
  { id: "k1", name: "生产 · N8N 机器人", prefix: "sk_live_a3f2", last_used_at: ago(0.03), created_at: ago(24 * 18), revoked: false },
  { id: "k2", name: "CI 脚本", prefix: "sk_live_8c1b", last_used_at: ago(3), created_at: ago(24 * 54), revoked: false },
  { id: "k3", name: "临时测试", prefix: "sk_test_5f9d", last_used_at: null, created_at: ago(24 * 10), revoked: true },
];
