import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AlertTriangle, Bus as BusIcon, Check, ChevronRight, Download, KeyRound, Send,
} from "lucide-react";
/** 品牌幽灵 · viewBox 精确 = 幽灵实际边界（无透明留白）· 外层 className 控大小和位置
 *  viewBox 56×75（比例 ≈ 3:4）· className 传 w/h 保持这个比例 */
const BrandGhost = (props: React.SVGProps<SVGSVGElement>) => (
  <svg viewBox="22 14 59 77" fill="none" xmlns="http://www.w3.org/2000/svg" {...props}>
    {/* 脸 · 白 */}
    <path d="M34.4585 71.0686C27.2879 86.9479 42.5607 90.934 53.8257 81.6404C57.1402 92.0605 69.5533 84.2833 74.016 76.2029C83.8296 58.3955 79.8652 40.2415 78.847 36.4937C71.8713 10.9525 36.9932 10.9092 30.9924 36.6237C29.5843 41.1297 29.5626 46.2423 28.7827 51.5498C28.3928 54.2361 28.0895 55.9475 27.0713 58.7638C26.4647 60.3885 25.6632 61.8183 24.3634 64.2446C22.3703 68.0141 23.2152 75.2713 33.527 71.5019L34.5019 71.0686H34.4585Z" fill="#fff" />
    {/* 左眼 · 黑 */}
    <path d="M55.1688 47.5639C52.3092 47.5639 51.876 44.1411 51.876 42.1047C51.876 40.2633 52.2009 38.8119 52.8292 37.8804C53.3708 37.0571 54.1723 36.6455 55.1688 36.6455C56.1653 36.6455 57.0319 37.0571 57.6385 37.902C58.3317 38.8552 58.7 40.3067 58.7 42.1047C58.7 45.5276 57.3785 47.5639 55.1905 47.5639H55.1688Z" fill="#000" />
    {/* 右眼 · 黑 */}
    <path d="M66.9319 47.5639C64.0723 47.5639 63.6391 44.1411 63.6391 42.1047C63.6391 40.2633 63.964 38.8119 64.5922 37.8804C65.1338 37.0571 65.9354 36.6455 66.9319 36.6455C67.9284 36.6455 68.795 37.0571 69.4015 37.902C70.0948 38.8552 70.463 40.3067 70.463 42.1047C70.463 45.5276 69.1416 47.5639 66.9536 47.5639H66.9319Z" fill="#000" />
  </svg>
);
import {
  useAssignEvents, useDownstream, useExtractEvents, useMe, usePullRecords,
} from "@/api/hooks";
import { AssignModal } from "@/components/AssignModal";
import { PullExtractForm } from "@/components/PullExtractForm";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead,
} from "@/components/ui/primitives";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { BulkActionBar } from "@/components/ui/bulk-action-bar";
import { Checkbox } from "@/components/ui/checkbox";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TokenTag, VendorTag } from "@/components/ui/tags";
import {
  cn, fmtCredits, fmtLifespan, fmtTime, vendorLabel,
} from "@/lib/utils";
import { EmptyState } from "@/components/ui/empty-state";
import { SkeletonTable } from "@/components/ui/skeleton";
import type { AssignEvent, Credential, ExtractEvent, PullResult } from "@/types";

type TabKey = "pending" | "extract-history" | "assign-history";

const TABS: { value: TabKey; labelKey: string }[] = [
  { value: "pending", labelKey: "tabs.pending" },
  { value: "extract-history", labelKey: "tabs.extract-history" },
  { value: "assign-history", labelKey: "tabs.assign-history" },
];

