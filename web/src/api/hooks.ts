import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, del, post, postIdempotent, put } from "./client";
import type {
  Activity, ApiKey, ApiKeyCreated, AssignEvent, AutoPickResult, Bus, BusMemberStats,
  Credential, DownstreamConfig,
  ExtractEvent, GlobalStrategy, ISOTime, LedgerEntry, Money, Overview, Paged,
  Passenger, PullRound,
  RedeemResult, StockSummary, TimeRange, TopupOrder, TrendMetric, TrendPoint,
  VendorHistory, VendorPriceTrend, VendorShare, VendorStat, VendorStock, Wallet, WebhookConfig,
  WebhookDelivery,
} from "@/types";

/* ── 账号 / 钱包 / 库存 ── */

export const useMe = () =>
  useQuery({ queryKey: ["me"], queryFn: () => api<Passenger>("/me") });

export const useWallet = () =>
  useQuery({ queryKey: ["wallet"], queryFn: () => api<Wallet>("/me/wallet") });

export const useStock = () =>
  useQuery({
    queryKey: ["stock"],
    queryFn: () => api<StockSummary>("/vendors/stock"),
    refetchInterval: 60_000,
  });

export const useLedger = () =>
  useQuery({ queryKey: ["ledger"], queryFn: () => api<Paged<LedgerEntry>>("/me/ledger") });

/** 顶部跑马灯活动位 · **公开端点**（未登录也能拉）
 *  文案 / 跳转 / 倒计时都在后端 config.promo.items · 运营改了不用重新部署前端 */
export interface PromoItem {
  id: string;
  /** 兜底文案 · text_i18n 里没匹配到当前语言时用这个 */
  text: string;
  /** BCP-47 → 文案 · 前端按 i18n.language 选一条 */
  text_i18n?: Record<string, string>;
  /** 空 / 缺省 = 不可点（纯公告） */
  to?: string;
  /** 空 / 缺省 = 不显示倒计时 · 非空是 RFC3339 */
  countdown_until?: string;
}

export const usePromos = () =>
  useQuery({
    queryKey: ["promos"],
    queryFn: () => api<{ items: PromoItem[]; server_now: string }>("/promos"),
    // 活动位不需要实时 · 5 分钟够了（运营改完最多 5 分钟生效）
    staleTime: 5 * 60_000,
    // 未登录也要显示 · 401 不重试（这个端点本来就不需要登录·真 401 说明后端配错了）
    retry: false,
  });

/** 社群渠道入口（GET /api/community/channels）· 公开端点
 *  空数组 = 前端展示"敬请期待"占位 · 未上线的渠道 backend 不下发 */
export interface CommunityChannel {
  /** telegram_channel / telegram_group / discord / github / x · 前端按它挑 logo */
  id: string;
  /** 兜底展示名 · name_i18n 里没匹配到当前语言时用这个 */
  name: string;
  name_i18n?: Record<string, string>;
  url: string;
}

/** Vendor 上游状态（GET /api/vendors/status）· **公开端点**
 *  展示层严格脱敏 · 匿名 label（"AWS-Q Kiro Vendor 01"）· 无真名 · 无价格
 *  真名只有登录后 wholesale 档才能看到（走别的端点）· 见 decisions §10.4 */
/** Vendor 自报的 fleet 累计数据（走 vendor 的 /api/status 端点）
 *  支持的 vendor（多家 vendor）才有值 · 其他 vendor 这个字段整个 undefined
 *  这是 vendor 侧的**真实历史累计**（不是我方探针积累的）· 上线首日就有数据 */
export interface VendorPublicStatus {
  keys_active?: number;
  keys_alive?: number;
  keys_dead?: number;
  keys_stock?: number;
  keys_suspect?: number;
  keys_total?: number;
  generating?: boolean;
  uptime_seconds?: number;
  /** vendor 平台启动时间 · RFC3339 UTC */
  started_at?: string;
}

/** Backfill 汇总（vendor 侧真历史 · 上线一秒到手）
 *  vendor 支持 order + key history 端点才有 · 本 vendor 这种没端点的字段 undefined */
export interface VendorHistoryOut {
  total_orders: number;
  total_keys: number;
  active_keys: number;
  dead_keys: number;
  /** 平均寿命秒数 · 用于前端算 lifespan_bucket 或直接展示 */
  avg_lifespan_sec?: number;
  /** vendor 侧第一单时间 · RFC3339 UTC */
  first_order_at?: string;
  /** vendor 侧最新一单时间 · RFC3339 UTC */
  last_order_at?: string;
}

