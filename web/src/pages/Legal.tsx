import { useEffect, useMemo } from "react";
import { Link, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import ReactMarkdown from "react-markdown";
import { ArrowLeft, ChevronRight } from "lucide-react";
import { AppFooter } from "@/components/AppFooter";
import { PromoBar } from "@/components/PromoBar";
import { PublicHeader } from "@/components/PublicHeader";
import { DocumentMeta } from "@/components/DocumentMeta";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/primitives";
import { resolveLang } from "@/i18n";

/* Vite 编译期把所有 md 打进包 · key 例：./legal/terms.en.md · 值是原始字符串 */
const modules = import.meta.glob("../content/legal/*.md", {
  eager: true,
  query: "?raw",
  import: "default",
}) as Record<string, string>;

/** 页面 slug 白名单 · 顺序即 index 页展示顺序 · 跟 100b.best 一致 */
export const LEGAL_PAGES = [
  { slug: "terms", titleKey: "legal.pages.terms" },
  { slug: "usage", titleKey: "legal.pages.usage" },
  { slug: "services", titleKey: "legal.pages.services" },
  { slug: "regions", titleKey: "legal.pages.regions" },
] as const;

type LegalSlug = (typeof LEGAL_PAGES)[number]["slug"];

/** 按 slug + 当前语言取 markdown 内容 · 缺失 fallback 到英文 · 都缺就空 */
function loadLegal(slug: string, lang: string): string {
  const l = resolveLang(lang);
  const primary = modules[`../content/legal/${slug}.${l}.md`];
  if (primary) return primary;
  const en = modules[`../content/legal/${slug}.en.md`];
  return en ?? "";
}

/** 未登录也能看 · 挂在 /legal /legal/:slug · 独立布局（不套 AppLayout · 那个要登录） */
export default function LegalLayout() {
  const { slug } = useParams<{ slug?: string }>();
  const isIndex = !slug;
  return (
    <div className="flex min-h-dvh flex-col bg-bg">
      <DocumentMeta />
      <PromoBar />
      <PublicHeader />

      <main className="flex-1">
        {isIndex ? <LegalIndex /> : <LegalPage slug={slug as LegalSlug} />}
      </main>

      <AppFooter />
    </div>
  );
}

function LegalIndex() {
  const { t } = useTranslation();
  return (
    <div className="page-container py-14 lg:py-20">
      <div className="mx-auto max-w-3xl">
        <h1 className="text-hero font-semibold tracking-tight">
          {t("legal.title")}
        </h1>
        <p className="mt-3 leading-relaxed text-fg-secondary">
          {t("legal.intro")}
        </p>

        <div className="mt-8 space-y-3">
          {LEGAL_PAGES.map((p) => (
            <Link
              key={p.slug}
              to={`/legal/${p.slug}`}
              className="group block rounded-panel border border-hairline bg-bg p-5 transition-all hover:-translate-y-0.5 hover:border-brand hover:shadow-hover"
            >
              <div className="flex items-center justify-between gap-4">
                <div>
                  <h2 className="text-body-lg font-semibold">
                    {t(p.titleKey)}
                  </h2>
                  <p className="mt-1 text-label text-fg-tertiary">
                    {t(`legal.summary.${p.slug}`)}
                  </p>
                </div>
                <ChevronRight className="size-5 shrink-0 text-fg-tertiary transition-transform group-hover:translate-x-0.5 group-hover:text-brand-strong" />
              </div>
            </Link>
          ))}
        </div>
      </div>
    </div>
  );
}

function LegalPage({ slug }: { slug: LegalSlug }) {
  const { t, i18n } = useTranslation();
  const md = useMemo(() => loadLegal(slug, i18n.language), [slug, i18n.language]);

  // 页面切换时滚到顶（长文，链接跳过来时用户期望从头开始读）
  useEffect(() => {
    window.scrollTo({ top: 0, behavior: "instant" });
  }, [slug]);

  if (!md) {
    return (
      <div className="page-container py-14 text-center text-fg-secondary">
        {t("legal.notFound")}
      </div>
    );
  }

  return (
    <div className="page-container py-10 lg:py-14">
      <div className="mx-auto max-w-3xl">
        <Button variant="ghost" size="sm" asChild className="mb-4 -ml-2 text-fg-tertiary hover:text-fg">
          <Link to="/legal">
            <ArrowLeft className="size-4" />
            {t("legal.backToIndex")}
          </Link>
        </Button>

        <Card className="p-8 lg:p-10">
          {/* Tailwind Typography 未装 · 自己给核心标签写点排版
             跟站内 h1/h2/p 走一样的字号栈 · 但 md 内表达灵活，用 prose-like 类实现 */}
          <article className="legal-prose">
            <ReactMarkdown>{md}</ReactMarkdown>
          </article>
        </Card>
      </div>
    </div>
  );
}
