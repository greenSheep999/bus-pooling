import type { Bus } from "@/types";
import { avatarColor, avatarLetter, cn, fmtCredits } from "@/lib/utils";

/** 左列车小卡（Buses 页 · 车列表用）· 点选后右侧展示 focal 大卡
    kind/成员数 · 车名 · 号活着数 · 今日消费 · 车主头像 */
export function BusMiniCard({
  bus, role, active, onClick,
}: {
  bus: Bus;
  role?: "owner" | "member";
  active: boolean;
  onClick: () => void;
}) {
  const kindLabel = bus.kind === "single" ? "独享" : bus.kind === "team" ? "邀请码" : "搭车";
  const memberLabel =
    bus.kind === "single" ? "个人" : `${bus.member_count} 车友`;

  // 车主头像 · 用车名当种子出个稳定色
  const seed = bus.name;
  const { bg, fg } = avatarColor(seed);
  const letter = avatarLetter(seed);

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
      {/* 头 · kind + 成员数 · 头像靠右 */}
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-1 text-label font-medium">
            <span className={cn(
              bus.kind === "single" ? "text-brand-strong" : "text-fg-secondary",
            )}>{kindLabel}</span>
            <span className="text-fg-tertiary">·</span>
            <span className="text-fg-tertiary">{memberLabel}</span>
            {role === "owner" && (
              <span className="ml-1 shrink-0 whitespace-nowrap rounded-md bg-brand-subtle px-1.5 py-[1px] text-[10px] font-semibold leading-[1.4] text-brand-strong">
                我发起
              </span>
            )}
          </div>
          <div className="truncate text-section font-semibold tracking-tight">{bus.name}</div>
        </div>
        <span
          className="grid size-8 shrink-0 place-items-center rounded-full font-semibold"
          style={{ backgroundColor: bg, color: fg }}
        >
          {letter}
        </span>
      </div>

      {/* 大数字 · 号活着 */}
      <div className="flex items-baseline justify-between">
        <div className="flex items-baseline gap-1.5">
          <span className="text-num font-semibold tnum">{bus.alive_count}</span>
        </div>
        {bus.spend_today > 0 && (
          <span className="text-label font-semibold tnum text-danger-fg">
            -{fmtCredits(bus.spend_today)} 积分
          </span>
        )}
      </div>

      {/* 底 · 状态点 · 副标 */}
      <div className="flex items-center justify-between text-label">
        <span className="flex items-center gap-1.5 text-fg-secondary">
          <span className="size-1.5 rounded-full bg-ok-solid" />
          号活着 · 失效 <span className="font-semibold tnum">{bus.dead_count}</span>
        </span>
        <span className="text-fg-tertiary">今日</span>
      </div>
    </button>
  );
}
