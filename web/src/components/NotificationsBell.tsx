import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
  AlertTriangle, ArrowUpFromLine, Bell, Check, Gift, KeyRound, X,
} from "lucide-react";
import { useActivities, useMe, useWallet } from "@/api/hooks";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn, fmtCredits, fmtRelative, MICRO } from "@/lib/utils";
import { notifLink } from "@/lib/notif-link";
import { useTranslation } from "react-i18next";
import type { Activity, ActivityKind } from "@/types";

/** 余额低于这个积分 · 铃铛顶部挂一条「将不足」横幅（状态，不是事件） */
const LOW_BALANCE_CREDITS = 50;
const BELL_LIMIT = 8;
const READ_KEY = (pid: string) => `bp.notif.readAt.${pid}`;

function readAtOf(pid: string): number {
  const raw = localStorage.getItem(READ_KEY(pid));
  const n = raw ? Number(raw) : 0;
  return Number.isFinite(n) ? n : 0;
}

function markRead(pid: string) {
  localStorage.setItem(READ_KEY(pid), String(Date.now()));
}

const KIND_ICON: Record<ActivityKind, typeof Check> = {
  dead: X,
  refill: Check,
  topup: Gift,
  redeem: Gift,
  extract: KeyRound,
  into_bus: Check,
  push: ArrowUpFromLine,
  handoff: ArrowUpFromLine,
};

const KIND_WRAP: Record<ActivityKind, string> = {
  dead: "bg-danger-bg text-danger-fg",
  refill: "bg-ok-bg text-ok-fg",
  topup: "bg-ok-bg text-ok-fg",
  redeem: "bg-ok-bg text-ok-fg",
  extract: "bg-info-bg text-info-fg",
  into_bus: "bg-ok-bg text-ok-fg",
  push: "bg-brand-subtle text-brand-strong",
  handoff: "bg-brand-subtle text-brand-strong",
};

function itemCopy(a: Activity, t: (k: string, o?: Record<string, unknown>) => string): { title: string; desc: string } {
  const count = a.count ?? 1;
  const amount = a.amount != null ? fmtCredits(Math.abs(a.amount)) : "";
  const source = a.source || "";
  const target = a.target || "";
  switch (a.kind) {
    case "dead":
      return {
        title: t("header.notif.dead_title", { count }),
        desc: [source, target].filter(Boolean).join(" · ") || t("header.notif.dead_desc_fallback"),
      };
    case "refill":
      return {
        title: t("header.notif.refill_title", { count }),
        desc: target || a.summary || t("header.notif.refill_desc_fallback"),
      };
    case "topup":
      return {
        title: amount ? t("header.notif.topup_title", { amount }) : t("header.notif.topup_title_plain"),
        desc: a.summary || (a.summary_code ? t(`activity.ledger.${a.summary_code}`) : t("header.notif.topup_desc")),
      };
    case "redeem":
      return {
        title: amount ? t("header.notif.redeem_title", { amount }) : t("header.notif.redeem_title_plain"),
        desc: a.summary || t("header.notif.redeem_desc"),
      };
    case "extract":
      return {
        title: t("header.notif.extract_title", { count }),
        desc: amount
          ? t("header.notif.extract_desc_paid", { source: source || t("header.notif.source_fallback"), amount })
          : source || t("header.notif.extract_desc_fallback"),
      };
    case "into_bus":
      return {
        title: t("header.notif.into_bus_title", { count }),
        desc: target || source || t("header.notif.into_bus_desc"),
      };
    case "push":
      return {
        title: t("header.notif.push_title", { count }),
        desc: target || t("header.notif.push_desc"),
      };
    case "handoff":
      return {
        title: t("header.notif.handoff_title", { count }),
        desc: t("header.notif.handoff_desc"),
      };
  }
}

/** 顶栏铃铛 · 真通知列表（活动流派生 · 不加假数据）
 *  未读用本机 last-read 游标 · 阶段 1 不做跨设备已读同步 */
