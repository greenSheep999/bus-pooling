import * as PopoverPrimitive from "@radix-ui/react-popover";
import * as React from "react";
import { cn } from "@/lib/utils";

/** shadcn/ui Popover · 基于 @radix-ui/react-popover · 统一 header 下拉 / scope picker / 立即拼车三选一 */
const Popover = PopoverPrimitive.Root;
const PopoverTrigger = PopoverPrimitive.Trigger;
const PopoverAnchor = PopoverPrimitive.Anchor;

const PopoverContent = React.forwardRef<
  React.ElementRef<typeof PopoverPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof PopoverPrimitive.Content>
>(({ className, align = "end", sideOffset = 8, ...props }, ref) => (
  <PopoverPrimitive.Portal>
    <PopoverPrimitive.Content
      ref={ref}
      align={align}
      sideOffset={sideOffset}
      className={cn(
        "z-50 rounded-2xl border border-hairline bg-bg p-2 shadow-pop outline-none",
        // 入场 · 沿用 dialog 那套 tailwindcss-animate
        "data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95",
        "data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
        // 方向偏移 · 从触发按钮方向滑入
        "data-[side=bottom]:slide-in-from-top-2 data-[side=top]:slide-in-from-bottom-2",
        "data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2",
        className,
      )}
      {...props}
    />
  </PopoverPrimitive.Portal>
));
PopoverContent.displayName = PopoverPrimitive.Content.displayName;

/** Popover 内的单个可选项（下拉菜单项）· 抽象 · 复用 hover / 选中态 */
export function PopoverItem({
  onSelect,
  children,
  className,
}: {
  onSelect?: () => void;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex w-full items-center gap-2 rounded-xl px-3 py-2 text-left transition-colors hover:bg-bg-elevated",
        "focus-visible:bg-bg-elevated focus-visible:outline-none",
        className,
      )}
    >
      {children}
    </button>
  );
}

/** Popover 内分组标签（section header · uppercase 小字灰） */
export function PopoverSectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-3 pb-1 pt-2 text-[10px] font-medium uppercase tracking-wider text-fg-tertiary">
      {children}
    </div>
  );
}

/** Popover 内分割线 */
export function PopoverSeparator() {
  return <div className="my-1 h-px bg-hairline" />;
}

export { Popover, PopoverTrigger, PopoverContent, PopoverAnchor };
