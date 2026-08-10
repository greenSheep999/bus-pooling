import { Cloud, FileCheck2, Send, Timer } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Card, Chip } from "@/components/ui/primitives";

/** 3 张能力卡 · 阶段 3b/3c 真做的时候这页直接填内容，结构不推翻（decisions §8.3） */
const FEATURES: { icon: LucideIcon; titleKey: string; descKey: string }[] = [
  {
    icon: Cloud,
    titleKey: "feature.aws.title",
    descKey: "feature.aws.desc",
  },
  {
    icon: FileCheck2,
    titleKey: "feature.audit.title",
    descKey: "feature.audit.desc",
  },
  {
    icon: Timer,
    titleKey: "feature.lifetime.title",
    descKey: "feature.lifetime.desc",
  },
];

export default function Dispatch() {
  const { t } = useTranslation("dispatch");
  return (
    <div className="space-y-section">
      {/* Hero · focal 光晕 · 这页没有数据，唯一的内容就是"将来会有什么" */}
      <Card focal focalTone="brand" className="p-10 text-center sm:p-14">
        <div className="mx-auto max-w-[560px] space-y-5">
          <span className="grid size-14 place-items-center rounded-2xl bg-brand-subtle mx-auto">
            <Send className="size-7 text-brand-strong" />
          </span>

          {/* 标题到描述恒 8px（space-y-2）· 跟其他页一致，见 13-design-principles §5.2b */}
          <div className="space-y-2">
            <Chip tone="brand">{t("hero.badge")}</Chip>
            <h1 className="text-hero font-semibold">{t("hero.title")}</h1>
            <p className="text-fg-tertiary">
              {t("hero.desc")}
            </p>
          </div>

          <div className="flex flex-wrap items-center justify-center gap-2 pt-1">
            <Button variant="ghost" asChild>
              <Link to="/buses">{t("hero.cta.pool")}</Link>
            </Button>
            <Button variant="ghost" asChild>
              <Link to="/docs">{t("hero.cta.docs")}</Link>
            </Button>
          </div>
        </div>
      </Card>

      {/* 3 张能力卡 */}
      <div className="grid grid-cols-1 gap-6 sm:grid-cols-3">
        {FEATURES.map((f) => (
          <Card key={f.titleKey} className="flex flex-col gap-3 p-6">
            <span className="grid size-9 place-items-center rounded-xl bg-bg-elevated">
              <f.icon className="size-4 text-fg-secondary" />
            </span>
            <div className="space-y-1">
              <div className="font-semibold">{t(f.titleKey)}</div>
              <p className="text-label text-fg-tertiary">{t(f.descKey)}</p>
            </div>
          </Card>
        ))}
      </div>

      <p className="text-center text-label text-fg-tertiary">
        {t("footer.prefix")}<Link to="/buses" className="font-semibold text-brand-strong hover:underline">{t("footer.link.pool")}</Link>
        {t("footer.and")}<Link to="/extract" className="font-semibold text-brand-strong hover:underline">{t("footer.link.extract")}</Link>
        {t("footer.suffix")}
      </p>
    </div>
  );
}