export default function Extract() {
  const { t } = useTranslation("extract");
  const { data: records } = usePullRecords();
  const { data: downstream } = useDownstream();
  const items = records?.items ?? [];

  const [assignOpen, setAssignOpen] = useState(false);
  /** 从悬浮栏带进弹窗的去向 · 跳过弹窗里再选一遍 */
  const [assignKind, setAssignKind] = useState<"into_bus" | "push_pool" | "handoff">("into_bus");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [tab, setTab] = useState<TabKey>("pending");

  const selectedRecords = useMemo(
    () => items.filter((c) => selected.has(c.id)),
    [items, selected],
  );

  const passengerpoolOk = !!downstream?.connected;

  /** 从悬浮栏挑去向 → 开弹窗确认 */
  const startAssign = (kind: "into_bus" | "push_pool" | "handoff") => {
    setAssignKind(kind);
    setAssignOpen(true);
  };

  return (
    <div className="space-y-section">
      <AssignModal
        open={assignOpen}
        onClose={() => { setAssignOpen(false); setSelected(new Set()); }}
        records={selectedRecords}
        passengerpoolConnected={passengerpoolOk}
        presetKind={assignKind}
      />

      {/* Hero */}
      <div className="min-w-0 space-y-2">
        <h1 className="text-hero font-semibold">{t("hero.title")}</h1>
        <p className="text-fg-tertiary">
          {t("hero.desc")}
          <span className="mx-1"><TokenTag>{t("hero.tag.into-bus")}</TokenTag></span>
          <span className="mx-1"><TokenTag>{t("hero.tag.push-pool")}</TokenTag></span>
          <span className="mx-1"><TokenTag>{t("hero.tag.handoff")}</TokenTag></span>
        </p>
      </div>

      {/* 提号 · focal 大 card · 页面主操作面板 · 右上白色 K 幽灵半露出 card */}
      <Card focal focalTone="brand" className="relative p-7">
        {/* 品牌幽灵 · viewBox 56x75 (3:4) · 外框 w-40 h-52 · 右上角 · 装饰不可点 */}
        <BrandGhost
          aria-hidden
          className="pointer-events-none absolute right-6 top-4 z-0 h-52 w-40 opacity-90"
        />
        {/* 内容层 · z-10 叠在幽灵上但 · 幽灵通过下方 form 卡的透明背景透出 */}
        <div className="relative z-10">
          <div className="mb-5 flex items-center gap-2.5">
            <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-brand-subtle">
              <KeyRound className="size-4 text-brand-strong" />
            </span>
            <div className="min-w-0 space-y-1">
              <h2 className="text-section font-semibold">{t("form.card.title")}</h2>
              <p className="text-label text-fg-tertiary">{t("form.card.sub")}</p>
            </div>
          </div>
          <PullExtractForm />
        </div>
      </Card>

      {!passengerpoolOk && (
        <Alert tone="warn" icon={AlertTriangle} title={t("downstream.warn.title")}>
          {t("downstream.warn.prefix")}
          <a href="/settings/downstream" className="font-semibold text-brand-strong hover:underline">
            {t("downstream.warn.link")}
          </a>
          {t("downstream.warn.suffix")}
        </Alert>
      )}

      {/* Tabs · 3 段 */}
      <Tabs value={tab} onValueChange={(v) => setTab(v as TabKey)} className="space-y-6">
        <TabsList>
          {TABS.map((tb) => (
            <TabsTrigger key={tb.value} value={tb.value}>{t(tb.labelKey)}</TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="pending">
          <PendingTab
            items={items}
            selected={selected}
            setSelected={setSelected}
          />
        </TabsContent>
        <TabsContent value="extract-history"><ExtractHistoryTab /></TabsContent>
        <TabsContent value="assign-history"><AssignHistoryTab /></TabsContent>
      </Tabs>

      {/* 批量操作悬浮栏 · 只在待派 tab 且有选中时出现
          三个去向直接给按钮 —— 跳过"先开弹窗再选去向"这一步 */}
      <BulkActionBar
        open={tab === "pending" && selected.size > 0}
        count={selected.size}
        onClear={() => setSelected(new Set())}
      >
        <Button variant="brand" size="sm" onClick={() => startAssign("into_bus")}>
          <BusIcon />
          {t("action.into-bus")}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => startAssign("push_pool")}
          disabled={!passengerpoolOk}
          title={passengerpoolOk ? undefined : t("action.push-pool.disabled-hint")}
        >
          <Send />
          {t("action.push-pool")}
        </Button>
        <Button variant="ghost" size="sm" onClick={() => startAssign("handoff")}>
          <Download />
          {t("action.handoff")}
        </Button>
      </BulkActionBar>
    </div>
  );
}

/* ─────────────── Tab · 待派 ─────────────── */

function PendingTab({
  items, selected, setSelected,
}: {
  items: Credential[];
  selected: Set<string>;
  setSelected: React.Dispatch<React.SetStateAction<Set<string>>>;
}) {
  const { t } = useTranslation("extract");
  const toggle = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  /* 全选只选可用的 · 失效号不能派 · decisions §8.25 */
  const usable = items.filter((c) => c.status !== "dead");
  const toggleAll = () => {
    if (selected.size === usable.length) setSelected(new Set());
    else setSelected(new Set(usable.map((c) => c.id)));
  };

  const totalCredits = items.reduce((s, c) => s + c.credits_used, 0);
  const vendors = new Set(items.map((c) => c.vendor_id)).size;

  return (
    <Card className="p-7">
      {/* 标题区不放操作按钮 —— 列表长了要滚回来才能点
          批量操作走底部悬浮栏（BulkActionBar），始终在手边 */}
      <div className="mb-4">
        <SectionHead
          title={t("pending.section.title")}
          sub={
            items.length > 0 ? (
              <>
                {t("pending.section.sub.usable-prefix")}<Em>{usable.length}</Em>{t("pending.section.sub.usable-suffix")}
                {items.length > usable.length && (
                  <>{t("pending.section.sub.dead-sep")}<span className="font-semibold tnum text-danger-fg">
                    {items.length - usable.length}
                  </span>{t("pending.section.sub.dead-suffix")}</>
                )}
                {t("pending.section.sub.from")}
                <span className="font-semibold tnum">{vendors}</span>{t("pending.section.sub.vendor-mid")}
                <span className="font-semibold tnum">{fmtCredits(totalCredits)}</span>{t("pending.section.sub.credits-tip")}
              </>
            ) : (
              t("pending.section.sub.empty")
            )
          }
        />
      </div>

      {items.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title={t("pending.empty.title")}
          desc={t("pending.empty.desc")}
        />
      ) : (
        <div className="overflow-x-auto">
          <div className="min-w-[720px]">
            <BareHead>
              <span className="w-8 shrink-0 pl-2">
                <Checkbox
                  checked={selected.size === usable.length && usable.length > 0}
                  onCheckedChange={toggleAll}
                />
              </span>
              <span className="min-w-0 flex-1">{t("pending.col.key")}</span>
              <span className="w-14 shrink-0 text-center">{t("pending.col.region")}</span>
              <span className="w-16 shrink-0 text-center">{t("pending.col.lifespan")}</span>
              <span className="w-20 shrink-0 text-center">{t("pending.col.used")}</span>
              <span className="w-24 shrink-0 text-right">{t("pending.col.pulled-at")}</span>
            </BareHead>
            <BareList>
              {items.map((c) => (
                <RecordRow
                  key={c.id} c={c}
                  picked={selected.has(c.id)}
                  onToggle={() => toggle(c.id)}
                />
              ))}
            </BareList>
          </div>
        </div>
      )}
    </Card>
  );
}

function RecordRow({
  c, picked, onToggle,
}: { c: Credential; picked: boolean; onToggle: () => void }) {
  const { t } = useTranslation("extract");
  const { data: me } = useMe();
  /* 失效的号不能派（推不上去、进车也没意义）· checkbox disable · decisions §8.25 */
  const dead = c.status === "dead";
  /* 质保内失效 = 可退 · 过了质保只能认（拉下来放太久没派的情况） */
  const inWarranty =
    dead && c.warranty_until != null && new Date(c.warranty_until) > new Date();

  return (
    <BareRow
      onClick={dead ? undefined : onToggle}
      className={cn(picked && "bg-brand-subtle/40", dead && "opacity-55")}
    >
      <span className="w-8 shrink-0 pl-2">
        <Checkbox
          checked={picked}
          disabled={dead}
          onCheckedChange={onToggle}
          onClick={(e) => e.stopPropagation()}
        />
      </span>
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span
          className={cn(
            "truncate font-mono text-label font-medium",
            dead ? "text-fg-tertiary line-through" : "text-fg-secondary",
          )}
        >
          {c.key_masked}
        </span>
        <VendorTag name={vendorLabel(c.vendor_id, !!me?.invited)} />
        {/* 状态标记 · 正常 / 已失效（质保内的标出来，能退） */}
        {dead ? (
          inWarranty ? (
            <Chip tone="warn" dot>{t("row.chip.warranty-refund")}</Chip>
          ) : (
            <Chip tone="danger" dot>{t("row.chip.dead")}</Chip>
          )
        ) : (
          <Chip tone="ok" dot>{t("row.chip.alive")}</Chip>
        )}
      </span>
      <span className="w-14 shrink-0 text-center">
        {c.region ? (
          <span className="text-label font-medium text-fg-secondary">
            {c.region.startsWith("us") ? "us" : c.region.startsWith("eu") ? "eu" : c.region}
          </span>
        ) : (
          <span className="text-fg-tertiary">—</span>
        )}
      </span>
      <span className="w-16 shrink-0 text-center text-label font-medium tnum text-fg-secondary">
        {fmtLifespan(c.lifespan_seconds)}
      </span>
      <span className="w-20 shrink-0 text-center text-label font-semibold tnum">
        {fmtCredits(c.credits_used)}
        <span className="ml-0.5 font-medium text-fg-tertiary">{t("unit.credits")}</span>
      </span>
      <span className="w-24 shrink-0 text-right text-label font-medium tnum text-fg-tertiary">
        {fmtTime(c.pulled_at)}
      </span>
    </BareRow>
  );
}

/* ─────────────── Tab · 提取历史 ─────────────── */

const EXTRACT_RESULT: Record<PullResult, { labelKey: string; tone: "ok" | "warn" | "danger" | "brand" }> = {
  success: { labelKey: "extract-result.success", tone: "ok" },
  partial: { labelKey: "extract-result.partial", tone: "warn" },
  failed: { labelKey: "extract-result.failed", tone: "danger" },
  refunded: { labelKey: "extract-result.refunded", tone: "brand" },
};

function ExtractHistoryTab() {
  const { t } = useTranslation("extract");
  const { data, isLoading } = useExtractEvents();
  const events = data?.items ?? [];

  return (
    <Card className="p-7">
      <div className="mb-4">
        <SectionHead
          title={t("extract-history.section.title")}
          sub={<>{t("extract-history.section.sub.prefix")}<Em>{events.length}</Em>{t("extract-history.section.sub.suffix")}</>}
        />
      </div>

      {isLoading && !data ? (
        <SkeletonTable rows={5} cols={["w-20", "w-14", "w-1/4", "w-16", "w-20", "w-20"]} />
      ) : events.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title={t("extract-history.empty.title")}
          desc={t("extract-history.empty.desc")}
        />
      ) : (
        <div className="overflow-x-auto">
          <div className="min-w-[760px]">
            <BareHead>
              <span className="w-[92px] shrink-0">{t("extract-history.col.time")}</span>
              <span className="w-16 shrink-0">{t("extract-history.col.result")}</span>
              <span className="min-w-0 flex-1">{t("extract-history.col.vendor-zone")}</span>
              <span className="w-24 shrink-0 text-center">{t("extract-history.col.count")}</span>
              <span className="w-24 shrink-0 text-center">{t("extract-history.col.progress")}</span>
              <span className="w-24 shrink-0 text-right">{t("extract-history.col.cost")}</span>
            </BareHead>
            <BareList>
              {events.map((e) => <ExtractEventRow key={e.id} e={e} />)}
            </BareList>
          </div>
        </div>
      )}
    </Card>
  );
}

