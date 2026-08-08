import { useState } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import {
  BookOpen, Bell, Bot, Check, ChevronDown, ChevronRight, Globe, KeyRound,
  LayoutDashboard, LogOut, Moon, Send, Settings, User, Users, Wallet,
} from "lucide-react";
import { useMe, useStock, useWallet } from "@/api/hooks";
import { Muted } from "@/components/ui/primitives";
import { avatarColor, avatarLetter, cn, fmtCredits } from "@/lib/utils";

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
  return (
    <div className="hidden items-center gap-2 rounded-full border border-hairline bg-bg-elevated px-3 py-1.5 lg:flex">
      <span className="size-1.5 rounded-full bg-ok-solid" />
      <span className="text-label font-medium text-fg-secondary">上游</span>
      <span className="font-semibold tnum">{n ?? "-"}</span>
      <Muted className="font-medium">个可拉</Muted>
    </div>
  );
}

function CreditPill() {
  const { data } = useWallet();
  return (
    <Link
      to="/wallet"
      className="flex items-center gap-2 rounded-full bg-credit-bg px-3.5 py-1.5 transition-opacity hover:opacity-85"
    >
      <Wallet className="size-3.5 text-credit-fg" />
      <span className="font-semibold tnum text-credit-fg">
        {data ? fmtCredits(data.balance) : "-"} 积分
      </span>
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
    <div className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1.5 rounded-full transition-opacity hover:opacity-85"
      >
        <span
          className="grid size-8 place-items-center rounded-full font-semibold"
          style={{ backgroundColor: bg, color: fg }}
        >
          {avatarLetter(seed)}
        </span>
        <ChevronDown className="size-3.5 text-fg-tertiary" />
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

export default function AppLayout() {
  return (
    <div className="min-h-dvh bg-bg">
      <header className="sticky top-0 z-30 border-b border-hairline bg-bg/85 backdrop-blur-xl">
        <div className="flex h-16 items-center justify-between px-gutter">
          <div className="flex items-center gap-10">
            <Link to="/" className="flex items-center gap-2.5">
              <span className="grid size-7 place-items-center rounded-lg bg-brand font-semibold text-white">
                K
              </span>
            </Link>

            <nav className="flex items-center gap-1">
              {TABS.map((t) => (
                <NavLink
                  key={t.to}
                  to={t.to}
                  end={t.end}
                  className={({ isActive }) =>
                    cn(
                      "flex items-center gap-2 rounded-lg px-3 py-2 font-medium transition-colors",
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

          <div className="flex items-center gap-3">
            <StockBadge />
            <CreditPill />
            <button className="grid size-9 place-items-center rounded-full transition-colors hover:bg-bg-elevated">
              <Bell className="size-4 text-fg-secondary" />
            </button>
            <AvatarMenu />
          </div>
        </div>
      </header>

      <main className="px-gutter py-16">
        <Outlet />
      </main>
    </div>
  );
}
