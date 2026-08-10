import type { LucideIcon } from "lucide-react";
import {
  ArrowRight, Boxes, ChevronDown, Database, GitCompareArrows, Layers,
  RefreshCw, Users, Webhook,
} from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { AppFooter } from "@/components/AppFooter";
import { PromoBar } from "@/components/PromoBar";
import { PublicControls } from "@/components/PublicControls";
import { Button } from "@/components/ui/button";
import { CodeBlock } from "@/components/ui/code-block";
import {
  Collapsible, CollapsibleContent, CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Card, Chip, Meter } from "@/components/ui/primitives";
import { Reveal } from "@/components/ui/reveal";
import LogoMark from "@/assets/logo/mark.svg";

/** Landing · 未登录访客首页 · RootGate 判 me 为空时渲染
 *
 *  跟 app 内页共用：PromoBar（顶部活动条）· AppFooter（页脚）· PublicControls（语言 / 主题）
 *  header 结构和 class 抄 AppLayout 的（h-14 · sticky · backdrop-blur）· 两个界面读起来是同一个产品
 *
 *  内容主线（README + docs/00 §2）：号价按人头平分 → 拼车集单吃阶梯价 → webhook / API 自动化
 *  不出现内部术语（housepool / provider / tier 枚举）· CLAUDE.md §12.6 */
export default function Landing() {
  const { t } = useTranslation("landing");

  const anchors = [
    { id: "save", label: t("nav.save") },
    { id: "pool", label: t("nav.pool") },
    { id: "auto", label: t("nav.auto") },
    { id: "faq", label: t("nav.faq") },
  ];

  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <PromoBar />

      {/* header · 跟 AppLayout 同款（h-14 · sticky · blur）· 中间是锚点导航 */}
      <header className="sticky top-0 z-30 border-b border-hairline bg-bg/85 backdrop-blur-xl">
        <div className="page-container flex h-14 items-center justify-between gap-4">
          <Link to="/" className="flex shrink-0 items-center gap-2.5">
            <img src={LogoMark} alt="" className="size-7 shrink-0 rounded-lg" />
            <span className="text-body-lg font-semibold tracking-tight">bus-pooling</span>
          </Link>

          {/* 锚点 · md 起显示（窄屏藏掉，避免挤成两行） */}
          <nav className="hidden items-center gap-1 md:flex">
            {anchors.map((a) => (
              <a
                key={a.id}
                href={`#${a.id}`}
                className="rounded-full px-3 py-1.5 font-medium text-fg-secondary transition-colors hover:bg-bg-elevated hover:text-fg"
              >
                {a.label}
              </a>
            ))}
          </nav>

          <div className="flex shrink-0 items-center gap-2">
            <PublicControls />
            <div className="mx-1 hidden h-6 w-px bg-hairline sm:block" />
            <Button variant="ghost" size="sm" asChild>
              <Link to="/login">{t("nav.login")}</Link>
            </Button>
            <Button variant="primary" size="sm" asChild>
              <Link to="/register">{t("nav.signup")}</Link>
            </Button>
          </div>
        </div>
      </header>

      <main className="flex-1">
        <Hero />
        <SaveBand />
        <PoolGrid />
        <AutoSection />
        <DestStrip />
        <Faq />
        <FinalCta />
      </main>

      <AppFooter />
    </div>
  );
}

/* ─── Hero · 非对称分栏（左文案 / 右分账图）──────────────────── */

function Hero() {
  const { t } = useTranslation("landing");
  return (
    <section className="page-container grid items-center gap-10 pb-12 pt-14 lg:grid-cols-[1.05fr_1fr] lg:gap-16 lg:pb-20 lg:pt-20">
      <Reveal>
        <div className="space-y-5">
          <Chip tone="brand">{t("hero.chip")}</Chip>
          <h1 className="text-hero font-semibold tracking-tight sm:text-giant">
            {t("hero.title")}
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

      {/* 分账图 · 用产品自己的 Card / Chip 拼，不画假截图 */}
      <Reveal delay={120}>
        <SplitDiagram />
      </Reveal>
    </section>
  );
}

/** 号价分账示意 · 一把 key 的号价 ÷ 人数
 *  数字来自 README 的举例（20 积分 4 人）· 卡里标了「示例」 */
