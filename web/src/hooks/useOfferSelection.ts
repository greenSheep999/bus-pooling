import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useAutoPick, useMe, useVendorOffers, useVendorStock,
  type OfferItem,
} from "@/api/hooks";
import { I18N_VENDOR_IDS } from "@/lib/utils";
import type { Zone } from "@/types";

/** Offer 维度选择的取数与派生逻辑 · Extract 页顶部表单和车内立即拉号弹窗共用（docs/24 §3）
 *  纯计算 · 无 UI · 两个表单各写各的界面和提交路径 · 数据源统一走 useVendorOffers。
 *
 *  seed:车级策略种子(preferredVendor / defaultCount)· 只作 state 初值 ·
 *  之后由 category reset / 库存收紧等 effect 接管 · 调用方需在种子落空(该档下该 vendor
 *  supported=false)时自行回落 "auto"(否则 SelectValue 空白)。 */
export function useOfferSelection({
  category,
  seedCount,
  seedVendor,
}: {
  category: "enterprise" | "personal";
  seedCount?: number;
  seedVendor?: string;
}) {
  /** vendor 展示名的翻译 · 只我方自营那家要翻(前 6 家品牌名 / 匿名编号都不翻) */
  const { t: tVendor } = useTranslation("vendor");
  const { data: me } = useMe();
  /** Offer matrix · **唯一数据源** · vendor + subscription 联动都从它算（docs/24 §3） */
  const { data: offers } = useVendorOffers();

  const [count, setCount] = useState(seedCount ?? 3);
  const [vendorId, setVendorId] = useState<string>(seedVendor ?? "auto");
  const [zone, setZone] = useState<Zone | "auto">("auto");

  /** 当前 category 下 · 列**所有 supported 的 vendor** · 不 filter available
   *  缺货 vendor 也进下拉(下拉项右边标缺货 pill)· supported=false 才不进
   *  label 用后端返的 vendor_label(已按 tier 判过匿名)· 我方自营那家过 i18n */
  const availableVendors = useMemo(() => {
    return (offers?.vendors ?? [])
      .filter((v) => v.categories[category]?.supported)
      .map((v) => ({
        vendor_id: v.vendor_id,
        vendor_label: I18N_VENDOR_IDS.has(v.vendor_id)
          ? tVendor(v.vendor_id, { defaultValue: v.vendor_label })
          : v.vendor_label,
        available: v.categories[category]?.available ?? 0,
      }));
  }, [offers, category, tVendor]);

  /** subscription 下拉合法档 · **纯从 Offer matrix 派生 · 不写死任何档**
   *  vendor=auto 时:全网该 category 下任何 vendor supported 的档集合
   *  vendor=具体 时:只看该 vendor 该 category 支持的档 */
  const subscriptionOptions = useMemo(() => {
    const LABEL: Record<string, string> = {
      power: "Power 10000", pro: "PRO 1000", pro_plus: "PRO+ 2000", pro_max: "PRO Max",
    };
    const availByPlan = new Map<string, number>();
    for (const v of offers?.vendors ?? []) {
      if (vendorId !== "auto" && v.vendor_id !== vendorId) continue;
      if (!v.categories[category]?.supported) continue;
      for (const o of v.categories[category]?.offers ?? []) {
        if (!o.subscription) continue;
        availByPlan.set(o.subscription, (availByPlan.get(o.subscription) ?? 0) + (o.available ?? 0));
      }
    }
    return [...availByPlan.entries()].map(([plan, avail]) => ({
      value: plan,
      label: LABEL[plan] ?? plan,
      available: avail,
    }));
  }, [offers, category, vendorId]);

  const [subscription, setSubscription] = useState<string>("");
  /** 用户是否手动点过档位 · 手动选过就尊重他的选择(哪怕缺货)· 不再自动纠正 */
  const [planPicked, setPlanPicked] = useState(false);
  /** 选默认档:优先第一个 available>0 的·全缺货就选第一个(下拉不留空) */
  const pickDefaultPlan = (
    opts: { value: string; available: number }[],
  ): string => {
    const hot = opts.find((o) => o.available > 0);
    return (hot ?? opts[0])?.value ?? "";
  };
  // category 切时重置 subscription 到该 category 的默认档
  // 切个人 tab 时 zone 必须归位("auto")· 个人池多数不分区
  useEffect(() => {
    setSubscription(pickDefaultPlan(subscriptionOptions));
    setPlanPicked(false);
    setVendorId("auto");
    if (category === "personal") setZone("auto");
  }, // eslint-disable-next-line react-hooks/exhaustive-deps
  [category]);
  /** 选项变了(切 vendor / 库存刷新)要回落的两种情况:
   *    ① 当前档在新选项里没了 → 必须换
   *    ② 当前档缺货 · 但**别的档有货** → 换到有货那档(用户没手动指定过时) */
  useEffect(() => {
    if (subscriptionOptions.length === 0) return;
    const cur = subscriptionOptions.find((o) => o.value === subscription);
    const anyInStock = subscriptionOptions.some((o) => o.available > 0);
    const needFallback =
      !subscription || !cur || (!planPicked && cur.available === 0 && anyInStock);
    if (needFallback) setSubscription(pickDefaultPlan(subscriptionOptions));
  }, [subscriptionOptions, subscription, planPicked]);

  /* 具体 vendor 的 stock（vendorId 是 auto 时不发请求） */
  const { data: stock } = useVendorStock(vendorId === "auto" ? undefined : vendorId);
  /* auto 模式的系统派号推荐 · 提供最终价用于预估 */
  const { data: pick } = useAutoPick(zone);

  const isAuto = vendorId === "auto";

  /** trigger 上显示缺货灰字 · 当前选中的 vendor/subscription 在 offer matrix 里 available=0 */
  const selectedVendorOOS = useMemo(() => {
    if (isAuto) return false;
    const v = availableVendors.find((x) => x.vendor_id === vendorId);
    return v ? v.available === 0 : false;
  }, [availableVendors, vendorId, isAuto]);
  const selectedSubOOS = useMemo(() => {
    const s = subscriptionOptions.find((x) => x.value === subscription);
    return s ? s.available === 0 : false;
  }, [subscriptionOptions, subscription]);

  /* 具体 zone 的单价（用于预估）· auto 时选最便宜一区 */
  const activeZone = useMemo(() => {
    if (!stock) return null;
    if (stock.zones.length === 1) return stock.zones[0];
    if (zone === "auto") {
      return stock.zones.reduce((a, b) => (a.unit_price <= b.unit_price ? a : b));
    }
    return stock.zones.find((z) => z.zone === zone) ?? stock.zones[0];
  }, [stock, zone]);

  /** 当前 (kind, subscription) 组合的**权威库存**·从 offers 派生
   *  auto:全 vendor 该 (kind, plan) 总量 · 具体 vendor:那家该 (kind, plan) 总量 */
  const available = useMemo(() => {
    let n = 0;
    for (const v of offers?.vendors ?? []) {
      if (vendorId !== "auto" && v.vendor_id !== vendorId) continue;
      if (!v.categories[category]?.supported) continue;
      for (const o of v.categories[category]?.offers ?? []) {
        if (o.subscription !== subscription) continue;
        n += o.available ?? 0;
      }
    }
    return n;
  }, [offers, vendorId, category, subscription]);

  /** 当前 (kind, plan) 的数量分档表 · 部分 vendor 买得多单价更低
   *  auto 时用最便宜那家的分档(跟 auto 的"比价"语义一致) */
  const currentBands = useMemo(() => {
    let best: NonNullable<OfferItem["price_bands"]> | null = null;
    let bestFirst = Infinity;
    for (const v of offers?.vendors ?? []) {
      if (vendorId !== "auto" && v.vendor_id !== vendorId) continue;
      if (!v.categories[category]?.supported) continue;
      for (const o of v.categories[category]?.offers ?? []) {
        if (o.subscription !== subscription) continue;
        if (!o.price_bands?.length) continue;
        const first = o.price_bands[0].unit_price_credits;
        if (first < bestFirst) { bestFirst = first; best = o.price_bands; }
      }
    }
    return best;
  }, [offers, vendorId, category, subscription]);

  /** 分档单价 · 找 count 落在哪个区间(Upper=0 = 及以上)· 无分档返 null */
  const bandUnitPrice = useMemo(() => {
    if (!currentBands?.length) return null;
    const hit = currentBands.find(
      (b) => count >= b.lower && (b.upper === 0 || count <= b.upper),
    );
    return hit?.unit_price_credits ?? null;
  }, [currentBands, count]);

  /** 数量折扣 · 当前档比第一档(基准价)便宜时算"省了多少" */
  const qtyDiscount = useMemo(() => {
    if (!currentBands?.length || bandUnitPrice == null) return null;
    const base = currentBands[0].unit_price_credits;
    if (base <= bandUnitPrice) return null;
    return { base, saved: (base - bandUnitPrice) * count };
  }, [currentBands, bandUnitPrice, count]);

  /** 当前单价 · 有数量分档优先用分档价(切数量会变)· 否则走 auto 推荐 / 该区单价 */
  const unitPrice =
    bandUnitPrice ?? (isAuto ? pick?.unit_price ?? null : activeZone?.unit_price ?? null);

  /** 可买上限 = min(vendor.max_per_order, available) · ⚠️ 不写死 200 */
  const rawMax = isAuto ? pick?.max_per_order : stock?.max_per_order;
  const rawMin = isAuto ? pick?.min_per_order : stock?.min_per_order;
  const capMax = rawMax != null && rawMax > 0 ? rawMax : null;
  const capAvail = available > 0 ? available : null;
  let maxCount = 1; // 保底 · 数据未到手时不锁死输入
  if (capMax != null && capAvail != null) maxCount = Math.min(capMax, capAvail);
  else if (capMax != null) maxCount = capMax;
  else if (capAvail != null) maxCount = capAvail;
  const minCount = rawMin && rawMin > 0 ? rawMin : 1;

  /** 实际会派到的 zone · auto 时来自推荐结果 · 单区时不显示区名 */
  const effectiveZone =
    isAuto ? pick?.zone ?? null
      : activeZone ? (stock!.zones.length === 1 ? null : activeZone.zone) : null;

  /** 质保时长(分钟)· auto 来自推荐 · 具体 vendor 来自 stock */
  const warrantyMinutes = (isAuto ? pick?.warranty_minutes : stock?.warranty_minutes) ?? 0;

  const outOfStock = available === 0;

  /* 切换 vendor / 库存刷新时 count 超过新 max · 收紧 */
  useEffect(() => {
    if (count > maxCount) setCount(maxCount);
  }, [maxCount, count]);

  return {
    tier: me?.tier,
    count, setCount,
    vendorId, setVendorId,
    zone, setZone,
    subscription, setSubscription,
    planPicked, setPlanPicked,
    availableVendors,
    subscriptionOptions,
    selectedVendorOOS,
    selectedSubOOS,
    activeZone,
    effectiveZone,
    isAuto,
    pick,
    stock,
    available,
    unitPrice,
    bandUnitPrice,
    qtyDiscount,
    maxCount,
    minCount,
    warrantyMinutes,
    outOfStock,
  };
}
