import { useEffect, useState } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import {
  Activity, BookOpen, Check, ChevronDown, ChevronRight, Gift, Globe,
  KeyRound, LayoutDashboard, LogOut, Moon, Send, Settings,
  User, Users, Wallet,
} from "lucide-react";
import { useLogout, useMe, useVendorOffers, useWallet } from "@/api/hooks";
import { AppFooter } from "@/components/AppFooter";
import { NotificationsBell } from "@/components/NotificationsBell";
import { PromoBar } from "@/components/PromoBar";
import { Muted } from "@/components/ui/primitives";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { avatarColor, avatarLetter, cn, fmtCredits } from "@/lib/utils";
import { useTheme, type ThemeMode } from "@/lib/theme";
import { useTranslation } from "react-i18next";
import { LANGUAGES, resolveLang } from "@/i18n";
import { BrandLogo } from "@/components/BrandLogo";
import { DocumentMeta } from "@/components/DocumentMeta";

/** 5 个 tab · label 走 i18n key（nav.*）· 不 hardcode 文案 */
const TABS = [
  { to: "/overview", labelKey: "nav.overview", icon: LayoutDashboard, end: true },
  { to: "/buses", labelKey: "nav.buses", icon: Users },
  { to: "/extract", labelKey: "nav.extract", icon: KeyRound },
  { to: "/dispatch", labelKey: "nav.dispatch", icon: Send },
  { to: "/status", labelKey: "nav.status", icon: Activity },
  { to: "/docs", labelKey: "nav.docs", icon: BookOpen },
];

