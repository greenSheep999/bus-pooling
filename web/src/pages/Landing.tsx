import type { LucideIcon } from "lucide-react";
import {
  Activity, ArrowUpRight, Bus, Database, Shield, Sparkles, Users,
} from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { usePromos } from "@/api/hooks";
import { DiscordLogo, TelegramLogo } from "@/components/ui/brand-icons";
import { Button } from "@/components/ui/button";
import { Card, Chip } from "@/components/ui/primitives";
import { PublicControls } from "@/components/PublicControls";
import LogoMark from "@/assets/logo/mark.svg";

/** Landing · 未登录访客首页（迁自 kiro-auto MarketingPage · 迁移时删掉套餐 / 价格 · 换 bus-pooling 组件系统）
 *  已登录用户由 RootGate 直接跳 /overview · 这里只面向未登录
 *  内容不放具体价格 / 号池细节 · CLAUDE.md §0.1 不许暴露加价幅度 */
export default function Landing() {
  const { t } = useTranslation("landing");
  const { data: promos } = usePromos();

  return (
    <div className="min-h-dvh bg-bg text-fg">
      {/* 极简公共 header · 不用 AppLayout（那个假设已登录）
          右侧顺序：语言 / 主题（PublicControls）· 登录 · 注册 */}
      <header className="border-b border-hairline bg-bg/85 backdrop-blur-xl">
        <div className="page-container flex h-14 items-center justify-between">
          <Link to="/" className="flex items-center gap-2.5">
            <img src={LogoMark} alt="" className="size-7 shrink-0 rounded-lg" />
            <span className="text-body-lg font-semibold tracking-tight">bus-pooling</span>
          </Link>
          <div className="flex items-center gap-2">
            <PublicControls />
            <div className="ml-1 hidden h-6 w-px bg-hairline sm:block" />
            <Button variant="ghost" asChild>
              <Link to="/login">{t("nav.login")}</Link>
            </Button>
            <Button variant="primary" asChild>
              <Link to="/register">{t("nav.register")}</Link>
            </Button>
          </div>
        </div>
      </header>

      <main className="page-container space-y-section py-12 sm:py-16">
        {/* Hero · 两段 desc + 5 个特性 chip · 迁自 kiro-auto MarketingPage 的调性 */}
        <section className="flex flex-col items-center gap-4 text-center">
          <Chip tone="brand" icon={<Sparkles className="size-3" />}>{t("hero.badge")}</Chip>
          <h1 className="max-w-2xl text-hero font-semibold sm:text-giant">
            {t("hero.title")}
          </h1>
          <p className="mx-auto max-w-2xl text-body-lg text-fg-secondary">
            {t("hero.desc")}
          </p>
          <p className="mx-auto max-w-2xl text-body-lg text-fg-tertiary">
            {t("hero.desc-2")}
          </p>

          <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
            <Button size="lg" variant="brand" asChild>
              <Link to="/register">
                <Bus /> {t("hero.cta-primary")}
                <ArrowUpRight />
              </Link>
            </Button>
            <Button size="lg" variant="ghost" asChild>
              <Link to="/login">{t("hero.cta-secondary")}</Link>
            </Button>
          </div>

          {/* Feature chips · 支持的模型 · 5 个 · 迁自 kiro-auto */}
          <div className="mt-6 flex flex-col items-center gap-2.5">
            <span className="text-label font-semibold uppercase tracking-wide text-fg-tertiary">
              {t("features.heading")}
            </span>
            <div className="flex flex-wrap justify-center gap-2">
              {["item-1", "item-2", "item-3", "item-4", "item-5"].map((k) => (
                <Chip key={k} tone="brand">{t(`features.${k}`)}</Chip>
              ))}
            </div>
          </div>
        </section>

        {/* 3 条 promo · 让访客看到"正在发生的" · 空则不显示 */}
        {promos?.items && promos.items.length > 0 && (
          <section className="space-y-3">
            <SectionEyebrow>{t("promo.eyebrow")}</SectionEyebrow>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              {promos.items.slice(0, 3).map((p) => (
                <Card key={p.id} className="flex items-start gap-3 p-5">
                  <span className="mt-0.5 grid size-7 shrink-0 place-items-center rounded-lg bg-brand-subtle">
                    <Sparkles className="size-3.5 text-brand-strong" />
                  </span>
                  <p className="text-label text-fg-secondary">{p.text}</p>
                </Card>
              ))}
            </div>
          </section>
        )}

        {/* 3 张 explainer 卡 · 什么·怎么·凭什么（借旧项目结构） */}
        <section className="grid gap-4 sm:grid-cols-3">
          {EXPLAINERS.map((e) => (
            <Explainer key={e.titleKey} icon={e.icon} title={t(e.titleKey)} body={t(e.bodyKey)} />
          ))}
        </section>

        {/* 4 大价值卡 · 品牌色焦点第一张 */}
        <section className="space-y-4">
          <SectionHeading
            eyebrow={t("values.eyebrow")}
            heading={t("values.heading")}
            subheading={t("values.subheading")}
          />
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {VALUES.map((v, i) => (
              <Card key={v.titleKey} focal={i === 0} focalTone={i === 0 ? "brand" : undefined} className="flex flex-col gap-3 p-6">
                <span className="grid size-10 place-items-center rounded-xl bg-brand-subtle">
                  <v.icon className="size-5 text-brand-strong" />
                </span>
                <div className="space-y-1">
                  <h3 className="font-semibold">{t(v.titleKey)}</h3>
                  <p className="text-label text-fg-tertiary">{t(v.descKey)}</p>
                </div>
              </Card>
            ))}
          </div>
        </section>

        {/* 3 步开始 */}
        <section className="space-y-4">
          <SectionHeading
            eyebrow={t("steps.eyebrow")}
            heading={t("steps.heading")}
            subheading={t("steps.subheading")}
          />
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            {STEPS.map((s, i) => (
              <Card key={s.titleKey} className="flex flex-col gap-3 p-6">
                <div className="flex items-center gap-3">
                  <span className="grid size-8 place-items-center rounded-full bg-bg-elevated text-label font-semibold text-fg-secondary">
                    {i + 1}
                  </span>
                  <h3 className="font-semibold">{t(s.titleKey)}</h3>
                </div>
                <p className="text-label text-fg-tertiary">{t(s.descKey)}</p>
              </Card>
            ))}
          </div>
        </section>

        {/* 社群入口 · 迁自旧项目 · 但改成 TG / Discord 双入口
            链接开放后填 · 现在灰态占位 */}
        <section className="space-y-4">
          <SectionHeading
            eyebrow={t("community.eyebrow")}
            heading={t("community.heading")}
            subheading={t("community.subheading")}
          />
          <div className="grid gap-3 sm:grid-cols-2">
            <CommunityLink
              logo={<TelegramLogo className="size-8" />}
              name="Telegram"
              desc={t("community.telegram-desc")}
            />
            <CommunityLink
              logo={<DiscordLogo className="size-8" />}
              name="Discord"
              desc={t("community.discord-desc")}
            />
          </div>
        </section>

        {/* CTA 收尾 · focal 卡强调 · 收束到"注册" */}
        <Card focal focalTone="brand" className="p-10 text-center sm:p-12">
          <div className="mx-auto max-w-[520px] space-y-4">
            <h2 className="text-section font-semibold sm:text-hero">{t("cta.heading")}</h2>
            <p className="text-fg-tertiary">{t("cta.subheading")}</p>
            <div className="flex flex-wrap items-center justify-center gap-2 pt-1">
              <Button size="lg" variant="brand" asChild>
                <Link to="/register">{t("cta.primary")}</Link>
              </Button>
              <Button size="lg" variant="ghost" asChild>
                <Link to="/login">{t("cta.secondary")}</Link>
              </Button>
            </div>
          </div>
        </Card>
      </main>

      <footer className="mt-section border-t border-hairline">
        <div className="page-container flex flex-col gap-3 py-8 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-2.5">
            <img src={LogoMark} alt="" className="size-6 shrink-0 rounded-md" />
            <span className="text-label font-semibold">bus-pooling</span>
            <span className="text-label text-fg-tertiary">· {t("footer.beta")}</span>
          </div>
          <p className="text-label text-fg-tertiary">{t("footer.tagline")}</p>
        </div>
      </footer>
    </div>
  );
}

