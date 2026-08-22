import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, ChevronDown, KeyRound, Plus, Ticket, UserPlus, Users, X } from "lucide-react";
import {
  useBusPulls, useBuses, useMe,
} from "@/api/hooks";
import { BusCard } from "@/components/BusCard";
import { BusFocalCard } from "@/components/BusFocalCard";
import { BusMiniCard } from "@/components/BusMiniCard";
import { StartCarpoolModal } from "@/components/StartCarpoolModal";
import { JoinAnonModal } from "@/components/JoinAnonModal";
import { JoinByInviteModal } from "@/components/JoinByInviteModal";
import { PullNowModal } from "@/components/PullNowModal";
import { EmptyState } from "@/components/ui/empty-state";
import { SkeletonCard } from "@/components/ui/skeleton";
import { BareHead, BareList, BareRow, Card, Chip, Em, SectionHead } from "@/components/ui/primitives";
import { Button } from "@/components/ui/button";
import { LoadMoreButton } from "@/components/ui/load-more-button";
import { Popover, PopoverContent, PopoverItem, PopoverTrigger } from "@/components/ui/popover";
import { MicroStat, TokenTag, VendorTag } from "@/components/ui/tags";
import {
  cn, fmtCredits, fmtTime, vendorLabel,
} from "@/lib/utils";
import type { Bus, PullResult, PullRound, PushState } from "@/types";

