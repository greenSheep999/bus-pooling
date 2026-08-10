import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { ArrowRight } from "lucide-react";
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

/** 上游状态页 · /status · 公开可查
 *
 *  数据来源（脱敏后）：
 *   - vendor_probe · 每 60s 探针（我方视角 · 存活 + 库存）
 *   - vendor_dispatch · 平台开号历史（vendor 官方 fleet-wide gen-logs · 无端点时从探针增量推）
 *   - vendor_daily · 事件日聚合（7 天）
 *
 *  展示口径：每家一张卡 · 三列讲一个故事
 *   1. 能不能用（当下） · 在线 + 库存档位 + 覆盖区域 + 质保
 *   2. 有多稳（近 24h） · uptime% + stockout + 7 天事件 + 存活曲线
 *   3. 多可靠（历史）  · 开号批数 + 累计发号 + 平均间隔 + 平均寿命 + 节奏图
 *
 *  严格脱敏：无价格 · 无内部 vendor id · 无平均寿命秒数（只有档位） */
export default function StatusPage() {
  const { t, i18n } = useTranslation("status");
  const { data, isLoading } = useVendorStatus();
  const vendors = data?.vendors ?? [];
  const noData = !isLoading && vendors.length === 0;

  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <DocumentMeta titleKey="status:meta.title" descriptionKey="status:meta.description" />
      <PromoBar />
      <PublicHeader />

      <main className="flex-1">
        <section className="page-container py-14 lg:py-20">
          <div className="mx-auto max-w-5xl space-y-10">
            {/* Hero */}
            <div className="space-y-4">
              <span className="font-mono text-label font-semibold uppercase tracking-widest text-fg-tertiary">
                {t("hero.eyebrow")}
              </span>
              <h1 className="text-hero font-semibold tracking-tight">
                {t("hero.title")}
              </h1>
              <p className="max-w-[65ch] leading-relaxed text-fg-secondary">
                {t("hero.subtitle")}
              </p>
              {data?.probed_at && (
                <p className="text-label text-fg-tertiary">
                  {t("hero.probed-at", {
                    time: new Date(data.probed_at).toLocaleString(i18n.language),
                  })}
                </p>
              )}
            </div>

            {isLoading ? (
              <div className="space-y-4">
                {Array.from({ length: 6 }).map((_, i) => (
                  <Skeleton key={i} className="h-56 w-full" />
                ))}
              </div>
            ) : noData ? (
              <Card className="p-8 text-center">
                <p className="text-fg-secondary">{t("hero.no-data")}</p>
              </Card>
            ) : (
              <div className="space-y-4">
                {vendors.map((v) => (
                  <VendorCard key={v.anon_id} vendor={v} />
                ))}
              </div>
            )}

            {/* CTA · 两块 · 想看价格 → prices · 想试试拼车 → register */}
            <div className="grid gap-4 md:grid-cols-2">
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
          </div>
        </section>
      </main>

      <AppFooter />
    </div>
  );
}

/** 单家 vendor 卡 · 3 列讲 3 件事 · 关键设计:
 *   - 顶行 = 匿名 label + 在线/掉线 chip · 一眼看得到"能不能用"
 *   - 3 列都是"事实一句 + 支撑一句" · 无需解读
 *   - 桌面横排 · 移动竖排 · 每列有独立的 chart/sparkline */
function VendorCard({ vendor }: { vendor: VendorStatusRow }) {
  const { t } = useTranslation("status");

  return (
    <Card className="p-5 md:p-6">
      {/* 头 · label + 总体状态 */}
      <div className="mb-4 flex items-center justify-between gap-3">
        <h3 className="min-w-0 truncate text-body-lg font-semibold">
          {vendor.anon_label}
        </h3>
        <Chip tone={vendor.alive ? "ok" : "danger"} dot>
          {vendor.alive ? t("three-things.avail.yes") : t("three-things.avail.no")}
        </Chip>
      </div>

      <div className="grid gap-5 md:grid-cols-3 md:gap-6">
        <AvailColumn v={vendor} />
        <StableColumn v={vendor} />
        <ReliableColumn v={vendor} />
      </div>
    </Card>
  );
}

/** 列 1 · 能不能用 · 库存档位 + 区域数 + 质保 */
function AvailColumn({ v }: { v: VendorStatusRow }) {
  const { t } = useTranslation("status");

  const stockLine =
    v.stock_bucket === "many" ? t("three-things.avail.stock-many") :
    v.stock_bucket === "low"  ? t("three-things.avail.stock-low")  :
    v.stock_bucket === "out"  ? t("three-things.avail.stock-out")  :
                                t("three-things.avail.stock-unknown");
  const stockTone: "ok" | "warn" | "danger" | "neutral" =
    v.stock_bucket === "many" ? "ok"  :
    v.stock_bucket === "low"  ? "warn":
    v.stock_bucket === "out"  ? "danger":
                                "neutral";

  const regionLine =
    v.region_count > 0
      ? t("three-things.avail.region-count", { count: v.region_count })
      : t("three-things.avail.no-region");

  const warrantyLine =
    v.has_warranty && v.warranty_minutes
      ? t("three-things.avail.warranty-yes", { minutes: v.warranty_minutes })
      : t("three-things.avail.warranty-no");

  return (
    <ColumnShell label={t("three-things.avail.label")}>
      <div className="space-y-2">
        <div>
          <Chip tone={stockTone}>{stockLine}</Chip>
        </div>
        <p className="text-label text-fg-secondary">{regionLine}</p>
        <p className="text-label text-fg-secondary">{warrantyLine}</p>
      </div>
    </ColumnShell>
  );
}

