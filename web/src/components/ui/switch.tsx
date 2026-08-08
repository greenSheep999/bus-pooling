import * as SwitchPrimitive from "@radix-ui/react-switch";
import * as React from "react";
import { cn } from "@/lib/utils";

/** shadcn/ui Switch · 基于 @radix-ui/react-switch · 保持当前项目视觉风格
    未开 = hairline 灰底 · 开 = brand 紫底 · 白色圆点滑块 */
const Switch = React.forwardRef<
  React.ElementRef<typeof SwitchPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof SwitchPrimitive.Root>
>(({ className, ...props }, ref) => (
  <SwitchPrimitive.Root
    ref={ref}
    className={cn(
      "peer inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors",
      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30",
      "disabled:cursor-not-allowed disabled:opacity-50",
      "data-[state=checked]:bg-brand data-[state=unchecked]:bg-hairline",
      className,
    )}
    {...props}
  >
    <SwitchPrimitive.Thumb
      className={cn(
        "pointer-events-none block size-5 rounded-full bg-white shadow-card transition-transform",
        "data-[state=checked]:translate-x-[22px] data-[state=unchecked]:translate-x-0.5",
      )}
    />
  </SwitchPrimitive.Root>
));
Switch.displayName = SwitchPrimitive.Root.displayName;

export { Switch };
