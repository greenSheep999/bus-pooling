import { MICRO, vendorLabel } from "@/lib/utils";
import type {
  Activity, ApiKey, AssignEvent, AutoPickResult, Bus, Credential, DownstreamConfig,
  ExtractEvent, ExtractRecord, LedgerEntry, Overview, Passenger, PullRound,
  StockSummary, TrendPoint, VendorHistory, VendorPricePoint, VendorPriceTrend,
  VendorShare, VendorStat, VendorStock, Wallet, WebhookConfig, WebhookDelivery, Zone,
} from "@/types";

const C = (n: number) => n * MICRO;
const ago = (h: number) => new Date(Date.now() - h * 3600_000).toISOString();

export const passenger: Passenger = {
  id: "psg_01H8Z3M",
  username: "danlio",
  email: "danlio@example.com",
  email_verified: true,
  created_at: ago(24 * 54),
  /* false = 散客视角（Vendor 01/02 + 加价）· 改 true 看社群视角 · decisions §8.20 */
  invited: false,
};

/** mock 内部 · 按当前 passenger 身份出 vendor 显示名 · decisions §8.20
    真实后端在 API 层做这个映射 · mock 里活动流 / 流水的文案也要走它，不许 hardcode 真名 */
const vl = (id: string) => vendorLabel(id, passenger.invited);

/** 附加费率 · decisions §8.20 · 无注册码且无消费码时叠加
    后台可组合多条（当前只有 region_markup）· UI 绝不展示这个数 */
export const SURCHARGE_RATE = 0.2;

/** 按身份算最终单价 · 有码 = 原价 · 无码 = 原价 × (1 + 加价) */
export const finalPrice = (base: number, waived: boolean) =>
  waived ? base : Math.round(base * (1 + SURCHARGE_RATE));

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
    id: "bus_kiro", name: "Kiro 常驻车", kind: "team", status: "active",
    member_count: 3, invite_code: "K7X-2M4", created_at: ago(24 * 30),
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
  /* record group · owner_bus_id=null · 用户通过 /pull 拉的号，还没派去向
     `/pull-records` 页会列这批 · 派 3 种去向后此字段更新 */
  mkCred(21, "kirooo",  1200, 4,  true, false, null),
  mkCred(22, "kirooo",  980,  4,  true, false, null),
  mkCred(23, "91kiro",  2200, 8,  true, false, null),
  mkCred(24, "kiroceo", 600,  2,  true, false, null),
  mkCred(25, "kirodrop",1450, 6,  true, false, null),
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
  /* 日常一号 · bus_daily */
  { id: "rd_20", vendor_id: "91kiro",  bus_id: "bus_daily", bus_name: "日常一号", result: "success", count_requested: 2, count_purchased: 2, alive_count: 2, dead_count: 0, push_state: "pushed",  push_ratio: null,  total_cost: -C(8),  fail_reason: null, created_at: ago(3.2) },
  { id: "rd_21", vendor_id: "kiroceo", bus_id: "bus_daily", bus_name: "日常一号", result: "success", count_requested: 2, count_purchased: 2, alive_count: 2, dead_count: 0, push_state: "none",    push_ratio: null,  total_cost: -C(7),  fail_reason: null, created_at: ago(24) },
  { id: "rd_22", vendor_id: "91kiro",  bus_id: "bus_daily", bus_name: "日常一号", result: "partial", count_requested: 3, count_purchased: 2, alive_count: 2, dead_count: 0, push_state: "partial", push_ratio: "2/3", total_cost: -C(8),  fail_reason: null, created_at: ago(76) },
  /* Kiro 常驻车 · bus_kiro · 手动模式，拉号频率低 */
  { id: "rd_30", vendor_id: "91kiro",  bus_id: "bus_kiro",  bus_name: "Kiro 常驻车", result: "success", count_requested: 2, count_purchased: 2, alive_count: 2, dead_count: 0, push_state: "pushed", push_ratio: null, total_cost: -C(8),  fail_reason: null, created_at: ago(18) },
  { id: "rd_31", vendor_id: "kirodrop", bus_id: "bus_kiro",  bus_name: "Kiro 常驻车", result: "success", count_requested: 3, count_purchased: 3, alive_count: 3, dead_count: 0, push_state: "pushed", push_ratio: null, total_cost: -C(9),  fail_reason: null, created_at: ago(52) },
];

/* ── 提取记录 ── */

