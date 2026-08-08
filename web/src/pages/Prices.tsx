import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowDown, ArrowLeft, ArrowUp, Minus } from "lucide-react";
import { useVendorPrices } from "@/api/hooks";
import { Card, Chip, Segmented } from "@/components/ui/primitives";
import { PriceBoxPlot, RoundsTooltip } from "@/components/PriceBoxPlot";
import { cn, toCredits } from "@/lib/utils";
import type { VendorPriceTrend } from "@/types";

/** 箱线矩阵专用色 · 每行独立不需要区分色相 · 统一品牌紫（深浅表达轮数）
 *  之前给 6 家配 6 个色相是为了区分重叠折线 —— 现在每家独占一行，不需要了 */
const ROW_COLOR = "#9147FF";

/** 标记 → Chip tone */
const TAG_TONE: Record<string, "ok" | "brand" | "neutral"> = {
  车最稳: "ok",
  最便宜: "brand",
  车最多: "neutral",
};

/** 每行箱线图高度 */
const ROW_H = 44;

/** vendor 价格页 · docs/15-prices-page-design.md · decisions §8.22
 *  设计要点：数据是三层（vendor → 每天 → 每轮），一根曲线表达不了
 *  → 箱线矩阵：6 家各一行，每根竖条 = 某天全部轮次的价格范围
 *  → hover 竖条 → tooltip 列出那天每一轮的时刻/区/单价/号数 */
export default function Prices() {
  const [days, setDays] = useState<number>(30);
  const [zone, setZone] = useState<string>("us");
  const [hoveredVendor, setHoveredVendor] = useState<string | null>(null);
  const [hoveredDate, setHoveredDate] = useState<string | null>(null);
  const { data, isLoading } = useVendorPrices(days, zone);
  const trends = data?.trends ?? [];

  /* 按均价升序 · 便宜的排前面 */
  const sorted = useMemo(
    () => [...trends].sort((a, b) => a.price_avg - b.price_avg),
    [trends],
  );

  /* 日期刻度 · 取 5 个 */
  const dateTicks = useMemo(() => {
    const d = trends[0]?.days ?? [];
    if (d.length === 0) return [];
    const step = Math.max(1, Math.floor(d.length / 5));
    return d.filter((_, i) => i % step === 0).map((x) => x.date);
  }, [trends]);

  /* 标记 · 跨家比较才能算 · 可并存 */
  const tags = useMemo(() => {
    if (trends.length === 0) return {} as Record<string, string[]>;
    const minAvg = Math.min(...trends.map((t) => t.price_avg));
    const maxRounds = Math.max(...trends.map((t) => t.avg_rounds_per_day));
    const out: Record<string, string[]> = {};
    for (const t of trends) {
      const list: string[] = [];
      if (t.price_avg === minAvg) list.push("最便宜");
      if (t.no_service_days === 0) list.push("车最稳");
      if (t.avg_rounds_per_day === maxRounds) list.push("车最多");
      out[t.vendor_id] = list;
    }
    return out;
  }, [trends]);

  /* 当前最便宜且有车的 */
  const cheapest = useMemo(() => {
    const pool = trends.filter((t) => t.in_stock_now);
    const use = pool.length > 0 ? pool : trends;
    if (use.length === 0) return null;
    return use.reduce((a, b) => (a.current_price <= b.current_price ? a : b));
  }, [trends]);

  /* hover 中的那天明细 */
  const hoveredRounds = useMemo(() => {
    if (!hoveredVendor || !hoveredDate) return null;
    const t = trends.find((x) => x.vendor_id === hoveredVendor);
    const d = t?.days.find((x) => x.date === hoveredDate);
    if (!t || !d) return null;
    return { label: t.vendor_label, date: d.date, rounds: d.rounds };
  }, [hoveredVendor, hoveredDate, trends]);

  return (
    <div className="space-y-section">
      {/* Hero */}
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
              上游一天发多轮车 · 每轮产量不同、单价不同 · 竖条 = 那天的价格范围 ·{" "}
              {cheapest ? (
                <>
                  当前最便宜：
                  <span className="font-semibold text-fg-secondary"> {cheapest.vendor_label}</span>
                  <span className="font-semibold tnum text-fg-secondary">
                    {" · "}{toCredits(cheapest.current_price)} 积分
                  </span>
                </>
              ) : (
                "暂无数据"
              )}
            </p>
          </div>

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

      {/* 箱线矩阵 */}
      <Card className="relative p-7">
        {isLoading ? (
          <div className="grid h-96 place-items-center text-label text-fg-tertiary">加载中…</div>
        ) : sorted.length === 0 ? (
          <div className="grid h-96 place-items-center text-label text-fg-tertiary">暂无数据</div>
        ) : (
          <>
            {/* 表头 */}
            <div className="flex items-end gap-4 border-b border-hairline pb-2.5 text-label font-semibold text-fg-tertiary">
              <span className="w-[248px] shrink-0">vendor</span>
              <span className="min-w-0 flex-1">
                {days} 天价格分布 · 竖条高度 = 当天最低~最高轮价
              </span>
              <span className="w-24 shrink-0 text-right">{days} 天涨跌</span>
            </div>

            {/* 6 行 · 每行一家 */}
            <div className="divide-y divide-hairline">
              {sorted.map((t) => (
                <VendorRow
                  key={t.vendor_id}
                  t={t}
                  tags={tags[t.vendor_id] ?? []}
                  dim={hoveredVendor != null && hoveredVendor !== t.vendor_id}
                  hoveredDate={hoveredVendor === t.vendor_id ? hoveredDate : null}
                  onEnter={() => setHoveredVendor(t.vendor_id)}
                  onLeave={() => { setHoveredVendor(null); setHoveredDate(null); }}
                  onHoverDate={setHoveredDate}
                />
              ))}
            </div>

            {/* 日期轴 */}
            <div className="flex gap-4 pt-2">
              <span className="w-[248px] shrink-0" />
              <div className="relative min-w-0 flex-1">
                {dateTicks.map((d, i) => (
                  <span
                    key={d}
                    className="absolute text-label tnum text-fg-tertiary"
                    style={{
                      left: `${(i / Math.max(1, dateTicks.length - 1)) * 100}%`,
                      transform:
                        i === 0
                          ? "none"
                          : i === dateTicks.length - 1
                            ? "translateX(-100%)"
                            : "translateX(-50%)",
                    }}
                  >
                    {d.slice(5).replace("-", "/")}
                  </span>
                ))}
              </div>
              <span className="w-24 shrink-0" />
            </div>

            {/* 图例 */}
            <div className="mt-8 flex flex-wrap items-center justify-center gap-x-5 gap-y-2 text-label text-fg-tertiary">
              <span className="flex items-center gap-1.5">
                <span
                  className="inline-block w-[6px] rounded-full"
                  style={{ height: 16, backgroundColor: ROW_COLOR }}
                />
                <span>当天价格范围（多轮）</span>
              </span>
              <span className="flex items-center gap-1.5">
                <span
                  className="inline-block h-[2px] w-3 rounded-full"
                  style={{ backgroundColor: ROW_COLOR }}
                />
                <span>只发 1 轮</span>
              </span>
              <span className="flex items-center gap-1.5">
                <span
                  className="inline-block w-[6px] rounded-full opacity-40"
                  style={{ height: 16, backgroundColor: ROW_COLOR }}
                />
                <span>颜色越深 = 那天车越多</span>
              </span>
              <span className="flex items-center gap-1.5">
                <span className="inline-block size-1.5 rounded-full bg-fg-tertiary/40" />
                <span>没发车</span>
              </span>
            </div>
          </>
        )}

        {/* hover tooltip · 跟随在图右上（不用 recharts，自己定位） */}
        {hoveredRounds && (
          <div className="pointer-events-none absolute right-7 top-7 z-10">
            <RoundsTooltip
              date={hoveredRounds.date}
              rounds={hoveredRounds.rounds}
              label={hoveredRounds.label}
              color={ROW_COLOR}
            />
          </div>
        )}
      </Card>

      <p className="text-center text-label text-fg-tertiary">
        当前数据为演示用 mock · 真实数据将从上游轮次记录聚合
      </p>
    </div>
  );
}

