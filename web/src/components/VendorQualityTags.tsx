import { useTranslation } from "react-i18next";
import { Chip } from "@/components/ui/primitives";
import type { VendorQualityTag } from "@/api/hooks";

/** Vendor 质量标签渲染 · 全站共用（Status / 未来 Buses / Extract）
 *
 *  后端 computeQuality 决定挂哪些 kind · 前端只做 kind → 色调 + i18n 文案映射。
 *  Chip tone: ok=绿(稳/有货) · brand=紫(高产/保质) · info→neutral(活跃) · warn=黄(观察中)
 *
 *  标签顺序：正向标签先 · 观察态最后（视觉上"好"的信号排前面）
 */
const TAG_TONE: Record<VendorQualityTag["kind"], "ok" | "brand" | "neutral" | "warn"> = {
  "stable":      "ok",
  "high-volume": "brand",
  "active":      "neutral",
  "in-stock":    "ok",
  "warranty":    "brand",
  "watching":    "warn",
};

/** 排序权重 · 数字越小越靠前 */
const TAG_ORDER: Record<VendorQualityTag["kind"], number> = {
  "high-volume": 1,
  "stable":      2,
  "active":      3,
  "in-stock":    4,
  "warranty":    5,
  "watching":    99,
};

export function QualityTags({
  tags,
  className,
}: {
  tags: VendorQualityTag[] | undefined;
  className?: string;
}) {
  const { t } = useTranslation("status");
  if (!tags || tags.length === 0) return null;

  const sorted = [...tags].sort((a, b) => TAG_ORDER[a.kind] - TAG_ORDER[b.kind]);

  return (
    <div className={"flex flex-wrap gap-1.5 " + (className ?? "")}>
      {sorted.map((tag) => (
        <Chip key={tag.kind} tone={TAG_TONE[tag.kind]}>
          {t(`tags-quality.${tag.kind}`)}
        </Chip>
      ))}
    </div>
  );
}
