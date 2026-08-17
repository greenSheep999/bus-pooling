import { useTranslation } from "react-i18next";
import { ArrowRight, Check, X } from "lucide-react";
import { useMe } from "@/api/hooks";
import { BareRow, Chip } from "./ui/primitives";
import { TokenTag } from "./ui/tags";
import {
  cn, fmtCredits, fmtLifespan, fmtTime, vendorLabel,
} from "@/lib/utils";
import type { Activity, PullResult, PullRound, PushState } from "@/types";

/* ── 拉号轮次行 · 6 列 ── */

const RESULT_TONE: Record<PullResult, "ok" | "warn" | "danger" | "brand"> = {
  success: "ok",
  partial: "warn",
  failed: "danger",
  refunded: "brand",
};

function PushCell({ state, ratio }: { state: PushState; ratio: string | null }) {
  const { t } = useTranslation("common");
  if (state === "pushed") return <Chip tone="ok" icon={<Check className="size-3" />}>{t("status.push.pushed")}</Chip>;
  if (state === "partial") return <Chip tone="warn" icon={<Check className="size-3" />}>{t("status.push.partial", { ratio })}</Chip>;
  if (state === "failed") return <Chip tone="danger" icon={<X className="size-3" />}>{t("status.push.failed")}</Chip>;
  return <Chip tone="neutral">{t("status.push.none")}</Chip>;
}

export function PullRow({ r }: { r: PullRound }) {
  const { t } = useTranslation("common");
  const { data: me } = useMe();
  const failed = r.result === "failed";
  const resTone = RESULT_TONE[r.result];
  const resLabel = t(`status.pull-result.${r.result}`);

  return (
    <BareRow>
      <span className="w-[70px] shrink-0 text-label font-medium text-fg-tertiary">
        {fmtTime(r.created_at)}
      </span>

      <span className="w-14 shrink-0">
        <Chip tone={resTone} dot className="w-full justify-center">
          {resLabel}
        </Chip>
      </span>

      {/* 流向 */}
      <div className="flex min-w-0 flex-1 items-center gap-2">
        {failed ? (
          <span className="truncate text-fg-tertiary">
            {t("activity.pull.failed", {
              vendor: vendorLabel(r.vendor_id, me?.tier),
              reason: r.fail_reason,
            })}
          </span>
        ) : (
          <>
            <span className="font-semibold tnum">+{r.count_purchased}</span>
            <span className="text-fg-secondary">{t("activity.pull.count-suffix")}</span>
            <span className="text-fg-secondary">{vendorLabel(r.vendor_id, me?.tier)}</span>
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
        {r.total_cost === 0 ? t("activity.pull.credits-zero") : fmtCredits(r.total_cost, { sign: true })} {t("activity.pull.credits-suffix")}
      </span>
    </BareRow>
  );
}

/* ── 活动流行 ── */

type BadgeTone = "ok" | "warn" | "danger" | "brand" | "neutral";

const KIND_TONE: Record<Activity["kind"], BadgeTone> = {
  into_bus: "brand",
  extract: "warn",
  refill: "brand",
  dead: "danger",
  topup: "ok",
  redeem: "ok",
  push: "neutral",
  handoff: "neutral",
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
  const { t } = useTranslation("common");
  const isFlow = a.target_kind && FLOW_TARGETS[a.target_kind];

  /* 固定去向（待派 / 我的号池 / 已拿走）**没有 target** —— 文案由 target_kind 出 i18n ·
     所以这里不能要求 a.target 非空（要求了那几行会整行空白） */
  if (isFlow && a.source) {
    /* 号流转行 · 完整中文描述句：
       「共 <动词> N 个号 / 个 key，从 [vendor] → [目的地]」
       动词按 kind 派生：提取 / 入车 / 推池 · 数量加粗嵌在句子里 · 流转 badge 在后 */
    const verbKey =
      a.kind === "extract" ? "extract"
        : a.kind === "into_bus" ? "into_bus"
          : a.kind === "push" ? "push"
            : "fallback";
    const verb = t(`activity.verb.${verbKey}`);

    return (
      <span className="flex min-w-0 items-center gap-1.5 truncate">
        <span className="shrink-0 text-fg-secondary">
          {t("activity.flow.sum-prefix", { verb })}
        </span>
        {a.count !== undefined && (
          <span className="shrink-0 font-semibold tnum text-fg">
            {a.count}
          </span>
        )}
        {/* 量词走 i18n · **不用后端的 count_unit**（那是后端硬编码的中文 ·
            英文用户会看到"个号"）· 后端只该给数字 · 量词是文案 */}
        <span className="shrink-0 text-fg-secondary">
          {t("activity.flow.count-unit-fallback")}{t("activity.flow.from")}
        </span>
        <FlowBadge>{a.source}</FlowBadge>
        <ArrowRight className="size-3 shrink-0 text-fg-tertiary" />
        {/* 去向:车名是数据(后端给)· 固定去向(待派/我的号池/已拿走)是文案(走 i18n) */}
        <FlowBadge>{a.target || t(`activity.target.${a.target_kind}`)}</FlowBadge>
      </span>
    );
  }

  // 号失效 · 前端组句（后端别塞中文 summary —— 英文用户会看到中文）
  // masked key 和 vendor 是数据 · "失效"是文案
  if (a.kind === "dead" && a.target) {
    return (
      <span className="flex min-w-0 items-center gap-1.5">
        <FlowBadge>{a.target}</FlowBadge>
        {a.source && <FlowBadge>{a.source}</FlowBadge>}
        <span className="shrink-0 text-fg-secondary">{t("activity.dead-suffix")}</span>
      </span>
    );
  }

  // 补车 / 充值 / 兑换：summary 非空 = 运营写的 memo 原文（**数据**·直接显示）·
  // 空则按 summary_code 出 i18n 兜底文案（后端只给码·不给中文）
  return (
    <span className="min-w-0 truncate font-medium text-fg-secondary">
      {a.summary || (a.summary_code ? t(`activity.ledger.${a.summary_code}`) : "")}
    </span>
  );
}

export function ActivityRow({ a, onClick }: { a: Activity; onClick?: () => void }) {
  const { t } = useTranslation("common");
  const tone = KIND_TONE[a.kind];
  const kindLabel = t(`activity.kind.${a.kind}`);

  return (
    <BareRow onClick={onClick}>
      {/* 时间 · 统一 MM/DD HH:mm · tnum 对齐 */}
      <span className="w-[86px] shrink-0 text-label font-medium tnum text-fg-tertiary">
        {fmtTime(a.created_at)}
      </span>

      {/* 类型 badge · **按内容自适应**（原来 w-14 写死 + Chip w-full 撑满 ——
          中文"补车"两字刚好 · 英文 "Key expired" 直接撑出格子）·
          给 min-w 保证短词（Push / 推池）也对齐 · 不写死上限 */}
      <span className="flex min-w-[56px] shrink-0 justify-start">
        <Chip tone={tone} className="whitespace-nowrap">
          {kindLabel}
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
        {a.amount === null ? "-" : `${fmtCredits(a.amount, { sign: true })} ${t("activity.pull.amount-suffix")}`}
      </span>
    </BareRow>
  );
}

export { fmtLifespan };
