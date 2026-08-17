import { useTranslation } from "react-i18next";
import { Chip } from "@/components/ui/primitives";
import { RANK_TONE, rankOfBus, rankOfLifespan } from "@/lib/rank";

/** 号的评价 · 按存活时长给档（活号按"当前已存活"· 会随时间升级）
 *
 *  文案走 common:rank.* · 阈值走 lib/rank.ts（车和 key 共用一套 · 别在这儿另写） */
export function KeyRankBadge({
  lifespanSeconds,
  className,
}: {
  lifespanSeconds: number;
  className?: string;
}) {
  const { t } = useTranslation();
  const rank = rankOfLifespan(lifespanSeconds);
  return (
    <Chip tone={RANK_TONE[rank]} className={className}>
      {t(`rank.${rank}`)}
    </Chip>
  );
}

/** 车（母号）的评价 · 车没死就是装甲车 · 死了按整车最长存活给档
 *
 *  **个人号没有车 · 别对个人号用这个**（`lib/rank.ts` 注释说明了为什么分开） */
export function BusRankBadge({
  aliveCount,
  maxLifespanSeconds,
  className,
}: {
  aliveCount: number;
  maxLifespanSeconds: number;
  className?: string;
}) {
  const { t } = useTranslation();
  const rank = rankOfBus(aliveCount, maxLifespanSeconds);
  return (
    <Chip tone={RANK_TONE[rank]} className={className}>
      {t(`rank.${rank}`)}
    </Chip>
  );
}

/** 企业号 / 个人号标签 · 提取页 + 车内号列表都要
 *
 *  只有一种档时用户看不出区别（车主:"少了这是个人还是企业的标签 · 多了就不知道了"）·
 *  所以**一律显示**（不做"只有混合时才显示"的聪明逻辑 · 那会让单档时反而没信息）。
 *  account_kind 缺失（老数据）时不显示 —— 不猜。 */
export function AccountKindTag({
  kind,
  className,
}: {
  kind?: "enterprise" | "personal";
  className?: string;
}) {
  const { t } = useTranslation("extract");
  if (!kind) return null;
  return (
    <Chip tone={kind === "enterprise" ? "brand" : "neutral"} className={className}>
      {t(`account-kind.${kind}`)}
    </Chip>
  );
}
