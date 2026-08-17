import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

/** 小号 badge 基础（10px · 高密度 · 表格 / 卡片内嵌） · 不给外部直接用，走下面语义组件 */
function MicroTag({
  className,
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center whitespace-nowrap rounded-lg px-1.5 py-[1px] text-[10px] font-medium leading-[1.4]",
        className,
      )}
    >
      {children}
    </span>
  );
}

/** 通用「有边 token 标签」· 中性灰底 · 行内嵌实体（vendor / bus 名 / 号池名）
 *
 *  只有 10px 一种尺寸（老 size="sm" 已废 · docs/13 §4）——
 *  行内 badge 全站统一小号 · 12px Chip 只留给行首主状态列 · 两种大小混一行会乱 */
export function TokenTag({ children }: { children: ReactNode }) {
  return (
    <MicroTag className="border border-hairline bg-bg-elevated text-fg-secondary shadow-card">
      {children}
    </MicroTag>
  );
}

/** Vendor 标签 · 语义包装 TokenTag（保留独立组件 · 未来若要给 vendor 加 icon 走这里） */
export function VendorTag({ name }: { name: string }) {
  return <TokenTag>{name}</TokenTag>;
}

/** "我发起" · 车 badge · 紫底强调
 *
 *  文案**走 i18n**（原来默认值写死中文 · 4 个调用点全没传 children ·
 *  于是英文用户到处看到"我发起"）· children 仍可覆盖（个别场景要换词） */
export function OwnerBadge({ children }: { children?: ReactNode }) {
  const { t } = useTranslation("buses");
  return (
    <MicroTag className="bg-brand-subtle font-semibold text-brand-strong">
      {children ?? t("card.owner-badge")}
    </MicroTag>
  );
}

/** 语义 micro badge · 数字变化 / 状态点缀（vs OwnerBadge · 这个是"数据"不是"身份"） */
export function MicroStat({
  tone,
  className,
  children,
}: {
  tone: "ok" | "warn" | "danger" | "brand" | "info" | "neutral";
  className?: string;
  children: ReactNode;
}) {
  const map = {
    ok: "bg-ok-bg text-ok-fg font-semibold",
    warn: "bg-warn-bg text-warn-fg font-semibold",
    danger: "bg-danger-bg text-danger-fg font-semibold",
    brand: "bg-brand-subtle text-brand-strong font-semibold",
    info: "bg-info-bg text-info-fg font-semibold",
    neutral: "bg-bg-elevated text-fg-secondary",
  };
  return <MicroTag className={cn(map[tone], className)}>{children}</MicroTag>;
}
