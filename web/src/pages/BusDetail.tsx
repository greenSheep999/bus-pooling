import { useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Activity as ActivityIcon, AlertTriangle, ArrowLeft, Bus as BusIcon, Check,
  KeyRound, RefreshCw, Send, Settings, X, Zap, ZapOff,
} from "lucide-react";
import {
  useBus, useBusCredentials, useBusPulls, useDownstream, useMe,
} from "@/api/hooks";
import {
  BareHead, BareList, BareRow, Card, Chip,
} from "@/components/ui/primitives";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TokenTag, VendorTag } from "@/components/ui/tags";
import { KpiCard } from "@/components/KpiCard";
import { PullNowModal } from "@/components/PullNowModal";
import { BusSettingsModal } from "@/components/BusSettingsModal";
import { BusStats } from "@/components/BusStats";
import { EditStrategyPanel } from "@/components/EditStrategyPanel";
import {
  cn, fmtCredits, fmtLifespan, fmtTime, vendorLabel,
} from "@/lib/utils";
import type { Credential, PullResult, PullRound, PushState } from "@/types";

type TabKey = "credentials" | "pulls" | "pushes" | "strategy" | "stats";

const TABS: { value: TabKey; label: string }[] = [
  { value: "credentials", label: "号列表" },
  { value: "pulls", label: "拉号历史" },
  { value: "pushes", label: "推送记录" },
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
              <span className="font-semibold tnum text-fg-secondary">{bus.member_count}</span>
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
        <TabsContent value="strategy">
          <EditStrategyPanel busId={id} strategy={bus.strategy} />
        </TabsContent>
        <TabsContent value="stats"><BusStats busId={id} /></TabsContent>
      </Tabs>
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
            正常 <span className="font-semibold tnum text-ok-fg">{items.filter((c) => c.status === "alive").length}</span>{" "}
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
  /** 失败原因 · 售后追溯（decisions §8.24）· 客服靠这个判断是用户配错还是我方问题 */
  error: string | null;
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
      <div className="mb-4 flex items-baseline justify-between gap-4">
        <div>
          <h2 className="text-section font-semibold">推送记录</h2>
          <p className="text-label text-fg-tertiary">
            号从我方推给你号池的事件 · 共{" "}
            <span className="font-semibold tnum text-fg-secondary">{events.length}</span> 条 · 成功{" "}
            <span className="font-semibold tnum text-ok-fg">{success}</span> · 失败{" "}
            <span className="font-semibold tnum text-danger-fg">{failed}</span>
          </p>
        </div>
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




