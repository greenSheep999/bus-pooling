import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { KeyRound, Sparkles } from "lucide-react";
import {
  useGlobalStrategy, useMe, usePullForBus, useVendorStats,
} from "@/api/hooks";
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
import {
  toCredits, vendorLabel,
} from "@/lib/utils";

/** 车详情立即拉号模态 · 参数用车策略默认，允许覆盖 */
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
  const { data: me } = useMe();
  const pull = usePullForBus(busId);
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  const [count, setCount] = useState(defaultCount);
  const [vendorId, setVendorId] = useState(preferredVendor ?? "auto");

  useEffect(() => {
    if (open) {
      setCount(defaultCount);
      setVendorId(preferredVendor ?? "auto");
    }
  }, [open, defaultCount, preferredVendor]);

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

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const picked = vendorId === "auto" ? undefined : vendorId;
    await pull.mutateAsync({ count, vendor_id: picked });
    onClose();
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
            <Field label={t("pull-now-modal.field-count")}>
              <Input
                type="number" min={1} max={200} value={count}
                onChange={(e) => setCount(Math.max(1, Math.min(200, Number(e.target.value) || 1)))}
              />
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
              <Select value={vendorId} onValueChange={setVendorId}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">{t("pull-now-modal.vendor-auto")}</SelectItem>
                  {availableVendors.map((v) => (
                    <SelectItem key={v.vendor_id} value={v.vendor_id}>
                      {vendorLabel(v.vendor_id, me?.tier)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>

            {bargain && (
              <Alert tone="neutral" icon={Sparkles} title={t("pull-now-modal.bargain-alert-title")}>
                {t("pull-now-modal.bargain-alert-body")}
              </Alert>
            )}
          </form>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>{t("pull-now-modal.cancel")}</Button>
          <Button type="submit" form="pull-now-form" disabled={pull.isPending}>
            {pull.isPending ? t("pull-now-modal.submit-pending") : t("pull-now-modal.submit", { count })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