function ExtractEventRow({ e }: { e: ExtractEvent }) {
  const { t } = useTranslation("extract");
  const { data: me } = useMe();
  const res = EXTRACT_RESULT[e.result];
  const failed = e.result === "failed";
  return (
    <BareRow>
      <span className="w-[92px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(e.created_at)}
      </span>
      <span className="w-16 shrink-0">
        <Chip tone={res.tone} dot>{t(res.labelKey)}</Chip>
      </span>
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <VendorTag name={vendorLabel(e.vendor_id, !!me?.invited)} size="sm" />
        {e.zone && (
          <TokenTag>
            {e.zone}
          </TokenTag>
        )}
        {!e.zone && <span className="text-label text-fg-tertiary">{t("extract-history.zone.all")}</span>}
      </span>
      <span className="w-24 shrink-0 text-center text-label font-medium tnum">
        {failed ? (
          <span className="text-fg-tertiary">
            0 / <span className="text-fg-secondary">{e.count_requested}</span>
          </span>
        ) : (
          <>
            <span className="font-semibold text-fg">{e.count_purchased}</span>
            {e.count_purchased !== e.count_requested && (
              <span className="text-fg-tertiary"> / {e.count_requested}</span>
            )}
            <span className="ml-0.5 text-fg-tertiary"> {t("unit.count")}</span>
          </>
        )}
      </span>
      <span className="w-24 shrink-0 text-center text-label">
        {failed ? (
          <span className="text-fg-tertiary">—</span>
        ) : e.pending_count > 0 ? (
          <span className="text-fg-secondary">
            {t("extract-history.progress.pending-prefix")}<span className="font-semibold tnum">{e.pending_count}</span>
          </span>
        ) : (
          <span className="text-ok-fg">{t("extract-history.progress.all-assigned")}</span>
        )}
      </span>
      <span
        className={cn(
          "w-24 shrink-0 text-right font-semibold tnum",
          e.total_cost < 0 ? "text-fg" : "text-fg-tertiary",
        )}
      >
        {e.total_cost === 0 ? "—" : fmtCredits(e.total_cost, { sign: true })}
        {e.total_cost !== 0 && <span className="ml-0.5 font-medium text-fg-tertiary">{t("unit.credits")}</span>}
      </span>
    </BareRow>
  );
}