export const extractRecords: ExtractRecord[] = [
  { id: "ex_01", vendor_id: "kirodrop", count: 2, destination: "push_pool", destination_label: "我的号池", alive_count: 2, dead_count: 0, credits_used: C(6400), lifespan_seconds: 6 * 3600, total_cost: -C(6), created_at: ago(0.5) },
  { id: "ex_02", vendor_id: "kirooo", count: 10, destination: "pending", destination_label: "待派去向", alive_count: 10, dead_count: 0, credits_used: C(1600), lifespan_seconds: 4 * 3600, total_cost: -C(25), created_at: ago(4.3) },
  { id: "ex_03", vendor_id: "91kiro", count: 1, destination: "into_bus", destination_label: "周末拼车局", alive_count: 1, dead_count: 0, credits_used: C(8200), lifespan_seconds: 31 * 3600, total_cost: -C(22), created_at: ago(7.9) },
  { id: "ex_04", vendor_id: "kiroceo", count: 3, destination: "handoff", destination_label: "已 handoff", alive_count: 0, dead_count: 0, credits_used: 0, lifespan_seconds: 0, total_cost: -C(18), created_at: ago(26) },
  { id: "ex_05", vendor_id: "kiroappio", count: 5, destination: "push_pool", destination_label: "我的号池", alive_count: 5, dead_count: 0, credits_used: C(4500), lifespan_seconds: 27 * 3600, total_cost: -C(15), created_at: ago(31) },
];

/* ── 上游库存（PullExtractModal 上游即时状态面板） · docs/14 §4.3 ── */

export const vendorStocks: Record<string, VendorStock> = {
  "91kiro": {
    vendor_id: "91kiro",
    currency: "credits",
    warranty_minutes: 10,
    max_per_order: 200,
    min_per_order: 1,
    hold_cap_remaining: 5,
    zones: [
      { zone: "us", label: "美国区", enabled: true, available: 42, unit_price: C(30) },
      { zone: "eu", label: "欧洲区", enabled: true, available: 8,  unit_price: C(35) },
    ],
  },
  "kiroceo": {
    vendor_id: "kiroceo",
    currency: "credits",
    warranty_minutes: 15,
    max_per_order: 10,
    min_per_order: 1,
    hold_cap_remaining: null,
    zones: [
      { zone: "us", label: "美国区", enabled: true, available: 12, unit_price: C(50) },
      { zone: "eu", label: "欧洲区", enabled: true, available: 6,  unit_price: C(35) },
    ],
  },
  "kirooo": {
    vendor_id: "kirooo",
    currency: "credits",
    warranty_minutes: 5,
    max_per_order: 50,
    min_per_order: 1,
    hold_cap_remaining: null,
    zones: [
      { zone: "us", label: "美国区", enabled: true, available: 0,  unit_price: C(28) },
      { zone: "eu", label: "欧洲区", enabled: true, available: 3,  unit_price: C(20) },
    ],
  },
  "kiroappio": {
    vendor_id: "kiroappio",
    currency: "credits",
    warranty_minutes: 20,
    max_per_order: 10,
    min_per_order: 1,
    hold_cap_remaining: null,
    zones: [
      { zone: "us", label: "美国区", enabled: true, available: 18, unit_price: C(25) },
      { zone: "eu", label: "欧洲区", enabled: true, available: 4,  unit_price: C(22) },
    ],
  },
  "kiroappcc": {
    vendor_id: "kiroappcc",
    currency: "credits",
    warranty_minutes: 0,           // 无质保
    max_per_order: 20,
    min_per_order: 1,
    hold_cap_remaining: null,
    zones: [                       // 无区域拆分 · 前端识别 zones.length===1 或 zone === undefined
      { zone: "us", label: "全区", enabled: true, available: 30, unit_price: C(50) },
    ],
  },
  "kirodrop": {
    vendor_id: "kirodrop",
    currency: "cny_usd",           // 混币 · UI 显示美元定价警示
    warranty_minutes: 30,
    max_per_order: 100,
    min_per_order: 1,
    hold_cap_remaining: null,
    zones: [
      { zone: "us", label: "美国区", enabled: true, available: 25, unit_price: C(45) },
      { zone: "eu", label: "欧洲区", enabled: true, available: 10, unit_price: C(38) },
    ],
  },
};

/* ── 我方历史统计（近 30 天） ── */

