import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { AlertCircle, ChevronDown, Info, X } from "lucide-react";
import { useCreateBus, useVendorStats } from "@/api/hooks";
import { Chip } from "./ui/primitives";
import { cn, vendorName } from "@/lib/utils";

/** 发起拼车模态：建车 + 首次拉号一步完成
    - 基本：车名 · 数量 · vendor（可让系统选） · count=1 时议价提示
    - 高级折叠：自动补车 · 单价上限 · 日轮次上限 · 日花费上限 · 首选 vendor */
export function StartCarpoolModal({
  open, onClose,
}: { open: boolean; onClose: () => void }) {
  const nav = useNavigate();
  const createBus = useCreateBus();
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  const [name, setName] = useState("我的车");
  const [count, setCount] = useState(3);
  const [vendorId, setVendorId] = useState<string>("");
  const [advOpen, setAdvOpen] = useState(false);
  const [autoRefill, setAutoRefill] = useState(false);
  const [maxPrice, setMaxPrice] = useState("");
  const [dailyRoundLimit, setDailyRoundLimit] = useState("");
  const [dailySpendLimit, setDailySpendLimit] = useState("");
  const [preferredVendor, setPreferredVendor] = useState("");
  const [refillWatermark, setRefillWatermark] = useState(3);
  const [perRoundCount, setPerRoundCount] = useState(3);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    // 打开时锁滚动
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [open, onClose]);

  if (!open) return null;

  const showBargainWarning = count === 1;

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const strategy = advOpen
      ? {
          auto_refill_enabled: autoRefill,
          refill_watermark: refillWatermark,
          refill_min_count: null,
          per_round_count: perRoundCount,
          max_unit_price: maxPrice ? Number(maxPrice) * 1_000_000 : null,
          daily_round_limit: dailyRoundLimit ? Number(dailyRoundLimit) : null,
          daily_spend_limit: dailySpendLimit ? Number(dailySpendLimit) * 1_000_000 : null,
          preferred_vendor: preferredVendor || null,
        }
      : undefined;

    const bus = await createBus.mutateAsync({ name, strategy });
    onClose();
    nav(`/buses/${bus.id}`);
  };

  return (
    <>
      <div
        className="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm"
        onClick={onClose}
      />
      <div className="fixed inset-x-4 top-1/2 z-50 mx-auto max-w-[560px] -translate-y-1/2 rounded-[16px] border border-hairline bg-bg shadow-modal">
        {/* 头 */}
        <div className="flex items-center justify-between border-b border-hairline px-6 py-4">
          <div>
            <h2 className="text-body-lg font-semibold">发起拼车</h2>
            <p className="text-label text-fg-tertiary">建一辆自己的车 · 首次发车一并完成</p>
          </div>
          <button
            onClick={onClose}
            className="grid size-8 place-items-center rounded-lg transition-colors hover:bg-bg-elevated"
            aria-label="关闭"
          >
            <X className="size-4 text-fg-secondary" />
          </button>
        </div>

        <form onSubmit={onSubmit} className="max-h-[calc(100dvh-160px)] space-y-5 overflow-y-auto p-6">
          {/* 车名 */}
          <Field label="车名">
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="给车起个名字"
              className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-medium transition-colors focus:border-brand focus:outline-none"
              required
              maxLength={30}
            />
          </Field>

          {/* 数量 · vendor 并排 */}
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-[140px_1fr]">
            <Field label="拉几个号">
              <input
                type="number"
                min={1}
                max={200}
                value={count}
                onChange={(e) => setCount(Math.max(1, Math.min(200, Number(e.target.value) || 1)))}
                className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-semibold tnum transition-colors focus:border-brand focus:outline-none"
              />
            </Field>
            <Field label="vendor（可选 · 空 = 系统选便宜的）">
              <select
                value={vendorId}
                onChange={(e) => setVendorId(e.target.value)}
                className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-medium transition-colors focus:border-brand focus:outline-none"
              >
                <option value="">让系统选（按有效成本比价）</option>
                {availableVendors.map((v) => (
                  <option key={v.vendor_id} value={v.vendor_id}>
                    {vendorName(v.vendor_id)}
                  </option>
                ))}
              </select>
            </Field>
          </div>

          {/* count=1 时议价提示（decisions §2.15） */}
          {showBargainWarning && (
            <div className="flex items-start gap-2 rounded-lg bg-warn-bg p-3 text-label">
              <AlertCircle className="mt-0.5 size-4 shrink-0 text-warn-fg" />
              <div className="min-w-0 flex-1">
                <div className="font-semibold text-warn-fg">单次拉号 · 加 20% 议价</div>
                <div className="text-fg-secondary">
                  拉 1 个号时收 20% 单次议价费。拉 2 个及以上不收。
                </div>
              </div>
            </div>
          )}

          {/* 高级折叠 */}
          <div className="rounded-lg border border-hairline">
            <button
              type="button"
              onClick={() => setAdvOpen((v) => !v)}
              className="flex w-full items-center justify-between gap-2 px-4 py-3 font-medium transition-colors hover:bg-bg-elevated"
            >
              <span className="flex items-center gap-2">
                <span>高级选项</span>
                <Chip tone="neutral" className="text-[10px]">可选</Chip>
                {!advOpen && (
                  <span className="text-label font-normal text-fg-tertiary">
                    · 自动补车 · 单价上限 · 日限
                  </span>
                )}
              </span>
              <ChevronDown className={cn("size-4 text-fg-tertiary transition-transform", advOpen && "rotate-180")} />
            </button>

            {advOpen && (
              <div className="space-y-4 border-t border-hairline p-4">
                {/* 自动补车开关 · 打开后展开水位 / 每轮个数 */}
                <label className="flex items-start gap-3 rounded-lg bg-bg-elevated p-3">
                  <input
                    type="checkbox"
                    checked={autoRefill}
                    onChange={(e) => setAutoRefill(e.target.checked)}
                    className="mt-1 size-4 accent-brand"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="font-semibold">开启自动补车</div>
                    <div className="text-label text-fg-tertiary">
                      号池活号跌破水位时，系统自动拉一轮补车
                    </div>
                  </div>
                </label>

                {autoRefill && (
                  <div className="grid grid-cols-2 gap-4">
                    <Field label="水位阈值（活号 ≤）" hint="低于此数触发">
                      <input
                        type="number"
                        min={1}
                        value={refillWatermark}
                        onChange={(e) => setRefillWatermark(Number(e.target.value) || 3)}
                        className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-semibold tnum focus:border-brand focus:outline-none"
                      />
                    </Field>
                    <Field label="每轮补几个">
                      <input
                        type="number"
                        min={1}
                        value={perRoundCount}
                        onChange={(e) => setPerRoundCount(Number(e.target.value) || 3)}
                        className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-semibold tnum focus:border-brand focus:outline-none"
                      />
                    </Field>
                  </div>
                )}

                {/* 单价上限 · 日轮次 · 日花费 */}
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                  <Field label="单价上限（积分）" hint="空 = 不限">
                    <input
                      type="number"
                      value={maxPrice}
                      onChange={(e) => setMaxPrice(e.target.value)}
                      placeholder="不限"
                      className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-semibold tnum focus:border-brand focus:outline-none"
                    />
                  </Field>
                  <Field label="日轮次上限" hint="空 = 不限">
                    <input
                      type="number"
                      value={dailyRoundLimit}
                      onChange={(e) => setDailyRoundLimit(e.target.value)}
                      placeholder="不限"
                      className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-semibold tnum focus:border-brand focus:outline-none"
                    />
                  </Field>
                  <Field label="日花费上限（积分）" hint="空 = 不限">
                    <input
                      type="number"
                      value={dailySpendLimit}
                      onChange={(e) => setDailySpendLimit(e.target.value)}
                      placeholder="不限"
                      className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-semibold tnum focus:border-brand focus:outline-none"
                    />
                  </Field>
                </div>

                <Field label="首选 vendor" hint="空 = 每次按比价挑">
                  <select
                    value={preferredVendor}
                    onChange={(e) => setPreferredVendor(e.target.value)}
                    className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-medium focus:border-brand focus:outline-none"
                  >
                    <option value="">让系统比价选</option>
                    {availableVendors.map((v) => (
                      <option key={v.vendor_id} value={v.vendor_id}>
                        {vendorName(v.vendor_id)}
                      </option>
                    ))}
                  </select>
                </Field>
              </div>
            )}
          </div>

          {/* 说明 */}
          <div className="flex items-start gap-2 rounded-lg bg-brand-subtle/50 p-3 text-label">
            <Info className="mt-0.5 size-4 shrink-0 text-brand-strong" />
            <div className="text-fg-secondary">
              建车后自动发首轮车 · <span className="font-semibold text-fg">{count}</span> 个号进池 ·
              可在车详情页调策略、查看号列表、手动再拉
            </div>
          </div>
        </form>

        {/* 底 · 提交栏 */}
        <div className="flex items-center justify-end gap-2 border-t border-hairline px-6 py-4">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-hairline bg-bg px-4 py-2 font-medium text-fg-secondary transition-colors hover:bg-bg-elevated"
          >
            取消
          </button>
          <button
            type="submit"
            onClick={onSubmit}
            disabled={createBus.isPending}
            className="rounded-lg bg-brand px-4 py-2 font-semibold text-white shadow-card transition-opacity hover:opacity-90 disabled:opacity-60"
          >
            {createBus.isPending ? "发车中…" : "发车"}
          </button>
        </div>
      </div>
    </>
  );
}

function Field({
  label, hint, children,
}: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-label font-semibold text-fg-secondary">{label}</span>
        {hint && <span className="text-label text-fg-tertiary">{hint}</span>}
      </div>
      {children}
    </label>
  );
}
