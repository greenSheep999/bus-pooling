import { Bus as BusIcon, Zap, ZapOff } from "lucide-react";
import type { Bus } from "@/types";
import { Chip, Em } from "@/components/ui/primitives";
import { OwnerBadge } from "@/components/ui/tags";
import { avatarColor, avatarLetter, cn, fmtCredits } from "@/lib/utils";

/** 左列车小卡（Buses 页 · 车列表用）· 点选后右侧展示 focal 大卡
    - 头：kind Chip + owner badge · 头像靠右（跟 BusCard/BusFocalCard 视觉统一）
    - 车名 · 大数字 · 底部：补车模式 + 失效数 */
export function BusMiniCard({
  bus, role, active, onClick,
}: {
  bus: Bus;
  role?: "owner" | "member";
  active: boolean;
  onClick: () => void;
}) {
  // label 按当前人数（一辆车就是一辆车·CLAUDE.md §2）
  const kindLabel =
    bus.kind === "anon"
      ? `搭车 · ${bus.member_count} 车友`
      : bus.member_count > 1
        ? `拼车 · ${bus.member_count} 车友`
        : "独享";

  // 车主头像 · 用车名当种子出个稳定色
  const seed = bus.name;
  const { bg, fg } = avatarColor(seed);
  const letter = avatarLetter(seed);

  const auto = bus.strategy.auto_refill_enabled;

  return (
    <button
      onClick={onClick}
      className={cn(
        "flex h-full w-full flex-1 flex-col gap-3 rounded-panel border p-5 text-left shadow-card transition-all",
        active
          ? "border-brand bg-brand-subtle/30 shadow-hover"
          : "border-hairline bg-bg hover:-translate-y-0.5 hover:shadow-hover",
      )}
    >
      {/* 头 · kind Chip · 头像靠右 */}
      <div className="flex items-start justify-between gap-2">
        <Chip tone="brand">
          <BusIcon className="size-3" />
          {kindLabel}
        </Chip>
        <span
          className="grid size-8 shrink-0 place-items-center rounded-full font-semibold"
          style={{ backgroundColor: bg, color: fg }}
        >
          {letter}
        </span>
      </div>

      {/* 车名 · 我发起 badge 跟标题一行 */}
      <div className="flex items-center gap-2">
        <h3 className="min-w-0 truncate text-section font-semibold tracking-tight">
          {bus.name}
        </h3>
        {role === "owner" && <OwnerBadge />}
      </div>

      {/* 大数字 · 正常号 */}
      <div className="flex items-baseline justify-between">
        <div className="flex items-baseline gap-1.5">
          <span className="text-num font-semibold tnum">{bus.alive_count}</span>
          <span className="text-label text-fg-tertiary">
            个 · 已失效 <Em>{bus.dead_count}</Em>
          </span>
        </div>
        {bus.spend_today > 0 && (
          <span className="text-label font-semibold tnum text-danger-fg">
            -{fmtCredits(bus.spend_today)} 积分
          </span>
        )}
      </div>

      {/* 底 · 补车模式 · 跟 BusCard 呼应（简版） */}
      <div className="flex items-center gap-1.5 text-label">
        {auto ? (
          <>
            <Zap className="size-3.5 text-brand-strong" />
            <span className="font-semibold text-brand-strong">自动补车</span>
            <span className="text-fg-tertiary">
              · 保活 <Em>{bus.strategy.refill_watermark}</Em>
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
    </button>
  );
}
