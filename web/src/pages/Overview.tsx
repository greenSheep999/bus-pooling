import { lazy, Suspense, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Activity as ActivityIcon, Check, ChevronDown, KeyRound, Send,
  TrendingDown, Users, Wallet,
} from "lucide-react";
import {
  useActivities, useMe, useOverview, useStock, useTrend, useVendorStats,
} from "@/api/hooks";
import { KpiCard } from "@/components/KpiCard";
import { TrendLegend } from "@/components/TrendLegend";
import { EmptyState } from "@/components/ui/empty-state";
import {
  SkeletonChart, SkeletonKpi, SkeletonRows, SkeletonTable,
} from "@/components/ui/skeleton";

// TrendChart 走 recharts · 图表出现的位置比首屏靠下 · lazy 一起放到 recharts chunk
const TrendChart = lazy(() => import("@/components/TrendChart"));

// 图表包（recharts）400+KB · 拆到自己的 chunk · Overview 里图表位置一小块 · fallback 空 div 即可
const VendorSharePie = lazy(() => import("@/components/VendorSharePie"));
import { ActivityRow } from "@/components/rows";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, Label, Meter, Muted, SectionHead, Segmented, Stat,
} from "@/components/ui/primitives";
import { MicroStat, OwnerBadge } from "@/components/ui/tags";
import { LoadMoreButton } from "@/components/ui/load-more-button";
import {
  Popover, PopoverContent, PopoverItem, PopoverSectionLabel, PopoverSeparator, PopoverTrigger,
} from "@/components/ui/popover";
import {
  cn, fmtCredits, fmtDelta, fmtK, fmtLifespan, toCredits, vendorColor, vendorLabel,
} from "@/lib/utils";
import type { Activity, Destination, TimeRange, TrendMetric } from "@/types";

/** 小字里嵌数字 · 就是 <Em> 加上"带正负号时上语义色"（+绿 / -红）
 *  样式本体在 primitives 的 Em 里 —— 别在这里另写一套 */
function Num({
  children,
  sign = "",
}: { children: React.ReactNode; sign?: "+" | "-" | "" }) {
  return (
    <Em tone={sign === "+" ? "ok" : sign === "-" ? "spend" : undefined}>
      {children}
    </Em>
  );
}

/* 号池状态 pill · 呼吸绿点 = 心跳（system live）· 告急/停运 用静态实心点
   阈值：0 = 停运 · <20 = 告急 · 其他 = 正常。跟 header 库存徽标共用 useStock */
function PoolStatus() {
  const { t } = useTranslation("overview");
  const { data } = useStock();
  const n = data?.total_available;

  const state =
    n === undefined ? "loading"
      : n === 0 ? "down"
        : n < 20 ? "warn"
          : "ok";

  const cfg = {
    loading: { dot: "bg-hairline", label: t("pool_status.loading") },
    ok: { dot: "bg-ok-solid animate-breath", label: t("pool_status.ok") },
    warn: { dot: "bg-warn-solid", label: t("pool_status.warn") },
    down: { dot: "bg-danger-solid", label: t("pool_status.down") },
  }[state];

  return (
    <div className="flex items-center gap-1.5">
      <span className={cn("size-2 shrink-0 rounded-full", cfg.dot)} />
      <span className="font-medium text-fg-secondary">{cfg.label}</span>
    </div>
  );
}

const RANGE_KEYS: { value: TimeRange; labelKey: string }[] = [
  { value: "today", labelKey: "range.today" },
  { value: "7d", labelKey: "range.d7" },
  { value: "30d", labelKey: "range.d30" },
  { value: "90d", labelKey: "range.d90" },
  { value: "all", labelKey: "range.all" },
];

const METRIC_KEYS: { value: TrendMetric; labelKey: string }[] = [
  { value: "credits", labelKey: "metric.credits" },
  { value: "pulls", labelKey: "metric.pulls" },
  { value: "lifespan", labelKey: "metric.lifespan" },
];

