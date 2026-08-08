import { useEffect, useState } from "react";
import { ChevronDown, ChevronUp, Plus, Ticket, UserPlus, Users } from "lucide-react";
import { useBuses, useBusPulls } from "@/api/hooks";
import { BusCard } from "@/components/BusCard";
import { BusFocalCard } from "@/components/BusFocalCard";
import { BusMiniCard } from "@/components/BusMiniCard";
import { StartCarpoolModal } from "@/components/StartCarpoolModal";
import { PullNowModal } from "@/components/PullNowModal";
import { BareHead, BareList, BareRow, Card, Chip, SectionHead } from "@/components/ui/primitives";
import { cn, fmtCredits, fmtTime, vendorName } from "@/lib/utils";
import type { Bus, PullResult, PullRound, PushState } from "@/types";

export default function Buses() {
  const { data: buses } = useBuses();
  const items = buses?.items ?? [];

  const [modalKind, setModalKind] = useState<"single" | null>(null);
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

  return (
    <div className="space-y-section">
      <StartCarpoolModal open={modalKind !== null} onClose={() => setModalKind(null)} />
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
          <h1 className="text-hero font-semibold">拼车</h1>
          <p className="text-fg-tertiary">
            <span className="font-semibold tnum text-fg">{items.length}</span> 辆车正在运转{" "}
            · <span className="font-semibold tnum text-fg">{totalAlive}</span> 个号在池{" "}
            · 失效 <span className="font-semibold tnum text-fg-secondary">{totalDead}</span>{" "}
            · 今日消费{" "}
            <span className="font-semibold tnum text-danger-fg">
              {totalSpendToday > 0 ? `-${fmtCredits(totalSpendToday)}` : "0"}
            </span>{" "}
            积分
          </p>
        </div>

        <StartCarpoolCTA
          open={ctaOpen}
          onToggle={() => setCtaOpen((v) => !v)}
          onClose={() => setCtaOpen(false)}
          onPickSingle={() => {
            setCtaOpen(false);
            setModalKind("single");
          }}
        />
      </div>

      {/* 空态 */}
      {items.length === 0 ? (
        <Card className="flex flex-col items-center gap-4 p-12 text-center">
          <span className="grid size-12 place-items-center rounded-2xl bg-brand-subtle">
            <Users className="size-6 text-brand-strong" />
          </span>
          <div className="space-y-1">
            <div className="text-body-lg font-semibold">还没有拼车</div>
            <p className="text-fg-tertiary">建一辆自己的车 · 或加入他人的拼车</p>
          </div>
          <button
            onClick={() => setModalKind("single")}
            className="rounded-lg bg-brand px-4 py-2 font-semibold text-white transition-opacity hover:opacity-90"
          >
            立即拼车
          </button>
        </Card>
      ) : (
        /* 车列表整块 · 内部间距 24px 跟卡片左右 gap-6 统一 · 按钮 pt-6 保持独立呼吸 */
        <div className="space-y-6">
          {/* 主视图 · 严格照 mock：左 2 mini + 右 1 focal · 焦点优先多人车 → 选中车 → 第一辆 */}
          {(() => {
            const multiCandidate = items.find((b) => b.kind !== "single");
            const focal = multiCandidate ?? items.find((b) => b.id === selectedId) ?? items[0];
            const miniList = items.filter((b) => b.id !== focal.id).slice(0, 2);
            return (
              <div className="grid grid-cols-1 items-stretch gap-6 lg:grid-cols-[340px_1fr]">
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
                  bus={focal}
                  role={roleOf(items.findIndex((b) => b.id === focal.id))}
                  onPullClick={() => setPullBus(focal)}
                />
              </div>
            );
          })()}

          {/* 展开后 · 所有车 BusCard grid · 跟上面主视图紧贴（外层 space-y-4） */}
          {expanded && (
            <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
              {items.map((b, i) => (
                <BusCard key={b.id} bus={b} role={roleOf(i)} />
              ))}
            </div>
          )}

          {/* 按钮独立带 pt-6 顶部空间 · 展开态在列表下 · 收起态在主视图下 */}
          <div className="flex justify-center pt-6">
            <button
              onClick={() => setExpanded((v) => !v)}
              className="flex items-center gap-2 rounded-lg border border-hairline bg-bg px-4 py-2 font-medium text-fg-secondary shadow-card transition-colors hover:bg-bg-elevated"
            >
              {expanded ? (
                <><ChevronUp className="size-4" />收起</>
              ) : (
                <><ChevronDown className="size-4" />查看全部 · <span className="tnum font-semibold">{items.length}</span> 辆车</>
              )}
            </button>
          </div>
        </div>
      )}

      {/* 底部拼车拉号记录 · decisions §8.13 */}
      <PoolingPullHistory buses={items.map((b) => b.id)} />
    </div>
  );
}
/* 立即拼车 ▾ · 3 选一 · 阶段 1a 只有 single 可点 */
function StartCarpoolCTA({
  open, onToggle, onClose, onPickSingle,
}: {
  open: boolean;
  onToggle: () => void;
  onClose: () => void;
  onPickSingle: () => void;
}) {
  return (
    <div className="relative shrink-0">
      <button
        onClick={onToggle}
        className={cn(
          "flex items-center gap-2 rounded-lg bg-brand px-4 py-2 font-semibold text-white shadow-card transition-opacity hover:opacity-90",
          open && "opacity-90",
        )}
      >
        <Plus className="size-4" />
        立即拼车
        <ChevronDown className={cn("size-3.5 transition-transform", open && "rotate-180")} />
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={onClose} />
          <div className="absolute right-0 z-50 mt-2 w-72 rounded-[14px] border border-hairline bg-bg p-2 shadow-pop">
            <button
              onClick={onPickSingle}
              className="flex w-full items-start gap-3 rounded-lg p-3 text-left transition-colors hover:bg-bg-elevated"
            >
              <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-brand-subtle">
                <Users className="size-4 text-brand-strong" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="font-semibold">发起拼车</div>
                <div className="text-label text-fg-tertiary">建一辆自己的车 · 独享号池</div>
              </div>
            </button>

            <div className="flex w-full items-start gap-3 rounded-lg p-3 opacity-45">
              <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-bg-elevated">
                <UserPlus className="size-4 text-fg-tertiary" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 font-semibold">
                  搭车
                  <Chip tone="neutral" className="text-[10px]">阶段 2b</Chip>
                </div>
                <div className="text-label text-fg-tertiary">系统撮合他人拼车 · 共享号池摊单价</div>
              </div>
            </div>

            <div className="flex w-full items-start gap-3 rounded-lg p-3 opacity-45">
              <span className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-lg bg-bg-elevated">
                <Ticket className="size-4 text-fg-tertiary" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 font-semibold">
                  输邀请码加入
                  <Chip tone="neutral" className="text-[10px]">阶段 2a</Chip>
                </div>
                <div className="text-label text-fg-tertiary">用朋友给的邀请码加入他的车</div>
              </div>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
/* 底部拼车拉号记录 · decisions §8.13 · 只列拼车相关 */
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

function PoolingPullHistory({ buses }: { buses: string[] }) {
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
        title="拼车拉号记录"
        sub={`共 ${allRounds.length} 轮 · 只列拼车相关（跟概览混流分开）`}
      />
      <div className="overflow-x-auto">
        <div className="min-w-[820px]">
          <BareHead>
            <span className="w-[86px] shrink-0">时间</span>
            <span className="w-14 shrink-0">结果</span>
            <span className="min-w-0 flex-1">流向</span>
            <span className="w-20 shrink-0 text-center">号状态</span>
            <span className="w-24 shrink-0 text-center">推池</span>
            <span className="w-24 shrink-0 text-right">花费</span>
          </BareHead>
          <BareList>
            {visible.map((r) => <PullHistRow key={r.id} r={r} />)}
          </BareList>
        </div>
      </div>
      {remain > 0 && (
        <div className="flex justify-center pt-1">
          <button
            onClick={() => setShown((s) => s + 10)}
            className="rounded-lg border border-hairline bg-bg px-4 py-1.5 font-medium text-fg-secondary shadow-card transition-colors hover:bg-bg-elevated"
          >
            加载更多 <span className="text-fg-tertiary">· 还剩 {remain} 轮</span>
          </button>
        </div>
      )}
    </div>
  );
}
function PullHistRow({ r }: { r: PullRound }) {
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
            未拉到号 · 尝试 <span className="font-medium">{vendorName(r.vendor_id)}</span>{" "}
            · {r.fail_reason ?? "缺货"}
          </span>
        ) : (
          <>
            <span className="shrink-0 font-semibold tnum text-fg">+{r.count_purchased}</span>
            <span className="shrink-0 text-fg-secondary">个号，从</span>
            <span className="shrink-0 whitespace-nowrap rounded-md border border-hairline bg-bg-elevated px-2 py-[2px] text-label font-medium text-fg-secondary shadow-card">
              {vendorName(r.vendor_id)}
            </span>
            <span className="shrink-0 text-fg-tertiary">→</span>
            <span className="shrink-0 whitespace-nowrap rounded-md border border-hairline bg-bg-elevated px-2 py-[2px] text-label font-medium text-fg-secondary shadow-card">
              {r.bus_name}
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





