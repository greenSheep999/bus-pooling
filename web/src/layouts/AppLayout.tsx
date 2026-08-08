import { useEffect, useState } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import {
  ArrowRight, BookOpen, Bell, Bot, Check, ChevronDown, ChevronRight, Globe,
  KeyRound, LayoutDashboard, LogOut, Moon, Send, Settings,
  User, Users, Wallet,
} from "lucide-react";
import { useMe, useStock, useWallet } from "@/api/hooks";
import { Muted } from "@/components/ui/primitives";
import { avatarColor, avatarLetter, cn, fmtCredits } from "@/lib/utils";
import LogoMark from "@/assets/logo/mark.svg";

const TABS = [
  { to: "/", label: "概览", icon: LayoutDashboard, end: true },
  { to: "/buses", label: "拼车", icon: Users },
  { to: "/extract", label: "提取 key", icon: KeyRound },
  { to: "/dispatch", label: "我的发车", icon: Send },
  { to: "/docs", label: "对接文档", icon: BookOpen },
];

function StockBadge() {
  const { data } = useStock();
  const n = data?.total_available;
  /* 移动端只显示"呼吸点 + 数字" · md+ 显示完整"上游 128 个可拉"
     跟 CreditPill 一样的响应式收缩策略 */
  return (
    <div className="flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border border-hairline bg-bg-elevated px-2.5 py-1 sm:gap-2 sm:px-3 sm:py-1.5">
      <span className="size-1.5 rounded-full bg-ok-solid" />
      <span className="hidden text-label font-medium text-fg-secondary md:inline">上游</span>
      <span className="font-semibold tnum">{n ?? "-"}</span>
      <Muted className="hidden font-medium md:inline">个可拉</Muted>
    </div>
  );
}

function CreditPill() {
  const { data } = useWallet();
  return (
    <Link
      to="/wallet"
      className="flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full bg-credit-bg px-2.5 py-1 transition-opacity hover:opacity-85 sm:gap-2 sm:px-3.5 sm:py-1.5"
    >
      <Wallet className="size-3.5 shrink-0 text-credit-fg" />
      <span className="font-semibold tnum text-credit-fg">
        {data ? fmtCredits(data.balance) : "-"}
      </span>
      {/* 「积分」二字移动端隐藏 · sm+ 才显示 */}
      <span className="hidden font-semibold text-credit-fg sm:inline">积分</span>
    </Link>
  );
}

/* 语言：阶段 1a 只有中文，英文占位不可选（见 docs/12-frontend-pages.md §i18n 词条） */
const LANGS = [
  { code: "zh-CN", label: "简体中文" },
  { code: "en", label: "English", soon: true },
];

const THEMES = [
  { code: "system", label: "跟随系统" },
  { code: "light", label: "浅色" },
  { code: "dark", label: "深色" },
];

type MenuItem =
  | { sep: true }
  | {
      icon: typeof User;
      label: string;
      sub?: string;
      to?: string;
      hint?: string;
      submenu?: { code: string; label: string; soon?: boolean }[];
      value?: string;
      onPick?: (code: string) => void;
    };

function AvatarMenu() {
  const [open, setOpen] = useState(false);
  const [flyout, setFlyout] = useState<number | null>(null);
  const [lang, setLang] = useState("zh-CN");
  const [theme, setTheme] = useState("system");
  const nav = useNavigate();
  const { data: me } = useMe();
  const seed = me?.email ?? me?.username ?? "?";
  const { bg, fg } = avatarColor(seed);

  const items: MenuItem[] = [
    { icon: User, label: me?.username ?? "-", sub: me?.email, to: "/settings/profile" },
    { icon: KeyRound, label: "API key", to: "/settings/api-keys" },
    { icon: Bot, label: "机器人通知", to: "/settings/webhook" },
    { icon: Settings, label: "设置", to: "/settings/downstream" },
    { sep: true },
    {
      icon: Globe,
      label: "语言",
      submenu: LANGS,
      value: lang,
      onPick: setLang,
      hint: LANGS.find((l) => l.code === lang)?.label,
    },
    {
      icon: Moon,
      label: "主题",
      submenu: THEMES,
      value: theme,
      onPick: setTheme,
      hint: THEMES.find((t) => t.code === theme)?.label,
    },
    { sep: true },
    { icon: LogOut, label: "登出", to: "/login" },
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
                      if (it.to) nav(it.to);
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
                          {o.soon && <Muted>暂未开放</Muted>}
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
        aria-label="切换菜单"
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
              {TABS.map((t) => (
                <NavLink
                  key={t.to}
                  to={t.to}
                  end={t.end}
                  onClick={() => setOpen(false)}
                  className={({ isActive }) =>
                    cn(
                      "flex items-center gap-3 rounded-xl px-3 py-2.5 font-medium transition-colors",
                      isActive
                        ? "bg-brand-subtle font-semibold text-brand-strong"
                        : "text-fg-secondary hover:bg-bg-elevated hover:text-fg",
                    )
                  }
                >
                  <t.icon className="size-[16px] shrink-0" />
                  {t.label}
                </NavLink>
              ))}
            </nav>
          </div>
        </>
      )}
    </>
  );
}

