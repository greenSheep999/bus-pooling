import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import { Check } from "lucide-react";
import * as React from "react";
import { cn } from "@/lib/utils";

/** shadcn/ui Checkbox · 基于 @radix-ui/react-checkbox · 保持当前项目视觉风格
    未勾选灰边框圆角小方块 · 勾选紫底白√ · focus 紫 ring */
const Checkbox = React.forwardRef<
  React.ElementRef<typeof CheckboxPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof CheckboxPrimitive.Root>
>(({ className, ...props }, ref) => (
  <CheckboxPrimitive.Root
    ref={ref}
    className={cn(
      "peer size-[18px] shrink-0 rounded-[5px] border border-hairline bg-bg transition-colors",
      "hover:border-fg-tertiary",
      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30",
      "disabled:cursor-not-allowed disabled:opacity-50",
      "data-[state=checked]:border-brand data-[state=checked]:bg-brand data-[state=checked]:text-white",
      className,
    )}
    {...props}
  >
    <CheckboxPrimitive.Indicator className="grid place-items-center text-current">
      <Check className="size-3" strokeWidth={3} />
    </CheckboxPrimitive.Indicator>
  </CheckboxPrimitive.Root>
));
Checkbox.displayName = CheckboxPrimitive.Root.displayName;

export { Checkbox };
