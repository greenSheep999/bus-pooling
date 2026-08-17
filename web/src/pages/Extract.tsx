import { useEffect, useMemo, useState } from "react";
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
  useAssignEvents, useDownstream, useExtractEvents, useMe, usePullRecords, useVendorOffers,
} from "@/api/hooks";
import { AssignModal } from "@/components/AssignModal";
import { PullExtractForm } from "@/components/PullExtractForm";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead,
} from "@/components/ui/primitives";
import { UsageMeter } from "@/components/UsageMeter";
import { AccountKindTag, KeyRankBadge } from "@/components/RankBadge";
import { liveLifespanSeconds, useNowTick } from "@/lib/useNowTick";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { BulkActionBar } from "@/components/ui/bulk-action-bar";
import { Checkbox } from "@/components/ui/checkbox";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { MicroStat, TokenTag, VendorTag } from "@/components/ui/tags";
import {
  cn, fmtCredits, fmtLifespan, fmtTime, vendorLabel,
} from "@/lib/utils";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton, SkeletonTable } from "@/components/ui/skeleton";
import type { AssignEvent, Credential, ExtractEvent, PullResult } from "@/types";
/* 档位 tab 图标 · 企业 = 机器人幽灵 · 个人 = 猫幽灵 */
import ghostRobotPng from "@/assets/marketing/ghost-robot.png";
import ghostCatPng from "@/assets/marketing/ghost-cat.png";

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
  const { data: offers, isLoading: offersLoading } = useVendorOffers();
  const items = records?.items ?? [];
  /** 首次加载:offer matrix 未到手时铺骨架 · tab 不渲染
   *  刷新时保留旧 tab · 别闪成骨架 · 见 skeleton.tsx 用法原则 §1 */
  const firstLoad = offersLoading && !offers;

  // 文件夹 tab · category 选择
  const [category, setCategory] = useState<"enterprise" | "personal">("enterprise");
  /** 用户是否手动切过 tab · 手动切过就不再自动纠正 · 尊重用户选择 */
  const [manualPicked, setManualPicked] = useState(false);

  /** tab 上显示每档存量 · 从 Offer matrix 直接聚合（docs/24 §3 · Step 4）
   *
   *  好处:supported/available 分离 · 前端能区分"不提供"vs"暂时缺货" ·
   *  数字跟 vendor 下拉、subscription 下拉从**同一份数据**算 · 不会漂移。 */
  const enterpriseCount = useMemo(
    () => (offers?.vendors ?? []).reduce((n, v) => n + (v.categories.enterprise?.available ?? 0), 0),
    [offers],
  );
  const personalCount = useMemo(
    () => (offers?.vendors ?? []).reduce((n, v) => n + (v.categories.personal?.available ?? 0), 0),
    [offers],
  );
  /** 是否至少一家 vendor 支持该 category（"该 vendor 不提供" vs "暂时缺货"分离） */
  const enterpriseSupported = (offers?.vendors ?? []).some((v) => v.categories.enterprise?.supported);
  const personalSupported = (offers?.vendors ?? []).some((v) => v.categories.personal?.supported);

  /** offer matrix 到手后自动挑"有货"的 tab
   *    - 企业有货 → 企业(用户默认想要 Power)
   *    - 企业缺货 · 个人有货 → 个人
   *    - 两个都缺 → 停在企业
   *  只在首次到手 + 用户没手动切过时生效 */
  useEffect(() => {
    if (manualPicked || !offers) return;
    if (enterpriseCount === 0 && personalCount > 0) {
      setCategory("personal");
    } else if (enterpriseCount > 0) {
      setCategory("enterprise");
    }
  }, [offers, enterpriseCount, personalCount, manualPicked]);

  /** 用户点 tab · 记 manualPicked · 后续 stock 变化不再自动切 */
  const pickCategory = (c: "enterprise" | "personal") => {
    setManualPicked(true);
    setCategory(c);
  };
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

      {/* 提号区 · 首次加载(stock 未到手)铺骨架 · 到手后渲染真 tab + card
          §8.45 · 之前 tab 会带着 0 库存先渲染再跳数字 · 骨架化后一步到位 */}
      {firstLoad ? (
        <ExtractPullSkeleton />
      ) : (
        <div className="relative">
          {/* 文件夹 tab 效果(§8.45 · 2026-08-16) · 用户切"企业版 / 个人版"
              结构:tabs 贴左对齐(无 pl)· 与下方 card 的 border 视觉连成一体
              斜边用 SVG 画(clip-path 会砍掉 border · 用 SVG 才能带边框) */}
          <div className="flex items-end gap-1">
            <FolderTab
              active={category === "enterprise"}
              onClick={() => pickCategory("enterprise")}
              label={t("category.enterprise")}
              sub={t("category.enterprise-sub")}
              count={enterpriseCount}
              outOfStockLabel={enterpriseSupported ? t("category.out-of-stock") : t("category.not-open")}
              icon={ghostRobotPng}
              disabled={!enterpriseSupported}
            />
            <FolderTab
              active={category === "personal"}
              onClick={() => pickCategory("personal")}
              label={t("category.personal")}
              sub={t("category.personal-sub")}
              count={personalCount}
              outOfStockLabel={personalSupported ? t("category.out-of-stock") : t("category.not-open")}
              icon={ghostCatPng}
              disabled={!personalSupported}
            />
          </div>

          {/* 主 card · 右上角圆角撤(0)· 让 tab 视觉上跟 card 连成一体
              border 补齐:Card 组件默认已含 border · 用 rounded-tl-none 使 active tab 融合 */}
          <Card focal focalTone="brand" className="relative p-7 rounded-tl-none">
            <BrandGhost
              aria-hidden
              className="pointer-events-none absolute right-6 top-4 z-0 h-52 w-40 opacity-90"
            />
            <div className="relative z-10">
              <div className="mb-5 flex items-center gap-2.5">
                <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-brand-subtle">
                  <KeyRound className="size-4 text-brand-strong" />
                </span>
                <div className="min-w-0 space-y-1">
                  <h2 className="text-section font-semibold">{t("form.card.title")}</h2>
                  <p className="text-label text-fg-tertiary">
                    {category === "enterprise" ? t("form.card.sub-enterprise") : t("form.card.sub-personal")}
                  </p>
                </div>
              </div>
              <PullExtractForm category={category} />
            </div>
          </Card>
        </div>
      )}

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

  /* 用量合计 · 活号看实时采样(usage_current) · 死号 credits_used 才是终值
     只读 credits_used 会让活号全算 0（那字段只在号死那一刻写一次） */
  const totalCredits = items.reduce((s, c) => s + (c.usage_current || c.credits_used), 0);
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
          {/* 加了评价列 · min-w 跟上(否则窄屏列会挤) */}
          <div className="min-w-[820px]">
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
              {/* 评价档单独一列 · 不跟寿命叠在一格里 */}
              <span className="w-24 shrink-0 text-center">{t("pending.col.rank")}</span>
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
  /* 寿命本地 tick · 活号数字在两次 refetch 之间也在走（死号是定值不动） */
  const now = useNowTick();
  const liveSecs = liveLifespanSeconds(c, now);

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
        <VendorTag name={vendorLabel(c.vendor_id, me?.tier)} />
        {/* 企业 / 个人 —— 一律显示（只有一种档时用户也得知道这是哪种号） */}
        <AccountKindTag kind={c.account_kind} />
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
      {/* 寿命 · live 秒数本地 tick · 两次 refetch 之间数字也在走（useNowTick） */}
      <span className="w-16 shrink-0 text-center text-label font-medium tnum text-fg-secondary">
        {fmtLifespan(liveSecs)}
      </span>
      {/* 评价档 · 独立一列 · 活号按"当前已存活"给档 · 会随时间升级（lib/rank.ts） */}
      <span className="flex w-24 shrink-0 items-center justify-center">
        <KeyRankBadge lifespanSeconds={liveSecs} />
      </span>
      {/* 用量 · 活号读实时采样 · 死号才用 credits_used 终值（同 BusDetail 口径）·
          数字下方带进度条 · max 走 usage_limit 真值 · 老数据按 subscription 兜底 */}
      <span className="flex w-20 shrink-0 flex-col items-center gap-1">
        <span className="text-label font-semibold tnum">
          {fmtCredits(c.usage_current || c.credits_used)}
          <span className="ml-1 font-medium text-fg-tertiary">{t("unit.credits")}</span>
        </span>
        <UsageMeter c={c} className="w-full" />
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
        <VendorTag name={vendorLabel(e.vendor_id, me?.tier)} />
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
          e.total_cost > 0 ? "text-fg" : "text-fg-tertiary",
        )}
      >
        {e.total_cost === 0 ? "—" : fmtCredits(e.total_cost)}
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
            <TokenTag>{e.bus_name}</TokenTag>
          ) : e.target_host ? (
            <TokenTag>
              <Send className="size-2.5" />
              <span className="ml-1">{e.target_host}</span>
            </TokenTag>
          ) : (
            /* 已下载 = 成功完成的动作 · 用 ok 绿不用 danger 红
               红色留给失败 / 危险操作 · 下载成功不是危险 ·
               尺寸跟同列的车名/host tag 一致（10px · 行内不混两种大小） */
            <MicroStat tone="ok"><Check className="mr-1 size-2.5" />{t("assign-history.chip.downloaded")}</MicroStat>
          )}
        </span>

        <span className="w-16 shrink-0 text-center text-label font-semibold tnum">
          {e.count}
          <span className="ml-0.5 font-medium text-fg-tertiary">{t("unit.count")}</span>
        </span>

        <span className="flex min-w-0 flex-[0.9] flex-wrap items-center gap-1">
          {e.vendors.map((v) => (
            <VendorTag key={v} name={vendorLabel(v, me?.tier)} />
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
                  {k.key_masked || "—"}
                </span>
                <span className="w-24 shrink-0 tnum text-fg-tertiary">{k.region}</span>
                {/* 用量 + 进度条 · 跟待派列表 / 车内号列表同一个组件(口径不漂) */}
                <span className="w-24 shrink-0 text-right">
                  <span className="font-semibold tnum">{fmtCredits(k.usage_current || k.credits_used)}</span>
                  <span className="ml-0.5 font-medium text-fg-tertiary">{t("unit.credits")}</span>
                  <UsageMeter c={k} className="mt-0.5 w-full" />
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

/** ExtractPullSkeleton · 提号区骨架 · 首次加载 stock 时用
 *  两个 tab 用同一个 SkeletonFolderTab 保形状/高度一致(只在 active 深浅上区分) */
function ExtractPullSkeleton() {
  return (
    <div className="relative">
      <div className="flex items-end gap-1">
        <SkeletonFolderTab active />
        <SkeletonFolderTab />
      </div>
      {/* Card 骨架 · 高度贴近真实(标题行 + 4 个字段 + 状态行 + 提交行) */}
      <div className="rounded-2xl rounded-tl-none border border-hairline bg-bg p-7">
        <div className="mb-5 flex items-center gap-2.5">
          <Skeleton className="size-9 rounded-xl" />
          <div className="space-y-2">
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-3 w-56" />
          </div>
        </div>
        <div className="grid grid-cols-1 gap-4 md:grid-cols-4 mb-5">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="space-y-2">
              <Skeleton className="h-3 w-12" />
              <Skeleton className="h-10 w-full rounded-xl" />
            </div>
          ))}
        </div>
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 mb-5">
          <Skeleton className="h-24 rounded-xl" />
          <Skeleton className="h-24 rounded-xl" />
        </div>
        <div className="flex items-end justify-between gap-3">
          <div className="space-y-2">
            <Skeleton className="h-3 w-32" />
            <Skeleton className="h-3 w-64" />
          </div>
          <Skeleton className="h-11 w-40 rounded-lg" />
        </div>
      </div>
    </div>
  );
}

