import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowRight } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { Passenger } from "@/types";
import { Button } from "@/components/ui/button";
import { BrandLogo } from "@/components/BrandLogo";
import { PublicControls } from "@/components/PublicControls";

/** 公开页专用的登录探测 · 401 不重试、不 throw、不进 error state
 *  避免 landing / legal 上刷新时 react-query 报未处理错误
 *  跟 AppLayout 里 useMe 是两条不同链路 —— 那边 401 就跳 login，这里 401 只是"没登录" */
function useMaybeMe() {
  return useQuery({
    queryKey: ["me"],
    queryFn: async () => {
      try {
        return await api<Passenger>("/me");
      } catch {
        return null;
      }
    },
    retry: false,
    staleTime: 60_000,
  });
}

/** 未登录页共用的顶栏（Landing / Legal / 之后的公开页）
 *
 *  - 左侧 brand logo → 回首页
 *  - 中间可选 anchor 导航（Landing 传，其它页不传就不渲染）
 *  - 右侧：语言/主题切换 · 未登录：登录 + 免费上车；已登录：进入控制台
 *
 *  没走 AppLayout 是因为公开页也要给未登录用户看，AppLayout 会 redirect */
export function PublicHeader({
  anchors,
}: {
  /** 页内锚点导航项 · Landing 传 · 其他公开页不传 */
  anchors?: { id: string; labelKey: string }[];
}) {
  const { t } = useTranslation();
  const { data: me, isPending } = useMaybeMe();
  const authed = !!me;

  return (
    <header className="sticky top-0 z-30 border-b border-hairline bg-bg/85 backdrop-blur-xl">
      <div className="page-container flex h-14 items-center justify-between gap-4">
        <Link to="/" className="flex shrink-0 items-center">
          <BrandLogo />
        </Link>

        {anchors && anchors.length > 0 ? (
          <nav className="hidden items-center gap-1 md:flex">
            {anchors.map((a) => (
              <a
                key={a.id}
                href={`#${a.id}`}
                className="rounded-full px-3 py-1.5 font-medium text-fg-secondary transition-colors hover:bg-bg-elevated hover:text-fg"
              >
                {t(a.labelKey)}
              </a>
            ))}
          </nav>
        ) : null}

        <div className="flex shrink-0 items-center gap-2">
          <PublicControls />
          <div className="mx-1 hidden h-6 w-px bg-hairline sm:block" />
          {/* 登录态未定前不闪按钮 · 保留占位避免布局跳 · isPending 时给个透明按钮 */}
          {isPending ? (
            <div className="h-8 w-24" aria-hidden />
          ) : authed ? (
            <Button variant="primary" size="sm" asChild>
              <Link to="/overview">
                {t("nav.enter_dashboard")}
                <ArrowRight />
              </Link>
            </Button>
          ) : (
            <>
              <Button variant="ghost" size="sm" asChild>
                <Link to="/login">{t("nav.login")}</Link>
              </Button>
              <Button variant="primary" size="sm" asChild>
                <Link to="/register">{t("nav.signup")}</Link>
              </Button>
            </>
          )}
        </div>
      </div>
    </header>
  );
}