export function NotificationsBell() {
  const [open, setOpen] = useState(false);
  const [readAt, setReadAt] = useState(0);
  const { t, i18n } = useTranslation();
  const { data: me } = useMe();
  const { data: wallet } = useWallet();
  const { data: acts, refetch } = useActivities("30d");

  const pid = me?.id ?? "";

  useEffect(() => {
    if (pid) setReadAt(readAtOf(pid));
  }, [pid]);

  useEffect(() => {
    if (open) void refetch();
  }, [open, refetch]);

  const items = useMemo(() => (acts?.items ?? []).slice(0, BELL_LIMIT), [acts]);
  const unread = items.filter((a) => new Date(a.created_at).getTime() > readAt).length;
  const balance = wallet?.balance ?? 0;
  const low = wallet !== undefined && balance < LOW_BALANCE_CREDITS * MICRO;

  const onMarkAll = () => {
    if (!pid) return;
    markRead(pid);
    setReadAt(Date.now());
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="relative grid size-9 place-items-center rounded-full transition-colors hover:bg-bg-elevated focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/30"
          aria-label={t("header.notifications")}
        >
          <Bell className="size-4 text-fg-secondary" />
          {unread > 0 && (
            <span className="absolute right-1.5 top-1.5 size-2 rounded-full bg-brand ring-2 ring-bg" />
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(380px,calc(100vw-24px))] overflow-hidden p-0">
        <div className="flex items-center justify-between gap-3 px-4 py-3">
          <div className="flex items-center gap-2">
            <div className="font-semibold">{t("header.notifications_title")}</div>
            {unread > 0 && (
              <span className="grid min-w-5 place-items-center rounded-full bg-brand px-1.5 py-0.5 text-[10px] font-semibold leading-none text-white">
                {unread}
              </span>
            )}
          </div>
          <button
            type="button"
            onClick={onMarkAll}
            disabled={unread === 0}
            className="text-label font-semibold text-brand-strong disabled:text-fg-tertiary disabled:opacity-50"
          >
            {t("header.notifications_mark_all")}
          </button>
        </div>

        {low && (
          <Link
            to="/wallet"
            onClick={() => setOpen(false)}
            className="mx-3 mb-2 flex items-start gap-2.5 rounded-xl bg-warn-bg/80 px-3 py-2.5"
          >
            <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-warn-bg text-warn-fg">
              <AlertTriangle className="size-3.5" />
            </span>
            <span className="min-w-0">
              <span className="block font-semibold text-fg">{t("header.notif.low_title")}</span>
              <span className="block text-label text-fg-secondary">
                {t("header.notif.low_desc", { balance: fmtCredits(balance) })}
              </span>
            </span>
          </Link>
        )}

        {items.length === 0 ? (
          <div className="px-4 py-10 text-center">
            <div className="mx-auto mb-3 grid size-10 place-items-center rounded-full bg-bg-elevated text-fg-tertiary">
              <Bell className="size-4" />
            </div>
            <div className="font-semibold">{t("header.notifications_empty_title")}</div>
            <p className="mt-1 text-label text-fg-tertiary">{t("header.notifications_empty_desc")}</p>
          </div>
        ) : (
          <ul className="max-h-[min(420px,60vh)] overflow-y-auto border-t border-hairline">
            {items.map((a) => {
              const unreadItem = new Date(a.created_at).getTime() > readAt;
              const copy = itemCopy(a, t);
              const Icon = KIND_ICON[a.kind];
              const inner = (
                <>
                  <span
                    className={cn(
                      "mt-1 size-1.5 shrink-0 rounded-full",
                      unreadItem ? "bg-brand" : "bg-transparent",
                    )}
                  />
                  <span className={cn("grid size-7 shrink-0 place-items-center rounded-full", KIND_WRAP[a.kind])}>
                    <Icon className="size-3.5" strokeWidth={2.4} />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-start justify-between gap-2">
                      <span className="font-semibold leading-snug text-fg">{copy.title}</span>
                      <span className="shrink-0 text-[11px] text-fg-tertiary">
                        {fmtRelative(a.created_at, i18n.language)}
                      </span>
                    </span>
                    {copy.desc && (
                      <span className="mt-0.5 block truncate text-label text-fg-tertiary">{copy.desc}</span>
                    )}
                  </span>
                </>
              );
              // notifLink · 后端 link 优先 · 前端按 kind 兜底（refill 等后端未填的类别也能点）
              const to = notifLink(a);
              const cls = "flex items-start gap-2.5 px-3 py-3 transition-colors hover:bg-bg-elevated";
              return (
                <li key={a.id} className="border-b border-hairline last:border-b-0">
                  {to ? (
                    <Link to={to} onClick={() => setOpen(false)} className={cls}>
                      {inner}
                    </Link>
                  ) : (
                    <div className={cls}>{inner}</div>
                  )}
                </li>
              );
            })}
          </ul>
        )}

        <div className="border-t border-hairline px-4 py-2.5 text-center">
          <Link
            to="/notifications"
            onClick={() => setOpen(false)}
            className="text-label font-semibold text-brand-strong hover:opacity-80"
          >
            {t("header.notifications_view_all")}
            <span aria-hidden className="ml-0.5">→</span>
          </Link>
        </div>
      </PopoverContent>
    </Popover>
  );
}
