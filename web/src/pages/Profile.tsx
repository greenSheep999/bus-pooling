import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import {
  ArrowUpRight, BadgeCheck, Bus, ChevronRight, Copy, Settings,
  Ticket, Wallet,
} from "lucide-react";
import {
  useBuses, useMe, useMyInvite, useOverview, useWallet,
} from "@/api/hooks";
import { useTranslation } from "react-i18next";
import { Card, Chip, SectionHead } from "@/components/ui/primitives";
import { avatarColor, avatarLetter, fmtCredits } from "@/lib/utils";

/** 我的 · Me 页面
 *  个人信息 + 快捷入口 + 个人维度的数据。不放设置子项（那是设置的事）。
 *  登出在头像下拉菜单里做（AppLayout），这里不重复。 */
export default function Profile() {
  const { t } = useTranslation("profile");
  const { data: me } = useMe();
  const { data: wallet } = useWallet();
  const { data: buses } = useBuses();
  const { data: invite } = useMyInvite();
  // 30d 是 Overview 页面默认口径 · Me 这里的"近 30 天"跟它对齐
  const { data: overview } = useOverview("30d");

  const av = avatarColor(me?.username ?? "?");
  const activeBuses = (buses?.items ?? []).filter((b) => b.status === "active").length;

  return (
    <div className="space-y-section">
      <div className="min-w-0 space-y-2">
        <h1 className="text-hero font-semibold">{t("title")}</h1>
        <p className="text-fg-tertiary">{t("subtitle")}</p>
      </div>

      {/* 顶部三栏 · 左：个人信息（占 2 栏宽）· 右：余额卡 + 车数卡（各 1 栏） */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-4">
        {/* 个人信息卡 · 大卡 · 左侧头像 + 右侧信息行列表 */}
        <Card className="p-7 lg:col-span-2">
          <div className="flex items-start gap-5">
            <span
              className="grid size-16 shrink-0 place-items-center rounded-full text-body-lg font-semibold"
              style={{ backgroundColor: av.bg, color: av.fg }}
            >
              {avatarLetter(me?.username ?? "?")}
            </span>
            <div className="min-w-0 flex-1 space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="truncate text-body-lg font-semibold">
                  {me?.username ?? "-"}
                </span>
                {/* 档次徽章 · 只有绑了专属邀请码的人才带 chip · 具体档次内部区分 · 对外不暴露 */}
                {me?.tier && me.tier !== "retail" && (
                  <Chip tone="brand" icon={<Ticket className="size-3" />}>{t("member.chip")}</Chip>
                )}
              </div>

              <div className="grid grid-cols-1 gap-x-6 gap-y-2 sm:grid-cols-2">
                <InfoLine label={t("info.email")}>
                  <span className="flex items-center gap-1.5">
                    <span className="truncate">{me?.email ?? "-"}</span>
                    {me?.email_verified && <BadgeCheck className="size-3.5 shrink-0 text-ok-fg" />}
                  </span>
                </InfoLine>
                <InfoLine label={t("info.identity")}>
                  {me?.tier && me.tier !== "retail"
                    ? <span className="text-ok-fg">{t("info.identity.member")}</span>
                    : <span className="text-fg-secondary">{t("info.identity.none")}</span>}
                </InfoLine>
                <InfoLine label={t("info.joined_at")}>
                  {me
                    ? new Date(me.created_at).toLocaleDateString("zh-CN", {
                        year: "numeric", month: "2-digit", day: "2-digit",
                      })
                    : "-"}
                </InfoLine>
                <InfoLine label={t("info.my_invite_code")}>
                  {invite?.code
                    ? <InviteCodeInline code={invite.code} />
                    : <span className="text-fg-tertiary">-</span>}
                </InfoLine>
              </div>
            </div>
          </div>
        </Card>

        {/* 余额卡 */}
        <MiniStatCard
          to="/wallet"
          icon={Wallet}
          label={t("card.balance.label")}
          value={wallet ? fmtCredits(wallet.balance) : "-"}
          hint={t("card.balance.hint")}
          tone="credit"
        />

        {/* 进行中车数卡 · 用 buses 的 length，跟 Overview KPI 的 alive_count 是两码事（那是号数） */}
        <MiniStatCard
          to="/buses"
          icon={Bus}
          label={t("card.buses.label")}
          value={String(activeBuses)}
          unit={activeBuses > 0 ? t("card.buses.unit") : undefined}
          hint={t("card.buses.hint")}
        />
      </div>

      {/* 累计数据 · 近 30 天口径（跟 Overview 默认时间范围一致） */}
      <div className="space-y-3">
        <SectionHead title={t("rollup.section.title")} sub={t("rollup.section.sub")} />
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <RollupStat
            label={t("rollup.topup")}
            value={overview ? fmtCredits(overview.kpi.balance_delta_topup) : "-"}
          />
          <RollupStat
            label={t("rollup.spend")}
            value={overview ? fmtCredits(overview.kpi.balance_delta_spend) : "-"}
          />
          <RollupStat
            label={t("rollup.pull")}
            value={overview ? String(overview.kpi.pull_total) : "-"}
            unit={overview && overview.kpi.pull_total > 0 ? t("rollup.pull.unit") : undefined}
          />
          <RollupStat
            label={t("rollup.invited")}
            value={invite ? String(invite.invited_count) : "-"}
            unit={invite && invite.invited_count > 0 ? t("rollup.invited.unit") : undefined}
          />
        </div>
      </div>

      {/* 快捷入口 · 3 大入口（不平铺设置子项） */}
      <div className="space-y-3">
        <SectionHead title={t("quick.section.title")} />
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <NavCard to="/wallet" icon={Wallet} title={t("quick.wallet.title")} desc={t("quick.wallet.desc")} />
          <NavCard to="/buses" icon={Bus} title={t("quick.buses.title")} desc={t("quick.buses.desc")} />
          <NavCard to="/settings" icon={Settings} title={t("quick.settings.title")} desc={t("quick.settings.desc")} />
        </div>
      </div>
    </div>
  );
}

/* ─── 子组件 ─────────────────────────────────────────────── */

function InfoLine({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline gap-2 text-label">
      <span className="shrink-0 text-fg-tertiary">{label}</span>
      <span className="min-w-0 truncate font-medium">{children}</span>
    </div>
  );
}

function InviteCodeInline({ code }: { code: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="font-mono">{code}</span>
      <button
        type="button"
        aria-label={useTranslation("profile").t("invite.copy_aria")}
        onClick={() => { navigator.clipboard?.writeText(code); }}
        className="rounded p-0.5 text-fg-tertiary transition-colors hover:bg-bg-elevated hover:text-fg-secondary"
      >
        <Copy className="size-3" />
      </button>
    </span>
  );
}

function MiniStatCard({
  to, icon: Icon, label, value, unit, hint, tone,
}: {
  to: string;
  icon: LucideIcon;
  label: string;
  value: string;
  unit?: string;
  hint: string;
  tone?: "credit";
}) {
  return (
    <Card to={to} className="flex flex-col justify-between p-6" focal={tone === "credit"} focalTone="credit">
      <div className="flex items-center gap-2.5">
        <span className={`grid size-7 shrink-0 place-items-center rounded-lg ${tone === "credit" ? "bg-credit-bg" : "bg-bg-elevated"}`}>
          <Icon className={`size-3.5 ${tone === "credit" ? "text-credit-fg" : "text-fg-secondary"}`} />
        </span>
        <span className="text-label font-medium tracking-wide text-fg-secondary">{label}</span>
      </div>
      <div className="mt-3 flex items-baseline gap-1">
        <span className="text-num font-semibold tnum">{value}</span>
        {unit && <span className="text-label text-fg-tertiary">{unit}</span>}
      </div>
      <div className="mt-3 flex items-center gap-1 text-label text-fg-tertiary">
        <span>{hint}</span>
        <ArrowUpRight className="size-3" />
      </div>
    </Card>
  );
}

function RollupStat({ label, value, unit }: { label: string; value: string; unit?: string }) {
  return (
    <Card className="p-5">
      <div className="text-label text-fg-tertiary">{label}</div>
      <div className="mt-1.5 flex items-baseline gap-1">
        <span className="text-body-lg font-semibold tnum">{value}</span>
        {unit && <span className="text-label text-fg-tertiary">{unit}</span>}
      </div>
    </Card>
  );
}

function NavCard({
  to, icon: Icon, title, desc,
}: { to: string; icon: LucideIcon; title: string; desc: string }) {
  return (
    <Card to={to} className="flex items-center gap-4 p-5">
      <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-bg-elevated">
        <Icon className="size-4 text-fg-secondary" />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="font-semibold">{title}</span>
          <ChevronRight className="size-3.5 text-fg-tertiary" />
        </div>
        <p className="text-label text-fg-tertiary">{desc}</p>
      </div>
    </Card>
  );
}
