import type { LucideIcon } from "lucide-react";
import { useState } from "react";
import {
  Activity, ArrowRight, Boxes, Check, ChevronDown, Code, Download,
  Ghost, Info, MousePointer2, RadioTower, Receipt, Server, Sparkles,
  Terminal, Ticket, User, UsersRound, Wallet, Webhook, Zap,
} from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { AppFooter } from "@/components/AppFooter";
import { PromoBar } from "@/components/PromoBar";
import { PublicHeader } from "@/components/PublicHeader";
import { Button } from "@/components/ui/button";
import { Card, Chip } from "@/components/ui/primitives";
import {
  Collapsible, CollapsibleContent, CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Reveal } from "@/components/ui/reveal";
import { cn, TOPUP_PRESETS, topupUsdBreakdown } from "@/lib/utils";
import { DocumentMeta } from "@/components/DocumentMeta";
import busPng from "@/assets/marketing/bus.png";
import workPng from "@/assets/marketing/work.png";
import privacyPng from "@/assets/marketing/privacy.png";

/** Landing · 未登录访客首页 (mock v2 · 严格照 design/mockups/05-home.pen 的 "Landing · 落地页 v1")
 *
 *  共用件：PromoBar · AppFooter · PublicControls
 *  节奏（背景灰白 alternate）：
 *    hero (白) → features (灰) → value (白) → who (灰) → uses (白) → pricing (灰) → faq (白) → cta (品牌淡紫)
 *  Hero 右侧 3 张透视卡（rotate + 阴影 + 拖影 · hover 时轻回正 + 提升） */
export default function Landing() {
  // 顶层不直接用 t · 各 Section 子组件自己 useTranslation · 保留调用是为了
  // 确保 landing namespace 已经初始化再渲染子树（i18next 是懒加载）
  useTranslation("landing");

  // 锚点 label 走 landing namespace · 显式加前缀，PublicHeader 用 common
  // namespace 也能取到（i18next 支持 `<ns>:<key>` 语法）
  const anchors = [
    { id: "features", labelKey: "landing:nav.features" },
    { id: "value", labelKey: "landing:nav.value" },
    { id: "uses", labelKey: "landing:nav.uses" },
    { id: "pricing", labelKey: "landing:nav.pricing" },
    { id: "faq", labelKey: "landing:nav.faq" },
  ];

  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <DocumentMeta />
      <PromoBar />
      <PublicHeader anchors={anchors} />

      <main className="flex-1">
        <Hero />
        <FeaturesSection />
        <ValueSection />
        <WhoSection />
        <UsesSection />
        <PricingSection />
        <FaqSection />
        <FinalCta />
      </main>

      <AppFooter />
    </div>
  );
}

/* ─── Hero ─── */

function Hero() {
  const { t } = useTranslation("landing");
  return (
    <section className="page-container grid items-center gap-10 pb-14 pt-14 lg:grid-cols-[minmax(0,520fr)_minmax(0,720fr)] lg:gap-12 lg:pb-24 lg:pt-20">
      <Reveal>
        <div className="space-y-6">
          <Chip tone="brand">{t("hero.chip")}</Chip>
          <h1 className="text-hero font-semibold leading-[1.05] tracking-tight sm:text-giant">
            {t("hero.title1")}
            <br />
            <span className="text-brand-strong">{t("hero.title2")}</span>
          </h1>
          <p className="max-w-[46ch] text-body-lg leading-relaxed text-fg-secondary">
            {t("hero.subtitle")}
          </p>
          <div className="flex flex-wrap items-center gap-2 pt-1">
            <Button size="lg" variant="brand" asChild>
              <Link to="/register">
                {t("hero.cta")}
                <ArrowRight />
              </Link>
            </Button>
            <Button size="lg" variant="ghost" asChild>
              <Link to="/docs">{t("hero.docs")}</Link>
            </Button>
          </div>
        </div>
      </Reveal>

      <Reveal delay={140}>
        <HeroCards />
      </Reveal>
    </section>
  );
}

/** 3 张 hero 卡 · rotate + shadow + 拖影 · hover: 微上浮 + rotate 回正 + trail 更亮
 *  group + transition · 静态数字（登录后的真数据在 app 里 · 这里是产品语言的示意） */
/** 拖影公共样式 · 每张卡自带一个 · 跟卡同一个 wrapper · 卡挪到哪影子跟到哪 */
const HERO_TRAIL_CLASS =
  "pointer-events-none absolute -bottom-16 left-4 right-4 h-24 rounded-panel " +
  "bg-gradient-to-b from-brand/40 to-transparent opacity-40 blur-xl " +
  "transition-opacity duration-500 group-hover:opacity-60";

