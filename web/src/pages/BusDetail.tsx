import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Activity as ActivityIcon, AlertTriangle, ArrowLeft, KeyRound, Trash2, Users, Zap, ZapOff,
} from "lucide-react";
import {
  useBus, useBusCredentials, useBusPulls, useDissolveBus,
} from "@/api/hooks";
import {
  BareHead, BareList, BareRow, Card, Chip, Segmented,
} from "@/components/ui/primitives";
import { KpiCard } from "@/components/KpiCard";
import { PullNowModal } from "@/components/PullNowModal";
import { EditStrategyPanel } from "@/components/EditStrategyPanel";
import {
  cn, fmtCredits, fmtLifespan, fmtTime, vendorName,
} from "@/lib/utils";
import type { Credential, PullResult, PullRound, PushState } from "@/types";

type TabKey = "credentials" | "pulls" | "strategy" | "members" | "danger";

const TABS: { value: TabKey; label: string }[] = [
  { value: "credentials", label: "号列表" },
  { value: "pulls", label: "拉号历史" },
  { value: "strategy", label: "补车策略" },
  { value: "members", label: "成员" },
  { value: "danger", label: "危险区" },
];

export default function BusDetail() {
  const { id } = useParams();
  const nav = useNavigate();
  const { data: bus } = useBus(id);
  const [tab, setTab] = useState<TabKey>("credentials");
  const [pullOpen, setPullOpen] = useState(false);

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
                <Users className="size-4 text-brand-strong" />
              </span>
              <h1 className="min-w-0 truncate text-hero font-semibold">{bus.name}</h1>
              <Chip tone={bus.status === "active" ? "ok" : "neutral"}>
                {bus.status === "active" ? "活跃" : "已解散"}
              </Chip>
            </div>
            <p className="text-fg-tertiary">
              {bus.kind === "single" ? "1 人车" : bus.kind === "team" ? "邀请码车" : "搭车"} ·{" "}
              {new Date(bus.created_at).toLocaleDateString("zh-CN")} 建 · 成员{" "}
              <span className="font-semibold tnum text-fg-secondary">{bus.member_count}</span>
            </p>
          </div>

          <button
            onClick={() => setPullOpen(true)}
            className="flex shrink-0 items-center gap-2 rounded-lg bg-brand px-4 py-2 font-semibold text-white shadow-card transition-opacity hover:opacity-90"
          >
            <KeyRound className="size-4" />
            立即拉号
          </button>
        </div>
      </div>

      {/* KPI · 4 卡 */}
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
        <KpiCard
          icon={ActivityIcon}
          label="活跃号"
          value={String(bus.alive_count)}
          unit="个"
          sub={<>失效 <span className="font-semibold tnum">{bus.dead_count}</span></>}
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
          icon={Users}
          label="补车模式"
          value={bus.strategy.auto_refill_enabled ? "自动" : "手动"}
          sub={
            bus.strategy.auto_refill_enabled ? (
              <>水位 <span className="font-semibold tnum">{bus.strategy.refill_watermark}</span></>
            ) : (
              "号少时提醒不自动拉"
            )
          }
        />
      </div>

      {/* Tab · 5 段 */}
      <div className="space-y-6">
        <Segmented options={TABS} value={tab} onChange={setTab} />

        {tab === "credentials" && <TabCredentials busId={id} />}
        {tab === "pulls" && <TabPulls busId={id} />}
        {tab === "strategy" && (
          <EditStrategyPanel busId={id} strategy={bus.strategy} />
        )}
        {tab === "members" && <TabMembers bus={bus} />}
        {tab === "danger" && (
          <TabDanger busId={id} name={bus.name} onDone={() => nav("/buses")} />
        )}
      </div>
    </div>
  );
}
/* ── Tab · 号列表 ── */

