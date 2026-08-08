import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { cn } from "@/lib/utils";

/* ─────────────── Card ─────────────── */

/* 传 to：整卡可点，自动带 hover 悬浮（可点区域 = 浮起区域，不能只让角上的链接可点） */
export function Card({
  focal,
  focalTone,
  hover,
  to,
  className,
  children,
}: {
  focal?: boolean;
  /** focal 卡的强调色。默认紫；积分/余额类用 "credit"（绿光，跟 header pill 统一） */
  focalTone?: "brand" | "credit";
  hover?: boolean;
  to?: string;
  className?: string;
  children: ReactNode;
}) {
  const focalCls = focalTone === "credit" ? "card-focal-credit" : "card-focal";
  const cls = cn(
    focal ? focalCls : "card",
    (hover || to) && "card-hover cursor-pointer",
    className,
  );

  if (to) {
    return (
      <Link to={to} className={cls}>
        {children}
      </Link>
    );
  }

  return <div className={cls}>{children}</div>;
}

/* ─────────────── 文字语义组件（避免到处写 text-*） ─────────────── */

/** 次要说明 · 12px 灰 */
export function Muted({
  className,
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <span className={cn("text-label text-fg-tertiary", className)}>{children}</span>
  );
}

/** 字段标签 · 12px 灰 + 半粗 */
export function Label({
  className,
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <span className={cn("text-label font-semibold text-fg-tertiary", className)}>
      {children}
    </span>
  );
}

/* ─────────────── Section 头 ─────────────── */

export function SectionHead({
  title,
  sub,
  right,
}: {
  title: string;
  /** 用 ReactNode 让 sub 里能嵌加粗数字 / <Num> 之类，不局限 string */
  sub?: ReactNode;
  right?: ReactNode;
}) {
  return (
    /* 响应式：窄屏 right 换到下面 · md+ 才并排 · gap 收紧避免挤压 */
    <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between md:gap-4">
      <div className="min-w-0 space-y-1">
        <h2 className="text-section font-semibold">{title}</h2>
        {sub && <p className="text-label text-fg-tertiary">{sub}</p>}
      </div>
      {right && <div className="shrink-0">{right}</div>}
    </div>
  );
}

/* ─────────────── 数字 + 单位（基线对齐） ─────────────── */

export function Stat({
  value,
  unit,
  size = "num",
  tone,
  className,
}: {
  value: string;
  unit?: string;
  size?: "stat" | "num" | "hero" | "giant";
  tone?: string;
  className?: string;
}) {
  const unitPad = size === "giant" ? "pb-2" : size === "hero" ? "pb-1.5" : "pb-1";
  return (
    <div className={cn("flex items-end gap-1.5", className)}>
      <span
        className={cn(`text-${size} font-semibold tnum`)}
        style={tone ? { color: tone } : undefined}
      >
        {value}
      </span>
      {unit && (
        <span className={cn("text-label font-medium text-fg-tertiary", unitPad)}>
          {unit}
        </span>
      )}
    </div>
  );
}

/* ─────────────── Chip / Badge ─────────────── */

type ChipTone = "ok" | "warn" | "danger" | "brand" | "neutral";

const CHIP: Record<ChipTone, string> = {
  ok: "bg-ok-bg text-ok-fg",
  warn: "bg-warn-bg text-warn-fg",
  danger: "bg-danger-bg text-danger-fg",
  brand: "bg-brand-subtle text-brand-strong",
  neutral: "bg-bg-elevated text-fg-tertiary",
};

export function Chip({
  tone = "neutral",
  dot,
  icon,
  children,
  className,
}: {
  tone?: ChipTone;
  dot?: boolean;
  icon?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <span
      className={cn(
        /* shrink-0 + whitespace-nowrap：flex 挤压时绝不变形（是 Chip 应有属性 · 别指望调用侧每处都加） */
        "inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md px-2 py-0.5 text-label font-semibold",
        CHIP[tone],
        className,
      )}
    >
      {dot && <span className="size-1.5 rounded-full bg-current" />}
      {icon}
      {children}
    </span>
  );
}

/** 号状态：活 / 已失效（只有两态） */
export function StatusChip({ alive }: { alive: boolean }) {
  return (
    <Chip tone={alive ? "ok" : "danger"} dot>
      {alive ? "活" : "已失效"}
    </Chip>
  );
}

/* ─────────────── 进度条 ─────────────── */

export function Meter({
  value,
  max,
  color,
  className,
}: {
  value: number;
  max: number;
  color: string;
  className?: string;
}) {
  const pct = Math.min(100, (value / max) * 100);
  return (
    <div className={cn("h-1 overflow-hidden rounded-full bg-hairline", className)}>
      <div
        className="h-full rounded-full transition-all"
        style={{ width: `${pct}%`, backgroundColor: color }}
      />
    </div>
  );
}

/* ─────────────── 分段控件（时间/维度切换） ─────────────── */

export function Segmented<T extends string>({
  options,
  value,
  onChange,
  solid,
}: {
  options: { value: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
  solid?: boolean;
}) {
  return (
    <div className="inline-flex gap-0.5 rounded-lg bg-bg-elevated p-0.5">
      {options.map((o) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            onClick={() => onChange(o.value)}
            className={cn(
              "rounded-[7px] px-3 py-1.5 text-label font-medium transition-colors",
              on
                ? solid
                  ? "bg-brand text-white font-semibold"
                  : "bg-bg text-fg font-semibold shadow-sm"
                : "text-fg-tertiary hover:text-fg-secondary",
            )}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}

/* ─────────────── 裸列表（无卡壳 · hairline 分隔） ─────────────── */

export function BareList({ children }: { children: ReactNode }) {
  return <div className="divide-y divide-hairline">{children}</div>;
}

export function BareRow({
  className,
  children,
  onClick,
}: {
  className?: string;
  children: ReactNode;
  onClick?: () => void;
}) {
  return (
    <div
      onClick={onClick}
      className={cn(
        "flex items-center gap-4 px-1 py-3.5",
        onClick && "cursor-pointer transition-colors hover:bg-bg-elevated/60",
        className,
      )}
    >
      {children}
    </div>
  );
}

/** 表头（跟 BareList 配套 · 中列居中 · 末列居右） */
export function BareHead({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-4 border-b border-hairline px-1 pb-2.5 text-label font-semibold text-fg-tertiary">
      {children}
    </div>
  );
}
