import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Save, Zap, ZapOff } from "lucide-react";
import {
  useGlobalStrategy, useMe, useUpdateStrategy, useVendorStats,
} from "@/api/hooks";
import { Card, Chip, Segmented } from "./ui/primitives";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import {
  fmtCredits, MICRO, toCredits, vendorLabel,
} from "@/lib/utils";
import type { BusStrategy } from "@/types";

/** 补车策略编辑 · docs/15-scheduling §4.3.5.2 二态 toggle UI · sprint-1f
 *
 *  **覆盖字段**(auto/watermark/min_count/perRound/preferred_vendor)带 Segmented toggle:
 *    - "跟随全局默认" → 保存时发 null · 输入框灰显只读显示当前全局值
 *    - "覆盖本车"     → 保存时发用户填的值(含 0/false) · 输入框可编辑
 *
 *  **硬上限字段**(max_unit_price)无 toggle · 直接输入 · 旁边显示
 *    "实际生效 min(车级, 全局)"(§4.3.5.1)
 *
 *  策略跟车绑不是全局 · decisions §8.6 */
export function EditStrategyPanel({
  busId, strategy,
}: { busId: string; strategy: BusStrategy }) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
  const { data: gs } = useGlobalStrategy();
  const upd = useUpdateStrategy(busId);
  const { data: vendors } = useVendorStats();
  const availableVendors = (vendors?.stats ?? []).filter((v) => !v.out_of_stock);

  /* ── 每个"覆盖字段"独立记 mode(跟随/覆盖)+ value(用户填的) ──
     mode 决定保存时发 null 还是发 value · CLAUDE §7.1 别拿"是否非空"判"是否覆盖" */
  const [autoMode, setAutoMode] = useState<"inherit" | "override">(
    strategy.auto_refill_enabled === null ? "inherit" : "override",
  );
  const [autoVal, setAutoVal] = useState<boolean>(
    strategy.auto_refill_enabled ?? gs?.default_auto_refill_enabled ?? false,
  );

  const [watermarkMode, setWatermarkMode] = useState<"inherit" | "override">(
    strategy.refill_watermark === null ? "inherit" : "override",
  );
  const [watermarkVal, setWatermarkVal] = useState<number>(
    strategy.refill_watermark ?? gs?.default_refill_watermark ?? 3,
  );

  const [minCountMode, setMinCountMode] = useState<"inherit" | "override">(
    strategy.refill_min_count === null ? "inherit" : "override",
  );
  const [minCountVal, setMinCountVal] = useState<number>(
    strategy.refill_min_count ?? gs?.default_refill_min_count ?? 3,
  );

  const [perRoundMode, setPerRoundMode] = useState<"inherit" | "override">(
    strategy.per_round_count === null ? "inherit" : "override",
  );
  const [perRoundVal, setPerRoundVal] = useState<number>(
    strategy.per_round_count ?? gs?.per_round_count ?? 3,
  );

  const [prefMode, setPrefMode] = useState<"inherit" | "override">(
    strategy.preferred_vendor === null ? "inherit" : "override",
  );
  const [prefVal, setPrefVal] = useState<string>(strategy.preferred_vendor ?? "auto");

  /* ── 硬上限 · 无 toggle · null = 不加严 ── */
  const [maxPrice, setMaxPrice] = useState(
    strategy.max_unit_price ? String(toCredits(strategy.max_unit_price)) : "",
  );

  const [saved, setSaved] = useState(false);

  /* dirty · 各字段比原态 */
  const dirty = useMemo(() => {
    const inheritOrOverride = (
      currMode: "inherit" | "override",
      currVal: unknown,
      orig: unknown,
    ) => {
      if (currMode === "inherit") return orig !== null;
      return currVal !== orig;
    };

    return (
      inheritOrOverride(autoMode, autoVal, strategy.auto_refill_enabled) ||
      inheritOrOverride(watermarkMode, watermarkVal, strategy.refill_watermark) ||
      inheritOrOverride(minCountMode, minCountVal, strategy.refill_min_count) ||
      inheritOrOverride(perRoundMode, perRoundVal, strategy.per_round_count) ||
      inheritOrOverride(
        prefMode,
        prefVal === "auto" ? null : prefVal,
        strategy.preferred_vendor,
      ) ||
      maxPrice !== (strategy.max_unit_price ? String(toCredits(strategy.max_unit_price)) : "")
    );
  }, [
    autoMode, autoVal, watermarkMode, watermarkVal, minCountMode, minCountVal,
    perRoundMode, perRoundVal, prefMode, prefVal, maxPrice, strategy,
  ]);

  const onSave = async () => {
    if (!dirty) return;
    setSaved(false);
    await upd.mutateAsync({
      auto_refill_enabled: autoMode === "inherit" ? null : autoVal,
      refill_watermark: watermarkMode === "inherit" ? null : watermarkVal,
      refill_min_count: minCountMode === "inherit" ? null : minCountVal,
      per_round_count: perRoundMode === "inherit" ? null : perRoundVal,
      max_unit_price: maxPrice ? Number(maxPrice) * MICRO : null,
      preferred_vendor:
        prefMode === "inherit" ? null : prefVal === "auto" ? null : prefVal,
      /* daily_* 车级已废弃(§4.1) · 不再发 · 后端只读全局 */
    });
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  /* ── 展示辅助:实际生效值(§4.3.5.1) ── */
  const effectiveAutoText =
    autoMode === "inherit"
      ? gs?.default_auto_refill_enabled
        ? t("strategy-panel.follow-global-on")
        : t("strategy-panel.follow-global-off")
      : autoVal
        ? t("strategy-panel.auto.on-label")
        : t("strategy-panel.auto.off-label");

  const effectiveWatermark =
    watermarkMode === "inherit" ? (gs?.default_refill_watermark ?? 0) : watermarkVal;

  const effectiveMinCount = minCountMode === "inherit" ? gs?.default_refill_min_count ?? null : minCountVal;

  const effectivePerRound = perRoundMode === "inherit" ? gs?.per_round_count ?? 3 : perRoundVal;

  const globalPrefLabel = gs?.preferred_vendor
    ? vendorLabel(gs.preferred_vendor, me?.tier)
    : t("strategy-panel.preferred.auto");

  /* max_unit_price 实际生效 = min(车级, 全局) · 两个都可能是 null */
  const busCapMicro = maxPrice ? Number(maxPrice) * MICRO : null;
  const globalCapMicro = gs?.max_unit_price ?? null;
  const effectiveCapMicro =
    busCapMicro === null
      ? globalCapMicro
      : globalCapMicro === null
        ? busCapMicro
        : Math.min(busCapMicro, globalCapMicro);

  const modeOptions = [
    { value: "inherit" as const, label: t("strategy-panel.follow-global") },
    { value: "override" as const, label: t("strategy-panel.override-bus") },
  ];

  const InheritChip = () => (
    <Chip tone="neutral" className="ml-2">
      {t("strategy-panel.follow-global-chip")}
    </Chip>
  );

  return (
    <Card className="p-7">
      <div className="mb-5 flex items-start justify-between gap-4">
        <div>
          <h2 className="text-section font-semibold">{t("strategy-panel.title")}</h2>
          <p className="text-label text-fg-tertiary">{t("strategy-panel.sub")}</p>
        </div>
        {(dirty || saved) && (
          <Button onClick={onSave} disabled={!dirty || upd.isPending}>
            <Save />
            {upd.isPending
              ? t("strategy-panel.action.saving")
              : saved
                ? t("strategy-panel.action.saved")
                : t("strategy-panel.action.save")}
          </Button>
        )}
      </div>

      <div className="space-y-6">
        {/* ── 自动补车总开关 ── */}
        <div className="rounded-2xl border border-hairline bg-bg p-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              {autoMode === "override" && autoVal ? (
                <Zap className="size-4 text-brand-strong" />
              ) : (
                <ZapOff className="size-4 text-fg-tertiary" />
              )}
              <span className="font-semibold">{t("strategy-panel.field.auto")}</span>
              {autoMode === "inherit" && <InheritChip />}
            </div>
            <Segmented value={autoMode} onChange={setAutoMode} options={modeOptions} />
          </div>
          <p className="text-label text-fg-tertiary">
            {autoMode === "inherit"
              ? t("strategy-panel.current-global-value", { value: effectiveAutoText })
              : autoVal
                ? t("strategy-panel.auto.on-desc")
                : t("strategy-panel.auto.off-desc")}
          </p>
          {autoMode === "override" && (
            <div className="mt-3 flex gap-2">
              <button
                type="button"
                onClick={() => setAutoVal(true)}
                className={`rounded-lg border px-3 py-1.5 text-label font-medium transition-colors ${
                  autoVal
                    ? "border-brand-strong bg-brand-strong text-white"
                    : "border-hairline bg-bg text-fg-secondary hover:border-brand"
                }`}
              >
                {t("strategy-panel.auto.on-label")}
              </button>
              <button
                type="button"
                onClick={() => setAutoVal(false)}
                className={`rounded-lg border px-3 py-1.5 text-label font-medium transition-colors ${
                  !autoVal
                    ? "border-brand-strong bg-brand-strong text-white"
                    : "border-hairline bg-bg text-fg-secondary hover:border-brand"
                }`}
              >
                {t("strategy-panel.auto.off-label")}
              </button>
            </div>
          )}
        </div>

        {/* ── 水位 + min_count ── */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <ToggleField
            label={t("strategy-panel.field.watermark")}
            mode={watermarkMode}
            onModeChange={setWatermarkMode}
            modeOptions={modeOptions}
            globalValue={effectiveWatermark}
            globalLabel={t("strategy-panel.current-global-value", { value: effectiveWatermark })}
          >
            <Input
              type="number"
              min={0}
              value={watermarkVal}
              onChange={(e) => setWatermarkVal(Math.max(0, Number(e.target.value) || 0))}
            />
          </ToggleField>

          <ToggleField
            label={t("strategy-panel.field.min-count")}
            mode={minCountMode}
            onModeChange={setMinCountMode}
            modeOptions={modeOptions}
            globalValue={effectiveMinCount}
            globalLabel={
              effectiveMinCount == null
                ? t("strategy-panel.current-global-gap-fallback")
                : t("strategy-panel.current-global-value", { value: effectiveMinCount })
            }
          >
            <Input
              type="number"
              min={1}
              value={minCountVal}
              onChange={(e) => setMinCountVal(Math.max(1, Number(e.target.value) || 1))}
            />
          </ToggleField>
        </div>

        {/* ── per_round + preferred_vendor ── */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <ToggleField
            label={t("strategy-panel.field.per-round")}
            mode={perRoundMode}
            onModeChange={setPerRoundMode}
            modeOptions={modeOptions}
            globalValue={effectivePerRound}
            globalLabel={t("strategy-panel.current-global-value", { value: effectivePerRound })}
          >
            <Input
              type="number"
              min={1}
              value={perRoundVal}
              onChange={(e) => setPerRoundVal(Math.max(1, Number(e.target.value) || 1))}
            />
          </ToggleField>

          <ToggleField
            label={t("strategy-panel.field.preferred-vendor")}
            mode={prefMode}
            onModeChange={setPrefMode}
            modeOptions={modeOptions}
            globalValue={globalPrefLabel}
            globalLabel={t("strategy-panel.current-global-value", { value: globalPrefLabel })}
          >
            <Select value={prefVal} onValueChange={setPrefVal}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">{t("strategy-panel.preferred.auto")}</SelectItem>
                {availableVendors.map((v) => (
                  <SelectItem key={v.vendor_id} value={v.vendor_id}>
                    {vendorLabel(v.vendor_id, me?.tier)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </ToggleField>
        </div>

        {/* ── 硬上限 max_unit_price · 无 toggle · 显示 min(车级, 全局) ── */}
        <div className="rounded-2xl border border-hairline bg-bg p-4">
          <Field label={t("strategy-panel.field.max-price")}>
            <Input
              type="number"
              value={maxPrice}
              onChange={(e) => setMaxPrice(e.target.value)}
              placeholder={t("strategy-panel.field.unlimited-placeholder")}
            />
          </Field>
          <p className="mt-2 text-label text-fg-tertiary">
            {t("strategy-panel.hard-limit-note", {
              value:
                effectiveCapMicro == null
                  ? t("strategy-panel.field.unlimited-placeholder")
                  : `${fmtCredits(effectiveCapMicro)} ${t("strategy-panel.credits-unit")}`,
            })}
          </p>
        </div>

        <p className="rounded-xl bg-bg-elevated p-3 text-label text-fg-tertiary">
          <Chip tone="brand" className="mr-2">{t("strategy-panel.tip.chip")}</Chip>
          {t("strategy-panel.tip.text")}
        </p>
      </div>
    </Card>
  );
}

/** 通用二态 toggle 字段 · 跟随全局(灰显 chip)/ 覆盖本车(可编辑) */
function ToggleField({
  label, mode, onModeChange, modeOptions, globalLabel, children,
}: {
  label: string;
  mode: "inherit" | "override";
  onModeChange: (v: "inherit" | "override") => void;
  modeOptions: { value: "inherit" | "override"; label: string }[];
  globalValue: unknown;
  globalLabel: string;
  children: React.ReactNode;
}) {
  const { t } = useTranslation("buses");
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-label font-semibold text-fg-secondary">{label}</span>
        <Segmented value={mode} onChange={onModeChange} options={modeOptions} />
      </div>
      {mode === "inherit" ? (
        <div className="rounded-lg border border-dashed border-hairline bg-bg-elevated/50 px-3 py-2 text-label text-fg-tertiary">
          <span className="mr-1 font-medium text-fg-secondary">
            {t("strategy-panel.follow-global-chip")}
          </span>
          {globalLabel}
        </div>
      ) : (
        children
      )}
    </div>
  );
}
