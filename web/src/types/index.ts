/**
 * 前端类型 · 只含**用户可见态**（CLAUDE.md §12.5 状态收敛）
 * 内部枚举（initiated / reserved / imported / handed_off …）由后端 API 层映射后再下发。
 */

// ── 通用
export type Money = number; // microunit
export type ISOTime = string;

export interface Paged<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

// ── 乘客 / 钱包
export interface Passenger {
  id: string;
  username: string;
  email: string;
  email_verified: boolean;
  created_at: ISOTime;
  /** 注册时填过邀请码 · decisions §8.20
   *  true  = 社群：看 vendor 真名 + 无加价
   *  false = 散客：看 Vendor 01/02 + 默认加价（拉号时填消费码可免） */
  invited: boolean;
}

export interface Wallet {
  balance: Money;
  reserved: Money;
  updated_at: ISOTime;
}

export type LedgerType =
  | "topup"
  | "spend"
  | "redeem"
  | "refund"
  | "warranty_refund";

export interface LedgerEntry {
  id: string;
  type: LedgerType;
  amount: Money; // 正=入账 负=出账
  balance_after: Money;
  memo: string;
  created_at: ISOTime;
}

// ── Bus
export type BusKind = "single" | "anon" | "team";
export type BusStatus = "active" | "dissolved"; // UI: 活跃 / 已解散

export interface BusStrategy {
  auto_refill_enabled: boolean;
  refill_watermark: number;
  refill_min_count: number | null;
  per_round_count: number | null;
  max_unit_price: Money | null;
  daily_round_limit: number | null;
  daily_spend_limit: Money | null;
  preferred_vendor: string | null;
}

export interface Bus {
  id: string;
  name: string;
  kind: BusKind;
  status: BusStatus;
  member_count: number;
  invite_code: string | null; // team 才有
  created_at: ISOTime;
  // 汇总
  alive_count: number;
  dead_count: number;
  spend_today: Money;
  avg_lifespan_seconds: number;
  strategy: BusStrategy;
}

export interface BusMember {
  passenger_id: string;
  username: string;
  role: "owner" | "member";
  joined_at: ISOTime;
}

// ── 号（credential）· UI 只有 活 / 已失效
export type CredentialStatus = "alive" | "dead";

export interface Credential {
  id: string;
  vendor_id: string;
  status: CredentialStatus;
  // 凭据（按 vendor KeyPayloadShape 分派，空串=该 vendor 不给）
  key_masked: string; // 列表页只给打码，明文走 handoff
  account: string;
  region: string;
  issuer_url: string;
  // 额度 / 时间
  credits_used: Money;
  pulled_at: ISOTime;
  warranty_until: ISOTime | null;
  dead_at: ISOTime | null;
  lifespan_seconds: number;
  paid: Money;
  // 去向
  owner_bus_id: string | null;
  pushed_at: ISOTime | null; // 推 passengerpool 时间，null=未推
  push_failed: boolean;
}

// ── 拉号轮次 · UI: 成功 / 部分 / 失败 / 已退款
export type PullResult = "success" | "partial" | "failed" | "refunded";
export type PushState = "pushed" | "partial" | "failed" | "none";

export interface PullRound {
  id: string;
  vendor_id: string;
  bus_id: string | null;
  bus_name: string | null;
  result: PullResult;
  count_requested: number;
  count_purchased: number;
  alive_count: number;
  dead_count: number;
  push_state: PushState;
  push_ratio: string | null; // "2/3"
  total_cost: Money; // 负=支出 正=退款
  fail_reason: string | null; // "缺货" / "vendor 500"
  created_at: ISOTime;
}

// ── 区域 · 上游 vendor 的 us/eu 区（有的 vendor 无区域）
export type Zone = "us" | "eu";

/** 系统派号推荐结果（auto 模式）· decisions §8.20
 *  散客默认走这个 · 必须展示推荐到谁 + 最终价 + 库存质保成活率 · 不留空占位 */
export interface AutoPickResult {
  /** 推荐到的 vendor · 显示名已按身份处理（真名 or Vendor 0N） */
  vendor_label: string;
  /** 内部 id · 仅用于取色，不展示 */
  vendor_id: string;
  zone: Zone | null;
  available: number;
  /** 最终单价（已含所有附加费）· 不下发原价 */
  unit_price: Money;
  warranty_minutes: number;
  max_per_order: number;
  min_per_order: number;
  avg_lifespan_seconds: number;
  alive_rate_30d: number;
  /** 为什么推荐它 · 一句人话（"库存足 · 成活率最高"） */
  reason: string;
}