/* ─────────────── Tab · 派发历史 ─────────────── */

/** 三种去向都是正常动作 · 不用 danger 红（红留给失败 / 危险）
 *  handoff 的"不可恢复"提示走展开区的说明，不靠颜色吓人 */
const DEST_META: Record<
  AssignEvent["destination"],
  { labelKey: string; icon: React.ComponentType<{ className?: string }>; tone: "brand" | "neutral" }
> = {
  into_bus:  { labelKey: "dest.into-bus",  icon: BusIcon,  tone: "brand" },
  push_pool: { labelKey: "dest.push-pool", icon: Send,     tone: "neutral" },
  handoff:   { labelKey: "dest.handoff",   icon: Download, tone: "neutral" },
};

function AssignHistoryTab() {
  const { t } = useTranslation("extract");
  const { data, isLoading } = useAssignEvents();
  const events = data?.items ?? [];

  return (
    <Card className="p-7">
      <div className="mb-4">
        <SectionHead
          title={t("assign-history.section.title")}
          sub={<>{t("assign-history.section.sub.prefix")}<Em>{events.length}</Em>{t("assign-history.section.sub.suffix")}</>}
        />
      </div>

      {isLoading && !data ? (
        <SkeletonTable rows={5} cols={["w-20", "w-14", "w-1/4", "w-16", "w-20", "w-20"]} />
      ) : events.length === 0 ? (
        <EmptyState
          icon={Send}
          title={t("assign-history.empty.title")}
          desc={t("assign-history.empty.desc")}
        />
      ) : (
        <div className="overflow-x-auto">
          <div className="min-w-[760px]">
            <BareHead>
              <span className="w-6 shrink-0" />
              <span className="w-[92px] shrink-0">{t("assign-history.col.time")}</span>
              <span className="w-24 shrink-0">{t("assign-history.col.dest")}</span>
              <span className="min-w-0 flex-1">{t("assign-history.col.target")}</span>
              <span className="w-16 shrink-0 text-center">{t("assign-history.col.count")}</span>
              <span className="min-w-0 flex-[0.9]">{t("assign-history.col.vendor")}</span>
            </BareHead>
            <BareList>
              {events.map((e) => <AssignEventRow key={e.id} e={e} />)}
            </BareList>
          </div>
        </div>
      )}
    </Card>
  );
}