const DEST_LABEL_KEY: Record<Destination, string> = {
  pending: "dest.pending",
  into_bus: "dest.into_bus",
  push_pool: "dest.push_pool",
  handoff: "dest.handoff",
};

const DEST_COLOR: Record<Destination, string> = {
  pending: "#C9A9FF",
  into_bus: "#9147FF",
  push_pool: "#6420C7",
  handoff: "#D4D4D8",
};

/* Trend scope · "全部"（默认）/ 单车 / 单 vendor —— 二选一
   为什么不做去向 scope：见对话里的推导（handoff 号已离开系统、寿命跟去向无关，
   去向应该是"当前视图内的堆叠模式"而不是 scope） */
type Scope =
  | { kind: "all" }
  | { kind: "bus"; id: string; name: string }
  | { kind: "vendor"; id: string; name: string };

function ScopePicker({
  value,
  onChange,
  buses,
  vendors,
}: {
  value: Scope;
  onChange: (s: Scope) => void;
  buses: { id: string; name: string }[];
  vendors: { id: string; name: string }[];
}) {
  const { t } = useTranslation("overview");
  const [open, setOpen] = useState(false);

  /* 全部 trigger 显示"全部（N 车 · M vendor）"—— 光写"全部"用户不知道全部什么 */
  const label =
    value.kind === "all"
      ? t("scope.trigger_all", { buses: buses.length, vendors: vendors.length })
      : value.kind === "bus" ? t("scope.trigger_bus", { name: value.name })
        : t("scope.trigger_vendor", { name: value.name });

  const pick = (s: Scope) => { onChange(s); setOpen(false); };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          className={cn(
            "flex min-w-[200px] items-center justify-between gap-2 rounded-xl border border-hairline bg-bg px-3 py-1.5 font-medium shadow-card transition-colors hover:bg-bg-elevated",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30",
            open && "bg-bg-elevated",
          )}
        >
          <span className="truncate text-fg-secondary">{label}</span>
          <ChevronDown className="size-3.5 shrink-0 text-fg-tertiary" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-64">
        <ScopeOption picked={value.kind === "all"} onPick={() => pick({ kind: "all" })}>
          <div className="flex flex-col items-start">
            <span>{t("scope.all_title")}</span>
            <span className="text-[11px] font-normal text-fg-tertiary">
              {t("scope.all_sub", { buses: buses.length, vendors: vendors.length })}
            </span>
          </div>
        </ScopeOption>

        <PopoverSeparator />
        <PopoverSectionLabel>{t("scope.section_bus")}</PopoverSectionLabel>
        {buses.map((b) => (
          <ScopeOption
            key={b.id}
            picked={value.kind === "bus" && value.id === b.id}
            onPick={() => pick({ kind: "bus", id: b.id, name: b.name })}
          >
            {b.name}
          </ScopeOption>
        ))}

        <PopoverSeparator />
        <PopoverSectionLabel>{t("scope.section_vendor")}</PopoverSectionLabel>
        {vendors.map((v) => (
          <ScopeOption
            key={v.id}
            picked={value.kind === "vendor" && value.id === v.id}
            onPick={() => pick({ kind: "vendor", id: v.id, name: v.name })}
          >
            {v.name}
          </ScopeOption>
        ))}
      </PopoverContent>
    </Popover>
  );
}

function ScopeOption({
  picked, onPick, children,
}: { picked: boolean; onPick: () => void; children: React.ReactNode }) {
  return (
    <PopoverItem onSelect={onPick}>
      <span className="min-w-0 flex-1 truncate font-medium">{children}</span>
      <Check className={cn("size-3.5 shrink-0", picked ? "text-brand-strong" : "invisible")} />
    </PopoverItem>
  );
}

/* 活动记录 · 无详情页 · 首屏 N 条，"加载更多"渐进展开
   不做右上角"全部→"入口（没落地页），只用底部按钮控制显示条数 */
const ACT_STEP = 8;

