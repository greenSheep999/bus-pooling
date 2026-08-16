import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { ArrowUpRight, KeyRound, Sparkles, TrendingUp } from "lucide-react";
import { useExtract, useEstimate } from "@/api/hooks";
import { useOfferSelection } from "@/hooks/useOfferSelection";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { UpstreamStatusPanel } from "@/components/UpstreamStatusPanel";
import { ExtractConfirmModal } from "@/components/ExtractConfirmModal";
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
  const [confirmOpen, setConfirmOpen] = useState(false);

  /** Offer 维度取数与派生 · 跟车内立即拉号弹窗共用（docs/24 §3） */
  const {
    tier,
    count, setCount,
    vendorId, setVendorId,
    zone, setZone,
    subscription, setSubscription,
    setPlanPicked,
    availableVendors,
    subscriptionOptions,
    selectedVendorOOS,
    selectedSubOOS,
    isAuto,
    pick,
    unitPrice,
    bandUnitPrice,
    qtyDiscount,
    maxCount,
    minCount,
    warrantyMinutes,
    outOfStock,
    effectiveZone,
  } = useOfferSelection({ category });

  /** 缺货 pill 文案 · **不写 defaultValue** —— 中文兜底会让英文用户看到中文
   *  且 key 缺失时静默不报（实测踩过）· 缺 key 就让它显示 key 名 · 一眼能发现 */
  const outOfStockLabel = t("pull-form.vendor.out-of-stock");

  /** 实际会派到的 vendor 显示名（auto 时来自推荐结果）· 我方自营那家过 i18n */
  const effectiveVendorLabel = isAuto
    ? pick?.vendor_label ?? t("pull-form.vendor.auto-fallback")
    : I18N_VENDOR_IDS.has(vendorId)
      ? tVendor(vendorId, { defaultValue: vendorLabel(vendorId, tier) })
      : vendorLabel(vendorId, tier);

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
          warrantyMinutes,
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
