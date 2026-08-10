import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router-dom";
import { ArrowRight } from "lucide-react";
import {
  Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";
import { AppFooter } from "@/components/AppFooter";
import { PromoBar } from "@/components/PromoBar";
import { PublicHeader } from "@/components/PublicHeader";
import { DocumentMeta } from "@/components/DocumentMeta";
import { Button } from "@/components/ui/button";
import { Card, Chip } from "@/components/ui/primitives";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useVendorStatus, useVendorDispatchEvents,
  type VendorStatusRow, type VendorDispatchEvent,
} from "@/api/hooks";
import { cn } from "@/lib/utils";

/** 上游状态页 · /status 公开
 *
 *  **一种数据形状 · 一种图**：后端 /events 端点对 6 家返同一形状的开号事件流
 *  （有 fleet 端点的用上游自报 · 没有的从探针增量推 · 标 derived）。所以这里
 *  只有一种图（每小时聚合柱状）+ 一个事件 log 列表 · 没有 if/else 换图形。
 *
 *  术语（对外一律用"API key"不用内部说法 · CLAUDE.md §12.6）：
 *   - 一次「开号」= 上游放出一批 API key
 *   - count = 这批几个 key
 *   - 批 = 一次开号动作 */
export default function StatusPage() {
  const params = useParams();
  if (params.anonId) return <VendorDetail anonID={params.anonId} />;
  return <StatusOverview />;
}

// ═══════════════════════════════════════════════════════════
// 主页
// ═══════════════════════════════════════════════════════════

function StatusOverview() {
  const { t } = useTranslation("status");
  const { data, isLoading } = useVendorStatus();
  const vendors = data?.vendors ?? [];

  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <DocumentMeta titleKey="status:meta.title" descriptionKey="status:meta.description" />
      <PromoBar />
      <PublicHeader />

      <main className="flex-1">
        <section className="page-container py-10 lg:py-12">
          <div className="mx-auto max-w-5xl space-y-8">

            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <PingDot tone="ok" />
                <span className="font-mono text-label font-medium uppercase tracking-wider text-fg-tertiary">
                  {t("hero.eyebrow")}
                </span>
              </div>
              <h1 className="text-2xl font-semibold tracking-tight md:text-3xl">
                {t("hero.title")}
              </h1>
              <p className="max-w-[65ch] text-label leading-relaxed text-fg-secondary">
                {t("hero.subtitle")}
              </p>
            </div>

            {isLoading ? (
              <div className="space-y-3">
                {Array.from({ length: 6 }).map((_, i) => (
                  <Skeleton key={i} className="h-[132px] w-full" />
                ))}
              </div>
            ) : vendors.length === 0 ? (
              <Card className="p-8 text-center">
                <p className="text-fg-secondary">{t("hero.no-data")}</p>
              </Card>
            ) : (
              <div className="space-y-3">
                {vendors.map((v) => <VendorCard key={v.anon_id} vendor={v} />)}
              </div>
            )}

            <div className="grid gap-4 md:grid-cols-2">
              <Card className="flex flex-col justify-between gap-3 p-5">
                <div className="space-y-1">
                  <h3 className="font-semibold">{t("cta.prices.title")}</h3>
                  <p className="text-label leading-relaxed text-fg-secondary">{t("cta.prices.body")}</p>
                </div>
                <Button asChild variant="ghost" className="self-start">
                  <Link to="/prices">{t("cta.prices.action")}<ArrowRight /></Link>
                </Button>
              </Card>
              <Card className="flex flex-col justify-between gap-3 p-5 border-brand-hairline bg-brand-subtle/30">
                <div className="space-y-1">
                  <h3 className="font-semibold">{t("cta.join.title")}</h3>
                  <p className="text-label leading-relaxed text-fg-secondary">{t("cta.join.body")}</p>
                </div>
                <Button asChild variant="brand" className="self-start">
                  <Link to="/register">{t("cta.join.action")}<ArrowRight /></Link>
                </Button>
              </Card>
            </div>
          </div>
        </section>
      </main>

      <AppFooter />
    </div>
  );
}

/** 单家 vendor 卡 · 主页一行一家
 *
 *  布局：左（名字 + 标签）· 中（24h 柱图）· 右（3 个数字）
 *  所有 6 家同一种图 —— 数据来源不同只在图下角标一句说明。 */
