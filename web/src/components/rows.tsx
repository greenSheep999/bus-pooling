import { ArrowRight, Check, X } from "lucide-react";
import { BareRow, Chip } from "./ui/primitives";
import { cn, fmtCredits, fmtLifespan, fmtTime, vendorName } from "@/lib/utils";
import type { Activity, PullResult, PullRound, PushState } from "@/types";

/* ── 拉号轮次行 · 6 列 ── */

const RESULT: Record<PullResult, { label: string; tone: "ok" | "warn" | "danger" | "brand" }> = {
  success: { label: "成功", tone: "ok" },
  partial: { label: "部分", tone: "warn" },
  failed: { label: "失败", tone: "danger" },
  refunded: { label: "退款", tone: "brand" },
};

function PushCell({ state, ratio }: { state: PushState; ratio: string | null }) {
  if (state === "pushed")
    return (
      <Chip tone="ok" icon={<Check className="size-2.5" />}>
        已推
      </Chip>
    );
  if (state === "partial") return <Chip tone="warn">{ratio ?? "部分"} 推</Chip>;
  if (state === "failed")
    return (
      <Chip tone="danger" icon={<X className="size-2.5" />}>
        失败
      </Chip>
    );
  return <span className="text-micro font-medium text-fg-tertiary">未推</span>;
}

export function PullRow({ r }: { r: PullRound }) {
  const res = RESULT[r.result];
  const failed = r.result === "failed";

  return (
    <BareRow>
      <span className="w-[70px] shrink-0 text-micro font-medium text-fg-tertiary">
        {fmtTime(r.created_at)}
      </span>

      <span className="w-14 shrink-0">
        <Chip tone={res.tone} className="w-full justify-center">
          {res.label}
        </Chip>
      </span>

      {/* 流向 */}
      <div className="flex min-w-0 flex-1 items-center gap-2">
        {failed ? (
          <span className="truncate text-body text-fg-tertiary">
            未拉到号 · 尝试 {vendorName(r.vendor_id)} · {r.fail_reason}
          </span>
        ) : (
          <>
            <span className="text-body-lg font-semibold tnum">+{r.count_purchased}</span>
            <span className="text-body text-fg-secondary">号</span>
            <span className="text-body text-fg-secondary">{vendorName(r.vendor_id)}</span>
            <ArrowRight className="size-3 shrink-0 text-fg-tertiary" />
            <span className="truncate text-body font-medium">{r.bus_name}</span>
            {r.result === "refunded" && (
              <span className="shrink-0 text-micro text-fg-tertiary">· {r.fail_reason}</span>
            )}
          </>
        )}
      </div>

      {/* 号状态 */}
      <div className="flex w-[90px] shrink-0 items-center justify-center gap-2.5">
        {r.alive_count > 0 && (
          <span className="flex items-center gap-1 text-micro text-fg-secondary">
            <span className="size-1.5 rounded-full bg-ok-solid" />
            {r.alive_count}
          </span>
        )}
        {r.dead_count > 0 && (
          <span className="flex items-center gap-1 text-micro text-fg-secondary">
            <span className="size-1.5 rounded-full bg-danger-solid" />
            {r.dead_count}
          </span>
        )}
        {r.alive_count === 0 && r.dead_count === 0 && (
          <span className="text-micro text-fg-tertiary">—</span>
        )}
      </div>

      <div className="flex w-20 shrink-0 justify-center">
        <PushCell state={r.push_state} ratio={r.push_ratio} />
      </div>

      <span
        className={cn(
          "w-[90px] shrink-0 text-right text-body font-semibold tnum",
          r.total_cost > 0 ? "text-ok-fg" : r.total_cost === 0 ? "text-fg-tertiary" : "",
        )}
      >
        {r.total_cost === 0 ? "0" : fmtCredits(r.total_cost, { sign: true })} 积分
      </span>
    </BareRow>
  );
}

/* ── 活动流行 ── */

const KIND: Record<Activity["kind"], { label: string; tone: "ok" | "warn" | "danger" | "brand" | "neutral" }> = {
  into_bus: { label: "入车", tone: "brand" },
  extract: { label: "提取", tone: "warn" },
  refill: { label: "补车", tone: "brand" },
  dead: { label: "号死", tone: "danger" },
  topup: { label: "充值", tone: "ok" },
  redeem: { label: "兑换", tone: "ok" },
  push: { label: "推池", tone: "neutral" },
};

export function ActivityRow({ a, onClick }: { a: Activity; onClick?: () => void }) {
  const k = KIND[a.kind];
  return (
    <BareRow onClick={onClick}>
      <span className="w-12 shrink-0 text-micro font-medium text-fg-tertiary">
        {fmtTime(a.created_at)}
      </span>
      <span className="w-12 shrink-0">
        <Chip tone={k.tone} className="w-full justify-center">
          {k.label}
        </Chip>
      </span>
      <span className="min-w-0 flex-1 truncate text-body font-medium">{a.summary}</span>
      <span
        className={cn(
          "w-24 shrink-0 text-right text-body font-semibold tnum",
          a.amount === null
            ? "text-fg-tertiary font-medium"
            : a.amount > 0
              ? "text-ok-fg"
              : "",
        )}
      >
        {a.amount === null ? "—" : `${fmtCredits(a.amount, { sign: true })} 积分`}
      </span>
    </BareRow>
  );
}

export { fmtLifespan };