export interface VendorStatusRow {
  anon_id: string;
  anon_label: string;
  alive: boolean;
  error_kind?: string;
  /** many / low / out / unknown */
  stock_bucket: "many" | "low" | "out" | "unknown";
  region_count: number;
  has_warranty: boolean;
  warranty_minutes?: number;
  max_per_order?: number;
  /** 探测样本 < 10 时省略 · 前端展示 "-" */
  uptime_24h_pct?: number;
  stockout_24h_minutes?: number;
  /** long / mid / short / unknown */
  lifespan_bucket?: "long" | "mid" | "short" | "unknown";
  incidents_7d?: string[];
  public_status?: VendorPublicStatus;
  history?: VendorHistoryOut;
  /** Vendor 平台开号节奏 · 6 家都能有（有 FleetLister 就走真数据 · 否则从探针增量推） */
  dispatch?: VendorDispatchOut;
  /** 质量综合评估 · 后端算好 · 前端排序按 Score 降序（Score 不下发）· 显示 Tags */
  quality: VendorQuality;
}

/** Vendor 质量综合 · 后端 computeQuality 算完下发 · Score 内部用不给前端
 *  Tags 多维叠加 · 前端按 kind 映射颜色 */
export interface VendorQuality {
  tags: VendorQualityTag[];
}

export interface VendorQualityTag {
  /** 前端按 kind 映射色调 · 文案走 i18n status:tags-quality.<kind>
   *   stable / high-volume / active / in-stock / warranty / watching */
  kind: "stable" | "high-volume" | "active" | "in-stock" | "out-of-stock" | "warranty" | "watching";
}

/** Vendor 平台 fleet-wide 发货节奏 · 上线一秒到手（后端从 vendor 侧真历史或探针增量推） */
export interface VendorDispatchOut {
  total_batches: number;
  total_keys_dispatched: number;
  avg_interval_min?: number;
  last_dispatch_at?: string;
}

export interface VendorStatusOverview {
  probed_at?: string;
  vendors: VendorStatusRow[];
}

/** Vendor 状态趋势（GET /api/vendors/status/{anon_id}/trend）· **公开端点**
 *  两种数据源：
 *    - source=backfill · 桶 1h · 真历史 · 每桶 keys_born / keys_died / avg_lifespan_sec
 *    - source=probe    · 桶 15min · 我方探针 · 每桶 uptime_pct / stock_bucket / samples
 *  前端按 source 决定画哪种曲线 */
export interface VendorStatusTrendPoint {
  /** 桶起点 · RFC3339 UTC */
  t: string;
  /** probe 源字段 */
  uptime_pct?: number;
  stock_bucket?: "many" | "low" | "out" | "unknown";
  samples?: number;
  /** backfill 源字段 */
  keys_born?: number;
  keys_died?: number;
  avg_lifespan_sec?: number;
}

export interface VendorStatusTrend {
  anon_id: string;
  anon_label: string;
  window: string; // "24h"
  source: "backfill" | "probe" | "empty";
  points: VendorStatusTrendPoint[];
}

/** Status 页时间窗口 · "24h" / "168h" / "720h"
 *  影响后端 Quality 里 Volume/Freshness 的评估窗口 + 排序结果 */
export type StatusWindow = "24h" | "168h" | "720h";

export const useVendorStatus = (window: StatusWindow = "168h") =>
  useQuery({
    queryKey: ["vendor-status", window],
    queryFn: () => api<VendorStatusOverview>(`/vendors/status?window=${window}`),
    staleTime: 30_000,      // 探针 60s 采样 · 30s 缓存合理
    refetchInterval: 60_000, // 挂着看的话每分钟刷一次
    retry: false,           // 公开端点 · 401 不重试
  });

/** 统一的开号事件流（GET /api/vendors/status/{anon_id}/events）· **公开端点**
 *
 *  跟老的 /trend 的区别：**6 家同一形状**。有 fleet 端点的 vendor 用它自报的批次·
 *  没有的从我方探针增量推出同形状事件（derived=true）。前端只画一种图。 */