export default function Buses() {
  const { t } = useTranslation("buses");
  const { data: buses, isLoading: busesLoading } = useBuses();
  const items = buses?.items ?? [];
  // 首屏骨架 · 只在完全没数据时铺（刷新时保留旧列表不闪）
  const firstLoad = busesLoading && !buses;

  // 3 种入口（decisions §8.11）：single 建自己的车 · anon 搭车 · invite 输拼车码加入
  const [modalKind, setModalKind] = useState<"single" | "anon" | "invite" | null>(null);
  const [ctaOpen, setCtaOpen] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [pullBus, setPullBus] = useState<Bus | null>(null);

  // 默认选中第一辆车
  useEffect(() => {
    if (!selectedId && items.length > 0) setSelectedId(items[0].id);
  }, [items, selectedId]);

  // owner/member · 阶段 1a mock：前 2 辆 owner，后面 member
  const roleOf = (idx: number): "owner" | "member" =>
    idx < 2 ? "owner" : "member";

  const totalAlive = items.reduce((s, b) => s + b.alive_count, 0);
  const totalDead = items.reduce((s, b) => s + b.dead_count, 0);
  const totalSpendToday = items.reduce((s, b) => s + b.spend_today, 0);

  const closeModal = () => setModalKind(null);
  return (
    <div className="space-y-section">
      <StartCarpoolModal open={modalKind === "single"} onClose={closeModal} />
      <JoinAnonModal open={modalKind === "anon"} onClose={closeModal} />
      <JoinByInviteModal open={modalKind === "invite"} onClose={closeModal} />
      {pullBus && (
        <PullNowModal
          open={!!pullBus}
          onClose={() => setPullBus(null)}
          busId={pullBus.id}
          defaultCount={pullBus.strategy.per_round_count ?? 3}
          preferredVendor={pullBus.strategy.preferred_vendor}
          maxUnitPrice={pullBus.strategy.max_unit_price}
        />
      )}

      {/* Hero + 立即拼车 CTA */}
      <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div className="min-w-0 space-y-2">
          <h1 className="text-hero font-semibold">{t("hero.title")}</h1>
          <p className="text-fg-tertiary">
            <Em>{items.length}</Em> {t("hero.buses-active-suffix")}{" "}
            · <Em>{totalAlive}</Em> {t("hero.keys-in-pool-suffix")}{" "}
            · {t("hero.dead-prefix")} <Em>{totalDead}</Em>{" "}
            · {t("hero.spend-today-prefix")}{" "}
            <Em tone="spend">
              {totalSpendToday > 0 ? `-${fmtCredits(totalSpendToday)}` : "0"}
            </Em>{" "}
            {t("hero.credits-suffix")}
          </p>
        </div>

        <StartCarpoolCTA
          open={ctaOpen}
          onToggle={setCtaOpen}
          onPick={(kind) => {
            setCtaOpen(false);
            setModalKind(kind);
          }}
        />
      </div>

      {/* 首屏加载 · 铺 3 张卡骨架（跟真实列表同形状·加载完不跳位） */}
      {firstLoad ? (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
          <SkeletonCard lines={4} />
          <SkeletonCard lines={4} />
          <SkeletonCard lines={4} />
        </div>
      ) : items.length === 0 ? (
        <Card className="flex flex-col items-center gap-4 p-12 text-center">
          <span className="grid size-12 place-items-center rounded-2xl bg-brand-subtle">
            <Users className="size-6 text-brand-strong" />
          </span>
          <div className="space-y-1">
            <div className="text-body-lg font-semibold">{t("empty.title")}</div>
            <p className="text-fg-tertiary">{t("empty.desc")}</p>
          </div>
          <Button variant="brand" onClick={() => setModalKind("single")}>{t("empty.cta")}</Button>
        </Card>
      ) : (() => {
        /* 车列表 · 按车数 + 有没有多人车分档，纯声明式无 toggle
         *
         *   多人车 = member_count > 1（CLAUDE.md §2 · 1 人是状态不是类型 · 不看 kind）
         *
         *   ┌──────────┬──────────────┬─────────────────────────────────────┐
         *   │ 车数     │ 有多人车？   │ 布局                                │
         *   ├──────────┼──────────────┼─────────────────────────────────────┤
         *   │ 1 辆     │ 独享         │ 1 张 BusCard（1 列宽 · 靠左）        │
         *   │ 1 辆     │ 多人         │ 1 张 focal 大卡 · 全宽              │
         *   │ 2–3 辆   │ 任意         │ 左 mini 列表 + 右 focal（focal 挑   │
         *   │          │              │ 第一辆多人车，没多人就挑第一辆）    │
         *   │ > 3 辆   │ 全独享       │ 所有车 BusCard 3 列平铺             │
         *   │ > 3 辆   │ 有多人       │ 顶部 focal 独占一行（挑第一辆多人） │
         *   │          │              │ + 下面剩余车 3 列平铺（不再重复）   │
         *   └──────────┴──────────────┴─────────────────────────────────────┘ */
        const multiBus = items.find((b) => b.member_count > 1);

        // Case 1: 只有 1 辆
        if (items.length === 1) {
          const b = items[0];
          if (b.member_count > 1) {
            return (
              <BusFocalCard
                bus={b}
                role={roleOf(0)}
                onPullClick={() => setPullBus(b)}
              />
            );
          }
          // 独享 · 1 列宽 · 3 列 grid 里的第一个位置就是靠左
          return (
            <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
              <BusCard bus={b} role={roleOf(0)} />
            </div>
          );
        }

        // Case 2: 2–3 辆 · 有多人 → mini + focal · 全独享 → 直接平铺
        //   focal 大卡是给多人车设计的（车友头像组 / 邀请码 / 24h 柱图）
        //   全独享的时候硬套 focal 是为设计而设计，直接平铺更诚实
        if (items.length <= 3) {
          if (!multiBus) {
            return (
              <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
                {items.map((b, i) => (
                  <BusCard key={b.id} bus={b} role={roleOf(i)} />
                ))}
              </div>
            );
          }
          const miniList = items.filter((b) => b.id !== multiBus.id);
          return (
            <div className="grid grid-cols-1 items-stretch gap-6 lg:grid-cols-[340px_minmax(0,1fr)]">
              <div className="flex flex-col gap-3">
                {miniList.map((b) => {
                  const idx = items.findIndex((x) => x.id === b.id);
                  return (
                    <BusMiniCard
                      key={b.id} bus={b} role={roleOf(idx)}
                    />
                  );
                })}
              </div>
              <BusFocalCard
                bus={multiBus}
                role={roleOf(items.findIndex((b) => b.id === multiBus.id))}
                onPullClick={() => setPullBus(multiBus)}
              />
            </div>
          );
        }

        // > 3 辆 · 折叠 / 展开 toggle
        //   折叠：精选（有多人 → 顶部 focal + 剩下前几辆平铺；全独享 → 前 3 辆平铺）
        //   展开：所有车 BusCard 3 列平铺
        //   toggle 用 LoadMoreButton · label 显示总数 N
        const collapsedGrid = () => {
          if (!multiBus) {
            // > 3 辆 · 全独享 · 折叠 = 前 3 辆平铺
            return (
              <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
                {items.slice(0, 3).map((b, i) => (
                  <BusCard key={b.id} bus={b} role={roleOf(i)} />
                ))}
              </div>
            );
          }
          // > 3 辆 · 有多人 · 折叠 = 顶部 focal + 后面 3 辆 BusCard 平铺
          //   focal 那辆不重复出现在下方 grid 里
          const rest = items.filter((b) => b.id !== multiBus.id).slice(0, 3);
          return (
            <div className="space-y-6">
              <BusFocalCard
                bus={multiBus}
                role={roleOf(items.findIndex((b) => b.id === multiBus.id))}
                onPullClick={() => setPullBus(multiBus)}
              />
              <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
                {rest.map((b) => (
                  <BusCard key={b.id} bus={b} role={roleOf(items.findIndex((x) => x.id === b.id))} />
                ))}
              </div>
            </div>
          );
        };

        const expandedGrid = (
          <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
            {items.map((b, i) => (
              <BusCard key={b.id} bus={b} role={roleOf(i)} />
            ))}
          </div>
        );

        return (
          <div className="space-y-6">
            {expanded ? expandedGrid : collapsedGrid()}
            <LoadMoreButton
              expanded={expanded}
              onToggle={() => setExpanded((v) => !v)}
              labelExpand={<>{t("list.load-more-prefix")} <span className="tnum font-semibold">{items.length}</span> {t("list.load-more-suffix")}</>}
            />
          </div>
        );
      })()}

      {/* 底部拼车拉号记录 · decisions §8.13 */}
      <PoolingPullHistory buses={items.map((b) => b.id)} />
    </div>
  );
}
/* 立即拼车 ▾ · 3 选一（decisions §8.11 原设计 · 一字不改）
   - 发起拼车     = single
   - 搭车         = anon（1c 起可点）
   - 输拼车码加入  = team join（1c 起可点） */
