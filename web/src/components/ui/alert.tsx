import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/** Alert · 图标 + 标题 + 描述的提示卡 · 4 tone
 *  统一 5+ 处散写：StartCarpoolModal / PullNowModal / PullExtractModal / AssignModal / Extract 里的提示块
 *  - ok · 好消息（已省 · 已完成）
 *  - warn · 提醒（未配置 · 需要注意）
 *  - danger · 警告（唯一可见 · 不可逆动作）
 *  - neutral · 中性引导（改成 X 更划算）
 */
type AlertTone = "ok" | "warn" | "danger" | "neutral" | "brand";

/** Alert 底色跟 Chip 同思路 · solid 色低透明覆盖
 *   浅色下 solid/8 ~ 淡色底 · 深色下 solid/12 ~ 深底微透彩色
 *   前景 fg 变量两模式都用 · 见 CLAUDE.md §视觉 - 深色下不用高饱和实色底 */
const TONE: Record<AlertTone, { wrap: string; icon: string; title: string }> = {
  ok: {
    wrap: "bg-ok-solid/10 dark:bg-ok-solid/[.14]",
    icon: "text-ok-fg",
    title: "text-ok-fg",
  },
  warn: {
    wrap: "bg-warn-solid/10 dark:bg-warn-solid/[.14] border border-warn-solid/20",
    icon: "text-warn-fg",
    title: "text-warn-fg",
  },
  danger: {
    wrap: "bg-danger-solid/10 dark:bg-danger-solid/[.14]",
    icon: "text-danger-fg",
    title: "text-danger-fg",
  },
  neutral: {
    wrap: "border border-hairline bg-bg-elevated",
    icon: "text-fg-tertiary",
    title: "text-fg",
  },
  brand: {
    wrap: "bg-brand/8 dark:bg-brand/[.14]",
    icon: "text-brand-strong",
    title: "text-brand-strong",
  },
};

export function Alert({
  tone = "neutral",
  icon: Icon,
  title,
  children,
  className,
}: {
  tone?: AlertTone;
  icon?: LucideIcon;
  title?: ReactNode;
  children?: ReactNode;
  className?: string;
}) {
  const t = TONE[tone];
  return (
    <div
      className={cn(
        "flex items-start gap-2 rounded-xl p-3 text-label",
        t.wrap,
        className,
      )}
    >
      {Icon && <Icon className={cn("mt-0.5 size-4 shrink-0", t.icon)} />}
      <div className="min-w-0 flex-1">
        {title && <div className={cn("font-semibold", t.title)}>{title}</div>}
        {children && <div className="text-fg-secondary">{children}</div>}
      </div>
    </div>
  );
}
