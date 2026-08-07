import type { LucideIcon } from "lucide-react";
import { Card, Stat } from "./ui/primitives";
import { cn } from "@/lib/utils";

export function KpiCard({
  label,
  value,
  unit,
  sub,
  icon: Icon,
  focal,
  tone,
}: {
  label: string;
  value: string;
  unit?: string;
  sub?: string;
  icon: LucideIcon;
  focal?: boolean;
  tone?: "credit" | "danger";
}) {
  return (
    <Card focal={focal} className="p-6">
      <div className="flex items-start justify-between">
        <span className="text-micro font-semibold tracking-wide text-fg-tertiary">
          {label}
        </span>
        <span
          className={cn(
            "grid size-7 place-items-center rounded-lg",
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
      </div>

      <div className="mt-3">
        <Stat value={value} unit={unit} size="num" />
      </div>

      {sub && <p className="mt-2 text-micro text-fg-secondary">{sub}</p>}
    </Card>
  );
}
