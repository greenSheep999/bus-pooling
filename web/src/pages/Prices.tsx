import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
  CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";
import { ArrowDown, ArrowLeft, ArrowUp, Minus } from "lucide-react";
import { useVendorPrices } from "@/api/hooks";
import {
  BareHead, BareList, BareRow, Card, SectionHead, Segmented,
} from "@/components/ui/primitives";
import { cn, fmtCredits, toCredits } from "@/lib/utils";
import type { VendorPriceTrend } from "@/types";

/** 折线图专用调色板 · decisions §8.22
 *  全站 VENDOR_COLOR 是「同色系紫深浅」· 那套给饼图 / 分段条用（相邻色块能对比）
 *  6 条重叠折线上完全失效（3 个浅紫 + 2 个灰认不出）· 这里单独一套明确区分的色相
 *  品牌紫打头 · 其余 5 色低饱和不刺眼 · 不动全站规范 */
const LINE_COLOR: Record<string, string> = {
  "91kiro":   "#9147FF",   // 品牌紫
  kiroceo:    "#0EA5E9",   // 青
  kirooo:     "#F59E0B",   // 橙
  kiroappio:  "#10B981",   // 绿
  kiroappcc:  "#EC4899",   // 玫
  kirodrop:   "#64748B",   // 石板灰
};

const lineColor = (id: string) => LINE_COLOR[id] ?? "#9147FF";

/** vendor 价格走势页 · decisions §8.22
 *  - 30 / 7 天切换
 *  - 每 vendor 一条线 · 缺货日期在线上断开不连
 *  - hover 图例或表格行 · 高亮对应线（其他线淡化 20%）
 *  - 表格显示当前价 · 30 天涨跌 · 30 天区间 · 缺货天数 */