function SplitDiagram() {
  const { t } = useTranslation("landing");
  const riders = ["Z", "L", "W", "C"];
  return (
    <Card focal focalTone="brand" className="p-7">
      <div className="flex items-baseline justify-between gap-4">
        <span className="text-label font-medium text-fg-tertiary">
          {t("heroCard.label")}
        </span>
        <div className="flex items-end gap-1">
          <span className="text-num font-semibold tnum">{t("heroCard.cost")}</span>
          <span className="pb-1 text-label text-fg-tertiary">{t("heroCard.unit")}</span>
        </div>
      </div>

      {/* 4 个头像 = 车上的人 · 除号在中间 */}
      <div className="mt-6 flex items-center gap-3 border-t border-hairline pt-6">
        <div className="flex -space-x-2">
          {riders.map((r, i) => (
            <span
              key={r}
              className="grid size-8 place-items-center rounded-full border-2 border-bg bg-brand/15 text-label font-semibold text-brand-strong"
              style={{ zIndex: riders.length - i }}
            >
              {r}
            </span>
          ))}
        </div>
        <span className="text-label font-medium text-fg-secondary">
          {t("heroCard.riders")}
        </span>
      </div>

      <div className="mt-6 flex items-end justify-between gap-4 rounded-xl bg-bg-elevated px-4 py-3.5">
        <span className="text-label font-semibold text-fg-secondary">
          {t("heroCard.eachLabel")}
        </span>
        <div className="flex items-end gap-1">
          <span className="text-stat font-semibold tnum text-brand-strong">
            {t("heroCard.each")}
          </span>
          <span className="pb-0.5 text-label text-fg-tertiary">{t("heroCard.unit")}</span>
        </div>
      </div>

      <p className="mt-4 text-label text-fg-tertiary">{t("heroCard.note")}</p>
    </Card>
  );
}

/* ─── 省钱曲线 · 递进刻度条（人数越多单价越低）──────────────── */

const SPLITS = [
  { n: 1, each: 20 },
  { n: 2, each: 10 },
  { n: 4, each: 5 },
  { n: 8, each: 2.5 },
];

function SaveBand() {
  const { t } = useTranslation("landing");
  return (
    <section id="save" className="scroll-mt-20 border-y border-hairline bg-bg-elevated">
      <div className="page-container py-14 lg:py-20">
        <Reveal>
          <div className="max-w-2xl space-y-2">
            <h2 className="text-section font-semibold sm:text-hero">{t("save.title")}</h2>
            <p className="text-fg-secondary">{t("save.body")}</p>
          </div>
        </Reveal>

        {/* 4 档人数 · 条长 = 每人单价 · 越往下越短
            **不画背景轨道** —— 带底槽的进度条是仪表盘语言，落地页上只要看出"谁短谁便宜"
            条形宽度用百分比，不用 Meter（那个自带 bg-hairline 轨道） */}
        <div className="mt-9 space-y-4">
          {SPLITS.map((s, i) => {
            const last = i === SPLITS.length - 1;
            return (
              <Reveal key={s.n} delay={i * 70}>
                <div className="flex items-center gap-4 sm:gap-6">
                  <span className="w-14 shrink-0 text-label font-semibold text-fg-secondary sm:w-20">
                    {t("save.people", { n: s.n })}
                  </span>
                  <span className="flex-1">
                    <span
                      className="block h-2 rounded-full transition-[width] duration-700 ease-out motion-reduce:transition-none"
                      style={{
                        width: `${(s.each / SPLITS[0].each) * 100}%`,
                        backgroundColor: last ? "#9147FF" : "#C9A9FF",
                      }}
                    />
                  </span>
                  <span className="w-20 shrink-0 text-right sm:w-28">
                    <span
                      className={`text-body-lg font-semibold tnum ${last ? "text-brand-strong" : ""}`}
                    >
                      {s.each}
                    </span>
                    <span className="ml-1 text-label text-fg-tertiary">{t("save.per")}</span>
                  </span>
                </div>
              </Reveal>
            );
          })}
        </div>

        <p className="mt-6 text-label text-fg-tertiary">{t("save.note")}</p>
      </div>
    </section>
  );
}

/* ─── 拼车 · bento（1 大 + 3 小 · 4 内容 4 格）───────────────── */