function StartCarpoolCTA({
  open, onToggle, onPick,
}: {
  open: boolean;
  onToggle: (v: boolean) => void;
  onPick: (kind: "single" | "anon" | "invite") => void;
}) {
  const { t } = useTranslation("buses");
  return (
    <Popover open={open} onOpenChange={onToggle}>
      <PopoverTrigger asChild>
        <Button variant="brand" className={cn("shrink-0", open && "opacity-90")}>
          <Plus />
          {t("cta.trigger")}
          <ChevronDown className={cn("size-3.5 transition-transform", open && "rotate-180")} />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-72">
        <PopoverItem onSelect={() => onPick("single")} className="items-start gap-3 p-3">
          <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-brand-subtle">
            <Users className="size-4 text-brand-strong" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="font-semibold">{t("cta.single.title")}</div>
            <div className="text-label text-fg-tertiary">{t("cta.single.desc")}</div>
          </div>
        </PopoverItem>

        <PopoverItem onSelect={() => onPick("anon")} className="items-start gap-3 p-3">
          <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-brand-subtle">
            <UserPlus className="size-4 text-brand-strong" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="font-semibold">{t("cta.anon.title")}</div>
            <div className="text-label text-fg-tertiary">{t("cta.anon.desc")}</div>
          </div>
        </PopoverItem>

        <PopoverItem onSelect={() => onPick("invite")} className="items-start gap-3 p-3">
          <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-brand-subtle">
            <Ticket className="size-4 text-brand-strong" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="font-semibold">{t("cta.invite.title")}</div>
            <div className="text-label text-fg-tertiary">{t("cta.invite.desc")}</div>
          </div>
        </PopoverItem>
      </PopoverContent>
    </Popover>
  );
}
/* 底部拼车拉号记录 · decisions §8.13 · 只列拼车相关 */
const RESULT_TONE: Record<PullResult, "ok" | "warn" | "danger" | "brand"> = {
  success: "ok",
  partial: "warn",
  failed: "danger",
  refunded: "brand",
};

