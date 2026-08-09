import type { LucideIcon } from "lucide-react";
import {
  Activity, ArrowUpRight, Bus, Database, Shield, Sparkles, Users,
} from "lucide-react";
import { Link } from "react-router-dom";
import { usePromos } from "@/api/hooks";
import { DiscordLogo, TelegramLogo } from "@/components/ui/brand-icons";
import { Button } from "@/components/ui/button";
import { Card, Chip } from "@/components/ui/primitives";
import LogoMark from "@/assets/logo/mark.svg";

/** Landing · 未登录访客首页（迁自 kiro-auto MarketingPage · 迁移时删掉套餐 / 价格 · 换 bus-pooling 组件系统）
 *  已登录用户由 RootGate 直接跳 /overview · 这里只面向未登录
 *  内容不放具体价格 / 号池细节 · CLAUDE.md §0.1 不许暴露加价幅度 */
export default function Landing() {
  const { data: promos } = usePromos();

  return (
    <div className="min-h-dvh bg-bg text-fg">
      {/* 极简公共 header · 不用 AppLayout（那个假设已登录） */}
      <header className="border-b border-hairline bg-bg/85 backdrop-blur-xl">
        <div className="page-container flex h-14 items-center justify-between">
          <Link to="/" className="flex items-center gap-2.5">
            <img src={LogoMark} alt="" className="size-7 shrink-0 rounded-lg" />
            <span className="text-body-lg font-semibold tracking-tight">bus-pooling</span>
          </Link>
          <div className="flex items-center gap-2">
            <Button variant="ghost" asChild>
              <Link to="/login">登录</Link>
            </Button>
            <Button variant="primary" asChild>
              <Link to="/register">注册</Link>
            </Button>
          </div>
        </div>
      </header>

      <main className="page-container space-y-section py-12 sm:py-16">
        {/* Hero */}
        <section className="grid place-items-center gap-6 text-center">
          <Chip tone="brand" icon={<Sparkles className="size-3" />}>公测中</Chip>
          <h1 className="text-hero font-semibold sm:text-giant">
            一起拼车 · 号更便宜
          </h1>
          <p className="max-w-[560px] text-body-lg text-fg-tertiary">
            找不到同路人？我们撮合。号会死？我们盯着自动补。想放自己号池？一键推。
          </p>
          <div className="flex flex-wrap items-center justify-center gap-2 pt-2">
            <Button size="lg" variant="brand" asChild>
              <Link to="/register">
                <Bus /> 立即注册
                <ArrowUpRight />
              </Link>
            </Button>
            <Button size="lg" variant="ghost" asChild>
              <Link to="/login">已有账号</Link>
            </Button>
          </div>
        </section>

        {/* 3 条 promo · 让访客看到"正在发生的" · 空则不显示 */}
        {promos?.items && promos.items.length > 0 && (
          <section className="space-y-3">
            <SectionEyebrow>正在进行</SectionEyebrow>
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
            <Explainer key={e.title} {...e} />
          ))}
        </section>

        {/* 4 大价值卡 · 品牌色焦点第一张 */}
        <section className="space-y-4">
          <SectionHeading
            eyebrow="为什么用 bus-pooling"
            heading="拼一辆车，什么都省了"
            subheading="车主盯号池、盯补车、盯结算 · 你只管拉号"
          />
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {VALUES.map((v, i) => (
              <Card key={v.title} focal={i === 0} focalTone={i === 0 ? "brand" : undefined} className="flex flex-col gap-3 p-6">
                <span className="grid size-10 place-items-center rounded-xl bg-brand-subtle">
                  <v.icon className="size-5 text-brand-strong" />
                </span>
                <div className="space-y-1">
                  <h3 className="font-semibold">{v.title}</h3>
                  <p className="text-label text-fg-tertiary">{v.desc}</p>
                </div>
              </Card>
            ))}
          </div>
        </section>

        {/* 3 步开始 */}
        <section className="space-y-4">
          <SectionHeading
            eyebrow="怎么用"
            heading="3 步开始"
            subheading="30 秒注册 · 充值 · 拉号"
          />
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            {STEPS.map((s, i) => (
              <Card key={s.title} className="flex flex-col gap-3 p-6">
                <div className="flex items-center gap-3">
                  <span className="grid size-8 place-items-center rounded-full bg-bg-elevated text-label font-semibold text-fg-secondary">
                    {i + 1}
                  </span>
                  <h3 className="font-semibold">{s.title}</h3>
                </div>
                <p className="text-label text-fg-tertiary">{s.desc}</p>
              </Card>
            ))}
          </div>
        </section>

        {/* 社群入口 · 迁自旧项目 · 但改成 TG / Discord 双入口
            链接开放后填 · 现在灰态占位 */}
        <section className="space-y-4">
          <SectionHeading
            eyebrow="社群"
            heading="加入我们"
            subheading="公告 · 技术支持 · 公测期间的额度发放"
          />
          <div className="grid gap-3 sm:grid-cols-2">
            <CommunityLink
              logo={<TelegramLogo className="size-8" />}
              name="Telegram"
              desc="主频道 · 公告和额度发放"
            />
            <CommunityLink
              logo={<DiscordLogo className="size-8" />}
              name="Discord"
              desc="讨论 · 技术支持"
            />
          </div>
        </section>

        {/* CTA 收尾 · focal 卡强调 · 收束到"注册" */}
        <Card focal focalTone="brand" className="p-10 text-center sm:p-12">
          <div className="mx-auto max-w-[520px] space-y-4">
            <h2 className="text-section font-semibold sm:text-hero">准备好了吗</h2>
            <p className="text-fg-tertiary">注册免费 · 有专属邀请码拼车更便宜</p>
            <div className="flex flex-wrap items-center justify-center gap-2 pt-1">
              <Button size="lg" variant="brand" asChild>
                <Link to="/register">立即注册</Link>
              </Button>
              <Button size="lg" variant="ghost" asChild>
                <Link to="/login">登录</Link>
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
            <span className="text-label text-fg-tertiary">· 公测中</span>
          </div>
          <p className="text-label text-fg-tertiary">一起拼车 · 号更便宜</p>
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
  return (
    <Card className="flex items-center gap-4 p-5 opacity-70">
      <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-bg-elevated">
        {logo}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="font-semibold">{name}</span>
          <Chip tone="neutral">即将开放</Chip>
        </div>
        <p className="text-label text-fg-tertiary">{desc}</p>
      </div>
    </Card>
  );
}

