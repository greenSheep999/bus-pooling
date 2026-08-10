import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Trans, useTranslation } from "react-i18next";
import {
  Activity as ActivityIcon, AlertTriangle, ArrowLeft, Bus as BusIcon, Check,
  KeyRound, Link2 as LinkIcon, RefreshCw, Send, Settings, Trash2, UserCheck, UserMinus,
  X, Zap, ZapOff,
} from "lucide-react";
import {
  useBus, useBusCredentials, useBusPulls, useDownstream, useMe,
  useRegenInviteCode, useRemoveMember, useSetMemberSuspended,
} from "@/api/hooks";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead,
} from "@/components/ui/primitives";
import { lazy, Suspense } from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { SkeletonTable } from "@/components/ui/skeleton";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TokenTag, VendorTag } from "@/components/ui/tags";
import { KpiCard } from "@/components/KpiCard";
import { PullNowModal } from "@/components/PullNowModal";
import { BusSettingsModal } from "@/components/BusSettingsModal";
// 图表放 stats tab · 用户点开这个 tab 才拉 recharts
const BusStats = lazy(() => import("@/components/BusStats"));
import { EditStrategyPanel } from "@/components/EditStrategyPanel";
import {
  cn, fmtCredits, fmtLifespan, fmtTime, SUSPEND_AFTER, vendorLabel,
} from "@/lib/utils";
import type {
  Bus, BusMember, Credential, PullResult, PullRound, PushError, PushState,
} from "@/types";

type TabKey = "credentials" | "pulls" | "pushes" | "members" | "strategy" | "stats";

