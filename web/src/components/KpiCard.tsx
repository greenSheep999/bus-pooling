import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Card, Label, Muted, Stat } from "./ui/primitives";
import { cn } from "@/lib/utils";

export function KpiCard({
  label,
  value,
  unit,
  sub,
  subRight,
  icon: Icon,
  focal,
  tone,
}: {
  label: string;
  value: string;
  unit?: string;
  /** 同类数据（跟主数字同量纲，如"昨日 32"） */
  sub?: ReactNode;
  /** 异类数据（比率 / 时长等，靠右另起一栏） */
  subRight?: ReactNode;
  icon: LucideIcon;
  focal?: boolean;
  tone?: "credit" | "danger";
}) {
  return (
    <Card focal={focal} focalTone={tone === "credit" ? "credit" : "brand"} className="p-6">
      {/* 图标跟 label 并排（不钉右上角 · 跟下面 3 业务卡的头部同款） */}
      <div className="flex items-center gap-2.5">
        <span
          className={cn(
            "grid size-7 shrink-0 place-items-center rounded-lg",
            tone === "credit" ? "bg-credit-bg" : "bg-bg-elevated",
          )}
        >
          <Icon
            className={cn(
              "size-3.5",
              tone === "credit" ? "text-credit-fg" : "text-fg-secondary",
            )}
          />
        </span>
        <Label className="tracking-wide">{label}</Label>
      </div>

      <div className="mt-3">
        <Stat value={value} unit={unit} size="num" />
      </div>

      {(sub || subRight) && (
        <div className="mt-2 flex items-baseline justify-between gap-2">
          <Muted className="text-fg-secondary">{sub}</Muted>
          {subRight && <Muted className="text-fg-secondary">{subRight}</Muted>}
        </div>
      )}
    </Card>
  );
}