/** 顶部 promo 跑马灯 · 品牌紫底 + 白字居中 + 后箭头
    条目多时窄屏可以横滚 · 一个条目也 OK */
const PROMOS = [
  { text: "阶段 1a · 拼车公测中 · 邀请你的车友一起摊单价 · 立即建车", to: "/buses" },
  { text: "waffo 支付宝 / 微信充值 · 5% 通道费 pass-through · 无隐藏收费", to: "/wallet" },
  { text: "开源公益项目 · 无 KYC · 号池全监控 · 号死 30 分钟内自动质保", to: "/docs" },
];

function PromoBar() {
  const [i, setI] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setI((v) => (v + 1) % PROMOS.length), 6000);
    return () => clearInterval(id);
  }, []);
  const p = PROMOS[i];
  return (
    <div className="bg-brand text-white">
      <Link
        to={p.to}
        className="page-container flex items-center justify-center gap-2 py-1.5 text-label font-medium hover:opacity-90"
      >
        <span className="truncate text-center">{p.text}</span>
        <ArrowRight className="size-3.5 shrink-0" />
      </Link>
    </div>
  );
}

/** 内嵌 SVG · lucide-react 1.x 没有这几个品牌 icon，别拉图标包
    尺寸跟 lucide 一致（size-4 = 16px），继承 currentColor */
const IconTelegram = (props: React.SVGProps<SVGSVGElement>) => (
  <svg viewBox="0 0 24 24" fill="currentColor" {...props}>
    <path d="M9.78 18.65l.28-4.23 7.68-6.92c.34-.31-.07-.46-.52-.19L7.74 13.3 3.64 12c-.88-.25-.89-.86.2-1.3l15.97-6.16c.73-.33 1.43.18 1.15 1.3l-2.72 12.81c-.19.91-.74 1.13-1.5.71L12.6 16.3l-1.99 1.93c-.23.23-.42.42-.83.42z"/>
  </svg>
);
const IconDiscord = (props: React.SVGProps<SVGSVGElement>) => (
  <svg viewBox="0 0 24 24" fill="currentColor" {...props}>
    <path d="M19.27 5.33C17.94 4.71 16.5 4.26 15 4a.09.09 0 00-.07.03c-.18.33-.39.76-.53 1.09a16.09 16.09 0 00-4.8 0c-.14-.34-.35-.76-.54-1.09-.01-.02-.04-.03-.07-.03-1.5.26-2.93.71-4.27 1.33-.01 0-.02.01-.03.02-2.72 4.07-3.47 8.03-3.1 11.95 0 .02.01.04.03.05 1.8 1.32 3.53 2.12 5.24 2.65.03.01.06 0 .07-.02.4-.55.76-1.13 1.07-1.74.02-.04 0-.08-.04-.09-.57-.22-1.11-.48-1.64-.78-.04-.02-.04-.08-.01-.11.11-.08.22-.17.33-.25.02-.02.05-.02.07-.01 3.44 1.57 7.15 1.57 10.55 0 .02-.01.05-.01.07.01.11.09.22.17.33.26.04.03.04.09-.01.11-.52.31-1.07.56-1.64.78-.04.01-.05.06-.04.09.32.61.68 1.19 1.07 1.74.03.01.06.02.09.01 1.72-.53 3.45-1.33 5.25-2.65.02-.01.03-.03.03-.05.44-4.53-.73-8.46-3.1-11.95-.01-.01-.02-.02-.04-.02zM8.52 14.91c-1.03 0-1.89-.95-1.89-2.12s.84-2.12 1.89-2.12c1.06 0 1.9.96 1.89 2.12 0 1.17-.84 2.12-1.89 2.12zm6.97 0c-1.03 0-1.89-.95-1.89-2.12s.84-2.12 1.89-2.12c1.06 0 1.9.96 1.89 2.12 0 1.17-.83 2.12-1.89 2.12z"/>
  </svg>
);
const IconGithub = (props: React.SVGProps<SVGSVGElement>) => (
  <svg viewBox="0 0 24 24" fill="currentColor" {...props}>
    <path d="M12 .3a12 12 0 00-3.8 23.38c.6.12.83-.26.83-.57v-2c-3.33.72-4.03-1.61-4.03-1.61-.55-1.38-1.34-1.75-1.34-1.75-1.08-.74.09-.73.09-.73 1.2.08 1.83 1.23 1.83 1.23 1.07 1.83 2.81 1.3 3.5.99.1-.78.42-1.3.76-1.6-2.66-.3-5.47-1.33-5.47-5.93 0-1.31.47-2.38 1.24-3.22-.14-.3-.54-1.52.1-3.18 0 0 1-.32 3.3 1.23a11.5 11.5 0 016 0c2.3-1.55 3.3-1.23 3.3-1.23.64 1.66.24 2.88.12 3.18a4.65 4.65 0 011.23 3.22c0 4.6-2.81 5.63-5.48 5.92.43.37.81 1.1.81 2.22v3.29c0 .32.22.7.83.58A12 12 0 0012 .3"/>
  </svg>
);

