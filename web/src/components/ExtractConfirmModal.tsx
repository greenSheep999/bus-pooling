import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle, KeyRound, ShieldAlert, Tag } from "lucide-react";
import { Link } from "react-router-dom";
import { useGlobalStrategy } from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { fmtCredits, toCredits } from "@/lib/utils";
import type { Money, Zone } from "@/types";

export interface ExtractConfirmInfo {
  /** 会派到的 vendor 显示名（真名 or 匿名编号） */
  vendorLabel: string;
  /** 是否走系统派号 */
  isAuto: boolean;
  zone: Zone | null;
  count: number;
  /** 服务端给的最终单价 · 填优惠码后由父层重算 */
  unitPrice: Money | null;
  warrantyMinutes: number;
  /** 后端 estimate 结果（对外三项）· 父层用 useEstimate 拿 */
  estimate?: { unit_price: Money; service_fee: Money; total: Money } | null;
}

/** 提取确认窗 · decisions §8.20
 *  点「提取」不直接拉 → 先复核信息 + 可填优惠码 → 确认才真拉
 *  优惠码在这里填（不放外层表单）· 注册时叫「邀请码」· 这里叫「优惠码」 */
export function ExtractConfirmModal({
  open, onClose, onConfirm, pending, info,
}: {
  open: boolean;
  onClose: () => void;
  onConfirm: (couponCode?: string) => void | Promise<void>;
  pending: boolean;
  info: ExtractConfirmInfo;
}) {
  const { t } = useTranslation("extract");
  const [coupon, setCoupon] = useState("");
  const [applied, setApplied] = useState<string | null>(null);

  useEffect(() => {
    if (open) { setCoupon(""); setApplied(null); }
  }, [open]);

  const code = coupon.trim();

  /* 费用来自后端 · 对外三项（unit_price / service_fee / total）· 无本地定价
     优惠码折后价在后端 estimate 里算（1a 未接优惠码计算 · 参数带上但目前不减免） */
  const est = info.estimate;
  const total = est?.total ?? (info.unitPrice != null ? info.unitPrice * info.count : 0);
  const svcFee = est?.service_fee ?? 0;
  const discounted = est?.unit_price ?? info.unitPrice;

  /* 全局单价上限（decisions §8.27）· 超了不给确认
     判的是**优惠码折后价** —— 用码压到线内就该放行 */
  const { data: gs } = useGlobalStrategy();
  const cap = gs?.max_unit_price ?? null;
  const overCap = cap != null && discounted != null && discounted > cap;

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-[520px]">
        <DialogHeader>
          <DialogTitle>
            <span className="inline-flex items-center gap-2">
              <KeyRound className="size-4 text-brand-strong" />
              {t("confirm-modal.title")}
            </span>
          </DialogTitle>
        </DialogHeader>

        <DialogBody className="space-y-5">
          {/* 复核信息 */}
          <div className="rounded-xl border border-hairline bg-bg-elevated/40 p-4">
            <div className="mb-3 text-label font-semibold text-fg">{t("confirm-modal.info-title")}</div>
            <dl className="space-y-2 text-label">
              <InfoRow
                label={t("confirm-modal.info-source")}
                value={
                  <>
                    {info.vendorLabel}
                    {info.isAuto && <span className="ml-1.5 text-fg-tertiary">{t("confirm-modal.info-auto-suffix")}</span>}
                  </>
                }
              />
              <InfoRow label={t("confirm-modal.info-zone")} value={info.zone ?? t("confirm-modal.info-zone-all")} />
              <InfoRow
                label={t("confirm-modal.info-count")}
                value={<><strong className="tnum">{info.count}</strong> {t("confirm-modal.info-count-unit")}</>}
              />
              <InfoRow
                label={t("confirm-modal.info-unit-price")}
                value={
                  discounted != null ? (
                    <>
                      <strong className="tnum">{toCredits(discounted)}</strong> {t("confirm-modal.info-unit-price-suffix")}
                      {applied && info.unitPrice != null && (
                        <span className="ml-2 text-fg-tertiary line-through tnum">
                          {toCredits(info.unitPrice)}
                        </span>
                      )}
                    </>
                  ) : (
                    t("confirm-modal.info-dash")
                  )
                }
              />
              <InfoRow
                label={t("confirm-modal.info-warranty")}
                value={
                  info.warrantyMinutes > 0
                    ? <><strong className="tnum">{info.warrantyMinutes}</strong> {t("confirm-modal.info-warranty-prefix")}</>
                    : <span className="text-warn-fg">{t("confirm-modal.info-warranty-none")}</span>
                }
              />
            </dl>
          </div>

          {/* 优惠码 */}
          <Field label={t("confirm-modal.coupon-label")} hint={applied ? t("confirm-modal.coupon-hint-applied") : t("confirm-modal.coupon-hint-optional")}>
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Tag className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-fg-tertiary" />
                <Input
                  value={coupon}
                  onChange={(e) => { setCoupon(e.target.value); setApplied(null); }}
                  placeholder={t("confirm-modal.coupon-placeholder")}
                  className="pl-9"
                />
              </div>
              <Button
                type="button"
                variant="ghost"
                disabled={!code || applied === code}
                onClick={() => setApplied(code)}
              >
                {applied === code ? t("confirm-modal.coupon-btn-applied") : t("confirm-modal.coupon-btn-apply")}
              </Button>
            </div>
          </Field>

          {/* 合计 · 对外只展示单价 × 数量 / 服务费 / 小计（不展示计费分项 · §8.20） */}
          <div className="space-y-1.5 rounded-xl bg-bg-elevated p-4 text-label">
            <FeeRow
              label={discounted != null ? `${t("confirm-modal.fee-unit-label")} · ${toCredits(discounted)} × ${info.count}` : t("confirm-modal.fee-unit-label")}
              value={`${fmtCredits(discounted != null ? discounted * info.count : 0)} ${t("confirm-modal.fee-credits")}`}
            />
            {svcFee > 0 && (
              <FeeRow label={t("confirm-modal.fee-service")} value={`${fmtCredits(svcFee)} ${t("confirm-modal.fee-credits")}`} muted />
            )}
            <div className="mt-2 border-t border-hairline pt-2">
              <FeeRow
                label={t("confirm-modal.fee-total")}
                value={<strong className="tnum text-fg">{fmtCredits(total)} {t("confirm-modal.fee-credits")}</strong>}
                strong
              />
            </div>
          </div>

          {overCap ? (
            /* 超了你自己设的上限 · 拦住 · 说清超多少、去哪儿改（不给"就这次放行"的口子，
               要放行就去改上限 —— 免得护栏形同虚设） */
            <Alert tone="danger" icon={ShieldAlert} title={t("confirm-modal.cap-alert-title")}>
              {t("confirm-modal.cap-alert-prefix")}<strong className="tnum">{toCredits(discounted ?? 0)}</strong>
              {t("confirm-modal.cap-alert-mid1")}<strong className="tnum">{toCredits(cap!)}</strong>
              {t("confirm-modal.cap-alert-mid2")}<strong className="tnum">{toCredits((discounted ?? 0) - cap!)}</strong>
              <div className="mt-1">
                {t("confirm-modal.cap-alert-tail")}
                <Link
                  to="/settings/preferences"
                  className="mx-1 font-semibold text-brand-strong hover:underline"
                >
                  {t("confirm-modal.cap-alert-link")}
                </Link>
                {t("confirm-modal.cap-alert-tail-suffix")}
              </div>
            </Alert>
          ) : (
            <Alert tone="warn" icon={AlertTriangle}>
              {t("confirm-modal.warn-alert")}
            </Alert>
          )}
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>{t("confirm-modal.cancel")}</Button>
          <Button
            type="button"
            variant="primary"
            disabled={pending || overCap}
            onClick={() => onConfirm(applied ?? undefined)}
          >
            <KeyRound />
            {overCap ? t("confirm-modal.confirm-over-cap") : pending ? t("confirm-modal.confirm-pending") : t("confirm-modal.confirm-submit", { count: info.count })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <dt className="text-fg-tertiary">{label}</dt>
      <dd className="min-w-0 truncate text-right font-medium text-fg">{value}</dd>
    </div>
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
