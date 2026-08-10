import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  Bar, BarChart, CartesianGrid, Cell, Line, LineChart, ResponsiveContainer,
  Tooltip, XAxis, YAxis,
} from "recharts";
import {
  useBusCredentials, useBusMemberStats, useBusPulls, useDownstream, useMe,
} from "@/api/hooks";
import { Card, SectionHead } from "@/components/ui/primitives";
import { Alert } from "@/components/ui/alert";
import {
  fmtCredits, toCredits, vendorColor, vendorLabel,
} from "@/lib/utils";
import type { Credential, PullRound } from "@/types";

/** BusDetail「数据」tab · 阶段 1a 时间维度 4 图 · decisions §8.19
 *  数据源：credentials + pull_rounds 已有字段（不需要新 API）
 *  成员维度阶段 2a 落地（§8.18 分摊扣款接线后）· 现在留 EmptyState 占位 */
export default function BusStats({ busId }: { busId: string }) {
  const { t } = useTranslation("buses");
  const { data: creds } = useBusCredentials(busId);
  const { data: pulls } = useBusPulls(busId);
  const { data: downstream } = useDownstream();

  return (
    <div className="space-y-6">
      {/* 头 · 说明这是什么 · 减少用户困惑 */}
      <SectionHead
        title={t("stats.head.title")}
        sub={t("stats.head.sub")}
      />

      {/* 上排 · 2 图 · 拉号量 + 每日消费 */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <PullVolumeChart pulls={pulls ?? []} />
        <DailySpendChart pulls={pulls ?? []} />
      </div>

      {/* 下排 · 2 图 · 号存活 + 推送成功率 */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <LifespanHistogram creds={creds ?? []} />
        <PushSuccessRateChart
          creds={creds ?? []}
          hasDownstream={!!downstream?.connected}
        />
      </div>

      {/* 成员维度 · 多人车才显示（1 人车 return null） */}
      <MemberBreakdown busId={busId} />
    </div>
  );
}

/* ─────────────── util · 30 天空日历骨架 ─────────────── */

/** 生成过去 N 天的 YYYY-MM-DD 数组（含今天）· 用于 bar chart 骨架 · 空日期用 0 填 */
function last30DaysKeys(): string[] {
  const out: string[] = [];
  const today = new Date();
  for (let i = 29; i >= 0; i--) {
    const d = new Date(today);
    d.setDate(today.getDate() - i);
    out.push(d.toISOString().slice(0, 10));
  }
  return out;
}

function shortDay(dateISO: string): string {
  const d = new Date(dateISO);
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

/* ─────────────── 图 1 · 拉号量柱图 · 按 vendor 分色堆叠 ─────────────── */

function PullVolumeChart({ pulls }: { pulls: PullRound[] }) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  /* 30 天按 vendor 分组 · Recharts 需要每天一个 row · 每个 vendor 一列 */
  const { data, vendors } = useMemo(() => {
    const days = last30DaysKeys();
    const vendorSet = new Set<string>();
    /* 每天 vendor => count 累加 */
    const agg: Record<string, Record<string, number>> = {};
    for (const d of days) agg[d] = {};
    for (const p of pulls) {
      const day = p.created_at.slice(0, 10);
      if (!(day in agg)) continue;
      vendorSet.add(p.vendor_id);
      agg[day][p.vendor_id] = (agg[day][p.vendor_id] ?? 0) + p.count_purchased;
    }
    const vs = Array.from(vendorSet);
    return {
      vendors: vs,
      data: days.map((d) => {
        const row: Record<string, number | string> = { date: shortDay(d), _iso: d };
        for (const v of vs) row[v] = agg[d][v] ?? 0;
        return row;
      }),
    };
  }, [pulls]);

  const total = data.reduce(
    (s, row) => s + vendors.reduce((a, v) => a + (row[v] as number), 0),
    0,
  );

  return (
    <Card className="p-6">
      <ChartHead
        title={t("stats.pull-volume.title")}
        sub={<>{t("stats.pull-volume.sub-prefix")} <strong className="tnum text-fg">{total}</strong> {t("stats.pull-volume.sub-suffix")}</>}
      />
      {total === 0 ? (
        <EmptyChart hint={t("stats.pull-volume.empty")} />
      ) : (
        <>
          <div className="h-52">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={data} margin={{ top: 6, right: 0, bottom: 0, left: -20 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--hairline))" vertical={false} />
                <XAxis
                  dataKey="date" tickLine={false} axisLine={false}
                  tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
                  interval="preserveStartEnd"
                />
                <YAxis
                  tickLine={false} axisLine={false} allowDecimals={false}
                  tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
                />
                <Tooltip content={<StackedTooltip vendors={vendors} unit={t("stats.pull-volume.tooltip-unit")} />} />
                {vendors.map((v) => (
                  <Bar key={v} dataKey={v} stackId="a" fill={vendorColor(v)} radius={[3, 3, 0, 0]} />
                ))}
              </BarChart>
            </ResponsiveContainer>
          </div>
          {/* Legend · 按 vendor · 手写更好控（recharts 内建 legend 样式跟项目不合） */}
          <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-label">
            {vendors.map((v) => (
              <span key={v} className="flex items-center gap-1.5 text-fg-secondary">
                <span className="size-2 rounded-sm" style={{ backgroundColor: vendorColor(v) }} />
                {vendorLabel(v, !!me?.invited)}
              </span>
            ))}
          </div>
        </>
      )}
    </Card>
  );
}

/* ─────────────── 图 2 · 每日消费柱图 ─────────────── */

function DailySpendChart({ pulls }: { pulls: PullRound[] }) {
  const { t } = useTranslation("buses");
  const data = useMemo(() => {
    const days = last30DaysKeys();
    const agg: Record<string, number> = {};
    for (const d of days) agg[d] = 0;
    for (const p of pulls) {
      const day = p.created_at.slice(0, 10);
      if (day in agg && p.total_cost < 0) agg[day] += Math.abs(p.total_cost);
    }
    return days.map((d) => ({ date: shortDay(d), value: agg[d] }));
  }, [pulls]);

  const total = data.reduce((s, r) => s + r.value, 0);

  return (
    <Card className="p-6">
      <ChartHead
        title={t("stats.daily-spend.title")}
        sub={<>{t("stats.daily-spend.sub-prefix")} <strong className="tnum text-fg">{fmtCredits(total)}</strong> {t("stats.daily-spend.sub-suffix")}</>}
      />
      {total === 0 ? (
        <EmptyChart hint={t("stats.daily-spend.empty")} />
      ) : (
        <div className="h-52">
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={data} margin={{ top: 6, right: 0, bottom: 0, left: -10 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--hairline))" vertical={false} />
              <XAxis
                dataKey="date" tickLine={false} axisLine={false}
                tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
                interval="preserveStartEnd"
              />
              <YAxis
                tickLine={false} axisLine={false}
                tickFormatter={(v: number) => String(toCredits(v))}
                tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
              />
              <Tooltip content={<SpendTooltip />} />
              <Bar dataKey="value" fill="#EF4444" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
    </Card>
  );
}

/* ─────────────── 图 3 · 号存活时长分布 ─────────────── */

const LIFE_BUCKETS: { key: string; min: number; max: number }[] = [
  { key: "under-1h", min: 0, max: 3_600 },
  { key: "1-6h", min: 3_600, max: 6 * 3_600 },
  { key: "6-24h", min: 6 * 3_600, max: 24 * 3_600 },
  { key: "1-3d", min: 24 * 3_600, max: 3 * 24 * 3_600 },
  { key: "over-3d", min: 3 * 24 * 3_600, max: Infinity },
];

function LifespanHistogram({ creds }: { creds: Credential[] }) {
  const { t } = useTranslation("buses");
  const data = useMemo(() => {
    const buckets = LIFE_BUCKETS.map((b) => ({ ...b, count: 0 }));
    for (const c of creds) {
      const life = c.lifespan_seconds ?? 0;
      if (!life) continue;
      for (const b of buckets) {
        if (life >= b.min && life < b.max) { b.count += 1; break; }
      }
    }
    return buckets.map((b) => ({ label: t(`stats.lifespan.bucket.${b.key}`), count: b.count }));
  }, [creds, t]);

  const total = data.reduce((s, b) => s + b.count, 0);
  /* 用平均寿命当"这辆车的号质量" · 只统计有寿命数据的号 */
  const avgLife = useMemo(() => {
    const withLife = creds.filter((c) => c.lifespan_seconds);
    if (!withLife.length) return 0;
    const sum = withLife.reduce((s, c) => s + (c.lifespan_seconds ?? 0), 0);
    return sum / withLife.length;
  }, [creds]);

  return (
    <Card className="p-6">
      <ChartHead
        title={t("stats.lifespan.title")}
        sub={
          total > 0 ? (
            <>{total} {t("stats.lifespan.sub-count-mid")} <strong className="tnum text-fg">{formatHours(avgLife, t)}</strong></>
          ) : (
            t("stats.lifespan.sub-empty")
          )
        }
      />
      {total === 0 ? (
        <EmptyChart hint={t("stats.lifespan.empty")} />
      ) : (
        <div className="h-52">
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={data} margin={{ top: 6, right: 0, bottom: 0, left: -20 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--hairline))" vertical={false} />
              <XAxis
                dataKey="label" tickLine={false} axisLine={false}
                tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
              />
              <YAxis
                tickLine={false} axisLine={false} allowDecimals={false}
                tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
              />
              <Tooltip content={<HistogramTooltip unit={t("stats.lifespan.tooltip-unit")} />} />
              <Bar dataKey="count" radius={[3, 3, 0, 0]}>
                {data.map((_, i) => (
                  /* 越靠右（活得久）越紫 · 直观暗示"号质量" */
                  <Cell key={i} fill={["#EF4444", "#F59E0B", "#C9A9FF", "#A574FF", "#9147FF"][i]} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
    </Card>
  );
}

function formatHours(seconds: number, t: (k: string) => string): string {
  if (seconds < 3_600) return `${Math.round(seconds / 60)} ${t("stats.lifespan.hours-min-suffix")}`;
  if (seconds < 24 * 3_600) return `${(seconds / 3_600).toFixed(1)} ${t("stats.lifespan.hours-hour-suffix")}`;
  return `${(seconds / 24 / 3_600).toFixed(1)} ${t("stats.lifespan.hours-day-suffix")}`;
}

/* ─────────────── 图 4 · 推送成功率折线 ─────────────── */

function PushSuccessRateChart({
  creds, hasDownstream,
}: { creds: Credential[]; hasDownstream: boolean }) {
  const { t } = useTranslation("buses");
  const data = useMemo(() => {
    const days = last30DaysKeys();
    /* 每天 · 有 pushed_at 的 credential 记一次 attempt，成功/失败按 push_failed 判 */
    const agg: Record<string, { ok: number; fail: number }> = {};
    for (const d of days) agg[d] = { ok: 0, fail: 0 };
    for (const c of creds) {
      const t = c.pushed_at ?? (c.push_failed ? c.pulled_at : null);
      if (!t) continue;
      const day = t.slice(0, 10);
      if (!(day in agg)) continue;
      if (c.push_failed) agg[day].fail += 1;
      else if (c.pushed_at) agg[day].ok += 1;
    }
    return days.map((d) => {
      const { ok, fail } = agg[d];
      const total = ok + fail;
      return {
        date: shortDay(d),
        rate: total > 0 ? Math.round((ok / total) * 100) : null,
        total,
      };
    });
  }, [creds]);

  const totalAttempts = data.reduce((s, r) => s + r.total, 0);

  if (!hasDownstream) {
    return (
      <Card className="p-6">
        <ChartHead title={t("stats.push-rate.title")} sub={t("stats.push-rate.sub-desc")} />
        <div className="grid h-52 place-items-center">
          <Alert tone="warn">
            {t("stats.push-rate.warn-prefix")} <strong className="text-fg">{t("stats.push-rate.warn-link")}</strong> {t("stats.push-rate.warn-suffix")}
          </Alert>
        </div>
      </Card>
    );
  }

  return (
    <Card className="p-6">
      <ChartHead
        title={t("stats.push-rate.title")}
        sub={
          totalAttempts > 0 ? (
            <>{t("stats.push-rate.sub-attempts-prefix")} <strong className="tnum text-fg">{totalAttempts}</strong> {t("stats.push-rate.sub-attempts-suffix")}</>
          ) : (
            t("stats.push-rate.sub-empty")
          )
        }
      />
      {totalAttempts === 0 ? (
        <EmptyChart hint={t("stats.push-rate.empty")} />
      ) : (
        <div className="h-52">
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={data} margin={{ top: 6, right: 6, bottom: 0, left: -20 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="hsl(var(--hairline))" vertical={false} />
              <XAxis
                dataKey="date" tickLine={false} axisLine={false}
                tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
                interval="preserveStartEnd"
              />
              <YAxis
                tickLine={false} axisLine={false} domain={[0, 100]}
                tickFormatter={(v: number) => `${v}%`}
                tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
              />
              <Tooltip content={<RateTooltip rateLabel={t("stats.push-rate.tooltip-rate")} emptyLabel={t("stats.push-rate.tooltip-empty")} />} />
              <Line
                type="monotone" dataKey="rate" stroke="#22C55E"
                strokeWidth={2} dot={{ r: 2 }} activeDot={{ r: 4 }}
                connectNulls
              />
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}
    </Card>
  );
}

/* ─────────────── 成员维度 · 1c 多人拼车落地 ─────────────── */

/** 各成员分到多少号 / 花了多少积分 · 数据来自 pull_round 的实际号数分配
 *  1 人车只有自己一行（占比 100%）· 没什么可比的·所以只在多人时显示 */
function MemberBreakdown({ busId }: { busId: string }) {
  const { t } = useTranslation("buses");
  const { data } = useBusMemberStats(busId);
  const members = data?.members ?? [];

  // 1 人车不显示这块 —— 跟自己比占比没有信息量
  if (members.length <= 1) return null;

  const totalKeys = data?.total_keys ?? 0;
  const totalSpend = data?.total_spend ?? 0;
  const maxKeys = Math.max(1, ...members.map((m) => m.keys_taken));
  // 有人推过号池才显示推送列（1e 之前恒为 0·没必要占位）
  const anyPush = members.some((m) => m.pushed_ok + m.push_failed > 0);

  return (
    <Card className="p-6">
      <ChartHead
        title={t("stats.member.title")}
        sub={
          <>
            {t("stats.member.sub-prefix")}{" "}
            <span className="font-semibold tnum">{totalKeys}</span> {t("stats.member.sub-mid")}{" "}
            <span className="font-semibold tnum">{fmtCredits(totalSpend)}</span> {t("stats.member.sub-suffix")}
          </>
        }
      />

      <div className="space-y-3">
        {members.map((m) => {
          const keyPct = totalKeys > 0 ? (m.keys_taken / totalKeys) * 100 : 0;
          const barPct = (m.keys_taken / maxKeys) * 100;
          const pushTotal = m.pushed_ok + m.push_failed;
          return (
            <div key={m.passenger_id} className="space-y-1.5">
              <div className="flex items-baseline justify-between gap-3">
                <span className="flex min-w-0 items-center gap-2">
                  <span className="truncate font-medium">{m.username}</span>
                  {m.role === "owner" && (
                    <span className="shrink-0 rounded-md bg-brand-subtle px-1.5 py-0.5 text-[10px] font-semibold text-brand-strong">
                      {t("stats.member.role-owner")}
                    </span>
                  )}
                  {m.status === "suspended" && (
                    <span className="shrink-0 rounded-md bg-bg-elevated px-1.5 py-0.5 text-[10px] font-medium text-fg-tertiary">
                      {t("stats.member.status-suspended")}
                    </span>
                  )}
                </span>
                <span className="shrink-0 text-label text-fg-tertiary">
                  <span className="font-semibold tnum text-fg">{m.keys_taken}</span> {t("stats.member.count-suffix")}
                  {" · "}
                  <span className="tnum">{keyPct.toFixed(0)}%</span>
                  {" · "}
                  <span className="font-semibold tnum text-fg">{fmtCredits(m.spend_total)}</span> {t("stats.member.credits-suffix")}
                  {anyPush && pushTotal > 0 && (
                    <>
                      {" "}{t("stats.member.push-prefix")}{" "}
                      <span className="tnum">
                        {m.pushed_ok}/{pushTotal}
                      </span>
                    </>
                  )}
                </span>
              </div>
              {/* 横条 · 相对最大值 · 挂起的人用灰色区分 */}
              <div className="h-2 overflow-hidden rounded-full bg-bg-elevated">
                <div
                  className={
                    m.status === "suspended"
                      ? "h-full rounded-full bg-fg-tertiary/40"
                      : "h-full rounded-full bg-brand"
                  }
                  style={{ width: `${Math.max(barPct, m.keys_taken > 0 ? 4 : 0)}%` }}
                />
              </div>
              <div className="text-label text-fg-tertiary">
                {t("stats.member.row-prefix")} <span className="tnum">{m.rounds_joined}</span> {t("stats.member.row-mid")}{" "}
                <span className="tnum">{m.share_pct}{t("stats.member.row-percent")}</span>
              </div>
            </div>
          );
        })}
      </div>

      <p className="mt-4 text-label text-fg-tertiary">
        {t("stats.member.footnote")}
      </p>
    </Card>
  );
}

/* ─────────────── 小组件 · 图头 · 空态 ─────────────── */

function ChartHead({ title, sub }: { title: string; sub: React.ReactNode }) {
  return (
    <div className="mb-4">
      <h3 className="text-body-lg font-semibold">{title}</h3>
      <p className="mt-0.5 text-label text-fg-tertiary">{sub}</p>
    </div>
  );
}

function EmptyChart({ hint }: { hint: string }) {
  return (
    <div className="grid h-52 place-items-center text-label text-fg-tertiary">
      {hint}
    </div>
  );
}

/* ─────────────── Tooltip · 手写跟项目风格一致 · recharts 默认丑 ─────────────── */

type TooltipPayloadItem = { value: number; dataKey: string; color: string };
type TooltipProps = {
  active?: boolean;
  payload?: TooltipPayloadItem[];
  label?: string;
};

function TooltipShell({ label, children }: { label?: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-hairline bg-bg px-3 py-2 text-label shadow-pop">
      {label && <div className="mb-1 font-semibold">{label}</div>}
      {children}
    </div>
  );
}

function StackedTooltip({
  active, payload, label, vendors, unit,
}: TooltipProps & { vendors: string[]; unit: string }) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  if (!active || !payload?.length) return null;
  const total = payload.reduce((s, p) => s + p.value, 0);
  if (total === 0) return null;
  return (
    <TooltipShell label={label}>
      <div className="space-y-0.5">
        {vendors
          .filter((v) => (payload.find((p) => p.dataKey === v)?.value ?? 0) > 0)
          .map((v) => {
            const val = payload.find((p) => p.dataKey === v)?.value ?? 0;
            return (
              <div key={v} className="flex items-center gap-2">
                <span className="size-1.5 rounded-sm" style={{ backgroundColor: vendorColor(v) }} />
                <span className="text-fg-secondary">{vendorLabel(v, !!me?.invited)}</span>
                <span className="ml-auto font-semibold tnum">{val} {unit}</span>
              </div>
            );
          })}
        <div className="mt-1 flex justify-between border-t border-hairline pt-1">
          <span className="text-fg-tertiary">{t("stats.pull-volume.tooltip-total")}</span>
          <span className="font-semibold tnum">{total} {unit}</span>
        </div>
      </div>
    </TooltipShell>
  );
}

function SpendTooltip({ active, payload, label }: TooltipProps) {
  const { t } = useTranslation("buses");
  if (!active || !payload?.length) return null;
  const val = payload[0].value;
  return (
    <TooltipShell label={label}>
      <div className="flex items-center gap-2">
        <span className="text-fg-tertiary">{t("stats.daily-spend.tooltip-charge")}</span>
        <span className="font-semibold tnum text-danger-fg">-{fmtCredits(val)}</span>
        <span className="text-fg-tertiary">{t("stats.daily-spend.tooltip-credits")}</span>
      </div>
    </TooltipShell>
  );
}

function HistogramTooltip({ active, payload, label, unit }: TooltipProps & { unit: string }) {
  if (!active || !payload?.length) return null;
  return (
    <TooltipShell label={label}>
      <div className="flex items-center gap-2">
        <span className="font-semibold tnum">{payload[0].value}</span>
        <span className="text-fg-tertiary">{unit}</span>
      </div>
    </TooltipShell>
  );
}

function RateTooltip({ active, payload, label, rateLabel, emptyLabel }: TooltipProps & { rateLabel: string; emptyLabel: string }) {
  if (!active || !payload?.length) return null;
  const val = payload[0].value;
  return (
    <TooltipShell label={label}>
      {val == null ? (
        <span className="text-fg-tertiary">{emptyLabel}</span>
      ) : (
        <div className="flex items-center gap-2">
          <span className="text-fg-tertiary">{rateLabel}</span>
          <span className="font-semibold tnum text-ok-fg">{val}%</span>
        </div>
      )}
    </TooltipShell>
  );
}
