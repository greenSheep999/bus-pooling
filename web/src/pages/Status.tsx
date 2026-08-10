import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { ArrowRight, Activity, Zap, Clock, Shield } from "lucide-react";
import { AppFooter } from "@/components/AppFooter";
import { PromoBar } from "@/components/PromoBar";
import { PublicHeader } from "@/components/PublicHeader";
import { DocumentMeta } from "@/components/DocumentMeta";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/primitives";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useVendorStatus, useVendorStatusTrend,
  type VendorStatusRow, type VendorStatusTrendPoint,
} from "@/api/hooks";
import { cn } from "@/lib/utils";

/** 上游状态页 · /status · 公开可查
 *
 *  设计目标（震撼 + 直观）：
 *   1. 顶部 · 4 个大数字总览（累计发出的 keys / vendor 数 / 平均寿命 / 最新开号）
 *   2. 6 家 vendor · 每家一条 24h 节奏条（横向 timeline · 一眼看得到"这家忙不忙"）
 *   3. 全网时间线 · 跨 vendor 最近开号事件（"这个平台每分钟都在响应"）
 *
 *  数据来源（严格脱敏）：
 *   - vendor_probe · 我方每 60s 探针
 *   - vendor_dispatch · vendor 侧真开号历史 + webhookin 实时事件
 *   - 匿名规则：AWS-Q Kiro Vendor 01..06 · 真名只登录 wholesale 档才见 */