function HeroCards() {
  return (
    <div className="relative h-[500px] w-full lg:h-[560px]" style={{ transformStyle: "preserve-3d" }}>
      <VendorCard />
      <BusCard />
      <KpiCard />
    </div>
  );
}

function VendorCard() {
  const { t } = useTranslation("landing");
  const labels = t("heroCards.vendor.labels", { returnObjects: true }) as string[];
  const heights = [38, 44, 26, 32, 18, 22];
  const active = 1;
  return (
    <div
      className="group absolute right-[8px] top-0 w-[260px] origin-center transition-transform duration-300 hover:duration-500"
      style={{
        transform: "perspective(1400px) rotateY(-14deg) rotateX(4deg) rotateZ(5deg)",
        transformStyle: "preserve-3d",
      }}
      onMouseEnter={(e) => {
        (e.currentTarget as HTMLDivElement).style.transform =
          "perspective(1400px) rotateY(-8deg) rotateX(2deg) rotateZ(3deg) translateY(-6px)";
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLDivElement).style.transform =
          "perspective(1400px) rotateY(-14deg) rotateX(4deg) rotateZ(5deg)";
      }}
    >
      <div className={HERO_TRAIL_CLASS} />
      <div className="relative rounded-panel border border-hairline bg-bg p-4 shadow-hover">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <Boxes className="size-3.5 text-brand-strong" />
            <span className="text-label font-semibold tracking-wide text-fg-tertiary">
              {t("heroCards.vendor.title")}
            </span>
          </div>
          <div className="flex items-center gap-1">
            <span className="size-1.5 rounded-full bg-ok-solid" />
            <span className="font-mono text-[10px] font-semibold text-ok-solid">
              {t("heroCards.vendor.live")}
            </span>
          </div>
        </div>

        <div className="mt-3 flex items-end gap-1 h-12">
          {labels.map((_, i) => (
            <div
              key={i}
              className={cn(
                "flex-1 rounded-sm transition-colors",
                i === active ? "bg-brand" : "bg-brand/20",
              )}
              style={{ height: `${heights[i]}px` }}
            />
          ))}
        </div>
        <div className="mt-1 flex gap-1">
          {labels.map((l, i) => (
            <div key={i} className="flex flex-1 justify-center">
              <span
                className={cn(
                  "font-mono text-[9px] font-semibold",
                  i === active ? "text-brand-strong" : "text-fg-tertiary",
                )}
              >
                {l}
              </span>
            </div>
          ))}
        </div>

        <div className="mt-3 border-t border-hairline pt-2.5 flex items-center justify-between">
          <span className="text-label font-semibold text-fg">
            {t("heroCards.vendor.footL")}
          </span>
          <span className="font-mono text-[10px] font-semibold text-ok-fg">
            {t("heroCards.vendor.footR")}
          </span>
        </div>
      </div>
    </div>
  );
}

function BusCard() {
  const { t } = useTranslation("landing");
  return (
    <div
      className="group absolute left-0 top-[150px] w-[330px] origin-center transition-transform duration-300 hover:duration-500"
      style={{
        transform: "perspective(1400px) rotateY(12deg) rotateX(2deg) rotateZ(-3deg)",
        transformStyle: "preserve-3d",
      }}
      onMouseEnter={(e) => {
        (e.currentTarget as HTMLDivElement).style.transform =
          "perspective(1400px) rotateY(6deg) rotateX(1deg) rotateZ(-1deg) translateY(-8px)";
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLDivElement).style.transform =
          "perspective(1400px) rotateY(12deg) rotateX(2deg) rotateZ(-3deg)";
      }}
    >
      <div className={HERO_TRAIL_CLASS} />
      <div className="relative rounded-panel border border-hairline bg-bg p-5 shadow-hover">
        <div className="flex items-start justify-between">
          <Chip tone="brand">
            <UsersRound className="size-3" />
            {t("heroCards.bus.chip")}
          </Chip>
          <span className="grid size-8 place-items-center rounded-full bg-brand text-body-lg font-semibold text-white">
            Z
          </span>
        </div>
        <h3 className="mt-3 text-section font-semibold tracking-tight">
          {t("heroCards.bus.name")}
        </h3>
        <div className="mt-3 flex items-baseline justify-between">
          <div className="flex items-baseline gap-1.5">
            <span className="text-num font-semibold tnum">
              {t("heroCards.bus.count")}
            </span>
            <span className="text-label text-fg-tertiary">
              {t("heroCards.bus.countSuf")}
            </span>
          </div>
          <span className="font-mono text-label font-semibold text-danger-fg">
            {t("heroCards.bus.spend")}
          </span>
        </div>
        <div className="mt-2 flex items-center gap-1.5 text-label">
          <Zap className="size-3.5 text-brand-strong" />
          <span className="font-semibold text-brand-strong">
            {t("heroCards.bus.auto")}
          </span>
          <span className="text-fg-tertiary">{t("heroCards.bus.watermark")}</span>
        </div>
      </div>
    </div>
  );
}

