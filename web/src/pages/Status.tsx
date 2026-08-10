import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router-dom";
import { ArrowRight, ChevronRight } from "lucide-react";
import { AppFooter } from "@/components/AppFooter";
import { PromoBar } from "@/components/PromoBar";
import { PublicHeader } from "@/components/PublicHeader";
import { DocumentMeta } from "@/components/DocumentMeta";
import { Button } from "@/components/ui/button";
import { Card, Chip } from "@/components/ui/primitives";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useVendorStatus, useVendorStatusTrend,
  type VendorStatusRow, type VendorStatusTrendPoint,
} from "@/api/hooks";
import { cn } from "@/lib/utils";

/** 上游状态页 · /status 公开可查
 *
 *  信息层级：
 *   顶部 · 4 个总览数字（累计发号 / vendor 上架 / 平均间隔 / 最新一批）· 字号克制
 *   中部 · 每家一行 · 状态点 + 库存 + 稳定度 + 24h timeline + 累计 + 详情箭头
 *   详情 · 点行进 /status/:anon_id · 单家完整历史（另一个组件）
 *
 *  匿名 label（AWS-Q Kiro Vendor 01..06）永远脱敏 · 真名只 wholesale 档看 */
export default function StatusPage() {
  const params = useParams();
  if (params.anonId) return <VendorDetail anonID={params.anonId} />;
  return <StatusOverview />;
}

// ═══════════════════════════════════════════════════════════
// Overview · /status 主页
// ═══════════════════════════════════════════════════════════

