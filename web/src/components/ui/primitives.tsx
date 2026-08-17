import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
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

/* ─────────────── 描述里的重点（全站唯一写法） ─────────────── */

/** 描述文字里被强调的数字 / 名字 · 页面 hero 和卡片副标题都用它
 *
 *  规则（别再各页手写 span · 之前概览用 text-fg、价格和车详情用 text-fg-secondary，不一致）：
 *  - 默认 `text-fg`（近黑）· 描述本体是 text-fg-tertiary，重点靠这个跳出来
 *  - `tone="spend"` 花掉的钱 → 红 · `tone="ok"` 好消息 → 绿 · `tone="warn"` 要注意 → 黄
 *  - `tnum` 默认开（等宽数字，数字跳动时不抖）· 强调的是名字而非数字时传 `plain` 关掉
 */
export function Em({
  children,
  tone,
  plain,
  className,
}: {
  // 老代码经常在 <Trans/> 的 components 里传空 <Em />，让 react-i18next 塞子节点 ·
  // 所以 children 走 optional
  children?: ReactNode;
  tone?: "spend" | "ok" | "warn";
  /** 强调的是名字 / 文字（不是数字）· 关掉 tabular-nums */
  plain?: boolean;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "font-semibold",
        !plain && "tnum",
        tone === "spend" ? "text-danger-fg"
          : tone === "ok" ? "text-ok-fg"
            : tone === "warn" ? "text-warn-fg"
              : "text-fg",
        className,
      )}
    >
      {children}
    </span>
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

type ChipTone = "ok" | "warn" | "danger" | "brand" | "info" | "neutral";

/** Chip 底色 · 用 solid 色的低透明覆盖（不用固定 bg-*-bg）
 *   浅色下 solid/10 ≈ 极浅底 · 深色下 solid/15 ≈ 深底微透彩色
 *   字继续走 fg 变量 · 两模式都够对比度
 *   见 CLAUDE.md §视觉 - 深色下不用高饱和实色底 */
const CHIP: Record<ChipTone, string> = {
  ok: "bg-ok-solid/10 text-ok-fg dark:bg-ok-solid/[.15]",
  warn: "bg-warn-solid/10 text-warn-fg dark:bg-warn-solid/[.15]",
  danger: "bg-danger-solid/10 text-danger-fg dark:bg-danger-solid/[.15]",
  brand: "bg-brand/10 text-brand-strong dark:bg-brand/[.15]",
  info: "bg-info-solid/10 text-info-fg dark:bg-info-solid/[.15]",
  neutral: "bg-fg/[.06] text-fg-tertiary dark:bg-fg/[.10]",
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
  // 允许空 Chip 只显示 dot / icon（无 label 场景）· 老代码大量在用
  children?: ReactNode;
  className?: string;
}) {
  return (
    <span
      className={cn(
        /* shrink-0 + whitespace-nowrap：flex 挤压时绝不变形（是 Chip 应有属性 · 别指望调用侧每处都加） */
        "inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-lg px-2 py-0.5 text-label font-semibold",
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

/** 号状态：正常 / 已失效（只有两态） · CLAUDE.md §12.5 */
export function StatusChip({ alive }: { alive: boolean }) {
  const { t } = useTranslation();
  return (
    <Chip tone={alive ? "ok" : "danger"} dot>
      {alive ? t("ui.cred-alive") : t("ui.cred-dead")}
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
    <div className="inline-flex gap-0.5 rounded-xl bg-bg-elevated p-0.5">
      {options.map((o) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            onClick={() => onChange(o.value)}
            className={cn(
              "rounded-lg px-3 py-1.5 text-label font-medium transition-colors",
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
  onMouseEnter,
  onMouseLeave,
}: {
  className?: string;
  children: ReactNode;
  onClick?: () => void;
  onMouseEnter?: () => void;
  onMouseLeave?: () => void;
}) {
  return (
    <div
      onClick={onClick}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
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

/** 表头（跟 BareList 配套 · 中列居中 · 末列居右）
 *  `[&+*]:!border-t-0`：head 嵌在 BareList 里时，divide-y 会给紧跟它的那行加 border-t，
 *  跟 head 自己的 border-b 贴成 2px。把后一行的顶边去掉 —— head 放里放外都恒 1px。
 *  必须带 `!`：divide-y 生成 `.divide-y > :not([hidden]) ~ :not([hidden])`（特异度 0,3,0），
 *  普通 `[&+*]` 只有 0,1,0，赢不了 */
export function BareHead({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-4 border-b border-hairline px-1 pb-2.5 text-label font-semibold text-fg-tertiary [&+*]:!border-t-0">
      {children}
    </div>
  );
}