export default function BusDetail() {
  const { t } = useTranslation("buses");
  const TABS: { value: TabKey; label: string }[] = [
    { value: "credentials", label: t("tabs.credentials") },
    { value: "pulls", label: t("tabs.pulls") },
    { value: "pushes", label: t("tabs.pushes") },
    { value: "members", label: t("tabs.members") },
    { value: "strategy", label: t("tabs.strategy") },
    { value: "stats", label: t("tabs.stats") },
  ];
  const { id } = useParams();
  const nav = useNavigate();
  const { data: bus } = useBus(id);
  const [tab, setTab] = useState<TabKey>("credentials");
  const [headerCopied, setHeaderCopied] = useState(false);
  const [pullOpen, setPullOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);

  if (!id) return null;
  if (!bus) return <div className="py-12 text-center text-fg-tertiary">{t("loading")}</div>;

  return (
    <div className="space-y-section">
      <PullNowModal
        open={pullOpen}
        onClose={() => setPullOpen(false)}
        busId={id}
        defaultCount={bus.strategy.per_round_count ?? 3}
        preferredVendor={bus.strategy.preferred_vendor}
        maxUnitPrice={bus.strategy.max_unit_price}
      />
      <BusSettingsModal
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        bus={bus}
        onDissolved={() => nav("/buses")}
      />

      {/* Hero */}
      <div className="space-y-4">
        <Link
          to="/buses"
          className="inline-flex items-center gap-1 text-label font-medium text-fg-tertiary transition-colors hover:text-fg-secondary"
        >
          <ArrowLeft className="size-3.5" />
          {t("back")}
        </Link>

        <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div className="min-w-0 space-y-2">
            <div className="flex items-center gap-3">
              <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-brand-subtle">
                <BusIcon className="size-4 text-brand-strong" />
              </span>
              <h1 className="min-w-0 truncate text-hero font-semibold">{bus.name}</h1>
              <Chip tone={bus.status === "active" ? "ok" : "neutral"}>
                {bus.status === "active" ? t("status.active") : t("status.dissolved")}
              </Chip>
            </div>
            <p className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-fg-tertiary">
              <span>
                {bus.kind === "anon"
                  ? t("kind.anon")
                  : bus.member_count > 1
                    ? t("kind.multi", { count: bus.member_count })
                    : t("kind.solo")} ·{" "}
                {t("created-at", { date: new Date(bus.created_at).toLocaleDateString() })}
              </span>
              {/* 头部只放一个动作：复制邀请链接。码不在这儿露（成员 tab 里的链接看得到）·
                  它是"独享变拼车"的入口·不该埋在第 4 个 tab 里才找得到 */}
              {bus.kind !== "anon" && bus.invite_code && (
                <>
                  <span>·</span>
                  <span>{t("invite.code-label")}</span>
                  {/* 码当纯文本显示（可选中口述 / 手输）· 不给它单独的复制按钮 */}
                  <code className="font-mono font-semibold tracking-wider text-fg">
                    {bus.invite_code}
                  </code>
                  <button
                    type="button"
                    onClick={() => {
                      navigator.clipboard.writeText(
                        `${window.location.origin}/join/${bus.invite_code}`,
                      );
                      setHeaderCopied(true);
                      setTimeout(() => setHeaderCopied(false), 1600);
                    }}
                    title={t("invite.copy-link-title")}
                    className="inline-flex items-center gap-1 font-medium text-brand-strong underline-offset-2 hover:underline"
                  >
                    {headerCopied ? <Check className="size-3.5" /> : <LinkIcon className="size-3.5" />}
                    {headerCopied ? t("invite.link-copied") : t("invite.copy-link")}
                  </button>
                </>
              )}
            </p>
          </div>

          <div className="flex shrink-0 items-center gap-2">
            <Button variant="brand" onClick={() => setPullOpen(true)}>
              <KeyRound />
              {t("action.pull-now")}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setSettingsOpen(true)}
              aria-label={t("action.settings")}
              title={t("action.settings")}
            >
              <Settings />
            </Button>
          </div>
        </div>
      </div>

      {/* KPI · 4 卡 */}
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          icon={ActivityIcon}
          label={t("kpi.alive.label")}
          value={String(bus.alive_count)}
          unit={t("kpi.alive.unit")}
          sub={<Trans t={t} i18nKey="kpi.alive.sub" values={{ count: bus.dead_count }} components={{ 1: <span className="font-semibold tnum" /> }} />}
        />
        <KpiCard
          icon={ZapOff}
          label={t("kpi.spend-today.label")}
          value={fmtCredits(bus.spend_today)}
          unit={t("kpi.spend-today.unit")}
          sub={bus.spend_today > 0 ? <span className="text-danger-fg font-semibold">{t("kpi.spend-today.sub-spent")}</span> : t("kpi.spend-today.sub-empty")}
        />
        <KpiCard
          icon={Zap}
          label={t("kpi.lifespan.label")}
          value={fmtLifespan(bus.avg_lifespan_seconds)}
          sub={t("kpi.lifespan.sub")}
        />
        <KpiCard
          icon={bus.strategy.auto_refill_enabled ? Zap : ZapOff}
          label={t("kpi.refill.label")}
          value={bus.strategy.auto_refill_enabled ? t("kpi.refill.auto") : t("kpi.refill.manual")}
          sub={
            bus.strategy.auto_refill_enabled ? (
              <Trans t={t} i18nKey="kpi.refill.watermark" values={{ count: bus.strategy.refill_watermark }} components={{ 1: <span className="font-semibold tnum" /> }} />
            ) : (
              t("kpi.refill.manual-sub")
            )
          }
        />
      </div>

      {/* Tab · 3 段 · 只保留运行时数据（配置类走 ⚙ 设置 modal） */}
      <Tabs value={tab} onValueChange={(v) => setTab(v as TabKey)} className="space-y-6">
        <TabsList>
          {TABS.map((item) => (
            <TabsTrigger key={item.value} value={item.value}>{t(item.labelKey)}</TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="credentials"><TabCredentials busId={id} /></TabsContent>
        <TabsContent value="pulls"><TabPulls busId={id} /></TabsContent>
        <TabsContent value="pushes"><TabPushes busId={id} /></TabsContent>
        <TabsContent value="members"><TabMembers bus={bus} /></TabsContent>
        <TabsContent value="strategy">
          <EditStrategyPanel busId={id} strategy={bus.strategy} />
        </TabsContent>
        <TabsContent value="stats">
          <Suspense fallback={<div className="p-6 text-sm text-muted-foreground">{t("stats.loading")}</div>}>
            <BusStats busId={id} />
          </Suspense>
        </TabsContent>
      </Tabs>
    </div>
  );
}
/* ── Tab · 成员 · 挂起 / 移除 / 拉人（decisions §8.26） ── */

function TabMembers({ bus }: { bus: Bus }) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  const setSuspended = useSetMemberSuspended(bus.id);
  const removeMember = useRemoveMember(bus.id);
  const regenCode = useRegenInviteCode(bus.id);
  const [confirmRemove, setConfirmRemove] = useState<BusMember | null>(null);
  const [copied, setCopied] = useState(false); // 复制邀请链接

  const members = bus.members ?? [];

  // 邀请链接 · 朋友点开直接进车（未登录先引导登录再自动回来加入）
  const inviteLink = bus.invite_code
    ? `${window.location.origin}/join/${bus.invite_code}`
    : "";

  const onCopyLink = () => {
    if (!inviteLink) return;
    navigator.clipboard.writeText(inviteLink);
    setCopied(true);
    setTimeout(() => setCopied(false), 1600);
  };

  /* 邀请卡片 · 1 人独享和多人拼车**都要显示** ——
     邀请链接就是"从独享变拼车"的唯一入口·1 人时最需要它。
     只有系统撮合池（anon）没码（谁进由撮合决定）。
     只留一个动作：复制链接。码本身在链接尾巴上看得见·不再单独给复制按钮。 */
  const inviteCard = bus.kind !== "anon" && (
    <Card className="p-7">
      <SectionHead title={t("members.invite.title")} sub={t("members.invite.sub")} />

      {/* 码放大显示 —— 当面口述 / 让对方在「输拼车码加入」手输时用 */}
      <div className="mt-4 flex items-baseline gap-2">
        <span className="text-label text-fg-tertiary">{t("invite.code-label")}</span>
        <code className="select-all font-mono text-num font-semibold tracking-widest">
          {bus.invite_code ?? t("members.invite.code-dash")}
        </code>
      </div>

      {/* 链接 + 唯一的复制按钮（发微信 / TG 就用这个） */}
      <div className="mt-3 flex flex-col gap-2 sm:flex-row">
        <input
          readOnly
          value={inviteLink || t("members.invite.link-placeholder")}
          onFocus={(e) => e.currentTarget.select()}
          className="h-10 min-w-0 flex-1 rounded-xl border border-hairline bg-bg-elevated px-3 font-mono text-label text-fg-secondary outline-none focus:border-brand"
        />
        <Button className="h-10 shrink-0" onClick={onCopyLink} disabled={!inviteLink}>
          {copied ? <Check /> : <LinkIcon />}
          {copied ? t("members.invite.copied") : t("members.invite.copy")}
        </Button>
      </div>

      <p className="mt-3 text-label text-fg-tertiary">
        {t("members.invite.leaked-question")}
        <button
          type="button"
          onClick={() => regenCode.mutate()}
          disabled={regenCode.isPending}
          className="ml-1 font-medium text-brand-strong underline-offset-2 hover:underline disabled:opacity-50"
        >
          {t("members.invite.regen")}
        </button>
        {" "}{t("members.invite.regen-note")}
      </p>
    </Card>
  );

  /* 只有你时·没有成员管理这回事 —— 没人可挂起也没人可移除·但拼车码要给 */
  if (members.length <= 1) {
    return (
      <div className="space-y-6">
        <Card className="p-7">
          <SectionHead title={t("members.solo.title")} sub={t("members.solo.sub")} />
          <div className="mt-5 flex items-center gap-3 rounded-xl bg-bg-elevated p-4">
            <span className="grid size-10 shrink-0 place-items-center rounded-full bg-brand-subtle font-semibold text-brand-strong">
              {t("members.solo.me-avatar")}
            </span>
            <div className="min-w-0 flex-1">
              <div className="font-semibold">{me?.username ?? t("members.solo.me-fallback")}</div>
              <div className="text-label text-fg-tertiary">{t("members.solo.role-line")}</div>
            </div>
            <Chip tone="brand">{t("members.solo.me-chip")}</Chip>
          </div>
        </Card>
        {inviteCard}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <Card className="p-7">
        <div className="mb-4">
          <SectionHead
            title={t("members.list.title")}
            sub={
              <Trans t={t} i18nKey="members.list.sub" values={{ count: members.length }} components={{ 1: <Em /> }} />
            }
          />
        </div>

        <BareList>
          <BareHead>
            <span className="w-[200px] shrink-0">{t("members.header.member")}</span>
            <span className="w-20 shrink-0 text-right">{t("members.header.share")}</span>
            <span className="w-24 shrink-0 text-right">{t("members.header.balance")}</span>
            <span className="min-w-0 flex-1">{t("members.header.status")}</span>
            <span className="w-[132px] shrink-0 text-right">{t("members.header.actions")}</span>
          </BareHead>
          {members.map((m) => (
            <MemberRow
              key={m.passenger_id}
              m={m}
              isMe={m.passenger_id === me?.id}
              busy={setSuspended.isPending || removeMember.isPending}
              onToggleSuspend={() =>
                setSuspended.mutate({
                  memberId: m.passenger_id,
                  suspended: m.status !== "suspended",
                })
              }
              onRemove={() => setConfirmRemove(m)}
            />
          ))}
        </BareList>

        {/* 挂起规则 · 就一句话说清什么时候会自动挂起 */}
        <p className="mt-4 text-label text-fg-tertiary">
          <Trans t={t} i18nKey="members.suspend-note" values={{ limit: SUSPEND_AFTER }} components={{ 1: <Em plain />, 3: <Em /> }} />
        </p>
      </Card>

      {inviteCard}

      {/* 移除确认 · 会改其他人的分摊比例，得说清楚 */}
      <RemoveMemberModal
        member={confirmRemove}
        onClose={() => setConfirmRemove(null)}
        pending={removeMember.isPending}
        onConfirm={async () => {
          if (!confirmRemove) return;
          await removeMember.mutateAsync(confirmRemove.passenger_id);
          setConfirmRemove(null);
        }}
      />
    </div>
  );
}

function MemberRow({
  m, isMe, busy, onToggleSuspend, onRemove,
}: {
  m: BusMember;
  isMe: boolean;
  busy: boolean;
  onToggleSuspend: () => void;
  onRemove: () => void;
}) {
  const { t } = useTranslation("buses");
  const suspended = m.status === "suspended";
  /* 「余额够不够」是相对下一轮花多少的 · 那个数现在不知道（价格随行就市）
     所以不猜 —— 只报后端记下的事实：他被跳过过几次 */
  const behind = !suspended && m.skipped_count > 0;

  return (
    <BareRow>
      <span className="flex w-[200px] shrink-0 items-center gap-2.5">
        <span
          className={cn(
            "grid size-8 shrink-0 place-items-center rounded-full text-label font-semibold",
            suspended
              ? "bg-bg-elevated text-fg-tertiary"
              : isMe
                ? "bg-brand-subtle text-brand-strong"
                : "bg-bg-elevated text-fg-secondary",
          )}
        >
          {m.username.slice(0, 2)}
        </span>
        <span className="min-w-0">
          <span className="flex items-center gap-1.5">
            <span className={cn("truncate font-semibold", suspended && "text-fg-tertiary")}>
              {m.username}
            </span>
            {m.role === "owner" && <Chip tone="brand">{t("members.role.owner")}</Chip>}
          </span>
          <span className="block text-label text-fg-tertiary">
            {t("members.row.joined-at", { time: fmtTime(m.joined_at) })}
          </span>
        </span>
      </span>

      {/* 挂起不改 share_pct（这正是它跟"移除"的区别）· 所以不划掉，只调淡 */}
      <span
        className={cn(
          "w-20 shrink-0 text-right font-semibold tnum",
          suspended && "text-fg-tertiary",
        )}
      >
        {m.share_pct}%
      </span>

      <span
        className={cn(
          "w-24 shrink-0 text-right text-label font-medium tnum",
          behind ? "text-warn-fg" : "text-fg-secondary",
        )}
      >
        {fmtCredits(m.balance)}
      </span>

      <span className="flex min-w-0 flex-1 items-center gap-2 text-label">
        {suspended ? (
          <>
            <Chip tone="neutral" dot>{t("members.status.suspended")}</Chip>
            <span className="truncate text-fg-tertiary">{t("members.status.suspended-desc")}</span>
          </>
        ) : behind ? (
          <>
            <Chip tone="warn" dot>
              {t("members.status.skipped", { count: m.skipped_count, limit: SUSPEND_AFTER })}
            </Chip>
            <span className="truncate text-fg-tertiary">
              {t("members.status.skipped-desc", { remain: SUSPEND_AFTER - m.skipped_count })}
            </span>
          </>
        ) : (
          <Chip tone="ok" dot>{t("members.status.normal")}</Chip>
        )}
      </span>

      {/* 车主自己不能挂起 / 移除自己 —— 要退出走「车设置 → 解散」 */}
      <span className="flex w-[132px] shrink-0 items-center justify-end gap-1">
        {m.role === "owner" ? (
          <span className="text-label text-fg-tertiary">—</span>
        ) : (
          <>
            <Button variant="ghost" size="sm" onClick={onToggleSuspend} disabled={busy}>
              {suspended ? <UserCheck /> : <UserMinus />}
              {suspended ? t("members.action.unsuspend") : t("members.action.suspend")}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={onRemove}
              disabled={busy}
              aria-label={t("members.action.remove-aria", { name: m.username })}
              title={t("members.action.remove-title")}
            >
              <Trash2 />
            </Button>
          </>
        )}
      </span>
    </BareRow>
  );
}

function RemoveMemberModal({
  member, onClose, onConfirm, pending,
}: {
  member: BusMember | null;
  onClose: () => void;
  onConfirm: () => void;
  pending: boolean;
}) {
  const { t } = useTranslation("buses");
  const { t: tCommon } = useTranslation();
  return (
    <Dialog open={!!member} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[460px]">
        <DialogHeader>
          <DialogTitle>{t("remove.title", { name: member?.username })}</DialogTitle>
          <p className="text-label text-fg-tertiary">{t("remove.subtitle")}</p>
        </DialogHeader>
        <DialogBody>
          <Alert tone="warn" icon={AlertTriangle} title={t("remove.alert-title")}>
            <Trans t={t} i18nKey="remove.alert-body" values={{ share: member?.share_pct }} components={{ 1: <span className="font-semibold tnum" /> }} />
          </Alert>
          <p className="mt-3 text-label text-fg-tertiary">
            {t("remove.hint")}
          </p>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>{tCommon("action.cancel")}</Button>
          <Button variant="danger" onClick={onConfirm} disabled={pending}>
            {pending ? t("remove.action.confirming") : t("remove.action.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ── Tab · 号列表 ── */

function TabCredentials({ busId }: { busId: string }) {
  const { t } = useTranslation("buses");
  const { data: creds, isLoading } = useBusCredentials(busId);
  const items = creds ?? [];

  return (
    <Card className="p-7">
      <div className="mb-4">
        <SectionHead
          title={t("credentials.title")}
          sub={
            <Trans t={t} i18nKey="credentials.sub" values={{ count: items.length, alive: items.filter((c) => c.status === "alive").length, dead: items.filter((c) => c.status === "dead").length }} components={{ 1: <Em />, 3: <Em tone="ok" />, 5: <Em tone="spend" /> }} />
          }
        />
      </div>

      {isLoading && !creds ? (
        <SkeletonTable rows={5} cols={["w-14", "w-1/3", "w-16", "w-20", "w-16", "w-16"]} />
      ) : items.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title={t("credentials.empty.title")}
          desc={t("credentials.empty.desc")}
        />
      ) : (
      <div className="overflow-x-auto">
        <div className="min-w-[680px]">
          <BareHead>
            <span className="w-16 shrink-0">{t("credentials.header.status")}</span>
            <span className="min-w-0 flex-1">{t("credentials.header.key-vendor")}</span>
            <span className="w-20 shrink-0 text-center">{t("credentials.header.lifespan")}</span>
            <span className="w-24 shrink-0 text-center">{t("credentials.header.usage")}</span>
            <span className="w-20 shrink-0 text-center">{t("credentials.header.push")}</span>
            <span className="w-20 shrink-0 text-right">{t("credentials.header.pulled")}</span>
          </BareHead>
          <BareList>
            {items.map((c) => <CredentialRow key={c.id} c={c} />)}
          </BareList>
        </div>
      </div>
      )}
    </Card>
  );
}

function CredentialRow({ c }: { c: Credential }) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  const alive = c.status === "alive";
  return (
    <BareRow>
      <span className="w-16 shrink-0">
        <Chip tone={alive ? "ok" : "danger"} dot>
          {alive ? t("credentials.status.alive") : t("credentials.status.dead")}
        </Chip>
      </span>

      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span className="truncate font-mono text-label font-medium text-fg-secondary">
          {c.key_masked}
        </span>
        <VendorTag name={vendorLabel(c.vendor_id, !!me?.invited)} />
      </span>

      <span className="w-20 shrink-0 text-center text-label font-medium tnum text-fg-secondary">
        {fmtLifespan(c.lifespan_seconds)}
      </span>

      <span className="w-24 shrink-0 text-center text-label font-semibold tnum">
        {fmtCredits(c.credits_used)}
        <span className="ml-0.5 font-medium text-fg-tertiary">{t("credentials.unit.credits")}</span>
      </span>

      <span className="flex w-20 shrink-0 justify-center">
        {c.pushed_at ? (
          <Chip tone="ok" icon={<Check className="size-3" />}>{t("credentials.push.pushed")}</Chip>
        ) : c.push_failed ? (
          <Chip tone="danger" icon={<X className="size-3" />}>{t("credentials.push.failed")}</Chip>
        ) : (
          <Chip tone="neutral">{t("credentials.push.none")}</Chip>
        )}
      </span>

      <span className="w-20 shrink-0 text-right text-label font-medium tnum text-fg-tertiary">
        {fmtTime(c.pulled_at)}
      </span>
    </BareRow>
  );
}
/* ── Tab · 拉号历史 ── */

function useResultMap(): Record<PullResult, { label: string; tone: "ok" | "warn" | "danger" | "brand" }> {
  const { t } = useTranslation("buses");
  return {
    success: { label: t("pulls.result.success"), tone: "ok" },
    partial: { label: t("pulls.result.partial"), tone: "warn" },
    failed: { label: t("pulls.result.failed"), tone: "danger" },
    refunded: { label: t("pulls.result.refunded"), tone: "brand" },
  };
}

function PushCell({ state, ratio }: { state: PushState; ratio: string | null }) {
  const { t } = useTranslation("buses");
  if (state === "pushed") return <Chip tone="ok" icon={<Check className="size-3" />}>{t("pulls.push.pushed")}</Chip>;
  if (state === "partial") return <Chip tone="warn" icon={<Check className="size-3" />}>{t("pulls.push.partial", { ratio })}</Chip>;
  if (state === "failed") return <Chip tone="danger" icon={<X className="size-3" />}>{t("pulls.push.failed")}</Chip>;
  return <Chip tone="neutral">{t("pulls.push.none")}</Chip>;
}

function TabPulls({ busId }: { busId: string }) {
  const { t } = useTranslation("buses");
  const { data: pulls, isLoading } = useBusPulls(busId);
  const rounds = pulls ?? [];

  return (
    <Card className="p-7">
      <div className="mb-4">
        <SectionHead
          title={t("pulls.title")}
          sub={<Trans t={t} i18nKey="pulls.sub" values={{ count: rounds.length }} components={{ 1: <Em /> }} />}
        />
      </div>

      {isLoading && !pulls ? (
        <SkeletonTable rows={5} cols={["w-20", "w-14", "w-1/3", "w-16", "w-20", "w-20"]} />
      ) : rounds.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title={t("pulls.empty.title")}
          desc={t("pulls.empty.desc")}
        />
      ) : (
      <div className="overflow-x-auto">
        <div className="min-w-[720px]">
          <BareHead>
            <span className="w-[86px] shrink-0">{t("pulls.header.time")}</span>
            <span className="w-14 shrink-0">{t("pulls.header.result")}</span>
            <span className="min-w-0 flex-1">{t("pulls.header.flow")}</span>
            <span className="w-20 shrink-0 text-center">{t("pulls.header.key-status")}</span>
            <span className="w-24 shrink-0 text-center">{t("pulls.header.push")}</span>
            <span className="w-24 shrink-0 text-right">{t("pulls.header.cost")}</span>
          </BareHead>
          <BareList>
            {rounds.map((r) => <PullRow key={r.id} r={r} />)}
          </BareList>
        </div>
      </div>
      )}
    </Card>
  );
}

function PullRow({ r }: { r: PullRound }) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  const RESULT = useResultMap();
  const res = RESULT[r.result];
  const failed = r.result === "failed";
  return (
    <BareRow>
      <span className="w-[86px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(r.created_at)}
      </span>
      <span className="w-14 shrink-0">
        <Chip tone={res.tone} dot className="w-full justify-center">{res.label}</Chip>
      </span>

      <span className="flex min-w-0 flex-1 items-center gap-2 truncate">
        {failed ? (
          <span className="truncate text-fg-tertiary">
            {t("pulls.row.failed", { vendor: vendorLabel(r.vendor_id, !!me?.invited), reason: r.fail_reason ?? t("pulls.row.fail-reason-default") })}
          </span>
        ) : (
          <>
            <span className="shrink-0 text-fg-secondary">{t("pulls.row.flow-prefix")}</span>
            <span className="shrink-0 font-semibold tnum text-fg">{r.count_purchased}</span>
            <span className="shrink-0 text-fg-secondary">{t("pulls.row.flow-suffix")}</span>
            <VendorTag name={vendorLabel(r.vendor_id, !!me?.invited)} size="sm" />
          </>
        )}
      </span>

      <span className="flex w-20 shrink-0 items-center justify-center gap-2 text-label">
        {r.alive_count > 0 && (
          <span className="flex items-center gap-1 text-fg-secondary">
            <span className="size-1.5 rounded-full bg-ok-solid" />
            {r.alive_count}
          </span>
        )}
        {r.dead_count > 0 && (
          <span className="flex items-center gap-1 text-fg-secondary">
            <span className="size-1.5 rounded-full bg-danger-solid" />
            {r.dead_count}
          </span>
        )}
        {r.alive_count === 0 && r.dead_count === 0 && (
          <span className="text-fg-tertiary">-</span>
        )}
      </span>

      <span className="flex w-24 shrink-0 justify-center">
        <PushCell state={r.push_state} ratio={r.push_ratio} />
      </span>

      <span
        className={cn(
          "w-24 shrink-0 text-right font-semibold tnum",
          r.total_cost > 0 ? "text-ok-fg" : r.total_cost === 0 ? "text-fg-tertiary" : "text-fg",
        )}
      >
        {r.total_cost === 0 ? t("pulls.row.zero-cost") : fmtCredits(r.total_cost, { sign: true })}{" "}
        <span className="font-medium text-fg-tertiary">{t("pulls.row.credits")}</span>
      </span>
    </BareRow>
  );
}
/* ── Tab · 推送记录 ── 从 credentials 派生（pushed_at != null 或 push_failed）
   与拉号历史独立：拉号 = "vendor 出号"事件；推送 = "号出我方 → 进你的号池"事件
   去向 = 乘客配的 passengerpool URL（来自 useDownstream · 一个乘客只有一个号池） */

type PushEvent = {
  id: string;
  time: string;
  credId: string;
  keyMasked: string;
  vendorId: string;
  status: "success" | "failed";
  /** 失败原因 · 结构化（decisions §8.24）· 客服靠 code / status 判断是用户配错还是我方问题 */
  error: PushError | null;
};

/** 把 URL 简化成 host 展示（"https://pool.foo.com/api" → "pool.foo.com"）· 失败回退原串 */
function hostOf(url: string): string {
  try { return new URL(url).host; } catch { return url; }
}

function TabPushes({ busId }: { busId: string }) {
  const { t } = useTranslation("buses");
  const { data: creds } = useBusCredentials(busId);
  const { data: downstream } = useDownstream();

  const targetHost = downstream?.passengerpool_url
    ? hostOf(downstream.passengerpool_url)
    : null;

  const events = useMemo<PushEvent[]>(() => {
    const items = creds ?? [];
    const out: PushEvent[] = [];
    for (const c of items) {
      if (c.push_failed) {
        out.push({
          id: `${c.id}-failed`, time: c.pulled_at, credId: c.id,
          keyMasked: c.key_masked, vendorId: c.vendor_id, status: "failed",
          error: c.push_error,
        });
      }
      if (c.pushed_at) {
        out.push({
          id: `${c.id}-pushed`, time: c.pushed_at, credId: c.id,
          keyMasked: c.key_masked, vendorId: c.vendor_id, status: "success",
          error: null,
        });
      }
    }
    return out.sort((a, b) => new Date(b.time).getTime() - new Date(a.time).getTime());
  }, [creds]);

  const success = events.filter((e) => e.status === "success").length;
  const failed = events.filter((e) => e.status === "failed").length;

  return (
    <Card className="p-7">
      <div className="mb-4">
        <SectionHead
          title={t("pushes.title")}
          sub={
            <Trans t={t} i18nKey="pushes.sub" values={{ count: events.length, success, failed }} components={{ 1: <Em />, 3: <Em tone="ok" />, 5: <Em tone="spend" /> }} />
          }
        />
      </div>

      {events.length === 0 ? (
        <EmptyState
          icon={Send}
          title={t("pushes.empty.title")}
          desc={t("pushes.empty.desc")}
        />
      ) : (
        <div className="overflow-x-auto">
          <div className="min-w-[760px]">
            <BareHead>
              <span className="w-[92px] shrink-0">{t("pushes.header.time")}</span>
              <span className="w-24 shrink-0">{t("pushes.header.status")}</span>
              <span className="min-w-0 flex-1">{t("pushes.header.key-vendor")}</span>
              <span className="min-w-0 flex-[1.1]">{t("pushes.header.target-or-reason")}</span>
              <span className="w-28 shrink-0" />
            </BareHead>
            <BareList>
              {events.map((e) => <PushRow key={e.id} e={e} targetHost={targetHost} />)}
            </BareList>
          </div>
        </div>
      )}
    </Card>
  );
}

function PushRow({
  e, targetHost,
}: { e: PushEvent; targetHost: string | null }) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  return (
    <BareRow>
      <span className="w-[92px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(e.time)}
      </span>
      <span className="w-24 shrink-0">
        {e.status === "success" ? (
          <Chip tone="ok" icon={<Check className="size-3" />}>{t("pushes.status.success")}</Chip>
        ) : (
          <Chip tone="danger" icon={<X className="size-3" />}>{t("pushes.status.failed")}</Chip>
        )}
      </span>
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span className="truncate font-mono text-label font-medium text-fg-secondary">
          {e.keyMasked}
        </span>
        <VendorTag name={vendorLabel(e.vendorId, !!me?.invited)} />
      </span>
      <span className="flex min-w-0 flex-[1.1] items-center gap-2 text-label">
        {e.status === "failed" && e.error ? (
          /* 失败原因 + 状态码 · 售后追溯要的就是这个（decisions §8.24） */
          <span className="flex min-w-0 items-center gap-1.5 text-warn-fg">
            <AlertTriangle className="size-3.5 shrink-0" />
            <span className="truncate" title={e.error.message}>{e.error.message}</span>
            {e.error.status && (
              <span className="shrink-0 rounded bg-warn-bg px-1 py-px text-[10px] font-semibold tnum">
                {e.error.status}
              </span>
            )}
          </span>
        ) : (
          <>
            <span className="text-fg-tertiary">→</span>
            {targetHost ? (
              <TokenTag size="sm">
                <Send className="size-3" />
                <span className="ml-1 truncate">{targetHost}</span>
              </TokenTag>
            ) : (
              <span className="text-fg-tertiary">{t("pushes.target.default")}</span>
            )}
          </>
        )}
      </span>

      {/* 失败行的操作 · 按 retriable 分：能重试的给「重试」，不能的引导去改配置 */}
      <span className="flex w-28 shrink-0 items-center justify-end gap-1">
        {e.status === "failed" && e.error && (
          e.error.retriable ? (
            <Button variant="ghost" size="sm" title={t("pushes.action.retry-title", { count: e.error.attempts })}>
              <RefreshCw />
              {t("pushes.action.retry")}
            </Button>
          ) : (
            <Button variant="ghost" size="sm" asChild>
              <Link to="/settings/downstream">
                <Settings />
                {t("pushes.action.check")}
              </Link>
            </Button>
          )
        )}
      </span>
    </BareRow>
  );
}




