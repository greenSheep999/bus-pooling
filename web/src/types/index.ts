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

export interface Activity {
  id: string;
  kind: ActivityKind;
  summary: string;
  amount: Money | null;
  created_at: ISOTime;
  link: string | null; // 点击落地路由
}

// ── Vendor 监测
export interface VendorStat {
  vendor_id: string;
  rank: number;
  unit_price: Money;
  avg_lifespan_seconds: number;
  effective_cost: number; // 单价 ÷ 平均寿命
  alive_rate: number; // 0-100
  pulls_today: number;
  fallback_count: number;
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
  items: { id: string; name: string; alive: number; dead: number; spend: Money }[];
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