/** 派发事件行 · 点开看每个号的明细（masked / 区 / 已耗额度 / 派发时寿命） */
function AssignEventRow({ e }: { e: AssignEvent }) {
  const { t } = useTranslation("extract");
  const { data: me } = useMe();
  const [open, setOpen] = useState(false);
  const meta = DEST_META[e.destination];
  const Icon = meta.icon;

  return (
    <div>
      <BareRow onClick={() => setOpen((v) => !v)}>
        {/* 展开箭头 */}
        <span className="w-6 shrink-0">
          <ChevronRight
            className={cn(
              "size-3.5 text-fg-tertiary transition-transform",
              open && "rotate-90",
            )}
          />
        </span>

        <span className="w-[92px] shrink-0 text-label font-medium tnum text-fg-tertiary">
          {fmtTime(e.created_at)}
        </span>

        {/* 去向 · icon + label · tone 区分 */}
        <span className="flex w-24 shrink-0 items-center gap-1.5">
          <Icon
            className={cn(
              "size-3.5 shrink-0",
              meta.tone === "brand" && "text-brand-strong",
              meta.tone === "neutral" && "text-fg-secondary",
            )}
          />
          <span className="text-label font-medium">{t(meta.labelKey)}</span>
        </span>

        {/* 目标 · 进车给车名 · 推池给 host · 拿走给"已下载" chip */}
        <span className="flex min-w-0 flex-1 items-center gap-2">
          {e.bus_name ? (
            <TokenTag size="sm">{e.bus_name}</TokenTag>
          ) : e.target_host ? (
            <TokenTag size="sm">
              <Send className="size-3" />
              <span className="ml-1">{e.target_host}</span>
            </TokenTag>
          ) : (
            /* 已下载 = 成功完成的动作 · 用 ok 绿不用 danger 红
               红色留给失败 / 危险操作 · 下载成功不是危险 */
            <Chip tone="ok" icon={<Check className="size-3" />}>{t("assign-history.chip.downloaded")}</Chip>
          )}
        </span>

        <span className="w-16 shrink-0 text-center text-label font-semibold tnum">
          {e.count}
          <span className="ml-0.5 font-medium text-fg-tertiary">{t("unit.count")}</span>
        </span>

        <span className="flex min-w-0 flex-[0.9] flex-wrap items-center gap-1">
          {e.vendors.map((v) => (
            <VendorTag key={v} name={vendorLabel(v, !!me?.invited)} />
          ))}
        </span>
      </BareRow>

      {/* 展开 · 每个号的明细 */}
      {open && (
        <div className="border-t border-hairline bg-bg-elevated/40 px-1 py-2">
          {/* 拿走的号我方已删明文 · 说明为什么没有"重新下载" */}
          {e.destination === "handoff" && (
            <div className="mx-1 mb-2 flex items-start gap-2 rounded-lg bg-warn-solid/10 px-2.5 py-2 text-label dark:bg-warn-solid/[.14]">
              <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-warn-fg" />
              <span className="text-fg-secondary">
                {t("assign-history.handoff-warn.prefix")}<strong className="text-fg">{t("assign-history.handoff-warn.strong")}</strong>{t("assign-history.handoff-warn.suffix")}
              </span>
            </div>
          )}
          <div className="flex items-center gap-4 px-1 pb-1.5 text-[10px] font-semibold text-fg-tertiary">
            <span className="min-w-0 flex-1">{t("assign-history.detail.key")}</span>
            <span className="w-24 shrink-0">{t("assign-history.detail.region")}</span>
            <span className="w-24 shrink-0 text-right">{t("assign-history.detail.used")}</span>
            <span className="w-24 shrink-0 text-right">{t("assign-history.detail.lifespan")}</span>
          </div>
          <div className="space-y-0.5">
            {e.keys.map((k) => (
              <div
                key={k.credential_id}
                className="flex items-center gap-4 px-1 py-1 text-label"
              >
                <span className="min-w-0 flex-1 truncate font-mono text-fg-secondary">
                  {k.key_masked}
                </span>
                <span className="w-24 shrink-0 tnum text-fg-tertiary">{k.region}</span>
                <span className="w-24 shrink-0 text-right font-semibold tnum">
                  {fmtCredits(k.credits_used)}
                  <span className="ml-0.5 font-medium text-fg-tertiary">{t("unit.credits")}</span>
                </span>
                <span className="w-24 shrink-0 text-right tnum text-fg-secondary">
                  {k.lifespan_seconds > 0 ? fmtLifespan(k.lifespan_seconds) : t("assign-history.detail.just-pulled")}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