/** 列 2 · 有多稳 · uptime% + stockout + 事件 + 24h 曲线 */
function StableColumn({ v }: { v: VendorStatusRow }) {
  const { t } = useTranslation("status");

  const uptimeVal =
    v.uptime_24h_pct !== undefined
      ? t("three-things.stable.uptime-value", { pct: v.uptime_24h_pct })
      : t("three-things.stable.uptime-unknown");
  const uptimeToneClass =
    v.uptime_24h_pct === undefined ? "text-fg-tertiary" :
    v.uptime_24h_pct >= 99         ? "text-ok-fg"       :
    v.uptime_24h_pct >= 95         ? "text-warn-fg"     :
                                     "text-danger-fg";

  const stockoutLine =
    !v.stockout_24h_minutes || v.stockout_24h_minutes === 0
      ? t("three-things.stable.stockout-none")
      : t("three-things.stable.stockout-value", { minutes: v.stockout_24h_minutes });

  const incidentsLine =
    v.incidents_7d && v.incidents_7d.length > 0
      ? t("three-things.stable.incidents-value", { count: v.incidents_7d.length })
      : t("three-things.stable.incidents-none");

  return (
    <ColumnShell label={t("three-things.stable.label")}>
      <div className="space-y-2">
        <div className={cn("font-mono text-xl font-semibold tnum", uptimeToneClass)}>
          {uptimeVal}
        </div>
        <p className="text-label text-fg-secondary">{stockoutLine}</p>
        <p className="text-label text-fg-secondary">{incidentsLine}</p>
        <div className="pt-1">
          <UptimeSparkline anonID={v.anon_id} />
        </div>
      </div>
    </ColumnShell>
  );
}

/** 列 3 · 多可靠 · 开号批数 + 累计 key + 平均间隔 + 平均寿命 + 节奏图 */
function ReliableColumn({ v }: { v: VendorStatusRow }) {
  const { t } = useTranslation("status");
  const d = v.dispatch;
  const h = v.history;

  const lifeLine =
    v.lifespan_bucket === "long"  ? t("three-things.reliable.lifespan-long") :
    v.lifespan_bucket === "mid"   ? t("three-things.reliable.lifespan-mid")  :
    v.lifespan_bucket === "short" ? t("three-things.reliable.lifespan-short"):
                                    t("three-things.reliable.lifespan-unknown");

  return (
    <ColumnShell label={t("three-things.reliable.label")}>
      <div className="space-y-2">
        {d && d.total_batches > 0 ? (
          <>
            <div className="font-mono text-xl font-semibold tnum">
              {t("three-things.reliable.batches-value", { count: d.total_batches })}
            </div>
            <p className="text-label text-fg-secondary">
              {t("three-things.reliable.keys-value", { count: d.total_keys_dispatched })}
            </p>
            {d.avg_interval_min !== undefined && d.avg_interval_min > 0 && (
              <p className="text-label text-fg-secondary">
                {t("three-things.reliable.interval-value", {
                  minutes: Math.round(d.avg_interval_min),
                })}
              </p>
            )}
            {d.last_dispatch_at && (
              <p className="text-label text-fg-tertiary">
                <RelativeWhen iso={d.last_dispatch_at} />
              </p>
            )}
          </>
        ) : (
          <p className="text-label text-fg-tertiary">
            {t("three-things.reliable.no-dispatch")}
          </p>
        )}
        <p className="text-label text-fg-secondary">{lifeLine}</p>
        {h && h.total_keys > 0 && (
          <div className="pt-1">
            <ReliableSparkline anonID={v.anon_id} />
          </div>
        )}
      </div>
    </ColumnShell>
  );
}

/** 列骨架 · 一致的 label 样式 + 内容 slot */
function ColumnShell({
  label, children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-2">
      <div className="text-[10px] font-semibold uppercase tracking-wider text-fg-tertiary">
        {label}
      </div>
      {children}
    </div>
  );
}

/** 相对时间 · "刚刚 / X 分钟前 / X 小时前 / X 天前" */
function RelativeWhen({ iso }: { iso: string }) {
  const { t } = useTranslation("status");
  const diff = Date.now() - new Date(iso).getTime();
  const min = Math.floor(diff / 60000);
  let when: string;
  if (min < 2)             when = t("three-things.just-now");
  else if (min < 60)       when = t("three-things.mins-ago",  { count: min });
  else if (min < 60 * 24)  when = t("three-things.hours-ago", { count: Math.floor(min / 60) });
  else                     when = t("three-things.days-ago",  { count: Math.floor(min / 60 / 24) });
  return <>{t("three-things.reliable.last-value", { when })}</>;
}

