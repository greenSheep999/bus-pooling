import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";

/** 设置类页面统一的头 · 面包屑 +（hero 标题 + 描述）+ 右侧动作
 *
 *  4 个设置页共用 —— 别各写一遍，间距会飘（hero 8px / 描述 text-fg-tertiary，
 *  见 docs/13-design-principles.md §5.2b）
 */
export function SettingsHead({
  crumb,
  title,
  desc,
  right,
}: {
  /** 面包屑末级（前面固定「设置」）· 例："我的号池" */
  crumb: string;
  title: string;
  desc: ReactNode;
  right?: ReactNode;
}) {
  const { t } = useTranslation("settings");
  return (
    <div className="space-y-4">
      {/* 「设置」指向索引页 /settings（真实主入口）· 不是 /me —— 账号不是一种设置 */}
      <nav
        aria-label={t("head.breadcrumb-aria")}
        className="flex items-center gap-1 text-label font-medium text-fg-tertiary"
      >
        <Link to="/settings" className="transition-colors hover:text-fg-secondary">
          {t("head.root")}
        </Link>
        <ChevronRight className="size-3.5" aria-hidden />
        <span className="text-fg-secondary">{crumb}</span>
      </nav>

      <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div className="min-w-0 space-y-2">
          <h1 className="text-hero font-semibold">{title}</h1>
          <p className="text-fg-tertiary">{desc}</p>
        </div>
        {right && <div className="shrink-0">{right}</div>}
      </div>
    </div>
  );
}