// ── 上游即时快照（PullExtractModal 展示 · docs/14 §4.3）
export interface VendorStock {
  vendor_id: string;
  currency: "credits" | "cny_usd";         // 大多数是 credits · drop-kiro-ss 是混币
  warranty_minutes: number;                 // 0 = 无质保
  max_per_order: number;
  min_per_order: number;
  hold_cap_remaining: number | null;        // 91kiro 才有 · 其他家 null
  zones: {
    zone: Zone;
    label: string;                          // "美国区" / "欧洲区"
    enabled: boolean;
    available: number;                      // 缺货 = 0
    unit_price: Money;                      // microunit · 该区最便宜一档
  }[];
}

// ── 我方历史统计（近 30 天 · UpstreamStatusPanel 展示）
export interface VendorHistory {
  vendor_id: string;
  avg_lifespan_seconds: number;             // 平均活多久
  alive_rate_30d: number;                   // 0-100 · 30 天成活率
  total_pulled_30d: number;                 // 30 天累计拉过多少号
}

/** vendor 的一轮车 · decisions §8.22 · docs/15-prices-page-design.md
 *  上游一天发多轮车，每轮单价按整车产出量查阶梯表 —— 所以价格是**轮次级**数据
 *  存轮次不存每日聚合：聚合值（min/max/avg/轮数）前端派生，信息不丢 */
export interface VendorRound {
  /** 发车时刻 · ISO */
  time: ISOTime;
  zone: Zone | null;         // null = 该 vendor 不分区
  /** 这轮的单价 · 已含附加费 */
  unit_price: Money;
  /** 这轮产出多少个号（产量越大单价越低 —— 阶梯表） */
  keys_count: number;
}

/** vendor 某天的全部轮次 · rounds 空数组 = 那天没发车（缺货） */
export interface VendorDayRounds {
  date: string;              // YYYY-MM-DD
  rounds: VendorRound[];
}

export interface VendorPriceTrend {
  vendor_id: string;
  vendor_label: string;      // 按身份显示 · 真名 or AWS-Q Kiro Vendor 0N
  zone: Zone | null;
  /** 按日期升序 · 每天带该天全部轮次（空 = 那天没发车） */
  days: VendorDayRounds[];

  /* ── 以下都是从 days 派生的汇总 · 后端算好下发省前端遍历 ── */

  /** 最新一轮的单价 */
  current_price: Money;
  /** 区间内**轮次单价**的最高 / 最低（用户要知道实际能买到的最好 / 最差价） */
  price_high: Money;
  price_low: Money;
  /** 区间均价（所有轮次的简单均值） */
  price_avg: Money;
  /** 区间总轮数 · 日均轮数（发车密度 —— 判断这家活跃不活跃） */
  total_rounds: number;
  avg_rounds_per_day: number;
  /** 区间涨跌 · 百分比 · 末轮 vs 首轮 */
  change_30d_pct: number;
  /** 没发车的天数 */
  no_service_days: number;
  /** 最长连续发车天数 */
  longest_streak_days: number;
  /** 当前是否有车（最后一天发过） */
  in_stock_now: boolean;
}

// ── 提取事件 · 每次拉号操作一条 · docs/14 §6.5
export interface ExtractEvent {
  id: string;
  created_at: ISOTime;
  vendor_id: string;
  zone: Zone | null;                        // null = kiroapp-cc 无区域
  count_requested: number;
  count_purchased: number;
  total_cost: Money;                        // 负 = 支出 · 0 = 缺货未扣
  result: PullResult;                       // success / partial / failed / refunded
  fail_reason: string | null;
  /** 派发进度 · UI 派发历史 tab 展开明细用 */
  assigned_count: number;                   // 已派几个
  pending_count: number;                    // 待派几个
}

// ── 派发事件 · 每次派动作一条 · docs/14 §6.5
export interface AssignEvent {
  id: string;
  created_at: ISOTime;
  destination: "into_bus" | "push_pool" | "handoff";
  bus_id: string | null;                    // into_bus 时的车 id
  bus_name: string | null;
  count: number;
  /** credential id 数组 · 只是引用不含明文 · handoff 后 credential 已删也留元数据 */
  credential_ids: string[];
  /** UI 展示的 masked 数组（handoff 前的 masked · 拿走后仍能显示） */
  credential_maskeds: string[];
  vendors: string[];                        // 派的号涉及哪些 vendor
}

// ── 提取记录去向
export type Destination = "pending" | "into_bus" | "push_pool" | "handoff";