export default function StatusPage() {
  const { t } = useTranslation("status");
  const { data, isLoading } = useVendorStatus();
  const vendors = data?.vendors ?? [];
  const noData = !isLoading && vendors.length === 0;

  // 总览数字 · 前端聚合 · 后端不用再改
  const overview = useMemo(() => {
    const totalKeys = vendors.reduce((s, v) => s + (v.dispatch?.total_keys_dispatched ?? 0), 0);
    const totalBatches = vendors.reduce((s, v) => s + (v.dispatch?.total_batches ?? 0), 0);
    const aliveCount = vendors.filter(v => v.alive).length;
    // avg interval · 只算有 dispatch 数据的 vendor
    const intervals = vendors
      .map(v => v.dispatch?.avg_interval_min)
      .filter((x): x is number => typeof x === "number" && x > 0);
    const avgInterval = intervals.length
      ? intervals.reduce((s, x) => s + x, 0) / intervals.length
      : 0;
    // 最新一批时间 · 6 家最新的
    const latestTs = vendors
      .map(v => v.dispatch?.last_dispatch_at)
      .filter((x): x is string => !!x)
      .sort()
      .pop();
    return { totalKeys, totalBatches, aliveCount, avgInterval, latestTs };
  }, [vendors]);

  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <DocumentMeta titleKey="status:meta.title" descriptionKey="status:meta.description" />
      <PromoBar />
      <PublicHeader />

      <main className="flex-1">
        {/* Hero + 大数字总览 · 撑满宽 */}
        <section className="border-b border-hairline bg-gradient-to-b from-brand-subtle/20 to-transparent">
          <div className="page-container py-14 lg:py-16">
            <div className="mx-auto max-w-6xl space-y-8">
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <span className="relative flex size-2">
                    <span className="absolute inline-flex size-full animate-ping rounded-full bg-ok-solid opacity-75" />
                    <span className="relative inline-flex size-2 rounded-full bg-ok-solid" />
                  </span>
                  <span className="font-mono text-label font-semibold uppercase tracking-widest text-ok-fg">
                    {t("hero.eyebrow")}
                  </span>
                </div>
                <h1 className="text-hero font-semibold tracking-tight md:text-5xl">
                  {t("hero.title")}
                </h1>
                <p className="max-w-[65ch] leading-relaxed text-fg-secondary">
                  {t("hero.subtitle")}
                </p>
              </div>

              {/* 4 大数字 · 撑满 · 数字要大 */}
              {isLoading ? (
                <div className="grid grid-cols-2 gap-6 md:grid-cols-4">
                  {Array.from({ length: 4 }).map((_, i) => (
                    <Skeleton key={i} className="h-24 w-full" />
                  ))}
                </div>
              ) : (
                <div className="grid grid-cols-2 gap-6 md:grid-cols-4">
                  <BigStat
                    icon={Zap}
                    value={fmtCompact(overview.totalKeys)}
                    label={t("overview.total-keys")}
                    tone="brand"
                  />
                  <BigStat
                    icon={Activity}
                    value={`${overview.aliveCount} / ${vendors.length || 6}`}
                    label={t("overview.vendors-live")}
                    tone={overview.aliveCount === vendors.length ? "ok" : "warn"}
                  />
                  <BigStat
                    icon={Clock}
                    value={overview.avgInterval > 0 ? fmtInterval(overview.avgInterval) : "-"}
                    label={t("overview.avg-interval")}
                  />
                  <BigStat
                    icon={Shield}
                    value={overview.latestTs ? fmtRelative(overview.latestTs) : "-"}
                    label={t("overview.latest-batch")}
                  />
                </div>
              )}
            </div>
          </div>
        </section>

        {/* 6 家 vendor · 24h 节奏条 */}
        <section className="page-container py-14">
          <div className="mx-auto max-w-6xl space-y-6">
            <div className="flex items-baseline justify-between">
              <h2 className="text-2xl font-semibold tracking-tight">
                {t("fleet-timeline.title")}
              </h2>
              <p className="text-label text-fg-tertiary">
                {t("fleet-timeline.subtitle")}
              </p>
            </div>

            {isLoading ? (
              <div className="space-y-3">
                {Array.from({ length: 6 }).map((_, i) => (
                  <Skeleton key={i} className="h-16 w-full" />
                ))}
              </div>
            ) : noData ? (
              <Card className="p-8 text-center">
                <p className="text-fg-secondary">{t("hero.no-data")}</p>
              </Card>
            ) : (
              <div className="rounded-2xl border border-hairline bg-surface">
                {vendors.map((v, i) => (
                  <VendorTimelineRow
                    key={v.anon_id}
                    vendor={v}
                    isLast={i === vendors.length - 1}
                  />
                ))}
              </div>
            )}
          </div>
        </section>

        {/* CTA · 分两块 */}
        <section className="page-container pb-16">
          <div className="mx-auto grid max-w-6xl gap-4 md:grid-cols-2">
            <Card className="flex flex-col justify-between gap-4 p-6">
              <div className="space-y-2">
                <h3 className="text-body-lg font-semibold">
                  {t("cta.prices.title")}
                </h3>
                <p className="text-label leading-relaxed text-fg-secondary">
                  {t("cta.prices.body")}
                </p>
              </div>
              <Button asChild variant="ghost" className="self-start">
                <Link to="/prices">
                  {t("cta.prices.action")}
                  <ArrowRight />
                </Link>
              </Button>
            </Card>
            <Card className="flex flex-col justify-between gap-4 p-6 border-brand-hairline bg-brand-subtle/30">
              <div className="space-y-2">
                <h3 className="text-body-lg font-semibold">
                  {t("cta.join.title")}
                </h3>
                <p className="text-label leading-relaxed text-fg-secondary">
                  {t("cta.join.body")}
                </p>
              </div>
              <Button asChild variant="brand" className="self-start">
                <Link to="/register">
                  {t("cta.join.action")}
                  <ArrowRight />
                </Link>
              </Button>
            </Card>
          </div>
        </section>
      </main>

      <AppFooter />
    </div>
  );
}

