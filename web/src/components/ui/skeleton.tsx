import { cn } from "@/lib/utils";

/** 骨架屏基元 · 一个灰块
 *
 * **用法原则**（别乱铺）：
 *   1. **只在首次加载铺**（`isLoading` 且没有旧数据）。刷新 / 翻页时保留旧数据 +
 *      局部 loading 更好 —— 骨架屏会让已经看到的内容闪成灰块，比等一下更难受。
 *   2. **形状要接近真实内容**。骨架屏的价值是"告诉你这里将出现什么、多大"，
 *      随便糊一片灰不如直接转圈。
 *   3. **别超过 3 秒**。超过说明该给的是进度或错误态，不是骨架。
 */
export function Skeleton({
  className,
  style,
}: {
  className?: string;
  /** 只用来给动态高度（图表骨架）· 别拿它写颜色 */
  style?: React.CSSProperties;
}) {
  return (
    <div
      className={cn("animate-pulse rounded-md bg-bg-elevated", className)}
      style={style}
      aria-hidden
    />
  );
}

/** KPI 卡骨架 · 对应 KpiCard（标签 + 大数字 + 副行） */
export function SkeletonKpi() {
  return (
    <div className="rounded-2xl border border-hairline bg-bg p-6">
      <Skeleton className="h-3 w-16" />
      <Skeleton className="mt-3 h-7 w-24" />
      <Skeleton className="mt-2 h-3 w-20" />
    </div>
  );
}

/** 表格骨架 · rows 行 · 每行高度跟 BareRow 一致
 *  cols 给列宽比例（默认 4 列均分）· 让灰块位置贴近真实表头 */
export function SkeletonTable({
  rows = 5,
  cols = ["w-1/3", "w-1/5", "w-1/5", "w-1/6"],
}: {
  rows?: number;
  cols?: string[];
}) {
  return (
    <div className="divide-y divide-hairline">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex items-center gap-4 py-3.5">
          {cols.map((w, j) => (
            <Skeleton key={j} className={cn("h-4", w)} />
          ))}
        </div>
      ))}
    </div>
  );
}

/** 卡片骨架 · 标题 + 几行内容 · 通用兜底 */
export function SkeletonCard({ lines = 3 }: { lines?: number }) {
  return (
    <div className="rounded-2xl border border-hairline bg-bg p-6">
      <Skeleton className="h-4 w-28" />
      <div className="mt-4 space-y-2.5">
        {Array.from({ length: lines }).map((_, i) => (
          <Skeleton
            key={i}
            className={cn("h-3.5", i === lines - 1 ? "w-2/3" : "w-full")}
          />
        ))}
      </div>
    </div>
  );
}

/** 图表骨架 · 高度跟真图一致（避免加载完跳位）
 *  柱状用竖条 · 折线/面积用一片灰 */
export function SkeletonChart({
  height = 260,
  bars = 0,
}: {
  height?: number;
  /** >0 = 画 N 根高低不一的竖条（柱状图）· 0 = 一整片（折线 / 面积图） */
  bars?: number;
}) {
  if (bars <= 0) {
    return <Skeleton className="w-full rounded-xl" style={{ height }} />;
  }
  // 固定的高度序列 —— 不用 random，否则每次 render 都跳
  const heights = [45, 70, 30, 85, 55, 65, 40, 75, 50, 60, 35, 80];
  return (
    <div
      className="flex items-end gap-1.5"
      style={{ height }}
      aria-hidden
    >
      {Array.from({ length: bars }).map((_, i) => (
        <Skeleton
          key={i}
          className="flex-1 rounded-sm"
          style={{ height: `${heights[i % heights.length]}%` }}
        />
      ))}
    </div>
  );
}

/** 列表项骨架 · 头像 + 两行文字（成员列表 / 活动流 用） */
export function SkeletonRows({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-3">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex items-center gap-3">
          <Skeleton className="size-9 shrink-0 rounded-full" />
          <div className="min-w-0 flex-1 space-y-1.5">
            <Skeleton className="h-3.5 w-1/4" />
            <Skeleton className="h-3 w-1/2" />
          </div>
          <Skeleton className="h-3.5 w-16 shrink-0" />
        </div>
      ))}
    </div>
  );
}
