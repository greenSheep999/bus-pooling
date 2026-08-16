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
import { fmtCredits, toCredits, vendorLabel } from "@/lib/utils";
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
  const pull = useExtract();
  const { data: me } = useMe();
  /** Offer matrix · **唯一数据源** · vendor + subscription 联动都从它算（docs/24 §3） */
  const { data: offers } = useVendorOffers();

  // 前置 state · subscriptionOptions/availableVendors 的 useMemo 引用它们
  const [count, setCount] = useState(3);
  const [vendorId, setVendorId] = useState<string>("auto");
  const [zone, setZone] = useState<Zone | "auto">("auto");
  const [confirmOpen, setConfirmOpen] = useState(false);

  /** 当前 tab 下 · 哪些 vendor **该 category 有货**(available > 0)才出现在下拉
   *  supported=true available=0 走"暂时缺货"提示 · 不进下拉（避免用户选了没货） */
  const availableVendors = useMemo(() => {
    return (offers?.vendors ?? [])
      .filter((v) => v.categories[category]?.available > 0)
      .map((v) => ({ vendor_id: v.vendor_id, vendor_label: v.vendor_label }));
  }, [offers, category]);

  /** subscription 下拉合法档 · 来自 Offer matrix · 不是硬编码
   *  vendor=auto 时:全网该 category 下**存在** available>0 offer 的档位集合
   *  vendor=具体 时:只看该 vendor 该 category 的档位 */
  const subscriptionOptions = useMemo(() => {
    const seen = new Set<string>();
    const rows: OfferItem[] = [];
    for (const v of offers?.vendors ?? []) {
      if (vendorId !== "auto" && v.vendor_id !== vendorId) continue;
      for (const o of v.categories[category]?.offers ?? []) {
        if (o.available <= 0 || !o.subscription) continue;
        if (seen.has(o.subscription)) continue;
        seen.add(o.subscription);
        rows.push(o);
      }
    }
    if (rows.length === 0) {
      // 兜底 · 没数据时给用户一个默认档避免下拉空白（提交时后端会拒）
      return category === "enterprise"
        ? [{ value: "power", label: "Power" }]
        : [{ value: "pro", label: "PRO" }];
    }
    const LABEL: Record<string, string> = {
      power: "Power 10000", pro: "PRO 1000", pro_plus: "PRO+ 2000", pro_max: "PRO Max",
    };
    return rows.map((r) => ({ value: r.subscription, label: LABEL[r.subscription] ?? r.subscription }));
  }, [offers, category, vendorId]);

  const [subscription, setSubscription] = useState<string>("");
  // category 切时重置 subscription 到该 category 的第一个可选档
  // 切个人 tab 时 zone 必须归位("auto")· 个人池多数不分区
  useEffect(() => {
    setSubscription(subscriptionOptions[0]?.value ?? "");
    setVendorId("auto");
    if (category === "personal") setZone("auto");
  }, // eslint-disable-next-line react-hooks/exhaustive-deps
  [category]);
  // subscription 选项变了（切 vendor 后）· 如果当前档不在新选项里 · 回落到第一个
  useEffect(() => {
    if (subscription && !subscriptionOptions.find((o) => o.value === subscription)) {
      setSubscription(subscriptionOptions[0]?.value ?? "");
    }
  }, [subscriptionOptions, subscription]);

  /** 档次 · 决定 vendor 显示真名还是匿名编号（只 wholesale 看真名 · docs/10-pricing §2.1） */
  const tier = me?.tier;

  /* 具体 vendor 的 stock（vendorId 是 auto 时不发请求） */
  const { data: stock } = useVendorStock(vendorId === "auto" ? undefined : vendorId);
  /* auto 模式的系统派号推荐 · 提供最终价用于预估 */
  const { data: pick } = useAutoPick(zone);

  const isAuto = vendorId === "auto";

  /* 具体 zone 的单价（用于预估）· auto 时选最便宜一区 */
  const activeZone = useMemo(() => {
    if (!stock) return null;
    if (stock.zones.length === 1) return stock.zones[0];
    if (zone === "auto") {
      return stock.zones.reduce((a, b) => (a.unit_price <= b.unit_price ? a : b));
    }
    return stock.zones.find((z) => z.zone === zone) ?? stock.zones[0];
  }, [stock, zone]);

  /** 服务端给的当前单价 · auto 走推荐结果 · 具体 vendor 走该区单价 */
  const unitPrice = isAuto ? pick?.unit_price ?? null : activeZone?.unit_price ?? null;
  const available = isAuto ? pick?.available ?? null : activeZone?.available ?? null;

  /** 可买上限 = min(vendor.max_per_order, available) · 都从后端来
   *  - available = 该 vendor 实际库存(auto 走 pick.available · 具体 vendor 走 zone.available)
   *  - max_per_order = vendor 每单上限(可能不设 · 那就只受 available 约束)
   *  - 数据未到手时(available/max 都 null)· 保底 1 让输入框可用 · 加载完就到真值
   *  ⚠️ 不写死 200 · 用户能买超总量是核心 bug */
  const rawMax = isAuto ? pick?.max_per_order : stock?.max_per_order;
  const rawMin = isAuto ? pick?.min_per_order : stock?.min_per_order;
  const capMax = rawMax != null && rawMax > 0 ? rawMax : null;
  const capAvail = available != null && available > 0 ? available : null;
  let maxCount = 1; // 保底 · 数据未到手时不锁死输入
  if (capMax != null && capAvail != null) maxCount = Math.min(capMax, capAvail);
  else if (capMax != null) maxCount = capMax;
  else if (capAvail != null) maxCount = capAvail;
  const minCount = rawMin && rawMin > 0 ? rawMin : 1;
  /** 实际会派到的 vendor 显示名（auto 时来自推荐结果） */
  const effectiveVendorLabel = isAuto
    ? pick?.vendor_label ?? t("pull-form.vendor.auto-fallback")
    : vendorLabel(vendorId, tier);
  const effectiveZone = isAuto ? pick?.zone ?? null : activeZone ? (stock!.zones.length === 1 ? null : activeZone.zone) : null;

  /* 预估费用 · 走后端 /me/pull/estimate（对外只三项：unit_price / service_fee / total） */
  const estimateMut = useEstimate();
  const [estimate, setEstimate] = useState<{ unit_price: number; service_fee: number; total: number } | null>(null);
  useEffect(() => {
    if (unitPrice == null || isAuto || vendorId === "auto") {
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
  }, [vendorId, effectiveZone, count, unitPrice, isAuto]);

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
            <Select value={vendorId} onValueChange={setVendorId}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {/* 默认项 = 系统派号 · 面板里展示推荐结果和价格（decisions §8.20） */}
                <SelectItem value="auto">{t("pull-form.vendor.auto")}</SelectItem>
                {availableVendors.map((v) => (
                  <SelectItem key={v.vendor_id} value={v.vendor_id}>
                    {/* 后端已按 tier 判过匿名 · 直接用 vendor_label · 不重算 */}
                    {v.vendor_label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label={t("pull-form.field.subscription")}>
            <Select
              value={subscription}
              onValueChange={setSubscription}
              disabled={subscriptionOptions.length === 1}
            >
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {subscriptionOptions.map((s) => (
                  <SelectItem key={s.value} value={s.value}>{s.label}</SelectItem>
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
