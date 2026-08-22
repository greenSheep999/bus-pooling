import { ChevronDown, ChevronUp } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";

/** 「加载更多」/「查看全部 / 收起」的统一原子
 *  - 居中 · size sm · ghost 灰边 · pt-6 呼吸
 *  - expanded 存在 → 双态按钮（收起/展开），自动带 ChevronUp/Down
 *  - 只有 onLoadMore → 单态"加载更多"
 *
 *  统一了三处旧写法：Buses "查看全部/收起" py-2、Buses "加载更多" py-1.5、Overview "加载更多" · 高度 / 图标 / padding 全部对齐 */
export function LoadMoreButton({
  onLoadMore,
  expanded,
  onToggle,
  labelExpand,
  labelCollapse,
  remain,
  remainUnit,
  className,
}: {
  /** 加载更多模式：点击追加数据 */
  onLoadMore?: () => void;
  /** 展开/收起模式：受控展开状态 */
  expanded?: boolean;
  /** 展开/收起切换回调（跟 expanded 一起用） */
  onToggle?: () => void;
  /** 展开按钮标签（如"查看全部 · 3 辆车"） */
  labelExpand?: ReactNode;
  /** 收起按钮标签 · 默认"收起" */
  labelCollapse?: ReactNode;
  /** 加载更多模式的"还剩 N 条"数量 */
  remain?: number;
  /** 数量单位 · 默认"条" · 可为"轮" / "辆车" 等 */
  remainUnit?: string;
  className?: string;
}) {
  /* 默认文案走 i18n（原来 labelCollapse/remainUnit 默认值写死中文） */
  const { t } = useTranslation();

  // 展开/收起模式
  if (typeof expanded === "boolean" && onToggle) {
    return (
      <div className={"flex justify-center pt-6 " + (className ?? "")}>
        <Button variant="ghost" size="sm" onClick={onToggle}>
          {expanded ? (
            <><ChevronUp />{labelCollapse ?? t("ui.collapse")}</>
          ) : (
            <><ChevronDown />{labelExpand}</>
          )}
        </Button>
      </div>
    );
  }

  // 加载更多模式
  if (!onLoadMore || !remain || remain <= 0) return null;
  return (
    <div className={"flex justify-center pt-1 " + (className ?? "")}>
      <Button variant="ghost" size="sm" onClick={onLoadMore}>
        {t("ui.load-more")}{" "}
        <span className="text-fg-tertiary">
          · {t("ui.remain-prefix")} {remain} {remainUnit ?? t("ui.items-unit")}
        </span>
      </Button>
    </div>
  );
}