function StockBadge() {
  const { data } = useVendorOffers();
  const { t } = useTranslation();
  /** hover 打开明细 · 点击也能开(触屏 / 键盘可达)· 无 HoverCard 依赖 */
  const [open, setOpen] = useState(false);
  /* 从 Offer matrix 聚合总量 · 跟 Extract 页 tab 数字一致 · 不再单独调 /vendors/stock
     badge 显示总数 · hover 弹企业/个人明细 */
  const { total, enterprise, personal } = (() => {
    const vs = data?.vendors ?? [];
    let e = 0;
    let p = 0;
    for (const v of vs) {
      e += v.categories.enterprise?.available ?? 0;
      p += v.categories.personal?.available ?? 0;
    }
    return { total: e + p, enterprise: e, personal: p };
  })();
  const hasData = data !== undefined;
  /* 移动端只显示"呼吸点 + 数字" · md+ 显示完整"上游 N 个可拉" */
  return (
    <Popover open={open} onOpenChange={setOpen}>
      {/* hover handler 挂在外层 span —— PopoverTrigger asChild 会合并/覆盖子元素的
          事件处理，挂在 button 上不生效（实测 aria-expanded 一直 false）*/}
      <span
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        className="flex shrink-0"
      >
        <PopoverTrigger asChild>
          <button
            type="button"
            className="flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border border-hairline bg-bg-elevated px-2.5 py-1 transition-colors hover:bg-bg sm:gap-2 sm:px-3 sm:py-1.5"
            aria-label={t("header.stock_unit")}
          >
            <span className="size-1.5 rounded-full bg-ok-solid" />
            <span className="hidden text-label font-medium text-fg-secondary md:inline">{t("header.stock_unit")}</span>
            <span className="font-semibold tnum">{hasData ? total : "-"}</span>
            <Muted className="hidden font-medium md:inline">{t("header.stock_available_suffix")}</Muted>
          </button>
        </PopoverTrigger>
      </span>
      {/* §9.2 Popover 规范:w-64 上限 · rounded-[14px] · shadow-pop
          hover 场景:指针移到面板上不关 · 且不抢焦点(否则 hover 完键盘焦点被吞) */}
      <PopoverContent
        align="end"
        onOpenAutoFocus={(e) => e.preventDefault()}
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        className="w-64 rounded-[14px] p-3 shadow-pop"
      >
        <div className="mb-2 text-label font-semibold text-fg">{t("header.stock_breakdown")}</div>
        <div className="space-y-1.5 text-label">
          <div className="flex items-center justify-between">
            <span className="text-fg-secondary">{t("header.stock_enterprise")}</span>
            <span className="font-semibold tnum">{hasData ? enterprise : "-"}</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-fg-secondary">{t("header.stock_personal")}</span>
            <span className="font-semibold tnum">{hasData ? personal : "-"}</span>
          </div>
          <div className="mt-2 flex items-center justify-between border-t border-hairline pt-2">
            <span className="text-fg-tertiary">{t("header.stock_total")}</span>
            <span className="font-semibold tnum">{hasData ? total : "-"}</span>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function CreditPill() {
  const { data } = useWallet();
  const { t } = useTranslation();
  return (
    <Link
      to="/wallet"
      // 底用 credit-solid 低透明度（浅色 8% / 深色 12%）· 表达"credit 语义"但不喧宾夺主
      // 字用 credit-solid 保留识别度 · 深色下天然融合（透明底叠深底 → 更暗，不刺）
      className="flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full bg-ok-solid/10 px-2.5 py-1 transition-opacity hover:opacity-80 sm:gap-2 sm:px-3.5 sm:py-1.5 dark:bg-ok-solid/[.14]"
    >
      <Wallet className="size-3.5 shrink-0 text-ok-solid" />
      <span className="font-semibold tnum text-ok-solid">
        {data ? fmtCredits(data.balance) : "-"}
      </span>
      {/* 「积分」二字移动端隐藏 · sm+ 才显示 */}
      <span className="hidden font-semibold text-ok-solid sm:inline">{t("header.credits")}</span>
    </Link>
  );
}

/** 语言列表 · 从 i18n LANGUAGES 派生（阶段 1 支持 zh-CN + en）
 *  label 是各语言的**本地化自称**（Chinese speakers see 简体中文, English speakers see English），
 *  不跟随当前 UI 语言变 —— language picker 里让用户看到"目标语言的名字"更容易识别 */
const LANGS = LANGUAGES.map((l) => ({ code: l.code, label: l.label }));

/** 主题 · label 是 i18n key · 渲染时 t() 出真文案 */
const THEMES = [
  { code: "system", labelKey: "avatar.theme_system" },
  { code: "light", labelKey: "avatar.theme_light" },
  { code: "dark", labelKey: "avatar.theme_dark" },
];

type MenuItem =
  | { sep: true }
  | {
      icon: typeof User;
      label: string;
      sub?: string;
      to?: string;
      /** 点击回调 · logout 那种需要跑 mutation 再跳的场景用它 · 跟 to 二选一 */
      onClick?: () => void;
      hint?: string;
      submenu?: { code: string; label: string; soon?: boolean }[];
      value?: string;
      onPick?: (code: string) => void;
    };

function AvatarMenu() {
  const [open, setOpen] = useState(false);
  const [flyout, setFlyout] = useState<number | null>(null);
  const { t, i18n } = useTranslation();
  const [theme, setTheme] = useTheme();
  const nav = useNavigate();
  const { data: me } = useMe();
  const logout = useLogout();
  const seed = me?.email ?? me?.username ?? "?";
  const { bg, fg } = avatarColor(seed);

  // i18n.language 可能是 "en-US" / "zh"，resolveLang 规整到 LANGUAGES 里的 code
  const lang = resolveLang(i18n.language);
  const setLang = (code: string) => { void i18n.changeLanguage(code); };

  /* 「我的」= 账号本身（/me）· 「设置」= 设置主入口（/settings 索引页）
     号池 / 机器人通知 / API key 都是设置的**下级**，不跟「设置」并列摆在这里 ——
     并列会让人以为它们跟设置是平级的另外三件事 */
  const items: MenuItem[] = [
    { icon: User, label: me?.username ?? "-", sub: me?.email, to: "/me" },
    // 社群 · 绑专属邀请码的入口（decisions §8.38 §8.39）
    { icon: Users, label: t("avatar.community"), to: "/community" },
    { icon: Gift, label: t("avatar.invite"), to: "/invite" },
    { icon: Settings, label: t("avatar.settings"), to: "/settings" },
    { sep: true },
    {
      icon: Globe,
      label: t("avatar.language"),
      submenu: LANGS,
      value: lang,
      onPick: setLang,
      hint: LANGS.find((l) => l.code === lang)?.label,
    },
    {
      icon: Moon,
      label: t("avatar.theme"),
      submenu: THEMES.map((th) => ({ code: th.code, label: t(th.labelKey) })),
      value: theme,
      onPick: (c: string) => setTheme(c as ThemeMode),
      hint: t(THEMES.find((th) => th.code === theme)?.labelKey ?? "avatar.theme_system"),
    },
    { sep: true },
    {
      icon: LogOut,
      label: t("avatar.logout"),
      // 真调 /api/logout · 清 react-query 缓存 · 再跳 /login
      // 之前只是 to="/login" · session 不清 · 用户返回 /overview 就又"登着"了
      onClick: async () => {
        try { await logout.mutateAsync(); } catch { /* 即使失败也跳 login · 前端状态清干净 */ }
        nav("/login");
      },
    },
  ];

  return (
    <div className="relative shrink-0">
      <button
        onClick={() => setOpen((v) => !v)}
        className="grid size-8 place-items-center rounded-full font-semibold transition-opacity hover:opacity-85"
        style={{ backgroundColor: bg, color: fg }}
      >
        {avatarLetter(seed)}
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div className="absolute right-0 z-50 mt-2 w-64 rounded-[14px] border border-hairline bg-bg p-2 shadow-pop">
            {items.map((it, i) =>
              "sep" in it ? (
                <div key={i} className="my-1 h-px bg-hairline" />
              ) : (
                <div
                  key={i}
                  className="relative"
                  onMouseEnter={() => setFlyout(it.submenu ? i : null)}
                  onMouseLeave={() => it.submenu && setFlyout(null)}
                >
                  <button
                    onClick={() => {
                      if (it.submenu) {
                        setFlyout((v) => (v === i ? null : i));
                        return;
                      }
                      // 优先 onClick（logout 用它跑 mutation）· 否则走 to
                      if (it.onClick) { it.onClick(); }
                      else if (it.to) nav(it.to);
                      setOpen(false);
                    }}
                    className={cn(
                      "flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors hover:bg-bg-elevated",
                      flyout === i && "bg-bg-elevated",
                    )}
                  >
                    <it.icon className="size-4 shrink-0 text-fg-secondary" />
                    <span className="flex-1">
                      <span className="block font-medium">{it.label}</span>
                      {it.sub && <Muted className="block">{it.sub}</Muted>}
                    </span>
                    {it.hint && <Muted>{it.hint}</Muted>}
                    {it.submenu && (
                      <ChevronRight className="size-3.5 shrink-0 text-fg-tertiary" />
                    )}
                  </button>

                  {it.submenu && flyout === i && (
                    <div className="absolute right-full top-0 z-50 mr-1 w-44 rounded-[14px] border border-hairline bg-bg p-2 shadow-pop">
                      {it.submenu.map((o) => (
                        <button
                          key={o.code}
                          disabled={o.soon}
                          onClick={() => {
                            if (o.soon) return;
                            it.onPick?.(o.code);
                            setFlyout(null);
                            setOpen(false);
                          }}
                          className={cn(
                            "flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left transition-colors",
                            o.soon
                              ? "cursor-not-allowed opacity-45"
                              : "hover:bg-bg-elevated",
                          )}
                        >
                          <Check
                            className={cn(
                              "size-3.5 shrink-0",
                              it.value === o.code
                                ? "text-brand-strong"
                                : "invisible",
                            )}
                          />
                          <span className="flex-1 font-medium">{o.label}</span>
                          {o.soon && <Muted>{t("ui.not-open-yet")}</Muted>}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              ),
            )}
          </div>
        </>
      )}
    </div>
  );
}

/** 移动端菜单 · logo 右边 chevron 触发 · 向下摊开面板 · < lg 显示 */
function MobileNav() {
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    const onResize = () => window.innerWidth >= 1024 && setOpen(false);
    window.addEventListener("keydown", onKey);
    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("resize", onResize);
    };
  }, [open]);

  return (
    <>
      <button
        onClick={() => setOpen((v) => !v)}
        className="grid size-7 shrink-0 place-items-center rounded-md transition-colors hover:bg-bg-elevated lg:hidden"
        aria-label={t("ui.toggle-menu")}
      >
        <ChevronDown
          className={cn(
            "size-4 text-fg-tertiary transition-transform",
            open && "rotate-180",
          )}
        />
      </button>

      {open && (
        <>
          {/* backdrop · 点空关闭 · 只在移动端 */}
          <div
            className="fixed inset-0 z-30 lg:hidden"
            onClick={() => setOpen(false)}
          />
          {/* 面板 · 从 header 下方向下展开 · 全宽 */}
          <div className="absolute inset-x-0 top-full z-40 border-b border-hairline bg-bg shadow-pop lg:hidden">
            <nav className="page-container flex flex-col gap-1 py-3">
              {TABS.map((tab) => (
                <NavLink
                  key={tab.to}
                  to={tab.to}
                  end={tab.end}
                  onClick={() => setOpen(false)}
                  className={({ isActive }) =>
                    cn(
                      "flex items-center gap-3 rounded-xl px-3 py-2.5 font-medium transition-colors",
                      // 选中态 · primary 前景色反色（light 黑 dark 白）· 用户要求菜单不用品牌色
                      // 品牌紫留给"每页主操作"（brand 按钮）· 见 CLAUDE.md §视觉
                      isActive
                        ? "bg-fg font-semibold text-bg"
                        : "text-fg-secondary hover:bg-bg-elevated hover:text-fg",
                    )
                  }
                >
                  <tab.icon className="size-[16px] shrink-0" />
                  {t(tab.labelKey)}
                </NavLink>
              ))}
            </nav>
          </div>
        </>
      )}
    </>
  );
}


export default function AppLayout() {
  const { t } = useTranslation();
  const { data: me, isPending, isError } = useMe();

  /* 路由守卫 · me 加载失败（401/其他）→ 踢到 /login
     · 401 由 client.ts 自动踢过 · 这里兜底 · 也覆盖 me 缓存清空但没跳的场景
     · 用 window.location.replace 而不是 <Navigate> —— 后者会先 render AppLayout 再跳 · 造成闪 */
  useEffect(() => {
    if (!isPending && (isError || !me)) {
      const next = window.location.pathname + window.location.search;
      window.location.replace(`/login?next=${encodeURIComponent(next)}`);
    }
  }, [isPending, isError, me]);

  if (isPending) {
    return <div className="grid min-h-dvh place-items-center text-label text-fg-tertiary">…</div>;
  }
  if (!me) {
    return null; // 上面 effect 会跳走 · render 空避免闪
  }

  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <DocumentMeta />
      <PromoBar />

      <header className="sticky top-0 z-30 border-b border-hairline bg-bg/85 backdrop-blur-xl">
        {/* 排 1 · logo + wordmark + 移动菜单 chevron 靠左 · 右侧库存(md+) / 积分 / 铃铛(sm+) / 头像
            relative 让 MobileNav 的 top-full 面板定位到此 header 下沿 */}
        <div className="page-container relative flex h-14 items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <Link to="/" className="flex min-w-0 items-center">
              <BrandLogo mark className="sm:hidden" />
              <BrandLogo className="hidden sm:inline-flex" />
            </Link>
            <MobileNav />
          </div>

          <div className="flex shrink-0 items-center gap-2 sm:gap-3">
            <StockBadge />
            <CreditPill />
            <NotificationsBell />
            <AvatarMenu />
          </div>
        </div>

        {/* 排 1 与排 2 之间 · 极浅分割线（若隐若现 · 只做视觉过渡，不抢底部分割线） */}
        <div className="hidden h-px bg-hairline/40 lg:block" />

        {/* 排 2 · 主导航 tab · lg 才显示（<lg 走下拉展开面板） */}
        <div className="page-container hidden lg:block">
          <nav className="flex items-center gap-1 overflow-x-auto py-1.5">
            {TABS.map((tab) => (
              <NavLink
                key={tab.to}
                to={tab.to}
                end={tab.end}
                className={({ isActive }) =>
                  cn(
                    "flex shrink-0 items-center gap-2 rounded-full px-3.5 py-2 font-medium transition-colors",
                    // 桌面 tab 选中态 · primary 反色 · 品牌紫留给主 CTA
                    isActive
                      ? "bg-fg font-semibold text-bg"
                      : "text-fg-secondary hover:bg-bg-elevated hover:text-fg",
                  )
                }
              >
                <tab.icon className="size-[15px]" />
                {t(tab.labelKey)}
              </NavLink>
            ))}
          </nav>
        </div>
      </header>

      <main className="page-container flex-1 py-8 lg:py-12">
        <Outlet />
      </main>

      <AppFooter />
    </div>
  );
}
