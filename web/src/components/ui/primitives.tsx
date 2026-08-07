import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/* ─────────────── Card ─────────────── */

export function Card({
  focal,
  hover,
  className,
  children,
}: {
  focal?: boolean;
  hover?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div
      className={cn(
        focal ? "card-focal" : "card",
        hover && "card-hover cursor-pointer",
        className,
      )}
    >
      {children}
    </div>
  );
}

/* ─────────────── Section 头 ─────────────── */

export function SectionHead({
  title,
  sub,
  right,
}: {
  title: string;
  sub?: string;
  right?: ReactNode;
}) {
  return (
    <div className="flex items-end justify-between">
      <div className="space-y-1">
        <h2 className="text-section font-semibold">{title}</h2>
        {sub && <p className="text-micro text-fg-tertiary">{sub}</p>}
      </div>
      {right}
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
        <span className={cn("text-micro font-medium text-fg-tertiary", unitPad)}>
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
        "inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-micro font-semibold",
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
              "rounded-[7px] px-3 py-1.5 text-micro font-medium transition-colors",
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

/* ─────────────── Button ─────────────── */

export function Button({
  variant = "ghost",
  size = "md",
  icon,
  children,
  className,
  ...rest
}: {
  variant?: "primary" | "ghost" | "danger";
  size?: "sm" | "md";
  icon?: ReactNode;
  children?: ReactNode;
} & React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...rest}
      className={cn(
        "inline-flex items-center justify-center gap-2 rounded-xl font-semibold transition-all",
        size === "sm" ? "px-3.5 py-2 text-micro" : "px-4 py-2.5 text-body",
        variant === "primary" &&
          "bg-brand text-white shadow-[0_8px_20px_-2px_rgb(145_71_255/0.28)] hover:brightness-110 active:scale-[0.98]",
        variant === "ghost" &&
          "border border-hairline bg-bg text-fg-secondary hover:bg-bg-elevated",
        variant === "danger" &&
          "border border-danger-fg/40 bg-bg text-danger-fg hover:bg-danger-bg",
        className,
      )}
    >
      {icon}
      {children}
    </button>
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
    <div className="flex items-center gap-4 border-b border-hairline px-1 pb-2.5 text-micro font-semibold text-fg-tertiary">
      {children}
    </div>
  );
}