function PoolGrid() {
  const { t } = useTranslation("landing");
  return (
    <section id="pool" className="page-container scroll-mt-20 py-14 lg:py-20">
      <Reveal>
        <div className="max-w-2xl space-y-2">
          <h2 className="text-section font-semibold sm:text-hero">{t("pool.title")}</h2>
          <p className="text-fg-secondary">{t("pool.body")}</p>
        </div>
      </Reveal>

      <div className="mt-9 grid gap-4 md:grid-cols-3">
        {/* 大格 · 集单 · 品牌光晕 */}
        <Reveal className="md:col-span-2">
          <Card focal focalTone="brand" className="flex h-full flex-col gap-4 p-7">
            <BentoHead icon={Layers} title={t("pool.batch.title")} />
            <p className="max-w-[52ch] text-fg-secondary">{t("pool.batch.body")}</p>
            {/* 合流示意 · 3 个意图 → 1 次请求 */}
            <div className="mt-auto flex items-center gap-3 pt-2">
              <div className="flex flex-col gap-1.5">
                {[0, 1, 2].map((k) => (
                  <span key={k} className="h-1.5 w-10 rounded-full bg-brand/25" />
                ))}
              </div>
              <ArrowRight className="size-4 shrink-0 text-fg-tertiary" />
              <span className="h-1.5 w-20 rounded-full bg-brand" />
              <Chip tone="brand">{t("pool.batch.tier")}</Chip>
            </div>
          </Card>
        </Reveal>

        {/* 小格 · 6 家上游 · 匿名编号（对外不出真名） */}
        <Reveal delay={70}>
          <Card className="flex h-full flex-col gap-4 p-7">
            <BentoHead icon={Boxes} title={t("pool.vendors.title")} />
            <p className="text-label text-fg-secondary">{t("pool.vendors.body")}</p>
            <div className="mt-auto flex flex-wrap gap-1.5 pt-2">
              {[1, 2, 3, 4, 5, 6].map((n) => (
                <Chip key={n} tone="neutral">{`0${n}`}</Chip>
              ))}
            </div>
          </Card>
        </Reveal>

        {/* 小格 · 比有效成本 · 两根对比条 */}
        <Reveal delay={140}>
          <Card className="flex h-full flex-col gap-4 p-7">
            <BentoHead icon={GitCompareArrows} title={t("pool.compare.title")} />
            <p className="text-label text-fg-secondary">{t("pool.compare.body")}</p>
            {/* 两根对比条 · 同样不画底槽（跟 #save 一致）
                长的那根 = 有效成本更优的那家 · 只表达"能比出高低"，不给具体数（那是登录后的事） */}
            <div className="mt-auto space-y-2 pt-2">
              <span className="block h-1.5 w-[78%] rounded-full bg-brand" />
              <span className="block h-1.5 w-[41%] rounded-full bg-brand-soft" />
            </div>
          </Card>
        </Reveal>

        {/* 小格 · 自动补号 · 灰底做视觉变化 */}
        <Reveal delay={210} className="md:col-span-2">
          <Card className="flex h-full flex-col gap-4 bg-bg-elevated p-7">
            <BentoHead icon={RefreshCw} title={t("pool.refill.title")} />
            <p className="max-w-[52ch] text-fg-secondary">{t("pool.refill.body")}</p>
            <div className="mt-auto flex flex-wrap items-center gap-2 pt-2">
              <Chip tone="danger">{t("pool.refill.dead")}</Chip>
              <ArrowRight className="size-4 shrink-0 text-fg-tertiary" />
              <Chip tone="ok">{t("pool.refill.new")}</Chip>
              <ArrowRight className="size-4 shrink-0 text-fg-tertiary" />
              <Chip tone="brand">{t("pool.refill.credit")}</Chip>
            </div>
          </Card>
        </Reveal>
      </div>
    </section>
  );
}

function BentoHead({ icon: Icon, title }: { icon: LucideIcon; title: string }) {
  return (
    <div className="flex items-center gap-2.5">
      <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-bg-elevated">
        <Icon className="size-4 text-fg-secondary" />
      </span>
      <h3 className="font-semibold">{title}</h3>
    </div>
  );
}

/* ─── 自动化 · 竖向堆叠（标题 → 事件条 → 代码）───────────────── */

const PAYLOAD = `{
  "event": "new_keys_available",
  "bus_id": "01H8...",
  "new_keys": 5,
  "timestamp": "2026-08-07T09:12:44Z"
}`;