export const vendorHistories: Record<string, VendorHistory> = {
  "91kiro":    { vendor_id: "91kiro",    avg_lifespan_seconds: 12 * 3600, alive_rate_30d: 87, total_pulled_30d: 142 },
  "kiroceo":   { vendor_id: "kiroceo",   avg_lifespan_seconds: 18 * 3600, alive_rate_30d: 92, total_pulled_30d: 78  },
  "kirooo":    { vendor_id: "kirooo",    avg_lifespan_seconds: 4  * 3600, alive_rate_30d: 62, total_pulled_30d: 210 },
  "kiroappio": { vendor_id: "kiroappio", avg_lifespan_seconds: 24 * 3600, alive_rate_30d: 94, total_pulled_30d: 45  },
  "kiroappcc": { vendor_id: "kiroappcc", avg_lifespan_seconds: 8  * 3600, alive_rate_30d: 70, total_pulled_30d: 30  },
  "kirodrop":  { vendor_id: "kirodrop",  avg_lifespan_seconds: 20 * 3600, alive_rate_30d: 90, total_pulled_30d: 88  },
};

/* ── 系统派号推荐（auto 模式）· decisions §8.20 ──
   散客默认走这个 · 综合库存 + 单价 + 30 天成活率择优 · 返回最终价（已含附加费） */

export function autoPick(zone: Zone | "auto", waived: boolean): AutoPickResult {
  /* 候选：该 zone 有库存的 vendor */
  const candidates = Object.values(vendorStocks).flatMap((s) =>
    s.zones
      .filter((z) => z.available > 0 && (zone === "auto" || z.zone === zone))
      .map((z) => ({ stock: s, zone: z, hist: vendorHistories[s.vendor_id] })),
  );

  if (candidates.length === 0) {
    /* 全网缺货 · 返回一个空壳让 UI 显示缺货态 */
    const s = vendorStocks["91kiro"];
    return {
      vendor_label: "", vendor_id: "91kiro", zone: zone === "auto" ? "us" : zone,
      available: 0, unit_price: finalPrice(s.zones[0].unit_price, waived),
      warranty_minutes: s.warranty_minutes, max_per_order: s.max_per_order,
      min_per_order: s.min_per_order,
      avg_lifespan_seconds: 0, alive_rate_30d: 0, reason: "全网暂时缺货",
    };
  }

  /* 打分：成活率权重高 · 单价越低越好 · 库存够就行
     score = alive_rate / 100 × 0.6 + (1 - price/maxPrice) × 0.4 */
  const maxPrice = Math.max(...candidates.map((c) => c.zone.unit_price));
  const scored = candidates.map((c) => ({
    ...c,
    score:
      (c.hist?.alive_rate_30d ?? 50) / 100 * 0.6 +
      (1 - c.zone.unit_price / maxPrice) * 0.4,
  }));
  const best = scored.reduce((a, b) => (a.score >= b.score ? a : b));

  /* 推荐理由 · 一句人话 */
  const cheapest = candidates.reduce((a, b) => (a.zone.unit_price <= b.zone.unit_price ? a : b));
  const reason =
    best.stock.vendor_id === cheapest.stock.vendor_id
      ? "单价最低 · 库存充足"
      : (best.hist?.alive_rate_30d ?? 0) >= 90
        ? `30 天成活率 ${best.hist!.alive_rate_30d}% · 全网最稳`
        : "库存足 · 单价与成活率综合最优";

  return {
    vendor_label: "",                 // handler 按身份填
    vendor_id: best.stock.vendor_id,
    zone: best.stock.zones.length === 1 ? null : best.zone.zone,
    available: best.zone.available,
    unit_price: finalPrice(best.zone.unit_price, waived),
    warranty_minutes: best.stock.warranty_minutes,
    max_per_order: best.stock.max_per_order,
    min_per_order: best.stock.min_per_order,
    avg_lifespan_seconds: best.hist?.avg_lifespan_seconds ?? 0,
    alive_rate_30d: best.hist?.alive_rate_30d ?? 0,
    reason,
  };
}

/* ── vendor 价格走势 · Prices 页多线图 · decisions §8.22 ──
   前端 mock · mulberry32 + FNV-1a 生成稳定伪随机 · 每 vendor 不同波动率 · 每次刷新结果一致 */

