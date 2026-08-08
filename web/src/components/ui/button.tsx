import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import * as React from "react";
import { cn } from "@/lib/utils";

/** shadcn/ui Button · CVA 变体 · 保持当前项目视觉风格
    - primary（紫 CTA）· ghost（灰边）· danger（红危险动作）· link（纯文字）· subtle（灰底次要动作） */
const buttonVariants = cva(
  "inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-lg font-semibold transition-colors " +
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30 " +
    "disabled:pointer-events-none disabled:opacity-45 " +
    "[&_svg]:pointer-events-none [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        primary: "bg-brand text-white shadow-card hover:opacity-90",
        ghost: "border border-hairline bg-bg text-fg-secondary shadow-card hover:bg-bg-elevated",
        subtle: "bg-bg-elevated text-fg-secondary hover:bg-hairline",
        danger: "bg-danger-fg text-white shadow-card hover:opacity-90",
        link: "text-brand-strong underline-offset-4 hover:underline",
      },
      size: {
        sm: "h-8 px-3 text-label [&_svg]:size-3.5",
        md: "h-10 px-4 [&_svg]:size-4",
        lg: "h-11 px-5 [&_svg]:size-4",
        icon: "size-9 [&_svg]:size-4",
      },
    },
    defaultVariants: {
      variant: "primary",
      size: "md",
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    );
  },
);
Button.displayName = "Button";

export { Button, buttonVariants };
