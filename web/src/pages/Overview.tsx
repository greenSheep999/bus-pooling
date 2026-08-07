import { useState } from "react";
import { Link } from "react-router-dom";
import { Cell, Pie, PieChart, ResponsiveContainer } from "recharts";
import {
  Activity as ActivityIcon, ChevronRight, KeyRound, Send, TrendingDown, Users, Wallet,
} from "lucide-react";
import {
  useActivities, useOverview, useTrend, useVendorStats,
} from "@/api/hooks";
import { KpiCard } from "@/components/KpiCard";
import { TrendChart } from "@/components/TrendChart";
import { ActivityRow } from "@/components/rows";
import {
  BareHead, BareList, BareRow, Card, Chip, Meter, SectionHead, Segmented, Stat,
} from "@/components/ui/primitives";
import { cn, fmtCredits, fmtLifespan, toCredits, vendorColor, vendorName } from "@/lib/utils";
import type { Destination, TimeRange, TrendMetric } from "@/types";

const RANGES: { value: TimeRange; label: string }[] = [
  { value: "today", label: "今日" },
  { value: "7d", label: "7 天" },
  { value: "30d", label: "30 天" },
  { value: "90d", label: "90 天" },
  { value: "all", label: "全部" },
];

const METRICS: { value: TrendMetric; label: string }[] = [
  { value: "credits", label: "消耗" },
  { value: "pulls", label: "拉号" },
  { value: "lifespan", label: "寿命" },
];

const DEST_LABEL: Record<Destination, string> = {
  pending: "待派去向",
  into_bus: "已进车",
  push_pool: "已推池",
  handoff: "已拿走",
};

const DEST_COLOR: Record<Destination, string> = {
  pending: "#C9A9FF",
  into_bus: "#9147FF",
  push_pool: "#6420C7",
  handoff: "#D4D4D8",
};