/** 底部 footer · 左侧品牌 + 社群 icon · 右侧 3 栏菜单靠右 · 栏间距紧凑
    真实存在的政策入口，不堆 dead link */
function AppFooter() {
  return (
    <footer className="mt-auto border-t border-hairline bg-bg-elevated">
      <div className="page-container py-10">
        {/* 布局：品牌左（自适应窄一点，别撑）· 3 栏菜单右 · gap-16 拉开距离 · 移动堆叠 */}
        <div className="flex flex-col gap-10 lg:flex-row lg:justify-between lg:gap-16">
          {/* 品牌区 · 左侧 · max-w 收紧不占位 */}
          <div className="max-w-xs space-y-3">
            <div className="flex items-center gap-2.5">
              <img src={LogoMark} alt="" className="size-7 rounded-lg" />
              <span className="text-body-lg font-semibold tracking-tight">
                bus-pooling
              </span>
            </div>
            <p className="text-label leading-relaxed text-fg-tertiary">
              开源公益拼车工具 · 让不同规模的用户一起摊 vendor 单价 ·
              监控号存活 · 号死自动质保 · 数据透明可查
            </p>
            {/* 社群 · Telegram · Discord · GitHub（你要的就这 3 个 · 不放邮件） */}
            <div className="flex items-center gap-2 pt-1">
              <SocialLink href="https://t.me/bus-pooling" label="Telegram">
                <IconTelegram className="size-[18px]" />
              </SocialLink>
              <SocialLink href="https://discord.gg/bus-pooling" label="Discord">
                <IconDiscord className="size-[18px]" />
              </SocialLink>
              <SocialLink href="https://github.com/bus-pooling" label="GitHub">
                <IconGithub className="size-[18px]" />
              </SocialLink>
            </div>
          </div>

          {/* 3 栏菜单 · 每栏至少 120px 保证不瘦成一条 · gap 从内容自然拉开
              lg 起间距 gap-12（48px）· 之前 gap-20 是栏比 gap 还窄的错做法 */}
          <div className="grid grid-cols-2 gap-x-8 gap-y-8 sm:grid-cols-3 sm:gap-x-10 lg:gap-x-12 [&>div]:min-w-[120px]">
            <FooterCol title="产品">
              <FooterLink to="/">概览</FooterLink>
              <FooterLink to="/buses">拼车</FooterLink>
              <FooterLink to="/extract">提取 key</FooterLink>
              <FooterLink to="/dispatch">我的发车</FooterLink>
            </FooterCol>

            <FooterCol title="账户">
              <FooterLink to="/wallet">钱包 / 充值</FooterLink>
              <FooterLink to="/settings/profile">个人资料</FooterLink>
              <FooterLink to="/settings/api-keys">API key</FooterLink>
              <FooterLink to="/settings/webhook">机器人通知</FooterLink>
              <FooterLink to="/settings/downstream">我的号池</FooterLink>
            </FooterCol>

            <FooterCol title="说明与政策">
              <FooterLink to="/legal/terms">用户协议</FooterLink>
              <FooterLink to="/legal/privacy">隐私政策</FooterLink>
              <FooterLink to="/legal/compliance">合规声明</FooterLink>
              <FooterLink to="/docs">对接文档</FooterLink>
            </FooterCol>
          </div>
        </div>

        {/* 底行 · copyright + 状态入口 */}
        <div className="mt-8 flex flex-col gap-3 border-t border-hairline pt-6 text-label text-fg-tertiary md:flex-row md:items-center md:justify-between">
          <span>© 2026 bus-pooling · 开源公益项目</span>
          <div className="flex items-center gap-4">
            <span className="flex items-center gap-1.5">
              <span className="size-1.5 rounded-full bg-ok-solid" />
              系统正常
            </span>
            <a
              href="https://status.example"
              target="_blank"
              rel="noreferrer"
              className="transition-colors hover:text-fg-secondary"
            >
              状态页 →
            </a>
          </div>
        </div>
      </div>
    </footer>
  );
}

