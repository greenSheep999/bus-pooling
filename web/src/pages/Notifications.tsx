import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
  AlertTriangle, ArrowUpFromLine, Bell, Check, Gift, KeyRound, X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useActivities, useMe, useWallet } from "@/api/hooks";
import { Card, SectionHead } from "@/components/ui/primitives";
import { EmptyState } from "@/components/ui/empty-state";
import { cn, fmtCredits, fmtRelative, MICRO } from "@/lib/utils";
import { notifLink } from "@/lib/notif-link";
import type { Activity, ActivityKind } from "@/types";

/** 通知全部页 · 铃铛"查看全部通知 →" 的落地
 *
 *  数据源跟铃铛同一个（useActivities）· 分组按时间（今天 / 本周 / 更早）·
 *  未读游标复用铃铛的 localStorage 键 · 进这个页就当"看过了"（推进游标） */

const LOW_BALANCE_CREDITS = 50;
const READ_KEY = (pid: string) => `bp.notif.readAt.${pid}`;

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

function itemCopy(
  a: Activity,
  t: (k: string, o?: Record<string, unknown>) => string,
): { title: string; desc: string } {
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

/** 按时间分三档：24h 内 / 本周 / 更早 —— 用户扫的时候一眼分得清"新的 vs 旧的" */
type Bucket = "today" | "week" | "older";

function bucketOf(iso: string): Bucket {
  const diff = Date.now() - new Date(iso).getTime();
  if (diff < 24 * 3600_000) return "today";
  if (diff < 7 * 24 * 3600_000) return "week";
  return "older";
}

export default function NotificationsPage() {
  const { t, i18n } = useTranslation();
  const { data: me } = useMe();
  const { data: wallet } = useWallet();
  // 30d 窗口 · 全部页比铃铛给更长的历史（铃铛只取 8 条）
  const { data: acts, isLoading } = useActivities("30d");
  const [readAt, setReadAt] = useState(0);

  const pid = me?.id ?? "";

  // 进这个页就当看过了 · 推进 last-read 游标（铃铛红点消失）
  useEffect(() => {
    if (!pid) return;
    const raw = localStorage.getItem(READ_KEY(pid));
    setReadAt(raw ? Number(raw) || 0 : 0);
    localStorage.setItem(READ_KEY(pid), String(Date.now()));
  }, [pid]);

  const items = acts?.items ?? [];
  const balance = wallet?.balance ?? 0;
  const low = wallet !== undefined && balance < LOW_BALANCE_CREDITS * MICRO;

  const grouped = useMemo(() => {
    const g: Record<Bucket, Activity[]> = { today: [], week: [], older: [] };
    for (const a of items) g[bucketOf(a.created_at)].push(a);
    return g;
  }, [items]);

  const bucketLabel: Record<Bucket, string> = {
    today: t("notifications-page.bucket-today"),
    week: t("notifications-page.bucket-week"),
    older: t("notifications-page.bucket-older"),
  };

  return (
    <div className="mx-auto w-full max-w-[820px] p-4 sm:p-6">
      <SectionHead title={t("notifications-page.title")} sub={t("notifications-page.sub")} />

      {low && (
        <Link
          to="/wallet"
          className="mt-5 flex items-start gap-2.5 rounded-2xl bg-warn-bg/80 px-4 py-3 transition-colors hover:bg-warn-bg"
        >
          <span className="grid size-8 shrink-0 place-items-center rounded-lg bg-warn-bg text-warn-fg">
            <AlertTriangle className="size-4" />
          </span>
          <span className="min-w-0">
            <span className="block font-semibold text-fg">{t("header.notif.low_title")}</span>
            <span className="block text-label text-fg-secondary">
              {t("header.notif.low_desc", { balance: fmtCredits(balance) })}
            </span>
          </span>
        </Link>
      )}

      {isLoading ? (
        <Card className="mt-5 p-8 text-center text-fg-tertiary">
          <span className="inline-block size-5 animate-spin rounded-full border-2 border-hairline border-t-brand-strong" />
        </Card>
      ) : items.length === 0 ? (
        <EmptyState
          icon={Bell}
          title={t("header.notifications_empty_title")}
          desc={t("header.notifications_empty_desc")}
          size="page"
          className="mt-5"
        />
      ) : (
        <div className="mt-5 space-y-6">
          {(["today", "week", "older"] as const).map((b) =>
            grouped[b].length === 0 ? null : (
              <section key={b}>
                <div className="mb-2 px-1 text-label font-semibold text-fg-tertiary">
                  {bucketLabel[b]}
                </div>
                <Card className="divide-y divide-hairline overflow-hidden p-0">
                  {grouped[b].map((a) => {
                    const unread = new Date(a.created_at).getTime() > readAt;
                    const copy = itemCopy(a, t);
                    const Icon = KIND_ICON[a.kind];
                    const inner = (
                      <>
                        <span
                          className={cn(
                            "mt-1.5 size-1.5 shrink-0 rounded-full",
                            unread ? "bg-brand" : "bg-transparent",
                          )}
                        />
                        <span className={cn("grid size-8 shrink-0 place-items-center rounded-full", KIND_WRAP[a.kind])}>
                          <Icon className="size-4" strokeWidth={2.4} />
                        </span>
                        <span className="min-w-0 flex-1">
                          <span className="flex items-start justify-between gap-3">
                            <span className="font-semibold leading-snug text-fg">{copy.title}</span>
                            <span className="shrink-0 text-label text-fg-tertiary">
                              {fmtRelative(a.created_at, i18n.language)}
                            </span>
                          </span>
                          {copy.desc && (
                            <span className="mt-0.5 block text-label text-fg-secondary">{copy.desc}</span>
                          )}
                        </span>
                      </>
                    );
                    // notifLink · 后端 link 优先 · 前端按 kind 兜底
                    const to = notifLink(a);
                    const cls = "flex items-start gap-3 px-4 py-3 transition-colors hover:bg-bg-elevated";
                    return to ? (
                      <Link key={a.id} to={to} className={cls}>{inner}</Link>
                    ) : (
                      <div key={a.id} className={cls}>{inner}</div>
                    );
                  })}
                </Card>
              </section>
            ),
          )}
        </div>
      )}
    </div>
  );
}