/** 推送态 · 行内次级状态 · 10px 小 pill —— 12px Chip 只留给行首主状态列（docs/13 §4） */
function PushCell({ state, ratio }: { state: PushState; ratio: string | null }) {
  const { t } = useTranslation("buses");
  if (state === "pushed") return <MicroStat tone="ok"><Check className="mr-1 size-2.5" />{t("push.pushed")}</MicroStat>;
  if (state === "partial") return <MicroStat tone="warn"><Check className="mr-1 size-2.5" />{t("push.partial", { ratio: ratio ?? "" })}</MicroStat>;
  if (state === "failed") return <MicroStat tone="danger"><X className="mr-1 size-2.5" />{t("push.failed")}</MicroStat>;
  return <MicroStat tone="neutral">{t("push.none")}</MicroStat>;
}

function PoolingPullHistory({ buses }: { buses: string[] }) {
  const { t } = useTranslation("buses");
  const q0 = useBusPulls(buses[0]);
  const q1 = useBusPulls(buses[1]);
  const q2 = useBusPulls(buses[2]);
  const allRounds = [
    ...(q0.data ?? []),
    ...(q1.data ?? []),
    ...(q2.data ?? []),
  ].sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());

  const [shown, setShown] = useState(10);
  const visible = allRounds.slice(0, shown);
  const remain = Math.max(0, allRounds.length - shown);

  return (
    <div className="space-y-5">
      <SectionHead
        title={t("history.title")}
        sub={t("history.subtitle", { count: allRounds.length })}
      />
      {allRounds.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title={t("history.empty.title")}
          desc={t("history.empty.desc")}
        />
      ) : (
        <>
          <div className="overflow-x-auto">
            {/* result w-24 / push w-28：按英文最长词（Refunded / Push failed）定宽 ·
                原 w-14 + Chip w-full 会让英文撑出格子（中文两字侥幸没炸） */}
            <div className="min-w-[880px]">
              <BareHead>
                <span className="w-[86px] shrink-0">{t("history.table.time")}</span>
                <span className="w-24 shrink-0">{t("history.table.result")}</span>
                <span className="min-w-0 flex-1">{t("history.table.flow")}</span>
                <span className="w-20 shrink-0 text-center">{t("history.table.key-status")}</span>
                <span className="w-28 shrink-0 text-center">{t("history.table.push")}</span>
                <span className="w-24 shrink-0 text-right">{t("history.table.cost")}</span>
              </BareHead>
              <BareList>
                {visible.map((r) => <PullHistRow key={r.id} r={r} />)}
              </BareList>
            </div>
          </div>
          <LoadMoreButton
            onLoadMore={() => setShown((s) => s + 10)}
            remain={remain}
            remainUnit={t("history.remain-unit")}
          />
        </>
      )}
    </div>
  );
}
function PullHistRow({ r }: { r: PullRound }) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  const tone = RESULT_TONE[r.result];
  const label = t(`result.${r.result}`);
  const failed = r.result === "failed";
  return (
    <BareRow>
      <span className="w-[86px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(r.created_at)}
      </span>
      <span className="w-24 shrink-0">
        <Chip tone={tone} dot>{label}</Chip>
      </span>

      <span className="flex min-w-0 flex-1 items-center gap-2 truncate">
        {failed ? (
          <span className="truncate text-fg-tertiary">
            {t("row.failed-prefix")} <span className="font-medium">{vendorLabel(r.vendor_id, me?.tier)}</span>{" "}
            · {r.fail_reason ?? t("row.fail-reason-default")}
          </span>
        ) : (
          <>
            <span className="shrink-0 text-fg-secondary">{t("row.pulled-prefix")}</span>
            <span className="shrink-0 font-semibold tnum text-fg">{r.count_purchased}</span>
            <span className="shrink-0 text-fg-secondary">{t("row.pulled-mid")}</span>
            <VendorTag name={vendorLabel(r.vendor_id, me?.tier)} />
            <span className="shrink-0 text-fg-tertiary">→</span>
            <TokenTag>{r.bus_name}</TokenTag>
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

      <span className="flex w-28 shrink-0 justify-center">
        <PushCell state={r.push_state} ratio={r.push_ratio} />
      </span>

      <span
        className={cn(
          "w-24 shrink-0 text-right font-semibold tnum",
          r.total_cost > 0 ? "text-ok-fg" : r.total_cost === 0 ? "text-fg-tertiary" : "text-fg",
        )}
      >
        {r.total_cost === 0 ? "0" : fmtCredits(r.total_cost, { sign: true })}{" "}
        <span className="font-medium text-fg-tertiary">{t("history.credits-suffix")}</span>
      </span>
    </BareRow>
  );
}