function SocialLink({
  href, label, children,
}: { href: string; label: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      target={href.startsWith("http") ? "_blank" : undefined}
      rel="noreferrer"
      aria-label={label}
      title={label}
      className="grid size-9 place-items-center rounded-lg border border-hairline bg-bg text-fg-secondary transition-colors hover:border-fg-secondary hover:text-fg"
    >
      {children}
    </a>
  );
}

function FooterCol({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="space-y-3">
      <h4 className="text-[10px] font-semibold uppercase tracking-wider text-fg-tertiary">
        {title}
      </h4>
      <ul className="space-y-2">{children}</ul>
    </div>
  );
}

function FooterLink({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <li>
      <Link
        to={to}
        className="text-label font-medium text-fg-secondary transition-colors hover:text-fg"
      >
        {children}
      </Link>
    </li>
  );
}

export default function AppLayout() {
  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <PromoBar />

      <header className="sticky top-0 z-30 border-b border-hairline bg-bg/85 backdrop-blur-xl">
        {/* 排 1 · logo + wordmark + 移动菜单 chevron 靠左 · 右侧库存(md+) / 积分 / 铃铛(sm+) / 头像
            relative 让 MobileNav 的 top-full 面板定位到此 header 下沿 */}
        <div className="page-container relative flex h-14 items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <Link to="/" className="flex min-w-0 items-center gap-2.5">
              <img src={LogoMark} alt="" className="size-7 shrink-0 rounded-lg" />
              <span className="hidden text-body-lg font-semibold tracking-tight sm:inline">
                bus-pooling
              </span>
            </Link>
            <MobileNav />
          </div>

          <div className="flex shrink-0 items-center gap-2 sm:gap-3">
            <StockBadge />
            <CreditPill />
            <button
              className="hidden size-9 place-items-center rounded-full transition-colors hover:bg-bg-elevated focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30 sm:grid"
              aria-label="通知"
            >
              <Bell className="size-4 text-fg-secondary" />
            </button>
            <AvatarMenu />
          </div>
        </div>

        {/* 排 1 与排 2 之间 · 极浅分割线（若隐若现 · 只做视觉过渡，不抢底部分割线） */}
        <div className="hidden h-px bg-hairline/40 lg:block" />

        {/* 排 2 · 主导航 tab · lg 才显示（<lg 走下拉展开面板） */}
        <div className="page-container hidden lg:block">
          <nav className="flex items-center gap-1 overflow-x-auto py-1.5">
            {TABS.map((t) => (
              <NavLink
                key={t.to}
                to={t.to}
                end={t.end}
                className={({ isActive }) =>
                  cn(
                    "flex shrink-0 items-center gap-2 rounded-full px-3.5 py-2 font-medium transition-colors",
                    isActive
                      ? "bg-brand-subtle font-semibold text-brand-strong"
                      : "text-fg-secondary hover:bg-bg-elevated hover:text-fg",
                  )
                }
              >
                <t.icon className="size-[15px]" />
                {t.label}
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