export interface VendorDispatchEvent {
  /** 发出时刻 · RFC3339 */
  at: string;
  /** 这批发了几个号 */
  count: number;
  /** 人话区域名："美区" / "欧区" / "" = 不分区（内部 region id 不下发） */
  region?: string;
  alive?: number;
  dead?: number;
  /** 收敛三态：running 在架 · done 已发完 · dead 全挂 */
  status?: "running" | "done" | "dead";
  dead_at?: string;
  /** true = 我方探针推算（精度较低）· false/缺省 = vendor 自报 */
  derived?: boolean;
}

export interface VendorDispatchEventsSummary {
  batches: number;
  keys: number;
  avg_interval_min?: number;
  alive_now?: number;
  dead_total?: number;
}

export interface VendorDispatchEvents {
  anon_id: string;
  anon_label: string;
  window: string;
  /** vendor = 上游自报 · observed = 我方探针推算 · empty = 无数据 */
  source: "vendor" | "observed" | "empty";
  /** 时间倒序（最新在前） */
  events: VendorDispatchEvent[];
  summary: VendorDispatchEventsSummary;
}

export const useVendorDispatchEvents = (
  anonID: string | undefined,
  window: string = "168h",
) =>
  useQuery({
    queryKey: ["vendor-dispatch-events", anonID, window],
    queryFn: () =>
      api<VendorDispatchEvents>(`/vendors/status/${anonID}/events`, { params: { window } }),
    staleTime: 60_000,
    refetchInterval: 5 * 60_000,
    retry: false,
    enabled: !!anonID,
  });

export const useVendorStatusTrend = (anonID: string | undefined, window: string = "24h") =>
  useQuery({
    queryKey: ["vendor-status-trend", anonID, window],
    queryFn: () => api<VendorStatusTrend>(`/vendors/status/${anonID}/trend`, { params: { window } }),
    staleTime: 60_000,
    refetchInterval: 5 * 60_000,
    retry: false,
    enabled: !!anonID,
  });

export const useCommunityChannels = () =>
  useQuery({
    queryKey: ["community-channels"],
    queryFn: () => api<{ channels: CommunityChannel[] }>("/community/channels"),
    // 社群链接极少变 · 缓存 10 分钟
    staleTime: 10 * 60_000,
    retry: false,
  });

/** 个人邀请码 + 邀请记录（decisions §8.29）· 拉人注册平台用
 *  拉人进车用的是拼车码（bus.invite_code）· 两者不同（§8.38） */
export interface MyInvite {
  code: string;
  invited_count: number;
  /** 还剩几次手续费减免 */
  waiver_remaining: number;
  waiver_used: number;
  /** 邀请记录（最新在前）· 被邀请人只给脱敏标识（不给第三方完整邮箱） */
  referrals: InviteReferral[];
}

export interface InviteReferral {
  /** 脱敏后的被邀请人（如 zha***@gmail.com） */
  invitee: string;
  /** 这条带来几次减免额度 */
  waiver_granted: number;
  joined_at: ISOTime;
}

export const useMyInvite = () =>
  useQuery({ queryKey: ["myInvite"], queryFn: () => api<MyInvite>("/me/invite") });

/** 补绑专属邀请码 · 拿社群身份（看 vendor 真名 + 社群价）
 *  一个账号只能绑一次 · 绑完要刷 me（invited 变了·影响全站 vendor 显示名和价格） */
export const useBindSystemCode = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (code: string) =>
      post<Passenger>("/me/community-code", { code: code.trim().toUpperCase() }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["me"] });
      // vendor 显示名 / 单价都跟 invited 挂钩 · 一起刷
      qc.invalidateQueries({ queryKey: ["vendorStats"] });
      qc.invalidateQueries({ queryKey: ["stock"] });
      qc.invalidateQueries({ queryKey: ["vendorStock"] });
    },
  });
};

/* ── 概览 ── */

export const useOverview = (range: TimeRange) =>
  useQuery({ queryKey: ["overview", range], queryFn: () => api<Overview>("/me/overview", { params: { range } }) });

/* scope 可选：整体（省略）· 单车（bus_id=xxx）· 单 vendor（vendor=xxx）—— 二选一 */
export const useTrend = (
  range: TimeRange,
  metric: TrendMetric,
  scope?: { busId?: string; vendor?: string },
) =>
  useQuery({
    queryKey: ["trend", range, metric, scope?.busId ?? "", scope?.vendor ?? ""],
    queryFn: () =>
      api<TrendPoint[]>("/me/trend", {
        params: {
          range, metric,
          ...(scope?.busId ? { bus_id: scope.busId } : {}),
          ...(scope?.vendor ? { vendor: scope.vendor } : {}),
        },
      }),
  });

