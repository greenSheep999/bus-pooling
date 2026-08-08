import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowDown, ArrowLeft, ArrowUp, Minus, X } from "lucide-react";
import { useVendorPrices } from "@/api/hooks";
import {
  BareHead, BareList, BareRow, Card, Chip, SectionHead, Segmented,
} from "@/components/ui/primitives";
import { Button } from "@/components/ui/button";
import { LoadMoreButton } from "@/components/ui/load-more-button";
import { PriceBoxPlot, RoundsTooltip } from "@/components/PriceBoxPlot";
import { cn, toCredits } from "@/lib/utils";
import type { Money, VendorPriceTrend, VendorRound } from "@/types";

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
/** 列表每次加载多少条 */
const PAGE = 20;

export default function Prices() {
  const [days, setDays] = useState<number>(30);
  const [zone, setZone] = useState<string>("us");
  const [hoveredVendor, setHoveredVendor] = useState<string | null>(null);
  const [hoveredDate, setHoveredDate] = useState<string | null>(null);
  /** 列表筛选 · 点图上某天设进来 */
  const [filter, setFilter] = useState<{ vendorId: string; date: string } | null>(null);
  const [shown, setShown] = useState(PAGE);
  const { data, isLoading } = useVendorPrices(days, zone);
  const trends = data?.trends ?? [];

  /* 按均价升序 · 便宜的排前面 */
  const sorted = useMemo(
    () => [...trends].sort((a, b) => a.price_avg - b.price_avg),
    [trends],
  );

  /* 日期刻度 · 位置按**竖条槽位中心**算（跟箱线图里的 X 网格严格对齐）
     不能用百分比均分 —— 那样跟竖条位置错开，日期对不上数据 */
  const dateTicks = useMemo(() => {
    const d = trends[0]?.days ?? [];
    if (d.length === 0) return [];
    const step = Math.max(1, Math.floor(d.length / 5));
    const slotPct = 100 / d.length;
    return d
      .map((x, i) => ({ date: x.date, i }))
      .filter(({ i }) => i % step === 0)
      .map(({ date, i }) => ({
        date,
        /* 槽位中心 = i * slot + slot/2 */
        leftPct: slotPct * i + slotPct / 2,
      }));
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

  /* hover 的那天在图区里的百分比位置 · 整体指示线用（贯穿 6 行） */
  const hoverLeftPct = useMemo(() => {
    const d = trends[0]?.days ?? [];
    if (!hoveredDate || d.length === 0) return null;
    const i = d.findIndex((x) => x.date === hoveredDate);
    if (i < 0) return null;
    const slotPct = 100 / d.length;
    return slotPct * i + slotPct / 2;
  }, [hoveredDate, trends]);

  /* hover 中的那天概要 · tooltip 用 */
  const hoveredRounds = useMemo(() => {
    if (!hoveredVendor || !hoveredDate) return null;
    const t = trends.find((x) => x.vendor_id === hoveredVendor);
    const d = t?.days.find((x) => x.date === hoveredDate);
    if (!t || !d) return null;
    return { label: t.vendor_label, date: d.date, rounds: d.rounds };
  }, [hoveredVendor, hoveredDate, trends]);

  /* ── 下方记录列表 · 每条 = 一轮车 ──
     无筛选 = 全部 vendor 混流按时间倒序（看最近发车动态）
     有筛选 = 只看那家那天（点图上某天来的） */
  const rows = useMemo(() => {
    const out: {
      key: string;
      vendorId: string;
      label: string;
      date: string;
      round: VendorRound;
      /** 那天该家的最低 / 最高价 · 标绿标红用 */
      dayMin: Money;
      dayMax: Money;
      dayRounds: number;
    }[] = [];
    for (const t of trends) {
      if (filter && t.vendor_id !== filter.vendorId) continue;
      for (const d of t.days) {
        if (filter && d.date !== filter.date) continue;
        if (d.rounds.length === 0) continue;
        const prices = d.rounds.map((r) => r.unit_price);
        const dayMin = Math.min(...prices);
        const dayMax = Math.max(...prices);
        for (let i = 0; i < d.rounds.length; i++) {
          out.push({
            key: `${t.vendor_id}-${d.date}-${i}`,
            vendorId: t.vendor_id,
            label: t.vendor_label,
            date: d.date,
            round: d.rounds[i],
            dayMin,
            dayMax,
            dayRounds: d.rounds.length,
          });
        }
      }
    }
    /* 时间倒序 · 最近的在前 */
    return out.sort((a, b) => b.round.time.localeCompare(a.round.time));
  }, [trends, filter]);

  const visible = rows.slice(0, shown);
  const remain = Math.max(0, rows.length - shown);

  /* 点图上某天 → 筛列表 + 重置分页 */
  const pickDay = (vendorId: string, date: string) => {
    setFilter((prev) =>
      prev && prev.vendorId === vendorId && prev.date === date
        ? null                                     // 再点一次取消
        : { vendorId, date },
    );
    setShown(PAGE);
  };

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
              <span className="w-[300px] shrink-0">vendor · {days} 天概况</span>
              <span className="min-w-0 flex-1">
                {days} 天价格分布 · 竖条高度 = 当天最低~最高轮价
              </span>
              <span className="w-24 shrink-0 text-right">{days} 天涨跌</span>
            </div>

            {/* 6 行 · 每行一家
                relative + 绝对定位的 X 网格层 · 竖线贯穿全部 6 行（不在每行 SVG 内部各画一遍） */}
            <div className="relative mb-1.5 divide-y divide-hairline">
              {/* X 网格层 · 只覆盖图区（左列 300px + gap 16px 之后，右列 96px + gap 16px 之前） */}
              <div
                className="pointer-events-none absolute inset-y-0 z-0"
                style={{ left: 316, right: 112 }}
                aria-hidden
              >
                {dateTicks.map(({ date, leftPct }) => (
                  <span
                    key={`grid-${date}`}
                    className="absolute inset-y-0 w-px"
                    style={{
                      left: `${leftPct}%`,
                      background:
                        "repeating-linear-gradient(to bottom,#EDEDED 0,#EDEDED 3px,transparent 3px,transparent 6px)",
                    }}
                  />
                ))}
              </div>

              {/* hover 指示线 · 也是整体层 · 贯穿 6 行（之前每行 SVG 内部各画一遍，跨行断） */}
              {hoverLeftPct != null && (
                <div
                  className="pointer-events-none absolute inset-y-0 z-20"
                  style={{ left: 316, right: 112 }}
                  aria-hidden
                >
                  <span
                    className="absolute inset-y-0 w-px"
                    style={{
                      left: `${hoverLeftPct}%`,
                      background:
                        `repeating-linear-gradient(to bottom,${ROW_COLOR}66 0,${ROW_COLOR}66 3px,transparent 3px,transparent 6px)`,
                    }}
                  />
                </div>
              )}

              {sorted.map((t) => (
                <VendorRow
                  key={t.vendor_id}
                  t={t}
                  tags={tags[t.vendor_id] ?? []}
                  dim={hoveredVendor != null && hoveredVendor !== t.vendor_id}
                  hoveredDate={hoveredVendor === t.vendor_id ? hoveredDate : null}
                  selectedDate={filter?.vendorId === t.vendor_id ? filter.date : null}
                  onEnter={() => setHoveredVendor(t.vendor_id)}
                  onLeave={() => { setHoveredVendor(null); setHoveredDate(null); }}
                  onHoverDate={setHoveredDate}
                  onSelectDate={(d) => pickDay(t.vendor_id, d)}
                />
              ))}
            </div>

            {/* 日期轴 · 刻度位置跟整体 X 网格层严格对齐（同一套 leftPct） */}
            <div className="flex gap-4 border-t border-hairline pt-1.5">
              <span className="w-[300px] shrink-0" />
              <div className="relative h-4 min-w-0 flex-1">
                {dateTicks.map(({ date, leftPct }) => (
                  <span
                    key={date}
                    className="absolute top-0 flex flex-col items-center"
                    style={{ left: `${leftPct}%`, transform: "translateX(-50%)" }}
                  >
                    <span className="h-1 w-px bg-hairline" />
                    <span className="mt-0.5 whitespace-nowrap text-label tnum text-fg-tertiary">
                      {date.slice(5).replace("-", "/")}
                    </span>
                  </span>
                ))}
              </div>
              <span className="w-24 shrink-0" />
            </div>

            {/* 图例 · 跟日期轴拉开距离（之前贴太近混在一起） */}
            <div className="mt-10 flex flex-wrap items-center justify-center gap-x-5 gap-y-2 border-t border-hairline pt-4 text-label text-fg-tertiary">
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

      {/* 发车记录 · 每条 = 一轮车 · 图上点某天可筛到那天 */}
      <div className="space-y-5">
        <SectionHead
          title="发车记录"
          sub={
            filter ? (
              <>
                {trends.find((t) => t.vendor_id === filter.vendorId)?.vendor_label} ·{" "}
                {filter.date.slice(5).replace("-", "/")} ·{" "}
                <span className="font-semibold tnum">{rows.length}</span> 轮
              </>
            ) : (
              <>
                {zone === "us" ? "美国区" : "欧洲区"} 全部 vendor ·{" "}
                <span className="font-semibold tnum">{rows.length}</span> 轮 · 按时间倒序 ·
                点上图某天可筛
              </>
            )
          }
          right={
            filter && (
              <Button variant="ghost" size="sm" onClick={() => { setFilter(null); setShown(PAGE); }}>
                <X />
                清除筛选
              </Button>
            )
          }
        />

        {rows.length === 0 ? (
          <div className="py-12 text-center text-label text-fg-tertiary">暂无发车记录</div>
        ) : (
          <>
            <div className="overflow-x-auto">
              <div className="min-w-[680px]">
                <BareHead>
                  <span className="w-[92px] shrink-0">时间</span>
                  <span className="min-w-0 flex-1">vendor</span>
                  <span className="w-14 shrink-0 text-center">区域</span>
                  <span className="w-24 shrink-0 text-right">单价</span>
                  <span className="w-20 shrink-0 text-right">产出</span>
                  <span className="w-24 shrink-0 text-right">当天位置</span>
                </BareHead>
                <BareList>
                  {visible.map((r) => (
                    <RoundRow
                      key={r.key}
                      r={r}
                      onPick={() => pickDay(r.vendorId, r.date)}
                    />
                  ))}
                </BareList>
              </div>
            </div>
            <LoadMoreButton
              onLoadMore={() => setShown((s) => s + PAGE)}
              remain={remain}
              remainUnit="轮"
            />
          </>
        )}
      </div>

      <p className="text-center text-label text-fg-tertiary">
        当前数据为演示用 mock · 真实数据将从上游轮次记录聚合
      </p>
    </div>
  );
}

/* ─────────────── 记录行 · 一轮车 ─────────────── */

function RoundRow({
  r, onPick,
}: {
  r: {
    label: string;
    date: string;
    round: VendorRound;
    dayMin: Money;
    dayMax: Money;
    dayRounds: number;
  };
  onPick: () => void;
}) {
  const isMin = r.dayRounds > 1 && r.round.unit_price === r.dayMin;
  const isMax = r.dayRounds > 1 && r.round.unit_price === r.dayMax;
  return (
    <BareRow onClick={onPick}>
      <span className="w-[92px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {r.date.slice(5).replace("-", "/")} {r.round.time.slice(11, 16)}
      </span>
      <span className="min-w-0 flex-1 truncate text-label font-medium">{r.label}</span>
      <span className="w-14 shrink-0 text-center text-label font-medium text-fg-secondary">
        {r.round.zone ?? "全区"}
      </span>
      <span
        className={cn(
          "w-24 shrink-0 text-right font-semibold tnum",
          isMin && "text-ok-fg",
          isMax && "text-danger-fg",
        )}
      >
        {toCredits(r.round.unit_price)}
        <span className="ml-0.5 font-medium text-fg-tertiary">积分</span>
      </span>
      <span className="w-20 shrink-0 text-right text-label tnum text-fg-secondary">
        {r.round.keys_count} 个号
      </span>
      <span className="w-24 shrink-0 text-right">
        {/* 那天有多轮时才标 · 让用户知道这轮是当天最便宜还是最贵 */}
        {isMin ? (
          <Chip tone="ok" className="text-[10px]">当天最低</Chip>
        ) : isMax ? (
          <Chip tone="danger" className="text-[10px]">当天最高</Chip>
        ) : r.dayRounds > 1 ? (
          <span className="text-label text-fg-tertiary">{r.dayRounds} 轮中</span>
        ) : (
          <span className="text-label text-fg-tertiary">唯一一轮</span>
        )}
      </span>
    </BareRow>
  );
}

/* ─────────────── 单行 ─────────────── */

function VendorRow({
  t, tags, dim, hoveredDate, selectedDate, onEnter, onLeave, onHoverDate, onSelectDate,
}: {
  t: VendorPriceTrend;
  tags: string[];
  dim: boolean;
  hoveredDate: string | null;
  selectedDate: string | null;
  onEnter: () => void;
  onLeave: () => void;
  onHoverDate: (d: string | null) => void;
  onSelectDate: (d: string) => void;
}) {
  return (
    <div
      className={cn(
        /* relative z-10 · 内容盖在贯穿全行的 X 网格层之上 */
        "relative z-10 flex items-center gap-4 py-2.5 transition-opacity",
        dim && "opacity-30",
      )}
      onMouseEnter={onEnter}
      onMouseLeave={onLeave}
    >
      {/* 左：名字行 + 数据行 · 每个数字都带 label 和单位（裸数字没人看得懂） */}
      <div className="w-[300px] shrink-0 space-y-1.5">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="truncate font-medium">{t.vendor_label}</span>
          {tags.map((tag) => (
            <Chip key={tag} tone={TAG_TONE[tag] ?? "neutral"} className="text-[10px]">
              {tag}
            </Chip>
          ))}
        </div>
        <div className="flex items-baseline gap-4 text-label text-fg-tertiary">
          <MiniStat
            label="均价"
            value={<>{toCredits(t.price_avg)} <span className="font-normal text-fg-tertiary">积分</span></>}
            emphasis
          />
          <MiniStat
            label="区间"
            value={`${toCredits(t.price_low)}-${toCredits(t.price_high)}`}
          />
          <MiniStat
            label="日均"
            value={<>{t.avg_rounds_per_day} <span className="font-normal text-fg-tertiary">轮</span></>}
          />
          <MiniStat
            label="断供"
            value={
              t.no_service_days > 0 ? (
                <span className="text-warn-fg">{t.no_service_days} 天</span>
              ) : (
                <span className="text-fg-tertiary">无</span>
              )
            }
          />
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
          selectedDate={selectedDate}
          onSelectDate={onSelectDate}
        />
      </div>

      {/* 右：涨跌 */}
      <span className="w-24 shrink-0 text-right">
        <PctBadge pct={t.change_30d_pct} />
      </span>
    </div>
  );
}

/** 迷你键值对 · label 在上、值在下 · 裸数字必须带 label 和单位才看得懂 */
function MiniStat({
  label, value, emphasis,
}: {
  label: string;
  value: React.ReactNode;
  emphasis?: boolean;
}) {
  return (
    <span className="flex flex-col gap-0.5">
      <span className="text-[10px] leading-none text-fg-tertiary">{label}</span>
      <span
        className={cn(
          "whitespace-nowrap font-semibold leading-none tnum",
          emphasis ? "text-fg" : "text-fg-secondary",
        )}
      >
        {value}
      </span>
    </span>
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
