import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, del, post, put } from "./client";
import type {
  Activity, ApiKey, ApiKeyCreated, AssignEvent, AutoPickResult, Bus, Credential, DownstreamConfig,
  ExtractEvent, ExtractRecord, GlobalStrategy, LedgerEntry, Money, Overview, Paged, Passenger,
  PullRound,
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

export const useBusPulls = (id: string | undefined) =>
  useQuery({
    queryKey: ["busPulls", id],
    queryFn: () => api<PullRound[]>(`/me/buses/${id}/pulls`),
    enabled: !!id,
  });

export const useCreateBus = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; strategy?: unknown }) => post<Bus>("/me/buses", body),
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

/** 移除成员 · 剩下的人 share_pct 要重算 · 后端要走全员确认（§8.18） */
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
    mutationFn: (body: { count: number; vendor_id?: string }) =>
      post(`/me/buses/${busId}/pull`, body),
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

/* ── 提取 key ── */

export const useExtractRecords = () =>
  useQuery({
    queryKey: ["extractRecords"],
    queryFn: () => api<Paged<ExtractRecord>>("/me/extract/records"),
  });

export const useEstimate = () =>
  useMutation({
    mutationFn: (body: { vendor_id: string; count: number }) =>
      post<{ key_cost: number; single_pull_fee: number; service_fee: number; total: number }>(
        "/me/extract/estimate",
        body,
      ),
  });

export const useExtract = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      vendor_id: string; zone?: string; count: number;
      /** 优惠码 · 本次减免 · decisions §8.20（注册时叫邀请码 · 这里叫优惠码） */
      coupon_code?: string;
    }) => post("/me/extract", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["extractRecords"] });
      qc.invalidateQueries({ queryKey: ["extractEvents"] });
      qc.invalidateQueries({ queryKey: ["wallet"] });
    },
  });
};

/** 提取事件 · 每次拉号一条 · Extract 页"提取历史"tab */
export const useExtractEvents = () =>
  useQuery({
    queryKey: ["extractEvents"],
    queryFn: () => api<Paged<ExtractEvent>>("/me/extract/events"),
  });

/** 派发事件 · 每次派动作一条 · Extract 页"派发历史"tab */
export const useAssignEvents = () =>
  useQuery({
    queryKey: ["assignEvents"],
    queryFn: () => api<Paged<AssignEvent>>("/me/assign/events"),
  });

/** 上游 vendor 即时快照 · PullExtractModal 上游状态面板
 *  只在 vendorId 是具体 vendor（不是 "auto"）时才发请求
 *  单价是**最终价**（含附加费）· couponCode 传了则本次减免 · decisions §8.20 */
export const useVendorStock = (vendorId: string | undefined, couponCode?: string) =>
  useQuery({
    queryKey: ["vendorStock", vendorId, couponCode || null],
    queryFn: () =>
      api<VendorStock>(
        `/me/vendors/${vendorId}/stock${couponCode ? `?coupon_code=${encodeURIComponent(couponCode)}` : ""}`,
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
      return api<AutoPickResult>(`/me/vendors/auto-pick?${p}`);
    },
  });

/** vendor 价格走势 · Prices 页多线图 · decisions §8.22 */
export const useVendorPrices = (days: number, zone: string = "auto") =>
  useQuery({
    queryKey: ["vendorPrices", days, zone],
    queryFn: () =>
      api<{ trends: VendorPriceTrend[] }>(`/me/vendors/prices?days=${days}&zone=${zone}`),
  });

/** 我方历史统计 · 近 30 天 · PullExtractModal 上游状态面板 */
export const useVendorHistory = (vendorId: string | undefined) =>
  useQuery({
    queryKey: ["vendorHistory", vendorId],
    queryFn: () => api<VendorHistory>(`/me/vendors/${vendorId}/history`),
    enabled: !!vendorId && vendorId !== "auto",
  });

/* ── 拉号记录（record group · 未派去向号） ── */

export const usePullRecords = () =>
  useQuery({
    queryKey: ["pullRecords"],
    queryFn: () => api<Paged<Credential>>("/me/pull-records"),
  });

/* 派去向 · 三种：进车（into_bus + bus_id）· 推池（push_pool）· 拿走（handoff） */
export type AssignBody = {
  credential_ids: string[];
  destination: "into_bus" | "push_pool" | "handoff";
  bus_id?: string; // into_bus 才带
};

export const useAssign = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AssignBody) => post("/me/pull-records/assign", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["pullRecords"] });
      qc.invalidateQueries({ queryKey: ["buses"] });
    },
  });
};

/* ── 单独拉号（次入口） · 跟 bus 无关 · 拉完进 record group ── */

export const usePull = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { vendor_id?: string; count: number }) => post("/me/extract", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["pullRecords"] });
      qc.invalidateQueries({ queryKey: ["wallet"] });
    },
  });
};

/* ── 钱包 · 充值 / 兑换 ── */

/** 生成充值单 → 返回二维码 · 扫码付到 waffo */
export const useCreateTopup = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (paid: Money) => post<TopupOrder>("/me/topup", { paid }),
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