function ActivityFeed({
  items, total, loading,
}: {
  items: Activity[];
  total: number;
  loading?: boolean;
}) {
  const { t } = useTranslation("overview");
  const [shown, setShown] = useState(ACT_STEP);
  const visible = items.slice(0, shown);
  const remain = Math.max(0, items.length - shown);

  return (
    <div className="space-y-5">
      <SectionHead
        title={t("activity.title")}
        sub={t("activity.sub", { total })}
      />
      {loading && items.length === 0 ? (
        <SkeletonRows rows={4} />
      ) : items.length === 0 ? (
        <EmptyState
          icon={ActivityIcon}
          title={t("activity.empty_title")}
          desc={t("activity.empty_desc")}
        />
      ) : (
        <>
          {/* 列表容器：窄屏横滚 · 行按内容自然宽度不压缩，badge 不变形 */}
          <div className="overflow-x-auto">
            <div className="min-w-[640px]">
              <BareList>
                {visible.map((a) => (
                  <ActivityRow key={a.id} a={a} />
                ))}
              </BareList>
            </div>
          </div>
          <LoadMoreButton
            onLoadMore={() => setShown((s) => s + ACT_STEP)}
            remain={remain}
          />
        </>
      )}
    </div>
  );
}

/** 动态时钟：每秒 tick，返回本地化字符串（读秒） */
function useNowSecond() {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(id);
  }, []);
  return now;
}

