import { Trans, useTranslation } from "react-i18next";
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
  const { t, i18n } = useTranslation("buses");
  const { data: me } = useMe();
  const { data: creds } = useBusCredentials(bus.id);
  const s = bus.strategy;
  // 一辆车就是一辆车（CLAUDE.md §2）· label 按当前人数
  // - anon 搭车池 · 显示"搭车 · N 车友"
  // - 用户建的车（team/single 遗留）· 1 人显示"独享"·多人显示"N 人拼车"
  const kindLabel =
    bus.kind === "anon"
      ? t("kind.anon-with-count", { count: bus.member_count })
      : bus.member_count > 1
        ? t("kind.multi-card", { count: bus.member_count })
        : t("kind.solo");

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
          {bus.status === "active" && <Chip tone="ok" dot>{t("card.chip-active")}</Chip>}
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
          {t("card.view")} <ArrowUpRight className="size-3.5" />
        </span>
      </div>

      {/* 主数字 · 号在池 / 今日消费 左右分栏 */}
      <div className="grid grid-cols-2 gap-4">
        <div className="space-y-0.5">
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
          <div className="flex items-center gap-1.5 text-label font-medium text-fg-tertiary">
            <span className="size-1.5 rounded-full bg-ok-solid" />
            {t("card.alive-status")}
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
            {t("card.spend-today-label")}
          </div>
        </div>
      </div>

      {/* 号池分布区 · 按 vendor · 独立分区 */}
      <PoolDistribution credentials={creds} label={t("card.distribution-label")} variant="compact" />

      {/* 策略区 · mt-auto 沉底贴分隔线 · 号池行数不同时中间空档自然变化 */}
      <div className="mt-auto space-y-1.5 rounded-xl bg-bg-elevated p-3">
        <div className="flex items-center gap-1.5 text-label">
          {s.auto_refill_enabled ? (
            <>
              <Zap className="size-3.5 text-brand-strong" />
              <span className="font-semibold text-brand-strong">{t("card.refill.auto")}</span>
              <span className="text-fg-tertiary">
                <Trans
                  t={t}
                  i18nKey="card.refill.watermark"
                  values={{ count: s.refill_watermark }}
                  components={{ 1: <Em /> }}
                />
                {s.per_round_count && (
                  <>
                    {" "}
                    <Trans
                      t={t}
                      i18nKey="card.refill.per-round"
                      values={{ count: s.per_round_count }}
                      components={{ 1: <Em /> }}
                    />
                  </>
                )}
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
        {(s.max_unit_price || s.daily_spend_limit || s.preferred_vendor) && (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-label text-fg-tertiary">
            {s.max_unit_price && (
              <Trans
                t={t}
                i18nKey="card.strategy.max-price"
                values={{ value: toCredits(s.max_unit_price) }}
                components={{ 1: <Em /> }}
                parent="span"
              />
            )}
            {s.daily_spend_limit && (
              <Trans
                t={t}
                i18nKey="card.strategy.daily-limit"
                values={{ value: toCredits(s.daily_spend_limit) }}
                components={{ 1: <Em /> }}
                parent="span"
              />
            )}
            {s.preferred_vendor && (
              <Trans
                t={t}
                i18nKey="card.strategy.preferred"
                values={{ vendor: vendorLabel(s.preferred_vendor, me?.tier) }}
                components={{ 1: <Em plain /> }}
                parent="span"
              />
            )}
          </div>
        )}
      </div>

      {/* 底行 · 平均寿命 · 紧跟策略 pill · 分隔线区分 */}
      <div className="flex items-center justify-between border-t border-hairline pt-3 text-label">
        <span className="text-fg-tertiary">
          {t("card.lifespan-prefix")}{" "}
          <Em>{fmtLifespan(bus.avg_lifespan_seconds)}</Em>
        </span>
        <span className="text-fg-tertiary">
          {t("card.created-at", { date: new Date(bus.created_at).toLocaleDateString(i18n.language) })}
        </span>
      </div>
    </Card>
  );
}
