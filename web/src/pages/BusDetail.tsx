import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Activity as ActivityIcon, AlertTriangle, ArrowLeft, Bus as BusIcon, Check, Copy,
  KeyRound, RefreshCw, Send, Settings, Trash2, UserCheck, UserMinus, X, Zap, ZapOff,
} from "lucide-react";
import {
  useBus, useBusCredentials, useBusPulls, useDownstream, useMe,
  useRegenInviteCode, useRemoveMember, useSetMemberSuspended,
} from "@/api/hooks";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead,
} from "@/components/ui/primitives";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TokenTag, VendorTag } from "@/components/ui/tags";
import { KpiCard } from "@/components/KpiCard";
import { PullNowModal } from "@/components/PullNowModal";
import { BusSettingsModal } from "@/components/BusSettingsModal";
import { BusStats } from "@/components/BusStats";
import { EditStrategyPanel } from "@/components/EditStrategyPanel";
import {
  cn, fmtCredits, fmtLifespan, fmtTime, SUSPEND_AFTER, vendorLabel,
} from "@/lib/utils";
import type {
  Bus, BusMember, Credential, PullResult, PullRound, PushError, PushState,
} from "@/types";

type TabKey = "credentials" | "pulls" | "pushes" | "members" | "strategy" | "stats";

const TABS: { value: TabKey; label: string }[] = [
  { value: "credentials", label: "号列表" },
  { value: "pulls", label: "拉号历史" },
  { value: "pushes", label: "推送记录" },
  { value: "members", label: "成员" },
  { value: "strategy", label: "补车策略" },
  { value: "stats", label: "数据" },
];

