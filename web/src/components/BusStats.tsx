import { useMemo } from "react";
import {
  Bar, BarChart, CartesianGrid, Cell, Line, LineChart, ResponsiveContainer,
  Tooltip, XAxis, YAxis,
} from "recharts";
import { Users } from "lucide-react";
import {
  useBusCredentials, useBusPulls, useDownstream, useMe,
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
export function BusStats({ busId }: { busId: string }) {
  const { data: creds } = useBusCredentials(busId);
  const { data: pulls } = useBusPulls(busId);
  const { data: downstream } = useDownstream();

  return (
    <div className="space-y-6">
      {/* 头 · 说明这是什么 · 减少用户困惑 */}
      <SectionHead
        title="数据"
        sub="这辆车的时间维度趋势 · 每天多少号进池 / 多少积分花了 / 号能活多久 · 30 天窗口"
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

      {/* 成员维度占位 · 阶段 2a 落地 */}
      <MembersPlaceholder />
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
        title="拉号量"
        sub={<>30 天共 <strong className="tnum text-fg">{total}</strong> 个号进池 · 按 vendor 分色</>}
      />
      {total === 0 ? (
        <EmptyChart hint="这辆车 30 天没拉过号" />
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
                <Tooltip content={<StackedTooltip vendors={vendors} unit="个" />} />
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
        title="每日消费"
        sub={<>30 天共扣 <strong className="tnum text-fg">{fmtCredits(total)}</strong> 积分</>}
      />
      {total === 0 ? (
        <EmptyChart hint="这辆车 30 天没消费" />
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

const LIFE_BUCKETS: { label: string; min: number; max: number }[] = [
  { label: "<1h", min: 0, max: 3_600 },
  { label: "1-6h", min: 3_600, max: 6 * 3_600 },
  { label: "6-24h", min: 6 * 3_600, max: 24 * 3_600 },
  { label: "1-3d", min: 24 * 3_600, max: 3 * 24 * 3_600 },
  { label: ">3d", min: 3 * 24 * 3_600, max: Infinity },
];

function LifespanHistogram({ creds }: { creds: Credential[] }) {
  const data = useMemo(() => {
    const buckets = LIFE_BUCKETS.map((b) => ({ ...b, count: 0 }));
    for (const c of creds) {
      const life = c.lifespan_seconds ?? 0;
      if (!life) continue;
      for (const b of buckets) {
        if (life >= b.min && life < b.max) { b.count += 1; break; }
      }
    }
    return buckets.map((b) => ({ label: b.label, count: b.count }));
  }, [creds]);

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
        title="号存活时长"
        sub={
          total > 0 ? (
            <>{total} 个号 · 平均活 <strong className="tnum text-fg">{formatHours(avgLife)}</strong></>
          ) : (
            "还没有能统计寿命的号"
          )
        }
      />
      {total === 0 ? (
        <EmptyChart hint="拉号后 / 号失效后才有寿命数据" />
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
              <Tooltip content={<HistogramTooltip />} />
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

function formatHours(seconds: number): string {
  if (seconds < 3_600) return `${Math.round(seconds / 60)} 分`;
  if (seconds < 24 * 3_600) return `${(seconds / 3_600).toFixed(1)} 小时`;
  return `${(seconds / 24 / 3_600).toFixed(1)} 天`;
}

/* ─────────────── 图 4 · 推送成功率折线 ─────────────── */

function PushSuccessRateChart({
  creds, hasDownstream,
}: { creds: Credential[]; hasDownstream: boolean }) {
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
        <ChartHead title="推送成功率" sub="推我方号池 → 你的 passengerpool 的成功率" />
        <div className="grid h-52 place-items-center">
          <Alert tone="warn">
            未配置我的号池 · 在 <strong className="text-fg">设置 · 我的号池</strong> 里配 URL 后有数据
          </Alert>
        </div>
      </Card>
    );
  }

  return (
    <Card className="p-6">
      <ChartHead
        title="推送成功率"
        sub={
          totalAttempts > 0 ? (
            <>30 天 <strong className="tnum text-fg">{totalAttempts}</strong> 次推送尝试</>
          ) : (
            "还没有推送尝试"
          )
        }
      />
      {totalAttempts === 0 ? (
        <EmptyChart hint="没有号被推给你的号池" />
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
              <Tooltip content={<RateTooltip />} />
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

/* ─────────────── 成员维度占位 · 阶段 2a ─────────────── */

function MembersPlaceholder() {
  return (
    <Card className="flex items-start gap-3 p-6">
      <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-brand-subtle">
        <Users className="size-4 text-brand-strong" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="font-semibold">成员维度 · 阶段 2a 开放</div>
        <p className="mt-0.5 text-label text-fg-tertiary">
          多人车支持后加：各成员触发拉号次数占比 · 各成员推自己号池成功率对比 · 各成员积分消费占比（按分摊比例）
        </p>
      </div>
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
          <span className="text-fg-tertiary">共</span>
          <span className="font-semibold tnum">{total} {unit}</span>
        </div>
      </div>
    </TooltipShell>
  );
}

function SpendTooltip({ active, payload, label }: TooltipProps) {
  if (!active || !payload?.length) return null;
  const val = payload[0].value;
  return (
    <TooltipShell label={label}>
      <div className="flex items-center gap-2">
        <span className="text-fg-tertiary">扣</span>
        <span className="font-semibold tnum text-danger-fg">-{fmtCredits(val)}</span>
        <span className="text-fg-tertiary">积分</span>
      </div>
    </TooltipShell>
  );
}

function HistogramTooltip({ active, payload, label }: TooltipProps) {
  if (!active || !payload?.length) return null;
  return (
    <TooltipShell label={label}>
      <div className="flex items-center gap-2">
        <span className="font-semibold tnum">{payload[0].value}</span>
        <span className="text-fg-tertiary">个号</span>
      </div>
    </TooltipShell>
  );
}

function RateTooltip({ active, payload, label }: TooltipProps) {
  if (!active || !payload?.length) return null;
  const val = payload[0].value;
  return (
    <TooltipShell label={label}>
      {val == null ? (
        <span className="text-fg-tertiary">无推送数据</span>
      ) : (
        <div className="flex items-center gap-2">
          <span className="text-fg-tertiary">成功率</span>
          <span className="font-semibold tnum text-ok-fg">{val}%</span>
        </div>
      )}
    </TooltipShell>
  );
}