function KpiCard() {
  const { t } = useTranslation("landing");
  const spk = [8, 11, 7, 13, 10, 16, 12, 18, 14, 20, 15, 22];
  return (
    <div
      className="group absolute right-0 top-[290px] w-[220px] origin-center transition-transform duration-300 hover:duration-500"
      style={{
        transform: "perspective(1400px) rotateY(-16deg) rotateX(-3deg) rotateZ(-4deg)",
        transformStyle: "preserve-3d",
      }}
      onMouseEnter={(e) => {
        (e.currentTarget as HTMLDivElement).style.transform =
          "perspective(1400px) rotateY(-10deg) rotateX(-1deg) rotateZ(-2deg) translateY(-8px)";
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLDivElement).style.transform =
          "perspective(1400px) rotateY(-16deg) rotateX(-3deg) rotateZ(-4deg)";
      }}
    >
      <div className={HERO_TRAIL_CLASS} />
      <div className="relative rounded-panel border border-hairline bg-bg p-4 shadow-hover">
        <div className="flex items-center gap-2">
          <span className="grid size-7 place-items-center rounded-lg bg-credit-bg">
            <Wallet className="size-3.5 text-credit-fg" />
          </span>
          <span className="text-label font-semibold tracking-wide text-fg-tertiary">
            {t("heroCards.kpi.label")}
          </span>
        </div>
        <div className="mt-2.5 flex items-baseline gap-1">
          <span className="text-num font-semibold tnum">{t("heroCards.kpi.n")}</span>
          <span className="text-label text-fg-tertiary">{t("heroCards.kpi.unit")}</span>
        </div>
        <div className="mt-2 flex h-6 items-end gap-0.5">
          {spk.map((h, i) => (
            <div
              key={i}
              className={cn(
                "flex-1 rounded-sm",
                i === spk.length - 1 ? "bg-credit-fg" : "bg-credit-bg",
              )}
              style={{ height: `${h}px` }}
            />
          ))}
        </div>
        <div className="mt-2 flex items-baseline justify-between">
          <span className="text-label font-medium text-fg-secondary">
            {t("heroCards.kpi.subL")}
          </span>
          <span className="font-mono text-[11px] font-semibold text-credit-fg">
            {t("heroCards.kpi.subR")}
          </span>
        </div>
      </div>
    </div>
  );
}

/* ─── §2 Features 共性 ─── */

function FeaturesSection() {
  const { t } = useTranslation("landing");
  const items = [
    { key: "simple", img: busPng },
    { key: "saving", img: workPng },
    { key: "safety", img: privacyPng },
  ];
  return (
    <section id="features" className="scroll-mt-20 border-y border-hairline bg-bg-elevated">
      <div className="page-container py-14 lg:py-20">
        <div className="grid gap-4 md:grid-cols-3">
          {items.map((it) => (
            <Reveal key={it.key}>
              <Card className="card-hover group flex h-full flex-col gap-5 p-8">
                {/* 图片 hover 时轻微上抬 + 放大 · 用 group-hover · overflow-hidden 让阴影残影别溢出 */}
                <div className="relative size-24 overflow-visible">
                  <img
                    src={it.img}
                    alt=""
                    className={cn(
                      "size-24 object-contain transition-transform duration-500 ease-out",
                      "group-hover:-translate-y-1 group-hover:-rotate-3 group-hover:scale-[1.06]",
                    )}
                  />
                </div>
                <h3 className="text-stat font-bold tracking-tight">
                  {t(`features.items.${it.key}.title`)}
                </h3>
                <p className="text-body leading-relaxed text-fg-secondary">
                  {t(`features.items.${it.key}.body`)}
                </p>
              </Card>
            </Reveal>
          ))}
        </div>
      </div>
    </section>
  );
}