export interface ExtractRecord {
  id: string;
  vendor_id: string;
  count: number;
  destination: Destination;
  destination_label: string; // "周末拼车局" / "我的号池" / "已 handoff"
  alive_count: number;
  dead_count: number;
  credits_used: Money;
  lifespan_seconds: number;
  total_cost: Money;
  created_at: ISOTime;
}

// ── 活动流（混流）
export type ActivityKind =
  | "into_bus"
  | "extract"
  | "refill"
  | "dead"
  | "topup"
  | "redeem"
  | "push";

/** 去向枚举（跟 Destination 语义对齐，另加 refill/dead 场景的非去向 target） */
export type ActivityTarget =
  | "into_bus"      // 进车
  | "push_pool"     // 推池
  | "handoff"       // 拿走
  | "pending"       // 待派
  | "bus_refill"    // 补车动作的车名 target
  | "cred_dead"     // 号失效的号 target
  | "topup_source"; // 充值/兑换 target（waffo / 兑换码等）

export interface Activity {
  id: string;
  kind: ActivityKind;
  /** 结构化字段（首选）；summary 作为兜底 */
  source?: string;              // 号来源 / vendor 名 / 事件主体（"Kiro Drop" · "cred_...4F2" · "waffo · 支付宝"）
  target?: string;              // 去向 / 目标（"我的号池" · "Kiro 常驻车" · "存活 42h"）
  target_kind?: ActivityTarget;
  count?: number;               // 量（个号/个 key/次数）
  count_unit?: string;          // 量词（"个号" · "个 key" · "元"）
  summary: string;              // 兜底叙述，也用于结构化字段不足时
  amount: Money | null;
  created_at: ISOTime;
  link: string | null;
}

// ── Vendor 监测
export interface VendorStat {
  vendor_id: string;
  rank: number;
  unit_price: Money;
  avg_lifespan_seconds: number;
  effective_cost: number; // 单价 ÷ 平均寿命（仅内部保留，不上 UI）
  /** 平均每号消耗多少积分才挂（0~10k 区间，越大越耐用） */
  avg_credits_per_cred: Money;
  /** 保修次数：这家 vendor 出的号在 30 分钟内挂被退款过几次（越少越好） */
  warranty_count: number;
  alive_rate: number; // 0-100
  pulls_today: number;
  fallback_count: number; // 拉这家失败、我方 fallback 到别家的次数
  out_of_stock: boolean;
}

export interface VendorShare {
  vendor_id: string;
  pulls: number;
  ratio: number; // 0-1
}

export interface StockSummary {
  total_available: number;
  by_vendor: { vendor_id: string; available: number }[];
}

// ── 概览
export type TimeRange = "today" | "7d" | "30d" | "90d" | "all";

export interface OverviewKpi {
  balance: Money;
  balance_delta_topup: Money;
  balance_delta_spend: Money;
  spend_today: Money;
  spend_yesterday: Money;
  pull_total: number;
  pull_this_month: number;
  alive_count: number;
  dead_count: number;
  pending_refill: number;
  avg_lifespan_seconds: number;
}

export interface OverviewBuses {
  bus_count: number;
  total_credentials: number;
  refill_count: number;
  coalesce_rate: number;
  items: {
    id: string;
    name: string;
    /** 当前乘客在这辆车里的角色：owner=我发起的 · member=我参与的 */
    role: "owner" | "member";
    alive: number;
    dead: number;
    spend: Money;
  }[];
}

export interface OverviewExtract {
  count_today: number;
  total_credentials: number;
  pending: number;
  spend: Money;
  by_destination: { destination: Destination; count: number }[];
}

export interface Overview {
  kpi: OverviewKpi;
  buses: OverviewBuses;
  extract: OverviewExtract;
}

export interface TrendPoint {
  date: string; // YYYY-MM-DD
  value: number;
}

export type TrendMetric = "credits" | "pulls" | "lifespan";

// ── 配置
export interface DownstreamConfig {
  passengerpool_url: string;
  passengerpool_token_masked: string;
  connected: boolean;
  last_heartbeat_at: ISOTime | null;
  push_success_rate: number;
  push_total: number;
  push_failed: number;
  rules: {
    push_on_pull: boolean;
    resync_on_dead: boolean;
    retry_on_failure: boolean;
    bus_only: boolean;
  };
}

export interface WebhookConfig {
  url: string;
  secret_masked: string;
  enabled: boolean;
  events: string[];
}

export interface WebhookDelivery {
  id: string;
  event: string;
  ok: boolean;
  status_code: number | null;
  attempt: number;
  latency_ms: number;
  created_at: ISOTime;
}

export interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  last_used_at: ISOTime | null;
  created_at: ISOTime;
  revoked: boolean;
}
