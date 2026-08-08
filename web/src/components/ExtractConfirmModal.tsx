import { useEffect, useState } from "react";
import { AlertTriangle, KeyRound, ShieldAlert, Tag } from "lucide-react";
import { Link } from "react-router-dom";
import { useGlobalStrategy, useMe } from "@/api/hooks";
import {
  Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { fmtCredits, serviceFee, toCredits } from "@/lib/utils";
import type { Money, Zone } from "@/types";

export interface ExtractConfirmInfo {
  /** 会派到的 vendor 显示名（真名 or 匿名编号） */
  vendorLabel: string;
  /** 是否走系统派号 */
  isAuto: boolean;
  zone: Zone | null;
  count: number;
  /** 最终单价（含附加费）· 填优惠码后由父层重算 */
  unitPrice: Money | null;
  warrantyMinutes: number;
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
  const [coupon, setCoupon] = useState("");
  const [applied, setApplied] = useState<string | null>(null);
  const { data: me } = useMe();

  useEffect(() => {
    if (open) { setCoupon(""); setApplied(null); }
  }, [open]);

  const code = coupon.trim();

  /* 优惠码折扣 · 阶段 1a 前端按已知规则预览（真实减免由后端裁定）
     无注册邀请码时默认价含附加费 · 用码可减免那部分 */
  const discounted = applied && info.unitPrice != null
    ? Math.round(info.unitPrice / 1.2)
    : info.unitPrice;

  const keyCost = (discounted ?? 0) * info.count;
  const singlePullFee = info.count === 1 ? keyCost * 0.2 : 0;
  /* 服务费两档（§8.31）· 社群 1 积分 / 零售 7 积分
     注意用 me.invited（系统邀请码），**不看优惠码** —— 优惠码只免加价，不改服务费档位 */
  const svcFee = serviceFee(me?.invited);
  const total = keyCost + singlePullFee + svcFee;

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
              确认提取
            </span>
          </DialogTitle>
        </DialogHeader>

        <DialogBody className="space-y-5">
          {/* 复核信息 */}
          <div className="rounded-xl border border-hairline bg-bg-elevated/40 p-4">
            <div className="mb-3 text-label font-semibold text-fg">提取信息</div>
            <dl className="space-y-2 text-label">
              <InfoRow
                label="来源"
                value={
                  <>
                    {info.vendorLabel}
                    {info.isAuto && <span className="ml-1.5 text-fg-tertiary">系统派</span>}
                  </>
                }
              />
              <InfoRow label="区域" value={info.zone ?? "全区"} />
              <InfoRow
                label="数量"
                value={<><strong className="tnum">{info.count}</strong> 个</>}
              />
              <InfoRow
                label="单价"
                value={
                  discounted != null ? (
                    <>
                      <strong className="tnum">{toCredits(discounted)}</strong> 积分 / 个
                      {applied && info.unitPrice != null && (
                        <span className="ml-2 text-fg-tertiary line-through tnum">
                          {toCredits(info.unitPrice)}
                        </span>
                      )}
                    </>
                  ) : (
                    "—"
                  )
                }
              />
              <InfoRow
                label="质保"
                value={
                  info.warrantyMinutes > 0
                    ? <><strong className="tnum">{info.warrantyMinutes}</strong> 分钟内失效可退</>
                    : <span className="text-warn-fg">无质保</span>
                }
              />
            </dl>
          </div>

          {/* 优惠码 */}
          <Field label="优惠码" hint={applied ? "已应用" : "选填"}>
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Tag className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-fg-tertiary" />
                <Input
                  value={coupon}
                  onChange={(e) => { setCoupon(e.target.value); setApplied(null); }}
                  placeholder="有优惠码可减免"
                  className="pl-9"
                />
              </div>
              <Button
                type="button"
                variant="ghost"
                disabled={!code || applied === code}
                onClick={() => setApplied(code)}
              >
                {applied === code ? "已应用" : "应用"}
              </Button>
            </div>
          </Field>

          {/* 合计 */}
          <div className="space-y-1.5 rounded-xl bg-bg-elevated p-4 text-label">
            <FeeRow
              label={discounted != null ? `号价 · ${toCredits(discounted)} × ${info.count}` : "号价"}
              value={`${fmtCredits(keyCost)} 积分`}
            />
            {singlePullFee > 0 && (
              <FeeRow label="拉 1 个偏高" value={`+${fmtCredits(singlePullFee)} 积分`} muted />
            )}
            <FeeRow label="服务费" value={`${toCredits(svcFee)} 积分`} muted />
            <div className="mt-2 border-t border-hairline pt-2">
              <FeeRow
                label="合计扣除"
                value={<strong className="tnum text-fg">{fmtCredits(total)} 积分</strong>}
                strong
              />
            </div>
          </div>

          {overCap ? (
            /* 超了你自己设的上限 · 拦住 · 说清超多少、去哪儿改（不给"就这次放行"的口子，
               要放行就去改上限 —— 免得护栏形同虚设） */
            <Alert tone="danger" icon={ShieldAlert} title="超过你设的单价上限">
              现在 <strong className="tnum">{toCredits(discounted ?? 0)}</strong> 积分 / 个 ·
              你的上限是 <strong className="tnum">{toCredits(cap!)}</strong> ·
              超了 <strong className="tnum">{toCredits((discounted ?? 0) - cap!)}</strong>
              <div className="mt-1">
                等价格降下来 · 或者去
                <Link
                  to="/settings/preferences"
                  className="mx-1 font-semibold text-brand-strong hover:underline"
                >
                  拉号偏好
                </Link>
                调高上限
              </div>
            </Alert>
          ) : (
            <Alert tone="warn" icon={AlertTriangle}>
              价格受市场波动影响 · 实际扣除以成交为准 · 确认前请核对上面信息
            </Alert>
          )}
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>取消</Button>
          <Button
            type="button"
            variant="brand"
            disabled={pending || overCap}
            onClick={() => onConfirm(applied ?? undefined)}
          >
            <KeyRound />
            {overCap ? "超过上限 · 拉不了" : pending ? "提取中…" : `确认提取 ${info.count} 个`}
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
