import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { KeyRound, Sparkles } from "lucide-react";
import { useGlobalStrategy, useEstimate, usePullForBus } from "@/api/hooks";
import { useOfferSelection } from "@/hooks/useOfferSelection";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { notify } from "@/lib/toast";
import { fmtCredits, toCredits } from "@/lib/utils";

/** 车详情立即拉号模态 · 参数用车策略默认，允许覆盖
 *  §8.45 · Offer 维度(企业/个人 · vendor · 档位 · 真库存)跟 Extract 页共用取数(useOfferSelection) */
export function PullNowModal({
  open, onClose, busId, defaultCount, preferredVendor, maxUnitPrice,
}: {
  open: boolean;
  onClose: () => void;
  busId: string;
  defaultCount: number;
  preferredVendor: string | null;
  maxUnitPrice: number | null;
}) {
  const { t } = useTranslation("buses");
  /** vendor 展示名的翻译 · 只我方自营那家要翻(前 6 家品牌名 / 匿名编号都不翻) */
  const pull = usePullForBus(busId);

  /** 车里也能拉个人号 · 企业/个人自带切换 · 默认企业(兼容原行为) */
  const [category, setCategory] = useState<"enterprise" | "personal">("enterprise");
  const {
    count, setCount,
    vendorId, setVendorId,
    subscription, setSubscription, setPlanPicked,
    availableVendors, subscriptionOptions,
    selectedVendorOOS, selectedSubOOS,
    isAuto, unitPrice, bandUnitPrice, qtyDiscount,
    maxCount, minCount, outOfStock, effectiveZone,
  } = useOfferSelection({
    category,
    seedCount: defaultCount,
    seedVendor: preferredVendor ?? undefined,
  });

  /** 缺货 pill 文案 · **不写 defaultValue**（中文兜底会让英文用户看到中文） */
  const outOfStockLabel = t("pull-now-modal.out-of-stock");

  /** 开窗时用车级策略播种:defaultCount 作数量种子 · preferredVendor 作 vendor 种子
   *  种子 vendor 在当前档下 supported=false(不在 availableVendors)时回落 "auto" ·
   *  否则 SelectValue 空白。offers 未到手先按 open 播一次 · 到手后补播一次(seededRef 防抖) */
  const seededRef = useRef(false);
  useEffect(() => {
    if (!open) { seededRef.current = false; return; }
    if (seededRef.current) return;
    setCount(defaultCount);
    if (preferredVendor && availableVendors.some((v) => v.vendor_id === preferredVendor)) {
      setVendorId(preferredVendor);
      seededRef.current = true;
    } else if (availableVendors.length > 0) {
      // offers 已到手但没匹配到偏好 vendor · 定格 auto · 不再重播
      setVendorId("auto");
      seededRef.current = true;
    } else {
      // offers 还没到手 · 先按偏好试(下一次 availableVendors 变化再校正)
      setVendorId(preferredVendor ?? "auto");
    }
  }, [open, defaultCount, preferredVendor, availableVendors, setCount, setVendorId]);

  const bargain = count === 1;

  /* 生效的单价上限 = 车级和全局取更严的那个（AND 关系 · decisions §8.27）
     只展示不拦：这个弹窗看不到成交价（比价在后端），真正的拦在后端和提取确认窗 */
  const { data: gs } = useGlobalStrategy();
  const globalCap = gs?.max_unit_price ?? null;
  const effectiveCap =
    maxUnitPrice != null && globalCap != null ? Math.min(maxUnitPrice, globalCap)
      : maxUnitPrice ?? globalCap;
  const capFrom = effectiveCap == null ? null
    : effectiveCap === globalCap && (maxUnitPrice == null || globalCap! < maxUnitPrice) ? "global"
      : "bus";

  /* 预估费用 · 走后端 /me/pull/estimate（对外只三项：unit_price / service_fee / total）
     跟 PullExtractForm 一致:非 auto 且无数量分档才打后端 · 否则本地 unitPrice × count 估 */
  const estimateMut = useEstimate();
  const [estimate, setEstimate] = useState<{ unit_price: number; service_fee: number; total: number } | null>(null);
  useEffect(() => {
    if (unitPrice == null || isAuto || bandUnitPrice != null) {
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

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    // vendor_id/zone 的 "auto" 归一由后端做(bus.go)· 前端 auto 时不传 · 省一次约束
    try {
      const r = await pull.mutateAsync({
        count,
        vendor_id: isAuto ? undefined : vendorId,
        account_kind: category,
        plan: (subscription || undefined) as "power" | "pro" | "pro_plus" | "pro_max" | undefined,
      });
      notify.ok({
        title: t("common:toast.pull_ok_title", { count: r.purchased }),
        desc: t("common:toast.pull_ok_desc", { amount: fmtCredits(r.total_debit) }),
      });
      onClose();
    } catch (err) {
      notify.fail(err, t("common:toast.extract_fail"));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[440px]">
        <DialogHeader>
          <DialogTitle>
            <span className="inline-flex items-center gap-2">
              <KeyRound className="size-4 text-brand-strong" />
              {t("pull-now-modal.title")}
            </span>
          </DialogTitle>
        </DialogHeader>

        <DialogBody>
          <form id="pull-now-form" onSubmit={onSubmit} className="space-y-4">
            {/* 企业 / 个人 · 车里也能拉个人号 */}
            <Field label={t("pull-now-modal.field-kind")}>
              <Tabs value={category} onValueChange={(v) => setCategory(v as "enterprise" | "personal")}>
                <TabsList className="w-full">
                  <TabsTrigger value="enterprise">{t("pull-now-modal.kind-enterprise")}</TabsTrigger>
                  <TabsTrigger value="personal">{t("pull-now-modal.kind-personal")}</TabsTrigger>
                </TabsList>
              </Tabs>
            </Field>

            <Field
              label={t("pull-now-modal.field-vendor")}
              hint={
                effectiveCap != null ? (
                  <>
                    {t("pull-now-modal.hint-cap-prefix")}<span className="font-semibold tnum">{toCredits(effectiveCap)}</span>{t("pull-now-modal.hint-cap-suffix")}
                    {capFrom === "global" && <span className="text-fg-tertiary">{t("pull-now-modal.hint-cap-global")}</span>}
                  </>
                ) : undefined
              }
            >
              {/* 换 vendor 要重新评估档位默认值 · 缺货 vendor 也列(disabled + 缺货 pill · 不隐藏) */}
              <Select value={vendorId} onValueChange={(v) => { setPlanPicked(false); setVendorId(v); }}>
                <SelectTrigger hint={selectedVendorOOS ? outOfStockLabel : undefined}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">{t("pull-now-modal.vendor-auto")}</SelectItem>
                  {availableVendors.map((v) => (
                    <SelectItem
                      key={v.vendor_id}
                      value={v.vendor_id}
                      disabled={v.available === 0}
                      hint={v.available === 0 ? outOfStockLabel : undefined}
                    >
                      {v.vendor_label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            {/* 档位 · 缺货 disabled + 缺货 pill(不隐藏)· 跟 Extract 页一致 */}
            <Field label={t("pull-now-modal.field-subscription")}>
              <Select value={subscription} onValueChange={(v) => { setPlanPicked(true); setSubscription(v); }}>
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

            {/* 数量 · max 用真库存收紧(不再写死 200) */}
            <Field label={t("pull-now-modal.field-count")} hint={t("pull-now-modal.count-hint", { min: minCount, max: maxCount })}>
              <Input
                type="number" min={1} max={maxCount} value={count}
                onChange={(e) => setCount(Math.max(1, Math.min(maxCount, Number(e.target.value) || 1)))}
              />
            </Field>

            {/* 预估费用 · 对外只单价 / 服务费 / 小计（decisions §8.20） */}
            {estimate && (
              <div className="rounded-xl border border-hairline bg-bg-elevated/40 p-3 text-label">
                <FeeRow
                  label={t("pull-now-modal.estimate-unit", { count })}
                  value={t("pull-now-modal.estimate-unit-value", { unit: toCredits(estimate.unit_price), count })}
                />
                {qtyDiscount && (
                  <FeeRow
                    label={t("pull-now-modal.estimate-qty-discount")}
                    value={
                      <span className="text-ok-fg">
                        {t("pull-now-modal.estimate-qty-discount-value", {
                          base: toCredits(qtyDiscount.base),
                          saved: fmtCredits(qtyDiscount.saved),
                        })}
                      </span>
                    }
                  />
                )}
                {estimate.service_fee > 0 && (
                  <FeeRow
                    label={t("pull-now-modal.estimate-service-fee")}
                    value={t("pull-now-modal.estimate-service-fee-value", { value: fmtCredits(estimate.service_fee) })}
                    muted
                  />
                )}
                <div className="mt-2 border-t border-hairline pt-2">
                  <FeeRow
                    label={t("pull-now-modal.estimate-total")}
                    value={
                      <strong className="tnum text-fg">
                        {t("pull-now-modal.estimate-total-value", { value: fmtCredits(estimate.total) })}
                      </strong>
                    }
                    strong
                  />
                </div>
              </div>
            )}

            {bargain && (
              <Alert tone="neutral" icon={Sparkles} title={t("pull-now-modal.bargain-alert-title")}>
                {t("pull-now-modal.bargain-alert-body")}
              </Alert>
            )}
          </form>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>{t("pull-now-modal.cancel")}</Button>
          <Button type="submit" form="pull-now-form" disabled={pull.isPending || outOfStock}>
            {pull.isPending
              ? t("pull-now-modal.submit-pending")
              : outOfStock
                ? t("pull-now-modal.submit-out-of-stock")
                : t("pull-now-modal.submit", { count })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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
    <div className="flex items-baseline justify-between gap-3 py-0.5">
      <span className={muted ? "text-fg-tertiary" : "text-fg-secondary"}>{label}</span>
      <span className={strong ? "font-semibold" : "tnum text-fg-secondary"}>{value}</span>
    </div>
  );
}