function StatusOverview() {
  const { t } = useTranslation("status");
  const { data, isLoading } = useVendorStatus();
  const vendors = data?.vendors ?? [];

  const overview = useMemo(() => {
    const totalKeys = vendors.reduce((s, v) => s + (v.dispatch?.total_keys_dispatched ?? 0), 0);
    const aliveCount = vendors.filter(v => v.alive).length;
    const stockedCount = vendors.filter(v => v.stock_bucket === "many" || v.stock_bucket === "low").length;
    const intervals = vendors
      .map(v => v.dispatch?.avg_interval_min)
      .filter((x): x is number => typeof x === "number" && x > 0);
    const avgInterval = intervals.length
      ? intervals.reduce((s, x) => s + x, 0) / intervals.length
      : 0;
    const latestTs = vendors
      .map(v => v.dispatch?.last_dispatch_at)
      .filter((x): x is string => !!x)
      .sort()
      .pop();
    return { totalKeys, aliveCount, stockedCount, avgInterval, latestTs };
  }, [vendors]);

  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <DocumentMeta titleKey="status:meta.title" descriptionKey="status:meta.description" />
      <PromoBar />
      <PublicHeader />

      <main className="flex-1">
        <section className="page-container py-12 lg:py-14">
          <div className="mx-auto max-w-5xl space-y-8">

            {/* Hero · 简洁 · 不占太多屏幕 */}
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <PingDot tone={overview.aliveCount === vendors.length && vendors.length > 0 ? "ok" : "warn"} />
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

            {/* 总览数字 · 4 项 · 数字克制 (text-2xl) · label 灰色小字 */}
            {isLoading ? (
              <Skeleton className="h-16 w-full" />
            ) : (
              <div className="grid grid-cols-2 gap-4 rounded-2xl border border-hairline bg-surface p-5 md:grid-cols-4 md:gap-6">
                <Stat
                  value={fmtCompact(overview.totalKeys)}
                  label={t("overview.total-keys")} />
                <Stat
                  value={`${overview.stockedCount}/${vendors.length || 6}`}
                  label={t("overview.stocked")}
                  tone={overview.stockedCount === 0 ? "warn" : "ok"} />
                <Stat
                  value={overview.avgInterval > 0 ? fmtInterval(overview.avgInterval) : "-"}
                  label={t("overview.avg-interval")} />
                <Stat
                  value={overview.latestTs ? fmtRelative(overview.latestTs) : "-"}
                  label={t("overview.latest-batch")} />
              </div>
            )}

            {/* 6 家列表 · 一行一家 · 点击进详情 */}
            <div className="space-y-2">
              <div className="flex items-baseline justify-between">
                <h2 className="text-body-lg font-semibold">
                  {t("fleet.title")}
                </h2>
                <p className="text-[11px] text-fg-tertiary">
                  {t("fleet.subtitle")}
                </p>
              </div>

              {isLoading ? (
                <div className="space-y-2">
                  {Array.from({ length: 6 }).map((_, i) => (
                    <Skeleton key={i} className="h-14 w-full" />
                  ))}
                </div>
              ) : vendors.length === 0 ? (
                <Card className="p-8 text-center">
                  <p className="text-fg-secondary">{t("hero.no-data")}</p>
                </Card>
              ) : (
                <div className="overflow-hidden rounded-2xl border border-hairline bg-surface">
                  {vendors.map((v, i) => (
                    <VendorRow
                      key={v.anon_id}
                      vendor={v}
                      isLast={i === vendors.length - 1} />
                  ))}
                </div>
              )}
            </div>

            {/* CTA · 保留 */}
            <div className="grid gap-4 md:grid-cols-2">
              <Card className="flex flex-col justify-between gap-3 p-5">
                <div className="space-y-1">
                  <h3 className="font-semibold">{t("cta.prices.title")}</h3>
                  <p className="text-label leading-relaxed text-fg-secondary">
                    {t("cta.prices.body")}
                  </p>
                </div>
                <Button asChild variant="ghost" className="self-start">
                  <Link to="/prices">{t("cta.prices.action")}<ArrowRight /></Link>
                </Button>
              </Card>
              <Card className="flex flex-col justify-between gap-3 p-5 border-brand-hairline bg-brand-subtle/30">
                <div className="space-y-1">
                  <h3 className="font-semibold">{t("cta.join.title")}</h3>
                  <p className="text-label leading-relaxed text-fg-secondary">
                    {t("cta.join.body")}
                  </p>
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

/** 顶部 4 数字 · 克制字号 */
function Stat({
  value, label, tone,
}: {
  value: string;
  label: string;
  tone?: "ok" | "warn";
}) {
  const toneClass =
    tone === "ok" ? "text-ok-fg" :
    tone === "warn" ? "text-warn-fg" :
    "text-fg";
  return (
    <div className="space-y-0.5">
      <div className={cn("font-mono text-xl font-semibold tabular-nums md:text-2xl", toneClass)}>
        {value}
      </div>
      <div className="text-[11px] uppercase tracking-wider text-fg-tertiary">
        {label}
      </div>
    </div>
  );
}

/** 状态点 · ping 动画 · 表示"实时活着" */
function PingDot({ tone = "ok" }: { tone?: "ok" | "warn" | "danger" }) {
  const bg =
    tone === "ok" ? "bg-ok-solid" :
    tone === "warn" ? "bg-warn-solid" :
    "bg-danger-solid";
  return (
    <span className="relative flex size-2">
      <span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-60", bg)} />
      <span className={cn("relative inline-flex size-2 rounded-full", bg)} />
    </span>
  );
}

/** 单行 vendor · 主页只显示 · 点击进详情
 *  语义拆分：
 *   - 服务状态（alive/dead 网络通不通）
 *   - 库存（有货/缺货 stock_bucket）
 *   - 最近一批 · 相对时间
 *   - 24h timeline · 事件点风格 · 6 家统一 */
function VendorRow({ vendor, isLast }: { vendor: VendorStatusRow; isLast: boolean }) {
  const { t } = useTranslation("status");
  const disp = vendor.dispatch;
  const batches = disp?.total_batches ?? 0;
  const keys = disp?.total_keys_dispatched ?? 0;

  const stockLabel =
    vendor.stock_bucket === "many" ? t("row.stock-many") :
    vendor.stock_bucket === "low"  ? t("row.stock-low") :
    vendor.stock_bucket === "out"  ? t("row.stock-out") :
                                     t("row.stock-unknown");
  const stockTone: "ok" | "warn" | "danger" | "neutral" =
    vendor.stock_bucket === "many" ? "ok" :
    vendor.stock_bucket === "low"  ? "warn" :
    vendor.stock_bucket === "out"  ? "danger" :
                                     "neutral";

  // "服务状态"（不同于库存）
  const serviceOk = vendor.alive;
  const uptime = vendor.uptime_24h_pct;

  return (
    <Link
      to={`/status/${vendor.anon_id}`}
      className={cn(
        "group flex items-center gap-3 p-4 transition-colors hover:bg-fg/[0.02] md:gap-5 md:p-5",
        !isLast && "border-b border-hairline",
      )}
    >
      {/* 左 · 状态点 + label + 服务/uptime 小字 */}
      <div className="flex min-w-0 items-center gap-3 md:w-[190px]">
        <PingDot tone={serviceOk ? "ok" : "danger"} />
        <div className="min-w-0">
          <div className="truncate text-body-sm font-medium">
            {vendor.anon_label}
          </div>
          <div className="text-[10px] uppercase tracking-wider text-fg-tertiary">
            {uptime !== undefined
              ? t("row.uptime", { pct: uptime })
              : t("row.warming")}
          </div>
        </div>
      </div>

      {/* 中 · 库存 chip */}
      <div className="hidden md:block md:w-[90px]">
        <Chip tone={stockTone}>{stockLabel}</Chip>
      </div>

      {/* Timeline · 一致的事件点风格 */}
      <div className="hidden min-w-0 flex-1 md:block">
        <TimelineDots anonID={vendor.anon_id} />
      </div>

      {/* 右 · 累计数字 + 相对时间 */}
      <div className="ml-auto text-right md:min-w-[130px]">
        {batches > 0 ? (
          <>
            <div className="font-mono text-body-sm font-semibold tabular-nums">
              {fmtCompact(keys)}
              <span className="ml-1 text-[11px] text-fg-tertiary">
                {t("row.keys-unit")}
              </span>
            </div>
            <div className="text-[10px] text-fg-tertiary">
              {disp?.last_dispatch_at ? fmtRelative(disp.last_dispatch_at) : t("row.warming")}
            </div>
          </>
        ) : (
          <div className="text-[11px] text-fg-tertiary">{t("row.warming")}</div>
        )}
      </div>

      {/* 详情箭头 */}
      <ChevronRight className="size-4 shrink-0 text-fg-tertiary transition-transform group-hover:translate-x-0.5 group-hover:text-fg" />
    </Link>
  );
}

/** 24h timeline · 事件点风格（一堆小圆点分布在时间轴上）· 6 家统一
 *  优先 backfill · fallback probe · 都无就静默 */
function TimelineDots({ anonID }: { anonID: string }) {
  const { data, isLoading } = useVendorStatusTrend(anonID, "24h");
  if (isLoading) return <div className="h-8" />;
  const points = data?.points ?? [];
  if (points.length < 2) return <div className="h-8" />;

  // 每个 point 变成 0-N 个"事件点" · 数据越多点越多
  const events: { t: number; alive: boolean; weight: number }[] = [];
  const source = data?.source;
  for (const p of points) {
    const ts = new Date(p.t).getTime();
    if (source === "backfill") {
      const born = p.keys_born ?? 0;
      const died = p.keys_died ?? 0;
      if (born > 0) events.push({ t: ts, alive: true, weight: Math.min(born, 20) });
      if (died > 0) events.push({ t: ts, alive: false, weight: Math.min(died, 20) });
    } else if (p.uptime_pct !== undefined) {
      // probe · 每桶画一个点 · alive 用 uptime
      events.push({ t: ts, alive: p.uptime_pct >= 95, weight: 3 });
    }
  }
  if (events.length === 0) return <div className="h-8" />;

  const first = new Date(points[0].t).getTime();
  const last = new Date(points[points.length - 1].t).getTime();
  const span = Math.max(1, last - first);

  const W = 720, H = 32;
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" preserveAspectRatio="none" style={{ height: 32 }}>
      {/* 底轴 · 淡色线 */}
      <line x1={0} y1={H / 2} x2={W} y2={H / 2} className="stroke-hairline" strokeWidth={0.5} />
      {events.map((e, i) => {
        const cx = ((e.t - first) / span) * W;
        const r = 1.5 + Math.min(4, e.weight / 4);
        return (
          <circle
            key={i}
            cx={cx}
            cy={H / 2}
            r={r}
            className={e.alive ? "fill-ok-solid" : "fill-fg-tertiary"}
            opacity={e.alive ? 0.75 : 0.4}
          />
        );
      })}
    </svg>
  );
}

