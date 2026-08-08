import * as CollapsiblePrimitive from "@radix-ui/react-collapsible";
import { ChevronDown } from "lucide-react";
import * as React from "react";
import { cn } from "@/lib/utils";

/** shadcn/ui Collapsible · 基于 @radix-ui/react-collapsible
    项目里常用样式：外层 border + 头部 button + 内容 · 抽出 Panel 变体让业务直接用 */
const Collapsible = CollapsiblePrimitive.Root;
const CollapsibleTrigger = CollapsiblePrimitive.CollapsibleTrigger;
const CollapsibleContent = CollapsiblePrimitive.CollapsibleContent;

/** CollapsiblePanel · 项目风格封装 · title 一直可见 · 收起时可显示 subtitle 提示 · chevron 旋转
    默认样式跟当前风格一致（border-hairline · hover bg-elevated · rounded-xl） */
type CollapsiblePanelProps = Omit<
  React.ComponentPropsWithoutRef<typeof CollapsiblePrimitive.Root>,
  "title"
> & {
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  children: React.ReactNode;
};
const CollapsiblePanel = React.forwardRef<
  React.ElementRef<typeof CollapsiblePrimitive.Root>,
  CollapsiblePanelProps
>(({ title, subtitle, children, className, ...props }, ref) => (
  <Collapsible ref={ref} className={cn("overflow-hidden rounded-xl border border-hairline", className)} {...props}>
    <CollapsibleTrigger
      className={cn(
        "group flex w-full items-center justify-between gap-3 px-4 py-3 text-left transition-colors",
        "hover:bg-bg-elevated focus:outline-none focus-visible:bg-bg-elevated",
      )}
    >
      <span className="flex min-w-0 items-center gap-2 font-medium">
        <span>{title}</span>
        {subtitle && (
          <span className="min-w-0 truncate text-label font-normal text-fg-tertiary group-data-[state=open]:hidden">
            {subtitle}
          </span>
        )}
      </span>
      <ChevronDown className="size-4 shrink-0 text-fg-tertiary transition-transform group-data-[state=open]:rotate-180" />
    </CollapsibleTrigger>
    <CollapsibleContent
      className={cn(
        "overflow-hidden",
        "data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:slide-in-from-top-2",
        "data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:slide-out-to-top-2",
      )}
    >
      <div className="border-t border-hairline p-4">{children}</div>
    </CollapsibleContent>
  </Collapsible>
));
CollapsiblePanel.displayName = "CollapsiblePanel";

export { Collapsible, CollapsibleTrigger, CollapsibleContent, CollapsiblePanel };