/* ─── §3 Value · 3 场景卖点 ─── */

function ValueSection() {
  const { t } = useTranslation("landing");
  return (
    <section id="value" className="scroll-mt-20">
      <div className="page-container space-y-16 py-14 lg:space-y-24 lg:py-24">
        <Reveal>
          <div className="max-w-2xl space-y-2">
            <span className="font-mono text-label font-semibold uppercase tracking-widest text-fg-tertiary">
              {t("features.eyebrow")}
            </span>
            <h2 className="text-hero font-semibold tracking-tight">
              {t("features.title")}
            </h2>
          </div>
        </Reveal>

        <ValueBlock kind="one" reverse={false} visual={<VendorBoardVisual />} />
        <ValueBlock kind="two" reverse={true} visual={<PoolBillVisual />} />
        <ValueBlock kind="three" reverse={false} visual={<CliWebhookVisual />} />
      </div>
    </section>
  );
}

function ValueBlock({
  kind, reverse, visual,
}: { kind: "one" | "two" | "three"; reverse: boolean; visual: React.ReactNode }) {
  const { t } = useTranslation("landing");
  const points = t(`value.${kind}.points`, { returnObjects: true }) as string[];
  return (
    <Reveal>
      <div className={cn(
        "grid items-center gap-12 lg:grid-cols-2 lg:gap-16",
        reverse && "lg:[&>*:first-child]:order-2",
      )}>
        <div className="rounded-panel bg-brand/5 p-6 sm:p-8">{visual}</div>
        <div className="space-y-4">
          <Chip tone="brand">{t(`value.${kind}.tag`)}</Chip>
          <h3 className="text-section font-semibold tracking-tight sm:text-hero">
            {t(`value.${kind}.title`)}
          </h3>
          <p className="max-w-[52ch] leading-relaxed text-fg-secondary">
            {t(`value.${kind}.body`)}
          </p>
          <ul className="space-y-2 pt-2">
            {points.map((p) => (
              <li key={p} className="flex items-center gap-2 text-label">
                <Check className="size-3.5 shrink-0 text-brand-strong" />
                <span className="font-medium text-fg">{p}</span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </Reveal>
  );
}

/** §3-01 · vendor 监测卡 · 一张真实的 dashboard 卡 */
function VendorBoardVisual() {
  const { t } = useTranslation("landing");
  const vendors = [
    { n: "01", life: 82, lat: t("value.one.board.latency.low"), active: false, healthy: true },
    { n: "02", life: 94, lat: t("value.one.board.latency.low"), active: true, healthy: true },
    { n: "03", life: 71, lat: t("value.one.board.latency.mid"), active: false, healthy: true },
    { n: "04", life: 64, lat: t("value.one.board.latency.mid"), active: false, healthy: true },
    { n: "05", life: 38, lat: t("value.one.board.latency.high"), active: false, healthy: true },
    { n: "06", life: 22, lat: "—", active: false, healthy: false },
  ];
  return (
    <div className="mx-auto max-w-md rounded-panel border border-hairline bg-bg p-6 shadow-hover">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="grid size-7 place-items-center rounded-lg bg-brand/10">
            <RadioTower className="size-3.5 text-brand-strong" />
          </span>
          <span className="font-semibold">{t("value.one.board.title")}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="size-1.5 rounded-full bg-ok-solid" />
          <span className="font-mono text-[11px] font-semibold text-ok-solid">
            {t("value.one.board.live")}
          </span>
        </div>
      </div>
      <div className="my-3 h-px bg-hairline" />
      <ul className="space-y-2.5">
        {vendors.map((v) => (
          <li
            key={v.n}
            className={cn(
              "flex items-center gap-2.5 rounded-lg px-2 py-1.5",
              v.active && "bg-brand/10",
            )}
          >
            <span
              className={cn(
                "size-1.5 shrink-0 rounded-full",
                v.healthy ? "bg-ok-solid" : "bg-danger-solid",
              )}
            />
            <span
              className={cn(
                "font-mono text-label font-semibold",
                v.active ? "text-brand-strong" : "text-fg-secondary",
              )}
            >
              Vendor {v.n}
            </span>
            <div className="mx-1 h-1.5 flex-1 rounded-full bg-brand/10">
              <div
                className={cn(
                  "h-full rounded-full",
                  v.active
                    ? "bg-brand"
                    : v.healthy
                      ? "bg-brand/30"
                      : "bg-danger-solid",
                )}
                style={{ width: `${v.life}%` }}
              />
            </div>
            <span
              className={cn(
                "shrink-0 font-mono text-[11px] font-semibold",
                v.active ? "text-brand-strong" : "text-fg-tertiary",
              )}
            >
              {v.lat}
            </span>
            {v.active && <Activity className="size-3 shrink-0 text-brand-strong" />}
          </li>
        ))}
      </ul>
      <div className="my-3 h-px bg-hairline" />
      <div className="flex items-center justify-between text-label">
        <div className="flex items-center gap-1.5">
          <Activity className="size-3.5 text-brand-strong" />
          <span className="font-semibold">{t("value.one.board.footL")}</span>
        </div>
        <span className="text-fg-tertiary">{t("value.one.board.footR")}</span>
      </div>
    </div>
  );
}

/** §3-02 · 拼车 mini 卡 + 账单 mini 卡 */
function PoolBillVisual() {
  const { t } = useTranslation("landing");
  const billRows = t("value.two.bill.rows", { returnObjects: true }) as { k: string; v: string }[];
  return (
    <div className="mx-auto flex max-w-md flex-col items-center gap-4">
      <div className="w-full max-w-[280px] rounded-panel border border-hairline bg-bg p-4 shadow-card">
        <div className="flex items-center justify-between">
          <span className="font-semibold">{t("value.two.pool.name")}</span>
          <Chip tone="brand">{t("value.two.pool.chip")}</Chip>
        </div>
        <div className="mt-3 flex -space-x-1.5">
          {["Z", "L", "W", "C"].map((c, i) => (
            <span
              key={c}
              className="grid size-7 place-items-center rounded-full border-2 border-bg text-label font-semibold text-white"
              style={{
                background: ["#9147FF", "#6420C7", "#C9A9FF", "#A574FF"][i],
                zIndex: 4 - i,
              }}
            >
              {c}
            </span>
          ))}
        </div>
        <div className="mt-3 flex items-end justify-between rounded-lg bg-bg-elevated px-3 py-2.5">
          <span className="text-label font-medium text-fg-secondary">
            {t("value.two.pool.eachL")}
          </span>
          <div className="flex items-baseline gap-1">
            <span className="text-stat font-semibold tnum text-brand-strong">
              {t("value.two.pool.each")}
            </span>
            <span className="text-label text-fg-tertiary">
              {t("value.two.pool.eachU")}
            </span>
          </div>
        </div>
      </div>

      <div className="w-full max-w-[320px] rounded-panel border border-hairline bg-bg p-4 shadow-card">
        <div className="flex items-center justify-between">
          <span className="font-semibold">{t("value.two.bill.title")}</span>
          <span className="font-mono text-[11px] font-semibold text-credit-fg">
            {t("value.two.bill.save")}
          </span>
        </div>
        <ul className="mt-2 space-y-1">
          {billRows.map((r) => (
            <li key={r.k} className="flex items-center justify-between py-1 text-label">
              <span className="text-fg-tertiary">{r.k}</span>
              <span className="font-mono font-semibold">{r.v}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

/** §3-03 · curl 终端 · CLI 通吃 · webhook payload */
function CliWebhookVisual() {
  const { t } = useTranslation("landing");
  const clis: { key: string; label: string; icon: LucideIcon; color: string }[] = [
    { key: "claude", label: "Claude", icon: Sparkles, color: "#D97757" },
    { key: "cursor", label: "Cursor", icon: MousePointer2, color: "#000000" },
    { key: "kiro", label: "Kiro", icon: Ghost, color: "#9147FF" },
    { key: "codex", label: "codex", icon: Code, color: "#10A37F" },
    { key: "curl", label: "curl", icon: Terminal, color: "#073551" },
  ];
  return (
    <div className="mx-auto max-w-md space-y-3">
      <div className="rounded-panel bg-fg p-4">
        <div className="flex items-center gap-1.5">
          <span className="size-2 rounded-full bg-danger-solid/80" />
          <span className="size-2 rounded-full bg-warn-solid/80" />
          <span className="size-2 rounded-full bg-ok-solid/80" />
          <span className="ml-2 font-mono text-[10px] font-semibold uppercase text-bg/60">
            curl
          </span>
        </div>
        <pre className="mt-2 whitespace-pre-wrap font-mono text-[12px] leading-relaxed text-brand-light">
{'$ curl -H "X-API-Key: usr-..." \\\n    /api/me/pull'}
        </pre>
        <div className="mt-1 font-mono text-[12px] text-ok-solid">→ 5 keys pulled</div>
      </div>

      <div className="rounded-panel border border-hairline bg-bg p-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <Terminal className="size-3.5 text-brand-strong" />
            <span className="font-mono text-[11px] font-semibold tracking-wide">
              {t("value.three.cli.head")}
            </span>
          </div>
          <span className="font-mono text-[10px] text-fg-tertiary">
            {t("value.three.cli.note")}
          </span>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          {clis.map((c) => (
            <div
              key={c.key}
              className="flex items-center gap-1.5 rounded-full bg-bg-elevated px-2.5 py-1"
            >
              <span
                className="grid size-5 place-items-center rounded-full"
                style={{ background: c.color }}
              >
                <c.icon className="size-3 text-white" />
              </span>
              <span className="text-[11px] font-semibold">{c.label}</span>
            </div>
          ))}
          <span className="rounded-full border border-hairline px-2.5 py-1 text-[11px] font-medium text-fg-tertiary">
            {t("value.three.cli.more")}
          </span>
        </div>
      </div>

      <div className="rounded-panel border border-hairline bg-bg p-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <Webhook className="size-3.5 text-brand-strong" />
            <span className="font-mono text-[11px] font-semibold">
              {t("value.three.webhook.title")}
            </span>
          </div>
          <span className="font-mono text-[9px] font-semibold text-credit-fg">
            {t("value.three.webhook.method")}
          </span>
        </div>
        <pre className="mt-2 whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed text-fg-secondary">
          {t("value.three.webhook.body")}
        </pre>
      </div>
    </div>
  );
}

/* ─── §4 两类用户 ─── */

function WhoSection() {
  const { t } = useTranslation("landing");
  const cards = [
    { key: "casual", icon: User },
    { key: "power", icon: Server },
  ];
  return (
    <section className="scroll-mt-20 border-y border-hairline bg-bg-elevated">
      <div className="page-container py-14 lg:py-20">
        <Reveal>
          <div className="max-w-2xl space-y-2">
            <h2 className="text-hero font-semibold tracking-tight">
              {t("who.title")}
            </h2>
            <p className="text-fg-secondary">{t("who.body")}</p>
          </div>
        </Reveal>
        <div className="mt-9 grid gap-4 md:grid-cols-2">
          {cards.map((c) => (
            <Reveal key={c.key}>
              <Card className="flex h-full flex-col gap-3 p-7">
                <div className="flex items-center gap-2">
                  <span className="grid size-8 place-items-center rounded-lg bg-brand/10">
                    <c.icon className="size-4 text-brand-strong" />
                  </span>
                  <span className="text-label font-semibold text-fg-tertiary">
                    {t(`who.${c.key}.chip`)}
                  </span>
                </div>
                <h3 className="text-stat font-bold tracking-tight">
                  {t(`who.${c.key}.title`)}
                </h3>
                <p className="text-body leading-relaxed text-fg-secondary">
                  {t(`who.${c.key}.body`)}
                </p>
              </Card>
            </Reveal>
          ))}
        </div>
      </div>
    </section>
  );
}

/* ─── §5 三种使用方式 ─── */

function UsesSection() {
  const { t } = useTranslation("landing");
  const cells: { key: string; icon: LucideIcon; focal: boolean }[] = [
    { key: "hitch", icon: UsersRound, focal: true },
    { key: "friend", icon: Ticket, focal: false },
    { key: "extract", icon: Download, focal: false },
  ];
  return (
    <section id="uses" className="scroll-mt-20">
      <div className="page-container py-14 lg:py-24">
        <Reveal>
          <h2 className="text-hero font-semibold tracking-tight">
            {t("uses.title")}
          </h2>
        </Reveal>
        <div className="mt-9 grid overflow-hidden rounded-panel border border-hairline md:grid-cols-3">
          {cells.map((c, i) => {
            const points = t(`uses.${c.key}.points`, { returnObjects: true }) as string[];
            return (
              <Reveal key={c.key}>
                <div
                  className={cn(
                    "flex h-full flex-col gap-3.5 p-7",
                    c.focal ? "bg-brand/5" : "bg-bg",
                    i < cells.length - 1 && "md:border-r md:border-hairline",
                    i < cells.length - 1 && "border-b border-hairline md:border-b-0",
                  )}
                >
                  <div className="flex items-center gap-2">
                    <span
                      className={cn(
                        "grid size-10 place-items-center rounded-xl",
                        c.focal ? "bg-brand" : "bg-bg-elevated",
                      )}
                    >
                      <c.icon
                        className={cn(
                          "size-5",
                          c.focal ? "text-white" : "text-fg-secondary",
                        )}
                      />
                    </span>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="text-stat font-bold tracking-tight">
                      {t(`uses.${c.key}.title`)}
                    </h3>
                    <span
                      className={cn(
                        "rounded-full px-2 py-0.5 text-[11px] font-semibold",
                        c.focal
                          ? "bg-brand text-white"
                          : "bg-bg-elevated text-fg-secondary",
                      )}
                    >
                      {t(`uses.${c.key}.badge`)}
                    </span>
                  </div>
                  <p className="text-body leading-relaxed text-fg-secondary">
                    {t(`uses.${c.key}.body`)}
                  </p>
                  <ul className="space-y-1.5 pt-1">
                    {points.map((p) => (
                      <li key={p} className="flex items-center gap-2 text-body">
                        <Check className="size-3.5 shrink-0 text-brand-strong" />
                        <span className="font-medium text-fg-secondary">{p}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              </Reveal>
            );
          })}
        </div>
      </div>
    </section>
  );
}

/* ─── §5.5 定价 · 充值 ─── */

function PricingSection() {
  const { t } = useTranslation("landing");
  const rules: { key: string; icon: LucideIcon }[] = [
    { key: "clear", icon: Info },
    { key: "pool", icon: Receipt },
    { key: "gateway", icon: Wallet },
  ];
  // 默认 100 积分 · TOPUP_PRESETS[1] · 汇率 / 通道费全走 lib/utils.ts
  const [selected, setSelected] = useState<number>(TOPUP_PRESETS[1]);
  const unit = t("pricing.topup.creditUnit");
  const { usdCredits, usdFee, usdTotal } = topupUsdBreakdown(selected);
  const fmt = (n: number) => `$${n.toFixed(2)}`;
  const totalDisplay = Number.isInteger(usdTotal) ? `$${usdTotal}` : fmt(usdTotal);
  const rows: { k: string; v: string }[] = [
    { k: t("pricing.topup.rowLabels.credits"), v: `${selected} ${unit} · ${fmt(usdCredits)}` },
    { k: t("pricing.topup.rowLabels.fee"), v: `+${fmt(usdFee)}` },
    { k: t("pricing.topup.rowLabels.arrives"), v: `${selected} ${unit}` },
  ];

  return (
    <section id="pricing" className="scroll-mt-20 border-y border-hairline bg-bg-elevated">
      <div className="page-container grid gap-12 py-14 lg:grid-cols-[minmax(0,1fr)_400px] lg:gap-16 lg:py-20">
        <Reveal>
          <div className="space-y-6">
            <div className="space-y-2">
              <span className="font-mono text-label font-semibold uppercase tracking-widest text-fg-tertiary">
                {t("pricing.eyebrow")}
              </span>
              <h2 className="text-hero font-semibold tracking-tight">
                {t("pricing.title")}
              </h2>
              <p className="max-w-[52ch] leading-relaxed text-fg-secondary">
                {t("pricing.body")}
              </p>
            </div>
            <ul className="space-y-4">
              {rules.map((r) => (
                <li key={r.key} className="flex items-start gap-3">
                  <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-brand/10">
                    <r.icon className="size-4 text-brand-strong" />
                  </span>
                  <div>
                    <h4 className="font-semibold">
                      {t(`pricing.rules.${r.key}.title`)}
                    </h4>
                    <p className="mt-0.5 max-w-[52ch] text-label leading-relaxed text-fg-secondary">
                      {t(`pricing.rules.${r.key}.body`)}
                    </p>
                  </div>
                </li>
              ))}
            </ul>
            <div className="flex items-center gap-2 rounded-lg bg-bg px-3 py-2.5 text-label text-fg-tertiary">
              <Info className="size-3.5 shrink-0" />
              <span>{t("pricing.note")}</span>
            </div>
          </div>
        </Reveal>

        <Reveal delay={80}>
          <div className="rounded-panel border border-hairline bg-bg p-6 shadow-hover">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <span className="grid size-8 place-items-center rounded-lg bg-credit-bg">
                  <Wallet className="size-4 text-credit-fg" />
                </span>
                <span className="text-body-lg font-semibold">
                  {t("pricing.topup.title")}
                </span>
              </div>
              <span className="rounded-full bg-credit-bg px-2 py-0.5 text-[10px] font-semibold text-credit-fg">
                {t("pricing.topup.badge")}
              </span>
            </div>

            <div className="mt-5 text-label font-semibold uppercase tracking-widest text-fg-tertiary">
              {t("pricing.topup.amountLabel")}
            </div>
            <div className="mt-2 grid grid-cols-4 gap-2">
              {TOPUP_PRESETS.map((n) => (
                <button
                  key={n}
                  type="button"
                  onClick={() => setSelected(n)}
                  className={cn(
                    "rounded-lg border px-2 py-2 text-label font-semibold transition-colors",
                    selected === n
                      ? "border-brand bg-brand/10 text-brand-strong"
                      : "border-hairline text-fg-secondary hover:bg-bg-elevated",
                  )}
                >
                  {n}
                </button>
              ))}
            </div>

            <ul className="mt-5 space-y-2 rounded-xl bg-bg-elevated p-4">
              {rows.map((r) => (
                <li key={r.k} className="flex items-center justify-between text-label">
                  <span className="text-fg-tertiary">{r.k}</span>
                  <span className="font-mono font-semibold">{r.v}</span>
                </li>
              ))}
              <li className="my-1 h-px bg-hairline" />
              <li className="flex items-end justify-between">
                <span className="font-semibold">{t("pricing.topup.totalL")}</span>
                <div className="flex items-baseline gap-1">
                  <span className="text-stat font-semibold text-brand-strong tnum">
                    {totalDisplay}
                  </span>
                  <span className="font-mono text-[11px] text-fg-tertiary">
                    {t("pricing.topup.totalU")}
                  </span>
                </div>
              </li>
            </ul>

            <Button size="lg" variant="brand" className="mt-5 w-full" asChild>
              <Link to="/wallet">
                {t("pricing.topup.cta")}
                <ArrowRight />
              </Link>
            </Button>
          </div>
        </Reveal>
      </div>
    </section>
  );
}

/* ─── §6 FAQ ─── */

function FaqSection() {
  const { t } = useTranslation("landing");
  const items = t("faq.items", { returnObjects: true }) as { q: string; a: string; open?: boolean }[];
  return (
    <section id="faq" className="scroll-mt-20">
      <div className="page-container py-14 lg:py-20">
        <Reveal>
          <h2 className="text-hero font-semibold tracking-tight">
            {t("faq.title")}
          </h2>
        </Reveal>
        <div className="mt-9 max-w-3xl divide-y divide-hairline border-t border-hairline">
          {items.map((it, i) => (
            <Reveal key={i} delay={i * 40}>
              <Collapsible defaultOpen={i === 0}>
                <CollapsibleTrigger className="group flex w-full items-center justify-between gap-4 py-4 text-left font-semibold transition-colors hover:text-fg-secondary focus:outline-none focus-visible:text-fg-secondary">
                  <span>{it.q}</span>
                  <ChevronDown className="size-4 shrink-0 text-fg-tertiary transition-transform group-data-[state=open]:rotate-180" />
                </CollapsibleTrigger>
                <CollapsibleContent className="overflow-hidden data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:slide-out-to-top-2 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:slide-in-from-top-2">
                  <p className="max-w-[62ch] whitespace-pre-line pb-4 leading-relaxed text-fg-secondary">
                    {it.a}
                  </p>
                </CollapsibleContent>
              </Collapsible>
            </Reveal>
          ))}
        </div>
      </div>
    </section>
  );
}

/* ─── §7 最终 CTA ─── */

function FinalCta() {
  const { t } = useTranslation("landing");
  return (
    <section className="page-container pb-16 lg:pb-24 pt-14">
      <Reveal>
        <Card focal focalTone="brand" className="p-10 text-center sm:p-14">
          <div className="mx-auto max-w-[52ch] space-y-4">
            <h2 className="text-hero font-semibold tracking-tight sm:text-giant">
              {t("cta.title")}
            </h2>
            <p className="text-fg-secondary">{t("cta.body")}</p>
            <div className="flex flex-wrap items-center justify-center gap-2 pt-1">
              <Button size="lg" variant="brand" asChild>
                <Link to="/register">
                  {t("cta.primary")}
                  <ArrowRight />
                </Link>
              </Button>
              <Button size="lg" variant="ghost" asChild>
                <Link to="/login">{t("cta.secondary")}</Link>
              </Button>
            </div>
          </div>
        </Card>
      </Reveal>
    </section>
  );
}