export default function Overview() {
  const [range, setRange] = useState<TimeRange>("30d");
  const [metric, setMetric] = useState<TrendMetric>("credits");

  const { data: ov } = useOverview(range);
  const { data: trend } = useTrend(range, metric);
  const { data: vendors } = useVendorStats();
  const { data: acts } = useActivities(range);

  const kpi = ov?.kpi;
  const totalBusCreds = (ov?.buses.items ?? []).reduce((s, b) => s + b.alive, 0);
  const extractTotal = ov?.extract.total_credentials ?? 0;

  return (
    <div className="mx-auto max-w-[1440px] space-y-section">
      {/* ── Hero + 全页时间维度 ── */}
      <div className="flex items-end justify-between">
        <div className="space-y-2">
          <h1 className="text-hero font-semibold">概览</h1>
          <p className="text-body text-fg-tertiary">
            {new Date().toLocaleDateString("zh-CN")} · {ov?.buses.bus_count ?? 0} 辆车正在跑 ·{" "}
            {kpi?.alive_count ?? 0} 号活着
          </p>
        </div>
        <Segmented options={RANGES} value={range} onChange={setRange} />
      </div>

      {/* ── 4 KPI ── */}
      <div className="grid grid-cols-4 gap-6">
        <KpiCard
          focal
          tone="credit"
          icon={Wallet}
          label="总余额"
          value={kpi ? fmtCredits(kpi.balance) : "—"}
          unit="积分"
          sub={
            kpi
              ? `本月 +${fmtCredits(kpi.balance_delta_topup)} · -${fmtCredits(kpi.balance_delta_spend)}`
              : undefined
          }
        />
        <KpiCard
          icon={TrendingDown}
          label="今日消费"
          value={kpi ? fmtCredits(kpi.spend_today) : "—"}
          unit="积分"
          sub={kpi ? `昨日 ${fmtCredits(kpi.spend_yesterday)} · 环比 +40%` : undefined}
        />
        <KpiCard
          icon={KeyRound}
          label="累计拉号"
          value={kpi ? String(kpi.pull_total) : "—"}
          unit="次"
          sub={kpi ? `本月 ${kpi.pull_this_month} 次` : undefined}
        />
        <KpiCard
          icon={ActivityIcon}
          label="活跃号"
          value={kpi ? String(kpi.alive_count) : "—"}
          unit="号"
          sub={
            kpi
              ? `${kpi.dead_count} 死 · ${kpi.pending_refill} 待补 · 平均 ${fmtLifespan(kpi.avg_lifespan_seconds)}`
              : undefined
          }
        />
      </div>

      {/* ── 3 业务线 ── */}
      <div className="grid grid-cols-3 gap-6">
        {/* 拼车 */}
        <Card className="flex flex-col gap-4 p-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <span className="grid size-7 place-items-center rounded-lg bg-brand-subtle">
                <Users className="size-3.5 text-brand-strong" />
              </span>
              <h3 className="text-body-lg font-semibold">拼车</h3>
            </div>
            <Link to="/buses" className="text-micro font-semibold text-brand-strong">
              查看 →
            </Link>
          </div>

          <Stat
            value={String(ov?.buses.bus_count ?? 0)}
            unit={`辆车 · ${totalBusCreds} 号在池`}
            size="num"
          />

          <div className="space-y-2.5">
            <span className="text-micro font-semibold text-fg-tertiary">号池分布</span>
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
                  <span className="min-w-0 flex-1 truncate text-body font-medium text-fg-secondary">
                    {b.name}
                  </span>
                  <span className="text-body font-semibold tnum">{b.alive} 号</span>
                </div>
              ))}
            </div>
          </div>

          <div className="mt-auto flex items-center justify-between border-t border-hairline pt-3.5">
            <span className="text-micro font-medium text-fg-tertiary">
              补车 {ov?.buses.refill_count ?? 0} 次 · 集单率{" "}
              {Math.round((ov?.buses.coalesce_rate ?? 0) * 100)}%
            </span>
            <span className="text-body font-semibold tnum">
              {kpi ? fmtCredits(-kpi.spend_today, { sign: true }) : "—"}
            </span>
          </div>
        </Card>

        {/* 提取 key */}
        <Card className="flex flex-col gap-4 p-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <span className="grid size-7 place-items-center rounded-lg bg-warn-bg">
                <KeyRound className="size-3.5 text-warn-fg" />
              </span>
              <h3 className="text-body-lg font-semibold">提取 key</h3>
            </div>
            <Link to="/extract" className="text-micro font-semibold text-brand-strong">
              查看 →
            </Link>
          </div>

          <Stat
            value={String(extractTotal)}
            unit={`号 · 今日 ${ov?.extract.count_today ?? 0} 次`}
            size="num"
          />

          <div className="space-y-2.5">
            <span className="text-micro font-semibold text-fg-tertiary">去向分布</span>
            <div className="flex h-2.5 overflow-hidden rounded-full bg-hairline">
              {(ov?.extract.by_destination ?? []).map((d) => (
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
                  <span className="min-w-0 flex-1 truncate text-body font-medium text-fg-secondary">
                    {DEST_LABEL[d.destination]}
                  </span>
                  <span className="text-body font-semibold tnum">{d.count} 号</span>
                </div>
              ))}
            </div>
          </div>

          <div className="mt-auto flex items-center justify-between border-t border-hairline pt-3.5">
            <span className="text-micro font-medium text-fg-tertiary">
              待派 {ov?.extract.pending ?? 0} 号
            </span>
            <span className="text-body font-semibold tnum">
              {ov ? fmtCredits(-ov.extract.spend, { sign: true }) : "—"}
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
              <h3 className="text-body-lg font-semibold text-fg-tertiary">我的发车</h3>
            </div>
            <Chip tone="brand">阶段 3</Chip>
          </div>

          <Stat value="—" unit="未启用" size="num" />

          <p className="text-body text-fg-secondary">
            绑定你的 AWS 账户 · 我方转发上游 vendor 开号 · 号池归你
          </p>

          <div className="mt-auto space-y-2.5 border-t border-hairline pt-3.5">
            {["绑定的 AWS 账户", "今日发车次数", "转发成功率", "累计发车号数"].map((t) => (
              <div key={t} className="flex items-center gap-2">
                <span className="size-[7px] shrink-0 rounded-full bg-hairline" />
                <span className="flex-1 text-body font-medium text-fg-tertiary">{t}</span>
                <span className="text-body font-medium text-fg-tertiary">—</span>
              </div>
            ))}
          </div>
        </Card>
      </div>

      {/* ── 使用趋势（全宽 focal） ── */}
      <div className="space-y-5">
        <SectionHead
          title="使用趋势"
          sub={
            kpi
              ? `消耗 ${fmtCredits(kpi.balance_delta_spend)} 积分 · 拉号 ${kpi.pull_this_month} 次 · 补车 ${ov?.buses.refill_count ?? 0} 次`
              : undefined
          }
          right={<Segmented options={METRICS} value={metric} onChange={setMetric} />}
        />
        <Card focal className="p-7">
          <TrendChart data={trend ?? []} metric={metric} />
        </Card>
      </div>

      {/* ── Vendor 监测 + 占比 ── */}
      <div className="grid grid-cols-[1fr_400px] gap-6">
        <Card className="p-7">
          <SectionHead
            title="Vendor 监测"
            sub="有效成本 = 单价 ÷ 平均寿命 · 越低越划算"
          />
          <div className="mt-5">
            <BareHead>
              <span className="w-7 shrink-0">#</span>
              <span className="min-w-0 flex-1">vendor</span>
              <span className="w-16 shrink-0 text-center">单价</span>
              <span className="w-16 shrink-0 text-center">寿命</span>
              <span className="w-20 shrink-0 text-center">有效成本</span>
              <span className="w-28 shrink-0 text-center">存活率</span>
              <span className="w-16 shrink-0 text-center">今日拉</span>
              <span className="w-16 shrink-0 text-right">fallback</span>
            </BareHead>
            <BareList>
              {(vendors?.stats ?? []).map((v) => (
                <BareRow key={v.vendor_id}>
                  <span
                    className={cn(
                      "grid size-5 shrink-0 place-items-center rounded text-micro font-semibold",
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
                        "truncate text-body font-semibold",
                        v.out_of_stock && "text-fg-tertiary",
                      )}
                    >
                      {vendorName(v.vendor_id)}
                    </span>
                    {v.rank === 1 && <Chip tone="ok">最优</Chip>}
                    {v.out_of_stock && <Chip tone="danger">缺货</Chip>}
                  </span>

                  <span
                    className={cn(
                      "w-16 shrink-0 text-center text-body font-medium tnum",
                      v.out_of_stock && "text-fg-tertiary",
                    )}
                  >
                    {v.out_of_stock ? "—" : toCredits(v.unit_price)}
                  </span>

                  <span
                    className={cn(
                      "w-16 shrink-0 text-center text-body font-medium tnum",
                      v.out_of_stock && "text-fg-tertiary",
                    )}
                  >
                    {v.out_of_stock ? "—" : fmtLifespan(v.avg_lifespan_seconds)}
                  </span>

                  <span
                    className={cn(
                      "w-20 shrink-0 text-center text-body tnum",
                      v.out_of_stock
                        ? "text-fg-tertiary"
                        : v.rank === 1
                          ? "font-semibold text-ok-fg"
                          : "font-medium",
                    )}
                  >
                    {v.out_of_stock ? "—" : v.effective_cost.toFixed(2)}
                  </span>

                  <span className="flex w-28 shrink-0 items-center justify-center gap-2">
                    <Meter
                      value={v.alive_rate}
                      max={100}
                      color={
                        v.alive_rate >= 95 ? "#22C55E" : v.alive_rate >= 88 ? "#9147FF" : "#EF4444"
                      }
                      className="w-14"
                    />
                    <span className="text-micro tnum text-fg-tertiary">
                      {v.alive_rate > 0 ? `${v.alive_rate}%` : "—"}
                    </span>
                  </span>

                  <span
                    className={cn(
                      "w-16 shrink-0 text-center text-body font-medium tnum",
                      v.out_of_stock && "text-fg-tertiary",
                    )}
                  >
                    {v.pulls_today}
                  </span>

                  <span
                    className={cn(
                      "w-16 shrink-0 text-right text-micro font-medium tnum",
                      v.fallback_count > 0 ? "text-warn-fg" : "text-fg-tertiary",
                    )}
                  >
                    {v.fallback_count} 次
                  </span>
                </BareRow>
              ))}
            </BareList>
          </div>
        </Card>

        {/* 占比环形 */}
        <Card className="flex flex-col p-7">
          <SectionHead title="Vendor 占比" sub="按 vendor 分布" />
          <div className="relative mt-4 h-[180px]">
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie
                  data={vendors?.share ?? []}
                  dataKey="pulls"
                  nameKey="vendor_id"
                  innerRadius={58}
                  outerRadius={84}
                  paddingAngle={2}
                  strokeWidth={0}
                >
                  {(vendors?.share ?? []).map((s) => (
                    <Cell key={s.vendor_id} fill={vendorColor(s.vendor_id)} />
                  ))}
                </Pie>
              </PieChart>
            </ResponsiveContainer>
            <div className="pointer-events-none absolute inset-0 grid place-items-center">
              <div className="text-center">
                <div className="text-num font-semibold tnum">
                  {(vendors?.share ?? []).reduce((s, v) => s + v.pulls, 0)}
                </div>
                <div className="text-micro text-fg-tertiary">次拉号</div>
              </div>
            </div>
          </div>

          <div className="mt-5 space-y-3">
            {(vendors?.share ?? []).map((s) => (
              <div key={s.vendor_id} className="flex items-center gap-2.5">
                <span
                  className="size-2 shrink-0 rounded-full"
                  style={{ backgroundColor: vendorColor(s.vendor_id) }}
                />
                <span className="min-w-0 flex-1 truncate text-body font-medium text-fg-secondary">
                  {vendorName(s.vendor_id)}
                </span>
                <span className="text-body font-semibold tnum">{s.pulls} 次</span>
                <span className="w-9 text-right text-micro tnum text-fg-tertiary">
                  {Math.round(s.ratio * 100)}%
                </span>
              </div>
            ))}
          </div>
        </Card>
      </div>

      {/* ── 活动记录（裸列表） ── */}
      <div className="space-y-5">
        <SectionHead
          title="活动记录"
          sub={`共 ${acts?.total ?? 0} 条 · 拉号 / 补车 / 号死 / 资金`}
          right={
            <Link
              to="/buses"
              className="flex items-center gap-1 text-micro font-semibold text-brand-strong"
            >
              全部 <ChevronRight className="size-3" />
            </Link>
          }
        />
        <BareList>
          {(acts?.items ?? []).map((a) => (
            <ActivityRow key={a.id} a={a} />
          ))}
        </BareList>
      </div>
    </div>
  );
}