export default function BusDetail() {
  const { id } = useParams();
  const nav = useNavigate();
  const { data: bus } = useBus(id);
  const [tab, setTab] = useState<TabKey>("credentials");
  const [pullOpen, setPullOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);

  if (!id) return null;
  if (!bus) return <div className="py-12 text-center text-fg-tertiary">加载中…</div>;

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
          返回拼车
        </Link>

        <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div className="min-w-0 space-y-2">
            <div className="flex items-center gap-3">
              <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-brand-subtle">
                <BusIcon className="size-4 text-brand-strong" />
              </span>
              <h1 className="min-w-0 truncate text-hero font-semibold">{bus.name}</h1>
              <Chip tone={bus.status === "active" ? "ok" : "neutral"}>
                {bus.status === "active" ? "活跃" : "已解散"}
              </Chip>
            </div>
            <p className="text-fg-tertiary">
              {bus.kind === "single" ? "1 人车" : bus.kind === "team" ? "邀请码车" : "搭车"} ·{" "}
              创建于 {new Date(bus.created_at).toLocaleDateString("zh-CN")} · 成员{" "}
              <Em>{bus.member_count}</Em>
            </p>
          </div>

          <div className="flex shrink-0 items-center gap-2">
            <Button variant="brand" onClick={() => setPullOpen(true)}>
              <KeyRound />
              立即拉号
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setSettingsOpen(true)}
              aria-label="车设置"
              title="车设置"
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
          label="正常号"
          value={String(bus.alive_count)}
          unit="个"
          sub={<>已失效 <span className="font-semibold tnum">{bus.dead_count}</span></>}
        />
        <KpiCard
          icon={ZapOff}
          label="今日消费"
          value={fmtCredits(bus.spend_today)}
          unit="积分"
          sub={bus.spend_today > 0 ? <span className="text-danger-fg font-semibold">今日已扣</span> : "今日无消费"}
        />
        <KpiCard
          icon={Zap}
          label="平均寿命"
          value={fmtLifespan(bus.avg_lifespan_seconds)}
          sub="按已挂号 / 号池全体计"
        />
        <KpiCard
          icon={bus.strategy.auto_refill_enabled ? Zap : ZapOff}
          label="补车模式"
          value={bus.strategy.auto_refill_enabled ? "自动" : "手动"}
          sub={
            bus.strategy.auto_refill_enabled ? (
              <>保活 <span className="font-semibold tnum">{bus.strategy.refill_watermark}</span></>
            ) : (
              "号少时提醒不自动拉"
            )
          }
        />
      </div>

      {/* Tab · 3 段 · 只保留运行时数据（配置类走 ⚙ 设置 modal） */}
      <Tabs value={tab} onValueChange={(v) => setTab(v as TabKey)} className="space-y-6">
        <TabsList>
          {TABS.map((t) => (
            <TabsTrigger key={t.value} value={t.value}>{t.label}</TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="credentials"><TabCredentials busId={id} /></TabsContent>
        <TabsContent value="pulls"><TabPulls busId={id} /></TabsContent>
        <TabsContent value="pushes"><TabPushes busId={id} /></TabsContent>
        <TabsContent value="members"><TabMembers bus={bus} /></TabsContent>
        <TabsContent value="strategy">
          <EditStrategyPanel busId={id} strategy={bus.strategy} />
        </TabsContent>
        <TabsContent value="stats"><BusStats busId={id} /></TabsContent>
      </Tabs>
    </div>
  );
}
/* ── Tab · 成员 · 挂起 / 移除 / 拉人（decisions §8.26） ── */

function TabMembers({ bus }: { bus: Bus }) {
  const { data: me } = useMe();
  const setSuspended = useSetMemberSuspended(bus.id);
  const removeMember = useRemoveMember(bus.id);
  const regenCode = useRegenInviteCode(bus.id);
  const [confirmRemove, setConfirmRemove] = useState<BusMember | null>(null);
  const [copied, setCopied] = useState(false);

  const members = bus.members ?? [];

  /* 1 人车没有成员管理这回事 —— 只有你，没人可挂起也没人可移除 */
  if (members.length <= 1) {
    return (
      <Card className="p-7">
        <SectionHead title="成员" sub="1 人车 · 只有你 · 号和积分都是你自己的" />
        <div className="mt-5 flex items-center gap-3 rounded-xl bg-bg-elevated p-4">
          <span className="grid size-10 shrink-0 place-items-center rounded-full bg-brand-subtle font-semibold text-brand-strong">
            我
          </span>
          <div className="min-w-0 flex-1">
            <div className="font-semibold">{me?.username ?? "我"}</div>
            <div className="text-label text-fg-tertiary">发起人 · 独享 · 无分摊</div>
          </div>
          <Chip tone="brand">我发起</Chip>
        </div>
      </Card>
    );
  }

  const onCopyCode = () => {
    if (!bus.invite_code) return;
    navigator.clipboard.writeText(bus.invite_code);
    setCopied(true);
    setTimeout(() => setCopied(false), 1600);
  };

  return (
    <div className="space-y-6">
      <Card className="p-7">
        <div className="mb-4">
          <SectionHead
            title="成员"
            sub={
              <>
                共 <Em>{members.length}</Em> 人 · 分摊比例加起来 100% ·
                拉号和派号时按这个比例从各人钱包扣
              </>
            }
          />
        </div>

        <BareList>
          <BareHead>
            <span className="w-[200px] shrink-0">成员</span>
            <span className="w-20 shrink-0 text-right">分摊</span>
            <span className="w-24 shrink-0 text-right">余额</span>
            <span className="min-w-0 flex-1">状态</span>
            <span className="w-[132px] shrink-0 text-right">操作</span>
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
          余额不够时该成员<Em plain>本次跳过</Em>
          （不扣他积分，也不给他取这批号）· 连续被跳过{" "}
          <Em>{SUSPEND_AFTER}</Em> 次自动挂起 ·
          他充值后自己就恢复，不用你批
        </p>
      </Card>

      {/* 拉人进车 · 靠邀请码（team 车才有） */}
      {bus.kind === "team" && (
        <Card className="p-7">
          <SectionHead
            title="拉人进车"
            sub="把邀请码给他 · 他注册/登录后填码进车 · 进车后分摊比例要全员确认才生效"
          />
          <div className="mt-4 flex flex-wrap items-center gap-2">
            <code className="rounded-xl border border-hairline bg-bg-elevated px-4 py-2.5 font-mono text-num font-semibold tracking-wider">
              {bus.invite_code ?? "—"}
            </code>
            <Button variant="ghost" onClick={onCopyCode} disabled={!bus.invite_code}>
              {copied ? <Check /> : <Copy />}
              {copied ? "已复制" : "复制"}
            </Button>
            <Button
              variant="ghost"
              onClick={() => regenCode.mutate()}
              disabled={regenCode.isPending}
              title="旧码立即失效"
            >
              <RefreshCw />
              换一个
            </Button>
          </div>
          <p className="mt-3 text-label text-fg-tertiary">换码后旧码立即失效，已进车的人不受影响</p>
        </Card>
      )}

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
            {m.role === "owner" && <Chip tone="brand">车主</Chip>}
          </span>
          <span className="block text-label text-fg-tertiary">
            {fmtTime(m.joined_at)} 进车
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
            <Chip tone="neutral" dot>已挂起</Chip>
            <span className="truncate text-fg-tertiary">取不到号 · 不参与分摊</span>
          </>
        ) : behind ? (
          <>
            <Chip tone="warn" dot>
              已跳过 {m.skipped_count}/{SUSPEND_AFTER}
            </Chip>
            <span className="truncate text-fg-tertiary">
              再 {SUSPEND_AFTER - m.skipped_count} 次自动挂起
            </span>
          </>
        ) : (
          <Chip tone="ok" dot>正常</Chip>
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
              {suspended ? "解挂" : "挂起"}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={onRemove}
              disabled={busy}
              aria-label={`移除 ${m.username}`}
              title="移除出车"
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
  return (
    <Dialog open={!!member} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[460px]">
        <DialogHeader>
          <DialogTitle>移除 {member?.username}？</DialogTitle>
          <p className="text-label text-fg-tertiary">他立刻取不到这辆车的号</p>
        </DialogHeader>
        <DialogBody>
          <Alert tone="warn" icon={AlertTriangle} title="剩下的人分摊比例要重算">
            他那 <span className="font-semibold tnum">{member?.share_pct}%</span> 要摊给其他人 ·
            这是改所有人的钱，要等全员确认才生效
          </Alert>
          <p className="mt-3 text-label text-fg-tertiary">
            只是他暂时没钱的话，用「挂起」更合适 —— 挂起不动分摊比例，他充值后自己就回来了
          </p>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>取消</Button>
          <Button variant="danger" onClick={onConfirm} disabled={pending}>
            {pending ? "移除中…" : "确认移除"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ── Tab · 号列表 ── */

function TabCredentials({ busId }: { busId: string }) {
  const { data: creds } = useBusCredentials(busId);
  const items = creds ?? [];

  return (
    <Card className="p-7">
      <div className="mb-4">
        <SectionHead
          title="号列表"
          sub={
            <>
              共 <Em>{items.length}</Em> 个 ·
              正常 <Em tone="ok">{items.filter((c) => c.status === "alive").length}</Em>{" "}
              · 失效 <Em tone="spend">{items.filter((c) => c.status === "dead").length}</Em>
            </>
          }
        />
      </div>

      <div className="overflow-x-auto">
        <div className="min-w-[680px]">
          <BareHead>
            <span className="w-16 shrink-0">状态</span>
            <span className="min-w-0 flex-1">号 · vendor</span>
            <span className="w-20 shrink-0 text-center">寿命</span>
            <span className="w-24 shrink-0 text-center">消耗</span>
            <span className="w-20 shrink-0 text-center">推池</span>
            <span className="w-20 shrink-0 text-right">拉入</span>
          </BareHead>
          <BareList>
            {items.map((c) => <CredentialRow key={c.id} c={c} />)}
          </BareList>
        </div>
      </div>
    </Card>
  );
}

function CredentialRow({ c }: { c: Credential }) {
  const { data: me } = useMe();
  const alive = c.status === "alive";
  return (
    <BareRow>
      <span className="w-16 shrink-0">
        <Chip tone={alive ? "ok" : "danger"} dot>
          {alive ? "正常" : "已失效"}
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
        <span className="ml-0.5 font-medium text-fg-tertiary">积分</span>
      </span>

      <span className="flex w-20 shrink-0 justify-center">
        {c.pushed_at ? (
          <Chip tone="ok" icon={<Check className="size-3" />}>已推</Chip>
        ) : c.push_failed ? (
          <Chip tone="danger" icon={<X className="size-3" />}>推失败</Chip>
        ) : (
          <Chip tone="neutral">未推</Chip>
        )}
      </span>

      <span className="w-20 shrink-0 text-right text-label font-medium tnum text-fg-tertiary">
        {fmtTime(c.pulled_at)}
      </span>
    </BareRow>
  );
}
/* ── Tab · 拉号历史 ── */

const RESULT: Record<PullResult, { label: string; tone: "ok" | "warn" | "danger" | "brand" }> = {
  success: { label: "成功", tone: "ok" },
  partial: { label: "部分", tone: "warn" },
  failed: { label: "失败", tone: "danger" },
  refunded: { label: "退款", tone: "brand" },
};

function PushCell({ state, ratio }: { state: PushState; ratio: string | null }) {
  if (state === "pushed") return <Chip tone="ok" icon={<Check className="size-3" />}>已推</Chip>;
  if (state === "partial") return <Chip tone="warn" icon={<Check className="size-3" />}>部分推 {ratio}</Chip>;
  if (state === "failed") return <Chip tone="danger" icon={<X className="size-3" />}>推失败</Chip>;
  return <Chip tone="neutral">未推</Chip>;
}

function TabPulls({ busId }: { busId: string }) {
  const { data: pulls } = useBusPulls(busId);
  const rounds = pulls ?? [];

  return (
    <Card className="p-7">
      <div className="mb-4">
        <SectionHead
          title="拉号历史"
          sub={<>共 <Em>{rounds.length}</Em> 轮 · 只列这辆车的</>}
        />
      </div>

      <div className="overflow-x-auto">
        <div className="min-w-[720px]">
          <BareHead>
            <span className="w-[86px] shrink-0">时间</span>
            <span className="w-14 shrink-0">结果</span>
            <span className="min-w-0 flex-1">流向</span>
            <span className="w-20 shrink-0 text-center">号状态</span>
            <span className="w-24 shrink-0 text-center">推池</span>
            <span className="w-24 shrink-0 text-right">花费</span>
          </BareHead>
          <BareList>
            {rounds.map((r) => <PullRow key={r.id} r={r} />)}
          </BareList>
        </div>
      </div>
    </Card>
  );
}

function PullRow({ r }: { r: PullRound }) {
  const { data: me } = useMe();
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
            未拉到号 · 尝试 {vendorLabel(r.vendor_id, !!me?.invited)} · {r.fail_reason ?? "缺货"}
          </span>
        ) : (
          <>
            <span className="shrink-0 text-fg-secondary">共入车</span>
            <span className="shrink-0 font-semibold tnum text-fg">{r.count_purchased}</span>
            <span className="shrink-0 text-fg-secondary">个号，从</span>
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
        {r.total_cost === 0 ? "0" : fmtCredits(r.total_cost, { sign: true })}{" "}
        <span className="font-medium text-fg-tertiary">积分</span>
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
          title="推送记录"
          sub={
            <>
              号从我方推给你号池的事件 · 共 <Em>{events.length}</Em> 条 ·
              成功 <Em tone="ok">{success}</Em> · 失败 <Em tone="spend">{failed}</Em>
            </>
          }
        />
      </div>

      {events.length === 0 ? (
        <div className="grid place-items-center gap-3 py-12 text-center">
          <span className="grid size-10 place-items-center rounded-full bg-bg-elevated">
            <Send className="size-4 text-fg-tertiary" />
          </span>
          <div>
            <div className="font-semibold">还没有推送记录</div>
            <p className="mt-0.5 text-label text-fg-tertiary">
              配置了「推我的号池」的号才会有推送记录 · 在 设置 · 我的号池 里配 URL
            </p>
          </div>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <div className="min-w-[760px]">
            <BareHead>
              <span className="w-[92px] shrink-0">时间</span>
              <span className="w-24 shrink-0">状态</span>
              <span className="min-w-0 flex-1">号 · vendor</span>
              <span className="min-w-0 flex-[1.1]">去向 / 失败原因</span>
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
  const { data: me } = useMe();
  return (
    <BareRow>
      <span className="w-[92px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(e.time)}
      </span>
      <span className="w-24 shrink-0">
        {e.status === "success" ? (
          <Chip tone="ok" icon={<Check className="size-3" />}>已推</Chip>
        ) : (
          <Chip tone="danger" icon={<X className="size-3" />}>推失败</Chip>
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
              <span className="text-fg-tertiary">我的号池</span>
            )}
          </>
        )}
      </span>

      {/* 失败行的操作 · 按 retriable 分：能重试的给「重试」，不能的引导去改配置 */}
      <span className="flex w-28 shrink-0 items-center justify-end gap-1">
        {e.status === "failed" && e.error && (
          e.error.retriable ? (
            <Button variant="ghost" size="sm" title={`已试 ${e.error.attempts} 次`}>
              <RefreshCw />
              重试
            </Button>
          ) : (
            <Button variant="ghost" size="sm" asChild>
              <Link to="/settings/downstream">
                <Settings />
                去检查
              </Link>
            </Button>
          )
        )}
      </span>
    </BareRow>
  );
}