export default function Prices() {
  const [days, setDays] = useState<number>(30);
  const [zone, setZone] = useState<string>("us");
  const [hoveredVendor, setHoveredVendor] = useState<string | null>(null);
  const { data, isLoading } = useVendorPrices(days, zone);
  const trends = data?.trends ?? [];

  /* 构造 recharts 需要的 · 每天一行 · 每 vendor 一列 · 缺货日期该 vendor 列为 null */
  const chartData = useMemo(() => {
    if (trends.length === 0) return [];
    const dateSet = new Set<string>();
    for (const t of trends) for (const p of t.points) dateSet.add(p.date);
    const dates = Array.from(dateSet).sort();
    return dates.map((date) => {
      const row: Record<string, number | string | null> = { date: shortDay(date) };
      for (const t of trends) {
        const point = t.points.find((p) => p.date === date);
        row[t.vendor_id] = point?.price ?? null;
      }
      return row;
    });
  }, [trends]);

  /* Y 轴范围 · 贴数据区间上下各留 8% · 不从 0 起（否则 25-70 的波动被压在图上半部分看不出） */
  const yDomain = useMemo<[number, number]>(() => {
    const all = trends.flatMap((t) =>
      t.points.map((p) => p.price).filter((v): v is number => v != null),
    );
    if (all.length === 0) return [0, 100];
    const lo = Math.min(...all);
    const hi = Math.max(...all);
    const pad = (hi - lo) * 0.08 || hi * 0.08;
    return [Math.max(0, lo - pad), hi + pad];
  }, [trends]);

  /* 当前最低价 · 给用户一个"现在下单最划算"的锚点 */
  const cheapest = useMemo(() => {
    const withPrice = trends.filter((t) => t.current_price != null);
    if (withPrice.length === 0) return null;
    return withPrice.reduce((a, b) => (a.current_price! <= b.current_price! ? a : b));
  }, [trends]);

  return (
    <div className="space-y-section">
      {/* Hero · 返回入口 + 标题 + 说明 */}
      <div className="space-y-4">
        <Link
          to="/extract"
          className="inline-flex items-center gap-1 text-label font-medium text-fg-tertiary transition-colors hover:text-fg-secondary"
        >
          <ArrowLeft className="size-3.5" />
          返回提取 key
        </Link>

        <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div className="min-w-0 space-y-2">
            <h1 className="text-hero font-semibold">价格走势</h1>
            <p className="text-fg-tertiary">
              各 vendor {days} 天单价变化 · 缺货日期在线上断开 ·{" "}
              {cheapest && cheapest.current_price != null ? (
                <>
                  当前最便宜：
                  <span className="font-semibold text-fg-secondary"> {cheapest.vendor_label}</span>
                  <span className="font-semibold tnum text-fg-secondary">
                    {" · "}{toCredits(cheapest.current_price)} 积分
                  </span>
                </>
              ) : (
                "全网缺货"
              )}
            </p>
          </div>

          {/* 区域切换 + 时间范围 · 不同区价格不同（vendor 每区独立定价）· 必须能分开看 */}
          <div className="flex flex-wrap items-center gap-2">
            <Segmented
              options={[{ value: "us", label: "美国区" }, { value: "eu", label: "欧洲区" }]}
              value={zone}
              onChange={setZone}
            />
            <Segmented
              options={[{ value: 7, label: "7 天" }, { value: 30, label: "30 天" }]}
              value={days}
              onChange={setDays}
            />
          </div>
        </div>
      </div>

      {/* 多线图 */}
      <Card className="p-7">
        {isLoading ? (
          <div className="grid h-72 place-items-center text-label text-fg-tertiary">
            加载中…
          </div>
        ) : chartData.length === 0 ? (
          <div className="grid h-72 place-items-center text-label text-fg-tertiary">
            暂无数据
          </div>
        ) : (
          <>
            <div className="h-72">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart
                  data={chartData}
                  margin={{ top: 6, right: 12, bottom: 0, left: -10 }}
                >
                  <CartesianGrid
                    strokeDasharray="3 3"
                    stroke="hsl(var(--hairline))"
                    vertical={false}
                  />
                  <XAxis
                    dataKey="date"
                    tickLine={false}
                    axisLine={false}
                    tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
                    /* 30 天全显会挤成一坨 · 每 N 天一个刻度 · 7 天时全显 */
                    interval={days > 7 ? Math.floor(days / 6) : 0}
                    minTickGap={16}
                  />
                  <YAxis
                    tickLine={false}
                    axisLine={false}
                    tickFormatter={(v: number) => String(toCredits(v))}
                    tick={{ fontSize: 11, fill: "hsl(var(--fg-tertiary))" }}
                    /* 不从 0 起 · 贴着数据区间上下留 8% 余量 · 否则波动被压扁看不出 */
                    domain={yDomain}
                    allowDecimals={false}
                  />
                  <Tooltip content={<PriceTooltip trends={trends} />} />
                  {trends.map((t) => {
                    const dim = hoveredVendor != null && hoveredVendor !== t.vendor_id;
                    return (
                      <Line
                        key={t.vendor_id}
                        type="monotone"
                        dataKey={t.vendor_id}
                        stroke={lineColor(t.vendor_id)}
                        strokeWidth={hoveredVendor === t.vendor_id ? 2.5 : 1.75}
                        strokeOpacity={dim ? 0.2 : 1}
                        dot={false}
                        activeDot={{ r: 3 }}
                        connectNulls={false}
                        isAnimationActive={false}
                      />
                    );
                  })}
                </LineChart>
              </ResponsiveContainer>
            </div>

            {/* 不做图例 · 下方详情表已有色块 + hover 联动 · 图例是重复信息 */}
          </>
        )}
      </Card>

      {/* 详情表 */}
      <div className="space-y-5">
        <SectionHead
          title="详情"
          sub={
            <>
              {zone === "us" ? "美国区" : "欧洲区"} ·{" "}
              <span className="font-semibold tnum">{trends.length}</span> 家 vendor · 按当前价升序 · hover 行高亮图上对应线
            </>
          }
        />
        <Card className="p-4">
          <div className="overflow-x-auto">
            <div className="min-w-[720px]">
              <BareHead>
                <span className="w-3 shrink-0" />
                <span className="min-w-0 flex-1">vendor</span>
                <span className="w-14 shrink-0 text-center">区域</span>
                <span className="w-20 shrink-0 text-right">当前价</span>
                <span className="w-24 shrink-0 text-right">{days} 天涨跌</span>
                <span className="w-28 shrink-0 text-right">{days} 天区间</span>
                <span className="w-20 shrink-0 text-right">缺货</span>
              </BareHead>
              <BareList>
                {[...trends]
                  .sort((a, b) => {
                    if (a.current_price == null) return 1;
                    if (b.current_price == null) return -1;
                    return a.current_price - b.current_price;
                  })
                  .map((t) => (
                    <PriceRow
                      key={t.vendor_id}
                      t={t}
                      dim={hoveredVendor != null && hoveredVendor !== t.vendor_id}
                      onEnter={() => setHoveredVendor(t.vendor_id)}
                      onLeave={() => setHoveredVendor(null)}
                    />
                  ))}
              </BareList>
            </div>
          </div>
        </Card>
      </div>

      {/* mock 说明（阶段 1a 前端骨架 · 后端聚合在 1b+）*/}
      <p className="text-center text-label text-fg-tertiary">
        当前数据为演示用 mock · 真实价格走势将从后端聚合每日快照
      </p>
    </div>
  );
}