// ═══════════════════════════════════════════════════════════
// Detail · /status/:anon_id
// ═══════════════════════════════════════════════════════════

function VendorDetail({ anonID }: { anonID: string }) {
  const { t } = useTranslation("status");
  const { data: overview } = useVendorStatus();
  const { data: trend, isLoading } = useVendorStatusTrend(anonID, "168h"); // 7 天

  const vendor = overview?.vendors.find(v => v.anon_id === anonID);
  const notFound = !!overview && !vendor;

  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <DocumentMeta titleKey="status:meta.title" descriptionKey="status:meta.description" />
      <PromoBar />
      <PublicHeader />

      <main className="flex-1">
        <section className="page-container py-12">
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
                <Skeleton className="h-40 w-full" />
              </div>
            ) : (
              <>
                {/* 头部 · label + 服务状态 + 概览指标 */}
                <div className="space-y-3">
                  <div className="flex items-center gap-3">
                    <PingDot tone={vendor.alive ? "ok" : "danger"} />
                    <h1 className="text-2xl font-semibold">{vendor.anon_label}</h1>
                  </div>
                  <QualityTags vendor={vendor} />
                </div>

                {/* 关键指标 grid · 6 项 · 详情页信息比主页多 */}
                <div className="grid grid-cols-2 gap-4 rounded-2xl border border-hairline bg-surface p-5 md:grid-cols-3">
                  <Stat
                    value={fmtCompact(vendor.dispatch?.total_keys_dispatched ?? 0)}
                    label={t("detail.total-keys")} />
                  <Stat
                    value={String(vendor.dispatch?.total_batches ?? 0)}
                    label={t("detail.total-batches")} />
                  <Stat
                    value={vendor.dispatch?.avg_interval_min ? fmtInterval(vendor.dispatch.avg_interval_min) : "-"}
                    label={t("detail.avg-interval")} />
                  <Stat
                    value={vendor.uptime_24h_pct !== undefined ? `${vendor.uptime_24h_pct}%` : "-"}
                    label={t("detail.uptime-24h")}
                    tone={vendor.uptime_24h_pct !== undefined && vendor.uptime_24h_pct >= 99 ? "ok" : undefined} />
                  <Stat
                    value={vendor.warranty_minutes ? t("detail.warranty-value", { minutes: vendor.warranty_minutes }) : "-"}
                    label={t("detail.warranty")} />
                  <Stat
                    value={vendor.max_per_order ? String(vendor.max_per_order) : "-"}
                    label={t("detail.max-per-order")} />
                </div>

                {/* 7 天 timeline · 详情页放大版 */}
                <div className="space-y-2">
                  <h2 className="text-body-lg font-semibold">{t("detail.timeline-title")}</h2>
                  <p className="text-[11px] text-fg-tertiary">{t("detail.timeline-subtitle")}</p>
                  {isLoading ? (
                    <Skeleton className="h-24 w-full" />
                  ) : (
                    <div className="rounded-2xl border border-hairline bg-surface p-5">
                      <TimelineDots7d points={trend?.points ?? []} source={trend?.source} />
                    </div>
                  )}
                </div>

                {/* 事件列表 · 最近开号（如果 backfill 有数据） */}
                {trend?.source === "backfill" && (trend.points?.length ?? 0) > 0 && (
                  <RecentEvents points={trend.points} />
                )}
              </>
            )}
          </div>
        </section>
      </main>

      <AppFooter />
    </div>
  );
}

