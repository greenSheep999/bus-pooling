import { ArrowRight, Check, X } from "lucide-react";
import { useMe } from "@/api/hooks";
import { BareRow, Chip } from "./ui/primitives";
import { TokenTag } from "./ui/tags";
import {
  cn, fmtCredits, fmtLifespan, fmtTime, vendorLabel,
} from "@/lib/utils";
import type { Activity, PullResult, PullRound, PushState } from "@/types";

/* ── 拉号轮次行 · 6 列 ── */

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

export function PullRow({ r }: { r: PullRound }) {
  const { data: me } = useMe();
  const res = RESULT[r.result];
  const failed = r.result === "failed";

  return (
    <BareRow>
      <span className="w-[70px] shrink-0 text-label font-medium text-fg-tertiary">
        {fmtTime(r.created_at)}
      </span>

      <span className="w-14 shrink-0">
        <Chip tone={res.tone} dot className="w-full justify-center">
          {res.label}
        </Chip>
      </span>

      {/* 流向 */}
      <div className="flex min-w-0 flex-1 items-center gap-2">
        {failed ? (
          <span className="truncate text-fg-tertiary">
            未拉到号 · 尝试 {vendorLabel(r.vendor_id, !!me?.invited)} · {r.fail_reason}
          </span>
        ) : (
          <>
            <span className="font-semibold tnum">+{r.count_purchased}</span>
            <span className="text-fg-secondary">号</span>
            <span className="text-fg-secondary">{vendorLabel(r.vendor_id, !!me?.invited)}</span>
            <ArrowRight className="size-3 shrink-0 text-fg-tertiary" />
            <span className="truncate font-medium">{r.bus_name}</span>
            {r.result === "refunded" && (
              <span className="shrink-0 text-label text-fg-tertiary">· {r.fail_reason}</span>
            )}
          </>
        )}
      </div>

      {/* 号状态 */}
      <div className="flex w-[90px] shrink-0 items-center justify-center gap-2.5">
        {r.alive_count > 0 && (
          <span className="flex items-center gap-1 text-label text-fg-secondary">
            <span className="size-1.5 rounded-full bg-ok-solid" />
            {r.alive_count}
          </span>
        )}
        {r.dead_count > 0 && (
          <span className="flex items-center gap-1 text-label text-fg-secondary">
            <span className="size-1.5 rounded-full bg-danger-solid" />
            {r.dead_count}
          </span>
        )}
        {r.alive_count === 0 && r.dead_count === 0 && (
          <span className="text-label text-fg-tertiary">-</span>
        )}
      </div>

      <div className="flex w-20 shrink-0 justify-center">
        <PushCell state={r.push_state} ratio={r.push_ratio} />
      </div>

      <span
        className={cn(
          "w-[90px] shrink-0 text-right font-semibold tnum",
          r.total_cost > 0 ? "text-ok-fg" : r.total_cost === 0 ? "text-fg-tertiary" : "",
        )}
      >
        {r.total_cost === 0 ? "0" : fmtCredits(r.total_cost, { sign: true })} 积分
      </span>
    </BareRow>
  );
}

/* ── 活动流行 ── */

type BadgeTone = "ok" | "warn" | "danger" | "brand" | "neutral";

const KIND: Record<Activity["kind"], { label: string; tone: BadgeTone }> = {
  into_bus: { label: "入车", tone: "brand" },
  extract: { label: "提取", tone: "warn" },
  refill: { label: "补车", tone: "brand" },
  dead: { label: "号失效", tone: "danger" },
  topup: { label: "充值", tone: "ok" },
  redeem: { label: "兑换", tone: "ok" },
  push: { label: "推池", tone: "neutral" },
};

/* 去向流可视化：vendor [badge] → 车/号池 [badge]
   只在真正涉及"号从 A 到 B 的流转"时使用 —— extract / into_bus / push_pool / pending
   其他活动（补车 / 号失效 / 充值 / 兑换）直接叙述，不套 badge */
const FLOW_TARGETS: Record<string, boolean> = {
  into_bus: true,
  push_pool: true,
  handoff: true,
  pending: true,
};

/** 流转 badge · 走 TokenTag sm · 保持全站 vendor/bus 标签样式一致（避免第三处样式漂移） */
function FlowBadge({ children }: { children: React.ReactNode }) {
  return <TokenTag size="sm">{children}</TokenTag>;
}

/** 内容单元：把活动描述完整渲染在这一列，不做多列拆分
    - 号流转（vendor → 车/号池）用两个 badge + 箭头，清晰又紧凑
    - 补车 / 失效 / 充值 / 兑换：完整文字叙述，不硬套 badge */
function ActivityContent({ a }: { a: Activity }) {
  const isFlow = a.target_kind && FLOW_TARGETS[a.target_kind];

  if (isFlow && a.source && a.target) {
    /* 号流转行 · 完整中文描述句：
       「共 <动词> N 个号 / 个 key，从 [vendor] → [目的地]」
       动词按 kind 派生：提取 / 入车 / 推池 · 数量加粗嵌在句子里 · 流转 badge 在后 */
    const verb =
      a.kind === "extract" ? "提取"
        : a.kind === "into_bus" ? "入车"
          : a.kind === "push" ? "推池"
            : "拉";

    return (
      <span className="flex min-w-0 items-center gap-1.5 truncate">
        <span className="shrink-0 text-fg-secondary">
          共{verb}
        </span>
        {a.count !== undefined && (
          <span className="shrink-0 font-semibold tnum text-fg">
            {a.count}
          </span>
        )}
        <span className="shrink-0 text-fg-secondary">
          {a.count_unit ?? "个"}，从
        </span>
        <FlowBadge>{a.source}</FlowBadge>
        <ArrowRight className="size-3 shrink-0 text-fg-tertiary" />
        <FlowBadge>{a.target}</FlowBadge>
      </span>
    );
  }

  // 补车 / 失效 / 充值 / 兑换：完整叙述（用 summary 兜底最保险）
  return (
    <span className="min-w-0 truncate font-medium text-fg-secondary">
      {a.summary}
    </span>
  );
}

export function ActivityRow({ a, onClick }: { a: Activity; onClick?: () => void }) {
  const k = KIND[a.kind];

  return (
    <BareRow onClick={onClick}>
      {/* 时间 · 统一 MM/DD HH:mm · tnum 对齐 */}
      <span className="w-[86px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(a.created_at)}
      </span>

      {/* 类型 badge · 定宽不换行 */}
      <span className="w-14 shrink-0">
        <Chip tone={k.tone} className="w-full justify-center whitespace-nowrap">
          {k.label}
        </Chip>
      </span>

      {/* 内容：完整描述（量 · vendor → 目的地 · 或者完整叙述文字） */}
      <div className="min-w-0 flex-1">
        <ActivityContent a={a} />
      </div>

      {/* 金额 */}
      <span
        className={cn(
          "w-24 shrink-0 text-right font-semibold tnum",
          a.amount === null
            ? "text-fg-tertiary font-medium"
            : a.amount > 0
              ? "text-ok-fg"
              : "",
        )}
      >
        {a.amount === null ? "-" : `${fmtCredits(a.amount, { sign: true })} 积分`}
      </span>
    </BareRow>
  );
}

export { fmtLifespan };