/** SkeletonFolderTab · 骨架版的文件夹 tab · 用同一个 SVG path 保形状一致
 *  active 深一档 · 非 active 浅一档 · 高度/圆角/斜边完全对齐真 FolderTab */
function SkeletonFolderTab({ active = false }: { active?: boolean }) {
  return (
    <div className="relative -mb-px min-w-[220px] h-[60px]">
      <svg
        viewBox="0 0 220 60"
        preserveAspectRatio="none"
        aria-hidden
        className="absolute inset-0 h-full w-full overflow-visible"
      >
        <path
          d="M 0.5 12 Q 0.5 0.5 12 0.5 L 200 0.5 Q 208 0.5 211.5 8 L 219.5 59.5 L 0.5 59.5 Z"
          /* 骨架**不用品牌紫** —— 数据还没到 · 不该先把"选中"的强调色亮出来
             （骨架只占位形状 · 一律中性灰 · 见 skeleton.tsx 用法原则）
             active 只用底色深浅区分 · 保持两个 tab 的层次感 */
          fill={active ? "hsl(var(--bg))" : "hsl(var(--bg-elevated))"}
          stroke="hsl(var(--hairline))"
          strokeWidth={1}
          vectorEffect="non-scaling-stroke"
          strokeLinejoin="round"
        />
        {active && (
          <line
            x1="0.5" y1="59.5" x2="210" y2="59.5"
            stroke="hsl(var(--bg))"
            strokeWidth={2}
            vectorEffect="non-scaling-stroke"
          />
        )}
      </svg>
      <div className="relative flex h-full items-center gap-3 pl-5 pr-10">
        {/* 图标占位 · 跟真 tab 的 size-8 对齐 · 否则加载完文字会横向跳一下 */}
        <Skeleton className="size-8 shrink-0 rounded-lg" />
        <div className="flex flex-col gap-1.5">
          <Skeleton className="h-4 w-16" />
          <Skeleton className="h-2.5 w-20" />
        </div>
        <Skeleton className="ml-auto h-5 w-10 rounded-full" />
      </div>
    </div>
  );
}