function fnv1a(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

function mulberry32(seed: number) {
  let t = seed >>> 0;
  return () => {
    t = (t + 0x6d2b79f5) >>> 0;
    let x = t;
    x = Math.imul(x ^ (x >>> 15), x | 1);
    x ^= x + Math.imul(x ^ (x >>> 7), x | 61);
    return ((x ^ (x >>> 14)) >>> 0) / 4294967296;
  };
}

/** 每 vendor 的走势特征
 *  range = 价格振幅（相对基准价的 ±%）· outage = 缺货率
 *  不用「每天随机游走」—— 那样会累积成锯齿状抖动，不像真实价格走势
 *  改用「低频正弦叠加（趋势）+ 极小噪声」· 线平滑有方向感 */
const VOL: Record<string, { range: number; outage: number }> = {
  "91kiro":   { range: 0.14, outage: 0.02 },
  kiroceo:    { range: 0.09, outage: 0.03 },
  kirooo:     { range: 0.22, outage: 0.07 },     // 振幅最大 · 偶尔缺货
  kiroappio:  { range: 0.06, outage: 0.00 },     // 最稳 · 从不缺货
  kiroappcc:  { range: 0.11, outage: 0.05 },
  kirodrop:   { range: 0.18, outage: 0.03 },
};

/** 生成某 vendor 某区的价格走势
 *  @param zone 要看哪个区 · "auto" = 该 vendor 首选区（zones[0]）· 无区域 vendor 忽略此参数 */
export function vendorPriceTrend(
  vendorId: string,
  days = 30,
  waived = false,
  zone: Zone | "auto" = "auto",
): VendorPriceTrend {
  const stock = vendorStocks[vendorId];
  if (!stock) throw new Error(`unknown vendor ${vendorId}`);

  /* 无区域 vendor（zones 只有一条 label="全区"）· 忽略 zone 参数 */
  const noRegion = stock.zones.length === 1;
  const picked = noRegion
    ? stock.zones[0]
    : zone === "auto"
      ? stock.zones[0]
      : stock.zones.find((z) => z.zone === zone) ?? stock.zones[0];

  /* 基准价 = 该区当前单价（未加附加费）· 不同区价格不同，走势自然分离 */
  const base = picked.unit_price;
  const zoneOut = noRegion ? null : picked.zone;
  const cfg = VOL[vendorId] ?? { range: 0.10, outage: 0.03 };
  /* 种子带上区 · 同 vendor 的 us / eu 走势互相独立 */
  const rnd = mulberry32(fnv1a(`${vendorId}:${zoneOut ?? "all"}`));

  /* 走势 = 3 个不同周期的正弦波叠加（低频趋势）+ 极小噪声
     每 vendor 的相位 / 周期由种子决定 · 所以各家形态不同但都平滑 */
  const phase1 = rnd() * Math.PI * 2;
  const phase2 = rnd() * Math.PI * 2;
  const phase3 = rnd() * Math.PI * 2;
  const period1 = 18 + rnd() * 14;        // 主趋势 · 18-32 天一个周期
  const period2 = 7 + rnd() * 5;          // 次级 · 7-12 天
  const period3 = 3.5 + rnd() * 2;        // 短周期 · 3.5-5.5 天
  /* 整体漂移 · 让 30 天有个方向（涨 or 跌），不是纯震荡 */
  const drift = (rnd() - 0.5) * cfg.range * 0.8;

  const points: VendorPricePoint[] = [];
  const today = new Date();

  for (let i = days - 1; i >= 0; i--) {
    const d = new Date(today);
    d.setDate(today.getDate() - i);
    const date = d.toISOString().slice(0, 10);

    const x = days - 1 - i;                       // 0 .. days-1
    const t = days > 1 ? x / (days - 1) : 0;      // 0 .. 1 归一化

    /* 三层正弦 · 权重递减（0.55 / 0.3 / 0.15）· 合成平滑波形 */
    const wave =
      Math.sin((x / period1) * Math.PI * 2 + phase1) * 0.55 +
      Math.sin((x / period2) * Math.PI * 2 + phase2) * 0.30 +
      Math.sin((x / period3) * Math.PI * 2 + phase3) * 0.15;

    /* 极小噪声 · ±0.4% · 只做"不完全光滑"的质感，不产生锯齿 */
    const noise = (rnd() - 0.5) * 0.008;

    const factor = 1 + wave * cfg.range + drift * t + noise;
    const price = Math.round(finalPrice(base * factor, waived));

    /* 缺货日 · 价格照常报（forward fill 语义：价格不因缺货变化）· 只标 in_stock=false
       —— 缺货不代表价格变了，只是那天买不到 · 线不断、不归零 */
    points.push({ date, price, in_stock: rnd() >= cfg.outage });
  }

  const prices = points.map((p) => p.price);
  const current_price = prices[prices.length - 1];
  const price_high = Math.max(...prices);
  const price_low = Math.min(...prices);
  const change_30d_pct = Math.round(((current_price - prices[0]) / prices[0]) * 100);
  const outage_days = points.filter((p) => !p.in_stock).length;

  /* 最高 / 最低价那天 · 图上打点标注 */
  const peak_date = points[prices.indexOf(price_high)].date;
  const trough_date = points[prices.indexOf(price_low)].date;

  /* 最长连续有货天数 · 供货持续性 */
  let longest_streak_days = 0;
  let streak = 0;
  for (const p of points) {
    if (p.in_stock) {
      streak += 1;
      longest_streak_days = Math.max(longest_streak_days, streak);
    } else {
      streak = 0;
    }
  }

  return {
    vendor_id: vendorId,
    vendor_label: "",            // handler 按身份填
    zone: zoneOut,
    points,
    current_price,
    price_high,
    price_low,
    change_30d_pct,
    outage_days,
    in_stock_now: points[points.length - 1].in_stock,
    peak_date,
    trough_date,
    longest_streak_days,
  };
}

/* ── 提取事件（每次拉号）· docs/14 §3.3 提取历史 tab ── */

export const extractEvents: ExtractEvent[] = [
  { id: "ee_01", created_at: ago(0.5),  vendor_id: "kirodrop",  zone: "us", count_requested: 5, count_purchased: 5, total_cost: -C(12), result: "success", fail_reason: null, assigned_count: 0, pending_count: 5 },
  { id: "ee_02", created_at: ago(3.2),  vendor_id: "kiroceo",   zone: "eu", count_requested: 3, count_purchased: 3, total_cost: -C(8),  result: "success", fail_reason: null, assigned_count: 2, pending_count: 1 },
  { id: "ee_03", created_at: ago(6.0),  vendor_id: "kiroappcc", zone: null, count_requested: 2, count_purchased: 2, total_cost: -C(10), result: "success", fail_reason: null, assigned_count: 2, pending_count: 0 },
  { id: "ee_04", created_at: ago(9.7),  vendor_id: "kirooo",    zone: "us", count_requested: 3, count_purchased: 0, total_cost: 0,      result: "failed",  fail_reason: "缺货", assigned_count: 0, pending_count: 0 },
  { id: "ee_05", created_at: ago(22.3), vendor_id: "kirodrop",  zone: "us", count_requested: 2, count_purchased: 2, total_cost: -C(9),  result: "success", fail_reason: null, assigned_count: 2, pending_count: 0 },
  { id: "ee_06", created_at: ago(28.1), vendor_id: "91kiro",    zone: "us", count_requested: 5, count_purchased: 2, total_cost: -C(6),  result: "partial", fail_reason: null, assigned_count: 2, pending_count: 0 },
  { id: "ee_07", created_at: ago(50.5), vendor_id: "kiroappio", zone: "eu", count_requested: 1, count_purchased: 1, total_cost: -C(4),  result: "success", fail_reason: null, assigned_count: 1, pending_count: 0 },
  { id: "ee_08", created_at: ago(74.0), vendor_id: "kiroceo",   zone: "us", count_requested: 3, count_purchased: 3, total_cost: -C(15), result: "success", fail_reason: null, assigned_count: 3, pending_count: 0 },
];

/* ── 派发事件（每次派动作） · docs/14 §3.4 派发历史 tab ── */

export const assignEvents: AssignEvent[] = [
  { id: "ae_01", created_at: ago(0.2),  destination: "into_bus",  bus_id: "bus_weekend", bus_name: "周末拼车局", count: 3, credential_ids: ["cred_x1", "cred_x2", "cred_x3"], credential_maskeds: ["sk-****9a12", "sk-****4b57", "sk-****2c88"], vendors: ["kiroceo"] },
  { id: "ae_02", created_at: ago(0.4),  destination: "push_pool", bus_id: null,          bus_name: null,          count: 2, credential_ids: ["cred_x4", "cred_x5"],             credential_maskeds: ["sk-****7d3f", "sk-****1e02"],                 vendors: ["kiroappcc"] },
  { id: "ae_03", created_at: ago(4.1),  destination: "handoff",   bus_id: null,          bus_name: null,          count: 1, credential_ids: ["cred_x6"],                        credential_maskeds: ["sk-****9f4a"],                                vendors: ["kirodrop"] },
  { id: "ae_04", created_at: ago(22.0), destination: "into_bus",  bus_id: "bus_kiro",    bus_name: "Kiro 常驻车", count: 2, credential_ids: ["cred_x7", "cred_x8"],             credential_maskeds: ["sk-****3a91", "sk-****8b6c"],                 vendors: ["kirodrop"] },
  { id: "ae_05", created_at: ago(28.0), destination: "push_pool", bus_id: null,          bus_name: null,          count: 2, credential_ids: ["cred_x9", "cred_x10"],            credential_maskeds: ["sk-****4c22", "sk-****5d8e"],                 vendors: ["91kiro"] },
  { id: "ae_06", created_at: ago(50.2), destination: "into_bus",  bus_id: "bus_daily",   bus_name: "日常小车",   count: 1, credential_ids: ["cred_x11"],                       credential_maskeds: ["sk-****6e40"],                                vendors: ["kiroappio"] },
  { id: "ae_07", created_at: ago(74.1), destination: "handoff",   bus_id: null,          bus_name: null,          count: 3, credential_ids: ["cred_x12", "cred_x13", "cred_x14"], credential_maskeds: ["sk-****7f31", "sk-****8a20", "sk-****9b15"],  vendors: ["kiroceo"] },
];

/* ── 活动流 ── */

export const activities: Activity[] = [
  // 号流转（走 vendor → 车/号池 双 badge）· target 简洁一点，箭头承担"去向"语义
  { id: "a1", kind: "extract",  source: vl("kirodrop"),   target: "我的号池",     target_kind: "push_pool",  count: 2,  count_unit: "个 key", summary: `${vl("kirodrop")} → 我的号池`,             amount: -C(6),   created_at: ago(0.5), link: "/extract" },
  { id: "a2", kind: "push",     source: "号池",         target: "我的号池",     target_kind: "push_pool",  count: 2,  count_unit: "个号",   summary: "号池 → 我的号池",                   amount: null,    created_at: ago(0.6), link: "/settings/downstream" },
  { id: "a3", kind: "into_bus", source: vl("91kiro"), target: "Kiro 常驻车",   target_kind: "into_bus",   count: 3,  count_unit: "个号",   summary: `${vl("91kiro")} → Kiro 常驻车`,         amount: -C(8),   created_at: ago(1),   link: "/buses/bus_kiro" },
  { id: "a5", kind: "into_bus", source: vl("kiroceo"),    target: "周末拼车局",    target_kind: "into_bus",   count: 5,  count_unit: "个号",   summary: `${vl("kiroceo")} → 周末拼车局`,             amount: -C(12),  created_at: ago(2.7), link: "/buses/bus_weekend" },
  { id: "a7", kind: "extract",  source: vl("kirooo"),    target: "待派",          target_kind: "pending",    count: 10, count_unit: "个 key", summary: `${vl("kirooo")} → 待派`,                   amount: -C(25),  created_at: ago(4.3), link: "/extract" },

  // 非流转（走完整叙述文字，不套 badge）
  { id: "a4", kind: "refill",                                                                              summary: "周末拼车局 · 号少于 3 个 · 自动补车触发",                                                            amount: null,    created_at: ago(1.4), link: "/buses/bus_weekend" },
  { id: "a6", kind: "dead",                                                                                summary: `cred_…4F2 · ${vl("kirodrop")} · 存活 42h 后失效`,                                                          amount: null,    created_at: ago(3.5), link: "/buses/bus_weekend" },
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
  { id: "l1", type: "spend", amount: -C(6), balance_after: C(1245), memo: `提取 key · 2 个 · ${vl("kirodrop")}`, created_at: ago(0.5) },
  { id: "l2", type: "spend", amount: -C(25), balance_after: C(1251), memo: `提取 key · 10 个 · ${vl("kirooo")}`, created_at: ago(4.3) },
  { id: "l3", type: "spend", amount: -C(18), balance_after: C(1276), memo: `拿走 · 3 个号 · ${vl("kiroceo")}`, created_at: ago(26) },
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