function VendorCard({ vendor }: { vendor: VendorStatusRow }) {
  const { t, i18n } = useTranslation("status");
  const { data, isLoading } = useVendorDispatchEvents(vendor.anon_id, "168h");
  const events = data?.events ?? [];
  const summary = data?.summary;
  const derived = data?.source === "observed";

  const last = events[0];

  return (
    <Link
      to={`/status/${vendor.anon_id}`}
      className="group block rounded-2xl border border-hairline bg-surface p-5 transition-all hover:border-fg-tertiary hover:shadow-md"
    >
      {/* 头行 · 名字 + 标签 + 详情箭头 */}
      <div className="mb-4 flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-2">
          <div className="flex items-center gap-2">
            <PingDot tone={vendor.alive ? "ok" : "danger"} />
            <span className="font-semibold">{vendor.anon_label}</span>
          </div>
          <QualityTags vendor={vendor} />
        </div>
        <span className="shrink-0 text-label text-fg-tertiary transition-colors group-hover:text-fg">
          {t("card.detail")} →
        </span>
      </div>

      <div className="grid gap-4 md:grid-cols-[1fr_auto] md:gap-6">
        {/* 图 · 24h 每小时聚合 · 6 家同一种 */}
        <div className="min-w-0">
          {isLoading ? (
            <Skeleton className="h-[72px] w-full" />
          ) : (
            <DispatchChart events={events} hours={parseWindowHours(data?.window, 168)} height={72} compact />
          )}
          {/* 数据来源说明 · 只在有数据时显示（空态图内部已经有提示 · 别重复） */}
          {events.length > 0 && (
            <p className="mt-1 text-[10px] text-fg-tertiary">
              {derived ? t("chart.source-observed") : t("chart.source-vendor")}
            </p>
          )}
        </div>

        {/* 数字 · 3 项 · 单位写清楚 */}
        <div className="grid grid-cols-3 gap-4 md:w-[280px]">
          <Metric
            value={fmtCompact(summary?.keys ?? 0)}
            unit={t("unit.keys")}
            label={t("card.keys-7d")} />
          <Metric
            value={String(summary?.batches ?? 0)}
            unit={t("unit.batches")}
            label={t("card.batches-7d")} />
          <Metric
            value={last ? fmtRelative(last.at, i18n.language) : "—"}
            label={t("card.last-open")} />
        </div>
      </div>
    </Link>
  );
}

/** 数字 + 单位 + 说明 · 单位独立小字 · 避免"号"这种没头没尾的字 */
function Metric({
  value, unit, label,
}: {
  value: string;
  unit?: string;
  label: string;
}) {
  return (
    <div>
      <div className="font-mono text-lg font-semibold tabular-nums leading-tight">
        {value}
        {unit && <span className="ml-1 text-[11px] font-normal text-fg-tertiary">{unit}</span>}
      </div>
      <div className="mt-0.5 text-[10px] leading-tight text-fg-tertiary">{label}</div>
    </div>
  );
}

function PingDot({ tone }: { tone: "ok" | "warn" | "danger" }) {
  const bg = tone === "ok" ? "bg-ok-solid" : tone === "warn" ? "bg-warn-solid" : "bg-danger-solid";
  return (
    <span className="relative flex size-2">
      <span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-60", bg)} />
      <span className={cn("relative inline-flex size-2 rounded-full", bg)} />
    </span>
  );
}

function QualityTags({ vendor }: { vendor: VendorStatusRow }) {
  const { t } = useTranslation("status");
  const tags: { label: string; tone: "ok" | "warn" | "danger" | "neutral" | "brand" }[] = [];

  if (vendor.stock_bucket === "many") tags.push({ label: t("tags.stock-many"), tone: "ok" });
  else if (vendor.stock_bucket === "low") tags.push({ label: t("tags.stock-low"), tone: "warn" });
  else if (vendor.stock_bucket === "out") tags.push({ label: t("tags.stock-out"), tone: "danger" });

  if (vendor.lifespan_bucket === "long") tags.push({ label: t("tags.lifespan-long"), tone: "ok" });
  else if (vendor.lifespan_bucket === "mid") tags.push({ label: t("tags.lifespan-mid"), tone: "neutral" });
  else if (vendor.lifespan_bucket === "short") tags.push({ label: t("tags.lifespan-short"), tone: "warn" });

  if (vendor.has_warranty && vendor.warranty_minutes) {
    tags.push({ label: t("tags.warranty-min", { minutes: vendor.warranty_minutes }), tone: "brand" });
  }
  if (vendor.uptime_24h_pct !== undefined) {
    tags.push({
      label: t("tags.uptime", { pct: vendor.uptime_24h_pct }),
      tone: vendor.uptime_24h_pct >= 99 ? "ok" : vendor.uptime_24h_pct >= 95 ? "neutral" : "warn",
    });
  }
  if (vendor.incidents_7d && vendor.incidents_7d.length > 0) {
    tags.push({ label: t("tags.incidents", { count: vendor.incidents_7d.length }), tone: "warn" });
  }

  if (tags.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1.5">
      {tags.map((tag, i) => <Chip key={i} tone={tag.tone}>{tag.label}</Chip>)}
    </div>
  );
}

