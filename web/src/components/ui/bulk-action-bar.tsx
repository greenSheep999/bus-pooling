import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** 批量操作悬浮栏 · 选中项后从底部滑出
 *  为什么悬浮：列表长了之后顶部按钮要滚回去才能点 · 悬浮栏始终在手边
 *  左侧给选中汇总（不只是数量 —— 让用户确认选对了）· 右侧给动作 */
export function BulkActionBar({
  open,
  count,
  onClear,
  children,
}: {
  open: boolean;
  /** 选中数量 */
  count: number;
  onClear: () => void;
  /** 动作按钮 */
  children: ReactNode;
}) {
  const { t } = useTranslation();
  return (
    <div
      className={cn(
        "pointer-events-none fixed inset-x-0 bottom-0 z-40 flex justify-center px-4 pb-6",
        "transition-all duration-200",
        open ? "translate-y-0 opacity-100" : "pointer-events-none translate-y-4 opacity-0",
      )}
      aria-hidden={!open}
    >
      <div
        className={cn(
          "pointer-events-auto flex max-w-[calc(100vw-32px)] flex-wrap items-center gap-x-4 gap-y-3",
          "rounded-2xl border border-hairline bg-bg/95 px-4 py-3 shadow-modal backdrop-blur-md",
        )}
      >
        {/* 选中数 · 圆形计数 · 不放汇总（vendor 数 / 额度这些看列表就行，悬浮栏只管操作） */}
        <span className="flex items-center gap-2.5">
          <span className="grid size-7 shrink-0 place-items-center rounded-full bg-brand text-label font-semibold tnum text-white">
            {count}
          </span>
          <span className="text-label font-semibold">已选 {count} 个</span>
        </span>

        <span className="h-5 w-px bg-hairline" />

        {/* 动作区 */}
        <span className="flex flex-wrap items-center gap-2">{children}</span>

        {/* 取消选择 */}
        <Button variant="ghost" size="icon" onClick={onClear} aria-label={t("ui.clear-selection")}>
          <X />
        </Button>
      </div>
    </div>
  );
}
