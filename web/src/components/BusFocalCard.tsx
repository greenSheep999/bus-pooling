import { Link } from "react-router-dom";
import { ArrowUpRight, Clock, Download, UserPlus, Users } from "lucide-react";
import { useBusCredentials } from "@/api/hooks";
import { Card, Chip } from "./ui/primitives";
import { PoolDistribution } from "./PoolDistribution";
import { avatarColor, avatarLetter, fmtCredits, fmtLifespan } from "@/lib/utils";
import type { Bus } from "@/types";

/** 拼车列表主视图右侧 focal 大卡 · 严格照 mock 图
    - 头：kind chip + 活跃 · 车友头像组 · N 天前建
    - 车名 hero · 成员名副行
    - 4 KPI 一排（无边框，数字大 + label 小 + 状态点）
    - 24h 调用趋势淡紫柱图 + 右侧 N 次/峰值
    - 底：紫 CTA「给这辆车拉号 P」+ 副动作车详情/邀请 · 右侧「下次自动补车 · N 分钟后」 */
export function BusFocalCard({
  bus,
  role,
  onPullClick,
}: {
  bus: Bus;
  role?: "owner" | "member";
  onPullClick: () => void;
}) {
  const { data: creds } = useBusCredentials(bus.id);

  const kindLabel =
    bus.kind === "single"
      ? "独享 · 个人"
      : bus.kind === "team"
        ? `拼车 · ${bus.member_count} 车友`
        : `搭车 · ${bus.member_count} 车友`;

  const created = new Date(bus.created_at);
  const daysAgo = Math.max(1, Math.floor((Date.now() - created.getTime()) / 86_400_000));

  // 下次自动补车倒计时 · mock 用 24 分钟后（真实来自策略引擎）
  const nextRefillMinutes = 24;

  return (
    <Card focal focalTone="brand" className="flex h-full flex-col gap-4 p-6">
      {/* 头 · kind chip + 活跃 · 头像组 · 天数 · 一行 */}
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-wrap items-center gap-2">
          <Chip tone="brand">
            <Users className="size-3" />
            {kindLabel}
          </Chip>
          {bus.status === "active" && <Chip tone="ok" dot>活跃</Chip>}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <MembersStack bus={bus} />
          <span className="hidden text-label text-fg-tertiary sm:inline">
            创建于 {daysAgo} 天前
          </span>
        </div>
      </div>

      {/* 车名 · 成员名副行 · 紧凑不用 space-y */}
      <div>
        <h2 className="truncate text-hero font-semibold tracking-tight">
          {bus.name}
        </h2>
        <p className="mt-0.5 text-label text-fg-tertiary">
          {bus.kind === "single" ? (
            <>你{role === "owner" && <> · <span className="font-medium text-fg-secondary">我发起</span></>}</>
          ) : (
            <>
              你 · <span className="font-medium text-fg-secondary">@wei</span> ·{" "}
              <span className="font-medium text-fg-secondary">@lin</span>
            </>
          )}
        </p>
      </div>

      {/* 4 KPI 一排 · 数字用 text-num 不用超大 */}
      <div className="grid grid-cols-4 gap-3">
        <FocalStat value={String(bus.alive_count)} label="号活着" dot="ok" />
        <FocalStat
          value={String(bus.dead_count)}
          label="已失效"
          dot={bus.dead_count > 0 ? "danger" : "neutral"}
        />
        <FocalStat
          value={fmtCredits(bus.spend_today)}
          unit="积分"
          label="今日消费"
        />
        <FocalStat
          value={fmtLifespan(bus.avg_lifespan_seconds)}
          label="平均寿命"
        />
      </div>

      {/* 号池分布 · 按 vendor · compact 版跟 BusCard 呼应 */}
      <PoolDistribution credentials={creds} variant="compact" label="号池分布 · 按 vendor" />

      {/* 底部动作行 · mt-auto 沉底 */}
      <div className="mt-auto flex flex-wrap items-center justify-between gap-2 pt-1">
        <div className="flex flex-wrap items-center gap-2">
          <button
            onClick={onPullClick}
            className="flex items-center gap-2 rounded-lg bg-brand px-4 py-2 font-semibold text-white shadow-card transition-opacity hover:opacity-90"
          >
            <Download className="size-4" />
            给这辆车拉号
            <kbd className="ml-0.5 rounded bg-white/20 px-1 py-0.5 text-[10px] font-semibold">P</kbd>
          </button>
          <Link
            to={`/buses/${bus.id}`}
            className="flex items-center gap-1.5 rounded-lg border border-hairline bg-bg px-4 py-2 font-medium text-fg-secondary transition-colors hover:bg-bg-elevated"
          >
            车详情
            <ArrowUpRight className="size-3.5" />
          </Link>
          {bus.kind !== "single" && (
            <button
              disabled
              className="flex items-center gap-1.5 rounded-lg border border-hairline bg-bg px-4 py-2 font-medium text-fg-secondary transition-colors hover:bg-bg-elevated"
            >
              <UserPlus className="size-3.5" />
              邀请车友
            </button>
          )}
        </div>

        {bus.strategy.auto_refill_enabled && (
          <span className="flex items-center gap-1.5 rounded-full border border-hairline bg-bg/60 px-3 py-1 text-label font-medium text-fg-secondary">
            <Clock className="size-3.5" />
            下次自动补车 · <span className="font-semibold tnum">{nextRefillMinutes}</span> 分钟后
          </span>
        )}
      </div>
    </Card>
  );
}

/* KPI 单元 · 大数字 + label + 状态点 */
function FocalStat({
  value, unit, label, dot,
}: {
  value: string;
  unit?: string;
  label: string;
  dot?: "ok" | "danger" | "neutral";
}) {
  return (
    <div className="space-y-1">
      <div className="flex items-baseline gap-1.5">
        <span className="text-num font-semibold tnum">{value}</span>
        {unit && <span className="text-label font-medium text-fg-tertiary">{unit}</span>}
      </div>
      <div className="flex items-center gap-1.5 text-label font-medium text-fg-tertiary">
        {dot === "ok" && <span className="size-1.5 rounded-full bg-ok-solid" />}
        {dot === "danger" && <span className="size-1.5 rounded-full bg-danger-solid" />}
        {label}
      </div>
    </div>
  );
}

/* 车友头像组 · 阶段 1a single 只 1 个 · 多人车堆叠显示 */
function MembersStack({ bus }: { bus: Bus }) {
  const seeds =
    bus.kind === "single"
      ? [bus.name]
      : [bus.name, bus.name + "b", bus.name + "c"].slice(0, bus.member_count);
  return (
    <div className="flex -space-x-2">
      {seeds.map((s, i) => {
        const { bg, fg } = avatarColor(s);
        return (
          <span
            key={i}
            className="grid size-8 place-items-center rounded-full border-2 border-bg font-semibold"
            style={{ backgroundColor: bg, color: fg }}
          >
            {avatarLetter(s)}
          </span>
        );
      })}
    </div>
  );
}
