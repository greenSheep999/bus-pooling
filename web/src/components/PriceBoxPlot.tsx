import { useMemo, useState } from "react";
import { cn, fmtCredits, toCredits } from "@/lib/utils";
import type { VendorDayRounds, VendorRound } from "@/types";

/** 单家 vendor 的箱线行 · docs/15-prices-page-design.md
 *  每根竖条 = 某天全部轮次的价格范围（顶=最高轮价 底=最低轮价）
 *  一天只 1 轮 → 画短横线（没有区间）· 那天没发车 → 灰点
 *  颜色深浅 = 那天几轮车（紫色系深浅 · 不用杂色 —— 每行独立不需要区分色相）
 *  Y 轴每行独立：各家价格量级差太多（26-33 vs 54-67），共享轴会把竖条压成一条线 */
export function PriceBoxPlot({
  days,
  color,
  height = 44,
  hoveredDate,
  onHoverDate,
}: {
  days: VendorDayRounds[];
  color: string;
  height?: number;
  hoveredDate: string | null;
  onHoverDate: (d: string | null) => void;
}) {
  const [w, setW] = useState(800);

  /* 该行独立 Y 域 · 上下留 12% */
  const { lo, hi } = useMemo(() => {
    const all = days.flatMap((d) => d.rounds.map((r) => r.unit_price));
    if (all.length === 0) return { lo: 0, hi: 1 };
    const min = Math.min(...all);
    const max = Math.max(...all);
    const pad = (max - min) * 0.12 || max * 0.08;
    return { lo: min - pad, hi: max + pad };
  }, [days]);

  /* 轮数 → 颜色深浅 · 该行内的相对量（车多的日子色深） */
  const maxRounds = useMemo(
    () => Math.max(1, ...days.map((d) => d.rounds.length)),
    [days],
  );

  const y = (v: number) => height - ((v - lo) / (hi - lo)) * height;
  const slot = w / Math.max(1, days.length);
  const barW = Math.min(9, Math.max(3, slot * 0.55));

  return (
    <svg
      className="w-full"
      height={height}
      ref={(el) => { if (el) setW(el.clientWidth || 800); }}
      onMouseLeave={() => onHoverDate(null)}
    >
      {days.map((d, i) => {
        const cx = slot * i + slot / 2;
        const hovered = hoveredDate === d.date;

        /* 没发车 · 灰点在中线 */
        if (d.rounds.length === 0) {
          return (
            <g key={d.date} onMouseEnter={() => onHoverDate(d.date)}>
              <rect x={cx - slot / 2} y={0} width={slot} height={height} fill="transparent" />
              <circle
                cx={cx}
                cy={height / 2}
                r={hovered ? 2.5 : 1.5}
                fill="#D4D4D8"
              />
            </g>
          );
        }

        const prices = d.rounds.map((r) => r.unit_price);
        const min = Math.min(...prices);
        const max = Math.max(...prices);
        const opacity = 0.35 + (d.rounds.length / maxRounds) * 0.65;

        /* 只 1 轮 · 画短横线（没有区间） */
        if (d.rounds.length === 1) {
          return (
            <g key={d.date} onMouseEnter={() => onHoverDate(d.date)}>
              <rect x={cx - slot / 2} y={0} width={slot} height={height} fill="transparent" />
              <line
                x1={cx - barW / 2}
                x2={cx + barW / 2}
                y1={y(min)}
                y2={y(min)}
                stroke={color}
                strokeWidth={hovered ? 3 : 2}
                strokeOpacity={hovered ? 1 : opacity}
                strokeLinecap="round"
              />
            </g>
          );
        }

        /* 多轮 · 竖条 = min~max 区间 */
        const yTop = y(max);
        const yBot = y(min);
        return (
          <g key={d.date} onMouseEnter={() => onHoverDate(d.date)}>
            <rect x={cx - slot / 2} y={0} width={slot} height={height} fill="transparent" />
            <rect
              x={cx - barW / 2}
              y={yTop}
              width={barW}
              height={Math.max(2, yBot - yTop)}
              rx={barW / 2}
              fill={color}
              fillOpacity={hovered ? 1 : opacity}
              stroke={hovered ? color : "none"}
              strokeWidth={hovered ? 1.5 : 0}
            />
          </g>
        );
      })}

      {/* hover 竖虚线 · 指示当前列 */}
      {hoveredDate && (() => {
        const idx = days.findIndex((d) => d.date === hoveredDate);
        if (idx < 0) return null;
        const cx = slot * idx + slot / 2;
        return (
          <line
            x1={cx} x2={cx} y1={0} y2={height}
            stroke={color}
            strokeWidth={1}
            strokeDasharray="3 3"
            strokeOpacity={0.35}
          />
        );
      })()}
    </svg>
  );
}

/** 某天全部轮次的明细 tooltip · 这才是用户要的"每轮价格多少" */
export function RoundsTooltip({
  date, rounds, label, color,
}: {
  date: string;
  rounds: VendorRound[];
  label: string;
  color: string;
}) {
  if (rounds.length === 0) {
    return (
      <div className="rounded-xl border border-hairline bg-bg px-3 py-2 text-label shadow-pop">
        <div className="flex items-center gap-1.5">
          <span className="size-1.5 rounded-sm" style={{ backgroundColor: color }} />
          <span className="font-medium">{label}</span>
          <span className="text-fg-tertiary">· {date.slice(5)}</span>
        </div>
        <div className="mt-1 text-fg-tertiary">那天没发车</div>
      </div>
    );
  }

  const prices = rounds.map((r) => r.unit_price);
  const min = Math.min(...prices);
  const max = Math.max(...prices);

  return (
    <div className="min-w-[240px] rounded-xl border border-hairline bg-bg px-3 py-2.5 text-label shadow-pop">
      {/* 头 · vendor + 日期 + 轮数 */}
      <div className="flex items-center gap-1.5">
        <span className="size-1.5 rounded-sm" style={{ backgroundColor: color }} />
        <span className="font-medium">{label}</span>
        <span className="text-fg-tertiary">· {date.slice(5)}</span>
        <span className="ml-auto font-semibold tnum">{rounds.length} 轮</span>
      </div>

      {/* 每轮明细 · 时刻 / 区 / 单价 / 号数 */}
      <div className="mt-2 space-y-1 border-t border-hairline pt-2">
        {rounds.map((r, i) => {
          const isMin = r.unit_price === min;
          const isMax = r.unit_price === max;
          return (
            <div key={i} className="flex items-baseline gap-2 tnum">
              <span className="w-10 shrink-0 text-fg-tertiary">{r.time.slice(11, 16)}</span>
              {r.zone && <span className="w-5 shrink-0 text-fg-tertiary">{r.zone}</span>}
              <span
                className={cn(
                  "w-16 shrink-0 font-semibold",
                  isMin && rounds.length > 1 && "text-ok-fg",
                  isMax && rounds.length > 1 && "text-danger-fg",
                )}
              >
                {toCredits(r.unit_price)} 积分
              </span>
              <span className="ml-auto text-fg-tertiary">{r.keys_count} 个号</span>
            </div>
          );
        })}
      </div>

      {/* 底 · 当天区间 */}
      {rounds.length > 1 && (
        <div className="mt-2 flex items-baseline justify-between border-t border-hairline pt-2 text-fg-tertiary">
          <span>当天区间</span>
          <span className="font-semibold tnum text-fg">
            {fmtCredits(min)} - {fmtCredits(max)} 积分
          </span>
        </div>
      )}
    </div>
  );
}
