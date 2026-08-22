import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Save, Settings2, Zap, ZapOff } from "lucide-react";
import {
  useMe, useUpdateStrategy, useVendorStats,
} from "@/api/hooks";
import { Card, Chip } from "./ui/primitives";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Switch } from "@/components/ui/switch";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { notify } from "@/lib/toast";
import {
  toCredits, vendorLabel,
} from "@/lib/utils";
import type { BusStrategy } from "@/types";

/** 补车策略编辑 · decisions §8.6 · 策略跟车绑不是全局
 *  作为 BusDetail 的一级 tab · 高频编辑（保活 / 每轮 / 单价上限 会日常调）
 *
 *  **1f-refactor(migration 040)** · 撤回 5 个 toggle 镜像模型 · 回归朴素:
 *  auto_refill_enabled / refill_watermark 就是车级值 · 无"跟随全局" ·
 *  全局 default_* 只在建车向导预填 seed · 之后车级独立。
 *  跨车调度护栏走 Preferences 页(auto_refill_daily_budget 等 3 字段)。*/
export function EditStrategyPanel({
  busId, strategy,
}: { busId: string; strategy: BusStrategy }) {
  const { t } = useTranslation("buses");
  const { data: me } = useMe();
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
  const [pref, setPref] = useState(strategy.preferred_vendor ?? "auto");
  const [saved, setSaved] = useState(false);

  /* dirty · 逐字段跟原 strategy 比 · 未改动就不显示保存按钮 */
  const origMaxPrice = strategy.max_unit_price ? String(toCredits(strategy.max_unit_price)) : "";
  const origDailyRound = strategy.daily_round_limit ? String(strategy.daily_round_limit) : "";
  const origDailySpend = strategy.daily_spend_limit ? String(toCredits(strategy.daily_spend_limit)) : "";
  const origPref = strategy.preferred_vendor ?? "auto";
  const dirty = useMemo(
    () =>
      auto !== strategy.auto_refill_enabled ||
      watermark !== strategy.refill_watermark ||
      perRound !== (strategy.per_round_count ?? 3) ||
      maxPrice !== origMaxPrice ||
      dailyRound !== origDailyRound ||
      dailySpend !== origDailySpend ||
      pref !== origPref,
    [
      auto, watermark, perRound, maxPrice, dailyRound, dailySpend, pref,
      strategy.auto_refill_enabled, strategy.refill_watermark, strategy.per_round_count,
      origMaxPrice, origDailyRound, origDailySpend, origPref,
    ],
  );

  const onSave = async () => {
    if (!dirty) return;
    setSaved(false);
    try {
      await upd.mutateAsync({
        auto_refill_enabled: auto,
        refill_watermark: watermark,
        refill_min_count: null,
        per_round_count: perRound,
        max_unit_price: maxPrice ? Number(maxPrice) * 1_000_000 : null,
        daily_round_limit: dailyRound ? Number(dailyRound) : null,
        daily_spend_limit: dailySpend ? Number(dailySpend) * 1_000_000 : null,
        preferred_vendor: pref === "auto" ? null : pref,
      });
      setSaved(true);
      notify.ok({ title: t("common:toast.saved") });
      setTimeout(() => setSaved(false), 2000);
    } catch (err) {
      notify.fail(err, t("common:toast.generic_fail"));
    }
  };

  return (
    <Card className="p-7">
      <div className="mb-5 flex items-start justify-between gap-4">
        <div className="min-w-0 flex-1">
          <h2 className="text-section font-semibold">{t("strategy-panel.title")}</h2>
          <p className="text-label text-fg-tertiary">{t("strategy-panel.sub")}</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {/* 1f-refactor · 明显的"去全局设置"按钮(用户吐槽:灰字链接太弱看不到)
              放跟保存按钮同高 · outline 主色 · 一眼能定位 */}
          <Button asChild variant="subtle" size="sm">
            <Link to="/settings/preferences">
              <Settings2 className="size-3.5" />
              {t("strategy-panel.goto-global")}
            </Link>
          </Button>
          {/* 保存按钮 · 只在 dirty 或刚保存完的 2s toast 期间显示 · 未改动零动作零按钮 */}
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
      </div>

      <div className="space-y-6">
        {/* 主开关 · 是否自动补车 */}
        <div
          className="flex cursor-pointer items-center gap-3 rounded-2xl border border-hairline bg-bg p-4 transition-colors hover:bg-bg-elevated/40"
          onClick={() => setAuto((v) => !v)}
        >
          <span className="shrink-0">
            {auto ? (
              <Zap className="size-4 text-brand-strong" />
            ) : (
              <ZapOff className="size-4 text-fg-tertiary" />
            )}
          </span>
          <div className="min-w-0 flex-1">
            <div className="font-semibold">
              {auto ? t("strategy-panel.auto.on-label") : t("strategy-panel.auto.off-label")}
            </div>
            <div className="mt-0.5 text-label text-fg-tertiary">
              {auto
                ? t("strategy-panel.auto.on-desc")
                : t("strategy-panel.auto.off-desc")}
            </div>
          </div>
          <Switch
            checked={auto}
            onCheckedChange={setAuto}
            onClick={(e) => e.stopPropagation()}
          />
        </div>

        {auto && (
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label={t("strategy-panel.field.watermark")}>
              <Input
                type="number" min={1} value={watermark}
                onChange={(e) => setWatermark(Math.max(1, Number(e.target.value) || 1))}
              />
            </Field>
            <Field label={t("strategy-panel.field.per-round")}>
              <Input
                type="number" min={1} value={perRound}
                onChange={(e) => setPerRound(Math.max(1, Number(e.target.value) || 1))}
              />
            </Field>
          </div>
        )}

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <Field label={t("strategy-panel.field.max-price")}>
            <Input
              type="number" value={maxPrice}
              onChange={(e) => setMaxPrice(e.target.value)}
              placeholder={t("strategy-panel.field.unlimited-placeholder")}
            />
          </Field>
          <Field label={t("strategy-panel.field.daily-round")}>
            <Input
              type="number" value={dailyRound}
              onChange={(e) => setDailyRound(e.target.value)}
              placeholder={t("strategy-panel.field.unlimited-placeholder")}
            />
          </Field>
          <Field label={t("strategy-panel.field.daily-spend")}>
            <Input
              type="number" value={dailySpend}
              onChange={(e) => setDailySpend(e.target.value)}
              placeholder={t("strategy-panel.field.unlimited-placeholder")}
            />
          </Field>
        </div>

        <Field label={t("strategy-panel.field.preferred-vendor")}>
          <Select value={pref} onValueChange={setPref}>
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
        </Field>

        <p className="rounded-xl bg-bg-elevated p-3 text-label text-fg-tertiary">
          <Chip tone="brand" className="mr-2">{t("strategy-panel.tip.chip")}</Chip>
          {t("strategy-panel.tip.text")}
        </p>
      </div>
    </Card>
  );
}
