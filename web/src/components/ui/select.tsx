import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown, ChevronUp } from "lucide-react";
import * as React from "react";
import { cn } from "@/lib/utils";

/** shadcn/ui Select · 基于 @radix-ui/react-select · 官方风格 */
const Select = SelectPrimitive.Root;
const SelectGroup = SelectPrimitive.Group;
const SelectValue = SelectPrimitive.Value;

/** SelectTrigger 支持 hint prop:在右侧(chevron 前)放一段灰字(如"暂时缺货")
 *  文本左对齐 · SelectValue 主内容跟以前一样在最左边 · hint 灰字紧贴 chevron 之前 */
type SelectTriggerProps = React.ComponentPropsWithoutRef<typeof SelectPrimitive.Trigger> & {
  hint?: React.ReactNode;
};

const SelectTrigger = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Trigger>,
  SelectTriggerProps
>(({ className, children, hint, ...props }, ref) => (
  <SelectPrimitive.Trigger
    ref={ref}
    className={cn(
      "flex h-10 w-full items-center justify-between gap-2 rounded-xl border border-hairline bg-bg px-3 py-2 font-medium",
      "transition-colors placeholder:text-fg-tertiary",
      "focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand/20",
      "disabled:cursor-not-allowed disabled:bg-bg-elevated disabled:text-fg-tertiary",
      "[&>span]:line-clamp-1",
      "data-[placeholder]:text-fg-tertiary",
      className,
    )}
    {...props}
  >
    {/* §9.1 选中态明确:值靠左 + 缺货状态 pill 紧跟 · chevron 靠右 */}
    <span className="flex min-w-0 items-center gap-2">
      {children}
      {hint ? (
        /* §4.3 状态小 pill · 跟下拉项里同一个视觉 */
        <span className="inline-flex shrink-0 items-center rounded-md bg-warn-bg px-1.5 py-[1px] text-[10px] font-semibold leading-[1.4] text-warn-fg">
          {hint}
        </span>
      ) : null}
    </span>
    <SelectPrimitive.Icon asChild>
      <ChevronDown className="size-4 shrink-0 text-fg-tertiary transition-transform data-[state=open]:rotate-180" />
    </SelectPrimitive.Icon>
  </SelectPrimitive.Trigger>
));
SelectTrigger.displayName = SelectPrimitive.Trigger.displayName;

const SelectScrollUpButton = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.ScrollUpButton>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.ScrollUpButton>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.ScrollUpButton
    ref={ref}
    className={cn("flex cursor-default items-center justify-center py-1", className)}
    {...props}
  >
    <ChevronUp className="size-4" />
  </SelectPrimitive.ScrollUpButton>
));
SelectScrollUpButton.displayName = SelectPrimitive.ScrollUpButton.displayName;

const SelectScrollDownButton = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.ScrollDownButton>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.ScrollDownButton>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.ScrollDownButton
    ref={ref}
    className={cn("flex cursor-default items-center justify-center py-1", className)}
    {...props}
  >
    <ChevronDown className="size-4" />
  </SelectPrimitive.ScrollDownButton>
));
SelectScrollDownButton.displayName = SelectPrimitive.ScrollDownButton.displayName;

const SelectContent = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Content>
>(({ className, children, position = "popper", ...props }, ref) => (
  <SelectPrimitive.Portal>
    <SelectPrimitive.Content
      ref={ref}
      position={position}
      className={cn(
        "relative z-50 max-h-96 min-w-[8rem] overflow-hidden rounded-2xl border border-hairline bg-bg text-fg shadow-pop",
        "data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95",
        "data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
        position === "popper" &&
          "data-[side=bottom]:translate-y-1 data-[side=top]:-translate-y-1",
        className,
      )}
      {...props}
    >
      <SelectScrollUpButton />
      <SelectPrimitive.Viewport
        className={cn(
          "p-1",
          position === "popper" &&
            "h-[var(--radix-select-trigger-height)] w-full min-w-[var(--radix-select-trigger-width)]",
        )}
      >
        {children}
      </SelectPrimitive.Viewport>
      <SelectScrollDownButton />
    </SelectPrimitive.Content>
  </SelectPrimitive.Portal>
));
SelectContent.displayName = SelectPrimitive.Content.displayName;

const SelectLabel = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Label>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.Label
    ref={ref}
    className={cn("px-2 py-1.5 text-label font-semibold text-fg-tertiary", className)}
    {...props}
  />
));
SelectLabel.displayName = SelectPrimitive.Label.displayName;

/** SelectItem · hint 是属性标记(如"暂时缺货")· 走 13-frontend-design §4.3 状态小 pill
 *  文字左对齐 · 勾选右对齐(§9.3)· disabled 项 opacity-45(§9.3) */
type SelectItemProps = React.ComponentPropsWithoutRef<typeof SelectPrimitive.Item> & {
  hint?: React.ReactNode;
};

const SelectItem = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Item>,
  SelectItemProps
>(({ className, children, hint, ...props }, ref) => (
  <SelectPrimitive.Item
    ref={ref}
    className={cn(
      "relative flex w-full cursor-pointer select-none items-center gap-2 rounded-lg py-2 pl-3 pr-8 font-medium outline-none transition-colors",
      "focus:bg-bg-elevated data-[highlighted]:bg-bg-elevated",
      "data-[state=checked]:bg-brand-subtle data-[state=checked]:text-brand-strong",
      "data-[disabled]:pointer-events-none data-[disabled]:opacity-45",
      className,
    )}
    {...props}
  >
    {/* §9.3 文字左对齐 · flex-1 truncate · 勾选右对齐 */}
    <SelectPrimitive.ItemText asChild>
      <span className="min-w-0 flex-1 truncate">{children}</span>
    </SelectPrimitive.ItemText>
    {hint ? (
      /* §4.3 状态小 pill · 10px · 语义色底 · 尺寸最小 */
      <span className="inline-flex shrink-0 items-center rounded-md bg-warn-bg px-1.5 py-[1px] text-[10px] font-semibold leading-[1.4] text-warn-fg">
        {hint}
      </span>
    ) : null}
    <span className="absolute right-2 flex size-4 items-center justify-center">
      <SelectPrimitive.ItemIndicator>
        <Check className="size-4 text-brand-strong" />
      </SelectPrimitive.ItemIndicator>
    </span>
  </SelectPrimitive.Item>
));
SelectItem.displayName = SelectPrimitive.Item.displayName;

const SelectSeparator = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Separator>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.Separator
    ref={ref}
    className={cn("-mx-1 my-1 h-px bg-hairline", className)}
    {...props}
  />
));
SelectSeparator.displayName = SelectPrimitive.Separator.displayName;

export {
  Select,
  SelectGroup,
  SelectValue,
  SelectTrigger,
  SelectContent,
  SelectLabel,
  SelectItem,
  SelectSeparator,
  SelectScrollUpButton,
  SelectScrollDownButton,
};