export const useActivities = (range: TimeRange) =>
  useQuery({
    queryKey: ["activities", range],
    queryFn: () => api<Paged<Activity>>("/me/activities", { params: { range } }),
  });

/* ── Vendor ── */

export const useVendorStats = () =>
  useQuery({
    queryKey: ["vendorStats"],
    queryFn: () => api<{ stats: VendorStat[]; share: VendorShare[] }>("/vendors/stats"),
  });

/* ── Bus ── */

export const useBuses = () =>
  useQuery({ queryKey: ["buses"], queryFn: () => api<Paged<Bus>>("/me/buses") });

export const useBus = (id: string | undefined) =>
  useQuery({ queryKey: ["bus", id], queryFn: () => api<Bus>(`/me/buses/${id}`), enabled: !!id });

export const useBusCredentials = (id: string | undefined) =>
  useQuery({
    queryKey: ["busCredentials", id],
    queryFn: () => api<Credential[]>(`/me/buses/${id}/credentials`),
    enabled: !!id,
  });

/** 成员维度统计 · 多人车才有意义（1 人车返 1 条 100% 的行） */
export const useBusMemberStats = (id: string | undefined) =>
  useQuery({
    queryKey: ["busMemberStats", id],
    queryFn: () => api<BusMemberStats>(`/me/buses/${id}/member-stats`),
    enabled: !!id,
  });

export const useBusPulls = (id: string | undefined) =>
  useQuery({
    queryKey: ["busPulls", id],
    queryFn: () => api<PullRound[]>(`/me/buses/${id}/pulls`),
    enabled: !!id,
  });

/** 建车 · single / anon / team 三种 kind 都用它 · anon 传 zone + max_unit_price · team 后端生成邀请码 */
export const useCreateBus = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      name: string;
      kind?: "single" | "anon" | "team";
      strategy?: unknown;
      max_members?: number;
      anon_zone?: string;
      anon_max_unit_price?: number;
    }) => post<Bus>("/me/buses", body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["buses"] }),
  });
};

/** 撮合一辆已存在的 anon 车 · zone + max_unit_price 过滤 · auto_join=true 自动加入 */
export const useMatchAnonBus = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { zone?: string; max_unit_price?: number; auto_join?: boolean }) =>
      post<{ matched: boolean; reason?: string; bus?: Bus }>("/me/buses/anon/match", body),
    onSuccess: (result) => {
      if (result.matched) qc.invalidateQueries({ queryKey: ["buses"] });
    },
  });
};

/** 显式加入一辆 anon 车 · 幂等（已成员返 200 + 现状） */
export const useJoinAnonBus = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (busId: string) => post<Bus>(`/me/buses/${busId}/join`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["buses"] }),
  });
};

/** 用邀请码加入 team 车 · 幂等 · 无效码返 404 */
export const useJoinByInviteCode = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (code: string) =>
      post<Bus>("/me/buses/join-by-invite", { invite_code: code.trim().toUpperCase() }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["buses"] }),
  });
};

export const useUpdateStrategy = (busId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) => put(`/me/buses/${busId}/strategy`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["bus", busId] }),
  });
};

export const useRenameBus = (busId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => put(`/me/buses/${busId}`, { name }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["bus", busId] });
      qc.invalidateQueries({ queryKey: ["buses"] });
    },
  });
};

/* ── 成员管理（多人车）· decisions §8.26 ── */

/** 挂起 / 解挂某成员 · 挂起 = 撤 client_key（取不到号）+ 不参与分摊 · share_pct 不动 */
export const useSetMemberSuspended = (busId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ memberId, suspended }: { memberId: string; suspended: boolean }) =>
      put(`/me/buses/${busId}/members/${memberId}`, { suspended }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["bus", busId] }),
  });
};

/** 移除成员 · 车主有权直接移除（§8.36）· 剩下的人 share_pct 均分重算 */
export const useRemoveMember = (busId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (memberId: string) => del(`/me/buses/${busId}/members/${memberId}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["bus", busId] });
      qc.invalidateQueries({ queryKey: ["buses"] });
    },
  });
};

/** 重新生成邀请码 · 旧码立即失效 */
export const useRegenInviteCode = (busId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => post<{ invite_code: string }>(`/me/buses/${busId}/invite-code`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["bus", busId] }),
  });
};

export const usePullForBus = (busId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { count: number; vendor_id?: string; zone?: string }) =>
      postIdempotent(`/me/buses/${busId}/pull`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["bus", busId] });
      qc.invalidateQueries({ queryKey: ["busCredentials", busId] });
      qc.invalidateQueries({ queryKey: ["busPulls", busId] });
    },
  });
};

export const useDissolveBus = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => del(`/me/buses/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["buses"] }),
  });
};

