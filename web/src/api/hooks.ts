import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, del, post, put } from "./client";
import type {
  Activity, ApiKey, Bus, Credential, DownstreamConfig, ExtractRecord, LedgerEntry,
  Overview, Paged, Passenger, PullRound, StockSummary, TimeRange, TrendMetric,
  TrendPoint, VendorShare, VendorStat, Wallet, WebhookConfig, WebhookDelivery,
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
    mutationFn: (body: { vendor_id: string; count: number }) => post("/me/extract", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["extractRecords"] });
      qc.invalidateQueries({ queryKey: ["wallet"] });
    },
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

export const useApiKeys = () =>
  useQuery({ queryKey: ["apiKeys"], queryFn: () => api<ApiKey[]>("/me/api-keys") });
