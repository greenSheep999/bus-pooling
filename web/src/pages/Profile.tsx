import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import {
  ArrowUpRight, BadgeCheck, Bus, ChevronRight, Copy, Settings,
  Ticket, Wallet,
} from "lucide-react";
import { Link } from "react-router-dom";
import {
  useBuses, useMe, useMyInvite, useOverview, useWallet,
} from "@/api/hooks";
import { Card, Chip, SectionHead } from "@/components/ui/primitives";
import { avatarColor, avatarLetter, fmtCredits } from "@/lib/utils";

/** 我的 · Me 页面
 *  个人信息 + 快捷入口 + 个人维度的数据。不放设置子项（那是设置的事）。
 *  登出在头像下拉菜单里做（AppLayout），这里不重复。 */
export default function Profile() {
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
        <h1 className="text-hero font-semibold">我的</h1>
        <p className="text-fg-tertiary">账号信息与快捷入口</p>
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
                  <Chip tone="brand" icon={<Ticket className="size-3" />}>社群成员</Chip>
                )}
              </div>

              <div className="grid grid-cols-1 gap-x-6 gap-y-2 sm:grid-cols-2">
                <InfoLine label="邮箱">
                  <span className="flex items-center gap-1.5">
                    <span className="truncate">{me?.email ?? "-"}</span>
                    {me?.email_verified && <BadgeCheck className="size-3.5 shrink-0 text-ok-fg" />}
                  </span>
                </InfoLine>
                <InfoLine label="身份">
                  {me?.tier && me.tier !== "retail"
                    ? <span className="text-ok-fg">社群成员</span>
                    : <span className="text-fg-secondary">未加入</span>}
                </InfoLine>
                <InfoLine label="注册于">
                  {me
                    ? new Date(me.created_at).toLocaleDateString("zh-CN", {
                        year: "numeric", month: "2-digit", day: "2-digit",
                      })
                    : "-"}
                </InfoLine>
                <InfoLine label="我的邀请码">
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
          label="余额"
          value={wallet ? fmtCredits(wallet.balance) : "-"}
          hint="去充值"
          tone="credit"
        />

        {/* 进行中车数卡 · 用 buses 的 length，跟 Overview KPI 的 alive_count 是两码事（那是号数） */}
        <MiniStatCard
          to="/buses"
          icon={Bus}
          label="进行中的车"
          value={String(activeBuses)}
          unit={activeBuses > 0 ? "辆" : undefined}
          hint="去看车"
        />
      </div>

      {/* 累计数据 · 近 30 天口径（跟 Overview 默认时间范围一致） */}
      <div className="space-y-3">
        <SectionHead title="近 30 天" sub="更早的数据看概览页" />
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <RollupStat
            label="累计充值"
            value={overview ? fmtCredits(overview.kpi.balance_delta_topup) : "-"}
          />
          <RollupStat
            label="累计消费"
            value={overview ? fmtCredits(overview.kpi.balance_delta_spend) : "-"}
          />
          <RollupStat
            label="累计拉号"
            value={overview ? String(overview.kpi.pull_total) : "-"}
            unit={overview && overview.kpi.pull_total > 0 ? "个" : undefined}
          />
          <RollupStat
            label="邀请好友"
            value={invite ? String(invite.invited_count) : "-"}
            unit={invite && invite.invited_count > 0 ? "人" : undefined}
          />
        </div>
      </div>

      {/* 快捷入口 · 3 大入口（不平铺设置子项） */}
      <div className="space-y-3">
        <SectionHead title="快捷入口" />
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <NavCard to="/wallet" icon={Wallet} title="钱包 · 充值" desc="余额、账单、充值" />
          <NavCard to="/buses" icon={Bus} title="我的车" desc="查看和管理拼车" />
          <NavCard to="/settings" icon={Settings} title="设置" desc="号池 · 通知 · API key · 账号" />
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
        aria-label="复制邀请码"
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