/* ─── 内容 ─────────────────────────────────────────────── */

const EXPLAINERS: { icon: LucideIcon; title: string; body: string }[] = [
  {
    icon: Bus,
    title: "什么是拼车",
    body: "一辆 vendor 号被多个乘客共享 · 单价直接除以人数 · 每人只出自己那份",
  },
  {
    icon: Activity,
    title: "怎么运作",
    body: "号会死 · 我们盯着自动补 · 你不用手动重新买 · 死了退你剩下的钱",
  },
  {
    icon: Shield,
    title: "凭什么信我们",
    body: "每一次扣款都有明细 · 保修覆盖成活率 · 号能推到你自己的 kiro.rs",
  },
];

const VALUES: { icon: LucideIcon; title: string; desc: string }[] = [
  {
    icon: Users,
    title: "拼单价",
    desc: "多人拉号 · 单价直接摊掉 · 越多人越便宜",
  },
  {
    icon: Activity,
    title: "号死自动补",
    desc: "号死立刻拉新的进车 · 你的号池一直有活号",
  },
  {
    icon: Database,
    title: "推自己号池",
    desc: "拉到的号自动同步到你的 kiro.rs · 完整数据回流",
  },
  {
    icon: Shield,
    title: "结算透明",
    desc: "每次扣款都有明细 · 保修覆盖成活率",
  },
];

const STEPS: { title: string; desc: string }[] = [
  { title: "注册", desc: "邮箱 + 密码 · 30 秒完事" },
  { title: "充值", desc: "多渠道支付 · 通道费透传给支付方 · 我方不吃差价" },
  { title: "拼车拉号", desc: "自己建车 / 加入他人的车 / 系统撮合 · 三选一" },
];
