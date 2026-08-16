import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { ArrowUpRight, KeyRound, Sparkles, TrendingUp } from "lucide-react";
import {
  useAutoPick, useExtract, useMe,
  useVendorOffers, useVendorStock,
  type OfferItem,
} from "@/api/hooks";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { UpstreamStatusPanel } from "@/components/UpstreamStatusPanel";
import { ExtractConfirmModal } from "@/components/ExtractConfirmModal";
import { useEstimate } from "@/api/hooks";
import { fmtCredits, toCredits, vendorLabel, I18N_VENDOR_IDS } from "@/lib/utils";
import type { Zone } from "@/types";

/** 提取 key 表单 · 通用组件（Extract 页顶部 card + PullExtractModal 都用它）
 *  docs/14 §4：数量 · 区域 · vendor · 上游状态面板常驻 · 逐字段预估费用
 *  - 手动模式 · 无护栏字段（无单价上限）
 *  - 点「提取」不直接拉 → 弹确认窗（复核信息 + 填优惠码）· decisions §8.20 */
export function PullExtractForm({
  onSubmitted,
  submitVariant = "brand",
  submitClassName,
  category = "enterprise",
}: {
  onSubmitted?: () => void;
  submitVariant?: "brand" | "primary";
  submitClassName?: string;
  /** §8.45 · 企业版(6 家 vendor · 有档 Power)/ 个人版(Kiro Vendor Market · 档 PRO/PRO+) */
  category?: "enterprise" | "personal";
}) {
  const { t } = useTranslation("extract");
  /** vendor 展示名的翻译 · 只我方自营那家要翻(前 6 家品牌名 / 匿名编号都不翻) */
  const { t: tVendor } = useTranslation("vendor");
  const pull = useExtract();
  const { data: me } = useMe();
  /** Offer matrix · **唯一数据源** · vendor + subscription 联动都从它算（docs/24 §3） */
  const { data: offers } = useVendorOffers();

  // 前置 state · subscriptionOptions/availableVendors 的 useMemo 引用它们
  const [count, setCount] = useState(3);
  const [vendorId, setVendorId] = useState<string>("auto");
  const [zone, setZone] = useState<Zone | "auto">("auto");
  const [confirmOpen, setConfirmOpen] = useState(false);

  /** 缺货 pill 文案 · **不写 defaultValue** —— 中文兜底会让英文用户看到中文
   *  且 key 缺失时静默不报（实测踩过）· 缺 key 就让它显示 key 名 · 一眼能发现 */
  const outOfStockLabel = t("pull-form.vendor.out-of-stock");

  /** 当前 tab 下 · 列**所有 supported 的 vendor** · 不 filter available
   *  缺货 vendor 也进下拉 · trigger label 保持干净·下拉项右边标缺货 pill
   *  supported=false 才不进(该 vendor 根本不提供这种 kind)
   *
   *  label 用后端返的 vendor_label（已按 tier 判过匿名）· 我方自营那家过 i18n */
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
   *  vendor=auto 时:全网该 category 下**任何 vendor supported 的档**集合
   *  vendor=具体 时:只看该 vendor 该 category 支持的档
   *  offers 未到手时返 [] · 加载完立即刷 */
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
   *    ② 当前档缺货 · 但**别的档有货** → 换到有货那档(用户没手动指定过时)
   *  换 vendor 后停在缺货档、旁边明明有货 · 是漏 case(实测 v03 PRO+ 缺货 / PRO 有 5) */
  useEffect(() => {
    if (subscriptionOptions.length === 0) return;
    const cur = subscriptionOptions.find((o) => o.value === subscription);
    const anyInStock = subscriptionOptions.some((o) => o.available > 0);
    const needFallback =
      !subscription || !cur || (!planPicked && cur.available === 0 && anyInStock);
    if (needFallback) setSubscription(pickDefaultPlan(subscriptionOptions));
  }, [subscriptionOptions, subscription, planPicked]);

  /** 档次 · 决定 vendor 显示真名还是匿名编号（只 wholesale 看真名 · docs/10-pricing §2.1） */
  const tier = me?.tier;

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
   *  auto:全 vendor 该 (kind, plan) 总量 · 具体 vendor:那家该 (kind, plan) 总量
   *  取代老的 pick?.available(只覆盖 6 家 kiro · 不覆盖 kiro_market)
   *  个人 PRO+ 的 market 库存不再被漏 */
  const currentPlanAvail = useMemo(() => {
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

  /** 数量折扣 · 当前档比第一档(基准价)便宜时算"省了多少"
   *  base = 第一档单价（1 个起的价）· saved = (base - 现档价) × count
   *  没降档 / 无分档时返 null（不显示这行） */
  const qtyDiscount = useMemo(() => {
    if (!currentBands?.length || bandUnitPrice == null) return null;
    const base = currentBands[0].unit_price_credits;
    if (base <= bandUnitPrice) return null;
    return { base, saved: (base - bandUnitPrice) * count };
  }, [currentBands, bandUnitPrice, count]);

  /** 当前单价 · 有数量分档优先用分档价(切数量会变)· 否则走 auto 推荐 / 该区单价 */
  const unitPrice =
    bandUnitPrice ?? (isAuto ? pick?.unit_price ?? null : activeZone?.unit_price ?? null);
  const available = currentPlanAvail;

  /** 可买上限 = min(vendor.max_per_order, available)
   *  - available:offers 里 (kind, plan) 总量·权威·覆盖 6+1 家
   *  - max_per_order:vendor 每单上限(可能不设 · 那就只受 available 约束)
   *  ⚠️ 不写死 200 · 用户能买超总量是核心 bug */
  const rawMax = isAuto ? pick?.max_per_order : stock?.max_per_order;
  const rawMin = isAuto ? pick?.min_per_order : stock?.min_per_order;
  const capMax = rawMax != null && rawMax > 0 ? rawMax : null;
  const capAvail = available > 0 ? available : null;
  let maxCount = 1; // 保底 · 数据未到手时不锁死输入
  if (capMax != null && capAvail != null) maxCount = Math.min(capMax, capAvail);
  else if (capMax != null) maxCount = capMax;
  else if (capAvail != null) maxCount = capAvail;
  const minCount = rawMin && rawMin > 0 ? rawMin : 1;
  /** 实际会派到的 vendor 显示名（auto 时来自推荐结果）· 我方自营那家过 i18n */
  const effectiveVendorLabel = isAuto
    ? pick?.vendor_label ?? t("pull-form.vendor.auto-fallback")
    : I18N_VENDOR_IDS.has(vendorId)
      ? tVendor(vendorId, { defaultValue: vendorLabel(vendorId, tier) })
      : vendorLabel(vendorId, tier);
  const effectiveZone = isAuto ? pick?.zone ?? null : activeZone ? (stock!.zones.length === 1 ? null : activeZone.zone) : null;

  /* 预估费用 · 走后端 /me/pull/estimate（对外只三项：unit_price / service_fee / total） */
  const estimateMut = useEstimate();
  const [estimate, setEstimate] = useState<{ unit_price: number; service_fee: number; total: number } | null>(null);
  useEffect(() => {
    // bandUnitPrice != null 时也走本地算:后端 estimate 还不认数量分档
    // （分档 offer 打后端会返 flat 价 · 跟上面显示的分档单价打架）
    if (unitPrice == null || isAuto || vendorId === "auto" || bandUnitPrice != null) {
      // 系统派号那条 auto 分支还没接（后端 estimate 需要 vendor_id）· 先按单价 × count 估
      // TODO(1a): auto-pick 端点应返回 estimate 一起给
      setEstimate(unitPrice == null ? null : {
        unit_price: unitPrice,
        service_fee: 0,
        total: unitPrice * count,
      });
      return;
    }
    let alive = true;
    estimateMut.mutateAsync({ vendor_id: vendorId, zone: effectiveZone ?? undefined, count })
      .then((r) => { if (alive) setEstimate(r); })
      .catch(() => { if (alive) setEstimate(null); });
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [vendorId, effectiveZone, count, unitPrice, isAuto, bandUnitPrice]);

  const bargain = count === 1;
  const outOfStock = available === 0;

  /* 切换 vendor 时如果 count 超过新 max · 收紧 */
  useEffect(() => {
    if (count > maxCount) setCount(maxCount);
  }, [maxCount, count]);

  /** 确认窗里点「确认提取」才真拉 · couponCode 是本次减免码
   *  Step 5d · 带上 account_kind + plan · 后端硬约束（缺货不降级） */
  const onConfirm = async (couponCode?: string) => {
    await pull.mutateAsync({
      vendor_id: vendorId,
      zone: zone === "auto" ? undefined : zone,
      count,
      coupon_code: couponCode,
      account_kind: category,
      plan: subscription as "power" | "pro" | "pro_plus" | "pro_max" | undefined,
    });
    setConfirmOpen(false);
    setCount(3);
    setVendorId("auto");
    setZone("auto");
    onSubmitted?.();
  };

  return (
    <>
      <form
        onSubmit={(e) => { e.preventDefault(); setConfirmOpen(true); }}
        className="space-y-5"
      >
        {/* vendor / 档位 / [区域] / 数量 · §8.45 加档位维度
            企业版:vendor · Power 档 · 区域 · 数量(4 列)
            个人版:vendor · PRO/PRO+ 档 · 数量(3 列 · 个人号无区域概念) */}
        <div
          className={
            "grid grid-cols-1 gap-4 " +
            (category === "personal"
              ? "md:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1fr)]"
              : "md:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)]")
          }
        >
          <Field label={t("pull-form.field.vendor")}>
            {/* 换 vendor 要重新评估档位默认值(新 vendor 的档位/库存都不同) */}
            <Select
              value={vendorId}
              onValueChange={(v) => { setPlanPicked(false); setVendorId(v); }}
            >
              <SelectTrigger hint={selectedVendorOOS ? outOfStockLabel : undefined}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {/* 默认项 = 系统派号 · 面板里展示推荐结果和价格(decisions §8.20) */}
                <SelectItem value="auto">{t("pull-form.vendor.auto")}</SelectItem>
                {availableVendors.map((v) => (
                  <SelectItem
                    key={v.vendor_id}
                    value={v.vendor_id}
                    disabled={v.available === 0}
                    hint={v.available === 0 ? outOfStockLabel : undefined}
                  >
                    {/* 后端已按 tier 判过匿名 · 直接用 vendor_label · 不重算 */}
                    {v.vendor_label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label={t("pull-form.field.subscription")}>
            {/* 所有 supported 档全展示 · 缺货 disabled + 缺货 pill · 有默认值 */}
            <Select
              value={subscription}
              onValueChange={(v) => { setPlanPicked(true); setSubscription(v); }}
            >
              <SelectTrigger hint={selectedSubOOS ? outOfStockLabel : undefined}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {subscriptionOptions.map((s) => (
                  <SelectItem
                    key={s.value}
                    value={s.value}
                    disabled={s.available === 0}
                    hint={s.available === 0 ? outOfStockLabel : undefined}
                  >
                    {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {/* 区域只在企业档显示 · 个人号(PRO/PRO+)不分区 · §8.45 */}
          {category === "enterprise" && (
            <Field label={t("pull-form.field.zone")}>
              <Select value={zone} onValueChange={(v) => setZone(v as Zone | "auto")}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">{t("pull-form.zone.auto")}</SelectItem>
                  <SelectItem value="us">{t("pull-form.zone.us")}</SelectItem>
                  <SelectItem value="eu">{t("pull-form.zone.eu")}</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          )}
          <Field label={t("pull-form.field.count")} hint={t("pull-form.field.count-hint", { min: minCount, max: maxCount })}>
            <Input
              type="number"
              min={1}
              max={maxCount}
              value={count}
              onChange={(e) => setCount(Math.max(1, Math.min(maxCount, Number(e.target.value) || 1)))}
            />
          </Field>
        </div>

        {/* 上游状态 + 预估费用 · 并排（宽屏）/ 堆叠（窄屏） */}
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <UpstreamStatusPanel vendorId={vendorId} zone={zone} />

          {estimate ? (
            /* 预估费用卡 · 对外只显示单价 / 服务费 / 小计（decisions §8.20 · 不展示计费分项） */
            <div className="flex flex-col justify-between rounded-xl border border-hairline bg-bg-elevated/40 p-4 text-label">
              <div>
                <div className="mb-3 font-semibold text-fg">{t("pull-form.estimate.title")}</div>
                <div className="space-y-1.5">
                  <FeeRow
                    label={t("pull-form.estimate.unit-line", { count })}
                    value={t("pull-form.estimate.unit-value", { unit: toCredits(estimate.unit_price), count })}
                  />
                  {/* 数量折扣 · 买够数降档时说清"原价多少 · 省多少"
                      不然用户在上面看到 50 · 这里显示 40 · 不知道为什么 */}
                  {qtyDiscount && (
                    <FeeRow
                      label={t("pull-form.estimate.qty-discount")}
                      value={
                        <span className="text-ok-fg">
                          {t("pull-form.estimate.qty-discount-value", {
                            base: toCredits(qtyDiscount.base),
                            saved: fmtCredits(qtyDiscount.saved),
                          })}
                        </span>
                      }
                    />
                  )}
                  {estimate.service_fee > 0 && (
                    <FeeRow
                      label={t("pull-form.estimate.service-fee")}
                      value={t("pull-form.estimate.service-fee-value", { value: fmtCredits(estimate.service_fee) })}
                      muted
                    />
                  )}
                  {/* 通道费只在充值积分时收 · 拉号/提取都是抵扣积分 · decisions §8.21 · 不显示 */}
                </div>
              </div>
              <div className="mt-3 border-t border-hairline pt-2">
                <FeeRow
                  label={t("pull-form.estimate.total-label")}
                  value={
                    <strong className="tnum text-fg">
                      {t("pull-form.estimate.total-value", { value: fmtCredits(estimate.total) })}
                    </strong>
                  }
                  strong
                />
              </div>
            </div>
          ) : (
            <div className="grid place-items-center rounded-xl border border-hairline bg-bg-elevated/40 p-4 text-label text-fg-tertiary">
              {t("pull-form.estimate.loading")}
            </div>
          )}
        </div>

        {/* count=1 提示 */}
        {bargain && (
          <Alert tone="neutral" icon={Sparkles} title={t("pull-form.bargain.title")}>
            {t("pull-form.bargain.body")}
          </Alert>
        )}

        {/* 底行 · 左下角：价格趋势入口 + 波动提示 · 右侧提交 */}
        <div className="flex flex-col-reverse gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div className="max-w-md space-y-1.5">
            {/* 查看历史价格趋势 · 跳独立页看 vendor 价格走势 · decisions §8.22 */}
            <Link
              to="/prices"
              className="inline-flex items-center gap-1 text-label font-medium text-brand-strong transition-colors hover:text-brand"
            >
              <TrendingUp className="size-3.5" />
              {t("pull-form.history-link")}
              <ArrowUpRight className="size-3" />
            </Link>
            <p className="text-label leading-relaxed text-fg-tertiary">
              {t("pull-form.history-note")}
            </p>
          </div>
          <Button
            type="submit"
            variant={submitVariant}
            size="lg"
            disabled={outOfStock}
            className={submitClassName}
          >
            <KeyRound />
            {outOfStock ? t("pull-form.submit.out-of-stock") : t("pull-form.submit.text", { count })}
          </Button>
        </div>
      </form>

      {/* 确认窗 · 复核信息 + 填优惠码 + 确认才真拉 */}
      <ExtractConfirmModal
        open={confirmOpen}
        onClose={() => setConfirmOpen(false)}
        onConfirm={onConfirm}
        pending={pull.isPending}
        info={{
          vendorLabel: effectiveVendorLabel,
          isAuto,
          zone: effectiveZone,
          count,
          unitPrice,
          warrantyMinutes: (isAuto ? pick?.warranty_minutes : stock?.warranty_minutes) ?? 0,
          estimate,
        }}
      />
    </>
  );
}

function FeeRow({
  label, value, strong, muted,
}: {
  label: React.ReactNode;
  value: React.ReactNode;
  strong?: boolean;
  muted?: boolean;
}) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <span className={muted ? "text-fg-tertiary" : "text-fg-secondary"}>{label}</span>
      <span className={strong ? "font-semibold" : "tnum text-fg-secondary"}>{value}</span>
    </div>
  );
}