/** 唯一的图 · 把事件流按小时聚合成柱状
 *
 *  6 家共用 —— 事件来源（vendor 自报 / 探针推算）不改变图形，只改图下那句说明。
 *  y = 该小时发出的 API key 数 · x = 时间。 */
function DispatchChart({
  events, hours, height = 220, compact = false,
}: {
  events: VendorDispatchEvent[];
  hours: number;
  height?: number;
  compact?: boolean;
}) {
  const { t } = useTranslation("status");

  /** 桶宽自适应 · 窗口越长桶越粗 · 保证柱子数量在 24-90 根之间（太多糊成一片）
   *  ≤48h → 1 小时 · ≤336h(14d) → 6 小时 · 更长 → 1 天 */
  const bucketMs =
    hours <= 48  ? 3600_000 :
    hours <= 336 ? 6 * 3600_000 :
                   24 * 3600_000;

  const buckets = useMemo(() => {
    const now = Date.now();
    const from = now - hours * 3600_000;
    // 建连续空桶 · 让"这段时间没开号"也能看出来（跳着画的柱图会误导节奏）
    const map = new Map<number, number>();
    const start = Math.floor(from / bucketMs) * bucketMs;
    for (let ts = start; ts <= now; ts += bucketMs) map.set(ts, 0);
    for (const e of events) {
      const ts = new Date(e.at).getTime();
      if (ts < start) continue;
      const key = Math.floor(ts / bucketMs) * bucketMs;
      map.set(key, (map.get(key) ?? 0) + e.count);
    }
    return Array.from(map.entries())
      .sort((a, b) => a[0] - b[0])
      .map(([ts, keys]) => ({ ts, keys }));
  }, [events, hours, bucketMs]);

  const hasAny = buckets.some(b => b.keys > 0);
  if (!hasAny) {
    // 窗口 > 48h 时说"天"更好读（"近 30 天无开号记录" 比 "近 720 小时" 清楚）
    const label = hours >= 48
      ? t("chart.no-data-days", { days: Math.round(hours / 24) })
      : t("chart.no-data-window", { hours });
    return (
      <div className="grid place-items-center text-label text-fg-tertiary" style={{ height }}>
        {label}
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={buckets} margin={{ top: 4, right: 4, bottom: 0, left: compact ? 0 : -20 }}>
        {!compact && (
          <CartesianGrid strokeDasharray="0" vertical={false} stroke="hsl(var(--hairline))" />
        )}
        <XAxis
          dataKey="ts"
          hide={compact}
          tickLine={false}
          axisLine={false}
          interval="preserveStartEnd"
          minTickGap={48}
          tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
          tickFormatter={(ts: number) => {
            const d = new Date(ts);
            return bucketMs >= 24 * 3600_000
              ? `${d.getMonth() + 1}/${d.getDate()}`
              : bucketMs > 3600_000
                ? `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}h`
                : `${String(d.getHours()).padStart(2, "0")}:00`;
          }}
        />
        <YAxis
          hide={compact}
          tickLine={false}
          axisLine={false}
          width={40}
          allowDecimals={false}
          tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
        />
        <Tooltip
          cursor={{ fill: "hsl(var(--fg) / 0.04)" }}
          content={({ active, payload }) => {
            if (!active || !payload?.length) return null;
            const p = payload[0].payload as { ts: number; keys: number };
            const d = new Date(p.ts);
            const pad = (n: number) => String(n).padStart(2, "0");
            // 日桶不显示小时（那个小时数没意义 · 是桶起点不是事件时刻）
            const stamp = bucketMs >= 24 * 3600_000
              ? `${d.getMonth() + 1}/${d.getDate()}`
              : `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:00`;
            return (
              <div className="rounded-xl border border-hairline bg-bg px-3 py-2 shadow-pop">
                <div className="text-label text-fg-tertiary">{stamp}</div>
                <div className="mt-0.5 font-semibold tnum">
                  {p.keys} {t("unit.keys")}
                </div>
              </div>
            );
          }}
        />
        <Bar dataKey="keys" fill="hsl(var(--brand-solid))" radius={[2, 2, 0, 0]} />
      </BarChart>
    </ResponsiveContainer>
  );
}

// ═══════════════════════════════════════════════════════════
// 详情页 · /status/:anon_id
// ═══════════════════════════════════════════════════════════

function VendorDetail({ anonID }: { anonID: string }) {
  const { t } = useTranslation("status");
  const { data: overview } = useVendorStatus();
  const { data, isLoading } = useVendorDispatchEvents(anonID, "168h");

  const vendor = overview?.vendors.find(v => v.anon_id === anonID);
  const notFound = !!overview && !vendor;
  const events = data?.events ?? [];
  const summary = data?.summary;
  const derived = data?.source === "observed";

  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <DocumentMeta titleKey="status:meta.title" descriptionKey="status:meta.description" />
      <PromoBar />
      <PublicHeader />

      <main className="flex-1">
        <section className="page-container py-10">
          <div className="mx-auto max-w-5xl space-y-6">
            <Link to="/status" className="inline-flex items-center gap-1 text-label text-fg-tertiary hover:text-fg">
              ← {t("detail.back")}
            </Link>

            {notFound ? (
              <Card className="p-8 text-center">
                <p className="text-fg-secondary">{t("detail.not-found")}</p>
              </Card>
            ) : !vendor ? (
              <div className="space-y-4">
                <Skeleton className="h-24 w-full" />
                <Skeleton className="h-72 w-full" />
              </div>
            ) : (
              <>
                <div className="space-y-3">
                  <div className="flex items-center gap-3">
                    <PingDot tone={vendor.alive ? "ok" : "danger"} />
                    <h1 className="text-2xl font-semibold">{vendor.anon_label}</h1>
                  </div>
                  <QualityTags vendor={vendor} />
                </div>

                {/* 指标 · 单位写清楚 */}
                <div className="grid grid-cols-2 gap-5 rounded-2xl border border-hairline bg-surface p-5 md:grid-cols-4">
                  <Metric
                    value={fmtCompact(summary?.keys ?? 0)}
                    unit={t("unit.keys")}
                    label={t("detail.keys-7d")} />
                  <Metric
                    value={String(summary?.batches ?? 0)}
                    unit={t("unit.batches")}
                    label={t("detail.batches-7d")} />
                  <Metric
                    value={summary?.avg_interval_min ? fmtInterval(summary.avg_interval_min) : "—"}
                    label={t("detail.avg-interval")} />
                  <Metric
                    value={vendor.warranty_minutes ? String(vendor.warranty_minutes) : "—"}
                    unit={vendor.warranty_minutes ? t("unit.minutes") : undefined}
                    label={t("detail.warranty")} />
                </div>

                {/* 图 · 跟主页同一个组件 · 只是放大 + 显示坐标轴 */}
                <div className="space-y-2">
                  <div className="flex items-baseline justify-between">
                    <h2 className="text-body-lg font-semibold">{t("detail.chart-title")}</h2>
                    <p className="text-[11px] text-fg-tertiary">
                      {derived ? t("chart.source-observed") : t("chart.source-vendor")}
                    </p>
                  </div>
                  {isLoading ? (
                    <Skeleton className="h-56 w-full" />
                  ) : (
                    <div className="rounded-2xl border border-hairline bg-surface p-5">
                      <DispatchChart events={events} hours={parseWindowHours(data?.window, 168)} height={220} />
                    </div>
                  )}
                </div>

                {/* Log 列表 · 每次开号一行 · 6 家都有（有数据就有） */}
                <EventLog events={events} derived={derived} loading={isLoading} />
              </>
            )}
          </div>
        </section>
      </main>

      <AppFooter />
    </div>
  );
}