export default function Overview() {
  const { t, i18n } = useTranslation("overview");
  const { data: me } = useMe();
  const [range, setRange] = useState<TimeRange>("30d");
  const [metric, setMetric] = useState<TrendMetric>("credits");
  const [scope, setScope] = useState<Scope>({ kind: "all" });
  const now = useNowSecond();

  const { data: ov, isLoading: ovLoading } = useOverview(range);
  const { data: trend, isLoading: trendLoading } = useTrend(range, metric, {
    busId: scope.kind === "bus" ? scope.id : undefined,
    vendor: scope.kind === "vendor" ? scope.id : undefined,
  });
  const { data: vendors, isLoading: vendorsLoading } = useVendorStats();
  const { data: acts, isLoading: actsLoading } = useActivities(range);

  // 首屏骨架：**只在没有任何数据时**铺（换 range 时保留旧数据 · 不闪成灰块）
  const kpiSkeleton = ovLoading && !ov;

  const kpi = ov?.kpi;
  const totalBusCreds = (ov?.buses.items ?? []).reduce((s, b) => s + b.alive, 0);
  const extractTotal = ov?.extract.total_credentials ?? 0;

  return (
    <div className="space-y-section">
      {/* ── Hero + 全页时间维度 ──
          窄屏（<md）左右两列换行堆叠 · 右列 Segmented 可能超出时给个横滚兜底 */}
      <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div className="min-w-0 space-y-2">
          <h1 className="text-hero font-semibold">{t("hero.title")}</h1>
          <p className="text-fg-tertiary">
            <span className="tnum">
              {now.toLocaleDateString(i18n.language)}{" "}
              {now.toLocaleTimeString(i18n.language, { hour12: false })}
            </span>
            {" · "}
            <Num>{ov?.buses.bus_count ?? 0}</Num> {t("hero.buses_running_suffix")}
            {" · "}
            <Num>{kpi?.alive_count ?? 0}</Num> {t("hero.pool_suffix")}
          </p>
        </div>
        <div className="flex flex-col gap-2 md:items-end">
          <PoolStatus />
          <div className="-mx-1 overflow-x-auto px-1">
            <Segmented
              options={RANGE_KEYS.map((r) => ({ value: r.value, label: t(r.labelKey) }))}
              value={range}
              onChange={setRange}
            />
          </div>
        </div>
      </div>

      {/* ── 4 KPI ── */}
      {kpiSkeleton ? (
        <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
          <SkeletonKpi />
          <SkeletonKpi />
          <SkeletonKpi />
          <SkeletonKpi />
        </div>
      ) : (
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          focal
          tone="credit"
          icon={Wallet}
          label={t("kpi.balance.label")}
          value={kpi ? fmtCredits(kpi.balance) : "-"}
          unit={t("kpi.balance.unit")}
          sub={
            kpi ? (
              <>
                {t("kpi.balance.month_prefix")}{" "}
                <Num sign="+">+{fmtCredits(kpi.balance_delta_topup)}</Num>
                {" · "}
                <Num sign="-">-{fmtCredits(kpi.balance_delta_spend)}</Num>
              </>
            ) : undefined
          }
        />
        <KpiCard
          icon={TrendingDown}
          label={t("kpi.spend_today.label")}
          value={kpi ? fmtCredits(kpi.spend_today) : "-"}
          unit={t("kpi.spend_today.unit")}
          sub={
            kpi ? (
              <>{t("kpi.spend_today.yesterday_prefix")} <Num>{fmtCredits(kpi.spend_yesterday)}</Num></>
            ) : undefined
          }
          subRight={
            kpi ? (() => {
              const s = fmtDelta(kpi.spend_today, kpi.spend_yesterday);
              /* 消费涨了不是好事（红），跌了才是好（绿）· 跟到账/花掉的正负色对调 */
              const sign: "+" | "-" | "" =
                s.startsWith("+") ? "-" : s.startsWith("-") ? "+" : "";
              return <>{t("kpi.spend_today.delta_prefix")} <Num sign={sign}>{s}</Num></>;
            })() : undefined
          }
        />
        <KpiCard
          icon={KeyRound}
          label={t("kpi.pulls.label")}
          value={kpi ? String(kpi.pull_total) : "-"}
          unit={t("kpi.pulls.unit")}
          sub={
            kpi ? (
              <>{t("kpi.pulls.month_prefix")} <Num>{kpi.pull_this_month}</Num> {t("kpi.pulls.month_suffix")}</>
            ) : undefined
          }
        />
        <KpiCard
          icon={ActivityIcon}
          label={t("kpi.alive.label")}
          value={kpi ? String(kpi.alive_count) : "-"}
          unit={t("kpi.alive.unit")}
          sub={
            kpi ? (
              <>
                {t("kpi.alive.dead_prefix")} <Num>{kpi.dead_count}</Num>
                {" · "}
                {t("kpi.alive.pending_prefix")} <Num>{kpi.pending_refill}</Num>
              </>
            ) : undefined
          }
          subRight={
            kpi ? (
              <>{t("kpi.alive.avg_prefix")} <Num>{fmtLifespan(kpi.avg_lifespan_seconds)}</Num></>
            ) : undefined
          }
        />
      </div>
      )}

      {/* ── 3 业务线 ── */}
      <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
        {/* 拼车 */}
        <Card to="/buses" className="flex flex-col gap-4 p-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <span className="grid size-7 place-items-center rounded-lg bg-brand-subtle">
                <Users className="size-3.5 text-brand-strong" />
              </span>
              <h3 className="text-body-lg font-semibold">{t("card.bus.title")}</h3>
            </div>
            <span className="text-label font-semibold text-brand-strong">
              {t("card.view")}
            </span>
          </div>

          <Stat
            value={String(ov?.buses.bus_count ?? 0)}
            unit={t("card.bus.unit", { total: totalBusCreds })}
            size="num"
          />

          <div className="space-y-2.5">
            <Label>{t("card.bus.pool_dist_label")}</Label>
            <div className="flex h-2.5 overflow-hidden rounded-full bg-hairline">
              {(ov?.buses.items ?? []).map((b, i) => (
                <div
                  key={b.id}
                  style={{
                    width: `${(b.alive / Math.max(1, totalBusCreds)) * 100}%`,
                    backgroundColor: ["#9147FF", "#A574FF", "#C9A9FF"][i % 3],
                  }}
                />
              ))}
            </div>
            <div className="space-y-2.5 pt-1">
              {(ov?.buses.items ?? []).map((b, i) => (
                <div key={b.id} className="flex items-center gap-2">
                  <span
                    className="size-[7px] shrink-0 rounded-full"
                    style={{ backgroundColor: ["#9147FF", "#A574FF", "#C9A9FF"][i % 3] }}
                  />
                  <span className="min-w-0 flex-1 truncate font-medium text-fg-secondary">
                    {b.name}
                    {b.role === "owner" && (
                      <span className="ml-1.5"><OwnerBadge /></span>
                    )}
                  </span>
                  <span className="font-semibold tnum">{b.alive} {t("card.bus.row_suffix")}</span>
                </div>
              ))}
            </div>
          </div>

          <div className="mt-auto flex items-center justify-between border-t border-hairline pt-3.5">
            <Muted className="font-medium">
              {t("card.bus.footer_stat", {
                refill: ov?.buses.refill_count ?? 0,
                rate: Math.round((ov?.buses.coalesce_rate ?? 0) * 100),
              })}
            </Muted>
            <span className="font-semibold tnum">
              {kpi ? fmtCredits(-kpi.spend_today, { sign: true }) : "-"}
              <Muted className="ml-1 font-medium">{t("card.bus.credits_unit")}</Muted>
            </span>
          </div>
        </Card>

        {/* 提取 key */}
        <Card to="/extract" className="flex flex-col gap-4 p-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <span className="grid size-7 place-items-center rounded-lg bg-warn-bg">
                <KeyRound className="size-3.5 text-warn-fg" />
              </span>
              <h3 className="text-body-lg font-semibold">{t("card.extract.title")}</h3>
            </div>
            <span className="text-label font-semibold text-brand-strong">
              {t("card.view")}
            </span>
          </div>

          <Stat
            value={String(extractTotal)}
            unit={t("card.extract.unit", { today: ov?.extract.count_today ?? 0 })}
            size="num"
          />

          {/* 去向分布 · handoff 已离开池 · 单独 · 不算进堆栈条(会让 100% 溢出) */}
          <div className="space-y-2.5">
            <Label>{t("card.extract.dest_dist_label")}</Label>
            <div className="flex h-2.5 overflow-hidden rounded-full bg-hairline">
              {(ov?.extract.by_destination ?? [])
                .filter((d) => d.destination !== "handoff")
                .map((d) => (
                  <div
                    key={d.destination}
                    style={{
                      width: `${(d.count / Math.max(1, extractTotal)) * 100}%`,
                      backgroundColor: DEST_COLOR[d.destination],
                    }}
                  />
                ))}
            </div>
            <div className="space-y-2.5 pt-1">
              {(ov?.extract.by_destination ?? []).map((d) => (
                <div key={d.destination} className="flex items-center gap-2">
                  <span
                    className="size-[7px] shrink-0 rounded-full"
                    style={{ backgroundColor: DEST_COLOR[d.destination] }}
                  />
                  <span className="min-w-0 flex-1 truncate font-medium text-fg-secondary">
                    {t(DEST_LABEL_KEY[d.destination])}
                  </span>
                  <span className="font-semibold tnum">{d.count} {t("card.extract.row_suffix")}</span>
                </div>
              ))}
            </div>
          </div>

          <div className="mt-auto flex items-center justify-between border-t border-hairline pt-3.5">
            <Muted className="font-medium">{t("card.extract.footer_pending", { pending: ov?.extract.pending ?? 0 })}</Muted>
            <span className="font-semibold tnum">
              {ov ? fmtCredits(-ov.extract.spend, { sign: true }) : "-"}
              <Muted className="ml-1 font-medium">{t("card.extract.credits_unit")}</Muted>
            </span>
          </div>
        </Card>

        {/* 我的发车（阶段 3） */}
        <Card className="flex flex-col gap-4 bg-bg-elevated p-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <span className="grid size-7 place-items-center rounded-lg bg-hairline">
                <Send className="size-3.5 text-fg-tertiary" />
              </span>
              <h3 className="text-body-lg font-semibold text-fg-tertiary">{t("card.dispatch.title")}</h3>
            </div>
            <Chip tone="brand">{t("card.dispatch.phase_tag")}</Chip>
          </div>

          <Stat value="-" unit={t("card.dispatch.state_unavailable")} size="num" />

          <p className="text-fg-secondary">
            {t("card.dispatch.desc")}
          </p>

          <div className="mt-auto space-y-2.5 border-t border-hairline pt-3.5">
            {[
              t("card.dispatch.row_aws"),
              t("card.dispatch.row_today"),
              t("card.dispatch.row_forward"),
              t("card.dispatch.row_total"),
            ].map((label) => (
              <div key={label} className="flex items-center gap-2">
                <span className="size-[7px] shrink-0 rounded-full bg-hairline" />
                <span className="flex-1 font-medium text-fg-tertiary">{label}</span>
                <span className="font-medium text-fg-tertiary">-</span>
              </div>
            ))}
          </div>
        </Card>
      </div>

      {/* ── 使用趋势 ── 不用 focal：右上角有 Segmented，紫光会被 tab 吃掉 */}
      <Card className="p-7">
        <SectionHead
          title={t("trend.title")}
          sub={
            kpi ? (
              <>
                {t("trend.spend_prefix")} <Num>{fmtCredits(kpi.balance_delta_spend)}</Num> {t("trend.spend_suffix")}
                {" · "}
                {t("trend.pulls_prefix")} <Num>{kpi.pull_this_month}</Num> {t("trend.pulls_suffix")}
                {" · "}
                {t("trend.refill_prefix")} <Num>{ov?.buses.refill_count ?? 0}</Num> {t("trend.refill_suffix")}
              </>
            ) : undefined
          }
          right={
            <div className="flex flex-wrap items-center gap-2">
              <ScopePicker
                value={scope}
                onChange={setScope}
                buses={(ov?.buses.items ?? []).map((b) => ({ id: b.id, name: b.name }))}
                vendors={(vendors?.stats ?? [])
                  .filter((v) => !v.out_of_stock)
                  .map((v) => ({ id: v.vendor_id, name: vendorLabel(v.vendor_id, me?.tier) }))}
              />
              <Segmented
                options={METRIC_KEYS.map((m) => ({ value: m.value, label: t(m.labelKey) }))}
                value={metric}
                onChange={setMetric}
              />
            </div>
          }
        />
        <div className="mt-5">
          {trendLoading && !trend ? (
            <SkeletonChart height={260} bars={12} />
          ) : (
            <Suspense fallback={<SkeletonChart height={260} bars={12} />}>
              <TrendChart data={trend ?? []} metric={metric} />
            </Suspense>
          )}
          <TrendLegend />
        </div>
      </Card>

      {/* ── Vendor 监测 + 占比 ── */}
      {/* 2xl (1536) 才并排 · xl 主列不够放表格 min-w-[640]
          **必须 minmax(0,1fr) 不能写 1fr**：`1fr` = `minmax(auto,1fr)`，那个 auto 下限 =
          轨道的 min-content（这张卡因为表格里一堆 nowrap 的 chip，min-content 有 886px）·
          于是轨道拒绝缩到可用宽度，把右边 400px 那张卡挤出容器外。
          minmax(0,…) 把下限归零，表格该横滚就横滚（overflow-x-auto 本来就在那儿等着） */}
      <div className="grid grid-cols-1 gap-6 2xl:grid-cols-[minmax(0,1fr)_400px]">
        <Card className="p-7">
          <SectionHead
            title={t("vendor_table.title")}
            sub={t("vendor_table.sub")}
          />
          {/* 表容器：窄屏横滚 · 自然列宽（不压缩），避免 vendor 名 + badge 挤成一坨 */}
          <div className="-mx-7 mt-5 overflow-x-auto px-7">
            <div className="min-w-[640px]">
            <BareHead>
              <span className="w-7 shrink-0">#</span>
              <span className="min-w-0 flex-1">{t("vendor_table.col_vendor")}</span>
              <span className="w-14 shrink-0 text-center">{t("vendor_table.col_price")}</span>
              <span className="w-14 shrink-0 text-center">{t("vendor_table.col_lifespan")}</span>
              <span className="w-28 shrink-0 text-center">{t("vendor_table.col_durability")}</span>
              <span className="w-24 shrink-0 text-center">{t("vendor_table.col_alive_rate")}</span>
              <span className="w-14 shrink-0 text-center">{t("vendor_table.col_pulls_today")}</span>
              <span className="w-14 shrink-0 text-center">{t("vendor_table.col_warranty")}</span>
              <span className="w-14 shrink-0 text-right">{t("vendor_table.col_fallback")}</span>
            </BareHead>
            <BareList>
              {vendorsLoading && !vendors && (
                <SkeletonTable
                  rows={6}
                  cols={["w-5", "w-24", "w-14", "w-14", "w-24", "w-20", "w-12", "w-12"]}
                />
              )}
              {(vendors?.stats ?? []).map((v) => (
                <BareRow key={v.vendor_id}>
                  <span
                    className={cn(
                      "grid size-5 shrink-0 place-items-center rounded-md text-label font-semibold",
                      v.rank === 1
                        ? "bg-ok-bg text-ok-fg"
                        : v.out_of_stock
                          ? "bg-danger-bg text-danger-fg"
                          : "bg-bg-elevated text-fg-tertiary",
                    )}
                  >
                    {v.rank}
                  </span>

                  <span className="flex min-w-0 flex-1 items-center gap-2">
                    <span
                      className={cn(
                        "min-w-0 truncate font-semibold",
                        v.out_of_stock && "text-fg-tertiary",
                      )}
                    >
                      {vendorLabel(v.vendor_id, me?.tier)}
                    </span>
                    {v.rank === 1 && <MicroStat tone="ok">{t("vendor_table.tag_best")}</MicroStat>}
                    {v.out_of_stock && <MicroStat tone="danger">{t("vendor_table.tag_oos")}</MicroStat>}
                  </span>

                  <span
                    className={cn(
                      "w-14 shrink-0 text-center font-medium tnum",
                      v.out_of_stock && "text-fg-tertiary",
                    )}
                  >
                    {v.out_of_stock ? "-" : toCredits(v.unit_price)}
                  </span>

                  <span
                    className={cn(
                      "w-14 shrink-0 text-center font-medium tnum",
                      v.out_of_stock && "text-fg-tertiary",
                    )}
                  >
                    {v.out_of_stock ? "-" : fmtLifespan(v.avg_lifespan_seconds)}
                  </span>

                  {/* 耐用度：每号平均积分 · Meter 满格 = 10k（QUOTA_MAX）·
                      ≥8k 绿 · 5~8k 紫 · <5k 红 */}
                  <span className="flex w-28 shrink-0 items-center justify-center gap-2">
                    {v.out_of_stock ? (
                      <span className="text-fg-tertiary">-</span>
                    ) : (
                      <>
                        <Meter
                          value={toCredits(v.avg_credits_per_cred)}
                          max={10000}
                          color={
                            toCredits(v.avg_credits_per_cred) >= 8000 ? "#22C55E"
                              : toCredits(v.avg_credits_per_cred) >= 5000 ? "#F59E0B"
                                : "#EF4444"
                          }
                          className="w-12"
                        />
                        <span className="text-label tnum text-fg-tertiary">
                          {fmtK(toCredits(v.avg_credits_per_cred))}k
                        </span>
                      </>
                    )}
                  </span>

                  <span className="flex w-24 shrink-0 items-center justify-center gap-2">
                    {v.out_of_stock || v.alive_rate === 0 ? (
                      <span className="text-fg-tertiary">-</span>
                    ) : (
                      <>
                        <Meter
                          value={v.alive_rate}
                          max={100}
                          color={
                            v.alive_rate >= 95 ? "#22C55E" : v.alive_rate >= 88 ? "#F59E0B" : "#EF4444"
                          }
                          className="w-12"
                        />
                        <span className="text-label tnum text-fg-tertiary">
                          {v.alive_rate}%
                        </span>
                      </>
                    )}
                  </span>

                  <span
                    className={cn(
                      "w-14 shrink-0 text-center font-medium tnum",
                      v.out_of_stock && "text-fg-tertiary",
                    )}
                  >
                    {v.out_of_stock ? "-" : v.pulls_today}
                  </span>

                  <span
                    className={cn(
                      "w-14 shrink-0 text-center text-label font-medium tnum",
                      v.warranty_count > 0 ? "text-warn-fg" : "text-fg-tertiary",
                    )}
                  >
                    {v.out_of_stock ? "-" : t("vendor_table.warranty_val", { n: v.warranty_count })}
                  </span>

                  <span
                    className={cn(
                      "w-14 shrink-0 text-right text-label font-medium tnum",
                      v.fallback_count > 0 ? "text-warn-fg" : "text-fg-tertiary",
                    )}
                  >
                    {v.out_of_stock ? "-" : t("vendor_table.fallback_val", { n: v.fallback_count })}
                  </span>
                </BareRow>
              ))}
            </BareList>
            </div>
          </div>

          {/* 数据来源脚注 · 灰色小字，跟"活动记录"底部同一层级 */}
          <p className="mt-5 text-[11px] leading-relaxed text-fg-tertiary">
            {t("vendor_table.source_note")}
          </p>
        </Card>

        {/* 占比环形 */}
        <Card className="flex flex-col p-7">
          <SectionHead title={t("vendor_share.title")} sub={t("vendor_share.sub")} />
          <div className="relative mt-4 h-[180px]">
            <Suspense fallback={<div className="h-full" />}>
              <VendorSharePie data={vendors?.share ?? []} />
            </Suspense>
            <div className="pointer-events-none absolute inset-0 grid place-items-center">
              <div className="text-center">
                <div className="text-num font-semibold tnum">
                  {(vendors?.share ?? []).reduce((s, v) => s + v.pulls, 0)}
                </div>
                <Muted>{t("vendor_share.center_label")}</Muted>
              </div>
            </div>
          </div>

          {/* 图例：6 家全列，pulls=0 的用灰色 + "-" 表示没数据但确实存在 */}
          <div className="mt-5 space-y-3">
            {(vendors?.share ?? []).map((s) => {
              const noData = s.pulls === 0;
              return (
                <div key={s.vendor_id} className="flex items-center gap-2.5">
                  <span
                    className={cn(
                      "size-2 shrink-0 rounded-full",
                      noData && "opacity-40",
                    )}
                    style={{ backgroundColor: vendorColor(s.vendor_id) }}
                  />
                  <span
                    className={cn(
                      "min-w-0 flex-1 truncate font-medium",
                      noData ? "text-fg-tertiary" : "text-fg-secondary",
                    )}
                  >
                    {vendorLabel(s.vendor_id, me?.tier)}
                  </span>
                  <span
                    className={cn(
                      "font-semibold tnum",
                      noData && "text-fg-tertiary",
                    )}
                  >
                    {noData ? "-" : t("vendor_share.row_val", { n: s.pulls })}
                  </span>
                  <span className="w-9 text-right text-label tnum text-fg-tertiary">
                    {noData ? "-" : `${Math.round(s.ratio * 100)}%`}
                  </span>
                </div>
              );
            })}
          </div>
        </Card>
      </div>

      {/* ── 活动记录（裸列表 · 分页加载） ── */}
      <ActivityFeed items={acts?.items ?? []} total={acts?.total ?? 0} loading={actsLoading} />
    </div>
  );
}