/* ── 提取 key ──
   端点都在 /me/pull* 名下（跟后端矩阵一致）· "extract" 只是这个页面的中文叫法，
   不是独立资源 —— 单独拉号和拼车拉号是同一个动作，去向不同而已 */

/** 后端估价 · 只暴露对外三项（CLAUDE.md §0.1 · 计费分项不出）
 *  用于 ExtractConfirmModal / PullExtractForm 替换本地 pricing.ts 硬编码 */
export const useEstimate = () =>
  useMutation({
    mutationFn: (body: { vendor_id: string; zone?: string; count: number; coupon_code?: string }) =>
      post<{ unit_price: number; service_fee: number; total: number }>(
        "/me/pull/estimate",
        body,
      ),
  });

export const useExtract = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      vendor_id: string; zone?: string; count: number;
      /** 优惠码 · 本次减免 · 阶段 1a 后端估价还没接优惠码 · 前端先不发
       *  避免 pullRequest decodeStrict 拒未知字段（bad_json） */
      coupon_code?: string;
    }) => {
      const { coupon_code: _unused, vendor_id, ...rest } = body;
      void _unused;
      const payload = {
        ...rest,
        ...(vendor_id && vendor_id !== "auto" ? { vendor_id } : {}),
      };
      return postIdempotent("/me/pull", payload);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["extractRecords"] });
      qc.invalidateQueries({ queryKey: ["extractEvents"] });
      qc.invalidateQueries({ queryKey: ["pullRecords"] });
      qc.invalidateQueries({ queryKey: ["wallet"] });
    },
  });
};

/** 提取事件 · 每次拉号一条 · Extract 页"提取历史"tab */
export const useExtractEvents = () =>
  useQuery({
    queryKey: ["extractEvents"],
    queryFn: () => api<Paged<ExtractEvent>>("/me/pull/events"),
  });

/** 派发事件 · 每次派动作一条 · Extract 页"派发历史"tab */
export const useAssignEvents = () =>
  useQuery({
    queryKey: ["assignEvents"],
    queryFn: () => api<Paged<AssignEvent>>("/me/assign/events"),
  });

/** 上游 vendor 即时快照 · PullExtractModal 上游状态面板
 *  只在 vendorId 是具体 vendor（不是 "auto"）时才发请求
 *  单价是**最终价**（已含所有分项）· couponCode 传了则本次减免 · decisions §8.20 */
export const useVendorStock = (vendorId: string | undefined, couponCode?: string) =>
  useQuery({
    queryKey: ["vendorStock", vendorId, couponCode || null],
    queryFn: () =>
      api<VendorStock>(
        `/vendors/${vendorId}/stock${couponCode ? `?coupon_code=${encodeURIComponent(couponCode)}` : ""}`,
      ),
    enabled: !!vendorId && vendorId !== "auto",
  });

/** 系统派号推荐（auto 模式）· 散客默认走这个 · decisions §8.20
 *  返回推荐到的 vendor（真名 or Vendor 0N）+ 最终价 + 库存质保成活率 + 推荐理由 */
export const useAutoPick = (zone: string, couponCode?: string) =>
  useQuery({
    queryKey: ["autoPick", zone, couponCode || null],
    queryFn: () => {
      const p = new URLSearchParams({ zone });
      if (couponCode) p.set("coupon_code", couponCode);
      return api<AutoPickResult>(`/vendors/auto-pick?${p}`);
    },
  });

/** vendor 价格走势 · Prices 页多线图 · decisions §8.22 */
export const useVendorPrices = (days: number, zone: string = "auto") =>
  useQuery({
    queryKey: ["vendorPrices", days, zone],
    queryFn: () =>
      api<{ trends: VendorPriceTrend[] }>(`/vendors/prices?days=${days}&zone=${zone}`),
  });

/** 我方历史统计 · 近 30 天 · PullExtractModal 上游状态面板 */
export const useVendorHistory = (vendorId: string | undefined) =>
  useQuery({
    queryKey: ["vendorHistory", vendorId],
    queryFn: () => api<VendorHistory>(`/vendors/${vendorId}/history`),
    enabled: !!vendorId && vendorId !== "auto",
  });