/* ─── 局部组件 ─────────────────────────────────────────── */

function SectionEyebrow({ children }: { children: React.ReactNode }) {
  return (
    <span className="text-label font-semibold uppercase tracking-wide text-fg-tertiary">
      {children}
    </span>
  );
}

function SectionHeading({
  eyebrow, heading, subheading,
}: { eyebrow: string; heading: string; subheading: string }) {
  return (
    <div className="mx-auto max-w-2xl text-center space-y-2">
      <SectionEyebrow>{eyebrow}</SectionEyebrow>
      <h2 className="text-section font-semibold sm:text-hero">{heading}</h2>
      <p className="text-fg-tertiary">{subheading}</p>
    </div>
  );
}

function Explainer({ icon: Icon, title, body }: { icon: LucideIcon; title: string; body: string }) {
  return (
    <Card className="flex flex-col gap-3 p-6">
      <span className="grid size-11 place-items-center rounded-xl bg-brand-subtle">
        <Icon className="size-5 text-brand-strong" />
      </span>
      <h3 className="font-semibold">{title}</h3>
      <p className="text-label text-fg-tertiary">{body}</p>
    </Card>
  );
}

function CommunityLink({
  logo, name, desc,
}: { logo: React.ReactNode; name: string; desc: string }) {
  const { t } = useTranslation("landing");
  return (
    <Card className="flex items-center gap-4 p-5 opacity-70">
      <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-bg-elevated">
        {logo}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-semibold">{name}</span>
          <Chip tone="neutral">{t("community.coming-soon")}</Chip>
        </div>
        <p className="text-label text-fg-tertiary">{desc}</p>
      </div>
    </Card>
  );
}

/* ─── 内容 ─────────────────────────────────────────────── */

const EXPLAINERS: { icon: LucideIcon; titleKey: string; bodyKey: string }[] = [
  { icon: Bus, titleKey: "explainer.what.title", bodyKey: "explainer.what.body" },
  { icon: Activity, titleKey: "explainer.how.title", bodyKey: "explainer.how.body" },
  { icon: Shield, titleKey: "explainer.trust.title", bodyKey: "explainer.trust.body" },
];

const VALUES: { icon: LucideIcon; titleKey: string; descKey: string }[] = [
  { icon: Users, titleKey: "value.pool.title", descKey: "value.pool.desc" },
  { icon: Activity, titleKey: "value.refill.title", descKey: "value.refill.desc" },
  { icon: Database, titleKey: "value.push.title", descKey: "value.push.desc" },
  { icon: Shield, titleKey: "value.transparent.title", descKey: "value.transparent.desc" },
];

const STEPS: { titleKey: string; descKey: string }[] = [
  { titleKey: "step.register.title", descKey: "step.register.desc" },
  { titleKey: "step.topup.title", descKey: "step.topup.desc" },
  { titleKey: "step.pull.title", descKey: "step.pull.desc" },
];
