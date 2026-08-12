import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Info, Sparkles, Zap } from "lucide-react";
import {
  useCreateBus, useMe, useVendorStats,
} from "@/api/hooks";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Switch } from "@/components/ui/switch";
import { CollapsiblePanel } from "@/components/ui/collapsible";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  vendorLabel,
} from "@/lib/utils";

/** 发起拼车模态：建车 + 首次拉号一步完成
    - 基本：车名 · 数量 · vendor（可让系统选 · 同时作为策略里的 preferred_vendor）· count=1 单价提示
    - 主开关：自动补车（放高级选项外 · 用户先看到关键决策）
    - 高级折叠：3 个上限（单价 · 日轮次 · 日花费） */
export function StartCarpoolModal({
  open, onClose,
}: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  const nav = useNavigate();
  const createBus = useCreateBus();
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  const defaultName = t("start-modal.default-bus-name");
  const [name, setName] = useState(defaultName);
  const [count, setCount] = useState(3);
  const [vendorId, setVendorId] = useState<string>("auto");
  const [autoRefill, setAutoRefill] = useState(false);
  const [maxPrice, setMaxPrice] = useState("");
  const [dailyRoundLimit, setDailyRoundLimit] = useState("");
  const [dailySpendLimit, setDailySpendLimit] = useState("");
  const [refillWatermark, setRefillWatermark] = useState(3);
  const [perRoundCount, setPerRoundCount] = useState(3);

  // 打开时重置
  useEffect(() => {
    if (open) {
      setName(defaultName);
      setCount(3);
      setVendorId("auto");
      setAutoRefill(false);
      setMaxPrice("");
      setDailyRoundLimit("");
      setDailySpendLimit("");
    }
  }, [open, defaultName]);

  const bargain = count === 1;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const picked = vendorId === "auto" ? null : vendorId;
    const strategy = {
      auto_refill_enabled: autoRefill,
      refill_watermark: refillWatermark,
      refill_min_count: null,
      per_round_count: perRoundCount,
      max_unit_price: maxPrice ? Number(maxPrice) * 1_000_000 : null,
      daily_round_limit: dailyRoundLimit ? Number(dailyRoundLimit) : null,
      daily_spend_limit: dailySpendLimit ? Number(dailySpendLimit) * 1_000_000 : null,
      preferred_vendor: picked,
    };
    // 建车不传 kind —— 用户建的车都一样（后端默认 single·跟 team 行为一致·都带邀请码）。
    // 1 个人时是独享·把邀请码给朋友进来就是拼车·不需要建车时选类型。
    // max_members 走后端 config.bus.max_members·前端不传。
    const bus = await createBus.mutateAsync({ name, strategy });
    onClose();
    nav(`/buses/${bus.id}`);
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("start-modal.title")}</DialogTitle>
          <DialogDescription>{t("start-modal.desc")}</DialogDescription>
        </DialogHeader>

        <DialogBody>
          <form id="start-carpool-form" onSubmit={onSubmit} className="space-y-5">
            <Field label={t("start-modal.field-name")}>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t("start-modal.name-placeholder")}
                required
                maxLength={30}
              />
            </Field>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-[140px_minmax(0,1fr)]">
              <Field label={t("start-modal.field-count")}>
                <Input
                  type="number"
                  min={1}
                  max={200}
                  value={count}
                  onChange={(e) => setCount(Math.max(1, Math.min(200, Number(e.target.value) || 1)))}
                />
              </Field>
              <Field label={t("start-modal.field-vendor")}>
                <Select value={vendorId} onValueChange={setVendorId}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="auto">{t("start-modal.vendor-auto")}</SelectItem>
                    {availableVendors.map((v) => (
                      <SelectItem key={v.vendor_id} value={v.vendor_id}>
                        {vendorLabel(v.vendor_id, me?.tier)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </div>

            {/* 单价提示 · 向下省视角（不用内部计费术语） */}
            {bargain ? (
              <Alert tone="neutral" icon={Sparkles} title={t("start-modal.bargain-alert-title")}>
                {t("start-modal.bargain-alert-body")}
              </Alert>
            ) : (
              <Alert tone="ok" icon={Sparkles} title={t("start-modal.cheap-alert-title")}>
                {t("start-modal.cheap-alert-body-prefix")}<span className="font-semibold tnum">{count}</span>{t("start-modal.cheap-alert-body-suffix")}
              </Alert>
            )}

            {/* 主开关 · 是否开启自动补车 */}
            <div
              className="flex cursor-pointer items-center gap-3 rounded-2xl border border-hairline bg-bg p-4 transition-colors hover:bg-bg-elevated/40"
              onClick={() => setAutoRefill((v) => !v)}
            >
              <span className="shrink-0">
                {autoRefill ? (
                  <Zap className="size-4 text-brand-strong" />
                ) : (
                  <Zap className="size-4 text-fg-tertiary" />
                )}
              </span>
              <div className="min-w-0 flex-1">
                <div className="font-semibold">{t("start-modal.auto-refill-title")}</div>
                <div className="mt-0.5 text-label text-fg-tertiary">
                  {t("start-modal.auto-refill-desc")}
                </div>
              </div>
              <Switch
                checked={autoRefill}
                onCheckedChange={setAutoRefill}
                onClick={(e) => e.stopPropagation()}
              />
            </div>

            {autoRefill && (
              <div className="grid grid-cols-2 gap-4">
                <Field label={t("start-modal.field-watermark")}>
                  <Input
                    type="number"
                    min={1}
                    value={refillWatermark}
                    onChange={(e) => setRefillWatermark(Math.max(1, Number(e.target.value) || 1))}
                  />
                </Field>
                <Field label={t("start-modal.field-per-round")}>
                  <Input
                    type="number"
                    min={1}
                    value={perRoundCount}
                    onChange={(e) => setPerRoundCount(Math.max(1, Number(e.target.value) || 1))}
                  />
                </Field>
              </div>
            )}

            <CollapsiblePanel title={t("start-modal.advanced-title")} subtitle={t("start-modal.advanced-sub")}>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                <Field label={t("start-modal.field-max-price")}>
                  <Input
                    type="number"
                    value={maxPrice}
                    onChange={(e) => setMaxPrice(e.target.value)}
                    placeholder={t("start-modal.no-limit")}
                  />
                </Field>
                <Field label={t("start-modal.field-daily-round")}>
                  <Input
                    type="number"
                    value={dailyRoundLimit}
                    onChange={(e) => setDailyRoundLimit(e.target.value)}
                    placeholder={t("start-modal.no-limit")}
                  />
                </Field>
                <Field label={t("start-modal.field-daily-spend")}>
                  <Input
                    type="number"
                    value={dailySpendLimit}
                    onChange={(e) => setDailySpendLimit(e.target.value)}
                    placeholder={t("start-modal.no-limit")}
                  />
                </Field>
              </div>
            </CollapsiblePanel>

            <Alert tone="brand" icon={Info}>
              {t("start-modal.tip-prefix")}<span className="font-semibold text-fg">{count}</span>{t("start-modal.tip-mid")}
            </Alert>
          </form>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>{t("start-modal.cancel")}</Button>
          <Button
            type="submit"
            form="start-carpool-form"
            disabled={createBus.isPending}
          >
            {createBus.isPending ? t("start-modal.submit-pending") : t("start-modal.submit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
