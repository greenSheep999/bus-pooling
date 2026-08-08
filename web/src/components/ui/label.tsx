import * as LabelPrimitive from "@radix-ui/react-label";
import * as React from "react";
import { cn } from "@/lib/utils";

/** shadcn/ui Label · 保持当前项目视觉（跟原 primitives.Label 一致 · text-label + fg-tertiary + tracking-wide） */
const Label = React.forwardRef<
  React.ElementRef<typeof LabelPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root>
>(({ className, ...props }, ref) => (
  <LabelPrimitive.Root
    ref={ref}
    className={cn(
      "text-label font-semibold text-fg-secondary tracking-wide",
      "peer-disabled:cursor-not-allowed peer-disabled:opacity-70",
      className,
    )}
    {...props}
  />
));
Label.displayName = LabelPrimitive.Root.displayName;

export { Label };