/** 顶部大数字 · icon + 大字 + 小 label */
function BigStat({
  icon: Icon, value, label, tone = "neutral",
}: {
  icon: React.ComponentType<{ className?: string }>;
  value: string;
  label: string;
  tone?: "neutral" | "ok" | "warn" | "brand";
}) {
  const toneClass =
    tone === "ok"    ? "text-ok-fg"     :
    tone === "warn"  ? "text-warn-fg"   :
    tone === "brand" ? "text-brand-fg"  :
                       "text-fg";
  const iconTone =
    tone === "ok"    ? "text-ok-solid"    :
    tone === "warn"  ? "text-warn-solid"  :
    tone === "brand" ? "text-brand-solid" :
                       "text-fg-tertiary";
  return (
    <div className="space-y-1.5">
      <Icon className={cn("size-5", iconTone)} />
      <div className={cn("font-mono text-4xl font-semibold tracking-tight tnum md:text-5xl", toneClass)}>
        {value}
      </div>
      <div className="text-label text-fg-tertiary">{label}</div>
    </div>
  );
}

/** 单家 vendor 一行 · label + 24h 节奏条（backfill 双色柱 or probe 折线）+ 累计 chip */
function VendorTimelineRow({
  vendor, isLast,
}: {
  vendor: VendorStatusRow;
  isLast: boolean;
}) {
  const { t } = useTranslation("status");
  const disp = vendor.dispatch;
  const batches = disp?.total_batches ?? 0;
  const keys = disp?.total_keys_dispatched ?? 0;
  const avgInterval = disp?.avg_interval_min ?? 0;

  return (
    <div
      className={cn(
        "grid grid-cols-[auto_1fr_auto] items-center gap-4 p-4 md:gap-6 md:p-5",
        !isLast && "border-b border-hairline",
      )}
    >
      {/* 左 · label + 状态点 */}
      <div className="flex items-center gap-2.5 md:min-w-[180px]">
        <span className={cn(
          "size-2 rounded-full",
          vendor.alive ? "bg-ok-solid" : "bg-danger-solid",
        )} />
        <div>
          <div className="font-medium">{vendor.anon_label}</div>
          <div className="text-[11px] text-fg-tertiary">
            {vendor.uptime_24h_pct !== undefined
              ? t("row.uptime", { pct: vendor.uptime_24h_pct })
              : t("row.uptime-unknown")}
          </div>
        </div>
      </div>

      {/* 中 · 24h 节奏条 */}
      <div className="min-w-0">
        <TimelineBar anonID={vendor.anon_id} />
      </div>

      {/* 右 · 累计数字 */}
      <div className="text-right md:min-w-[140px]">
        {batches > 0 ? (
          <>
            <div className="font-mono text-xl font-semibold tabular-nums">
              {fmtCompact(keys)}
            </div>
            <div className="text-[11px] text-fg-tertiary">
              {t("row.batches", { count: batches })}
              {avgInterval > 0 && ` · ${fmtInterval(avgInterval)}`}
            </div>
          </>
        ) : (
          <div className="text-[11px] text-fg-tertiary">
            {t("row.warming-up")}
          </div>
        )}
      </div>
    </div>
  );
}

/** 节奏条 · 24h · 优先 backfill 双色柱 · fallback probe uptime · 都无就静默 */
function TimelineBar({ anonID }: { anonID: string }) {
  const { data, isLoading } = useVendorStatusTrend(anonID, "24h");
  if (isLoading) return <Skeleton className="h-10 w-full" />;

  const points = data?.points ?? [];
  const source = data?.source;
  if (points.length < 2 || source === "empty") {
    return <div className="h-10" />; // 占位·不显示"数据不足"·避免打扰
  }

  if (source === "backfill") {
    return <BackfillBars points={points} />;
  }
  return <ProbeLine points={points} />;
}

