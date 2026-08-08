import { useEffect, useState } from "react";
import { AlertCircle, KeyRound, X } from "lucide-react";
import { usePull, useVendorStats } from "@/api/hooks";
import { vendorName } from "@/lib/utils";

/** 次入口拉号模态（/extract 页触发）· 拉完进 record group 待派去向 */
export function PullExtractModal({
  open, onClose,
}: { open: boolean; onClose: () => void }) {
  const pull = usePull();
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  const [count, setCount] = useState(1);
  const [vendorId, setVendorId] = useState("");

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
            <KeyRound className="size-4 text-warn-fg" />
            <h2 className="font-semibold">提取 key</h2>
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
          <p className="text-label text-fg-tertiary">
            拉出来的 key 会进"待派"列表 · 之后你派 3 种去向：进车 / 推我的号池 / 拿走
          </p>

          <label className="block space-y-1.5">
            <span className="text-label font-semibold text-fg-secondary">数量</span>
            <input
              type="number" min={1} max={200} value={count}
              onChange={(e) => setCount(Math.max(1, Math.min(200, Number(e.target.value) || 1)))}
              className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-semibold tnum focus:border-brand focus:outline-none"
            />
          </label>

          <label className="block space-y-1.5">
            <span className="text-label font-semibold text-fg-secondary">vendor</span>
            <select
              value={vendorId} onChange={(e) => setVendorId(e.target.value)}
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
              <div>
                <div className="font-semibold text-warn-fg">单次拉号 · 加 20% 议价</div>
                <div className="text-fg-secondary">拉 2 个及以上不收议价费</div>
              </div>
            </div>
          )}
        </form>

        <div className="flex items-center justify-end gap-2 border-t border-hairline px-6 py-4">
          <button
            type="button" onClick={onClose}
            className="rounded-lg border border-hairline bg-bg px-4 py-2 font-medium text-fg-secondary hover:bg-bg-elevated"
          >
            取消
          </button>
          <button
            onClick={onSubmit} disabled={pull.isPending}
            className="rounded-lg bg-brand px-4 py-2 font-semibold text-white transition-opacity hover:opacity-90 disabled:opacity-60"
          >
            {pull.isPending ? "拉号中…" : `拉 ${count} 个 key`}
          </button>
        </div>
      </div>
    </>
  );
}
