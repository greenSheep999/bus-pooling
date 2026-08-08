import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowUpRight, KeyRound, Sparkles, TrendingUp } from "lucide-react";
import { useAutoPick, useExtract, useMe, useVendorStats, useVendorStock } from "@/api/hooks";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { UpstreamStatusPanel } from "@/components/UpstreamStatusPanel";
import { ExtractConfirmModal } from "@/components/ExtractConfirmModal";
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
}: {
  onSubmitted?: () => void;
  submitVariant?: "brand" | "primary";
  submitClassName?: string;
}) {
  const pull = useExtract();
  const { data: me } = useMe();
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  /** 有注册邀请码 = 社群成员 · 看 vendor 真名 + 无加价 · decisions §8.20 */
  const invited = !!me?.invited;

  const [count, setCount] = useState(3);
  const [vendorId, setVendorId] = useState<string>("auto");
  const [zone, setZone] = useState<Zone | "auto">("auto");
  const [confirmOpen, setConfirmOpen] = useState(false);

  /* 具体 vendor 的 stock（vendorId 是 auto 时不发请求） */
  const { data: stock } = useVendorStock(vendorId === "auto" ? undefined : vendorId);
  /* auto 模式的系统派号推荐 · 提供最终价用于预估 */
  const { data: pick } = useAutoPick(zone);

  const isAuto = vendorId === "auto";
  const maxCount = (isAuto ? pick?.max_per_order : stock?.max_per_order) ?? 200;
  const minCount = (isAuto ? pick?.min_per_order : stock?.min_per_order) ?? 1;

  /* 具体 zone 的单价（用于预估）· auto 时选最便宜一区 */
  const activeZone = useMemo(() => {
    if (!stock) return null;
    if (stock.zones.length === 1) return stock.zones[0];
    if (zone === "auto") {
      return stock.zones.reduce((a, b) => (a.unit_price <= b.unit_price ? a : b));
    }
    return stock.zones.find((z) => z.zone === zone) ?? stock.zones[0];
  }, [stock, zone]);

  /** 当前生效单价（已含附加费）· auto 走推荐结果 · 具体 vendor 走该区单价 */
  const unitPrice = isAuto ? pick?.unit_price ?? null : activeZone?.unit_price ?? null;
  const available = isAuto ? pick?.available ?? null : activeZone?.available ?? null;
  /** 实际会派到的 vendor 显示名（auto 时来自推荐结果） */
  const effectiveVendorLabel = isAuto
    ? pick?.vendor_label ?? "系统派号"
    : vendorLabel(vendorId, invited);
  const effectiveZone = isAuto ? pick?.zone ?? null : activeZone ? (stock!.zones.length === 1 ? null : activeZone.zone) : null;

  /* 预估费用 · auto 和具体 vendor 都要有（散客默认 auto · 必须能看到花多少） */
  const estimate = useMemo(() => {
    if (unitPrice == null) return null;
    const keyCost = unitPrice * count;
    const singlePullFee = count === 1 ? keyCost * 0.2 : 0;
    const serviceFee = 1_000_000;
    return { keyCost, singlePullFee, serviceFee, total: keyCost + singlePullFee + serviceFee };
  }, [unitPrice, count]);

  const bargain = count === 1;
  const outOfStock = available === 0;

  /* 切换 vendor 时如果 count 超过新 max · 收紧 */
  useEffect(() => {
    if (count > maxCount) setCount(maxCount);
  }, [maxCount, count]);

  /** 确认窗里点「确认提取」才真拉 · couponCode 是本次减免码 */
  const onConfirm = async (couponCode?: string) => {
    await pull.mutateAsync({
      vendor_id: vendorId,
      zone: zone === "auto" ? undefined : zone,
      count,
      coupon_code: couponCode,
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
        {/* vendor 第一（决定单价/库存/质保）· 区域 第二 · 数量 第三 · 一行 md+ · 窄屏堆叠 */}
        <div className="grid grid-cols-1 gap-4 md:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_minmax(0,1fr)]">
          <Field label="vendor">
            <Select value={vendorId} onValueChange={setVendorId}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                {/* 默认项 = 系统派号 · 面板里展示推荐结果和价格（decisions §8.20） */}
                <SelectItem value="auto">系统派号（推荐）</SelectItem>
                {availableVendors.map((v) => (
                  <SelectItem key={v.vendor_id} value={v.vendor_id}>
                    {vendorLabel(v.vendor_id, invited)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label="区域">
            <Select value={zone} onValueChange={(v) => setZone(v as Zone | "auto")}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">让系统选</SelectItem>
                <SelectItem value="us">美国区 (us)</SelectItem>
                <SelectItem value="eu">欧洲区 (eu)</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field label="数量" hint={`${minCount} - ${maxCount}`}>
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
            /* 预估费用卡 · 跟左侧上游状态卡 UI 完全统一 */
            <div className="flex flex-col justify-between rounded-xl border border-hairline bg-bg-elevated/40 p-4 text-label">
              <div>
                <div className="mb-3 font-semibold text-fg">预估费用</div>
                <div className="space-y-1.5">
                  <FeeRow
                    label={unitPrice != null ? `号价 · ${toCredits(unitPrice)} × ${count}` : "号价"}
                    value={`${fmtCredits(estimate.keyCost)} 积分`}
                  />
                  {estimate.singlePullFee > 0 && (
                    <FeeRow
                      label="拉 1 个偏高"
                      value={`+${fmtCredits(estimate.singlePullFee)} 积分`}
                      muted
                    />
                  )}
                  <FeeRow label="服务费" value="1 积分" muted />
                  {/* 通道费只在充值积分时收 · 拉号/提取都是抵扣积分 · decisions §8.21 · 不显示 */}
                </div>
              </div>
              <div className="mt-3 border-t border-hairline pt-2">
                <FeeRow
                  label="小计"
                  value={<strong className="tnum text-fg">{fmtCredits(estimate.total)} 积分</strong>}
                  strong
                />
              </div>
            </div>
          ) : (
            <div className="grid place-items-center rounded-xl border border-hairline bg-bg-elevated/40 p-4 text-label text-fg-tertiary">
              正在算价…
            </div>
          )}
        </div>

        {/* count=1 提示 */}
        {bargain && (
          <Alert tone="neutral" icon={Sparkles} title="拉 2 个及以上单价更低">
            一次只拉 1 个成本偏高
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
              查看历史价格趋势
              <ArrowUpRight className="size-3" />
            </Link>
            <p className="text-label leading-relaxed text-fg-tertiary">
              价格受市场波动影响，会有波动，提取前请仔细核对提取信息。
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
            {outOfStock ? "该区缺货" : `提取 ${count} 个 key`}
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
