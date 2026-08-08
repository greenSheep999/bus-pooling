import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import * as React from "react";
import { cn } from "@/lib/utils";

/** shadcn/ui Button · CVA 变体
 *  - brand · 紫底 · 页面 hero CTA / 品牌一级操作（立即拼车 / 提取 key / 派去向）
 *  - primary · 黑底 · card / dialog / section 内的主动作（跟品牌紫区分开）
 *  - ghost · 灰边灰底 · 次要动作（取消 / 加载更多 / 查看全部）
 *  - subtle · 灰底填充 · 更次一级
 *  - danger · 红底 · 危险动作（解散）
 *  - link · 纯文字下划线
 *
 *  圆角规则：全部 rounded-xl（12px · 比旧 rounded-lg 8px 大一档，跟输入框 / 下拉 / 卡片内元素呼应）
 *  注意：header 内的 chip/pill 才是 rounded-full，页面按钮不是。
 */
const buttonVariants = cva(
  "inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-xl font-semibold transition-colors " +
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30 " +
    "disabled:pointer-events-none disabled:opacity-45 " +
    "[&_svg]:pointer-events-none [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        brand: "bg-brand text-white shadow-card hover:bg-brand-strong",
        primary: "bg-fg text-bg shadow-card hover:opacity-90",
        ghost: "border border-hairline bg-bg text-fg-secondary shadow-card hover:bg-bg-elevated",
        subtle: "bg-bg-elevated text-fg-secondary hover:bg-hairline",
        danger: "bg-danger-solid text-white shadow-card hover:bg-danger-fg",
        link: "rounded-none text-brand-strong underline-offset-4 hover:underline",
      },
      size: {
        sm: "h-8 px-3 text-label [&_svg]:size-3.5",
        md: "h-10 px-4 [&_svg]:size-4",
        lg: "h-11 px-5 [&_svg]:size-4",
        icon: "size-10 [&_svg]:size-4",
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
