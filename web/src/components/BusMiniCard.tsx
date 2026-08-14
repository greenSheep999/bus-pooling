import { Link } from "react-router-dom";
import { Trans, useTranslation } from "react-i18next";
import { Bus as BusIcon, Zap, ZapOff } from "lucide-react";
import type { Bus } from "@/types";
import { useGlobalStrategy } from "@/api/hooks";
import { Chip, Em } from "@/components/ui/primitives";
import { OwnerBadge } from "@/components/ui/tags";
import { avatarColor, avatarLetter, fmtCredits } from "@/lib/utils";

/** 左列车小卡（Buses 页 · 车列表用）· 点进去到车详情
 *  - 头：kind Chip + owner badge · 头像靠右（跟 BusCard/BusFocalCard 视觉统一）
 *  - 车名 · 大数字 · 底部：补车模式 + 失效数 */
export function BusMiniCard({
  bus, role,
}: {
  bus: Bus;
  role?: "owner" | "member";
}) {
  const { t } = useTranslation("buses");
  const { data: gs } = useGlobalStrategy();

  // label 按当前人数（一辆车就是一辆车·CLAUDE.md §2 · anon 是系统撮合池）
  const kindLabel =
    bus.kind === "anon"
      ? t("kind.anon-with-count", { count: bus.member_count })
      : bus.member_count > 1
        ? t("kind.multi-card", { count: bus.member_count })
        : t("kind.solo");

  const seed = bus.name;
  const { bg, fg } = avatarColor(seed);
  const letter = avatarLetter(seed);

  /* 1f-B · auto_refill_enabled / refill_watermark 可为 null · null = 跟随全局
     卡片上显示的是"实际生效值"(§4.3.5.1) —— 车级 null 就读全局 */
  const auto = bus.strategy.auto_refill_enabled ?? gs?.default_auto_refill_enabled ?? false;
  const watermark = bus.strategy.refill_watermark ?? gs?.default_refill_watermark ?? 0;

  return (
    <Link
      to={`/buses/${bus.id}`}
      className="flex h-full w-full flex-1 flex-col gap-3 rounded-panel border border-hairline bg-bg p-5 text-left shadow-card transition-all hover:-translate-y-0.5 hover:border-brand hover:shadow-hover"
    >
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

      <div className="flex items-center gap-2">
        <h3 className="min-w-0 truncate text-section font-semibold tracking-tight">
          {bus.name}
        </h3>
        {role === "owner" && <OwnerBadge />}
      </div>

      <div className="flex items-baseline justify-between">
        <div className="flex items-baseline gap-1.5">
          <span className="text-num font-semibold tnum">{bus.alive_count}</span>
          <span className="text-label text-fg-tertiary">
            <Trans
              t={t}
              i18nKey="card.alive-suffix"
              values={{ dead: bus.dead_count }}
              components={{ 1: <Em /> }}
            />
          </span>
        </div>
        {bus.spend_today > 0 && (
          <span className="text-label font-semibold tnum text-danger-fg">
            -{fmtCredits(bus.spend_today)} {t("card.credits-unit")}
          </span>
        )}
      </div>

      <div className="flex items-center gap-1.5 text-label">
        {auto ? (
          <>
            <Zap className="size-3.5 text-brand-strong" />
            <span className="font-semibold text-brand-strong">{t("card.refill.auto")}</span>
            <span className="text-fg-tertiary">
              <Trans
                t={t}
                i18nKey="card.refill.watermark"
                values={{ count: watermark }}
                components={{ 1: <Em /> }}
              />
            </span>
          </>
        ) : (
          <>
            <ZapOff className="size-3.5 text-fg-tertiary" />
            <Em plain>{t("card.refill.manual")}</Em>
            <span className="text-fg-tertiary">{t("card.refill.manual-hint")}</span>
          </>
        )}
      </div>
    </Link>
  );
}