/** 24h 稳定度 sparkline · 用探针数据 · 折线 · 数据不足时静默 */
function UptimeSparkline({ anonID }: { anonID: string }) {
  const { t } = useTranslation("status");
  const { data, isLoading } = useVendorStatusTrend(anonID, "24h");
  if (isLoading) return <Skeleton className="h-8 w-full" />;
  const points = (data?.points ?? []).filter(p => p.uptime_pct !== undefined);
  if (points.length < 2) return null;

  return (
    <div>
      <ProbeSparkline points={points} />
      <p className="mt-1 text-[10px] text-fg-tertiary">
        {t("three-things.stable.sparkline-caption")}
      </p>
    </div>
  );
}

/** 开号节奏 sparkline · 用 backfill · 双色柱 · 数据不足时静默 */
function ReliableSparkline({ anonID }: { anonID: string }) {
  const { t } = useTranslation("status");
  const { data, isLoading } = useVendorStatusTrend(anonID, "24h");
  if (isLoading) return <Skeleton className="h-8 w-full" />;
  const points = data?.points ?? [];
  if (points.length < 2 || data?.source === "empty") return null;

  return (
    <div>
      <BackfillMiniBars points={points} />
      <p className="mt-1 text-[10px] text-fg-tertiary">
        {t("three-things.reliable.sparkline-caption")}
      </p>
    </div>
  );
}

/** 探针 uptime 折线 · SVG · 无第三方库 */
function ProbeSparkline({ points }: { points: VendorStatusTrendPoint[] }) {
  const { t } = useTranslation("status");
  const W = 260, H = 32, PAD = 2;
  const first = new Date(points[0].t).getTime();
  const last = new Date(points[points.length - 1].t).getTime();
  const span = Math.max(1, last - first);
  const xFor = (tt: string) =>
    PAD + ((new Date(tt).getTime() - first) / span) * (W - 2 * PAD);
  const yFor = (pct: number) => H - PAD - (pct / 100) * (H - 2 * PAD);

  const d = points
    .map((p, i) => `${i === 0 ? "M" : "L"}${xFor(p.t).toFixed(1)},${yFor(p.uptime_pct!).toFixed(1)}`)
    .join(" ");
  const avg = points.reduce((s, p) => s + (p.uptime_pct ?? 0), 0) / points.length;
  const toneClass =
    avg >= 99 ? "text-ok-solid" : avg >= 95 ? "text-warn-solid" : "text-danger-solid";

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      className={cn("w-full", toneClass)}
      preserveAspectRatio="none"
      aria-label={t("sparkline.aria")}
      style={{ height: 32 }}
    >
      <line x1={PAD} y1={yFor(100)} x2={W - PAD} y2={yFor(100)}
        className="stroke-hairline" strokeWidth={0.5} strokeDasharray="2 2" />
      <path d={d} fill="none" stroke="currentColor" strokeWidth={1.5}
        strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

/** Backfill 双色柱 · born 绿 · died 灰 · 极简版（每小时一根） */
function BackfillMiniBars({ points }: { points: VendorStatusTrendPoint[] }) {
  const { t } = useTranslation("status");
  const born = points.map(p => p.keys_born ?? 0);
  const died = points.map(p => p.keys_died ?? 0);
  const maxVal = Math.max(1, ...born, ...died);
  const W = 260, H = 32, PAD_X = 2, PAD_Y = 2;
  const availW = W - 2 * PAD_X;
  const availH = H - 2 * PAD_Y;
  const barW = Math.max(1.5, availW / points.length / 2 - 0.5);

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      className="w-full"
      preserveAspectRatio="none"
      aria-label={t("sparkline.aria")}
      style={{ height: 32 }}
    >
      <line x1={PAD_X} y1={H - PAD_Y} x2={W - PAD_X} y2={H - PAD_Y}
        className="stroke-hairline" strokeWidth={0.5} />
      {points.map((p, i) => {
        const cx = PAD_X + (availW * (i + 0.5)) / points.length;
        const bornH = (p.keys_born ?? 0) / maxVal * availH;
        const diedH = (p.keys_died ?? 0) / maxVal * availH;
        return (
          <g key={p.t}>
            {bornH > 0 && (
              <rect x={cx - barW - 0.3} y={H - PAD_Y - bornH}
                width={barW} height={bornH} className="fill-ok-solid" />
            )}
            {diedH > 0 && (
              <rect x={cx + 0.3} y={H - PAD_Y - diedH}
                width={barW} height={diedH} className="fill-fg-tertiary" opacity={0.6} />
            )}
          </g>
        );
      })}
    </svg>
  );
}