/** 开号 log · 一次开号一行 · 时间 / 区域 / 数量 / 状态 / 存活 */
function EventLog({
  events, derived, loading,
}: {
  events: VendorDispatchEvent[];
  derived: boolean;
  loading: boolean;
}) {
  const { t, i18n } = useTranslation("status");

  if (loading) return <Skeleton className="h-64 w-full" />;
  if (events.length === 0) return null;

  return (
    <div className="space-y-2">
      <div className="flex items-baseline justify-between">
        <h2 className="text-body-lg font-semibold">{t("log.title")}</h2>
        <p className="text-[11px] text-fg-tertiary">{t("log.count", { count: events.length })}</p>
      </div>

      <div className="overflow-hidden rounded-2xl border border-hairline bg-surface">
        {/* 表头 */}
        <div className="grid grid-cols-[1fr_auto_auto_auto] gap-3 border-b border-hairline px-4 py-2.5 text-[10px] uppercase tracking-wider text-fg-tertiary md:grid-cols-[160px_60px_1fr_auto_auto] md:gap-4 md:px-5">
          <span>{t("log.col-time")}</span>
          <span className="hidden md:block">{t("log.col-region")}</span>
          <span className="md:text-left">{t("log.col-count")}</span>
          <span className="text-right">{t("log.col-alive")}</span>
          <span className="text-right">{t("log.col-status")}</span>
        </div>

        {events.map((e, i) => (
          <div
            key={`${e.at}-${i}`}
            className={cn(
              "grid grid-cols-[1fr_auto_auto_auto] items-center gap-3 px-4 py-2.5 text-label md:grid-cols-[160px_60px_1fr_auto_auto] md:gap-4 md:px-5",
              i < events.length - 1 && "border-b border-hairline",
            )}
          >
            {/* 时间 · 绝对 + 相对 */}
            <div className="min-w-0">
              <div className="font-mono text-[11px] tabular-nums">{fmtDateTime(e.at)}</div>
              <div className="text-[10px] text-fg-tertiary">{fmtRelative(e.at, i18n.language)}</div>
            </div>

            {/* 区域 */}
            <div className="hidden text-[11px] text-fg-secondary md:block">
              {e.region || "—"}
            </div>

            {/* 数量 */}
            <div className="font-mono font-semibold tabular-nums text-brand-fg">
              +{e.count}
              <span className="ml-0.5 text-[10px] font-normal text-fg-tertiary">
                {t("unit.keys")}
              </span>
            </div>

            {/* 存活 · derived 源没有这个数据 */}
            <div className="text-right font-mono text-[11px] tabular-nums">
              {derived
                ? <span className="text-fg-tertiary">—</span>
                : e.alive
                  ? <span className="text-ok-fg">{e.alive}</span>
                  : <span className="text-fg-tertiary">0</span>}
            </div>

            {/* 状态 · 三态 */}
            <div className="text-right">
              {e.status === "running" ? (
                <Chip tone="ok">{t("log.status-running")}</Chip>
              ) : e.status === "dead" ? (
                <Chip tone="neutral">{t("log.status-dead")}</Chip>
              ) : (
                <Chip tone="neutral">{t("log.status-done")}</Chip>
              )}
            </div>
          </div>
        ))}
      </div>

      {derived && (
        <p className="text-[10px] leading-relaxed text-fg-tertiary">
          {t("log.derived-note")}
        </p>
      )}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════
// 格式化
// ═══════════════════════════════════════════════════════════

/** 解析后端回报的 window 字段（"168h" / "720h"）· 后端会因为自动扩窗改这个值 ·
 *  图必须按**实际**窗口画 · 不能按请求的窗口画（否则老数据落在图外看不见） */
function parseWindowHours(window: string | undefined, fallback: number): number {
  if (!window) return fallback;
  const n = parseInt(window.replace(/h$/, ""), 10);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

function fmtCompact(n: number): string {
  if (!n) return "0";
  if (n < 1000) return String(n);
  if (n < 10000) return (n / 1000).toFixed(1) + "K";
  if (n < 1_000_000) return Math.round(n / 1000) + "K";
  return (n / 1_000_000).toFixed(1) + "M";
}

function fmtInterval(min: number): string {
  if (min < 60) return `${Math.round(min)}min`;
  if (min < 60 * 24) return `${(min / 60).toFixed(1)}h`;
  return `${Math.round(min / 60 / 24)}d`;
}

/** 相对时间 · 用 Intl.RelativeTimeFormat 走浏览器本地化 · 不硬编中文
 *  （i18n.language 传进来 · 中文得"7 小时前"· 英文得"7 hours ago"） */
function fmtRelative(iso: string, lang: string): string {
  const diffMs = Date.now() - new Date(iso).getTime();
  const rtf = new Intl.RelativeTimeFormat(lang, { numeric: "auto" });
  const min = Math.round(diffMs / 60_000);
  if (min < 1) return rtf.format(0, "minute");
  if (min < 60) return rtf.format(-min, "minute");
  const h = Math.round(min / 60);
  if (h < 24) return rtf.format(-h, "hour");
  return rtf.format(-Math.round(h / 24), "day");
}

function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