/** Backfill 双色柱 · born 绿 · died 灰 · 每小时一根 */
function BackfillBars({ points }: { points: VendorStatusTrendPoint[] }) {
  const born = points.map(p => p.keys_born ?? 0);
  const died = points.map(p => p.keys_died ?? 0);
  const maxVal = Math.max(1, ...born, ...died);
  const W = 720, H = 40, PAD = 2;
  const availW = W - 2 * PAD;
  const availH = H - 2 * PAD;
  const barW = Math.max(1, availW / points.length / 2 - 0.4);

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      className="w-full"
      preserveAspectRatio="none"
      style={{ height: 40 }}
    >
      <line x1={PAD} y1={H - PAD} x2={W - PAD} y2={H - PAD}
        className="stroke-hairline" strokeWidth={0.5} />
      {points.map((p, i) => {
        const cx = PAD + (availW * (i + 0.5)) / points.length;
        const bornH = (p.keys_born ?? 0) / maxVal * availH;
        const diedH = (p.keys_died ?? 0) / maxVal * availH;
        return (
          <g key={p.t}>
            {bornH > 0 && (
              <rect x={cx - barW - 0.2} y={H - PAD - bornH}
                width={barW} height={bornH}
                className="fill-ok-solid" />
            )}
            {diedH > 0 && (
              <rect x={cx + 0.2} y={H - PAD - diedH}
                width={barW} height={diedH}
                className="fill-fg-tertiary" opacity={0.5} />
            )}
          </g>
        );
      })}
    </svg>
  );
}

/** Probe uptime 折线 · vendor 没 backfill 数据时用 */
function ProbeLine({ points }: { points: VendorStatusTrendPoint[] }) {
  const valid = points.filter(p => p.uptime_pct !== undefined);
  if (valid.length < 2) return <div className="h-10" />;

  const W = 720, H = 40, PAD = 2;
  const first = new Date(valid[0].t).getTime();
  const last = new Date(valid[valid.length - 1].t).getTime();
  const span = Math.max(1, last - first);
  const xFor = (tt: string) =>
    PAD + ((new Date(tt).getTime() - first) / span) * (W - 2 * PAD);
  const yFor = (pct: number) => H - PAD - (pct / 100) * (H - 2 * PAD);

  const d = valid.map((p, i) =>
    `${i === 0 ? "M" : "L"}${xFor(p.t).toFixed(1)},${yFor(p.uptime_pct!).toFixed(1)}`
  ).join(" ");
  const avg = valid.reduce((s, p) => s + (p.uptime_pct ?? 0), 0) / valid.length;
  const toneClass =
    avg >= 99 ? "text-ok-solid" : avg >= 95 ? "text-warn-solid" : "text-danger-solid";

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      className={cn("w-full", toneClass)}
      preserveAspectRatio="none"
      style={{ height: 40 }}
    >
      <line x1={PAD} y1={yFor(100)} x2={W - PAD} y2={yFor(100)}
        className="stroke-hairline" strokeWidth={0.5} strokeDasharray="2 2" />
      <path d={d} fill="none" stroke="currentColor" strokeWidth={1.75}
        strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

// —— 格式化工具 ——

/** 数字紧凑显示 · 1234 → 1.2K · 15600 → 15.6K · 1234567 → 1.2M */
function fmtCompact(n: number): string {
  if (!n) return "0";
  if (n < 1000) return String(n);
  if (n < 10000) return (n / 1000).toFixed(1) + "K";
  if (n < 1_000_000) return Math.round(n / 1000) + "K";
  return (n / 1_000_000).toFixed(1) + "M";
}

/** 间隔分钟数 → "23min" / "1.5h" / "2d" */
function fmtInterval(min: number): string {
  if (min < 60) return `${Math.round(min)}min`;
  if (min < 60 * 24) return `${(min / 60).toFixed(1)}h`;
  return `${Math.round(min / 60 / 24)}d`;
}

/** ISO 时间 → 相对时间 "刚刚" / "5min 前" / "2h 前" · i18n 只处理单位·数字前端算 */
function fmtRelative(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const min = Math.floor(diff / 60000);
  if (min < 1) return "刚刚";
  if (min < 60) return `${min}min 前`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h}h 前`;
  return `${Math.floor(h / 24)}d 前`;
}