/* ─────────────── 单行 ─────────────── */

function VendorRow({
  t, tags, dim, hoveredDate, onEnter, onLeave, onHoverDate,
}: {
  t: VendorPriceTrend;
  tags: string[];
  dim: boolean;
  hoveredDate: string | null;
  onEnter: () => void;
  onLeave: () => void;
  onHoverDate: (d: string | null) => void;
}) {
  return (
    <div
      className={cn(
        "flex items-center gap-4 py-2.5 transition-opacity",
        dim && "opacity-30",
      )}
      onMouseEnter={onEnter}
      onMouseLeave={onLeave}
    >
      {/* 左：vendor 名 + 标记 + 均价 + 区间 + 日均轮数 */}
      <div className="w-[248px] shrink-0 space-y-0.5">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="truncate text-label font-medium">{t.vendor_label}</span>
          {tags.map((tag) => (
            <Chip key={tag} tone={TAG_TONE[tag] ?? "neutral"} className="text-[10px]">
              {tag}
            </Chip>
          ))}
        </div>
        <div className="flex items-baseline gap-2 text-label text-fg-tertiary">
          <span>
            均 <strong className="tnum text-fg">{toCredits(t.price_avg)}</strong>
          </span>
          <span className="tnum">
            {toCredits(t.price_low)}-{toCredits(t.price_high)}
          </span>
          <span>·</span>
          <span>
            日均 <strong className="tnum text-fg-secondary">{t.avg_rounds_per_day}</strong> 轮
          </span>
          {t.no_service_days > 0 && (
            <span className="text-warn-fg">{t.no_service_days} 天没车</span>
          )}
        </div>
      </div>

      {/* 中：箱线图 */}
      <div className="min-w-0 flex-1">
        <PriceBoxPlot
          days={t.days}
          color={ROW_COLOR}
          height={ROW_H}
          hoveredDate={hoveredDate}
          onHoverDate={onHoverDate}
        />
      </div>

      {/* 右：涨跌 */}
      <span className="w-24 shrink-0 text-right">
        <PctBadge pct={t.change_30d_pct} />
      </span>
    </div>
  );
}

function PctBadge({ pct }: { pct: number }) {
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
        /* 价格涨 = 用户吃亏（红）· 跌 = 占便宜（绿） */
        up ? "text-danger-fg" : "text-ok-fg",
      )}
    >
      {up ? <ArrowUp className="size-3" /> : <ArrowDown className="size-3" />}
      {up ? "+" : ""}{pct}%
    </span>
  );
}
