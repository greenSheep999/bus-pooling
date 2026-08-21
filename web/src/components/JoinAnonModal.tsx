import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Sparkles, Users } from "lucide-react";
import { useCreateBus, useMatchAnonBus } from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { notify } from "@/lib/toast";

/**
 * 搭车（anon）模态：
 *   1. 收 zone + max_unit_price 上限
 *   2. 调 anon/match auto_join=true
 *      - matched=true → 跳车详情（搭上了别人的车）
 *      - matched=false → 建一辆新 anon 车（当前用户是 owner · 让后来人来搭）
 *   两条路都能上车 · 没车友时你先开车·有车友时直接搭
 */
export function JoinAnonModal({
  open, onClose,
}: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation("buses");
  const nav = useNavigate();
  const match = useMatchAnonBus();
  const createBus = useCreateBus();
  const [zone, setZone] = useState<string>("cn");
  const [maxPriceCredits, setMaxPriceCredits] = useState<string>("");

  useEffect(() => {
    if (open) {
      setZone("cn");
      setMaxPriceCredits("");
    }
  }, [open]);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const maxPriceMicro = maxPriceCredits ? Number(maxPriceCredits) * 1_000_000 : undefined;
    try {
      const result = await match.mutateAsync({
        zone,
        max_unit_price: maxPriceMicro,
        auto_join: true,
      });
      if (result.matched && result.bus) {
        notify.ok({ title: t("common:toast.joined") });
        onClose();
        nav(`/buses/${result.bus.id}`);
        return;
      }
      // 没匹配 · 建新 anon 车 · 当前用户是 owner · 等后来人搭
      const bus = await createBus.mutateAsync({
        name: t("join-anon-modal.new-bus-name"),
        kind: "anon",
        max_members: 5,
        anon_zone: zone,
        anon_max_unit_price: maxPriceMicro,
      });
      notify.info({ title: t("common:toast.anon_created") });
      onClose();
      nav(`/buses/${bus.id}`);
    } catch (err: unknown) {
      // 提交错误走全局 toast · 通道统一（不在模态里另挂红条 · docs/13 §7.4）
      notify.fail(err, t("join-anon-modal.error-generic"));
    }
  };

  const pending = match.isPending || createBus.isPending;

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("join-anon-modal.title")}</DialogTitle>
          <DialogDescription>
            {t("join-anon-modal.desc")}
          </DialogDescription>
        </DialogHeader>

        <DialogBody>
          <form id="join-anon-form" onSubmit={onSubmit} className="space-y-5">
            <Field label={t("join-anon-modal.field-zone")}>
              <Select value={zone} onValueChange={setZone}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="cn">{t("join-anon-modal.zone-cn")}</SelectItem>
                  <SelectItem value="overseas">{t("join-anon-modal.zone-overseas")}</SelectItem>
                </SelectContent>
              </Select>
            </Field>

            <Field label={t("join-anon-modal.field-max-price")}>
              <Input
                type="number"
                min={0}
                value={maxPriceCredits}
                onChange={(e) => setMaxPriceCredits(e.target.value)}
                placeholder={t("join-anon-modal.max-price-placeholder")}
              />
            </Field>

            <Alert tone="brand" icon={Sparkles} title={t("join-anon-modal.how-title")}>
              {t("join-anon-modal.how-body")}
            </Alert>
            {/* 错误走 notify.fail · 这里不再挂 inline 红条 */}
          </form>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>{t("join-anon-modal.cancel")}</Button>
          <Button type="submit" form="join-anon-form" disabled={pending}>
            <Users className="size-4" />
            {pending ? t("join-anon-modal.submit-pending") : t("join-anon-modal.submit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
