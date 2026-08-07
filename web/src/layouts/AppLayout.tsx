import { useState } from "react";
import { Link, NavLink, Outlet, useNavigate } from "react-router-dom";
import {
  BookOpen, Bell, Bot, ChevronDown, Globe, KeyRound, LayoutDashboard,
  LogOut, Moon, Send, Settings, User, Users, Wallet,
} from "lucide-react";
import { useStock, useWallet } from "@/api/hooks";
import { cn, fmtCredits } from "@/lib/utils";

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
      <span className="text-micro font-medium text-fg-secondary">上游</span>
      <span className="text-body font-semibold tnum">{n ?? "—"}</span>
      <span className="text-micro font-medium text-fg-tertiary">号可拉</span>
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
      <span className="text-body font-semibold tnum text-credit-fg">
        {data ? fmtCredits(data.balance) : "—"} 积分
      </span>
    </Link>
  );
}

function AvatarMenu() {
  const [open, setOpen] = useState(false);
  const nav = useNavigate();
  const items = [
    { icon: User, label: "danlio", sub: "我的", to: "/settings/profile" },
    { icon: KeyRound, label: "API key", to: "/settings/api-keys" },
    { icon: Bot, label: "机器人通知", to: "/settings/webhook" },
    { icon: Settings, label: "设置", to: "/settings/downstream" },
    { sep: true as const },
    { icon: Globe, label: "语言", hint: "中" },
    { icon: Moon, label: "主题", hint: "跟随系统" },
    { sep: true as const },
    { icon: LogOut, label: "登出", to: "/login" },
  ];

  return (
    <div className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1.5 rounded-full transition-opacity hover:opacity-85"
      >
        <span className="grid size-8 place-items-center rounded-full bg-[#0F172A] text-body font-semibold text-white">
          D
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
                <button
                  key={i}
                  onClick={() => {
                    if (it.to) nav(it.to);
                    setOpen(false);
                  }}
                  className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors hover:bg-bg-elevated"
                >
                  <it.icon className="size-4 shrink-0 text-fg-secondary" />
                  <span className="flex-1">
                    <span className="block text-body-lg font-medium">{it.label}</span>
                    {it.sub && (
                      <span className="block text-micro text-fg-tertiary">{it.sub}</span>
                    )}
                  </span>
                  {it.hint && (
                    <span className="text-micro text-fg-tertiary">{it.hint}</span>
                  )}
                </button>
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
        <div className="flex h-16 items-center justify-between px-8">
          <div className="flex items-center gap-10">
            <Link to="/" className="flex items-center gap-2.5">
              <span className="grid size-7 place-items-center rounded-lg bg-brand text-body font-semibold text-white">
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
                      "flex items-center gap-2 rounded-lg px-3 py-2 text-body-lg font-medium transition-colors",
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