/* ── 拉号记录（record group · 未派去向号） ── */

export const usePullRecords = () =>
  useQuery({
    queryKey: ["pullRecords"],
    queryFn: () => api<Paged<Credential>>("/me/pull-records"),
  });

/* 派去向 · 两种走 assign：进车（into_bus + bus_id）· 推池（push_pool）
   拿走（handoff）**不走这里** —— 它是三段式，见下面 useHandoff（09-transactions §4） */
export type AssignBody = {
  credential_ids: string[];
  destination: "into_bus" | "push_pool";
  bus_id?: string; // into_bus 才带
};

/** 派进多人车时后端返的份额清算结果（decisions §8.23）
 *  只给结果 · 不列各人 share_pct / 余额（那些在成员 tab 看） */
export interface AssignSettlement {
  /** 车友分摊后你实际收到多少（microunit） */
  income: Money;
  /** 因为有人本次跳过·你少收多少 · 0 = 所有人都参与了 */
  lost: Money;
  /** 本次跳过的车友（余额不足 / 已挂起） */
  skipped_usernames: string[];
}

export interface AssignResult {
  assigned: number;
  errors: { credential_id: string; code: string; message: string }[];
  /** 单人车 / 无清算时后端省略这个字段 */
  settlement?: AssignSettlement;
}

export const useAssign = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AssignBody) =>
      postIdempotent<AssignResult>("/me/pull-records/assign", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["pullRecords"] });
      qc.invalidateQueries({ queryKey: ["assignEvents"] });
      qc.invalidateQueries({ queryKey: ["buses"] });
      // 清算动了钱包（share_income / share_expense）· 余额和流水都要刷
      qc.invalidateQueries({ queryKey: ["wallet"] });
      qc.invalidateQueries({ queryKey: ["ledger"] });
    },
  });
};

/* ── 拿走 handoff · 三段式（09-transactions §4 · P0-3）──
   为什么不能一次性：号交出去不可逆。一次性做法是「删号 + 同一个响应返明文」，
   响应在网络上断线 → 号已删、明文没收到 → **明文永久丢失，钱白花**。
   三段式把「取明文」和「删号」分开，取明文那步 TTL 内可以反复重试。

   UI 上仍然只有两步（点「下载拿走」→ 看到明文 → 点「我已保存」），
   init + fulfill 合在打开弹窗那一下做完，用户感知不到三段。 */

export interface HandoffToken {
  download_token: string;
  expires_at: ISOTime;
}

export interface HandoffKeys {
  keys: { credential_id: string; key: string; vendor_id: string; account: string }[];
}

/** ① 发 token · 号还在池里（disabled），这步**不返回明文** */
export const useHandoffInit = () =>
  useMutation({
    mutationFn: (credential_ids: string[]) =>
      postIdempotent<HandoffToken>("/me/handoff", { credential_ids }),
  });

/** ② 用 token 取明文 · TTL 内可反复取（断线重试就靠这个） */
export const useHandoffFulfill = () =>
  useMutation({
    mutationFn: (token: string) => api<HandoffKeys>(`/me/handoff/${token}`),
  });

/** ③ 确认收到 → 这时才真删号 · 幂等 */
export const useHandoffConfirm = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (token: string) => postIdempotent(`/me/handoff/${token}/confirm`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["pullRecords"] });
      qc.invalidateQueries({ queryKey: ["assignEvents"] });
    },
  });
};

/* ── 单独拉号（次入口） · 跟 bus 无关 · 拉完进 record group ── */

export const usePull = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { vendor_id?: string; count: number; zone?: string }) => {
      const payload = {
        ...body,
        ...(body.vendor_id && body.vendor_id !== "auto" ? {} : { vendor_id: undefined }),
      };
      if (payload.vendor_id === undefined) delete payload.vendor_id;
      return postIdempotent("/me/pull", payload);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["pullRecords"] });
      qc.invalidateQueries({ queryKey: ["wallet"] });
    },
  });
};

/* ── 钱包 · 充值 / 兑换 ── */

/** 生成充值单 → 返回二维码 / 跳转链接 · 付到支付通道
 *  参数是**要净到账的积分**（CLAUDE.md §1.4）· 通道费 5% 加在本金上
 *  1 积分 ≡ 1 元 → paid = credits × 1.05 元 → 通道侧显示 USD = paid / 7 */
