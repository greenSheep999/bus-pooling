import type { LucideIcon } from "lucide-react";
import { cn } from "@/lib/utils";

/** 空态 · 全站统一
 *
 * 两种尺寸（对应现有两种用法，别再各写一遍）：
 *   - `size="page"`  整页 / 整卡没内容 · 大图标 + 标题 + 说明 + 主 CTA（原 Buses.tsx 的写法）
 *   - `size="inline"` 卡片内某个区块没内容 · 小图标 + 一行说明（原 Extract.tsx 的写法）
 *
 * **文案规矩**（写 title/desc 前先想）：
 *   - 说**下一步该干什么**，不只说"没有数据"
 *     ✅「还没有拼车 · 建一辆自己的车 · 或加入他人的拼车」
 *     ❌「暂无数据」（用户看了不知道该做什么）
 *   - 区分「从来没有」和「筛选后没有」——后者要提示改筛选条件，不要引导他去创建
 */
export function EmptyState({
  icon: Icon,
  title,
  desc,
  action,
  size = "inline",
  className,
}: {
  icon?: LucideIcon;
  title: string;
  /** 说明下一步怎么做 · 别只写"暂无数据" */
  desc?: React.ReactNode;
  /** 主动作（建车 / 去充值 / 清筛选）· 没有下一步就不给 */
  action?: React.ReactNode;
  size?: "page" | "inline";
  className?: string;
}) {
  const isPage = size === "page";
  return (
    <div
      className={cn(
        "grid place-items-center gap-3 text-center",
        isPage ? "gap-4 py-12" : "py-10",
        className,
      )}
    >
      {Icon && (
        <span
          className={cn(
            "grid shrink-0 place-items-center",
            isPage
              ? "size-12 rounded-2xl bg-brand-subtle"
              : "size-10 rounded-full bg-bg-elevated",
          )}
        >
          <Icon
            className={cn(
              isPage ? "size-6 text-brand-strong" : "size-4 text-fg-tertiary",
            )}
          />
        </span>
      )}
      <div className={isPage ? "space-y-1" : undefined}>
        <div className={isPage ? "text-body-lg font-semibold" : "font-semibold"}>
          {title}
        </div>
        {desc && (
          <p
            className={cn(
              "text-fg-tertiary",
              isPage ? undefined : "mt-0.5 text-label",
            )}
          >
            {desc}
          </p>
        )}
      </div>
      {action}
    </div>
  );
}

/** 加载失败态 · 跟空态区分开
 *
 * 为什么单独一个：空态是"确实没有"，错误态是"没拿到" —— 用户该做的事不一样
 * （前者去创建，后者点重试）。混成一个会让人以为数据被清空了。 */
export function ErrorState({
  title = "加载失败",
  desc = "网络或服务异常 · 点下面重试",
  onRetry,
  className,
}: {
  title?: string;
  desc?: React.ReactNode;
  onRetry?: () => void;
  className?: string;
}) {
  return (
    <div className={cn("grid place-items-center gap-3 py-10 text-center", className)}>
      <span className="grid size-10 place-items-center rounded-full bg-danger-bg">
        <svg viewBox="0 0 24 24" fill="none" className="size-4 text-danger-fg">
          <path
            d="M12 9v4m0 4h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </span>
      <div>
        <div className="font-semibold">{title}</div>
        <p className="mt-0.5 text-label text-fg-tertiary">{desc}</p>
      </div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="text-label font-medium text-brand-strong underline-offset-2 hover:underline"
        >
          重试
        </button>
      )}
    </div>
  );
}
