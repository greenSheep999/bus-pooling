import { ArrowUpRight, Bus as BusIcon, Zap, ZapOff } from "lucide-react";
import type { Bus } from "@/types";
import {
  useBusCredentials, useMe,
} from "@/api/hooks";
import { Card, Chip, Em } from "./ui/primitives";
import { OwnerBadge } from "./ui/tags";
import { PoolDistribution } from "./PoolDistribution";
import {
  avatarColor, avatarLetter, cn, fmtCredits, fmtLifespan, toCredits, vendorLabel,
} from "@/lib/utils";

/** 车卡 · 紧凑版（展开所有车时用）· 跟 Focal 大卡共享信息模块，只是密度更高
    - 头：kind chip + 活跃 · 车名 · 「我发起」· 头像 · 「查看→」
    - 主数字：号在池 · 今日消费（分左右）
    - 号池分布区（vendor 段条 + 明细）
    - 策略区（分区隔离，不跟 vendor 混）· 自动补车 · 单价 · 日限 */
export function BusCard({ bus, role }: { bus: Bus; role?: "owner" | "member" }) {
  const { data: me } = useMe();
  const { data: creds } = useBusCredentials(bus.id);
  const s = bus.strategy;
  const kindLabel =
    bus.kind === "single"
      ? "独享 · 个人"
      : bus.kind === "team"
        ? `拼车 · ${bus.member_count} 车友`
        : `搭车 · ${bus.member_count} 车友`;

  const seed = bus.name;
  const { bg, fg } = avatarColor(seed);

  return (
    <Card to={`/buses/${bus.id}`} className="flex flex-col gap-4 p-6">
      {/* 头 · 类型 chip + 活跃 · 头像组 · 「查看→」 */}
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <Chip tone="brand">
            <BusIcon className="size-3" />
            {kindLabel}
          </Chip>
          {bus.status === "active" && <Chip tone="ok" dot>活跃</Chip>}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <span
            className="grid size-7 place-items-center rounded-full font-semibold"
            style={{ backgroundColor: bg, color: fg }}
          >
            {avatarLetter(seed)}
          </span>
        </div>
      </div>

      {/* 车名 + 「我发起」 · 副行 · 「查看→」 */}
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h3 className="min-w-0 truncate text-body-lg font-semibold">
              {bus.name}
            </h3>
            {role === "owner" && <OwnerBadge />}
          </div>
        </div>
        <span className="flex shrink-0 items-center gap-1 text-label font-semibold text-brand-strong">
          查看 <ArrowUpRight className="size-3.5" />
        </span>
      </div>

      {/* 主数字 · 号在池 / 今日消费 左右分栏 */}
      <div className="grid grid-cols-2 gap-4">
        <div className="space-y-0.5">
          <div className="flex items-baseline gap-1.5">
            <span className="text-num font-semibold tnum">{bus.alive_count}</span>
            <span className="text-label text-fg-tertiary">
              个 · 已失效 <Em>{bus.dead_count}</Em>
            </span>
          </div>
          <div className="flex items-center gap-1.5 text-label font-medium text-fg-tertiary">
            <span className="size-1.5 rounded-full bg-ok-solid" />
            号在池
          </div>
        </div>
        <div className="space-y-0.5 text-right">
          <div
            className={cn(
              "text-num font-semibold tnum",
              bus.spend_today > 0 ? "text-danger-fg" : "text-fg-tertiary",
            )}
          >
            {bus.spend_today > 0 ? `-${fmtCredits(bus.spend_today)}` : "0"}
          </div>
          <div className="text-label font-medium text-fg-tertiary">
            今日消费 · 积分
          </div>
        </div>
      </div>

      {/* 号池分布区 · 按 vendor · 独立分区 */}
      <PoolDistribution credentials={creds} label="号池分布 · 按 vendor" variant="compact" />

      {/* 策略区 · mt-auto 沉底贴分隔线 · 号池行数不同时中间空档自然变化 */}
      <div className="mt-auto space-y-1.5 rounded-xl bg-bg-elevated p-3">
        <div className="flex items-center gap-1.5 text-label">
          {s.auto_refill_enabled ? (
            <>
              <Zap className="size-3.5 text-brand-strong" />
              <span className="font-semibold text-brand-strong">自动补车</span>
              <span className="text-fg-tertiary">
                · 保活 <Em>{s.refill_watermark}</Em>
                {s.per_round_count && (
                  <> · 每轮 <Em>{s.per_round_count}</Em></>
                )}
              </span>
            </>
          ) : (
            <>
              <ZapOff className="size-3.5 text-fg-tertiary" />
              <Em plain>手动模式</Em>
              <span className="text-fg-tertiary">· 号少时提醒</span>
            </>
          )}
        </div>
        {(s.max_unit_price || s.daily_spend_limit || s.preferred_vendor) && (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-label text-fg-tertiary">
            {s.max_unit_price && (
              <span>
                单价 ≤ <Em>{toCredits(s.max_unit_price)}</Em>
              </span>
            )}
            {s.daily_spend_limit && (
              <span>
                日限 <Em>{toCredits(s.daily_spend_limit)}</Em>
              </span>
            )}
            {s.preferred_vendor && (
              <span>
                首选 <Em plain>{vendorLabel(s.preferred_vendor, !!me?.invited)}</Em>
              </span>
            )}
          </div>
        )}
      </div>

      {/* 底行 · 平均寿命 · 紧跟策略 pill · 分隔线区分 */}
      <div className="flex items-center justify-between border-t border-hairline pt-3 text-label">
        <span className="text-fg-tertiary">
          平均寿命{" "}
          <Em>{fmtLifespan(bus.avg_lifespan_seconds)}</Em>
        </span>
        <span className="text-fg-tertiary">
          创建于 {new Date(bus.created_at).toLocaleDateString("zh-CN")}
        </span>
      </div>
    </Card>
  );
}
