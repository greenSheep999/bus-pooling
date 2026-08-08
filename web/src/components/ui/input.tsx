import * as React from "react";
import { cn } from "@/lib/utils";

/** shadcn/ui Input · 保持当前项目视觉风格（跟原 TextField 一致）
    - 40px 高 · rounded-lg · border-hairline · focus 紫 · disabled 灰
    - 用 CSS 全局隐藏了 number spinner 箭头（见 index.css） */
export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  /** 数字类字段自动加 tnum + font-semibold */
  tnum?: boolean;
}

const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, type, tnum, ...props }, ref) => {
    // number 类型自动应用 tnum
    const isNumeric = tnum || type === "number";
    return (
      <input
        type={type}
        ref={ref}
        className={cn(
          "flex h-10 w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-medium transition-colors",
          "placeholder:text-fg-tertiary",
          "focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand/20",
          "disabled:cursor-not-allowed disabled:bg-bg-elevated disabled:text-fg-tertiary",
          isNumeric && "tnum font-semibold",
          className,
        )}
        {...props}
      />
    );
  },
);
Input.displayName = "Input";

export { Input };