/** 质量标签 · 跟 Overview 页 vendor 卡对齐 */
function QualityTags({ vendor }: { vendor: VendorStatusRow }) {
  const { t } = useTranslation("status");
  const tags: { label: string; tone: "ok" | "warn" | "danger" | "neutral" | "brand" }[] = [];

  // 库存
  if (vendor.stock_bucket === "many") tags.push({ label: t("row.stock-many"), tone: "ok" });
  else if (vendor.stock_bucket === "low") tags.push({ label: t("row.stock-low"), tone: "warn" });
  else if (vendor.stock_bucket === "out") tags.push({ label: t("row.stock-out"), tone: "danger" });

  // 寿命
  if (vendor.lifespan_bucket === "long") tags.push({ label: t("tags.lifespan-long"), tone: "ok" });
  else if (vendor.lifespan_bucket === "mid") tags.push({ label: t("tags.lifespan-mid"), tone: "neutral" });
  else if (vendor.lifespan_bucket === "short") tags.push({ label: t("tags.lifespan-short"), tone: "warn" });

  // 质保
  if (vendor.has_warranty) {
    tags.push({
      label: vendor.warranty_minutes ? t("tags.warranty-min", { minutes: vendor.warranty_minutes }) : t("tags.warranty"),
      tone: "brand",
    });
  }

  // 24h 事件
  if (vendor.incidents_7d && vendor.incidents_7d.length > 0) {
    tags.push({ label: t("tags.incidents", { count: vendor.incidents_7d.length }), tone: "warn" });
  }

  return (
    <div className="flex flex-wrap gap-2">
      {tags.map((tag, i) => <Chip key={i} tone={tag.tone}>{tag.label}</Chip>)}
    </div>
  );
}

