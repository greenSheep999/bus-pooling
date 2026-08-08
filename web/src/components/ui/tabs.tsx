import * as TabsPrimitive from "@radix-ui/react-tabs";
import * as React from "react";
import { cn } from "@/lib/utils";

/** shadcn/ui Tabs · 基于 @radix-ui/react-tabs · 完全 mirror shadcn 官方新版（apps/v4）
 *  切换动画 = transition-all：白底 / 阴影 / 文字颜色**同时平滑过渡**（不是瞬间切换）*/
const Tabs = TabsPrimitive.Root;

const TabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.List
    ref={ref}
    className={cn(
      "inline-flex h-10 w-fit items-center justify-center rounded-xl bg-bg-elevated p-[3px] text-fg-tertiary",
      className,
    )}
    {...props}
  />
));
TabsList.displayName = TabsPrimitive.List.displayName;

const TabsTrigger = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Trigger
    ref={ref}
    className={cn(
      "inline-flex h-[calc(100%-1px)] flex-1 items-center justify-center gap-1.5 rounded-lg border border-transparent px-3.5 py-1 text-label font-medium whitespace-nowrap",
      /* 关键：transition-all —— 让 bg / text / shadow / border 一起平滑过渡（shadcn 官方就是靠这一句做出"淡入淡出"式切换的） */
      "transition-all",
      "hover:text-fg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30",
      "disabled:pointer-events-none disabled:opacity-45",
      /* 选中态：白底 + 深字 + 阴影 · 所有属性走 transition-all 平滑过渡 */
      "data-[state=active]:bg-bg data-[state=active]:text-fg data-[state=active]:font-semibold data-[state=active]:shadow-sm",
      className,
    )}
    {...props}
  />
));
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName;

const TabsContent = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content
    ref={ref}
    className={cn("flex-1 outline-none focus-visible:outline-none", className)}
    {...props}
  />
));
TabsContent.displayName = TabsPrimitive.Content.displayName;

export { Tabs, TabsList, TabsTrigger, TabsContent };