/* ─────────────── 表格行 ─────────────── */

function PriceRow({
  t, dim, onEnter, onLeave,
}: {
  t: VendorPriceTrend;
  dim: boolean;
  onEnter: () => void;
  onLeave: () => void;
}) {
  const outOfStock = t.current_price == null;
  return (
    <BareRow
      className={cn("transition-opacity", dim && "opacity-30")}
      onMouseEnter={onEnter}
      onMouseLeave={onLeave}
    >
      <span
        className="w-3 shrink-0 rounded-sm"
        style={{ backgroundColor: lineColor(t.vendor_id), height: 12 }}
      />
      <span className="min-w-0 flex-1 truncate font-medium">{t.vendor_label}</span>
      <span className="w-14 shrink-0 text-center text-label font-medium text-fg-secondary">
        {/* zone=null 的 vendor 不分区域 · 一档到底 · 任何区筛选下都显示它 */}
        {t.zone ?? <span className="text-fg-tertiary">全区</span>}
      </span>
      <span className="w-20 shrink-0 text-right font-semibold tnum">
        {outOfStock ? (
          <span className="text-danger-fg">缺货</span>
        ) : (
          <>
            {toCredits(t.current_price!)}
            <span className="ml-0.5 font-medium text-fg-tertiary">积分</span>
          </>
        )}
      </span>
      <span className="w-24 shrink-0 text-right">
        <PctBadge pct={t.change_30d_pct} />
      </span>
      <span className="w-28 shrink-0 text-right text-label tnum text-fg-tertiary">
        {t.price_low != null && t.price_high != null
          ? `${toCredits(t.price_low)} - ${toCredits(t.price_high)}`
          : "—"}
      </span>
      <span className="w-20 shrink-0 text-right text-label tnum">
        {t.outage_days > 0 ? (
          <span className="text-warn-fg">{t.outage_days} 天</span>
        ) : (
          <span className="text-fg-tertiary">0 天</span>
        )}
      </span>
    </BareRow>
  );
}

function PctBadge({ pct }: { pct: number | null }) {
  if (pct == null) return <span className="text-label text-fg-tertiary">—</span>;
  if (pct === 0) {
    return (
      <span className="inline-flex items-center gap-0.5 text-label font-medium text-fg-tertiary">
        <Minus className="size-3" />0%
      </span>
    );
  }
  const up = pct > 0;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-0.5 text-label font-semibold tnum",
        /* 价格涨 = 用户吃亏（红）· 价格跌 = 用户占便宜（绿）· 反直觉但符合"看价格趋势"语义 */
        up ? "text-danger-fg" : "text-ok-fg",
      )}
    >
      {up ? <ArrowUp className="size-3" /> : <ArrowDown className="size-3" />}
      {up ? "+" : ""}{pct}%
    </span>
  );
}

/* ─────────────── util · shortDay ─────────────── */

function shortDay(iso: string): string {
  const d = new Date(iso);
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

/* ─────────────── Tooltip · 显示某天所有 vendor 价格 ─────────────── */

type TooltipProps = {
  active?: boolean;
  payload?: { value: number | null; dataKey: string; color: string }[];
  label?: string;
};

function PriceTooltip({
  active, payload, label, trends,
}: TooltipProps & { trends: VendorPriceTrend[] }) {
  if (!active || !payload?.length) return null;
  /* 按价格升序 · null 排最后 */
  const rows = payload
    .map((p) => {
      const t = trends.find((x) => x.vendor_id === p.dataKey);
      return { label: t?.vendor_label ?? p.dataKey, value: p.value, color: p.color };
    })
    .sort((a, b) => {
      if (a.value == null) return 1;
      if (b.value == null) return -1;
      return a.value - b.value;
    });
  return (
    <div className="rounded-lg border border-hairline bg-bg px-3 py-2 text-label shadow-pop">
      <div className="mb-1 font-semibold">{label}</div>
      <div className="space-y-0.5">
        {rows.map((r) => (
          <div key={r.label} className="flex items-center gap-2">
            <span className="size-1.5 rounded-sm" style={{ backgroundColor: r.color }} />
            <span className="text-fg-secondary">{r.label}</span>
            <span className="ml-auto font-semibold tnum">
              {r.value == null ? (
                <span className="text-fg-tertiary">缺货</span>
              ) : (
                <>{fmtCredits(r.value)} <span className="font-medium text-fg-tertiary">积分</span></>
              )}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

/* 未用 · 编译器 quiet */
export type { VendorPriceTrend as _T };