function AutoSection() {
  const { t } = useTranslation("landing");
  const events = [
    { key: "new", tone: "ok" as const },
    { key: "dead", tone: "danger" as const },
    { key: "refund", tone: "brand" as const },
    { key: "test", tone: "neutral" as const },
  ];
  return (
    <section id="auto" className="scroll-mt-20 border-y border-hairline bg-bg-elevated">
      <div className="page-container py-14 lg:py-20">
        <Reveal>
          <div className="max-w-2xl space-y-2">
            <div className="flex items-center gap-2.5">
              <Webhook className="size-5 text-brand-strong" />
              <h2 className="text-section font-semibold sm:text-hero">{t("auto.title")}</h2>
            </div>
            <p className="text-fg-secondary">{t("auto.body")}</p>
          </div>
        </Reveal>

        <Reveal delay={70}>
          <div className="mt-7 flex flex-wrap gap-2">
            {events.map((e) => (
              <Chip key={e.key} tone={e.tone} dot>
                {t(`auto.events.${e.key}`)}
              </Chip>
            ))}
          </div>
        </Reveal>

        <Reveal delay={140}>
          <div className="mt-6 grid gap-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
            <CodeBlock code={PAYLOAD} lang={t("auto.payload")} />
            <p className="max-w-[34ch] text-label leading-relaxed text-fg-secondary">
              {t("auto.apikey")}
            </p>
          </div>
        </Reveal>
      </div>
    </section>
  );
}

/* ─── 三种去向 · 一条分栏带（不是三张卡）────────────────────── */

function DestStrip() {
  const { t } = useTranslation("landing");
  const dests = [
    { key: "bus", icon: Users },
    { key: "pool", icon: Database },
    { key: "handoff", icon: ArrowRight },
  ];
  return (
    <section className="page-container py-14 lg:py-20">
      <Reveal>
        <h2 className="max-w-2xl text-section font-semibold sm:text-hero">
          {t("dest.title")}
        </h2>
      </Reveal>

      <Reveal delay={70}>
        <div className="mt-7 overflow-hidden rounded-panel border border-hairline">
          <div className="grid divide-y divide-hairline sm:grid-cols-3 sm:divide-x sm:divide-y-0">
            {dests.map((d, i) => (
              <div
                key={d.key}
                /* 第一格是主入口 · 给灰底做非对称强调 */
                className={i === 0 ? "space-y-2 bg-bg-elevated p-7" : "space-y-2 p-7"}
              >
                <d.icon className="size-4 text-fg-tertiary" />
                <h3 className="font-semibold">{t(`dest.${d.key}.title`)}</h3>
                <p className="text-label leading-relaxed text-fg-tertiary">
                  {t(`dest.${d.key}.body`)}
                </p>
              </div>
            ))}
          </div>
        </div>
      </Reveal>
    </section>
  );
}

/* ─── FAQ · 手风琴（5 条 · 不用裸 ul）───────────────────────── */

function Faq() {
  const { t } = useTranslation("landing");
  const qs = ["q1", "q2", "q3", "q4", "q5"];
  return (
    <section id="faq" className="scroll-mt-20 border-t border-hairline">
      <div className="page-container py-14 lg:py-20">
        <Reveal>
          <h2 className="max-w-2xl text-section font-semibold sm:text-hero">
            {t("faq.title")}
          </h2>
        </Reveal>

        <div className="mt-7 max-w-3xl divide-y divide-hairline border-t border-hairline">
          {qs.map((q, i) => (
            <Reveal key={q} delay={i * 50}>
              <Collapsible>
                {/* chevron 旋转跟项目 CollapsiblePanel 一个套路（group-data-[state=open]）
                    这里不用 Panel 本体 —— 它自带 border + rounded 卡壳，FAQ 要的是通栏 divide-y */}
                <CollapsibleTrigger className="group flex w-full items-center justify-between gap-4 py-4 text-left font-semibold transition-colors hover:text-fg-secondary focus:outline-none focus-visible:text-fg-secondary">
                  <span>{t(`faq.${q}.q`)}</span>
                  <ChevronDown className="size-4 shrink-0 text-fg-tertiary transition-transform group-data-[state=open]:rotate-180" />
                </CollapsibleTrigger>
                <CollapsibleContent
                  className="overflow-hidden data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:slide-out-to-top-2 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:slide-in-from-top-2"
                >
                  <p className="max-w-[62ch] pb-4 leading-relaxed text-fg-secondary">
                    {t(`faq.${q}.a`)}
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

/* ─── 收尾 CTA · 居中 focal 卡（整页唯一一次居中）─────────────── */

function FinalCta() {
  const { t } = useTranslation("landing");
  return (
    <section className="page-container pb-16 lg:pb-24">
      <Reveal>
        <Card focal focalTone="brand" className="p-10 text-center sm:p-14">
          <div className="mx-auto max-w-[46ch] space-y-4">
            <h2 className="text-section font-semibold sm:text-hero">{t("cta.title")}</h2>
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
