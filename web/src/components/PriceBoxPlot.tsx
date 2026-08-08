import { useMemo, useState } from "react";
import { toCredits } from "@/lib/utils";
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
  selectedDate,
  onSelectDate,
}: {
  days: VendorDayRounds[];
  color: string;
  height?: number;
  hoveredDate: string | null;
  onHoverDate: (d: string | null) => void;
  /** 已选中的那天 · 下方列表正在筛这天 */
  selectedDate?: string | null;
  /** 点某天 → 下方列表筛到该 vendor + 该天 */
  onSelectDate?: (d: string) => void;
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
      {/* Y 网格 · 该行的价格参考线（每行 Y 域独立，所以必须画在行内）
          X 网格不在这画 —— 那是贯穿 6 行的整体背景层，见 Prices.tsx GridOverlay */}
      {[0, 0.5, 1].map((f) => (
        <line
          key={`y${f}`}
          x1={0}
          x2={w}
          y1={height * f}
          y2={height * f}
          stroke="#F2F2F2"
          strokeWidth={1}
          strokeDasharray={f === 0.5 ? "4 4" : "0"}
        />
      ))}

      {days.map((d, i) => {
        const cx = slot * i + slot / 2;
        const hovered = hoveredDate === d.date;
        const selected = selectedDate === d.date;
        const lit = hovered || selected;

        /* 整列命中区 · 点了筛下方列表 */
        const hit = (
          <rect
            x={cx - slot / 2}
            y={0}
            width={slot}
            height={height}
            fill="transparent"
            className={onSelectDate ? "cursor-pointer" : undefined}
            onClick={() => onSelectDate?.(d.date)}
          />
        );

        /* 没发车 · 灰点在中线 */
        if (d.rounds.length === 0) {
          return (
            <g key={d.date} onMouseEnter={() => onHoverDate(d.date)}>
              {hit}
              <circle cx={cx} cy={height / 2} r={lit ? 2.5 : 1.5} fill="#D4D4D8" />
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
              {hit}
              <line
                x1={cx - barW / 2}
                x2={cx + barW / 2}
                y1={y(min)}
                y2={y(min)}
                stroke={color}
                strokeWidth={lit ? 3 : 2}
                strokeOpacity={lit ? 1 : opacity}
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
            {hit}
            <rect
              x={cx - barW / 2}
              y={yTop}
              width={barW}
              height={Math.max(2, yBot - yTop)}
              rx={barW / 2}
              fill={color}
              fillOpacity={lit ? 1 : opacity}
              stroke={lit ? color : "none"}
              strokeWidth={lit ? 1.5 : 0}
            />
            {/* 选中态 · 加个外圈让它在列表筛选时可辨认 */}
            {selected && (
              <rect
                x={cx - barW / 2 - 2.5}
                y={yTop - 2.5}
                width={barW + 5}
                height={Math.max(2, yBot - yTop) + 5}
                rx={(barW + 5) / 2}
                fill="none"
                stroke={color}
                strokeWidth={1}
                strokeOpacity={0.4}
              />
            )}
          </g>
        );
      })}

      {/* hover 指示线不在这画 —— 那也是贯穿 6 行的整体层，见 Prices.tsx */}
    </svg>
  );
}

/** 当天概要 tooltip · 只给"轮数 + 区间 + 均价"
 *  每轮明细**不放这里** —— 下沉到页面下方的记录列表（信息分层） */
export function RoundsTooltip({
  date, rounds, label, color,
}: {
  date: string;
  rounds: VendorRound[];
  label: string;
  color: string;
}) {
  const head = (
    <div className="flex items-center gap-1.5">
      <span className="size-1.5 rounded-sm" style={{ backgroundColor: color }} />
      <span className="font-medium">{label}</span>
      <span className="text-fg-tertiary">· {date.slice(5).replace("-", "/")}</span>
    </div>
  );

  if (rounds.length === 0) {
    return (
      <div className="rounded-xl border border-hairline bg-bg px-3 py-2 text-label shadow-pop">
        {head}
        <div className="mt-1 text-fg-tertiary">那天没发车</div>
      </div>
    );
  }

  const prices = rounds.map((r) => r.unit_price);
  const min = Math.min(...prices);
  const max = Math.max(...prices);
  const avg = Math.round(prices.reduce((a, b) => a + b, 0) / prices.length);

  return (
    <div className="min-w-[180px] rounded-xl border border-hairline bg-bg px-3 py-2.5 text-label shadow-pop">
      {head}
      <div className="mt-2 space-y-1 border-t border-hairline pt-2">
        <TipRow label="发车" value={<><strong className="tnum">{rounds.length}</strong> 轮</>} />
        <TipRow
          label="区间"
          value={
            min === max ? (
              <><strong className="tnum">{toCredits(min)}</strong> 积分</>
            ) : (
              <>
                <strong className="tnum">{toCredits(min)}</strong>
                {" - "}
                <strong className="tnum">{toCredits(max)}</strong> 积分
              </>
            )
          }
        />
        <TipRow label="均价" value={<><strong className="tnum">{toCredits(avg)}</strong> 积分</>} />
      </div>
      <div className="mt-2 border-t border-hairline pt-1.5 text-fg-tertiary">
        点一下 · 下方看每轮明细
      </div>
    </div>
  );
}

function TipRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <span className="text-fg-tertiary">{label}</span>
      <span className="text-fg">{value}</span>
    </div>
  );
}
