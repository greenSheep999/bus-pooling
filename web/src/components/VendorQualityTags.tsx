import { useTranslation } from "react-i18next";
import { Chip } from "@/components/ui/primitives";
import { MicroStat } from "@/components/ui/tags";
import type { VendorQualityTag } from "@/api/hooks";

/** Vendor 质量标签渲染 · 全站共用（Status / Overview vendor 监测 / 未来 Buses/Extract）
 *
 *  后端 computeQuality 决定挂哪些 kind · 前端只做 kind → 色调 + i18n 文案映射。
 *  色调：ok=绿(稳/有货) · brand=紫(高产/保质) · info=蓝(活跃) · warn=黄(观察中)
 *
 *  size：
 *   - "chip"（默认）· Status 页卡片里用 · 12px Chip
 *   - "micro" · 表格行内用（Overview vendor 监测）· 10px MicroStat ·
 *     跟同行的 Best/缺货 pill 同尺寸 —— 一行里两种大小的 badge 会显得乱
 */
const TAG_TONE: Record<VendorQualityTag["kind"], "ok" | "brand" | "info" | "warn" | "danger"> = {
  "stable":       "ok",
  "high-volume":  "brand",
  "active":       "info",
  "in-stock":     "ok",
  "out-of-stock": "danger",
  "warranty":     "brand",
  "watching":     "warn",
};

/** 排序权重 · 数字越小越靠前 · 缺货最靠前(用户最想一眼看到"这家买不到") */
const TAG_ORDER: Record<VendorQualityTag["kind"], number> = {
  "out-of-stock": 0,
  "high-volume":  1,
  "stable":       2,
  "active":       3,
  "in-stock":     4,
  "warranty":     5,
  "watching":     99,
};

export function QualityTags({
  tags,
  size = "chip",
  className,
}: {
  tags: VendorQualityTag[] | undefined;
  size?: "chip" | "micro";
  className?: string;
}) {
  const { t } = useTranslation("status");
  if (!tags || tags.length === 0) return null;

  const sorted = [...tags].sort((a, b) => TAG_ORDER[a.kind] - TAG_ORDER[b.kind]);

  return (
    <div className={"flex flex-wrap items-center gap-1.5 " + (className ?? "")}>
      {sorted.map((tag) =>
        size === "micro" ? (
          <MicroStat key={tag.kind} tone={TAG_TONE[tag.kind]}>
            {t(`tags-quality.${tag.kind}`)}
          </MicroStat>
        ) : (
          <Chip key={tag.kind} tone={TAG_TONE[tag.kind]}>
            {t(`tags-quality.${tag.kind}`)}
          </Chip>
        ),
      )}
    </div>
  );
}
