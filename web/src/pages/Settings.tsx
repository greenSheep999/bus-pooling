import type { LucideIcon } from "lucide-react";
import { Bot, ChevronRight, Database, KeyRound, SlidersHorizontal, UserCog } from "lucide-react";
import type { ReactNode } from "react";
import { useApiKeys, useDownstream, useGlobalStrategy, useMe, useWebhook } from "@/api/hooks";
import { Trans, useTranslation } from "react-i18next";
import { Card, Chip, Em } from "@/components/ui/primitives";
import { fmtCredits } from "@/lib/utils";

/** 设置索引 · 设置的主入口 */
export default function Settings() {
  const { t } = useTranslation("settings");
  const { data: gs } = useGlobalStrategy();
  const { data: ds } = useDownstream();
  const { data: wh } = useWebhook();
  const { data: keys } = useApiKeys();
  const { data: me } = useMe();

  const activeKeys = (keys ?? []).filter((k) => !k.revoked).length;

  return (
    <div className="space-y-section">
      <div className="min-w-0 space-y-2">
        <h1 className="text-hero font-semibold">{t("page.title")}</h1>
        <p className="text-fg-tertiary">{t("page.subtitle")}</p>
      </div>

      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
        {/* 拉号偏好放第一张 —— 它是唯一会**拦下操作**的设置（每日上限），
            另外三个都是"连了/没连"性质。用户最该先看到会花钱和被限流的那个 */}
        <SettingCard
          to="/settings/preferences"
          icon={SlidersHorizontal}
          title={t("card.preferences.title")}
          desc={t("card.preferences.desc")}
          status={
            gs && (gs.daily_round_limit != null || gs.daily_spend_limit != null)
              ? <Chip tone="ok" dot>{t("card.preferences.status.limit-set")}</Chip>
              : <Chip tone="warn" dot>{t("card.preferences.status.limit-unset")}</Chip>
          }
          meta={
            gs
              ? (
                <Trans
                  i18nKey="card.preferences.meta"
                  ns="settings"
                  values={{
                    rounds: gs.used_today.rounds,
                    roundLimit: gs.daily_round_limit != null ? t("card.preferences.meta.round-limit", { limit: gs.daily_round_limit }) : "",
                    spend: fmtCredits(gs.used_today.spend),
                    spendLimit: gs.daily_spend_limit != null ? t("card.preferences.meta.spend-limit", { limit: fmtCredits(gs.daily_spend_limit) }) : "",
                  }}
                  components={{ 1: <Em />, 3: <Em /> }}
                />
              )
              : null
          }
        />

        <SettingCard
          to="/settings/downstream"
          icon={Database}
          title={t("card.downstream.title")}
          desc={t("card.downstream.desc")}
          status={
            ds?.connected
              ? <Chip tone="ok" dot>{t("card.downstream.status.connected")}</Chip>
              : <Chip tone="danger" dot>{t("card.downstream.status.disconnected")}</Chip>
          }
          meta={
            ds
              ? (
                <Trans
                  i18nKey="card.downstream.meta"
                  ns="settings"
                  values={{ rate: (ds.push_success_rate * 100).toFixed(1), total: ds.push_total }}
                  components={{ 1: <Em />, 3: <Em /> }}
                />
              )
              : null
          }
        />

        <SettingCard
          to="/settings/webhook"
          icon={Bot}
          title={t("card.webhook.title")}
          desc={t("card.webhook.desc")}
          status={
            wh?.enabled
              ? <Chip tone="ok" dot>{t("card.webhook.status.enabled")}</Chip>
              : <Chip tone="neutral" dot>{t("card.webhook.status.disabled")}</Chip>
          }
          meta={wh ? (
            <Trans
              i18nKey="card.webhook.meta"
              ns="settings"
              values={{ count: wh.events.length }}
              components={{ 1: <Em /> }}
            />
          ) : null}
        />

        <SettingCard
          to="/settings/api-keys"
          icon={KeyRound}
          title={t("card.api-keys.title")}
          desc={t("card.api-keys.desc")}
          status={
            activeKeys > 0
              ? <Chip tone="ok" dot>{t("card.api-keys.status.active", { count: activeKeys })}</Chip>
              : <Chip tone="neutral" dot>{t("card.api-keys.status.none")}</Chip>
          }
          meta={keys ? (
            <Trans
              i18nKey="card.api-keys.meta"
              ns="settings"
              values={{ total: keys.length, revoked: keys.length - activeKeys }}
              components={{ 1: <Em />, 3: <Em /> }}
            />
          ) : null}
        />

        <SettingCard
          to="/settings/account"
          icon={UserCog}
          title={t("card.account.title")}
          desc={t("card.account.desc")}
          status={<Chip tone="neutral" dot>{t("card.account.status")}</Chip>}
          meta={
            <>
              {t("card.account.meta.email-label")} <Em>{me?.email_verified ? t("card.account.meta.email.verified") : t("card.account.meta.email.unverified")}</Em>
              {" · "}{t("card.account.meta.social-label")} <Em>{t("card.account.meta.social.unbound")}</Em>
            </>
          }
        />
      </div>
    </div>
  );
}

function SettingCard({
  to, icon: Icon, title, desc, status, meta,
}: {
  to: string;
  icon: LucideIcon;
  title: string;
  desc: string;
  status: ReactNode;
  meta: ReactNode;
}) {
  /* Card 传 to 就整卡可点 + 自带 hover 悬浮（可点区域 = 浮起区域） */
  return (
    <Card to={to} className="flex flex-col gap-3 p-6">
      <div className="flex items-start justify-between gap-3">
        <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-bg-elevated">
          <Icon className="size-4 text-fg-secondary" />
        </span>
        {status}
      </div>

      <div className="min-w-0 space-y-1">
        <div className="flex items-center gap-1.5">
          <span className="font-semibold">{title}</span>
          <ChevronRight className="size-3.5 text-fg-tertiary" />
        </div>
        <p className="text-label text-fg-tertiary">{desc}</p>
      </div>

      {meta && <div className="mt-auto text-label text-fg-tertiary">{meta}</div>}
    </Card>
  );
}