/** 7 天详细 timeline · 大图 · x 轴显示日期 */
function TimelineDots7d({
  points, source,
}: {
  points: VendorStatusTrendPoint[];
  source?: string;
}) {
  if (points.length < 2) {
    return <p className="text-label text-fg-tertiary">数据积累中</p>;
  }
  const first = new Date(points[0].t).getTime();
  const last = new Date(points[points.length - 1].t).getTime();
  const span = Math.max(1, last - first);
  const W = 900, H = 80;

  const events: { t: number; alive: boolean; weight: number }[] = [];
  for (const p of points) {
    const ts = new Date(p.t).getTime();
    if (source === "backfill") {
      const born = p.keys_born ?? 0;
      const died = p.keys_died ?? 0;
      if (born > 0) events.push({ t: ts, alive: true, weight: born });
      if (died > 0) events.push({ t: ts, alive: false, weight: died });
    } else if (p.uptime_pct !== undefined) {
      events.push({ t: ts, alive: p.uptime_pct >= 95, weight: 3 });
    }
  }

  const maxWeight = Math.max(1, ...events.map(e => e.weight));

  return (
    <div className="space-y-2">
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full" preserveAspectRatio="none" style={{ height: 80 }}>
        <line x1={0} y1={H / 2} x2={W} y2={H / 2} className="stroke-hairline" strokeWidth={0.5} />
        {events.map((e, i) => {
          const cx = ((e.t - first) / span) * W;
          const r = 2 + (e.weight / maxWeight) * 10;
          return (
            <circle
              key={i}
              cx={cx}
              cy={H / 2}
              r={r}
              className={e.alive ? "fill-ok-solid" : "fill-fg-tertiary"}
              opacity={e.alive ? 0.7 : 0.35}
            />
          );
        })}
      </svg>
      <div className="flex justify-between text-[10px] uppercase tracking-wider text-fg-tertiary">
        <span>{fmtDate(points[0].t)}</span>
        <span>{fmtDate(points[points.length - 1].t)}</span>
      </div>
    </div>
  );
}

/** 最近开号事件表格 */
function RecentEvents({ points }: { points: VendorStatusTrendPoint[] }) {
  const { t } = useTranslation("status");
  // 过滤有事件的桶 · 倒序 · 最多 15
  const rows = points
    .filter(p => (p.keys_born ?? 0) > 0 || (p.keys_died ?? 0) > 0)
    .slice()
    .reverse()
    .slice(0, 15);

  if (rows.length === 0) return null;

  return (
    <div className="space-y-2">
      <h2 className="text-body-lg font-semibold">{t("detail.recent-events")}</h2>
      <div className="overflow-hidden rounded-2xl border border-hairline bg-surface">
        {rows.map((p, i) => (
          <div
            key={p.t}
            className={cn(
              "grid grid-cols-[auto_1fr_auto] items-center gap-4 p-3 md:p-4",
              i < rows.length - 1 && "border-b border-hairline",
            )}
          >
            <div className="font-mono text-[11px] text-fg-tertiary">
              {fmtDateTime(p.t)}
            </div>
            <div className="flex items-center gap-4 text-label">
              {(p.keys_born ?? 0) > 0 && (
                <span className="text-ok-fg">
                  +{p.keys_born}
                </span>
              )}
              {(p.keys_died ?? 0) > 0 && (
                <span className="text-fg-tertiary">
                  −{p.keys_died}
                </span>
              )}
            </div>
            <div className="text-[10px] text-fg-tertiary">
              {fmtRelative(p.t)}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ═══════════════════════════════════════════════════════════
// Format helpers
// ═══════════════════════════════════════════════════════════

function fmtCompact(n: number): string {
  if (!n) return "0";
  if (n < 1000) return String(n);
  if (n < 10000) return (n / 1000).toFixed(1) + "K";
  if (n < 1_000_000) return Math.round(n / 1000) + "K";
  return (n / 1_000_000).toFixed(1) + "M";
}

function fmtInterval(min: number): string {
  if (min < 60) return `${Math.round(min)}m`;
  if (min < 60 * 24) return `${(min / 60).toFixed(1)}h`;
  return `${Math.round(min / 60 / 24)}d`;
}

function fmtRelative(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const min = Math.floor(diff / 60000);
  if (min < 1) return "刚刚";
  if (min < 60) return `${min}m 前`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h}h 前`;
  return `${Math.floor(h / 24)}d 前`;
}

function fmtDate(iso: string): string {
  const d = new Date(iso);
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
