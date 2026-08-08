import { useEffect, useState } from "react";
import { AlertCircle, KeyRound, X } from "lucide-react";
import { usePullForBus, useVendorStats } from "@/api/hooks";
import { toCredits, vendorName } from "@/lib/utils";

/** 车详情立即拉号模态 · 参数用车策略默认，允许覆盖
    - count（默认 per_round_count）
    - vendor（可选，默认 preferred_vendor 或让系统选）
    - count=1 时议价提示 */
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
  const pull = usePullForBus(busId);
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  const [count, setCount] = useState(defaultCount);
  const [vendorId, setVendorId] = useState(preferredVendor ?? "");

  useEffect(() => {
    if (open) {
      setCount(defaultCount);
      setVendorId(preferredVendor ?? "");
    }
  }, [open, defaultCount, preferredVendor]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [open, onClose]);

  if (!open) return null;

  const bargain = count === 1;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    await pull.mutateAsync({ count, vendor_id: vendorId || undefined });
    onClose();
  };

  return (
    <>
      <div className="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm" onClick={onClose} />
      <div className="fixed inset-x-4 top-1/2 z-50 mx-auto max-w-[440px] -translate-y-1/2 rounded-[16px] border border-hairline bg-bg shadow-modal">
        <div className="flex items-center justify-between border-b border-hairline px-6 py-4">
          <div className="flex items-center gap-2">
            <KeyRound className="size-4 text-brand-strong" />
            <h2 className="font-semibold">立即拉号</h2>
          </div>
          <button
            onClick={onClose}
            className="grid size-8 place-items-center rounded-lg transition-colors hover:bg-bg-elevated"
            aria-label="关闭"
          >
            <X className="size-4 text-fg-secondary" />
          </button>
        </div>

        <form onSubmit={onSubmit} className="space-y-4 p-6">
          <label className="block space-y-1.5">
            <span className="text-label font-semibold text-fg-secondary">拉几个号</span>
            <input
              type="number"
              min={1}
              max={200}
              value={count}
              onChange={(e) => setCount(Math.max(1, Math.min(200, Number(e.target.value) || 1)))}
              className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-semibold tnum focus:border-brand focus:outline-none"
            />
          </label>

          <label className="block space-y-1.5">
            <div className="flex items-baseline justify-between gap-2">
              <span className="text-label font-semibold text-fg-secondary">vendor</span>
              {maxUnitPrice && (
                <span className="text-label text-fg-tertiary">
                  单价上限 <span className="font-semibold tnum">{toCredits(maxUnitPrice)}</span> 积分
                </span>
              )}
            </div>
            <select
              value={vendorId}
              onChange={(e) => setVendorId(e.target.value)}
              className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-medium focus:border-brand focus:outline-none"
            >
              <option value="">让系统选（按有效成本比价）</option>
              {availableVendors.map((v) => (
                <option key={v.vendor_id} value={v.vendor_id}>
                  {vendorName(v.vendor_id)}
                </option>
              ))}
            </select>
          </label>

          {bargain && (
            <div className="flex items-start gap-2 rounded-lg bg-warn-bg p-3 text-label">
              <AlertCircle className="mt-0.5 size-4 shrink-0 text-warn-fg" />
              <div className="min-w-0 flex-1">
                <div className="font-semibold text-warn-fg">单次拉号 · 加 20% 议价</div>
                <div className="text-fg-secondary">拉 2 个及以上不收议价费</div>
              </div>
            </div>
          )}
        </form>

        <div className="flex items-center justify-end gap-2 border-t border-hairline px-6 py-4">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-hairline bg-bg px-4 py-2 font-medium text-fg-secondary transition-colors hover:bg-bg-elevated"
          >
            取消
          </button>
          <button
            onClick={onSubmit}
            disabled={pull.isPending}
            className="rounded-lg bg-brand px-4 py-2 font-semibold text-white transition-opacity hover:opacity-90 disabled:opacity-60"
          >
            {pull.isPending ? "拉号中…" : `拉 ${count} 个号`}
          </button>
        </div>
      </div>
    </>
  );
}