function TabCredentials({ busId }: { busId: string }) {
  const { data: creds } = useBusCredentials(busId);
  const items = creds ?? [];

  return (
    <Card className="p-7">
      <div className="mb-4 flex items-baseline justify-between gap-4">
        <div>
          <h2 className="text-section font-semibold">号列表</h2>
          <p className="text-label text-fg-tertiary">
            共 <span className="font-semibold tnum text-fg-secondary">{items.length}</span> 个 ·
            活 <span className="font-semibold tnum text-ok-fg">{items.filter((c) => c.status === "alive").length}</span>{" "}
            · 失效 <span className="font-semibold tnum text-danger-fg">{items.filter((c) => c.status === "dead").length}</span>
          </p>
        </div>
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
  const alive = c.status === "alive";
  return (
    <BareRow>
      <span className="w-16 shrink-0">
        <Chip tone={alive ? "ok" : "danger"} dot>
          {alive ? "活" : "失效"}
        </Chip>
      </span>

      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span className="truncate font-mono text-label font-medium text-fg-secondary">
          {c.key_masked}
        </span>
        <span className="shrink-0 whitespace-nowrap rounded-md border border-hairline bg-bg-elevated px-1.5 py-[1px] text-[10px] font-medium text-fg-secondary shadow-card">
          {vendorName(c.vendor_id)}
        </span>
      </span>

      <span className="w-20 shrink-0 text-center text-label font-medium tnum text-fg-secondary">
        {fmtLifespan(c.lifespan_seconds)}
      </span>

      <span className="w-24 shrink-0 text-center text-label font-semibold tnum">
        {fmtCredits(c.credits_used)}
        <span className="ml-0.5 font-medium text-fg-tertiary">积分</span>
      </span>

      <span className="w-20 shrink-0 text-center text-label">
        {c.pushed_at ? (
          <span className="text-ok-fg">✓ 已推</span>
        ) : c.push_failed ? (
          <span className="text-danger-fg">✗ 失败</span>
        ) : (
          <span className="text-fg-tertiary">未推</span>
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
  if (state === "pushed") return <Chip tone="ok">推池 ✓</Chip>;
  if (state === "partial") return <Chip tone="warn">部分推 {ratio}</Chip>;
  if (state === "failed") return <Chip tone="danger">推池 ✗</Chip>;
  return <span className="text-label text-fg-tertiary">未推</span>;
}

function TabPulls({ busId }: { busId: string }) {
  const { data: pulls } = useBusPulls(busId);
  const rounds = pulls ?? [];

  return (
    <Card className="p-7">
      <div className="mb-4">
        <h2 className="text-section font-semibold">拉号历史</h2>
        <p className="text-label text-fg-tertiary">
          共 <span className="font-semibold tnum text-fg-secondary">{rounds.length}</span> 轮 · 只列这辆车的
        </p>
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
  const res = RESULT[r.result];
  const failed = r.result === "failed";
  return (
    <BareRow>
      <span className="w-[86px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(r.created_at)}
      </span>
      <span className="w-14 shrink-0">
        <Chip tone={res.tone} className="w-full justify-center">{res.label}</Chip>
      </span>

      <span className="flex min-w-0 flex-1 items-center gap-2 truncate">
        {failed ? (
          <span className="truncate text-fg-tertiary">
            未拉到号 · 尝试 {vendorName(r.vendor_id)} · {r.fail_reason ?? "缺货"}
          </span>
        ) : (
          <>
            <span className="shrink-0 text-fg-secondary">共入车</span>
            <span className="shrink-0 font-semibold tnum text-fg">{r.count_purchased}</span>
            <span className="shrink-0 text-fg-secondary">个号，从</span>
            <span className="shrink-0 whitespace-nowrap rounded-md border border-hairline bg-bg-elevated px-2 py-[2px] text-label font-medium text-fg-secondary shadow-card">
              {vendorName(r.vendor_id)}
            </span>
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
/* ── Tab · 成员 · single 只显示自己 ── */

function TabMembers({ bus }: { bus: { kind: string; member_count: number } }) {
  if (bus.kind === "single") {
    return (
      <Card className="p-7">
        <h2 className="text-section font-semibold">成员</h2>
        <p className="mt-1 text-label text-fg-tertiary">1 人车 · 只有你</p>
        <div className="mt-5 flex items-center gap-3 rounded-lg bg-bg-elevated p-4">
          <span className="grid size-10 place-items-center rounded-full bg-brand-subtle font-semibold text-brand-strong">
            我
          </span>
          <div className="min-w-0 flex-1">
            <div className="font-semibold">我（发起人）</div>
            <div className="text-label text-fg-tertiary">独享号池 · 无邀请码</div>
          </div>
          <Chip tone="brand">我发起</Chip>
        </div>
      </Card>
    );
  }
  return (
    <Card className="p-7 text-center">
      <p className="text-fg-tertiary">多人车成员管理 · 阶段 2 开放</p>
    </Card>
  );
}

/* ── Tab · 危险区 · 解散 ── */

function TabDanger({
  busId, name, onDone,
}: { busId: string; name: string; onDone: () => void }) {
  const dissolve = useDissolveBus();
  const [confirmText, setConfirmText] = useState("");
  const canDissolve = confirmText === name;

  const onDissolve = async () => {
    if (!canDissolve) return;
    await dissolve.mutateAsync(busId);
    onDone();
  };

  return (
    <Card className="p-7">
      <div className="flex items-center gap-2">
        <AlertTriangle className="size-4 text-danger-fg" />
        <h2 className="text-section font-semibold text-danger-fg">危险区</h2>
      </div>
      <p className="mt-1 text-label text-fg-tertiary">
        解散车后：活号挪到你的提取记录 · 死号归档 · 已扣积分不退
      </p>

      <div className="mt-6 rounded-lg border border-danger-fg/20 bg-danger-bg/40 p-5">
        <div className="mb-3 font-semibold text-danger-fg">解散这辆车</div>
        <p className="mb-4 text-label text-fg-secondary">
          请输入车名 <span className="rounded bg-bg px-1.5 py-0.5 font-mono font-semibold">{name}</span> 确认
        </p>
        <div className="flex flex-col gap-2 sm:flex-row">
          <input
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder={name}
            className="flex-1 rounded-lg border border-hairline bg-bg px-3 py-2 focus:border-danger-fg focus:outline-none"
          />
          <button
            onClick={onDissolve}
            disabled={!canDissolve || dissolve.isPending}
            className="flex items-center justify-center gap-1.5 rounded-lg bg-danger-fg px-4 py-2 font-semibold text-white transition-opacity hover:opacity-90 disabled:opacity-45"
          >
            <Trash2 className="size-4" />
            {dissolve.isPending ? "解散中…" : "确认解散"}
          </button>
        </div>
      </div>
    </Card>
  );
}




