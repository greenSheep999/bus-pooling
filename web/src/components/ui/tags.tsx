import type { ReactNode } from "react";
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

/** 通用「有边 token 标签」· 中性灰底 · 句子中嵌入实体（vendor / bus 名 / 号池名）
 *  - micro（默认）· 10px · 表格 / 列表内嵌
 *  - sm · 12px · 句子中嵌入
 */
export function TokenTag({
  children,
  size = "micro",
}: {
  children: ReactNode;
  size?: "micro" | "sm";
}) {
  if (size === "sm") {
    return (
      <span className="inline-flex shrink-0 items-center whitespace-nowrap rounded-lg border border-hairline bg-bg-elevated px-2 py-[2px] text-label font-medium leading-[1.4] text-fg-secondary shadow-card">
        {children}
      </span>
    );
  }
  return (
    <MicroTag className="border border-hairline bg-bg-elevated text-fg-secondary shadow-card">
      {children}
    </MicroTag>
  );
}

/** Vendor 标签 · 语义包装 TokenTag（保留独立组件 · 未来若要给 vendor 加 icon 走这里） */
export function VendorTag({
  name,
  size = "micro",
}: {
  name: string;
  size?: "micro" | "sm";
}) {
  return <TokenTag size={size}>{name}</TokenTag>;
}

/** "我发起" · 车 badge · 紫底强调 */
export function OwnerBadge({ children = "我发起" }: { children?: ReactNode }) {
  return (
    <MicroTag className="bg-brand-subtle font-semibold text-brand-strong">
      {children}
    </MicroTag>
  );
}

/** 语义 micro badge · 数字变化 / 状态点缀（vs OwnerBadge · 这个是"数据"不是"身份"） */
export function MicroStat({
  tone,
  children,
}: {
  tone: "ok" | "warn" | "danger" | "brand" | "neutral";
  children: ReactNode;
}) {
  const map = {
    ok: "bg-ok-bg text-ok-fg font-semibold",
    warn: "bg-warn-bg text-warn-fg font-semibold",
    danger: "bg-danger-bg text-danger-fg font-semibold",
    brand: "bg-brand-subtle text-brand-strong font-semibold",
    neutral: "bg-bg-elevated text-fg-secondary",
  };
  return <MicroTag className={map[tone]}>{children}</MicroTag>;
}
