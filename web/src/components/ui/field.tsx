import * as React from "react";
import { cn } from "@/lib/utils";

/** 表单字段容器 · label + hint + children + error
    hint 采用 placeholder 优先原则 · label 里不塞括号说明
    真需要额外说明时 hint 靠右显示 · flex-wrap 允许长 label 自动换行 */
export function Field({
  label,
  hint,
  error,
  children,
  className,
}: {
  label?: React.ReactNode;
  hint?: React.ReactNode;
  error?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("space-y-1.5", className)}>
      {(label || hint) && (
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
          {label && (
            <span className="text-label font-semibold text-fg-secondary">{label}</span>
          )}
          {hint && (
            <span className="text-label text-fg-tertiary">{hint}</span>
          )}
        </div>
      )}
      {children}
      {error && (
        <p className="text-label font-medium text-danger-fg">{error}</p>
      )}
    </div>
  );
}
