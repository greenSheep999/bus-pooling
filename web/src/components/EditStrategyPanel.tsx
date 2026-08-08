import { useState } from "react";
import { Save, Zap, ZapOff } from "lucide-react";
import { useUpdateStrategy, useVendorStats } from "@/api/hooks";
import { Card, Chip } from "./ui/primitives";
import { toCredits, vendorName } from "@/lib/utils";
import type { BusStrategy } from "@/types";

/** 补车策略编辑 · decisions §8.6 · 策略跟车绑不是全局
    字段：auto_refill · refill_watermark · per_round_count · max_unit_price
         · daily_round_limit · daily_spend_limit · preferred_vendor */
export function EditStrategyPanel({
  busId, strategy,
}: { busId: string; strategy: BusStrategy }) {
  const upd = useUpdateStrategy(busId);
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  const [auto, setAuto] = useState(strategy.auto_refill_enabled);
  const [watermark, setWatermark] = useState(strategy.refill_watermark);
  const [perRound, setPerRound] = useState(strategy.per_round_count ?? 3);
  const [maxPrice, setMaxPrice] = useState(
    strategy.max_unit_price ? String(toCredits(strategy.max_unit_price)) : "",
  );
  const [dailyRound, setDailyRound] = useState(
    strategy.daily_round_limit ? String(strategy.daily_round_limit) : "",
  );
  const [dailySpend, setDailySpend] = useState(
    strategy.daily_spend_limit ? String(toCredits(strategy.daily_spend_limit)) : "",
  );
  const [pref, setPref] = useState(strategy.preferred_vendor ?? "");
  const [saved, setSaved] = useState(false);

  const onSave = async () => {
    setSaved(false);
    await upd.mutateAsync({
      auto_refill_enabled: auto,
      refill_watermark: watermark,
      refill_min_count: null,
      per_round_count: perRound,
      max_unit_price: maxPrice ? Number(maxPrice) * 1_000_000 : null,
      daily_round_limit: dailyRound ? Number(dailyRound) : null,
      daily_spend_limit: dailySpend ? Number(dailySpend) * 1_000_000 : null,
      preferred_vendor: pref || null,
    });
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  return (
    <Card className="p-7">
      <div className="mb-5 flex items-start justify-between gap-4">
        <div>
          <h2 className="text-section font-semibold">补车策略</h2>
          <p className="text-label text-fg-tertiary">
            策略跟这辆车绑 · 不影响其他车
          </p>
        </div>
        <button
          onClick={onSave}
          disabled={upd.isPending}
          className="flex shrink-0 items-center gap-1.5 rounded-lg bg-brand px-4 py-2 font-semibold text-white shadow-card transition-opacity hover:opacity-90 disabled:opacity-60"
        >
          <Save className="size-4" />
          {upd.isPending ? "保存中…" : saved ? "已保存 ✓" : "保存策略"}
        </button>
      </div>

      <div className="space-y-6">
        {/* 自动补车开关 */}
        <label className="flex items-start gap-3 rounded-lg bg-bg-elevated p-4">
          <input
            type="checkbox"
            checked={auto}
            onChange={(e) => setAuto(e.target.checked)}
            className="mt-1 size-4 accent-brand"
          />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 font-semibold">
              {auto ? (
                <><Zap className="size-4 text-brand-strong" /> 自动补车</>
              ) : (
                <><ZapOff className="size-4 text-fg-tertiary" /> 手动模式</>
              )}
            </div>
            <div className="mt-0.5 text-label text-fg-tertiary">
              {auto
                ? "号池活号跌破水位 · 系统自动拉一轮补车"
                : "号少时只提醒 · 不自动拉 · 你手动决定何时拉号"}
            </div>
          </div>
        </label>

        {auto && (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label="水位阈值（活号 ≤）" hint="低于此数触发补车">
              <NumInput value={watermark} onChange={setWatermark} min={1} />
            </Field>
            <Field label="每轮补几个">
              <NumInput value={perRound} onChange={setPerRound} min={1} />
            </Field>
          </div>
        )}

        {/* 单价 · 日限 · 首选 */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Field label="单价上限（积分）" hint="空 = 不限">
            <TextInput value={maxPrice} onChange={setMaxPrice} placeholder="不限" numeric />
          </Field>
          <Field label="日轮次上限" hint="空 = 不限">
            <TextInput value={dailyRound} onChange={setDailyRound} placeholder="不限" numeric />
          </Field>
          <Field label="日花费上限（积分）" hint="空 = 不限">
            <TextInput value={dailySpend} onChange={setDailySpend} placeholder="不限" numeric />
          </Field>
        </div>

        <Field label="首选 vendor" hint="空 = 每次按比价挑">
          <select
            value={pref}
            onChange={(e) => setPref(e.target.value)}
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

        <p className="rounded-lg bg-bg-elevated p-3 text-label text-fg-tertiary">
          <Chip tone="brand" className="mr-2">Tip</Chip>
          策略变更立即生效 · 下次自动补车 / 手动拉号都按新策略走
        </p>
      </div>
    </Card>
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

function NumInput({
  value, onChange, min,
}: { value: number; onChange: (v: number) => void; min: number }) {
  return (
    <input
      type="number"
      min={min}
      value={value}
      onChange={(e) => onChange(Math.max(min, Number(e.target.value) || min))}
      className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-semibold tnum focus:border-brand focus:outline-none"
    />
  );
}

function TextInput({
  value, onChange, placeholder, numeric,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  numeric?: boolean;
}) {
  return (
    <input
      type={numeric ? "number" : "text"}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className="w-full rounded-lg border border-hairline bg-bg px-3 py-2 font-semibold tnum focus:border-brand focus:outline-none"
    />
  );
}
