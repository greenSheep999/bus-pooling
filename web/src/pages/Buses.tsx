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
import { TokenTag, VendorTag } from "@/components/ui/tags";
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
      ) : (
        /* 车列表整块 · focal 显示条件（方案 A · 决定见对话）：
           - 有多人车（team/anon）→ 启用双列 focal + mini（多人车做 focal · 单人车做 mini）
           - 全是 single → 3 张 BusCard 平铺（前 3 辆）· 剩下的走"查看全部"展开
           理由：focal 大卡（车友头像组 · 邀请车友 · 24h 柱图）本身是给多人车设计的
                 阶段 1a 全 single 时不 focal，避免为设计而设计 */
        <div className="space-y-6">
          {(() => {
            const multiBus = items.find((b) => b.kind !== "single");

            if (multiBus) {
              // 阶段 2+ 有多人车：左 2 mini + 右 focal
              const miniList = items.filter((b) => b.id !== multiBus.id).slice(0, 2);
              return (
                <div className="grid grid-cols-1 items-stretch gap-6 lg:grid-cols-[340px_minmax(0,1fr)]">
                  <div className="flex flex-col gap-3">
                    {miniList.map((b) => {
                      const idx = items.findIndex((x) => x.id === b.id);
                      return (
                        <BusMiniCard
                          key={b.id} bus={b} role={roleOf(idx)}
                          active={false}
                          onClick={() => setSelectedId(b.id)}
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

            // 阶段 1a · 全 single · 3 张 BusCard 平铺（前 3 辆）
            return (
              <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
                {items.slice(0, 3).map((b, i) => (
                  <BusCard key={b.id} bus={b} role={roleOf(i)} />
                ))}
              </div>
            );
          })()}

          {/* 展开 · 所有车 BusCard grid · 永远可展开 · 跟主视图紧贴 */}
          {expanded && (
            <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
              {items.map((b, i) => (
                <BusCard key={b.id} bus={b} role={roleOf(i)} />
              ))}
            </div>
          )}

          {/* 按钮 · 永远显示 · pt-6 独立呼吸 */}
          <LoadMoreButton
            expanded={expanded}
            onToggle={() => setExpanded((v) => !v)}
            labelExpand={<>{t("list.load-more-prefix")} <span className="tnum font-semibold">{items.length}</span> {t("list.load-more-suffix")}</>}
          />
        </div>
      )}

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

function PushCell({ state, ratio }: { state: PushState; ratio: string | null }) {
  const { t } = useTranslation("buses");
  if (state === "pushed") return <Chip tone="ok" icon={<Check className="size-3" />}>{t("push.pushed")}</Chip>;
  if (state === "partial") return <Chip tone="warn" icon={<Check className="size-3" />}>{t("push.partial", { ratio: ratio ?? "" })}</Chip>;
  if (state === "failed") return <Chip tone="danger" icon={<X className="size-3" />}>{t("push.failed")}</Chip>;
  return <Chip tone="neutral">{t("push.none")}</Chip>;
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
            <div className="min-w-[820px]">
              <BareHead>
                <span className="w-[86px] shrink-0">{t("history.table.time")}</span>
                <span className="w-14 shrink-0">{t("history.table.result")}</span>
                <span className="min-w-0 flex-1">{t("history.table.flow")}</span>
                <span className="w-20 shrink-0 text-center">{t("history.table.key-status")}</span>
                <span className="w-24 shrink-0 text-center">{t("history.table.push")}</span>
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
      <span className="w-14 shrink-0">
        <Chip tone={tone} dot className="w-full justify-center">{label}</Chip>
      </span>

      <span className="flex min-w-0 flex-1 items-center gap-2 truncate">
        {failed ? (
          <span className="truncate text-fg-tertiary">
            {t("row.failed-prefix")} <span className="font-medium">{vendorLabel(r.vendor_id, !!me?.invited)}</span>{" "}
            · {r.fail_reason ?? t("row.fail-reason-default")}
          </span>
        ) : (
          <>
            <span className="shrink-0 text-fg-secondary">{t("row.pulled-prefix")}</span>
            <span className="shrink-0 font-semibold tnum text-fg">{r.count_purchased}</span>
            <span className="shrink-0 text-fg-secondary">{t("row.pulled-mid")}</span>
            <VendorTag name={vendorLabel(r.vendor_id, !!me?.invited)} size="sm" />
            <span className="shrink-0 text-fg-tertiary">→</span>
            <TokenTag size="sm">{r.bus_name}</TokenTag>
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
        <span className="font-medium text-fg-tertiary">{t("history.credits-suffix")}</span>
      </span>
    </BareRow>
  );
}