export const useCreateTopup = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (credits: Money) =>
      postIdempotent<TopupOrder>("/me/topup", { credits, channel: "waffo" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["wallet"] });
      qc.invalidateQueries({ queryKey: ["ledger"] });
    },
  });
};

export const useRedeem = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (code: string) => post<RedeemResult>("/me/redeem", { code }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["wallet"] });
      qc.invalidateQueries({ queryKey: ["ledger"] });
    },
  });
};

/* ── 全局策略 ── */

export const useGlobalStrategy = () =>
  useQuery({ queryKey: ["globalStrategy"], queryFn: () => api<GlobalStrategy>("/me/strategy") });

export const useSaveGlobalStrategy = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Partial<Omit<GlobalStrategy, "used_today">>) =>
      put("/me/strategy", body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["globalStrategy"] }),
  });
};

/* ── 配置 ── */

export const useDownstream = () =>
  useQuery({ queryKey: ["downstream"], queryFn: () => api<DownstreamConfig>("/me/downstream") });

export const useWebhook = () =>
  useQuery({ queryKey: ["webhook"], queryFn: () => api<WebhookConfig>("/me/downstream/webhook") });

export const useWebhookDeliveries = () =>
  useQuery({
    queryKey: ["webhookDeliveries"],
    queryFn: () => api<WebhookDelivery[]>("/me/downstream/webhook/deliveries"),
  });

/** 存我的号池配置 · url / token / 4 条推送规则 */
export const useSaveDownstream = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Partial<DownstreamConfig> & { token?: string }) =>
      put("/me/downstream/passengerpool", body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["downstream"] }),
  });
};

/** 测连通 · 不写库，只回 latency */
export const useTestDownstream = () =>
  useMutation({
    mutationFn: (body?: { url?: string; token?: string }) =>
      post<{ ok: boolean; latency_ms: number }>("/me/downstream/passengerpool/test", body ?? {}),
  });

export const useSaveWebhook = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Partial<WebhookConfig> & { secret?: string }) =>
      put("/me/downstream/webhook", body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhook"] }),
  });
};

export const useTestWebhook = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      post<{ ok: boolean; status_code: number; latency_ms: number }>(
        "/me/downstream/webhook/test",
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhookDeliveries"] }),
  });
};

/** 重新生成 webhook secret · 旧的立即失效 */
export const useRegenWebhookSecret = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => post<{ secret: string }>("/me/downstream/webhook/secret"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhook"] }),
  });
};

export const useApiKeys = () =>
  useQuery({ queryKey: ["apiKeys"], queryFn: () => api<ApiKey[]>("/me/api-keys") });

/** 建 api key · plaintext 只在响应里出现这一次 */
export const useCreateApiKey = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => post<ApiKeyCreated>("/me/api-keys", { name }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["apiKeys"] }),
  });
};

export const useRevokeApiKey = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => del(`/me/api-keys/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["apiKeys"] }),
  });
};

/* ── 账号 ── */

/** 改密码 · 阶段 1a 前端表单先做，后端 1b 才支持 */
export const useChangePassword = () =>
  useMutation({
    mutationFn: (body: { old_password: string; new_password: string }) =>
      post("/me/password", body),
  });

/* 登录 / 注册 / 退出后必须把 query 缓存清掉：
   身份换了，缓存里全是上一个身份的数据。不清的话会拿着旧 ["me"] 继续渲染
   （最直接的症状：带邀请码注册完，vendor 还显示 Vendor 0N 而不是真名）

   用 removeQueries() 而**不是** clear()：clear() 连 mutation 缓存一起清，
   于是当前这个 mutation 自己被清掉、mutateAsync 的 promise 不 resolve，
   调用侧 `await mutateAsync(); nav("/")` 的跳转就永远不执行 */
export const useLogin = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { account: string; password: string; remember: boolean }) =>
      post("/login", body),
    onSuccess: () => qc.removeQueries(),
  });
};

export const useRegister = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      email: string; username: string; password: string; invite_code?: string;
    }) => post("/register", body),
    onSuccess: () => qc.removeQueries(),
  });
};

export const useLogout = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => post("/logout"),
    // 同上 · 不能用 clear()，否则 Profile 里 await 完的 nav("/login") 不执行
    onSuccess: () => qc.removeQueries(),
  });
};