/** FolderTab · 文件夹标签样式
 *
 *  斜边用 **SVG 描边**(不是 clip-path)· clip-path 会把 border 也一起砍掉 ·
 *  只有 SVG path 能沿着斜边画出线来 · 这是唯一带边框的做法。
 *
 *  结构:
 *    ┌──────────────────╲   ← SVG path 描出这条闭合线(左上圆角 + 右斜边)
 *    │ Enterprise    5  │╲     文字与数字左右分布(gap-auto)
 *    │ Power 10000      │ ╲    数字在整个文字块右侧
 *    └──────────────────┴──   下方 card 顶 border 与之相连
 *
 *  active tab:brand-subtle 填充 · brand 描边 · 无下边框(与 card 融合)
 *  非 active:灰底 · 灰描边 · 有下边框(视觉上"没打开")
 *  count>0 显示数字 · count=0 显示"缺货" */
function FolderTab({
  active, onClick, label, sub, count, outOfStockLabel, icon, disabled = false,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  sub: string;
  count: number;
  outOfStockLabel: string;
  /** 档位小图标 · 放在文字左边（企业 = 机器人幽灵 · 个人 = 猫幽灵） */
  icon?: string;
  /** 该档还不能选（缺货 / 未开放）· §4.1 要求 tab 仍显示 · 只是点不动 */
  disabled?: boolean;
}) {
  const inStock = count > 0;
  return (
    <button
      type="button"
      onClick={disabled ? undefined : onClick}
      disabled={disabled}
      className={
        "group relative -mb-px min-w-[220px] focus:outline-none focus-visible:ring-2 focus-visible:ring-brand " +
        "focus-visible:rounded-tl-xl transition-colors " +
        (disabled ? "cursor-not-allowed opacity-60" : "")
      }
    >
      {/* SVG 描边 · 铺满按钮 · path 沿"左上圆角 → 右上大圆角 → 斜边 → 左下"闭合 ·
          preserveAspectRatio=none 让 path 跟随实际尺寸拉伸 · stroke 恒 1(vectorEffect) ·
          overflow-visible 关键 —— path 底部会伸出 viewBox 让 stroke 完整可见
          颜色用 hsl(var(--x)) 因为 tailwind 里 --brand-subtle 存的是 raw HSL 三元组 */}
      <svg
        viewBox="0 0 220 60"
        preserveAspectRatio="none"
        aria-hidden
        className="absolute inset-0 h-full w-full overflow-visible"
      >
        <path
          /* 左上圆角(半径 12) + 上边 + 右上圆(半径 8) + 斜边 + 底边 · 闭合
             坐标微调 · 让 stroke 都在 viewBox 内(左 0.5 · 上 0.5 · 右 -0.5)
             底边故意画在 60 · 与 card 顶 border 相接 · active 时会用一条覆盖线遮住 */
          d="M 0.5 12 Q 0.5 0.5 12 0.5 L 200 0.5 Q 208 0.5 211.5 8 L 219.5 59.5 L 0.5 59.5 Z"
          fill={active ? "hsl(var(--brand-subtle))" : "hsl(var(--bg-elevated))"}
          /* 选中 tab 的描边跟下方 focal card 的 border 同色(index.css .card-focal ·
             brand 紫 12% 透明)· 否则紫底配灰边 · tab 跟 card 看着不是一体 */
          stroke={active ? "rgb(145 71 255 / 0.12)" : "hsl(var(--hairline))"}
          strokeWidth={1}
          vectorEffect="non-scaling-stroke"
          strokeLinejoin="round"
        />
        {/* active tab 底边遮住 · 与下方 card 融为一体(画一条与背景同色的横线覆盖 path 底边)
            覆盖到 x2=210 —— 只到斜边起点(不覆盖斜边下沿) */}
        {active && (
          <line
            x1="0.5" y1="59.5" x2="210" y2="59.5"
            stroke="hsl(var(--brand-subtle))"
            strokeWidth={2}
            vectorEffect="non-scaling-stroke"
          />
        )}
      </svg>

      {/* 内容层 · relative 让它盖在 SVG 上 · 左内边距避圆角 · 右内边距避斜边
          企业版 / 个人版文字用 text-body(比 text-label 大一档 · 视觉更突出) */}
      <div className="relative flex items-center gap-3 pl-5 pr-10 py-3.5">
        {/* 档位图标 · 装饰性(alt="") · 未选中时压暗 · 选中变实 · 跟文字色一起变化 */}
        {icon && (
          <img
            src={icon}
            alt=""
            aria-hidden
            className={
              "size-8 shrink-0 select-none object-contain transition-opacity " +
              (active ? "opacity-100" : "opacity-60 group-hover:opacity-80")
            }
          />
        )}
        <div className="flex min-w-0 flex-col items-start leading-tight">
          <span className={"text-body font-semibold " + (active ? "text-brand-strong" : "text-fg-secondary")}>
            {label}
          </span>
          <span className="text-[11px] text-fg-tertiary mt-0.5">{sub}</span>
        </div>
        {/* 库存标记 · 挤到最右 · 有货绿数字 · 缺货灰"缺货" · ml-auto 让它右对齐 */}
        <span
          className={
            "ml-auto inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-semibold tnum " +
            (inStock
              ? active
                ? "bg-ok-solid/15 text-ok-fg"
                : "bg-ok-solid/10 text-ok-fg/80"
              : "bg-bg-alt text-fg-tertiary")
          }
        >
          {inStock ? count.toLocaleString("zh-CN") : outOfStockLabel}
        </span>
      </div>
    </button>
  );
}
